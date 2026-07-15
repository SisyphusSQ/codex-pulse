package liveindex

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io/fs"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/SisyphusSQ/codex-pulse/internal/codex/logs"
	"github.com/SisyphusSQ/codex-pulse/internal/indexer"
	"github.com/SisyphusSQ/codex-pulse/internal/store"
)

const (
	defaultReadChunkBytes = 256 << 10
	maxReadChunkBytes     = 4 << 20
	maxSliceFiles         = int64(1_000_000)
	maxSliceBytes         = int64(1 << 50)
	maxSliceActive        = 24 * time.Hour
	liveJobIDPrefix       = "live-"
	liveResumePrefix      = "live-resume-"
)

var (
	ErrInvalidRuntime     = errors.New("invalid live index runtime")
	ErrInvalidRequest     = errors.New("invalid live index request")
	ErrInvalidSliceBudget = errors.New("invalid live index slice budget")
	ErrRunAlreadyActive   = errors.New("live index run is already active")
	ErrSourceUnavailable  = errors.New("live index source is unavailable")
	errSliceTimeBudget    = errors.New("live index time budget exhausted")
)

type Config struct {
	Repository     *store.Repository
	Clock          func() time.Time
	ReadChunkBytes int
}

type LiveRequest struct {
	RequestID      string
	HomeGeneration int64
	HomePath       string
	HomeDeviceID   string
	HomeInode      int64
	Action         logs.ReconcileAction
	RequestedAtMS  int64
}

type SliceBudget struct {
	MaxFiles  int64
	MaxBytes  int64
	MaxActive time.Duration
}

type SliceStopReason string

const (
	SliceStopNone       SliceStopReason = ""
	SliceStopCompleted  SliceStopReason = "completed"
	SliceStopFileBudget SliceStopReason = "file_budget"
	SliceStopByteBudget SliceStopReason = "byte_budget"
	SliceStopTimeBudget SliceStopReason = "time_budget"
)

type SliceReport struct {
	store.JobRun
	FilesProcessed int64
	BytesRead      int64
	Active         time.Duration
	Complete       bool
	ExhaustedBy    SliceStopReason
}

type Runtime struct {
	repository     *store.Repository
	clock          func() time.Time
	readChunkBytes int

	timeMu sync.Mutex
	lastMS int64
	mu     sync.Mutex
	active map[string]struct{}
}

func New(config Config) (*Runtime, error) {
	if config.Repository == nil || config.ReadChunkBytes < 0 || config.ReadChunkBytes > maxReadChunkBytes {
		return nil, ErrInvalidRuntime
	}
	if config.Clock == nil {
		config.Clock = time.Now
	}
	if config.ReadChunkBytes == 0 {
		config.ReadChunkBytes = defaultReadChunkBytes
	}
	return &Runtime{
		repository: config.Repository, clock: config.Clock,
		readChunkBytes: config.ReadChunkBytes, active: make(map[string]struct{}),
	}, nil
}

func (runtime *Runtime) Start(ctx context.Context, request LiveRequest) (store.JobRun, error) {
	if runtime == nil || runtime.repository == nil {
		return store.JobRun{}, ErrInvalidRuntime
	}
	facts, err := liveFactsFromRequest(request)
	if err != nil {
		return store.JobRun{}, err
	}
	jobID := stableID(liveJobIDPrefix, request.RequestID)
	facts.JobID = jobID
	job := store.JobRun{
		JobID: jobID, JobType: "codex_live_index", RequestedBy: "live-reconcile", Priority: 20,
		State: store.JobQueued, Phase: store.JobPhaseLive,
		CreatedAtMS: request.RequestedAtMS, UpdatedAtMS: request.RequestedAtMS,
	}
	existingJob, existingFacts, readErr := runtime.repository.LiveScanRun(ctx, jobID)
	if readErr == nil {
		if liveStartMatches(existingJob, existingFacts, job, facts) {
			return existingJob, nil
		}
		return store.JobRun{}, store.ErrLiveScanConflict
	}
	if !errors.Is(readErr, store.ErrNotFound) {
		return store.JobRun{}, readErr
	}
	metadata, err := logs.NewHomeProbe().Probe(ctx, request.HomePath)
	if err != nil {
		return store.JobRun{}, err
	}
	if metadata.Path != request.HomePath || metadata.DeviceID != request.HomeDeviceID ||
		metadata.Inode != request.HomeInode {
		return store.JobRun{}, ErrInvalidRequest
	}
	if err := runtime.repository.CreateLiveScanJob(ctx, job, facts); err != nil {
		return store.JobRun{}, err
	}
	stored, _, err := runtime.repository.LiveScanRun(ctx, jobID)
	return stored, err
}

