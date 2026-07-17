package store

import (
	"context"
	"errors"
	"sort"
	"strings"

	"gorm.io/gorm"

	storesqlite "github.com/SisyphusSQ/codex-pulse/internal/store/sqlite"
)

const (
	runtimeLocalSourcePrefix  = "local_file:"
	runtimeOnlineSourcePrefix = "online:"
)

func (repository *Repository) RuntimeSourcePage(
	ctx context.Context,
	filter RuntimeSourceQuery,
) (RuntimeSourcePage, error) {
	if repository == nil || repository.database == nil {
		return RuntimeSourcePage{}, ErrInvalidRepository
	}
	limit, err := validateRuntimeQuery(filter.Limit, filter.Direction)
	if err != nil || !validRuntimeSourceFilter(filter) {
		return RuntimeSourcePage{}, invalidRecord("runtime source query is invalid")
	}
	var page RuntimeSourcePage
	err = repository.database.View(ctx, func(ctx context.Context, connection storesqlite.ReadConn) error {
		return connection.WithContext(ctx).Transaction(func(transaction *gorm.DB) error {
			candidates := make([]RuntimeSourceRecord, 0, 2*(limit+1))
			selectedKinds := 0
			kindErrors := make([]error, 0, 2)
			if runtimeSourceKindSelected(filter.Kinds, RuntimeSourceLocalFile) {
				selectedKinds++
				records, count, attention, err := repository.runtimeLocalSources(
					ctx, transaction, filter, limit,
				)
				if err != nil {
					if runtimeQueryMustStop(ctx, err) {
						return err
					}
					page.UnavailableKinds = append(page.UnavailableKinds, RuntimeSourceLocalFile)
					kindErrors = append(kindErrors, err)
				} else {
					page.Summary.LocalFiles = count
					page.Summary.Attention += attention
					candidates = append(candidates, records...)
				}
			}
			if runtimeSourceKindSelected(filter.Kinds, RuntimeSourceOnline) {
				selectedKinds++
				records, count, attention, err := repository.runtimeOnlineSources(
					ctx, transaction, filter, limit,
				)
				if err != nil {
					if runtimeQueryMustStop(ctx, err) {
						return err
					}
					page.UnavailableKinds = append(page.UnavailableKinds, RuntimeSourceOnline)
					kindErrors = append(kindErrors, err)
				} else {
					page.Summary.OnlineSources = count
					page.Summary.Attention += attention
					candidates = append(candidates, records...)
				}
			}
			if selectedKinds == 0 || len(kindErrors) == selectedKinds {
				return errors.Join(kindErrors...)
			}
			page.Summary.Total = page.Summary.LocalFiles + page.Summary.OnlineSources
			page.MatchedCount = page.Summary.Total
			sortRuntimeSourceRecords(candidates, filter.Direction)
			if len(candidates) > limit {
				candidates = candidates[:limit]
				last := candidates[len(candidates)-1]
				page.NextCursor = &RuntimeSourceCursor{
					UpdatedAtMS: last.UpdatedAtMS, SourceKey: last.SourceKey,
				}
			}
			page.Records = candidates
			if page.Records == nil {
				page.Records = make([]RuntimeSourceRecord, 0)
			}
			if page.UnavailableKinds == nil {
				page.UnavailableKinds = make([]RuntimeSourceKind, 0)
			}
			return nil
		})
	})
	return page, err
}

func (repository *Repository) runtimeLocalSources(
	ctx context.Context,
	transaction *gorm.DB,
	filter RuntimeSourceQuery,
	limit int,
) ([]RuntimeSourceRecord, int64, int64, error) {
	if err := repository.callRuntimeQueryReadHook("source_local_before"); err != nil {
		return nil, 0, 0, err
	}
	query := runtimeLocalSourceQuery(transaction.WithContext(ctx), filter.States)
	count, attention, err := runtimeLocalSourceCounts(query)
	if err != nil {
		return nil, 0, 0, err
	}
	if err := repository.callRuntimeQueryReadHook("source_local_after_counts"); err != nil {
		return nil, 0, 0, err
	}
	query, err = applyRuntimeSourceCursor(
		query, "source_file_id", runtimeLocalSourcePrefix, filter.After, filter.Direction,
	)
	if err != nil {
		return nil, 0, 0, err
	}
	var models []sourceFileModel
	if err := runtimeOrder(query, "updated_at_ms", "source_file_id", filter.Direction).
		Limit(limit + 1).Find(&models).Error; err != nil {
		return nil, 0, 0, err
	}
	records := make([]RuntimeSourceRecord, 0, len(models))
	for _, model := range models {
		value := sourceFileFromModel(model)
		records = append(records, RuntimeSourceRecord{
			SourceKey: runtimeLocalSourcePrefix + value.SourceFileID,
			Kind:      RuntimeSourceLocalFile, UpdatedAtMS: value.UpdatedAtMS, Local: &value,
		})
	}
	return records, count, attention, nil
}

