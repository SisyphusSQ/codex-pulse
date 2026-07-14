package store

import (
	"context"
	"errors"
	"reflect"
	"testing"
)

func TestBootstrapRepositoryCreatesFreezesAndReadsTypedPlan(t *testing.T) {
	t.Parallel()

	repository := openRuntimeRepository(t)
	ctx := context.Background()
	job := JobRun{
		JobID: "bootstrap-job-a", JobType: "codex_home_bootstrap", RequestedBy: "home-switch",
		Priority: 10, State: JobQueued, Phase: JobPhaseDiscover, CreatedAtMS: 10, UpdatedAtMS: 10,
	}
	facts := BootstrapJobFacts{
		JobID: job.JobID, SwitchID: "home-switch:a", HomeGeneration: 2,
		HomePath: "/tmp/codex-home-a", HomeDeviceID: "device-home", HomeInode: 9,
		DataStoreKey: "home-a", Strategy: "independent_database",
		PlanState: BootstrapPlanPending, ETAState: BootstrapETAUnknown, UpdatedAtMS: 10,
	}
	if err := repository.CreateBootstrapJob(ctx, job, facts); err != nil {
		t.Fatalf("CreateBootstrapJob() error = %v", err)
	}
	if err := repository.CreateBootstrapJob(ctx, job, facts); err != nil {
		t.Fatalf("CreateBootstrapJob(replay) error = %v", err)
	}

	first := testSourceFingerprint("source-fast", "/tmp/codex-home-a/sessions/fast.jsonl", 11, 40, 100)
	second := testSourceFingerprint("source-backfill", "/tmp/codex-home-a/sessions/backfill.jsonl", 12, 80, 50)
	items := []BootstrapPlanItem{
		{
			JobID: job.JobID, Ordinal: 0, Pass: 0, Lane: BootstrapLaneFast,
			Tier: BootstrapTierToday, ActionKind: BootstrapActionAdded, Current: &first,
			State: BootstrapItemQueued, ProgressCurrent: 0, ProgressTotal: 40, UpdatedAtMS: 20,
		},
		{
			JobID: job.JobID, Ordinal: 1, Pass: 0, Lane: BootstrapLaneBackfill,
			Tier: BootstrapTierRecent30Days, ActionKind: BootstrapActionAdded, Current: &second,
			State: BootstrapItemQueued, ProgressCurrent: 0, ProgressTotal: 80, UpdatedAtMS: 20,
		},
	}
	if err := repository.FreezeBootstrapPlan(ctx, job.JobID, items, 20); err != nil {
		t.Fatalf("FreezeBootstrapPlan() error = %v", err)
	}
	if err := repository.FreezeBootstrapPlan(ctx, job.JobID, items, 20); err != nil {
		t.Fatalf("FreezeBootstrapPlan(replay) error = %v", err)
	}

	gotJob, gotFacts, err := repository.BootstrapRun(ctx, job.JobID)
	if err != nil {
		t.Fatalf("BootstrapRun() error = %v", err)
	}
	if gotJob.ProgressCurrent == nil || *gotJob.ProgressCurrent != 0 ||
		gotJob.ProgressTotal == nil || *gotJob.ProgressTotal != 120 {
		t.Fatalf("BootstrapRun() job progress = %#v, want 0/120", gotJob)
	}
	if gotFacts.PlanState != BootstrapPlanReady || gotFacts.PlanSHA256.String() == "" ||
		gotFacts.PhaseProgressCurrent != 0 || gotFacts.PhaseProgressTotal != 40 {
		t.Fatalf("BootstrapRun() facts = %#v, want ready fast 0/40", gotFacts)
	}
	gotItems, err := repository.ListBootstrapPlanItems(ctx, BootstrapPlanItemFilter{JobID: job.JobID})
	if err != nil {
		t.Fatalf("ListBootstrapPlanItems() error = %v", err)
	}
	if !reflect.DeepEqual(gotItems, items) {
		t.Fatalf("ListBootstrapPlanItems() = %#v, want %#v", gotItems, items)
	}
}

