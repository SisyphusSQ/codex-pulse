package reporting_do

type CanonicalSnapshot struct {
	SessionKey string `gorm:"primaryKey"`
	Payload    string
}

func (CanonicalSnapshot) TableName() string { return "pulse_session_canonical" }
