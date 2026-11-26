package quota_srv

import (
	"errors"
	"testing"
	"time"

	reportingv1 "github.com/SisyphusSQ/codex-pulse/api/codexpulse/reporting/v1"
	quota_dto "github.com/SisyphusSQ/codex-pulse/server/internal/models/dto/quota_dto"
	centerfixture "github.com/SisyphusSQ/codex-pulse/server/internal/testsupport/center"
	"github.com/SisyphusSQ/codex-pulse/server/utils"
)

func TestAccountModelUsageReconcilesAccountFactsAcrossModelsAndDevices(t *testing.T) {
	s, reporting, admin, clients := quotaFixture(t)
	reset := quotaNow + 3600000
	for _, raw := range []string{"a", "b"} {
		batch := accountFacts(raw)
		window := quotaFact("weekly-"+raw, raw, 10, quotaNow-500, reset)
		window.WindowMinutes = new(int64(10080))
		window.WindowKind = "secondary"
		batch.Quotas = []reportingv1.QuotaObservation{window}
		sendQuota(t, reporting, clients[0], batch)
		if raw == "a" {
			sendQuota(t, reporting, clients[1], batch)
		}
	}
	account := reportingv1.Key("codex", "a")
	summary := readQuota(t, s, admin, quota_dto.Query{AccountKey: account, View: "summary"})
	q := quota_dto.Query{AccountKey: account, WindowKey: summary.Windows[0].Key}
	out, err := s.AccountModelUsage(t.Context(), admin, q)
	if err != nil || out.RecordedTokens != nil || len(out.Models) != 0 {
		t.Fatalf("unreported usage: %+v %v", out, err)
	}
	sendQuota(t, reporting, clients[0], reportingv1.Batch{AccountUsage: []reportingv1.AccountTokenUsage{accountUsage("a", "", quotaNow-400, reset, 0)}})
	out, err = s.AccountModelUsage(t.Context(), admin, q)
	if err != nil || out.RecordedTokens == nil || *out.RecordedTokens != "0" || len(out.Models) != 0 {
		t.Fatalf("recorded empty usage: %+v %v", out, err)
	}

	snapshot := centerfixture.Snapshot()
	snapshot.CollectedAtMS = quotaNow
	first := accountUsage("a", snapshot.SessionID, quotaNow-300, reset, 100)
	fact := &first.Facts[0]
	fact.InputTokens, fact.CachedTokens, fact.OutputTokens, fact.ReasoningTokens = 80, 70, 12, new(int64(8))
	fact.ID = reportingv1.ContributionID("codex", snapshot.SessionID, fact.Contribution(), 0)
	second := accountUsage("a", snapshot.SessionID, quotaNow-280, reset, 60)
	unknown := accountUsage("a", snapshot.SessionID, quotaNow-260, reset, 20)
	late := accountUsage("a", snapshot.SessionID, quotaNow-220, reset, 10)
	otherAccount := accountUsage("b", snapshot.SessionID, quotaNow-240, reset, 1000)
	old := accountUsage("a", snapshot.SessionID, reset-10080*60000-1, reset-10080*60000, 900)
	snapshot.Contributions = []reportingv1.Contribution{
		old.Facts[0].Contribution(),
		fact.Contribution(),
		second.Facts[0].Contribution(),
		unknown.Facts[0].Contribution(),
		otherAccount.Facts[0].Contribution(),
	}
	snapshot.Contributions[1].Model = new("gpt-6.2-astra")
	snapshot.Contributions[2].Model = new("gpt-6.2-sol")
	snapshot.Contributions[4].Model = new("gpt-6.2-sol")
	// 每个账号每批仅有一个相同周期分组。使用多个正常批次模拟晚到及多设备副本。
	for _, usage := range []reportingv1.AccountTokenUsage{first, second, unknown, late, otherAccount, old} {
		sendQuota(t, reporting, clients[0], reportingv1.Batch{AccountUsage: []reportingv1.AccountTokenUsage{usage}})
	}
	sendQuota(t, reporting, clients[1], reportingv1.Batch{AccountUsage: []reportingv1.AccountTokenUsage{first}})
	centerfixture.SendSnapshot(t, reporting, clients[0], snapshot)
	out, err = s.AccountModelUsage(t.Context(), admin, q)
	if err != nil || out.RecordedTokens == nil || *out.RecordedTokens != "190" || len(out.Models) != 4 {
		t.Fatalf("model usage: %+v %v", out, err)
	}
	if astra := out.Models[0]; astra.Model == nil || *astra.Model != "gpt-6.2-astra" || astra.TotalTokens != "100" ||
		*astra.InputTokens != "80" || *astra.CachedTokens != "70" || *astra.OutputTokens != "12" || *astra.ReasoningTokens != "8" {
		t.Fatalf("Astra counters: %+v", astra)
	}
	if sol, unknown, late := out.Models[1], out.Models[2], out.Models[3]; sol.TotalTokens != "60" ||
		unknown.Kind != "unknown_model" || unknown.TotalTokens != "20" || unknown.InputTokens == nil ||
		late.Kind != "unmatched" || late.TotalTokens != "10" || late.InputTokens != nil {
		t.Fatalf("other rows: %+v %+v %+v", sol, unknown, late)
	}
	if summary := readQuota(t, s, admin, quota_dto.Query{AccountKey: account, View: "summary"}); *summary.Windows[0].RecordedTokens != *out.RecordedTokens {
		t.Fatalf("summary and detail disagree: %s vs %s", *summary.Windows[0].RecordedTokens, *out.RecordedTokens)
	}
	q.ClientID = clients[1].ID
	out, err = s.AccountModelUsage(t.Context(), admin, q)
	if err != nil || out.RecordedTokens == nil || *out.RecordedTokens != "100" || len(out.Models) != 1 {
		t.Fatalf("device scope: %+v %v", out, err)
	}

	snapshot.Revision++
	snapshot.Contributions = append(snapshot.Contributions, late.Facts[0].Contribution())
	snapshot.Contributions[len(snapshot.Contributions)-1].Model = new("gpt-6.2-sol")
	centerfixture.SendSnapshot(t, reporting, clients[0], snapshot)
	q.ClientID = ""
	out, err = s.AccountModelUsage(t.Context(), admin, q)
	if err != nil || len(out.Models) != 3 || out.Models[1].TotalTokens != "70" || *out.RecordedTokens != "190" {
		t.Fatalf("late usage did not fill details: %+v %v", out, err)
	}
	if _, err := s.AccountModelUsage(t.Context(), clients[0], q); !errors.Is(err, utils.ErrForbidden) {
		t.Fatalf("collector must be forbidden: %v", err)
	}
	wrong := q
	wrong.AccountKey = reportingv1.Key("codex", "b")
	if _, err := s.AccountModelUsage(t.Context(), admin, wrong); !errors.Is(err, utils.ErrNotFound) {
		t.Fatalf("another account must not resolve this window: %v", err)
	}
	s.now = func() time.Time { return time.UnixMilli(reset) }
	out, err = s.AccountModelUsage(t.Context(), admin, q)
	if err != nil || out.RecordedTokens != nil || len(out.Models) != 0 {
		t.Fatalf("expired usage: %+v %v", out, err)
	}
}

