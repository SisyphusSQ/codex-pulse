package store

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"
	"testing"

	storesqlite "github.com/SisyphusSQ/codex-pulse/internal/store/sqlite"
)

// 测试 EnsureApplicationSchema 在空库中原子创建全部 core/runtime STRICT 表。
func TestEnsureApplicationSchemaCreatesStrictRuntimeTables(t *testing.T) {
	t.Parallel()

	database := openTestDatabase(t)
	if err := NewRepository(database).EnsureApplicationSchema(context.Background()); err != nil {
		t.Fatalf("EnsureApplicationSchema() error = %v", err)
	}

	wantTables := []string{
		"health_events",
		"job_runs",
		"model_prices",
		"pricing_versions",
		"projects",
		"session_current",
		"session_usage_current",
		"sessions",
		"source_attempts",
		"source_files",
		"source_state",
		"turn_usage",
		"turns",
	}
	gotTables, strictByTable, err := applicationTableContract(context.Background(), database)
	if err != nil {
		t.Fatalf("inspect application schema: %v", err)
	}
	if !equalStrings(gotTables, wantTables) {
		t.Fatalf("tables = %v, want %v", gotTables, wantTables)
	}
	for _, table := range wantTables {
		if !strictByTable[table] {
			t.Errorf("table %q is not STRICT", table)
		}
	}
}

// 测试 Runtime Schema 的列、外键、整数价格和索引与冻结 contract 一致。
func TestRuntimeSchemaColumnsForeignKeysAndIndexes(t *testing.T) {
	t.Parallel()

	database := openTestDatabase(t)
	if err := NewRepository(database).EnsureApplicationSchema(context.Background()); err != nil {
		t.Fatalf("EnsureApplicationSchema() error = %v", err)
	}

	wantColumns := map[string][]string{
		"source_files": {
			"source_file_id", "provider", "session_id", "current_path", "device_id",
			"inode", "size_bytes", "mtime_ns", "parsed_offset", "parser_version",
			"active_generation", "state", "last_scanned_at_ms", "last_error_class", "updated_at_ms",
		},
		"source_state": {
			"source_instance_id", "source_type", "scope_key", "last_attempt_at_ms",
			"last_success_at_ms", "next_due_at_ms", "consecutive_failures",
			"last_error_class", "freshness_state", "cursor_version", "updated_at_ms",
		},
		"source_attempts": {
			"request_id", "source_instance_id", "started_at_ms", "finished_at_ms",
			"outcome", "http_status", "error_class", "payload_sha256",
		},
		"job_runs": {
			"job_id", "job_type", "requested_by", "priority", "state", "phase",
			"source_file_id", "resume_of_job_id", "created_at_ms", "started_at_ms",
			"finished_at_ms", "progress_current", "progress_total", "resume_generation", "resume_offset",
			"error_class", "updated_at_ms",
		},
		"health_events": {
			"event_id", "fingerprint", "domain", "severity", "code", "source_file_id",
			"job_id", "error_class", "first_seen_at_ms", "last_seen_at_ms",
			"resolved_at_ms", "occurrence_count", "updated_at_ms",
		},
		"pricing_versions": {
			"pricing_version", "source", "currency", "effective_from_ms", "created_at_ms",
		},
		"model_prices": {
			"pricing_version", "match_kind", "model_pattern", "priority",
			"input_micros_per_million", "cached_input_micros_per_million",
			"output_micros_per_million",
		},
	}
	wantForeignKeys := []string{
		"health_events.job_id->job_runs.job_id/SET NULL",
		"health_events.source_file_id->source_files.source_file_id/SET NULL",
		"job_runs.resume_of_job_id->job_runs.job_id/SET NULL",
		"job_runs.source_file_id->source_files.source_file_id/SET NULL",
		"model_prices.pricing_version->pricing_versions.pricing_version/CASCADE",
		"source_attempts.source_instance_id->source_state.source_instance_id/CASCADE",
		"source_files.session_id->sessions.session_id/SET NULL",
	}
	wantIndexes := []string{
		"idx_health_events_active",
		"idx_health_events_history",
		"idx_health_events_job",
		"idx_health_events_relation",
		"idx_health_events_severity",
		"idx_job_runs_all",
		"idx_job_runs_source_history",
		"idx_job_runs_state_queue",
		"idx_model_prices_match",
		"idx_pricing_versions_effective",
		"idx_source_attempts_history",
		"idx_source_files_session_state",
		"idx_source_state_due",
	}

	gotColumns := make(map[string][]string)
	var gotForeignKeys []string
	var gotIndexes []string
	columnTypes := make(map[string]string)
	err := database.View(context.Background(), func(ctx context.Context, connection storesqlite.ReadConn) error {
		for table := range wantColumns {
			rows, err := connection.QueryContext(ctx, `
				SELECT name, type FROM pragma_table_info(?) ORDER BY cid
			`, table)
			if err != nil {
				return err
			}
			for rows.Next() {
				var name, columnType string
				if err := rows.Scan(&name, &columnType); err != nil {
					rows.Close()
					return err
				}
				gotColumns[table] = append(gotColumns[table], name)
				columnTypes[table+"."+name] = columnType
			}
			if err := rows.Close(); err != nil {
				return err
			}
			if err := rows.Err(); err != nil {
				return err
			}

			foreignRows, err := connection.QueryContext(ctx, `
				SELECT "from", "table", "to", on_delete FROM pragma_foreign_key_list(?)
			`, table)
			if err != nil {
				return err
			}
			for foreignRows.Next() {
				var fromColumn, parentTable, parentColumn, onDelete string
				if err := foreignRows.Scan(&fromColumn, &parentTable, &parentColumn, &onDelete); err != nil {
					foreignRows.Close()
					return err
				}
				gotForeignKeys = append(gotForeignKeys, fmt.Sprintf(
					"%s.%s->%s.%s/%s", table, fromColumn, parentTable, parentColumn, onDelete,
				))
			}
			if err := foreignRows.Close(); err != nil {
				return err
			}
			if err := foreignRows.Err(); err != nil {
				return err
			}
		}

		rows, err := connection.QueryContext(ctx, `
			SELECT name FROM sqlite_schema
			WHERE type = 'index' AND name LIKE 'idx_%'
			  AND name NOT IN (
				'idx_turns_source_position', 'idx_turns_session_lifecycle',
				'idx_turns_project_time', 'idx_turns_model_time',
				'idx_session_current_activity', 'idx_turn_usage_observed_final'
			  )
			ORDER BY name
		`)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var name string
			if err := rows.Scan(&name); err != nil {
				return err
			}
			gotIndexes = append(gotIndexes, name)
		}
		return rows.Err()
	})
	if err != nil {
		t.Fatalf("inspect runtime schema: %v", err)
	}

	for table, want := range wantColumns {
		if got := gotColumns[table]; !equalStrings(got, want) {
			t.Errorf("%s columns = %v, want %v", table, got, want)
		}
	}
	sort.Strings(gotForeignKeys)
	if !equalStrings(gotForeignKeys, wantForeignKeys) {
		t.Errorf("foreign keys = %v, want %v", gotForeignKeys, wantForeignKeys)
	}
	if !equalStrings(gotIndexes, wantIndexes) {
		t.Errorf("indexes = %v, want %v", gotIndexes, wantIndexes)
	}
	for _, column := range []string{
		"model_prices.priority",
		"model_prices.input_micros_per_million",
		"model_prices.cached_input_micros_per_million",
		"model_prices.output_micros_per_million",
		"pricing_versions.effective_from_ms",
	} {
		if columnTypes[column] != "INTEGER" {
			t.Errorf("%s type = %q, want INTEGER", column, columnTypes[column])
		}
	}
}

