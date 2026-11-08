package statistics_srv

import (
	"encoding/json/v2"
	"testing"

	access_do "github.com/SisyphusSQ/codex-pulse/server/internal/models/do/mysql/access_do"
	reporting_do "github.com/SisyphusSQ/codex-pulse/server/internal/models/do/mysql/reporting_do"
	access_dto "github.com/SisyphusSQ/codex-pulse/server/internal/models/dto/access_dto"
	"github.com/SisyphusSQ/codex-pulse/server/internal/repository/mysql/statistics_repo"
	centerfixture "github.com/SisyphusSQ/codex-pulse/server/internal/testsupport/center"
)

func TestStatisticsSourceStreamDecodeKeepsStrictValidation(t *testing.T) {
	snap := centerfixture.Snapshot()
	for i := range 2000 {
		snap.Contributions = append(snap.Contributions, centerfixture.Contribution(1, int64(2000+i)))
	}
	body, err := json.Marshal(snap)
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name, payload string
		valid         bool
	}{
		{"large-valid", string(body), true},
		{"whitespace", string(body) + " \n\t", true},
		{"unknown-root", `{"unexpected":true}`, false},
		{"unknown-contribution", `{"contributions":[{"unexpected":true}]}`, false},
		{"unknown-unused-invocation", `{"invocations":[{"unexpected":true}]}`, false},
		{"duplicate", `{"provider":"codex","provider":"codex"}`, false},
		{"trailing-value", string(body) + "{}", false},
		{"truncated", string(body[:len(body)-1]), false},
		{"wrong-type", `{"contributions":"invalid"}`, false},
		{"invalid-utf8", "{\"title\":\"\xff\"}", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			engine := centerfixture.Engine(t)
			db := engine.DB(t.Context())
			for _, row := range []any{&access_do.Client{ID: "collector", Name: "synthetic", Purpose: access_dto.PurposeCollector, SecretHash: "synthetic"}, &reporting_do.Session{ID: "session", Provider: "codex"}, &reporting_do.SessionSource{ID: "source", SessionKey: "session", ClientID: "collector", Payload: tc.payload}} {
				if err := db.Create(row).Error; err != nil {
					t.Fatal(err)
				}
			}
			stats := NewStatistics(statistics_repo.NewStatistics(engine))
			admin := access_dto.Principal{ID: "admin", Purpose: access_dto.PurposeAdmin}
			q := statisticsTestQuery(t, nil)
			sources, e := stats.SourceUsage(t.Context(), admin, q)
			if (e == nil) != tc.valid {
				t.Fatal("source decode validation changed", e)
			}
			q.ClientID = "collector"
			summary, e := stats.Summary(t.Context(), admin, q)
			if (e == nil) != tc.valid {
				t.Fatal("collector decode validation changed", e)
			}
			if tc.valid {
				decimal(t, sources.Items[0].Totals.TotalTokens, "2100")
				decimal(t, summary.Totals.TotalTokens, "2100")
			}
		})
	}
}
