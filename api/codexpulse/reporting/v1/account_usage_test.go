package reportingv1

import (
	"math"
	"testing"
)

func TestAccountUsageStableIdentityAndBoundaries(t *testing.T) {
	f := AccountTokenFact{SessionID: "session", ObservedAtMS: 1000, InputTokens: 10, CachedTokens: 3, OutputTokens: 2, ReasoningTokens: 1, TotalTokens: 13}
	f.ID = ContributionID("codex", f.SessionID, f.Contribution(), 0)
	u := AccountTokenUsage{Provider: "codex", AccountID: "account-a", LocalScope: "scope-a", WindowStartAtMS: 0, ResetsAtMS: 10080 * 60000, CollectedAtMS: 2000, Facts: []AccountTokenFact{f}}
	if !u.Valid() {
		t.Fatal("valid fact rejected")
	}
	u.AccountID = "account-b"
	u.LocalScope = "scope-b"
	if !u.Valid() {
		t.Fatal("identity incorrectly depends on account")
	}
	for _, mutate := range []func(*AccountTokenUsage){func(u *AccountTokenUsage) { u.Facts[0].TotalTokens++ }, func(u *AccountTokenUsage) { u.Facts[0].ObservedAtMS = u.ResetsAtMS }, func(u *AccountTokenUsage) { u.Facts[0].Ordinal++ }, func(u *AccountTokenUsage) { u.Facts = append(u.Facts, u.Facts[0]) }, func(u *AccountTokenUsage) { u.LocalScope = "default" }, func(u *AccountTokenUsage) { u.Facts[0].InputTokens = math.MaxInt64 }, func(u *AccountTokenUsage) { u.ResetsAtMS++ }} {
		bad := u
		bad.Facts = append([]AccountTokenFact(nil), u.Facts...)
		mutate(&bad)
		if bad.Valid() {
			t.Fatal("invalid or ambiguous fact accepted")
		}
	}
	u.Facts = nil
	if !u.Valid() {
		t.Fatal("zero recording capability rejected")
	}
}
