package store

type ResetCreditStatus string

const (
	ResetCreditAvailable ResetCreditStatus = "available"
	ResetCreditRedeemed  ResetCreditStatus = "redeemed"
	ResetCreditExpired   ResetCreditStatus = "expired"
	ResetCreditUsed      ResetCreditStatus = "used"
)

type ResetCreditType string

const (
	ResetCreditTypeCodexRateLimits ResetCreditType = "codex_rate_limits"
	ResetCreditTypeUnknown         ResetCreditType = "unknown"
)

const (
	ResetCreditsSourceInstanceWhamDefault = "reset_credits:wham:default"
	ResetCreditsSourceTypeWham            = "wham_reset_credits"
	maxResetCreditsPerSnapshot            = 100
)

// ResetCredit is one content-free credit fact. CreditIDHash is the only
// retained form of the upstream identifier; user/profile/title text is never
// part of this contract.
type ResetCredit struct {
	CreditIDHash SHA256Digest
	Status       ResetCreditStatus
	Type         ResetCreditType
	GrantedAtMS  int64
	ExpiresAtMS  int64
	RedeemedAtMS *int64
}

// ResetCreditsSnapshot is one successful bounded response.
type ResetCreditsSnapshot struct {
	SnapshotID     string
	RequestID      string
	AccountScope   string
	AvailableCount int64
	ObservedAtMS   int64
	Credits        []ResetCredit
}

// ResetCreditsFetchRecord atomically records one attempt and, on success, its
// typed snapshot. Failed and cancelled attempts never carry a snapshot.
type ResetCreditsFetchRecord struct {
	SourceInstanceID string
	SourceType       string
	ScopeKey         string
	Attempt          SourceAttempt
	Snapshot         *ResetCreditsSnapshot
}

// ResetCreditsSummary is recomputed at EvaluationAtMS so expired credits do
// not remain available merely because no new response arrived. Pointer counts
// distinguish never-loaded from a real zero.
type ResetCreditsSummary struct {
	AccountScope          string
	SnapshotID            *string
	AvailableCount        *int64
	TotalCount            *int64
	RedeemedCount         *int64
	CumulativeRemainingMS *int64
	NextExpiresAtMS       *int64
	LastSuccessAtMS       *int64
	LastAttemptAtMS       *int64
	LastFailureCode       *SourceFailureCode
	FreshnessState        SourceFreshness
	EvaluationAtMS        int64
	Credits               []ResetCredit
}

func cloneResetCreditsSnapshot(value *ResetCreditsSnapshot) *ResetCreditsSnapshot {
	if value == nil {
		return nil
	}
	cloned := *value
	cloned.Credits = make([]ResetCredit, len(value.Credits))
	for index, credit := range value.Credits {
		cloned.Credits[index] = credit
		if credit.RedeemedAtMS != nil {
			redeemedAt := *credit.RedeemedAtMS
			cloned.Credits[index].RedeemedAtMS = &redeemedAt
		}
	}
	return &cloned
}
