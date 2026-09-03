package lightindex

import (
	"context"
	"errors"
	"io"
	"io/fs"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/SisyphusSQ/codex-pulse/internal/codex/appserver"
	logsource "github.com/SisyphusSQ/codex-pulse/internal/codex/logs/source"
	"github.com/SisyphusSQ/codex-pulse/internal/store"
	storelight "github.com/SisyphusSQ/codex-pulse/internal/store/lightindex"
	storesqlite "github.com/SisyphusSQ/codex-pulse/internal/store/sqlite"
)

const (
	defaultScanBatchBytes  int64 = 8 << 20
	maxSessionScanAttempts       = 8
)

var errTokenScanLineTooLong = errors.New("token scan line exceeds maximum")

type MetadataProvider interface {
	List(context.Context, string) (appserver.ThreadList, error)
}

type RuntimeConfig struct {
	Repository        *storelight.Repository
	DeepRepository    *store.Repository
	Metadata          MetadataProvider
	ScanBatchBytes    int64
	RefreshInterval   time.Duration
	Clock             func() time.Time
	BeforeTokenScan   func(context.Context) error
	MetadataCommitted func()
	BatchCommitted    func(storelight.LightTokenScan)
	RefreshCommitted  func()
	RefreshFailed     func(error)
}

type Runtime struct {
	repository        *storelight.Repository
	deepRepository    *store.Repository
	metadata          MetadataProvider
	scanBatchBytes    int64
	refreshInterval   time.Duration
	clock             func() time.Time
	beforeTokenScan   func(context.Context) error
	metadataCommitted func()
	batchCommitted    func(storelight.LightTokenScan)
	refreshCommitted  func()
	refreshFailed     func(error)
	deepMu            sync.Mutex
}

type Run struct {
	cancel   context.CancelFunc
	trigger  chan struct{}
	done     chan error
	finished chan struct{}
	once     sync.Once
}

func NewRuntime(config RuntimeConfig) (*Runtime, error) {
	if config.Repository == nil || config.Metadata == nil || config.RefreshInterval < 0 {
		return nil, errors.New("invalid lightweight index runtime")
	}
	batchBytes := config.ScanBatchBytes
	if batchBytes <= 0 {
		batchBytes = defaultScanBatchBytes
	}
	clock := config.Clock
	if clock == nil {
		clock = time.Now
	}
	return &Runtime{
		repository: config.Repository, deepRepository: config.DeepRepository,
		metadata: config.Metadata, scanBatchBytes: batchBytes,
		refreshInterval: config.RefreshInterval, clock: clock, beforeTokenScan: config.BeforeTokenScan,
		metadataCommitted: config.MetadataCommitted, batchCommitted: config.BatchCommitted,
		refreshCommitted: config.RefreshCommitted, refreshFailed: config.RefreshFailed,
	}, nil
}

func (runtime *Runtime) Start(ctx context.Context, home storelight.LightHomeIdentity) (*Run, error) {
	if !runtime.validStart(ctx, home) {
		return nil, errors.New("invalid lightweight index start")
	}
	metadata, err := runtime.metadata.List(ctx, home.Path)
	if err != nil {
		if ctx.Err() != nil || fatalRefreshError(err) {
			return nil, err
		}
		runtime.notifyRefreshFailed(err)
		return runtime.startWorker(ctx, home, false), nil
	}
	metadataChanged, err := runtime.reconcileMetadata(ctx, home, metadata)
	if err != nil {
		return nil, err
	}
	return runtime.startWorker(ctx, home, metadataChanged), nil
}

// StartHomeSwitch publishes metadata for a newly confirmed Home only when the
// currently indexed Home still matches expected. The metadata RPC completes
// before old lightweight derived rows are replaced.
func (runtime *Runtime) StartHomeSwitch(
	ctx context.Context,
	expected storelight.LightHomeIdentity,
	next storelight.LightHomeIdentity,
) (*Run, error) {
	if !runtime.validStart(ctx, expected) || !runtime.validStart(ctx, next) || expected == next {
		return nil, errors.New("invalid lightweight Home switch")
	}
	metadata, err := runtime.metadata.List(ctx, next.Path)
	if err != nil {
		return nil, err
	}
	snapshot := runtime.metadataSnapshot(next, 1, metadata)
	if err := runtime.repository.ReplaceLightMetadataForHomeSwitch(ctx, expected, snapshot); err != nil {
		return nil, err
	}
	runtime.notifyMetadataCommitted()
	return runtime.startWorker(ctx, next, true), nil
}

