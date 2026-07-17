package store

import (
	"context"
	"database/sql"

	"gorm.io/gorm"

	"github.com/SisyphusSQ/codex-pulse/internal/runtimeclock"
	storesqlite "github.com/SisyphusSQ/codex-pulse/internal/store/sqlite"
)

type schedulerMetricsRow struct {
	CycleCount               int64         `gorm:"column:cycle_count"`
	CompletedCycles          int64         `gorm:"column:completed_cycles"`
	YieldedCycles            int64         `gorm:"column:yielded_cycles"`
	FailedCycles             int64         `gorm:"column:failed_cycles"`
	InterruptedCycles        int64         `gorm:"column:interrupted_cycles"`
	FilesScanned             int64         `gorm:"column:files_scanned"`
	BytesRead                int64         `gorm:"column:bytes_read"`
	ActiveMS                 int64         `gorm:"column:active_ms"`
	MaxCycleActiveMS         int64         `gorm:"column:max_cycle_active_ms"`
	LastProgressAtMS         sql.NullInt64 `gorm:"column:last_progress_at_ms"`
	LastBackfillProgressAtMS sql.NullInt64 `gorm:"column:last_backfill_progress_at_ms"`
}

type activeJobMetricsRow struct {
	Queued      int64 `gorm:"column:queued"`
	Running     int64 `gorm:"column:running"`
	Interrupted int64 `gorm:"column:interrupted"`
}

type terminalJobMetricsRow struct {
	Succeeded       int64 `gorm:"column:succeeded"`
	Failed          int64 `gorm:"column:failed"`
	Cancelled       int64 `gorm:"column:cancelled"`
	DurationCount   int64 `gorm:"column:duration_count"`
	DurationTotalMS int64 `gorm:"column:duration_total_ms"`
	DurationMaxMS   int64 `gorm:"column:duration_max_ms"`
}

type sourceStateMetricsRow struct {
	Total                  int64         `gorm:"column:total"`
	Unknown                int64         `gorm:"column:unknown"`
	Current                int64         `gorm:"column:current"`
	Stale                  int64         `gorm:"column:stale"`
	Unavailable            int64         `gorm:"column:unavailable"`
	ConsecutiveFailures    int64         `gorm:"column:consecutive_failures"`
	MaxConsecutiveFailures int64         `gorm:"column:max_consecutive_failures"`
	LastAttemptAtMS        sql.NullInt64 `gorm:"column:last_attempt_at_ms"`
	LastSuccessAtMS        sql.NullInt64 `gorm:"column:last_success_at_ms"`
	NextRetryAtMS          sql.NullInt64 `gorm:"column:next_retry_at_ms"`
}

type sourceAttemptMetricsRow struct {
	Attempts          int64 `gorm:"column:attempts"`
	SucceededAttempts int64 `gorm:"column:succeeded_attempts"`
	FailedAttempts    int64 `gorm:"column:failed_attempts"`
	CancelledAttempts int64 `gorm:"column:cancelled_attempts"`
	ResponseBytes     int64 `gorm:"column:response_bytes"`
}

type metricBucketRow struct {
	Value string `gorm:"column:value"`
	Count int64  `gorm:"column:count"`
}

// MetricsSnapshot 在一个显式 read transaction 中读取统一 metrics snapshot。
func (repository *Repository) MetricsSnapshot(
	ctx context.Context,
	filter MetricsSnapshotFilter,
) (MetricsSnapshot, error) {
	if repository == nil || repository.database == nil {
		return MetricsSnapshot{}, ErrInvalidRepository
	}
	if err := validateMetricsSnapshotFilter(filter); err != nil {
		return MetricsSnapshot{}, err
	}
	snapshot := MetricsSnapshot{FromMS: filter.FromMS, UntilMS: filter.UntilMS}
	err := repository.database.View(ctx, func(ctx context.Context, connection storesqlite.ReadConn) error {
		return connection.WithContext(ctx).Transaction(func(transaction *gorm.DB) error {
			return repository.readMetricsSnapshotIn(ctx, transaction, filter, &snapshot)
		})
	})
	return snapshot, err
}