func TestAGYAccountModelUsageSupportsMultipleWindowsDisplayNamesAndDeviceScope(t *testing.T) {
	s, reporting, admin, clients := quotaFixture(t)
	scopeA := reportingv1.Key("agy-account", "user-a")
	scopeB := reportingv1.Key("agy-account", "user-b")
	localA := reportingv1.Key("agy-scope", clients[0].ID, scopeA)
	localA1 := reportingv1.Key("agy-scope", clients[1].ID, scopeA)
	localB := reportingv1.Key("agy-scope", clients[0].ID, scopeB)
	// Deliberately share the same resets_at_ms between a 5h window and a weekly window
	// to verify period IDs and duration matching stay strictly isolated.
	reset := quotaNow + 2*3600000
	start5h := reset - 300*60000
	startWeekly := reset - 10080*60000

	for idx, info := range []struct {
		clientIdx  int
		localScope string
	}{{0, localA}, {1, localA1}} {
		sendQuota(t, reporting, clients[info.clientIdx], reportingv1.Batch{
			Accounts: []reportingv1.Account{{Provider: "agy", ID: scopeA, Plan: new("Google AI Ultra 20×"), CollectedAtMS: quotaNow - 1000}},
			Bindings: []reportingv1.AccountBinding{{Provider: "agy", LocalScope: info.localScope, AccountID: scopeA, ConfirmedAtMS: quotaNow - 1000}},
			Quotas: []reportingv1.QuotaObservation{
				{
					Provider: "agy", ID: reportingv1.Key("agy-q", string(rune('0'+idx)), "5h"), AccountID: new(scopeA), LocalScope: info.localScope,
					LimitID: "agy.gemini", WindowKind: "primary", WindowMinutes: new(int64(300)),
					WindowStartAtMS: new(start5h), ResetsAtMS: new(reset), ObservedAtMS: quotaNow - 1000,
					UsedPercent: new(float64(20)), Validity: "accepted", Source: "agy_ls_rpc", HistoryOrigin: "confirmed",
				},
				{
					Provider: "agy", ID: reportingv1.Key("agy-q", string(rune('0'+idx)), "weekly"), AccountID: new(scopeA), LocalScope: info.localScope,
					LimitID: "agy.gemini", WindowKind: "secondary", WindowMinutes: new(int64(10080)),
					WindowStartAtMS: new(startWeekly), ResetsAtMS: new(reset), ObservedAtMS: quotaNow - 1000,
					UsedPercent: new(float64(10)), Validity: "accepted", Source: "agy_ls_rpc", HistoryOrigin: "confirmed",
				},
			},
		})
	}
	sendQuota(t, reporting, clients[0], reportingv1.Batch{
		Accounts: []reportingv1.Account{{Provider: "agy", ID: scopeB, Plan: new("Google AI Pro"), CollectedAtMS: quotaNow - 1000}},
		Bindings: []reportingv1.AccountBinding{{Provider: "agy", LocalScope: localB, AccountID: scopeB, ConfirmedAtMS: quotaNow - 1000}},
	})

	accountKeyA := reportingv1.Key("agy", scopeA)
	accountKeyB := reportingv1.Key("agy", scopeB)
	summary := readQuota(t, s, admin, quota_dto.Query{Provider: "agy", AccountKey: accountKeyA, View: "summary"})
	if len(summary.Windows) != 2 {
		t.Fatalf("expected 2 AGY windows, got %d", len(summary.Windows))
	}
	var key5h, keyWeekly string
	for _, w := range summary.Windows {
		if w.RecordedTokens != nil {
			t.Fatalf("unreported AGY window invented zero: %+v", w)
		}
		if *w.WindowMinutes == 300 {
			key5h = w.Key
		} else {
			keyWeekly = w.Key
		}
	}

	// Build AGY facts: one inside weekly-only (older than 5h), two inside both 5h & weekly (one with nil reasoning),
	// and one unmatched fact not yet in session snapshot.
	makeAGYFact := func(session string, at, in, cached, out int64, reasoning *int64) reportingv1.AccountTokenFact {
		f := reportingv1.AccountTokenFact{
			SessionID: session, ObservedAtMS: at,
			InputTokens: in, CachedTokens: cached, OutputTokens: out, ReasoningTokens: reasoning, TotalTokens: in + out,
		}
		f.ID = reportingv1.ContributionID("agy", session, f.Contribution(), 0)
		return f
	}
	oldWeeklyFact := makeAGYFact("agy-sess-1", start5h-3600000, 1000, 400, 200, new(int64(50)))
	recentCarbon := makeAGYFact("agy-sess-1", quotaNow-5000, 1500, 500, 100, new(int64(40)))
	recentClaudeNoReason := makeAGYFact("agy-sess-1", quotaNow-4000, 300, 100, 50, nil)
	recentUnmatched := makeAGYFact("agy-sess-1", quotaNow-3000, 90, 0, 10, nil)

	snap := reportingv1.SessionSnapshot{
		Provider: "agy", HomeID: "agy-home", SessionID: "agy-sess-1", Revision: 1,
		CollectedAtMS: quotaNow, Title: "AGY 模型明细会话", ProjectID: "p1", ProjectName: "proj",
		CreatedAtMS: new(start5h - 4000000), LastActiveAtMS: new(quotaNow - 3000),
		Complete: true, SourceKind: "agy_local", SessionKind: "session",
		Contributions: []reportingv1.Contribution{
			oldWeeklyFact.Contribution(),
			recentCarbon.Contribution(),
			recentClaudeNoReason.Contribution(),
		},
	}
	snap.Contributions[0].Model = new("gemini-4-carbon-high")
	snap.Contributions[1].Model = new("gemini-4-carbon-high")
	snap.Contributions[2].Model = new("claude-opus-5-5-high")
	centerfixture.SendSnapshot(t, reporting, clients[0], snap)

	// Send weekly period & facts on client 0, and duplicate recentCarbon on client 1.
	sendQuota(t, reporting, clients[0], reportingv1.Batch{AccountUsage: []reportingv1.AccountTokenUsage{{
		Provider: "agy", AccountID: scopeA, LocalScope: localA,
		WindowStartAtMS: startWeekly, ResetsAtMS: reset, CollectedAtMS: quotaNow - 2000,
		Facts: []reportingv1.AccountTokenFact{oldWeeklyFact, recentCarbon, recentClaudeNoReason, recentUnmatched},
	}}})
	sendQuota(t, reporting, clients[1], reportingv1.Batch{AccountUsage: []reportingv1.AccountTokenUsage{{
		Provider: "agy", AccountID: scopeA, LocalScope: localA1,
		WindowStartAtMS: startWeekly, ResetsAtMS: reset, CollectedAtMS: quotaNow - 2000,
		Facts: []reportingv1.AccountTokenFact{recentCarbon},
	}}})

	// Before 5h period is reported, 5h window must stay unknown while weekly is already available.
	summary = readQuota(t, s, admin, quota_dto.Query{Provider: "agy", AccountKey: accountKeyA, View: "summary"})
	for _, w := range summary.Windows {
		if *w.WindowMinutes == 300 && w.RecordedTokens != nil {
			t.Fatalf("weekly period falsely activated 5h window: %+v", w)
		}
		if *w.WindowMinutes == 10080 && (w.RecordedTokens == nil || *w.RecordedTokens != "3250") {
			t.Fatalf("weekly recorded tokens = %v, want 3250", w.RecordedTokens)
		}
	}

	// Now report empty 5h period marker (same resets_at_ms, different window_start_at_ms!).
	sendQuota(t, reporting, clients[0], reportingv1.Batch{AccountUsage: []reportingv1.AccountTokenUsage{{
		Provider: "agy", AccountID: scopeA, LocalScope: localA,
		WindowStartAtMS: start5h, ResetsAtMS: reset, CollectedAtMS: quotaNow - 1500,
		Facts: []reportingv1.AccountTokenFact{},
	}}})

	// Query weekly model usage (omit provider filter to also test provider auto-resolution from window).
	weeklyDetail, err := s.AccountModelUsage(t.Context(), admin, quota_dto.Query{AccountKey: accountKeyA, WindowKey: keyWeekly})
	if err != nil || weeklyDetail.RecordedTokens == nil || *weeklyDetail.RecordedTokens != "3250" || len(weeklyDetail.Models) != 3 {
		t.Fatalf("AGY weekly detail: %+v err=%v", weeklyDetail, err)
	}
	if carbon := weeklyDetail.Models[0]; carbon.Model == nil || *carbon.Model != "gemini-4-carbon-high" ||
		carbon.Name == nil || *carbon.Name != "Gemini 4 Carbon (High)" ||
		carbon.TotalTokens != "2800" || *carbon.InputTokens != "2500" || *carbon.CachedTokens != "900" ||
		*carbon.OutputTokens != "300" || *carbon.ReasoningTokens != "90" {
		t.Fatalf("AGY Carbon row mismatch: %+v", carbon)
	}
	if claude := weeklyDetail.Models[1]; claude.Model == nil || *claude.Model != "claude-opus-5-5-high" ||
		claude.Name == nil || *claude.Name != "Claude Opus 5.5 (High)" ||
		claude.TotalTokens != "350" || claude.ReasoningTokens != nil {
		t.Fatalf("AGY Claude nullable reasoning mismatch: %+v", claude)
	}
	if unmatched := weeklyDetail.Models[2]; unmatched.Kind != "unmatched" || unmatched.TotalTokens != "100" || unmatched.InputTokens != nil {
		t.Fatalf("AGY unmatched row mismatch: %+v", unmatched)
	}

	// Query 5h model usage: excludes oldWeeklyFact, reconciles to 1600 + 350 + 100 = 2050.
	fiveHDetail, err := s.AccountModelUsage(t.Context(), admin, quota_dto.Query{Provider: "agy", AccountKey: accountKeyA, WindowKey: key5h})
	if err != nil || fiveHDetail.RecordedTokens == nil || *fiveHDetail.RecordedTokens != "2050" || len(fiveHDetail.Models) != 3 {
		t.Fatalf("AGY 5h detail: %+v err=%v", fiveHDetail, err)
	}
	// Device 1 scope only sees recentCarbon (1600).
	dev1Detail, err := s.AccountModelUsage(t.Context(), admin, quota_dto.Query{Provider: "agy", AccountKey: accountKeyA, WindowKey: keyWeekly, ClientID: clients[1].ID})
	if err != nil || dev1Detail.RecordedTokens == nil || *dev1Detail.RecordedTokens != "1600" || len(dev1Detail.Models) != 1 {
		t.Fatalf("AGY device 1 detail: %+v err=%v", dev1Detail, err)
	}
	// Another AGY account cannot query Account A's window.
	if _, err := s.AccountModelUsage(t.Context(), admin, quota_dto.Query{Provider: "agy", AccountKey: accountKeyB, WindowKey: keyWeekly}); !errors.Is(err, utils.ErrNotFound) {
		t.Fatalf("cross-account AGY query not rejected: %v", err)
	}
}
