package quota

import (
	"fmt"
	"strings"
	"testing"

	"github.com/SisyphusSQ/codex-pulse/internal/codex/appserver"
	"github.com/SisyphusSQ/codex-pulse/internal/store"
)

func TestResetCreditsClientFetchesTypedReadOnlySnapshot(t *testing.T) {
	t.Parallel()

	key := testScopeKey(0x41)
	request := testBoundRequest(t, key, "acct-test-a", "reset-client-success")
	granted := int64(1_783_000_000)
	expiresA := int64(1_783_010_800)
	expiresB := int64(1_783_014_400)
	expiresC := int64(1_784_000_000)
	snapshot := testRateLimitsSnapshot("acct-test-a", 1, 1, &appserver.RateLimitResetCreditsSummary{
		AvailableCount: 2,
		Credits: []appserver.RateLimitResetCredit{
			{
				ID: "credit-private-a", Status: "available", ResetType: "codexRateLimits",
				GrantedAtSeconds: granted, ExpiresAtSeconds: &expiresA,
			},
			{
				ID: "credit-private-b", Status: "available", ResetType: "codexRateLimits",
				GrantedAtSeconds: granted, ExpiresAtSeconds: &expiresB,
			},
			{
				ID: "credit-private-c", Status: "redeemed", ResetType: "codexRateLimits",
				GrantedAtSeconds: granted, ExpiresAtSeconds: &expiresC,
			},
		},
	})
	client := mustResetCreditsClient(t, key, &scriptedRateLimitsReader{snapshots: []appserver.AccountRateLimitsSnapshot{snapshot}})
	result, err := client.Fetch(t.Context(), request)
	if err != nil {
		t.Fatalf("Fetch() error = %v", err)
	}
	if result.Failure != nil || result.Snapshot == nil || result.AttemptCount != 1 ||
		result.HTTPStatus != nil || len(result.Snapshot.Credits) != 3 ||
		result.Snapshot.AvailableCount != 2 || result.Snapshot.DetailsStatus != store.ResetCreditDetailsComplete ||
		result.PayloadSHA256 == nil {
		t.Fatalf("result = %#v", result)
	}
	if got := result.Snapshot.Credits[0].CreditIDHash.String(); got == "" || strings.Contains(got, "private") {
		t.Fatalf("credit digest = %q", got)
	}
	serialized := fmt.Sprintf("%#v", result)
	for _, marker := range []string{"credit-private-a", "credit-private-b", "credit-private-c", "acct-test-a"} {
		if strings.Contains(serialized, marker) {
			t.Fatalf("result leaked %q: %s", marker, serialized)
		}
	}
}

func TestResetCreditsClientFailsClosedOnInvalidPayloads(t *testing.T) {
	t.Parallel()

	key := testScopeKey(0x42)
	request := testBoundRequest(t, key, "acct-test-a", "reset-invalid")
	tests := []struct {
		name     string
		snapshot appserver.AccountRateLimitsSnapshot
	}{
		{
			name: "missing credits object",
			snapshot: func() appserver.AccountRateLimitsSnapshot {
				value := testRateLimitsSnapshot("acct-test-a", 1, 1, nil)
				return value
			}(),
		},
		{
			name: "missing credit id",
			snapshot: testRateLimitsSnapshot("acct-test-a", 1, 1, &appserver.RateLimitResetCreditsSummary{
				AvailableCount: 1,
				Credits:        []appserver.RateLimitResetCredit{{Status: "available", ResetType: "codexRateLimits", GrantedAtSeconds: 1_783_000_000}},
			}),
		},
	}
	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			client := mustResetCreditsClient(t, key, &scriptedRateLimitsReader{
				snapshots: []appserver.AccountRateLimitsSnapshot{testCase.snapshot},
			})
			result, err := client.Fetch(t.Context(), request)
			if err != nil || result.Failure == nil || result.Failure.Code != store.SourceFailureSchemaIncompatible ||
				result.Snapshot != nil {
				t.Fatalf("Fetch() = %#v, %v", result, err)
			}
		})
	}
}