func (runtime *Runtime) RunSlice(
	ctx context.Context,
	jobID string,
	budget SliceBudget,
) (SliceReport, error) {
	if runtime == nil || runtime.repository == nil || jobID == "" {
		return SliceReport{}, ErrInvalidRuntime
	}
	if !validSliceBudget(budget) {
		return SliceReport{}, ErrInvalidSliceBudget
	}
	job, facts, err := runtime.repository.LiveScanRun(ctx, jobID)
	if err != nil {
		return SliceReport{}, err
	}
	if job.State == store.JobSucceeded {
		return SliceReport{JobRun: job, Complete: true, ExhaustedBy: SliceStopCompleted}, nil
	}
	if job.State != store.JobQueued && job.State != store.JobRunning {
		return SliceReport{JobRun: job}, ErrSourceUnavailable
	}
	if !runtime.register(jobID) {
		return SliceReport{JobRun: job}, ErrRunAlreadyActive
	}
	defer runtime.release(jobID)
	started := runtime.clock()
	if err := ctx.Err(); err != nil {
		return SliceReport{JobRun: job}, err
	}
	job, err = runtime.promote(ctx, job, facts)
	if err != nil {
		return SliceReport{}, err
	}
	if runtime.clock().Sub(started) >= budget.MaxActive {
		return SliceReport{
			JobRun: job, FilesProcessed: 1, Active: runtime.elapsed(started),
			ExhaustedBy: SliceStopTimeBudget,
		}, nil
	}

	action := reconcileActionFromFacts(facts)
	ingester, err := indexer.New(runtime.repository)
	if err != nil {
		return runtime.fail(ctx, job, facts, started, 0, err)
	}
	stream, err := ingester.Open(ctx, indexer.OpenRequest{
		Action: action, AtMS: runtime.nowAfter(job.UpdatedAtMS),
	})
	if err != nil {
		return runtime.fail(ctx, job, facts, started, 0, err)
	}
	cursor, err := stream.Cursor()
	if err != nil {
		return runtime.fail(ctx, job, facts, started, 0, err)
	}
	reader, err := logs.NewConfirmedSnapshotReader(
		facts.HomePath, facts.HomeDeviceID, facts.HomeInode, runtime.readChunkBytes,
	)
	if err != nil {
		return runtime.fail(ctx, job, facts, started, 0, err)
	}
	snapshot := snapshotFromFingerprint(facts.Current)
	readResult, readErr := reader.ReadLimited(
		ctx, snapshot, cursor.CommittedOffset, budget.MaxBytes,
		func(chunk []byte, eof bool) error {
			result, feedErr := stream.Feed(ctx, chunk, eof, runtime.nowAfter(job.UpdatedAtMS))
			if feedErr != nil {
				return feedErr
			}
			if result.Committed {
				if err := runtime.syncProgress(
					ctx, jobID, result.Cursor.Generation, result.Cursor.Checkpoint.CommittedOffset,
				); err != nil {
					return err
				}
			}
			if !eof && runtime.clock().Sub(started) >= budget.MaxActive {
				return logs.StopSnapshotRead(errSliceTimeBudget)
			}
			return nil
		},
	)
	if errors.Is(readErr, errSliceTimeBudget) {
		job, _, err = runtime.repository.LiveScanRun(context.WithoutCancel(ctx), jobID)
		return SliceReport{
			JobRun: job, FilesProcessed: 1, BytesRead: readResult.BytesRead,
			Active: runtime.elapsed(started), ExhaustedBy: SliceStopTimeBudget,
		}, err
	}
	if readErr != nil {
		return runtime.fail(ctx, job, facts, started, readResult.BytesRead, readErr)
	}
	if !readResult.EOF {
		job, _, err = runtime.repository.LiveScanRun(context.WithoutCancel(ctx), jobID)
		return SliceReport{
			JobRun: job, FilesProcessed: 1, BytesRead: readResult.BytesRead,
			Active: runtime.elapsed(started), ExhaustedBy: SliceStopByteBudget,
		}, err
	}
	latest, err := stream.Cursor()
	if err != nil {
		return runtime.fail(ctx, job, facts, started, readResult.BytesRead, err)
	}
	if err := runtime.syncProgress(ctx, jobID, latest.Generation, latest.CommittedOffset); err != nil {
		return runtime.fail(ctx, job, facts, started, readResult.BytesRead, err)
	}
	job, _, err = runtime.repository.LiveScanRun(ctx, jobID)
	if err != nil {
		return SliceReport{}, err
	}
	job, err = runtime.succeed(ctx, job)
	if err != nil {
		return SliceReport{}, err
	}
	return SliceReport{
		JobRun: job, FilesProcessed: 1, BytesRead: readResult.BytesRead,
		Active: runtime.elapsed(started), Complete: true, ExhaustedBy: SliceStopCompleted,
	}, nil
}