func (repository *Repository) readMetricsSnapshotIn(
	ctx context.Context,
	transaction *gorm.DB,
	filter MetricsSnapshotFilter,
	snapshot *MetricsSnapshot,
) error {
	if err := readRuntimeSamples(ctx, transaction, filter, snapshot); err != nil {
		return err
	}
	if repository.metricsSnapshotReadHook != nil {
		if err := repository.metricsSnapshotReadHook("runtime"); err != nil {
			return err
		}
	}
	if err := readSchedulerMetrics(ctx, transaction, filter, snapshot); err != nil {
		return err
	}
	if err := readJobMetrics(ctx, transaction, filter, snapshot); err != nil {
		return err
	}
	return readSourceMetrics(ctx, transaction, filter, snapshot)
}

func readRuntimeSamples(
	ctx context.Context,
	transaction *gorm.DB,
	filter MetricsSnapshotFilter,
	snapshot *MetricsSnapshot,
) error {
	var models []appRuntimeSampleModel
	if err := transaction.WithContext(ctx).
		Where("captured_at_ms >= ? AND captured_at_ms < ?", filter.FromMS, filter.UntilMS).
		Order("captured_at_ms DESC").Limit(MaxAppRuntimeSampleQueryLimit + 1).Find(&models).Error; err != nil {
		return err
	}
	if len(models) > MaxAppRuntimeSampleQueryLimit {
		return invalidRecord("runtime metrics exceed the complete 24h snapshot capacity")
	}
	snapshot.RuntimeSamples = make([]AppRuntimeSample, len(models))
	for index, model := range models {
		value := appRuntimeSampleFromModel(model)
		if err := validateAppRuntimeSample(value); err != nil {
			return err
		}
		snapshot.RuntimeSamples[index] = value
	}
	return nil
}

