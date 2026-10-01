package statistics_srv

import (
	"net/url"
	"testing"

	reportingv1 "github.com/SisyphusSQ/codex-pulse/api/codexpulse/reporting/v1"
	centerfixture "github.com/SisyphusSQ/codex-pulse/server/internal/testsupport/center"
)

func throughputSnapshot() reportingv1.SessionSnapshot {
	s := centerfixture.Snapshot()
	s.Contributions = nil
	for _, at := range []int64{2000, 3602000} {
		c := centerfixture.Contribution(0, at)
		c.OutputTokens, c.TotalTokens = new(int64(1000)), new(int64(1000))
		c.ID = reportingv1.ContributionID(s.Provider, s.SessionID, c, 0)
		s.Contributions = append(s.Contributions, c)
	}
	measures := reportingv1.ThroughputMeasures{OutputTokens: new(int64(2000)), ActiveDurationMS: new(int64(110000)), IncludedTurns: 2, CoverageKnown: true, Status: "complete", DurationSource: "duration_ms"}
	s.Throughput = &reportingv1.ThroughputCapsule{Version: 1, Basis: "closed_turn_lifetime_output", Measures: measures, TurnsTotal: 2}
	for i, span := range [][2]int64{{1000, 11000}, {3601000, 3701000}} {
		turn := measures
		turn.IncludedTurns = 1
		turn.OutputTokens, turn.ActiveDurationMS = new(int64(1000)), new(span[1]-span[0])
		s.Throughput.RecentTurns = append(s.Throughput.RecentTurns, reportingv1.ThroughputTurn{Key: reportingv1.Key("synthetic-turn", string(rune('a'+i))), StartedAtMS: new(span[0]), EndedAtMS: new(span[1]), Measures: turn})
	}
	return s
}

func TestStatisticsThroughputLifetimeCopiesRangeLimitAndSourceConflict(t *testing.T) {
	stats, reporting, admin, clients := statisticsFixture(t)
	snapshot := throughputSnapshot()
	for _, p := range clients {
		centerfixture.SendSnapshot(t, reporting, p, snapshot)
	}
	q := statisticsTestQuery(t, nil)
	list, err := stats.Sessions(t.Context(), admin, q)
	if err != nil || len(list.Items) != 1 {
		t.Fatal("session query", err)
	}
	decimal(t, list.Items[0].Totals.OutputTokens, "1000")
	view := list.Items[0].Throughput
	if view == nil || view.Status != "complete" || view.Conflict || view.SourceClientID == nil {
		t.Fatal("missing canonical throughput/provenance")
	}
	decimal(t, view.AverageOutputMilliTPS, "18182")
	decimal(t, view.OutputTokens, "2000")
	decimal(t, view.ActiveDurationMS, "110000")
	decimal(t, view.IncludedTurns, "2")
	q.ThroughputLimit = 1
	detail, err := stats.Session(t.Context(), admin, q, list.Items[0].ID)
	if err != nil || len(detail.ThroughputTurns.Items) != 1 || !detail.ThroughputTurns.Truncated {
		t.Fatal("bounded recent turns", err)
	}
	decimal(t, detail.Session.Throughput.AverageOutputMilliTPS, "18182")
	decimal(t, detail.ThroughputTurns.Total, "2")
	q = statisticsTestQuery(t, url.Values{"start_at_ms": {"3600000"}, "end_at_ms": {"3800000"}})
	detail, err = stats.Session(t.Context(), admin, q, list.Items[0].ID)
	if err != nil {
		t.Fatal(err)
	}
	decimal(t, detail.Session.Throughput.AverageOutputMilliTPS, "18182")
	// A second complete source has identical output facts but different credible timing.
	snapshot.Revision = 2
	snapshot.Throughput.Measures.ActiveDurationMS = new(int64(111000))
	snapshot.Throughput.RecentTurns[0].EndedAtMS = new(int64(12000))
	snapshot.Throughput.RecentTurns[0].Measures.ActiveDurationMS = new(int64(11000))
	centerfixture.SendSnapshot(t, reporting, clients[1], snapshot)
	detail, err = stats.Session(t.Context(), admin, q, list.Items[0].ID)
	if err != nil || !detail.Session.Throughput.Conflict || detail.Session.Throughput.Reason != "source_conflict" {
		t.Fatal("complete TPS conflict hidden", err)
	}
	q.ClientID = clients[1].ID
	detail, err = stats.Session(t.Context(), admin, q, list.Items[0].ID)
	if err != nil || detail.Session.Throughput.Conflict {
		t.Fatal("own source mixed with another device", err)
	}
	decimal(t, detail.Session.Throughput.AverageOutputMilliTPS, "18018")
}

