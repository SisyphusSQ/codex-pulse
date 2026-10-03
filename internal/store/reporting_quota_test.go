package store

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"
)

func TestReportingQuotaHistoryKeepsObservedTimesAndCreditsPrivacy(t *testing.T) {
	repo := openRuntimeRepository(t)
	ctx := t.Context()
	at := int64(1784000000000)
	samples := []QuotaObservationSample{quotaProjectionWhamSample("legacy-q", 0, at, at+18000000), quotaProjectionWhamSample("legacy-q2", 12, at+1000, at+18000000)}
	for _, sample := range samples {
		if err := repo.UpsertFacts(ctx, FactBatch{QuotaObservation: &sample}); err != nil {
			t.Fatal(err)
		}
	}
	credit := successfulResetCreditsFetchRecord("credential-like-request-not-uploaded", at)
	if err := repo.RecordResetCreditsFetch(ctx, credit); err != nil {
		t.Fatal(err)
	}
	page, err := repo.ReportingQuotaPage(ctx, "codex", "", 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(page.Batch.Quotas) != 2 || page.Batch.Quotas[0].ObservedAtMS != at || *page.Batch.Quotas[0].UsedPercent != 0 || page.Batch.Quotas[0].AccountID != nil || page.Batch.Quotas[0].HistoryOrigin != "legacy_unassigned" {
		t.Fatal("legacy/time/zero changed")
	}
	page, err = repo.ReportingQuotaPage(ctx, "codex", page.Next, 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(page.Batch.Credits) != 1 {
		t.Fatal("credits absent")
	}
	c := page.Batch.Credits[0]
	if c.Inventory == nil || *c.Inventory != 2 || c.NextResetAtMS != nil || c.NextExpiresAtMS == nil || *c.NextExpiresAtMS != at+3600000 || len(c.ExpirySchedule) != 2 || c.DetailsStatus != "complete" {
		t.Fatal("credits/reset semantics changed")
	}
	body, _ := json.Marshal(page.Batch)
	for _, secret := range []string{"credential-like-request-not-uploaded", "credit-a", "credit-b", "credit-c", "credit_id_hash", "source_file_id", "request_id", "source_offset", "source_generation"} {
		if strings.Contains(string(body), secret) {
			t.Fatal("private source identity escaped", secret)
		}
	}
	final, err := repo.ReportingQuotaPage(ctx, "codex", page.Next, 0)
	if err != nil || !final.Done {
		t.Fatal("paging never completes", err)
	}
	if _, err := repo.ReportingQuotaPage(ctx, "codex", "corrupt cursor", 0); err == nil {
		t.Fatal("corrupt cursor ignored")
	}
}
func TestReportingQuotaCursorAndGrokUseHistoricalWindowsWithoutGeneration(t *testing.T) {
	repo := openRuntimeRepository(t)
	ctx := t.Context()
	start := int64(1780000000000)
	end := start + 600000
	observed := start + 1000
	if err := repo.ReplaceCursorSnapshot(ctx, CursorSnapshot{Generation: 1, CollectedAtMS: observed}); err != nil {
		t.Fatal(err)
	}
	for generation := int64(1); generation <= 2; generation++ {
		snap := CursorDashboardSnapshot{Generation: generation, CollectedAtMS: observed + generation, WindowStartMS: start, WindowEndMS: end, BillingCycleEndMS: end, QuotaWindows: []CursorDashboardQuotaWindow{{LimitID: "cursor.models", UsedPercent: float64(generation), CycleStartAtMS: start, CycleEndAtMS: end}}}
		if err := repo.CommitCursorDashboardSnapshot(ctx, snap); err != nil {
			t.Fatal(err)
		}
	}
	cursor, err := repo.ReportingQuotaPage(ctx, "cursor", "", 0)
	if err != nil || len(cursor.Batch.Quotas) != 2 {
		t.Fatal("cursor history missing", err)
	}
	for _, q := range cursor.Batch.Quotas {
		if q.WindowStartAtMS == nil || *q.WindowStartAtMS != start || q.ResetsAtMS == nil || *q.ResetsAtMS != end || q.AccountID != nil {
			t.Fatal("window or raw account guessed")
		}
	}
	if err := repo.ReplaceGrokSnapshot(ctx, GrokSnapshot{Generation: 1, CollectedAtMS: observed}); err != nil {
		t.Fatal(err)
	}
	if err := repo.CommitGrokBillingSnapshot(ctx, GrokBillingSnapshot{Generation: 1, CollectedAtMS: observed, PeriodType: "weekly", PeriodStartMS: start, PeriodEndMS: end, UsedPercent: 0}); err != nil {
		t.Fatal(err)
	}
	grok, err := repo.ReportingQuotaPage(ctx, "grok", "", 0)
	if err != nil || len(grok.Batch.Quotas) == 0 {
		t.Fatal("grok current/history absent", err)
	}
	body, _ := json.Marshal(grok.Batch)
	if strings.Contains(string(body), "generation") {
		t.Fatal("local generation uploaded")
	}
	if *grok.Batch.Quotas[0].UsedPercent != 0 {
		t.Fatal("grok real zero lost")
	}
}

func TestReportingCurrentQuotaBypassesOldHistoricalPage(t *testing.T) {
	repo := openRuntimeRepository(t)
	ctx := t.Context()
	at := int64(1784000000000)
	for index := range 120 {
		sample := quotaProjectionWhamSample("history-"+fmt.Sprint(index), float64(index%100), at+int64(index)*1000, at+18000000)
		if err := repo.UpsertFacts(ctx, FactBatch{QuotaObservation: &sample}); err != nil {
			t.Fatal(err)
		}
	}
	history, err := repo.ReportingQuotaPage(ctx, "codex", "", 0)
	if err != nil {
		t.Fatal(err)
	}
	current, err := repo.ReportingCurrentQuotaPage(ctx, "codex", 0)
	if err != nil {
		t.Fatal(err)
	}
	if !current.Done || len(current.Batch.Quotas) != 1 || current.Batch.Quotas[0].ObservedAtMS != at+119000 || history.Batch.Quotas[len(history.Batch.Quotas)-1].ObservedAtMS >= current.Batch.Quotas[0].ObservedAtMS {
		t.Fatal("current followed oldest history")
	}
}
