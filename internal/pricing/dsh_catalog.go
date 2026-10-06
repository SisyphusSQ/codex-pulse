package pricing

import (
	"math/big"
	"strings"
	"time"
)

const DSHPriceSource = "https://api-docs.deepseek.com/quick_start/pricing/"
const DSHPriceVersion = "deepseek-usd-2026-10-06"

// DSHRate 保存每百万 Token 的整数微美元费率及命中的历史时段。
type DSHRate struct {
	Version          string
	Period           string
	InputMicros      int64
	CachedMicros     int64
	CacheWriteMicros int64
	OutputMicros     int64
}

var dshFlashEffective = time.Date(2026, 9, 10, 4, 0, 0, 0, time.UTC).UnixMilli()

// 从已核验的现行组合规则开始；更早的周末/假期变更缺少完整证据，不回填。
var dshCalendarEffective = time.Date(2026, 9, 10, 4, 0, 0, 0, time.UTC).UnixMilli()
var dshBeijing = time.FixedZone("Asia/Shanghai", 8*60*60)

// 2026 国务院假期通知（包含调休休息日）；周末补班仍按官方周末谷价。
// https://www.beijing.gov.cn/cs/gncs/zcwj/202603/t20260327_4568275.html
var dshHolidays2026 = [][2]string{
	{"2026-01-01", "2026-01-03"}, {"2026-02-15", "2026-02-23"},
	{"2026-04-04", "2026-04-06"}, {"2026-05-01", "2026-05-05"},
	{"2026-06-19", "2026-06-21"}, {"2026-09-25", "2026-09-27"},
	{"2026-10-01", "2026-10-07"},
}

// DSHBillingPeriod 按北京时间官方时段判断；未收录的年份在可能峰时拒绝猜价。
func DSHBillingPeriod(atMS int64) (string, bool) {
	if atMS < dshCalendarEffective {
		return "", false
	}
	t := time.UnixMilli(atMS).In(dshBeijing)
	peakHour := (t.Hour() >= 9 && t.Hour() < 12) || (t.Hour() >= 14 && t.Hour() < 18)
	if !peakHour || t.Weekday() == time.Saturday || t.Weekday() == time.Sunday {
		return "off_peak", true
	}
	if t.Year() != 2026 {
		return "", false
	}
	date := t.Format("2006-01-02")
	for _, holiday := range dshHolidays2026 {
		if date >= holiday[0] && date <= holiday[1] {
			return "off_peak", true
		}
	}
	return "peak", true
}

// DSHRateAt 只给官方 DeepSeek 路由定价，不把第三方同名模型按官方价格计费。
// Flash 新价生效时间取自 news260910；Pro 沿用现行独立费率，不执行已撤回的重定向。
func DSHRateAt(provider, model string, atMS int64) (DSHRate, bool) {
	switch strings.ToLower(provider) {
	case "deepseek", "deepseek-official", "deepseek-account":
	default:
		return DSHRate{}, false
	}
	period, ok := DSHBillingPeriod(atMS)
	if !ok {
		return DSHRate{}, false
	}
	rate := DSHRate{Version: DSHPriceVersion, Period: period}
	switch strings.ToLower(model) {
	case "deepseek-flash", "deepseek-v4-flash", "deepseek-v4-flash-vision-exp":
		if atMS < dshFlashEffective {
			return DSHRate{}, false
		}
		rate.InputMicros, rate.CachedMicros, rate.OutputMicros = 150_000, 3_000, 600_000
	case "deepseek-v4-pro":
		rate.InputMicros, rate.CachedMicros, rate.OutputMicros = 660_000, 22_000, 1_980_000
	default:
		return DSHRate{}, false
	}
	if period == "peak" {
		rate.InputMicros *= 2
		rate.CachedMicros *= 2
		rate.OutputMicros *= 2
	}
	return rate, true
}

// EstimateDSHCost 对互斥的未缓存、缓存读、缓存写、输出计数统一舍入一次。
func EstimateDSHCost(rate DSHRate, input, cached, written, output int64) (int64, bool) {
	if input < 0 || cached < 0 || written < 0 || output < 0 || rate.InputMicros < 0 || rate.CachedMicros < 0 || rate.OutputMicros < 0 || (written > 0 && rate.CacheWriteMicros == 0) {
		return 0, false
	}
	total := new(big.Int)
	for _, pair := range [][2]int64{{input, rate.InputMicros}, {cached, rate.CachedMicros}, {written, rate.CacheWriteMicros}, {output, rate.OutputMicros}} {
		total.Add(total, new(big.Int).Mul(big.NewInt(pair[0]), big.NewInt(pair[1])))
	}
	total.Add(total, big.NewInt(500_000))
	total.Quo(total, big.NewInt(1_000_000))
	if !total.IsInt64() {
		return 0, false
	}
	return total.Int64(), true
}

var DSHPricingVerifiedAtMS = time.Date(2026, 10, 6, 0, 0, 0, 0, time.UTC).UnixMilli()

type DSHReferenceRate struct {
	ModelID                                 string
	InputMicros, CachedMicros, OutputMicros int64
}

func DSHReferenceRates() []DSHReferenceRate {
	return []DSHReferenceRate{
		{"deepseek-flash / 谷时", 150000, 3000, 600000}, {"deepseek-flash / 峰时", 300000, 6000, 1200000},
		{"deepseek-v4-pro / 谷时", 660000, 22000, 1980000}, {"deepseek-v4-pro / 峰时", 1320000, 44000, 3960000},
	}
}
