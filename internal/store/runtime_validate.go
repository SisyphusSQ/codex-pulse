package store

import (
	"context"
	"errors"
	"net/url"

	"github.com/SisyphusSQ/codex-pulse/internal/runtimeclock"
	storesqlite "github.com/SisyphusSQ/codex-pulse/internal/store/sqlite"
)

// ClassifyRuntimeError 将运行错误降维为可持久化 class，不返回或保存原始错误正文。
func ClassifyRuntimeError(err error) RuntimeErrorClass {
	switch {
	case errors.Is(err, context.DeadlineExceeded):
		return RuntimeErrorTimeout
	case errors.Is(err, context.Canceled), errors.Is(err, storesqlite.ErrCanceled):
		return RuntimeErrorCanceled
	case errors.Is(err, storesqlite.ErrBusy), errors.Is(err, storesqlite.ErrQueueFull):
		return RuntimeErrorBusy
	case errors.Is(err, storesqlite.ErrDiskFull):
		return RuntimeErrorDiskFull
	case errors.Is(err, storesqlite.ErrReadOnly):
		return RuntimeErrorReadOnly
	case errors.Is(err, storesqlite.ErrPermission):
		return RuntimeErrorPermission
	case errors.Is(err, storesqlite.ErrIO):
		return RuntimeErrorIO
	case errors.Is(err, storesqlite.ErrCorrupt):
		return RuntimeErrorCorrupt
	case errors.Is(err, storesqlite.ErrClosing), errors.Is(err, storesqlite.ErrClosed):
		return RuntimeErrorUnavailable
	case errors.Is(err, storesqlite.ErrInvalidConfig), errors.Is(err, storesqlite.ErrInvalidPath):
		return RuntimeErrorInvalid
	default:
		return RuntimeErrorUnknown
	}
}

func validateSourceFile(file SourceFile) error {
	if file.SourceFileID == "" || file.Provider == "" || file.CurrentPath == "" ||
		file.DeviceID == "" || file.ParserVersion == "" {
		return invalidRecord("source file identity is incomplete")
	}
	if file.Inode < 0 || file.SizeBytes < 0 || file.MTimeNS < 0 || file.ParsedOffset < 0 ||
		file.ParsedOffset > file.SizeBytes || file.ActiveGeneration < 0 || file.UpdatedAtMS < 0 {
		return invalidRecord("source file position or timestamps are invalid")
	}
	if file.SessionID != nil && *file.SessionID == "" {
		return invalidRecord("source file session ID must be nil or non-empty")
	}
	if file.LastScannedAtMS != nil && *file.LastScannedAtMS < 0 {
		return invalidRecord("source file scan timestamp must not be negative")
	}
	if !validSourceFileState(file.State) {
		return invalidRecord("source file state is invalid")
	}
	return validateRuntimeErrorClass(file.LastErrorClass)
}

func validateSourceState(state SourceState) error {
	if state.SourceInstanceID == "" || state.SourceType == "" || state.ScopeKey == "" {
		return invalidRecord("source state identity is incomplete")
	}
	for _, value := range []*int64{state.LastAttemptAtMS, state.LastSuccessAtMS, state.NextDueAtMS} {
		if value != nil && *value < 0 {
			return invalidRecord("source state timestamp must not be negative")
		}
	}
	if state.LastAttemptAtMS != nil && state.LastSuccessAtMS != nil &&
		*state.LastSuccessAtMS > *state.LastAttemptAtMS {
		return invalidRecord("source success is newer than the last attempt")
	}
	if state.ConsecutiveFailures < 0 || state.CursorVersion < 0 || state.UpdatedAtMS < 0 {
		return invalidRecord("source state counters are invalid")
	}
	if !validSourceFreshness(state.FreshnessState) {
		return invalidRecord("source freshness state is invalid")
	}
	if err := validateRuntimeErrorClass(state.LastErrorClass); err != nil {
		return err
	}
	return validateSourceFailureCode(state.LastFailureCode)
}

