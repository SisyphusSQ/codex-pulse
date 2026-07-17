package runtimeinfo

import (
	"context"
	"errors"
	"math"

	basequery "github.com/SisyphusSQ/codex-pulse/internal/query"
	"github.com/SisyphusSQ/codex-pulse/internal/store"
)

const (
	MaxDataHealthRuntimePoints = 289
	MaxDataHealthActivityItems = 12
)

// DataHealth 返回最近 24 小时的 content-free 资源与运行聚合事实。
func (service *Service) DataHealth(ctx context.Context, evaluatedAtMS int64) (DataHealthResponse, error) {
	if service == nil || service.runtime == nil {
		return DataHealthResponse{}, ErrInvalidService
	}
	ctx, err := queryContext(ctx)
	if err != nil {
		return DataHealthResponse{}, err
	}
	if evaluatedAtMS < store.MetricsSnapshotWindowMS || evaluatedAtMS > basequery.JavaScriptMaxSafeInteger {
		return DataHealthResponse{}, basequery.NewValidationFailure("evaluatedAtMS", nil)
	}
	filter := store.MetricsSnapshotFilter{
		FromMS:  evaluatedAtMS - store.MetricsSnapshotWindowMS,
		UntilMS: evaluatedAtMS,
	}
	snapshot, err := service.runtime.MetricsSnapshot(ctx, filter)
	if err != nil {
		return DataHealthResponse{}, runtimeReadFailure(err)
	}
	if snapshot.FromMS != filter.FromMS || snapshot.UntilMS != filter.UntilMS || snapshot.RuntimeSamples == nil {
		return DataHealthResponse{}, basequery.NewUnavailableFailure(errors.New("data health snapshot is inconsistent"))
	}
	response, err := mapDataHealth(snapshot, evaluatedAtMS)
	if err != nil {
		return DataHealthResponse{}, err
	}
	currentJobs, recentJobs, err := service.dataHealthJobs(ctx, filter.FromMS, evaluatedAtMS)
	if err != nil {
		return DataHealthResponse{}, err
	}
	openEvents, recentEvents, err := service.dataHealthEvents(ctx, filter.FromMS, evaluatedAtMS)
	if err != nil {
		return DataHealthResponse{}, err
	}
	response.CurrentJobs = currentJobs
	response.RecentJobs = recentJobs
	response.OpenEvents = openEvents
	response.RecentEvents = recentEvents
	return response, nil
}

func (service *Service) dataHealthJobs(
	ctx context.Context,
	fromMS int64,
	evaluatedAtMS int64,
) ([]JobItem, []JobItem, error) {
	currentPage, err := service.runtime.RuntimeJobPage(ctx, store.RuntimeJobQuery{
		CurrentOnly: true, Limit: MaxDataHealthActivityItems, Direction: store.RuntimeQueryDescending,
	})
	if err != nil {
		return nil, nil, runtimeReadFailure(err)
	}
	if len(currentPage.Records) > MaxDataHealthActivityItems {
		return nil, nil, basequery.NewUnavailableFailure(errors.New("data health current job page is inconsistent"))
	}
	current, err := mapDataHealthJobRecords(currentPage.Records, fromMS, evaluatedAtMS, true)
	if err != nil {
		return nil, nil, basequery.NewUnavailableFailure(err)
	}
	remaining := MaxDataHealthActivityItems - len(current)
	if remaining <= 0 {
		return current, []JobItem{}, nil
	}
	recentPage, err := service.runtime.RuntimeJobPage(ctx, store.RuntimeJobQuery{
		States: []store.JobState{store.JobSucceeded, store.JobFailed, store.JobCancelled},
		Limit:  remaining, Direction: store.RuntimeQueryDescending,
	})
	if err != nil {
		return nil, nil, runtimeReadFailure(err)
	}
	if len(recentPage.Records) > remaining {
		return nil, nil, basequery.NewUnavailableFailure(errors.New("data health recent job page is inconsistent"))
	}
	recent, err := mapDataHealthJobRecords(recentPage.Records, fromMS, evaluatedAtMS, false)
	if err != nil {
		return nil, nil, basequery.NewUnavailableFailure(err)
	}
	return current, recent, nil
}