func (repository *Repository) runtimeOnlineSources(
	ctx context.Context,
	transaction *gorm.DB,
	filter RuntimeSourceQuery,
	limit int,
) ([]RuntimeSourceRecord, int64, int64, error) {
	if err := repository.callRuntimeQueryReadHook("source_online_before"); err != nil {
		return nil, 0, 0, err
	}
	query := runtimeOnlineSourceQuery(transaction.WithContext(ctx), filter.States)
	count, attention, err := runtimeOnlineSourceCounts(query)
	if err != nil {
		return nil, 0, 0, err
	}
	if err := repository.callRuntimeQueryReadHook("source_online_after_counts"); err != nil {
		return nil, 0, 0, err
	}
	query, err = applyRuntimeSourceCursor(
		query, "source_instance_id", runtimeOnlineSourcePrefix, filter.After, filter.Direction,
	)
	if err != nil {
		return nil, 0, 0, err
	}
	var models []sourceStateModel
	if err := runtimeOrder(query, "updated_at_ms", "source_instance_id", filter.Direction).
		Limit(limit + 1).Find(&models).Error; err != nil {
		return nil, 0, 0, err
	}
	records := make([]RuntimeSourceRecord, 0, len(models))
	for _, model := range models {
		value := sourceStateFromModel(model)
		records = append(records, RuntimeSourceRecord{
			SourceKey: runtimeOnlineSourcePrefix + value.SourceInstanceID,
			Kind:      RuntimeSourceOnline, UpdatedAtMS: value.UpdatedAtMS, Online: &value,
		})
	}
	return records, count, attention, nil
}

func sortRuntimeSourceRecords(records []RuntimeSourceRecord, direction RuntimeQueryDirection) {
	sort.Slice(records, func(left, right int) bool {
		if records[left].UpdatedAtMS != records[right].UpdatedAtMS {
			if direction == RuntimeQueryAscending {
				return records[left].UpdatedAtMS < records[right].UpdatedAtMS
			}
			return records[left].UpdatedAtMS > records[right].UpdatedAtMS
		}
		if direction == RuntimeQueryAscending {
			return records[left].SourceKey < records[right].SourceKey
		}
		return records[left].SourceKey > records[right].SourceKey
	})
}

func runtimeQueryMustStop(ctx context.Context, err error) bool {
	return errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) ||
		ctx.Err() != nil
}

func (repository *Repository) callRuntimeQueryReadHook(stage string) error {
	if repository != nil && repository.runtimeQueryReadHook != nil {
		return repository.runtimeQueryReadHook(stage)
	}
	return nil
}

func (repository *Repository) RuntimeSource(
	ctx context.Context,
	sourceKey string,
) (RuntimeSourceRecord, error) {
	kind, identity, ok := parseRuntimeSourceKey(sourceKey)
	if repository == nil || repository.database == nil || !ok {
		return RuntimeSourceRecord{}, ErrInvalidRepository
	}
	if kind == RuntimeSourceLocalFile {
		file, err := repository.SourceFile(ctx, identity)
		return RuntimeSourceRecord{
			SourceKey: sourceKey, Kind: kind, UpdatedAtMS: file.UpdatedAtMS, Local: &file,
		}, err
	}
	state, err := repository.SourceState(ctx, identity)
	return RuntimeSourceRecord{
		SourceKey: sourceKey, Kind: kind, UpdatedAtMS: state.UpdatedAtMS, Online: &state,
	}, err
}

