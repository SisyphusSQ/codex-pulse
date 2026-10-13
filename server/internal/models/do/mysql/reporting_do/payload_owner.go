package reporting_do

// PayloadOwner 与业务指针同事务更新，后台只能回收不再被引用的分段快照。
type PayloadOwner struct {
	ID         string `gorm:"primaryKey"`
	PayloadKey string
}

func (PayloadOwner) TableName() string { return "pulse_payload_owners" }