func readSchedulerMetrics(
	ctx context.Context,
	transaction *gorm.DB,
	filter MetricsSnapshotFilter,
	snapshot *MetricsSnapshot,
) error {
	var row schedulerMetricsRow
	err := transaction.WithContext(ctx).Model(&schedulerCycleModel{}).
		Select(`COUNT(*) AS cycle_count,
			COALESCE(SUM(CASE WHEN outcome = 'completed' THEN 1 ELSE 0 END), 0) AS completed_cycles,
			COALESCE(SUM(CASE WHEN outcome = 'yielded' THEN 1 ELSE 0 END), 0) AS yielded_cycles,
			COALESCE(SUM(CASE WHEN outcome = 'failed' THEN 1 ELSE 0 END), 0) AS failed_cycles,
			COALESCE(SUM(CASE WHEN outcome = 'interrupted' THEN 1 ELSE 0 END), 0) AS interrupted_cycles,
			COALESCE(SUM(consumed_files), 0) AS files_scanned,
			COALESCE(SUM(consumed_bytes), 0) AS bytes_read,
			COALESCE(SUM(active_ms), 0) AS active_ms,
			COALESCE(MAX(active_ms), 0) AS max_cycle_active_ms,
			MAX(CASE WHEN consumed_files > 0 OR consumed_bytes > 0 THEN finished_at_ms END) AS last_progress_at_ms,
			MAX(CASE WHEN lane = 'backfill' AND (consumed_files > 0 OR consumed_bytes > 0) THEN finished_at_ms END) AS last_backfill_progress_at_ms`).
		Where("finished_at_ms >= ? AND finished_at_ms < ?", filter.FromMS, filter.UntilMS).
		Scan(&row).Error
	if err != nil {
		return err
	}
	snapshot.Scheduler = SchedulerMetrics{
		CycleCount: row.CycleCount, CompletedCycles: row.CompletedCycles,
		YieldedCycles: row.YieldedCycles, FailedCycles: row.FailedCycles,
		InterruptedCycles: row.InterruptedCycles, FilesScanned: row.FilesScanned,
		BytesRead: row.BytesRead, ActiveMS: row.ActiveMS, MaxCycleActiveMS: row.MaxCycleActiveMS,
	}
	if row.LastProgressAtMS.Valid {
		snapshot.Scheduler.LastProgressAtMS = &row.LastProgressAtMS.Int64
	}
	if row.LastBackfillProgressAtMS.Valid {
		snapshot.Scheduler.LastBackfillProgressAtMS = &row.LastBackfillProgressAtMS.Int64
	}
	activeStates := []string{
		string(SchedulerTaskQueued), string(SchedulerTaskRunning), string(SchedulerTaskInterrupted),
	}
	var taskStates []metricBucketRow
	if err := transaction.WithContext(ctx).Model(&schedulerTaskModel{}).
		Select("state AS value, COUNT(*) AS count").Where("state IN ?", activeStates).
		Group("state").Order("state").Scan(&taskStates).Error; err != nil {
		return err
	}
	for _, bucket := range taskStates {
		snapshot.Scheduler.TaskStates = append(snapshot.Scheduler.TaskStates, SchedulerTaskStateMetric{
			State: SchedulerTaskState(bucket.Value), Count: bucket.Count,
		})
	}
	var lanes []metricBucketRow
	if err := transaction.WithContext(ctx).Model(&schedulerTaskModel{}).
		Select("lane AS value, COUNT(*) AS count").Where("state IN ?", activeStates).
		Group("lane").Order("lane").Scan(&lanes).Error; err != nil {
		return err
	}
	for _, bucket := range lanes {
		snapshot.Scheduler.Lanes = append(snapshot.Scheduler.Lanes, SchedulerLaneMetric{
			Lane: SchedulerLane(bucket.Value), Count: bucket.Count,
		})
	}
	var serviceClasses []metricBucketRow
	if err := transaction.WithContext(ctx).Model(&schedulerTaskModel{}).
		Select("service_class AS value, COUNT(*) AS count").Where("state IN ?", activeStates).
		Group("service_class").Order("service_class").Scan(&serviceClasses).Error; err != nil {
		return err
	}
	for _, bucket := range serviceClasses {
		snapshot.Scheduler.ServiceClasses = append(snapshot.Scheduler.ServiceClasses, SchedulerServiceClassMetric{
			ServiceClass: SchedulerServiceClass(bucket.Value), Count: bucket.Count,
		})
	}
	var stopReasons []metricBucketRow
	if err := transaction.WithContext(ctx).Model(&schedulerCycleModel{}).
		Select("stop_reason AS value, COUNT(*) AS count").
		Where("finished_at_ms >= ? AND finished_at_ms < ?", filter.FromMS, filter.UntilMS).
		Group("stop_reason").Order("stop_reason").Scan(&stopReasons).Error; err != nil {
		return err
	}
	for _, bucket := range stopReasons {
		snapshot.Scheduler.StopReasons = append(snapshot.Scheduler.StopReasons, SchedulerStopReasonMetric{
			Reason: SchedulerStopReason(bucket.Value), Count: bucket.Count,
		})
	}
	var retries []metricBucketRow
	if err := transaction.WithContext(ctx).Model(&schedulerRetryStateModel{}).
		Select("disposition AS value, COUNT(*) AS count").
		Where("disposition IN ?", []string{string(SchedulerRetryWaiting), string(SchedulerRetryBlocked)}).
		Group("disposition").Order("disposition").Scan(&retries).Error; err != nil {
		return err
	}
	for _, bucket := range retries {
		snapshot.Scheduler.RetryDispositions = append(snapshot.Scheduler.RetryDispositions,
			SchedulerRetryDispositionMetric{Disposition: SchedulerRetryDisposition(bucket.Value), Count: bucket.Count})
	}
	return validateSchedulerMetrics(snapshot.Scheduler)
}