func TestBootstrapRepositoryRejectsConflictingPlanReplayAndInvalidFacts(t *testing.T) {
	t.Parallel()

	repository := openRuntimeRepository(t)
	ctx := context.Background()
	job := JobRun{
		JobID: "bootstrap-job-invalid", JobType: "codex_home_bootstrap", RequestedBy: "home-switch",
		State: JobQueued, Phase: JobPhaseDiscover, CreatedAtMS: 10, UpdatedAtMS: 10,
	}
	facts := BootstrapJobFacts{
		JobID: job.JobID, SwitchID: "home-switch:invalid", HomeGeneration: 1,
		HomePath: "/tmp/codex-home-invalid", HomeDeviceID: "device-home", HomeInode: 1,
		DataStoreKey: "home-invalid", Strategy: "clear_and_rebuild",
		PlanState: BootstrapPlanPending, ETAState: BootstrapETAUnknown, UpdatedAtMS: 10,
	}
	if err := repository.CreateBootstrapJob(ctx, job, facts); err != nil {
		t.Fatalf("CreateBootstrapJob() error = %v", err)
	}
	snapshot := testSourceFingerprint(
		"source-a", "/tmp/codex-home-invalid/sessions/a.jsonl", 4, 20, 20,
	)
	items := []BootstrapPlanItem{{
		JobID: job.JobID, Lane: BootstrapLaneFast, Tier: BootstrapTierToday,
		ActionKind: BootstrapActionAdded, Current: &snapshot,
		State: BootstrapItemQueued, ProgressTotal: 20, UpdatedAtMS: 20,
	}}
	if err := repository.FreezeBootstrapPlan(ctx, job.JobID, items, 20); err != nil {
		t.Fatalf("FreezeBootstrapPlan() error = %v", err)
	}
	conflict := append([]BootstrapPlanItem(nil), items...)
	conflict[0].Tier = BootstrapTierOlder
	if err := repository.FreezeBootstrapPlan(ctx, job.JobID, conflict, 20); !errors.Is(err, ErrInvalidRecord) {
		t.Fatalf("FreezeBootstrapPlan(conflict) error = %v, want ErrInvalidRecord", err)
	}

	invalidJob := job
	invalidJob.JobID = "bootstrap-job-bad-eta"
	invalidFacts := facts
	invalidFacts.JobID = invalidJob.JobID
	invalidFacts.ETAState = BootstrapETAKnown
	if err := repository.CreateBootstrapJob(ctx, invalidJob, invalidFacts); !errors.Is(err, ErrInvalidRecord) {
		t.Fatalf("CreateBootstrapJob(known ETA without remaining) error = %v, want ErrInvalidRecord", err)
	}
}

