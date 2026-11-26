package statistics_srv

import (
	"encoding/json/v2"
	"fmt"
	"testing"

	reportingv1 "github.com/SisyphusSQ/codex-pulse/api/codexpulse/reporting/v1"
	access_do "github.com/SisyphusSQ/codex-pulse/server/internal/models/do/mysql/access_do"
	reporting_do "github.com/SisyphusSQ/codex-pulse/server/internal/models/do/mysql/reporting_do"
	schema_do "github.com/SisyphusSQ/codex-pulse/server/internal/models/do/mysql/schema_do"
	access_dto "github.com/SisyphusSQ/codex-pulse/server/internal/models/dto/access_dto"
	reporting_repo "github.com/SisyphusSQ/codex-pulse/server/internal/repository/mysql/reporting_repo"
	"github.com/SisyphusSQ/codex-pulse/server/internal/repository/mysql/schema_repo"
	statistics_repo "github.com/SisyphusSQ/codex-pulse/server/internal/repository/mysql/statistics_repo"
	"github.com/SisyphusSQ/codex-pulse/server/internal/service/reporting_srv"
	centerfixture "github.com/SisyphusSQ/codex-pulse/server/internal/testsupport/center"
)

func TestSourceUsageArbitratesHomesAndKeepsMachinesSeparate(t *testing.T) {
	for _, scenario := range []string{"superset", "conflict", "deleted"} {
		t.Run(scenario, func(t *testing.T) {
			engine := centerfixture.Engine(t)
			db := engine.DB(t.Context())
			for _, id := range []string{"one", "two"} {
				if err := db.Create(&access_do.Client{ID: id, Purpose: access_dto.PurposeCollector, Name: id, SecretHash: "synthetic-" + id}).Error; err != nil {
					t.Fatal(err)
				}
			}
			// 999 个较早会话与多个来源同流读取，来源 ID 本身不按会话分组。
			for i := range 999 {
				key := fmt.Sprintf("a-%03d", i)
				if err := db.Create(&reporting_do.Session{ID: key, Provider: "codex"}).Error; err != nil {
					t.Fatal(err)
				}
				snap := centerfixture.Snapshot()
				snap.SessionID = key
				snap.Contributions = nil
				body, err := json.Marshal(snap)
				if err != nil {
					t.Fatal(err)
				}
				if err := db.Create(&reporting_do.SessionSource{ID: "filler-" + key, SessionKey: key, ClientID: "one", Payload: string(body)}).Error; err != nil {
					t.Fatal(err)
				}
			}
			for _, key := range []string{"b-target", "c-other"} {
				if err := db.Create(&reporting_do.Session{ID: key, Provider: "codex"}).Error; err != nil {
					t.Fatal(err)
				}
			}
			for i, id := range []string{"a-copy", "z-copy", "m-other-machine", "n-other-session"} {
				snap := centerfixture.Snapshot()
				client, key := "one", "b-target"
				snap.HomeID = id
				if i == 1 {
					snap.Contributions = append(snap.Contributions, centerfixture.Contribution(25, 2000))
					if scenario == "conflict" {
						snap.Contributions = []reportingv1.Contribution{centerfixture.Contribution(200, 2000)}
					}
					snap.Deleted = scenario == "deleted"
				}
				if i == 2 {
					client = "two"
				}
				if i == 3 {
					key = "c-other"
					snap.SessionID = "other"
					snap.Contributions = []reportingv1.Contribution{centerfixture.Contribution(11, 1000)}
				}
				body, err := json.Marshal(snap)
				if err != nil {
					t.Fatal(err)
				}
				payload := string(body)
				if i == 1 {
					payload, err = reporting_repo.NewReporting(engine).StagePayload(t.Context(), payload, snap.Complete, snap.Deleted)
					if err != nil {
						t.Fatal(err)
					}
				}
				if err := reporting_repo.NewReporting(engine).SaveSource(t.Context(), reporting_do.SessionSource{ID: id, SessionKey: key, ClientID: client, HomeID: snap.HomeID, Payload: payload}); err != nil {
					t.Fatal(err)
				}
			}
			if err := db.Migrator().DropTable(&reporting_do.MachineUsageSource{}); err != nil {
				t.Fatal(err)
			}
			if err := db.Model(&schema_do.SchemaVersion{}).Where("id = ?", 1).Updates(map[string]any{
				"version": 9, "checksum": "a79125d7ad241f8b4a09a73d65a5c897e50155c4f6f7f9ec9b7c484a14a59a4a",
			}).Error; err != nil {
				t.Fatal(err)
			}
			if err := schema_repo.NewSchema(engine).Migrate(t.Context()); err != nil {
				t.Fatal(err)
			}
			stats := NewStatistics(statistics_repo.NewStatistics(engine))
			admin := access_dto.Principal{ID: "admin", Purpose: access_dto.PurposeAdmin}
			q := statisticsTestQuery(t, nil)
			out, err := stats.SourceUsage(t.Context(), admin, q)
			if err != nil || len(out.Items) != 2 {
				t.Fatal(out, err)
			}
			want := "111"
			if scenario == "superset" {
				want = "136"
			}
			for _, item := range out.Items {
				if item.Machine.ClientID == "one" {
					decimal(t, item.Totals.TotalTokens, want)
					if (item.Coverage.ConflictedSessions > 0) != (scenario == "conflict") {
						t.Fatal("conflict evidence lost", item.Coverage)
					}
				} else {
					decimal(t, item.Totals.TotalTokens, "100")
				}
			}
			q.ClientID = "one"
			filtered, err := stats.Summary(t.Context(), admin, q)
			if err != nil {
				t.Fatal(err)
			}
			decimal(t, filtered.Totals.TotalTokens, want)
		})
	}
}

