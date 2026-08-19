package apisubscriptions

import (
	"context"
	"math/big"
	"sort"
	"strings"
	"sync"
	"time"
)

type BalanceObservation struct {
	CredentialEpoch string
	ObservedAtMS    int64
	Balance         Balance
}

type BalanceHistory interface {
	Record(context.Context, BalanceObservation) error
	Observations(context.Context, string, int64, int64) ([]BalanceObservation, error)
}

type QuotaObservation struct {
	CredentialEpoch string
	ObservedAtMS    int64
	Quota           Quota
}

type QuotaHistory interface {
	RecordQuota(context.Context, QuotaObservation) error
	QuotaObservations(context.Context, string, int64, int64) ([]QuotaObservation, error)
}

type latestBalanceHistory interface {
	Latest(context.Context, string, int64) (BalanceObservation, bool, error)
}

type memoryBalanceHistory struct {
	mu           sync.RWMutex
	observations map[string][]BalanceObservation
	quotas       map[string][]QuotaObservation
}

func newMemoryBalanceHistory() *memoryBalanceHistory {
	return &memoryBalanceHistory{
		observations: make(map[string][]BalanceObservation), quotas: make(map[string][]QuotaObservation),
	}
}

func (history *memoryBalanceHistory) RecordQuota(_ context.Context, observation QuotaObservation) error {
	if history == nil || !validQuotaObservation(observation) {
		return ErrProtocol
	}
	history.mu.Lock()
	defer history.mu.Unlock()
	rows := history.quotas[observation.CredentialEpoch]
	for index := range rows {
		if rows[index].ObservedAtMS == observation.ObservedAtMS {
			if equalQuota(rows[index].Quota, observation.Quota) {
				return nil
			}
			return ErrProtocol
		}
	}
	rows = append(rows, cloneQuotaObservation(observation))
	sort.Slice(rows, func(left, right int) bool { return rows[left].ObservedAtMS < rows[right].ObservedAtMS })
	history.quotas[observation.CredentialEpoch] = rows
	return nil
}

func (history *memoryBalanceHistory) QuotaObservations(
	_ context.Context,
	credentialEpoch string,
	startsAtMS int64,
	endsAtMS int64,
) ([]QuotaObservation, error) {
	if history == nil || !validCredentialEpoch(credentialEpoch) || startsAtMS < 0 || endsAtMS < startsAtMS {
		return nil, ErrProtocol
	}
	history.mu.RLock()
	defer history.mu.RUnlock()
	result := make([]QuotaObservation, 0)
	for _, observation := range history.quotas[credentialEpoch] {
		if observation.ObservedAtMS < startsAtMS {
			continue
		}
		if observation.ObservedAtMS > endsAtMS {
			break
		}
		result = append(result, cloneQuotaObservation(observation))
	}
	return result, nil
}

func (history *memoryBalanceHistory) Record(_ context.Context, observation BalanceObservation) error {
	if history == nil || observation.CredentialEpoch == "" || observation.ObservedAtMS < 0 {
		return ErrProtocol
	}
	history.mu.Lock()
	defer history.mu.Unlock()
	rows := history.observations[observation.CredentialEpoch]
	for index := range rows {
		if rows[index].ObservedAtMS == observation.ObservedAtMS {
			rows[index] = cloneBalanceObservation(observation)
			history.observations[observation.CredentialEpoch] = rows
			return nil
		}
	}
	rows = append(rows, cloneBalanceObservation(observation))
	sort.Slice(rows, func(left, right int) bool {
		return rows[left].ObservedAtMS < rows[right].ObservedAtMS
	})
	history.observations[observation.CredentialEpoch] = rows
	return nil
}

func (history *memoryBalanceHistory) Observations(
	_ context.Context,
	credentialEpoch string,
	startsAtMS int64,
	endsAtMS int64,
) ([]BalanceObservation, error) {
	if history == nil || credentialEpoch == "" || startsAtMS < 0 || endsAtMS < startsAtMS {
		return nil, ErrProtocol
	}
	history.mu.RLock()
	defer history.mu.RUnlock()
	result := make([]BalanceObservation, 0)
	for _, observation := range history.observations[credentialEpoch] {
		if observation.ObservedAtMS < startsAtMS {
			continue
		}
		if observation.ObservedAtMS > endsAtMS {
			break
		}
		result = append(result, cloneBalanceObservation(observation))
	}
	return result, nil
}

