package query

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"
)

func TestSpecificationValidateNormalizesLocalDateRangeAcrossDST(t *testing.T) {
	specification, err := NewSpecification(validSpecificationConfig())
	if err != nil {
		t.Fatalf("NewSpecification() error = %v", err)
	}

	tests := []struct {
		name     string
		start    string
		end      string
		timeZone string
		duration time.Duration
	}{
		{name: "Shanghai regular day", start: "2026-07-16", end: "2026-07-17", timeZone: "Asia/Shanghai", duration: 24 * time.Hour},
		{name: "Los Angeles spring forward", start: "2026-03-08", end: "2026-03-09", timeZone: "America/Los_Angeles", duration: 23 * time.Hour},
		{name: "Los Angeles fall back", start: "2026-11-01", end: "2026-11-02", timeZone: "America/Los_Angeles", duration: 25 * time.Hour},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			requestRange := &LocalDateRange{
				StartDate: test.start, EndDateExclusive: test.end, TimeZone: test.timeZone,
			}
			validated, err := specification.Validate(t.Context(), Request{TimeRange: requestRange})
			if err != nil {
				t.Fatalf("Validate() error = %v", err)
			}
			if validated.TimeRange == nil || validated.TimeRange.TimeZone != test.timeZone {
				t.Fatalf("validated time range = %#v", validated.TimeRange)
			}
			if got := time.Duration(validated.TimeRange.EndAtMS-validated.TimeRange.StartAtMS) * time.Millisecond; got != test.duration {
				t.Fatalf("UTC duration = %s, want %s", got, test.duration)
			}

			requestRange.StartDate = "2030-01-01"
			if validated.TimeRange.TimeZone != test.timeZone || validated.TimeRange.EndAtMS <= validated.TimeRange.StartAtMS {
				t.Fatalf("validated range changed after caller mutation: %#v", validated.TimeRange)
			}
		})
	}
}

func TestSpecificationValidateRejectsInvalidLocalDateRange(t *testing.T) {
	specification, err := NewSpecification(validSpecificationConfig())
	if err != nil {
		t.Fatalf("NewSpecification() error = %v", err)
	}

	tests := []struct {
		name  string
		value LocalDateRange
		field string
	}{
		{name: "non canonical start", value: LocalDateRange{StartDate: "2026-7-1", EndDateExclusive: "2026-07-02", TimeZone: "Asia/Shanghai"}, field: "timeRange.startDate"},
		{name: "impossible end", value: LocalDateRange{StartDate: "2026-07-01", EndDateExclusive: "2026-02-30", TimeZone: "Asia/Shanghai"}, field: "timeRange.endDateExclusive"},
		{name: "machine local timezone", value: LocalDateRange{StartDate: "2026-07-01", EndDateExclusive: "2026-07-02", TimeZone: "Local"}, field: "timeRange.timeZone"},
		{name: "unknown timezone", value: LocalDateRange{StartDate: "2026-07-01", EndDateExclusive: "2026-07-02", TimeZone: "Mars/Olympus"}, field: "timeRange.timeZone"},
		{name: "empty range", value: LocalDateRange{StartDate: "2026-07-01", EndDateExclusive: "2026-07-01", TimeZone: "Asia/Shanghai"}, field: "timeRange"},
		{name: "reverse range", value: LocalDateRange{StartDate: "2026-07-02", EndDateExclusive: "2026-07-01", TimeZone: "Asia/Shanghai"}, field: "timeRange"},
		{name: "range over endpoint max", value: LocalDateRange{StartDate: "2025-01-01", EndDateExclusive: "2026-01-03", TimeZone: "Asia/Shanghai"}, field: "timeRange"},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			_, err := specification.Validate(t.Context(), Request{TimeRange: &test.value})
			assertValidationField(t, err, test.field)
		})
	}
}

func TestNumericValueDistinguishesUnknownAndRealZero(t *testing.T) {
	zero, err := KnownNumeric(0, NumericTokens)
	if err != nil {
		t.Fatalf("KnownNumeric(0) error = %v", err)
	}
	if zero.Value == nil || *zero.Value != 0 || zero.UnknownReason != nil {
		t.Fatalf("known zero = %#v", zero)
	}
	unknown, err := UnknownNumeric(NumericMicroUSD, UnknownNeverLoaded)
	if err != nil {
		t.Fatalf("UnknownNumeric() error = %v", err)
	}
	if unknown.Value != nil || unknown.UnknownReason == nil || *unknown.UnknownReason != UnknownNeverLoaded {
		t.Fatalf("unknown value = %#v", unknown)
	}

	encodedZero, err := json.Marshal(zero)
	if err != nil {
		t.Fatalf("json.Marshal(zero) error = %v", err)
	}
	encodedUnknown, err := json.Marshal(unknown)
	if err != nil {
		t.Fatalf("json.Marshal(unknown) error = %v", err)
	}
	if !strings.Contains(string(encodedZero), `"value":0`) ||
		!strings.Contains(string(encodedZero), `"unknownReason":null`) {
		t.Fatalf("known zero JSON = %s", encodedZero)
	}
	if !strings.Contains(string(encodedUnknown), `"value":null`) ||
		!strings.Contains(string(encodedUnknown), `"unknownReason":"never_loaded"`) {
		t.Fatalf("unknown JSON = %s", encodedUnknown)
	}
}

func TestNumericValueRejectsUnsafeOrInconsistentState(t *testing.T) {
	if value, err := KnownNumeric(JavaScriptMaxSafeInteger, NumericMicroUSD); err != nil || value.Value == nil {
		t.Fatalf("KnownNumeric(max safe) = %#v, %v", value, err)
	}

	tests := []struct {
		name  string
		value NumericValue
		field string
	}{
		{name: "negative", value: NumericValue{Value: int64Pointer(-1), Unit: NumericTokens}, field: "numeric.value"},
		{name: "unsafe integer", value: NumericValue{Value: int64Pointer(JavaScriptMaxSafeInteger + 1), Unit: NumericTokens}, field: "numeric.value"},
		{name: "unknown without reason", value: NumericValue{Unit: NumericTokens}, field: "numeric.unknownReason"},
		{name: "known with reason", value: NumericValue{Value: int64Pointer(0), Unit: NumericTokens, UnknownReason: unknownReasonPointer(UnknownUnavailable)}, field: "numeric.unknownReason"},
		{name: "invalid unit", value: NumericValue{Value: int64Pointer(1), Unit: "float"}, field: "numeric.unit"},
		{name: "invalid reason", value: NumericValue{Unit: NumericCount, UnknownReason: unknownReasonPointer("missing")}, field: "numeric.unknownReason"},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			assertValidationField(t, test.value.Validate(), test.field)
		})
	}

	if _, err := KnownNumeric(-1, NumericTokens); !errors.Is(err, ErrValidation) {
		t.Fatalf("KnownNumeric(-1) error = %v, want ErrValidation", err)
	}
	if _, err := UnknownNumeric(NumericTokens, "missing"); !errors.Is(err, ErrValidation) {
		t.Fatalf("UnknownNumeric(invalid reason) error = %v, want ErrValidation", err)
	}
}

func int64Pointer(value int64) *int64 {
	return &value
}

func unknownReasonPointer(value UnknownReason) *UnknownReason {
	return &value
}
