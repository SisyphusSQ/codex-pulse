package store

import (
	"context"
	"database/sql"
	"errors"

	storesqlite "github.com/SisyphusSQ/codex-pulse/internal/store/sqlite"
)

// UpsertSourceFile 只允许稳定物理身份下 generation/offset 单调推进。
func (repository *Repository) UpsertSourceFile(ctx context.Context, file SourceFile) error {
	if repository == nil || repository.database == nil {
		return ErrInvalidRepository
	}
	if err := validateSourceFile(file); err != nil {
		return err
	}
	return repository.database.Write(ctx, func(ctx context.Context, transaction storesqlite.WriteTx) error {
		if file.SessionID != nil {
			if err := requireSession(ctx, transaction, *file.SessionID); err != nil {
				return err
			}
		}
		existing, found, err := sourceFileByID(ctx, transaction, file.SourceFileID)
		if err != nil {
			return err
		}
		if found {
			if err := validateSourceFileProgression(existing, file); err != nil {
				return err
			}
			if sourceFilesEqual(existing, file) {
				return nil
			}
		} else {
			var otherID string
			err := transaction.QueryRowContext(ctx, `
				SELECT source_file_id FROM source_files
				WHERE provider = ? AND device_id = ? AND inode = ?
			`, file.Provider, file.DeviceID, file.Inode).Scan(&otherID)
			if err == nil {
				return invalidRecord("source physical identity belongs to another source file")
			}
			if !errors.Is(err, sql.ErrNoRows) {
				return err
			}
		}
		_, err = transaction.ExecContext(ctx, `
			INSERT INTO source_files (
				source_file_id, provider, session_id, current_path, device_id, inode,
				size_bytes, mtime_ns, parsed_offset, parser_version, active_generation,
				state, last_scanned_at_ms, last_error_class, updated_at_ms
			) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
			ON CONFLICT(source_file_id) DO UPDATE SET
				session_id = excluded.session_id,
				current_path = excluded.current_path,
				size_bytes = excluded.size_bytes,
				mtime_ns = excluded.mtime_ns,
				parsed_offset = excluded.parsed_offset,
				parser_version = excluded.parser_version,
				active_generation = excluded.active_generation,
				state = excluded.state,
				last_scanned_at_ms = excluded.last_scanned_at_ms,
				last_error_class = excluded.last_error_class,
				updated_at_ms = excluded.updated_at_ms
		`,
			file.SourceFileID, file.Provider, nullableString(file.SessionID), file.CurrentPath,
			file.DeviceID, file.Inode, file.SizeBytes, file.MTimeNS, file.ParsedOffset,
			file.ParserVersion, file.ActiveGeneration, string(file.State),
			nullableInt64(file.LastScannedAtMS), nullableRuntimeErrorClass(file.LastErrorClass), file.UpdatedAtMS,
		)
		return err
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
			var otherID string
			err := transaction.QueryRowContext(ctx, `
				SELECT source_instance_id FROM source_state WHERE source_type = ? AND scope_key = ?
			`, state.SourceType, state.ScopeKey).Scan(&otherID)
			if err == nil {
				return invalidRecord("source state scope belongs to another identity")
			}
			if !errors.Is(err, sql.ErrNoRows) {
				return err
			}
		}
		_, err = transaction.ExecContext(ctx, `
			INSERT INTO source_state (
				source_instance_id, source_type, scope_key, last_attempt_at_ms,
				last_success_at_ms, next_due_at_ms, consecutive_failures,
				last_error_class, freshness_state, cursor_version, updated_at_ms
			) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
			ON CONFLICT(source_instance_id) DO UPDATE SET
				last_attempt_at_ms = excluded.last_attempt_at_ms,
				last_success_at_ms = excluded.last_success_at_ms,
				next_due_at_ms = excluded.next_due_at_ms,
				consecutive_failures = excluded.consecutive_failures,
				last_error_class = excluded.last_error_class,
				freshness_state = excluded.freshness_state,
				cursor_version = excluded.cursor_version,
				updated_at_ms = excluded.updated_at_ms
		`,
			state.SourceInstanceID, state.SourceType, state.ScopeKey,
			nullableInt64(state.LastAttemptAtMS), nullableInt64(state.LastSuccessAtMS),
			nullableInt64(state.NextDueAtMS), state.ConsecutiveFailures,
			nullableRuntimeErrorClass(state.LastErrorClass), string(state.FreshnessState),
			state.CursorVersion, state.UpdatedAtMS,
		)
		return err
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
			ctx, transaction, `SELECT 1 FROM source_state WHERE source_instance_id = ?`,
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
		_, err = transaction.ExecContext(ctx, `
			INSERT INTO source_attempts (
				request_id, source_instance_id, started_at_ms, finished_at_ms,
				outcome, http_status, error_class, payload_sha256
			) VALUES (?, ?, ?, ?, ?, ?, ?, ?)
		`,
			attempt.RequestID, attempt.SourceInstanceID, attempt.StartedAtMS, attempt.FinishedAtMS,
			string(attempt.Outcome), nullableInt64(attempt.HTTPStatus),
			nullableRuntimeErrorClass(attempt.ErrorClass), nullableSHA256Digest(attempt.PayloadSHA256),
		)
		return err
	})
}

