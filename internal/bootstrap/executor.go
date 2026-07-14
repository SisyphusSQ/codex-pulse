package bootstrap

import (
	"context"
	"errors"
	"io/fs"

	"github.com/SisyphusSQ/codex-pulse/internal/codex/logs"
	"github.com/SisyphusSQ/codex-pulse/internal/indexer"
	"github.com/SisyphusSQ/codex-pulse/internal/store"
)

func (runtime *Runtime) Run(ctx context.Context, jobID string) (RunReport, error) {
	if runtime == nil || runtime.repository == nil || jobID == "" {
		return RunReport{}, ErrInvalidRuntime
	}
	job, facts, err := runtime.repository.BootstrapRun(ctx, jobID)
	if err != nil {
		return RunReport{}, err
	}
	if job.State == store.JobSucceeded {
		return reportFromRun(job, facts), nil
	}
	if job.State != store.JobQueued && job.State != store.JobRunning {
		return reportFromRun(job, facts), ErrSourceUnavailable
	}
	runCtx, release, err := runtime.registerRun(ctx, facts.HomeGeneration, jobID)
	if err != nil {
		return reportFromRun(job, facts), err
	}
	defer release()

	err = runtime.execute(runCtx, jobID)
	if err != nil {
		writeCtx := context.WithoutCancel(ctx)
		var terminalErr error
		if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
			var pause *store.BootstrapPauseReason
			if runtime.isDraining(facts.HomeGeneration) {
				value := store.BootstrapPauseApplicationDraining
				pause = &value
			}
			terminalErr = runtime.terminate(writeCtx, jobID, store.JobInterrupted, err, pause)
		} else {
			terminalErr = runtime.terminate(writeCtx, jobID, store.JobFailed, err, sourcePauseReason(err))
		}
		err = errors.Join(err, terminalErr)
	}
	job, facts, readErr := runtime.repository.BootstrapRun(context.WithoutCancel(ctx), jobID)
	if readErr != nil {
		return RunReport{}, errors.Join(err, readErr)
	}
	return reportFromRun(job, facts), err
}

func (runtime *Runtime) execute(ctx context.Context, jobID string) error {
	job, facts, err := runtime.repository.BootstrapRun(ctx, jobID)
	if err != nil {
		return err
	}
	if facts.PlanState != store.BootstrapPlanReady {
		return ErrInvalidRuntime
	}
	if job.State == store.JobQueued {
		if err := runtime.promoteQueuedAttempt(ctx, job, facts); err != nil {
			return err
		}
		job.State = store.JobRunning
	}
	if job.Phase == store.JobPhaseDiscover {
		if err := runtime.transitionPhase(ctx, jobID, store.JobPhaseFastBootstrap, store.BootstrapLaneFast); err != nil {
			return err
		}
		job.Phase = store.JobPhaseFastBootstrap
	}
	if job.Phase == store.JobPhaseFastBootstrap {
		if err := runtime.processLane(ctx, jobID, store.BootstrapLaneFast, nil); err != nil {
			return err
		}
		if err := runtime.ensureFirstScreenReady(ctx, jobID); err != nil {
			return err
		}
		if err := runtime.transitionPhase(
			ctx, jobID, store.JobPhaseHistoryBackfill, store.BootstrapLaneBackfill,
		); err != nil {
			return err
		}
		job.Phase = store.JobPhaseHistoryBackfill
	}
	if job.Phase == store.JobPhaseHistoryBackfill {
		if err := runtime.processLane(ctx, jobID, store.BootstrapLaneBackfill, nil); err != nil {
			return err
		}
		if err := runtime.transitionPhase(ctx, jobID, store.JobPhaseReconcile, store.BootstrapLaneReconcile); err != nil {
			return err
		}
		job.Phase = store.JobPhaseReconcile
	}
	if job.Phase != store.JobPhaseReconcile {
		return ErrInvalidRuntime
	}
	if err := runtime.freezeFinalReconcile(ctx, jobID); err != nil {
		return err
	}
	_, facts, err = runtime.repository.BootstrapRun(ctx, jobID)
	if err != nil {
		return err
	}
	pass := facts.ReconcilePass
	if pass < 1 {
		return ErrInvalidRuntime
	}
	if err := runtime.processLane(ctx, jobID, store.BootstrapLaneReconcile, &pass); err != nil {
		return err
	}
	_, facts, err = runtime.repository.BootstrapRun(ctx, jobID)
	if err != nil {
		return err
	}
	if facts.ReconcileIssueCount > 0 {
		return ErrDiscoveryIncomplete
	}
	if err := runtime.requireReconcilePassClosed(ctx, jobID, pass); err != nil {
		return err
	}
	return runtime.succeed(ctx, jobID)
}

