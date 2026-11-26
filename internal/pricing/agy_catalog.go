package pricing

import (
	"math/big"
	"sort"
	"strings"
	"time"
)

const (
	AGYPriceVersion = "agy-api-2026-11-25"
	AGYPriceSource  = "agy_provider_api_pricing"
)

var (
	AGYPricingVerifiedAtMS = time.Date(2026, 11, 25, 0, 0, 0, 0, time.UTC).UnixMilli()

	agyCarbonPhaseCutoffMS = time.Date(2026, 12, 1, 0, 0, 0, 0, time.UTC).UnixMilli()
	agyFlashPhaseCutoffMS  = time.Date(2027, 1, 1, 0, 0, 0, 0, time.UTC).UnixMilli()
)

// AGYRate 保存 Antigravity 单个模型在指定时刻生效的每百万 Token 整数微美元参考费率。
type AGYRate struct {
	Version      string
	Source       string
	Phase        string
	ModelID      string
	DisplayName  string
	Family       string
	InputMicros  int64
	CachedMicros int64
	OutputMicros int64
}

// PricingVersion 返回可审计的价格版本标识（含生效阶段后缀）。
func (rate AGYRate) PricingVersion() string {
	if rate.Phase != "" {
		return rate.Version + ":" + rate.Phase
	}
	return rate.Version
}

// AGYReferenceRate 表示面向价格目录展示的单个模型参考费率。
type AGYReferenceRate struct {
	ModelID      string
	DisplayName  string
	Family       string
	InputMicros  int64
	CachedMicros int64
	OutputMicros int64
}

type agyRatePhase struct {
	FromMS       *int64
	UntilMS      *int64
	PhaseTag     string
	InputMicros  int64
	CachedMicros int64
	OutputMicros int64
}

type agyCatalogEntry struct {
	ModelID      string
	DisplayName  string
	Family       string
	InputMicros  int64
	CachedMicros int64
	OutputMicros int64
	Phases       []agyRatePhase
	HideInMenu   bool
}

type agyPlaceholderEntry struct {
	ModelID     string
	DisplayName string
}

var (
	agyCarbonPhases = []agyRatePhase{
		{
			UntilMS:      &agyCarbonPhaseCutoffMS,
			PhaseTag:     "phase1",
			InputMicros:  2_000_000,
			CachedMicros: 100_000,
			OutputMicros: 10_000_000,
		},
		{
			FromMS:       &agyCarbonPhaseCutoffMS,
			PhaseTag:     "phase2",
			InputMicros:  4_000_000,
			CachedMicros: 200_000,
			OutputMicros: 20_000_000,
		},
	}

	agyFlash3638Phases = []agyRatePhase{
		{
			UntilMS:      &agyFlashPhaseCutoffMS,
			PhaseTag:     "phase1",
			InputMicros:  750_000,
			CachedMicros: 75_000,
			OutputMicros: 3_750_000,
		},
		{
			FromMS:       &agyFlashPhaseCutoffMS,
			PhaseTag:     "phase2",
			InputMicros:  1_500_000,
			CachedMicros: 150_000,
			OutputMicros: 7_500_000,
		},
	}
)

