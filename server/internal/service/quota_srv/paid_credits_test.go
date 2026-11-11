package quota_srv

import (
	"errors"
	"github.com/SisyphusSQ/codex-pulse/server/utils"
	"testing"
	"uuid"

	reportingv1 "github.com/SisyphusSQ/codex-pulse/api/codexpulse/reporting/v1"
	"github.com/SisyphusSQ/codex-pulse/internal/creditbalance"
	quota_dto "github.com/SisyphusSQ/codex-pulse/server/internal/models/dto/quota_dto"
)

func TestPaidCreditsOfficialZeroDeduplicatesDevicesAndKeepsConsumptionUnknown(t *testing.T) {
	s, report, admin, clients := quotaFixture(t)
	for _, client := range clients {
		batch := accountFacts("credit-account")
		batch.PaidCredits = []reportingv1.PaidCredit{{ID: "balance-zero", Provider: "codex", AccountID: new("credit-account"), LocalScope: "scope-credit-account", Snapshot: creditbalance.Snapshot{LimitID: "codex", State: "known", Balance: new("0"), HasCredits: new(false), Unlimited: new(false), ObservedAtMS: quotaNow - 100}}}
		sendQuota(t, report, client, batch)
		sendQuota(t, report, client, batch)
	}
	out := readQuota(t, s, admin, quota_dto.Query{})
	if len(out.PaidCredits) != 1 {
		t.Fatalf("per-device balance duplicated: %+v", out.PaidCredits)
	}
	c := out.PaidCredits[0].Credit
	if c.Balance == nil || *c.Balance != "0" || len(c.History) != 1 || c.ConsumptionState != "source_unavailable" {
		t.Fatalf("credit=%+v", c)
	}
	filtered := readQuota(t, s, admin, quota_dto.Query{ClientID: clients[0].ID})
	if len(filtered.PaidCredits) != 1 || *filtered.PaidCredits[0].Credit.Balance != "0" {
		t.Fatal("source filter discarded balance")
	}
}

func TestPaidCreditsRejectsChangedReplayAndUnboundAccount(t *testing.T) {
	s, report, admin, clients := quotaFixture(t)
	batch := accountFacts("paid-replay")
	fact := reportingv1.PaidCredit{ID: "stable-credit", Provider: "codex", AccountID: new("paid-replay"), LocalScope: "scope-paid-replay", Snapshot: creditbalance.Snapshot{LimitID: "codex", State: "known", Balance: new("10"), HasCredits: new(true), Unlimited: new(false), ObservedAtMS: quotaNow - 100}}
	batch.PaidCredits = []reportingv1.PaidCredit{fact}
	sendQuota(t, report, clients[0], batch)
	fact.Snapshot.Balance = new("9")
	changed := reportingv1.Batch{Version: 1, ID: uuid.New().String(), PaidCredits: []reportingv1.PaidCredit{fact}}
	if _, err := report.Accept(t.Context(), clients[0], changed); !errors.Is(err, utils.ErrConflict) {
		t.Fatalf("changed replay accepted: %v", err)
	}
	fact.ID = "unbound-credit"
	changed.ID = uuid.New().String()
	changed.PaidCredits = []reportingv1.PaidCredit{fact}
	if _, err := report.Accept(t.Context(), clients[1], changed); err == nil {
		t.Fatal("other collector borrowed account binding")
	}
	out := readQuota(t, s, admin, quota_dto.Query{})
	if len(out.PaidCredits) != 1 || out.PaidCredits[0].Credit.Balance == nil || *out.PaidCredits[0].Credit.Balance != "10" {
		t.Fatal("rejected transaction changed accepted balance")
	}
}
