package store

type quotaHistoryAssociationModel struct {
	LegacyAccountScope string `gorm:"column:legacy_account_scope;primaryKey"`
	AccountScope       string `gorm:"column:account_scope"`
	Revision           int64  `gorm:"column:revision"`
	LinkedAtMS         int64  `gorm:"column:linked_at_ms"`
	UpdatedAtMS        int64  `gorm:"column:updated_at_ms"`
}

type quotaHistoryAssociationGenerationModel struct {
	LegacyAccountScope string `gorm:"column:legacy_account_scope;primaryKey"`
	Revision           int64  `gorm:"column:revision"`
	UpdatedAtMS        int64  `gorm:"column:updated_at_ms"`
}

func (quotaHistoryAssociationModel) TableName() string {
	return "codex_quota_history_associations"
}

func (quotaHistoryAssociationGenerationModel) TableName() string {
	return "codex_quota_history_association_generations"
}
