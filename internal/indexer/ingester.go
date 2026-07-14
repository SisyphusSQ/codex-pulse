package indexer

import (
	"context"
	"errors"
	"fmt"
	"sync"

	"github.com/SisyphusSQ/codex-pulse/internal/codex/logs"
	"github.com/SisyphusSQ/codex-pulse/internal/store"
)

var (
	ErrInvalidIngester    = errors.New("invalid rollout ingester")
	ErrInvalidOpenRequest = errors.New("invalid rollout open request")
	ErrNoStream           = errors.New("reconcile action has no readable rollout stream")
	ErrStreamInvalidated  = errors.New("rollout stream was invalidated by a failed feed")
)

type Ingester struct {
	repository *store.Repository
}

type OpenRequest struct {
	Action logs.ReconcileAction
	JobID  string
	AtMS   int64
}

type CommitResult struct {
	Cursor            store.GenerationCursor
	Stats             logs.ParseStats
	ReadOffset        int64
	CommittableOffset int64
	BufferedBytes     int
	Committed         bool
}

type Stream struct {
	mu sync.Mutex

	repository          *store.Repository
	parser              *logs.StreamParser
	projector           *projector
	cursor              store.GenerationCursor
	readOffset          int64
	previousFingerprint *store.SourceFingerprint
	pendingFingerprint  bool
	job                 *streamJob
	poisoned            bool
}

type streamJob struct {
	jobID string
	state store.JobState
	phase store.JobPhase
}

func New(repository *store.Repository) (*Ingester, error) {
	if repository == nil {
		return nil, ErrInvalidIngester
	}
	return &Ingester{repository: repository}, nil
}

func (ingester *Ingester) Open(ctx context.Context, request OpenRequest) (*Stream, error) {
	if ingester == nil || ingester.repository == nil {
		return nil, ErrInvalidIngester
	}
	if request.AtMS < 0 {
		return nil, invalidOpenRequest("timestamp is invalid")
	}
	mode, previous, current, replaces, err := generationRequestFromAction(request.Action)
	if err != nil {
		return nil, err
	}
	if current.SourceKind == string(logs.SourceKindSessionIndex) {
		return nil, logs.ErrUnsupportedParserSource
	}
	if file, lookupErr := ingester.repository.SourceFile(ctx, current.SourceFileID); lookupErr == nil {
		if file.ParserVersion != logs.ParserVersion {
			mode = store.GenerationModeRebuild
		}
	} else if !errors.Is(lookupErr, store.ErrNotFound) {
		return nil, lookupErr
	}
	building, err := ingester.buildingForOpen(ctx, current, previous, replaces)
	if err != nil {
		return nil, err
	}
	var supersedeBuilding *store.BuildingGenerationExpectation
	if building != nil {
		mode = store.GenerationModeRebuild
		if building.SourceFileID != current.SourceFileID || building.Fingerprint != current ||
			building.ParserVersion != logs.ParserVersion ||
			!buildingLineageMatchesOpen(building.ReplacesSourceFileID, replaces) {
			supersedeBuilding = &store.BuildingGenerationExpectation{
				SourceFileID: building.SourceFileID, Generation: building.Generation,
				FingerprintSHA256: building.Fingerprint.FingerprintSHA256,
				ParserVersion:     building.ParserVersion,
			}
			candidateFingerprint := building.Fingerprint
			previous = &candidateFingerprint
			if building.SourceFileID != current.SourceFileID {
				candidateSourceFileID := building.SourceFileID
				replaces = &candidateSourceFileID
			}
		}
	}
	var jobState *streamJob
	if request.JobID != "" {
		job, err := ingester.repository.JobRun(ctx, request.JobID)
		if err != nil {
			return nil, err
		}
		if job.SourceFileID == nil || *job.SourceFileID != current.SourceFileID ||
			(job.State != store.JobQueued && job.State != store.JobRunning) {
			return nil, invalidOpenRequest("job does not own the readable source")
		}
		jobState = &streamJob{jobID: job.JobID, state: job.State, phase: job.Phase}
	}
	cursor, err := ingester.repository.PrepareGeneration(ctx, store.PrepareGenerationRequest{
		Mode: mode, Previous: previous, Current: current, ParserVersion: logs.ParserVersion,
		ReplacesSourceFileID: replaces, SupersedeBuilding: supersedeBuilding, AtMS: request.AtMS,
	})
	if err != nil {
		return nil, err
	}
	seed := parserSeedFromCheckpoint(cursor.Checkpoint.Seed)
	currentSourceKind := logs.SourceKind(cursor.Fingerprint.SourceKind)
	if seed != nil && seed.Session != nil {
		seed.Session.SourceKind = currentSourceKind
	}
	parser, err := logs.NewStreamParser(logs.ParserConfig{
		SourceKind:  currentSourceKind,
		StartOffset: cursor.Checkpoint.CommittedOffset, Seed: seed,
	})
	if err != nil {
		return nil, err
	}
	projectorCheckpoint, err := ingester.projectorCheckpointForOpen(ctx, cursor)
	if err != nil {
		return nil, err
	}
	projector, err := newProjector(cursor.Generation, mode, seed, projectorCheckpoint)
	if err != nil {
		return nil, err
	}
	stream := &Stream{
		repository: ingester.repository, parser: parser, projector: projector,
		cursor: cursor, readOffset: cursor.Checkpoint.CommittedOffset, job: jobState,
	}
	// PreviousFingerprint is the active append CAS token. Rebuild lineage is
	// persisted by PrepareGeneration and must not be compared as the new source's
	// own fingerprint when a physical replacement changes SourceFileID.
	if mode == store.GenerationModeAppend && previous != nil && *previous != current {
		copy := *previous
		stream.previousFingerprint = &copy
		stream.pendingFingerprint = true
	}
	return stream, nil
}

