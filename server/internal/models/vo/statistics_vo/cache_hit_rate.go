package statistics_vo

type CacheHitRateView struct {
	BasisPoints       *string `json:"basis_points"`
	InputTokens       *string `json:"input_tokens"`
	CachedInputTokens *string `json:"cached_input_tokens"`
	Unit              string  `json:"unit"`
	Basis             string  `json:"basis"`
	Status            string  `json:"status"`
	Reason            string  `json:"reason"`
	SourceClientID    *string `json:"source_client_id"`
	Conflict          bool    `json:"conflict"`
}
