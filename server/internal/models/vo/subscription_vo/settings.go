package subscription_vo

type Update struct {
	ExpectedRevision *int64  `json:"expected_revision,string"`
	Alias            *string `json:"alias"`
	ManualPlan       *string `json:"manual_plan"`
	DateKind         string  `json:"date_kind"`
	RenewalDay       *int    `json:"renewal_day"`
	MembershipDate   *string `json:"membership_date"`
	TimeZone         string  `json:"time_zone"`
}

type View struct {
	AccountKey     string  `json:"account_key"`
	Revision       int64   `json:"revision,string"`
	Alias          *string `json:"alias"`
	AutomaticPlan  *string `json:"automatic_plan"`
	ManualPlan     *string `json:"manual_plan"`
	ResolvedPlan   *string `json:"resolved_plan"`
	DateKind       string  `json:"date_kind"`
	RenewalDay     *int    `json:"renewal_day"`
	MembershipDate *string `json:"membership_date"`
	NextDate       *string `json:"next_date"`
	DayDelta       *int    `json:"day_delta"`
	DateState      string  `json:"date_state"`
	TimeZone       string  `json:"time_zone"`
	UpdatedAtMS    *int64  `json:"updated_at_ms"`
}
