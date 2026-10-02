package catalog_vo

type Model struct {
	Key             string  `json:"key"`
	Provider        string  `json:"provider"`
	Model           string  `json:"model"`
	Mode            string  `json:"mode"`
	Currency        string  `json:"currency"`
	Unit            string  `json:"unit"`
	InputPrice      *string `json:"input_price"`
	CachedPrice     *string `json:"cached_price"`
	CacheWritePrice *string `json:"cache_write_price"`
	OutputPrice     *string `json:"output_price"`
	Version         string  `json:"version"`
	SourceURL       string  `json:"source_url"`
	VerifiedAtMS    int64   `json:"verified_at_ms"`
	EffectiveFromMS *int64  `json:"effective_from_ms"`
	Evidence        string  `json:"evidence"`
	Notes           string  `json:"notes"`
}
type Plan struct {
	Key          string  `json:"key"`
	Provider     string  `json:"provider"`
	Name         string  `json:"name"`
	Price        *string `json:"price"`
	Currency     string  `json:"currency"`
	Cycle        string  `json:"cycle"`
	Allowance    string  `json:"allowance"`
	ResetRule    string  `json:"reset_rule"`
	Region       string  `json:"region"`
	SourceURL    string  `json:"source_url"`
	VerifiedAtMS int64   `json:"verified_at_ms"`
}
type Response struct {
	Models  []Model `json:"models"`
	Plans   []Plan  `json:"plans"`
	Version string  `json:"version"`
}
