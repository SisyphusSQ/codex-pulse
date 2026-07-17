package store

import (
	"context"
	"errors"

	"github.com/SisyphusSQ/codex-pulse/internal/runtimeclock"
	"gorm.io/gorm"

	storesqlite "github.com/SisyphusSQ/codex-pulse/internal/store/sqlite"
)

// SourceFile 返回当前 source file snapshot。
func (repository *Repository) SourceFile(ctx context.Context, sourceFileID string) (SourceFile, error) {
	if repository == nil || repository.database == nil {
		return SourceFile{}, ErrInvalidRepository
	}
	if sourceFileID == "" {
		return SourceFile{}, invalidRecord("source file ID must not be empty")
	}
	var file SourceFile
	err := repository.database.View(ctx, func(ctx context.Context, connection storesqlite.ReadConn) error {
		var found bool
		var err error
		file, found, err = sourceFileByID(ctx, connection, sourceFileID)
		if err == nil && !found {
			return ErrNotFound
		}
		return err
	})
	return file, err
}

// ListSourceFilesBySessionState 返回 session 下指定状态的来源文件，固定走 session/state 查询路径。
func (repository *Repository) ListSourceFilesBySessionState(
	ctx context.Context,
	sessionID string,
	state SourceFileState,
	limit int,
) ([]SourceFile, error) {
	if repository == nil || repository.database == nil {
		return nil, ErrInvalidRepository
	}
	if sessionID == "" || !validSourceFileState(state) {
		return nil, invalidRecord("source file session or state filter is invalid")
	}
	validatedLimit, err := validateRuntimeLimit(limit)
	if err != nil {
		return nil, err
	}
	var files []SourceFile
	err = repository.database.View(ctx, func(ctx context.Context, connection storesqlite.ReadConn) error {
		var models []sourceFileModel
		if err := connection.WithContext(ctx).
			Where("session_id = ? AND state = ?", sessionID, string(state)).
			Order("last_scanned_at_ms").Order("source_file_id").Limit(validatedLimit).
			Find(&models).Error; err != nil {
			return err
		}
		files = make([]SourceFile, 0, len(models))
		for _, model := range models {
			files = append(files, sourceFileFromModel(model))
		}
		return nil
	})
	return files, err
}

// SourceState 返回当前逻辑来源状态。
func (repository *Repository) SourceState(ctx context.Context, sourceInstanceID string) (SourceState, error) {
	if repository == nil || repository.database == nil {
		return SourceState{}, ErrInvalidRepository
	}
	if sourceInstanceID == "" {
		return SourceState{}, invalidRecord("source instance ID must not be empty")
	}
	var state SourceState
	err := repository.database.View(ctx, func(ctx context.Context, connection storesqlite.ReadConn) error {
		var found bool
		var err error
		state, found, err = sourceStateByID(ctx, connection, sourceInstanceID)
		if err == nil && !found {
			return ErrNotFound
		}
		return err
	})
	return state, err
}

// ListDueSources 返回到期的逻辑来源，按 due time 和 identity 稳定排序。
func (repository *Repository) ListDueSources(ctx context.Context, nowMS int64, limit int) ([]SourceState, error) {
	if repository == nil || repository.database == nil {
		return nil, ErrInvalidRepository
	}
	if nowMS < 0 {
		return nil, invalidRecord("due source timestamp must not be negative")
	}
	validatedLimit, err := validateRuntimeLimit(limit)
	if err != nil {
		return nil, err
	}
	var states []SourceState
	err = repository.database.View(ctx, func(ctx context.Context, connection storesqlite.ReadConn) error {
		var models []sourceStateModel
		if err := connection.WithContext(ctx).Where("next_due_at_ms <= ?", nowMS).
			Order("next_due_at_ms").Order("source_instance_id").Limit(validatedLimit).
			Find(&models).Error; err != nil {
			return err
		}
		states = make([]SourceState, 0, len(models))
		for _, model := range models {
			states = append(states, sourceStateFromModel(model))
		}
		return nil
	})
	return states, err
}

