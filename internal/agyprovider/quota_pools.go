package agyprovider

import (
	"math"
	"regexp"
	"sort"
	"strings"

	"github.com/SisyphusSQ/codex-pulse/internal/pricing"
)

const (
	AGYLimitGemini    = "agy.gemini"
	AGYLimitArgon     = "agy.argon"
	AGYLimitCarbon    = "agy.carbon"
	AGYLimitClaudeGPT = "agy.claude_gpt"

	AGYWindow5hMinutes     = int64(300)
	AGYWindowWeeklyMinutes = int64(10080)
)

var (
	thirdPartyPoolRe = regexp.MustCompile(`(?i)\b(?:claude|anthropic|opus|sonnet|haiku|fable|gpt|openai|third[-_\s]?party|partner)\b`)
	fiveHourWinRe    = regexp.MustCompile(`(?i)(?:^|[-_\s])5h(?:$|[-_\s])|five[-_\s]?hour`)
	slugInvalidRe    = regexp.MustCompile(`[^a-z0-9]+`)
)

// NormalizedQuotaWindow 表示归一化后的单个 AGY 配额池窗口事实。
type NormalizedQuotaWindow struct {
	LimitID       string
	LimitName     string
	WindowKind    string // "primary" (5h) | "secondary" (weekly)
	WindowMinutes int64
	UsedPercent   int64
	ResetsAtMS    *int64
}

type normFraction struct {
	remainingFraction float64
	usedPercent       int64
	resetsAtMS        *int64
}

