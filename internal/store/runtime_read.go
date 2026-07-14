package store

import (
	"context"
	"database/sql"
	"errors"

	storesqlite "github.com/SisyphusSQ/codex-pulse/internal/store/sqlite"
)

type runtimeRowScanner interface {
	Scan(destinations ...any) error
}

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
		rows, err := connection.QueryContext(
			ctx, listSourceFilesBySessionStateQuery, sessionID, string(state), validatedLimit,
		)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			file, err := scanSourceFile(rows)
			if err != nil {
				return err
			}
			files = append(files, file)
		}
		return rows.Err()
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
		rows, err := connection.QueryContext(ctx, listDueSourcesQuery, nowMS, validatedLimit)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			state, err := scanSourceState(rows)
			if err != nil {
				return err
			}
			states = append(states, state)
		}
		return rows.Err()
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
		rows, err := connection.QueryContext(ctx, listSourceAttemptsQuery, sourceInstanceID, validatedLimit)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			attempt, err := scanSourceAttempt(rows)
			if err != nil {
				return err
			}
			attempts = append(attempts, attempt)
		}
		return rows.Err()
	})
	return attempts, err
}

func sourceFileByID(ctx context.Context, querier interface {
	QueryRowContext(context.Context, string, ...any) *sql.Row
}, sourceFileID string) (SourceFile, bool, error) {
	file, err := scanSourceFile(querier.QueryRowContext(ctx, `
		SELECT source_file_id, provider, session_id, current_path, device_id, inode,
			size_bytes, mtime_ns, parsed_offset, parser_version, active_generation,
			state, last_scanned_at_ms, last_error_class, updated_at_ms
		FROM source_files WHERE source_file_id = ?
	`, sourceFileID))
	if errors.Is(err, sql.ErrNoRows) {
		return SourceFile{}, false, nil
	}
	return file, err == nil, err
}

func sourceStateByID(ctx context.Context, querier interface {
	QueryRowContext(context.Context, string, ...any) *sql.Row
}, sourceInstanceID string) (SourceState, bool, error) {
	state, err := scanSourceState(querier.QueryRowContext(ctx, `
		SELECT source_instance_id, source_type, scope_key, last_attempt_at_ms,
			last_success_at_ms, next_due_at_ms, consecutive_failures,
			last_error_class, freshness_state, cursor_version, updated_at_ms
		FROM source_state WHERE source_instance_id = ?
	`, sourceInstanceID))
	if errors.Is(err, sql.ErrNoRows) {
		return SourceState{}, false, nil
	}
	return state, err == nil, err
}

func sourceAttemptByID(ctx context.Context, querier interface {
	QueryRowContext(context.Context, string, ...any) *sql.Row
}, requestID string) (SourceAttempt, bool, error) {
	attempt, err := scanSourceAttempt(querier.QueryRowContext(ctx, `
		SELECT request_id, source_instance_id, started_at_ms, finished_at_ms,
			outcome, http_status, error_class, payload_sha256
		FROM source_attempts WHERE request_id = ?
	`, requestID))
	if errors.Is(err, sql.ErrNoRows) {
		return SourceAttempt{}, false, nil
	}
	return attempt, err == nil, err
}

func scanSourceFile(scanner runtimeRowScanner) (SourceFile, error) {
	var file SourceFile
	var sessionID, errorClass sql.NullString
	var lastScannedAt sql.NullInt64
	var state string
	err := scanner.Scan(
		&file.SourceFileID, &file.Provider, &sessionID, &file.CurrentPath, &file.DeviceID,
		&file.Inode, &file.SizeBytes, &file.MTimeNS, &file.ParsedOffset, &file.ParserVersion,
		&file.ActiveGeneration, &state, &lastScannedAt, &errorClass, &file.UpdatedAtMS,
	)
	if err != nil {
		return SourceFile{}, err
	}
	file.SessionID = stringPointer(sessionID)
	file.State = SourceFileState(state)
	file.LastScannedAtMS = int64Pointer(lastScannedAt)
	file.LastErrorClass = runtimeErrorClassPointer(errorClass)
	return file, nil
}

func scanSourceState(scanner runtimeRowScanner) (SourceState, error) {
	var state SourceState
	var lastAttempt, lastSuccess, nextDue sql.NullInt64
	var errorClass sql.NullString
	var freshness string
	err := scanner.Scan(
		&state.SourceInstanceID, &state.SourceType, &state.ScopeKey,
		&lastAttempt, &lastSuccess, &nextDue, &state.ConsecutiveFailures,
		&errorClass, &freshness, &state.CursorVersion, &state.UpdatedAtMS,
	)
	if err != nil {
		return SourceState{}, err
	}
	state.LastAttemptAtMS = int64Pointer(lastAttempt)
	state.LastSuccessAtMS = int64Pointer(lastSuccess)
	state.NextDueAtMS = int64Pointer(nextDue)
	state.LastErrorClass = runtimeErrorClassPointer(errorClass)
	state.FreshnessState = SourceFreshness(freshness)
	return state, nil
}

