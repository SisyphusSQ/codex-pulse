package reporting_do

type DeviceStatus struct {
	SyncState       string `gorm:"->;-:migration"`
	SyncCheckedAtMS *int64 `gorm:"->;-:migration"`
	FullSyncState   string `gorm:"->;-:migration"`
	ClientID        string `gorm:"primaryKey"`
	Provider        string `gorm:"primaryKey"`
	Version         string
	CollectedAtMS   *int64
	CoverageStartMS *int64
	CoverageEndMS   *int64
	PendingBatches  int64
	Status          string
	ReceivedAtMS    int64
}

func (DeviceStatus) TableName() string { return "pulse_device_status" }
