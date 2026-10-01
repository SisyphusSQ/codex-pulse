package reportingv1

import (
	"errors"
	"testing"
)

func TestThroughputProtocolUnknownZeroBudgetsAndOutputReconciliation(t *testing.T) {
	valid := func() Batch {
		b := validBatch()
		b.Sessions[0].SourceKind = "light_index"
		c := &b.Sessions[0].Contributions[0]
		c.OutputTokens = new(int64(0))
		c.ID = ContributionID("codex", "session", *c, 0)
		b.Sessions[0].Throughput = &ThroughputCapsule{Version: 1, Basis: "closed_turn_lifetime_output", Measures: ThroughputMeasures{OutputTokens: new(int64(0)), ActiveDurationMS: new(int64(1000)), IncludedTurns: 1, CoverageKnown: true, Status: "complete", DurationSource: "duration_ms"}, TurnsTotal: 1}
		return b
	}
	if err := valid().Validate(); err != nil {
		t.Fatal(err)
	}
	for _, mutate := range []func(*Batch){
		func(b *Batch) { b.Sessions[0].Throughput.Measures.OutputTokens = new(int64(1)) },
		func(b *Batch) { b.Sessions[0].Throughput.Measures.ActiveDurationMS = new(int64(0)) },
		func(b *Batch) { b.Sessions[0].Throughput.Measures.IncludedTurns = 50001 },
		func(b *Batch) { b.Sessions[0].Throughput.Measures.OpenTurns = 1 },
		func(b *Batch) { b.Sessions[0].Throughput.RecentTurns = make([]ThroughputTurn, 51) },
		func(b *Batch) { b.Sessions[0].HistoryStartAtMS = 1 },
		func(b *Batch) { b.Sessions[0].Provider = "cursor" },
		func(b *Batch) {
			b.Sessions[0].Throughput.Measures.Status = "partial"
			b.Sessions[0].Throughput.Measures.Reason = "incomplete_coverage"
		},
	} {
		b := valid()
		mutate(&b)
		if err := b.Validate(); !errors.Is(err, ErrInvalid) {
			t.Fatal("invalid throughput accepted", err)
		}
	}
	b := valid()
	b.Sessions[0].Throughput.Version = 2
	if err := b.Validate(); !errors.Is(err, ErrVersion) {
		t.Fatal("future throughput version not rejected explicitly")
	}
	b = valid()
	b.Sessions[0].Throughput.Measures = ThroughputMeasures{Status: "unavailable", Reason: "index_pending"}
	b.Sessions[0].Throughput.TurnsTotal = 0
	if err := b.Validate(); err != nil {
		t.Fatal("pending unknown rejected", err)
	}
}