func (runtime *Runtime) Interrupt(
	ctx context.Context,
	jobID string,
	class store.RuntimeErrorClass,
) error {
	if runtime == nil || runtime.repository == nil || jobID == "" {
		return ErrInvalidRuntime
	}
	job, _, err := runtime.repository.LiveScanRun(ctx, jobID)
	if err != nil {
		return err
	}
	if job.State == store.JobInterrupted {
		return nil
	}
	if job.State != store.JobQueued && job.State != store.JobRunning {
		return ErrSourceUnavailable
	}
	return runtime.repository.TransitionJobRun(ctx, store.JobTransition{
		JobID: job.JobID, ExpectedState: job.State, State: store.JobInterrupted,
		Phase: store.JobPhaseLive, ProgressCurrent: job.ProgressCurrent,
		ProgressTotal: job.ProgressTotal, ResumeCursor: job.ResumeCursor,
		ErrorClass: &class, AtMS: runtime.nowAfter(job.UpdatedAtMS),
	})
}

func (runtime *Runtime) Recover(ctx context.Context, jobID string) (store.JobRun, error) {
	if runtime == nil || runtime.repository == nil || jobID == "" {
		return store.JobRun{}, ErrInvalidRuntime
	}
	job, facts, err := runtime.repository.LiveScanRun(ctx, jobID)
	if err != nil {
		return store.JobRun{}, err
	}
	if job.State == store.JobSucceeded || job.State == store.JobFailed || job.State == store.JobCancelled {
		return job, nil
	}
	if job.State == store.JobQueued || job.State == store.JobRunning {
		if err := runtime.Interrupt(ctx, jobID, store.RuntimeErrorUnknown); err != nil {
			return store.JobRun{}, err
		}
		job, facts, err = runtime.repository.LiveScanRun(ctx, jobID)
		if err != nil {
			return store.JobRun{}, err
		}
	}
	if job.State != store.JobInterrupted {
		return store.JobRun{}, ErrSourceUnavailable
	}
	resumedID := stableID(liveResumePrefix, jobID)
	requestID := stableID("resume-request-", jobID)
	atMS := job.UpdatedAtMS + 1
	oldID := job.JobID
	resumed := store.JobRun{
		JobID: resumedID, JobType: job.JobType, RequestedBy: job.RequestedBy, Priority: job.Priority,
		State: store.JobQueued, Phase: store.JobPhaseLive,
		ResumeOfJobID: &oldID, CreatedAtMS: atMS,
		ProgressCurrent: cloneInt64(job.ProgressCurrent), ProgressTotal: cloneInt64(job.ProgressTotal),
		ResumeCursor: cloneCursor(job.ResumeCursor), UpdatedAtMS: atMS,
	}
	facts.JobID = resumedID
	facts.RequestID = requestID
	facts.UpdatedAtMS = atMS
	if err := runtime.repository.ResumeLiveScanJob(ctx, jobID, resumed, facts); err != nil {
		return store.JobRun{}, err
	}
	return resumed, nil
}

func (runtime *Runtime) promote(
	ctx context.Context,
	job store.JobRun,
	facts store.LiveScanJob,
) (store.JobRun, error) {
	if job.State == store.JobRunning {
		return job, nil
	}
	current, total := int64(0), facts.Current.SizeBytes
	if job.ProgressCurrent != nil {
		current = *job.ProgressCurrent
	}
	if err := runtime.repository.TransitionJobRun(ctx, store.JobTransition{
		JobID: job.JobID, ExpectedState: store.JobQueued, State: store.JobRunning,
		Phase: store.JobPhaseLive, ProgressCurrent: &current, ProgressTotal: &total,
		ResumeCursor: job.ResumeCursor, AtMS: runtime.nowAfter(job.UpdatedAtMS),
	}); err != nil {
		return store.JobRun{}, err
	}
	value, _, err := runtime.repository.LiveScanRun(ctx, job.JobID)
	return value, err
}

