package throughput

import "testing"

func TestWeightedAverageExcludesIdleAndIncludesOutputOnce(t *testing.T) {
	a := NewAccumulator()
	for _, event := range []Event{
		{Kind: "start", TurnID: id("one"), AtMS: number(0), TimeSource: "log_timestamp"},
		{Kind: "usage", OutputDelta: number(500)}, {Kind: "usage", OutputDelta: number(500)}, {Kind: "usage", OutputDelta: number(0)},
		{Kind: "complete", TurnID: id("one"), AtMS: number(10000), DurationMS: number(10000)},
		{Kind: "start", TurnID: id("two"), AtMS: number(3_600_000), TimeSource: "log_timestamp"},
		{Kind: "usage", OutputDelta: number(1000)},
		{Kind: "abort", TurnID: id("two"), AtMS: number(3_700_000), DurationMS: number(100000)},
	} {
		a.Add(event)
	}
	s, turns := a.Result()
	if s.Status != "complete" || *s.OutputTokens != 2000 || *s.ActiveMS != 110000 || *s.AverageMilliTPS != 18182 || len(turns) != 2 {
		t.Fatalf("weighted result = %+v, turns = %+v", s, turns)
	}
}

func TestCoverageAndZero(t *testing.T) {
	for _, tc := range []struct {
		name     string
		delta    *int64
		close    bool
		status   string
		eligible int64
	}{
		{"zero", number(0), true, "complete", 1}, {"missing", nil, true, "unavailable", 0}, {"open", number(10), false, "unavailable", 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			a := NewAccumulator()
			a.Add(Event{Kind: "start", TurnID: id("t"), AtMS: number(1000), TimeSource: "log_timestamp"})
			a.Add(Event{Kind: "usage", OutputDelta: tc.delta})
			if tc.close {
				a.Add(Event{Kind: "complete", TurnID: id("t"), AtMS: number(2000)})
			}
			s, _ := a.Result()
			if s.Status != tc.status || s.IncludedTurns != tc.eligible {
				t.Fatalf("stats = %+v", s)
			}
			if tc.name == "zero" && (s.AverageMilliTPS == nil || *s.AverageMilliTPS != 0) {
				t.Fatal("observed zero lost")
			}
		})
	}
}

func TestOverlapUsesUnionAndAmbiguousUsageIsExcluded(t *testing.T) {
	for _, explicit := range []bool{true, false} {
		a := NewAccumulator()
		a.Add(Event{Kind: "start", TurnID: id("a"), AtMS: number(0), TimeSource: "log_timestamp"})
		a.Add(Event{Kind: "start", TurnID: id("b"), AtMS: number(5000), TimeSource: "log_timestamp"})
		var turn *string
		if explicit {
			turn = id("a")
		}
		a.Add(Event{Kind: "usage", TurnID: turn, OutputDelta: number(1000)})
		a.Add(Event{Kind: "usage", TurnID: id("b"), OutputDelta: number(1000)})
		a.Add(Event{Kind: "complete", TurnID: id("a"), AtMS: number(10000)})
		a.Add(Event{Kind: "complete", TurnID: id("b"), AtMS: number(15000)})
		s, _ := a.Result()
		if explicit {
			if s.Status != "complete" || *s.ActiveMS != 15000 || *s.AverageMilliTPS != 133333 {
				t.Fatalf("union = %+v", s)
			}
		} else if s.IncludedTurns != 0 || s.AverageMilliTPS != nil {
			t.Fatalf("ambiguous = %+v", s)
		}
	}
}

func id(value string) *string { return &value }

func TestMissingCounterDoesNotLeakPreviousTurnOutputIntoNextTurn(t *testing.T) {
	a := NewAccumulator()
	for i, name := range []string{"a", "b", "c"} {
		start := int64(i * 2000)
		end := start + 1000
		a.Add(Event{Kind: "start", TurnID: id(name), AtMS: number(start), TimeSource: "log_timestamp"})
		e := Event{Kind: "usage", OutputDelta: number(10), OutputObserved: true}
		if i == 0 {
			e.OutputDelta = nil
			e.OutputObserved = false
		}
		a.Add(e)
		a.Add(Event{Kind: "complete", TurnID: id(name), AtMS: number(end), TimeSource: "log_timestamp"})
	}
	s, _ := a.Result()
	if s.Status != "partial" || s.ExcludedTurns != 2 || s.IncludedTurns != 1 || *s.OutputTokens != 10 {
		t.Fatalf("counter gap = %+v", s)
	}
}

func TestClosedTurnWithoutCounterFencesNextDelta(t *testing.T) {
	a := NewAccumulator()
	for i, name := range []string{"a", "b", "c"} {
		start, end := int64(i*2000), int64(i*2000+1000)
		a.Add(Event{Kind: "start", TurnID: id(name), AtMS: number(start)})
		if i > 0 {
			a.Add(Event{Kind: "usage", OutputDelta: number(10), OutputObserved: true})
		}
		a.Add(Event{Kind: "complete", TurnID: id(name), AtMS: number(end)})
	}
	s, _ := a.Result()
	if s.ExcludedTurns != 2 || s.IncludedTurns != 1 || s.OutputTokens == nil || *s.OutputTokens != 10 {
		t.Fatalf("missing turn counter = %+v", s)
	}
}

func TestTimingEvidenceAndNumericBoundary(t *testing.T) {
	for _, tc := range []struct {
		name              string
		start, end, usage int64
		duration          *int64
		output            int64
		eligible          bool
	}{
		{"timestamp fallback", 2000, 12000, 5000, nil, 100, true},
		{"duration conflict", 2000, 12000, 5000, number(1000), 100, false},
		{"late start cannot absorb earlier usage", 10000, 20000, 1000, nil, 100, false},
		{"zero duration", 2000, 2000, 2000, number(0), 0, false},
		{"rate overflow", 2000, 2001, 2000, nil, MaxInteger, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			a := NewAccumulator()
			a.Add(Event{Kind: "usage", TurnID: id("t"), AtMS: number(tc.usage), OutputDelta: number(tc.output), OutputObserved: true})
			a.Add(Event{Kind: "start", TurnID: id("t"), AtMS: number(tc.start), TimeSource: "log_timestamp"})
			a.Add(Event{Kind: "complete", TurnID: id("t"), AtMS: number(tc.end), TimeSource: "log_timestamp", DurationMS: tc.duration})
			s, _ := a.Result()
			if (s.IncludedTurns == 1) != tc.eligible {
				t.Fatalf("timing coverage = %+v", s)
			}
			if tc.name == "rate overflow" {
				if s.Reason != "numeric_overflow" || s.AverageMilliTPS != nil {
					t.Fatalf("overflow = %+v", s)
				}
			} else if tc.eligible && (s.AverageMilliTPS == nil || *s.AverageMilliTPS != 10000 || s.DurationSource != "log_timestamp") {
				t.Fatalf("timestamp fallback = %+v", s)
			}
		})
	}
}
