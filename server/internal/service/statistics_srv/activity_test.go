package statistics_srv

import (
	"testing"

	statistics_vo "github.com/SisyphusSQ/codex-pulse/server/internal/models/vo/statistics_vo"
)

func TestStatisticsActivityObservedFacts(t *testing.T) {
	days := []statistics_vo.StatisticsDay{}
	for _, tokens := range []*string{nil, new("0"), new("9007199254740993"), new("9007199254740992"), new("0")} {
		days = append(days, statistics_vo.StatisticsDay{Totals: statistics_vo.StatisticsTotals{TotalTokens: tokens}})
	}
	out := statisticsActivity(days, new("18014398509481985"))
	decimal(t, out.TotalTokens, "18014398509481985")
	decimal(t, out.PeakDailyTokens, "9007199254740993")
	decimal(t, out.ActiveDays, "2")
	decimal(t, out.CurrentStreakDays, "2")
	decimal(t, out.LongestStreakDays, "2")
	if out.ObservedDays != 4 || out.UnknownDays != 1 {
		t.Fatal("unknown day treated as observed zero")
	}
	days[1].Totals.TotalTokens = nil
	out = statisticsActivity(days, new("18014398509481985"))
	if out.CurrentStreakDays != nil {
		t.Fatal("unknown streak boundary treated as an interruption")
	}
	days[4].Totals.TotalTokens = nil
	if statisticsActivity(days, nil).CurrentStreakDays != nil {
		t.Fatal("unknown today treated as inactive")
	}
}

func TestStatisticsActivityUnknownAndZero(t *testing.T) {
	unknown := []statistics_vo.StatisticsDay{{}, {}}
	out := statisticsActivity(unknown, nil)
	if out.TotalTokens != nil || out.PeakDailyTokens != nil || out.ActiveDays != nil || out.CurrentStreakDays != nil || out.LongestStreakDays != nil || out.UnknownDays != 2 {
		t.Fatal("unobserved year returned zero metrics")
	}
	for index := range unknown {
		unknown[index].Totals.TotalTokens = new("0")
	}
	out = statisticsActivity(unknown, new("0"))
	decimal(t, out.PeakDailyTokens, "0")
	decimal(t, out.ActiveDays, "0")
	decimal(t, out.CurrentStreakDays, "0")
	decimal(t, out.LongestStreakDays, "0")
}
