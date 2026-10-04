package statistics_srv

import (
	"math/big"
	"strconv"

	statistics_vo "github.com/SisyphusSQ/codex-pulse/server/internal/models/vo/statistics_vo"
)

func statisticsActivity(days []statistics_vo.StatisticsDay, total *string) statistics_vo.StatisticsActivity {
	out := statistics_vo.StatisticsActivity{TotalTokens: total}
	var peak *big.Int
	active, running, longest := 0, 0, 0
	for _, day := range days {
		if day.Totals.TotalTokens == nil {
			out.UnknownDays++
			running = 0
			continue
		}
		out.ObservedDays++
		n, _ := new(big.Int).SetString(*day.Totals.TotalTokens, 10)
		if peak == nil || n.Cmp(peak) > 0 {
			peak = n
		}
		if n.Sign() > 0 {
			active++
			running++
			longest = max(longest, running)
		} else {
			running = 0
		}
	}
	if peak == nil {
		return out
	}
	out.PeakDailyTokens = new(peak.String())
	out.ActiveDays = new(strconv.Itoa(active))
	out.LongestStreakDays = new(strconv.Itoa(longest))
	// 当前连续天数只统计中心已收到的活动事实；没有活动证据的日期不连接连续段。
	// 今天尚无活动时允许截至昨天，不要求连续段之前的日期具有明确零用量。
	index := len(days) - 1
	if index >= 0 && (days[index].Totals.TotalTokens == nil || *days[index].Totals.TotalTokens == "0") {
		index--
	}
	current := 0
	for index >= 0 {
		value := days[index].Totals.TotalTokens
		if value == nil || *value == "0" {
			break
		}
		current++
		index--
	}
	out.CurrentStreakDays = new(strconv.Itoa(current))
	return out
}
