package quota_dto

type AccountModelFact struct {
	Matched         bool
	Model           *string
	TotalTokens     int64
	InputTokens     *int64
	CachedTokens    *int64
	OutputTokens    *int64
	ReasoningTokens *int64
}
