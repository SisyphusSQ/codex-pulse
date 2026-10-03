package statistics_srv

import (
	"reflect"
	"testing"

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
