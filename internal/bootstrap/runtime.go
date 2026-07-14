package bootstrap

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"math"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/SisyphusSQ/codex-pulse/internal/codex/index"
	"github.com/SisyphusSQ/codex-pulse/internal/codex/logs"
	"github.com/SisyphusSQ/codex-pulse/internal/preferences"
	"github.com/SisyphusSQ/codex-pulse/internal/store"
)

const (
	bootstrapJobType      = "codex_home_bootstrap"
	bootstrapRequestedBy  = "home-switch"
	bootstrapPriority     = int64(10)
	defaultFastMaxFiles   = 8
	defaultFastMaxBytes   = int64(16 << 20)
	defaultReadChunkBytes = 256 << 10
	bootstrapJobIDPrefix  = "bootstrap-"
	bootstrapResumePrefix = "bootstrap-resume-"
)

var (
	ErrInvalidRuntime      = errors.New("invalid bootstrap runtime")
	ErrInvalidRequest      = errors.New("invalid bootstrap request")
	ErrGenerationDraining  = errors.New("bootstrap generation is draining")
	ErrRunAlreadyActive    = errors.New("bootstrap run is already active")
	ErrSourceUnavailable   = errors.New("bootstrap source is unavailable")
	ErrDiscoveryIncomplete = errors.New("bootstrap discovery contains blocking issues")
)

type RuntimeConfig struct {
	Repository     *store.Repository
	Clock          func() time.Time
	FastMaxFiles   int
	FastMaxBytes   int64
	ReadChunkBytes int
}

type RunReport struct {
	JobID            string
	State            store.JobState
	Phase            store.JobPhase
	FirstScreenReady bool
	FullHistoryReady bool
	ProgressCurrent  int64
	ProgressTotal    int64
	ReconcileChanges int64
	ReconcileIssues  int64
}

type runtimeHooks struct {
	afterHomeProbe        func(context.Context)
	afterBootstrapCreate  func(context.Context)
	beforeInitialFreeze   func(context.Context)
	afterInitialDiscovery func(context.Context)
	afterChunk            func(store.BootstrapPlanItem, int64)
	beforeReconcile       func()
	afterReconcilePlan    func()
	afterResumeLookup     func(context.Context)
}

type activeRun struct {
	cancel context.CancelFunc
	done   chan struct{}
}

// Runtime is the bounded TOO-259 adapter. It owns admission for synchronous Run
// calls, but deliberately does not start the TOO-260 scheduler.
type Runtime struct {
	repository     *store.Repository
	clock          func() time.Time
	fastMaxFiles   int
	fastMaxBytes   int64
	readChunkBytes int
	hooks          runtimeHooks

	timeMu sync.Mutex
	lastMS int64

	mu       sync.Mutex
	draining map[int64]bool
	active   map[int64]map[string]*activeRun
	drains   map[int64]chan struct{}
}

func NewRuntime(config RuntimeConfig) (*Runtime, error) {
	return newRuntime(config, runtimeHooks{})
}

func newRuntime(config RuntimeConfig, hooks runtimeHooks) (*Runtime, error) {
	if config.Repository == nil {
		return nil, ErrInvalidRuntime
	}
	if config.Clock == nil {
		config.Clock = time.Now
	}
	if config.FastMaxFiles == 0 {
		config.FastMaxFiles = defaultFastMaxFiles
	}
	if config.FastMaxBytes == 0 {
		config.FastMaxBytes = defaultFastMaxBytes
	}
	if config.ReadChunkBytes == 0 {
		config.ReadChunkBytes = defaultReadChunkBytes
	}
	if config.FastMaxFiles < 0 || config.FastMaxBytes < 0 || config.ReadChunkBytes < 0 {
		return nil, ErrInvalidRuntime
	}
	return &Runtime{
		repository: config.Repository, clock: config.Clock,
		fastMaxFiles: config.FastMaxFiles, fastMaxBytes: config.FastMaxBytes,
		readChunkBytes: config.ReadChunkBytes, hooks: hooks,
		draining: make(map[int64]bool), active: make(map[int64]map[string]*activeRun),
		drains: make(map[int64]chan struct{}),
	}, nil
}