func (runtime *Runtime) promoteQueuedAttempt(
	ctx context.Context,
	job store.JobRun,
	facts store.BootstrapJobFacts,
) error {
	atMS := runtime.nowAfter(job.UpdatedAtMS)
	facts.UpdatedAtMS = atMS
	return runtime.repository.AdvanceBootstrapRun(ctx, store.BootstrapAdvance{
		Job: store.JobTransition{
			JobID: job.JobID, ExpectedState: store.JobQueued, State: store.JobRunning,
			Phase: job.Phase, ProgressCurrent: job.ProgressCurrent, ProgressTotal: job.ProgressTotal,
			ResumeCursor: job.ResumeCursor, AtMS: atMS,
		},
		Facts: facts,
	})
}

func (runtime *Runtime) processLane(
	ctx context.Context,
	jobID string,
	lane store.BootstrapLane,
	pass *int64,
) error {
	items, err := runtime.repository.ListBootstrapPlanItems(ctx, store.BootstrapPlanItemFilter{
		JobID: jobID, Lane: &lane,
	})
	if err != nil {
		return err
	}
	for _, item := range items {
		if pass != nil && item.Pass != *pass {
			continue
		}
		if err := ctx.Err(); err != nil {
			return err
		}
		switch item.State {
		case store.BootstrapItemSucceeded, store.BootstrapItemDrifted:
			continue
		case store.BootstrapItemFailed:
			return ErrSourceUnavailable
		case store.BootstrapItemQueued:
			if err := runtime.markItemRunning(ctx, item); err != nil {
				return err
			}
			item.State = store.BootstrapItemRunning
		case store.BootstrapItemRunning:
		default:
			return ErrInvalidRuntime
		}
		if err := runtime.applyItem(ctx, item); err != nil {
			return err
		}
	}
	return nil
}

func (runtime *Runtime) requireReconcilePassClosed(
	ctx context.Context,
	jobID string,
	pass int64,
) error {
	lane := store.BootstrapLaneReconcile
	items, err := runtime.repository.ListBootstrapPlanItems(ctx, store.BootstrapPlanItemFilter{
		JobID: jobID, Lane: &lane,
	})
	if err != nil {
		return err
	}
	for _, item := range items {
		if item.Pass == pass && item.State != store.BootstrapItemSucceeded {
			return ErrSourceUnavailable
		}
	}
	return nil
}

func (runtime *Runtime) applyItem(ctx context.Context, item store.BootstrapPlanItem) error {
	switch item.ActionKind {
	case store.BootstrapActionUnreadable:
		// Initial discovery is only a fixed scheduling snapshot. Deferring an
		// unreadable pass-0 source as drift lets the mandatory final discovery
		// prove whether it recovered; reconcile-pass unreadable items still
		// block full-ready and require Resume to create a fresh pass.
		if item.Pass == 0 {
			return runtime.finishItem(ctx, item, store.BootstrapItemDrifted, nil, 0, false)
		}
		return ErrSourceUnavailable
	case store.BootstrapActionDeleted:
		if item.Previous == nil {
			return ErrInvalidRuntime
		}
		atMS := runtime.nowAfter(item.UpdatedAtMS)
		if err := runtime.repository.MarkBootstrapSourceUnavailable(ctx, *item.Previous, atMS); err != nil {
			return err
		}
		return runtime.finishItem(ctx, item, store.BootstrapItemSucceeded, nil, 0, false)
	}
	if item.Current == nil {
		return ErrInvalidRuntime
	}
	var action logs.ReconcileAction
	if cursor, usable, found, err := runtime.authoritativeItemCursor(ctx, item); err != nil {
		return err
	} else if found {
		if cursor.Checkpoint.CommittedOffset == item.Current.SizeBytes {
			generation := cursor.Generation
			return runtime.finishItem(
				ctx, item, store.BootstrapItemSucceeded, &generation,
				cursor.Checkpoint.CommittedOffset, usable,
			)
		}
		if cursor.Checkpoint.CommittedOffset < 0 ||
			cursor.Checkpoint.CommittedOffset > item.Current.SizeBytes {
			return ErrInvalidRuntime
		}
		current := snapshotFromFingerprint(*item.Current)
		previous := current
		action = logs.ReconcileAction{
			Kind: logs.ChangeUnchanged, Previous: &previous, Current: &current,
		}
	} else {
		action = reconcileActionFromItem(item)
	}
	ingesterService, err := indexer.New(runtime.repository)
	if err != nil {
		return err
	}
	stream, err := ingesterService.Open(ctx, indexer.OpenRequest{
		Action: action, AtMS: runtime.nowAfter(item.UpdatedAtMS),
	})
	if err != nil {
		return err
	}
	cursor, err := stream.Cursor()
	if err != nil {
		return err
	}
	return runtime.readItem(ctx, item, stream, cursor)
}

