package store

import (
	"context"

	"github.com/SisyphusSQ/codex-pulse/internal/runtimeclock"
	storesqlite "github.com/SisyphusSQ/codex-pulse/internal/store/sqlite"
)

// UpsertSourceFile 只允许稳定物理身份下 generation/offset 单调推进。
func (repository *Repository) UpsertSourceFile(ctx context.Context, file SourceFile) error {
	return repository.WithinWriteUnit(ctx, func(unit *WriteUnit) error {
		return unit.UpsertSourceFile(file)
	})
}

// UpsertSourceState 以 updated_at_ms 为 ordering key 更新逻辑来源状态。
func (repository *Repository) UpsertSourceState(ctx context.Context, state SourceState) error {
	if repository == nil || repository.database == nil {
		return ErrInvalidRepository
	}
	if err := validateSourceState(state); err != nil {
		return err
	}
	return repository.database.Write(ctx, func(ctx context.Context, transaction storesqlite.WriteTx) error {
		existing, found, err := sourceStateByID(ctx, transaction, state.SourceInstanceID)
		if err != nil {
			return err
		}
		if found {
			if existing.SourceType != state.SourceType || existing.ScopeKey != state.ScopeKey {
				return invalidRecord("source state stable identity conflicts")
			}
			if state.UpdatedAtMS < existing.UpdatedAtMS {
				return invalidRecord("source state update time regresses")
			}
			if state.CursorVersion < existing.CursorVersion {
				return invalidRecord("source cursor version regresses")
			}
			if state.UpdatedAtMS == existing.UpdatedAtMS {
				if sourceStatesEqual(existing, state) {
					return nil
				}
				return invalidRecord("source state conflicts at the same update time")
			}
			if int64PointerRegresses(existing.LastAttemptAtMS, state.LastAttemptAtMS) ||
				int64PointerRegresses(existing.LastSuccessAtMS, state.LastSuccessAtMS) {
				return invalidRecord("source state observation timestamp regresses")
			}
		} else {
			var count int64
			err := transaction.WithContext(ctx).Model(&sourceStateModel{}).
				Where("source_type = ? AND scope_key = ?", state.SourceType, state.ScopeKey).
				Count(&count).Error
			if err != nil {
				return err
			}
			if count > 0 {
				return invalidRecord("source state scope belongs to another identity")
			}
		}
		model := sourceStateModelFromDomain(state)
		if !found {
			if err := transaction.WithContext(ctx).Create(&model).Error; err != nil {
				return err
			}
		} else if err := transaction.WithContext(ctx).Model(&sourceStateModel{}).
			Where("source_instance_id = ?", state.SourceInstanceID).Updates(sourceStateUpdates(model)).Error; err != nil {
			return err
		}
		if state.SourceInstanceID != QuotaSourceInstanceWhamDefault || state.SourceType != QuotaSourceTypeWham ||
			state.ScopeKey != QuotaAccountScopeDefault || !transaction.Migrator().HasTable(&quotaCurrentModel{}) {
			return nil
		}
		evaluatedAtMS, err := repository.quotaEvaluationTimeMS()
		if err != nil {
			return err
		}
		return repository.rebuildQuotaScopeProjectionInTransaction(
			ctx, transaction.WithContext(ctx), state.ScopeKey, evaluatedAtMS, defaultQuotaArbitrationRule(),
		)
	})
}

// AppendSourceAttempt 追加一次已结束尝试；同 request ID 只允许完全一致重放。
func (repository *Repository) AppendSourceAttempt(ctx context.Context, attempt SourceAttempt) error {
	if repository == nil || repository.database == nil {
		return ErrInvalidRepository
	}
	if err := validateSourceAttempt(attempt); err != nil {
		return err
	}
	return repository.database.Write(ctx, func(ctx context.Context, transaction storesqlite.WriteTx) error {
		if err := requireStoredReference(
			ctx, transaction, &sourceStateModel{}, "source_instance_id = ?",
			attempt.SourceInstanceID, "source state",
		); err != nil {
			return err
		}
		existing, found, err := sourceAttemptByID(ctx, transaction, attempt.RequestID)
		if err != nil {
			return err
		}
		if found {
			if sourceAttemptsEqual(existing, attempt) {
				return nil
			}
			return invalidRecord("source attempt conflicts with append-only history")
		}
		model := sourceAttemptModelFromDomain(attempt)
		return transaction.WithContext(ctx).Create(&model).Error
	})
}