func TestBootstrapRepositoryAdvancesFactsAndItemAtomically(t *testing.T) {
	t.Parallel()

	repository := openRuntimeRepository(t)
	job, facts, items := createFrozenBootstrapFixture(t, repository, "bootstrap-advance")
	item := items[0]
	item.State = BootstrapItemRunning
	item.SourceGeneration = pointerTo(int64(0))
	item.ProgressCurrent = 10
	item.UpdatedAtMS = 30
	facts.PhaseProgressCurrent = 10
	facts.ETAState = BootstrapETAKnown
	facts.ETARemainingMS = pointerTo(int64(90))
	facts.UpdatedAtMS = 30
	progress, total := int64(10), int64(20)
	advance := BootstrapAdvance{
		Job: JobTransition{
			JobID: job.JobID, ExpectedState: JobQueued, State: JobRunning,
			Phase: JobPhaseFastBootstrap, ProgressCurrent: &progress, ProgressTotal: &total, AtMS: 30,
		},
		Facts: facts,
		Item:  &item,
	}
	if err := repository.AdvanceBootstrapRun(context.Background(), advance); err != nil {
		t.Fatalf("AdvanceBootstrapRun() error = %v", err)
	}
	if err := repository.AdvanceBootstrapRun(context.Background(), advance); err != nil {
		t.Fatalf("AdvanceBootstrapRun(replay) error = %v", err)
	}
	gotJob, gotFacts, err := repository.BootstrapRun(context.Background(), job.JobID)
	if err != nil {
		t.Fatalf("BootstrapRun() error = %v", err)
	}
	gotItems, err := repository.ListBootstrapPlanItems(
		context.Background(), BootstrapPlanItemFilter{JobID: job.JobID},
	)
	if err != nil {
		t.Fatalf("ListBootstrapPlanItems() error = %v", err)
	}
	if gotJob.State != JobRunning || gotJob.Phase != JobPhaseFastBootstrap ||
		!bootstrapJobFactsEqual(gotFacts, facts) || len(gotItems) != 1 ||
		!bootstrapPlanItemEqual(gotItems[0], item) {
		t.Fatalf("advanced run = %#v %#v %#v", gotJob, gotFacts, gotItems)
	}

	regressed := advance
	regressed.Job.ExpectedState = JobRunning
	regressed.Job.ProgressCurrent = pointerTo(int64(9))
	regressed.Job.AtMS = 31
	regressed.Facts.PhaseProgressCurrent = 9
	regressed.Facts.UpdatedAtMS = 31
	regressed.Item.ProgressCurrent = 9
	regressed.Item.UpdatedAtMS = 31
	if err := repository.AdvanceBootstrapRun(context.Background(), regressed); !errors.Is(err, ErrInvalidRecord) {
		t.Fatalf("AdvanceBootstrapRun(regression) error = %v, want ErrInvalidRecord", err)
	}
}

func TestBootstrapRepositoryResumesInterruptedAttemptWithClonedPlan(t *testing.T) {
	t.Parallel()

	repository := openRuntimeRepository(t)
	job, facts, items := createFrozenBootstrapFixture(t, repository, "bootstrap-resume")
	progress, total := int64(0), int64(20)
	runningFacts := facts
	runningFacts.UpdatedAtMS = 30
	if err := repository.AdvanceBootstrapRun(context.Background(), BootstrapAdvance{
		Job: JobTransition{
			JobID: job.JobID, ExpectedState: JobQueued, State: JobRunning,
			Phase: JobPhaseFastBootstrap, ProgressCurrent: &progress, ProgressTotal: &total, AtMS: 30,
		},
		Facts: runningFacts,
	}); err != nil {
		t.Fatalf("AdvanceBootstrapRun(running) error = %v", err)
	}
	pause := BootstrapPauseApplicationDraining
	interruptedFacts := runningFacts
	interruptedFacts.PauseReason = &pause
	interruptedFacts.UpdatedAtMS = 40
	if err := repository.AdvanceBootstrapRun(context.Background(), BootstrapAdvance{
		Job: JobTransition{
			JobID: job.JobID, ExpectedState: JobRunning, State: JobInterrupted,
			Phase: JobPhaseFastBootstrap, ProgressCurrent: &progress, ProgressTotal: &total, AtMS: 40,
		},
		Facts: interruptedFacts,
	}); err != nil {
		t.Fatalf("AdvanceBootstrapRun(interrupted) error = %v", err)
	}
	oldID := job.JobID
	resumed := JobRun{
		JobID: "bootstrap-resume-attempt-2", JobType: job.JobType, RequestedBy: job.RequestedBy,
		Priority: job.Priority, State: JobQueued, Phase: JobPhaseFastBootstrap,
		ResumeOfJobID: &oldID, CreatedAtMS: 50, ProgressCurrent: &progress,
		ProgressTotal: &total, UpdatedAtMS: 50,
	}
	if err := repository.ResumeBootstrapJob(context.Background(), oldID, resumed); err != nil {
		t.Fatalf("ResumeBootstrapJob() error = %v", err)
	}
	if err := repository.ResumeBootstrapJob(context.Background(), oldID, resumed); err != nil {
		t.Fatalf("ResumeBootstrapJob(replay) error = %v", err)
	}
	gotJob, gotFacts, err := repository.BootstrapRun(context.Background(), resumed.JobID)
	if err != nil {
		t.Fatalf("BootstrapRun(resumed) error = %v", err)
	}
	gotItems, err := repository.ListBootstrapPlanItems(
		context.Background(), BootstrapPlanItemFilter{JobID: resumed.JobID},
	)
	if err != nil {
		t.Fatalf("ListBootstrapPlanItems(resumed) error = %v", err)
	}
	if !jobRunsEqual(gotJob, resumed) || gotFacts.JobID != resumed.JobID ||
		gotFacts.PauseReason != nil || gotFacts.UpdatedAtMS != 50 || len(gotItems) != len(items) ||
		gotItems[0].JobID != resumed.JobID || gotItems[0].UpdatedAtMS != 50 {
		t.Fatalf("resumed run = %#v %#v %#v", gotJob, gotFacts, gotItems)
	}
}