func (runtime *Runtime) refreshMetadata(
	ctx context.Context,
	home storelight.LightHomeIdentity,
) (bool, error) {
	metadata, err := runtime.metadata.List(ctx, home.Path)
	if err != nil {
		return false, err
	}
	return runtime.reconcileMetadata(ctx, home, metadata)
}

func (runtime *Runtime) reconcileMetadata(
	ctx context.Context,
	home storelight.LightHomeIdentity,
	metadata appserver.ThreadList,
) (bool, error) {
	generation := int64(1)
	state, stateErr := runtime.repository.LightIndexState(ctx)
	switch {
	case stateErr == nil:
		if state.Home != home {
			return false, storelight.ErrLightHomeFence
		}
		generation = state.MetadataGeneration + 1
	case errors.Is(stateErr, storelight.ErrNotFound):
	default:
		return false, stateErr
	}
	changed, err := runtime.repository.ReconcileLightMetadata(
		ctx, runtime.metadataSnapshot(home, generation, metadata),
	)
	if err != nil {
		return false, err
	}
	if changed {
		runtime.notifyMetadataCommitted()
	}
	return changed, nil
}

func (runtime *Runtime) notifyRefreshFailed(err error) {
	if runtime.refreshFailed != nil {
		runtime.refreshFailed(err)
	}
}

func (runtime *Runtime) notifyMetadataCommitted() {
	if runtime.metadataCommitted != nil {
		runtime.metadataCommitted()
	}
}

func (runtime *Runtime) validStart(ctx context.Context, home storelight.LightHomeIdentity) bool {
	return runtime != nil && runtime.repository != nil && runtime.metadata != nil && ctx != nil &&
		home.Path != "" && home.DeviceID != "" && home.Inode > 0
}

func (runtime *Runtime) metadataSnapshot(
	home storelight.LightHomeIdentity,
	generation int64,
	metadata appserver.ThreadList,
) storelight.LightMetadataSnapshot {
	snapshot := storelight.LightMetadataSnapshot{
		Home: home, Generation: generation, ReadyAtMS: runtime.clock().UnixMilli(),
		Sessions: make([]storelight.LightSessionMetadata, 0, len(metadata.Threads)),
	}
	for _, thread := range metadata.Threads {
		snapshot.Sessions = append(snapshot.Sessions, storelight.LightSessionMetadata{
			SessionID: thread.SessionID, ThreadName: thread.Name, CWD: thread.CWD,
			RolloutPath: thread.RolloutPath, CreatedAtMS: thread.CreatedAtMS,
			UpdatedAtMS: thread.UpdatedAtMS, RecencyAtMS: thread.RecencyAtMS,
		})
	}
	return snapshot
}

func (runtime *Runtime) startWorker(
	ctx context.Context,
	home storelight.LightHomeIdentity,
	initialMetadataChanged bool,
) *Run {
	workerCtx, cancel := context.WithCancel(ctx)
	run := &Run{
		cancel: cancel, trigger: make(chan struct{}, 1), done: make(chan error, 1), finished: make(chan struct{}),
	}
	go func() {
		run.done <- runtime.run(workerCtx, home, initialMetadataChanged, run.trigger)
		close(run.done)
		close(run.finished)
	}()
	return run
}

