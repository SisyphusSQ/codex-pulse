package store

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