func (runtime *Runtime) syncProgress(
	ctx context.Context,
	jobID string,
	generation int64,
	committedOffset int64,
) error {
	job, facts, err := runtime.repository.LiveScanRun(ctx, jobID)
	if err != nil {
		return err
	}
	current, total := committedOffset, facts.Current.SizeBytes
	if job.ProgressCurrent != nil && *job.ProgressCurrent == current && job.ResumeCursor != nil &&
		job.ResumeCursor.Generation == generation && job.ResumeCursor.Offset == current {
		return nil
	}
	return runtime.repository.TransitionJobRun(ctx, store.JobTransition{
		JobID: job.JobID, ExpectedState: store.JobRunning, State: store.JobRunning,
		Phase: store.JobPhaseLive, ProgressCurrent: &current, ProgressTotal: &total,
		ResumeCursor: &store.JobCursor{Generation: generation, Offset: current},
		AtMS:         runtime.nowAfter(job.UpdatedAtMS),
	})
}

func (runtime *Runtime) succeed(ctx context.Context, job store.JobRun) (store.JobRun, error) {
	if err := runtime.repository.TransitionJobRun(ctx, store.JobTransition{
		JobID: job.JobID, ExpectedState: store.JobRunning, State: store.JobSucceeded,
		Phase: store.JobPhaseLive, ProgressCurrent: job.ProgressCurrent,
		ProgressTotal: job.ProgressTotal, ResumeCursor: job.ResumeCursor,
		AtMS: runtime.nowAfter(job.UpdatedAtMS),
	}); err != nil {
		return store.JobRun{}, err
	}
	value, _, err := runtime.repository.LiveScanRun(ctx, job.JobID)
	return value, err
}

func (runtime *Runtime) fail(
	ctx context.Context,
	job store.JobRun,
	_ store.LiveScanJob,
	started time.Time,
	bytesRead int64,
	cause error,
) (SliceReport, error) {
	writeCtx := context.WithoutCancel(ctx)
	var terminalErr error
	if errors.Is(cause, context.Canceled) || errors.Is(cause, context.DeadlineExceeded) {
		terminalErr = runtime.Interrupt(writeCtx, job.JobID, store.ClassifyRuntimeError(cause))
	} else {
		current, _, readErr := runtime.repository.LiveScanRun(writeCtx, job.JobID)
		if readErr != nil {
			terminalErr = readErr
		} else if current.State == store.JobRunning {
			class := classifyLiveError(cause)
			terminalErr = runtime.repository.TransitionJobRun(writeCtx, store.JobTransition{
				JobID: current.JobID, ExpectedState: store.JobRunning, State: store.JobFailed,
				Phase: store.JobPhaseLive, ProgressCurrent: current.ProgressCurrent,
				ProgressTotal: current.ProgressTotal, ResumeCursor: current.ResumeCursor,
				ErrorClass: &class, AtMS: runtime.nowAfter(current.UpdatedAtMS),
			})
		}
	}
	current, _, readErr := runtime.repository.LiveScanRun(writeCtx, job.JobID)
	return SliceReport{
		JobRun: current, FilesProcessed: 1, BytesRead: bytesRead, Active: runtime.elapsed(started),
	}, errors.Join(cause, terminalErr, readErr)
}

