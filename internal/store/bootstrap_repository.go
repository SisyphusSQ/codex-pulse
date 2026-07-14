package store

import (
	"context"
	"crypto/sha256"
	"errors"
	"hash"
	"math"
	"path/filepath"

	"gorm.io/gorm"

	storesqlite "github.com/SisyphusSQ/codex-pulse/internal/store/sqlite"
)

func (repository *Repository) CreateBootstrapJob(
	ctx context.Context,
	job JobRun,
	facts BootstrapJobFacts,
) error {
	if repository == nil || repository.database == nil {
		return ErrInvalidRepository
	}
	if err := validateNewJobRun(job); err != nil {
		return err
	}
	if job.Phase != JobPhaseDiscover || job.ResumeOfJobID != nil ||
		job.ProgressCurrent != nil || job.ProgressTotal != nil || job.ResumeCursor != nil {
		return invalidRecord("bootstrap job must start as an empty discover job")
	}
	if err := validateBootstrapJobFacts(facts); err != nil {
		return err
	}
	if facts.JobID != job.JobID || facts.PlanState != BootstrapPlanPending {
		return invalidRecord("bootstrap job and facts do not match pending creation")
	}
	return repository.database.Write(ctx, func(ctx context.Context, transaction storesqlite.WriteTx) error {
		existingJob, jobFound, err := jobRunByID(ctx, transaction, job.JobID)
		if err != nil {
			return err
		}
		var existingFacts bootstrapJobModel
		factsErr := transaction.WithContext(ctx).Take(&existingFacts, "job_id = ?", job.JobID).Error
		factsFound := factsErr == nil
		if factsErr != nil && !errors.Is(factsErr, gorm.ErrRecordNotFound) {
			return factsErr
		}
		if jobFound || factsFound {
			if !jobFound || !factsFound || !jobRunsEqual(existingJob, job) {
				return invalidRecord("bootstrap job conflicts with stable identity")
			}
			stored, err := bootstrapJobFromModel(existingFacts)
			if err != nil || !bootstrapJobFactsEqual(stored, facts) {
				return invalidRecord("bootstrap facts conflict with stable identity")
			}
			return nil
		}
		if err := createJobRun(ctx, transaction, job); err != nil {
			return err
		}
		model := bootstrapJobModelFromDomain(facts)
		return transaction.WithContext(ctx).Create(&model).Error
	})
}

func (repository *Repository) FreezeBootstrapPlan(
	ctx context.Context,
	jobID string,
	items []BootstrapPlanItem,
	atMS int64,
) error {
	if repository == nil || repository.database == nil {
		return ErrInvalidRepository
	}
	if jobID == "" || atMS < 0 {
		return invalidRecord("bootstrap plan identity or time is invalid")
	}
	validated, total, fastTotal, digest, err := validateBootstrapPlan(jobID, items, atMS)
	if err != nil {
		return err
	}
	return repository.database.Write(ctx, func(ctx context.Context, transaction storesqlite.WriteTx) error {
		job, found, err := jobRunByID(ctx, transaction, jobID)
		if err != nil {
			return err
		}
		if !found || (job.State != JobQueued && job.State != JobRunning) || job.Phase != JobPhaseDiscover {
			return invalidRecord("bootstrap plan requires an active discover job")
		}
		facts, found, err := bootstrapJobByID(ctx, transaction, jobID)
		if err != nil {
			return err
		}
		if !found {
			return invalidRecord("bootstrap facts do not exist")
		}
		if facts.PlanState == BootstrapPlanReady {
			if facts.PlanSHA256 != digest {
				return invalidRecord("bootstrap plan replay conflicts with frozen digest")
			}
			stored, err := bootstrapPlanItems(ctx, transaction, BootstrapPlanItemFilter{JobID: jobID})
			if err != nil {
				return err
			}
			if !bootstrapPlanItemsEqual(stored, validated) {
				return invalidRecord("bootstrap plan replay conflicts with frozen items")
			}
			return nil
		}
		if atMS <= facts.UpdatedAtMS || atMS < job.UpdatedAtMS {
			return invalidRecord("bootstrap plan time does not advance")
		}
		models := make([]bootstrapPlanItemModel, len(validated))
		for index, item := range validated {
			models[index] = bootstrapPlanItemModelFromDomain(item)
		}
		if len(models) > 0 {
			if err := transaction.WithContext(ctx).Create(&models).Error; err != nil {
				return err
			}
		}
		zero := int64(0)
		if err := transaction.WithContext(ctx).Model(&jobRunModel{}).Where("job_id = ?", jobID).
			Updates(map[string]any{
				"progress_current": &zero, "progress_total": &total, "updated_at_ms": atMS,
			}).Error; err != nil {
			return err
		}
		facts.PlanState = BootstrapPlanReady
		facts.PlanSHA256 = digest
		facts.PhaseProgressCurrent = 0
		facts.PhaseProgressTotal = fastTotal
		facts.UpdatedAtMS = atMS
		model := bootstrapJobModelFromDomain(facts)
		return transaction.WithContext(ctx).Model(&bootstrapJobModel{}).Where("job_id = ?", jobID).
			Updates(bootstrapJobUpdates(model)).Error
	})
}

func (repository *Repository) BootstrapRun(
	ctx context.Context,
	jobID string,
) (JobRun, BootstrapJobFacts, error) {
	if repository == nil || repository.database == nil {
		return JobRun{}, BootstrapJobFacts{}, ErrInvalidRepository
	}
	if jobID == "" {
		return JobRun{}, BootstrapJobFacts{}, invalidRecord("bootstrap job ID must not be empty")
	}
	var job JobRun
	var facts BootstrapJobFacts
	err := repository.database.View(ctx, func(ctx context.Context, connection storesqlite.ReadConn) error {
		var found bool
		var err error
		job, found, err = jobRunByID(ctx, connection, jobID)
		if err != nil {
			return err
		}
		if !found {
			return ErrNotFound
		}
		facts, found, err = bootstrapJobByID(ctx, connection, jobID)
		if err != nil {
			return err
		}
		if !found {
			return ErrNotFound
		}
		return nil
	})
	return job, facts, err
}

// BootstrapRunByIdentity returns the newest attempt for one durable Home switch identity.
func (repository *Repository) BootstrapRunByIdentity(
	ctx context.Context,
	switchID string,
	homeGeneration int64,
) (JobRun, BootstrapJobFacts, error) {
	if switchID == "" || homeGeneration < 0 {
		return JobRun{}, BootstrapJobFacts{}, invalidRecord("bootstrap identity is invalid")
	}
	return repository.bootstrapRunByFactsQuery(ctx, func(query *gorm.DB) *gorm.DB {
		return query.Where("switch_id = ? AND home_generation = ?", switchID, homeGeneration)
	})
}