func buildingLineageMatchesOpen(persisted, observed *string) bool {
	return equalOptionalString(persisted, observed) || observed == nil && persisted != nil
}

func (ingester *Ingester) projectorCheckpointForOpen(
	ctx context.Context,
	cursor store.GenerationCursor,
) (store.ProjectorCheckpoint, error) {
	checkpoint := cursor.Checkpoint.Projector
	if checkpoint.SessionSourceKind != "" || cursor.Base == nil ||
		cursor.SourceFileID != cursor.Base.SourceFileID {
		return checkpoint, nil
	}
	base, err := ingester.repository.GenerationCursor(
		ctx, cursor.Base.SourceFileID, cursor.Base.Generation,
	)
	if err != nil {
		return store.ProjectorCheckpoint{}, err
	}
	if base.State != store.GenerationActive ||
		base.Fingerprint.FingerprintSHA256 != cursor.Base.FingerprintSHA256 {
		return store.ProjectorCheckpoint{}, invalidOpenRequest("building base fingerprint changed")
	}
	checkpoint.SessionSourceKind = base.Checkpoint.Projector.SessionSourceKind
	if checkpoint.SessionSourceKind == "" && base.Checkpoint.Seed != nil && base.Checkpoint.Seed.Session != nil {
		checkpoint.SessionSourceKind = base.Checkpoint.Seed.Session.SourceKind
	}
	return checkpoint, nil
}

func (ingester *Ingester) buildingForOpen(
	ctx context.Context,
	current store.SourceFingerprint,
	previous *store.SourceFingerprint,
	replaces *string,
) (*store.GenerationCursor, error) {
	cursors, err := ingester.repository.BuildingGenerationCursors(ctx)
	if err != nil {
		return nil, err
	}
	// source_file_id 是 durable reconcile identity。崩溃前允许同 base/path 的
	// sibling building 并存时，恢复自己的 source 必须优先于较弱的 path/lineage
	// 匹配，才能进入 EOF activation 原子清理竞争者。
	for _, cursor := range cursors {
		if cursor.SourceFileID != current.SourceFileID {
			continue
		}
		copy := cursor
		return &copy, nil
	}
	var candidate *store.GenerationCursor
	for _, cursor := range cursors {
		if !buildingMatchesOpen(cursor, current, previous, replaces) {
			continue
		}
		if candidate != nil {
			return nil, invalidOpenRequest("multiple building generations match the reconcile action")
		}
		copy := cursor
		candidate = &copy
	}
	return candidate, nil
}

