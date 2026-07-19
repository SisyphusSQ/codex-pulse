package main

import "testing"

func TestAggregateRoundsRequiresEveryColdRoundToPass(t *testing.T) {
	t.Parallel()
	round := validRound()
	result, err := aggregateRounds([]roundSummary{round, round, round})
	if err != nil || result.Rounds != 3 || result.Result != "passed" {
		t.Fatalf("aggregateRounds() = %#v, %v", result, err)
	}
	failed := round
	failed.ProjectP95Micros = maximumProjectP95Micros + 1
	if _, err := aggregateRounds([]roundSummary{round, failed, round}); err == nil {
		t.Fatal("aggregateRounds() accepted one threshold failure")
	}
}

func TestAggregateRoundsDoesNotAcceptSingleBestRound(t *testing.T) {
	t.Parallel()
	fast := validRound()
	fast.BootstrapMS = 100_000
	fast.BootstrapBytesPerSec = 20 << 20
	slow := validRound()
	slow.BootstrapMS = maximumBootstrapMS + 1
	if _, err := aggregateRounds([]roundSummary{fast, slow, fast}); err == nil {
		t.Fatal("aggregateRounds() accepted a slow round because other rounds were fast")
	}
}

func TestAggregateRoundsRejectsMissingPositiveMetrics(t *testing.T) {
	t.Parallel()
	for name, mutate := range map[string]func(*roundSummary){
		"peak RSS":      func(round *roundSummary) { round.PeakRSSBytes = 0 },
		"average CPU":   func(round *roundSummary) { round.AverageCPUPercent = 0 },
		"database size": func(round *roundSummary) { round.DatabaseBytes = 0 },
		"session p95":   func(round *roundSummary) { round.SessionP95Micros = 0 },
		"project p95":   func(round *roundSummary) { round.ProjectP95Micros = 0 },
		"usage p95":     func(round *roundSummary) { round.UsageP95Micros = 0 },
		"quota p95":     func(round *roundSummary) { round.QuotaP95Micros = 0 },
		"popover p95":   func(round *roundSummary) { round.PopoverP95Micros = 0 },
	} {
		t.Run(name, func(t *testing.T) {
			round := validRound()
			mutate(&round)
			if _, err := aggregateRounds([]roundSummary{validRound(), round, validRound()}); err == nil {
				t.Fatal("aggregateRounds() accepted a missing required metric")
			}
		})
	}
}

func TestAggregateRoundsUsesWorstRoundAsThreeSampleP95(t *testing.T) {
	t.Parallel()
	first, second, third := validRound(), validRound(), validRound()
	first.BootstrapMS, second.BootstrapMS, third.BootstrapMS = 100_000, 200_000, 300_000
	result, err := aggregateRounds([]roundSummary{first, second, third})
	if err != nil || result.BootstrapP50MS != 200_000 || result.BootstrapP95MS != 300_000 {
		t.Fatalf("aggregateRounds() percentiles = %#v, %v", result, err)
	}
}

func validRound() roundSummary {
	return roundSummary{
		Version: "m11-real-home-v1", Result: "passed", SourceFilesBefore: 100,
		BootstrapMS: 300_000, FirstScreenMS: 10_000, BootstrapBytes: 4 << 30,
		BootstrapBytesPerSec: 10 << 20, PeakRSSBytes: 256 << 20, AverageCPUPercent: 80,
		DatabaseBytes: 128 << 20, WALBytes: 32 << 20, WarmQuerySamples: 30,
		SessionP95Micros: 1_000, ProjectP95Micros: 2_000, UsageP95Micros: 3_000,
		QuotaP95Micros: 1_000, PopoverP95Micros: 7_000, FullHistoryReady: true,
		DTOPrivacyPassed: true, SourceReadOnlyPassed: true,
		IsolatedStoreClosed: true, IsolatedRootRemoved: true,
	}
}
