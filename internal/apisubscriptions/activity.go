package apisubscriptions

import (
	"context"
	"sort"
	"time"
)

const apiSubscriptionActivityDayCount = 365

func subscriptionActivityCalendar(
	ctx context.Context,
	balanceHistory BalanceHistory,
	quotaHistory QuotaHistory,
	deepSeekEpoch string,
	openCodeGoEpoch string,
	evaluatedAtMS int64,
	location *time.Location,
) (APISubscriptionActivityCalendar, error) {
	if balanceHistory == nil || quotaHistory == nil || location == nil || evaluatedAtMS < 0 {
		return APISubscriptionActivityCalendar{}, ErrProtocol
	}
	evaluated := time.UnixMilli(evaluatedAtMS).In(location)
	today := time.Date(evaluated.Year(), evaluated.Month(), evaluated.Day(), 0, 0, 0, 0, location)
	firstDay := today.AddDate(0, 0, -(apiSubscriptionActivityDayCount - 1))

	balanceByDay := make(map[string][]BalanceObservation)
	if deepSeekEpoch != "" {
		observations, err := balanceHistory.Observations(
			ctx, deepSeekEpoch, firstDay.UnixMilli(), evaluatedAtMS,
		)
		if err != nil {
			return APISubscriptionActivityCalendar{}, err
		}
		for _, observation := range observations {
			key := observationDateKey(observation.ObservedAtMS, location)
			balanceByDay[key] = append(balanceByDay[key], observation)
		}
	}

	quotaByDay := make(map[string][]QuotaObservation)
	if openCodeGoEpoch != "" {
		observations, err := quotaHistory.QuotaObservations(
			ctx, openCodeGoEpoch, firstDay.UnixMilli(), evaluatedAtMS,
		)
		if err != nil {
			return APISubscriptionActivityCalendar{}, err
		}
		for _, observation := range observations {
			key := observationDateKey(observation.ObservedAtMS, location)
			quotaByDay[key] = append(quotaByDay[key], observation)
		}
	}

	days := make([]APISubscriptionActivityDay, 0, apiSubscriptionActivityDayCount)
	for offset := 0; offset < apiSubscriptionActivityDayCount; offset++ {
		dayStart := firstDay.AddDate(0, 0, offset)
		key := dayStart.Format("2006-01-02")
		day := APISubscriptionActivityDay{
			DateKey: key, StartsAtMS: dayStart.UnixMilli(), DeepSeek: make([]DeepSeekDailyActivity, 0),
		}
		if observations := balanceByDay[key]; len(observations) > 0 {
			activities, err := observedBalanceActivities(observations)
			if err != nil {
				return APISubscriptionActivityCalendar{}, err
			}
			currencies := make([]string, 0, len(activities))
			for currency := range activities {
				currencies = append(currencies, currency)
			}
			sort.Strings(currencies)
			for _, currency := range currencies {
				activity := activities[currency]
				day.DeepSeek = append(day.DeepSeek, DeepSeekDailyActivity{
					Currency: currency, TotalRecharged: activity.TotalRecharged,
					TotalConsumed: activity.TotalConsumed, SampleCount: activity.SampleCount,
				})
			}
		}
		day.OpenCodeGo = openCodeGoDailyActivity(quotaByDay[key])
		days = append(days, day)
	}
	return APISubscriptionActivityCalendar{ReportingTimeZone: location.String(), Days: days}, nil
}

func observationDateKey(observedAtMS int64, location *time.Location) string {
	return time.UnixMilli(observedAtMS).In(location).Format("2006-01-02")
}

func openCodeGoDailyActivity(observations []QuotaObservation) *OpenCodeGoFiveHourDailyActivity {
	var result *OpenCodeGoFiveHourDailyActivity
	for _, observation := range observations {
		for _, window := range observation.Quota.Windows {
			if window.Kind != WindowFiveHour {
				continue
			}
			if result == nil {
				result = &OpenCodeGoFiveHourDailyActivity{}
			}
			result.SampleCount++
			result.MaxFiveHourUsedPercent = max(result.MaxFiveHourUsedPercent, window.UsedPercent)
			result.LatestFiveHourUsedPercent = window.UsedPercent
			result.LatestFiveHourRemainingPercent = window.RemainingPercent
		}
	}
	return result
}
