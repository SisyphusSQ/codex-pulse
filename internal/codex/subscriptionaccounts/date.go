package subscriptionaccounts

import (
	"fmt"
	"time"

	"github.com/SisyphusSQ/codex-pulse/internal/runtimeclock"
)

type CivilDate struct {
	Year  int
	Month int
	Day   int
}

func (date CivilDate) String() string {
	return fmt.Sprintf("%04d-%02d-%02d", date.Year, date.Month, date.Day)
}

func ParseCivilDate(value string) (CivilDate, error) {
	parsed, err := time.Parse(time.DateOnly, value)
	if err != nil || parsed.Year() < 1 || parsed.Format(time.DateOnly) != value {
		return CivilDate{}, ErrInvalidMembershipDate
	}
	return CivilDate{Year: parsed.Year(), Month: int(parsed.Month()), Day: parsed.Day()}, nil
}

func EvaluationCivilDate(evaluatedAtMS int64, timeZone string) (CivilDate, error) {
	location, err := LoadTimeZone(timeZone)
	if err != nil {
		return CivilDate{}, err
	}
	if err := ParseEvaluationTime(evaluatedAtMS); err != nil {
		return CivilDate{}, err
	}
	local := time.UnixMilli(evaluatedAtMS).In(location)
	return CivilDate{Year: local.Year(), Month: int(local.Month()), Day: local.Day()}, nil
}

func LoadTimeZone(timeZone string) (*time.Location, error) {
	if timeZone == "" || timeZone == "Local" {
		return nil, ErrInvalidTimeZone
	}
	location, err := time.LoadLocation(timeZone)
	if err != nil || location == nil {
		return nil, ErrInvalidTimeZone
	}
	return location, nil
}

func ParseEvaluationTime(evaluatedAtMS int64) error {
	if evaluatedAtMS < 0 || evaluatedAtMS > runtimeclock.MaxTimestampMS {
		return ErrInvalidEvaluationTime
	}
	return nil
}

func DayDelta(target, evaluation CivilDate) int {
	return int((civilDateTime(target).Unix() - civilDateTime(evaluation).Unix()) / secondsPerDay)
}

func ClassifyDayDelta(delta int) DateState {
	switch {
	case delta > 0:
		return DateStateFuture
	case delta == 0:
		return DateStateToday
	default:
		return DateStateNeedsUpdate
	}
}

func ResolveDateStatus(date *string, kind *DateKind, evaluatedAtMS int64, timeZone string) (DateStatus, error) {
	if date == nil && kind == nil {
		return DateStatus{Source: ValueSourceUnavailable, State: DateStateUnavailable}, nil
	}
	if date == nil || kind == nil {
		return DateStatus{}, ErrInvalidDatePair
	}
	target, err := ParseCivilDate(*date)
	if err != nil {
		return DateStatus{}, err
	}
	if err := ParseDateKind(*kind); err != nil {
		return DateStatus{}, err
	}
	evaluation, err := EvaluationCivilDate(evaluatedAtMS, timeZone)
	if err != nil {
		return DateStatus{}, err
	}
	effectiveTarget := target
	if *kind == DateKindNextRenewal {
		effectiveTarget = nextMonthlyRenewalDate(evaluation, target.Day)
	}
	delta := DayDelta(effectiveTarget, evaluation)
	copiedDate := target.String()
	copiedKind := *kind
	return DateStatus{
		Kind:   &copiedKind,
		Date:   &copiedDate,
		Source: ValueSourceManual,
		State:  ClassifyDayDelta(delta),
		Delta:  pointerTo(delta),
	}, nil
}

func nextMonthlyRenewalDate(evaluation CivilDate, renewalDay int) CivilDate {
	candidate := monthlyRenewalDate(evaluation.Year, evaluation.Month, renewalDay)
	if DayDelta(candidate, evaluation) >= 0 {
		return candidate
	}
	year, month := evaluation.Year, evaluation.Month+1
	if month > 12 {
		year++
		month = 1
	}
	return monthlyRenewalDate(year, month, renewalDay)
}

func monthlyRenewalDate(year, month, renewalDay int) CivilDate {
	lastDay := time.Date(year, time.Month(month)+1, 0, 0, 0, 0, 0, time.UTC).Day()
	return CivilDate{Year: year, Month: month, Day: min(renewalDay, lastDay)}
}

const secondsPerDay = 24 * 60 * 60

func civilDateTime(date CivilDate) time.Time {
	return time.Date(date.Year, time.Month(date.Month), date.Day, 0, 0, 0, 0, time.UTC)
}