func (repository *Repository) RuntimeJobPage(
	ctx context.Context,
	filter RuntimeJobQuery,
) (RuntimeJobPage, error) {
	if repository == nil || repository.database == nil {
		return RuntimeJobPage{}, ErrInvalidRepository
	}
	limit, err := validateRuntimeQuery(filter.Limit, filter.Direction)
	if err != nil || !validRuntimeJobFilter(filter) {
		return RuntimeJobPage{}, invalidRecord("runtime job query is invalid")
	}
	var page RuntimeJobPage
	err = repository.database.View(ctx, func(ctx context.Context, connection storesqlite.ReadConn) error {
		return connection.WithContext(ctx).Transaction(func(transaction *gorm.DB) error {
			query := runtimeJobQuery(transaction.WithContext(ctx), filter)
			if err := runtimeJobCounts(query, &page.Summary); err != nil {
				return err
			}
			if err := repository.callRuntimeQueryReadHook("job_after_counts"); err != nil {
				return err
			}
			page.MatchedCount = page.Summary.Total
			if filter.After != nil {
				query = runtimeCursorWhere(
					query, "updated_at_ms", "job_id", filter.After.UpdatedAtMS,
					filter.After.JobID, filter.Direction,
				)
			}
			var models []jobRunModel
			if err := runtimeOrder(query, "updated_at_ms", "job_id", filter.Direction).
				Limit(limit + 1).Find(&models).Error; err != nil {
				return err
			}
			hasMore := len(models) > limit
			if hasMore {
				models = models[:limit]
			}
			if err := repository.callRuntimeQueryReadHook("job_after_rows"); err != nil {
				return err
			}
			records, err := runtimeJobRecords(ctx, transaction, models)
			if err != nil {
				return err
			}
			page.Records = records
			if page.Records == nil {
				page.Records = make([]RuntimeJobRecord, 0)
			}
			if hasMore {
				last := records[len(records)-1].Job
				page.NextCursor = &RuntimeJobCursor{UpdatedAtMS: last.UpdatedAtMS, JobID: last.JobID}
			}
			return nil
		})
	})
	return page, err
}

func (repository *Repository) RuntimeJob(
	ctx context.Context,
	jobID string,
) (RuntimeJobRecord, error) {
	if repository == nil || repository.database == nil || jobID == "" {
		return RuntimeJobRecord{}, ErrInvalidRepository
	}
	var record RuntimeJobRecord
	err := repository.database.View(ctx, func(ctx context.Context, connection storesqlite.ReadConn) error {
		return connection.WithContext(ctx).Transaction(func(transaction *gorm.DB) error {
			var model jobRunModel
			result := transaction.WithContext(ctx).Where("job_id = ?", jobID).Take(&model)
			if errors.Is(result.Error, gorm.ErrRecordNotFound) {
				return ErrNotFound
			}
			if result.Error != nil {
				return result.Error
			}
			if err := repository.callRuntimeQueryReadHook("job_detail_after_job"); err != nil {
				return err
			}
			records, err := runtimeJobRecords(ctx, transaction, []jobRunModel{model})
			if err == nil {
				record = records[0]
			}
			return err
		})
	})
	return record, err
}