func validateSourceFileProgression(existing, incoming SourceFile) error {
	if existing.Provider != incoming.Provider || existing.DeviceID != incoming.DeviceID || existing.Inode != incoming.Inode {
		return invalidRecord("source file stable identity conflicts")
	}
	if existing.SessionID != nil && !equalStringPointer(existing.SessionID, incoming.SessionID) &&
		incoming.ActiveGeneration <= existing.ActiveGeneration {
		return invalidRecord("source file session identity conflicts")
	}
	if incoming.UpdatedAtMS < existing.UpdatedAtMS {
		return invalidRecord("source file update time regresses")
	}
	if incoming.UpdatedAtMS == existing.UpdatedAtMS {
		if sourceFilesEqual(existing, incoming) {
			return nil
		}
		return invalidRecord("source file conflicts at the same update time")
	}
	if incoming.ActiveGeneration < existing.ActiveGeneration {
		return invalidRecord("source file generation regresses")
	}
	if incoming.ActiveGeneration == existing.ActiveGeneration &&
		(incoming.ParsedOffset < existing.ParsedOffset || incoming.SizeBytes < existing.SizeBytes || incoming.MTimeNS < existing.MTimeNS) {
		return invalidRecord("source file cursor regresses within a generation")
	}
	return nil
}

func int64PointerRegresses(existing, incoming *int64) bool {
	return existing != nil && (incoming == nil || *incoming < *existing)
}

func nullableRuntimeErrorClass(value *RuntimeErrorClass) any {
	if value == nil {
		return nil
	}
	return string(*value)
}

func nullableSHA256Digest(value *SHA256Digest) any {
	if value == nil {
		return nil
	}
	return value.String()
}

// CreateJobRun 创建 queued job；同 ID 只允许完全一致重放。
func (repository *Repository) CreateJobRun(ctx context.Context, job JobRun) error {
	if repository == nil || repository.database == nil {
		return ErrInvalidRepository
	}
	if err := validateNewJobRun(job); err != nil {
		return err
	}
	if job.ResumeOfJobID != nil {
		return invalidRecord("new job resume lineage must use ResumeInterruptedJob")
	}
	return repository.database.Write(ctx, func(ctx context.Context, transaction storesqlite.WriteTx) error {
		return createJobRun(ctx, transaction, job)
	})
}

// TransitionJobRun 在同一 writer transaction 中比较并推进作业状态。
func (repository *Repository) TransitionJobRun(ctx context.Context, transition JobTransition) error {
	if repository == nil || repository.database == nil {
		return ErrInvalidRepository
	}
	if err := validateJobTransition(transition); err != nil {
		return err
	}
	return repository.database.Write(ctx, func(ctx context.Context, transaction storesqlite.WriteTx) error {
		return transitionJobRun(ctx, transaction, transition)
	})
}

func transitionJobRun(
	ctx context.Context,
	transaction storesqlite.WriteTx,
	transition JobTransition,
) error {
	existing, found, err := jobRunByID(ctx, transaction, transition.JobID)
	if err != nil {
		return err
	}
	if !found {
		return invalidRecord("job run does not exist")
	}
	projected, err := projectJobTransition(existing, transition)
	if err != nil {
		return err
	}
	if jobRunsEqual(existing, projected) {
		return nil
	}
	return updateJobRun(ctx, transaction, projected)
}

// InterruptIncompleteJobs 原子终结启动时遗留的 queued/running jobs。
func (repository *Repository) InterruptIncompleteJobs(ctx context.Context, atMS int64) (int64, error) {
	if repository == nil || repository.database == nil {
		return 0, ErrInvalidRepository
	}
	if atMS < 0 || atMS > runtimeclock.MaxTimestampMS {
		return 0, invalidRecord("job interruption timestamp must not be negative")
	}
	var interrupted int64
	err := repository.database.Write(ctx, func(ctx context.Context, transaction storesqlite.WriteTx) error {
		var newer int64
		if err := transaction.WithContext(ctx).Model(&jobRunModel{}).
			Where("state IN ? AND updated_at_ms > ?", []string{string(JobQueued), string(JobRunning)}, atMS).
			Count(&newer).Error; err != nil {
			return err
		}
		if newer > 0 {
			return invalidRecord("job interruption timestamp precedes an incomplete job update")
		}
		result := transaction.WithContext(ctx).Model(&jobRunModel{}).
			Where("state IN ?", []string{string(JobQueued), string(JobRunning)}).
			Updates(map[string]any{
				"state": string(JobInterrupted), "finished_at_ms": atMS,
				"error_class": nil, "updated_at_ms": atMS,
			})
		interrupted = result.RowsAffected
		return result.Error
	})
	return interrupted, err
}