// NormalizeOfficialQuotaPools 统一归一化 Ultra 共享池、Pro 独立 Argon/Carbon 组/桶、
// clientModelConfigs 独立差异推断与 Proto3 零值省略耗尽语义。
func NormalizeOfficialQuotaPools(
	summary LSQuotaSummary,
	clientConfigs []LSClientModelConfig,
	nowMS int64,
) []NormalizedQuotaWindow {
	type poolBuilder struct {
		limitID      string
		limitName    string
		bucket5h     *normFraction
		bucketWeekly *normFraction
		fallback5h   *normFraction
	}

	pools := make(map[string]*poolBuilder)
	ensurePool := func(limitID, limitName string) *poolBuilder {
		if existing := pools[limitID]; existing != nil {
			return existing
		}
		pb := &poolBuilder{limitID: limitID, limitName: limitName}
		pools[limitID] = pb
		return pb
	}

	groups := append([]LSQuotaGroup(nil), summary.Groups...)
	if len(groups) == 0 && len(summary.Buckets) > 0 {
		groups = append(groups, LSQuotaGroup{Buckets: summary.Buckets})
	}

	for _, grp := range groups {
		for _, b := range grp.Buckets {
			norm, ok := normalizeQuotaFraction(b.RemainingFraction, b.Disabled, b.ResetTime, nowMS)
			if !ok {
				continue
			}
			limitID, limitName := classifyQuotaPool(grp.DisplayName, grp.Description, b.BucketID, b.DisplayName)
			win := classifyBucketWindow(b.Window, b.BucketID, b.DisplayName, norm.resetsAtMS, nowMS)
			pb := ensurePool(limitID, limitName)
			if win == "primary" {
				if pb.bucket5h == nil {
					cp := norm
					pb.bucket5h = &cp
				} else if pb.bucketWeekly == nil {
					// 两个无显式标签桶均临近 <6h 重置时，按重置时间早晚区分 5h 与周窗
					if pb.bucket5h.resetsAtMS != nil && norm.resetsAtMS != nil && *norm.resetsAtMS < *pb.bucket5h.resetsAtMS {
						pb.bucketWeekly = pb.bucket5h
						cp := norm
						pb.bucket5h = &cp
					} else {
						cp := norm
						pb.bucketWeekly = &cp
					}
				}
			} else if pb.bucketWeekly == nil {
				cp := norm
				pb.bucketWeekly = &cp
			}
		}
	}

	// 采样 clientModelConfigs 中的 Carbon / Argon 与标准 Gemini 配额
	var sampleCarbon, sampleArgon, sampleStdGemini *normFraction
	for _, cfg := range clientConfigs {
		if cfg.QuotaInfo == nil {
			continue
		}
		norm, ok := normalizeQuotaFraction(cfg.QuotaInfo.RemainingFraction, cfg.QuotaInfo.Disabled, cfg.QuotaInfo.ResetTime, nowMS)
		if !ok {
			continue
		}
		rawKey := ""
		if cfg.ModelOrAlias != nil {
			rawKey = cfg.ModelOrAlias.Model
		}
		canonical, _ := pricing.ResolveAGYModelID(rawKey, cfg.ModelID)
		family := inferModelFamily(rawKey, canonical, cfg.Label)
		if family != "Gemini" {
			continue
		}
		switch {
		case isCarbonIdentifier(rawKey, cfg.ModelID, cfg.Label, canonical):
			if sampleCarbon == nil {
				cp := norm
				sampleCarbon = &cp
			}
		case isArgonIdentifier(rawKey, cfg.ModelID, cfg.Label, canonical):
			if sampleArgon == nil {
				cp := norm
				sampleArgon = &cp
			}
		default:
			if sampleStdGemini == nil {
				cp := norm
				sampleStdGemini = &cp
			}
		}
	}

	// Pro 形态 3 回退检测：clientModelConfigs.quotaInfo 与共享 Gemini 池明显分离时合成独立周窗
	if pools[AGYLimitArgon] == nil && sampleArgon != nil {
		gem := pools[AGYLimitGemini]
		hasBaseline := sampleStdGemini != nil || (gem != nil && (gem.bucket5h != nil || gem.bucketWeekly != nil))
		matches := fractionsMatch(sampleArgon, sampleStdGemini) ||
			(gem != nil && (fractionsMatch(sampleArgon, gem.bucket5h) || fractionsMatch(sampleArgon, gem.bucketWeekly)))
		if hasBaseline && !matches {
			pb := ensurePool(AGYLimitArgon, "Gemini 4 Argon")
			pb.bucketWeekly = sampleArgon
		}
	}
	if pools[AGYLimitCarbon] == nil && sampleCarbon != nil {
		gem := pools[AGYLimitGemini]
		arg := pools[AGYLimitArgon]
		hasBaseline := sampleStdGemini != nil || sampleArgon != nil ||
			(gem != nil && (gem.bucket5h != nil || gem.bucketWeekly != nil)) ||
			(arg != nil && arg.bucketWeekly != nil)
		matches := fractionsMatch(sampleCarbon, sampleStdGemini) ||
			fractionsMatch(sampleCarbon, sampleArgon) ||
			(gem != nil && (fractionsMatch(sampleCarbon, gem.bucket5h) || fractionsMatch(sampleCarbon, gem.bucketWeekly))) ||
			(arg != nil && fractionsMatch(sampleCarbon, arg.bucketWeekly))
		if hasBaseline && !matches {
			pb := ensurePool(AGYLimitCarbon, "Gemini 4 Carbon")
			pb.bucketWeekly = sampleCarbon
		}
	}

	// 回退 clientModelConfigs 填充缺失池（如仅 GetUserStatus 有数据时）
	hasArgon := pools[AGYLimitArgon] != nil
	hasCarbon := pools[AGYLimitCarbon] != nil
	for _, cfg := range clientConfigs {
		if cfg.QuotaInfo == nil {
			continue
		}
		norm, ok := normalizeQuotaFraction(cfg.QuotaInfo.RemainingFraction, cfg.QuotaInfo.Disabled, cfg.QuotaInfo.ResetTime, nowMS)
		if !ok {
			continue
		}
		rawKey := ""
		if cfg.ModelOrAlias != nil {
			rawKey = cfg.ModelOrAlias.Model
		}
		canonical, _ := pricing.ResolveAGYModelID(rawKey, cfg.ModelID)
		var limitID, limitName string
		switch {
		case isCarbonIdentifier(rawKey, cfg.ModelID, cfg.Label, canonical) && hasCarbon:
			limitID, limitName = AGYLimitCarbon, "Gemini 4 Carbon"
		case isArgonIdentifier(rawKey, cfg.ModelID, cfg.Label, canonical) && hasArgon:
			limitID, limitName = AGYLimitArgon, "Gemini 4 Argon"
		case inferModelFamily(rawKey, canonical, cfg.Label) == "Gemini":
			limitID, limitName = AGYLimitGemini, "Gemini"
		default:
			limitID, limitName = AGYLimitClaudeGPT, "Claude & GPT"
		}
		pb := ensurePool(limitID, limitName)
		if pb.bucket5h == nil && pb.bucketWeekly == nil && pb.fallback5h == nil {
			isWeekly := limitID == AGYLimitArgon || limitID == AGYLimitCarbon ||
				(norm.resetsAtMS != nil && *norm.resetsAtMS > nowMS+6*3600*1000)
			cp := norm
			if isWeekly {
				pb.bucketWeekly = &cp
			} else {
				pb.fallback5h = &cp
			}
		}
	}

	// 按固定顺序输出所有已知窗口
	orderedIDs := make([]string, 0, len(pools))
	for id := range pools {
		orderedIDs = append(orderedIDs, id)
	}
	sort.Slice(orderedIDs, func(i, j int) bool {
		ri, rj := agyPoolOrderRank(orderedIDs[i]), agyPoolOrderRank(orderedIDs[j])
		if ri != rj {
			return ri < rj
		}
		return orderedIDs[i] < orderedIDs[j]
	})

	var windows []NormalizedQuotaWindow
	for _, id := range orderedIDs {
		pb := pools[id]
		primary := pb.bucket5h
		if primary == nil {
			primary = pb.fallback5h
		}
		if primary != nil {
			windows = append(windows, NormalizedQuotaWindow{
				LimitID:       pb.limitID,
				LimitName:     pb.limitName,
				WindowKind:    "primary",
				WindowMinutes: AGYWindow5hMinutes,
				UsedPercent:   primary.usedPercent,
				ResetsAtMS:    cloneInt64Ptr(primary.resetsAtMS),
			})
		}
		if pb.bucketWeekly != nil {
			windows = append(windows, NormalizedQuotaWindow{
				LimitID:       pb.limitID,
				LimitName:     pb.limitName,
				WindowKind:    "secondary",
				WindowMinutes: AGYWindowWeeklyMinutes,
				UsedPercent:   pb.bucketWeekly.usedPercent,
				ResetsAtMS:    cloneInt64Ptr(pb.bucketWeekly.resetsAtMS),
			})
		}
	}
	return windows
}

