package lightindex

import (
	"context"
	"errors"
	"io"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/SisyphusSQ/codex-pulse/internal/codex/appserver"
	"github.com/SisyphusSQ/codex-pulse/internal/codex/logs"
	"github.com/SisyphusSQ/codex-pulse/internal/store"
)

const defaultScanBatchBytes int64 = 8 << 20

type MetadataProvider interface {
	List(context.Context, string) (appserver.ThreadList, error)
}

type RuntimeConfig struct {
	Repository      *store.Repository
	Metadata        MetadataProvider
	ScanBatchBytes  int64
	Clock           func() time.Time
	BeforeTokenScan func(context.Context) error
	BatchCommitted  func(store.LightTokenScan)
}

type Runtime struct {
	repository      *store.Repository
	metadata        MetadataProvider
	scanBatchBytes  int64
	clock           func() time.Time
	beforeTokenScan func(context.Context) error
	batchCommitted  func(store.LightTokenScan)
	deepMu          sync.Mutex
}

type Run struct {
	cancel context.CancelFunc
	done   chan error
	once   sync.Once
}

func NewRuntime(config RuntimeConfig) (*Runtime, error) {
	if config.Repository == nil || config.Metadata == nil {
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
		repository: config.Repository, metadata: config.Metadata, scanBatchBytes: batchBytes,
		clock: clock, beforeTokenScan: config.BeforeTokenScan, batchCommitted: config.BatchCommitted,
	}, nil
}

func (runtime *Runtime) Start(ctx context.Context, home store.LightHomeIdentity) (*Run, error) {
	if !runtime.validStart(ctx, home) {
		return nil, errors.New("invalid lightweight index start")
	}
	metadata, err := runtime.metadata.List(ctx, home.Path)
	if err != nil {
		return nil, err
	}
	generation := int64(1)
	state, stateErr := runtime.repository.LightIndexState(ctx)
	switch {
	case stateErr == nil:
		if state.Home != home {
			return nil, store.ErrLightHomeFence
		}
		generation = state.MetadataGeneration + 1
	case errors.Is(stateErr, store.ErrNotFound):
	default:
		return nil, stateErr
	}
	snapshot := runtime.metadataSnapshot(home, generation, metadata)
	if err := runtime.repository.ReplaceLightMetadata(ctx, snapshot); err != nil {
		return nil, err
	}
	return runtime.startWorker(ctx, home), nil
}

// StartHomeSwitch publishes metadata for a newly confirmed Home only when the
// currently indexed Home still matches expected. The metadata RPC completes
// before old lightweight derived rows are replaced.
func (runtime *Runtime) StartHomeSwitch(
	ctx context.Context,
	expected store.LightHomeIdentity,
	next store.LightHomeIdentity,
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
	return runtime.startWorker(ctx, next), nil
}

func (runtime *Runtime) validStart(ctx context.Context, home store.LightHomeIdentity) bool {
	return runtime != nil && runtime.repository != nil && runtime.metadata != nil && ctx != nil &&
		home.Path != "" && home.DeviceID != "" && home.Inode > 0
}

func (runtime *Runtime) metadataSnapshot(
	home store.LightHomeIdentity,
	generation int64,
	metadata appserver.ThreadList,
) store.LightMetadataSnapshot {
	snapshot := store.LightMetadataSnapshot{
		Home: home, Generation: generation, ReadyAtMS: runtime.clock().UnixMilli(),
		Sessions: make([]store.LightSessionMetadata, 0, len(metadata.Threads)),
	}
	for _, thread := range metadata.Threads {
		snapshot.Sessions = append(snapshot.Sessions, store.LightSessionMetadata{
			SessionID: thread.SessionID, ThreadName: thread.Name, CWD: thread.CWD,
			RolloutPath: thread.RolloutPath, CreatedAtMS: thread.CreatedAtMS,
			UpdatedAtMS: thread.UpdatedAtMS, RecencyAtMS: thread.RecencyAtMS,
		})
	}
	return snapshot
}

func (runtime *Runtime) startWorker(ctx context.Context, home store.LightHomeIdentity) *Run {
	workerCtx, cancel := context.WithCancel(ctx)
	run := &Run{cancel: cancel, done: make(chan error, 1)}
	go func() {
		run.done <- runtime.scanAll(workerCtx, home)
		close(run.done)
	}()
	return run
}

