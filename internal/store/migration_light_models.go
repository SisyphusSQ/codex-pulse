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

type lightTokenCounterCheckpointMigrationModel struct {
	LastRawInputTokens        *int64 `gorm:"column:last_raw_input_tokens;type:INTEGER CHECK (last_raw_input_tokens IS NULL OR last_raw_input_tokens >= 0)"`
	LastRawInputPresent       bool   `gorm:"column:last_raw_input_present;type:INTEGER NOT NULL DEFAULT 0 CHECK (last_raw_input_present IN (0, 1))"`
	LastRawCachedInputTokens  *int64 `gorm:"column:last_raw_cached_input_tokens;type:INTEGER CHECK (last_raw_cached_input_tokens IS NULL OR last_raw_cached_input_tokens >= 0)"`
	LastRawCachedInputPresent bool   `gorm:"column:last_raw_cached_input_present;type:INTEGER NOT NULL DEFAULT 0 CHECK (last_raw_cached_input_present IN (0, 1))"`
	LastRawOutputTokens       *int64 `gorm:"column:last_raw_output_tokens;type:INTEGER CHECK (last_raw_output_tokens IS NULL OR last_raw_output_tokens >= 0)"`
	LastRawOutputPresent      bool   `gorm:"column:last_raw_output_present;type:INTEGER NOT NULL DEFAULT 0 CHECK (last_raw_output_present IN (0, 1))"`
	LastRawReasoningTokens    *int64 `gorm:"column:last_raw_reasoning_tokens;type:INTEGER CHECK (last_raw_reasoning_tokens IS NULL OR last_raw_reasoning_tokens >= 0)"`
	LastRawReasoningPresent   bool   `gorm:"column:last_raw_reasoning_present;type:INTEGER NOT NULL DEFAULT 0 CHECK (last_raw_reasoning_present IN (0, 1))"`
	CounterEpoch              int64  `gorm:"column:counter_epoch;type:INTEGER NOT NULL DEFAULT 0 CHECK (counter_epoch >= 0)"`
}

func (lightTokenCounterCheckpointMigrationModel) TableName() string { return "light_token_scans" }
