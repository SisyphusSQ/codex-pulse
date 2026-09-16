package subscriptionaccounts

import (
	"strings"
	"testing"
	"time"
)

func TestParseCivilDateRejectsInvalidCalendars(t *testing.T) {
	t.Parallel()
	for name, value := range map[string]string{
		"not leap":  "2026-02-29",
		"century":   "2100-02-29",
		"april 31":  "2026-04-31",
		"month 13":  "2026-13-01",
		"day 0":     "2026-01-00",
		"year 0":    "0000-01-01",
		"timestamp": "2026-09-15T00:00:00Z",
		"slash":     "2026/09/15",
		"spaces":    "2026-9-15",
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			_, err := ParseCivilDate(value)
			if err != ErrInvalidMembershipDate {
				t.Fatalf("ParseCivilDate(%s) error = %v", name, err)
			}
			if strings.Contains(err.Error(), value) {
				t.Fatalf("error leaked date: %v", err)
			}
		})
	}
	if _, err := ParseCivilDate("2024-02-29"); err != nil {
		t.Fatalf("leap day error = %v", err)
	}
}

func TestDayDeltaUsesCivilOrdinalNotElapsedHours(t *testing.T) {
	t.Parallel()
	start := CivilDate{Year: 2026, Month: 3, Day: 7}
	end := CivilDate{Year: 2026, Month: 3, Day: 9}
	if got := DayDelta(end, start); got != 2 {
		t.Fatalf("civil delta = %d, want 2", got)
	}
	location, err := time.LoadLocation("America/Los_Angeles")
	if err != nil {
		t.Fatal(err)
	}
	elapsed := time.Date(2026, 3, 9, 0, 0, 0, 0, location).Sub(
		time.Date(2026, 3, 7, 0, 0, 0, 0, location),
	)
	if int(elapsed/(24*time.Hour)) == 2 {
		t.Fatal("fixture no longer discriminates duration/24h")
	}
}

func TestResolveDateStatusAcrossTimeZonesAndMidnight(t *testing.T) {
	t.Parallel()
	shanghai, err := time.LoadLocation("Asia/Shanghai")
	if err != nil {
		t.Fatal(err)
	}
	losAngeles, err := time.LoadLocation("America/Los_Angeles")
	if err != nil {
		t.Fatal(err)
	}
	beforeMidnight := time.Date(2026, 9, 15, 23, 59, 59, 0, shanghai).UnixMilli()
	afterMidnight := time.Date(2026, 9, 16, 0, 0, 0, 0, shanghai).UnixMilli()
	target := pointer("2026-09-16")
	kind := pointer(DateKindNextRenewal)

	future, err := ResolveDateStatus(target, kind, beforeMidnight, "Asia/Shanghai")
	if err != nil || future.State != DateStateFuture || future.Delta == nil || *future.Delta != 1 {
		t.Fatalf("before midnight = %#v %v", future, err)
	}
	today, err := ResolveDateStatus(target, kind, afterMidnight, "Asia/Shanghai")
	if err != nil || today.State != DateStateToday || today.Delta == nil || *today.Delta != 0 {
		t.Fatalf("after midnight = %#v %v", today, err)
	}

	dstMorning := time.Date(2026, 3, 8, 1, 30, 0, 0, losAngeles).UnixMilli()
	dstAfternoon := time.Date(2026, 3, 8, 15, 0, 0, 0, losAngeles).UnixMilli()
	sameDay, err := ResolveDateStatus(pointer("2026-03-08"), kind, dstMorning, "America/Los_Angeles")
	if err != nil || sameDay.State != DateStateToday {
		t.Fatalf("DST morning = %#v %v", sameDay, err)
	}
	stillSame, err := ResolveDateStatus(pointer("2026-03-08"), kind, dstAfternoon, "America/Los_Angeles")
	if err != nil || stillSame.State != DateStateToday {
		t.Fatalf("DST afternoon = %#v %v", stillSame, err)
	}

	springStart := time.Date(2026, 3, 7, 12, 0, 0, 0, losAngeles).UnixMilli()
	acrossDST, err := ResolveDateStatus(pointer("2026-03-09"), kind, springStart, "America/Los_Angeles")
	if err != nil || acrossDST.Delta == nil || *acrossDST.Delta != 2 || acrossDST.State != DateStateFuture {
		t.Fatalf("DST spring civil delta = %#v %v", acrossDST, err)
	}
}

