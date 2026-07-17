package store

import (
	"context"
	"errors"
	"fmt"
	"time"

	storesqlite "github.com/SisyphusSQ/codex-pulse/internal/store/sqlite"
	"gorm.io/gorm"
)

const (
	// RetentionWindow is the fixed v0.1 rolling window for disposable runtime rows.
	RetentionWindow = 24 * time.Hour

	DefaultRetentionBatchSize = 100
	MaxRetentionBatchSize     = 1000
)

var ErrInvalidRetentionOptions = errors.New("invalid retention cleanup options")

// RetentionCleanupOptions controls operational batching without making the
// product retention window configurable. A zero Now uses the current time.
type RetentionCleanupOptions struct {
	Now       time.Time
	BatchSize int
	Observe   func(RetentionCleanupProgress)
}

// RetentionDeletedCounts reports committed deletions by runtime table.
type RetentionDeletedCounts struct {
	RuntimeSamples int64
	HealthEvents   int64
	JobRuns        int64
	SourceAttempts int64
}

// Total returns the number of committed rows represented by the counts.
func (counts RetentionDeletedCounts) Total() int64 {
	return counts.RuntimeSamples + counts.HealthEvents + counts.JobRuns + counts.SourceAttempts
}

func (counts *RetentionDeletedCounts) add(other RetentionDeletedCounts) {
	counts.RuntimeSamples += other.RuntimeSamples
	counts.HealthEvents += other.HealthEvents
	counts.JobRuns += other.JobRuns
	counts.SourceAttempts += other.SourceAttempts
}

// RetentionCleanupBatchReport describes one independently committed batch.
type RetentionCleanupBatchReport struct {
	CutoffMS int64
	Deleted  RetentionDeletedCounts
	More     bool
}

// RetentionCleanupReport aggregates all batches committed by one cleanup run.
type RetentionCleanupReport struct {
	CutoffMS int64
	Batches  int
	Deleted  RetentionDeletedCounts
}

// RetentionCleanupProgress is emitted after a non-empty batch commits.
type RetentionCleanupProgress struct {
	Batch    int
	CutoffMS int64
	Deleted  RetentionDeletedCounts
	Total    RetentionDeletedCounts
	More     bool
}

// CleanupRetention repeatedly commits bounded low-priority batches. Cancellation
// between batches preserves the report for work already committed; a later call
// safely recomputes candidates from the fixed cutoff predicates.
func (repository *Repository) CleanupRetention(
	ctx context.Context,
	options RetentionCleanupOptions,
) (RetentionCleanupReport, error) {
	normalized, cutoffMS, err := normalizeRetentionOptions(options)
	if err != nil {
		return RetentionCleanupReport{}, err
	}
	report := RetentionCleanupReport{CutoffMS: cutoffMS}
	if repository == nil || repository.database == nil {
		return report, ErrInvalidRepository
	}

	for {
		batch, err := repository.cleanupRetentionBatch(ctx, normalized, cutoffMS)
		if err != nil {
			return report, err
		}
		if batch.Deleted.Total() == 0 {
			return report, nil
		}
		report.Batches++
		report.Deleted.add(batch.Deleted)
		if normalized.Observe != nil {
			normalized.Observe(RetentionCleanupProgress{
				Batch: report.Batches, CutoffMS: cutoffMS, Deleted: batch.Deleted,
				Total: report.Deleted, More: batch.More,
			})
		}
		if !batch.More {
			return report, nil
		}
	}
}

// CleanupRetentionBatch deletes at most BatchSize eligible rows in one
// low-priority transaction. It never exposes counts from a rolled-back batch.
func (repository *Repository) CleanupRetentionBatch(
	ctx context.Context,
	options RetentionCleanupOptions,
) (RetentionCleanupBatchReport, error) {
	normalized, cutoffMS, err := normalizeRetentionOptions(options)
	if err != nil {
		return RetentionCleanupBatchReport{}, err
	}
	if repository == nil || repository.database == nil {
		return RetentionCleanupBatchReport{CutoffMS: cutoffMS}, ErrInvalidRepository
	}
	report, err := repository.cleanupRetentionBatch(ctx, normalized, cutoffMS)
	if err == nil && report.Deleted.Total() > 0 && normalized.Observe != nil {
		normalized.Observe(RetentionCleanupProgress{
			Batch: 1, CutoffMS: cutoffMS, Deleted: report.Deleted,
			Total: report.Deleted, More: report.More,
		})
	}
	return report, err
}

