package reporting_do

// PayloadPart 将白名单快照按字节保存；BLOB 允许 UTF-8 字符跨片。
type PayloadPart struct {
	PayloadKey  string `gorm:"primaryKey"`
	PartIndex   int    `gorm:"primaryKey"`
	Body        []byte
	CreatedAtMS int64
}

func (PayloadPart) TableName() string { return "pulse_payload_parts" }
