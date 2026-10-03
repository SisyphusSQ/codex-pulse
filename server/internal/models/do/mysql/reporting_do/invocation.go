package reporting_do

type Invocation struct {
	SessionKey   string `gorm:"primaryKey"`
	InvocationID string `gorm:"primaryKey"`
	ObservedAtMS int64
	Kind         string
	ToolName     string
	Outcome      string
	DurationMS   *int64
}

func (Invocation) TableName() string { return "pulse_invocations" }
