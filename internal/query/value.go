package query

import (
	"time"
)

const (
	// JavaScriptMaxSafeInteger 是 Wails/TypeScript number 可精确表示的最大整数。
	JavaScriptMaxSafeInteger int64 = 9_007_199_254_740_991
	dateLayout                     = "2006-01-02"
)

// NumericUnit 固定跨端整数的业务单位，不允许无单位 number。
type NumericUnit string

const (
	NumericTokens       NumericUnit = "tokens"
	NumericMicroUSD     NumericUnit = "micro_usd"
	NumericCount        NumericUnit = "count"
	NumericMilliseconds NumericUnit = "milliseconds"
)

// UnknownReason 区分从未加载、不适用、暂不可用和尚未计算。
type UnknownReason string

const (
	UnknownNeverLoaded   UnknownReason = "never_loaded"
	UnknownNotApplicable UnknownReason = "not_applicable"
	UnknownUnavailable   UnknownReason = "unavailable"
	UnknownNotComputed   UnknownReason = "not_computed"
)

// NumericValue 使用 value/reason xor 区分 unknown 与真实零。
type NumericValue struct {
	Value         *int64         `json:"value"`
	Unit          NumericUnit    `json:"unit"`
	UnknownReason *UnknownReason `json:"unknownReason"`
}

// KnownNumeric 构造跨端可精确表示的非负整数，0 是合法已知值。
func KnownNumeric(value int64, unit NumericUnit) (NumericValue, error) {
	numeric := NumericValue{Value: &value, Unit: unit}
	if err := numeric.Validate(); err != nil {
		return NumericValue{}, err
	}
	return numeric, nil
}

// UnknownNumeric 构造无值但有固定原因的数值。
func UnknownNumeric(unit NumericUnit, reason UnknownReason) (NumericValue, error) {
	numeric := NumericValue{Unit: unit, UnknownReason: &reason}
	if err := numeric.Validate(); err != nil {
		return NumericValue{}, err
	}
	return numeric, nil
}

// Validate 检查单位、JS-safe 范围和 value/reason xor。
func (numeric NumericValue) Validate() error {
	if !validNumericUnit(numeric.Unit) {
		return validationFailure("numeric.unit")
	}
	if numeric.Value != nil {
		if *numeric.Value < 0 || *numeric.Value > JavaScriptMaxSafeInteger {
			return validationFailure("numeric.value")
		}
		if numeric.UnknownReason != nil {
			return validationFailure("numeric.unknownReason")
		}
		return nil
	}
	if numeric.UnknownReason == nil || !validUnknownReason(*numeric.UnknownReason) {
		return validationFailure("numeric.unknownReason")
	}
	return nil
}

func normalizeLocalDateRange(input LocalDateRange, maxDays int) (*UTCTimeRange, error) {
	if input.TimeZone == "" || input.TimeZone == "Local" {
		return nil, validationFailure("timeRange.timeZone")
	}
	location, err := time.LoadLocation(input.TimeZone)
	if err != nil {
		return nil, validationFailure("timeRange.timeZone")
	}
	start, err := parseLocalDate(input.StartDate, location)
	if err != nil {
		return nil, validationFailure("timeRange.startDate")
	}
	end, err := parseLocalDate(input.EndDateExclusive, location)
	if err != nil {
		return nil, validationFailure("timeRange.endDateExclusive")
	}
	if !start.Before(end) {
		return nil, validationFailure("timeRange")
	}
	calendarStart := time.Date(start.Year(), start.Month(), start.Day(), 0, 0, 0, 0, time.UTC)
	calendarEnd := time.Date(end.Year(), end.Month(), end.Day(), 0, 0, 0, 0, time.UTC)
	days := int(calendarEnd.Sub(calendarStart) / (24 * time.Hour))
	if days < 1 || days > maxDays {
		return nil, validationFailure("timeRange")
	}
	startAtMS := start.UnixMilli()
	endAtMS := end.UnixMilli()
	if startAtMS < 0 || endAtMS <= startAtMS || endAtMS > JavaScriptMaxSafeInteger {
		return nil, validationFailure("timeRange")
	}
	return &UTCTimeRange{
		StartAtMS: startAtMS, EndAtMS: endAtMS, TimeZone: input.TimeZone,
	}, nil
}

func parseLocalDate(value string, location *time.Location) (time.Time, error) {
	parsed, err := time.ParseInLocation(dateLayout, value, location)
	if err != nil || parsed.Format(dateLayout) != value {
		return time.Time{}, validationFailure("date")
	}
	return parsed, nil
}

func validNumericUnit(value NumericUnit) bool {
	switch value {
	case NumericTokens, NumericMicroUSD, NumericCount, NumericMilliseconds:
		return true
	default:
		return false
	}
}

func validUnknownReason(value UnknownReason) bool {
	switch value {
	case UnknownNeverLoaded, UnknownNotApplicable, UnknownUnavailable, UnknownNotComputed:
		return true
	default:
		return false
	}
}
