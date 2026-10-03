package quota_srv

import (
	"testing"

	reporting_do "github.com/SisyphusSQ/codex-pulse/server/internal/models/do/mysql/reporting_do"
)

func TestCreditsWithoutValidObservationDoNotPublishInventory(t *testing.T) {
	for _, status := range []string{"unavailable", "unknown"} {
		rows := []reporting_do.ResetCredits{{ID: "invalid", Provider: "codex", ClientID: "synthetic-client", AccountKey: new("synthetic-account"), ObservedAtMS: quotaNow, Inventory: new(int64(3)), Status: status, DetailsStatus: "complete", ExpirySchedule: `[{"count":"3","expires_at_ms":null}]`}}
		out, err := buildCredits(rows, quotaNow)
		if err != nil {
			t.Fatal(err)
		}
		if len(out) != 1 || out[0].ObservedInventory != nil || out[0].AvailableInventory != nil || out[0].SnapshotAvailableInventory != nil || out[0].SnapshotNextExpiresAtMS != nil {
			t.Fatalf("invalid observation published inventory: %#v", out)
		}
	}
}
