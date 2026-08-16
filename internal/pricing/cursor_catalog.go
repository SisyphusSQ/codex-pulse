package pricing

import (
	"strings"
	"time"
)

const (
	CursorPricingVersion      = "cursor-docs-2026-08-16"
	CursorPricingSourceURL    = "https://cursor.com/docs/models-and-pricing"
	CursorPricingVerifiedAtMS = int64(1_786_838_400_000)
)

var (
	cursorGrok46DiscountStartMS = time.Date(2026, 8, 12, 0, 0, 0, 0, time.UTC).UnixMilli()
	cursorGrok46DiscountEndMS   = time.Date(2026, 8, 19, 0, 0, 0, 0, time.UTC).UnixMilli()
)

// CursorModelRate 保留 Cursor 官方价格表的四类 token 费率。
// nil cache write 表示该模型的官方表格未提供这一列，而不是零价格。
type CursorModelRate struct {
	ModelID              string
	Patterns             []string
	InputMicros          int64
	CacheWriteMicros     *int64
	CacheReadMicros      int64
	OutputMicros         int64
	Grok46LaunchDiscount bool
}

var builtinCursorModelRates = []CursorModelRate{
	cursorRate("Grok 4.6 (Fast)", []string{"grok-4.6-fast", "grok-4-6-fast"}, 4_000_000, nil, 1_000_000, 12_000_000, true),
	cursorRate("Grok 4.6", []string{"grok-4.6", "grok-4-6"}, 2_000_000, nil, 500_000, 6_000_000, true),
	cursorRate("Grok 4.5 (Fast)", []string{"grok-4.5-fast", "grok-4-5-fast"}, 4_000_000, nil, 1_000_000, 12_000_000, false),
	cursorRate("Grok 4.5", []string{"grok-4.5", "grok-4-5"}, 2_000_000, nil, 500_000, 6_000_000, false),
	cursorRate("Composer 2.5 (Fast)", []string{"composer-2.5-fast", "composer-2-5-fast"}, 3_000_000, nil, 500_000, 15_000_000, false),
	cursorRate("Composer 2.5", []string{"composer-2.5", "composer-2-5"}, 500_000, nil, 200_000, 2_500_000, false),
	cursorRate("Claude Fable 5", []string{"claude-fable-5", "claude-5-fable"}, 10_000_000, pointerInt64(12_500_000), 1_000_000, 50_000_000, false),
	cursorRate("Claude Opus 5", []string{"claude-opus-5", "claude-5-opus"}, 5_000_000, pointerInt64(6_250_000), 500_000, 25_000_000, false),
	cursorRate("Claude Sonnet 5", []string{"claude-sonnet-5", "claude-5-sonnet"}, 2_000_000, pointerInt64(2_500_000), 200_000, 10_000_000, false),
	cursorRate("Gemini 3.1 Pro", []string{"gemini-3.1-pro", "gemini-3-1-pro"}, 2_000_000, nil, 200_000, 12_000_000, false),
	cursorRate("Gemini 3.7 Flash", []string{"gemini-3.7-flash", "gemini-3-7-flash"}, 750_000, nil, 75_000, 3_500_000, false),
	cursorRate("GPT-5.6 Luna", []string{"gpt-5.6-luna", "gpt-5-6-luna"}, 200_000, pointerInt64(250_000), 20_000, 1_200_000, false),
	cursorRate("GPT-5.6 Sol", []string{"gpt-5.6-sol", "gpt-5-6-sol"}, 5_000_000, pointerInt64(6_250_000), 500_000, 30_000_000, false),
	cursorRate("GPT-5.6 Terra", []string{"gpt-5.6-terra", "gpt-5-6-terra"}, 2_000_000, pointerInt64(2_500_000), 200_000, 12_000_000, false),
	cursorRate("Auto Cost", []string{"auto-cost", "auto_cost"}, 1_250_000, pointerInt64(1_250_000), 250_000, 6_000_000, false),
}

// BuiltinCursorModelRates 返回独立副本，供参考价格展示和费用估算共享同一事实源。
func BuiltinCursorModelRates() []CursorModelRate {
	rates := make([]CursorModelRate, len(builtinCursorModelRates))
	for index, rate := range builtinCursorModelRates {
		rates[index] = rate
		rates[index].Patterns = append([]string(nil), rate.Patterns...)
		if rate.CacheWriteMicros != nil {
			value := *rate.CacheWriteMicros
			rates[index].CacheWriteMicros = &value
		}
	}
	return rates
}

// CursorRateForModel 返回事件发生时适用的费率；临时 Grok 4.6 折扣只影响估算，
// 不修改展示用的标准参考价格快照。
func CursorRateForModel(model string, occurredAtMS int64) (CursorModelRate, bool) {
	normalized := strings.ToLower(strings.TrimSpace(model))
	normalized = strings.NewReplacer("_", "-", " ", "-", "/", "-").Replace(normalized)
	for _, rate := range builtinCursorModelRates {
		for _, pattern := range rate.Patterns {
			if normalized != pattern && !strings.Contains(normalized, pattern) {
				continue
			}
			if rate.Grok46LaunchDiscount && occurredAtMS >= cursorGrok46DiscountStartMS && occurredAtMS < cursorGrok46DiscountEndMS {
				rate.InputMicros /= 2
				if rate.CacheWriteMicros != nil {
					value := *rate.CacheWriteMicros / 2
					rate.CacheWriteMicros = &value
				}
				rate.CacheReadMicros /= 2
				rate.OutputMicros /= 2
			}
			return rate, true
		}
	}
	return CursorModelRate{}, false
}

func cursorRate(
	modelID string,
	patterns []string,
	input int64,
	cacheWrite *int64,
	cacheRead int64,
	output int64,
	discount bool,
) CursorModelRate {
	return CursorModelRate{
		ModelID: modelID, Patterns: patterns, InputMicros: input,
		CacheWriteMicros: cacheWrite, CacheReadMicros: cacheRead,
		OutputMicros: output, Grok46LaunchDiscount: discount,
	}
}

func pointerInt64(value int64) *int64 { return &value }