func (history *memoryBalanceHistory) Latest(
	_ context.Context,
	credentialEpoch string,
	endsAtMS int64,
) (BalanceObservation, bool, error) {
	if history == nil || credentialEpoch == "" || endsAtMS < 0 {
		return BalanceObservation{}, false, ErrProtocol
	}
	history.mu.RLock()
	defer history.mu.RUnlock()
	rows := history.observations[credentialEpoch]
	for index := len(rows) - 1; index >= 0; index-- {
		if rows[index].ObservedAtMS <= endsAtMS {
			return cloneBalanceObservation(rows[index]), true, nil
		}
	}
	return BalanceObservation{}, false, nil
}

func balancePeriods(
	ctx context.Context,
	history BalanceHistory,
	credentialEpoch string,
	current BalanceObservation,
	location *time.Location,
) ([]BalancePeriod, error) {
	return balancePeriodsAt(ctx, history, credentialEpoch, current, current.ObservedAtMS, location)
}

func balancePeriodsAt(
	ctx context.Context,
	history BalanceHistory,
	credentialEpoch string,
	current BalanceObservation,
	periodAtMS int64,
	location *time.Location,
) ([]BalancePeriod, error) {
	if history == nil || location == nil {
		return nil, ErrProtocol
	}
	periods := naturalPeriods(periodAtMS, location)
	result := make([]BalancePeriod, 0, len(periods))
	for _, period := range periods {
		if period.StartsAtMS > current.ObservedAtMS {
			result = append(result, BalancePeriod{
				Kind: period.Kind, StartsAtMS: period.StartsAtMS,
				Changes: make([]CurrencyBalanceChange, 0), Series: make([]CurrencyBalanceSeries, 0),
			})
			continue
		}
		observations, err := history.Observations(
			ctx, credentialEpoch, period.StartsAtMS, current.ObservedAtMS,
		)
		if err != nil {
			return nil, err
		}
		item := BalancePeriod{
			Kind: period.Kind, StartsAtMS: period.StartsAtMS,
			Changes: make([]CurrencyBalanceChange, 0), Series: balanceSeries(observations),
		}
		if len(observations) > 0 {
			baseline := observations[0]
			item.BaselineAtMS = int64Pointer(baseline.ObservedAtMS)
			changes, err := compareBalances(baseline.Balance, current.Balance)
			if err != nil {
				return nil, err
			}
			activities, err := observedBalanceActivities(observations)
			if err != nil {
				return nil, err
			}
			for index := range changes {
				activity := activities[changes[index].Currency]
				changes[index].TotalRecharged = activity.TotalRecharged
				changes[index].TotalConsumed = activity.TotalConsumed
			}
			item.Changes = changes
		}
		result = append(result, item)
	}
	return result, nil
}

func balanceSeries(observations []BalanceObservation) []CurrencyBalanceSeries {
	series := make([]CurrencyBalanceSeries, 0)
	indexByCurrency := make(map[string]int)
	for _, observation := range observations {
		for _, balance := range observation.Balance.Balances {
			index, ok := indexByCurrency[balance.Currency]
			if !ok {
				index = len(series)
				indexByCurrency[balance.Currency] = index
				series = append(series, CurrencyBalanceSeries{
					Currency: balance.Currency,
					Points:   make([]BalanceTrendPoint, 0, len(observations)),
				})
			}
			series[index].Points = append(series[index].Points, BalanceTrendPoint{
				ObservedAtMS: observation.ObservedAtMS,
				Total:        balance.Total, Granted: balance.Granted, ToppedUp: balance.ToppedUp,
			})
		}
	}
	for index := range series {
		series[index].Points = compressBalanceTrendPoints(series[index].Points)
	}
	return series
}

func compressBalanceTrendPoints(points []BalanceTrendPoint) []BalanceTrendPoint {
	if len(points) <= 1 {
		return append([]BalanceTrendPoint(nil), points...)
	}
	result := make([]BalanceTrendPoint, 0, len(points))
	result = append(result, points[0])
	previous := points[0]
	for _, point := range points[1 : len(points)-1] {
		if sameBalanceTrendValue(previous, point) {
			continue
		}
		result = append(result, point)
		previous = point
	}
	result = append(result, points[len(points)-1])
	return result
}

func sameBalanceTrendValue(left BalanceTrendPoint, right BalanceTrendPoint) bool {
	return left.Total == right.Total && left.Granted == right.Granted && left.ToppedUp == right.ToppedUp
}

