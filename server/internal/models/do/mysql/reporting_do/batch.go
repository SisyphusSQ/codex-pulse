package reporting_do

// Batch 是已提交的幂等收据，不保存重复传输的正文。
type Batch struct {
	ClientID     string `gorm:"primaryKey"`
	BatchID      string `gorm:"primaryKey"`
	Digest       string
	ReceivedAtMS int64
}

func (Batch) TableName() string { return "pulse_batches" }