// 测试 required indexes 被真实运行查询的 SQLite query planner 选中。
func TestRuntimeSchemaRequiredIndexesServeQueries(t *testing.T) {
	t.Parallel()

	database := openTestDatabase(t)
	if err := NewRepository(database).EnsureApplicationSchema(context.Background()); err != nil {
		t.Fatalf("EnsureApplicationSchema() error = %v", err)
	}

	type queryPlanCase struct {
		index     string
		statement string
		arguments []any
	}
	queued := JobQueued
	sourceFileID := "file-a"
	jobID := "job-a"
	active := true
	inactive := false
	warning := HealthWarning
	jobStateQuery, jobStateArguments := buildJobRunsQuery(JobRunFilter{State: &queued}, 10)
	jobSourceQuery, jobSourceArguments := buildJobRunsQuery(JobRunFilter{SourceFileID: &sourceFileID}, 10)
	jobAllQuery, jobAllArguments := buildJobRunsQuery(JobRunFilter{}, 10)
	healthActiveQuery, healthActiveArguments := buildHealthEventsQuery(HealthEventFilter{Active: &active}, 10)
	healthInactiveQuery, healthInactiveArguments := buildHealthEventsQuery(HealthEventFilter{Active: &inactive}, 10)
	healthSeverityQuery, healthSeverityArguments := buildHealthEventsQuery(HealthEventFilter{Severity: &warning}, 10)
	healthSourceQuery, healthSourceArguments := buildHealthEventsQuery(
		HealthEventFilter{SourceFileID: &sourceFileID}, 10,
	)
	healthJobQuery, healthJobArguments := buildHealthEventsQuery(HealthEventFilter{JobID: &jobID}, 10)
	queries := []queryPlanCase{
		{"idx_source_files_session_state", listSourceFilesBySessionStateQuery, []any{"session-a", "active", 10}},
		{"idx_source_state_due", listDueSourcesQuery, []any{100, 10}},
		{"idx_source_attempts_history", listSourceAttemptsQuery, []any{"source-a", 10}},
		{"idx_job_runs_state_queue", jobStateQuery, jobStateArguments},
		{"idx_job_runs_source_history", jobSourceQuery, jobSourceArguments},
		{"idx_job_runs_all", jobAllQuery, jobAllArguments},
		{"idx_health_events_active", healthActiveQuery, healthActiveArguments},
		{"idx_health_events_history", healthInactiveQuery, healthInactiveArguments},
		{"idx_health_events_severity", healthSeverityQuery, healthSeverityArguments},
		{"idx_health_events_relation", healthSourceQuery, healthSourceArguments},
		{"idx_health_events_job", healthJobQuery, healthJobArguments},
		{"idx_pricing_versions_effective", effectivePricingVersionQuery, []any{"builtin", "USD", 100}},
		{"idx_model_prices_match", pricingVersionModelsQuery, []any{"pricing-a"}},
	}

	err := database.View(context.Background(), func(ctx context.Context, connection storesqlite.ReadConn) error {
		for _, query := range queries {
			rows, err := connection.QueryContext(ctx, "EXPLAIN QUERY PLAN "+query.statement, query.arguments...)
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
			if !strings.Contains(plan, query.index) {
				t.Errorf("query plan = %q, want %s", plan, query.index)
			}
			if strings.Contains(plan, "USE TEMP B-TREE") {
				t.Errorf("query plan = %q, want no temporary ordering for %s", plan, query.index)
			}
		}
		return nil
	})
	if err != nil {
		t.Fatalf("inspect runtime query plans: %v", err)
	}
}