func scanSourceAttempt(scanner runtimeRowScanner) (SourceAttempt, error) {
	var attempt SourceAttempt
	var outcome string
	var httpStatus sql.NullInt64
	var errorClass, payloadSHA256 sql.NullString
	err := scanner.Scan(
		&attempt.RequestID, &attempt.SourceInstanceID, &attempt.StartedAtMS,
		&attempt.FinishedAtMS, &outcome, &httpStatus, &errorClass, &payloadSHA256,
	)
	if err != nil {
		return SourceAttempt{}, err
	}
	attempt.Outcome = SourceAttemptOutcome(outcome)
	attempt.HTTPStatus = int64Pointer(httpStatus)
	attempt.ErrorClass = runtimeErrorClassPointer(errorClass)
	parsedPayloadSHA256, err := sha256DigestPointer(payloadSHA256)
	if err != nil {
		return SourceAttempt{}, err
	}
	attempt.PayloadSHA256 = parsedPayloadSHA256
	return attempt, nil
}

func runtimeErrorClassPointer(value sql.NullString) *RuntimeErrorClass {
	if !value.Valid {
		return nil
	}
	converted := RuntimeErrorClass(value.String)
	return &converted
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
		left.FreshnessState == right.FreshnessState && left.CursorVersion == right.CursorVersion &&
		left.UpdatedAtMS == right.UpdatedAtMS
}

func sourceAttemptsEqual(left, right SourceAttempt) bool {
	return left.RequestID == right.RequestID && left.SourceInstanceID == right.SourceInstanceID &&
		left.StartedAtMS == right.StartedAtMS && left.FinishedAtMS == right.FinishedAtMS &&
		left.Outcome == right.Outcome && equalInt64Pointer(left.HTTPStatus, right.HTTPStatus) &&
		equalRuntimeErrorClassPointer(left.ErrorClass, right.ErrorClass) &&
		equalSHA256DigestPointer(left.PayloadSHA256, right.PayloadSHA256)
}

func sha256DigestPointer(value sql.NullString) (*SHA256Digest, error) {
	if !value.Valid {
		return nil, nil
	}
	converted, ok := parseSHA256Digest(value.String)
	if !ok {
		return nil, invalidRecord("stored source payload SHA-256 is invalid")
	}
	return &converted, nil
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
	query, arguments := buildJobRunsQuery(filter, limit)

	var jobs []JobRun
	err = repository.database.View(ctx, func(ctx context.Context, connection storesqlite.ReadConn) error {
		rows, err := connection.QueryContext(ctx, query, arguments...)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			job, err := scanJobRun(rows)
			if err != nil {
				return err
			}
			jobs = append(jobs, job)
		}
		return rows.Err()
	})
	return jobs, err
}

func jobRunByID(ctx context.Context, querier interface {
	QueryRowContext(context.Context, string, ...any) *sql.Row
}, jobID string) (JobRun, bool, error) {
	job, err := scanJobRun(querier.QueryRowContext(ctx, `
		SELECT job_id, job_type, requested_by, priority, state, phase, source_file_id,
			resume_of_job_id, created_at_ms, started_at_ms, finished_at_ms,
			progress_current, progress_total, resume_generation, resume_offset,
			error_class, updated_at_ms
		FROM job_runs WHERE job_id = ?
	`, jobID))
	if errors.Is(err, sql.ErrNoRows) {
		return JobRun{}, false, nil
	}
	return job, err == nil, err
}

func scanJobRun(scanner runtimeRowScanner) (JobRun, error) {
	var job JobRun
	var state, phase string
	var sourceFileID, resumeOfJobID, errorClass sql.NullString
	var startedAt, finishedAt, progressCurrent, progressTotal, resumeGeneration, resumeOffset sql.NullInt64
	err := scanner.Scan(
		&job.JobID, &job.JobType, &job.RequestedBy, &job.Priority, &state, &phase,
		&sourceFileID, &resumeOfJobID, &job.CreatedAtMS, &startedAt, &finishedAt,
		&progressCurrent, &progressTotal, &resumeGeneration, &resumeOffset, &errorClass, &job.UpdatedAtMS,
	)
	if err != nil {
		return JobRun{}, err
	}
	job.State = JobState(state)
	job.Phase = JobPhase(phase)
	job.SourceFileID = stringPointer(sourceFileID)
	job.ResumeOfJobID = stringPointer(resumeOfJobID)
	job.StartedAtMS = int64Pointer(startedAt)
	job.FinishedAtMS = int64Pointer(finishedAt)
	job.ProgressCurrent = int64Pointer(progressCurrent)
	job.ProgressTotal = int64Pointer(progressTotal)
	if resumeGeneration.Valid != resumeOffset.Valid {
		return JobRun{}, invalidRecord("stored job cursor columns are inconsistent")
	}
	if resumeGeneration.Valid {
		job.ResumeCursor = &JobCursor{Generation: resumeGeneration.Int64, Offset: resumeOffset.Int64}
	}
	job.ErrorClass = runtimeErrorClassPointer(errorClass)
	return job, nil
}

func jobRunsEqual(left, right JobRun) bool {
	return left.JobID == right.JobID && left.JobType == right.JobType && left.RequestedBy == right.RequestedBy &&
		left.Priority == right.Priority && left.State == right.State && left.Phase == right.Phase &&
		equalStringPointer(left.SourceFileID, right.SourceFileID) &&
		equalStringPointer(left.ResumeOfJobID, right.ResumeOfJobID) && left.CreatedAtMS == right.CreatedAtMS &&
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

// HealthEvent 返回 fingerprint 聚合后的完整生命周期。
