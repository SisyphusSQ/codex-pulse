package reporting_do

type DeviceSync struct {
	ClientID        string `gorm:"primaryKey"`
	Provider        string `gorm:"primaryKey"`
	SyncState       string
	SyncCheckedAtMS int64
	FullSyncState   string
}

func (DeviceSync) TableName() string { return "pulse_device_sync" }
