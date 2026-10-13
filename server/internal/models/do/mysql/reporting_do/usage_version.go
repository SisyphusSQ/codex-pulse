package reporting_do

// UsageVersion 在发布指针切换前不可见；ID 由完整投影内容决定。
type UsageVersion struct {
	Generation string `gorm:"primaryKey"`
	Usage      `gorm:"embedded"`
}

func (UsageVersion) TableName() string { return "pulse_usage_versions" }
