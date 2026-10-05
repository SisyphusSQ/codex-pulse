package reportingv1

import "math"

// AccountTokenUsage 只记录当前已确认账号周期内新观察到的消耗，不承接 Home 历史。
// 空 Facts 表示客户端已开始记录该周期，区别于旧客户端尚未提供能力。
type AccountTokenUsage struct {
	Provider        string             `json:"provider"`
	AccountID       string             `json:"account_id"`
	LocalScope      string             `json:"local_scope"`
	WindowStartAtMS int64              `json:"window_start_at_ms"`
	ResetsAtMS      int64              `json:"resets_at_ms"`
	CollectedAtMS   int64              `json:"collected_at_ms"`
	Facts           []AccountTokenFact `json:"facts"`
}

// AccountTokenFact 复用全局 ContributionID，账号与设备不参与事实身份。
type AccountTokenFact struct {
	SessionID       string `json:"session_id"`
	ID              string `json:"id"`
	ObservedAtMS    int64  `json:"observed_at_ms"`
	InputTokens     int64  `json:"input_tokens,string"`
	CachedTokens    int64  `json:"cached_tokens,string"`
	OutputTokens    int64  `json:"output_tokens,string"`
	ReasoningTokens int64  `json:"reasoning_tokens,string"`
	TotalTokens     int64  `json:"total_tokens,string"`
	Ordinal         int64  `json:"ordinal"`
}

func (f AccountTokenFact) Contribution() Contribution {
	return Contribution{ID: f.ID, ObservedAtMS: &f.ObservedAtMS, InputTokens: &f.InputTokens, CachedTokens: &f.CachedTokens, OutputTokens: &f.OutputTokens, ReasoningTokens: &f.ReasoningTokens, TotalTokens: &f.TotalTokens, CostStatus: "unpriced"}
}

func (u AccountTokenUsage) Valid() bool {
	if u.Provider != "codex" || !identifier(u.AccountID, 255) || !identifier(u.LocalScope, 128) || u.LocalScope == "default" || !timestamp(u.WindowStartAtMS) || !timestamp(u.ResetsAtMS) || !timestamp(u.CollectedAtMS) || u.ResetsAtMS-u.WindowStartAtMS != 10080*60000 || u.CollectedAtMS < u.WindowStartAtMS || len(u.Facts) > 1000 {
		return false
	}
	seen := map[string]bool{}
	for _, f := range u.Facts {
		if !identifier(f.SessionID, 255) || !timestamp(f.ObservedAtMS) || f.ObservedAtMS < u.WindowStartAtMS || f.ObservedAtMS >= u.ResetsAtMS || f.ObservedAtMS > u.CollectedAtMS || f.Ordinal < 0 || f.Ordinal > MaxSnapshotFacts || f.InputTokens < 0 || f.CachedTokens < 0 || f.OutputTokens < 0 || f.ReasoningTokens < 0 || f.TotalTokens < 0 || f.InputTokens > math.MaxInt64-f.OutputTokens || f.InputTokens+f.OutputTokens > math.MaxInt64-f.ReasoningTokens || f.TotalTokens != f.InputTokens+f.OutputTokens+f.ReasoningTokens || f.ID != ContributionID(u.Provider, f.SessionID, f.Contribution(), f.Ordinal) || seen[f.ID] {
			return false
		}
		seen[f.ID] = true
	}
	return true
}
