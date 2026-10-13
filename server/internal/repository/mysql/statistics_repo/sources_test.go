package statistics_repo

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"
	"testing"

	"gorm.io/gorm"

	reporting_do "github.com/SisyphusSQ/codex-pulse/server/internal/models/do/mysql/reporting_do"
	statistics_dto "github.com/SisyphusSQ/codex-pulse/server/internal/models/dto/statistics_dto"
	reporting_repo "github.com/SisyphusSQ/codex-pulse/server/internal/repository/mysql/reporting_repo"
	centerfixture "github.com/SisyphusSQ/codex-pulse/server/internal/testsupport/center"
)

func TestStreamSourcesProductionScaleKeepsSessionGroupsAndBoundsQueries(t *testing.T) {
	engine := centerfixture.Engine(t)
	db := engine.DB(t.Context())
	var sessions []reporting_do.Session
	var sources []reporting_do.SessionSource
	for i := range 3079 {
		key := fmt.Sprintf("session-%04d", i/3)
		if i%3 == 0 {
			sessions = append(sessions, reporting_do.Session{ID: key, Provider: "codex"})
		}
		// ID 顺序故意将同会话的不同来源分散，不能用 ID 顺序代替会话分组。
		sources = append(sources, reporting_do.SessionSource{ID: fmt.Sprintf("source-%d-%04d", i%3, i/3), SessionKey: key, ClientID: "collector", Payload: "{}"})
	}
	if err := db.CreateInBatches(sessions, 100).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.CreateInBatches(sources, 100).Error; err != nil {
		t.Fatal(err)
	}
	queries := 0
	if err := db.Callback().Query().After("gorm:query").Register("count_sources", func(tx *gorm.DB) {
		if tx.Statement.Table == "pulse_session_sources" || tx.Statement.Table == "src" {
			queries++
		}
	}); err != nil {
		t.Fatal(err)
	}
	if err := db.Callback().Row().After("gorm:row").Register("count_source_streams", func(tx *gorm.DB) {
		if tx.Statement.Table == "pulse_session_sources" || tx.Statement.Table == "src" {
			queries++
		}
	}); err != nil {
		t.Fatal(err)
	}
	slices.SortFunc(sources, func(a, b reporting_do.SessionSource) int {
		if n := strings.Compare(a.SessionKey, b.SessionKey); n != 0 {
			return n
		}
		return strings.Compare(a.ID, b.ID)
	})
	repo := NewStatistics(engine)
	got := 0
	err := repo.Snapshot(t.Context(), func(ctx context.Context) error {
		return repo.StreamSources(ctx, statistics_dto.StatisticsQuery{Provider: "codex"}, func(row reporting_do.SessionSource) error {
			if got >= len(sources) || row.ID != sources[got].ID || row.Payload != "{}" {
				t.Fatalf("source ordering/payload changed at %d", got)
			}
			got++
			return nil
		})
	})
	if err != nil || got != len(sources) {
		t.Fatal("incomplete scan", got, err)
	}
	if queries < 1 || queries > 2 {
		t.Fatalf("production-scale scan used %d source queries", queries)
	}
	t.Logf("%d sources in %d queries", got, queries)
}

func TestStreamSourcesStreamsInlineAndResolvesPartsInSnapshot(t *testing.T) {
	engine := centerfixture.Engine(t)
	db := engine.DB(t.Context())
	if err := db.Create(&reporting_do.Session{ID: "session", Provider: "codex"}).Error; err != nil {
		t.Fatal(err)
	}
	// 七份约 3 MiB 的内联来源逐行读取，分片在同一有序流中读出并校验。
	payload := strings.Repeat("界", 1<<20)
	ref, err := reporting_repo.NewReporting(engine).StagePayload(t.Context(), payload, true, false)
	if err != nil {
		t.Fatal(err)
	}
	for i := range 8 {
		value := payload
		if i == 7 {
			value = ref
		}
		if err := reporting_repo.NewReporting(engine).SaveSource(t.Context(), reporting_do.SessionSource{ID: fmt.Sprint(i), SessionKey: "session", ClientID: "collector", Payload: value}); err != nil {
			t.Fatal(err)
		}
	}
	pages := 0
	if err := db.Callback().Row().After("gorm:row").Register("count_payload_streams", func(tx *gorm.DB) {
		if tx.Statement.Table == "pulse_session_sources" || tx.Statement.Table == "src" {
			pages++
		}
	}); err != nil {
		t.Fatal(err)
	}
	repo := NewStatistics(engine)
	got := 0
	err = repo.Snapshot(t.Context(), func(ctx context.Context) error {
		return repo.StreamSources(ctx, statistics_dto.StatisticsQuery{ClientID: "collector"}, func(row reporting_do.SessionSource) error {
			if row.Payload != payload {
				t.Fatal("segmented/inline payload changed")
			}
			got++
			return nil
		})
	})
	if err != nil || got != 8 || pages != 1 {
		t.Fatal("streamed scan", got, pages, err)
	}
	stop := errors.New("visitor stopped")
	err = repo.Snapshot(t.Context(), func(ctx context.Context) error {
		return repo.StreamSources(ctx, statistics_dto.StatisticsQuery{}, func(reporting_do.SessionSource) error { return stop })
	})
	if !errors.Is(err, stop) {
		t.Fatal("visitor error lost", err)
	}
	if err := db.Model(&reporting_do.PayloadPart{}).Where("part_index = ?", 0).Update("body", []byte(strings.Repeat("x", 256<<10))).Error; err != nil {
		t.Fatal(err)
	}
	err = repo.Snapshot(t.Context(), func(ctx context.Context) error {
		return repo.StreamSources(ctx, statistics_dto.StatisticsQuery{}, func(reporting_do.SessionSource) error { return nil })
	})
	if err == nil || !strings.Contains(err.Error(), "stored snapshot digest mismatch") {
		t.Fatal("corrupt streamed parts were accepted", err)
	}
	if err := db.Exec("DELETE FROM pulse_payload_parts").Error; err != nil {
		t.Fatal(err)
	}
	err = repo.Snapshot(t.Context(), func(ctx context.Context) error {
		return repo.StreamSources(ctx, statistics_dto.StatisticsQuery{}, func(reporting_do.SessionSource) error { return nil })
	})
	if err == nil || !strings.Contains(err.Error(), "missing stored snapshot parts") {
		t.Fatal("broken manifest was accepted", err)
	}
}