func validateSourceAttempt(attempt SourceAttempt) error {
	if attempt.RequestID == "" || attempt.SourceInstanceID == "" {
		return invalidRecord("source attempt identity is incomplete")
	}
	if attempt.StartedAtMS < 0 || attempt.FinishedAtMS < attempt.StartedAtMS {
		return invalidRecord("source attempt timestamps are invalid")
	}
	if !validSourceAttemptOutcome(attempt.Outcome) {
		return invalidRecord("source attempt outcome is invalid")
	}
	if attempt.HTTPStatus != nil && (*attempt.HTTPStatus < 100 || *attempt.HTTPStatus > 599) {
		return invalidRecord("source attempt HTTP status is invalid")
	}
	if attempt.PayloadSHA256 != nil {
		if err := validateSHA256Digest(*attempt.PayloadSHA256, "source attempt payload SHA-256"); err != nil {
			return err
		}
	}
	if attempt.AttemptCount < 0 || attempt.AttemptCount > 3 || attempt.ResponseBytes < 0 ||
		attempt.RetryAtMS != nil && *attempt.RetryAtMS < attempt.FinishedAtMS {
		return invalidRecord("source attempt metrics are invalid")
	}
	if err := validateRuntimeErrorClass(attempt.ErrorClass); err != nil {
		return err
	}
	if err := validateSourceFailureCode(attempt.FailureCode); err != nil {
		return err
	}
	if attempt.Outcome == SourceAttemptSucceeded && (attempt.ErrorClass != nil || attempt.FailureCode != nil || attempt.RetryAtMS != nil) {
		return invalidRecord("successful source attempt must not have failure state")
	}
	if attempt.Outcome == SourceAttemptFailed && (attempt.ErrorClass == nil || attempt.FailureCode == nil) {
		return invalidRecord("failed source attempt requires typed failure state")
	}
	if attempt.Outcome == SourceAttemptCancelled &&
		(attempt.ErrorClass == nil || *attempt.ErrorClass != RuntimeErrorCanceled ||
			attempt.FailureCode == nil || *attempt.FailureCode != SourceFailureCancelled || attempt.RetryAtMS != nil) {
		return invalidRecord("cancelled source attempt has invalid failure state")
	}
	if attempt.RetryAtMS != nil && (attempt.FailureCode == nil || *attempt.FailureCode != SourceFailureHTTP429) {
		return invalidRecord("only rate-limit failure may carry retry time")
	}
	return nil
}

func validateRuntimeLimit(limit int) (int, error) {
	if limit == 0 {
		limit = 100
	}
	if limit < 1 || limit > 500 {
		return 0, invalidRecord("runtime query limit must be between 1 and 500")
	}
	return limit, nil
}

func validateRuntimeErrorClass(value *RuntimeErrorClass) error {
	if value != nil && !validRuntimeErrorClass(*value) {
		return invalidRecord("runtime error class is invalid")
	}
	return nil
}

func validateSourceFailureCode(value *SourceFailureCode) error {
	if value != nil && !validSourceFailureCode(*value) {
		return invalidRecord("source failure code is invalid")
	}
	return nil
}

func validSourceFailureCode(value SourceFailureCode) bool {
	switch value {
	case SourceFailureNetworkUnavailable, SourceFailureTimeout, SourceFailureAuthRequired,
		SourceFailureHTTP429, SourceFailureServerError, SourceFailureSchemaIncompatible,
		SourceFailureCancelled:
		return true
	default:
		return false
	}
}

func validRuntimeErrorClass(value RuntimeErrorClass) bool {
	switch value {
	case RuntimeErrorCanceled, RuntimeErrorBusy, RuntimeErrorDiskFull, RuntimeErrorReadOnly,
		RuntimeErrorPermission, RuntimeErrorIO, RuntimeErrorCorrupt, RuntimeErrorTimeout,
		RuntimeErrorUnavailable, RuntimeErrorInvalid, RuntimeErrorUnknown:
		return true
	default:
		return false
	}
}

func validSourceFileState(value SourceFileState) bool {
	switch value {
	case SourceFileDiscovered, SourceFileActive, SourceFileCompleted, SourceFileUnavailable, SourceFileFailed:
		return true
	default:
		return false
	}
}

func validSourceFreshness(value SourceFreshness) bool {
	switch value {
	case SourceFreshnessUnknown, SourceFreshnessCurrent, SourceFreshnessStale, SourceFreshnessUnavailable:
		return true
	default:
		return false
	}
}

func validSourceAttemptOutcome(value SourceAttemptOutcome) bool {
	switch value {
	case SourceAttemptSucceeded, SourceAttemptFailed, SourceAttemptCancelled:
		return true
	default:
		return false
	}
}