var agyPlaceholderCatalog = map[string]agyPlaceholderEntry{
	"MODEL_PLACEHOLDER_M667":                      {ModelID: "gemini-pro-orchestrator", DisplayName: "Orchestrator (Carbon)"},
	"MODEL_PLACEHOLDER_M681":                      {ModelID: "gemini-pro-orchestrator-argon", DisplayName: "Orchestrator (Argon)"},
	"MODEL_PLACEHOLDER_M537":                      {ModelID: "gemini-pro-orchestrator-neon", DisplayName: "Orchestrator (Neon)"},
	"MODEL_PLACEHOLDER_M669":                      {ModelID: "gemini-flash-worker-agy", DisplayName: "Orchestrator Worker (Hydrogen)"},
	"MODEL_PLACEHOLDER_M535":                      {ModelID: "gemini-4-neon-xhigh", DisplayName: "Gemini 4 Neon (XHigh)"},
	"MODEL_PLACEHOLDER_M429":                      {ModelID: "gemini-4-carbon-high", DisplayName: "Gemini 4 Carbon (High)"},
	"MODEL_PLACEHOLDER_M427":                      {ModelID: "gemini-4-carbon-high", DisplayName: "Gemini 4 Carbon (High)"},
	"MODEL_PLACEHOLDER_M437":                      {ModelID: "gemini-4-carbon-medium", DisplayName: "Gemini 4 Carbon (Medium)"},
	"MODEL_PLACEHOLDER_M438":                      {ModelID: "gemini-4-carbon-low", DisplayName: "Gemini 4 Carbon (Low)"},
	"MODEL_PLACEHOLDER_M528":                      {ModelID: "gemini-4-hydrogen-xhigh", DisplayName: "Gemini 4 Hydrogen (XHigh)"},
	"MODEL_PLACEHOLDER_M525":                      {ModelID: "gemini-4-hydrogen-high", DisplayName: "Gemini 4 Hydrogen (High)"},
	"MODEL_PLACEHOLDER_M526":                      {ModelID: "gemini-4-hydrogen-medium", DisplayName: "Gemini 4 Hydrogen (Medium)"},
	"MODEL_PLACEHOLDER_M527":                      {ModelID: "gemini-4-hydrogen-low", DisplayName: "Gemini 4 Hydrogen (Low)"},
	"MODEL_PLACEHOLDER_M417":                      {ModelID: "gemini-4-argon-high", DisplayName: "Gemini 4 Argon (High)"},
	"MODEL_PLACEHOLDER_M418":                      {ModelID: "gemini-4-argon-medium", DisplayName: "Gemini 4 Argon (Medium)"},
	"MODEL_PLACEHOLDER_M419":                      {ModelID: "gemini-4-argon-low", DisplayName: "Gemini 4 Argon (Low)"},
	"MODEL_PLACEHOLDER_M318":                      {ModelID: "gemini-3.8-flash-high", DisplayName: "Gemini 3.8 Flash (High)"},
	"MODEL_PLACEHOLDER_M319":                      {ModelID: "gemini-3.8-flash-medium", DisplayName: "Gemini 3.8 Flash (Medium)"},
	"MODEL_PLACEHOLDER_M320":                      {ModelID: "gemini-3.8-flash-low", DisplayName: "Gemini 3.8 Flash (Low)"},
	"MODEL_PLACEHOLDER_M322":                      {ModelID: "gemini-3.8-flash-tiered", DisplayName: "Gemini 3.8 Flash (Tiered)"},
	"MODEL_PLACEHOLDER_M298":                      {ModelID: "gemini-3.7-flash-high", DisplayName: "Gemini 3.7 Flash (High)"},
	"MODEL_PLACEHOLDER_M299":                      {ModelID: "gemini-3.7-flash-medium", DisplayName: "Gemini 3.7 Flash (Medium)"},
	"MODEL_PLACEHOLDER_M300":                      {ModelID: "gemini-3.7-flash-low", DisplayName: "Gemini 3.7 Flash (Low)"},
	"MODEL_PLACEHOLDER_M301":                      {ModelID: "gemini-3.7-flash-tiered", DisplayName: "Gemini 3.7 Flash (Tiered)"},
	"MODEL_PLACEHOLDER_M264":                      {ModelID: "gemini-3.6-flash-high", DisplayName: "Gemini 3.6 Flash (High)"},
	"MODEL_PLACEHOLDER_M71":                       {ModelID: "gemini-3.6-flash-high", DisplayName: "Gemini 3.6 Flash (High)"},
	"MODEL_PLACEHOLDER_M72":                       {ModelID: "gemini-3.6-flash-medium", DisplayName: "Gemini 3.6 Flash (Medium)"},
	"MODEL_PLACEHOLDER_M73":                       {ModelID: "gemini-3.6-flash-low", DisplayName: "Gemini 3.6 Flash (Low)"},
	"MODEL_PLACEHOLDER_M196":                      {ModelID: "gemini-3.6-flash", DisplayName: "Gemini 3.6 Flash"},
	"MODEL_PLACEHOLDER_M149":                      {ModelID: "gemini-3.5-flash-high", DisplayName: "Gemini 3.5 Flash (High)"},
	"MODEL_PLACEHOLDER_M132":                      {ModelID: "gemini-3.5-flash-high", DisplayName: "Gemini 3.5 Flash (High)"},
	"MODEL_PLACEHOLDER_M84":                       {ModelID: "gemini-3.5-flash-high", DisplayName: "Gemini 3.5 Flash (High)"},
	"MODEL_PLACEHOLDER_M15":                       {ModelID: "gemini-3.5-flash-high", DisplayName: "Gemini 3.5 Flash (High)"},
	"MODEL_PLACEHOLDER_M133":                      {ModelID: "gemini-3.5-flash", DisplayName: "Gemini 3.5 Flash"},
	"MODEL_PLACEHOLDER_M20":                       {ModelID: "gemini-3.5-flash", DisplayName: "Gemini 3.5 Flash"},
	"MODEL_PLACEHOLDER_M187":                      {ModelID: "gemini-3.5-flash-extra-low", DisplayName: "Gemini 3.5 Flash (Extra Low)"},
	"MODEL_PLACEHOLDER_M198":                      {ModelID: "gemini-3.5-flash-lite", DisplayName: "Gemini 3.5 Flash Lite"},
	"MODEL_PLACEHOLDER_M37":                       {ModelID: "gemini-3.1-pro-high", DisplayName: "Gemini 3.1 Pro (High)"},
	"MODEL_PLACEHOLDER_M16":                       {ModelID: "gemini-3.1-pro-high", DisplayName: "Gemini 3.1 Pro (High)"},
	"MODEL_PLACEHOLDER_M36":                       {ModelID: "gemini-3.1-pro-low", DisplayName: "Gemini 3.1 Pro (Low)"},
	"MODEL_PLACEHOLDER_M50":                       {ModelID: "gemini-3.1-flash-lite", DisplayName: "Gemini 3.1 Flash Lite"},
	"MODEL_PLACEHOLDER_M59":                       {ModelID: "gemini-for-google-3.1-flash", DisplayName: "Gemini for Google 3.1 Flash"},
	"MODEL_PLACEHOLDER_M21":                       {ModelID: "gemini-3.1-flash-image", DisplayName: "Gemini 3.1 Flash Image"},
	"MODEL_GOOGLE_GEMINI_3_1_FLASH_IMAGE_PREVIEW": {ModelID: "gemini-3.1-flash-image-preview", DisplayName: "Gemini 3.1 Flash Image"},
	"MODEL_PLACEHOLDER_M47":                       {ModelID: "gemini-3-flash", DisplayName: "Gemini 3 Flash"},
	"MODEL_PLACEHOLDER_M18":                       {ModelID: "gemini-3-flash", DisplayName: "Gemini 3 Flash"},
	"MODEL_PLACEHOLDER_M9":                        {ModelID: "gemini-3-pro-image", DisplayName: "Gemini 3 Pro Image"},
	"MODEL_GOOGLE_GEMINI_3_PRO_IMAGE_PREVIEW":     {ModelID: "gemini-3-pro-image-preview", DisplayName: "Gemini 3 Pro Image"},
	"MODEL_GOOGLE_GEMINI_2_5_PRO":                 {ModelID: "gemini-2.5-pro", DisplayName: "Gemini 2.5 Pro"},
	"MODEL_GOOGLE_GEMINI_2_5_FLASH_IMAGE_PREVIEW": {ModelID: "gemini-2.5-flash-image-preview", DisplayName: "Gemini 2.5 Flash Image"},
	"MODEL_GOOGLE_GEMINI_2_5_FLASH":               {ModelID: "gemini-2.5-flash", DisplayName: "Gemini 2.5 Flash"},
	"MODEL_GOOGLE_GEMINI_2_5_FLASH_THINKING":      {ModelID: "gemini-2.5-flash", DisplayName: "Gemini 2.5 Flash (Thinking)"},
	"MODEL_PLACEHOLDER_M330":                      {ModelID: "gemini-2.5-flash-lite", DisplayName: "Gemini 2.5 Flash Lite"},
	"MODEL_GOOGLE_GEMINI_2_5_FLASH_LITE":          {ModelID: "gemini-2.5-flash-lite", DisplayName: "Gemini 2.5 Flash Lite"},
	"MODEL_PLACEHOLDER_M406":                      {ModelID: "claude-opus-5-5-max", DisplayName: "Claude Opus 5.5 (Max)"},
	"MODEL_PLACEHOLDER_M402":                      {ModelID: "claude-opus-5-5-high", DisplayName: "Claude Opus 5.5 (High)"},
	"MODEL_PLACEHOLDER_M401":                      {ModelID: "claude-opus-5-5-medium", DisplayName: "Claude Opus 5.5 (Medium)"},
	"MODEL_PLACEHOLDER_M400":                      {ModelID: "claude-opus-5-5-low", DisplayName: "Claude Opus 5.5 (Low)"},
	"MODEL_PLACEHOLDER_M368":                      {ModelID: "claude-opus-5-5", DisplayName: "Claude Opus 5.5"},
	"MODEL_PLACEHOLDER_M407":                      {ModelID: "claude-sonnet-5-5-max", DisplayName: "Claude Sonnet 5.5 (Max)"},
	"MODEL_PLACEHOLDER_M405":                      {ModelID: "claude-sonnet-5-5-high", DisplayName: "Claude Sonnet 5.5 (High)"},
	"MODEL_PLACEHOLDER_M404":                      {ModelID: "claude-sonnet-5-5-medium", DisplayName: "Claude Sonnet 5.5 (Medium)"},
	"MODEL_PLACEHOLDER_M403":                      {ModelID: "claude-sonnet-5-5-low", DisplayName: "Claude Sonnet 5.5 (Low)"},
	"MODEL_PLACEHOLDER_M254":                      {ModelID: "claude-opus-4-8", DisplayName: "Claude Opus 4.8"},
	"MODEL_PLACEHOLDER_M26":                       {ModelID: "claude-opus-4-6-thinking", DisplayName: "Claude Opus 4.6 (Thinking)"},
	"MODEL_PLACEHOLDER_M35":                       {ModelID: "claude-sonnet-4-6", DisplayName: "Claude Sonnet 4.6 (Thinking)"},
	"MODEL_PLACEHOLDER_M12":                       {ModelID: "claude-opus-4-5-thinking", DisplayName: "Claude Opus 4.5 (Thinking)"},
	"MODEL_PLACEHOLDER_M11":                       {ModelID: "claude-opus-4-5", DisplayName: "Claude Opus 4.5"},
	"MODEL_CLAUDE_4_5_SONNET_THINKING":            {ModelID: "claude-sonnet-4-5-thinking", DisplayName: "Claude Sonnet 4.5 (Thinking)"},
	"MODEL_CLAUDE_4_5_SONNET":                     {ModelID: "claude-sonnet-4-5", DisplayName: "Claude Sonnet 4.5"},
	"MODEL_OPENAI_GPT_OSS_120B_MEDIUM":            {ModelID: "gpt-oss-120b-medium", DisplayName: "GPT-OSS 120B (Medium)"},
}

