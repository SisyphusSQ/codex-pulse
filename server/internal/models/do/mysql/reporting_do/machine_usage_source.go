package reporting_do

// MachineUsageSource 索引同一机器在同一会话内选中的来源及其可计时用量范围。
type MachineUsageSource struct {
	SessionKey        string `gorm:"primaryKey"`
	ClientID          string `gorm:"primaryKey"`
	SourceID          string
	SessionID         string
	Title             string
	ProjectID         string
	CollectedAtMS     int64
	Complete          bool
	Conflict          bool
	FirstObservedAtMS *int64
	LastObservedAtMS  *int64
	UntimedModels     *string
}

func (MachineUsageSource) TableName() string { return "pulse_machine_usage_sources" }
