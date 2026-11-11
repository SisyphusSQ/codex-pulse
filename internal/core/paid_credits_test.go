package core

import (
	"testing"

	corev1 "github.com/SisyphusSQ/codex-pulse/api/codexpulse/core/v1"
	"github.com/SisyphusSQ/codex-pulse/internal/codex/quota"
	"github.com/SisyphusSQ/codex-pulse/internal/creditbalance"
)

func TestPaidCreditsCoreMappingKeepsPresenceAndExactDecimal(t *testing.T) {
	source := quota.CurrentResponse{PaidCredits: []creditbalance.View{{Snapshot: creditbalance.Snapshot{LimitID: "codex", State: "known", Balance: new("0.000000000000000001"), HasCredits: new(false), Unlimited: new(false)}, Freshness: "fresh", ConsumptionState: "source_unavailable", History: []creditbalance.Point{{ObservedAtMS: 1, Balance: new("0")}}}}}
	var target corev1.CurrentQuota
	if err := EncodeResponse(source, &target); err != nil {
		t.Fatal(err)
	}
	if len(target.PaidCredits) != 1 || target.PaidCredits[0].Balance == nil || *target.PaidCredits[0].Balance != "0.000000000000000001" || target.PaidCredits[0].HasCredits == nil || *target.PaidCredits[0].HasCredits || len(target.PaidCredits[0].History) != 1 {
		t.Fatalf("mapping lost balance presence: %+v", target.PaidCredits)
	}
}