func (repository *Repository) cleanupRetentionBatch(
	ctx context.Context,
	options RetentionCleanupOptions,
	cutoffMS int64,
) (RetentionCleanupBatchReport, error) {
	report := RetentionCleanupBatchReport{CutoffMS: cutoffMS}
	var candidate RetentionDeletedCounts
	var more bool
	err := repository.database.WriteMaintenance(ctx, func(ctx context.Context, transaction storesqlite.WriteTx) error {
		remaining := options.BatchSize
		var err error
		candidate.RuntimeSamples, err = deleteExpiredRuntimeSamples(ctx, transaction, cutoffMS, remaining)
		if err != nil {
			return err
		}
		remaining -= int(candidate.RuntimeSamples)

		candidate.HealthEvents, err = deleteExpiredHealthEvents(ctx, transaction, cutoffMS, remaining)
		if err != nil {
			return err
		}
		remaining -= int(candidate.HealthEvents)

		candidate.JobRuns, err = deleteExpiredJobRuns(ctx, transaction, cutoffMS, remaining)
		if err != nil {
			return err
		}
		remaining -= int(candidate.JobRuns)

		candidate.SourceAttempts, err = deleteExpiredSourceAttempts(ctx, transaction, cutoffMS, remaining)
		if err != nil {
			return err
		}
		remaining -= int(candidate.SourceAttempts)

		// Deleting a resume leaf can make its terminal parent eligible inside
		// the same transaction even when this batch still has capacity.
		if candidate.Total() > 0 {
			more, err = hasExpiredRuntimeRows(ctx, transaction, cutoffMS)
			if err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		return report, err
	}
	report.Deleted = candidate
	report.More = more
	return report, nil
}

func deleteExpiredRuntimeSamples(
	ctx context.Context,
	transaction *gorm.DB,
	cutoffMS int64,
	limit int,
) (int64, error) {
	if limit == 0 {
		return 0, nil
	}
	var timestamps []int64
	err := expiredRuntimeSamples(transaction.WithContext(ctx), cutoffMS).
		Order("captured_at_ms").Limit(limit).Pluck("captured_at_ms", &timestamps).Error
	if err != nil || len(timestamps) == 0 {
		return 0, err
	}
	result := transaction.WithContext(ctx).Where("captured_at_ms IN ?", timestamps).Delete(&appRuntimeSampleModel{})
	return requireDeletedRows("runtime samples", result, len(timestamps))
}

func normalizeRetentionOptions(options RetentionCleanupOptions) (RetentionCleanupOptions, int64, error) {
	if options.Now.IsZero() {
		options.Now = time.Now().UTC()
	}
	if options.Now.UnixMilli() < 0 {
		return RetentionCleanupOptions{}, 0, fmt.Errorf("%w: now must not precede the Unix epoch", ErrInvalidRetentionOptions)
	}
	if options.BatchSize == 0 {
		options.BatchSize = DefaultRetentionBatchSize
	}
	if options.BatchSize < 1 || options.BatchSize > MaxRetentionBatchSize {
		return RetentionCleanupOptions{}, 0, fmt.Errorf(
			"%w: batch size must be between 1 and %d", ErrInvalidRetentionOptions, MaxRetentionBatchSize,
		)
	}
	return options, options.Now.Add(-RetentionWindow).UnixMilli(), nil
}

func deleteExpiredHealthEvents(
	ctx context.Context,
	transaction *gorm.DB,
	cutoffMS int64,
	limit int,
) (int64, error) {
	if limit == 0 {
		return 0, nil
	}
	var eventIDs []string
	err := expiredHealthEvents(transaction.WithContext(ctx), cutoffMS).
		Order("resolved_at_ms, event_id").Limit(limit).Pluck("event_id", &eventIDs).Error
	if err != nil || len(eventIDs) == 0 {
		return 0, err
	}
	result := transaction.WithContext(ctx).Where("event_id IN ?", eventIDs).Delete(&healthEventModel{})
	return requireDeletedRows("health events", result, len(eventIDs))
}

func deleteExpiredJobRuns(
	ctx context.Context,
	transaction *gorm.DB,
	cutoffMS int64,
	limit int,
) (int64, error) {
	if limit == 0 {
		return 0, nil
	}
	jobIDs := make([]string, 0, limit)
	for _, state := range []JobState{JobSucceeded, JobFailed, JobCancelled} {
		if len(jobIDs) == limit {
			break
		}
		var stateJobIDs []string
		err := expiredJobRunsForState(transaction.WithContext(ctx), cutoffMS, state).
			Order("finished_at_ms, job_id").Limit(limit-len(jobIDs)).Pluck("job_id", &stateJobIDs).Error
		if err != nil {
			return 0, err
		}
		jobIDs = append(jobIDs, stateJobIDs...)
	}
	if len(jobIDs) == 0 {
		return 0, nil
	}
	result := transaction.WithContext(ctx).Where("job_id IN ?", jobIDs).Delete(&jobRunModel{})
	return requireDeletedRows("job runs", result, len(jobIDs))
}

func deleteExpiredSourceAttempts(
	ctx context.Context,
	transaction *gorm.DB,
	cutoffMS int64,
	limit int,
) (int64, error) {
	if limit == 0 {
		return 0, nil
	}
	var requestIDs []string
	err := expiredSourceAttempts(transaction.WithContext(ctx), cutoffMS).
		Order("finished_at_ms, request_id").Limit(limit).Pluck("request_id", &requestIDs).Error
	if err != nil || len(requestIDs) == 0 {
		return 0, err
	}
	result := transaction.WithContext(ctx).Where("request_id IN ?", requestIDs).Delete(&sourceAttemptModel{})
	return requireDeletedRows("source attempts", result, len(requestIDs))
}

func expiredHealthEvents(database *gorm.DB, cutoffMS int64) *gorm.DB {
	return database.Model(&healthEventModel{}).
		Where("resolved_at_ms IS NOT NULL AND resolved_at_ms < ?", cutoffMS)
}

func expiredRuntimeSamples(database *gorm.DB, cutoffMS int64) *gorm.DB {
	return database.Model(&appRuntimeSampleModel{}).Where("captured_at_ms < ?", cutoffMS)
}

func expiredJobRuns(database *gorm.DB, cutoffMS int64) *gorm.DB {
	return expiredJobRunsBase(database, cutoffMS).
		Where("state IN ?", []string{string(JobSucceeded), string(JobFailed), string(JobCancelled)})
}

func expiredJobRunsForState(database *gorm.DB, cutoffMS int64, state JobState) *gorm.DB {
	return expiredJobRunsBase(database, cutoffMS).Where("state = ?", string(state))
}

func expiredJobRunsBase(database *gorm.DB, cutoffMS int64) *gorm.DB {
	healthReference := database.Session(&gorm.Session{NewDB: true}).Model(&healthEventModel{}).
		Select("1").Where("health_events.job_id = job_runs.job_id")
	resumeReference := database.Session(&gorm.Session{NewDB: true}).Table("job_runs AS resumed_jobs").
		Select("1").Where("resumed_jobs.resume_of_job_id = job_runs.job_id")
	return database.Model(&jobRunModel{}).
		Where("finished_at_ms IS NOT NULL AND finished_at_ms < ?", cutoffMS).
		Where("NOT EXISTS (?)", healthReference).
		Where("NOT EXISTS (?)", resumeReference)
}

func expiredSourceAttempts(database *gorm.DB, cutoffMS int64) *gorm.DB {
	return database.Model(&sourceAttemptModel{}).Where("finished_at_ms < ?", cutoffMS)
}

func hasExpiredRuntimeRows(ctx context.Context, transaction *gorm.DB, cutoffMS int64) (bool, error) {
	queries := []struct {
		column string
		query  *gorm.DB
	}{
		{column: "captured_at_ms", query: expiredRuntimeSamples(transaction.WithContext(ctx), cutoffMS)},
		{column: "event_id", query: expiredHealthEvents(transaction.WithContext(ctx), cutoffMS)},
		{column: "job_id", query: expiredJobRuns(transaction.WithContext(ctx), cutoffMS)},
		{column: "request_id", query: expiredSourceAttempts(transaction.WithContext(ctx), cutoffMS)},
	}
	for _, query := range queries {
		var identifier string
		result := query.query.Limit(1).Pluck(query.column, &identifier)
		if result.Error != nil {
			return false, result.Error
		}
		if result.RowsAffected > 0 {
			return true, nil
		}
	}
	return false, nil
}

func requireDeletedRows(kind string, result *gorm.DB, selected int) (int64, error) {
	if result.Error != nil {
		return 0, result.Error
	}
	if result.RowsAffected != int64(selected) {
		return 0, fmt.Errorf("retention cleanup deleted %d of %d selected %s", result.RowsAffected, selected, kind)
	}
	return result.RowsAffected, nil
}