func (runtime *Runtime) run(
	ctx context.Context,
	home storelight.LightHomeIdentity,
	initialMetadataChanged bool,
	trigger <-chan struct{},
) error {
	published, err := runtime.scanAll(ctx, home)
	if initialMetadataChanged && !published {
		runtime.notifyRefreshCommitted()
	}
	if err != nil {
		if runtime.refreshInterval <= 0 || ctx.Err() != nil || fatalRefreshError(err) {
			return err
		}
		runtime.notifyRefreshFailed(err)
	}
	if runtime.refreshInterval <= 0 {
		return nil
	}
	ticker := time.NewTicker(runtime.refreshInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
		case <-trigger:
		}
		if err := runtime.refreshOnce(ctx, home); err != nil {
			if ctx.Err() != nil {
				return ctx.Err()
			}
			runtime.notifyRefreshFailed(err)
			if fatalRefreshError(err) {
				return err
			}
		}
	}
}

func (runtime *Runtime) refreshOnce(ctx context.Context, home storelight.LightHomeIdentity) error {
	metadataChanged, err := runtime.refreshMetadata(ctx, home)
	if err != nil {
		return err
	}
	published, scanErr := runtime.scanAll(ctx, home)
	if metadataChanged && !published {
		runtime.notifyRefreshCommitted()
	}
	return scanErr
}

func fatalRefreshError(err error) bool {
	return errors.Is(err, storelight.ErrLightHomeFence) || errors.Is(err, logsource.ErrHomeChanged) ||
		errors.Is(err, logsource.ErrInvalidHome) || errors.Is(err, logsource.ErrUnsafeHome)
}

func recoverableSessionScanError(err error) bool {
	if err == nil || errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) ||
		fatalRefreshError(err) || errors.Is(err, storelight.ErrInvalidRepository) ||
		errors.Is(err, storelight.ErrInvalidRecord) || sqliteScanError(err) {
		return false
	}
	return errors.Is(err, storelight.ErrLightTokenConflict) ||
		errors.Is(err, storelight.ErrNotFound) ||
		errors.Is(err, logsource.ErrChangedDuringScan) ||
		errors.Is(err, logsource.ErrUnsafeSource) ||
		errors.Is(err, logsource.ErrUnsupportedFile) ||
		errors.Is(err, fs.ErrNotExist) ||
		errors.Is(err, fs.ErrPermission) ||
		errors.Is(err, errTokenScanLineTooLong)
}

func sqliteScanError(err error) bool {
	return errors.Is(err, storesqlite.ErrInvalidConfig) ||
		errors.Is(err, storesqlite.ErrInvalidPath) ||
		errors.Is(err, storesqlite.ErrQueueFull) ||
		errors.Is(err, storesqlite.ErrClosing) ||
		errors.Is(err, storesqlite.ErrClosed) ||
		errors.Is(err, storesqlite.ErrCanceled) ||
		errors.Is(err, storesqlite.ErrBusy) ||
		errors.Is(err, storesqlite.ErrOwnerLeaseBusy) ||
		errors.Is(err, storesqlite.ErrDiskFull) ||
		errors.Is(err, storesqlite.ErrReadOnly) ||
		errors.Is(err, storesqlite.ErrPermission) ||
		errors.Is(err, storesqlite.ErrIO) ||
		errors.Is(err, storesqlite.ErrCorrupt) ||
		errors.Is(err, storesqlite.ErrCallbackPanic)
}

func (run *Run) Cancel() {
	if run == nil || run.cancel == nil {
		return
	}
	run.once.Do(run.cancel)
}

// Trigger requests one coalesced metadata and rollout refresh. It returns false
// after a one-shot run finishes or a monitored run has stopped.
func (run *Run) Trigger() bool {
	if run == nil || run.trigger == nil || run.finished == nil {
		return false
	}
	select {
	case <-run.finished:
		return false
	default:
	}
	select {
	case run.trigger <- struct{}{}:
	default:
	}
	return true
}

func (run *Run) Wait(ctx context.Context) error {
	if run == nil || run.done == nil || ctx == nil {
		return errors.New("invalid lightweight index run")
	}
	select {
	case <-ctx.Done():
		return ctx.Err()
	case err := <-run.done:
		return err
	}
}