func readJobMetrics(
	ctx context.Context,
	transaction *gorm.DB,
	filter MetricsSnapshotFilter,
	snapshot *MetricsSnapshot,
) error {
	var active activeJobMetricsRow
	if err := transaction.WithContext(ctx).Model(&jobRunModel{}).
		Select(`COALESCE(SUM(CASE WHEN state = 'queued' THEN 1 ELSE 0 END), 0) AS queued,
			COALESCE(SUM(CASE WHEN state = 'running' THEN 1 ELSE 0 END), 0) AS running,
			COALESCE(SUM(CASE WHEN state = 'interrupted' THEN 1 ELSE 0 END), 0) AS interrupted`).
		Where("state IN ? OR (state = ? AND resume_consumed_by_job_id IS NULL)",
			[]string{string(JobQueued), string(JobRunning)}, string(JobInterrupted)).
		Scan(&active).Error; err != nil {
		return err
	}
	var terminal terminalJobMetricsRow
	if err := transaction.WithContext(ctx).Model(&jobRunModel{}).
		Select(`COALESCE(SUM(CASE WHEN state = 'succeeded' THEN 1 ELSE 0 END), 0) AS succeeded,
			COALESCE(SUM(CASE WHEN state = 'failed' THEN 1 ELSE 0 END), 0) AS failed,
			COALESCE(SUM(CASE WHEN state = 'cancelled' THEN 1 ELSE 0 END), 0) AS cancelled,
			COALESCE(SUM(CASE WHEN started_at_ms IS NOT NULL THEN 1 ELSE 0 END), 0) AS duration_count,
			COALESCE(SUM(CASE WHEN started_at_ms IS NOT NULL THEN finished_at_ms - started_at_ms ELSE 0 END), 0) AS duration_total_ms,
			COALESCE(MAX(CASE WHEN started_at_ms IS NOT NULL THEN finished_at_ms - started_at_ms ELSE 0 END), 0) AS duration_max_ms`).
		Where("state IN ? AND finished_at_ms >= ? AND finished_at_ms < ?",
			[]string{string(JobSucceeded), string(JobFailed), string(JobCancelled)},
			filter.FromMS, filter.UntilMS).
		Scan(&terminal).Error; err != nil {
		return err
	}
	snapshot.Jobs = JobMetrics{
		Queued: active.Queued, Running: active.Running, Interrupted: active.Interrupted,
		Succeeded: terminal.Succeeded, Failed: terminal.Failed, Cancelled: terminal.Cancelled,
		DurationCount: terminal.DurationCount, DurationTotalMS: terminal.DurationTotalMS,
		DurationMaxMS: terminal.DurationMaxMS,
	}
	return validateJobMetrics(snapshot.Jobs)
}

func readSourceMetrics(
	ctx context.Context,
	transaction *gorm.DB,
	filter MetricsSnapshotFilter,
	snapshot *MetricsSnapshot,
) error {
	var states sourceStateMetricsRow
	if err := transaction.WithContext(ctx).Model(&sourceStateModel{}).
		Select(`COUNT(*) AS total,
			COALESCE(SUM(CASE WHEN freshness_state = 'unknown' THEN 1 ELSE 0 END), 0) AS unknown,
			COALESCE(SUM(CASE WHEN freshness_state = 'current' THEN 1 ELSE 0 END), 0) AS current,
			COALESCE(SUM(CASE WHEN freshness_state = 'stale' THEN 1 ELSE 0 END), 0) AS stale,
			COALESCE(SUM(CASE WHEN freshness_state = 'unavailable' THEN 1 ELSE 0 END), 0) AS unavailable,
			COALESCE(SUM(consecutive_failures), 0) AS consecutive_failures,
			COALESCE(MAX(consecutive_failures), 0) AS max_consecutive_failures,
			MAX(last_attempt_at_ms) AS last_attempt_at_ms,
			MAX(last_success_at_ms) AS last_success_at_ms,
			MIN(next_due_at_ms) AS next_retry_at_ms`).Scan(&states).Error; err != nil {
		return err
	}
	var attempts sourceAttemptMetricsRow
	if err := transaction.WithContext(ctx).Model(&sourceAttemptModel{}).
		Select(`COUNT(*) AS attempts,
			COALESCE(SUM(CASE WHEN outcome = 'succeeded' THEN 1 ELSE 0 END), 0) AS succeeded_attempts,
			COALESCE(SUM(CASE WHEN outcome = 'failed' THEN 1 ELSE 0 END), 0) AS failed_attempts,
			COALESCE(SUM(CASE WHEN outcome = 'cancelled' THEN 1 ELSE 0 END), 0) AS cancelled_attempts,
			COALESCE(SUM(response_bytes), 0) AS response_bytes`).
		Where("finished_at_ms >= ? AND finished_at_ms < ?", filter.FromMS, filter.UntilMS).
		Scan(&attempts).Error; err != nil {
		return err
	}
	snapshot.Sources = SourceMetrics{
		Total: states.Total, Unknown: states.Unknown, Current: states.Current,
		Stale: states.Stale, Unavailable: states.Unavailable,
		ConsecutiveFailures:    states.ConsecutiveFailures,
		MaxConsecutiveFailures: states.MaxConsecutiveFailures,
		Attempts:               attempts.Attempts, SucceededAttempts: attempts.SucceededAttempts,
		FailedAttempts: attempts.FailedAttempts, CancelledAttempts: attempts.CancelledAttempts,
		ResponseBytes:   attempts.ResponseBytes,
		LastAttemptAtMS: metricNullableInt64(states.LastAttemptAtMS),
		LastSuccessAtMS: metricNullableInt64(states.LastSuccessAtMS),
		NextRetryAtMS:   metricNullableInt64(states.NextRetryAtMS),
	}
	if err := readSourceMetricBuckets(ctx, transaction, filter, &snapshot.Sources); err != nil {
		return err
	}
	return validateSourceMetrics(snapshot.Sources)
}

