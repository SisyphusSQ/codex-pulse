package reporting_srv

import (
	"testing"
	"time"
	"uuid"

	reportingv1 "github.com/SisyphusSQ/codex-pulse/api/codexpulse/reporting/v1"
	reporting_do "github.com/SisyphusSQ/codex-pulse/server/internal/models/do/mysql/reporting_do"
)

func agyTestSnapshot(sessionID string, revision int64, output int64, cost int64, version string) reportingv1.SessionSnapshot {
	c := reportingv1.Contribution{
		ObservedAtMS:    new(int64(1784000000000)),
		Model:           new("gemini-4-carbon"),
		InputTokens:     new(int64(1500)),
		CachedTokens:    new(int64(500)),
		OutputTokens:    new(output),
		ReasoningTokens: new(int64(50)),
		TotalTokens:     new(1500 + output),
		CostMicroUSD:    new(cost),
		PricingVersion:  new(version),
		CostStatus:      "known",
		PricingMode:     "event_cost",
	}
	c.ID = reportingv1.ContributionID("agy", sessionID, c, 0)
	return reportingv1.SessionSnapshot{
		Provider:       "agy",
		HomeID:         "home-agy",
		SessionID:      sessionID,
		Revision:       revision,
		CollectedAtMS:  1784000005000,
		Title:          "AGY 中心会话",
		ProjectID:      reportingv1.Key("agy", "workspace"),
		ProjectName:    "workspace",
		CreatedAtMS:    new(int64(1783999998000)),
		LastActiveAtMS: new(int64(1784000000000)),
		SourceKind:     "agy_local",
		SessionKind:    "session",
		Complete:       true,
		Contributions:  []reportingv1.Contribution{c},
		CacheUsage: &reportingv1.CacheUsageCapsule{
			Version:           1,
			Basis:             "lifetime_cached_input",
			InputTokens:       new(int64(1500)),
			CachedInputTokens: new(int64(500)),
		},
		Throughput: &reportingv1.ThroughputCapsule{
			Version: 1,
			Basis:   "closed_turn_lifetime_output",
			Measures: reportingv1.ThroughputMeasures{
				OutputTokens:     new(output),
				ActiveDurationMS: new(int64(2000)),
				IncludedTurns:    1,
				CoverageKnown:    true,
				Status:           "complete",
				DurationSource:   "step_metadata",
			},
			TurnsTotal: 1,
		},
	}
}

