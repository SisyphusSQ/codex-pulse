package store

import "gorm.io/gorm"

// A published empty metadata snapshot is authoritative for its Home. Session
// count cannot select the read mode: falling back would expose retained deep
// facts from the previous Home after switching to an empty one.
func hasLightAnalyticsSnapshot(database *gorm.DB) (bool, error) {
	var count int64
	err := database.Table("light_index_state").Where("state_id = 1 AND metadata_generation > 0").Count(&count).Error
	return count > 0, err
}

type lightTokenTimedQueryRow struct {
	ObservedAtMS      int64   `gorm:"column:observed_at_ms"`
	InputTokens       int64   `gorm:"column:input_tokens"`
	CachedInputTokens int64   `gorm:"column:cached_input_tokens"`
	OutputTokens      int64   `gorm:"column:output_tokens"`
	ReasoningTokens   int64   `gorm:"column:reasoning_tokens"`
	ModelKey          *string `gorm:"column:model_key"`
	ModelSource       string  `gorm:"column:model_source"`
}

type lightIndexStateQueryRow struct {
	MetadataGeneration  int64 `gorm:"column:metadata_generation"`
	TokenScanGeneration int64 `gorm:"column:token_scan_generation"`
}