// ResumeInterruptedJob 创建带血缘的新 queued job，旧 terminal row 保持不变。
func (repository *Repository) ResumeInterruptedJob(ctx context.Context, oldJobID string, resumed JobRun) error {
	if repository == nil || repository.database == nil {
		return ErrInvalidRepository
	}
	if oldJobID == "" {
		return invalidRecord("interrupted job ID must not be empty")
	}
	if err := validateNewJobRun(resumed); err != nil {
		return err
	}
	return repository.database.Write(ctx, func(ctx context.Context, transaction storesqlite.WriteTx) error {
		old, found, err := jobRunByID(ctx, transaction, oldJobID)
		if err != nil {
			return err
		}
		if !found || old.State != JobInterrupted {
			return invalidRecord("resume source must be an interrupted job")
		}
		if resumed.CreatedAtMS < old.UpdatedAtMS || resumed.UpdatedAtMS < old.UpdatedAtMS {
			return invalidRecord("resumed job time precedes interrupted history")
		}
		if resumed.ResumeOfJobID == nil || *resumed.ResumeOfJobID != oldJobID ||
			resumed.JobType != old.JobType || resumed.Priority != old.Priority || resumed.Phase != old.Phase ||
			!equalStringPointer(resumed.SourceFileID, old.SourceFileID) ||
			!equalInt64Pointer(resumed.ProgressCurrent, old.ProgressCurrent) ||
			!equalInt64Pointer(resumed.ProgressTotal, old.ProgressTotal) ||
			!equalJobCursorPointer(resumed.ResumeCursor, old.ResumeCursor) {
			return invalidRecord("resumed job does not preserve interrupted cursor and lineage")
		}
		replay, err := interruptedResumeConsumption(old, resumed)
		if err != nil {
			return err
		}
		if replay {
			existing, found, readErr := jobRunByID(ctx, transaction, resumed.JobID)
			if readErr != nil || !found {
				return readErr
			}
			if !jobRunsEqual(existing, resumed) {
				return invalidRecord("resumed job conflicts with consumed stable identity")
			}
			return nil
		}
		if err := createJobRun(ctx, transaction, resumed); err != nil {
			return err
		}
		return markInterruptedResumeConsumed(ctx, transaction, old, resumed)
	})
}

func interruptedResumeConsumption(old JobRun, resumed JobRun) (bool, error) {
	if old.State != JobInterrupted || old.ResumeConsumedByJobID == nil {
		return false, nil
	}
	if *old.ResumeConsumedByJobID == resumed.JobID {
		return true, nil
	}
	return false, invalidRecord("interrupted job resume was already consumed")
}

func markInterruptedResumeConsumed(
	ctx context.Context,
	transaction storesqlite.WriteTx,
	old JobRun,
	resumed JobRun,
) error {
	if old.State != JobInterrupted {
		return nil
	}
	if old.ResumeConsumedByJobID != nil {
		if *old.ResumeConsumedByJobID == resumed.JobID {
			return nil
		}
		return invalidRecord("interrupted job resume was already consumed")
	}
	result := transaction.WithContext(ctx).Model(&jobRunModel{}).
		Where("job_id = ? AND state = ? AND resume_consumed_by_job_id IS NULL", old.JobID, JobInterrupted).
		UpdateColumn("resume_consumed_by_job_id", resumed.JobID)
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected != 1 {
		return invalidRecord("interrupted job resume consumption conflicted")
	}
	return nil
}

func createJobRun(ctx context.Context, transaction storesqlite.WriteTx, job JobRun) error {
	if job.SourceFileID != nil {
		if err := requireStoredReference(
			ctx, transaction, &sourceFileModel{}, "source_file_id = ?", *job.SourceFileID, "source file",
		); err != nil {
			return err
		}
	}
	if job.ResumeOfJobID != nil {
		if err := requireStoredReference(
			ctx, transaction, &jobRunModel{}, "job_id = ?", *job.ResumeOfJobID, "resume job",
		); err != nil {
			return err
		}
	}
	existing, found, err := jobRunByID(ctx, transaction, job.JobID)
	if err != nil {
		return err
	}
	if found {
		if jobRunsEqual(existing, job) {
			return nil
		}
		return invalidRecord("job run conflicts with stable identity")
	}
	model := jobRunModelFromDomain(job)
	return transaction.WithContext(ctx).Create(&model).Error
}

