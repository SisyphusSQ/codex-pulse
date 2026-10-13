package reporting_do

type SessionPublication struct {
	SessionKey string `gorm:"primaryKey"`
	Generation string
}

func (SessionPublication) TableName() string { return "pulse_session_publications" }
