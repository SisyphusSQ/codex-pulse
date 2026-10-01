package store

// CodexQuotaLimitActive 复用本机当前窗口的明确退役标识规则。
func CodexQuotaLimitActive(limitID string) bool { return !retiredCodexQuotaLimit(limitID) }

// Retired Codex model-specific limits remain in observation history and
// rebuildable projections, but are not active quota windows. Match the stable
// source identity, not limit_name: the default codex bucket can carry the same
// display name without becoming a Spark-only limit.
func retiredCodexQuotaLimit(limitID string) bool {
	switch limitID {
	case "codex_spark", "codex_bengalfox":
		return true
	default:
		return false
	}
}
