package runtimeinfo

import (
	"context"
	"errors"

	basequery "github.com/SisyphusSQ/codex-pulse/internal/query"
	"github.com/SisyphusSQ/codex-pulse/internal/store"
)

const jobCursorEndpoint = "runtime-jobs"

func (service *Service) ListJobs(
	ctx context.Context,
	request basequery.Request,
) (JobListResponse, error) {
	if service == nil || service.runtime == nil {
		return JobListResponse{}, ErrInvalidService
	}
	ctx, err := queryContext(ctx)
	if err != nil {
		return JobListResponse{}, err
	}
	validated, err := validateRuntimeRequest(ctx, service.jobSpec, request, "updatedAt")
	if err != nil {
		return JobListResponse{}, err
	}
	primary := validated.Sort[0]
	filter := store.RuntimeJobQuery{
		Limit: validated.Page.Limit, Direction: runtimeDirection(primary.Direction),
	}
	if validated.Page.Cursor != nil {
		value, identity, err := decodeCursor(
			*validated.Page.Cursor, jobCursorEndpoint, primary.Field, primary.Direction,
		)
		if err != nil {
			return JobListResponse{}, err
		}
		filter.After = &store.RuntimeJobCursor{UpdatedAtMS: value, JobID: identity}
	}
	if err := applyJobFilters(validated.Filters, &filter); err != nil {
		return JobListResponse{}, err
	}
	page, err := service.runtime.RuntimeJobPage(ctx, filter)
	if err != nil {
		return JobListResponse{}, runtimeReadFailure(err)
	}
	return mapJobList(page, validated.Page.Limit, primary)
}

func (service *Service) Job(
	ctx context.Context,
	request JobDetailRequest,
) (JobDetailResponse, error) {
	if service == nil || service.runtime == nil {
		return JobDetailResponse{}, ErrInvalidService
	}
	ctx, err := queryContext(ctx)
	if err != nil {
		return JobDetailResponse{}, err
	}
	if !validOpaqueIdentity(request.JobID) {
		return JobDetailResponse{}, basequery.NewValidationFailure("jobId", nil)
	}
	record, err := service.runtime.RuntimeJob(ctx, request.JobID)
	if err != nil {
		return JobDetailResponse{}, runtimeReadFailure(err)
	}
	item, err := mapJobItem(record)
	if err != nil {
		return JobDetailResponse{}, basequery.NewUnavailableFailure(err)
	}
	meta, err := basequery.NewResponseMeta(basequery.ResponseComplete, nil, nil)
	if err != nil {
		return JobDetailResponse{}, err
	}
	return JobDetailResponse{Meta: meta, Item: item}, nil
}

func applyJobFilters(filters []basequery.FilterTerm, target *store.RuntimeJobQuery) error {
	seenFields := make(map[string]struct{}, len(filters))
	seenStates := make(map[store.JobState]struct{})
	seenPhases := make(map[store.JobPhase]struct{})
	for _, filter := range filters {
		if _, exists := seenFields[filter.Field]; exists {
			return basequery.NewValidationFailure("filters", nil)
		}
		seenFields[filter.Field] = struct{}{}
		for _, value := range filter.Values {
			switch filter.Field {
			case "state":
				state := store.JobState(value)
				if !validJobState(state) {
					return basequery.NewValidationFailure("filters.values", nil)
				}
				if _, exists := seenStates[state]; exists {
					return basequery.NewValidationFailure("filters.values", nil)
				}
				seenStates[state] = struct{}{}
				target.States = append(target.States, state)
			case "phase":
				phase := store.JobPhase(value)
				if !validJobPhase(phase) {
					return basequery.NewValidationFailure("filters.values", nil)
				}
				if _, exists := seenPhases[phase]; exists {
					return basequery.NewValidationFailure("filters.values", nil)
				}
				seenPhases[phase] = struct{}{}
				target.Phases = append(target.Phases, phase)
			default:
				return basequery.NewValidationFailure("filters.field", nil)
			}
		}
	}
	return nil
}