func projectJobTransition(existing JobRun, transition JobTransition) (JobRun, error) {
	projected := existing
	if transition.ProgressCurrent != nil {
		projected.ProgressCurrent = transition.ProgressCurrent
	}
	if transition.ProgressTotal != nil {
		projected.ProgressTotal = transition.ProgressTotal
	}
	if transition.ResumeCursor != nil {
		projected.ResumeCursor = transition.ResumeCursor
	}
	projected.State = transition.State
	projected.Phase = transition.Phase
	projected.ErrorClass = transition.ErrorClass
	projected.UpdatedAtMS = transition.AtMS
	if transition.State == JobRunning && projected.StartedAtMS == nil {
		projected.StartedAtMS = &projected.UpdatedAtMS
	}
	if isTerminalJobState(transition.State) {
		projected.FinishedAtMS = &projected.UpdatedAtMS
	}

	if jobRunsEqual(existing, projected) {
		return projected, nil
	}
	if existing.State != transition.ExpectedState {
		return JobRun{}, invalidRecord("job transition expected state is stale")
	}
	if !validJobStateTransition(existing.State, transition.State) {
		return JobRun{}, invalidRecord("job state transition is illegal")
	}
	if transition.AtMS <= existing.UpdatedAtMS {
		return JobRun{}, invalidRecord("job transition time does not advance")
	}
	existingPhase, _ := jobPhaseRank(existing.Phase)
	incomingPhase, _ := jobPhaseRank(transition.Phase)
	if incomingPhase < existingPhase {
		return JobRun{}, invalidRecord("job phase regresses")
	}
	if int64PointerRegresses(existing.ProgressCurrent, projected.ProgressCurrent) ||
		int64PointerRegresses(existing.ProgressTotal, projected.ProgressTotal) {
		return JobRun{}, invalidRecord("job progress regresses")
	}
	if err := validateJobProgress(projected.ProgressCurrent, projected.ProgressTotal); err != nil {
		return JobRun{}, err
	}
	return projected, nil
}

func validJobStateTransition(from, to JobState) bool {
	switch from {
	case JobQueued:
		return to == JobRunning || to == JobCancelled || to == JobInterrupted
	case JobRunning:
		return to == JobRunning || to == JobSucceeded || to == JobFailed || to == JobCancelled || to == JobInterrupted
	default:
		return false
	}
}

func isTerminalJobState(state JobState) bool {
	return state == JobSucceeded || state == JobFailed || state == JobCancelled || state == JobInterrupted
}

func updateJobRun(ctx context.Context, transaction storesqlite.WriteTx, job JobRun) error {
	model := jobRunModelFromDomain(job)
	return transaction.WithContext(ctx).Model(&jobRunModel{}).Where("job_id = ?", job.JobID).Updates(map[string]any{
		"state": model.State, "phase": model.Phase, "started_at_ms": model.StartedAtMS,
		"finished_at_ms": model.FinishedAtMS, "progress_current": model.ProgressCurrent,
		"progress_total": model.ProgressTotal, "resume_generation": model.ResumeGeneration,
		"resume_offset": model.ResumeOffset, "error_class": model.ErrorClass,
		"updated_at_ms": model.UpdatedAtMS,
	}).Error
}

func jobCursorGeneration(cursor *JobCursor) any {
	if cursor == nil {
		return nil
	}
	return cursor.Generation
}

func jobCursorOffset(cursor *JobCursor) any {
	if cursor == nil {
		return nil
	}
	return cursor.Offset
}

func sourceFileModelFromDomain(file SourceFile) sourceFileModel {
	return sourceFileModel{
		SourceFileID: file.SourceFileID, Provider: file.Provider, SessionID: file.SessionID,
		CurrentPath: file.CurrentPath, DeviceID: file.DeviceID, Inode: file.Inode,
		SizeBytes: file.SizeBytes, MTimeNS: file.MTimeNS, ParsedOffset: file.ParsedOffset,
		ParserVersion: file.ParserVersion, ActiveGeneration: file.ActiveGeneration,
		State: string(file.State), LastScannedAtMS: file.LastScannedAtMS,
		LastErrorClass: runtimeErrorStringPointer(file.LastErrorClass), UpdatedAtMS: file.UpdatedAtMS,
	}
}

