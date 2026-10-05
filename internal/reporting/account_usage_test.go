package reporting

import (
	"context"
	"encoding/json"
	"testing"

	reportingv1 "github.com/SisyphusSQ/codex-pulse/api/codexpulse/reporting/v1"
)

func usageSession(times ...int64) reportingv1.SessionSnapshot {
	s := reportingv1.SessionSnapshot{Provider: "codex", SessionID: "one", CreatedAtMS: new(int64(1))}
	ordinals := map[string]int64{}
	for _, at := range times {
		c := reportingv1.Contribution{ObservedAtMS: new(at), InputTokens: new(int64(10)), CachedTokens: new(int64(2)), OutputTokens: new(int64(3)), ReasoningTokens: new(int64(4)), TotalTokens: new(int64(17))}
		base := reportingv1.ContributionID("codex", s.SessionID, c, 0)
		c.ID = reportingv1.ContributionID("codex", s.SessionID, c, ordinals[base])
		ordinals[base]++
		s.Contributions = append(s.Contributions, c)
	}
	return s
}
func TestAccountTokenFactsSwitchRestartAndRepeatedOrdinals(t *testing.T) {
	s := usageSession(90, 110, 120, 120, 210, 220, 310, 320)
	a := accountTokenFacts(s, 100, 150, 1000)
	if len(a) != 2 || a[0].ObservedAtMS != 120 || a[1].Ordinal != 1 {
		t.Fatal("cross-switch delta or ordinal changed", a)
	}
	b := accountTokenFacts(s, 200, 250, 1000)
	if len(b) != 1 || b[0].ObservedAtMS != 220 {
		t.Fatal("B included A")
	}
	a2 := accountTokenFacts(s, 300, 350, 1000)
	if len(a2) != 1 || a2[0].ObservedAtMS != 320 {
		t.Fatal("A return replayed old history")
	}
	if len(accountTokenFacts(s, 400, 450, 1000)) != 0 {
		t.Fatal("restart backfilled history")
	}
	if len(accountTokenFacts(s, 100, 150, 120)) != 0 {
		t.Fatal("reset boundary included")
	}
	s = usageSession(110)
	s.CreatedAtMS = new(int64(105))
	if len(accountTokenFacts(s, 100, 150, 1000)) != 1 {
		t.Fatal("new session first delta dropped")
	}
}
func TestAccountUsageOutboxBodySurvivesReopenAndDeduplicates(t *testing.T) {
	state, path := testState(t)
	s := usageSession(110, 120)
	u := reportingv1.AccountTokenUsage{Provider: "codex", AccountID: "a", LocalScope: "scope-a", WindowStartAtMS: 0, ResetsAtMS: 10080 * 60000, CollectedAtMS: 120, Facts: accountTokenFacts(s, 100, 150, 10080*60000)}
	groups := []FactsGroup{{Key: reportingv1.Key("usage"), Batch: reportingv1.Batch{AccountUsage: []reportingv1.AccountTokenUsage{u}}}}
	if changed, err := state.EnqueueFactGroups(t.Context(), "center", groups, -1); err != nil || !changed {
		t.Fatal(err)
	}
	first, err := state.next(t.Context(), "center")
	if err != nil {
		t.Fatal(err)
	}
	if changed, err := state.EnqueueFactGroups(t.Context(), "center", groups, -1); err != nil || changed {
		t.Fatal("replayed page queued twice", err)
	}
	if err := state.Close(t.Context()); err != nil {
		t.Fatal(err)
	}
	reopened, err := OpenState(t.Context(), path)
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close(context.Background())
	second, err := reopened.next(t.Context(), "center")
	if err != nil || string(first.Body) != string(second.Body) {
		t.Fatal("immutable queue changed", err)
	}
	var batch reportingv1.Batch
	if err := json.Unmarshal(second.Body, &batch); err != nil || len(batch.AccountUsage) != 1 || len(batch.AccountUsage[0].Facts) != 1 {
		t.Fatal("account usage lost", err)
	}
}
