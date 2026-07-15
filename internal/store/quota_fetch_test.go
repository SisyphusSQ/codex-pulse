package store

import (
	"context"
	"errors"
	"reflect"
	"testing"
)

func TestRecordQuotaFetchAtomicallyStoresSuccessAndExactReplay(t *testing.T) {
	t.Parallel()

	repository := openRuntimeRepository(t)
	record := successfulQuotaFetchRecord("wham-success", 1_784_000_000_000, 41, 9)
	if err := repository.RecordQuotaFetch(context.Background(), record); err != nil {
		t.Fatalf("RecordQuotaFetch(success) error = %v", err)
	}
	if err := repository.RecordQuotaFetch(context.Background(), record); err != nil {
		t.Fatalf("RecordQuotaFetch(replay) error = %v", err)
	}
	state, err := repository.SourceState(context.Background(), record.SourceInstanceID)
	if err != nil {
		t.Fatalf("SourceState() error = %v", err)
	}
	if state.SourceType != QuotaSourceTypeWham || state.ScopeKey != QuotaAccountScopeDefault ||
		state.LastAttemptAtMS == nil || *state.LastAttemptAtMS != record.Attempt.FinishedAtMS ||
		state.LastSuccessAtMS == nil || *state.LastSuccessAtMS != record.Attempt.FinishedAtMS ||
		state.ConsecutiveFailures != 0 || state.LastErrorClass != nil || state.LastFailureCode != nil ||
		state.FreshnessState != SourceFreshnessCurrent || state.CursorVersion != 1 {
		t.Fatalf("source state = %#v", state)
	}
	attempts, err := repository.ListSourceAttempts(context.Background(), record.SourceInstanceID, 10)
	if err != nil || !reflect.DeepEqual(attempts, []SourceAttempt{record.Attempt}) {
		t.Fatalf("attempts = %#v, %v", attempts, err)
	}
	observations, err := repository.ListQuotaObservations(context.Background(), QuotaObservationFilter{
		Source: pointerTo(QuotaSourceWham), Limit: 10,
	})
	if err != nil || len(observations) != 2 {
		t.Fatalf("observations = %#v, %v", observations, err)
	}
	for _, observation := range observations {
		if observation.RequestID == nil || *observation.RequestID != record.Attempt.RequestID ||
			observation.SourceFileID != nil || observation.SessionID != nil || observation.SampleCount != 1 {
			t.Fatalf("online observation = %#v", observation)
		}
	}

	conflict := record
	conflict.Observations = append([]QuotaObservationSample(nil), record.Observations...)
	conflict.Observations[0].UsedPercent++
	if err := repository.RecordQuotaFetch(context.Background(), conflict); !errors.Is(err, ErrInvalidRecord) {
		t.Fatalf("RecordQuotaFetch(conflict) error = %v, want ErrInvalidRecord", err)
	}
	stateAfter, _ := repository.SourceState(context.Background(), record.SourceInstanceID)
	if !reflect.DeepEqual(stateAfter, state) {
		t.Fatalf("state changed after conflict: %#v -> %#v", state, stateAfter)
	}
}