func (runtime *Runtime) StartBootstrap(
	ctx context.Context,
	request preferences.BootstrapRequest,
) error {
	if runtime == nil || runtime.repository == nil {
		return ErrInvalidRuntime
	}
	if err := validateBootstrapRequest(request); err != nil {
		return err
	}
	generation := int64(request.Generation)
	jobID := stableBootstrapJobID(request.SwitchID, request.Generation)
	if !runtime.isDraining(generation) {
		handled, err := runtime.exactStartReadback(ctx, request, generation, jobID)
		if handled || err != nil {
			return err
		}
	}
	operationCtx, release, err := runtime.registerOperation(
		ctx, generation, "start:"+jobID, false, true,
	)
	if err != nil {
		return err
	}
	defer release()
	ctx = operationCtx
	job, facts, err := runtime.repository.BootstrapRunByIdentity(ctx, request.SwitchID, generation)
	create := false
	switch {
	case err == nil:
		if !bootstrapRequestMatchesFacts(request, facts) ||
			facts.PlanState == store.BootstrapPlanPending && job.JobID != jobID {
			return fmt.Errorf("%w: stable bootstrap identity conflicts", ErrInvalidRequest)
		}
		if facts.PlanState == store.BootstrapPlanReady || job.State == store.JobSucceeded ||
			job.State == store.JobFailed || job.State == store.JobCancelled || job.State == store.JobInterrupted {
			return nil
		}
	case errors.Is(err, store.ErrNotFound):
		create = true
	default:
		return err
	}

	metadata, err := logs.NewHomeProbe().Probe(ctx, request.Source.Path)
	if err != nil {
		return err
	}
	if metadata.Path != request.Source.Path || metadata.DeviceID != request.Source.DeviceID ||
		metadata.Inode != request.Source.Inode {
		return fmt.Errorf("%w: confirmed Home identity changed", ErrInvalidRequest)
	}
	if runtime.hooks.afterHomeProbe != nil {
		runtime.hooks.afterHomeProbe(ctx)
	}
	if create {
		createdAtMS := runtime.nowAfter(request.Source.ConfirmedAtMS - 1)
		job = store.JobRun{
			JobID: jobID, JobType: bootstrapJobType, RequestedBy: bootstrapRequestedBy,
			Priority: bootstrapPriority, State: store.JobQueued, Phase: store.JobPhaseDiscover,
			CreatedAtMS: createdAtMS, UpdatedAtMS: createdAtMS,
		}
		facts = store.BootstrapJobFacts{
			JobID: jobID, SwitchID: request.SwitchID, HomeGeneration: generation,
			HomePath: request.Source.Path, HomeDeviceID: request.Source.DeviceID,
			HomeInode: request.Source.Inode, DataStoreKey: request.DataStoreKey,
			Strategy: string(request.Strategy), PlanState: store.BootstrapPlanPending,
			ETAState: store.BootstrapETAUnknown, UpdatedAtMS: createdAtMS,
		}
		if err := runtime.repository.CreateBootstrapJob(ctx, job, facts); err != nil {
			return err
		}
		if runtime.hooks.afterBootstrapCreate != nil {
			runtime.hooks.afterBootstrapCreate(ctx)
		}
	}

	if job.State == store.JobQueued {
		atMS := runtime.nowAfter(job.UpdatedAtMS)
		facts.UpdatedAtMS = atMS
		if err := runtime.repository.AdvanceBootstrapRun(ctx, store.BootstrapAdvance{
			Job: store.JobTransition{
				JobID: job.JobID, ExpectedState: store.JobQueued, State: store.JobRunning,
				Phase: store.JobPhaseDiscover, AtMS: atMS,
			},
			Facts: facts,
		}); err != nil {
			return err
		}
		job.State = store.JobRunning
		job.UpdatedAtMS = atMS
	}
	if runtime.hooks.beforeInitialFreeze != nil {
		runtime.hooks.beforeInitialFreeze(ctx)
	}
	if err := runtime.freezeInitialPlan(ctx, request, job.JobID); err != nil {
		terminalErr := runtime.terminate(
			context.WithoutCancel(ctx), job.JobID, store.JobFailed, err, sourcePauseReason(err),
		)
		return errors.Join(err, terminalErr)
	}
	return nil
}

