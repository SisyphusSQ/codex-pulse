package app

import (
	"testing"

	"github.com/SisyphusSQ/codex-pulse/internal/store"
)

func TestQuotaFreshnessDeadlineUsesTrustedBoundaries(t *testing.T) {
	snapshot := store.QuotaCurrentSnapshot{Windows: []store.QuotaCurrentWindowSnapshot{{Current: store.QuotaCurrent{FreshnessState: store.QuotaCurrentFresh, FreshUntilMS: new(int64(1000)), ResetsAtMS: new(int64(2000))}}}}
	for _, test := range []struct{ now, want int64 }{{999, 1001}, {1000, 1001}, {1001, 2000}} {
		if got := quotaFreshnessDeadline(snapshot, test.now); got == nil || *got != test.want {
			t.Fatalf("deadline(%d)=%v, want %d", test.now, got, test.want)
		}
	}
	if got := quotaFreshnessDeadline(snapshot, 2000); got != nil {
		t.Fatalf("expired boundary repeated: %d", *got)
	}
	snapshot.ResetCredits.NextExpiresAtMS = new(int64(500))
	if got := quotaFreshnessDeadline(snapshot, 1); got == nil || *got != 500 {
		t.Fatal("reset credit expiry not considered")
	}
}
