package statistics_repo

import (
	"context"
	"encoding/json/v2"
	"reflect"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"

	"gorm.io/gorm"

	reporting_do "github.com/SisyphusSQ/codex-pulse/server/internal/models/do/mysql/reporting_do"
	statistics_dto "github.com/SisyphusSQ/codex-pulse/server/internal/models/dto/statistics_dto"
	reporting_repo "github.com/SisyphusSQ/codex-pulse/server/internal/repository/mysql/reporting_repo"
	centerfixture "github.com/SisyphusSQ/codex-pulse/server/internal/testsupport/center"
)

func TestUsageProjectionPreservesFactsAndPublishedGeneration(t *testing.T) {
	engine := centerfixture.Engine(t)
	db := engine.DB(t.Context())
	for _, key := range []string{"legacy", "published"} {
		if err := db.Create(&reporting_do.Session{ID: key, Provider: "codex"}).Error; err != nil {
			t.Fatal(err)
		}
	}
	fact := reporting_do.Usage{SessionKey: "legacy", ContributionID: "fact", Position: 9, ObservedAtMS: new(int64(100)), Model: new("model"), InputTokens: new(int64(9007199254740993)), CachedTokens: new(int64(0)), CacheWriteTokens: new(int64(2)), OutputTokens: new(int64(3)), ReasoningTokens: new(int64(4)), TotalTokens: new(int64(9007199254740996)), CostMicroUSD: new(int64(5)), ReportedChargeMicroUSD: new(int64(6)), PricingVersion: new("historical"), PricingMode: "event", InputPrice: new(int64(7)), CachedPrice: new(int64(8)), CacheWritePrice: new(int64(9)), OutputPrice: new(int64(10)), CostStatus: "known"}
	if err := db.Create(&fact).Error; err != nil {
		t.Fatal(err)
	}
	fact.SessionKey = "published"
	if err := db.Create(&fact).Error; err != nil {
		t.Fatal(err)
	}
	for _, generation := range []string{"current", "obsolete"} {
		for _, id := range []string{"one", "two", "untimed"} {
			row := fact
			row.ContributionID = id
			row.InputTokens, row.Model = nil, nil
			if id == "untimed" {
				row.ObservedAtMS = nil
			}
			if err := db.Create(&reporting_do.UsageVersion{Generation: generation, Usage: row}).Error; err != nil {
				t.Fatal(err)
			}
		}
	}
	if err := db.Create(&reporting_do.SessionPublication{SessionKey: "published", Generation: "current"}).Error; err != nil {
		t.Fatal(err)
	}
	repo := NewStatistics(engine)
	q := statistics_dto.StatisticsQuery{StartAtMS: 100, EndAtMS: 101}
	for _, stream := range []func(context.Context, statistics_dto.StatisticsQuery, func(reporting_do.Usage) error) error{repo.StreamUsage, repo.StreamHeatmapUsage} {
		var got []reporting_do.Usage
		if err := stream(t.Context(), q, func(row reporting_do.Usage) error { got = append(got, row); return nil }); err != nil {
			t.Fatal(err)
		}
		// 两条数值相同的已发布事实仍是两条；不能用 DISTINCT 缩窄传输。
		if len(got) != 3 {
			t.Fatal("facts lost or obsolete/legacy versions included", len(got))
		}
		for _, row := range got {
			want := fact
			want.SessionKey, want.ContributionID, want.Position = row.SessionKey, "", 0
			if row.SessionKey == "published" {
				want.InputTokens, want.Model = nil, nil
			}
			if !reflect.DeepEqual(row, want) {
				t.Fatal("statistics fields changed", row)
			}
		}
	}
	untimed := 0
	if err := repo.StreamUntimed(t.Context(), q, func(row reporting_do.Usage) error {
		untimed++
		if row.SessionKey != "published" || row.Model != nil {
			t.Fatal("untimed fields changed")
		}
		return nil
	}); err != nil || untimed != 1 {
		t.Fatal("untimed coverage changed", untimed, err)
	}
}