func (repository *Repository) RuntimeHealthPage(
	ctx context.Context,
	filter RuntimeHealthQuery,
) (RuntimeHealthPage, error) {
	if repository == nil || repository.database == nil {
		return RuntimeHealthPage{}, ErrInvalidRepository
	}
	limit, err := validateRuntimeQuery(filter.Limit, filter.Direction)
	if err != nil || !validRuntimeHealthFilter(filter) {
		return RuntimeHealthPage{}, invalidRecord("runtime health query is invalid")
	}
	var page RuntimeHealthPage
	err = repository.database.View(ctx, func(ctx context.Context, connection storesqlite.ReadConn) error {
		return connection.WithContext(ctx).Transaction(func(transaction *gorm.DB) error {
			query := runtimeHealthQuery(transaction.WithContext(ctx), filter)
			if err := runtimeHealthCounts(
				query, &page.Summary, repository.runtimeHealthAggregateRowsHook,
			); err != nil {
				return err
			}
			if err := repository.callRuntimeQueryReadHook("health_after_counts"); err != nil {
				return err
			}
			lifecycle, found, err := schedulerLifecycleIn(ctx, transaction)
			if err != nil {
				return err
			}
			if found {
				page.Lifecycle = &lifecycle
			}
			page.MatchedCount = page.Summary.Total
			if filter.After != nil {
				query = runtimeCursorWhere(
					query, "last_seen_at_ms", "event_id", filter.After.LastSeenAtMS,
					filter.After.EventID, filter.Direction,
				)
			}
			var models []healthEventModel
			if err := runtimeOrder(query, "last_seen_at_ms", "event_id", filter.Direction).
				Limit(limit + 1).Find(&models).Error; err != nil {
				return err
			}
			hasMore := len(models) > limit
			if hasMore {
				models = models[:limit]
			}
			page.Records = make([]HealthEvent, len(models))
			for index, model := range models {
				value, err := healthEventFromModel(model)
				if err != nil {
					return err
				}
				page.Records[index] = value
			}
			if hasMore {
				last := page.Records[len(page.Records)-1]
				page.NextCursor = &RuntimeHealthCursor{
					LastSeenAtMS: last.LastSeenAtMS, EventID: last.EventID,
				}
			}
			return nil
		})
	})
	return page, err
}

func (repository *Repository) RuntimeHealth(ctx context.Context, eventID string) (HealthEvent, error) {
	return repository.HealthEvent(ctx, eventID)
}

func validateRuntimeQuery(limit int, direction RuntimeQueryDirection) (int, error) {
	validated, err := validateRuntimeLimit(limit)
	if err != nil || (direction != RuntimeQueryAscending && direction != RuntimeQueryDescending) {
		return 0, ErrInvalidRecord
	}
	return validated, nil
}

func validRuntimeSourceFilter(filter RuntimeSourceQuery) bool {
	if filter.After != nil {
		if filter.After.UpdatedAtMS < 0 {
			return false
		}
		if _, _, ok := parseRuntimeSourceKey(filter.After.SourceKey); !ok {
			return false
		}
	}
	if !uniqueRuntimeSourceKinds(filter.Kinds) || len(filter.States) > 16 {
		return false
	}
	seen := make(map[string]struct{}, len(filter.States))
	for _, state := range filter.States {
		if !validRuntimeSourceState(state) {
			return false
		}
		if _, exists := seen[state]; exists {
			return false
		}
		seen[state] = struct{}{}
	}
	return true
}

func validRuntimeJobFilter(filter RuntimeJobQuery) bool {
	if filter.After != nil && (filter.After.UpdatedAtMS < 0 || filter.After.JobID == "") {
		return false
	}
	if filter.CurrentOnly && len(filter.States) > 0 {
		return false
	}
	seenStates := make(map[JobState]struct{}, len(filter.States))
	for _, state := range filter.States {
		if !validJobState(state) {
			return false
		}
		if _, exists := seenStates[state]; exists {
			return false
		}
		seenStates[state] = struct{}{}
	}
	seenPhases := make(map[JobPhase]struct{}, len(filter.Phases))
	for _, phase := range filter.Phases {
		if !validJobPhase(phase) {
			return false
		}
		if _, exists := seenPhases[phase]; exists {
			return false
		}
		seenPhases[phase] = struct{}{}
	}
	return true
}

func validRuntimeHealthFilter(filter RuntimeHealthQuery) bool {
	if filter.After != nil && (filter.After.LastSeenAtMS < 0 || filter.After.EventID == "") {
		return false
	}
	seenSeverity := make(map[HealthSeverity]struct{}, len(filter.Severities))
	for _, severity := range filter.Severities {
		if !validHealthSeverity(severity) {
			return false
		}
		if _, exists := seenSeverity[severity]; exists {
			return false
		}
		seenSeverity[severity] = struct{}{}
	}
	seenDomain := make(map[HealthDomain]struct{}, len(filter.Domains))
	for _, domain := range filter.Domains {
		if !validHealthDomain(domain) {
			return false
		}
		if _, exists := seenDomain[domain]; exists {
			return false
		}
		seenDomain[domain] = struct{}{}
	}
	return true
}

