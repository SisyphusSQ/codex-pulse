package core

import (
	"testing"

	corev1 "github.com/SisyphusSQ/codex-pulse/api/codexpulse/core/v1"
	query "github.com/SisyphusSQ/codex-pulse/internal/query"
	"github.com/SisyphusSQ/codex-pulse/internal/query/usagecost"
)

func TestThroughputProtoPreservesUnitsCoverageAndZero(t *testing.T) {
	known := func(value int64, unit query.NumericUnit) query.NumericValue {
		t.Helper()
		result, err := query.KnownNumeric(value, unit)
		if err != nil {
			t.Fatal(err)
		}
		return result
	}
	source := usagecost.ThroughputStats{
		AverageOutputMilliTPS: known(0, query.NumericMilliTPS), OutputTokens: known(0, query.NumericTokens), ActiveDurationMS: known(1000, query.NumericMilliseconds),
		IncludedTurns: known(1, query.NumericCount), ExcludedTurns: known(0, query.NumericCount), OpenTurns: known(0, query.NumericCount), UnattributedEvents: known(0, query.NumericCount),
		Status: "complete", DurationSource: "duration_ms", Basis: "closed_turn_lifetime_output",
	}
	target := &corev1.ThroughputStats{}
	if err := EncodeResponse(source, target); err != nil {
		t.Fatal(err)
	}
	if target.AverageOutputMilliTps.Value == nil || target.AverageOutputMilliTps.GetValue() != 0 || target.AverageOutputMilliTps.Unit != "milli_tokens_per_second" || target.IncludedTurns.GetValue() != 1 {
		t.Fatalf("proto=%+v", target)
	}
	source.AverageOutputMilliTPS, _ = query.UnknownNumeric(query.NumericMilliTPS, query.UnknownNotComputed)
	if err := EncodeResponse(source, target); err != nil {
		t.Fatal(err)
	}
	if target.AverageOutputMilliTps.Value != nil || target.AverageOutputMilliTps.GetUnknownReason() != "not_computed" {
		t.Fatal("unknown TPS mapped to zero")
	}
}
