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
	fact.InputTokens, fact.CachedTokens, fact.OutputTokens, fact.ReasoningTokens = 80, 70, 12, 8
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