func TestReportingAGYIngestDedupConflictAndMultiDeviceQuota(t *testing.T) {
	s, db, clients := reportingFixture(t)
	s.now = func() time.Time { return time.UnixMilli(1784000010000) }

	scope := reportingv1.Key("agy-account", "digest-a")
	reset5h := int64(1784018000000)
	resetWeekly := int64(1784604800000)
	snap := agyTestSnapshot("agy-cascade-1", 1, 250, 4550, "agy-api-2026-11-25:phase1")

	// Device 1 and Device 2 report identical AGY session + multi-pool quotas.
	for i, client := range clients[:2] {
		publicScope := reportingv1.Key("public-agy", client.ID, scope)
		batch := reportingv1.Batch{
			Version:  1,
			ID:       uuid.New().String(),
			Sessions: []reportingv1.SessionSnapshot{snap},
			Accounts: []reportingv1.Account{{
				Provider:      "agy",
				ID:            scope,
				Plan:          new("Google AI Ultra 20×"),
				CollectedAtMS: 1784000005000 + int64(i)*1000,
			}},
			Bindings: []reportingv1.AccountBinding{{
				Provider:      "agy",
				LocalScope:    publicScope,
				AccountID:     scope,
				ConfirmedAtMS: 1784000005000,
			}},
			Quotas: []reportingv1.QuotaObservation{
				{
					Provider:        "agy",
					ID:              reportingv1.Key("q", client.ID, "gemini"),
					AccountID:       new(scope),
					LocalScope:      publicScope,
					LimitID:         "agy.gemini",
					WindowKind:      "primary",
					WindowMinutes:   new(int64(300)),
					WindowStartAtMS: new(reset5h - 300*60000),
					ResetsAtMS:      new(reset5h),
					ObservedAtMS:    1784000000000 + int64(i)*1000,
					UsedPercent:     new(float64(25 + i*5)),
					Validity:        "accepted",
					Source:          "agy_ls_rpc",
					HistoryOrigin:   "confirmed",
				},
				{
					Provider:        "agy",
					ID:              reportingv1.Key("q", client.ID, "claude"),
					AccountID:       new(scope),
					LocalScope:      publicScope,
					LimitID:         "agy.claude_gpt",
					WindowKind:      "secondary",
					WindowMinutes:   new(int64(10080)),
					WindowStartAtMS: new(resetWeekly - 10080*60000),
					ResetsAtMS:      new(resetWeekly),
					ObservedAtMS:    1784000000000 + int64(i)*1000,
					UsedPercent:     new(float64(10)),
					Validity:        "accepted",
					Source:          "agy_ls_rpc",
					HistoryOrigin:   "confirmed",
				},
			},
			Status: []reportingv1.DeviceStatus{
				{Provider: "codex", Status: "ready", SyncState: "ready", SyncCheckedAtMS: new(int64(1784000005000))},
				{Provider: "cursor", Status: "disabled", SyncState: "ready", SyncCheckedAtMS: new(int64(1784000005000))},
				{Provider: "grok", Status: "disabled", SyncState: "ready", SyncCheckedAtMS: new(int64(1784000005000))},
				{Provider: "dsh", Status: "disabled", SyncState: "ready", SyncCheckedAtMS: new(int64(1784000005000))},
				{Provider: "agy", Status: "ready", SyncState: "ready", CollectedAtMS: new(int64(1784000005000)), SyncCheckedAtMS: new(int64(1784000005000))},
			},
		}
		first, err := s.Accept(t.Context(), client, batch)
		if err != nil {
			t.Fatalf("device %d AGY batch rejected: %v", i, err)
		}
		// Idempotent replay must return original receipt timestamp.
		s.now = func() time.Time { return time.UnixMilli(1784000099000) }
		replay, err := s.Accept(t.Context(), client, batch)
		if err != nil || replay.ReceivedAtMS != first.ReceivedAtMS {
			t.Fatalf("AGY batch replay broke idempotency: first=%+v replay=%+v err=%v", first, replay, err)
		}
		s.now = func() time.Time { return time.UnixMilli(1784000010000) }
	}

	meta, total := readSession(t, db)
	if meta.Provider != "agy" || meta.Conflict || total != 1750 {
		t.Fatalf("AGY multi-device session dedup failed: meta=%+v total=%d", meta, total)
	}

	var quotaCount int64
	if err := db.Model(&reporting_do.QuotaObservation{}).Where("provider = ?", "agy").Count(&quotaCount).Error; err != nil {
		t.Fatal(err)
	}
	if quotaCount != 4 {
		t.Fatalf("expected 4 distinct device/pool AGY quota rows, got %d", quotaCount)
	}

	// Device 3 reports conflicting facts for the same AGY session: conflict flag must be set without summing.
	divergent := agyTestSnapshot("agy-cascade-1", 1, 999, 9999, "agy-api-2026-11-25:phase1")
	if _, err := s.Accept(t.Context(), clients[2], reportingv1.Batch{
		Version:  1,
		ID:       uuid.New().String(),
		Sessions: []reportingv1.SessionSnapshot{divergent},
	}); err != nil {
		t.Fatal(err)
	}
	meta, total = readSession(t, db)
	if !meta.Conflict || total != 1750 {
		t.Fatalf("expected conflict=true and preserved canonical total=1750, got conflict=%v total=%d", meta.Conflict, total)
	}
}