// authoritativeItemCursor closes the crash window where source facts and its
// checkpoint committed but the lagging bootstrap item did not. Exact
// fingerprint equality is required; a newer/different source still re-enters
// reconcile ingestion instead of being guessed complete.
func (runtime *Runtime) authoritativeItemCursor(
	ctx context.Context,
	item store.BootstrapPlanItem,
) (store.GenerationCursor, bool, bool, error) {
	file, err := runtime.repository.SourceFile(ctx, item.Current.SourceFileID)
	if errors.Is(err, store.ErrNotFound) {
		return store.GenerationCursor{}, false, false, nil
	}
	if err != nil {
		return store.GenerationCursor{}, false, false, err
	}
	cursor, err := runtime.repository.GenerationCursor(
		ctx, file.SourceFileID, file.ActiveGeneration,
	)
	if errors.Is(err, store.ErrNotFound) {
		return store.GenerationCursor{}, false, false, nil
	}
	if err != nil {
		return store.GenerationCursor{}, false, false, err
	}
	if cursor.State != store.GenerationActive || cursor.Fingerprint != *item.Current {
		return store.GenerationCursor{}, false, false, nil
	}
	return cursor, file.SessionID != nil, true, nil
}

func (runtime *Runtime) readItem(
	ctx context.Context,
	item store.BootstrapPlanItem,
	stream *indexer.Stream,
	cursor indexer.StreamCursor,
) error {
	_, facts, err := runtime.repository.BootstrapRun(ctx, item.JobID)
	if err != nil {
		return err
	}
	reader, err := logs.NewConfirmedSnapshotReader(
		facts.HomePath, facts.HomeDeviceID, facts.HomeInode, runtime.readChunkBytes,
	)
	if err != nil {
		return err
	}
	snapshot := snapshotFromFingerprint(*item.Current)
	_, readErr := reader.Read(ctx, snapshot, cursor.CommittedOffset, func(chunk []byte, eof bool) error {
		result, feedErr := stream.Feed(ctx, chunk, eof, runtime.nowAfter(item.UpdatedAtMS))
		if feedErr != nil {
			return feedErr
		}
		if runtime.hooks.afterChunk != nil {
			runtime.hooks.afterChunk(item, result.ReadOffset)
		}
		return nil
	})
	latest, cursorErr := stream.Cursor()
	if cursorErr != nil {
		return cursorErr
	}
	generation := latest.Generation
	usable := false
	if file, fileErr := runtime.repository.SourceFile(ctx, latest.SourceFileID); fileErr == nil {
		usable = file.SessionID != nil
	} else if !errors.Is(fileErr, store.ErrNotFound) {
		return fileErr
	}
	if errors.Is(readErr, logs.ErrChangedDuringScan) {
		return runtime.finishItem(
			ctx, item, store.BootstrapItemDrifted, &generation, latest.CommittedOffset, usable,
		)
	}
	if readErr != nil {
		return readErr
	}
	return runtime.finishItem(
		ctx, item, store.BootstrapItemSucceeded, &generation, latest.CommittedOffset, usable,
	)
}