// 测试 Runtime Schema 不提供原始错误、鉴权材料或会话正文列。
func TestRuntimeSchemaExcludesSensitiveContentColumns(t *testing.T) {
	t.Parallel()

	database := openTestDatabase(t)
	if err := NewRepository(database).EnsureApplicationSchema(context.Background()); err != nil {
		t.Fatalf("EnsureApplicationSchema() error = %v", err)
	}

	forbiddenFragments := []string{
		"token", "cookie", "authorization", "raw_error", "error_message", "error_detail",
		"stack", "prompt", "response", "tool_output", "jsonl", "payload_body",
	}
	err := database.View(context.Background(), func(ctx context.Context, connection storesqlite.ReadConn) error {
		rows, err := connection.QueryContext(ctx, `
			SELECT m.name, p.name
			FROM sqlite_schema AS m
			JOIN pragma_table_info(m.name) AS p
			WHERE m.type = 'table' AND m.name IN (
				'source_files', 'source_state', 'source_attempts', 'job_runs',
				'health_events', 'pricing_versions', 'model_prices'
			)
		`)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var table, column string
			if err := rows.Scan(&table, &column); err != nil {
				return err
			}
			for _, fragment := range forbiddenFragments {
				if strings.Contains(strings.ToLower(column), fragment) {
					t.Errorf("sensitive content column found: %s.%s matches %q", table, column, fragment)
				}
			}
		}
		return rows.Err()
	})
	if err != nil {
		t.Fatalf("inspect runtime privacy columns: %v", err)
	}
}

