package subscriptionaccounts

import (
	"slices"
	"strings"

	"github.com/SisyphusSQ/codex-pulse/internal/codex/subscriptiontier"
)

var publicPlanAliases = map[string]Plan{
	"free":                            PlanFree,
	"go":                              PlanGo,
	"plus":                            PlanPlus,
	"team":                            PlanTeam,
	"business":                        PlanBusiness,
	"self_serve_business_usage_based": PlanBusiness,
	"self_serve_business_prolite":     PlanBusiness,
	"enterprise":                      PlanEnterprise,
	"enterprise_cbp_usage_based":      PlanEnterprise,
	"enterprise_cbp_automation":       PlanEnterprise,
	"ent26":                           PlanEnterprise,
	"edu":                             PlanEdu,
	"edu_plus":                        PlanEdu,
	"edu_pro":                         PlanEdu,
}

func ResolveAutomaticPlan(evidence subscriptiontier.Evidence) AutomaticPlanFact {
	snapshot := subscriptiontier.Resolve(evidence)
	switch snapshot.State {
	case subscriptiontier.StateConflict:
		return AutomaticPlanFact{State: AutomaticPlanConflict}
	case subscriptiontier.StateKnown:
		if snapshot.Tier != nil && *snapshot.Tier == subscriptiontier.Tier5X {
			return AutomaticPlanFact{State: AutomaticPlanKnown, Plan: clonePlan(PlanPro5X)}
		}
		if snapshot.Tier != nil && *snapshot.Tier == subscriptiontier.Tier20X {
			return AutomaticPlanFact{State: AutomaticPlanKnown, Plan: clonePlan(PlanPro20X)}
		}
		return AutomaticPlanFact{State: AutomaticPlanUnknown}
	}
	token, conflict, present := uniquePlanToken(evidence)
	if conflict {
		return AutomaticPlanFact{State: AutomaticPlanConflict}
	}
	if !present {
		return AutomaticPlanFact{State: AutomaticPlanUnknown}
	}
	plan, ok := publicPlanAliases[token]
	if !ok {
		return AutomaticPlanFact{State: AutomaticPlanUnknown}
	}
	return AutomaticPlanFact{State: AutomaticPlanKnown, Plan: clonePlan(plan)}
}

func ResolvePlan(automatic AutomaticPlanFact, manual *Plan) (ResolvedPlan, error) {
	if err := automatic.Validate(); err != nil {
		return ResolvedPlan{}, err
	}
	if manual != nil {
		if err := ParsePlan(*manual); err != nil {
			return ResolvedPlan{}, err
		}
	}
	if manual != nil {
		return ResolvedPlan{Plan: clonePlan(*manual), Source: ValueSourceManual}, nil
	}
	if automatic.State == AutomaticPlanKnown {
		return ResolvedPlan{Plan: clonePlan(*automatic.Plan), Source: ValueSourceAutomatic}, nil
	}
	return ResolvedPlan{Source: ValueSourceUnavailable}, nil
}

func (fact AutomaticPlanFact) Validate() error {
	switch fact.State {
	case AutomaticPlanKnown:
		if fact.Plan == nil || ParsePlan(*fact.Plan) != nil {
			return ErrInvalidAutomaticPlanFact
		}
	case AutomaticPlanUnavailable, AutomaticPlanUnknown, AutomaticPlanConflict:
		if fact.Plan != nil {
			return ErrInvalidAutomaticPlanFact
		}
	default:
		return ErrInvalidAutomaticPlanFact
	}
	return nil
}

func uniquePlanToken(evidence subscriptiontier.Evidence) (string, bool, bool) {
	values := make([]string, 0, 1+len(evidence.BeforeRateLimitPlanTypes)+len(evidence.AfterRateLimitPlanTypes))
	if plan := normalizePlanToken(evidence.AccountPlanType); plan != "" {
		values = append(values, plan)
	}
	before, beforeConflict := uniqueNormalized(evidence.BeforeRateLimitPlanTypes)
	after, afterConflict := uniqueNormalized(evidence.AfterRateLimitPlanTypes)
	if beforeConflict || afterConflict {
		return "", true, true
	}
	values = append(values, before...)
	values = append(values, after...)
	compact := uniqueSorted(values)
	switch len(compact) {
	case 0:
		return "", false, false
	case 1:
		return compact[0], false, true
	default:
		return "", true, true
	}
}

func uniqueNormalized(values []string) ([]string, bool) {
	normalized := make([]string, 0, len(values))
	for _, value := range values {
		plan := strings.ToLower(strings.TrimSpace(value))
		if plan != "" {
			normalized = append(normalized, plan)
		}
	}
	compact := uniqueSorted(normalized)
	return compact, len(compact) > 1
}

func uniqueSorted(values []string) []string {
	if len(values) == 0 {
		return nil
	}
	cloned := append([]string(nil), values...)
	slices.Sort(cloned)
	return slices.Compact(cloned)
}

func normalizePlanToken(value *string) string {
	if value == nil {
		return ""
	}
	return strings.ToLower(strings.TrimSpace(*value))
}

func clonePlan(value Plan) *Plan {
	copied := value
	return &copied
}