func mapDataHealthJobRecords(
	records []store.RuntimeJobRecord,
	fromMS int64,
	evaluatedAtMS int64,
	current bool,
) ([]JobItem, error) {
	items := make([]JobItem, 0, len(records))
	for _, record := range records {
		state := record.Job.State
		isCurrent := state == store.JobQueued || state == store.JobRunning || state == store.JobInterrupted
		if isCurrent != current || record.Job.UpdatedAtMS > evaluatedAtMS {
			return nil, errors.New("data health job page is inconsistent")
		}
		if !current && record.Job.UpdatedAtMS < fromMS {
			continue
		}
		item, err := mapJobItem(record)
		if err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, nil
}

func (service *Service) dataHealthEvents(
	ctx context.Context,
	fromMS int64,
	evaluatedAtMS int64,
) ([]HealthItem, []HealthItem, error) {
	active := true
	openPage, err := service.runtime.RuntimeHealthPage(ctx, store.RuntimeHealthQuery{
		Active: &active, Limit: MaxDataHealthActivityItems, Direction: store.RuntimeQueryDescending,
	})
	if err != nil {
		return nil, nil, runtimeReadFailure(err)
	}
	if len(openPage.Records) > MaxDataHealthActivityItems {
		return nil, nil, basequery.NewUnavailableFailure(errors.New("data health open event page is inconsistent"))
	}
	open, err := mapDataHealthEventRecords(openPage.Records, fromMS, evaluatedAtMS, true)
	if err != nil {
		return nil, nil, basequery.NewUnavailableFailure(err)
	}
	remaining := MaxDataHealthActivityItems - len(open)
	if remaining <= 0 {
		return open, []HealthItem{}, nil
	}
	active = false
	recentPage, err := service.runtime.RuntimeHealthPage(ctx, store.RuntimeHealthQuery{
		Active: &active, Limit: remaining, Direction: store.RuntimeQueryDescending,
	})
	if err != nil {
		return nil, nil, runtimeReadFailure(err)
	}
	if len(recentPage.Records) > remaining {
		return nil, nil, basequery.NewUnavailableFailure(errors.New("data health recent event page is inconsistent"))
	}
	recent, err := mapDataHealthEventRecords(recentPage.Records, fromMS, evaluatedAtMS, false)
	if err != nil {
		return nil, nil, basequery.NewUnavailableFailure(err)
	}
	return open, recent, nil
}

func mapDataHealthEventRecords(
	records []store.HealthEvent,
	fromMS int64,
	evaluatedAtMS int64,
	active bool,
) ([]HealthItem, error) {
	items := make([]HealthItem, 0, len(records))
	for _, record := range records {
		isActive := record.ResolvedAtMS == nil
		if isActive != active || record.LastSeenAtMS > evaluatedAtMS {
			return nil, errors.New("data health event page is inconsistent")
		}
		if !active && record.LastSeenAtMS < fromMS {
			continue
		}
		item, err := mapHealthItem(record)
		if err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, nil
}

func mapDataHealth(snapshot store.MetricsSnapshot, evaluatedAtMS int64) (DataHealthResponse, error) {
	meta, err := basequery.NewResponseMeta(basequery.ResponseComplete, nil, nil)
	if err != nil {
		return DataHealthResponse{}, err
	}
	evaluatedAt, err := knownNumeric(evaluatedAtMS, basequery.NumericMilliseconds)
	if err != nil {
		return DataHealthResponse{}, basequery.NewUnavailableFailure(err)
	}
	from, err := knownNumeric(snapshot.FromMS, basequery.NumericMilliseconds)
	if err != nil {
		return DataHealthResponse{}, basequery.NewUnavailableFailure(err)
	}
	until, err := knownNumeric(snapshot.UntilMS, basequery.NumericMilliseconds)
	if err != nil {
		return DataHealthResponse{}, basequery.NewUnavailableFailure(err)
	}
	runtimePoints, err := mapDataHealthRuntime(snapshot)
	if err != nil {
		return DataHealthResponse{}, basequery.NewUnavailableFailure(err)
	}
	scheduler, err := mapDataHealthScheduler(snapshot.Scheduler)
	if err != nil {
		return DataHealthResponse{}, basequery.NewUnavailableFailure(err)
	}
	jobs, err := mapDataHealthJobs(snapshot.Jobs)
	if err != nil {
		return DataHealthResponse{}, basequery.NewUnavailableFailure(err)
	}
	sources, err := mapDataHealthSources(snapshot.Sources)
	if err != nil {
		return DataHealthResponse{}, basequery.NewUnavailableFailure(err)
	}
	response := DataHealthResponse{
		Meta: meta, EvaluatedAtMS: evaluatedAt,
		Window:  DataHealthWindow{FromMS: from, UntilMS: until},
		Runtime: runtimePoints, Scheduler: scheduler, Jobs: jobs, Sources: sources,
	}
	if len(runtimePoints) > 0 {
		latest := runtimePoints[0]
		response.Latest = &latest
	}
	return response, nil
}

func mapDataHealthRuntime(snapshot store.MetricsSnapshot) ([]DataHealthRuntimePoint, error) {
	var previous int64
	for index, sample := range snapshot.RuntimeSamples {
		if sample.CapturedAtMS < snapshot.FromMS || sample.CapturedAtMS >= snapshot.UntilMS ||
			index > 0 && sample.CapturedAtMS >= previous || !validCPUPercent(sample.CPUPercent) {
			return nil, errors.New("data health runtime sample is invalid")
		}
		previous = sample.CapturedAtMS
	}
	indices := dataHealthRuntimeIndices(len(snapshot.RuntimeSamples))
	points := make([]DataHealthRuntimePoint, len(indices))
	for pointIndex, sampleIndex := range indices {
		sample := snapshot.RuntimeSamples[sampleIndex]
		values := []struct {
			value int64
			unit  basequery.NumericUnit
		}{
			{sample.CapturedAtMS, basequery.NumericMilliseconds},
			{sample.RSSBytes, basequery.NumericBytes}, {sample.PeakRSSBytes, basequery.NumericBytes},
			{sample.DBBytes, basequery.NumericBytes}, {sample.WALBytes, basequery.NumericBytes},
			{sample.DiskFreeBytes, basequery.NumericBytes}, {sample.LiveQueueDepth, basequery.NumericCount},
			{sample.BackfillQueueDepth, basequery.NumericCount}, {sample.OldestLiveWaitMS, basequery.NumericMilliseconds},
			{sample.OldestBackfillWaitMS, basequery.NumericMilliseconds}, {sample.DroppedSamples, basequery.NumericCount},
		}
		mapped := make([]basequery.NumericValue, len(values))
		for valueIndex, value := range values {
			var err error
			mapped[valueIndex], err = knownNumeric(value.value, value.unit)
			if err != nil {
				return nil, err
			}
		}
		points[pointIndex] = DataHealthRuntimePoint{
			CapturedAtMS: mapped[0], CPUPercent: sample.CPUPercent,
			RSSBytes: mapped[1], PeakRSSBytes: mapped[2], DBBytes: mapped[3], WALBytes: mapped[4],
			DiskFreeBytes: mapped[5], LiveQueueDepth: mapped[6], BackfillQueueDepth: mapped[7],
			OldestLiveWaitMS: mapped[8], OldestBackfillWaitMS: mapped[9], DroppedSamples: mapped[10],
		}
	}
	return points, nil
}

func dataHealthRuntimeIndices(count int) []int {
	if count <= 0 {
		return []int{}
	}
	limit := count
	if limit > MaxDataHealthRuntimePoints {
		limit = MaxDataHealthRuntimePoints
	}
	indices := make([]int, limit)
	if limit == count {
		for index := range indices {
			indices[index] = index
		}
		return indices
	}
	for index := range indices {
		indices[index] = index * (count - 1) / (limit - 1)
	}
	return indices
}

func mapDataHealthScheduler(value store.SchedulerMetrics) (DataHealthScheduler, error) {
	values, err := mapKnownValues([]numericFact{
		{value.CycleCount, basequery.NumericCount}, {value.CompletedCycles, basequery.NumericCount},
		{value.YieldedCycles, basequery.NumericCount}, {value.FailedCycles, basequery.NumericCount},
		{value.InterruptedCycles, basequery.NumericCount}, {value.FilesScanned, basequery.NumericCount},
		{value.BytesRead, basequery.NumericBytes}, {value.ActiveMS, basequery.NumericMilliseconds},
		{value.MaxCycleActiveMS, basequery.NumericMilliseconds},
	})
	if err != nil {
		return DataHealthScheduler{}, err
	}
	lastProgress, err := optionalNumeric(value.LastProgressAtMS, basequery.NumericMilliseconds, basequery.UnknownNeverLoaded)
	if err != nil {
		return DataHealthScheduler{}, err
	}
	lastBackfill, err := optionalNumeric(value.LastBackfillProgressAtMS, basequery.NumericMilliseconds, basequery.UnknownNeverLoaded)
	if err != nil {
		return DataHealthScheduler{}, err
	}
	return DataHealthScheduler{
		CycleCount: values[0], CompletedCycles: values[1], YieldedCycles: values[2],
		FailedCycles: values[3], InterruptedCycles: values[4], FilesScanned: values[5],
		BytesRead: values[6], ActiveMS: values[7], MaxCycleActiveMS: values[8],
		LastProgressAtMS: lastProgress, LastBackfillProgressAtMS: lastBackfill,
	}, nil
}

func mapDataHealthJobs(value store.JobMetrics) (DataHealthJobs, error) {
	values, err := mapKnownValues([]numericFact{
		{value.Queued, basequery.NumericCount}, {value.Running, basequery.NumericCount},
		{value.Interrupted, basequery.NumericCount}, {value.Succeeded, basequery.NumericCount},
		{value.Failed, basequery.NumericCount}, {value.Cancelled, basequery.NumericCount},
		{value.DurationCount, basequery.NumericCount}, {value.DurationTotalMS, basequery.NumericMilliseconds},
		{value.DurationMaxMS, basequery.NumericMilliseconds},
	})
	if err != nil {
		return DataHealthJobs{}, err
	}
	return DataHealthJobs{
		Queued: values[0], Running: values[1], Interrupted: values[2], Succeeded: values[3],
		Failed: values[4], Cancelled: values[5], DurationCount: values[6],
		DurationTotalMS: values[7], DurationMaxMS: values[8],
	}, nil
}

func mapDataHealthSources(value store.SourceMetrics) (DataHealthSources, error) {
	values, err := mapKnownValues([]numericFact{
		{value.Total, basequery.NumericCount}, {value.Unknown, basequery.NumericCount},
		{value.Current, basequery.NumericCount}, {value.Stale, basequery.NumericCount},
		{value.Unavailable, basequery.NumericCount}, {value.ConsecutiveFailures, basequery.NumericCount},
		{value.MaxConsecutiveFailures, basequery.NumericCount}, {value.Attempts, basequery.NumericCount},
		{value.SucceededAttempts, basequery.NumericCount}, {value.FailedAttempts, basequery.NumericCount},
		{value.CancelledAttempts, basequery.NumericCount}, {value.ResponseBytes, basequery.NumericBytes},
	})
	if err != nil {
		return DataHealthSources{}, err
	}
	lastAttempt, err := optionalNumeric(value.LastAttemptAtMS, basequery.NumericMilliseconds, basequery.UnknownNeverLoaded)
	if err != nil {
		return DataHealthSources{}, err
	}
	lastSuccess, err := optionalNumeric(value.LastSuccessAtMS, basequery.NumericMilliseconds, basequery.UnknownNeverLoaded)
	if err != nil {
		return DataHealthSources{}, err
	}
	nextRetry, err := optionalNumeric(value.NextRetryAtMS, basequery.NumericMilliseconds, basequery.UnknownNotApplicable)
	if err != nil {
		return DataHealthSources{}, err
	}
	return DataHealthSources{
		Total: values[0], Unknown: values[1], Current: values[2], Stale: values[3],
		Unavailable: values[4], ConsecutiveFailures: values[5], MaxConsecutiveFailures: values[6],
		Attempts: values[7], SucceededAttempts: values[8], FailedAttempts: values[9],
		CancelledAttempts: values[10], ResponseBytes: values[11], LastAttemptAtMS: lastAttempt,
		LastSuccessAtMS: lastSuccess, NextRetryAtMS: nextRetry,
	}, nil
}

type numericFact struct {
	value int64
	unit  basequery.NumericUnit
}

func mapKnownValues(facts []numericFact) ([]basequery.NumericValue, error) {
	values := make([]basequery.NumericValue, len(facts))
	for index, fact := range facts {
		value, err := knownNumeric(fact.value, fact.unit)
		if err != nil {
			return nil, err
		}
		values[index] = value
	}
	return values, nil
}

func validCPUPercent(value float64) bool {
	return !math.IsNaN(value) && !math.IsInf(value, 0) && value >= 0 && value <= 102_400
}