func (run *Run) Cancel() {
	if run == nil || run.cancel == nil {
		return
	}
	run.once.Do(run.cancel)
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

func (runtime *Runtime) scanAll(ctx context.Context, home store.LightHomeIdentity) error {
	if runtime.beforeTokenScan != nil {
		if err := runtime.beforeTokenScan(ctx); err != nil {
			return err
		}
	}
	discoverer, err := logs.NewConfirmedDiscoverer(home.Path, home.DeviceID, home.Inode)
	if err != nil {
		return err
	}
	reader, err := logs.NewConfirmedSnapshotReader(
		home.Path, home.DeviceID, home.Inode, DefaultTokenScanChunkBytes,
	)
	if err != nil {
		return err
	}
	sessions, err := runtime.repository.ListLightSessions(ctx)
	if err != nil {
		return err
	}
	for _, session := range sessions {
		if err := ctx.Err(); err != nil {
			return err
		}
		if session.RolloutPath == nil {
			continue
		}
		if err := runtime.scanSession(ctx, home, session, discoverer, reader); err != nil {
			return err
		}
	}
	return nil
}

func (runtime *Runtime) scanSession(
	ctx context.Context,
	home store.LightHomeIdentity,
	session store.LightSessionMetadata,
	discoverer *logs.Discoverer,
	reader *logs.SnapshotReader,
) error {
	pending, pendingErr := runtime.repository.PendingLightTokenScan(ctx, session.SessionID)
	if pendingErr == nil {
		previous := snapshotFromStoredScan(pending)
		current, err := discoverer.Inspect(ctx, *session.RolloutPath, &previous)
		if err != nil {
			return err
		}
		if !sameStoredSnapshot(pending, current) {
			return store.ErrLightTokenConflict
		}
		return runtime.scanPending(ctx, pending, current, reader)
	}
	if !errors.Is(pendingErr, store.ErrNotFound) {
		return pendingErr
	}

	active, activeErr := runtime.repository.ActiveLightTokenScan(ctx, session.SessionID)
	var previous *logs.Snapshot
	if activeErr == nil {
		value := snapshotFromStoredScan(active)
		previous = &value
		if active.ParserVersion == TokenParserVersion {
			unchanged, err := discoverer.Unchanged(ctx, *session.RolloutPath, value)
			if err != nil {
				return err
			}
			if unchanged {
				return nil
			}
		}
	} else if !errors.Is(activeErr, store.ErrNotFound) {
		return activeErr
	}
	current, err := discoverer.Inspect(ctx, *session.RolloutPath, previous)
	if err != nil {
		return err
	}
	identity := identityFromSnapshot(home, current)
	if activeErr == nil {
		decision := DecideRefresh(
			checkpointFromStoredScan(active), homeIdentity(active.Identity.Home), fileIdentity(identity), TokenParserVersion,
		)
		switch decision.Kind {
		case RefreshReuse:
			return nil
		case RefreshAppend:
			generation, err := runtime.repository.StartLightTokenAppend(
				ctx, session.SessionID, identity, TokenParserVersion, runtime.clock().UnixMilli(),
			)
			if err != nil {
				return err
			}
			pending, err = runtime.repository.PendingLightTokenScan(ctx, session.SessionID)
			if err != nil || pending.Generation != generation {
				return errors.Join(store.ErrLightTokenConflict, err)
			}
			return runtime.scanPending(ctx, pending, current, reader)
		case RefreshDefer:
			return store.ErrLightHomeFence
		}
	}
	generation, err := runtime.repository.StartLightTokenRebuild(
		ctx, session.SessionID, identity, TokenParserVersion, runtime.clock().UnixMilli(),
	)
	if err != nil {
		return err
	}
	pending, err = runtime.repository.PendingLightTokenScan(ctx, session.SessionID)
	if err != nil || pending.Generation != generation {
		return errors.Join(store.ErrLightTokenConflict, err)
	}
	return runtime.scanPending(ctx, pending, current, reader)
}

func (runtime *Runtime) scanPending(
	ctx context.Context,
	pending store.LightTokenScan,
	snapshot logs.Snapshot,
	reader *logs.SnapshotReader,
) error {
	budget := runtime.scanBatchBytes
	minimumBudget := snapshot.Fingerprint.PrefixBytes + 1
	if budget < minimumBudget {
		budget = minimumBudget
	}
	for {
		if err := ctx.Err(); err != nil {
			return err
		}
		startOffset := pending.Checkpoint.DurableOffset
		scanResult, readResult, err := scanSnapshotBatch(ctx, reader, snapshot, pending, budget)
		if err != nil {
			return err
		}
		checkpoint := store.LightTokenCheckpoint{
			DurableOffset: scanResult.DurableOffset,
			Complete:      readResult.EOF && scanResult.Complete,
			InputTokens:   scanResult.State.HighWater.Input, CachedInputTokens: scanResult.State.HighWater.CachedInput,
			OutputTokens: scanResult.State.HighWater.Output, ReasoningTokens: scanResult.State.HighWater.Reasoning,
			CurrentModelKey:    scanResult.State.CurrentModelKey,
			CurrentModelSource: scanResult.State.CurrentModelSource,
			PhysicalBytesRead:  pending.Checkpoint.PhysicalBytesRead + readResult.BytesRead,
			LinesSeen:          pending.Checkpoint.LinesSeen + scanResult.LinesSeen,
			CandidateLines:     pending.Checkpoint.CandidateLines + scanResult.CandidateLines,
			JSONDecoded:        pending.Checkpoint.JSONDecoded + scanResult.JSONDecoded,
		}
		batch := store.LightTokenBatch{
			SessionID: pending.SessionID, Generation: pending.Generation, Checkpoint: checkpoint,
			DailyDeltas: dailyDeltasToStore(scanResult.DailyDeltas), Activate: checkpoint.Complete,
			TimedDeltas: timedDeltasToStore(scanResult.TokenDeltas),
			UpdatedAtMS: runtime.clock().UnixMilli(),
		}
		if err := runtime.repository.CommitLightTokenBatch(ctx, batch); err != nil {
			return err
		}
		if checkpoint.Complete {
			if runtime.batchCommitted != nil {
				active, _ := runtime.repository.ActiveLightTokenScan(ctx, pending.SessionID)
				runtime.batchCommitted(active)
			}
			return nil
		}
		pending, err = runtime.repository.PendingLightTokenScan(ctx, pending.SessionID)
		if err != nil {
			return err
		}
		if runtime.batchCommitted != nil {
			runtime.batchCommitted(pending)
		}
		if readResult.EOF {
			return nil
		}
		if pending.Checkpoint.DurableOffset == startOffset {
			if budget >= int64(DefaultTokenScanMaxLine)+snapshot.Fingerprint.PrefixBytes {
				return errors.New("token scan line exceeds maximum")
			}
			budget *= 2
			if limit := int64(DefaultTokenScanMaxLine) + snapshot.Fingerprint.PrefixBytes; budget > limit {
				budget = limit
			}
		}
	}
}

func scanSnapshotBatch(
	ctx context.Context,
	reader *logs.SnapshotReader,
	snapshot logs.Snapshot,
	pending store.LightTokenScan,
	budget int64,
) (ScanResult, logs.SnapshotReadResult, error) {
	pipeReader, pipeWriter := io.Pipe()
	type readOutcome struct {
		result logs.SnapshotReadResult
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
		HighWater: TokenTotals{
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

func identityFromSnapshot(home store.LightHomeIdentity, snapshot logs.Snapshot) store.LightRolloutIdentity {
	return store.LightRolloutIdentity{
		Path: snapshot.Path, SourceFileID: snapshot.SourceFileID, Home: home,
		DeviceID: snapshot.Fingerprint.DeviceID, Inode: snapshot.Fingerprint.Inode,
		SizeBytes: snapshot.Fingerprint.SizeBytes, MTimeNS: snapshot.Fingerprint.MTimeNS,
		PrefixBytes: snapshot.Fingerprint.PrefixBytes, PrefixSHA256: snapshot.Fingerprint.PrefixSHA256,
		FingerprintSHA256: snapshot.Fingerprint.Digest,
	}
}

func snapshotFromStoredScan(scan store.LightTokenScan) logs.Snapshot {
	kind := logs.SourceKindSession
	if strings.Contains(scan.Identity.Path, string(filepath.Separator)+"archived_sessions"+string(filepath.Separator)) {
		kind = logs.SourceKindArchivedSession
	}
	return logs.Snapshot{
		SourceFileID: scan.Identity.SourceFileID, Provider: logs.ProviderCodex, Kind: kind, Path: scan.Identity.Path,
		Fingerprint: logs.Fingerprint{
			DeviceID: scan.Identity.DeviceID, Inode: scan.Identity.Inode, SizeBytes: scan.Identity.SizeBytes,
			MTimeNS: scan.Identity.MTimeNS, PrefixBytes: scan.Identity.PrefixBytes,
			PrefixSHA256: scan.Identity.PrefixSHA256, Digest: scan.Identity.FingerprintSHA256,
		},
	}
}

func sameStoredSnapshot(scan store.LightTokenScan, snapshot logs.Snapshot) bool {
	identity := scan.Identity
	return identity.Path == snapshot.Path && identity.SourceFileID == snapshot.SourceFileID &&
		identity.DeviceID == snapshot.Fingerprint.DeviceID && identity.Inode == snapshot.Fingerprint.Inode &&
		identity.SizeBytes == snapshot.Fingerprint.SizeBytes && identity.MTimeNS == snapshot.Fingerprint.MTimeNS &&
		identity.PrefixBytes == snapshot.Fingerprint.PrefixBytes && identity.PrefixSHA256 == snapshot.Fingerprint.PrefixSHA256 &&
		identity.FingerprintSHA256 == snapshot.Fingerprint.Digest && scan.ParserVersion == TokenParserVersion
}

func checkpointFromStoredScan(scan store.LightTokenScan) *ScanCheckpoint {
	return &ScanCheckpoint{
		Home: homeIdentity(scan.Identity.Home), File: fileIdentity(scan.Identity), ParserVersion: scan.ParserVersion,
		DurableOffset: scan.Checkpoint.DurableOffset, Complete: scan.Checkpoint.Complete,
		HighWater: TokenTotals{
			Input: scan.Checkpoint.InputTokens, CachedInput: scan.Checkpoint.CachedInputTokens,
			Output: scan.Checkpoint.OutputTokens, Reasoning: scan.Checkpoint.ReasoningTokens,
		},
	}
}

func homeIdentity(value store.LightHomeIdentity) HomeIdentity {
	return HomeIdentity{Path: value.Path, DeviceID: value.DeviceID, Inode: value.Inode}
}

func fileIdentity(value store.LightRolloutIdentity) RolloutFileIdentity {
	return RolloutFileIdentity{
		Path: value.Path, DeviceID: value.DeviceID, Inode: value.Inode, SizeBytes: value.SizeBytes,
		MTimeNS: value.MTimeNS, PrefixBytes: value.PrefixBytes, PrefixSHA256: value.PrefixSHA256,
	}
}

func dailyDeltasToStore(values []DailyTokenDelta) []store.LightTokenDailyDelta {
	output := make([]store.LightTokenDailyDelta, 0, len(values))
	for _, value := range values {
		day, err := time.Parse("2006-01-02", value.Day)
		if err != nil {
			continue
		}
		output = append(output, store.LightTokenDailyDelta{
			DayStartMS: day.UTC().UnixMilli(), InputTokens: value.Tokens.Input,
			CachedInputTokens: value.Tokens.CachedInput, OutputTokens: value.Tokens.Output,
			ReasoningTokens: value.Tokens.Reasoning,
		})
	}
	return output
}

func timedDeltasToStore(values []TimedTokenDelta) []store.LightTokenTimedDelta {
	output := make([]store.LightTokenTimedDelta, 0, len(values))
	for _, value := range values {
		output = append(output, store.LightTokenTimedDelta{
			SourceOffset: value.SourceOffset, ObservedAtMS: value.ObservedAtMS,
			ModelKey: value.ModelKey, ModelSource: value.ModelSource,
			InputTokens: value.Tokens.Input, CachedInputTokens: value.Tokens.CachedInput,
			OutputTokens: value.Tokens.Output, ReasoningTokens: value.Tokens.Reasoning,
		})
	}
	return output
}