// LatestBootstrapRunByGeneration returns the newest attempt admitted for a Home generation.
func (repository *Repository) LatestBootstrapRunByGeneration(
	ctx context.Context,
	homeGeneration int64,
) (JobRun, BootstrapJobFacts, error) {
	if homeGeneration < 0 {
		return JobRun{}, BootstrapJobFacts{}, invalidRecord("bootstrap generation is invalid")
	}
	return repository.bootstrapRunByFactsQuery(ctx, func(query *gorm.DB) *gorm.DB {
		return query.Where("home_generation = ?", homeGeneration)
	})
}

func (repository *Repository) bootstrapRunByFactsQuery(
	ctx context.Context,
	filter func(*gorm.DB) *gorm.DB,
) (JobRun, BootstrapJobFacts, error) {
	if repository == nil || repository.database == nil {
		return JobRun{}, BootstrapJobFacts{}, ErrInvalidRepository
	}
	var job JobRun
	var facts BootstrapJobFacts
	err := repository.database.View(ctx, func(ctx context.Context, connection storesqlite.ReadConn) error {
		var model bootstrapJobModel
		query := filter(connection.WithContext(ctx).Model(&bootstrapJobModel{}))
		if err := query.Order("updated_at_ms DESC").Order("job_id DESC").Take(&model).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return ErrNotFound
			}
			return err
		}
		var err error
		facts, err = bootstrapJobFromModel(model)
		if err != nil {
			return err
		}
		var found bool
		job, found, err = jobRunByID(ctx, connection, facts.JobID)
		if err != nil {
			return err
		}
		if !found {
			return invalidRecord("bootstrap job facts have no public job")
		}
		return nil
	})
	return job, facts, err
}

func (repository *Repository) ListBootstrapPlanItems(
	ctx context.Context,
	filter BootstrapPlanItemFilter,
) ([]BootstrapPlanItem, error) {
	if repository == nil || repository.database == nil {
		return nil, ErrInvalidRepository
	}
	if err := validateBootstrapPlanItemFilter(filter); err != nil {
		return nil, err
	}
	var items []BootstrapPlanItem
	err := repository.database.View(ctx, func(ctx context.Context, connection storesqlite.ReadConn) error {
		var err error
		items, err = bootstrapPlanItems(ctx, connection, filter)
		return err
	})
	return items, err
}

func (repository *Repository) AdvanceBootstrapRun(ctx context.Context, advance BootstrapAdvance) error {
	if repository == nil || repository.database == nil {
		return ErrInvalidRepository
	}
	if err := validateJobTransition(advance.Job); err != nil {
		return err
	}
	if err := validateBootstrapJobFacts(advance.Facts); err != nil {
		return err
	}
	if advance.Facts.JobID != advance.Job.JobID || advance.Facts.UpdatedAtMS != advance.Job.AtMS {
		return invalidRecord("bootstrap advance facts do not match job transition")
	}
	if advance.Item != nil && (advance.Item.JobID != advance.Job.JobID || advance.Item.UpdatedAtMS != advance.Job.AtMS) {
		return invalidRecord("bootstrap advance item does not match job transition")
	}
	return repository.database.Write(ctx, func(ctx context.Context, transaction storesqlite.WriteTx) error {
		existingJob, found, err := jobRunByID(ctx, transaction, advance.Job.JobID)
		if err != nil {
			return err
		}
		if !found {
			return invalidRecord("bootstrap job does not exist")
		}
		existingFacts, found, err := bootstrapJobByID(ctx, transaction, advance.Job.JobID)
		if err != nil {
			return err
		}
		if !found {
			return invalidRecord("bootstrap facts do not exist")
		}
		projectedJob, err := projectJobTransition(existingJob, advance.Job)
		if err != nil {
			return err
		}
		if err := validateBootstrapFactsAdvance(
			existingJob, projectedJob, existingFacts, advance.Facts,
		); err != nil {
			return err
		}
		var existingItem *BootstrapPlanItem
		if advance.Item != nil {
			item, found, err := bootstrapPlanItemByID(
				ctx, transaction, advance.Item.JobID, advance.Item.Ordinal,
			)
			if err != nil {
				return err
			}
			if !found {
				return invalidRecord("bootstrap plan item does not exist")
			}
			if err := validateBootstrapItemAdvance(item, *advance.Item); err != nil {
				return err
			}
			existingItem = &item
		}
		if jobRunsEqual(existingJob, projectedJob) && bootstrapJobFactsEqual(existingFacts, advance.Facts) &&
			(advance.Item == nil || bootstrapPlanItemEqual(*existingItem, *advance.Item)) {
			return nil
		}
		if err := transitionJobRun(ctx, transaction, advance.Job); err != nil {
			return err
		}
		factsModel := bootstrapJobModelFromDomain(advance.Facts)
		if err := transaction.WithContext(ctx).Model(&bootstrapJobModel{}).
			Where("job_id = ?", advance.Job.JobID).Updates(bootstrapJobUpdates(factsModel)).Error; err != nil {
			return err
		}
		if advance.Item != nil {
			itemModel := bootstrapPlanItemModelFromDomain(*advance.Item)
			if err := transaction.WithContext(ctx).Model(&bootstrapPlanItemModel{}).
				Where("job_id = ? AND ordinal = ?", advance.Item.JobID, advance.Item.Ordinal).
				Updates(bootstrapPlanItemUpdates(itemModel)).Error; err != nil {
				return err
			}
		}
		return nil
	})
}

