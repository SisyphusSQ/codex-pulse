package quota_dto

const MaximumObservations = 100000
const MaximumAccounts = 10000

type Query struct {
	Provider   string
	AccountKey string
	WindowKey  string
	View       string
	Page       int
	Limit      int
	ClientID   string
}
