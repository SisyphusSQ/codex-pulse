package reporting_do

// RetiredObservation 保留精简事实的不可变摘要，完整补传不得恢复已精简正文。
type RetiredObservation struct {
	ID          string `gorm:"primaryKey"`
	Digest      string
	RetiredAtMS int64
}

func (RetiredObservation) TableName() string { return "pulse_retired_observations" }
