package subscriptionaccounts

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/SisyphusSQ/codex-pulse/internal/codex/subscriptiontier"
)

func TestResolveAutomaticPlanKnownProTiers(t *testing.T) {
	t.Parallel()
	for name, testCase := range map[string]struct {
		plan string
		want Plan
	}{
		"prolite": {plan: " ProLite ", want: PlanPro5X},
		"pro":     {plan: "PRO", want: PlanPro20X},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			fact := ResolveAutomaticPlan(alignedEvidence(testCase.plan))
			if err := fact.Validate(); err != nil {
				t.Fatal(err)
			}
			if fact.State != AutomaticPlanKnown || fact.Plan == nil || *fact.Plan != testCase.want {
				t.Fatalf("ResolveAutomaticPlan(%s) = %#v", name, fact)
			}
		})
	}
}

func TestResolveAutomaticPlanMapsNonProAliases(t *testing.T) {
	t.Parallel()
	for name, want := range map[string]Plan{
		"plus":                            PlanPlus,
		"team":                            PlanTeam,
		"self_serve_business_usage_based": PlanBusiness,
		"enterprise_cbp_usage_based":      PlanEnterprise,
		"edu_plus":                        PlanEdu,
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			fact := ResolveAutomaticPlan(alignedEvidence(name))
			if fact.State != AutomaticPlanKnown || fact.Plan == nil || *fact.Plan != want {
				t.Fatalf("ResolveAutomaticPlan(%s) = %#v", name, fact)
			}
		})
	}
}

func TestResolveAutomaticPlanUnknownAndConflict(t *testing.T) {
	t.Parallel()
	missing := ResolveAutomaticPlan(subscriptiontier.Evidence{})
	if missing.State != AutomaticPlanUnknown || missing.Plan != nil {
		t.Fatalf("missing = %#v", missing)
	}
	unsupported := ResolveAutomaticPlan(alignedEvidence("codex_ultra"))
	if unsupported.State != AutomaticPlanUnknown || unsupported.Plan != nil {
		t.Fatalf("unsupported = %#v", unsupported)
	}
	encoded, err := json.Marshal(unsupported)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(encoded), "codex_ultra") {
		t.Fatalf("automatic plan leaked token: %s", encoded)
	}
	conflict := ResolveAutomaticPlan(subscriptiontier.Evidence{
		AccountPlanType:          pointer("pro"),
		BeforeRateLimitPlanTypes: []string{"prolite"},
		AfterRateLimitPlanTypes:  []string{"prolite"},
	})
	if conflict.State != AutomaticPlanConflict || conflict.Plan != nil {
		t.Fatalf("conflict = %#v", conflict)
	}
}

func TestResolvePlanPriority(t *testing.T) {
	t.Parallel()
	manual := PlanPlus
	known, err := ResolvePlan(AutomaticPlanFact{State: AutomaticPlanKnown, Plan: clonePlan(PlanPro20X)}, &manual)
	if err != nil || known.Source != ValueSourceManual || known.Plan == nil || *known.Plan != PlanPlus {
		t.Fatalf("manual override = %#v %v", known, err)
	}
	unknown, err := ResolvePlan(AutomaticPlanFact{State: AutomaticPlanUnknown}, &manual)
	if err != nil || unknown.Source != ValueSourceManual || unknown.Plan == nil || *unknown.Plan != PlanPlus {
		t.Fatalf("unknown fallback = %#v %v", unknown, err)
	}
	conflict, err := ResolvePlan(AutomaticPlanFact{State: AutomaticPlanConflict}, &manual)
	if err != nil || conflict.Source != ValueSourceManual || conflict.Plan == nil || *conflict.Plan != PlanPlus {
		t.Fatalf("conflict fallback = %#v %v", conflict, err)
	}
	empty, err := ResolvePlan(AutomaticPlanFact{State: AutomaticPlanUnavailable}, nil)
	if err != nil || empty.Source != ValueSourceUnavailable || empty.Plan != nil {
		t.Fatalf("both missing = %#v %v", empty, err)
	}
}

func TestAutomaticPlanFactValidateRejectsInvalidPresence(t *testing.T) {
	t.Parallel()
	if err := (AutomaticPlanFact{State: AutomaticPlanKnown}).Validate(); err != ErrInvalidAutomaticPlanFact {
		t.Fatalf("known without plan error = %v", err)
	}
	if err := (AutomaticPlanFact{State: AutomaticPlanUnknown, Plan: clonePlan(PlanPlus)}).Validate(); err != ErrInvalidAutomaticPlanFact {
		t.Fatalf("unknown with plan error = %v", err)
	}
}

func alignedEvidence(plan string) subscriptiontier.Evidence {
	return subscriptiontier.Evidence{
		AccountPlanType:          pointer(plan),
		BeforeRateLimitPlanTypes: []string{plan},
		AfterRateLimitPlanTypes:  []string{plan},
	}
}