func (runtime *Runtime) scanAll(
	ctx context.Context,
	home storelight.LightHomeIdentity,
) (bool, error) {
	if runtime.beforeTokenScan != nil {
		if err := runtime.beforeTokenScan(ctx); err != nil {
			return false, err
		}
	}
	discoverer, err := logsource.NewConfirmedDiscoverer(home.Path, home.DeviceID, home.Inode)
	if err != nil {
		return false, err
	}
	reader, err := logsource.NewConfirmedSnapshotReader(
		home.Path, home.DeviceID, home.Inode, DefaultTokenScanChunkBytes,
	)
	if err != nil {
		return false, err
	}
	sessions, err := runtime.repository.ListLightSessions(ctx)
	if err != nil {
		return false, err
	}
	published := false
	for _, session := range sessions {
		if err := ctx.Err(); err != nil {
			if published {
				runtime.notifyRefreshCommitted()
			}
			return published, err
		}
		if session.RolloutPath == nil {
			continue
		}
		sessionPublished, err := runtime.scanSession(ctx, home, session, discoverer, reader)
		published = published || sessionPublished
		if err != nil {
			if recoverableSessionScanError(err) {
				continue
			}
			if published {
				runtime.notifyRefreshCommitted()
			}
			return published, err
		}
	}
	if published {
		runtime.notifyRefreshCommitted()
	}
	return published, nil
}

func (runtime *Runtime) scanSession(
	ctx context.Context,
	home storelight.LightHomeIdentity,
	session storelight.LightSessionMetadata,
	discoverer *logsource.Discoverer,
	reader *logsource.SnapshotReader,
) (bool, error) {
	var lastErr error
	for attempt := 0; attempt < maxSessionScanAttempts; attempt++ {
		published, err := runtime.scanSessionOnce(ctx, home, session, discoverer, reader)
		if err == nil {
			return published, nil
		}
		lastErr = err
		if !errors.Is(err, storelight.ErrLightTokenConflict) {
			return published, err
		}
	}
	return false, lastErr
}

func (runtime *Runtime) scanSessionOnce(
	ctx context.Context,
	home storelight.LightHomeIdentity,
	session storelight.LightSessionMetadata,
	discoverer *logsource.Discoverer,
	reader *logsource.SnapshotReader,
) (bool, error) {
	pending, pendingErr := runtime.repository.PendingLightTokenScan(ctx, session.SessionID)
	if pendingErr == nil {
		return runtime.scanExistingPending(ctx, home, session, pending, discoverer, reader)
	}
	if !errors.Is(pendingErr, storelight.ErrNotFound) {
		return false, pendingErr
	}

	active, activeErr := runtime.repository.ActiveLightTokenScan(ctx, session.SessionID)
	var previous *logsource.Snapshot
	if activeErr == nil {
		value := snapshotFromStoredScan(active)
		previous = &value
		if active.ParserVersion == TokenParserVersion && value.Path == *session.RolloutPath {
			unchanged, err := discoverer.Unchanged(ctx, *session.RolloutPath, value)
			if err != nil {
				return false, err
			}
			if unchanged {
				return false, nil
			}
		}
	} else if !errors.Is(activeErr, storelight.ErrNotFound) {
		return false, activeErr
	}
	inspectPrevious := previous
	if inspectPrevious != nil && inspectPrevious.Path != *session.RolloutPath {
		inspectPrevious = nil
	}
	current, err := discoverer.Inspect(ctx, *session.RolloutPath, inspectPrevious)
	if err != nil {
		return false, err
	}
	identity := identityFromSnapshot(home, current)
	if activeErr == nil {
		decision := DecideRefresh(
			checkpointFromStoredScan(active), homeIdentity(active.Identity.Home), fileIdentity(identity), TokenParserVersion,
		)
		switch decision.Kind {
		case RefreshReuse:
			return false, nil
		case RefreshAppend:
			generation, err := runtime.repository.StartLightTokenAppend(
				ctx, session.SessionID, identity, TokenParserVersion, runtime.clock().UnixMilli(),
			)
			if err != nil {
				return false, err
			}
			pending, err = runtime.repository.PendingLightTokenScan(ctx, session.SessionID)
			if err != nil || pending.Generation != generation {
				return false, errors.Join(storelight.ErrLightTokenConflict, err)
			}
			return runtime.scanPending(ctx, pending, current, reader)
		case RefreshDefer:
			return false, storelight.ErrLightHomeFence
		}
	}
	generation, err := runtime.repository.StartLightTokenRebuild(
		ctx, session.SessionID, identity, TokenParserVersion, runtime.clock().UnixMilli(),
	)
	if err != nil {
		return false, err
	}
	pending, err = runtime.repository.PendingLightTokenScan(ctx, session.SessionID)
	if err != nil || pending.Generation != generation {
		return false, errors.Join(storelight.ErrLightTokenConflict, err)
	}
	return runtime.scanPending(ctx, pending, current, reader)
}