func validateNewJobRun(job JobRun) error {
	if job.JobID == "" || job.JobType == "" || job.RequestedBy == "" {
		return invalidRecord("job identity is incomplete")
	}
	if job.Priority < 0 || job.CreatedAtMS < 0 || job.UpdatedAtMS < job.CreatedAtMS ||
		job.CreatedAtMS > runtimeclock.MaxContinuableTimestampMS ||
		job.UpdatedAtMS > runtimeclock.MaxContinuableTimestampMS {
		return invalidRecord("job priority or timestamps are invalid")
	}
	if job.State != JobQueued || job.StartedAtMS != nil || job.FinishedAtMS != nil || job.ErrorClass != nil ||
		job.ResumeConsumedByJobID != nil {
		return invalidRecord("new job must be a non-started queued job")
	}
	if !validJobPhase(job.Phase) {
		return invalidRecord("job phase is invalid")
	}
	if err := validateOptionalStrings(job.SourceFileID, job.ResumeOfJobID); err != nil {
		return err
	}
	if err := validateJobCursor(job.ResumeCursor); err != nil {
		return err
	}
	return validateJobProgress(job.ProgressCurrent, job.ProgressTotal)
}

func validateJobTransition(transition JobTransition) error {
	if transition.JobID == "" || transition.AtMS < 0 || transition.AtMS > runtimeclock.MaxTimestampMS ||
		!validJobState(transition.ExpectedState) ||
		!validJobState(transition.State) || !validJobPhase(transition.Phase) {
		return invalidRecord("job transition identity, state, phase, or time is invalid")
	}
	if err := validateJobCursor(transition.ResumeCursor); err != nil {
		return err
	}
	if err := validateJobProgress(transition.ProgressCurrent, transition.ProgressTotal); err != nil {
		return err
	}
	if err := validateRuntimeErrorClass(transition.ErrorClass); err != nil {
		return err
	}
	if transition.State == JobRunning && transition.AtMS > runtimeclock.MaxInProgressTimestampMS {
		return invalidRecord("running job timestamp has no terminal successor")
	}
	switch transition.State {
	case JobRunning, JobSucceeded:
		if transition.ErrorClass != nil {
			return invalidRecord("running or succeeded job must not have an error class")
		}
	case JobFailed:
		if transition.ErrorClass == nil {
			return invalidRecord("failed job requires an error class")
		}
	}
	return nil
}

func validateJobProgress(current, total *int64) error {
	if current != nil && *current < 0 || total != nil && *total < 0 {
		return invalidRecord("job progress must not be negative")
	}
	if current != nil && total != nil && *current > *total {
		return invalidRecord("job progress exceeds total")
	}
	return nil
}

func validJobState(value JobState) bool {
	switch value {
	case JobQueued, JobRunning, JobSucceeded, JobFailed, JobCancelled, JobInterrupted:
		return true
	default:
		return false
	}
}

func validJobPhase(value JobPhase) bool {
	_, ok := jobPhaseRank(value)
	return ok
}

func jobPhaseRank(value JobPhase) (int, bool) {
	switch value {
	case JobPhaseDiscover:
		return 0, true
	case JobPhaseFastBootstrap:
		return 1, true
	case JobPhaseHistoryBackfill:
		return 2, true
	case JobPhaseReconcile:
		return 3, true
	case JobPhaseLive:
		return 4, true
	case JobPhaseMaintenance:
		return 5, true
	default:
		return 0, false
	}
}

func validateHealthObservation(observation HealthObservation) error {
	if observation.EventID == "" || observation.Domain == "" || observation.Code == "" ||
		observation.ObservedAtMS < 0 {
		return invalidRecord("health observation identity or timestamp is invalid")
	}
	if !validHealthDomain(observation.Domain) || !validHealthSeverity(observation.Severity) {
		return invalidRecord("health domain or severity is invalid")
	}
	if err := validateSHA256Digest(observation.Fingerprint, "health fingerprint"); err != nil {
		return err
	}
	if !validHealthCode(observation.Domain, observation.Code) {
		return invalidRecord("health code is not allowed for its domain")
	}
	if err := validateRuntimeErrorClass(observation.ErrorClass); err != nil {
		return err
	}
	if err := validateOptionalStrings(observation.SourceFileID, observation.JobID); err != nil {
		return err
	}
	return nil
}

func validHealthSeverity(value HealthSeverity) bool {
	switch value {
	case HealthInfo, HealthWarning, HealthError, HealthCritical:
		return true
	default:
		return false
	}
}

