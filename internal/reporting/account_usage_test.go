package reporting

import (
	"context"
	"encoding/json"
	"slices"
	"testing"

	reportingv1 "github.com/SisyphusSQ/codex-pulse/api/codexpulse/reporting/v1"
	"github.com/SisyphusSQ/codex-pulse/internal/store"
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
	a := accountTokenFacts(s, []store.ReportingAccountRange{{FromMS: 100, ThroughMS: 150}, {FromMS: 300, ThroughMS: 350}}, 1000)
	if len(a) != 3 || a[0].ObservedAtMS != 120 || a[1].Ordinal != 1 || a[2].ObservedAtMS != 320 {
		t.Fatal("cross-switch delta or ordinal changed", a)
	}
	b := accountTokenFacts(s, []store.ReportingAccountRange{{FromMS: 200, ThroughMS: 250}}, 1000)
	if len(b) != 1 || b[0].ObservedAtMS != 220 {
		t.Fatal("B included A")
	}
	if len(accountTokenFacts(s, []store.ReportingAccountRange{{FromMS: 100, ThroughMS: 150}}, 120)) != 0 {
		t.Fatal("reset boundary included")
	}
	s = usageSession(110)
	s.CreatedAtMS = new(int64(105))
	if len(accountTokenFacts(s, []store.ReportingAccountRange{{FromMS: 100, ThroughMS: 150}}, 1000)) != 1 {
		t.Fatal("new session first delta dropped")
	}
}
func TestAccountUsageOutboxBodySurvivesReopenAndDeduplicates(t *testing.T) {
	state, path := testState(t)
	s := usageSession(110, 120)
	u := reportingv1.AccountTokenUsage{Provider: "codex", AccountID: "a", LocalScope: "scope-a", WindowStartAtMS: 0, ResetsAtMS: accountUsageWindowMS, CollectedAtMS: 120, Facts: accountTokenFacts(s, []store.ReportingAccountRange{{FromMS: 100, ThroughMS: 150}}, accountUsageWindowMS)}
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
	cursor, err := state.cursor(t.Context(), "center", "codex-account-usage", "home")
	if err != nil {
		t.Fatal(err)
	}
	cursor.After = s.SessionID
	if err := state.saveCursor(t.Context(), cursor); err != nil {
		t.Fatal(err)
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
	resumed, err := reopened.cursor(t.Context(), "center", "codex-account-usage", "home")
	if err != nil || resumed.After != s.SessionID {
		t.Fatal("account usage cursor did not resume", resumed.After, err)
	}
	var batch reportingv1.Batch
	if err := json.Unmarshal(second.Body, &batch); err != nil || len(batch.AccountUsage) != 1 || len(batch.AccountUsage[0].Facts) != 1 {
		t.Fatal("account usage lost", err)
	}
}

func TestAccountUsageRestartRecoversProvenRangesAndKeepsUnknownGap(t *testing.T) {
	state, _ := testState(t)
	source := store.ReportingSource{HomeID: "home", HomeGeneration: 1}
	now := int64(400)
	e := &Exporter{state: state, startedAtMS: 300}
	a := AccountIdentity{Provider: "codex", LocalScope: "scope-a", AccountID: "a"}
	b := AccountIdentity{Provider: "codex", LocalScope: "scope-b", AccountID: "b"}
	binding := store.CodexAccountBinding{State: store.CodexAccountBindingConfirmed, AccountScope: new(a.LocalScope), BindingGeneration: 3, ObservedAtMS: 300}
	quotas := []reportingv1.QuotaObservation{
		{LocalScope: a.LocalScope, LimitID: "codex", Source: "app_server", Validity: "accepted", WindowMinutes: new(int64(10080)), ResetsAtMS: new(int64(accountUsageWindowMS + 105)), ObservedAtMS: 350},
		{LocalScope: b.LocalScope, LimitID: "codex", Source: "app_server", Validity: "accepted", WindowMinutes: new(int64(10080)), ResetsAtMS: new(int64(accountUsageWindowMS)), ObservedAtMS: 250},
	}
	ranges := []store.ReportingAccountRange{
		{AccountScope: a.LocalScope, BindingGeneration: 1, FromMS: 100, ThroughMS: 150},
		{AccountScope: b.LocalScope, BindingGeneration: 2, FromMS: 200, ThroughMS: 250},
		{AccountScope: a.LocalScope, BindingGeneration: 3, FromMS: 320, ThroughMS: 350},
	}
	plans, cutoff := e.accountUsagePlans(source, binding, []AccountIdentity{a, b}, quotas, ranges, 0, now)
	if len(plans) != 2 || cutoff != 105 {
		t.Fatalf("plans=%v cutoff=%d", plans, cutoff)
	}
	session := usageSession(90, 103, 110, 120, 170, 210, 220, 280, 310, 330, 340, 380)
	groups := e.accountUsageGroups(source.HomeID, plans, []reportingv1.SessionSnapshot{session})
	got := map[string][]int64{}
	for _, group := range groups {
		for _, usage := range group.Batch.AccountUsage {
			if !usage.Valid() {
				t.Fatal("invalid recovery payload")
			}
			for _, f := range usage.Facts {
				got[usage.AccountID] = append(got[usage.AccountID], f.ObservedAtMS)
			}
		}
	}
	if !slices.Equal(got["a"], []int64{120, 340}) || !slices.Equal(got["b"], []int64{220}) {
		t.Fatalf("restart, window or switch boundary changed: %v", got)
	}
}

func TestAccountUsageLateIndexKeepsExistingFactIdentity(t *testing.T) {
	state, _ := testState(t)
	e := &Exporter{state: state}
	s := usageSession(110, 120)
	plan := accountUsagePlan{
		identity: AccountIdentity{Provider: "codex", AccountID: "a", LocalScope: "private-scope"},
		period: reportingv1.AccountTokenUsage{
			Provider: "codex", AccountID: "a", LocalScope: "public-scope",
			WindowStartAtMS: 0, ResetsAtMS: accountUsageWindowMS, CollectedAtMS: 150,
		},
		ranges: []store.ReportingAccountRange{{FromMS: 100, ThroughMS: 150}},
	}
	before := e.accountUsageGroups("home", []accountUsagePlan{plan}, []reportingv1.SessionSnapshot{s})
	if len(before) != 2 || len(before[1].Batch.AccountUsage[0].Facts) != 1 {
		t.Fatal("first sweep lost account facts")
	}
	restarted := &Exporter{state: state}
	s = usageSession(110, 120, 130)
	after := restarted.accountUsageGroups("home", []accountUsagePlan{plan}, []reportingv1.SessionSnapshot{s})
	if len(after) != 2 || after[1].Key != before[1].Key {
		t.Fatal("late index changed the packet key")
	}
	facts := after[1].Batch.AccountUsage[0].Facts
	if len(facts) != 2 || facts[0] != before[1].Batch.AccountUsage[0].Facts[0] || facts[1].ObservedAtMS != 130 {
		t.Fatal("late sweep lost existing identity or the new fact", facts)
	}
}

func TestAccountUsagePeriodWaitsForObservedWindowStart(t *testing.T) {
	const observed = int64(1793291252894)
	const start = observed + 3106
	identity := AccountIdentity{Provider: "codex", LocalScope: "private-scope", AccountID: "account"}
	q := reportingv1.QuotaObservation{Provider: "codex", LocalScope: identity.LocalScope, LimitID: "codex", Source: "app_server", Validity: "accepted", WindowMinutes: new(int64(10080)), ResetsAtMS: new(start + 10080*60000), ObservedAtMS: observed}
	for _, now := range []int64{observed, start, start + 60000} {
		u := accountUsagePeriod([]reportingv1.QuotaObservation{q}, identity, "public-scope", now)
		if u.ResetsAtMS != 0 {
			t.Fatal("old observation became valid just by waiting", u)
		}
	}
	if q.ObservedAtMS != observed {
		t.Fatal("source observation changed")
	}
	state, _ := testState(t)
	for _, at := range []int64{start, start + 1} {
		q.ObservedAtMS = at
		u := accountUsagePeriod([]reportingv1.QuotaObservation{q}, identity, "public-scope", at)
		if !u.Valid() || u.CollectedAtMS != at || u.WindowStartAtMS != start || u.LocalScope != "public-scope" {
			t.Fatal("valid successor period lost", u)
		}
		if _, err := state.EnqueueFactGroups(t.Context(), "center", []FactsGroup{{Key: reportingv1.Key("period"), Batch: reportingv1.Batch{AccountUsage: []reportingv1.AccountTokenUsage{u}}}}); err != nil {
			t.Fatal(err)
		}
	}
	q.LocalScope = "other-account"
	if u := accountUsagePeriod([]reportingv1.QuotaObservation{q}, identity, "public-scope", start+1); u.ResetsAtMS != 0 {
		t.Fatal("another account supplied the period")
	}
}