var agyPricingCatalog = []agyCatalogEntry{
	// Gemini Orchestrator & Gemini 4 Carbon / Argon / Neon / Hydrogen / Helix
	{ModelID: "gemini-pro-orchestrator", DisplayName: "Orchestrator (Carbon)", Family: "Gemini", InputMicros: 2_000_000, CachedMicros: 100_000, OutputMicros: 10_000_000, Phases: agyCarbonPhases},
	{ModelID: "orchestrator-carbon", DisplayName: "Orchestrator (Carbon)", Family: "Gemini", InputMicros: 2_000_000, CachedMicros: 100_000, OutputMicros: 10_000_000, Phases: agyCarbonPhases, HideInMenu: true},
	{ModelID: "gemini-pro-orchestrator-argon", DisplayName: "Orchestrator (Argon)", Family: "Gemini", InputMicros: 2_000_000, CachedMicros: 100_000, OutputMicros: 10_000_000, Phases: agyCarbonPhases},
	{ModelID: "orchestrator-argon", DisplayName: "Orchestrator (Argon)", Family: "Gemini", InputMicros: 2_000_000, CachedMicros: 100_000, OutputMicros: 10_000_000, Phases: agyCarbonPhases, HideInMenu: true},
	{ModelID: "gemini-pro-orchestrator-neon", DisplayName: "Orchestrator (Neon)", Family: "Gemini", InputMicros: 3_000_000, CachedMicros: 150_000, OutputMicros: 15_000_000},
	{ModelID: "orchestrator-neon", DisplayName: "Orchestrator (Neon)", Family: "Gemini", InputMicros: 3_000_000, CachedMicros: 150_000, OutputMicros: 15_000_000, HideInMenu: true},
	{ModelID: "orchestrator-worker-neon", DisplayName: "Orchestrator Worker (Neon)", Family: "Gemini", InputMicros: 3_000_000, CachedMicros: 150_000, OutputMicros: 15_000_000, HideInMenu: true},
	{ModelID: "gemini-neon-worker-agy", DisplayName: "Orchestrator Worker (Neon)", Family: "Gemini", InputMicros: 3_000_000, CachedMicros: 150_000, OutputMicros: 15_000_000, HideInMenu: true},
	{ModelID: "gemini-4-neon-worker-agy", DisplayName: "Orchestrator Worker (Neon)", Family: "Gemini", InputMicros: 3_000_000, CachedMicros: 150_000, OutputMicros: 15_000_000, HideInMenu: true},
	{ModelID: "gemini-4-carbon", DisplayName: "Gemini 4 Carbon", Family: "Gemini", InputMicros: 2_000_000, CachedMicros: 100_000, OutputMicros: 10_000_000, Phases: agyCarbonPhases},
	{ModelID: "gemini-4-carbon-high", DisplayName: "Gemini 4 Carbon (High)", Family: "Gemini", InputMicros: 2_000_000, CachedMicros: 100_000, OutputMicros: 10_000_000, Phases: agyCarbonPhases, HideInMenu: true},
	{ModelID: "gemini-4-carbon-medium", DisplayName: "Gemini 4 Carbon (Medium)", Family: "Gemini", InputMicros: 2_000_000, CachedMicros: 100_000, OutputMicros: 10_000_000, Phases: agyCarbonPhases, HideInMenu: true},
	{ModelID: "gemini-4-carbon-low", DisplayName: "Gemini 4 Carbon (Low)", Family: "Gemini", InputMicros: 2_000_000, CachedMicros: 100_000, OutputMicros: 10_000_000, Phases: agyCarbonPhases, HideInMenu: true},
	{ModelID: "gemini-4-argon", DisplayName: "Gemini 4 Argon", Family: "Gemini", InputMicros: 2_000_000, CachedMicros: 100_000, OutputMicros: 10_000_000, Phases: agyCarbonPhases},
	{ModelID: "gemini-4-argon-high", DisplayName: "Gemini 4 Argon (High)", Family: "Gemini", InputMicros: 2_000_000, CachedMicros: 100_000, OutputMicros: 10_000_000, Phases: agyCarbonPhases, HideInMenu: true},
	{ModelID: "gemini-4-argon-medium", DisplayName: "Gemini 4 Argon (Medium)", Family: "Gemini", InputMicros: 2_000_000, CachedMicros: 100_000, OutputMicros: 10_000_000, Phases: agyCarbonPhases, HideInMenu: true},
	{ModelID: "gemini-4-argon-low", DisplayName: "Gemini 4 Argon (Low)", Family: "Gemini", InputMicros: 2_000_000, CachedMicros: 100_000, OutputMicros: 10_000_000, Phases: agyCarbonPhases, HideInMenu: true},
	{ModelID: "gemini-4-argon-b", DisplayName: "Gemini 4 Argon", Family: "Gemini", InputMicros: 2_000_000, CachedMicros: 100_000, OutputMicros: 10_000_000, Phases: agyCarbonPhases, HideInMenu: true},
	{ModelID: "gemini-4-argon-f", DisplayName: "Gemini 4 Argon", Family: "Gemini", InputMicros: 2_000_000, CachedMicros: 100_000, OutputMicros: 10_000_000, Phases: agyCarbonPhases, HideInMenu: true},
	{ModelID: "gemini-4-argon-trial", DisplayName: "Gemini 4 Argon", Family: "Gemini", InputMicros: 2_000_000, CachedMicros: 100_000, OutputMicros: 10_000_000, Phases: agyCarbonPhases, HideInMenu: true},
	{ModelID: "gemini-4-argon-disabled", DisplayName: "Gemini 4 Argon", Family: "Gemini", InputMicros: 2_000_000, CachedMicros: 100_000, OutputMicros: 10_000_000, Phases: agyCarbonPhases, HideInMenu: true},
	{ModelID: "gemini-4-neon", DisplayName: "Gemini 4 Neon", Family: "Gemini", InputMicros: 3_000_000, CachedMicros: 150_000, OutputMicros: 15_000_000},
	{ModelID: "gemini-4-neon-xhigh", DisplayName: "Gemini 4 Neon (XHigh)", Family: "Gemini", InputMicros: 3_000_000, CachedMicros: 150_000, OutputMicros: 15_000_000, HideInMenu: true},
	{ModelID: "gemini-4-neon-high", DisplayName: "Gemini 4 Neon (High)", Family: "Gemini", InputMicros: 3_000_000, CachedMicros: 150_000, OutputMicros: 15_000_000, HideInMenu: true},
	{ModelID: "gemini-4-neon-medium", DisplayName: "Gemini 4 Neon (Medium)", Family: "Gemini", InputMicros: 3_000_000, CachedMicros: 150_000, OutputMicros: 15_000_000, HideInMenu: true},
	{ModelID: "gemini-4-neon-low", DisplayName: "Gemini 4 Neon (Low)", Family: "Gemini", InputMicros: 3_000_000, CachedMicros: 150_000, OutputMicros: 15_000_000, HideInMenu: true},
	{ModelID: "gemini-4-neon-tiered", DisplayName: "Gemini 4 Neon (Tiered)", Family: "Gemini", InputMicros: 3_000_000, CachedMicros: 150_000, OutputMicros: 15_000_000, HideInMenu: true},
	{ModelID: "gemini-neon", DisplayName: "Gemini Neon", Family: "Gemini", InputMicros: 3_000_000, CachedMicros: 150_000, OutputMicros: 15_000_000, HideInMenu: true},
	{ModelID: "gemini-neon-xhigh", DisplayName: "Gemini Neon (XHigh)", Family: "Gemini", InputMicros: 3_000_000, CachedMicros: 150_000, OutputMicros: 15_000_000, HideInMenu: true},
	{ModelID: "gemini-neon-high", DisplayName: "Gemini Neon (High)", Family: "Gemini", InputMicros: 3_000_000, CachedMicros: 150_000, OutputMicros: 15_000_000, HideInMenu: true},
	{ModelID: "gemini-neon-medium", DisplayName: "Gemini Neon (Medium)", Family: "Gemini", InputMicros: 3_000_000, CachedMicros: 150_000, OutputMicros: 15_000_000, HideInMenu: true},
	{ModelID: "gemini-neon-low", DisplayName: "Gemini Neon (Low)", Family: "Gemini", InputMicros: 3_000_000, CachedMicros: 150_000, OutputMicros: 15_000_000, HideInMenu: true},
	{ModelID: "gemini-neon-tiered", DisplayName: "Gemini Neon (Tiered)", Family: "Gemini", InputMicros: 3_000_000, CachedMicros: 150_000, OutputMicros: 15_000_000, HideInMenu: true},
	{ModelID: "gemini-4-hydrogen", DisplayName: "Gemini 4 Hydrogen", Family: "Gemini", InputMicros: 1_500_000, CachedMicros: 75_000, OutputMicros: 7_500_000},
	{ModelID: "gemini-4-hydrogen-xhigh", DisplayName: "Gemini 4 Hydrogen (XHigh)", Family: "Gemini", InputMicros: 1_500_000, CachedMicros: 75_000, OutputMicros: 7_500_000, HideInMenu: true},
	{ModelID: "gemini-4-hydrogen-high", DisplayName: "Gemini 4 Hydrogen (High)", Family: "Gemini", InputMicros: 1_500_000, CachedMicros: 75_000, OutputMicros: 7_500_000, HideInMenu: true},
	{ModelID: "gemini-4-hydrogen-medium", DisplayName: "Gemini 4 Hydrogen (Medium)", Family: "Gemini", InputMicros: 1_500_000, CachedMicros: 75_000, OutputMicros: 7_500_000, HideInMenu: true},
	{ModelID: "gemini-4-hydrogen-low", DisplayName: "Gemini 4 Hydrogen (Low)", Family: "Gemini", InputMicros: 1_500_000, CachedMicros: 75_000, OutputMicros: 7_500_000, HideInMenu: true},
	{ModelID: "gemini-4-hydrogen-tiered", DisplayName: "Gemini 4 Hydrogen (Tiered)", Family: "Gemini", InputMicros: 1_500_000, CachedMicros: 75_000, OutputMicros: 7_500_000, HideInMenu: true},
	{ModelID: "gemini-flash-worker-agy", DisplayName: "Orchestrator Worker (Hydrogen)", Family: "Gemini", InputMicros: 1_500_000, CachedMicros: 75_000, OutputMicros: 7_500_000, HideInMenu: true},
	{ModelID: "gemini-hydrogen", DisplayName: "Gemini Hydrogen", Family: "Gemini", InputMicros: 1_500_000, CachedMicros: 75_000, OutputMicros: 7_500_000, HideInMenu: true},
	{ModelID: "gemini-hydrogen-xhigh", DisplayName: "Gemini Hydrogen (XHigh)", Family: "Gemini", InputMicros: 1_500_000, CachedMicros: 75_000, OutputMicros: 7_500_000, HideInMenu: true},
	{ModelID: "gemini-hydrogen-high", DisplayName: "Gemini Hydrogen (High)", Family: "Gemini", InputMicros: 1_500_000, CachedMicros: 75_000, OutputMicros: 7_500_000, HideInMenu: true},
	{ModelID: "gemini-hydrogen-medium", DisplayName: "Gemini Hydrogen (Medium)", Family: "Gemini", InputMicros: 1_500_000, CachedMicros: 75_000, OutputMicros: 7_500_000, HideInMenu: true},
	{ModelID: "gemini-hydrogen-low", DisplayName: "Gemini Hydrogen (Low)", Family: "Gemini", InputMicros: 1_500_000, CachedMicros: 75_000, OutputMicros: 7_500_000, HideInMenu: true},
	{ModelID: "gemini-hydrogen-tiered", DisplayName: "Gemini Hydrogen (Tiered)", Family: "Gemini", InputMicros: 1_500_000, CachedMicros: 75_000, OutputMicros: 7_500_000, HideInMenu: true},
	{ModelID: "gemini-4-helix", DisplayName: "Gemini 4 Helix", Family: "Gemini", InputMicros: 300_000, CachedMicros: 15_000, OutputMicros: 2_500_000},
	{ModelID: "gemini-4-helix-high", DisplayName: "Gemini 4 Helix (High)", Family: "Gemini", InputMicros: 300_000, CachedMicros: 15_000, OutputMicros: 2_500_000, HideInMenu: true},
	{ModelID: "gemini-4-helix-medium", DisplayName: "Gemini 4 Helix (Medium)", Family: "Gemini", InputMicros: 300_000, CachedMicros: 15_000, OutputMicros: 2_500_000, HideInMenu: true},
	{ModelID: "gemini-4-helix-low", DisplayName: "Gemini 4 Helix (Low)", Family: "Gemini", InputMicros: 300_000, CachedMicros: 15_000, OutputMicros: 2_500_000, HideInMenu: true},
	{ModelID: "gemini-helix", DisplayName: "Gemini Helix", Family: "Gemini", InputMicros: 300_000, CachedMicros: 15_000, OutputMicros: 2_500_000, HideInMenu: true},
	{ModelID: "gemini-helix-high", DisplayName: "Gemini Helix (High)", Family: "Gemini", InputMicros: 300_000, CachedMicros: 15_000, OutputMicros: 2_500_000, HideInMenu: true},
	{ModelID: "gemini-helix-medium", DisplayName: "Gemini Helix (Medium)", Family: "Gemini", InputMicros: 300_000, CachedMicros: 15_000, OutputMicros: 2_500_000, HideInMenu: true},
	{ModelID: "gemini-helix-low", DisplayName: "Gemini Helix (Low)", Family: "Gemini", InputMicros: 300_000, CachedMicros: 15_000, OutputMicros: 2_500_000, HideInMenu: true},

	// Gemini 3.x / 2.5
	{ModelID: "gemini-3.8-flash", DisplayName: "Gemini 3.8 Flash", Family: "Gemini", InputMicros: 750_000, CachedMicros: 75_000, OutputMicros: 3_750_000, Phases: agyFlash3638Phases},
	{ModelID: "gemini-3.8-flash-high", DisplayName: "Gemini 3.8 Flash (High)", Family: "Gemini", InputMicros: 750_000, CachedMicros: 75_000, OutputMicros: 3_750_000, Phases: agyFlash3638Phases, HideInMenu: true},
	{ModelID: "gemini-3.8-flash-medium", DisplayName: "Gemini 3.8 Flash (Medium)", Family: "Gemini", InputMicros: 750_000, CachedMicros: 75_000, OutputMicros: 3_750_000, Phases: agyFlash3638Phases, HideInMenu: true},
	{ModelID: "gemini-3.8-flash-low", DisplayName: "Gemini 3.8 Flash (Low)", Family: "Gemini", InputMicros: 300_000, CachedMicros: 30_000, OutputMicros: 2_500_000},
	{ModelID: "gemini-3.8-flash-tiered", DisplayName: "Gemini 3.8 Flash (Tiered)", Family: "Gemini", InputMicros: 750_000, CachedMicros: 75_000, OutputMicros: 3_750_000, Phases: agyFlash3638Phases, HideInMenu: true},
	{ModelID: "gemini-3.8-flash-n", DisplayName: "Gemini 3.8 Flash", Family: "Gemini", InputMicros: 750_000, CachedMicros: 75_000, OutputMicros: 3_750_000, Phases: agyFlash3638Phases, HideInMenu: true},
	{ModelID: "gemini-3.7-flash", DisplayName: "Gemini 3.7 Flash", Family: "Gemini", InputMicros: 750_000, CachedMicros: 75_000, OutputMicros: 3_750_000, Phases: agyFlash3638Phases},
	{ModelID: "gemini-3.7-flash-high", DisplayName: "Gemini 3.7 Flash (High)", Family: "Gemini", InputMicros: 750_000, CachedMicros: 75_000, OutputMicros: 3_750_000, Phases: agyFlash3638Phases, HideInMenu: true},
	{ModelID: "gemini-3.7-flash-medium", DisplayName: "Gemini 3.7 Flash (Medium)", Family: "Gemini", InputMicros: 750_000, CachedMicros: 75_000, OutputMicros: 3_750_000, Phases: agyFlash3638Phases, HideInMenu: true},
	{ModelID: "gemini-3.7-flash-low", DisplayName: "Gemini 3.7 Flash (Low)", Family: "Gemini", InputMicros: 300_000, CachedMicros: 30_000, OutputMicros: 2_500_000, HideInMenu: true},
	{ModelID: "gemini-3.7-flash-tiered", DisplayName: "Gemini 3.7 Flash (Tiered)", Family: "Gemini", InputMicros: 750_000, CachedMicros: 75_000, OutputMicros: 3_750_000, Phases: agyFlash3638Phases, HideInMenu: true},
	{ModelID: "gemini-3.7-flash-safety-le", DisplayName: "Gemini 3.7 Flash", Family: "Gemini", InputMicros: 750_000, CachedMicros: 75_000, OutputMicros: 3_750_000, Phases: agyFlash3638Phases, HideInMenu: true},
	{ModelID: "gemini-3.6-flash", DisplayName: "Gemini 3.6 Flash", Family: "Gemini", InputMicros: 750_000, CachedMicros: 75_000, OutputMicros: 3_750_000, Phases: agyFlash3638Phases},
	{ModelID: "gemini-3.6-flash-high", DisplayName: "Gemini 3.6 Flash (High)", Family: "Gemini", InputMicros: 750_000, CachedMicros: 75_000, OutputMicros: 3_750_000, Phases: agyFlash3638Phases, HideInMenu: true},
	{ModelID: "gemini-3.6-flash-medium", DisplayName: "Gemini 3.6 Flash (Medium)", Family: "Gemini", InputMicros: 750_000, CachedMicros: 75_000, OutputMicros: 3_750_000, Phases: agyFlash3638Phases, HideInMenu: true},
	{ModelID: "gemini-3.6-flash-low", DisplayName: "Gemini 3.6 Flash (Low)", Family: "Gemini", InputMicros: 300_000, CachedMicros: 30_000, OutputMicros: 2_500_000, HideInMenu: true},
	{ModelID: "gemini-3.5-flash", DisplayName: "Gemini 3.5 Flash", Family: "Gemini", InputMicros: 1_500_000, CachedMicros: 150_000, OutputMicros: 9_000_000},
	{ModelID: "gemini-3.5-flash-high", DisplayName: "Gemini 3.5 Flash (High)", Family: "Gemini", InputMicros: 1_500_000, CachedMicros: 150_000, OutputMicros: 9_000_000, HideInMenu: true},
	{ModelID: "gemini-3.5-flash-medium", DisplayName: "Gemini 3.5 Flash (Medium)", Family: "Gemini", InputMicros: 1_500_000, CachedMicros: 150_000, OutputMicros: 9_000_000, HideInMenu: true},
	{ModelID: "gemini-3.5-flash-low", DisplayName: "Gemini 3.5 Flash (Low)", Family: "Gemini", InputMicros: 1_500_000, CachedMicros: 150_000, OutputMicros: 9_000_000, HideInMenu: true},
	{ModelID: "gemini-3.5-flash-extra-low", DisplayName: "Gemini 3.5 Flash (Extra Low)", Family: "Gemini", InputMicros: 300_000, CachedMicros: 30_000, OutputMicros: 2_500_000, HideInMenu: true},
	{ModelID: "gemini-3.5-flash-lite", DisplayName: "Gemini 3.5 Flash Lite", Family: "Gemini", InputMicros: 300_000, CachedMicros: 30_000, OutputMicros: 2_500_000},
	{ModelID: "gemini-3-flash-a", DisplayName: "Gemini 3.5 Flash", Family: "Gemini", InputMicros: 1_500_000, CachedMicros: 150_000, OutputMicros: 9_000_000, HideInMenu: true},
	{ModelID: "gemini-3-flash-b", DisplayName: "Gemini 3.5 Flash", Family: "Gemini", InputMicros: 1_500_000, CachedMicros: 150_000, OutputMicros: 9_000_000, HideInMenu: true},
	{ModelID: "gemini-3-flash-d", DisplayName: "Gemini 3.5 Flash", Family: "Gemini", InputMicros: 1_500_000, CachedMicros: 150_000, OutputMicros: 9_000_000, HideInMenu: true},
	{ModelID: "gemini-3-flash-agent", DisplayName: "Gemini 3.5 Flash", Family: "Gemini", InputMicros: 1_500_000, CachedMicros: 150_000, OutputMicros: 9_000_000, HideInMenu: true},
	{ModelID: "gemini-3.1-pro", DisplayName: "Gemini 3.1 Pro", Family: "Gemini", InputMicros: 2_000_000, CachedMicros: 200_000, OutputMicros: 12_000_000},
	{ModelID: "gemini-3.1-pro-high", DisplayName: "Gemini 3.1 Pro (High)", Family: "Gemini", InputMicros: 2_000_000, CachedMicros: 200_000, OutputMicros: 12_000_000, HideInMenu: true},
	{ModelID: "gemini-3.1-pro-low", DisplayName: "Gemini 3.1 Pro (Low)", Family: "Gemini", InputMicros: 2_000_000, CachedMicros: 200_000, OutputMicros: 12_000_000, HideInMenu: true},
	{ModelID: "gemini-3.1-pro-low-thinking", DisplayName: "Gemini 3.1 Pro (Low)", Family: "Gemini", InputMicros: 2_000_000, CachedMicros: 200_000, OutputMicros: 12_000_000, HideInMenu: true},
	{ModelID: "gemini-3.1-pro-preview", DisplayName: "Gemini 3.1 Pro", Family: "Gemini", InputMicros: 2_000_000, CachedMicros: 200_000, OutputMicros: 12_000_000, HideInMenu: true},
	{ModelID: "gemini-3.1-pro-preview-customtools", DisplayName: "Gemini 3.1 Pro", Family: "Gemini", InputMicros: 2_000_000, CachedMicros: 200_000, OutputMicros: 12_000_000, HideInMenu: true},
	{ModelID: "gemini-pro-default", DisplayName: "Gemini 3.1 Pro", Family: "Gemini", InputMicros: 2_000_000, CachedMicros: 200_000, OutputMicros: 12_000_000, HideInMenu: true},
	{ModelID: "gemini-pro-agent", DisplayName: "Gemini 3.1 Pro", Family: "Gemini", InputMicros: 2_000_000, CachedMicros: 200_000, OutputMicros: 12_000_000, HideInMenu: true},
	{ModelID: "gemini-pro-b", DisplayName: "Gemini 3.1 Pro", Family: "Gemini", InputMicros: 2_000_000, CachedMicros: 200_000, OutputMicros: 12_000_000, HideInMenu: true},
	{ModelID: "gemini-3.1-flash-lite", DisplayName: "Gemini 3.1 Flash Lite", Family: "Gemini", InputMicros: 250_000, CachedMicros: 25_000, OutputMicros: 1_500_000},
	{ModelID: "gemini-3.1-flash-lite-preview", DisplayName: "Gemini 3.1 Flash Lite", Family: "Gemini", InputMicros: 250_000, CachedMicros: 25_000, OutputMicros: 1_500_000, HideInMenu: true},
	{ModelID: "gemini-for-google-3.1-flash", DisplayName: "Gemini for Google 3.1 Flash", Family: "Gemini", InputMicros: 500_000, CachedMicros: 50_000, OutputMicros: 3_000_000, HideInMenu: true},
	{ModelID: "gemini-3.1-flash-image", DisplayName: "Gemini 3.1 Flash Image", Family: "Gemini", InputMicros: 500_000, CachedMicros: 50_000, OutputMicros: 3_000_000, HideInMenu: true},
	{ModelID: "gemini-3.1-flash-image-preview", DisplayName: "Gemini 3.1 Flash Image", Family: "Gemini", InputMicros: 500_000, CachedMicros: 50_000, OutputMicros: 3_000_000, HideInMenu: true},
	{ModelID: "gemini-3-flash", DisplayName: "Gemini 3 Flash", Family: "Gemini", InputMicros: 500_000, CachedMicros: 50_000, OutputMicros: 3_000_000},
	{ModelID: "gemini-3-flash-preview", DisplayName: "Gemini 3 Flash", Family: "Gemini", InputMicros: 500_000, CachedMicros: 50_000, OutputMicros: 3_000_000, HideInMenu: true},
	{ModelID: "gemini-3-flash-c", DisplayName: "Gemini 3 Flash", Family: "Gemini", InputMicros: 500_000, CachedMicros: 50_000, OutputMicros: 3_000_000, HideInMenu: true},
	{ModelID: "gemini-3-pro-image", DisplayName: "Gemini 3 Pro Image", Family: "Gemini", InputMicros: 2_000_000, CachedMicros: 200_000, OutputMicros: 12_000_000, HideInMenu: true},
	{ModelID: "gemini-3-pro-image-preview", DisplayName: "Gemini 3 Pro Image", Family: "Gemini", InputMicros: 2_000_000, CachedMicros: 200_000, OutputMicros: 12_000_000, HideInMenu: true},
	{ModelID: "gemini-2.5-pro", DisplayName: "Gemini 2.5 Pro", Family: "Gemini", InputMicros: 1_250_000, CachedMicros: 125_000, OutputMicros: 10_000_000},
	{ModelID: "gemini-2.5-flash", DisplayName: "Gemini 2.5 Flash", Family: "Gemini", InputMicros: 300_000, CachedMicros: 30_000, OutputMicros: 2_500_000},
	{ModelID: "gemini-2.5-flash-image", DisplayName: "Gemini 2.5 Flash Image", Family: "Gemini", InputMicros: 300_000, CachedMicros: 30_000, OutputMicros: 2_500_000, HideInMenu: true},
	{ModelID: "gemini-2.5-flash-image-preview", DisplayName: "Gemini 2.5 Flash Image", Family: "Gemini", InputMicros: 300_000, CachedMicros: 30_000, OutputMicros: 2_500_000, HideInMenu: true},
	{ModelID: "gemini-2.5-flash-lite", DisplayName: "Gemini 2.5 Flash Lite", Family: "Gemini", InputMicros: 100_000, CachedMicros: 10_000, OutputMicros: 400_000},

	// Vertex Claude
	{ModelID: "orchestrator-opus-5-5", DisplayName: "Orchestrator (Opus 5.5)", Family: "Claude", InputMicros: 4_000_000, CachedMicros: 200_000, OutputMicros: 20_000_000},
	{ModelID: "orchestrator-claude-opus-5-5", DisplayName: "Orchestrator (Opus 5.5)", Family: "Claude", InputMicros: 4_000_000, CachedMicros: 200_000, OutputMicros: 20_000_000, HideInMenu: true},
	{ModelID: "claude-opus-5-5-orchestrator", DisplayName: "Orchestrator (Opus 5.5)", Family: "Claude", InputMicros: 4_000_000, CachedMicros: 200_000, OutputMicros: 20_000_000, HideInMenu: true},
	{ModelID: "claude-fable-5-2", DisplayName: "Claude Fable 5.2", Family: "Claude", InputMicros: 8_000_000, CachedMicros: 200_000, OutputMicros: 40_000_000},
	{ModelID: "claude-fable-5-2-max", DisplayName: "Claude Fable 5.2 (Max)", Family: "Claude", InputMicros: 8_000_000, CachedMicros: 200_000, OutputMicros: 40_000_000, HideInMenu: true},
	{ModelID: "claude-fable-5-2-high", DisplayName: "Claude Fable 5.2 (High)", Family: "Claude", InputMicros: 8_000_000, CachedMicros: 200_000, OutputMicros: 40_000_000, HideInMenu: true},
	{ModelID: "claude-fable-5-2-medium", DisplayName: "Claude Fable 5.2 (Medium)", Family: "Claude", InputMicros: 8_000_000, CachedMicros: 200_000, OutputMicros: 40_000_000, HideInMenu: true},
	{ModelID: "claude-fable-5-2-low", DisplayName: "Claude Fable 5.2 (Low)", Family: "Claude", InputMicros: 8_000_000, CachedMicros: 200_000, OutputMicros: 40_000_000, HideInMenu: true},
	{ModelID: "claude-fable-5-1", DisplayName: "Claude Fable 5.1", Family: "Claude", InputMicros: 10_000_000, CachedMicros: 250_000, OutputMicros: 50_000_000},
	{ModelID: "claude-fable-5", DisplayName: "Claude Fable 5", Family: "Claude", InputMicros: 10_000_000, CachedMicros: 1_000_000, OutputMicros: 50_000_000},
	{ModelID: "claude-opus-5-5", DisplayName: "Claude Opus 5.5", Family: "Claude", InputMicros: 4_000_000, CachedMicros: 200_000, OutputMicros: 20_000_000},
	{ModelID: "claude-opus-5-5-max", DisplayName: "Claude Opus 5.5 (Max)", Family: "Claude", InputMicros: 4_000_000, CachedMicros: 200_000, OutputMicros: 20_000_000, HideInMenu: true},
	{ModelID: "claude-opus-5-5-high", DisplayName: "Claude Opus 5.5 (High)", Family: "Claude", InputMicros: 4_000_000, CachedMicros: 200_000, OutputMicros: 20_000_000, HideInMenu: true},
	{ModelID: "claude-opus-5-5-medium", DisplayName: "Claude Opus 5.5 (Medium)", Family: "Claude", InputMicros: 4_000_000, CachedMicros: 200_000, OutputMicros: 20_000_000, HideInMenu: true},
	{ModelID: "claude-opus-5-5-low", DisplayName: "Claude Opus 5.5 (Low)", Family: "Claude", InputMicros: 4_000_000, CachedMicros: 200_000, OutputMicros: 20_000_000, HideInMenu: true},
	{ModelID: "claude-opus-5", DisplayName: "Claude Opus 5", Family: "Claude", InputMicros: 5_000_000, CachedMicros: 500_000, OutputMicros: 25_000_000},
	{ModelID: "claude-opus-4-8", DisplayName: "Claude Opus 4.8", Family: "Claude", InputMicros: 5_000_000, CachedMicros: 500_000, OutputMicros: 25_000_000},
	{ModelID: "claude-opus-4-7", DisplayName: "Claude Opus 4.7", Family: "Claude", InputMicros: 5_000_000, CachedMicros: 500_000, OutputMicros: 25_000_000},
	{ModelID: "claude-opus-4-6", DisplayName: "Claude Opus 4.6", Family: "Claude", InputMicros: 5_000_000, CachedMicros: 500_000, OutputMicros: 25_000_000},
	{ModelID: "claude-opus-4-6-thinking", DisplayName: "Claude Opus 4.6 (Thinking)", Family: "Claude", InputMicros: 5_000_000, CachedMicros: 500_000, OutputMicros: 25_000_000, HideInMenu: true},
	{ModelID: "claude-opus-4-5", DisplayName: "Claude Opus 4.5", Family: "Claude", InputMicros: 5_000_000, CachedMicros: 500_000, OutputMicros: 25_000_000},
	{ModelID: "claude-opus-4-5-thinking", DisplayName: "Claude Opus 4.5 (Thinking)", Family: "Claude", InputMicros: 5_000_000, CachedMicros: 500_000, OutputMicros: 25_000_000, HideInMenu: true},
	{ModelID: "claude-sonnet-5-5", DisplayName: "Claude Sonnet 5.5", Family: "Claude", InputMicros: 2_000_000, CachedMicros: 100_000, OutputMicros: 10_000_000},
	{ModelID: "claude-sonnet-5-5-max", DisplayName: "Claude Sonnet 5.5 (Max)", Family: "Claude", InputMicros: 2_000_000, CachedMicros: 100_000, OutputMicros: 10_000_000, HideInMenu: true},
	{ModelID: "claude-sonnet-5-5-high", DisplayName: "Claude Sonnet 5.5 (High)", Family: "Claude", InputMicros: 2_000_000, CachedMicros: 100_000, OutputMicros: 10_000_000, HideInMenu: true},
	{ModelID: "claude-sonnet-5-5-medium", DisplayName: "Claude Sonnet 5.5 (Medium)", Family: "Claude", InputMicros: 2_000_000, CachedMicros: 100_000, OutputMicros: 10_000_000, HideInMenu: true},
	{ModelID: "claude-sonnet-5-5-low", DisplayName: "Claude Sonnet 5.5 (Low)", Family: "Claude", InputMicros: 2_000_000, CachedMicros: 100_000, OutputMicros: 10_000_000, HideInMenu: true},
	{ModelID: "claude-sonnet-5", DisplayName: "Claude Sonnet 5", Family: "Claude", InputMicros: 2_000_000, CachedMicros: 200_000, OutputMicros: 10_000_000},
	{ModelID: "claude-sonnet-4-6", DisplayName: "Claude Sonnet 4.6", Family: "Claude", InputMicros: 3_000_000, CachedMicros: 300_000, OutputMicros: 15_000_000},
	{ModelID: "claude-sonnet-4-5", DisplayName: "Claude Sonnet 4.5", Family: "Claude", InputMicros: 3_000_000, CachedMicros: 300_000, OutputMicros: 15_000_000},
	{ModelID: "claude-sonnet-4-5-thinking", DisplayName: "Claude Sonnet 4.5 (Thinking)", Family: "Claude", InputMicros: 3_000_000, CachedMicros: 300_000, OutputMicros: 15_000_000, HideInMenu: true},
	{ModelID: "claude-haiku-5-5", DisplayName: "Claude Haiku 5.5", Family: "Claude", InputMicros: 100_000, CachedMicros: 10_000, OutputMicros: 500_000},
	{ModelID: "claude-haiku-4-5", DisplayName: "Claude Haiku 4.5", Family: "Claude", InputMicros: 1_000_000, CachedMicros: 100_000, OutputMicros: 5_000_000},

	// OpenAI GPT & o-series
	{ModelID: "gpt-6-astra", DisplayName: "GPT-6 Astra", Family: "GPT", InputMicros: 10_000_000, CachedMicros: 1_000_000, OutputMicros: 50_000_000},
	{ModelID: "gpt-6.1-sol", DisplayName: "GPT-6.1 Sol", Family: "GPT", InputMicros: 2_000_000, CachedMicros: 100_000, OutputMicros: 10_000_000},
	{ModelID: "gpt-6.1-terra", DisplayName: "GPT-6.1 Terra", Family: "GPT", InputMicros: 1_000_000, CachedMicros: 50_000, OutputMicros: 5_000_000},
	{ModelID: "gpt-6-sol", DisplayName: "GPT-6 Sol", Family: "GPT", InputMicros: 2_000_000, CachedMicros: 200_000, OutputMicros: 10_000_000},
	{ModelID: "gpt-6-luna", DisplayName: "GPT-6 Luna", Family: "GPT", InputMicros: 100_000, CachedMicros: 10_000, OutputMicros: 500_000},
	{ModelID: "gpt-5.6", DisplayName: "GPT-5.6", Family: "GPT", InputMicros: 4_000_000, CachedMicros: 400_000, OutputMicros: 20_000_000},
	{ModelID: "gpt-5.6-sol", DisplayName: "GPT-5.6 Sol", Family: "GPT", InputMicros: 4_000_000, CachedMicros: 400_000, OutputMicros: 20_000_000},
	{ModelID: "gpt-5.6-terra", DisplayName: "GPT-5.6 Terra", Family: "GPT", InputMicros: 2_000_000, CachedMicros: 200_000, OutputMicros: 12_000_000},
	{ModelID: "gpt-5.6-luna", DisplayName: "GPT-5.6 Luna", Family: "GPT", InputMicros: 200_000, CachedMicros: 20_000, OutputMicros: 1_200_000},
	{ModelID: "gpt-5.5", DisplayName: "GPT-5.5", Family: "GPT", InputMicros: 5_000_000, CachedMicros: 500_000, OutputMicros: 30_000_000},
	{ModelID: "gpt-5.4", DisplayName: "GPT-5.4", Family: "GPT", InputMicros: 2_500_000, CachedMicros: 250_000, OutputMicros: 15_000_000},
	{ModelID: "gpt-5.4-mini", DisplayName: "GPT-5.4 Mini", Family: "GPT", InputMicros: 750_000, CachedMicros: 75_000, OutputMicros: 4_500_000},
	{ModelID: "gpt-5.4-nano", DisplayName: "GPT-5.4 Nano", Family: "GPT", InputMicros: 200_000, CachedMicros: 20_000, OutputMicros: 1_250_000},
	{ModelID: "gpt-5.3-codex", DisplayName: "GPT-5.3 Codex", Family: "GPT", InputMicros: 1_750_000, CachedMicros: 175_000, OutputMicros: 14_000_000},
	{ModelID: "gpt-5.2", DisplayName: "GPT-5.2", Family: "GPT", InputMicros: 1_750_000, CachedMicros: 175_000, OutputMicros: 14_000_000},
	{ModelID: "gpt-5.1", DisplayName: "GPT-5.1", Family: "GPT", InputMicros: 1_250_000, CachedMicros: 125_000, OutputMicros: 10_000_000},
	{ModelID: "gpt-5", DisplayName: "GPT-5", Family: "GPT", InputMicros: 1_250_000, CachedMicros: 125_000, OutputMicros: 10_000_000},
	{ModelID: "gpt-5-mini", DisplayName: "GPT-5 Mini", Family: "GPT", InputMicros: 250_000, CachedMicros: 25_000, OutputMicros: 2_000_000},
	{ModelID: "gpt-5-nano", DisplayName: "GPT-5 Nano", Family: "GPT", InputMicros: 50_000, CachedMicros: 5_000, OutputMicros: 400_000},
	{ModelID: "gpt-4.1", DisplayName: "GPT-4.1", Family: "GPT", InputMicros: 2_000_000, CachedMicros: 500_000, OutputMicros: 8_000_000},
	{ModelID: "gpt-4.1-mini", DisplayName: "GPT-4.1 Mini", Family: "GPT", InputMicros: 400_000, CachedMicros: 100_000, OutputMicros: 1_600_000},
	{ModelID: "gpt-4o", DisplayName: "GPT-4o", Family: "GPT", InputMicros: 2_500_000, CachedMicros: 1_250_000, OutputMicros: 10_000_000},
	{ModelID: "gpt-4o-mini", DisplayName: "GPT-4o Mini", Family: "GPT", InputMicros: 150_000, CachedMicros: 75_000, OutputMicros: 600_000},
	{ModelID: "o4-mini", DisplayName: "o4-mini", Family: "GPT", InputMicros: 1_100_000, CachedMicros: 275_000, OutputMicros: 4_400_000},
	{ModelID: "o3", DisplayName: "o3", Family: "GPT", InputMicros: 2_000_000, CachedMicros: 500_000, OutputMicros: 8_000_000},
	{ModelID: "o3-mini", DisplayName: "o3-mini", Family: "GPT", InputMicros: 1_100_000, CachedMicros: 550_000, OutputMicros: 4_400_000},
	{ModelID: "gpt-oss-120b-medium", DisplayName: "GPT-OSS 120B (Medium)", Family: "GPT", InputMicros: 800_000, CachedMicros: 100_000, OutputMicros: 2_400_000},
}