func TestBootstrapRepositoryReadsLatestAttemptByIdentityAndGeneration(t *testing.T) {
	t.Parallel()

	repository := openRuntimeRepository(t)
	job, facts, _ := createFrozenBootstrapFixture(t, repository, "bootstrap-latest")
	progress, total := int64(0), int64(20)
	runningFacts := facts
	runningFacts.UpdatedAtMS = 30
	if err := repository.AdvanceBootstrapRun(context.Background(), BootstrapAdvance{
		Job: JobTransition{
			JobID: job.JobID, ExpectedState: JobQueued, State: JobRunning,
			Phase: JobPhaseFastBootstrap, ProgressCurrent: &progress, ProgressTotal: &total, AtMS: 30,
		},
		Facts: runningFacts,
	}); err != nil {
		t.Fatalf("AdvanceBootstrapRun(running) error = %v", err)
	}
	pause := BootstrapPauseApplicationDraining
	interruptedFacts := runningFacts
	interruptedFacts.PauseReason = &pause
	interruptedFacts.UpdatedAtMS = 40
	if err := repository.AdvanceBootstrapRun(context.Background(), BootstrapAdvance{
		Job: JobTransition{
			JobID: job.JobID, ExpectedState: JobRunning, State: JobInterrupted,
			Phase: JobPhaseFastBootstrap, ProgressCurrent: &progress, ProgressTotal: &total, AtMS: 40,
		},
		Facts: interruptedFacts,
	}); err != nil {
		t.Fatalf("AdvanceBootstrapRun(interrupted) error = %v", err)
	}
	oldID := job.JobID
	resumed := JobRun{
		JobID: "bootstrap-latest-2", JobType: job.JobType, RequestedBy: job.RequestedBy,
		Priority: job.Priority, State: JobQueued, Phase: JobPhaseFastBootstrap,
		ResumeOfJobID: &oldID, CreatedAtMS: 50, ProgressCurrent: &progress,
		ProgressTotal: &total, UpdatedAtMS: 50,
	}
	if err := repository.ResumeBootstrapJob(context.Background(), oldID, resumed); err != nil {
		t.Fatalf("ResumeBootstrapJob() error = %v", err)
	}

	gotJob, gotFacts, err := repository.BootstrapRunByIdentity(
		context.Background(), facts.SwitchID, facts.HomeGeneration,
	)
	if err != nil || gotJob.JobID != resumed.JobID || gotFacts.JobID != resumed.JobID {
		t.Fatalf("BootstrapRunByIdentity() = %#v %#v, %v", gotJob, gotFacts, err)
	}
	gotJob, gotFacts, err = repository.LatestBootstrapRunByGeneration(
		context.Background(), facts.HomeGeneration,
	)
	if err != nil || gotJob.JobID != resumed.JobID || gotFacts.JobID != resumed.JobID {
		t.Fatalf("LatestBootstrapRunByGeneration() = %#v %#v, %v", gotJob, gotFacts, err)
	}
	if _, _, err := repository.BootstrapRunByIdentity(
		context.Background(), "missing", facts.HomeGeneration,
	); !errors.Is(err, ErrNotFound) {
		t.Fatalf("BootstrapRunByIdentity(missing) error = %v, want ErrNotFound", err)
	}
}