func TestStatisticsThroughputLegacyPendingAndUnsupportedAreUnknown(t *testing.T) {
	stats, reporting, admin, clients := statisticsFixture(t)
	s := centerfixture.Snapshot()
	centerfixture.SendSnapshot(t, reporting, clients[0], s)
	q := statisticsTestQuery(t, nil)
	list, err := stats.Sessions(t.Context(), admin, q)
	if err != nil || list.Items[0].Throughput.AverageOutputMilliTPS != nil || list.Items[0].Throughput.Reason != "not_reported" {
		t.Fatal("legacy missing became zero", err)
	}
	s.Revision = 2
	s.Throughput = &reportingv1.ThroughputCapsule{Version: 1, Basis: "closed_turn_lifetime_output", Measures: reportingv1.ThroughputMeasures{Status: "unavailable", Reason: "index_pending"}}
	centerfixture.SendSnapshot(t, reporting, clients[0], s)
	list, err = stats.Sessions(t.Context(), admin, q)
	if err != nil || list.Items[0].Throughput.IncludedTurns != nil || list.Items[0].Throughput.Reason != "index_pending" {
		t.Fatal("pending coverage invented", err)
	}
	for _, value := range []string{"0", "51", "NaN"} {
		if _, err := ParseStatisticsQuery(url.Values{"throughput_limit": {value}}, stats.now()); err == nil {
			t.Fatal("unbounded recent turns accepted")
		}
	}
	legacy := centerfixture.Snapshot()
	legacy.Provider, legacy.SourceKind, legacy.SessionID = "cursor", "cursor_local", "cursor-synthetic"
	fixStatisticsIDs(&legacy)
	centerfixture.SendSnapshot(t, reporting, clients[0], legacy)
	q.Provider = "cursor"
	list, err = stats.Sessions(t.Context(), admin, q)
	if err != nil || len(list.Items) != 1 || list.Items[0].Throughput.Reason != "unsupported_provider" || list.Items[0].Throughput.AverageOutputMilliTPS != nil {
		t.Fatal("unsupported TPS invented", err)
	}
}

func TestStatisticsThroughputMeasuredZeroAndInheritedHistory(t *testing.T) {
	stats, reporting, admin, clients := statisticsFixture(t)
	s := centerfixture.Snapshot()
	s.Contributions = []reportingv1.Contribution{centerfixture.Contribution(0, 1000)}
	s.Contributions[0].OutputTokens = new(int64(0))
	fixStatisticsIDs(&s)
	s.Throughput = &reportingv1.ThroughputCapsule{Version: 1, Basis: "closed_turn_lifetime_output", TurnsTotal: 1, Measures: reportingv1.ThroughputMeasures{OutputTokens: new(int64(0)), ActiveDurationMS: new(int64(1000)), IncludedTurns: 1, CoverageKnown: true, Status: "complete", DurationSource: "duration_ms"}}
	centerfixture.SendSnapshot(t, reporting, clients[0], s)
	q := statisticsTestQuery(t, nil)
	list, err := stats.Sessions(t.Context(), admin, q)
	if err != nil {
		t.Fatal(err)
	}
	decimal(t, list.Items[0].Throughput.AverageOutputMilliTPS, "0")
	s.Revision++
	s.Throughput = &reportingv1.ThroughputCapsule{Version: 1, Basis: "closed_turn_lifetime_output", Measures: reportingv1.ThroughputMeasures{Status: "unavailable", Reason: "inherited_history"}}
	centerfixture.SendSnapshot(t, reporting, clients[0], s)
	list, err = stats.Sessions(t.Context(), admin, q)
	if err != nil || list.Items[0].Throughput.IncludedTurns != nil || list.Items[0].Throughput.AverageOutputMilliTPS != nil || list.Items[0].Throughput.Reason != "inherited_history" {
		t.Fatal("inheritance became zero", err)
	}
}