func TestRecordQuotaFetchPreservesLastKnownGoodAcrossTypedFailures(t *testing.T) {
	t.Parallel()

	repository := openRuntimeRepository(t)
	success := successfulQuotaFetchRecord("wham-lkg-success", 1_784_000_000_000, 35, 11)
	if err := repository.RecordQuotaFetch(context.Background(), success); err != nil {
		t.Fatalf("RecordQuotaFetch(success) error = %v", err)
	}
	auth := failedQuotaFetchRecord(
		"wham-lkg-auth", 1_784_000_010_000, SourceAttemptFailed,
		RuntimeErrorPermission, SourceFailureAuthRequired, nil,
	)
	if err := repository.RecordQuotaFetch(context.Background(), auth); err != nil {
		t.Fatalf("RecordQuotaFetch(auth) error = %v", err)
	}
	state, err := repository.SourceState(context.Background(), success.SourceInstanceID)
	if err != nil || state.LastSuccessAtMS == nil || *state.LastSuccessAtMS != success.Attempt.FinishedAtMS ||
		state.ConsecutiveFailures != 1 || state.LastFailureCode == nil || *state.LastFailureCode != SourceFailureAuthRequired ||
		state.LastErrorClass == nil || *state.LastErrorClass != RuntimeErrorPermission ||
		state.FreshnessState != SourceFreshnessStale || state.NextDueAtMS != nil {
		t.Fatalf("state after auth = %#v, %v", state, err)
	}
	retryAt := int64(1_784_000_400_000)
	rateLimited := failedQuotaFetchRecord(
		"wham-lkg-429", 1_784_000_020_000, SourceAttemptFailed,
		RuntimeErrorUnavailable, SourceFailureHTTP429, &retryAt,
	)
	if err := repository.RecordQuotaFetch(context.Background(), rateLimited); err != nil {
		t.Fatalf("RecordQuotaFetch(429) error = %v", err)
	}
	state, _ = repository.SourceState(context.Background(), success.SourceInstanceID)
	if state.ConsecutiveFailures != 2 || state.NextDueAtMS == nil || *state.NextDueAtMS != retryAt ||
		state.LastFailureCode == nil || *state.LastFailureCode != SourceFailureHTTP429 {
		t.Fatalf("state after 429 = %#v", state)
	}
	cancelled := failedQuotaFetchRecord(
		"wham-lkg-cancel", 1_784_000_030_000, SourceAttemptCancelled,
		RuntimeErrorCanceled, SourceFailureCancelled, nil,
	)
	cancelled.Attempt.AttemptCount = 0
	if err := repository.RecordQuotaFetch(context.Background(), cancelled); err != nil {
		t.Fatalf("RecordQuotaFetch(cancelled) error = %v", err)
	}
	state, _ = repository.SourceState(context.Background(), success.SourceInstanceID)
	if state.ConsecutiveFailures != 2 || state.LastFailureCode == nil || *state.LastFailureCode != SourceFailureCancelled ||
		state.FreshnessState != SourceFreshnessStale || state.NextDueAtMS != nil {
		t.Fatalf("state after cancelled = %#v", state)
	}

	observations, err := repository.ListQuotaObservations(context.Background(), QuotaObservationFilter{
		Source: pointerTo(QuotaSourceWham), Limit: 10,
	})
	if err != nil || len(observations) != 2 || observations[0].UsedPercent == 0 || observations[1].UsedPercent == 0 {
		t.Fatalf("last-known-good observations = %#v, %v", observations, err)
	}
}

func TestRecordQuotaFetchStoresPartialAndIgnoresLateStateRegression(t *testing.T) {
	t.Parallel()

	repository := openRuntimeRepository(t)
	partial := successfulQuotaFetchRecord("wham-partial", 1_784_000_100_000, 22, 0)
	partial.Observations = partial.Observations[:1]
	partial.Attempt.Outcome = SourceAttemptFailed
	partial.Attempt.ErrorClass = pointerTo(RuntimeErrorInvalid)
	partial.Attempt.FailureCode = pointerTo(SourceFailureSchemaIncompatible)
	if err := repository.RecordQuotaFetch(context.Background(), partial); err != nil {
		t.Fatalf("RecordQuotaFetch(partial) error = %v", err)
	}
	state, _ := repository.SourceState(context.Background(), partial.SourceInstanceID)
	if state.LastSuccessAtMS == nil || *state.LastSuccessAtMS != partial.Attempt.FinishedAtMS ||
		state.FreshnessState != SourceFreshnessStale || state.ConsecutiveFailures != 1 {
		t.Fatalf("partial state = %#v", state)
	}

	newer := successfulQuotaFetchRecord("wham-newer", 1_784_000_200_000, 27, 12)
	if err := repository.RecordQuotaFetch(context.Background(), newer); err != nil {
		t.Fatalf("RecordQuotaFetch(newer) error = %v", err)
	}
	newerState, _ := repository.SourceState(context.Background(), newer.SourceInstanceID)
	late := failedQuotaFetchRecord(
		"wham-late", 1_784_000_150_000, SourceAttemptFailed,
		RuntimeErrorUnavailable, SourceFailureNetworkUnavailable, nil,
	)
	if err := repository.RecordQuotaFetch(context.Background(), late); err != nil {
		t.Fatalf("RecordQuotaFetch(late) error = %v", err)
	}
	afterLate, _ := repository.SourceState(context.Background(), newer.SourceInstanceID)
	if !reflect.DeepEqual(afterLate, newerState) {
		t.Fatalf("late attempt regressed state: %#v -> %#v", newerState, afterLate)
	}
	attempts, err := repository.ListSourceAttempts(context.Background(), newer.SourceInstanceID, 10)
	if err != nil || len(attempts) != 3 {
		t.Fatalf("attempt history after late result = %#v, %v", attempts, err)
	}
}