func TestResolveDateStatusLeapAndMonthEnd(t *testing.T) {
	t.Parallel()
	kind := pointer(DateKindMembershipExpiry)
	leap, err := ResolveDateStatus(
		pointer("2024-03-01"),
		kind,
		time.Date(2024, 2, 29, 12, 0, 0, 0, time.UTC).UnixMilli(),
		"UTC",
	)
	if err != nil || leap.Delta == nil || *leap.Delta != 1 {
		t.Fatalf("leap to march = %#v %v", leap, err)
	}
	monthEnd, err := ResolveDateStatus(
		pointer("2026-02-01"),
		kind,
		time.Date(2026, 1, 31, 8, 0, 0, 0, time.UTC).UnixMilli(),
		"UTC",
	)
	if err != nil || monthEnd.Delta == nil || *monthEnd.Delta != 1 {
		t.Fatalf("month end = %#v %v", monthEnd, err)
	}
	past, err := ResolveDateStatus(
		pointer("2026-09-01"),
		kind,
		time.Date(2026, 9, 15, 0, 0, 0, 0, time.UTC).UnixMilli(),
		"UTC",
	)
	if err != nil || past.State != DateStateNeedsUpdate || past.Delta == nil || *past.Delta != -14 {
		t.Fatalf("past date = %#v %v", past, err)
	}
}

func TestResolveMonthlyRenewalRollsForwardAndClampsMonthEnd(t *testing.T) {
	t.Parallel()
	kind := pointer(DateKindNextRenewal)

	rolled, err := ResolveDateStatus(
		pointer("2026-09-11"),
		kind,
		time.Date(2026, 9, 15, 0, 0, 0, 0, time.UTC).UnixMilli(),
		"UTC",
	)
	if err != nil || rolled.State != DateStateFuture || rolled.Delta == nil || *rolled.Delta != 26 {
		t.Fatalf("rolled monthly renewal = %#v %v", rolled, err)
	}
	if rolled.Date == nil || *rolled.Date != "2026-09-11" {
		t.Fatalf("stored renewal anchor changed = %#v", rolled)
	}

	beforeMonthEnd, err := ResolveDateStatus(
		pointer("2000-01-31"),
		kind,
		time.Date(2026, 2, 27, 0, 0, 0, 0, time.UTC).UnixMilli(),
		"UTC",
	)
	if err != nil || beforeMonthEnd.State != DateStateFuture || beforeMonthEnd.Delta == nil || *beforeMonthEnd.Delta != 1 {
		t.Fatalf("short month renewal = %#v %v", beforeMonthEnd, err)
	}
	monthEnd, err := ResolveDateStatus(
		pointer("2000-01-31"),
		kind,
		time.Date(2026, 2, 28, 0, 0, 0, 0, time.UTC).UnixMilli(),
		"UTC",
	)
	if err != nil || monthEnd.State != DateStateToday || monthEnd.Delta == nil || *monthEnd.Delta != 0 {
		t.Fatalf("month-end renewal day = %#v %v", monthEnd, err)
	}
	afterMonthEnd, err := ResolveDateStatus(
		pointer("2000-01-31"),
		kind,
		time.Date(2026, 3, 1, 0, 0, 0, 0, time.UTC).UnixMilli(),
		"UTC",
	)
	if err != nil || afterMonthEnd.State != DateStateFuture || afterMonthEnd.Delta == nil || *afterMonthEnd.Delta != 30 {
		t.Fatalf("next full month renewal = %#v %v", afterMonthEnd, err)
	}
	yearEnd, err := ResolveDateStatus(
		pointer("2000-01-05"),
		kind,
		time.Date(2026, 12, 31, 0, 0, 0, 0, time.UTC).UnixMilli(),
		"UTC",
	)
	if err != nil || yearEnd.State != DateStateFuture || yearEnd.Delta == nil || *yearEnd.Delta != 5 {
		t.Fatalf("year-end monthly renewal = %#v %v", yearEnd, err)
	}
}

func TestResolveDateStatusUnavailableWithoutDate(t *testing.T) {
	t.Parallel()
	status, err := ResolveDateStatus(nil, nil, 0, "UTC")
	if err != nil || status.State != DateStateUnavailable || status.Source != ValueSourceUnavailable || status.Delta != nil {
		t.Fatalf("missing date = %#v %v", status, err)
	}
}

func TestLoadTimeZoneRejectsLocalAndEmpty(t *testing.T) {
	t.Parallel()
	if _, err := LoadTimeZone(""); err != ErrInvalidTimeZone {
		t.Fatalf("empty zone error = %v", err)
	}
	if _, err := LoadTimeZone("Local"); err != ErrInvalidTimeZone {
		t.Fatalf("Local zone error = %v", err)
	}
	if _, err := LoadTimeZone("Not/AZone"); err != ErrInvalidTimeZone {
		t.Fatalf("unknown zone error = %v", err)
	}
}