// ListSourceAttempts 返回指定来源的 append-only 尝试历史。
func (repository *Repository) ListSourceAttempts(ctx context.Context, sourceInstanceID string, limit int) ([]SourceAttempt, error) {
	if repository == nil || repository.database == nil {
		return nil, ErrInvalidRepository
	}
	if sourceInstanceID == "" {
		return nil, invalidRecord("source instance ID must not be empty")
	}
	validatedLimit, err := validateRuntimeLimit(limit)
	if err != nil {
		return nil, err
	}
	var attempts []SourceAttempt
	err = repository.database.View(ctx, func(ctx context.Context, connection storesqlite.ReadConn) error {
		var models []sourceAttemptModel
		if err := connection.WithContext(ctx).Where("source_instance_id = ?", sourceInstanceID).
			Order("started_at_ms DESC").Order("request_id DESC").Limit(validatedLimit).
			Find(&models).Error; err != nil {
			return err
		}
		attempts = make([]SourceAttempt, 0, len(models))
		for _, model := range models {
			attempt, err := sourceAttemptFromModel(model)
			if err != nil {
				return err
			}
			attempts = append(attempts, attempt)
		}
		return nil
	})
	return attempts, err
}

func sourceFileByID(ctx context.Context, querier *gorm.DB, sourceFileID string) (SourceFile, bool, error) {
	var model sourceFileModel
	err := querier.WithContext(ctx).Take(&model, "source_file_id = ?", sourceFileID).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return SourceFile{}, false, nil
	}
	return sourceFileFromModel(model), err == nil, err
}

func sourceStateByID(ctx context.Context, querier *gorm.DB, sourceInstanceID string) (SourceState, bool, error) {
	var model sourceStateModel
	err := querier.WithContext(ctx).Take(&model, "source_instance_id = ?", sourceInstanceID).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return SourceState{}, false, nil
	}
	return sourceStateFromModel(model), err == nil, err
}

func sourceAttemptByID(ctx context.Context, querier *gorm.DB, requestID string) (SourceAttempt, bool, error) {
	var model sourceAttemptModel
	err := querier.WithContext(ctx).Take(&model, "request_id = ?", requestID).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return SourceAttempt{}, false, nil
	}
	if err != nil {
		return SourceAttempt{}, false, err
	}
	attempt, err := sourceAttemptFromModel(model)
	return attempt, err == nil, err
}

// SourceAttempt returns one content-free durable attempt by request identity.
// Scheduler claim IDs intentionally equal request IDs, allowing crash recovery
// to distinguish an unrecorded request from a recorded result without replaying
// either credentials or response content.
func (repository *Repository) SourceAttempt(ctx context.Context, requestID string) (SourceAttempt, error) {
	if repository == nil || repository.database == nil || requestID == "" || len(requestID) > 512 {
		return SourceAttempt{}, ErrInvalidRepository
	}
	var attempt SourceAttempt
	err := repository.database.View(ctx, func(ctx context.Context, connection storesqlite.ReadConn) error {
		value, found, err := sourceAttemptByID(ctx, connection, requestID)
		if err != nil {
			return err
		}
		if !found {
			return ErrNotFound
		}
		attempt = value
		return nil
	})
	return attempt, err
}

func sourceFileFromModel(model sourceFileModel) SourceFile {
	return SourceFile{
		SourceFileID: model.SourceFileID, Provider: model.Provider, SessionID: model.SessionID,
		CurrentPath: model.CurrentPath, DeviceID: model.DeviceID, Inode: model.Inode,
		SizeBytes: model.SizeBytes, MTimeNS: model.MTimeNS, ParsedOffset: model.ParsedOffset,
		ParserVersion: model.ParserVersion, ActiveGeneration: model.ActiveGeneration,
		State: SourceFileState(model.State), LastScannedAtMS: model.LastScannedAtMS,
		LastErrorClass: runtimeErrorClassFromString(model.LastErrorClass), UpdatedAtMS: model.UpdatedAtMS,
	}
}

