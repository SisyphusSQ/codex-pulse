package quota_srv

import (
	"errors"
	"math"
	"testing"
	"time"

	reportingv1 "github.com/SisyphusSQ/codex-pulse/api/codexpulse/reporting/v1"
	quota_dto "github.com/SisyphusSQ/codex-pulse/server/internal/models/dto/quota_dto"
	"github.com/SisyphusSQ/codex-pulse/server/utils"
)

func accountUsage(raw, session string, at, reset, total int64) reportingv1.AccountTokenUsage {
	u := reportingv1.AccountTokenUsage{Provider: "codex", AccountID: raw, LocalScope: "scope-" + raw, WindowStartAtMS: reset - 10080*60000, ResetsAtMS: reset, CollectedAtMS: at}
	if session != "" {
		f := reportingv1.AccountTokenFact{SessionID: session, ObservedAtMS: at, InputTokens: total, TotalTokens: total}
		f.ID = reportingv1.ContributionID("codex", session, f.Contribution(), 0)
		u.Facts = []reportingv1.AccountTokenFact{f}
	}
	return u
}
func TestAccountTokensDeduplicateMachinesIsolateSwitchesAndKeepZeroDistinct(t *testing.T) {
	s, reporting, admin, clients := quotaFixture(t)
	reset := quotaNow + 3600000
	for _, raw := range []string{"a", "b"} {
		b := accountFacts(raw)
		q := quotaFact("weekly-"+raw, raw, 0, quotaNow-1000, reset)
		q.WindowMinutes = new(int64(10080))
		q.WindowKind = "secondary"
		b.Quotas = []reportingv1.QuotaObservation{q}
		sendQuota(t, reporting, clients[0], b)
	}
	read := func(raw, client string) *string {
		out := readQuota(t, s, admin, quota_dto.Query{AccountKey: reportingv1.Key("codex", raw), ClientID: client})
		if len(out.Windows) != 1 {
			t.Fatal("weekly missing")
		}
		return out.Windows[0].RecordedTokens
	}
	if read("a", "") != nil {
		t.Fatal("old client invented zero")
	}
	sendQuota(t, reporting, clients[0], reportingv1.Batch{AccountUsage: []reportingv1.AccountTokenUsage{accountUsage("a", "", quotaNow-500, reset, 0)}})
	if value := read("a", ""); value == nil || *value != "0" {
		t.Fatal("recorded zero unknown")
	}
	first := accountUsage("a", "same-session", quotaNow-400, reset, 100)
	for _, client := range clients {
		b := accountFacts("a")
		q := quotaFact("weekly-a", "a", 0, quotaNow-1000, reset)
		q.WindowMinutes = new(int64(10080))
		q.WindowKind = "secondary"
		b.Quotas = []reportingv1.QuotaObservation{q}
		b.AccountUsage = []reportingv1.AccountTokenUsage{first}
		sendQuota(t, reporting, client, b)
	}
	sendQuota(t, reporting, clients[0], reportingv1.Batch{AccountUsage: []reportingv1.AccountTokenUsage{accountUsage("b", "b-session", quotaNow-300, reset, 200), accountUsage("a", "a-return", quotaNow-200, reset, 50)}})
	if value := read("a", ""); value == nil || *value != "150" {
		t.Fatal("duplicate or return incorrect", value)
	}
	if value := read("b", ""); value == nil || *value != "200" {
		t.Fatal("switch mixed accounts")
	}
	if value := read("a", clients[1].ID); value == nil || *value != "100" {
		t.Fatal("device provenance lost")
	}
	conflict := first
	conflict.AccountID = "b"
	conflict.LocalScope = "scope-b"
	_, err := reporting.Accept(t.Context(), clients[0], reportingv1.Batch{Version: 1, ID: "00000000-0000-0000-0000-000000000099", AccountUsage: []reportingv1.AccountTokenUsage{conflict}})
	if !errors.Is(err, utils.ErrConflict) {
		t.Fatal("same fact reassigned", err)
	}
	if value := read("b", ""); value == nil || *value != "200" {
		t.Fatal("conflict transaction leaked")
	}
	old := accountUsage("a", "expired", reset-10080*60000-1000, reset-10080*60000, 900)
	sendQuota(t, reporting, clients[0], reportingv1.Batch{AccountUsage: []reportingv1.AccountTokenUsage{old}})
	if value := read("a", ""); value == nil || *value != "150" {
		t.Fatal("old period counted")
	}
	s.now = func() time.Time { return time.UnixMilli(reset + 1) }
	if read("a", "") != nil {
		t.Fatal("expired quota invented current token count")
	}
}
func TestAccountTokensAggregateBeyondInt64WithoutPrecisionLoss(t *testing.T) {
	s, reporting, admin, clients := quotaFixture(t)
	reset := quotaNow + 3600000
	b := accountFacts("big")
	q := quotaFact("weekly", "big", 0, quotaNow-1000, reset)
	q.WindowMinutes = new(int64(10080))
	b.Quotas = []reportingv1.QuotaObservation{q}
	b.AccountUsage = []reportingv1.AccountTokenUsage{accountUsage("big", "one", quotaNow-400, reset, math.MaxInt64), accountUsage("big", "two", quotaNow-300, reset, math.MaxInt64)}
	sendQuota(t, reporting, clients[0], b)
	out := readQuota(t, s, admin, quota_dto.Query{})
	if len(out.Windows) != 1 || out.Windows[0].RecordedTokens == nil || *out.Windows[0].RecordedTokens != "18446744073709551614" {
		t.Fatal("overflow")
	}
}
