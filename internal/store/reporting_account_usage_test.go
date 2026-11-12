package store

import (
	"fmt"
	"slices"
	"testing"
)

func TestReportingAccountRangesUseCompletedClaimsAndKeepRestartGenerations(t *testing.T) {
	repo := openRuntimeRepository(t)
	ctx := t.Context()
	const base = int64(1_784_000_000_000)
	key, scope, generation := confirmSyntheticCodexAccount(t, repo, "account-a", base)
	sourceID := QuotaSourceInstanceAppServer(scope)
	var revision int64
	record := func(offset, generation int64, complete bool) {
		t.Helper()
		at := base + offset
		schedule, err := repo.UpsertSourceRefreshSchedule(ctx, SourceRefreshScheduleUpdate{
			SourceInstanceID: sourceID, SourceType: QuotaSourceTypeAppServerRateLimits,
			ScopeKey: scope, BindingGeneration: generation, ExpectedRevision: revision,
			NextDueAtMS: new(at - 10), Reason: RefreshReasonStartup, AtMS: at - 20,
		})
		if err != nil {
			t.Fatal(err)
		}
		requestID := fmt.Sprintf("claim-%d", offset)
		claimed, ok, err := repo.ClaimSourceRefresh(ctx, sourceID, schedule.Revision, requestID,
			RefreshTriggerScheduled, at-10, 30_000, generation)
		if err != nil || !ok {
			t.Fatalf("claim: %v %v", ok, err)
		}
		if err := repo.RecordQuotaFetch(ctx, appServerQuotaFetchRecord(requestID, scope, generation, at)); err != nil {
			t.Fatal(err)
		}
		if !complete {
			return
		}
		finalized, err := repo.CompleteSourceRefresh(ctx, SourceRefreshCompletion{
			SourceInstanceID: sourceID, ClaimID: requestID, ExpectedRevision: claimed.Revision,
			NextDueAtMS: new(at + 50), Reason: RefreshReasonNormalInterval, AtMS: at,
		})
		if err != nil {
			t.Fatal(err)
		}
		revision = finalized.Revision
	}
	record(100, generation, true)
	record(200, generation, true)
	if _, err := repo.MarkCodexAccountBindingPending(ctx, base+250, CodexAccountBindingReasonStartup); err != nil {
		t.Fatal(err)
	}
	_, afterRestart := confirmSyntheticCodexAccountWithKey(t, repo, key, "account-a", base+260)
	record(300, afterRestart, true)
	record(400, afterRestart, false)
	got, err := repo.ReportingAccountRanges(ctx, base+50, base+500)
	want := []ReportingAccountRange{
		{AccountScope: scope, BindingGeneration: generation, FromMS: base + 100, ThroughMS: base + 200},
		{AccountScope: scope, BindingGeneration: afterRestart, FromMS: base + 300, ThroughMS: base + 300},
	}
	if err != nil || !slices.Equal(got, want) {
		t.Fatalf("ranges = %#v, %v, want %#v", got, err, want)
	}
	got, err = repo.ReportingAccountRanges(ctx, base+150, base+500)
	want[0].FromMS = base + 200
	if err != nil || !slices.Equal(got, want) {
		t.Fatalf("clipped ranges = %#v, %v, want %#v", got, err, want)
	}
}