func naturalPeriods(observedAtMS int64, location *time.Location) []BalancePeriod {
	observed := time.UnixMilli(observedAtMS).In(location)
	today := time.Date(observed.Year(), observed.Month(), observed.Day(), 0, 0, 0, 0, location)
	weekdayOffset := (int(today.Weekday()) + 6) % 7
	week := today.AddDate(0, 0, -weekdayOffset)
	month := time.Date(observed.Year(), observed.Month(), 1, 0, 0, 0, 0, location)
	return []BalancePeriod{
		{Kind: PeriodToday, StartsAtMS: today.UnixMilli()},
		{Kind: PeriodWeek, StartsAtMS: week.UnixMilli()},
		{Kind: PeriodMonth, StartsAtMS: month.UnixMilli()},
	}
}

func compareBalances(starting Balance, current Balance) ([]CurrencyBalanceChange, error) {
	byCurrency := make(map[string]CurrencyBalance, len(starting.Balances))
	for _, balance := range starting.Balances {
		byCurrency[balance.Currency] = balance
	}
	changes := make([]CurrencyBalanceChange, 0, len(current.Balances))
	for _, balance := range current.Balances {
		baseline, ok := byCurrency[balance.Currency]
		if !ok {
			continue
		}
		totalDelta, err := subtractDecimal(balance.Total, baseline.Total)
		if err != nil {
			return nil, err
		}
		grantedDelta, err := subtractDecimal(balance.Granted, baseline.Granted)
		if err != nil {
			return nil, err
		}
		toppedUpDelta, err := subtractDecimal(balance.ToppedUp, baseline.ToppedUp)
		if err != nil {
			return nil, err
		}
		changes = append(changes, CurrencyBalanceChange{
			Currency:      balance.Currency,
			StartingTotal: baseline.Total, TotalDelta: totalDelta,
			StartingGranted: baseline.Granted, GrantedDelta: grantedDelta,
			StartingToppedUp: baseline.ToppedUp, ToppedUpDelta: toppedUpDelta,
			TotalRecharged: "0", TotalConsumed: "0",
		})
	}
	return changes, nil
}

type observedBalanceActivity struct {
	TotalRecharged string
	TotalConsumed  string
	SampleCount    int
}

func observedBalanceActivities(observations []BalanceObservation) (map[string]observedBalanceActivity, error) {
	result := make(map[string]observedBalanceActivity)
	previous := make(map[string]CurrencyBalance)
	for _, observation := range observations {
		current := make(map[string]CurrencyBalance, len(observation.Balance.Balances))
		for _, balance := range observation.Balance.Balances {
			current[balance.Currency] = balance
			activity := result[balance.Currency]
			activity.SampleCount++
			if activity.TotalRecharged == "" {
				activity.TotalRecharged = zeroWithScale(balance.ToppedUp)
			}
			if activity.TotalConsumed == "" {
				activity.TotalConsumed = zeroWithScale(balance.Total)
			}
			if prior, ok := previous[balance.Currency]; ok {
				toppedUpDelta, err := subtractDecimal(balance.ToppedUp, prior.ToppedUp)
				if err != nil {
					return nil, err
				}
				grantedDelta, err := subtractDecimal(balance.Granted, prior.Granted)
				if err != nil {
					return nil, err
				}
				if decimalSign(toppedUpDelta) > 0 {
					activity.TotalRecharged, err = addDecimal(activity.TotalRecharged, toppedUpDelta)
					if err != nil {
						return nil, err
					}
				} else if decimalSign(toppedUpDelta) < 0 {
					activity.TotalConsumed, err = addDecimal(activity.TotalConsumed, absoluteDecimal(toppedUpDelta))
					if err != nil {
						return nil, err
					}
				}
				if decimalSign(grantedDelta) < 0 {
					activity.TotalConsumed, err = addDecimal(activity.TotalConsumed, absoluteDecimal(grantedDelta))
					if err != nil {
						return nil, err
					}
				}
			}
			result[balance.Currency] = activity
		}
		previous = current
	}
	return result, nil
}

func addDecimal(left string, right string) (string, error) {
	leftInteger, leftScale, err := parseDecimal(left)
	if err != nil {
		return "", err
	}
	rightInteger, rightScale, err := parseDecimal(right)
	if err != nil {
		return "", err
	}
	scale := max(leftScale, rightScale)
	leftInteger.Mul(leftInteger, powerOfTen(scale-leftScale))
	rightInteger.Mul(rightInteger, powerOfTen(scale-rightScale))
	return formatDecimal(new(big.Int).Add(leftInteger, rightInteger), scale), nil
}

