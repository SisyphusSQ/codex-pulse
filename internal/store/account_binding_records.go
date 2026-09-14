package store

import "errors"

var ErrCodexAccountBindingChanged = errors.New("Codex account binding changed")

type CodexAccountBindingState string

const (
	CodexAccountBindingUnknown             CodexAccountBindingState = "unknown"
	CodexAccountBindingPending             CodexAccountBindingState = "pending"
	CodexAccountBindingConfirmed           CodexAccountBindingState = "confirmed"
	CodexAccountBindingSignedOut           CodexAccountBindingState = "signed_out"
	CodexAccountBindingIdentityUnavailable CodexAccountBindingState = "identity_unavailable"
)

type CodexAccountBindingReason string

const (
	CodexAccountBindingReasonStartup              CodexAccountBindingReason = "startup"
	CodexAccountBindingReasonStable               CodexAccountBindingReason = "stable"
	CodexAccountBindingReasonAccountChanged       CodexAccountBindingReason = "account_changed"
	CodexAccountBindingReasonSignedOut            CodexAccountBindingReason = "signed_out"
	CodexAccountBindingReasonMissingAccountID     CodexAccountBindingReason = "missing_account_id"
	CodexAccountBindingReasonConfirmationFailed   CodexAccountBindingReason = "confirmation_failed"
	CodexAccountBindingReasonUnsupportedAppServer CodexAccountBindingReason = "unsupported_app_server"
)

type CodexAccountBinding struct {
	State             CodexAccountBindingState  `json:"state"`
	AccountScope      *string                   `json:"accountScope,omitempty"`
	BindingGeneration int64                     `json:"bindingGeneration"`
	ObservedAtMS      int64                     `json:"observedAtMs"`
	Reason            CodexAccountBindingReason `json:"reason,omitempty"`
}