var agyPricingIndex = buildAGYPricingIndex()

func buildAGYPricingIndex() map[string]agyCatalogEntry {
	index := make(map[string]agyCatalogEntry, len(agyPricingCatalog))
	for _, entry := range agyPricingCatalog {
		index[entry.ModelID] = entry
	}
	return index
}

// ResolveAGYModelID 将原始模型枚举、占位符（MODEL_PLACEHOLDER_M*）或响应模型名规范化为稳定模型标识。
// 未知模型保持原样归一化值，不套用其他模型映射。
func ResolveAGYModelID(rawModel, responseModel string) (canonicalID string, displayName string) {
	raw := strings.TrimSpace(rawModel)
	resp := strings.TrimSpace(responseModel)
	if strings.EqualFold(raw, "unknown") {
		raw = ""
	}
	if strings.EqualFold(resp, "unknown") {
		resp = ""
	}
	if raw != "" {
		if mapped, ok := agyPlaceholderCatalog[strings.ToUpper(raw)]; ok {
			return mapped.ModelID, mapped.DisplayName
		}
		lower := strings.ToLower(raw)
		if entry, ok := agyPricingIndex[lower]; ok {
			return entry.ModelID, entry.DisplayName
		}
	}
	if resp != "" {
		if mapped, ok := agyPlaceholderCatalog[strings.ToUpper(resp)]; ok {
			return mapped.ModelID, mapped.DisplayName
		}
		lowerResp := strings.ToLower(resp)
		if entry, ok := agyPricingIndex[lowerResp]; ok {
			return entry.ModelID, entry.DisplayName
		}
	}
	if raw != "" {
		return raw, ""
	}
	return resp, ""
}