func buildingMatchesOpen(
	cursor store.GenerationCursor,
	current store.SourceFingerprint,
	previous *store.SourceFingerprint,
	replaces *string,
) bool {
	if cursor.SourceFileID == current.SourceFileID || cursor.Fingerprint.CurrentPath == current.CurrentPath {
		return true
	}
	identifiers := make([]string, 0, 2)
	if previous != nil {
		identifiers = append(identifiers, previous.SourceFileID)
	}
	if replaces != nil {
		identifiers = append(identifiers, *replaces)
	}
	for _, sourceFileID := range identifiers {
		if cursor.SourceFileID == sourceFileID ||
			cursor.ReplacesSourceFileID != nil && *cursor.ReplacesSourceFileID == sourceFileID ||
			cursor.Base != nil && cursor.Base.SourceFileID == sourceFileID {
			return true
		}
	}
	return false
}

func equalOptionalString(left, right *string) bool {
	if left == nil || right == nil {
		return left == nil && right == nil
	}
	return *left == *right
}

func (stream *Stream) Feed(
	ctx context.Context,
	chunk []byte,
	eof bool,
	atMS int64,
) (CommitResult, error) {
	if stream == nil {
		return CommitResult{}, ErrInvalidIngester
	}
	stream.mu.Lock()
	defer stream.mu.Unlock()
	if stream.poisoned {
		return CommitResult{}, ErrStreamInvalidated
	}
	if atMS < 0 || int64(len(chunk)) > stream.cursor.Fingerprint.SizeBytes-stream.readOffset {
		return CommitResult{}, stream.invalidate(invalidOpenRequest("feed exceeds the opened source snapshot"))
	}
	result, err := stream.parser.Feed(stream.readOffset, chunk)
	if err != nil {
		return CommitResult{}, stream.invalidate(err)
	}
	if eof && result.ReadOffset != stream.cursor.Fingerprint.SizeBytes {
		return CommitResult{}, stream.invalidate(invalidOpenRequest("EOF does not match the opened source size"))
	}
	facts, projectorCheckpoint, err := stream.projector.Project(result.Events)
	if err != nil {
		return CommitResult{}, stream.invalidate(err)
	}
	stream.readOffset = result.ReadOffset
	output := CommitResult{
		Cursor: stream.cursor, Stats: result.Stats, ReadOffset: result.ReadOffset,
		CommittableOffset: result.CommittableOffset, BufferedBytes: result.BufferedBytes,
	}
	shouldCommit := result.CommittableOffset > stream.cursor.Checkpoint.CommittedOffset ||
		(eof && (stream.cursor.State == store.GenerationBuilding || stream.pendingFingerprint))
	if !shouldCommit {
		return output, nil
	}
	seedCheckpoint := parserSeedToCheckpoint(result.NextSeed)
	if result.CommittableOffset == 0 {
		seedCheckpoint = nil
	}
	checkpoint := store.ParserCheckpoint{
		Version: store.ParserCheckpointVersion, ParserVersion: logs.ParserVersion,
		CommittedOffset: result.CommittableOffset, Seed: seedCheckpoint,
		Projector: projectorCheckpoint,
	}
	batch := store.IngestBatch{
		SourceFileID: stream.cursor.SourceFileID, Generation: stream.cursor.Generation,
		PreviousCommittedOffset: stream.cursor.Checkpoint.CommittedOffset,
		PreviousFingerprint:     cloneSourceFingerprint(stream.previousFingerprint),
		Fingerprint:             stream.cursor.Fingerprint, Facts: facts,
		Diagnostics: diagnosticsToStore(result.Diagnostics), Checkpoint: checkpoint,
		EOF: eof, AtMS: atMS,
	}
	if stream.job != nil {
		progress, total := result.CommittableOffset, stream.cursor.Fingerprint.SizeBytes
		batch.JobTransition = &store.JobTransition{
			JobID: stream.job.jobID, ExpectedState: stream.job.state, State: store.JobRunning,
			Phase: stream.job.phase, ProgressCurrent: &progress, ProgressTotal: &total,
			ResumeCursor: &store.JobCursor{Generation: stream.cursor.Generation, Offset: result.CommittableOffset},
			AtMS:         atMS,
		}
	}
	committed, err := stream.repository.CommitIngestBatch(ctx, batch)
	if err != nil {
		return CommitResult{}, stream.invalidate(err)
	}
	stream.cursor = committed
	stream.previousFingerprint = nil
	stream.pendingFingerprint = false
	if stream.job != nil {
		stream.job.state = store.JobRunning
	}
	output.Cursor = committed
	output.Committed = true
	return output, nil
}