func normalizeQuotaFraction(
	remainingFraction *float64,
	disabled bool,
	resetTime string,
	nowMS int64,
) (normFraction, bool) {
	var resetsAtMS *int64
	if ns := parseISONS(resetTime); ns > 0 {
		ms := ns / 1_000_000
		resetsAtMS = &ms
	}
	var rem float64
	switch {
	case disabled:
		rem = 0
	case remainingFraction != nil && !math.IsNaN(*remainingFraction) && !math.IsInf(*remainingFraction, 0):
		rem = math.Max(0, math.Min(1, *remainingFraction))
	case resetsAtMS != nil && *resetsAtMS > nowMS:
		// Proto3 零值省略：仅当 resetTime > now 时才判为已耗尽（0% 剩余 / 100% 已用）
		rem = 0
	default:
		return normFraction{}, false
	}
	usedPct := int64(math.Round((1 - rem) * 100))
	if usedPct < 0 {
		usedPct = 0
	} else if usedPct > 100 {
		usedPct = 100
	}
	return normFraction{
		remainingFraction: rem,
		usedPercent:       usedPct,
		resetsAtMS:        resetsAtMS,
	}, true
}

func classifyBucketWindow(windowField, bucketID, bucketName string, resetsAtMS *int64, nowMS int64) string {
	w := strings.ToLower(strings.TrimSpace(windowField))
	idAndName := strings.ToLower(bucketID + " " + bucketName)
	if w == "5h" || fiveHourWinRe.MatchString(w) || fiveHourWinRe.MatchString(idAndName) {
		return "primary"
	}
	if w == "weekly" || w == "7d" || strings.Contains(w, "week") || strings.Contains(idAndName, "week") {
		return "secondary"
	}
	if resetsAtMS != nil && *resetsAtMS > 0 && *resetsAtMS <= nowMS+6*3600*1000 {
		return "primary"
	}
	return "secondary"
}