// AppendBootstrapReconcilePlan freezes one final discovery pass without rewriting
// the initial fast/backfill denominator. Exact replay is a no-op.
func (repository *Repository) AppendBootstrapReconcilePlan(
	ctx context.Context,
	jobID string,
	items []BootstrapPlanItem,
	pass int64,
	changeCount int64,
	issueCount int64,
	atMS int64,
) error {
	if repository == nil || repository.database == nil {
		return ErrInvalidRepository
	}
	validated, addedTotal, err := validateBootstrapReconcileItems(jobID, items, atMS)
	if err != nil || pass < 1 || changeCount < 0 || issueCount < 0 {
		if err != nil {
			return err
		}
		return invalidRecord("bootstrap reconcile counts are invalid")
	}
	return repository.database.Write(ctx, func(ctx context.Context, transaction storesqlite.WriteTx) error {
		job, found, err := jobRunByID(ctx, transaction, jobID)
		if err != nil {
			return err
		}
		if !found || job.State != JobRunning || job.Phase != JobPhaseReconcile ||
			job.ProgressCurrent == nil || job.ProgressTotal == nil {
			return invalidRecord("bootstrap reconcile pass requires a running reconcile job")
		}
		facts, found, err := bootstrapJobByID(ctx, transaction, jobID)
		if err != nil {
			return err
		}
		if !found || facts.PlanState != BootstrapPlanReady {
			return invalidRecord("bootstrap reconcile facts are not ready")
		}
		stored, err := bootstrapPlanItems(ctx, transaction, BootstrapPlanItemFilter{JobID: jobID})
		if err != nil {
			return err
		}
		if facts.ReconcilePlanAtMS != nil {
			if atMS != *facts.ReconcilePlanAtMS || facts.ReconcileChangeCount != changeCount ||
				facts.ReconcileIssueCount != issueCount || facts.ReconcilePass != pass {
				return invalidRecord("bootstrap reconcile replay conflicts with frozen pass")
			}
			if len(validated) == 0 {
				return nil
			}
			start := validated[0].Ordinal
			if start < 0 || start+int64(len(validated)) > int64(len(stored)) ||
				!bootstrapPlanItemsEqual(stored[start:start+int64(len(validated))], validated) {
				return invalidRecord("bootstrap reconcile replay conflicts with frozen pass")
			}
			return nil
		}
		if pass != facts.ReconcilePass+1 {
			return invalidRecord("bootstrap reconcile pass number does not advance")
		}
		if len(validated) > 0 && validated[0].Pass != pass {
			return invalidRecord("bootstrap reconcile item pass does not match facts")
		}
		if len(stored) >= len(validated) && len(validated) > 0 {
			start := validated[0].Ordinal
			if start >= 0 && start+int64(len(validated)) <= int64(len(stored)) {
				replayed := stored[start : start+int64(len(validated))]
				if bootstrapPlanItemsEqual(replayed, validated) &&
					facts.ReconcileChangeCount == changeCount && facts.ReconcileIssueCount == issueCount {
					return nil
				}
				return invalidRecord("bootstrap reconcile replay conflicts with frozen pass")
			}
		}
		if len(validated) > 0 && validated[0].Ordinal != int64(len(stored)) {
			return invalidRecord("bootstrap reconcile ordinal is not contiguous")
		}
		if atMS <= facts.UpdatedAtMS || atMS <= job.UpdatedAtMS ||
			changeCount < facts.ReconcileChangeCount || issueCount < facts.ReconcileIssueCount ||
			*job.ProgressTotal > math.MaxInt64-addedTotal {
			return invalidRecord("bootstrap reconcile pass regresses persisted state")
		}
		if len(stored) > 0 && len(validated) > 0 && validated[0].Pass <= stored[len(stored)-1].Pass {
			return invalidRecord("bootstrap reconcile pass number does not advance")
		}
		if len(validated) > 0 {
			models := make([]bootstrapPlanItemModel, len(validated))
			for index, item := range validated {
				models[index] = bootstrapPlanItemModelFromDomain(item)
			}
			if err := transaction.WithContext(ctx).Create(&models).Error; err != nil {
				return err
			}
		}
		newTotal := *job.ProgressTotal + addedTotal
		if err := transitionJobRun(ctx, transaction, JobTransition{
			JobID: jobID, ExpectedState: JobRunning, State: JobRunning, Phase: JobPhaseReconcile,
			ProgressCurrent: job.ProgressCurrent, ProgressTotal: &newTotal,
			ResumeCursor: job.ResumeCursor, AtMS: atMS,
		}); err != nil {
			return err
		}
		facts.PhaseProgressCurrent = 0
		facts.PhaseProgressTotal = addedTotal
		facts.ReconcilePass = pass
		facts.ReconcilePlanAtMS = &atMS
		facts.ReconcileChangeCount = changeCount
		facts.ReconcileIssueCount = issueCount
		facts.UpdatedAtMS = atMS
		model := bootstrapJobModelFromDomain(facts)
		return transaction.WithContext(ctx).Model(&bootstrapJobModel{}).Where("job_id = ?", jobID).
			Updates(bootstrapJobUpdates(model)).Error
	})
}

// MarkBootstrapSourceUnavailable applies a deleted action only when the active
// generation still matches the discovery fingerprint used by the plan.
func (repository *Repository) MarkBootstrapSourceUnavailable(
	ctx context.Context,
	expected SourceFingerprint,
	atMS int64,
) error {
	if !validSourceFingerprint(expected) || atMS < 0 {
		return invalidRecord("bootstrap deleted source expectation is invalid")
	}
	return repository.WithinWriteUnit(ctx, func(unit *WriteUnit) error {
		active, found, err := generationByState(ctx, unit.transaction, expected.SourceFileID, GenerationActive)
		if err != nil {
			return err
		}
		if !found || !sourceFingerprintEqual(sourceFingerprintFromGeneration(active), expected) {
			return invalidRecord("bootstrap deleted source fingerprint is stale")
		}
		file, found, err := sourceFileByID(ctx, unit.transaction, expected.SourceFileID)
		if err != nil {
			return err
		}
		if !found {
			return invalidRecord("bootstrap deleted source does not exist")
		}
		file.State = SourceFileUnavailable
		file.LastScannedAtMS = &atMS
		file.UpdatedAtMS = atMS
		return unit.UpsertSourceFile(file)
	})
}