func (stream *Stream) invalidate(err error) error {
	stream.poisoned = true
	return err
}

func generationRequestFromAction(
	action logs.ReconcileAction,
) (
	store.GenerationMode,
	*store.SourceFingerprint,
	store.SourceFingerprint,
	*string,
	error,
) {
	switch action.Kind {
	case logs.ChangeDeleted, logs.ChangeUnreadable:
		return "", nil, store.SourceFingerprint{}, nil, ErrNoStream
	case logs.ChangeAdded, logs.ChangeTruncated, logs.ChangeReplaced,
		logs.ChangeGrown, logs.ChangeMoved, logs.ChangeUnchanged:
	default:
		return "", nil, store.SourceFingerprint{}, nil, invalidOpenRequest("reconcile action kind is invalid")
	}
	if action.Current == nil {
		return "", nil, store.SourceFingerprint{}, nil, invalidOpenRequest("reconcile action has no current snapshot")
	}
	current := sourceFingerprintFromSnapshot(*action.Current)
	var previous *store.SourceFingerprint
	if action.Previous != nil {
		value := sourceFingerprintFromSnapshot(*action.Previous)
		previous = &value
	}
	mode := store.GenerationModeAppend
	switch action.Kind {
	case logs.ChangeAdded, logs.ChangeTruncated, logs.ChangeReplaced:
		mode = store.GenerationModeRebuild
	}
	var replaces *string
	if action.Kind == logs.ChangeReplaced && action.Previous != nil &&
		action.Previous.SourceFileID != action.Current.SourceFileID {
		value := action.Previous.SourceFileID
		replaces = &value
	}
	return mode, previous, current, replaces, nil
}

func sourceFingerprintFromSnapshot(snapshot logs.Snapshot) store.SourceFingerprint {
	return store.SourceFingerprint{
		SourceFileID: snapshot.SourceFileID, Provider: snapshot.Provider,
		SourceKind: string(snapshot.Kind), CurrentPath: snapshot.Path,
		DeviceID: snapshot.Fingerprint.DeviceID, Inode: snapshot.Fingerprint.Inode,
		SizeBytes: snapshot.Fingerprint.SizeBytes, MTimeNS: snapshot.Fingerprint.MTimeNS,
		PrefixBytes:       snapshot.Fingerprint.PrefixBytes,
		PrefixSHA256:      snapshot.Fingerprint.PrefixSHA256,
		FingerprintSHA256: snapshot.Fingerprint.Digest,
	}
}

func diagnosticsToStore(values []logs.ParserDiagnostic) []store.IngestDiagnostic {
	diagnostics := make([]store.IngestDiagnostic, len(values))
	for index, value := range values {
		diagnostics[index] = store.IngestDiagnostic{
			Class: string(value.Class), Code: string(value.Code),
			StartOffset: value.StartOffset, EndOffset: value.EndOffset, Retryable: value.Retryable,
		}
	}
	return diagnostics
}

func cloneSourceFingerprint(value *store.SourceFingerprint) *store.SourceFingerprint {
	if value == nil {
		return nil
	}
	copy := *value
	return &copy
}

func invalidOpenRequest(message string) error {
	return fmt.Errorf("%w: %s", ErrInvalidOpenRequest, message)
}