func classifyQuotaPool(groupName, groupDesc, bucketID, bucketName string) (string, string) {
	primaryText := bucketID + " " + bucketName + " " + groupName
	lowerPrimary := strings.ToLower(primaryText)
	lowerDesc := strings.ToLower(groupDesc)
	lowerGroup := strings.ToLower(strings.TrimSpace(groupName))

	if strings.Contains(lowerPrimary, "carbon") ||
		(strings.Contains(lowerDesc, "carbon") && !strings.Contains(lowerDesc, "flash") && lowerGroup != "gemini models") {
		return AGYLimitCarbon, "Gemini 4 Carbon"
	}
	if strings.Contains(lowerPrimary, "argon") ||
		(strings.Contains(lowerDesc, "argon") && !strings.Contains(lowerDesc, "flash") && lowerGroup != "gemini models") {
		return AGYLimitArgon, "Gemini 4 Argon"
	}
	if thirdPartyPoolRe.MatchString(primaryText) ||
		(!strings.Contains(lowerPrimary, "gemini") && thirdPartyPoolRe.MatchString(groupDesc)) {
		return AGYLimitClaudeGPT, "Claude & GPT"
	}
	if strings.Contains(lowerPrimary+" "+lowerDesc, "gemini") {
		return AGYLimitGemini, "Gemini"
	}
	label := sanitizeLabel(firstNonEmpty(groupName, bucketID), "Other")
	slug := strings.Trim(slugInvalidRe.ReplaceAllString(strings.ToLower(label), "_"), "_")
	if slug == "" {
		slug = "other"
	}
	return "agy.pool." + slug, label
}

func fractionsMatch(a, b *normFraction) bool {
	if a == nil || b == nil {
		return false
	}
	if math.Abs(a.remainingFraction-b.remainingFraction) > 0.015 {
		return false
	}
	if a.resetsAtMS != nil && b.resetsAtMS != nil {
		diff := *a.resetsAtMS - *b.resetsAtMS
		if diff < 0 {
			diff = -diff
		}
		if diff > 120_000 {
			return false
		}
	}
	return true
}

func isCarbonIdentifier(parts ...string) bool {
	text := strings.ToLower(strings.Join(parts, " "))
	return strings.Contains(text, "carbon") ||
		strings.Contains(text, "model_placeholder_m427") ||
		strings.Contains(text, "model_placeholder_m429") ||
		strings.Contains(text, "model_placeholder_m437") ||
		strings.Contains(text, "model_placeholder_m438") ||
		strings.Contains(text, "model_placeholder_m667")
}

func isArgonIdentifier(parts ...string) bool {
	text := strings.ToLower(strings.Join(parts, " "))
	return strings.Contains(text, "argon") ||
		strings.Contains(text, "model_placeholder_m417") ||
		strings.Contains(text, "model_placeholder_m418") ||
		strings.Contains(text, "model_placeholder_m419") ||
		strings.Contains(text, "model_placeholder_m681")
}

func inferModelFamily(parts ...string) string {
	text := strings.ToUpper(strings.Join(parts, " "))
	if strings.Contains(text, "CLAUDE") || strings.Contains(text, "ANTHROPIC") ||
		strings.Contains(text, "OPUS") || strings.Contains(text, "SONNET") ||
		strings.Contains(text, "HAIKU") || strings.Contains(text, "FABLE") {
		return "Claude"
	}
	if strings.Contains(text, "GPT") || strings.Contains(text, "OPENAI") {
		return "GPT"
	}
	if strings.Contains(text, "GEMINI") || strings.Contains(text, "CARBON") ||
		strings.Contains(text, "ARGON") || strings.Contains(text, "NEON") ||
		strings.Contains(text, "HYDROGEN") || strings.Contains(text, "HELIX") ||
		strings.Contains(text, "FLASH") || strings.Contains(text, "ORCHESTRATOR") {
		return "Gemini"
	}
	return "Other"
}

func agyPoolOrderRank(limitID string) int {
	switch limitID {
	case AGYLimitGemini:
		return 0
	case AGYLimitArgon:
		return 1
	case AGYLimitCarbon:
		return 2
	case AGYLimitClaudeGPT:
		return 3
	default:
		return 4
	}
}

func cloneInt64Ptr(v *int64) *int64 {
	if v == nil {
		return nil
	}
	cp := *v
	return &cp
}