// ResumeBootstrapJob creates a new queued attempt from a recoverable terminal
// attempt and clones the fixed plan;
// source generation/checkpoint rows remain authoritative and are not copied.
func (repository *Repository) ResumeBootstrapJob(
	ctx context.Context,
	oldJobID string,
	resumed JobRun,
) error {
	if repository == nil || repository.database == nil {
		return ErrInvalidRepository
	}
	if oldJobID == "" {
		return invalidRecord("interrupted bootstrap job ID must not be empty")
	}
	if err := validateNewJobRun(resumed); err != nil {
		return err
	}
	return repository.database.Write(ctx, func(ctx context.Context, transaction storesqlite.WriteTx) error {
		old, found, err := jobRunByID(ctx, transaction, oldJobID)
		if err != nil {
			return err
		}
		if !found || (old.State != JobInterrupted && old.State != JobFailed && old.State != JobCancelled) {
			return invalidRecord("resume source must be a recoverable terminal bootstrap job")
		}
		if resumed.CreatedAtMS < old.UpdatedAtMS || resumed.UpdatedAtMS < old.UpdatedAtMS ||
			resumed.ResumeOfJobID == nil || *resumed.ResumeOfJobID != oldJobID ||
			resumed.JobType != old.JobType || resumed.RequestedBy != old.RequestedBy ||
			resumed.Priority != old.Priority || resumed.Phase != old.Phase ||
			!equalStringPointer(resumed.SourceFileID, old.SourceFileID) ||
			!equalInt64Pointer(resumed.ProgressCurrent, old.ProgressCurrent) ||
			!equalInt64Pointer(resumed.ProgressTotal, old.ProgressTotal) ||
			!equalJobCursorPointer(resumed.ResumeCursor, old.ResumeCursor) {
			return invalidRecord("resumed bootstrap job does not preserve interrupted lineage")
		}
		oldFacts, found, err := bootstrapJobByID(ctx, transaction, oldJobID)
		if err != nil || !found {
			if err != nil {
				return err
			}
			return invalidRecord("interrupted bootstrap facts do not exist")
		}
		if oldFacts.PlanState != BootstrapPlanReady {
			return invalidRecord("bootstrap resume requires a frozen plan")
		}
		oldItems, err := bootstrapPlanItems(ctx, transaction, BootstrapPlanItemFilter{JobID: oldJobID})
		if err != nil {
			return err
		}
		resumedFacts := cloneBootstrapFactsForResume(oldFacts, resumed.JobID, resumed.UpdatedAtMS)
		resumedItems := cloneBootstrapItemsForResume(oldItems, resumed.JobID, resumed.UpdatedAtMS)
		existing, existingFound, err := jobRunByID(ctx, transaction, resumed.JobID)
		if err != nil {
			return err
		}
		if existingFound {
			if !jobRunsEqual(existing, resumed) {
				return invalidRecord("resumed bootstrap job conflicts with stable identity")
			}
			facts, factsFound, err := bootstrapJobByID(ctx, transaction, resumed.JobID)
			if err != nil || !factsFound {
				return invalidRecord("resumed bootstrap facts are incomplete")
			}
			items, err := bootstrapPlanItems(ctx, transaction, BootstrapPlanItemFilter{JobID: resumed.JobID})
			if err != nil {
				return err
			}
			if !bootstrapJobFactsEqual(facts, resumedFacts) || !bootstrapPlanItemsEqual(items, resumedItems) {
				return invalidRecord("resumed bootstrap payload conflicts with stable identity")
			}
			return nil
		}
		if err := createJobRun(ctx, transaction, resumed); err != nil {
			return err
		}
		factsModel := bootstrapJobModelFromDomain(resumedFacts)
		if err := transaction.WithContext(ctx).Create(&factsModel).Error; err != nil {
			return err
		}
		if len(resumedItems) > 0 {
			models := make([]bootstrapPlanItemModel, len(resumedItems))
			for index, item := range resumedItems {
				models[index] = bootstrapPlanItemModelFromDomain(item)
			}
			if err := transaction.WithContext(ctx).Create(&models).Error; err != nil {
				return err
			}
		}
		return nil
	})
}

func validateBootstrapFactsAdvance(
	existingJob JobRun,
	projectedJob JobRun,
	existing BootstrapJobFacts,
	incoming BootstrapJobFacts,
) error {
	if bootstrapJobFactsEqual(existing, incoming) && jobRunsEqual(existingJob, projectedJob) {
		return nil
	}
	if incoming.UpdatedAtMS <= existing.UpdatedAtMS || existing.JobID != incoming.JobID ||
		existing.SwitchID != incoming.SwitchID || existing.HomeGeneration != incoming.HomeGeneration ||
		existing.HomePath != incoming.HomePath || existing.HomeDeviceID != incoming.HomeDeviceID ||
		existing.HomeInode != incoming.HomeInode || existing.DataStoreKey != incoming.DataStoreKey ||
		existing.Strategy != incoming.Strategy || existing.PlanState != incoming.PlanState ||
		existing.PlanSHA256 != incoming.PlanSHA256 ||
		incoming.ReconcilePass < existing.ReconcilePass ||
		incoming.ReconcileChangeCount < existing.ReconcileChangeCount ||
		incoming.ReconcileIssueCount < existing.ReconcileIssueCount ||
		!timestampPointerAdvances(existing.FirstScreenReadyAtMS, incoming.FirstScreenReadyAtMS) ||
		!timestampPointerAdvances(existing.ReconcilePlanAtMS, incoming.ReconcilePlanAtMS) ||
		!timestampPointerAdvances(existing.FullHistoryReadyAtMS, incoming.FullHistoryReadyAtMS) ||
		!timestampPointerAdvances(existing.ReconciledAtMS, incoming.ReconciledAtMS) {
		return invalidRecord("bootstrap facts regress or change immutable identity")
	}
	if existingJob.Phase == projectedJob.Phase &&
		(incoming.PhaseProgressCurrent < existing.PhaseProgressCurrent ||
			incoming.PhaseProgressTotal < existing.PhaseProgressTotal) {
		return invalidRecord("bootstrap phase progress regresses")
	}
	if incoming.ETAState == BootstrapETAComplete &&
		(projectedJob.State != JobSucceeded || incoming.FullHistoryReadyAtMS == nil) {
		return invalidRecord("complete bootstrap ETA requires succeeded full history")
	}
	if incoming.FullHistoryReadyAtMS != nil &&
		(projectedJob.Phase != JobPhaseReconcile || projectedJob.State != JobSucceeded) {
		return invalidRecord("full history ready requires succeeded reconcile")
	}
	return nil
}

func validateBootstrapItemAdvance(existing, incoming BootstrapPlanItem) error {
	if bootstrapPlanItemEqual(existing, incoming) {
		return nil
	}
	if existing.JobID != incoming.JobID || existing.Ordinal != incoming.Ordinal ||
		existing.Pass != incoming.Pass || existing.Lane != incoming.Lane || existing.Tier != incoming.Tier ||
		existing.ActionKind != incoming.ActionKind ||
		!equalSourceFingerprintPointer(existing.Previous, incoming.Previous) ||
		!equalSourceFingerprintPointer(existing.Current, incoming.Current) ||
		existing.ProgressTotal != incoming.ProgressTotal || incoming.ProgressCurrent < existing.ProgressCurrent ||
		incoming.ProgressCurrent > incoming.ProgressTotal || incoming.UpdatedAtMS <= existing.UpdatedAtMS ||
		!generationPointerAdvances(existing.SourceGeneration, incoming.SourceGeneration) ||
		!validBootstrapItemStateTransition(existing.State, incoming.State) {
		return invalidRecord("bootstrap item transition regresses or changes fixed plan")
	}
	return nil
}

