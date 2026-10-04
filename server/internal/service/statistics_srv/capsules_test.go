package statistics_srv

import (
	"net/url"
	"reflect"
	"testing"

	reportingv1 "github.com/SisyphusSQ/codex-pulse/api/codexpulse/reporting/v1"
	"github.com/SisyphusSQ/codex-pulse/server/internal/repository/mysql/access_repo"
	"github.com/SisyphusSQ/codex-pulse/server/internal/repository/mysql/reporting_repo"
	"github.com/SisyphusSQ/codex-pulse/server/internal/repository/mysql/statistics_repo"
	"github.com/SisyphusSQ/codex-pulse/server/internal/service/access_srv"
	"github.com/SisyphusSQ/codex-pulse/server/internal/service/reporting_srv"
	centerfixture "github.com/SisyphusSQ/codex-pulse/server/internal/testsupport/center"
)

func TestCapsuleRebuildAndGlobalReadsDoNotDecodeFullSnapshots(t *testing.T) {
	engine := centerfixture.Engine(t)
	access := access_srv.NewAccess(access_repo.NewAccess(engine))
	admin := centerfixture.Admin(t, access).Principal
	client := centerfixture.Collector(t, access, admin, "synthetic")
	reporting := reporting_srv.NewReporting(reporting_repo.NewReporting(engine))
	stats := NewStatistics(statistics_repo.NewStatistics(engine))
	centerfixture.SendSnapshot(t, reporting, client, throughputSnapshot())
	q := statisticsTestQuery(t, nil)
	before, err := stats.Sessions(t.Context(), admin, q)
	if err != nil {
		t.Fatal(err)
	}
	if err := engine.DB(t.Context()).Exec("DELETE FROM pulse_session_capsules").Error; err != nil {
		t.Fatal(err)
	}
	if _, err := reporting.WarmCapsules(t.Context(), 1); err != nil {
		t.Fatal(err)
	}
	if _, err := reporting.WarmCapsules(t.Context(), 1); err != nil {
		t.Fatal(err)
	}
	after, err := stats.Sessions(t.Context(), admin, q)
	if err != nil || !reflect.DeepEqual(before.Items, after.Items) {
		t.Fatal("rebuild changed facts", err)
	}
	for _, sql := range []string{"UPDATE pulse_session_sources SET payload='invalid synthetic JSON'", "UPDATE pulse_session_canonical SET payload='invalid synthetic JSON'"} {
		if err := engine.DB(t.Context()).Exec(sql).Error; err != nil {
			t.Fatal(err)
		}
	}
	after, err = stats.Sessions(t.Context(), admin, q)
	if err != nil || !reflect.DeepEqual(before.Items, after.Items) {
		t.Fatal("list decoded full payload", err)
	}
	if _, err := stats.Summary(t.Context(), admin, q); err != nil {
		t.Fatal("summary decoded full payload", err)
	}
}

func TestHeatmapReadKeepsTotalsDaysAndCoverage(t *testing.T) {
	stats, reporting, admin, clients := statisticsFixture(t)
	_ = admin
	snapshot := throughputSnapshot()
	centerfixture.SendSnapshot(t, reporting, clients[0], snapshot)
	q := statisticsTestQuery(t, nil)
	for _, client := range []string{"", clients[0].ID} {
		q.ClientID = client
		full, err := stats.read(t.Context(), q)
		if err != nil {
			t.Fatal(err)
		}
		annual, err := stats.readFacts(t.Context(), q, false, true)
		if err != nil {
			t.Fatal(err)
		}
		a, _ := full.total.finish(true)
		b, _ := annual.total.finish(true)
		if !reflect.DeepEqual(a, b) || !reflect.DeepEqual(full.trend(), annual.trend()) || !reflect.DeepEqual(full.coverage(stats.now()), annual.coverage(stats.now())) {
			t.Fatal("annual read changed totals/days/coverage")
		}
		if len(annual.models) != 0 || len(annual.hours) != 0 || len(annual.sessions) != 0 {
			t.Fatal("unused annual breakdowns populated")
		}
	}
}

func TestHeatmapReadPreservesFilteredFacts(t *testing.T) {
	stats, reporting, _, clients := statisticsFixture(t)
	snapshot := centerfixture.Snapshot()
	snapshot.Contributions[0].InputTokens = new(int64(9007199254740993))
	snapshot.Contributions[0].TotalTokens = new(int64(9007199254740993))
	snapshot.Contributions[0].Model = new("synthetic-model")
	zero := centerfixture.Contribution(0, 2000)
	zero.Model = new("synthetic-model")
	unknown := centerfixture.Contribution(0, 3000)
	unknown.TotalTokens = nil
	unknown.Model = new("other-model")
	snapshot.Contributions = append(snapshot.Contributions, zero, unknown)
	fixStatisticsIDs(&snapshot)
	centerfixture.SendSnapshot(t, reporting, clients[0], snapshot)
	centerfixture.SendSnapshot(t, reporting, clients[1], snapshot)
	other := centerfixture.Snapshot()
	other.SessionID, other.ProjectID, other.Title = "other-session", "other-project", "other-title"
	fixStatisticsIDs(&other)
	centerfixture.SendSnapshot(t, reporting, clients[0], other)

	for name, values := range map[string]url.Values{
		"all":            {},
		"provider":       {"provider": {"codex"}},
		"empty_provider": {"provider": {"cursor"}},
		"model":          {"model": {"synthetic-model"}},
		"search":         {"search": {"other-title"}},
		"project":        {"project_id": {reportingv1.Key(clients[0].ID, snapshot.Provider, snapshot.ProjectID)}},
		"date":           {"start_at_ms": {"2000"}, "end_at_ms": {"3000"}},
	} {
		t.Run(name, func(t *testing.T) {
			q := statisticsTestQuery(t, values)
			full, err := stats.read(t.Context(), q)
			if err != nil {
				t.Fatal(err)
			}
			annual, err := stats.readFacts(t.Context(), q, false, true)
			if err != nil {
				t.Fatal(err)
			}
			a, _ := full.total.finish(true)
			b, _ := annual.total.finish(true)
			if !reflect.DeepEqual(a, b) || !reflect.DeepEqual(full.trend(), annual.trend()) || !reflect.DeepEqual(full.coverage(stats.now()), annual.coverage(stats.now())) {
				t.Fatal("heatmap query changed filtered facts or coverage")
			}
			if name == "model" || name == "project" {
				decimal(t, b.TotalTokens, "9007199254740993")
			}
			if name == "search" {
				decimal(t, b.TotalTokens, "100")
			}
			if name == "date" || name == "empty_provider" {
				decimal(t, b.TotalTokens, "0")
			}
		})
	}
}