func liveFactsFromRequest(request LiveRequest) (store.LiveScanJob, error) {
	if request.RequestID == "" || len(request.RequestID) > 256 || request.HomeGeneration < 0 ||
		request.HomeDeviceID == "" || request.HomeInode <= 0 || request.RequestedAtMS <= 0 ||
		!filepath.IsAbs(request.HomePath) || filepath.Clean(request.HomePath) != request.HomePath ||
		request.Action.Current == nil || request.Action.Issue != nil {
		return store.LiveScanJob{}, fmt.Errorf("%w: identity", ErrInvalidRequest)
	}
	actionKind, err := liveActionKind(request.Action.Kind)
	if err != nil {
		return store.LiveScanJob{}, err
	}
	if !validActionShape(request.Action) {
		return store.LiveScanJob{}, fmt.Errorf("%w: action shape", ErrInvalidRequest)
	}
	if !liveSnapshotAllowed(request.HomePath, *request.Action.Current) {
		return store.LiveScanJob{}, fmt.Errorf("%w: current snapshot", ErrInvalidRequest)
	}
	current := fingerprintFromSnapshot(*request.Action.Current)
	var previous *store.SourceFingerprint
	if request.Action.Previous != nil {
		if !liveSnapshotAllowed(request.HomePath, *request.Action.Previous) {
			return store.LiveScanJob{}, fmt.Errorf("%w: previous snapshot", ErrInvalidRequest)
		}
		value := fingerprintFromSnapshot(*request.Action.Previous)
		previous = &value
	}
	return store.LiveScanJob{
		RequestID: request.RequestID, HomeGeneration: request.HomeGeneration,
		HomePath: request.HomePath, HomeDeviceID: request.HomeDeviceID, HomeInode: request.HomeInode,
		ActionKind: actionKind, Previous: previous, Current: current,
		UpdatedAtMS: request.RequestedAtMS,
	}, nil
}

func validActionShape(action logs.ReconcileAction) bool {
	if action.Current == nil {
		return false
	}
	if action.Kind == logs.ChangeAdded {
		return action.Previous == nil && !action.PathChanged
	}
	if action.Previous == nil {
		return false
	}
	return action.PathChanged == (action.Previous.Path != action.Current.Path)
}

func liveActionKind(kind logs.ChangeKind) (store.LiveScanActionKind, error) {
	switch kind {
	case logs.ChangeAdded:
		return store.LiveScanActionAdded, nil
	case logs.ChangeUnchanged:
		return store.LiveScanActionUnchanged, nil
	case logs.ChangeGrown:
		return store.LiveScanActionGrown, nil
	case logs.ChangeTruncated:
		return store.LiveScanActionTruncated, nil
	case logs.ChangeMoved:
		return store.LiveScanActionMoved, nil
	case logs.ChangeReplaced:
		return store.LiveScanActionReplaced, nil
	default:
		return "", ErrInvalidRequest
	}
}

func liveSnapshotAllowed(home string, snapshot logs.Snapshot) bool {
	if snapshot.Provider != logs.ProviderCodex || snapshot.SourceFileID == "" ||
		snapshot.Fingerprint.DeviceID == "" || snapshot.Fingerprint.Inode <= 0 ||
		snapshot.Fingerprint.SizeBytes < 0 || snapshot.Fingerprint.MTimeNS < 0 ||
		snapshot.Fingerprint.PrefixBytes < 0 || snapshot.Fingerprint.PrefixBytes > logs.PrefixLimitBytes ||
		snapshot.Fingerprint.PrefixSHA256 == "" || snapshot.Fingerprint.Digest == "" {
		return false
	}
	var root string
	switch snapshot.Kind {
	case logs.SourceKindSession:
		root = filepath.Join(home, "sessions")
	case logs.SourceKindArchivedSession:
		root = filepath.Join(home, "archived_sessions")
	default:
		return false
	}
	relative, err := filepath.Rel(root, snapshot.Path)
	return err == nil && relative != "." && !filepath.IsAbs(relative) && relative != ".." &&
		!strings.HasPrefix(relative, ".."+string(filepath.Separator)) && filepath.Ext(snapshot.Path) == ".jsonl"
}

func fingerprintFromSnapshot(snapshot logs.Snapshot) store.SourceFingerprint {
	return store.SourceFingerprint{
		SourceFileID: snapshot.SourceFileID, Provider: snapshot.Provider, SourceKind: string(snapshot.Kind),
		CurrentPath: snapshot.Path, DeviceID: snapshot.Fingerprint.DeviceID,
		Inode: snapshot.Fingerprint.Inode, SizeBytes: snapshot.Fingerprint.SizeBytes,
		MTimeNS: snapshot.Fingerprint.MTimeNS, PrefixBytes: snapshot.Fingerprint.PrefixBytes,
		PrefixSHA256:      snapshot.Fingerprint.PrefixSHA256,
		FingerprintSHA256: snapshot.Fingerprint.Digest,
	}
}

