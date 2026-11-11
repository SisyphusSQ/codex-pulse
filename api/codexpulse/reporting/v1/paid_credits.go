package reportingv1

import "github.com/SisyphusSQ/codex-pulse/internal/creditbalance"

// PaidCredit is an official balance observation, never a derived debit.
type PaidCredit struct {
	ID         string                 `json:"id"`
	Provider   string                 `json:"provider"`
	AccountID  *string                `json:"account_id"`
	LocalScope string                 `json:"local_scope"`
	Snapshot   creditbalance.Snapshot `json:"snapshot"`
}

func (c PaidCredit) Valid() bool {
	return identifier(c.ID, 128) && c.Provider == "codex" && optionalID(c.AccountID, 255) && identifier(c.LocalScope, 128) && c.LocalScope != "default" && c.Snapshot.Valid()
}
