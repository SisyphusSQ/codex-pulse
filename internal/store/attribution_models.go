package store

type sessionAttributionModel struct {
	SessionID         string  `gorm:"column:session_id;primaryKey"`
	DisplayTitle      string  `gorm:"column:display_title"`
	TitleConfidence   string  `gorm:"column:title_confidence"`
	TitleSource       string  `gorm:"column:title_source"`
	TitleReason       string  `gorm:"column:title_reason"`
	ProjectID         *string `gorm:"column:project_id"`
	ProjectDisplay    *string `gorm:"column:project_display_name"`
	ProjectConfidence string  `gorm:"column:project_confidence"`
	ProjectSource     string  `gorm:"column:project_source"`
	ProjectReason     string  `gorm:"column:project_reason"`
	ModelKey          *string `gorm:"column:model_key"`
	ModelDisplay      *string `gorm:"column:model_display_name"`
	ModelConfidence   string  `gorm:"column:model_confidence"`
	ModelSource       string  `gorm:"column:model_source"`
	ModelReason       string  `gorm:"column:model_reason"`
	RuleVersion       int     `gorm:"column:rule_version"`
	UpdatedAtMS       int64   `gorm:"column:updated_at_ms"`
}

func (sessionAttributionModel) TableName() string { return "session_attributions" }

type turnAttributionModel struct {
	TurnID            string  `gorm:"column:turn_id;primaryKey"`
	ProjectID         *string `gorm:"column:project_id"`
	ProjectDisplay    *string `gorm:"column:project_display_name"`
	ProjectConfidence string  `gorm:"column:project_confidence"`
	ProjectSource     string  `gorm:"column:project_source"`
	ProjectReason     string  `gorm:"column:project_reason"`
	ModelKey          *string `gorm:"column:model_key"`
	ModelDisplay      *string `gorm:"column:model_display_name"`
	ModelConfidence   string  `gorm:"column:model_confidence"`
	ModelSource       string  `gorm:"column:model_source"`
	ModelReason       string  `gorm:"column:model_reason"`
	RuleVersion       int     `gorm:"column:rule_version"`
	UpdatedAtMS       int64   `gorm:"column:updated_at_ms"`
}

func (turnAttributionModel) TableName() string { return "turn_attributions" }
