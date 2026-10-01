// Package throughput derives content-free output efficiency from committed turn events.
package throughput

import (
	"math/big"
	"sort"
)

const MaxInteger int64 = 9_007_199_254_740_991

const ParserVersion = "codex-token-model-invocation-throughput-v5"

// Event contains only allowlisted lifecycle and output-counter facts.
type Event struct {
	Offset         int64
	Kind           string
	TurnID         *string
	AtMS           *int64
	TimeSource     string
	StartedAtMS    *int64
	DurationMS     *int64
	OutputDelta    *int64
	OutputObserved bool
}

// Stats keeps the measured subset separate from missing or still-open turns.
type Stats struct {
	AverageMilliTPS    *int64
	OutputTokens       *int64
	ActiveMS           *int64
	IncludedTurns      int64
	ExcludedTurns      int64
	OpenTurns          int64
	UnattributedEvents int64
	Status             string
	Reason             string
	DurationSource     string
}

// Turn is an internal read model; its identity must be hashed at the public boundary.
type Turn struct {
	ID          string
	StartedAtMS *int64
	EndedAtMS   *int64
	Stats       Stats
}

type turnState struct {
	Turn
	startSource string
	terminal    *Event
	output      int64
	seenOutput  bool
	sawCounter  bool
	firstUsage  *int64
	lastUsage   *int64
	started     bool
	invalid     bool
}

// Accumulator replays safe facts without retaining the event stream.
type Accumulator struct {
	turns        map[string]*turnState
	open         map[string]*turnState
	unattributed int64
	counterGap   bool
	limited      bool
	inherited    bool
}

func NewAccumulator() *Accumulator {
	return &Accumulator{turns: make(map[string]*turnState), open: make(map[string]*turnState)}
}

func (a *Accumulator) Add(event Event) {
	if a.limited {
		return
	}
	var turn *turnState
	if event.TurnID != nil {
		turn = a.turns[*event.TurnID]
		if turn == nil {
			if len(a.turns) >= 50_000 {
				a.limited = true
				return
			}
			turn = &turnState{Turn: Turn{ID: *event.TurnID}}
			a.turns[turn.ID] = turn
		}
	} else if len(a.open) == 1 {
		for _, candidate := range a.open {
			turn = candidate
		}
	}
	switch event.Kind {
	case "inherited":
		a.inherited = true
	case "start":
		if turn == nil {
			a.unattributed++
			return
		}
		if turn.started {
			if !sameNumber(turn.StartedAtMS, event.AtMS) {
				turn.invalid = true
			}
			return
		}
		turn.started = true
		if event.AtMS != nil {
			turn.StartedAtMS, turn.startSource = event.AtMS, event.TimeSource
		}
		if turn.terminal == nil {
			a.open[turn.ID] = turn
		}
	case "complete", "abort":
		if turn == nil {
			a.unattributed++
			return
		}
		if turn.terminal != nil {
			if !sameNumber(turn.terminal.AtMS, event.AtMS) || !sameNumber(turn.terminal.DurationMS, event.DurationMS) {
				turn.invalid = true
			}
			return
		}
		copy := event
		turn.terminal, turn.EndedAtMS = &copy, event.AtMS
		if !turn.sawCounter {
			a.counterGap = true
		}
		if turn.StartedAtMS == nil && event.StartedAtMS != nil {
			turn.StartedAtMS, turn.startSource = event.StartedAtMS, "source_seconds"
		}
		delete(a.open, turn.ID)
	case "usage":
		gap := a.counterGap
		a.counterGap = event.OutputDelta == nil && !event.OutputObserved
		if turn == nil {
			if event.OutputDelta == nil || *event.OutputDelta > 0 {
				a.unattributed++
				for _, candidate := range a.open {
					candidate.invalid = true
				}
			}
			return
		}
		turn.sawCounter = turn.sawCounter || event.OutputObserved || event.OutputDelta != nil
		if event.AtMS != nil {
			if turn.firstUsage == nil || *event.AtMS < *turn.firstUsage {
				turn.firstUsage = event.AtMS
			}
			if turn.lastUsage == nil || *event.AtMS > *turn.lastUsage {
				turn.lastUsage = event.AtMS
			}
		}
		if event.OutputDelta == nil {
			turn.invalid = true
			return
		}
		if gap {
			turn.invalid = true
			return
		}
		if turn.terminal != nil || *event.OutputDelta < 0 || *event.OutputDelta > MaxInteger-turn.output {
			turn.invalid = true
			return
		}
		if event.AtMS != nil && turn.StartedAtMS != nil && *event.AtMS < *turn.StartedAtMS {
			turn.invalid = true
		}
		turn.seenOutput = true
		turn.output += *event.OutputDelta
	case "gap":
		a.counterGap = true
		a.unattributed++
		for _, candidate := range a.open {
			candidate.invalid = true
		}
	}
}