func (runtime *Runtime) scanExistingPending(
	ctx context.Context,
	home storelight.LightHomeIdentity,
	session storelight.LightSessionMetadata,
	pending storelight.LightTokenScan,
	discoverer *logsource.Discoverer,
	reader *logsource.SnapshotReader,
) (bool, error) {
	previous := snapshotFromStoredScan(pending)
	inspectPrevious := &previous
	if previous.Path != *session.RolloutPath {
		inspectPrevious = nil
	}
	current, err := discoverer.Inspect(ctx, *session.RolloutPath, inspectPrevious)
	if err != nil {
		return false, err
	}
	identity := identityFromSnapshot(home, current)
	decision := DecideRefresh(
		checkpointFromStoredScan(pending), homeIdentity(pending.Identity.Home), fileIdentity(identity), TokenParserVersion,
	)
	switch decision.Kind {
	case RefreshReuse:
		return runtime.scanPending(ctx, pending, current, reader)
	case RefreshAppend:
		if pendingNeedsIdentityUpdate(pending, identity) {
			if err := runtime.repository.UpdateLightTokenPendingIdentity(
				ctx, session.SessionID, pending.Identity, identity, TokenParserVersion, runtime.clock().UnixMilli(),
			); err != nil {
				return false, err
			}
			pending, err = runtime.repository.PendingLightTokenScan(ctx, session.SessionID)
			if err != nil {
				return false, errors.Join(storelight.ErrLightTokenConflict, err)
			}
		}
		return runtime.scanPending(ctx, pending, current, reader)
	case RefreshDefer:
		return false, storelight.ErrLightHomeFence
	case RefreshRebuild:
		generation, err := runtime.repository.RestartLightTokenPendingRebuild(
			ctx, session.SessionID, pending.Identity, identity, TokenParserVersion, runtime.clock().UnixMilli(),
		)
		if err != nil {
			return false, err
		}
		pending, err = runtime.repository.PendingLightTokenScan(ctx, session.SessionID)
		if err != nil || pending.Generation != generation {
			return false, errors.Join(storelight.ErrLightTokenConflict, err)
		}
		return runtime.scanPending(ctx, pending, current, reader)
	default:
		return false, storelight.ErrLightTokenConflict
	}
}

func pendingNeedsIdentityUpdate(pending storelight.LightTokenScan, identity storelight.LightRolloutIdentity) bool {
	return identity.SizeBytes != pending.Identity.SizeBytes ||
		identity.MTimeNS != pending.Identity.MTimeNS ||
		identity.FingerprintSHA256 != pending.Identity.FingerprintSHA256
}