// 测试 Runtime Schema 在绕过 Repository 时仍拒绝非 allowlisted class 与 REAL 价格。
func TestRuntimeSchemaRejectsInvalidClassesAndPriceTypesWithoutRepository(t *testing.T) {
	t.Parallel()

	database := openTestDatabase(t)
	if err := NewRepository(database).EnsureApplicationSchema(context.Background()); err != nil {
		t.Fatalf("EnsureApplicationSchema() error = %v", err)
	}
	err := database.Write(context.Background(), func(ctx context.Context, transaction storesqlite.WriteTx) error {
		if _, err := transaction.ExecContext(ctx, `
			INSERT INTO source_state VALUES (
				'source-valid', 'quota', 'default', NULL, NULL, NULL, 0, NULL, 'unknown', 0, 0
			)
		`); err != nil {
			return err
		}
		_, err := transaction.ExecContext(ctx, `
			INSERT INTO pricing_versions VALUES ('pricing-valid', 'builtin', 'USD', 0, 0)
		`)
		return err
	})
	if err != nil {
		t.Fatalf("create direct constraint fixtures: %v", err)
	}

	cases := []struct {
		name      string
		statement string
	}{
		{
			name: "source file raw error class",
			statement: `INSERT INTO source_files VALUES (
				'file-invalid', 'codex', NULL, '/synthetic/file', 'device', 1,
				0, 0, 0, 'v1', 0, 'failed', NULL, 'raw-sensitive-text', 0
			)`,
		},
		{
			name: "source state raw error class",
			statement: `INSERT INTO source_state VALUES (
				'source-invalid', 'quota', 'invalid', NULL, NULL, NULL, 1,
				'raw-sensitive-text', 'stale', 0, 0
			)`,
		},
		{
			name: "source attempt raw error class",
			statement: `INSERT INTO source_attempts VALUES (
				'attempt-invalid', 'source-valid', 0, 1, 'failed', NULL,
				'raw-sensitive-text', NULL
			)`,
		},
		{
			name: "source attempt non-digest payload",
			statement: `INSERT INTO source_attempts VALUES (
				'attempt-payload-invalid', 'source-valid', 0, 1, 'succeeded', NULL,
				NULL, 'sk-proj-ABC123'
			)`,
		},
		{
			name: "job raw error class",
			statement: `INSERT INTO job_runs VALUES (
				'job-invalid', 'scan', 'test', 0, 'failed', 'discover', NULL, NULL,
				0, 0, 1, NULL, NULL, NULL, NULL, 'raw-sensitive-text', 1
			)`,
		},
		{
			name: "health raw error class",
			statement: `INSERT INTO health_events VALUES (
				'health-invalid', 'aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa',
				'store', 'error', 'store.error',
				NULL, NULL, 'raw-sensitive-text', 0, 0, NULL, 1, 0
			)`,
		},
		{
			name: "health identifier-shaped token code",
			statement: `INSERT INTO health_events VALUES (
				'health-token-code', 'bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb',
				'store', 'error', 'sk.proj_abc123',
				NULL, NULL, NULL, 0, 0, NULL, 1, 0
			)`,
		},
		{
			name: "health domain code mismatch",
			statement: `INSERT INTO health_events VALUES (
				'health-domain-code', 'cccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccc',
				'store', 'error', 'source.timeout',
				NULL, NULL, NULL, 0, 0, NULL, 1, 0
			)`,
		},
		{
			name: "job opaque cursor text",
			statement: `INSERT INTO job_runs VALUES (
				'job-token-cursor', 'scan', 'test', 0, 'queued', 'discover', NULL, NULL,
				0, NULL, NULL, NULL, NULL, 'session_id=abc;token=secret', 0, NULL, 0
			)`,
		},
		{
			name: "real model price",
			statement: `INSERT INTO model_prices VALUES (
				'pricing-valid', 'default', '*', 0, 1.5, NULL, NULL
			)`,
		},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			err := database.Write(context.Background(), func(ctx context.Context, transaction storesqlite.WriteTx) error {
				_, err := transaction.ExecContext(ctx, testCase.statement)
				return err
			})
			if err == nil {
				t.Fatal("direct invalid runtime write succeeded")
			}
		})
	}
}

// 测试 Application Schema 遇到不兼容 runtime object 时回滚本轮 core DDL。
func TestEnsureApplicationSchemaRejectsIncompatibleRuntimeTableAtomically(t *testing.T) {
	t.Parallel()

	database := openTestDatabase(t)
	err := database.Write(context.Background(), func(ctx context.Context, transaction storesqlite.WriteTx) error {
		_, err := transaction.ExecContext(ctx, `CREATE TABLE source_files (source_file_id TEXT PRIMARY KEY) STRICT`)
		return err
	})
	if err != nil {
		t.Fatalf("create incompatible runtime table: %v", err)
	}

	err = NewRepository(database).EnsureApplicationSchema(context.Background())
	if !errors.Is(err, ErrSchemaContract) {
		t.Fatalf("EnsureApplicationSchema() error = %v, want ErrSchemaContract", err)
	}

	gotTables, _, err := applicationTableContract(context.Background(), database)
	if err != nil {
		t.Fatalf("inspect tables after failed application bootstrap: %v", err)
	}
	if want := []string{"source_files"}; !equalStrings(gotTables, want) {
		t.Fatalf("tables after failed bootstrap = %v, want %v", gotTables, want)
	}
}

func applicationTableContract(
	ctx context.Context,
	database *storesqlite.Store,
) ([]string, map[string]bool, error) {
	var tables []string
	strictByTable := make(map[string]bool)
	err := database.View(ctx, func(ctx context.Context, connection storesqlite.ReadConn) error {
		rows, err := connection.QueryContext(ctx, `
			SELECT name, strict
			FROM pragma_table_list
			WHERE schema = 'main' AND type = 'table' AND name NOT LIKE 'sqlite_%'
			ORDER BY name
		`)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var name string
			var strict int
			if err := rows.Scan(&name, &strict); err != nil {
				return err
			}
			tables = append(tables, name)
			strictByTable[name] = strict == 1
		}
		return rows.Err()
	})
	return tables, strictByTable, err
}
