package statistics_vo

// StatisticsCache 表示 Server 计算结果的新鲜度，与来源采集 coverage 独立。
type StatisticsCache struct {
	ComputedAtMS   int64  `json:"computed_at_ms"`
	RefreshAfterMS int64  `json:"refresh_after_ms"`
	AgeMS          int64  `json:"age_ms"`
	Stale          bool   `json:"stale"`
	State          string `json:"state"`
}
