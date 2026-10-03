package reporting_do

type Project struct {
	ID          string `gorm:"primaryKey"`
	ClientID    string
	Provider    string
	LocalID     string
	Name        string
	GroupID     string
	UpdatedAtMS int64
}

func (Project) TableName() string { return "pulse_projects" }
