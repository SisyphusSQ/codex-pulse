package subscription_do

// Settings 只保存中心手动补充，不修改设备提供的账号身份和套餐事实。
type Settings struct {
	AccountKey     string `gorm:"primaryKey"`
	Revision       int64
	Alias          *string
	ManualPlan     *string
	DateKind       string
	RenewalDay     *int
	MembershipDate *string
	TimeZone       string
	UpdatedAtMS    int64
}

func (Settings) TableName() string { return "pulse_account_settings" }
