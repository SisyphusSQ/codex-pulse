package quota_dto

const MaximumObservations = 100000
const MaximumAccounts = 10000

type Query struct {
	// RawHistory 仅供内部保留维护扫描；HTTP 解析不设置此字段。
	RawHistory bool
	Direction  string
	Provider   string
	AccountKey string
	WindowKey  string
	View       string
	Page       int
	Limit      int
	ClientID   string
}