func (runtime *Runtime) markItemRunning(ctx context.Context, item store.BootstrapPlanItem) error {
	job, facts, err := runtime.repository.BootstrapRun(ctx, item.JobID)
	if err != nil {
		return err
	}
	atMS := runtime.nowAfter(maxInt64(job.UpdatedAtMS, item.UpdatedAtMS))
	item.State = store.BootstrapItemRunning
	item.UpdatedAtMS = atMS
	facts.UpdatedAtMS = atMS
	return runtime.repository.AdvanceBootstrapRun(ctx, store.BootstrapAdvance{
		Job: store.JobTransition{
			JobID: job.JobID, ExpectedState: job.State, State: store.JobRunning, Phase: job.Phase,
			ProgressCurrent: job.ProgressCurrent, ProgressTotal: job.ProgressTotal,
			ResumeCursor: job.ResumeCursor, AtMS: atMS,
		},
		Facts: facts, Item: &item,
	})
}

func (runtime *Runtime) finishItem(
	ctx context.Context,
	item store.BootstrapPlanItem,
	state store.BootstrapItemState,
	generation *int64,
	progress int64,
	usable bool,
) error {
	job, facts, err := runtime.repository.BootstrapRun(ctx, item.JobID)
	if err != nil {
		return err
	}
	stored, err := runtime.planItem(ctx, item.JobID, item.Ordinal)
	if err != nil {
		return err
	}
	if progress < stored.ProgressCurrent || progress > stored.ProgressTotal {
		return ErrInvalidRuntime
	}
	delta := progress - stored.ProgressCurrent
	current := pointerValue(job.ProgressCurrent) + delta
	total := pointerValue(job.ProgressTotal)
	atMS := runtime.nowAfter(maxInt64(job.UpdatedAtMS, stored.UpdatedAtMS))
	stored.State = state
	stored.SourceGeneration = cloneInt64(generation)
	stored.ProgressCurrent = progress
	stored.UpdatedAtMS = atMS
	facts.PhaseProgressCurrent += delta
	if usable && stored.Lane == store.BootstrapLaneFast && facts.FirstScreenReadyAtMS == nil {
		facts.FirstScreenReadyAtMS = &atMS
	}
	facts.UpdatedAtMS = atMS
	var resumeCursor *store.JobCursor
	if generation != nil {
		resumeCursor = &store.JobCursor{Generation: *generation, Offset: progress}
	} else {
		resumeCursor = job.ResumeCursor
	}
	return runtime.repository.AdvanceBootstrapRun(ctx, store.BootstrapAdvance{
		Job: store.JobTransition{
			JobID: job.JobID, ExpectedState: job.State, State: store.JobRunning, Phase: job.Phase,
			ProgressCurrent: &current, ProgressTotal: &total, ResumeCursor: resumeCursor, AtMS: atMS,
		},
		Facts: facts, Item: &stored,
	})
}

func (runtime *Runtime) transitionPhase(
	ctx context.Context,
	jobID string,
	phase store.JobPhase,
	lane store.BootstrapLane,
) error {
	job, facts, err := runtime.repository.BootstrapRun(ctx, jobID)
	if err != nil {
		return err
	}
	items, err := runtime.repository.ListBootstrapPlanItems(ctx, store.BootstrapPlanItemFilter{
		JobID: jobID, Lane: &lane,
	})
	if err != nil {
		return err
	}
	var phaseCurrent, phaseTotal int64
	for _, item := range items {
		phaseCurrent += item.ProgressCurrent
		phaseTotal += item.ProgressTotal
	}
	if job.State == store.JobRunning && job.Phase == phase &&
		facts.PhaseProgressCurrent == phaseCurrent && facts.PhaseProgressTotal == phaseTotal &&
		facts.PauseReason == nil {
		return nil
	}
	atMS := runtime.nowAfter(job.UpdatedAtMS)
	facts.PhaseProgressCurrent = phaseCurrent
	facts.PhaseProgressTotal = phaseTotal
	facts.PauseReason = nil
	facts.UpdatedAtMS = atMS
	return runtime.repository.AdvanceBootstrapRun(ctx, store.BootstrapAdvance{
		Job: store.JobTransition{
			JobID: jobID, ExpectedState: job.State, State: store.JobRunning, Phase: phase,
			ProgressCurrent: job.ProgressCurrent, ProgressTotal: job.ProgressTotal,
			ResumeCursor: job.ResumeCursor, AtMS: atMS,
		},
		Facts: facts,
	})
}