func validBootstrapItemStateTransition(from, to BootstrapItemState) bool {
	switch from {
	case BootstrapItemQueued:
		return to == BootstrapItemRunning || to == BootstrapItemSucceeded ||
			to == BootstrapItemDrifted || to == BootstrapItemFailed
	case BootstrapItemRunning:
		return to == BootstrapItemRunning || to == BootstrapItemSucceeded ||
			to == BootstrapItemDrifted || to == BootstrapItemFailed
	default:
		return false
	}
}

func timestampPointerAdvances(existing, incoming *int64) bool {
	if existing == nil {
		return incoming == nil || *incoming >= 0
	}
	return incoming != nil && *incoming == *existing
}

func generationPointerAdvances(existing, incoming *int64) bool {
	if existing == nil {
		return incoming == nil || *incoming >= 0
	}
	return incoming != nil && *incoming == *existing
}

func bootstrapPlanItemByID(
	ctx context.Context,
	querier *gorm.DB,
	jobID string,
	ordinal int64,
) (BootstrapPlanItem, bool, error) {
	var model bootstrapPlanItemModel
	err := querier.WithContext(ctx).Where("job_id = ? AND ordinal = ?", jobID, ordinal).Take(&model).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return BootstrapPlanItem{}, false, nil
	}
	if err != nil {
		return BootstrapPlanItem{}, false, err
	}
	item, err := bootstrapPlanItemFromModel(model)
	return item, err == nil, err
}

func bootstrapPlanItemUpdates(model bootstrapPlanItemModel) map[string]any {
	return map[string]any{
		"state": model.State, "source_generation": model.SourceGeneration,
		"progress_current": model.ProgressCurrent, "progress_total": model.ProgressTotal,
		"updated_at_ms": model.UpdatedAtMS,
	}
}

func cloneBootstrapFactsForResume(
	value BootstrapJobFacts,
	jobID string,
	atMS int64,
) BootstrapJobFacts {
	value.JobID = jobID
	value.PauseReason = nil
	if value.ReconcilePlanAtMS != nil && value.FullHistoryReadyAtMS == nil {
		value.ReconcilePlanAtMS = nil
		value.ReconcileChangeCount = 0
		value.ReconcileIssueCount = 0
		value.PhaseProgressCurrent = 0
		value.PhaseProgressTotal = 0
	}
	value.UpdatedAtMS = atMS
	return value
}

func cloneBootstrapItemsForResume(
	values []BootstrapPlanItem,
	jobID string,
	atMS int64,
) []BootstrapPlanItem {
	cloned := make([]BootstrapPlanItem, len(values))
	for index, value := range values {
		cloned[index] = cloneBootstrapPlanItem(value)
		cloned[index].JobID = jobID
		cloned[index].UpdatedAtMS = atMS
		if cloned[index].Pass == 0 &&
			(cloned[index].State == BootstrapItemRunning || cloned[index].State == BootstrapItemFailed) {
			cloned[index].State = BootstrapItemQueued
		}
	}
	return cloned
}

func validateBootstrapJobFacts(facts BootstrapJobFacts) error {
	if facts.JobID == "" || facts.SwitchID == "" || facts.HomeGeneration < 0 ||
		!filepath.IsAbs(facts.HomePath) || filepath.Clean(facts.HomePath) != facts.HomePath ||
		facts.HomeDeviceID == "" || facts.HomeInode < 0 || facts.DataStoreKey == "" ||
		(facts.Strategy != "independent_database" && facts.Strategy != "clear_and_rebuild") ||
		!validBootstrapPlanState(facts.PlanState) || !validBootstrapETAState(facts.ETAState) ||
		facts.PhaseProgressCurrent < 0 || facts.PhaseProgressTotal < facts.PhaseProgressCurrent ||
		facts.ReconcilePass < 0 || facts.ReconcileChangeCount < 0 ||
		facts.ReconcileIssueCount < 0 || facts.UpdatedAtMS < 0 {
		return invalidRecord("bootstrap facts are invalid")
	}
	if (facts.PlanState == BootstrapPlanPending) != (facts.PlanSHA256.String() == "") {
		return invalidRecord("bootstrap plan state and digest are inconsistent")
	}
	if facts.ETAState == BootstrapETAKnown {
		if facts.ETARemainingMS == nil || *facts.ETARemainingMS < 0 {
			return invalidRecord("known bootstrap ETA requires non-negative remaining time")
		}
	} else if facts.ETARemainingMS != nil {
		return invalidRecord("unknown or complete bootstrap ETA must not have remaining time")
	}
	if facts.PauseReason != nil && !validBootstrapPauseReason(*facts.PauseReason) {
		return invalidRecord("bootstrap pause reason is invalid")
	}
	if facts.ReconcilePlanAtMS != nil && facts.ReconcilePass == 0 {
		return invalidRecord("bootstrap reconcile timestamp requires a persisted pass")
	}
	if !validOrderedTimestamps(
		facts.FirstScreenReadyAtMS, facts.ReconcilePlanAtMS,
		facts.FullHistoryReadyAtMS, facts.ReconciledAtMS,
	) {
		return invalidRecord("bootstrap ready timestamps are inconsistent")
	}
	return nil
}

func validOrderedTimestamps(first, planned, full, reconciled *int64) bool {
	for _, value := range []*int64{first, planned, full, reconciled} {
		if value != nil && *value < 0 {
			return false
		}
	}
	if planned != nil && first == nil || full != nil && planned == nil || reconciled != nil && full == nil {
		return false
	}
	return first == nil || planned == nil || *planned >= *first &&
		(full == nil || *full >= *planned && (reconciled == nil || *reconciled >= *full))
}