func readSourceMetricBuckets(
	ctx context.Context,
	transaction *gorm.DB,
	filter MetricsSnapshotFilter,
	metrics *SourceMetrics,
) error {
	var currentErrors []metricBucketRow
	if err := transaction.WithContext(ctx).Model(&sourceStateModel{}).
		Select("last_error_class AS value, COUNT(*) AS count").Where("last_error_class IS NOT NULL").
		Group("last_error_class").Order("last_error_class").Scan(&currentErrors).Error; err != nil {
		return err
	}
	for _, bucket := range currentErrors {
		metrics.CurrentErrorClasses = append(metrics.CurrentErrorClasses,
			RuntimeErrorClassMetric{ErrorClass: RuntimeErrorClass(bucket.Value), Count: bucket.Count})
	}
	var currentFailures []metricBucketRow
	if err := transaction.WithContext(ctx).Model(&sourceStateModel{}).
		Select("last_failure_code AS value, COUNT(*) AS count").Where("last_failure_code IS NOT NULL").
		Group("last_failure_code").Order("last_failure_code").Scan(&currentFailures).Error; err != nil {
		return err
	}
	for _, bucket := range currentFailures {
		metrics.CurrentFailureCodes = append(metrics.CurrentFailureCodes,
			SourceFailureCodeMetric{FailureCode: SourceFailureCode(bucket.Value), Count: bucket.Count})
	}
	attemptQuery := func() *gorm.DB {
		return transaction.WithContext(ctx).Model(&sourceAttemptModel{}).
			Where("finished_at_ms >= ? AND finished_at_ms < ?", filter.FromMS, filter.UntilMS)
	}
	var attemptErrors []metricBucketRow
	if err := attemptQuery().Select("error_class AS value, COUNT(*) AS count").Where("error_class IS NOT NULL").
		Group("error_class").Order("error_class").Scan(&attemptErrors).Error; err != nil {
		return err
	}
	for _, bucket := range attemptErrors {
		metrics.AttemptErrorClasses = append(metrics.AttemptErrorClasses,
			RuntimeErrorClassMetric{ErrorClass: RuntimeErrorClass(bucket.Value), Count: bucket.Count})
	}
	var attemptFailures []metricBucketRow
	if err := attemptQuery().Select("failure_code AS value, COUNT(*) AS count").Where("failure_code IS NOT NULL").
		Group("failure_code").Order("failure_code").Scan(&attemptFailures).Error; err != nil {
		return err
	}
	for _, bucket := range attemptFailures {
		metrics.AttemptFailureCodes = append(metrics.AttemptFailureCodes,
			SourceFailureCodeMetric{FailureCode: SourceFailureCode(bucket.Value), Count: bucket.Count})
	}
	return nil
}

func metricNullableInt64(value sql.NullInt64) *int64 {
	if !value.Valid {
		return nil
	}
	return &value.Int64
}

func validateMetricsSnapshotFilter(filter MetricsSnapshotFilter) error {
	if filter.FromMS < 0 || filter.UntilMS <= filter.FromMS ||
		filter.UntilMS > runtimeclock.MaxTimestampMS+1 || filter.UntilMS-filter.FromMS != MetricsSnapshotWindowMS {
		return invalidRecord("metrics snapshot window must be exactly 24 hours")
	}
	return nil
}