func TestRecordQuotaFetchDoesNotPromoteSuspiciousOnlyPartialToLastSuccess(t *testing.T) {
	t.Parallel()

	repository := openRuntimeRepository(t)
	suspiciousOnly := successfulQuotaFetchRecord("wham-suspicious-only", 1_784_000_100_000, 22, 17)
	suspiciousOnly.Observations = suspiciousOnly.Observations[1:]
	suspiciousOnly.Observations[0].Validity = QuotaValiditySuspicious
	suspiciousOnly.Observations[0].RejectionReason = pointerTo(QuotaReasonMissingPrimaryWindow)
	suspiciousOnly.Attempt.Outcome = SourceAttemptFailed
	suspiciousOnly.Attempt.ErrorClass = pointerTo(RuntimeErrorInvalid)
	suspiciousOnly.Attempt.FailureCode = pointerTo(SourceFailureSchemaIncompatible)
	if err := repository.RecordQuotaFetch(context.Background(), suspiciousOnly); err != nil {
		t.Fatalf("RecordQuotaFetch(first suspicious-only) error = %v", err)
	}
	state, err := repository.SourceState(context.Background(), suspiciousOnly.SourceInstanceID)
	if err != nil || state.LastSuccessAtMS != nil || state.FreshnessState != SourceFreshnessUnavailable ||
		state.ConsecutiveFailures != 1 {
		t.Fatalf("state after first suspicious-only partial = %#v, %v", state, err)
	}

	success := successfulQuotaFetchRecord("wham-before-suspicious", 1_784_000_200_000, 31, 12)
	if err := repository.RecordQuotaFetch(context.Background(), success); err != nil {
		t.Fatalf("RecordQuotaFetch(success) error = %v", err)
	}
	suspiciousAfterSuccess := suspiciousOnly
	suspiciousAfterSuccess.Attempt.RequestID = "wham-suspicious-after-success"
	suspiciousAfterSuccess.Attempt.StartedAtMS = 1_784_000_299_990
	suspiciousAfterSuccess.Attempt.FinishedAtMS = 1_784_000_300_000
	suspiciousAfterSuccess.Observations = append([]QuotaObservationSample(nil), suspiciousOnly.Observations...)
	suspiciousAfterSuccess.Observations[0].ObservationID = "quota-wham-suspicious-after-success"
	suspiciousAfterSuccess.Observations[0].ObservedAtMS = suspiciousAfterSuccess.Attempt.FinishedAtMS
	suspiciousAfterSuccess.Observations[0].RequestID = pointerTo(suspiciousAfterSuccess.Attempt.RequestID)
	if err := repository.RecordQuotaFetch(context.Background(), suspiciousAfterSuccess); err != nil {
		t.Fatalf("RecordQuotaFetch(suspicious after success) error = %v", err)
	}
	state, err = repository.SourceState(context.Background(), success.SourceInstanceID)
	if err != nil || state.LastSuccessAtMS == nil || *state.LastSuccessAtMS != success.Attempt.FinishedAtMS ||
		state.FreshnessState != SourceFreshnessStale || state.ConsecutiveFailures != 1 {
		t.Fatalf("state after suspicious-only with LKG = %#v, %v", state, err)
	}
}