func validateBootstrapPlan(
	jobID string,
	items []BootstrapPlanItem,
	atMS int64,
) ([]BootstrapPlanItem, int64, int64, SHA256Digest, error) {
	validated := make([]BootstrapPlanItem, len(items))
	var total int64
	var fastTotal int64
	for index, item := range items {
		if item.JobID != jobID || item.Ordinal != int64(index) || item.Pass != 0 ||
			!validBootstrapLane(item.Lane) || item.Lane == BootstrapLaneReconcile ||
			!validBootstrapTier(item.Tier) || item.Tier == BootstrapTierReconcile ||
			!validBootstrapActionKind(item.ActionKind) ||
			item.State != BootstrapItemQueued || item.SourceGeneration != nil ||
			item.ProgressCurrent != 0 || item.ProgressTotal < 0 || item.UpdatedAtMS != atMS ||
			!validBootstrapActionSnapshots(item.ActionKind, item.Previous, item.Current) {
			return nil, 0, 0, SHA256Digest{}, invalidRecord("bootstrap plan item is invalid")
		}
		if item.Current != nil && item.ProgressTotal != item.Current.SizeBytes {
			return nil, 0, 0, SHA256Digest{}, invalidRecord("bootstrap item total does not match current snapshot")
		}
		if item.Current == nil && item.ProgressTotal != 0 {
			return nil, 0, 0, SHA256Digest{}, invalidRecord("bootstrap absence item must have zero total")
		}
		if total > math.MaxInt64-item.ProgressTotal {
			return nil, 0, 0, SHA256Digest{}, invalidRecord("bootstrap plan total overflows")
		}
		total += item.ProgressTotal
		if item.Lane == BootstrapLaneFast {
			fastTotal += item.ProgressTotal
		}
		validated[index] = cloneBootstrapPlanItem(item)
	}
	return validated, total, fastTotal, bootstrapPlanDigest(validated), nil
}

func validateBootstrapReconcileItems(
	jobID string,
	items []BootstrapPlanItem,
	atMS int64,
) ([]BootstrapPlanItem, int64, error) {
	if jobID == "" || atMS < 0 {
		return nil, 0, invalidRecord("bootstrap reconcile identity or time is invalid")
	}
	validated := make([]BootstrapPlanItem, len(items))
	var total int64
	var pass int64
	for index, item := range items {
		if index == 0 {
			pass = item.Pass
		}
		if item.JobID != jobID || item.Ordinal < 0 || item.Pass < 1 || item.Pass != pass ||
			item.Lane != BootstrapLaneReconcile || item.Tier != BootstrapTierReconcile ||
			!validBootstrapActionKind(item.ActionKind) || item.State != BootstrapItemQueued ||
			item.SourceGeneration != nil || item.ProgressCurrent != 0 || item.ProgressTotal < 0 ||
			item.UpdatedAtMS != atMS || !validBootstrapActionSnapshots(item.ActionKind, item.Previous, item.Current) {
			return nil, 0, invalidRecord("bootstrap reconcile item is invalid")
		}
		if index > 0 && item.Ordinal != items[index-1].Ordinal+1 {
			return nil, 0, invalidRecord("bootstrap reconcile item ordinals are not contiguous")
		}
		if item.Current != nil && item.ProgressTotal != item.Current.SizeBytes ||
			item.Current == nil && item.ProgressTotal != 0 || total > math.MaxInt64-item.ProgressTotal {
			return nil, 0, invalidRecord("bootstrap reconcile item total is invalid")
		}
		total += item.ProgressTotal
		validated[index] = cloneBootstrapPlanItem(item)
	}
	return validated, total, nil
}

func validBootstrapActionSnapshots(
	kind BootstrapActionKind,
	previous *SourceFingerprint,
	current *SourceFingerprint,
) bool {
	if previous != nil && !validSourceFingerprint(*previous) || current != nil && !validSourceFingerprint(*current) {
		return false
	}
	switch kind {
	case BootstrapActionAdded:
		return previous == nil && current != nil
	case BootstrapActionDeleted, BootstrapActionUnreadable:
		return previous != nil && current == nil
	case BootstrapActionUnchanged, BootstrapActionGrown, BootstrapActionTruncated,
		BootstrapActionMoved, BootstrapActionReplaced:
		return previous != nil && current != nil
	default:
		return false
	}
}

func bootstrapPlanDigest(items []BootstrapPlanItem) SHA256Digest {
	hasher := sha256.New()
	for _, item := range items {
		writeFingerprintInt64(hasher, item.Ordinal)
		writeFingerprintInt64(hasher, item.Pass)
		writeFingerprintString(hasher, string(item.Lane))
		writeFingerprintString(hasher, string(item.Tier))
		writeFingerprintString(hasher, string(item.ActionKind))
		writeBootstrapSnapshotDigest(hasher, item.Previous)
		writeBootstrapSnapshotDigest(hasher, item.Current)
		writeFingerprintInt64(hasher, item.ProgressTotal)
	}
	return SHA256DigestOf(hasher.Sum(nil))
}

func writeBootstrapSnapshotDigest(hasher hash.Hash, value *SourceFingerprint) {
	if value == nil {
		writeFingerprintString(hasher, "")
		return
	}
	writeFingerprintString(hasher, sourceTargetIdentityDigest(*value))
}

func validateBootstrapPlanItemFilter(filter BootstrapPlanItemFilter) error {
	if filter.JobID == "" || filter.Lane != nil && !validBootstrapLane(*filter.Lane) ||
		filter.State != nil && !validBootstrapItemState(*filter.State) {
		return invalidRecord("bootstrap plan filter is invalid")
	}
	return nil
}

func bootstrapJobByID(
	ctx context.Context,
	querier *gorm.DB,
	jobID string,
) (BootstrapJobFacts, bool, error) {
	var model bootstrapJobModel
	err := querier.WithContext(ctx).Take(&model, "job_id = ?", jobID).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return BootstrapJobFacts{}, false, nil
	}
	if err != nil {
		return BootstrapJobFacts{}, false, err
	}
	facts, err := bootstrapJobFromModel(model)
	return facts, err == nil, err
}

func bootstrapPlanItems(
	ctx context.Context,
	querier *gorm.DB,
	filter BootstrapPlanItemFilter,
) ([]BootstrapPlanItem, error) {
	query := querier.WithContext(ctx).Where("job_id = ?", filter.JobID)
	if filter.Lane != nil {
		query = query.Where("lane = ?", string(*filter.Lane))
	}
	if filter.State != nil {
		query = query.Where("state = ?", string(*filter.State))
	}
	var models []bootstrapPlanItemModel
	if err := query.Order("pass").Order("ordinal").Find(&models).Error; err != nil {
		return nil, err
	}
	items := make([]BootstrapPlanItem, len(models))
	for index, model := range models {
		item, err := bootstrapPlanItemFromModel(model)
		if err != nil {
			return nil, err
		}
		items[index] = item
	}
	return items, nil
}

