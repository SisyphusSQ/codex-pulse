// Package subscriptiontier resolves the display-only Codex Pro tier from
// account plan evidence collected by the App Server sandwich read.
package subscriptiontier

const ContractVersion = "codex-pro-tier-v1"

type Tier string

const (
	Tier5X  Tier = "CODEX_PRO_TIER_5X"
	Tier20X Tier = "CODEX_PRO_TIER_20X"
)

type State string

const (
	StateKnown         State = "CODEX_PRO_TIER_STATE_KNOWN"
	StateProUnknown    State = "CODEX_PRO_TIER_STATE_PRO_UNKNOWN"
	StateConflict      State = "CODEX_PRO_TIER_STATE_CONFLICT"
	StateNotApplicable State = "CODEX_PRO_TIER_STATE_NOT_APPLICABLE"
)

const (
	ReasonExact           = "exact"
	ReasonMissingPlan     = "missing_plan_type"
	ReasonUnsupportedPlan = "unsupported_plan_type"
	ReasonSourceConflict  = "source_conflict"
	ReasonNonProPlan      = "non_pro_plan"
)

type Evidence struct {
	AccountPlanType          *string
	BeforeRateLimitPlanTypes []string
	AfterRateLimitPlanTypes  []string
}

type Snapshot struct {
	State  State  `json:"state"`
	Tier   *Tier  `json:"tier,omitempty"`
	Reason string `json:"reason"`
}