func sourceFileUpdates(model sourceFileModel) map[string]any {
	return map[string]any{
		"session_id": model.SessionID, "current_path": model.CurrentPath,
		"size_bytes": model.SizeBytes, "mtime_ns": model.MTimeNS,
		"parsed_offset": model.ParsedOffset, "parser_version": model.ParserVersion,
		"active_generation": model.ActiveGeneration, "state": model.State,
		"last_scanned_at_ms": model.LastScannedAtMS, "last_error_class": model.LastErrorClass,
		"updated_at_ms": model.UpdatedAtMS,
	}
}

func sourceStateModelFromDomain(state SourceState) sourceStateModel {
	return sourceStateModel{
		SourceInstanceID: state.SourceInstanceID, SourceType: state.SourceType, ScopeKey: state.ScopeKey,
		LastAttemptAtMS: state.LastAttemptAtMS, LastSuccessAtMS: state.LastSuccessAtMS,
		NextDueAtMS: state.NextDueAtMS, ConsecutiveFailures: state.ConsecutiveFailures,
		LastErrorClass:  runtimeErrorStringPointer(state.LastErrorClass),
		LastFailureCode: sourceFailureStringPointer(state.LastFailureCode),
		FreshnessState:  string(state.FreshnessState), CursorVersion: state.CursorVersion,
		UpdatedAtMS: state.UpdatedAtMS,
	}
}

func sourceStateUpdates(model sourceStateModel) map[string]any {
	return map[string]any{
		"last_attempt_at_ms": model.LastAttemptAtMS, "last_success_at_ms": model.LastSuccessAtMS,
		"next_due_at_ms": model.NextDueAtMS, "consecutive_failures": model.ConsecutiveFailures,
		"last_error_class": model.LastErrorClass, "last_failure_code": model.LastFailureCode,
		"freshness_state": model.FreshnessState,
		"cursor_version":  model.CursorVersion, "updated_at_ms": model.UpdatedAtMS,
	}
}

func sourceAttemptModelFromDomain(attempt SourceAttempt) sourceAttemptModel {
	return sourceAttemptModel{
		RequestID: attempt.RequestID, SourceInstanceID: attempt.SourceInstanceID,
		StartedAtMS: attempt.StartedAtMS, FinishedAtMS: attempt.FinishedAtMS,
		Outcome: string(attempt.Outcome), HTTPStatus: attempt.HTTPStatus,
		ErrorClass:    runtimeErrorStringPointer(attempt.ErrorClass),
		FailureCode:   sourceFailureStringPointer(attempt.FailureCode),
		PayloadSHA256: digestStringPointer(attempt.PayloadSHA256),
		AttemptCount:  attempt.AttemptCount, ResponseBytes: attempt.ResponseBytes,
		RetryAtMS: attempt.RetryAtMS,
	}
}

func jobRunModelFromDomain(job JobRun) jobRunModel {
	model := jobRunModel{
		JobID: job.JobID, JobType: job.JobType, RequestedBy: job.RequestedBy,
		Priority: job.Priority, State: string(job.State), Phase: string(job.Phase),
		SourceFileID: job.SourceFileID, ResumeOfJobID: job.ResumeOfJobID,
		ResumeConsumedByJobID: job.ResumeConsumedByJobID,
		CreatedAtMS:           job.CreatedAtMS, StartedAtMS: job.StartedAtMS, FinishedAtMS: job.FinishedAtMS,
		ProgressCurrent: job.ProgressCurrent, ProgressTotal: job.ProgressTotal,
		ErrorClass: runtimeErrorStringPointer(job.ErrorClass), UpdatedAtMS: job.UpdatedAtMS,
	}
	if job.ResumeCursor != nil {
		model.ResumeGeneration = &job.ResumeCursor.Generation
		model.ResumeOffset = &job.ResumeCursor.Offset
	}
	return model
}

func runtimeErrorStringPointer(value *RuntimeErrorClass) *string {
	if value == nil {
		return nil
	}
	converted := string(*value)
	return &converted
}

func sourceFailureStringPointer(value *SourceFailureCode) *string {
	if value == nil {
		return nil
	}
	converted := string(*value)
	return &converted
}

func digestStringPointer(value *SHA256Digest) *string {
	if value == nil {
		return nil
	}
	converted := value.String()
	return &converted
}
