package reportingv1

import (
	"encoding/json"
	"math"
	"testing"
)

func TestAccountUsageStableIdentityAndBoundaries(t *testing.T) {
	f := AccountTokenFact{SessionID: "session", ObservedAtMS: 1000, InputTokens: 10, CachedTokens: 3, OutputTokens: 2, ReasoningTokens: new(int64(1)), TotalTokens: 13}
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
	for _, mutate := range []func(*AccountTokenUsage){
		func(u *AccountTokenUsage) { u.Facts[0].TotalTokens++ },
		func(u *AccountTokenUsage) { u.Facts[0].ObservedAtMS = u.ResetsAtMS },
		func(u *AccountTokenUsage) { u.Facts[0].Ordinal++ },
		func(u *AccountTokenUsage) { u.Facts = append(u.Facts, u.Facts[0]) },
		func(u *AccountTokenUsage) { u.LocalScope = "default" },
		func(u *AccountTokenUsage) { u.Facts[0].InputTokens = math.MaxInt64 },
		func(u *AccountTokenUsage) { u.Facts[0].ReasoningTokens = nil },
		func(u *AccountTokenUsage) { u.ResetsAtMS++ },
	} {
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

func TestAccountUsageAGYMultiWindowAndNullableReasoning(t *testing.T) {
	// AGY: TotalTokens == InputTokens + OutputTokens (thinking/reasoning is included in output),
	// and ReasoningTokens may be nil when unknown while preserving exact ContributionID parity.
	withReasoning := AccountTokenFact{
		SessionID: "agy-sess-1", ObservedAtMS: 1000,
		InputTokens: 1500, CachedTokens: 500, OutputTokens: 100, ReasoningTokens: new(int64(40)), TotalTokens: 1600,
	}
	withReasoning.ID = ContributionID("agy", withReasoning.SessionID, withReasoning.Contribution(), 0)
	withoutReasoning := AccountTokenFact{
		SessionID: "agy-sess-1", ObservedAtMS: 2000,
		InputTokens: 300, CachedTokens: 100, OutputTokens: 50, ReasoningTokens: nil, TotalTokens: 350,
	}
	withoutReasoning.ID = ContributionID("agy", withoutReasoning.SessionID, withoutReasoning.Contribution(), 0)
	// Confirm ContributionID matches an exported AGY Contribution with nil ReasoningTokens.
	exported := Contribution{
		ObservedAtMS: new(int64(2000)),
		InputTokens:  new(int64(300)), CachedTokens: new(int64(100)),
		OutputTokens: new(int64(50)), ReasoningTokens: nil, TotalTokens: new(int64(350)),
	}
	if withoutReasoning.ID != ContributionID("agy", "agy-sess-1", exported, 0) {
		t.Fatal("AGY nil-reasoning ContributionID mismatch")
	}

	for _, minutes := range []int64{300, 10080} {
		u := AccountTokenUsage{
			Provider: "agy", AccountID: "agy-acct-a", LocalScope: "agy-scope-a",
			WindowStartAtMS: 0, ResetsAtMS: minutes * 60000, CollectedAtMS: 3000,
			Facts: []AccountTokenFact{withReasoning, withoutReasoning},
		}
		if !u.Valid() {
			t.Fatalf("valid AGY %dm account usage rejected", minutes)
		}
		raw, err := json.Marshal(u)
		if err != nil {
			t.Fatal(err)
		}
		var decoded AccountTokenUsage
		if err := json.Unmarshal(raw, &decoded); err != nil {
			t.Fatal(err)
		}
		if !decoded.Valid() || decoded.Facts[0].ReasoningTokens == nil || *decoded.Facts[0].ReasoningTokens != 40 || decoded.Facts[1].ReasoningTokens != nil {
			t.Fatalf("AGY JSON roundtrip broke reasoning semantics: %+v", decoded.Facts)
		}
	}

	// Reject AGY fact where reasoning was erroneously added onto total, or non-minute window.
	badTotal := AccountTokenUsage{
		Provider: "agy", AccountID: "agy-acct-a", LocalScope: "agy-scope-a",
		WindowStartAtMS: 0, ResetsAtMS: 300 * 60000, CollectedAtMS: 3000,
		Facts: []AccountTokenFact{withReasoning},
	}
	badTotal.Facts[0].TotalTokens = 1640
	badTotal.Facts[0].ID = ContributionID("agy", badTotal.Facts[0].SessionID, badTotal.Facts[0].Contribution(), 0)
	if badTotal.Valid() {
		t.Fatal("AGY double-counted reasoning total accepted")
	}
	badSpan := badTotal
	badSpan.Facts = nil
	badSpan.ResetsAtMS = 300*60000 + 1
	if badSpan.Valid() {
		t.Fatal("non-minute aligned window accepted")
	}
}