func decimalSign(value string) int {
	if strings.HasPrefix(value, "-") {
		if integer, _, err := parseDecimal(strings.TrimPrefix(value, "-")); err == nil && integer.Sign() > 0 {
			return -1
		}
		return 0
	}
	integer, _, err := parseDecimal(value)
	if err != nil {
		return 0
	}
	return integer.Sign()
}

func absoluteDecimal(value string) string {
	return strings.TrimPrefix(value, "-")
}

func zeroWithScale(value string) string {
	parts := strings.SplitN(value, ".", 2)
	if len(parts) == 1 {
		return "0"
	}
	return "0." + strings.Repeat("0", len(parts[1]))
}

func subtractDecimal(current string, starting string) (string, error) {
	currentInteger, currentScale, err := parseDecimal(current)
	if err != nil {
		return "", err
	}
	startingInteger, startingScale, err := parseDecimal(starting)
	if err != nil {
		return "", err
	}
	scale := max(currentScale, startingScale)
	currentInteger.Mul(currentInteger, powerOfTen(scale-currentScale))
	startingInteger.Mul(startingInteger, powerOfTen(scale-startingScale))
	delta := new(big.Int).Sub(currentInteger, startingInteger)
	return formatDecimal(delta, scale), nil
}

func parseDecimal(value string) (*big.Int, int, error) {
	if !validMoney(value) {
		return nil, 0, ErrProtocol
	}
	parts := strings.SplitN(value, ".", 2)
	scale := 0
	digits := parts[0]
	if len(parts) == 2 {
		scale = len(parts[1])
		digits += parts[1]
	}
	integer, ok := new(big.Int).SetString(digits, 10)
	if !ok {
		return nil, 0, ErrProtocol
	}
	return integer, scale, nil
}

func powerOfTen(exponent int) *big.Int {
	return new(big.Int).Exp(big.NewInt(10), big.NewInt(int64(exponent)), nil)
}

func formatDecimal(value *big.Int, scale int) string {
	if scale == 0 {
		return value.String()
	}
	sign := ""
	abs := new(big.Int).Set(value)
	if abs.Sign() < 0 {
		sign = "-"
		abs.Abs(abs)
	}
	digits := abs.String()
	if len(digits) <= scale {
		digits = strings.Repeat("0", scale-len(digits)+1) + digits
	}
	boundary := len(digits) - scale
	return sign + digits[:boundary] + "." + digits[boundary:]
}

func cloneBalanceObservation(value BalanceObservation) BalanceObservation {
	value.Balance = cloneBalance(value.Balance)
	return value
}

func cloneQuotaObservation(value QuotaObservation) QuotaObservation {
	value.Quota = cloneQuota(value.Quota)
	return value
}

func validQuotaObservation(observation QuotaObservation) bool {
	if !validCredentialEpoch(observation.CredentialEpoch) || observation.ObservedAtMS < 0 ||
		len(observation.Quota.Windows) == 0 {
		return false
	}
	seen := make(map[string]struct{}, len(observation.Quota.Windows))
	for _, window := range observation.Quota.Windows {
		if (window.Kind != WindowFiveHour && window.Kind != WindowWeekly && window.Kind != WindowMonthly) ||
			(window.Status != StatusOK && window.Status != StatusRateLimited) ||
			window.UsedPercent < 0 || window.UsedPercent > 100 ||
			window.RemainingPercent < 0 || window.RemainingPercent > 100 || window.ResetsAtMS < 0 {
			return false
		}
		if _, exists := seen[window.Kind]; exists {
			return false
		}
		seen[window.Kind] = struct{}{}
	}
	return true
}

func equalQuota(left Quota, right Quota) bool {
	if len(left.Windows) != len(right.Windows) {
		return false
	}
	leftCopy := append([]QuotaWindow(nil), left.Windows...)
	rightCopy := append([]QuotaWindow(nil), right.Windows...)
	sort.Slice(leftCopy, func(i, j int) bool { return leftCopy[i].Kind < leftCopy[j].Kind })
	sort.Slice(rightCopy, func(i, j int) bool { return rightCopy[i].Kind < rightCopy[j].Kind })
	for index := range leftCopy {
		if leftCopy[index] != rightCopy[index] {
			return false
		}
	}
	return true
}
