package quota_srv

import (
	"testing"

	reportingv1 "github.com/SisyphusSQ/codex-pulse/api/codexpulse/reporting/v1"
	quota_dto "github.com/SisyphusSQ/codex-pulse/server/internal/models/dto/quota_dto"
)

func TestCodexPlanWindowsFilterSQLBeforeProjectionAndRetainRawHistory(t *testing.T) {
	s, r, admin, clients := quotaFixture(t)
	for _, plan := range []string{"pro", "prolite", "pro5x", "pro20x", "plus"} {
		b := accountFacts(plan)
		b.Accounts[0].Plan = new(plan)
		for _, minutes := range []int64{300, 10080, 43200} {
			fact := quotaFact(plan+string(rune(minutes)), plan, 20, quotaNow-1000, quotaNow+1000)
			fact.WindowMinutes = new(minutes)
			b.Quotas = append(b.Quotas, fact)
		}
		sendQuota(t, r, clients[0], b)
		key := reportingv1.Key("codex", plan)
		for _, view := range []string{"summary", "", "evidence"} {
			out := readQuota(t, s, admin, quota_dto.Query{AccountKey: key, View: view})
			want := 1
			if plan == "plus" {
				want = 2
			}
			if len(out.Windows) != want {
				t.Fatalf("%s %s windows=%d", plan, view, len(out.Windows))
			}
			for _, w := range out.Windows {
				if *w.WindowMinutes == 43200 || plan != "plus" && *w.WindowMinutes != 10080 {
					t.Fatal("unexpected plan window")
				}
			}
		}
		raw, err := s.repository.Windows(t.Context(), quota_dto.Query{RawHistory: true, AccountKey: key})
		if err != nil || len(raw) != 3 {
			t.Fatal("raw history lost", err)
		}
	}
	fact := quotaFact("unassigned", "unused", 20, quotaNow-1000, quotaNow+1000)
	fact.AccountID = nil
	fact.HistoryOrigin = "pending_association"
	sendQuota(t, r, clients[0], reportingv1.Batch{Quotas: []reportingv1.QuotaObservation{fact}, Credits: []reportingv1.ResetCredits{{Provider: "codex", ID: "unassigned", LocalScope: "unused", ObservedAtMS: quotaNow, Status: "accepted", Inventory: new(int64(2)), DetailsStatus: "unavailable"}}})
	out := readQuota(t, s, admin, quota_dto.Query{})
	if len(out.Windows) != 6 || len(out.Credits) != 0 {
		t.Fatal("unassigned quota/credits leaked")
	}
}
