package subscriptiontier

import (
	"errors"
	"slices"
	"strings"
)

var ErrInvalidSnapshot = errors.New("invalid Codex Pro tier snapshot")

var knownNonProPlans = map[string]struct{}{
	"free":                            {},
	"go":                              {},
	"plus":                            {},
	"team":                            {},
	"self_serve_business_usage_based": {},
	"business":                        {},
	"enterprise_cbp_usage_based":      {},
	"enterprise":                      {},
	"edu":                             {},
}

func Resolve(evidence Evidence) Snapshot {
	plan, conflict, present := uniquePlan(evidence)
	switch {
	case conflict:
		return Snapshot{State: StateConflict, Reason: ReasonSourceConflict}
	case !present:
		return Snapshot{State: StateProUnknown, Reason: ReasonMissingPlan}
	}
	switch plan {
	case "prolite":
		return Snapshot{State: StateKnown, Tier: cloneTier(Tier5X), Reason: ReasonExact}
	case "pro":
		return Snapshot{State: StateKnown, Tier: cloneTier(Tier20X), Reason: ReasonExact}
	}
	if _, ok := knownNonProPlans[plan]; ok {
		return Snapshot{State: StateNotApplicable, Reason: ReasonNonProPlan}
	}
	return Snapshot{State: StateProUnknown, Reason: ReasonUnsupportedPlan}
}

func (snapshot Snapshot) Validate() error {
	switch snapshot.State {
	case StateKnown:
		if snapshot.Tier == nil || !validTier(*snapshot.Tier) || snapshot.Reason != ReasonExact {
			return ErrInvalidSnapshot
		}
	case StateProUnknown:
		if snapshot.Tier != nil ||
			(snapshot.Reason != ReasonMissingPlan && snapshot.Reason != ReasonUnsupportedPlan) {
			return ErrInvalidSnapshot
		}
	case StateConflict:
		if snapshot.Tier != nil || snapshot.Reason != ReasonSourceConflict {
			return ErrInvalidSnapshot
		}
	case StateNotApplicable:
		if snapshot.Tier != nil || snapshot.Reason != ReasonNonProPlan {
			return ErrInvalidSnapshot
		}
	default:
		return ErrInvalidSnapshot
	}
	return nil
}

func uniquePlan(evidence Evidence) (string, bool, bool) {
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

func validTier(value Tier) bool {
	return value == Tier5X || value == Tier20X
}

func cloneTier(value Tier) *Tier {
	copied := value
	return &copied
}