func uniqueRuntimeSourceKinds(kinds []RuntimeSourceKind) bool {
	seen := make(map[RuntimeSourceKind]struct{}, len(kinds))
	for _, kind := range kinds {
		if kind != RuntimeSourceLocalFile && kind != RuntimeSourceOnline {
			return false
		}
		if _, exists := seen[kind]; exists {
			return false
		}
		seen[kind] = struct{}{}
	}
	return true
}

func validRuntimeSourceState(value string) bool {
	return validSourceFileState(SourceFileState(value)) || validSourceFreshness(SourceFreshness(value))
}

func runtimeSourceKindSelected(kinds []RuntimeSourceKind, target RuntimeSourceKind) bool {
	if len(kinds) == 0 {
		return true
	}
	for _, kind := range kinds {
		if kind == target {
			return true
		}
	}
	return false
}

func runtimeLocalSourceQuery(database *gorm.DB, states []string) *gorm.DB {
	query := database.Model(&sourceFileModel{})
	values := make([]string, 0, len(states))
	for _, state := range states {
		if validSourceFileState(SourceFileState(state)) {
			values = append(values, state)
		}
	}
	if len(states) > 0 {
		if len(values) == 0 {
			return query.Where("1 = 0")
		}
		query = query.Where("state IN ?", values)
	}
	return query
}

func runtimeOnlineSourceQuery(database *gorm.DB, states []string) *gorm.DB {
	query := database.Model(&sourceStateModel{})
	values := make([]string, 0, len(states))
	for _, state := range states {
		if validSourceFreshness(SourceFreshness(state)) {
			values = append(values, state)
		}
	}
	if len(states) > 0 {
		if len(values) == 0 {
			return query.Where("1 = 0")
		}
		query = query.Where("freshness_state IN ?", values)
	}
	return query
}

func runtimeLocalSourceCounts(query *gorm.DB) (int64, int64, error) {
	var total, attention int64
	if err := query.Session(&gorm.Session{}).Count(&total).Error; err != nil {
		return 0, 0, err
	}
	if err := query.Session(&gorm.Session{}).
		Where("state IN ? OR last_error_class IS NOT NULL", []string{
			string(SourceFileFailed), string(SourceFileUnavailable),
		}).Count(&attention).Error; err != nil {
		return 0, 0, err
	}
	return total, attention, nil
}

func runtimeOnlineSourceCounts(query *gorm.DB) (int64, int64, error) {
	var total, attention int64
	if err := query.Session(&gorm.Session{}).Count(&total).Error; err != nil {
		return 0, 0, err
	}
	if err := query.Session(&gorm.Session{}).
		Where("freshness_state IN ? OR consecutive_failures > 0 OR last_error_class IS NOT NULL OR last_failure_code IS NOT NULL",
			[]string{string(SourceFreshnessStale), string(SourceFreshnessUnavailable)}).
		Count(&attention).Error; err != nil {
		return 0, 0, err
	}
	return total, attention, nil
}

func applyRuntimeSourceCursor(
	query *gorm.DB,
	identityColumn string,
	prefix string,
	after *RuntimeSourceCursor,
	direction RuntimeQueryDirection,
) (*gorm.DB, error) {
	if after == nil {
		return query, nil
	}
	_, cursorIdentity, ok := parseRuntimeSourceKey(after.SourceKey)
	if !ok {
		return nil, ErrInvalidRecord
	}
	cursorPrefix := strings.TrimSuffix(after.SourceKey, cursorIdentity)
	comparison := strings.Compare(prefix, cursorPrefix)
	if direction == RuntimeQueryDescending {
		switch {
		case comparison < 0:
			return query.Where("updated_at_ms <= ?", after.UpdatedAtMS), nil
		case comparison > 0:
			return query.Where("updated_at_ms < ?", after.UpdatedAtMS), nil
		default:
			return query.Where(
				"updated_at_ms < ? OR (updated_at_ms = ? AND "+identityColumn+" < ?)",
				after.UpdatedAtMS, after.UpdatedAtMS, cursorIdentity,
			), nil
		}
	}
	switch {
	case comparison > 0:
		return query.Where("updated_at_ms >= ?", after.UpdatedAtMS), nil
	case comparison < 0:
		return query.Where("updated_at_ms > ?", after.UpdatedAtMS), nil
	default:
		return query.Where(
			"updated_at_ms > ? OR (updated_at_ms = ? AND "+identityColumn+" > ?)",
			after.UpdatedAtMS, after.UpdatedAtMS, cursorIdentity,
		), nil
	}
}

