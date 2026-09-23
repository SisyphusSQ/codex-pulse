package pricing

import (
	"strings"
	"time"
)

const (
	CursorPricingVersion       = "cursor-docs-2026-09-23"
	CursorPricingSourceURL     = "https://cursor.com/docs/models-and-pricing"
	CursorPricingVerifiedAtMS  = int64(1_790_123_508_000)
	CursorUsagePoolModels      = "cursor.models"
	CursorUsagePoolOtherModels = "cursor.other_models"
	CursorUsagePoolUnknown     = "cursor.unknown"
)

var (
	cursorGrok46DiscountStartMS = time.Date(2026, 8, 12, 0, 0, 0, 0, time.UTC).UnixMilli()
	cursorGrok46DiscountEndMS   = time.Date(2026, 8, 19, 0, 0, 0, 0, time.UTC).UnixMilli()
	cursorGrok47ReleaseAtMS     = time.Date(2026, 9, 21, 0, 0, 0, 0, time.UTC).UnixMilli()
	cursorOtherModelPrefixes    = []string{"claude-", "gemini-", "gpt-", "glm-", "kimi-", "muse-"}
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

// 旧版快照仅用于历史事件计价；现行目录从副本构建，不回写旧费率。
var cursor20260816Rates = []CursorModelRate{
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

var builtinCursorModelRates = cursor20260923Rates()

func cursor20260923Rates() []CursorModelRate {
	rates := append([]CursorModelRate(nil), cursor20260816Rates...)
	for index := range rates {
		switch rates[index].ModelID {
		case "Grok 4.5 (Fast)":
			rates[index].OutputMicros = 18_000_000
		case "GPT-5.6 Sol":
			rates[index].InputMicros = 4_000_000
			rates[index].CacheWriteMicros = pointerInt64(5_000_000)
			rates[index].CacheReadMicros = 400_000
			rates[index].OutputMicros = 20_000_000
		}
	}
	return append(rates,
		cursorRate("Grok 4.7", []string{"grok-4.7", "grok-4-7"}, CursorUsagePoolModels, 2_000_000, nil, 500_000, 6_000_000, false),
		cursorRate("Grok 4.7 (Fast)", []string{"grok-4.7-fast", "grok-4-7-fast"}, CursorUsagePoolModels, 4_000_000, nil, 1_000_000, 12_000_000, false),
		cursorRate("Grok 4.7 500k", []string{"grok-4.7-500k", "grok-4-7-500k"}, CursorUsagePoolModels, 4_000_000, nil, 1_000_000, 12_000_000, false),
		cursorRate("Grok 4.7 500k (Fast)", []string{"grok-4.7-500k-fast", "grok-4-7-500k-fast"}, CursorUsagePoolModels, 6_000_000, nil, 1_500_000, 18_000_000, false),
		cursorRate("Claude Fable 5.1", []string{"claude-fable-5.1", "claude-fable-5-1"}, CursorUsagePoolOtherModels, 10_000_000, pointerInt64(12_500_000), 250_000, 50_000_000, false),
		cursorRate("Claude Opus 5.5", []string{"claude-opus-5.5", "claude-opus-5-5"}, CursorUsagePoolOtherModels, 4_000_000, pointerInt64(5_000_000), 200_000, 20_000_000, false),
		cursorRate("Claude Opus 5.5 (Fast)", []string{"claude-opus-5.5-fast", "claude-opus-5-5-fast"}, CursorUsagePoolOtherModels, 8_000_000, pointerInt64(10_000_000), 400_000, 40_000_000, false),
		cursorRate("Gemini 3.8 Flash", []string{"gemini-3.8-flash", "gemini-3-8-flash"}, CursorUsagePoolOtherModels, 750_000, nil, 75_000, 3_500_000, false),
		cursorRate("Muse Spark 1.3", []string{"muse-spark-1.3", "muse-spark-1-3"}, CursorUsagePoolOtherModels, 1_250_000, nil, 150_000, 4_250_000, false),
		// 官网“显示更多模型”中的仍公开费率。未证实旧事件的历史价格，
		// 这些新增规则从本次核验时刻才参与估算。
		cursorRate("Claude 4 Sonnet", []string{"claude-4-sonnet"}, CursorUsagePoolOtherModels, 3_000_000, pointerInt64(3_750_000), 300_000, 15_000_000, false),
		cursorRate("Claude 4 Sonnet 1M", []string{"claude-4-sonnet-1m"}, CursorUsagePoolOtherModels, 6_000_000, pointerInt64(7_500_000), 600_000, 22_500_000, false),
		cursorRate("Claude 4.5 Haiku", []string{"claude-4.5-haiku", "claude-4-5-haiku"}, CursorUsagePoolOtherModels, 1_000_000, pointerInt64(1_250_000), 100_000, 5_000_000, false),
		cursorRate("Claude 4.5 Opus", []string{"claude-4.5-opus", "claude-4-5-opus"}, CursorUsagePoolOtherModels, 5_000_000, pointerInt64(6_250_000), 500_000, 25_000_000, false),
		cursorRate("Claude 4.5 Sonnet", []string{"claude-4.5-sonnet", "claude-4-5-sonnet"}, CursorUsagePoolOtherModels, 3_000_000, pointerInt64(3_750_000), 300_000, 15_000_000, false),
		cursorRate("Claude 4.6 Opus", []string{"claude-4.6-opus", "claude-4-6-opus"}, CursorUsagePoolOtherModels, 5_000_000, pointerInt64(6_250_000), 500_000, 25_000_000, false),
		cursorRate("Claude 4.6 Sonnet", []string{"claude-4.6-sonnet", "claude-4-6-sonnet"}, CursorUsagePoolOtherModels, 3_000_000, pointerInt64(3_750_000), 300_000, 15_000_000, false),
		cursorRate("Claude 4.7 Opus", []string{"claude-4.7-opus", "claude-4-7-opus"}, CursorUsagePoolOtherModels, 5_000_000, pointerInt64(6_250_000), 500_000, 25_000_000, false),
		cursorRate("Claude Opus 4.7 (fast mode)", []string{"claude-opus-4.7-fast", "claude-opus-4-7-fast"}, CursorUsagePoolOtherModels, 30_000_000, pointerInt64(37_500_000), 3_000_000, 150_000_000, false),
		cursorRate("Claude Opus 4.8", []string{"claude-opus-4.8", "claude-opus-4-8"}, CursorUsagePoolOtherModels, 5_000_000, pointerInt64(6_250_000), 500_000, 25_000_000, false),
		cursorRate("Gemini 2.5 Flash", []string{"gemini-2.5-flash", "gemini-2-5-flash"}, CursorUsagePoolOtherModels, 300_000, nil, 30_000, 2_500_000, false),
		cursorRate("Gemini 3 Flash", []string{"gemini-3-flash"}, CursorUsagePoolOtherModels, 500_000, nil, 50_000, 3_000_000, false),
		cursorRate("Gemini 3 Pro", []string{"gemini-3-pro"}, CursorUsagePoolOtherModels, 2_000_000, nil, 200_000, 12_000_000, false),
		cursorRate("Gemini 3 Pro Image Preview", []string{"gemini-3-pro-image-preview"}, CursorUsagePoolOtherModels, 2_000_000, nil, 200_000, 12_000_000, false),
		cursorRate("Gemini 3.5 Flash", []string{"gemini-3.5-flash", "gemini-3-5-flash"}, CursorUsagePoolOtherModels, 1_500_000, nil, 150_000, 9_000_000, false),
		cursorRate("Gemini 3.6 Flash", []string{"gemini-3.6-flash", "gemini-3-6-flash"}, CursorUsagePoolOtherModels, 1_500_000, nil, 150_000, 7_500_000, false),
		cursorRate("GLM 5.2", []string{"glm-5.2", "glm-5-2"}, CursorUsagePoolOtherModels, 1_400_000, nil, 260_000, 4_400_000, false),
		cursorRate("GPT-5", []string{"gpt-5"}, CursorUsagePoolOtherModels, 1_250_000, nil, 125_000, 10_000_000, false),
		cursorRate("GPT-5 Fast", []string{"gpt-5-fast"}, CursorUsagePoolOtherModels, 2_500_000, nil, 250_000, 20_000_000, false),
		cursorRate("GPT-5 Mini", []string{"gpt-5-mini"}, CursorUsagePoolOtherModels, 250_000, nil, 25_000, 2_000_000, false),
		cursorRate("GPT-5-Codex", []string{"gpt-5-codex"}, CursorUsagePoolOtherModels, 1_250_000, nil, 125_000, 10_000_000, false),
		cursorRate("GPT-5.1 Codex", []string{"gpt-5.1-codex", "gpt-5-1-codex"}, CursorUsagePoolOtherModels, 1_250_000, nil, 125_000, 10_000_000, false),
		cursorRate("GPT-5.1 Codex Max", []string{"gpt-5.1-codex-max", "gpt-5-1-codex-max"}, CursorUsagePoolOtherModels, 1_250_000, nil, 125_000, 10_000_000, false),
		cursorRate("GPT-5.1 Codex Mini", []string{"gpt-5.1-codex-mini", "gpt-5-1-codex-mini"}, CursorUsagePoolOtherModels, 250_000, nil, 25_000, 2_000_000, false),
		cursorRate("GPT-5.2", []string{"gpt-5.2", "gpt-5-2"}, CursorUsagePoolOtherModels, 1_750_000, nil, 175_000, 14_000_000, false),
		cursorRate("GPT-5.2 Codex", []string{"gpt-5.2-codex", "gpt-5-2-codex"}, CursorUsagePoolOtherModels, 1_750_000, nil, 175_000, 14_000_000, false),
		cursorRate("GPT-5.3 Codex", []string{"gpt-5.3-codex", "gpt-5-3-codex"}, CursorUsagePoolOtherModels, 1_750_000, nil, 175_000, 14_000_000, false),
		cursorRate("GPT-5.4", []string{"gpt-5.4", "gpt-5-4"}, CursorUsagePoolOtherModels, 2_500_000, nil, 250_000, 15_000_000, false),
		cursorRate("GPT-5.4 Mini", []string{"gpt-5.4-mini", "gpt-5-4-mini"}, CursorUsagePoolOtherModels, 750_000, nil, 75_000, 4_500_000, false),
		cursorRate("GPT-5.4 Nano", []string{"gpt-5.4-nano", "gpt-5-4-nano"}, CursorUsagePoolOtherModels, 200_000, nil, 20_000, 1_250_000, false),
		cursorRate("GPT-5.5", []string{"gpt-5.5", "gpt-5-5"}, CursorUsagePoolOtherModels, 5_000_000, nil, 500_000, 30_000_000, false),
		cursorRate("Kimi K2.7 Code", []string{"kimi-k2.7-code", "kimi-k2-7-code"}, CursorUsagePoolOtherModels, 950_000, nil, 190_000, 4_000_000, false),
		cursorRate("Kimi K3", []string{"kimi-k3"}, CursorUsagePoolOtherModels, 3_000_000, nil, 300_000, 15_000_000, false),
	)
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
	normalized := cursorRateKey(normalizeCursorModel(model))
	rates := cursor20260816Rates
	if occurredAtMS >= CursorPricingVerifiedAtMS {
		rates = builtinCursorModelRates
	} else if occurredAtMS >= cursorGrok47ReleaseAtMS {
		rates = append(append([]CursorModelRate(nil), cursor20260816Rates...),
			builtinCursorModelRates[len(cursor20260816Rates):len(cursor20260816Rates)+4]...)
	}
	for _, rate := range rates {
		for _, pattern := range rate.Patterns {
			if normalized != pattern {
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

// CursorRateForUsage 根据单条 Dashboard 事件的 prompt token 类别选择 Grok 4.7
// 的 256k 档位。相同事件的 OccurrenceCount 只表示重复次数，不改变单次 prompt 长度。
func CursorRateForUsage(model string, occurredAtMS, input, cacheWrite, cacheRead int64) (CursorModelRate, bool) {
	rate, ok := CursorRateForModel(model, occurredAtMS)
	if !ok || input < 0 || cacheWrite < 0 || cacheRead < 0 {
		return CursorModelRate{}, false
	}
	switch rate.ModelID {
	case "Grok 4.7", "Grok 4.7 (Fast)", "Grok 4.7 500k", "Grok 4.7 500k (Fast)":
		if input > (1<<62)-cacheWrite || input+cacheWrite > (1<<62)-cacheRead {
			return CursorModelRate{}, false
		}
		long := input+cacheWrite+cacheRead > 256_000
		fast := rate.ModelID == "Grok 4.7 (Fast)" || rate.ModelID == "Grok 4.7 500k (Fast)"
		target := "Grok 4.7"
		switch {
		case long && fast:
			target = "Grok 4.7 500k (Fast)"
		case long:
			target = "Grok 4.7 500k"
		case fast:
			target = "Grok 4.7 (Fast)"
		}
		for _, tier := range builtinCursorModelRates {
			if tier.ModelID == target {
				return tier, true
			}
		}
		return CursorModelRate{}, false
	}
	return rate, true
}

func cursorRateKey(model string) string {
	for _, prefix := range []string{"cursor-models-", "other-models-", "cursor-"} {
		model = strings.TrimPrefix(model, prefix)
	}
	if !strings.HasPrefix(model, "grok-") {
		return model
	}
	for _, effort := range []string{"low", "medium", "high", "xhigh", "max", "ultra"} {
		model = strings.TrimSuffix(model, "-"+effort)
		model = strings.Replace(model, "-"+effort+"-fast", "-fast", 1)
	}
	return model
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