func TestBootstrapRepositoryAppendsFixedReconcilePassAtomically(t *testing.T) {
	t.Parallel()

	repository := openRuntimeRepository(t)
	job, facts, _ := createFrozenBootstrapFixture(t, repository, "bootstrap-reconcile")
	progress, total := int64(20), int64(20)
	facts.PhaseProgressCurrent = 0
	facts.PhaseProgressTotal = 0
	facts.FirstScreenReadyAtMS = pointerTo(int64(25))
	facts.UpdatedAtMS = 30
	if err := repository.AdvanceBootstrapRun(context.Background(), BootstrapAdvance{
		Job: JobTransition{
			JobID: job.JobID, ExpectedState: JobQueued, State: JobRunning,
			Phase: JobPhaseReconcile, ProgressCurrent: &progress, ProgressTotal: &total, AtMS: 30,
		},
		Facts: facts,
	}); err != nil {
		t.Fatalf("AdvanceBootstrapRun(reconcile) error = %v", err)
	}
	snapshot := testSourceFingerprint(
		"source-reconcile", facts.HomePath+"/sessions/reconcile.jsonl", 12, 7, 200,
	)
	items := []BootstrapPlanItem{{
		JobID: job.JobID, Ordinal: 1, Pass: 1, Lane: BootstrapLaneReconcile,
		Tier: BootstrapTierReconcile, ActionKind: BootstrapActionAdded, Current: &snapshot,
		State: BootstrapItemQueued, ProgressTotal: 7, UpdatedAtMS: 40,
	}}
	if err := repository.AppendBootstrapReconcilePlan(
		context.Background(), job.JobID, items, 1, 1, 2, 40,
	); err != nil {
		t.Fatalf("AppendBootstrapReconcilePlan() error = %v", err)
	}
	if err := repository.AppendBootstrapReconcilePlan(
		context.Background(), job.JobID, items, 1, 1, 2, 40,
	); err != nil {
		t.Fatalf("AppendBootstrapReconcilePlan(replay) error = %v", err)
	}
	gotJob, gotFacts, err := repository.BootstrapRun(context.Background(), job.JobID)
	if err != nil {
		t.Fatalf("BootstrapRun() error = %v", err)
	}
	gotItems, err := repository.ListBootstrapPlanItems(
		context.Background(), BootstrapPlanItemFilter{JobID: job.JobID},
	)
	if err != nil || len(gotItems) != 2 || !bootstrapPlanItemEqual(gotItems[1], items[0]) {
		t.Fatalf("ListBootstrapPlanItems() = %#v, %v", gotItems, err)
	}
	if gotJob.ProgressTotal == nil || *gotJob.ProgressTotal != 27 ||
		gotFacts.PhaseProgressTotal != 7 || gotFacts.ReconcilePass != 1 || gotFacts.ReconcileChangeCount != 1 ||
		gotFacts.ReconcileIssueCount != 2 || gotFacts.ReconcilePlanAtMS == nil ||
		*gotFacts.ReconcilePlanAtMS != 40 || gotFacts.UpdatedAtMS != 40 {
		t.Fatalf("reconcile run = %#v %#v", gotJob, gotFacts)
	}
	conflict := append([]BootstrapPlanItem(nil), items...)
	conflict[0].ProgressTotal = 6
	conflict[0].Current = pointerTo(*items[0].Current)
	conflict[0].Current.SizeBytes = 6
	if err := repository.AppendBootstrapReconcilePlan(
		context.Background(), job.JobID, conflict, 1, 1, 2, 40,
	); !errors.Is(err, ErrInvalidRecord) {
		t.Fatalf("AppendBootstrapReconcilePlan(conflict) error = %v, want ErrInvalidRecord", err)
	}
}