func (runtime *Runtime) ensureFirstScreenReady(ctx context.Context, jobID string) error {
	job, facts, err := runtime.repository.BootstrapRun(ctx, jobID)
	if err != nil || facts.FirstScreenReadyAtMS != nil {
		return err
	}
	atMS := runtime.nowAfter(job.UpdatedAtMS)
	facts.FirstScreenReadyAtMS = &atMS
	facts.UpdatedAtMS = atMS
	return runtime.repository.AdvanceBootstrapRun(ctx, store.BootstrapAdvance{
		Job: store.JobTransition{
			JobID: jobID, ExpectedState: job.State, State: store.JobRunning, Phase: job.Phase,
			ProgressCurrent: job.ProgressCurrent, ProgressTotal: job.ProgressTotal,
			ResumeCursor: job.ResumeCursor, AtMS: atMS,
		},
		Facts: facts,
	})
}

func (runtime *Runtime) freezeFinalReconcile(ctx context.Context, jobID string) error {
	existing, err := runtime.repository.ListBootstrapPlanItems(ctx, store.BootstrapPlanItemFilter{JobID: jobID})
	if err != nil {
		return err
	}
	_, persistedFacts, err := runtime.repository.BootstrapRun(ctx, jobID)
	if err != nil {
		return err
	}
	if persistedFacts.ReconcilePlanAtMS != nil {
		return nil
	}
	if runtime.hooks.beforeReconcile != nil {
		runtime.hooks.beforeReconcile()
	}
	facts := persistedFacts
	previous, err := runtime.repository.CodexSnapshots(ctx)
	if err != nil {
		return err
	}
	previousSnapshots := snapshotsFromFingerprints(previous)
	discoverer, err := logs.NewConfirmedDiscoverer(
		facts.HomePath, facts.HomeDeviceID, facts.HomeInode,
	)
	if err != nil {
		return err
	}
	discovery, err := discoverer.DiscoverAgainst(ctx, previousSnapshots)
	if err != nil {
		return err
	}
	reconcile, err := logs.PlanReconcile(facts.HomePath, previousSnapshots, discovery)
	if err != nil {
		return err
	}
	pass := facts.ReconcilePass + 1
	atMS := runtime.nowAfter(facts.UpdatedAtMS)
	items, err := FreezeReconcilePlan(ReconcilePlanRequest{
		JobID: jobID, Reconcile: reconcile, StartOrdinal: int64(len(existing)), Pass: pass, AtMS: atMS,
	})
	if err != nil {
		return err
	}
	if err := runtime.repository.AppendBootstrapReconcilePlan(
		ctx, jobID, items, pass, int64(len(items)), int64(len(discovery.Issues)), atMS,
	); err != nil {
		return err
	}
	if runtime.hooks.afterReconcilePlan != nil {
		runtime.hooks.afterReconcilePlan()
	}
	return nil
}

func (runtime *Runtime) succeed(ctx context.Context, jobID string) error {
	job, facts, err := runtime.repository.BootstrapRun(ctx, jobID)
	if err != nil {
		return err
	}
	atMS := runtime.nowAfter(job.UpdatedAtMS)
	if facts.FirstScreenReadyAtMS == nil {
		facts.FirstScreenReadyAtMS = &atMS
		atMS = runtime.nowAfter(atMS)
	}
	facts.FullHistoryReadyAtMS = &atMS
	facts.ReconciledAtMS = &atMS
	facts.ETAState = store.BootstrapETAComplete
	facts.ETARemainingMS = nil
	facts.PauseReason = nil
	facts.UpdatedAtMS = atMS
	return runtime.repository.AdvanceBootstrapRun(ctx, store.BootstrapAdvance{
		Job: store.JobTransition{
			JobID: jobID, ExpectedState: job.State, State: store.JobSucceeded,
			Phase: store.JobPhaseReconcile, ProgressCurrent: job.ProgressCurrent,
			ProgressTotal: job.ProgressTotal, ResumeCursor: job.ResumeCursor, AtMS: atMS,
		},
		Facts: facts,
	})
}

func (runtime *Runtime) terminate(
	ctx context.Context,
	jobID string,
	state store.JobState,
	cause error,
	pause *store.BootstrapPauseReason,
) error {
	job, facts, err := runtime.repository.BootstrapRun(ctx, jobID)
	if err != nil {
		return err
	}
	if job.State != store.JobQueued && job.State != store.JobRunning {
		return nil
	}
	atMS := runtime.nowAfter(job.UpdatedAtMS)
	facts.PauseReason = clonePauseReason(pause)
	facts.UpdatedAtMS = atMS
	var errorClass *store.RuntimeErrorClass
	if state == store.JobFailed {
		value := classifyBootstrapError(cause)
		errorClass = &value
	}
	return runtime.repository.AdvanceBootstrapRun(ctx, store.BootstrapAdvance{
		Job: store.JobTransition{
			JobID: jobID, ExpectedState: job.State, State: state, Phase: job.Phase,
			ProgressCurrent: job.ProgressCurrent, ProgressTotal: job.ProgressTotal,
			ResumeCursor: job.ResumeCursor, ErrorClass: errorClass, AtMS: atMS,
		},
		Facts: facts,
	})
}

