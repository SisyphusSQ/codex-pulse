package store

type LegacyQuotaHistoryState string

const (
	LegacyQuotaHistoryUnavailable     LegacyQuotaHistoryState = "unavailable"
	LegacyQuotaHistoryAvailable       LegacyQuotaHistoryState = "available"
	LegacyQuotaHistoryLinked          LegacyQuotaHistoryState = "linked"
	LegacyQuotaHistoryLinkedElsewhere LegacyQuotaHistoryState = "linked_elsewhere"
)

type LegacyQuotaHistoryMutationResult string

const (
	LegacyQuotaHistoryMutationApplied  LegacyQuotaHistoryMutationResult = "applied"
	LegacyQuotaHistoryMutationNoop     LegacyQuotaHistoryMutationResult = "noop"
	LegacyQuotaHistoryMutationConflict LegacyQuotaHistoryMutationResult = "conflict"
)

const (
	LegacyQuotaHistoryReasonRevisionChanged       = "revision_changed"
	LegacyQuotaHistoryReasonAlreadyLinked         = "already_linked"
	LegacyQuotaHistoryReasonCurrentAccountChanged = "current_account_changed"
	LegacyQuotaHistoryReasonLinkTargetChanged     = "link_target_changed"
)

type LegacyQuotaHistoryStatus struct {
	State                   LegacyQuotaHistoryState
	ObservationCount        int64
	CycleCount              int64
	FirstObservedAtMS       *int64
	LastObservedAtMS        *int64
	AssociationRevision     *int64
	AssociationAccountScope *string
}

type LegacyQuotaHistoryMutation struct {
	Result LegacyQuotaHistoryMutationResult
	Reason *string
}

type LegacyQuotaHistoryLinkRequest struct {
	DetectedAccountID        string
	ExpectedDetectedRevision int64
	NowMS                    int64
}

type LegacyQuotaHistoryUnlinkRequest struct {
	DetectedAccountID           string
	ExpectedDetectedRevision    int64
	ExpectedAssociationRevision int64
}