func TestUsageAccessPathPreservesPublishedFactsAndSelectiveQueries(t *testing.T) {
	engine := centerfixture.Engine(t)
	db := engine.DB(t.Context())
	const start = int64(1800000000000)
	day := (24 * time.Hour).Milliseconds()
	if err := db.Create(&reporting_do.Project{ID: "project", GroupID: "group"}).Error; err != nil {
		t.Fatal(err)
	}
	for _, key := range []string{"legacy", "published", "deleted"} {
		if err := db.Create(&reporting_do.Session{ID: key, Provider: "codex", ProjectID: "project", Deleted: key == "deleted"}).Error; err != nil {
			t.Fatal(err)
		}
		if key != "legacy" {
			if err := db.Create(&reporting_do.SessionPublication{SessionKey: key, Generation: "current"}).Error; err != nil {
				t.Fatal(err)
			}
		}
		if err := db.Create(&reporting_do.SessionSource{ID: key, SessionKey: key, ClientID: "client", Payload: "{}"}).Error; err != nil {
			t.Fatal(err)
		}
		for i, at := range []*int64{new(start - 1), new(start), new(start), new(start + day), new(start + 7*day), nil} {
			models := []*string{nil, new(""), new("unknown"), new("model")}
			row := reporting_do.Usage{SessionKey: key, ContributionID: strconv.Itoa(i), Position: int64(i), ObservedAtMS: at, Model: models[i%len(models)], TotalTokens: new(int64(9007199254740993)), CachedTokens: new(int64(0))}
			if err := db.Create(&row).Error; err != nil {
				t.Fatal(err)
			}
			if key != "legacy" {
				for _, gen := range []string{"current", "obsolete"} {
					if err := db.Create(&reporting_do.UsageVersion{Generation: gen, Usage: row}).Error; err != nil {
						t.Fatal(err)
					}
				}
			}
		}
	}
	var queries []string
	if err := db.Callback().Row().After("gorm:row").Register("test-usage-path", func(tx *gorm.DB) {
		queries = append(queries, tx.Statement.SQL.String())
	}); err != nil {
		t.Fatal(err)
	}
	repo := NewStatistics(engine)
	for _, tc := range []struct {
		name    string
		q       statistics_dto.StatisticsQuery
		heatmap bool
		indexed bool
	}{
		{name: "day", q: statistics_dto.StatisticsQuery{EndAtMS: start + day}, indexed: true},
		{name: "week", q: statistics_dto.StatisticsQuery{EndAtMS: start + 7*day}, indexed: true},
		{name: "over-week", q: statistics_dto.StatisticsQuery{EndAtMS: start + 7*day + 1}},
		{name: "annual", q: statistics_dto.StatisticsQuery{EndAtMS: start + 365*day}, heatmap: true},
		{name: "heatmap-narrow", heatmap: true},
		{name: "provider", q: statistics_dto.StatisticsQuery{Provider: "codex"}, indexed: true},
		{name: "model", q: statistics_dto.StatisticsQuery{Model: "model"}, indexed: true},
		{name: "unknown", q: statistics_dto.StatisticsQuery{Model: "unknown"}, indexed: true},
		{name: "session", q: statistics_dto.StatisticsQuery{SessionKey: "published"}},
		{name: "project", q: statistics_dto.StatisticsQuery{ProjectID: "group"}},
		{name: "client", q: statistics_dto.StatisticsQuery{ClientID: "client"}},
		{name: "search", q: statistics_dto.StatisticsQuery{Search: "term"}},
		{name: "matched", q: statistics_dto.StatisticsQuery{MatchedSessionKeys: []string{"published"}}},
		{name: "empty-match", q: statistics_dto.StatisticsQuery{MatchedSessionKeys: []string{}}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			q := tc.q
			q.StartAtMS = start
			if q.EndAtMS == 0 {
				q.EndAtMS = start + 2*day
			}
			// 原已发布事实定义作为对照，不依赖新的索引选择逻辑。
			var expected []reporting_do.Usage
			base := db.Table(reporting_repo.PublishedUsage).Where("u.observed_at_ms >= ? AND u.observed_at_ms < ?", q.StartAtMS, q.EndAtMS)
			if err := repo.usageFilter(t.Context(), q, base).Find(&expected).Error; err != nil {
				t.Fatal(err)
			}
			queries = nil
			var actual []reporting_do.Usage
			if err := repo.streamUsage(t.Context(), q, tc.heatmap, func(row reporting_do.Usage) error {
				actual = append(actual, row)
				return nil
			}); err != nil {
				t.Fatal(err)
			}
			if len(queries) != 2 || strings.Contains(queries[0], "INDEX(u") || strings.Contains(queries[1], "INDEX(u idx_usage_versions_time)") != tc.indexed {
				t.Fatal("unexpected access path", queries)
			}
			for _, sql := range queries {
				if strings.Contains(sql, "USE_HASH(u)") != tc.heatmap {
					t.Fatal("annual strategy changed", sql)
				}
			}
			normalize := func(rows []reporting_do.Usage) []string {
				out := make([]string, 0, len(rows))
				for _, row := range rows {
					row.ContributionID, row.Position = "", 0
					b, err := json.Marshal(row)
					if err != nil {
						t.Fatal(err)
					}
					out = append(out, string(b))
				}
				slices.Sort(out)
				return out
			}
			if !reflect.DeepEqual(normalize(actual), normalize(expected)) {
				t.Fatal("published fact multiset changed")
			}
		})
	}
}