func sourceStateFromModel(model sourceStateModel) SourceState {
	return SourceState{
		SourceInstanceID: model.SourceInstanceID, SourceType: model.SourceType, ScopeKey: model.ScopeKey,
		LastAttemptAtMS: model.LastAttemptAtMS, LastSuccessAtMS: model.LastSuccessAtMS,
		NextDueAtMS: model.NextDueAtMS, ConsecutiveFailures: model.ConsecutiveFailures,
		LastErrorClass:  runtimeErrorClassFromString(model.LastErrorClass),
		LastFailureCode: sourceFailureCodeFromString(model.LastFailureCode),
		FreshnessState:  SourceFreshness(model.FreshnessState), CursorVersion: model.CursorVersion,
		UpdatedAtMS: model.UpdatedAtMS,
	}
}

func sourceAttemptFromModel(model sourceAttemptModel) (SourceAttempt, error) {
	digest, err := sha256DigestFromString(model.PayloadSHA256)
	if err != nil {
		return SourceAttempt{}, err
	}
	return SourceAttempt{
		RequestID: model.RequestID, SourceInstanceID: model.SourceInstanceID,
		StartedAtMS: model.StartedAtMS, FinishedAtMS: model.FinishedAtMS,
		Outcome: SourceAttemptOutcome(model.Outcome), HTTPStatus: model.HTTPStatus,
		ErrorClass:  runtimeErrorClassFromString(model.ErrorClass),
		FailureCode: sourceFailureCodeFromString(model.FailureCode), PayloadSHA256: digest,
		AttemptCount: model.AttemptCount, ResponseBytes: model.ResponseBytes, RetryAtMS: model.RetryAtMS,
	}, nil
}

func runtimeErrorClassFromString(value *string) *RuntimeErrorClass {
	if value == nil {
		return nil
	}
	converted := RuntimeErrorClass(*value)
	return &converted
}

func sourceFailureCodeFromString(value *string) *SourceFailureCode {
	if value == nil {
		return nil
	}
	converted := SourceFailureCode(*value)
	return &converted
}

func sha256DigestFromString(value *string) (*SHA256Digest, error) {
	if value == nil {
		return nil, nil
	}
	converted, ok := parseSHA256Digest(*value)
	if !ok {
		return nil, invalidRecord("stored source payload SHA-256 is invalid")
	}
	return &converted, nil
}

func sourceFilesEqual(left, right SourceFile) bool {
	return left.SourceFileID == right.SourceFileID && left.Provider == right.Provider &&
		equalStringPointer(left.SessionID, right.SessionID) && left.CurrentPath == right.CurrentPath &&
		left.DeviceID == right.DeviceID && left.Inode == right.Inode && left.SizeBytes == right.SizeBytes &&
		left.MTimeNS == right.MTimeNS && left.ParsedOffset == right.ParsedOffset &&
		left.ParserVersion == right.ParserVersion && left.ActiveGeneration == right.ActiveGeneration &&
		left.State == right.State && equalInt64Pointer(left.LastScannedAtMS, right.LastScannedAtMS) &&
		equalRuntimeErrorClassPointer(left.LastErrorClass, right.LastErrorClass) && left.UpdatedAtMS == right.UpdatedAtMS
}

func sourceStatesEqual(left, right SourceState) bool {
	return left.SourceInstanceID == right.SourceInstanceID && left.SourceType == right.SourceType &&
		left.ScopeKey == right.ScopeKey && equalInt64Pointer(left.LastAttemptAtMS, right.LastAttemptAtMS) &&
		equalInt64Pointer(left.LastSuccessAtMS, right.LastSuccessAtMS) &&
		equalInt64Pointer(left.NextDueAtMS, right.NextDueAtMS) &&
		left.ConsecutiveFailures == right.ConsecutiveFailures &&
		equalRuntimeErrorClassPointer(left.LastErrorClass, right.LastErrorClass) &&
		equalSourceFailureCodePointer(left.LastFailureCode, right.LastFailureCode) &&
		left.FreshnessState == right.FreshnessState && left.CursorVersion == right.CursorVersion &&
		left.UpdatedAtMS == right.UpdatedAtMS
}