// AGYRateAt 按模型标识与发生时间匹配冻结 API 参考单价；未收录模型返回 false（保持 unpriced）。
func AGYRateAt(modelKey string, atMS int64) (AGYRate, bool) {
	canonical, _ := ResolveAGYModelID(modelKey, "")
	if canonical == "" {
		return AGYRate{}, false
	}
	entry, ok := agyPricingIndex[strings.ToLower(canonical)]
	if !ok {
		return AGYRate{}, false
	}
	rate := AGYRate{
		Version:      AGYPriceVersion,
		Source:       AGYPriceSource,
		ModelID:      entry.ModelID,
		DisplayName:  entry.DisplayName,
		Family:       entry.Family,
		InputMicros:  entry.InputMicros,
		CachedMicros: entry.CachedMicros,
		OutputMicros: entry.OutputMicros,
	}
	if len(entry.Phases) > 0 && atMS > 0 {
		for _, phase := range entry.Phases {
			if phase.FromMS != nil && atMS < *phase.FromMS {
				continue
			}
			if phase.UntilMS != nil && atMS >= *phase.UntilMS {
				continue
			}
			rate.Phase = phase.PhaseTag
			rate.InputMicros = phase.InputMicros
			rate.CachedMicros = phase.CachedMicros
			rate.OutputMicros = phase.OutputMicros
			break
		}
	}
	return rate, true
}