func mapJobList(
	page store.RuntimeJobPage,
	limit int,
	primary basequery.SortTerm,
) (JobListResponse, error) {
	if page.MatchedCount < 0 || page.MatchedCount != page.Summary.Total ||
		len(page.Records) > limit || (page.NextCursor != nil && len(page.Records) == 0) {
		return JobListResponse{}, basequery.NewUnavailableFailure(errors.New("job page is inconsistent"))
	}
	items := make([]JobItem, len(page.Records))
	for index, record := range page.Records {
		item, err := mapJobItem(record)
		if err != nil {
			return JobListResponse{}, basequery.NewUnavailableFailure(err)
		}
		items[index] = item
	}
	var nextCursor *string
	var err error
	if page.NextCursor != nil {
		nextCursor, err = encodeCursor(
			jobCursorEndpoint, primary.Field, primary.Direction,
			page.NextCursor.UpdatedAtMS, page.NextCursor.JobID,
		)
		if err != nil {
			return JobListResponse{}, basequery.NewUnavailableFailure(err)
		}
	}
	meta, err := basequery.NewResponseMeta(basequery.ResponseComplete, &basequery.PageInfo{
		Limit: limit, HasMore: nextCursor != nil, NextCursor: nextCursor,
	}, nil)
	if err != nil {
		return JobListResponse{}, err
	}
	matched, err := knownNumeric(page.MatchedCount, basequery.NumericCount)
	if err != nil {
		return JobListResponse{}, basequery.NewUnavailableFailure(err)
	}
	summary, err := mapJobSummary(page.Summary)
	if err != nil {
		return JobListResponse{}, basequery.NewUnavailableFailure(err)
	}
	return JobListResponse{Meta: meta, Items: items, MatchedCount: matched, Summary: summary}, nil
}

func mapJobItem(record store.RuntimeJobRecord) (JobItem, error) {
	job := record.Job
	if job.JobID == "" || job.JobType == "" || job.RequestedBy == "" ||
		!validJobState(job.State) || !validJobPhase(job.Phase) || job.CreatedAtMS < 0 ||
		job.UpdatedAtMS < job.CreatedAtMS || !validOpaqueIdentity(job.JobID) ||
		!validPublicToken(job.JobType, 128) || !validPublicToken(job.RequestedBy, 128) ||
		!validRuntimeErrorPointer(job.ErrorClass) {
		return JobItem{}, errors.New("job record is invalid")
	}
	if !validJobShape(job) {
		return JobItem{}, errors.New("job lifecycle shape is invalid")
	}
	created, err := knownNumeric(job.CreatedAtMS, basequery.NumericMilliseconds)
	if err != nil {
		return JobItem{}, err
	}
	started, err := optionalNumeric(job.StartedAtMS, basequery.NumericMilliseconds, basequery.UnknownNeverLoaded)
	if err != nil {
		return JobItem{}, err
	}
	finished, err := optionalNumeric(job.FinishedAtMS, basequery.NumericMilliseconds, basequery.UnknownNotApplicable)
	if err != nil {
		return JobItem{}, err
	}
	lastSuccessValue := (*int64)(nil)
	lastSuccessReason := basequery.UnknownNotApplicable
	if job.State == store.JobSucceeded {
		lastSuccessValue = job.FinishedAtMS
		lastSuccessReason = basequery.UnknownUnavailable
	}
	lastSuccess, err := optionalNumeric(lastSuccessValue, basequery.NumericMilliseconds, lastSuccessReason)
	if err != nil {
		return JobItem{}, err
	}
	progressCurrent, err := optionalNumeric(job.ProgressCurrent, basequery.NumericCount, basequery.UnknownNotComputed)
	if err != nil {
		return JobItem{}, err
	}
	progressTotal, err := optionalNumeric(job.ProgressTotal, basequery.NumericCount, basequery.UnknownNotComputed)
	if err != nil {
		return JobItem{}, err
	}
	updated, err := knownNumeric(job.UpdatedAtMS, basequery.NumericMilliseconds)
	if err != nil {
		return JobItem{}, err
	}
	failureCountValue := int64(0)
	var nextRetry *int64
	action := jobRecovery(record)
	if record.Retry != nil {
		if record.Task == nil || record.Retry.TaskID != record.Task.TaskID ||
			record.Retry.FailureCount < 0 || !validOpaqueIdentity(record.Retry.TaskID) ||
			!validRetryState(*record.Retry) {
			return JobItem{}, errors.New("job retry record is invalid")
		}
		failureCountValue = record.Retry.FailureCount
		nextRetry = record.Retry.NextRetryAtMS
	}
	failureCount, err := knownNumeric(failureCountValue, basequery.NumericCount)
	if err != nil {
		return JobItem{}, err
	}
	nextRetryValue, err := optionalNumeric(nextRetry, basequery.NumericMilliseconds, basequery.UnknownNotApplicable)
	if err != nil {
		return JobItem{}, err
	}
	item := JobItem{
		JobID: job.JobID, JobType: job.JobType, RequestedBy: job.RequestedBy,
		State: string(job.State), Phase: string(job.Phase), CreatedAtMS: created,
		StartedAtMS: started, FinishedAtMS: finished, LastSuccessAtMS: lastSuccess,
		Progress:     JobProgress{Current: progressCurrent, Total: progressTotal},
		FailureCount: failureCount, NextRetryAtMS: nextRetryValue,
		ErrorClass: runtimeErrorString(job.ErrorClass), UpdatedAtMS: updated,
		RecoveryAction: action,
	}
	if job.SourceFileID != nil {
		if !validOpaqueIdentity(*job.SourceFileID) {
			return JobItem{}, errors.New("job source identity is invalid")
		}
		item.SourceKey = cloneString("local_file:" + *job.SourceFileID)
	}
	return item, nil
}