func TestBootstrapRepositoryMarksDeletedSourceUnavailableWithFingerprintCAS(t *testing.T) {
	t.Parallel()

	repository := openRuntimeRepository(t)
	ctx := context.Background()
	previous := testSourceFingerprint(
		"source-delete", "/tmp/delete-home/sessions/a.jsonl", 13, 0, 100,
	)
	cursor, err := repository.PrepareGeneration(ctx, PrepareGenerationRequest{
		Mode: GenerationModeRebuild, Current: previous, ParserVersion: "codex-rollout-v1", AtMS: 10,
	})
	if err != nil {
		t.Fatalf("PrepareGeneration() error = %v", err)
	}
	if _, err := repository.CommitIngestBatch(ctx, IngestBatch{
		SourceFileID: previous.SourceFileID, Generation: cursor.Generation,
		Fingerprint: previous, Checkpoint: ParserCheckpoint{
			Version: ParserCheckpointVersion, ParserVersion: "codex-rollout-v1",
			CommittedOffset: previous.SizeBytes,
		},
		EOF: true, AtMS: 11,
	}); err != nil {
		t.Fatalf("CommitIngestBatch() error = %v", err)
	}
	if err := repository.MarkBootstrapSourceUnavailable(ctx, previous, 20); err != nil {
		t.Fatalf("MarkBootstrapSourceUnavailable() error = %v", err)
	}
	if err := repository.MarkBootstrapSourceUnavailable(ctx, previous, 20); err != nil {
		t.Fatalf("MarkBootstrapSourceUnavailable(replay) error = %v", err)
	}
	file, err := repository.SourceFile(ctx, previous.SourceFileID)
	if err != nil || file.State != SourceFileUnavailable || file.UpdatedAtMS != 20 {
		t.Fatalf("SourceFile() = %#v, %v, want unavailable", file, err)
	}
	stale := previous
	stale.FingerprintSHA256 = SHA256DigestOf([]byte("stale")).String()
	if err := repository.MarkBootstrapSourceUnavailable(ctx, stale, 21); !errors.Is(err, ErrInvalidRecord) {
		t.Fatalf("MarkBootstrapSourceUnavailable(stale) error = %v, want ErrInvalidRecord", err)
	}
}

func createFrozenBootstrapFixture(
	t *testing.T,
	repository *Repository,
	jobID string,
) (JobRun, BootstrapJobFacts, []BootstrapPlanItem) {
	t.Helper()
	job := JobRun{
		JobID: jobID, JobType: "codex_home_bootstrap", RequestedBy: "home-switch",
		Priority: 10, State: JobQueued, Phase: JobPhaseDiscover, CreatedAtMS: 10, UpdatedAtMS: 10,
	}
	facts := BootstrapJobFacts{
		JobID: job.JobID, SwitchID: "home-switch:" + jobID, HomeGeneration: 2,
		HomePath: "/tmp/" + jobID, HomeDeviceID: "device-home", HomeInode: 9,
		DataStoreKey: "home-key", Strategy: "independent_database",
		PlanState: BootstrapPlanPending, ETAState: BootstrapETAUnknown, UpdatedAtMS: 10,
	}
	if err := repository.CreateBootstrapJob(context.Background(), job, facts); err != nil {
		t.Fatalf("CreateBootstrapJob() error = %v", err)
	}
	snapshot := testSourceFingerprint("source-"+jobID, facts.HomePath+"/sessions/a.jsonl", 11, 20, 100)
	items := []BootstrapPlanItem{{
		JobID: job.JobID, Lane: BootstrapLaneFast, Tier: BootstrapTierToday,
		ActionKind: BootstrapActionAdded, Current: &snapshot, State: BootstrapItemQueued,
		ProgressTotal: 20, UpdatedAtMS: 20,
	}}
	if err := repository.FreezeBootstrapPlan(context.Background(), job.JobID, items, 20); err != nil {
		t.Fatalf("FreezeBootstrapPlan() error = %v", err)
	}
	job.ProgressCurrent = pointerTo(int64(0))
	job.ProgressTotal = pointerTo(int64(20))
	job.UpdatedAtMS = 20
	facts.PlanState = BootstrapPlanReady
	_, facts, err := repository.BootstrapRun(context.Background(), job.JobID)
	if err != nil {
		t.Fatalf("BootstrapRun() error = %v", err)
	}
	return job, facts, items
}