// exactStartReadback lets an immutable replay observe a durable ready or
// terminal attempt without competing with an active Run. Pending attempts
// still enter admission so one Start owner finishes plan freeze while exact
// concurrent callers wait for that owner's authoritative readback.
func (runtime *Runtime) exactStartReadback(
	ctx context.Context,
	request preferences.BootstrapRequest,
	generation int64,
	jobID string,
) (bool, error) {
	job, facts, err := runtime.repository.BootstrapRunByIdentity(
		ctx, request.SwitchID, generation,
	)
	if errors.Is(err, store.ErrNotFound) {
		return false, nil
	}
	if err != nil {
		return true, err
	}
	if !bootstrapRequestMatchesFacts(request, facts) ||
		facts.PlanState == store.BootstrapPlanPending && job.JobID != jobID {
		return true, fmt.Errorf("%w: stable bootstrap identity conflicts", ErrInvalidRequest)
	}
	if facts.PlanState == store.BootstrapPlanReady || job.State == store.JobSucceeded ||
		job.State == store.JobFailed || job.State == store.JobCancelled || job.State == store.JobInterrupted {
		return true, nil
	}
	return false, nil
}

func (runtime *Runtime) freezeInitialPlan(
	ctx context.Context,
	request preferences.BootstrapRequest,
	jobID string,
) error {
	previous, err := runtime.repository.CodexSnapshots(ctx)
	if err != nil {
		return err
	}
	previousSnapshots := snapshotsFromFingerprints(previous)
	committedOffsets, err := runtime.incompleteCommittedOffsets(ctx, previous)
	if err != nil {
		return err
	}
	discoverer, err := logs.NewConfirmedDiscoverer(
		request.Source.Path, request.Source.DeviceID, request.Source.Inode,
	)
	if err != nil {
		return err
	}
	discovery, err := discoverer.DiscoverAgainst(ctx, previousSnapshots)
	if err != nil {
		return err
	}
	if runtime.hooks.afterInitialDiscovery != nil {
		runtime.hooks.afterInitialDiscovery(ctx)
	}
	reconcile, err := logs.PlanReconcile(request.Source.Path, previousSnapshots, discovery)
	if err != nil {
		return err
	}
	now := runtime.clock()
	hints, err := loadSessionIndexHints(
		ctx, request.Source.Path, request.Source.DeviceID, request.Source.Inode,
		discovery.Snapshots, now.UnixMilli(),
	)
	if err != nil {
		return err
	}
	dayStart := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, now.Location())
	_, facts, err := runtime.repository.BootstrapRun(ctx, jobID)
	if err != nil {
		return err
	}
	atMS := runtime.nowAfter(facts.UpdatedAtMS)
	items, err := FreezeInitialPlan(PlanRequest{
		JobID: jobID, Reconcile: reconcile, NowMS: now.UnixMilli(), DayStartMS: dayStart.UnixMilli(),
		FastMaxFiles: runtime.fastMaxFiles, FastMaxBytes: runtime.fastMaxBytes,
		RecencyHints: hints, CommittedOffsets: committedOffsets, AtMS: atMS,
	})
	if err != nil {
		return err
	}
	return runtime.repository.FreezeBootstrapPlan(ctx, jobID, items, atMS)
}

