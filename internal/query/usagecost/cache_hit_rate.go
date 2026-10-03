package usagecost

import (
	"github.com/SisyphusSQ/codex-pulse/internal/cachehitrate"
	basequery "github.com/SisyphusSQ/codex-pulse/internal/query"
)

// mapCacheHitRate 使用 Codex 会话总量；cached 已包含在 input 中。
func mapCacheHitRate(totals UsageTotals) (basequery.NumericValue, error) {
	input, cached := totals.InputTokens, totals.CachedInputTokens
	if input.Validate() != nil || cached.Validate() != nil ||
		input.Unit != basequery.NumericTokens || cached.Unit != basequery.NumericTokens {
		return basequery.UnknownNumeric(basequery.NumericBasisPoints, basequery.UnknownUnavailable)
	}
	if input.Value == nil {
		return basequery.UnknownNumeric(basequery.NumericBasisPoints, *input.UnknownReason)
	}
	if cached.Value != nil && *cached.Value > *input.Value {
		return basequery.UnknownNumeric(basequery.NumericBasisPoints, basequery.UnknownUnavailable)
	}
	if *input.Value == 0 {
		return basequery.UnknownNumeric(basequery.NumericBasisPoints, basequery.UnknownNotApplicable)
	}
	if cached.Value == nil {
		return basequery.UnknownNumeric(basequery.NumericBasisPoints, *cached.UnknownReason)
	}
	return basequery.KnownNumeric(*cachehitrate.BasisPoints(*input.Value, *cached.Value), basequery.NumericBasisPoints)
}
