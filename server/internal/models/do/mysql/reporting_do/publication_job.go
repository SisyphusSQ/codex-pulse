package reporting_do

// PublicationJob 保留最终批次及处理租约；只有发布事务会创建最终 receipt。
type PublicationJob struct {
	ID              string `gorm:"primaryKey"`
	ClientID        string
	BatchID         string
	Digest          string
	SourceKey       string
	Revision        int64
	Body            string
	State           string
	FailureCode     string
	CreatedAtMS     int64
	NextAttemptAtMS int64
	LeaseUntilMS    int64
	LeaseToken      string
}

func (PublicationJob) TableName() string { return "pulse_publication_jobs" }
