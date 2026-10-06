package reportingv1

import (
	"math/big"
	"slices"
)

// CacheUsageCapsule 只含整段已索引会话的输入总量；不含事件或索引身份。
type CacheUsageCapsule struct {
	Version           int    `json:"version"`
	Basis             string `json:"basis"`
	InputTokens       *int64 `json:"input_tokens,string"`
	CachedInputTokens *int64 `json:"cached_input_tokens,string"`
	Reason            string `json:"reason,omitempty"`
}

func validCacheUsage(snapshot SessionSnapshot) bool {
	c := snapshot.CacheUsage
	if c == nil {
		return true
	}
	if !((snapshot.Provider == "codex" && snapshot.SourceKind == "light_index") || (snapshot.Provider == "dsh" && snapshot.SourceKind == "dsh_local")) || snapshot.Deleted || c.Basis != "lifetime_cached_input" || c.Version != 1 {
		return false
	}
	if c.Reason != "" {
		return c.InputTokens == nil && c.CachedInputTokens == nil && slices.Contains([]string{"rollup_missing", "rollup_ambiguous", "unavailable", "history_filtered"}, c.Reason) && (c.Reason != "history_filtered" || snapshot.HistoryStartAtMS > 0)
	}
	if snapshot.HistoryStartAtMS > 0 || c.InputTokens == nil || c.CachedInputTokens == nil || *c.InputTokens < 0 || *c.CachedInputTokens < 0 {
		return false
	}
	// 累计缓存可能大于输入，保留合法计数由查询标 unavailable；不破坏其他数据。
	input, cached := new(big.Int), new(big.Int)
	for _, fact := range snapshot.Contributions {
		if fact.InputTokens == nil || fact.CachedTokens == nil {
			return false
		}
		input.Add(input, big.NewInt(*fact.InputTokens))
		cached.Add(cached, big.NewInt(*fact.CachedTokens))
	}
	return input.Cmp(big.NewInt(*c.InputTokens)) == 0 && cached.Cmp(big.NewInt(*c.CachedInputTokens)) == 0
}