func bootstrapJobModelFromDomain(value BootstrapJobFacts) bootstrapJobModel {
	model := bootstrapJobModel{
		JobID: value.JobID, SwitchID: value.SwitchID, HomeGeneration: value.HomeGeneration,
		HomePath: value.HomePath, HomeDeviceID: value.HomeDeviceID, HomeInode: value.HomeInode,
		DataStoreKey: value.DataStoreKey, Strategy: value.Strategy, PlanState: string(value.PlanState),
		PhaseProgressCurrent: value.PhaseProgressCurrent, PhaseProgressTotal: value.PhaseProgressTotal,
		ETAState: string(value.ETAState), ETARemainingMS: value.ETARemainingMS,
		FirstScreenReadyAtMS: value.FirstScreenReadyAtMS, FullHistoryReadyAtMS: value.FullHistoryReadyAtMS,
		ReconcilePass: value.ReconcilePass, ReconcilePlanAtMS: value.ReconcilePlanAtMS,
		ReconciledAtMS:       value.ReconciledAtMS,
		ReconcileChangeCount: value.ReconcileChangeCount,
		ReconcileIssueCount:  value.ReconcileIssueCount, UpdatedAtMS: value.UpdatedAtMS,
	}
	if digest := value.PlanSHA256.String(); digest != "" {
		model.PlanSHA256 = &digest
	}
	if value.PauseReason != nil {
		converted := string(*value.PauseReason)
		model.PauseReason = &converted
	}
	return model
}

func bootstrapJobFromModel(model bootstrapJobModel) (BootstrapJobFacts, error) {
	var digest SHA256Digest
	if model.PlanSHA256 != nil {
		var ok bool
		digest, ok = parseSHA256Digest(*model.PlanSHA256)
		if !ok {
			return BootstrapJobFacts{}, invalidRecord("stored bootstrap plan digest is invalid")
		}
	}
	value := BootstrapJobFacts{
		JobID: model.JobID, SwitchID: model.SwitchID, HomeGeneration: model.HomeGeneration,
		HomePath: model.HomePath, HomeDeviceID: model.HomeDeviceID, HomeInode: model.HomeInode,
		DataStoreKey: model.DataStoreKey, Strategy: model.Strategy, PlanState: BootstrapPlanState(model.PlanState),
		PlanSHA256: digest, PhaseProgressCurrent: model.PhaseProgressCurrent,
		PhaseProgressTotal: model.PhaseProgressTotal, ETAState: BootstrapETAState(model.ETAState),
		ETARemainingMS: model.ETARemainingMS, FirstScreenReadyAtMS: model.FirstScreenReadyAtMS,
		ReconcilePass: model.ReconcilePass, ReconcilePlanAtMS: model.ReconcilePlanAtMS,
		FullHistoryReadyAtMS: model.FullHistoryReadyAtMS,
		ReconciledAtMS:       model.ReconciledAtMS,
		ReconcileChangeCount: model.ReconcileChangeCount, ReconcileIssueCount: model.ReconcileIssueCount,
		UpdatedAtMS: model.UpdatedAtMS,
	}
	if model.PauseReason != nil {
		converted := BootstrapPauseReason(*model.PauseReason)
		value.PauseReason = &converted
	}
	if err := validateBootstrapJobFacts(value); err != nil {
		return BootstrapJobFacts{}, err
	}
	return value, nil
}

func bootstrapJobUpdates(model bootstrapJobModel) map[string]any {
	return map[string]any{
		"plan_state": model.PlanState, "plan_sha256": model.PlanSHA256,
		"phase_progress_current": model.PhaseProgressCurrent,
		"phase_progress_total":   model.PhaseProgressTotal, "eta_state": model.ETAState,
		"eta_remaining_ms": model.ETARemainingMS, "pause_reason": model.PauseReason,
		"first_screen_ready_at_ms": model.FirstScreenReadyAtMS,
		"reconcile_pass":           model.ReconcilePass,
		"reconcile_plan_at_ms":     model.ReconcilePlanAtMS,
		"full_history_ready_at_ms": model.FullHistoryReadyAtMS,
		"reconciled_at_ms":         model.ReconciledAtMS,
		"reconcile_change_count":   model.ReconcileChangeCount,
		"reconcile_issue_count":    model.ReconcileIssueCount, "updated_at_ms": model.UpdatedAtMS,
	}
}

func bootstrapPlanItemModelFromDomain(value BootstrapPlanItem) bootstrapPlanItemModel {
	model := bootstrapPlanItemModel{
		JobID: value.JobID, Ordinal: value.Ordinal, Pass: value.Pass, Lane: string(value.Lane),
		Tier: string(value.Tier), ActionKind: string(value.ActionKind), State: string(value.State),
		SourceGeneration: value.SourceGeneration, ProgressCurrent: value.ProgressCurrent,
		ProgressTotal: value.ProgressTotal, UpdatedAtMS: value.UpdatedAtMS,
	}
	assignPreviousSnapshot(&model, value.Previous)
	assignCurrentSnapshot(&model, value.Current)
	return model
}

func assignPreviousSnapshot(model *bootstrapPlanItemModel, value *SourceFingerprint) {
	if value == nil {
		return
	}
	model.PreviousSourceID = pointerToValue(value.SourceFileID)
	model.PreviousKind = pointerToValue(value.SourceKind)
	model.PreviousPath = pointerToValue(value.CurrentPath)
	model.PreviousDeviceID = pointerToValue(value.DeviceID)
	model.PreviousInode = pointerToValue(value.Inode)
	model.PreviousSize = pointerToValue(value.SizeBytes)
	model.PreviousMTimeNS = pointerToValue(value.MTimeNS)
	model.PreviousPrefixN = pointerToValue(value.PrefixBytes)
	model.PreviousPrefix = pointerToValue(value.PrefixSHA256)
	model.PreviousDigest = pointerToValue(value.FingerprintSHA256)
}

func assignCurrentSnapshot(model *bootstrapPlanItemModel, value *SourceFingerprint) {
	if value == nil {
		return
	}
	model.CurrentSourceID = pointerToValue(value.SourceFileID)
	model.CurrentKind = pointerToValue(value.SourceKind)
	model.CurrentPath = pointerToValue(value.CurrentPath)
	model.CurrentDeviceID = pointerToValue(value.DeviceID)
	model.CurrentInode = pointerToValue(value.Inode)
	model.CurrentSize = pointerToValue(value.SizeBytes)
	model.CurrentMTimeNS = pointerToValue(value.MTimeNS)
	model.CurrentPrefixN = pointerToValue(value.PrefixBytes)
	model.CurrentPrefix = pointerToValue(value.PrefixSHA256)
	model.CurrentDigest = pointerToValue(value.FingerprintSHA256)
}