func (runtime *Runtime) interrupt(
	ctx context.Context,
	job store.JobRun,
	facts store.BootstrapJobFacts,
	pause *store.BootstrapPauseReason,
) error {
	atMS := runtime.nowAfter(job.UpdatedAtMS)
	facts.PauseReason = clonePauseReason(pause)
	facts.UpdatedAtMS = atMS
	return runtime.repository.AdvanceBootstrapRun(ctx, store.BootstrapAdvance{
		Job: store.JobTransition{
			JobID: job.JobID, ExpectedState: job.State, State: store.JobInterrupted, Phase: job.Phase,
			ProgressCurrent: job.ProgressCurrent, ProgressTotal: job.ProgressTotal,
			ResumeCursor: job.ResumeCursor, AtMS: atMS,
		},
		Facts: facts,
	})
}

func (runtime *Runtime) planItem(
	ctx context.Context,
	jobID string,
	ordinal int64,
) (store.BootstrapPlanItem, error) {
	items, err := runtime.repository.ListBootstrapPlanItems(ctx, store.BootstrapPlanItemFilter{JobID: jobID})
	if err != nil {
		return store.BootstrapPlanItem{}, err
	}
	for _, item := range items {
		if item.Ordinal == ordinal {
			return item, nil
		}
	}
	return store.BootstrapPlanItem{}, store.ErrNotFound
}

func reconcileActionFromItem(item store.BootstrapPlanItem) logs.ReconcileAction {
	action := logs.ReconcileAction{Kind: logs.ChangeKind(item.ActionKind)}
	if item.Previous != nil {
		value := snapshotFromFingerprint(*item.Previous)
		action.Previous = &value
	}
	if item.Current != nil {
		value := snapshotFromFingerprint(*item.Current)
		action.Current = &value
	}
	action.PathChanged = action.Previous != nil && action.Current != nil &&
		action.Previous.Path != action.Current.Path
	return action
}

func sourcePauseReason(err error) *store.BootstrapPauseReason {
	if errors.Is(err, ErrSourceUnavailable) || errors.Is(err, ErrDiscoveryIncomplete) ||
		errors.Is(err, logs.ErrUnsafeSource) || errors.Is(err, logs.ErrUnsupportedFile) ||
		errors.Is(err, logs.ErrChangedDuringScan) || errors.Is(err, logs.ErrHomeChanged) ||
		errors.Is(err, fs.ErrPermission) {
		value := store.BootstrapPauseSourceUnavailable
		return &value
	}
	return nil
}

func classifyBootstrapError(err error) store.RuntimeErrorClass {
	switch {
	case errors.Is(err, fs.ErrPermission):
		return store.RuntimeErrorPermission
	case sourcePauseReason(err) != nil:
		return store.RuntimeErrorUnavailable
	default:
		return store.ClassifyRuntimeError(err)
	}
}

func reportFromRun(job store.JobRun, facts store.BootstrapJobFacts) RunReport {
	return RunReport{
		JobID: job.JobID, State: job.State, Phase: job.Phase,
		FirstScreenReady: facts.FirstScreenReadyAtMS != nil,
		FullHistoryReady: facts.FullHistoryReadyAtMS != nil,
		ProgressCurrent:  pointerValue(job.ProgressCurrent), ProgressTotal: pointerValue(job.ProgressTotal),
		ReconcileChanges: facts.ReconcileChangeCount, ReconcileIssues: facts.ReconcileIssueCount,
	}
}

func pointerValue(value *int64) int64 {
	if value == nil {
		return 0
	}
	return *value
}

func clonePauseReason(value *store.BootstrapPauseReason) *store.BootstrapPauseReason {
	if value == nil {
		return nil
	}
	copy := *value
	return &copy
}

func maxInt64(left, right int64) int64 {
	if left > right {
		return left
	}
	return right
}