func (runtime *Runtime) scanPending(
	ctx context.Context,
	pending storelight.LightTokenScan,
	snapshot logsource.Snapshot,
	reader *logsource.SnapshotReader,
) (bool, error) {
	budget := runtime.scanBatchBytes
	minimumBudget := snapshot.Fingerprint.PrefixBytes + 1
	if budget < minimumBudget {
		budget = minimumBudget
	}
	for {
		if err := ctx.Err(); err != nil {
			return false, err
		}
		startOffset := pending.Checkpoint.DurableOffset
		scanResult, readResult, err := scanSnapshotBatch(ctx, reader, snapshot, pending, budget)
		if err != nil {
			return false, err
		}
		checkpoint := storelight.LightTokenCheckpoint{
			DurableOffset: scanResult.DurableOffset,
			Complete:      readResult.EOF && scanResult.Complete,
			InputTokens:   scanResult.State.Aggregates.Input, CachedInputTokens: scanResult.State.Aggregates.CachedInput,
			OutputTokens: scanResult.State.Aggregates.Output, ReasoningTokens: scanResult.State.Aggregates.Reasoning,
			LastRawInputTokens:        presentCounterPointer(scanResult.State.LastRaw.Input),
			LastRawInputPresent:       scanResult.State.LastRaw.Input.Present,
			LastRawCachedInputTokens:  presentCounterPointer(scanResult.State.LastRaw.CachedInput),
			LastRawCachedInputPresent: scanResult.State.LastRaw.CachedInput.Present,
			LastRawOutputTokens:       presentCounterPointer(scanResult.State.LastRaw.Output),
			LastRawOutputPresent:      scanResult.State.LastRaw.Output.Present,
			LastRawReasoningTokens:    presentCounterPointer(scanResult.State.LastRaw.Reasoning),
			LastRawReasoningPresent:   scanResult.State.LastRaw.Reasoning.Present,
			CounterEpoch:              scanResult.State.CounterEpoch,
			CurrentModelKey:           scanResult.State.CurrentModelKey,
			CurrentModelSource:        scanResult.State.CurrentModelSource,
			PhysicalBytesRead:         pending.Checkpoint.PhysicalBytesRead + readResult.BytesRead,
			LinesSeen:                 pending.Checkpoint.LinesSeen + scanResult.LinesSeen,
			CandidateLines:            pending.Checkpoint.CandidateLines + scanResult.CandidateLines,
			JSONDecoded:               pending.Checkpoint.JSONDecoded + scanResult.JSONDecoded,
		}
		checkpoint.LatestEventAtMS = latestLightEventAt(pending.Checkpoint.LatestEventAtMS, scanResult)
		batch := storelight.LightTokenBatch{
			SessionID: pending.SessionID, Generation: pending.Generation, Checkpoint: checkpoint,
			DailyDeltas: dailyDeltasToStore(scanResult.DailyDeltas), Activate: checkpoint.Complete,
			TimedDeltas:      timedDeltasToStore(scanResult.TokenDeltas),
			InvocationDeltas: invocationDeltasToStore(scanResult.InvocationDeltas),
			UpdatedAtMS:      runtime.clock().UnixMilli(),
		}
		if err := runtime.repository.CommitLightTokenBatch(ctx, batch); err != nil {
			return false, err
		}
		if checkpoint.Complete {
			if runtime.batchCommitted != nil {
				active, _ := runtime.repository.ActiveLightTokenScan(ctx, pending.SessionID)
				runtime.batchCommitted(active)
			}
			return true, nil
		}
		pending, err = runtime.repository.PendingLightTokenScan(ctx, pending.SessionID)
		if err != nil {
			return false, err
		}
		if runtime.batchCommitted != nil {
			runtime.batchCommitted(pending)
		}
		if readResult.EOF {
			return false, nil
		}
		if pending.Checkpoint.DurableOffset == startOffset {
			if budget >= int64(DefaultTokenScanMaxLine)+snapshot.Fingerprint.PrefixBytes {
				return false, errTokenScanLineTooLong
			}
			budget *= 2
			if limit := int64(DefaultTokenScanMaxLine) + snapshot.Fingerprint.PrefixBytes; budget > limit {
				budget = limit
			}
		}
	}
}

func (runtime *Runtime) notifyRefreshCommitted() {
	if runtime.refreshCommitted != nil {
		runtime.refreshCommitted()
	}
}