func parseRuntimeSourceKey(value string) (RuntimeSourceKind, string, bool) {
	for prefix, kind := range map[string]RuntimeSourceKind{
		runtimeLocalSourcePrefix:  RuntimeSourceLocalFile,
		runtimeOnlineSourcePrefix: RuntimeSourceOnline,
	} {
		if strings.HasPrefix(value, prefix) {
			identity := strings.TrimPrefix(value, prefix)
			return kind, identity, identity != "" && len(identity) <= 512
		}
	}
	return "", "", false
}

func runtimeJobQuery(database *gorm.DB, filter RuntimeJobQuery) *gorm.DB {
	query := database.Model(&jobRunModel{})
	if filter.CurrentOnly {
		query = query.Where("state IN ? OR (state = ? AND resume_consumed_by_job_id IS NULL)",
			[]string{string(JobQueued), string(JobRunning)}, string(JobInterrupted))
	} else if len(filter.States) > 0 {
		query = query.Where("state IN ?", runtimeJobStateStrings(filter.States))
	}
	if len(filter.Phases) > 0 {
		query = query.Where("phase IN ?", runtimeJobPhaseStrings(filter.Phases))
	}
	return query
}

func runtimeJobCounts(query *gorm.DB, summary *RuntimeJobSummary) error {
	type countRow struct {
		State string
		Count int64
	}
	var rows []countRow
	if err := query.Session(&gorm.Session{}).Select("state, COUNT(*) AS count").
		Group("state").Scan(&rows).Error; err != nil {
		return err
	}
	for _, row := range rows {
		summary.Total += row.Count
		switch JobState(row.State) {
		case JobQueued:
			summary.Queued += row.Count
		case JobRunning:
			summary.Running += row.Count
		case JobSucceeded:
			summary.Succeeded += row.Count
		case JobFailed:
			summary.Failed += row.Count
		case JobCancelled:
			summary.Cancelled += row.Count
		case JobInterrupted:
			summary.Interrupted += row.Count
		default:
			return invalidRecord("stored job state is invalid")
		}
	}
	return nil
}

func runtimeJobRecords(
	ctx context.Context,
	connection *gorm.DB,
	models []jobRunModel,
) ([]RuntimeJobRecord, error) {
	records := make([]RuntimeJobRecord, len(models))
	jobIDs := make([]string, len(models))
	for index, model := range models {
		job, err := jobRunFromModel(model)
		if err != nil {
			return nil, err
		}
		records[index].Job = job
		jobIDs[index] = job.JobID
	}
	if len(jobIDs) == 0 {
		return records, nil
	}
	var taskModels []schedulerTaskModel
	if err := connection.WithContext(ctx).Where("target_id IN ?", jobIDs).
		Order("updated_at_ms DESC, task_id DESC").Find(&taskModels).Error; err != nil {
		return nil, err
	}
	tasksByJob := make(map[string]SchedulerTask, len(taskModels))
	for _, model := range taskModels {
		if _, exists := tasksByJob[model.TargetID]; exists {
			continue
		}
		task, err := schedulerTaskFromModel(model)
		if err != nil {
			return nil, err
		}
		tasksByJob[model.TargetID] = task
	}
	taskIDs := make([]string, 0, len(tasksByJob))
	for _, task := range tasksByJob {
		taskIDs = append(taskIDs, task.TaskID)
	}
	retriesByTask := make(map[string]SchedulerRetryState, len(taskIDs))
	if len(taskIDs) > 0 {
		var retryModels []schedulerRetryStateModel
		if err := connection.WithContext(ctx).Where("task_id IN ?", taskIDs).Find(&retryModels).Error; err != nil {
			return nil, err
		}
		for _, model := range retryModels {
			retry, err := schedulerRetryStateFromModel(model)
			if err != nil {
				return nil, err
			}
			retriesByTask[retry.TaskID] = retry
		}
	}
	for index := range records {
		task, exists := tasksByJob[records[index].Job.JobID]
		if !exists {
			continue
		}
		taskCopy := task
		records[index].Task = &taskCopy
		if retry, exists := retriesByTask[task.TaskID]; exists {
			retryCopy := retry
			records[index].Retry = &retryCopy
		}
	}
	return records, nil
}