// EstimateAGYCost 按整数微美元计算单步等效 API 费用，全部成分求和后仅做一次 HALF-UP 舍入。
// AGY 的 thinkingOutputTokens 属于 outputTokens 子集，这里只按 outputTokens 计费一次。
func EstimateAGYCost(rate AGYRate, uncachedInput, cacheRead, output int64) (int64, bool) {
	if uncachedInput < 0 || cacheRead < 0 || output < 0 ||
		rate.InputMicros < 0 || rate.CachedMicros < 0 || rate.OutputMicros < 0 {
		return 0, false
	}
	total := new(big.Int)
	for _, pair := range [][2]int64{
		{uncachedInput, rate.InputMicros},
		{cacheRead, rate.CachedMicros},
		{output, rate.OutputMicros},
	} {
		total.Add(total, new(big.Int).Mul(big.NewInt(pair[0]), big.NewInt(pair[1])))
	}
	rounded := RoundMicroUSD(total)
	if !rounded.IsInt64() {
		return 0, false
	}
	return rounded.Int64(), true
}

// AGYReferenceRatesAt 返回指定时刻生效的 Antigravity 模型参考价格目录（按 ModelID 升序）。
func AGYReferenceRatesAt(atMS int64) []AGYReferenceRate {
	items := make([]AGYReferenceRate, 0, len(agyPricingCatalog))
	for _, entry := range agyPricingCatalog {
		if entry.HideInMenu {
			continue
		}
		rate, ok := AGYRateAt(entry.ModelID, atMS)
		if !ok {
			continue
		}
		items = append(items, AGYReferenceRate{
			ModelID:      rate.ModelID,
			DisplayName:  rate.DisplayName,
			Family:       rate.Family,
			InputMicros:  rate.InputMicros,
			CachedMicros: rate.CachedMicros,
			OutputMicros: rate.OutputMicros,
		})
	}
	sort.Slice(items, func(i, j int) bool {
		return items[i].ModelID < items[j].ModelID
	})
	return items
}