func validateSchedulerMetrics(value SchedulerMetrics) error {
	if value.CycleCount < 0 || value.CompletedCycles < 0 || value.YieldedCycles < 0 ||
		value.FailedCycles < 0 || value.InterruptedCycles < 0 || value.FilesScanned < 0 ||
		value.BytesRead < 0 || value.ActiveMS < 0 || value.MaxCycleActiveMS < 0 ||
		!validMetricTimestamp(value.LastProgressAtMS) ||
		!validMetricTimestamp(value.LastBackfillProgressAtMS) ||
		value.CompletedCycles+value.YieldedCycles+value.FailedCycles+value.InterruptedCycles != value.CycleCount {
		return invalidRecord("stored scheduler metrics are invalid")
	}
	taskCount, err := validateSchedulerTaskStateMetrics(value.TaskStates)
	if err != nil {
		return err
	}
	laneCount, err := validateSchedulerLaneMetrics(value.Lanes)
	if err != nil || laneCount != taskCount {
		return invalidRecord("stored scheduler lane metrics are invalid")
	}
	serviceCount, err := validateSchedulerServiceClassMetrics(value.ServiceClasses)
	if err != nil || serviceCount != taskCount {
		return invalidRecord("stored scheduler service class metrics are invalid")
	}
	stopReasonCount, err := validateSchedulerStopReasonMetrics(value.StopReasons)
	if err != nil || stopReasonCount != value.CycleCount {
		return invalidRecord("stored scheduler stop reason metrics are invalid")
	}
	if _, err := validateSchedulerRetryDispositionMetrics(value.RetryDispositions); err != nil {
		return err
	}
	return nil
}

func validateJobMetrics(value JobMetrics) error {
	if value.Queued < 0 || value.Running < 0 || value.Interrupted < 0 ||
		value.Succeeded < 0 || value.Failed < 0 || value.Cancelled < 0 ||
		value.DurationCount < 0 || value.DurationTotalMS < 0 || value.DurationMaxMS < 0 ||
		value.DurationMaxMS > value.DurationTotalMS ||
		value.DurationCount > value.Succeeded+value.Failed+value.Cancelled {
		return invalidRecord("stored job metrics are invalid")
	}
	return nil
}

func validateSourceMetrics(value SourceMetrics) error {
	if value.Total < 0 || value.Unknown < 0 || value.Current < 0 || value.Stale < 0 ||
		value.Unavailable < 0 || value.ConsecutiveFailures < 0 || value.MaxConsecutiveFailures < 0 ||
		value.Attempts < 0 || value.SucceededAttempts < 0 || value.FailedAttempts < 0 ||
		value.CancelledAttempts < 0 || value.ResponseBytes < 0 ||
		value.Unknown+value.Current+value.Stale+value.Unavailable != value.Total ||
		value.SucceededAttempts+value.FailedAttempts+value.CancelledAttempts != value.Attempts ||
		!validMetricTimestamp(value.LastAttemptAtMS) || !validMetricTimestamp(value.LastSuccessAtMS) ||
		!validMetricTimestamp(value.NextRetryAtMS) {
		return invalidRecord("stored source metrics are invalid")
	}
	currentErrorCount, err := validateRuntimeErrorClassMetrics(value.CurrentErrorClasses)
	if err != nil || currentErrorCount > value.Total {
		return invalidRecord("stored source current error metrics are invalid")
	}
	currentFailureCount, err := validateSourceFailureCodeMetrics(value.CurrentFailureCodes)
	if err != nil || currentFailureCount > value.Total {
		return invalidRecord("stored source current failure metrics are invalid")
	}
	attemptErrorCount, err := validateRuntimeErrorClassMetrics(value.AttemptErrorClasses)
	if err != nil || attemptErrorCount > value.Attempts {
		return invalidRecord("stored source attempt error metrics are invalid")
	}
	attemptFailureCount, err := validateSourceFailureCodeMetrics(value.AttemptFailureCodes)
	if err != nil || attemptFailureCount > value.Attempts {
		return invalidRecord("stored source attempt failure metrics are invalid")
	}
	return nil
}

func validateSchedulerTaskStateMetrics(values []SchedulerTaskStateMetric) (int64, error) {
	seen := make(map[SchedulerTaskState]struct{}, len(values))
	var total int64
	for _, value := range values {
		if value.Count < 1 || value.State != SchedulerTaskQueued && value.State != SchedulerTaskRunning &&
			value.State != SchedulerTaskInterrupted {
			return 0, invalidRecord("stored scheduler task state metrics are invalid")
		}
		if _, exists := seen[value.State]; exists {
			return 0, invalidRecord("stored scheduler task state metrics contain duplicates")
		}
		seen[value.State] = struct{}{}
		total += value.Count
	}
	return total, nil
}

