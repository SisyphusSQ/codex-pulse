package quota_srv

import (
	"net/url"
	"testing"

	reportingv1 "github.com/SisyphusSQ/codex-pulse/api/codexpulse/reporting/v1"
	quota_dto "github.com/SisyphusSQ/codex-pulse/server/internal/models/dto/quota_dto"
)

func TestAGYMultiPoolMultiDeviceQuotaAndPaceQueries(t *testing.T) {
	s, reporting, admin, clients := quotaFixture(t)

	// Query parser accepts provider=agy.
	values, _ := url.ParseQuery("provider=agy&view=summary")
	parsed, err := ParseQuery(values)
	if err != nil || parsed.Provider != "agy" || parsed.View != "summary" {
		t.Fatalf("ParseQuery rejected provider=agy: %+v err=%v", parsed, err)
	}

	scopeA := reportingv1.Key("agy-account", "user-a")
	scopeB := reportingv1.Key("agy-account", "user-b")
	reset5h := quotaNow + 2*3600000
	resetWeekly := quotaNow + 4*86400000

	// Device 0 and Device 1 report Account A across multiple pools (agy.gemini, agy.argon, agy.carbon, agy.claude_gpt).
	for i, client := range clients[:2] {
		localScope := reportingv1.Key("agy-scope", client.ID, scopeA)
		at := quotaNow - int64(2-i)*60000
		sendQuota(t, reporting, client, reportingv1.Batch{
			Accounts: []reportingv1.Account{{
				Provider:      "agy",
				ID:            scopeA,
				Plan:          new("Google AI Ultra 20×"),
				CollectedAtMS: at,
			}},
			Bindings: []reportingv1.AccountBinding{{
				Provider:      "agy",
				LocalScope:    localScope,
				AccountID:     scopeA,
				ConfirmedAtMS: quotaNow - 3600000,
			}},
			Quotas: []reportingv1.QuotaObservation{
				{
					Provider: "agy", ID: reportingv1.Key("q", client.ID, "gemini-5h"),
					AccountID: new(scopeA), LocalScope: localScope, LimitID: "agy.gemini", WindowKind: "primary",
					WindowMinutes: new(int64(300)), WindowStartAtMS: new(reset5h - 300*60000), ResetsAtMS: new(reset5h),
					ObservedAtMS: at, UsedPercent: new(float64(30 + i*10)), Validity: "accepted", Source: "agy_ls_rpc", HistoryOrigin: "confirmed",
				},
				{
					Provider: "agy", ID: reportingv1.Key("q", client.ID, "gemini-wk"),
					AccountID: new(scopeA), LocalScope: localScope, LimitID: "agy.gemini", WindowKind: "secondary",
					WindowMinutes: new(int64(10080)), WindowStartAtMS: new(resetWeekly - 10080*60000), ResetsAtMS: new(resetWeekly),
					ObservedAtMS: at, UsedPercent: new(float64(12)), Validity: "accepted", Source: "agy_ls_rpc", HistoryOrigin: "confirmed",
				},
				{
					Provider: "agy", ID: reportingv1.Key("q", client.ID, "claude-5h"),
					AccountID: new(scopeA), LocalScope: localScope, LimitID: "agy.claude_gpt", WindowKind: "primary",
					WindowMinutes: new(int64(300)), WindowStartAtMS: new(reset5h - 300*60000), ResetsAtMS: new(reset5h),
					ObservedAtMS: at, UsedPercent: new(float64(50)), Validity: "accepted", Source: "agy_ls_rpc", HistoryOrigin: "confirmed",
				},
			},
		})
	}

	// Device 2 reports Account B (Pro with independent argon/carbon pools) + a Codex window to verify isolation.
	localScopeB := reportingv1.Key("agy-scope", clients[2].ID, scopeB)
	sendQuota(t, reporting, clients[2], reportingv1.Batch{
		Accounts: []reportingv1.Account{{
			Provider:      "agy",
			ID:            scopeB,
			Plan:          new("Google AI Pro"),
			CollectedAtMS: quotaNow - 30000,
		}},
		Bindings: []reportingv1.AccountBinding{{
			Provider:      "agy",
			LocalScope:    localScopeB,
			AccountID:     scopeB,
			ConfirmedAtMS: quotaNow - 30000,
		}},
		Quotas: []reportingv1.QuotaObservation{
			{
				Provider: "agy", ID: reportingv1.Key("q", clients[2].ID, "argon-5h"),
				AccountID: new(scopeB), LocalScope: localScopeB, LimitID: "agy.argon", WindowKind: "primary",
				WindowMinutes: new(int64(300)), WindowStartAtMS: new(reset5h - 300*60000), ResetsAtMS: new(reset5h),
				ObservedAtMS: quotaNow - 30000, UsedPercent: new(float64(15)), Validity: "accepted", Source: "agy_ls_rpc", HistoryOrigin: "confirmed",
			},
			{
				Provider: "agy", ID: reportingv1.Key("q", clients[2].ID, "carbon-5h"),
				AccountID: new(scopeB), LocalScope: localScopeB, LimitID: "agy.carbon", WindowKind: "primary",
				WindowMinutes: new(int64(300)), WindowStartAtMS: new(reset5h - 300*60000), ResetsAtMS: new(reset5h),
				ObservedAtMS: quotaNow - 30000, UsedPercent: new(float64(25)), Validity: "accepted", Source: "agy_ls_rpc", HistoryOrigin: "confirmed",
			},
		},
	})
	codexBatch := accountFacts("codex-a")
	codexBatch.Quotas = []reportingv1.QuotaObservation{quotaFact("codex-q", "codex-a", 10, quotaNow-10000, reset5h)}
	sendQuota(t, reporting, clients[0], codexBatch)

	// Query provider=agy: must return 5 distinct account×pool×window entries (3 for A + 2 for B), never summed across devices, never leaking Codex.
	allAGY := readQuota(t, s, admin, quota_dto.Query{Provider: "agy"})
	if len(allAGY.Accounts) != 2 || len(allAGY.Windows) != 5 {
		t.Fatalf("expected 2 AGY accounts and 5 pool windows, got accounts=%d windows=%d", len(allAGY.Accounts), len(allAGY.Windows))
	}
	for _, a := range allAGY.Accounts {
		if a.Provider != "agy" || a.Email != nil {
			t.Fatalf("AGY account leaked non-AGY or email: %+v", a)
		}
	}
	foundGeminiPrimary := false
	for _, w := range allAGY.Windows {
		if w.Provider != "agy" {
			t.Fatalf("non-AGY window leaked: %+v", w)
		}
		if w.LimitID == "agy.gemini" && w.WindowKind == "primary" {
			foundGeminiPrimary = true
			// Latest observation (Device 1 at quotaNow-60s) is 40%, NOT 30+40=70%!
			if w.Current.UsedPercent == nil || *w.Current.UsedPercent != 40 || len(w.Observations) != 2 {
				t.Fatalf("AGY multi-device observations summed or lost: used=%v obs=%d", w.Current.UsedPercent, len(w.Observations))
			}
		}
	}
	if !foundGeminiPrimary {
		t.Fatal("missing agy.gemini primary window")
	}

	// Filter by device 0: must see only Device 0's 30% observation (per-device preservation).
	dev0 := readQuota(t, s, admin, quota_dto.Query{Provider: "agy", ClientID: clients[0].ID})
	if len(dev0.Windows) != 3 {
		t.Fatalf("expected 3 AGY windows for device 0, got %d", len(dev0.Windows))
	}
	for _, w := range dev0.Windows {
		if w.LimitID == "agy.gemini" && w.WindowKind == "primary" {
			if w.Current.UsedPercent == nil || *w.Current.UsedPercent != 30 {
				t.Fatalf("expected device 0 used_percent=30, got %v", w.Current.UsedPercent)
			}
		}
	}

	// Filter by Account B: must see only argon + carbon pools.
	accB := readQuota(t, s, admin, quota_dto.Query{Provider: "agy", AccountKey: reportingv1.Key("agy", scopeB)})
	if len(accB.Windows) != 2 {
		t.Fatalf("expected 2 pools for Account B, got %d", len(accB.Windows))
	}

	// Pace endpoint works for provider=agy.
	pace, err := s.Pace(t.Context(), admin, quota_dto.Query{Provider: "agy"})
	if err != nil || len(pace.Windows) != 5 {
		t.Fatalf("AGY pace query failed: windows=%d err=%v", len(pace.Windows), err)
	}
}
