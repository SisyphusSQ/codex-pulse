package statistics_srv

import (
	"encoding/json/v2"
	"fmt"
	"testing"

	reportingv1 "github.com/SisyphusSQ/codex-pulse/api/codexpulse/reporting/v1"
	access_do "github.com/SisyphusSQ/codex-pulse/server/internal/models/do/mysql/access_do"
	reporting_do "github.com/SisyphusSQ/codex-pulse/server/internal/models/do/mysql/reporting_do"
	access_dto "github.com/SisyphusSQ/codex-pulse/server/internal/models/dto/access_dto"
	reporting_repo "github.com/SisyphusSQ/codex-pulse/server/internal/repository/mysql/reporting_repo"
	statistics_repo "github.com/SisyphusSQ/codex-pulse/server/internal/repository/mysql/statistics_repo"
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
