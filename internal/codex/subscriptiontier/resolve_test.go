package subscriptiontier

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestResolveKnownProTiers(t *testing.T) {
	t.Parallel()
	for name, testCase := range map[string]struct {
		plan string
		want Tier
	}{
		"pro lite": {plan: " ProLite ", want: Tier5X},
		"pro":      {plan: "PRO", want: Tier20X},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			snapshot := Resolve(alignedEvidence(testCase.plan))
			assertValidSnapshot(t, snapshot)
			if snapshot.State != StateKnown || snapshot.Reason != ReasonExact ||
				snapshot.Tier == nil || *snapshot.Tier != testCase.want {
				t.Fatalf("Resolve(%q) = %#v", testCase.plan, snapshot)
			}
		})
	}
}

func TestResolveConflictFailsClosed(t *testing.T) {
	t.Parallel()
	for name, evidence := range map[string]Evidence{
		"account and rate limits": {
			AccountPlanType:          pointer("pro"),
			BeforeRateLimitPlanTypes: []string{"prolite"},
			AfterRateLimitPlanTypes:  []string{"prolite"},
		},
		"before buckets": {
			AccountPlanType:          pointer("pro"),
			BeforeRateLimitPlanTypes: []string{"pro", "prolite"},
			AfterRateLimitPlanTypes:  []string{"pro"},
		},
		"before and after": {
			AccountPlanType:          pointer("pro"),
			BeforeRateLimitPlanTypes: []string{"pro"},
			AfterRateLimitPlanTypes:  []string{"prolite"},
		},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			snapshot := Resolve(evidence)
			assertValidSnapshot(t, snapshot)
			if snapshot.State != StateConflict || snapshot.Tier != nil ||
				snapshot.Reason != ReasonSourceConflict {
				t.Fatalf("Resolve(conflict) = %#v", snapshot)
			}
		})
	}
}

func TestResolveUnknownAndNonProPlans(t *testing.T) {
	t.Parallel()
	for name, testCase := range map[string]struct {
		evidence Evidence
		state    State
		reason   string
	}{
		"missing": {
			evidence: Evidence{},
			state:    StateProUnknown,
			reason:   ReasonMissingPlan,
		},
		"unsupported": {
			evidence: alignedEvidence("codex_ultra"),
			state:    StateProUnknown,
			reason:   ReasonUnsupportedPlan,
		},
		"non pro": {
			evidence: alignedEvidence("plus"),
			state:    StateNotApplicable,
			reason:   ReasonNonProPlan,
		},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			snapshot := Resolve(testCase.evidence)
			assertValidSnapshot(t, snapshot)
			if snapshot.State != testCase.state || snapshot.Reason != testCase.reason || snapshot.Tier != nil {
				t.Fatalf("Resolve(%s) = %#v", name, snapshot)
			}
		})
	}
}

func TestResolveDoesNotLeakUnsupportedPlan(t *testing.T) {
	t.Parallel()
	const rawPlan = "internal_future_plan"
	snapshot := Resolve(alignedEvidence(rawPlan))
	encoded, err := json.Marshal(snapshot)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(encoded), rawPlan) {
		t.Fatalf("snapshot leaked raw plan: %s", encoded)
	}
}

func TestSnapshotValidateRejectsInvalidPresence(t *testing.T) {
	t.Parallel()
	for name, snapshot := range map[string]Snapshot{
		"known without tier": {
			State: StateKnown, Reason: ReasonExact,
		},
		"unknown with tier": {
			State: StateProUnknown, Tier: pointer(Tier5X), Reason: ReasonMissingPlan,
		},
		"wrong reason": {
			State: StateNotApplicable, Reason: ReasonUnsupportedPlan,
		},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			if err := snapshot.Validate(); err == nil {
				t.Fatalf("Validate(%s) succeeded: %#v", name, snapshot)
			}
		})
	}
}

func alignedEvidence(plan string) Evidence {
	return Evidence{
		AccountPlanType:          pointer(plan),
		BeforeRateLimitPlanTypes: []string{plan},
		AfterRateLimitPlanTypes:  []string{plan},
	}
}

func pointer[T any](value T) *T {
	return &value
}

func assertValidSnapshot(t *testing.T, snapshot Snapshot) {
	t.Helper()
	if err := snapshot.Validate(); err != nil {
		t.Fatalf("Validate() error = %v snapshot = %#v", err, snapshot)
	}
}