func (runtime *Runtime) incompleteCommittedOffsets(
	ctx context.Context,
	snapshots []store.SourceFingerprint,
) (map[string]int64, error) {
	offsets := make(map[string]int64)
	for _, snapshot := range snapshots {
		file, err := runtime.repository.SourceFile(ctx, snapshot.SourceFileID)
		if err != nil {
			return nil, err
		}
		cursor, err := runtime.repository.GenerationCursor(
			ctx, snapshot.SourceFileID, file.ActiveGeneration,
		)
		if err != nil {
			return nil, err
		}
		if cursor.State != store.GenerationActive || cursor.Fingerprint != snapshot ||
			cursor.Checkpoint.CommittedOffset < 0 ||
			cursor.Checkpoint.CommittedOffset > snapshot.SizeBytes {
			return nil, ErrInvalidRuntime
		}
		if cursor.Checkpoint.CommittedOffset < snapshot.SizeBytes {
			offsets[snapshot.SourceFileID] = cursor.Checkpoint.CommittedOffset
		}
	}
	return offsets, nil
}

func (runtime *Runtime) BootstrapStatus(
	ctx context.Context,
	switchID string,
	generation uint64,
) (preferences.BootstrapStatus, error) {
	if runtime == nil || runtime.repository == nil || switchID == "" || generation > math.MaxInt64 {
		return "", ErrInvalidRequest
	}
	job, facts, err := runtime.repository.BootstrapRunByIdentity(ctx, switchID, int64(generation))
	if errors.Is(err, store.ErrNotFound) {
		return preferences.BootstrapStatusNotStarted, nil
	}
	if err != nil {
		return "", err
	}
	switch job.State {
	case store.JobQueued:
		return preferences.BootstrapStatusQueued, nil
	case store.JobRunning:
		return preferences.BootstrapStatusRunning, nil
	case store.JobSucceeded:
		return preferences.BootstrapStatusSucceeded, nil
	case store.JobFailed, store.JobCancelled, store.JobInterrupted:
		if facts.PlanState == store.BootstrapPlanPending {
			return preferences.BootstrapStatusFailedSafeRollback, nil
		}
		return preferences.BootstrapStatusFailedNeedsResume, nil
	default:
		return "", ErrInvalidRuntime
	}
}