func TestRecordQuotaFetchRollsBackStateAttemptAndObservationTogether(t *testing.T) {
	t.Parallel()

	repository := openRuntimeRepository(t)
	record := successfulQuotaFetchRecord("wham-rollback", 1_784_000_000_000, 14, 4)
	record.Observations[1].RequestID = pointerTo("wrong-request")
	if err := repository.RecordQuotaFetch(context.Background(), record); !errors.Is(err, ErrInvalidRecord) {
		t.Fatalf("RecordQuotaFetch(invalid provenance) error = %v, want ErrInvalidRecord", err)
	}
	if _, err := repository.SourceState(context.Background(), record.SourceInstanceID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("SourceState(after rollback) error = %v, want ErrNotFound", err)
	}
	if attempts, err := repository.ListSourceAttempts(context.Background(), record.SourceInstanceID, 10); err != nil || len(attempts) != 0 {
		t.Fatalf("ListSourceAttempts(after rollback) = %#v, %v", attempts, err)
	}
	for _, observation := range record.Observations {
		if _, err := repository.QuotaObservation(context.Background(), observation.ObservationID); !errors.Is(err, ErrNotFound) {
			t.Fatalf("QuotaObservation(%s) error = %v", observation.ObservationID, err)
		}
	}
}

func successfulQuotaFetchRecord(requestID string, finishedAtMS int64, primary, secondary float64) QuotaFetchRecord {
	limitID := "codex"
	requestIDPointer := requestID
	plan := "pro"
	digest := SHA256DigestOf([]byte("synthetic wham response " + requestID))
	window := func(kind QuotaWindowKind, used float64, minutes, reset int64) QuotaObservationSample {
		return QuotaObservationSample{
			ObservationID: "quota-" + requestID + "-" + string(kind),
			AccountScope:  QuotaAccountScopeDefault, Source: QuotaSourceWham,
			LimitID: &limitID, WindowKind: kind, UsedPercent: used, WindowMinutes: minutes,
			ResetsAtMS: reset, PlanType: &plan, ObservedAtMS: finishedAtMS,
			Validity: QuotaValidityAccepted, RequestID: &requestIDPointer,
		}
	}
	return QuotaFetchRecord{
		SourceInstanceID: QuotaSourceInstanceWhamDefault,
		SourceType:       QuotaSourceTypeWham,
		ScopeKey:         QuotaAccountScopeDefault,
		Attempt: SourceAttempt{
			RequestID: requestID, SourceInstanceID: QuotaSourceInstanceWhamDefault,
			StartedAtMS: finishedAtMS - 10, FinishedAtMS: finishedAtMS,
			Outcome: SourceAttemptSucceeded, HTTPStatus: pointerTo(int64(200)),
			PayloadSHA256: &digest, AttemptCount: 1, ResponseBytes: 512,
		},
		Observations: []QuotaObservationSample{
			window(QuotaWindowPrimary, primary, 300, finishedAtMS+3_600_000),
			window(QuotaWindowSecondary, secondary, 10080, finishedAtMS+604_800_000),
		},
	}
}

func failedQuotaFetchRecord(
	requestID string,
	finishedAtMS int64,
	outcome SourceAttemptOutcome,
	errorClass RuntimeErrorClass,
	failureCode SourceFailureCode,
	retryAtMS *int64,
) QuotaFetchRecord {
	var httpStatus *int64
	switch failureCode {
	case SourceFailureHTTP429:
		httpStatus = pointerTo(int64(429))
	case SourceFailureServerError:
		httpStatus = pointerTo(int64(503))
	case SourceFailureSchemaIncompatible:
		httpStatus = pointerTo(int64(200))
	}
	return QuotaFetchRecord{
		SourceInstanceID: QuotaSourceInstanceWhamDefault,
		SourceType:       QuotaSourceTypeWham,
		ScopeKey:         QuotaAccountScopeDefault,
		Attempt: SourceAttempt{
			RequestID: requestID, SourceInstanceID: QuotaSourceInstanceWhamDefault,
			StartedAtMS: finishedAtMS - 10, FinishedAtMS: finishedAtMS,
			Outcome: outcome, ErrorClass: &errorClass, FailureCode: &failureCode,
			HTTPStatus: httpStatus, AttemptCount: 1, ResponseBytes: 64, RetryAtMS: retryAtMS,
		},
	}
}
