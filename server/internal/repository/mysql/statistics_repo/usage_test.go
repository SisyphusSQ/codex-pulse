package statistics_repo

import (
	"context"
	"reflect"
	"testing"

	reporting_do "github.com/SisyphusSQ/codex-pulse/server/internal/models/do/mysql/reporting_do"
	statistics_dto "github.com/SisyphusSQ/codex-pulse/server/internal/models/dto/statistics_dto"
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