func (runtime *Runtime) Drain(ctx context.Context, generation uint64) error {
	if runtime == nil || runtime.repository == nil || generation > math.MaxInt64 {
		return ErrInvalidRequest
	}
	value := int64(generation)
	runs, finish, err := runtime.beginDrain(ctx, value)
	if err != nil {
		return err
	}
	defer finish()
	for _, run := range runs {
		select {
		case <-run.done:
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	job, facts, err := runtime.repository.LatestBootstrapRunByGeneration(ctx, value)
	if errors.Is(err, store.ErrNotFound) {
		return nil
	}
	if err != nil {
		return err
	}
	if job.State == store.JobQueued || job.State == store.JobRunning {
		pause := store.BootstrapPauseApplicationDraining
		return runtime.interrupt(ctx, job, facts, &pause)
	}
	return nil
}

func (runtime *Runtime) Resume(ctx context.Context, generation uint64) error {
	if runtime == nil || runtime.repository == nil || generation > math.MaxInt64 {
		return ErrInvalidRequest
	}
	value := int64(generation)
	operationCtx, release, err := runtime.registerOperation(
		ctx, value, "resume:"+fmt.Sprint(value), true, true,
	)
	if err != nil {
		return err
	}
	defer release()
	ctx = operationCtx
	job, facts, err := runtime.repository.LatestBootstrapRunByGeneration(ctx, value)
	if errors.Is(err, store.ErrNotFound) {
		return nil
	}
	if err != nil {
		return err
	}
	if runtime.hooks.afterResumeLookup != nil {
		runtime.hooks.afterResumeLookup(ctx)
	}
	if (job.State != store.JobInterrupted && job.State != store.JobFailed && job.State != store.JobCancelled) ||
		facts.PlanState != store.BootstrapPlanReady {
		return nil
	}
	atMS := runtime.nowAfter(job.UpdatedAtMS)
	oldID := job.JobID
	resumed := store.JobRun{
		JobID: stableResumeJobID(oldID), JobType: job.JobType, RequestedBy: job.RequestedBy,
		Priority: job.Priority, State: store.JobQueued, Phase: job.Phase,
		SourceFileID: cloneString(job.SourceFileID), ResumeOfJobID: &oldID,
		CreatedAtMS: atMS, ProgressCurrent: cloneInt64(job.ProgressCurrent),
		ProgressTotal: cloneInt64(job.ProgressTotal), ResumeCursor: cloneJobCursor(job.ResumeCursor),
		UpdatedAtMS: atMS,
	}
	return runtime.repository.ResumeBootstrapJob(ctx, oldID, resumed)
}

func (runtime *Runtime) registerRun(
	ctx context.Context,
	generation int64,
	jobID string,
) (context.Context, func(), error) {
	return runtime.registerOperation(ctx, generation, "run:"+jobID, false, false)
}

func (runtime *Runtime) registerOperation(
	ctx context.Context,
	generation int64,
	operationID string,
	reopen bool,
	waitForSame bool,
) (context.Context, func(), error) {
	for {
		runtime.mu.Lock()
		if operationID == "" {
			runtime.mu.Unlock()
			return nil, nil, ErrInvalidRuntime
		}
		if reopen {
			if _, drainingNow := runtime.drains[generation]; drainingNow {
				runtime.mu.Unlock()
				return nil, nil, ErrGenerationDraining
			}
			delete(runtime.draining, generation)
		} else if runtime.draining[generation] {
			runtime.mu.Unlock()
			return nil, nil, ErrGenerationDraining
		}
		if runtime.active[generation] == nil {
			runtime.active[generation] = make(map[string]*activeRun)
		}
		if len(runtime.active[generation]) > 0 {
			if waitForSame {
				if owner, found := runtime.active[generation][operationID]; found {
					done := owner.done
					runtime.mu.Unlock()
					select {
					case <-done:
						continue
					case <-ctx.Done():
						return nil, nil, ctx.Err()
					}
				}
			}
			runtime.mu.Unlock()
			return nil, nil, ErrRunAlreadyActive
		}
		runCtx, cancel := context.WithCancel(ctx)
		run := &activeRun{cancel: cancel, done: make(chan struct{})}
		runtime.active[generation][operationID] = run
		runtime.mu.Unlock()
		release := func() {
			cancel()
			runtime.mu.Lock()
			delete(runtime.active[generation], operationID)
			if len(runtime.active[generation]) == 0 {
				delete(runtime.active, generation)
			}
			close(run.done)
			runtime.mu.Unlock()
		}
		return runCtx, release, nil
	}
}

func (runtime *Runtime) beginDrain(
	ctx context.Context,
	generation int64,
) ([]*activeRun, func(), error) {
	for {
		runtime.mu.Lock()
		if existing, found := runtime.drains[generation]; found {
			runtime.mu.Unlock()
			select {
			case <-existing:
				continue
			case <-ctx.Done():
				return nil, nil, ctx.Err()
			}
		}
		done := make(chan struct{})
		runtime.drains[generation] = done
		runtime.draining[generation] = true
		runs := make([]*activeRun, 0, len(runtime.active[generation]))
		for _, operation := range runtime.active[generation] {
			runs = append(runs, operation)
			operation.cancel()
		}
		runtime.mu.Unlock()
		var finishOnce sync.Once
		finish := func() {
			finishOnce.Do(func() {
				runtime.mu.Lock()
				delete(runtime.drains, generation)
				close(done)
				runtime.mu.Unlock()
			})
		}
		return runs, finish, nil
	}
}

func (runtime *Runtime) isDraining(generation int64) bool {
	runtime.mu.Lock()
	defer runtime.mu.Unlock()
	return runtime.draining[generation]
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

func validateBootstrapRequest(request preferences.BootstrapRequest) error {
	if request.SwitchID == "" || len(request.SwitchID) > 128 || request.Generation > math.MaxInt64 ||
		!filepath.IsAbs(request.Source.Path) || filepath.Clean(request.Source.Path) != request.Source.Path ||
		request.Source.DeviceID == "" || request.Source.Inode <= 0 || request.Source.ConfirmedAtMS <= 0 ||
		request.DataStoreKey == "" ||
		(request.Strategy != preferences.HomeSwitchIndependentDatabase &&
			request.Strategy != preferences.HomeSwitchClearAndRebuild) {
		return ErrInvalidRequest
	}
	return nil
}

func bootstrapRequestMatchesFacts(
	request preferences.BootstrapRequest,
	facts store.BootstrapJobFacts,
) bool {
	return facts.SwitchID == request.SwitchID && facts.HomeGeneration == int64(request.Generation) &&
		facts.HomePath == request.Source.Path && facts.HomeDeviceID == request.Source.DeviceID &&
		facts.HomeInode == request.Source.Inode && facts.DataStoreKey == request.DataStoreKey &&
		facts.Strategy == string(request.Strategy)
}

func stableBootstrapJobID(switchID string, generation uint64) string {
	return bootstrapJobIDPrefix + stableDigest(switchID+"\x00"+fmt.Sprint(generation))
}

func stableResumeJobID(oldJobID string) string {
	return bootstrapResumePrefix + stableDigest(oldJobID)
}

func stableDigest(value string) string {
	digest := sha256.Sum256([]byte(value))
	return hex.EncodeToString(digest[:])
}

func snapshotsFromFingerprints(values []store.SourceFingerprint) []logs.Snapshot {
	result := make([]logs.Snapshot, len(values))
	for index, value := range values {
		result[index] = snapshotFromFingerprint(value)
	}
	return result
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

func loadSessionIndexHints(
	ctx context.Context,
	home string,
	deviceID string,
	inode int64,
	snapshots []logs.Snapshot,
	maximumMS int64,
) (map[string]int64, error) {
	hints := make(map[string]int64)
	if maximumMS < 0 {
		return hints, nil
	}
	indexFile, err := index.OpenConfirmedIndexFile(home, deviceID, inode)
	if err != nil {
		if errors.Is(err, index.ErrHomeChanged) {
			return nil, logs.ErrHomeChanged
		}
		return hints, nil
	}
	read, err := indexFile.Read(ctx)
	if err != nil {
		if errors.Is(err, index.ErrHomeChanged) {
			return nil, logs.ErrHomeChanged
		}
		return hints, nil
	}
	parsed, err := index.Parse(read.Content)
	clear(read.Content)
	if err != nil {
		return hints, nil
	}
	for _, snapshot := range snapshots {
		if snapshot.Kind == logs.SourceKindSessionIndex {
			continue
		}
		base := filepath.Base(snapshot.Path)
		for _, entry := range parsed.Entries {
			if entry.UpdatedAtMS == nil || *entry.UpdatedAtMS > maximumMS ||
				!strings.Contains(base, entry.ID) {
				continue
			}
			if current, found := hints[snapshot.SourceFileID]; !found || *entry.UpdatedAtMS > current {
				hints[snapshot.SourceFileID] = *entry.UpdatedAtMS
			}
		}
	}
	return hints, nil
}

func cloneString(value *string) *string {
	if value == nil {
		return nil
	}
	copy := *value
	return &copy
}

func cloneInt64(value *int64) *int64 {
	if value == nil {
		return nil
	}
	copy := *value
	return &copy
}

func cloneJobCursor(value *store.JobCursor) *store.JobCursor {
	if value == nil {
		return nil
	}
	copy := *value
	return &copy
}