func validHealthDomain(value HealthDomain) bool {
	switch value {
	case HealthDomainSource, HealthDomainJob, HealthDomainStore, HealthDomainPricing, HealthDomainRuntime:
		return true
	default:
		return false
	}
}

func validatePricingVersion(version PricingVersion) error {
	if version.PricingVersion == "" || version.Source == "" || version.Currency == "" ||
		version.EffectiveFromMS < 0 || version.CreatedAtMS < 0 {
		return invalidRecord("pricing version identity or timestamps are invalid")
	}
	if len(version.Models) == 0 {
		return invalidRecord("pricing version requires at least one model rule")
	}
	if (version.SourceURL == "") != (version.VerifiedAtMS == 0) {
		return invalidRecord("pricing catalog metadata must be fully present or absent")
	}
	if version.SourceURL != "" {
		parsed, err := url.Parse(version.SourceURL)
		if err != nil || parsed.Scheme != "https" || parsed.Host == "" ||
			len(version.SourceURL) > 2048 || version.VerifiedAtMS <= 0 {
			return invalidRecord("pricing catalog metadata is invalid")
		}
	}
	seen := make(map[string]struct{}, len(version.Models))
	for _, model := range version.Models {
		if err := validateModelPrice(model); err != nil {
			return err
		}
		key := string(model.MatchKind) + "\x00" + model.ModelPattern
		if _, exists := seen[key]; exists {
			return invalidRecord("pricing version contains a duplicate model rule")
		}
		seen[key] = struct{}{}
	}
	return nil
}

func validateModelPrice(model ModelPrice) error {
	if !validModelMatchKind(model.MatchKind) || model.ModelPattern == "" || model.Priority < 0 {
		return invalidRecord("model price identity or priority is invalid")
	}
	if model.MatchKind == ModelMatchDefault && model.ModelPattern != "*" {
		return invalidRecord("default model price pattern must be an asterisk")
	}
	if model.MatchKind != ModelMatchDefault && model.ModelPattern == "*" {
		return invalidRecord("non-default model price pattern must not be an asterisk")
	}
	values := []*int64{
		model.InputMicrosPerMillion,
		model.CachedInputMicrosPerMillion,
		model.OutputMicrosPerMillion,
	}
	known := false
	for _, value := range values {
		if value == nil {
			continue
		}
		known = true
		if *value < 0 {
			return invalidRecord("model price must not be negative")
		}
	}
	if !known {
		return invalidRecord("model price requires at least one known token category")
	}
	return nil
}

func validModelMatchKind(value ModelMatchKind) bool {
	switch value {
	case ModelMatchExact, ModelMatchPrefix, ModelMatchDefault:
		return true
	default:
		return false
	}
}

func validateJobCursor(value *JobCursor) error {
	if value == nil {
		return nil
	}
	if value.Generation < 0 || value.Offset < 0 {
		return invalidRecord("job resume cursor generation and offset must not be negative")
	}
	return nil
}

func validateSHA256Digest(value SHA256Digest, field string) error {
	if !value.valid || len(value.String()) != sha256DigestHexLength {
		return invalidRecord(field + " must be a 64-character lowercase hex digest")
	}
	return nil
}

const sha256DigestHexLength = 64

func validHealthCode(domain HealthDomain, code HealthCode) bool {
	switch domain {
	case HealthDomainSource:
		switch code {
		case HealthCodeSourceTimeout, HealthCodeSourceUnavailable, HealthCodeSourcePermission,
			HealthCodeSourceCorrupt, HealthCodeSourceStale:
			return true
		}
	case HealthDomainJob:
		switch code {
		case HealthCodeJobInterrupted, HealthCodeJobFailed, HealthCodeJobCancelled:
			return true
		}
	case HealthDomainStore:
		switch code {
		case HealthCodeStoreBusy, HealthCodeStoreDiskFull, HealthCodeStoreReadOnly,
			HealthCodeStorePermission, HealthCodeStoreIO, HealthCodeStoreCorrupt,
			HealthCodeStoreUnavailable, HealthCodeStoreUnknown:
			return true
		}
	case HealthDomainPricing:
		return code == HealthCodePricingUnavailable || code == HealthCodePricingInvalid
	case HealthDomainRuntime:
		return code == HealthCodeRuntimeUnknown
	}
	return false
}