func runtimeHealthQuery(database *gorm.DB, filter RuntimeHealthQuery) *gorm.DB {
	query := database.Model(&healthEventModel{})
	if filter.Active != nil {
		if *filter.Active {
			query = query.Where("resolved_at_ms IS NULL")
		} else {
			query = query.Where("resolved_at_ms IS NOT NULL")
		}
	}
	if len(filter.Severities) > 0 {
		query = query.Where("severity IN ?", runtimeHealthSeverityStrings(filter.Severities))
	}
	if len(filter.Domains) > 0 {
		query = query.Where("domain IN ?", runtimeHealthDomainStrings(filter.Domains))
	}
	return query
}

func runtimeHealthCounts(
	query *gorm.DB,
	summary *RuntimeHealthSummary,
	rowCountHook func(int) error,
) error {
	type countRow struct {
		Severity string
		Count    int64
	}
	aggregateRows := 0
	for _, active := range []bool{true, false} {
		bucket := query.Session(&gorm.Session{})
		if active {
			bucket = bucket.Where("resolved_at_ms IS NULL")
		} else {
			bucket = bucket.Where("resolved_at_ms IS NOT NULL")
		}
		var rows []countRow
		if err := bucket.Select("severity, COUNT(*) AS count").
			Group("severity").Scan(&rows).Error; err != nil {
			return err
		}
		aggregateRows += len(rows)
		for _, row := range rows {
			if err := addRuntimeHealthCount(summary, HealthSeverity(row.Severity), row.Count, active); err != nil {
				return err
			}
		}
	}
	if rowCountHook != nil {
		return rowCountHook(aggregateRows)
	}
	return nil
}

func addRuntimeHealthCount(
	summary *RuntimeHealthSummary,
	severity HealthSeverity,
	count int64,
	active bool,
) error {
	if count < 1 {
		return invalidRecord("stored health aggregate count is invalid")
	}
	summary.Total += count
	if active {
		summary.Active += count
	} else {
		summary.Resolved += count
	}
	switch severity {
	case HealthInfo:
		summary.Info += count
		if active {
			summary.ActiveInfo += count
		}
	case HealthWarning:
		summary.Warnings += count
		if active {
			summary.ActiveWarnings += count
		}
	case HealthError:
		summary.Errors += count
		if active {
			summary.ActiveErrors += count
		}
	case HealthCritical:
		summary.Critical += count
		if active {
			summary.ActiveCritical += count
		}
	default:
		return invalidRecord("stored health severity is invalid")
	}
	return nil
}

func runtimeCursorWhere(
	query *gorm.DB,
	valueColumn string,
	identityColumn string,
	value int64,
	identity string,
	direction RuntimeQueryDirection,
) *gorm.DB {
	operator := ">"
	if direction == RuntimeQueryDescending {
		operator = "<"
	}
	return query.Where(
		valueColumn+" "+operator+" ? OR ("+valueColumn+" = ? AND "+identityColumn+" "+operator+" ?)",
		value, value, identity,
	)
}

func runtimeOrder(
	query *gorm.DB,
	valueColumn string,
	identityColumn string,
	direction RuntimeQueryDirection,
) *gorm.DB {
	suffix := " ASC"
	if direction == RuntimeQueryDescending {
		suffix = " DESC"
	}
	return query.Order(valueColumn + suffix).Order(identityColumn + suffix)
}

func runtimeJobStateStrings(values []JobState) []string {
	result := make([]string, len(values))
	for index, value := range values {
		result[index] = string(value)
	}
	return result
}

func runtimeJobPhaseStrings(values []JobPhase) []string {
	result := make([]string, len(values))
	for index, value := range values {
		result[index] = string(value)
	}
	return result
}

func runtimeHealthSeverityStrings(values []HealthSeverity) []string {
	result := make([]string, len(values))
	for index, value := range values {
		result[index] = string(value)
	}
	return result
}

func runtimeHealthDomainStrings(values []HealthDomain) []string {
	result := make([]string, len(values))
	for index, value := range values {
		result[index] = string(value)
	}
	return result
}