func sourceAttemptsEqual(left, right SourceAttempt) bool {
	return left.RequestID == right.RequestID && left.SourceInstanceID == right.SourceInstanceID &&
		left.StartedAtMS == right.StartedAtMS && left.FinishedAtMS == right.FinishedAtMS &&
		left.Outcome == right.Outcome && equalInt64Pointer(left.HTTPStatus, right.HTTPStatus) &&
		equalRuntimeErrorClassPointer(left.ErrorClass, right.ErrorClass) &&
		equalSourceFailureCodePointer(left.FailureCode, right.FailureCode) &&
		equalSHA256DigestPointer(left.PayloadSHA256, right.PayloadSHA256) &&
		left.AttemptCount == right.AttemptCount && left.ResponseBytes == right.ResponseBytes &&
		equalInt64Pointer(left.RetryAtMS, right.RetryAtMS)
}

func equalSHA256DigestPointer(left, right *SHA256Digest) bool {
	if left == nil || right == nil {
		return left == nil && right == nil
	}
	return *left == *right
}

func equalRuntimeErrorClassPointer(left, right *RuntimeErrorClass) bool {
	if left == nil || right == nil {
		return left == nil && right == nil
	}
	return *left == *right
}

func equalSourceFailureCodePointer(left, right *SourceFailureCode) bool {
	if left == nil || right == nil {
		return left == nil && right == nil
	}
	return *left == *right
}

// JobRun 返回一条完整作业生命周期事实。
func (repository *Repository) JobRun(ctx context.Context, jobID string) (JobRun, error) {
	if repository == nil || repository.database == nil {
		return JobRun{}, ErrInvalidRepository
	}
	if jobID == "" {
		return JobRun{}, invalidRecord("job ID must not be empty")
	}
	var job JobRun
	err := repository.database.View(ctx, func(ctx context.Context, connection storesqlite.ReadConn) error {
		var found bool
		var err error
		job, found, err = jobRunByID(ctx, connection, jobID)
		if err == nil && !found {
			return ErrNotFound
		}
		return err
	})
	return job, err
}

// ListJobRuns 使用固定 filter 和稳定队列顺序返回作业历史。
func (repository *Repository) ListJobRuns(ctx context.Context, filter JobRunFilter) ([]JobRun, error) {
	if repository == nil || repository.database == nil {
		return nil, ErrInvalidRepository
	}
	limit, err := validateRuntimeLimit(filter.Limit)
	if err != nil {
		return nil, err
	}
	if filter.State != nil && !validJobState(*filter.State) {
		return nil, invalidRecord("job filter state is invalid")
	}
	if filter.SourceFileID != nil && *filter.SourceFileID == "" {
		return nil, invalidRecord("job filter source file ID must not be empty")
	}

	var jobs []JobRun
	err = repository.database.View(ctx, func(ctx context.Context, connection storesqlite.ReadConn) error {
		query := connection.WithContext(ctx).Model(&jobRunModel{})
		if filter.State != nil {
			query = query.Where("state = ?", string(*filter.State))
		}
		if filter.SourceFileID != nil {
			query = query.Where("source_file_id = ?", *filter.SourceFileID)
		}
		if filter.State == nil && filter.SourceFileID != nil {
			query = query.Order("created_at_ms DESC").Order("job_id DESC")
		} else {
			query = query.Order("updated_at_ms").Order("priority DESC").Order("job_id")
		}
		var models []jobRunModel
		if err := query.Limit(limit).Find(&models).Error; err != nil {
			return err
		}
		jobs = make([]JobRun, 0, len(models))
		for _, model := range models {
			job, err := jobRunFromModel(model)
			if err != nil {
				return err
			}
			jobs = append(jobs, job)
		}
		return nil
	})
	return jobs, err
}

func jobRunByID(ctx context.Context, querier *gorm.DB, jobID string) (JobRun, bool, error) {
	var model jobRunModel
	err := querier.WithContext(ctx).Take(&model, "job_id = ?", jobID).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return JobRun{}, false, nil
	}
	if err != nil {
		return JobRun{}, false, err
	}
	job, err := jobRunFromModel(model)
	return job, err == nil, err
}

