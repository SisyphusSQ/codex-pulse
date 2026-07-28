package store

type lightTokenScanMigrationModel struct {
	CurrentModelKey    *string `gorm:"column:current_model_key;type:TEXT CHECK (current_model_key IS NULL OR (length(current_model_key) BETWEEN 1 AND 128))"`
	CurrentModelSource string  `gorm:"column:current_model_source;type:TEXT NOT NULL DEFAULT 'missing' CHECK (current_model_source IN ('model_canonical','model_alias','missing','invalid_model'))"`
}

func (lightTokenScanMigrationModel) TableName() string { return "light_token_scans" }

type lightTokenTimedMigrationModel struct {
	ModelKey    *string `gorm:"column:model_key;type:TEXT CHECK (model_key IS NULL OR (length(model_key) BETWEEN 1 AND 128))"`
	ModelSource string  `gorm:"column:model_source;type:TEXT NOT NULL DEFAULT 'missing' CHECK (model_source IN ('model_canonical','model_alias','missing','invalid_model'))"`
}

func (lightTokenTimedMigrationModel) TableName() string { return "light_token_timed" }
