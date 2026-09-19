package subscriptionaccounts

const ContractVersion = "codex-subscription-accounts-v2"

type Plan string

const (
	PlanFree       Plan = "free"
	PlanGo         Plan = "go"
	PlanPlus       Plan = "plus"
	PlanPro5X      Plan = "pro_5x"
	PlanPro20X     Plan = "pro_20x"
	PlanTeam       Plan = "team"
	PlanBusiness   Plan = "business"
	PlanEnterprise Plan = "enterprise"
	PlanEdu        Plan = "edu"
)

type AutomaticPlanState string

const (
	AutomaticPlanUnavailable AutomaticPlanState = "unavailable"
	AutomaticPlanKnown       AutomaticPlanState = "known"
	AutomaticPlanUnknown     AutomaticPlanState = "unknown"
	AutomaticPlanConflict    AutomaticPlanState = "conflict"
)

type ValueSource string

const (
	ValueSourceUnavailable ValueSource = "unavailable"
	ValueSourceAutomatic   ValueSource = "automatic"
	ValueSourceManual      ValueSource = "manual"
)

type DateKind string

const (
	DateKindNextRenewal      DateKind = "next_renewal"
	DateKindMembershipExpiry DateKind = "membership_expiry"
)

type DateState string

const (
	DateStateUnavailable DateState = "unavailable"
	DateStateFuture      DateState = "future"
	DateStateToday       DateState = "today"
	DateStateNeedsUpdate DateState = "needs_update"
)

type AutomaticSource string

const AutomaticSourceAccountSandwich AutomaticSource = "account_sandwich"

type AutomaticDateCapability string

const AutomaticDateCapabilityManualOnly AutomaticDateCapability = "manual_only"

type MutationResult string

const (
	MutationApplied  MutationResult = "applied"
	MutationNoop     MutationResult = "noop"
	MutationConflict MutationResult = "conflict"
)

const (
	ReasonRevisionChanged                 = "revision_changed"
	ReasonAlreadyLinked                   = "already_linked"
	ReasonLinkTargetChanged               = "link_target_changed"
	ReasonManualEmailRequiredBeforeUnlink = "manual_email_required_before_unlink"
	ReasonRequestIDReused                 = "request_id_reused"
	ReasonCurrentAccount                  = "current_account"
)

const LinkCandidateSameEmail = "same_email"

type Mutation struct {
	Result string
	Reason *string
}

type BindingState string

const (
	BindingUnknown             BindingState = "unknown"
	BindingPending             BindingState = "pending"
	BindingConfirmed           BindingState = "confirmed"
	BindingSignedOut           BindingState = "signed_out"
	BindingIdentityUnavailable BindingState = "identity_unavailable"
)

type AccountFence struct {
	AccountScope      string
	BindingGeneration int64
}

type Binding struct {
	State             BindingState
	AccountScope      *string
	BindingGeneration int64
}

type DetectedAccount struct {
	AccountScope              string
	DetectedAccountID         string
	DetectedEmail             *string
	EmailMatchKey             *string
	DetectedEmailObservedAtMS *int64
	AutomaticPlan             *Plan
	AutomaticPlanState        AutomaticPlanState
	AutomaticPlanObservedAtMS *int64
	Revision                  int64
}

type ManualEntry struct {
	ManualEntryID  string
	Email          *string
	EmailMatchKey  *string
	Alias          *string
	ManualPlan     *Plan
	MembershipDate *string
	DateKind       *DateKind
	Revision       int64
	CreatedAtMS    int64
	UpdatedAtMS    int64
}

type Link struct {
	AccountScope  string
	ManualEntryID string
	Revision      int64
	LinkedAtMS    int64
	UpdatedAtMS   int64
}

type LegacyQuotaHistoryState string

const (
	LegacyQuotaHistoryUnavailable     LegacyQuotaHistoryState = "unavailable"
	LegacyQuotaHistoryAvailable       LegacyQuotaHistoryState = "available"
	LegacyQuotaHistoryLinked          LegacyQuotaHistoryState = "linked"
	LegacyQuotaHistoryLinkedElsewhere LegacyQuotaHistoryState = "linked_elsewhere"
)

type LegacyQuotaHistory struct {
	State               LegacyQuotaHistoryState
	ObservationCount    int64
	CycleCount          int64
	FirstObservedAtMS   *int64
	LastObservedAtMS    *int64
	AssociationRevision *int64
}

type Records struct {
	Binding  Binding
	Detected []DetectedAccount
	Manual   []ManualEntry
	Links    []Link
}

type AutomaticPlanFact struct {
	State AutomaticPlanState
	Plan  *Plan
}

type ResolvedPlan struct {
	Plan   *Plan
	Source ValueSource
}

type DateStatus struct {
	Kind   *DateKind
	Date   *string
	Source ValueSource
	State  DateState
	Delta  *int
}

type Account struct {
	AccountID                 string
	DetectedAccountID         *string
	ManualEntryID             *string
	Alias                     *string
	DisplayEmail              *string
	DetectedEmail             *string
	ManualEmail               *string
	Current                   bool
	Detected                  bool
	HasManual                 bool
	Linked                    bool
	DetectedEmailObservedAtMS *int64
	AutomaticPlan             *Plan
	AutomaticPlanState        AutomaticPlanState
	AutomaticPlanSource       *AutomaticSource
	AutomaticPlanObservedAtMS *int64
	ManualPlan                *Plan
	ResolvedPlan              *Plan
	ResolvedPlanSource        ValueSource
	MembershipDate            *string
	DateKind                  *DateKind
	DateSource                ValueSource
	DateState                 DateState
	DayDelta                  *int
	DetectedRevision          *int64
	ManualRevision            *int64
	LinkRevision              *int64
	LegacyQuotaHistory        *LegacyQuotaHistory
	accountScope              string
}

type LinkCandidate struct {
	DetectedAccountID string
	ManualEntryID     string
	Reason            string
}

type Snapshot struct {
	Version                 string
	EvaluatedAtMS           int64
	TimeZone                string
	AutomaticDateCapability AutomaticDateCapability
	Accounts                []Account
	LinkCandidates          []LinkCandidate
}

type ManualFields struct {
	Email          *string
	Alias          *string
	Plan           *Plan
	MembershipDate *string
	DateKind       *DateKind
}

type CreateRequest struct {
	ManualEntryID string
	Fields        ManualFields
	NowMS         int64
}

type UpdateRequest struct {
	AccountID              string
	Fields                 ManualFields
	NewManualEntryID       *string
	ExpectedManualRevision *int64
	ExpectedLinkRevision   *int64
	NowMS                  int64
}

type DeleteRequest struct {
	AccountID                string
	ExpectedDetectedRevision *int64
	ExpectedManualRevision   *int64
	ExpectedLinkRevision     *int64
}

type LinkRequest struct {
	DetectedAccountID      string
	ManualEntryID          string
	ExpectedManualRevision int64
	NowMS                  int64
}

type UnlinkRequest struct {
	DetectedAccountID      string
	ManualEntryID          string
	ExpectedManualRevision int64
	ExpectedLinkRevision   int64
	NowMS                  int64
}

type NormalizedManual struct {
	Email          *string
	EmailMatchKey  *string
	Alias          *string
	Plan           *Plan
	MembershipDate *string
	DateKind       *DateKind
}

type ManualRole int

const (
	ManualStandalone ManualRole = iota
	ManualLinkedSupplement
)