func jobRunFromModel(model jobRunModel) (JobRun, error) {
	job := JobRun{
		JobID: model.JobID, JobType: model.JobType, RequestedBy: model.RequestedBy,
		Priority: model.Priority, State: JobState(model.State), Phase: JobPhase(model.Phase),
		SourceFileID: model.SourceFileID, ResumeOfJobID: model.ResumeOfJobID,
		ResumeConsumedByJobID: model.ResumeConsumedByJobID,
		CreatedAtMS:           model.CreatedAtMS, StartedAtMS: model.StartedAtMS, FinishedAtMS: model.FinishedAtMS,
		ProgressCurrent: model.ProgressCurrent, ProgressTotal: model.ProgressTotal,
		ErrorClass: runtimeErrorClassFromString(model.ErrorClass), UpdatedAtMS: model.UpdatedAtMS,
	}
	if (model.ResumeGeneration == nil) != (model.ResumeOffset == nil) {
		return JobRun{}, invalidRecord("stored job cursor columns are inconsistent")
	}
	if model.ResumeGeneration != nil {
		job.ResumeCursor = &JobCursor{Generation: *model.ResumeGeneration, Offset: *model.ResumeOffset}
	}
	if job.CreatedAtMS < 0 || job.UpdatedAtMS < job.CreatedAtMS ||
		job.CreatedAtMS > runtimeclock.MaxTimestampMS || job.UpdatedAtMS > runtimeclock.MaxTimestampMS ||
		timestampPointerOutsideJobHistory(job.StartedAtMS, job.CreatedAtMS, job.UpdatedAtMS) ||
		timestampPointerOutsideJobHistory(job.FinishedAtMS, job.CreatedAtMS, job.UpdatedAtMS) {
		return JobRun{}, invalidRecord("stored job timestamps are outside the runtime boundary")
	}
	if job.State == JobQueued && job.UpdatedAtMS > runtimeclock.MaxContinuableTimestampMS ||
		job.State == JobRunning && job.UpdatedAtMS > runtimeclock.MaxInProgressTimestampMS {
		return JobRun{}, invalidRecord("stored job state has no logical-time headroom")
	}
	if job.ResumeConsumedByJobID != nil &&
		(job.State != JobInterrupted || *job.ResumeConsumedByJobID == "" ||
			*job.ResumeConsumedByJobID == job.JobID || job.FinishedAtMS == nil) {
		return JobRun{}, invalidRecord("stored job resume-consumed marker is invalid")
	}
	return job, nil
}

func timestampPointerOutsideJobHistory(value *int64, createdAtMS int64, updatedAtMS int64) bool {
	return value != nil && (*value < createdAtMS || *value > updatedAtMS || *value > runtimeclock.MaxTimestampMS)
}

func jobRunsEqual(left, right JobRun) bool {
	return left.JobID == right.JobID && left.JobType == right.JobType && left.RequestedBy == right.RequestedBy &&
		left.Priority == right.Priority && left.State == right.State && left.Phase == right.Phase &&
		equalStringPointer(left.SourceFileID, right.SourceFileID) &&
		equalStringPointer(left.ResumeOfJobID, right.ResumeOfJobID) &&
		equalStringPointer(left.ResumeConsumedByJobID, right.ResumeConsumedByJobID) &&
		left.CreatedAtMS == right.CreatedAtMS &&
		equalInt64Pointer(left.StartedAtMS, right.StartedAtMS) &&
		equalInt64Pointer(left.FinishedAtMS, right.FinishedAtMS) &&
		equalInt64Pointer(left.ProgressCurrent, right.ProgressCurrent) &&
		equalInt64Pointer(left.ProgressTotal, right.ProgressTotal) &&
		equalJobCursorPointer(left.ResumeCursor, right.ResumeCursor) &&
		equalRuntimeErrorClassPointer(left.ErrorClass, right.ErrorClass) && left.UpdatedAtMS == right.UpdatedAtMS
}

func equalJobCursorPointer(left, right *JobCursor) bool {
	if left == nil || right == nil {
		return left == nil && right == nil
	}
	return *left == *right
}
