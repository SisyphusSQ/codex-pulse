package retention

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"gorm.io/gorm"

	storesqlite "github.com/SisyphusSQ/codex-pulse/internal/store/sqlite"
)

func TestCandidateQueriesUseDedicatedIndexes(t *testing.T) {
	t.Parallel()

	dataDirectory := filepath.Join(t.TempDir(), "data")
	if err := os.Mkdir(dataDirectory, 0o700); err != nil {
		t.Fatalf("create SQLite data directory: %v", err)
	}
	database, err := storesqlite.Open(
		context.Background(),
		storesqlite.Config{Path: filepath.Join(dataDirectory, "retention-query-plan.db")},
	)
	if err != nil {
		t.Fatalf("open SQLite store: %v", err)
	}
	t.Cleanup(func() { _ = database.Close(context.Background()) })
	if err := database.Write(context.Background(), func(ctx context.Context, transaction *gorm.DB) error {
		for _, statement := range []string{
			`CREATE TABLE app_runtime_samples (
				captured_at_ms INTEGER PRIMARY KEY,
				payload TEXT NOT NULL DEFAULT ''
			) STRICT`,
			`CREATE INDEX idx_app_runtime_samples_retention ON app_runtime_samples(captured_at_ms)`,
			`CREATE TABLE source_attempts (request_id TEXT PRIMARY KEY, finished_at_ms INTEGER NOT NULL) STRICT`,
			`CREATE TABLE health_events (
				event_id TEXT PRIMARY KEY,
				job_id TEXT,
				resolved_at_ms INTEGER
			) STRICT`,
			`CREATE INDEX idx_health_events_job ON health_events(job_id, resolved_at_ms)`,
			`CREATE TABLE job_runs (
				job_id TEXT PRIMARY KEY,
				state TEXT NOT NULL,
				finished_at_ms INTEGER,
				resume_of_job_id TEXT
			) STRICT`,
		} {
			if err := transaction.WithContext(ctx).Exec(statement).Error; err != nil {
				return err
			}
		}
		for _, object := range SchemaObjects() {
			if err := transaction.WithContext(ctx).Exec(object.Statement).Error; err != nil {
				return err
			}
		}
		return nil
	}); err != nil {
		t.Fatalf("create retention query schema: %v", err)
	}

	cutoffMS := int64(100)
	testCases := []struct {
		name    string
		query   func(*gorm.DB) *gorm.DB
		indexes []string
	}{
		{
			name: "runtime samples",
			query: func(connection *gorm.DB) *gorm.DB {
				var timestamps []int64
				return expiredRuntimeSamples(connection, cutoffMS).
					Order("captured_at_ms").Limit(100).Pluck("captured_at_ms", &timestamps)
			},
			indexes: []string{"idx_app_runtime_samples_retention"},
		},
		{
			name: "health events",
			query: func(connection *gorm.DB) *gorm.DB {
				var ids []string
				return expiredHealthEvents(connection, cutoffMS).
					Order("resolved_at_ms, event_id").Limit(100).Pluck("event_id", &ids)
			},
			indexes: []string{"idx_health_events_retention"},
		},
		{
			name: "job runs and references",
			query: func(connection *gorm.DB) *gorm.DB {
				var ids []string
				return expiredJobRunsForState(connection, cutoffMS, "succeeded").
					Order("finished_at_ms, job_id").Limit(100).Pluck("job_id", &ids)
			},
			indexes: []string{
				"idx_job_runs_retention", "idx_health_events_job", "idx_job_runs_resume_lineage",
			},
		},
		{
			name: "source attempts",
			query: func(connection *gorm.DB) *gorm.DB {
				var ids []string
				return expiredSourceAttempts(connection, cutoffMS).
					Order("finished_at_ms, request_id").Limit(100).Pluck("request_id", &ids)
			},
			indexes: []string{"idx_source_attempts_retention"},
		},
	}

	if err := database.View(context.Background(), func(ctx context.Context, connection *gorm.DB) error {
		for _, testCase := range testCases {
			dryRun := testCase.query(connection.Session(&gorm.Session{DryRun: true, Context: ctx}))
			if dryRun.Error != nil {
				return dryRun.Error
			}
			rows, err := connection.WithContext(ctx).
				Raw("EXPLAIN QUERY PLAN "+dryRun.Statement.SQL.String(), dryRun.Statement.Vars...).
				Rows()
			if err != nil {
				return err
			}
			var details []string
			for rows.Next() {
				var detail string
				if err := rows.Scan(new(int), new(int), new(int), &detail); err != nil {
					rows.Close()
					return err
				}
				details = append(details, detail)
			}
			if err := rows.Close(); err != nil {
				return err
			}
			plan := strings.Join(details, "; ")
			for _, index := range testCase.indexes {
				if !strings.Contains(plan, index) {
					t.Errorf("%s query plan = %q, want %s", testCase.name, plan, index)
				}
			}
			if strings.Contains(plan, "USE TEMP B-TREE") {
				t.Errorf("%s query plan = %q, want no temporary ordering", testCase.name, plan)
			}
		}
		return nil
	}); err != nil {
		t.Fatalf("inspect retention query plans: %v", err)
	}
}