func scanSnapshotBatch(
	ctx context.Context,
	reader *logsource.SnapshotReader,
	snapshot logsource.Snapshot,
	pending storelight.LightTokenScan,
	budget int64,
) (ScanResult, logsource.SnapshotReadResult, error) {
	pipeReader, pipeWriter := io.Pipe()
	type readOutcome struct {
		result logsource.SnapshotReadResult
		err    error
	}
	readDone := make(chan readOutcome, 1)
	go func() {
		result, err := reader.ReadLimited(
			ctx, snapshot, pending.Checkpoint.DurableOffset, budget,
			func(chunk []byte, _ bool) error {
				_, writeErr := pipeWriter.Write(chunk)
				return writeErr
			},
		)
		_ = pipeWriter.CloseWithError(err)
		readDone <- readOutcome{result: result, err: err}
	}()
	seed := ScanState{
		DurableOffset:      pending.Checkpoint.DurableOffset,
		CurrentModelKey:    pending.Checkpoint.CurrentModelKey,
		CurrentModelSource: pending.Checkpoint.CurrentModelSource,
		LastRaw:            lastRawFromCheckpoint(pending.Checkpoint),
		CounterEpoch:       pending.Checkpoint.CounterEpoch,
		Aggregates: TokenTotals{
			Input: pending.Checkpoint.InputTokens, CachedInput: pending.Checkpoint.CachedInputTokens,
			Output: pending.Checkpoint.OutputTokens, Reasoning: pending.Checkpoint.ReasoningTokens,
		},
	}
	scanResult, scanErr := NewTokenScanner(TokenScannerOptions{}).Scan(ctx, pipeReader, seed)
	_ = pipeReader.CloseWithError(scanErr)
	read := <-readDone
	if scanErr != nil {
		return scanResult, read.result, scanErr
	}
	if read.err != nil {
		return scanResult, read.result, read.err
	}
	return scanResult, read.result, nil
}

func identityFromSnapshot(home storelight.LightHomeIdentity, snapshot logsource.Snapshot) storelight.LightRolloutIdentity {
	identity := storelight.LightRolloutIdentity{
		Path: snapshot.Path, SourceFileID: snapshot.SourceFileID, Home: home,
		DeviceID: snapshot.Fingerprint.DeviceID, Inode: snapshot.Fingerprint.Inode,
		SizeBytes: snapshot.Fingerprint.SizeBytes, MTimeNS: snapshot.Fingerprint.MTimeNS,
		PrefixBytes: snapshot.Fingerprint.PrefixBytes, PrefixSHA256: snapshot.Fingerprint.PrefixSHA256,
		FingerprintSHA256: snapshot.Fingerprint.Digest,
	}
	if snapshot.Comparison != nil {
		identity.Comparison = &storelight.LightPrefixComparison{
			PrefixBytes: snapshot.Comparison.PrefixBytes, PrefixSHA256: snapshot.Comparison.PrefixSHA256,
		}
	}
	return identity
}

func snapshotFromStoredScan(scan storelight.LightTokenScan) logsource.Snapshot {
	kind := logsource.SourceKindSession
	if strings.Contains(scan.Identity.Path, string(filepath.Separator)+"archived_sessions"+string(filepath.Separator)) {
		kind = logsource.SourceKindArchivedSession
	}
	return logsource.Snapshot{
		SourceFileID: scan.Identity.SourceFileID, Provider: logsource.ProviderCodex, Kind: kind, Path: scan.Identity.Path,
		Fingerprint: logsource.Fingerprint{
			DeviceID: scan.Identity.DeviceID, Inode: scan.Identity.Inode, SizeBytes: scan.Identity.SizeBytes,
			MTimeNS: scan.Identity.MTimeNS, PrefixBytes: scan.Identity.PrefixBytes,
			PrefixSHA256: scan.Identity.PrefixSHA256, Digest: scan.Identity.FingerprintSHA256,
		},
	}
}

func checkpointFromStoredScan(scan storelight.LightTokenScan) *ScanCheckpoint {
	return &ScanCheckpoint{
		Home: homeIdentity(scan.Identity.Home), File: fileIdentity(scan.Identity), ParserVersion: scan.ParserVersion,
		DurableOffset: scan.Checkpoint.DurableOffset, Complete: scan.Checkpoint.Complete,
		HighWater: TokenTotals{
			Input: scan.Checkpoint.InputTokens, CachedInput: scan.Checkpoint.CachedInputTokens,
			Output: scan.Checkpoint.OutputTokens, Reasoning: scan.Checkpoint.ReasoningTokens,
		},
	}
}