// Result computes a duration-weighted average over the union of eligible intervals.
func (a *Accumulator) Result() (Stats, []Turn) {
	if a.inherited {
		return Stats{Status: "unavailable", Reason: "inherited_history"}, nil
	}
	if a.limited {
		return Stats{Status: "unavailable", Reason: "state_limit"}, nil
	}
	stats := Stats{Status: "unavailable", Reason: "no_closed_turns", UnattributedEvents: a.unattributed}
	turns := make([]Turn, 0, len(a.turns))
	var intervals [][2]int64
	var output int64
	overflow := false
	for _, state := range a.turns {
		item := state.Turn
		item.Stats = Stats{Status: "unavailable", Reason: "missing_facts"}
		if state.terminal == nil && state.started {
			item.Stats.Reason, item.Stats.OpenTurns = "open_turn", 1
			stats.OpenTurns++
		} else if start, end, source, ok := interval(state); ok && state.started && state.seenOutput && !state.invalid && usageWithinInterval(state, start, end) {
			duration := end - start
			item.Stats = measured(state.output, duration)
			item.Stats.DurationSource = source
			item.StartedAtMS, item.EndedAtMS = number(start), number(end)
			if item.Stats.AverageMilliTPS == nil || state.output > MaxInteger-output {
				overflow = true
			}
			if state.output <= MaxInteger-output {
				output += state.output
			}
			stats.IncludedTurns++
			intervals = append(intervals, [2]int64{start, end})
			if stats.DurationSource == "" {
				stats.DurationSource = source
			} else if stats.DurationSource != source {
				stats.DurationSource = "mixed"
			}
		} else {
			item.Stats.ExcludedTurns = 1
			stats.ExcludedTurns++
		}
		turns = append(turns, item)
	}
	sort.Slice(turns, func(i, j int) bool {
		left, right := int64(-1), int64(-1)
		if turns[i].StartedAtMS != nil {
			left = *turns[i].StartedAtMS
		}
		if turns[j].StartedAtMS != nil {
			right = *turns[j].StartedAtMS
		}
		if left != right {
			return left > right
		}
		return turns[i].ID > turns[j].ID
	})
	if stats.IncludedTurns == 0 {
		if stats.ExcludedTurns > 0 {
			stats.Reason = "missing_facts"
		}
		return stats, turns
	}
	sort.Slice(intervals, func(i, j int) bool { return intervals[i][0] < intervals[j][0] })
	start, end, total := intervals[0][0], intervals[0][1], int64(0)
	for _, span := range intervals[1:] {
		if span[0] <= end {
			if span[1] > end {
				end = span[1]
			}
			continue
		}
		if end-start > MaxInteger-total {
			overflow = true
		} else {
			total += end - start
		}
		start, end = span[0], span[1]
	}
	if end-start > MaxInteger-total {
		overflow = true
	} else {
		total += end - start
	}
	value := measured(output, total)
	stats.AverageMilliTPS, stats.OutputTokens, stats.ActiveMS = value.AverageMilliTPS, value.OutputTokens, value.ActiveMS
	stats.Status, stats.Reason = "complete", ""
	if stats.OpenTurns+stats.ExcludedTurns+stats.UnattributedEvents > 0 {
		stats.Status, stats.Reason = "partial", "incomplete_coverage"
	}
	if overflow || stats.AverageMilliTPS == nil {
		stats.Status, stats.Reason = "unavailable", "numeric_overflow"
		stats.AverageMilliTPS, stats.OutputTokens, stats.ActiveMS = nil, nil, nil
	}
	return stats, turns
}

func usageWithinInterval(state *turnState, start, end int64) bool {
	// Native duration and envelope emission may differ by subsecond rounding.
	return (state.firstUsage == nil || *state.firstUsage >= start-1000) &&
		(state.lastUsage == nil || *state.lastUsage <= end+1000)
}

func interval(state *turnState) (int64, int64, string, bool) {
	if state.terminal == nil {
		return 0, 0, "", false
	}
	end, start := state.EndedAtMS, state.StartedAtMS
	if end == nil || start == nil || *end < *start {
		return 0, 0, "", false
	}
	if duration := state.terminal.DurationMS; duration != nil {
		if *duration <= 0 || *duration > *end {
			return 0, 0, "", false
		}
		// Source seconds and event emission can differ by subsecond rounding.
		difference := *end - *start - *duration
		if difference < -1000 || difference > 1000 {
			return 0, 0, "", false
		}
		return *end - *duration, *end, "duration_ms", true
	}
	if *end == *start {
		return 0, 0, "", false
	}
	source := "log_timestamp"
	if state.startSource != source || state.terminal.TimeSource != source {
		source = "source_seconds"
	}
	return *start, *end, source, true
}

func measured(output, duration int64) Stats {
	stats := Stats{Status: "complete", IncludedTurns: 1, OutputTokens: number(output), ActiveMS: number(duration)}
	if duration <= 0 || output < 0 {
		return Stats{Status: "unavailable", Reason: "missing_facts"}
	}
	value := new(big.Int).Mul(big.NewInt(output), big.NewInt(1_000_000))
	value.Add(value, big.NewInt(duration/2)).Quo(value, big.NewInt(duration))
	if !value.IsInt64() || value.Int64() > MaxInteger {
		stats.Status, stats.Reason = "unavailable", "numeric_overflow"
		return stats
	}
	stats.AverageMilliTPS = number(value.Int64())
	return stats
}

func number(value int64) *int64 { return &value }
func sameNumber(a, b *int64) bool {
	return (a == nil && b == nil) || (a != nil && b != nil && *a == *b)
}
