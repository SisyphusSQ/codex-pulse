package reporting_do

// SessionCapsule 是可从完整快照重建的生命周期指标投影。
type SessionCapsule struct {
	Kind        string `gorm:"primaryKey"`
	ID          string `gorm:"primaryKey"`
	SessionKey  string
	ClientID    string
	Complete    bool
	Deleted     bool
	FactsDigest string
	Throughput  *string
	CacheUsage  *string
}

func (SessionCapsule) TableName() string { return "pulse_session_capsules" }