func snapshotFromFingerprint(value store.SourceFingerprint) logs.Snapshot {
	return logs.Snapshot{
		SourceFileID: value.SourceFileID, Provider: value.Provider,
		Kind: logs.SourceKind(value.SourceKind), Path: value.CurrentPath,
		Fingerprint: logs.Fingerprint{
			DeviceID: value.DeviceID, Inode: value.Inode, SizeBytes: value.SizeBytes,
			MTimeNS: value.MTimeNS, PrefixBytes: value.PrefixBytes,
			PrefixSHA256: value.PrefixSHA256, Digest: value.FingerprintSHA256,
		},
	}
}

func reconcileActionFromFacts(facts store.LiveScanJob) logs.ReconcileAction {
	current := snapshotFromFingerprint(facts.Current)
	action := logs.ReconcileAction{Kind: logs.ChangeKind(facts.ActionKind), Current: &current}
	if facts.Previous != nil {
		previous := snapshotFromFingerprint(*facts.Previous)
		action.Previous = &previous
		action.PathChanged = previous.Path != current.Path
	}
	return action
}

func validSliceBudget(budget SliceBudget) bool {
	return budget.MaxFiles > 0 && budget.MaxFiles <= maxSliceFiles &&
		budget.MaxBytes >= logs.PrefixLimitBytes && budget.MaxBytes <= maxSliceBytes &&
		budget.MaxActive > 0 && budget.MaxActive <= maxSliceActive
}

func classifyLiveError(err error) store.RuntimeErrorClass {
	if errors.Is(err, logs.ErrChangedDuringScan) || errors.Is(err, logs.ErrHomeChanged) ||
		errors.Is(err, logs.ErrUnsafeSource) || errors.Is(err, logs.ErrUnsupportedFile) ||
		errors.Is(err, fs.ErrNotExist) {
		return store.RuntimeErrorUnavailable
	}
	return store.ClassifyRuntimeError(err)
}

func (runtime *Runtime) register(jobID string) bool {
	runtime.mu.Lock()
	defer runtime.mu.Unlock()
	if _, found := runtime.active[jobID]; found {
		return false
	}
	runtime.active[jobID] = struct{}{}
	return true
}

func (runtime *Runtime) release(jobID string) {
	runtime.mu.Lock()
	delete(runtime.active, jobID)
	runtime.mu.Unlock()
}

func (runtime *Runtime) nowAfter(minimum int64) int64 {
	runtime.timeMu.Lock()
	defer runtime.timeMu.Unlock()
	value := runtime.clock().UnixMilli()
	if value <= minimum {
		value = minimum + 1
	}
	if value <= runtime.lastMS {
		value = runtime.lastMS + 1
	}
	runtime.lastMS = value
	return value
}

func (runtime *Runtime) elapsed(start time.Time) time.Duration {
	value := runtime.clock().Sub(start)
	if value < 0 {
		return 0
	}
	return value
}

func stableID(prefix string, value string) string {
	digest := sha256.Sum256([]byte(value))
	return prefix + hex.EncodeToString(digest[:])
}

func cloneInt64(value *int64) *int64 {
	if value == nil {
		return nil
	}
	copy := *value
	return &copy
}

func cloneCursor(value *store.JobCursor) *store.JobCursor {
	if value == nil {
		return nil
	}
	copy := *value
	return &copy
}

func liveStartMatches(
	existingJob store.JobRun,
	existingFacts store.LiveScanJob,
	wantJob store.JobRun,
	wantFacts store.LiveScanJob,
) bool {
	return existingJob.JobID == wantJob.JobID && existingJob.JobType == wantJob.JobType &&
		existingJob.RequestedBy == wantJob.RequestedBy && existingJob.Priority == wantJob.Priority &&
		existingJob.Phase == wantJob.Phase && existingJob.CreatedAtMS == wantJob.CreatedAtMS &&
		existingJob.ResumeOfJobID == nil && existingJob.SourceFileID == nil &&
		liveFactsEqual(existingFacts, wantFacts)
}

func liveFactsEqual(left, right store.LiveScanJob) bool {
	return left.JobID == right.JobID && left.RequestID == right.RequestID &&
		left.HomeGeneration == right.HomeGeneration && left.HomePath == right.HomePath &&
		left.HomeDeviceID == right.HomeDeviceID && left.HomeInode == right.HomeInode &&
		left.ActionKind == right.ActionKind && fingerprintsEqual(left.Previous, right.Previous) &&
		left.Current == right.Current && left.UpdatedAtMS == right.UpdatedAtMS
}

func fingerprintsEqual(left, right *store.SourceFingerprint) bool {
	if left == nil || right == nil {
		return left == right
	}
	return *left == *right
}