func TestSourceUsageReadsSelectedDatesAndKeepsUntimedCoverage(t *testing.T) {
	engine := centerfixture.Engine(t)
	db := engine.DB(t.Context())
	collector := access_dto.Principal{ID: "collector", Purpose: access_dto.PurposeCollector}
	if err := db.Create(&access_do.Client{ID: collector.ID, Purpose: collector.Purpose, SecretHash: "synthetic"}).Error; err != nil {
		t.Fatal(err)
	}
	reporting := reporting_srv.NewReporting(reporting_repo.NewReporting(engine))
	stats := NewStatistics(statistics_repo.NewStatistics(engine))
	admin := access_dto.Principal{ID: "admin", Purpose: access_dto.PurposeAdmin}
	send := func(snapshot reportingv1.SessionSnapshot) {
		for i := range snapshot.Contributions {
			fact := &snapshot.Contributions[i]
			fact.ID = reportingv1.ContributionID("codex", snapshot.SessionID, *fact, 0)
		}
		centerfixture.SendSnapshot(t, reporting, collector, snapshot)
	}
	earlier := centerfixture.Snapshot()
	earlier.SessionID = "older"
	unknown := centerfixture.Contribution(101, 999)
	unknown.ObservedAtMS = nil
	modeled := centerfixture.Contribution(201, 999)
	modeled.ObservedAtMS = nil
	modeled.Model = new("gpt-5")
	earlier.Contributions = []reportingv1.Contribution{centerfixture.Contribution(1, 999), unknown, modeled}
	send(earlier)
	current := centerfixture.Snapshot()
	current.Contributions = []reportingv1.Contribution{
		centerfixture.Contribution(5, 1000), centerfixture.Contribution(7, 1999), centerfixture.Contribution(97, 2000),
	}
	send(current)
	future := centerfixture.Snapshot()
	future.SessionID = "future"
	future.Contributions = []reportingv1.Contribution{centerfixture.Contribution(86, 2000)}
	send(future)
	// 如果查询仍打开任一完全超出范围的来源，这个正文会使读取失败。
	disjoint := []string{reportingv1.Key("codex", earlier.SessionID), reportingv1.Key("codex", future.SessionID)}
	if err := db.Model(&reporting_do.SessionSource{}).Where("session_key IN ?", disjoint).Update("payload", "outside query range").Error; err != nil {
		t.Fatal(err)
	}
	q := statisticsTestQuery(t, nil)
	q.StartAtMS, q.EndAtMS = 1000, 2000
	out, err := stats.SourceUsage(t.Context(), admin, q)
	if err != nil || len(out.Items) != 1 {
		t.Fatal("range query failed", err)
	}
	item := out.Items[0]
	decimal(t, item.Totals.TotalTokens, "12")
	if item.Coverage.UntimedFacts != 2 || item.Totals.Sessions != 1 {
		t.Fatal("untimed or session coverage changed", item)
	}
	q.Model = "gpt-5"
	out, err = stats.SourceUsage(t.Context(), admin, q)
	if err != nil || out.Items[0].Totals.TotalTokens != nil || out.Items[0].Coverage.UntimedFacts != 1 {
		t.Fatal("model filter or unknown amount changed", out, err)
	}
	earlier.Revision++
	earlier.CollectedAtMS++
	earlier.Contributions = append(earlier.Contributions, centerfixture.Contribution(13, 1500))
	send(earlier)
	q.Model = ""
	out, err = stats.SourceUsage(t.Context(), admin, q)
	if err != nil || len(out.Items) != 1 || out.Items[0].Coverage.UntimedFacts != 2 {
		t.Fatal("late contribution did not update the range projection", out, err)
	}
	decimal(t, out.Items[0].Totals.TotalTokens, "25")
}