func validateSourceFileProgression(existing, incoming SourceFile) error {
	if existing.Provider != incoming.Provider || existing.DeviceID != incoming.DeviceID || existing.Inode != incoming.Inode {
		return invalidRecord("source file stable identity conflicts")
	}
	if existing.SessionID != nil && incoming.SessionID != nil && *existing.SessionID != *incoming.SessionID {
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
	})
}

// InterruptIncompleteJobs 原子终结启动时遗留的 queued/running jobs。
func (repository *Repository) InterruptIncompleteJobs(ctx context.Context, atMS int64) (int64, error) {
	if repository == nil || repository.database == nil {
		return 0, ErrInvalidRepository
	}
	if atMS < 0 {
		return 0, invalidRecord("job interruption timestamp must not be negative")
	}
	var interrupted int64
	err := repository.database.Write(ctx, func(ctx context.Context, transaction storesqlite.WriteTx) error {
		var newer int
		if err := transaction.QueryRowContext(ctx, `
			SELECT COUNT(*) FROM job_runs
			WHERE state IN ('queued', 'running') AND updated_at_ms > ?
		`, atMS).Scan(&newer); err != nil {
			return err
		}
		if newer > 0 {
			return invalidRecord("job interruption timestamp precedes an incomplete job update")
		}
		result, err := transaction.ExecContext(ctx, `
			UPDATE job_runs
			SET state = 'interrupted', finished_at_ms = ?, error_class = NULL, updated_at_ms = ?
			WHERE state IN ('queued', 'running')
		`, atMS, atMS)
		if err != nil {
			return err
		}
		interrupted, err = result.RowsAffected()
		return err
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
		return createJobRun(ctx, transaction, resumed)
	})
}

func createJobRun(ctx context.Context, transaction storesqlite.WriteTx, job JobRun) error {
	if job.SourceFileID != nil {
		if err := requireStoredReference(
			ctx, transaction, `SELECT 1 FROM source_files WHERE source_file_id = ?`, *job.SourceFileID, "source file",
		); err != nil {
			return err
		}
	}
	if job.ResumeOfJobID != nil {
		if err := requireStoredReference(
			ctx, transaction, `SELECT 1 FROM job_runs WHERE job_id = ?`, *job.ResumeOfJobID, "resume job",
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
	_, err = transaction.ExecContext(ctx, `
		INSERT INTO job_runs (
			job_id, job_type, requested_by, priority, state, phase, source_file_id,
			resume_of_job_id, created_at_ms, started_at_ms, finished_at_ms,
			progress_current, progress_total, resume_generation, resume_offset,
			error_class, updated_at_ms
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	`,
		job.JobID, job.JobType, job.RequestedBy, job.Priority, string(job.State), string(job.Phase),
		nullableString(job.SourceFileID), nullableString(job.ResumeOfJobID), job.CreatedAtMS,
		nullableInt64(job.StartedAtMS), nullableInt64(job.FinishedAtMS),
		nullableInt64(job.ProgressCurrent), nullableInt64(job.ProgressTotal), jobCursorGeneration(job.ResumeCursor),
		jobCursorOffset(job.ResumeCursor), nullableRuntimeErrorClass(job.ErrorClass), job.UpdatedAtMS,
	)
	return err
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
	_, err := transaction.ExecContext(ctx, `
		UPDATE job_runs SET
			state = ?, phase = ?, started_at_ms = ?, finished_at_ms = ?,
			progress_current = ?, progress_total = ?, resume_generation = ?, resume_offset = ?,
			error_class = ?, updated_at_ms = ?
		WHERE job_id = ?
	`,
		string(job.State), string(job.Phase), nullableInt64(job.StartedAtMS), nullableInt64(job.FinishedAtMS),
		nullableInt64(job.ProgressCurrent), nullableInt64(job.ProgressTotal), jobCursorGeneration(job.ResumeCursor),
		jobCursorOffset(job.ResumeCursor), nullableRuntimeErrorClass(job.ErrorClass), job.UpdatedAtMS, job.JobID,
	)
	return err
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