func validateSchedulerLaneMetrics(values []SchedulerLaneMetric) (int64, error) {
	seen := make(map[SchedulerLane]struct{}, len(values))
	var total int64
	for _, value := range values {
		if value.Count < 1 || !validSchedulerLane(value.Lane) {
			return 0, invalidRecord("stored scheduler lane metrics are invalid")
		}
		if _, exists := seen[value.Lane]; exists {
			return 0, invalidRecord("stored scheduler lane metrics contain duplicates")
		}
		seen[value.Lane] = struct{}{}
		total += value.Count
	}
	return total, nil
}

func validateSchedulerServiceClassMetrics(values []SchedulerServiceClassMetric) (int64, error) {
	seen := make(map[SchedulerServiceClass]struct{}, len(values))
	var total int64
	for _, value := range values {
		if value.Count < 1 || !validSchedulerServiceClass(value.ServiceClass) {
			return 0, invalidRecord("stored scheduler service class metrics are invalid")
		}
		if _, exists := seen[value.ServiceClass]; exists {
			return 0, invalidRecord("stored scheduler service class metrics contain duplicates")
		}
		seen[value.ServiceClass] = struct{}{}
		total += value.Count
	}
	return total, nil
}

func validateSchedulerStopReasonMetrics(values []SchedulerStopReasonMetric) (int64, error) {
	seen := make(map[SchedulerStopReason]struct{}, len(values))
	var total int64
	for _, value := range values {
		if value.Count < 1 || !validSchedulerStopReason(value.Reason) {
			return 0, invalidRecord("stored scheduler stop reason metrics are invalid")
		}
		if _, exists := seen[value.Reason]; exists {
			return 0, invalidRecord("stored scheduler stop reason metrics contain duplicates")
		}
		seen[value.Reason] = struct{}{}
		total += value.Count
	}
	return total, nil
}

func validateSchedulerRetryDispositionMetrics(values []SchedulerRetryDispositionMetric) (int64, error) {
	seen := make(map[SchedulerRetryDisposition]struct{}, len(values))
	var total int64
	for _, value := range values {
		if value.Count < 1 || value.Disposition != SchedulerRetryWaiting && value.Disposition != SchedulerRetryBlocked {
			return 0, invalidRecord("stored scheduler retry metrics are invalid")
		}
		if _, exists := seen[value.Disposition]; exists {
			return 0, invalidRecord("stored scheduler retry metrics contain duplicates")
		}
		seen[value.Disposition] = struct{}{}
		total += value.Count
	}
	return total, nil
}

func validateRuntimeErrorClassMetrics(values []RuntimeErrorClassMetric) (int64, error) {
	seen := make(map[RuntimeErrorClass]struct{}, len(values))
	var total int64
	for _, value := range values {
		if value.Count < 1 || !validRuntimeErrorClass(value.ErrorClass) {
			return 0, invalidRecord("stored runtime error class metrics are invalid")
		}
		if _, exists := seen[value.ErrorClass]; exists {
			return 0, invalidRecord("stored runtime error class metrics contain duplicates")
		}
		seen[value.ErrorClass] = struct{}{}
		total += value.Count
	}
	return total, nil
}

func validateSourceFailureCodeMetrics(values []SourceFailureCodeMetric) (int64, error) {
	seen := make(map[SourceFailureCode]struct{}, len(values))
	var total int64
	for _, value := range values {
		if value.Count < 1 || !validSourceFailureCode(value.FailureCode) {
			return 0, invalidRecord("stored source failure code metrics are invalid")
		}
		if _, exists := seen[value.FailureCode]; exists {
			return 0, invalidRecord("stored source failure code metrics contain duplicates")
		}
		seen[value.FailureCode] = struct{}{}
		total += value.Count
	}
	return total, nil
}

func validMetricTimestamp(value *int64) bool {
	return value == nil || (*value >= 0 && *value <= runtimeclock.MaxTimestampMS)
}
