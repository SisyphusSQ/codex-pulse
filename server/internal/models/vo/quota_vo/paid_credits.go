package quota_vo

import "github.com/SisyphusSQ/codex-pulse/internal/creditbalance"

type PaidCredit struct {
	AccountKey string             `json:"account_key"`
	Credit     creditbalance.View `json:"credit"`
}