func lastRawFromCheckpoint(checkpoint storelight.LightTokenCheckpoint) TokenCounterSnapshot {
	return TokenCounterSnapshot{
		Input:       optionalCounterFromStored(checkpoint.LastRawInputPresent, checkpoint.LastRawInputTokens),
		CachedInput: optionalCounterFromStored(checkpoint.LastRawCachedInputPresent, checkpoint.LastRawCachedInputTokens),
		Output:      optionalCounterFromStored(checkpoint.LastRawOutputPresent, checkpoint.LastRawOutputTokens),
		Reasoning:   optionalCounterFromStored(checkpoint.LastRawReasoningPresent, checkpoint.LastRawReasoningTokens),
	}
}

func optionalCounterFromStored(present bool, value *int64) OptionalCounter {
	if !present || value == nil {
		return OptionalCounter{}
	}
	return OptionalCounter{Value: *value, Present: true}
}

func presentCounterPointer(value OptionalCounter) *int64 {
	if !value.Present {
		return nil
	}
	cloned := value.Value
	return &cloned
}

func homeIdentity(value storelight.LightHomeIdentity) HomeIdentity {
	return HomeIdentity{Path: value.Path, DeviceID: value.DeviceID, Inode: value.Inode}
}

func fileIdentity(value storelight.LightRolloutIdentity) RolloutFileIdentity {
	identity := RolloutFileIdentity{
		Path: value.Path, DeviceID: value.DeviceID, Inode: value.Inode, SizeBytes: value.SizeBytes,
		MTimeNS: value.MTimeNS, PrefixBytes: value.PrefixBytes, PrefixSHA256: value.PrefixSHA256,
	}
	if value.Comparison != nil {
		identity.Comparison = &PrefixComparison{
			PrefixBytes: value.Comparison.PrefixBytes, PrefixSHA256: value.Comparison.PrefixSHA256,
		}
	}
	return identity
}

func dailyDeltasToStore(values []DailyTokenDelta) []storelight.LightTokenDailyDelta {
	output := make([]storelight.LightTokenDailyDelta, 0, len(values))
	for _, value := range values {
		day, err := time.Parse("2006-01-02", value.Day)
		if err != nil {
			continue
		}
		output = append(output, storelight.LightTokenDailyDelta{
			DayStartMS: day.UTC().UnixMilli(), InputTokens: value.Tokens.Input,
			CachedInputTokens: value.Tokens.CachedInput, OutputTokens: value.Tokens.Output,
			ReasoningTokens: value.Tokens.Reasoning,
		})
	}
	return output
}

func timedDeltasToStore(values []TimedTokenDelta) []storelight.LightTokenTimedDelta {
	output := make([]storelight.LightTokenTimedDelta, 0, len(values))
	for _, value := range values {
		output = append(output, storelight.LightTokenTimedDelta{
			SourceOffset: value.SourceOffset, ObservedAtMS: value.ObservedAtMS,
			ModelKey: value.ModelKey, ModelSource: value.ModelSource,
			InputTokens: value.Tokens.Input, CachedInputTokens: value.Tokens.CachedInput,
			OutputTokens: value.Tokens.Output, ReasoningTokens: value.Tokens.Reasoning,
		})
	}
	return output
}

func invocationDeltasToStore(values []InvocationDelta) []storelight.LightInvocationDelta {
	output := make([]storelight.LightInvocationDelta, 0, len(values))
	for _, value := range values {
		output = append(output, storelight.LightInvocationDelta{
			SourceOffset: value.SourceOffset, Ordinal: value.Ordinal, ObservedAtMS: value.ObservedAtMS,
			Kind: string(value.Kind), Name: value.Name, Source: string(value.Source),
			Outcome: string(value.Outcome), DurationMS: cloneInt64(value.DurationMS),
		})
	}
	return output
}

func latestLightEventAt(previous *int64, result ScanResult) *int64 {
	var latest *int64
	if previous != nil {
		value := *previous
		latest = &value
	}
	for _, delta := range result.TokenDeltas {
		if latest == nil || delta.ObservedAtMS > *latest {
			value := delta.ObservedAtMS
			latest = &value
		}
	}
	for _, delta := range result.InvocationDeltas {
		if latest == nil || delta.ObservedAtMS > *latest {
			value := delta.ObservedAtMS
			latest = &value
		}
	}
	return latest
}
