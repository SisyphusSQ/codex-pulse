package pricing

import (
	"strings"
	"time"
)

const (
	CursorPricingVersion       = "cursor-docs-2026-08-16"
	CursorPricingSourceURL     = "https://cursor.com/docs/models-and-pricing"
	CursorPricingVerifiedAtMS  = int64(1_786_838_400_000)
	CursorUsagePoolModels      = "cursor.models"
	CursorUsagePoolOtherModels = "cursor.other_models"
	CursorUsagePoolUnknown     = "cursor.unknown"
)

var (
	cursorGrok46DiscountStartMS = time.Date(2026, 8, 12, 0, 0, 0, 0, time.UTC).UnixMilli()
	cursorGrok46DiscountEndMS   = time.Date(2026, 8, 19, 0, 0, 0, 0, time.UTC).UnixMilli()
	cursorOtherModelPrefixes    = []string{"claude-", "gemini-", "gpt-"}
)

// CursorModelRate 保留 Cursor 官方价格表的四类 token 费率。
// nil cache write 表示该模型的官方表格未提供这一列，而不是零价格。
type CursorModelRate struct {
	ModelID              string
	Patterns             []string
	UsagePoolID          string
	InputMicros          int64
	CacheWriteMicros     *int64
	CacheReadMicros      int64
	OutputMicros         int64
	Grok46LaunchDiscount bool
}

var builtinCursorModelRates = []CursorModelRate{
	cursorRate("Grok 4.6 (Fast)", []string{"grok-4.6-fast", "grok-4-6-fast"}, CursorUsagePoolModels, 4_000_000, nil, 1_000_000, 12_000_000, true),
	cursorRate("Grok 4.6", []string{"grok-4.6", "grok-4-6"}, CursorUsagePoolModels, 2_000_000, nil, 500_000, 6_000_000, true),
	cursorRate("Grok 4.5 (Fast)", []string{"grok-4.5-fast", "grok-4-5-fast"}, CursorUsagePoolModels, 4_000_000, nil, 1_000_000, 12_000_000, false),
	cursorRate("Grok 4.5", []string{"grok-4.5", "grok-4-5"}, CursorUsagePoolModels, 2_000_000, nil, 500_000, 6_000_000, false),
	cursorRate("Composer 2.5 (Fast)", []string{"composer-2.5-fast", "composer-2-5-fast"}, CursorUsagePoolModels, 3_000_000, nil, 500_000, 15_000_000, false),
	cursorRate("Composer 2.5", []string{"composer-2.5", "composer-2-5"}, CursorUsagePoolModels, 500_000, nil, 200_000, 2_500_000, false),
	cursorRate("Claude Fable 5", []string{"claude-fable-5", "claude-5-fable"}, CursorUsagePoolOtherModels, 10_000_000, pointerInt64(12_500_000), 1_000_000, 50_000_000, false),
	cursorRate("Claude Opus 5", []string{"claude-opus-5", "claude-5-opus"}, CursorUsagePoolOtherModels, 5_000_000, pointerInt64(6_250_000), 500_000, 25_000_000, false),
	cursorRate("Claude Sonnet 5", []string{"claude-sonnet-5", "claude-5-sonnet"}, CursorUsagePoolOtherModels, 2_000_000, pointerInt64(2_500_000), 200_000, 10_000_000, false),
	cursorRate("Gemini 3.1 Pro", []string{"gemini-3.1-pro", "gemini-3-1-pro"}, CursorUsagePoolOtherModels, 2_000_000, nil, 200_000, 12_000_000, false),
	cursorRate("Gemini 3.7 Flash", []string{"gemini-3.7-flash", "gemini-3-7-flash"}, CursorUsagePoolOtherModels, 750_000, nil, 75_000, 3_500_000, false),
	cursorRate("GPT-5.6 Luna", []string{"gpt-5.6-luna", "gpt-5-6-luna"}, CursorUsagePoolOtherModels, 200_000, pointerInt64(250_000), 20_000, 1_200_000, false),
	cursorRate("GPT-5.6 Sol", []string{"gpt-5.6-sol", "gpt-5-6-sol"}, CursorUsagePoolOtherModels, 5_000_000, pointerInt64(6_250_000), 500_000, 30_000_000, false),
	cursorRate("GPT-5.6 Terra", []string{"gpt-5.6-terra", "gpt-5-6-terra"}, CursorUsagePoolOtherModels, 2_000_000, pointerInt64(2_500_000), 200_000, 12_000_000, false),
	cursorRate("Auto Cost", []string{"auto-cost", "auto_cost"}, CursorUsagePoolUnknown, 1_250_000, pointerInt64(1_250_000), 250_000, 6_000_000, false),
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
	normalized := normalizeCursorModel(model)
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

// CursorUsagePoolForModel 返回 Dashboard 模型事件所属的官方月度用量池。
// 用量池身份独立于当前参考价格表，因此同一家第三方模型的历史版本仍属于 Other Models。
// 无法确认实际路由模型的 Auto 与未知模型保留为未归类，避免误计入 Other Models。
func CursorUsagePoolForModel(model string, occurredAtMS int64) string {
	rate, ok := CursorRateForModel(model, occurredAtMS)
	if ok && rate.UsagePoolID != "" {
		return rate.UsagePoolID
	}
	normalized := normalizeCursorModel(model)
	for _, prefix := range cursorOtherModelPrefixes {
		if strings.HasPrefix(normalized, prefix) {
			return CursorUsagePoolOtherModels
		}
	}
	return CursorUsagePoolUnknown
}

func normalizeCursorModel(model string) string {
	normalized := strings.ToLower(strings.TrimSpace(model))
	return strings.NewReplacer("_", "-", " ", "-", "/", "-").Replace(normalized)
}

func cursorRate(
	modelID string,
	patterns []string,
	usagePoolID string,
	input int64,
	cacheWrite *int64,
	cacheRead int64,
	output int64,
	discount bool,
) CursorModelRate {
	return CursorModelRate{
		ModelID: modelID, Patterns: patterns, UsagePoolID: usagePoolID, InputMicros: input,
		CacheWriteMicros: cacheWrite, CacheReadMicros: cacheRead,
		OutputMicros: output, Grok46LaunchDiscount: discount,
	}
}

func pointerInt64(value int64) *int64 { return &value }