func bootstrapPlanItemFromModel(model bootstrapPlanItemModel) (BootstrapPlanItem, error) {
	previous, err := sourceFingerprintFromBootstrapColumns(
		model.PreviousSourceID, model.PreviousKind, model.PreviousPath, model.PreviousDeviceID,
		model.PreviousInode, model.PreviousSize, model.PreviousMTimeNS, model.PreviousPrefixN,
		model.PreviousPrefix, model.PreviousDigest,
	)
	if err != nil {
		return BootstrapPlanItem{}, err
	}
	current, err := sourceFingerprintFromBootstrapColumns(
		model.CurrentSourceID, model.CurrentKind, model.CurrentPath, model.CurrentDeviceID,
		model.CurrentInode, model.CurrentSize, model.CurrentMTimeNS, model.CurrentPrefixN,
		model.CurrentPrefix, model.CurrentDigest,
	)
	if err != nil {
		return BootstrapPlanItem{}, err
	}
	value := BootstrapPlanItem{
		JobID: model.JobID, Ordinal: model.Ordinal, Pass: model.Pass, Lane: BootstrapLane(model.Lane),
		Tier: BootstrapTier(model.Tier), ActionKind: BootstrapActionKind(model.ActionKind),
		Previous: previous, Current: current, State: BootstrapItemState(model.State),
		SourceGeneration: model.SourceGeneration, ProgressCurrent: model.ProgressCurrent,
		ProgressTotal: model.ProgressTotal, UpdatedAtMS: model.UpdatedAtMS,
	}
	if !validBootstrapLane(value.Lane) || !validBootstrapTier(value.Tier) ||
		!validBootstrapActionKind(value.ActionKind) || !validBootstrapItemState(value.State) ||
		!validBootstrapActionSnapshots(value.ActionKind, value.Previous, value.Current) {
		return BootstrapPlanItem{}, invalidRecord("stored bootstrap plan item is invalid")
	}
	return value, nil
}

func sourceFingerprintFromBootstrapColumns(
	sourceID, kind, path, deviceID *string,
	inode, size, mtime, prefixN *int64,
	prefix, digest *string,
) (*SourceFingerprint, error) {
	values := []bool{
		sourceID != nil, kind != nil, path != nil, deviceID != nil, inode != nil,
		size != nil, mtime != nil, prefixN != nil, prefix != nil, digest != nil,
	}
	allNil, allPresent := true, true
	for _, present := range values {
		allNil = allNil && !present
		allPresent = allPresent && present
	}
	if allNil {
		return nil, nil
	}
	if !allPresent {
		return nil, invalidRecord("stored bootstrap snapshot columns are inconsistent")
	}
	value := SourceFingerprint{
		SourceFileID: *sourceID, Provider: "codex", SourceKind: *kind, CurrentPath: *path,
		DeviceID: *deviceID, Inode: *inode, SizeBytes: *size, MTimeNS: *mtime,
		PrefixBytes: *prefixN, PrefixSHA256: *prefix, FingerprintSHA256: *digest,
	}
	if !validSourceFingerprint(value) {
		return nil, invalidRecord("stored bootstrap snapshot is invalid")
	}
	return &value, nil
}

func pointerToValue[T any](value T) *T { return &value }

func cloneBootstrapPlanItem(value BootstrapPlanItem) BootstrapPlanItem {
	copy := value
	if value.Previous != nil {
		previous := *value.Previous
		copy.Previous = &previous
	}
	if value.Current != nil {
		current := *value.Current
		copy.Current = &current
	}
	if value.SourceGeneration != nil {
		generation := *value.SourceGeneration
		copy.SourceGeneration = &generation
	}
	return copy
}

func bootstrapPlanItemsEqual(left, right []BootstrapPlanItem) bool {
	if len(left) != len(right) {
		return false
	}
	for index := range left {
		if !bootstrapPlanItemEqual(left[index], right[index]) {
			return false
		}
	}
	return true
}

func bootstrapPlanItemEqual(left, right BootstrapPlanItem) bool {
	return left.JobID == right.JobID && left.Ordinal == right.Ordinal && left.Pass == right.Pass &&
		left.Lane == right.Lane && left.Tier == right.Tier && left.ActionKind == right.ActionKind &&
		equalSourceFingerprintPointer(left.Previous, right.Previous) &&
		equalSourceFingerprintPointer(left.Current, right.Current) && left.State == right.State &&
		equalInt64Pointer(left.SourceGeneration, right.SourceGeneration) &&
		left.ProgressCurrent == right.ProgressCurrent && left.ProgressTotal == right.ProgressTotal &&
		left.UpdatedAtMS == right.UpdatedAtMS
}

func equalSourceFingerprintPointer(left, right *SourceFingerprint) bool {
	if left == nil || right == nil {
		return left == nil && right == nil
	}
	return *left == *right
}

func bootstrapJobFactsEqual(left, right BootstrapJobFacts) bool {
	return left.JobID == right.JobID && left.SwitchID == right.SwitchID &&
		left.HomeGeneration == right.HomeGeneration && left.HomePath == right.HomePath &&
		left.HomeDeviceID == right.HomeDeviceID && left.HomeInode == right.HomeInode &&
		left.DataStoreKey == right.DataStoreKey && left.Strategy == right.Strategy &&
		left.PlanState == right.PlanState && left.PlanSHA256 == right.PlanSHA256 &&
		left.PhaseProgressCurrent == right.PhaseProgressCurrent &&
		left.PhaseProgressTotal == right.PhaseProgressTotal && left.ETAState == right.ETAState &&
		equalInt64Pointer(left.ETARemainingMS, right.ETARemainingMS) &&
		equalBootstrapPausePointer(left.PauseReason, right.PauseReason) &&
		equalInt64Pointer(left.FirstScreenReadyAtMS, right.FirstScreenReadyAtMS) &&
		left.ReconcilePass == right.ReconcilePass &&
		equalInt64Pointer(left.ReconcilePlanAtMS, right.ReconcilePlanAtMS) &&
		equalInt64Pointer(left.FullHistoryReadyAtMS, right.FullHistoryReadyAtMS) &&
		equalInt64Pointer(left.ReconciledAtMS, right.ReconciledAtMS) &&
		left.ReconcileChangeCount == right.ReconcileChangeCount &&
		left.ReconcileIssueCount == right.ReconcileIssueCount && left.UpdatedAtMS == right.UpdatedAtMS
}

func equalBootstrapPausePointer(left, right *BootstrapPauseReason) bool {
	if left == nil || right == nil {
		return left == nil && right == nil
	}
	return *left == *right
}
