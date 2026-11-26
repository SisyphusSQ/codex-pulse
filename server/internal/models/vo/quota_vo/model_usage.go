package quota_vo

type AccountModelUsage struct {
	EvaluatedAtMS   int64             `json:"evaluated_at_ms"`
	WindowStartAtMS *int64            `json:"window_start_at_ms"`
	ResetsAtMS      *int64            `json:"resets_at_ms"`
	RecordedTokens  *string           `json:"recorded_tokens"`
	Models          []AccountModelRow `json:"models"`
}

type AccountModelRow struct {
	Kind            string  `json:"kind"`
	Model           *string `json:"model"`
	Name            *string `json:"name"`
	TotalTokens     string  `json:"total_tokens"`
	InputTokens     *string `json:"input_tokens"`
	CachedTokens    *string `json:"cached_tokens"`
	OutputTokens    *string `json:"output_tokens"`
	ReasoningTokens *string `json:"reasoning_tokens"`
}
