package statistics_srv

import (
	"errors"
	"net/url"
	"testing"
	"time"

	reportingv1 "github.com/SisyphusSQ/codex-pulse/api/codexpulse/reporting/v1"
	reporting_do "github.com/SisyphusSQ/codex-pulse/server/internal/models/do/mysql/reporting_do"
	statistics_vo "github.com/SisyphusSQ/codex-pulse/server/internal/models/vo/statistics_vo"
	centerfixture "github.com/SisyphusSQ/codex-pulse/server/internal/testsupport/center"
	"github.com/SisyphusSQ/codex-pulse/server/utils"
)

func TestOverviewCollectorCopiesRangeTopAndMachineNames(t *testing.T) {
	stats, r, admin, clients := statisticsFixture(t)
	snap := centerfixture.Snapshot()
	for _, client := range clients[:2] {
		centerfixture.SendSnapshot(t, r, client, snap)
	}
	snap.Revision = 2
	snap.Contributions = append(snap.Contributions, centerfixture.Contribution(25, 2000))
	centerfixture.SendSnapshot(t, r, clients[1], snap)
	q := statisticsTestQuery(t, nil)
	own, err := stats.SourceUsage(t.Context(), admin, q)
	if err != nil {
		t.Fatal(err)
	}
	if len(own.Items) != 3 || own.Scope != "collector_copies_may_overlap" {
		t.Fatal(own)
	}
	decimal(t, own.Items[0].Totals.TotalTokens, "125")
	decimal(t, own.Items[1].Totals.TotalTokens, "100")
	if own.Items[2].Totals.TotalTokens != nil {
		t.Fatal("missing collector became zero")
	}
	if own.Items[0].Machine.ClientID != clients[1].ID || own.Items[0].Machine.ClientName != "二" {
		t.Fatal("machine identity/name lost")
	}
	summary, err := stats.Summary(t.Context(), admin, q)
	if err != nil {
		t.Fatal(err)
	}
	decimal(t, summary.Totals.TotalTokens, "125")
	if len(summary.TopSessions) != 1 || len(summary.TopSessions[0].Sources) != 2 {
		t.Fatal("top session provenance")
	}
	decimal(t, summary.TopSessions[0].Totals.TotalTokens, "125")
	q.StartAtMS = 1500
	ranged, err := stats.Summary(t.Context(), admin, q)
	if err != nil {
		t.Fatal(err)
	}
	decimal(t, ranged.TopSessions[0].Totals.TotalTokens, "25")
	q.ClientID = clients[0].ID
	filtered, err := stats.SourceUsage(t.Context(), admin, q)
	if err != nil || len(filtered.Items) != 1 || filtered.Items[0].Totals.TotalTokens != nil {
		t.Fatal("collector range ignored", filtered, err)
	}
	projects, err := stats.Projects(t.Context(), admin, statisticsTestQuery(t, nil))
	if err != nil {
		t.Fatal(err)
	}
	if len(projects.Items) != 2 || len(projects.Items[0].Machines) != 1 {
		t.Fatal("project source identity lost")
	}
	if _, err = stats.SourceUsage(t.Context(), clients[0], q); !errors.Is(err, utils.ErrForbidden) {
		t.Fatal("collector read permitted")
	}
	q.ClientID = ""
	q.Provider = "cursor"
	cursor, err := stats.SourceUsage(t.Context(), admin, q)
	if err != nil || len(cursor.Items) != 0 {
		t.Fatal("Cursor appeared as Codex usage")
	}
}
func TestOverviewHourlyDSTAndSyntheticExclusion(t *testing.T) {
	stats, r, admin, clients := statisticsFixture(t)
	q := statisticsTestQuery(t, url.Values{"start_date": {"2026-11-01"}, "end_date_exclusive": {"2026-11-02"}, "time_zone": {"America/New_York"}})
	snap := centerfixture.Snapshot()
	snap.Contributions = []reportingv1.Contribution{centerfixture.Contribution(3, time.Date(2026, 11, 1, 5, 30, 0, 0, time.UTC).UnixMilli()), centerfixture.Contribution(7, time.Date(2026, 11, 1, 6, 30, 0, 0, time.UTC).UnixMilli())}
	centerfixture.SendSnapshot(t, r, clients[0], snap)
	summary, err := stats.Summary(t.Context(), admin, q)
	if err != nil {
		t.Fatal(err)
	}
	if summary.ActivityGranularity != "hour" || len(summary.ActivityTimeline) != 25 {
		t.Fatal("DST hour collapsed", len(summary.ActivityTimeline))
	}
	decimal(t, summary.ActivityTimeline[1].Tokens, "3")
	decimal(t, summary.ActivityTimeline[2].Tokens, "7")
	if summary.ActivityTimeline[0].Tokens != nil || summary.ActivityTimeline[0].Sessions != nil {
		t.Fatal("unknown hour filled zero")
	}
	if summary.ActivityTimeline[1].EndAtMS != summary.ActivityTimeline[2].StartAtMS {
		t.Fatal("bucket boundaries")
	}
	read := &statisticsRead{q: q, metadata: map[string]reporting_do.Session{"a": {ID: "a", SessionKind: "unassigned_usage"}}, sessions: map[string]*statisticsAggregate{"a": newStatisticsAggregate()}, sources: map[string][]statistics_vo.StatisticsSource{}, projects: map[string]reporting_do.Project{}, providerSeen: map[string]bool{}}
	read.sessions["a"].tokens.add(new(int64(100)))
	if len(read.topSessions()) != 0 {
		t.Fatal("billing aggregate shown as real session")
	}
}