func mapJobSummary(value store.RuntimeJobSummary) (JobSummary, error) {
	values := []int64{
		value.Total, value.Queued, value.Running, value.Succeeded,
		value.Failed, value.Cancelled, value.Interrupted,
	}
	if value.Total < 0 || value.Queued+value.Running+value.Succeeded+value.Failed+
		value.Cancelled+value.Interrupted != value.Total {
		return JobSummary{}, errors.New("job summary is invalid")
	}
	mapped := make([]basequery.NumericValue, len(values))
	for index, current := range values {
		value, err := knownNumeric(current, basequery.NumericCount)
		if err != nil {
			return JobSummary{}, err
		}
		mapped[index] = value
	}
	return JobSummary{
		Total: mapped[0], Queued: mapped[1], Running: mapped[2], Succeeded: mapped[3],
		Failed: mapped[4], Cancelled: mapped[5], Interrupted: mapped[6],
	}, nil
}

func validJobState(value store.JobState) bool {
	switch value {
	case store.JobQueued, store.JobRunning, store.JobSucceeded, store.JobFailed,
		store.JobCancelled, store.JobInterrupted:
		return true
	default:
		return false
	}
}

func validJobPhase(value store.JobPhase) bool {
	switch value {
	case store.JobPhaseDiscover, store.JobPhaseFastBootstrap, store.JobPhaseHistoryBackfill,
		store.JobPhaseReconcile, store.JobPhaseLive, store.JobPhaseMaintenance:
		return true
	default:
		return false
	}
}

func validJobShape(job store.JobRun) bool {
	if job.StartedAtMS != nil && (*job.StartedAtMS < job.CreatedAtMS || *job.StartedAtMS > job.UpdatedAtMS) ||
		job.FinishedAtMS != nil && (*job.FinishedAtMS < job.CreatedAtMS || *job.FinishedAtMS > job.UpdatedAtMS) ||
		job.StartedAtMS != nil && job.FinishedAtMS != nil && *job.FinishedAtMS < *job.StartedAtMS ||
		job.ProgressCurrent != nil && *job.ProgressCurrent < 0 ||
		job.ProgressTotal != nil && *job.ProgressTotal < 0 ||
		job.ProgressCurrent != nil && job.ProgressTotal != nil && *job.ProgressCurrent > *job.ProgressTotal {
		return false
	}
	switch job.State {
	case store.JobQueued:
		return job.StartedAtMS == nil && job.FinishedAtMS == nil && job.ErrorClass == nil
	case store.JobRunning:
		return job.StartedAtMS != nil && job.FinishedAtMS == nil && job.ErrorClass == nil
	case store.JobSucceeded:
		return job.StartedAtMS != nil && job.FinishedAtMS != nil && job.ErrorClass == nil
	case store.JobFailed:
		return job.StartedAtMS != nil && job.FinishedAtMS != nil && job.ErrorClass != nil
	case store.JobCancelled, store.JobInterrupted:
		return job.FinishedAtMS != nil
	default:
		return false
	}
}
