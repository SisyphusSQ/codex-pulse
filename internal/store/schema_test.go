package store

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	storesqlite "github.com/SisyphusSQ/codex-pulse/internal/store/sqlite"
)

// 测试 EnsureCoreSchema 在空数据库场景下创建全部 STRICT 核心表。
func TestEnsureCoreSchemaCreatesStrictCoreTables(t *testing.T) {
	t.Parallel()

	database := openTestDatabase(t)
	repository := NewRepository(database)
	if err := repository.EnsureCoreSchema(context.Background()); err != nil {
		t.Fatalf("EnsureCoreSchema() error = %v", err)
	}

	wantTables := []string{
		"projects",
		"session_current",
		"session_usage_current",
		"sessions",
		"turn_usage",
		"turns",
	}
	var gotTables []string
	strictByTable := make(map[string]bool)
	err := database.View(context.Background(), func(ctx context.Context, connection storesqlite.ReadConn) error {
		rows, err := rawQueryRows(ctx, connection, `
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
			gotTables = append(gotTables, name)
			strictByTable[name] = strict == 1
		}
		return rows.Err()
	})
	if err != nil {
		t.Fatalf("inspect schema: %v", err)
	}
	sort.Strings(gotTables)
	if !equalStrings(gotTables, wantTables) {
		t.Fatalf("tables = %v, want %v", gotTables, wantTables)
	}
	for _, table := range wantTables {
		if !strictByTable[table] {
			t.Errorf("table %q is not STRICT", table)
		}
	}
}

// 测试 Core Schema 的列、外键和查询索引与冻结 contract 一致。
func TestCoreSchemaColumnsForeignKeysAndIndexes(t *testing.T) {
	t.Parallel()

	database := openTestDatabase(t)
	if err := NewRepository(database).EnsureCoreSchema(context.Background()); err != nil {
		t.Fatalf("EnsureCoreSchema() error = %v", err)
	}

	wantColumns := map[string][]string{
		"projects": {
			"project_id", "display_name", "root_path", "git_remote_sanitized",
			"created_at_ms", "updated_at_ms",
		},
		"sessions": {
			"session_id", "provider", "originator", "source_kind", "model_provider",
			"initial_cwd", "project_id", "cli_version", "created_at_ms",
			"first_seen_at_ms", "last_seen_at_ms",
		},
		"turns": {
			"turn_id", "session_id", "started_at_ms", "completed_at_ms", "outcome",
			"model", "reasoning_effort", "cwd", "project_id", "source_generation",
			"start_offset", "complete_offset",
		},
		"session_current": {
			"session_id", "thread_name", "thread_name_updated_at_ms", "active_turn_id",
			"current_model", "current_cwd", "last_activity_at_ms", "updated_at_ms",
		},
		"turn_usage": {
			"turn_id", "observed_at_ms", "is_final", "input_tokens", "cached_input_tokens",
			"output_tokens", "reasoning_tokens", "context_window", "source_generation", "source_offset",
			"confidence", "updated_at_ms",
		},
		"session_usage_current": {
			"session_id", "counter_epoch", "total_input_tokens", "total_cached_tokens",
			"total_output_tokens", "total_reasoning_tokens", "observed_at_ms",
			"source_generation", "source_offset", "counter_state",
		},
	}

	wantForeignKeys := []string{
		"session_current.active_turn_id->turns.turn_id/SET NULL",
		"session_current.session_id->sessions.session_id/CASCADE",
		"session_usage_current.session_id->sessions.session_id/CASCADE",
		"sessions.project_id->projects.project_id/SET NULL",
		"turn_usage.turn_id->turns.turn_id/CASCADE",
		"turns.project_id->projects.project_id/SET NULL",
		"turns.session_id->sessions.session_id/CASCADE",
	}
	wantIndexes := []string{
		"idx_session_current_activity",
		"idx_turn_usage_observed_final",
		"idx_turns_model_time",
		"idx_turns_project_time",
		"idx_turns_session_lifecycle",
		"idx_turns_source_position",
	}
	integerColumns := stringSet(
		"projects.created_at_ms", "projects.updated_at_ms",
		"sessions.created_at_ms", "sessions.first_seen_at_ms", "sessions.last_seen_at_ms",
		"turns.started_at_ms", "turns.completed_at_ms", "turns.source_generation",
		"turns.start_offset", "turns.complete_offset",
		"session_current.thread_name_updated_at_ms", "session_current.last_activity_at_ms",
		"session_current.updated_at_ms",
		"turn_usage.observed_at_ms", "turn_usage.is_final", "turn_usage.input_tokens",
		"turn_usage.cached_input_tokens", "turn_usage.output_tokens", "turn_usage.reasoning_tokens",
		"turn_usage.context_window", "turn_usage.source_generation", "turn_usage.source_offset",
		"turn_usage.updated_at_ms",
		"session_usage_current.counter_epoch", "session_usage_current.total_input_tokens",
		"session_usage_current.total_cached_tokens", "session_usage_current.total_output_tokens",
		"session_usage_current.total_reasoning_tokens", "session_usage_current.observed_at_ms",
		"session_usage_current.source_generation", "session_usage_current.source_offset",
	)
	notNullColumns := stringSet(
		"projects.project_id", "projects.display_name", "projects.root_path",
		"projects.created_at_ms", "projects.updated_at_ms",
		"sessions.session_id", "sessions.provider", "sessions.source_kind",
		"sessions.created_at_ms", "sessions.first_seen_at_ms", "sessions.last_seen_at_ms",
		"turns.turn_id", "turns.session_id", "turns.started_at_ms",
		"turns.source_generation", "turns.start_offset",
		"session_current.session_id", "session_current.updated_at_ms",
		"turn_usage.turn_id", "turn_usage.observed_at_ms", "turn_usage.is_final",
		"turn_usage.source_generation", "turn_usage.source_offset", "turn_usage.confidence",
		"turn_usage.updated_at_ms",
		"session_usage_current.session_id", "session_usage_current.counter_epoch",
		"session_usage_current.observed_at_ms", "session_usage_current.source_generation",
		"session_usage_current.source_offset", "session_usage_current.counter_state",
	)
	primaryKeyColumns := stringSet(
		"projects.project_id", "sessions.session_id", "turns.turn_id",
		"session_current.session_id", "turn_usage.turn_id", "session_usage_current.session_id",
	)

	gotColumns := make(map[string][]string)
	gotDefinitions := make(map[string][]string)
	var gotForeignKeys []string
	var gotIndexes []string
	queryPlans := make(map[string]string)
	var sourceIndexUnique int
	err := database.View(context.Background(), func(ctx context.Context, connection storesqlite.ReadConn) error {
		for table := range wantColumns {
			rows, err := rawQueryRows(ctx, connection, `
				SELECT name, type, "notnull", pk FROM pragma_table_info(?) ORDER BY cid
			`, table)
			if err != nil {
				return err
			}
			for rows.Next() {
				var column, columnType string
				var notNull, primaryKey int
				if err := rows.Scan(&column, &columnType, &notNull, &primaryKey); err != nil {
					rows.Close()
					return err
				}
				gotColumns[table] = append(gotColumns[table], column)
				gotDefinitions[table] = append(
					gotDefinitions[table],
					fmt.Sprintf("%s:%s:%d:%d", column, columnType, notNull, primaryKey),
				)
			}
			if err := rows.Close(); err != nil {
				return err
			}
			if err := rows.Err(); err != nil {
				return err
			}

			foreignRows, err := rawQueryRows(ctx, connection, `
				SELECT "from", "table", "to", on_delete
				FROM pragma_foreign_key_list(?)
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
				gotForeignKeys = append(gotForeignKeys, table+"."+fromColumn+"->"+parentTable+"."+parentColumn+"/"+onDelete)
			}
			if err := foreignRows.Close(); err != nil {
				return err
			}
			if err := foreignRows.Err(); err != nil {
				return err
			}
		}

		indexRows, err := rawQueryRows(ctx, connection, `
			SELECT name
			FROM sqlite_schema
			WHERE type = 'index' AND name LIKE 'idx_%'
			ORDER BY name
		`)
		if err != nil {
			return err
		}
		defer indexRows.Close()
		for indexRows.Next() {
			var index string
			if err := indexRows.Scan(&index); err != nil {
				return err
			}
			gotIndexes = append(gotIndexes, index)
		}
		if err := indexRows.Err(); err != nil {
			return err
		}
		if err := rawQueryRow(ctx, connection, `
			SELECT "unique" FROM pragma_index_list('turns')
			WHERE name = 'idx_turns_source_position'
		`).Scan(&sourceIndexUnique); err != nil {
			return err
		}

		queries := []struct {
			name      string
			statement string
			arguments []any
		}{
			{
				name: "idx_turns_source_position",
				statement: `SELECT turn_id FROM turns
					WHERE session_id = ? AND source_generation = ? AND start_offset = ?`,
				arguments: []any{"session-1", 0, 42},
			},
			{
				name: "idx_turns_session_lifecycle",
				statement: `SELECT t.turn_id FROM turns AS t
					LEFT JOIN turn_usage AS u ON u.turn_id = t.turn_id
					WHERE t.session_id = ? AND t.started_at_ms >= ?
					ORDER BY t.started_at_ms DESC, t.turn_id DESC LIMIT 100`,
				arguments: []any{"session-1", 0},
			},
			{
				name: "idx_turns_project_time",
				statement: `SELECT t.turn_id FROM turns AS t
					LEFT JOIN turn_usage AS u ON u.turn_id = t.turn_id
					WHERE t.project_id = ? AND t.started_at_ms >= ?
					ORDER BY t.started_at_ms DESC, t.turn_id DESC LIMIT 100`,
				arguments: []any{"project-1", 0},
			},
			{
				name: "idx_turns_model_time",
				statement: `SELECT t.turn_id FROM turns AS t
					LEFT JOIN turn_usage AS u ON u.turn_id = t.turn_id
					WHERE t.model = ? AND t.started_at_ms >= ?
					ORDER BY t.started_at_ms DESC, t.turn_id DESC LIMIT 100`,
				arguments: []any{"gpt-5", 0},
			},
			{
				name:      "idx_session_current_activity",
				statement: `SELECT session_id FROM session_current WHERE last_activity_at_ms >= ?`,
				arguments: []any{0},
			},
			{
				name:      "idx_turn_usage_observed_final",
				statement: `SELECT turn_id FROM turn_usage WHERE observed_at_ms >= ? AND is_final = ?`,
				arguments: []any{0, 1},
			},
		}
		for _, query := range queries {
			rows, err := rawQueryRows(ctx, connection, "EXPLAIN QUERY PLAN "+query.statement, query.arguments...)
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
			if err := rows.Err(); err != nil {
				return err
			}
			queryPlans[query.name] = strings.Join(details, "; ")
		}
		return nil
	})
	if err != nil {
		t.Fatalf("inspect core schema: %v", err)
	}

	for table, want := range wantColumns {
		if got := gotColumns[table]; !equalStrings(got, want) {
			t.Errorf("%s columns = %v, want %v", table, got, want)
		}
		wantDefinitions := make([]string, 0, len(want))
		for _, column := range want {
			qualified := table + "." + column
			columnType := "TEXT"
			if integerColumns[qualified] {
				columnType = "INTEGER"
			}
			wantDefinitions = append(
				wantDefinitions,
				fmt.Sprintf(
					"%s:%s:%d:%d",
					column,
					columnType,
					boolInt(notNullColumns[qualified]),
					boolInt(primaryKeyColumns[qualified]),
				),
			)
		}
		if got := gotDefinitions[table]; !equalStrings(got, wantDefinitions) {
			t.Errorf("%s definitions = %v, want %v", table, got, wantDefinitions)
		}
	}
	sort.Strings(gotForeignKeys)
	if !equalStrings(gotForeignKeys, wantForeignKeys) {
		t.Errorf("foreign keys = %v, want %v", gotForeignKeys, wantForeignKeys)
	}
	if !equalStrings(gotIndexes, wantIndexes) {
		t.Errorf("indexes = %v, want %v", gotIndexes, wantIndexes)
	}
	if sourceIndexUnique != 1 {
		t.Errorf("idx_turns_source_position unique = %d, want 1", sourceIndexUnique)
	}
	for _, index := range wantIndexes {
		if plan := queryPlans[index]; !strings.Contains(plan, index) {
			t.Errorf("query plan = %q, want %s", plan, index)
		}
	}
	for _, index := range []string{
		"idx_turns_session_lifecycle",
		"idx_turns_project_time",
		"idx_turns_model_time",
	} {
		if plan := queryPlans[index]; strings.Contains(strings.ToUpper(plan), "TEMP B-TREE") {
			t.Errorf("query plan = %q, want %s without temporary sort", plan, index)
		}
	}
}

// 测试核心 Schema 不包含会持久化会话正文或鉴权材料的字段。
func TestCoreSchemaExcludesPrivateContentColumns(t *testing.T) {
	t.Parallel()

	database := openTestDatabase(t)
	if err := NewRepository(database).EnsureCoreSchema(context.Background()); err != nil {
		t.Fatalf("EnsureCoreSchema() error = %v", err)
	}

	forbiddenFragments := []string{
		"prompt",
		"response",
		"tool_output",
		"raw_json",
		"content",
		"token_secret",
		"access_token",
	}
	err := database.View(context.Background(), func(ctx context.Context, connection storesqlite.ReadConn) error {
		rows, err := rawQueryRows(ctx, connection, `
			SELECT m.name, p.name
			FROM sqlite_schema AS m
			JOIN pragma_table_info(m.name) AS p
			WHERE m.type = 'table' AND m.name NOT LIKE 'sqlite_%'
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
					t.Errorf("private content column found: %s.%s matches %q", table, column, fragment)
				}
			}
		}
		return rows.Err()
	})
	if err != nil {
		t.Fatalf("inspect privacy columns: %v", err)
	}
}

// 测试 EnsureCoreSchema 在既有同名表不兼容场景下拒绝开放并回滚本轮 DDL。
func TestEnsureCoreSchemaRejectsIncompatibleExistingTableAtomically(t *testing.T) {
	t.Parallel()

	database := openTestDatabase(t)
	err := database.Write(context.Background(), func(ctx context.Context, transaction storesqlite.WriteTx) error {
		_, err := rawExec(ctx, transaction, `CREATE TABLE sessions (session_id TEXT PRIMARY KEY) STRICT`)
		return err
	})
	if err != nil {
		t.Fatalf("create incompatible sessions table: %v", err)
	}

	repository := NewRepository(database)
	err = repository.EnsureCoreSchema(context.Background())
	if !errors.Is(err, ErrSchemaContract) {
		t.Fatalf("EnsureCoreSchema() error = %v, want ErrSchemaContract", err)
	}

	gotTables, err := coreTableNames(context.Background(), database)
	if err != nil {
		t.Fatalf("inspect tables after failed bootstrap: %v", err)
	}
	if want := []string{"sessions"}; !equalStrings(gotTables, want) {
		t.Fatalf("tables after failed bootstrap = %v, want %v", gotTables, want)
	}
}

// 测试 malformed table 或同名异类对象在执行依赖 DDL 前稳定返回 ErrSchemaContract。
func TestEnsureCoreSchemaClassifiesMalformedExistingObjects(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name      string
		statement string
		wantType  string
		wantName  string
	}{
		{
			name:      "turns missing indexed columns",
			statement: `CREATE TABLE turns (turn_id TEXT PRIMARY KEY) STRICT`,
			wantType:  "table",
			wantName:  "turns",
		},
		{
			name:      "view collides with projects table",
			statement: `CREATE VIEW projects AS SELECT 'project-a' AS project_id`,
			wantType:  "view",
			wantName:  "projects",
		},
	}

	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			database := openTestDatabase(t)
			err := database.Write(context.Background(), func(ctx context.Context, transaction storesqlite.WriteTx) error {
				_, err := rawExec(ctx, transaction, testCase.statement)
				return err
			})
			if err != nil {
				t.Fatalf("create malformed object: %v", err)
			}

			err = NewRepository(database).EnsureCoreSchema(context.Background())
			if !errors.Is(err, ErrSchemaContract) {
				t.Fatalf("EnsureCoreSchema() error = %v, want ErrSchemaContract", err)
			}

			var gotType string
			var objectCount int
			err = database.View(context.Background(), func(ctx context.Context, connection storesqlite.ReadConn) error {
				if err := rawQueryRow(
					ctx, connection,
					`SELECT type FROM sqlite_schema WHERE name = ?`,
					testCase.wantName,
				).Scan(&gotType); err != nil {
					return err
				}
				return rawQueryRow(ctx, connection, `
					SELECT COUNT(*) FROM sqlite_schema
					WHERE name NOT LIKE 'sqlite_%' AND sql IS NOT NULL
				`).Scan(&objectCount)
			})
			if err != nil {
				t.Fatalf("inspect objects after failed bootstrap: %v", err)
			}
			if gotType != testCase.wantType || objectCount != 1 {
				t.Fatalf("objects after failed bootstrap = (%s, %d), want (%s, 1)", gotType, objectCount, testCase.wantType)
			}
		})
	}
}

// 测试数据库约束本身拒绝非法事实，不依赖 Repository 的提前校验。
func TestCoreSchemaEnforcesConstraintsWithoutRepository(t *testing.T) {
	t.Parallel()

	database := openTestDatabase(t)
	if err := NewRepository(database).EnsureCoreSchema(context.Background()); err != nil {
		t.Fatalf("EnsureCoreSchema() error = %v", err)
	}
	err := database.Write(context.Background(), func(ctx context.Context, transaction storesqlite.WriteTx) error {
		if _, err := rawExec(ctx, transaction, `
			INSERT INTO projects VALUES ('project-a', 'Project', '/workspace', NULL, 0, 0)
		`); err != nil {
			return err
		}
		if _, err := rawExec(ctx, transaction, `
			INSERT INTO sessions VALUES (
				'session-a', 'codex', NULL, 'session_jsonl', NULL, NULL,
				'project-a', NULL, 0, 0, 0
			)
		`); err != nil {
			return err
		}
		_, err := rawExec(ctx, transaction, `
			INSERT INTO turns VALUES (
				'turn-a', 'session-a', 0, NULL, NULL, NULL, NULL, NULL,
				'project-a', 0, 10, NULL
			)
		`)
		return err
	})
	if err != nil {
		t.Fatalf("insert valid constraint fixtures: %v", err)
	}

	cases := []struct {
		name      string
		statement string
	}{
		{
			name: "foreign key",
			statement: `INSERT INTO turns VALUES (
				'turn-missing-session', 'missing', 0, NULL, NULL, NULL, NULL, NULL,
				NULL, 0, 20, NULL
			)`,
		},
		{
			name: "unique source position",
			statement: `INSERT INTO turns VALUES (
				'turn-duplicate', 'session-a', 0, NULL, NULL, NULL, NULL, NULL,
				NULL, 0, 10, NULL
			)`,
		},
		{
			name: "incomplete completion tuple",
			statement: `INSERT INTO turns VALUES (
				'turn-invalid-complete', 'session-a', 0, 10, NULL, NULL, NULL, NULL,
				NULL, 0, 30, 40
			)`,
		},
		{
			name: "invalid token and boolean",
			statement: `INSERT INTO turn_usage VALUES (
				'turn-a', 0, 2, -1, NULL, NULL, NULL, NULL, 0, 40, 'exact', 0
			)`,
		},
		{
			name: "unpaired thread name timestamp",
			statement: `INSERT INTO session_current VALUES (
				'session-a', 'name', NULL, NULL, NULL, NULL, NULL, 0
			)`,
		},
		{
			name: "thread name timestamp after row update",
			statement: `INSERT INTO session_current VALUES (
				'session-a', 'name', 20, NULL, NULL, NULL, NULL, 10
			)`,
		},
		{
			name: "empty project identity",
			statement: `INSERT INTO projects VALUES (
				'', 'name', '/tmp/project', NULL, 0, 0
			)`,
		},
		{
			name: "empty session identity",
			statement: `INSERT INTO sessions VALUES (
				'', 'codex', NULL, 'session_jsonl', NULL, NULL, NULL, NULL, 0, 0, 0
			)`,
		},
		{
			name: "empty turn identity",
			statement: `INSERT INTO turns VALUES (
				'', 'session-a', 0, NULL, NULL, NULL, NULL, NULL, NULL, 0, 50, NULL
			)`,
		},
		{
			name: "empty optional string",
			statement: `INSERT INTO projects VALUES (
				'project-empty-remote', 'name', '/tmp/project', '', 0, 0
			)`,
		},
		{
			name: "empty project display name",
			statement: `INSERT INTO projects VALUES (
				'project-empty-name', '', '/tmp/project', NULL, 0, 0
			)`,
		},
		{
			name: "empty project root path",
			statement: `INSERT INTO projects VALUES (
				'project-empty-root', 'name', '', NULL, 0, 0
			)`,
		},
		{
			name: "empty session provider",
			statement: `INSERT INTO sessions VALUES (
				'session-empty-provider', '', NULL, 'session_jsonl', NULL, NULL, NULL, NULL, 0, 0, 0
			)`,
		},
		{
			name: "empty session source kind",
			statement: `INSERT INTO sessions VALUES (
				'session-empty-source', 'codex', NULL, '', NULL, NULL, NULL, NULL, 0, 0, 0
			)`,
		},
		{
			name: "empty completed turn outcome",
			statement: `INSERT INTO turns VALUES (
				'turn-empty-outcome', 'session-a', 0, 10, '', NULL, NULL, NULL, NULL, 0, 60, 70
			)`,
		},
		{
			name: "empty turn usage confidence",
			statement: `INSERT INTO turn_usage VALUES (
				'turn-a', 0, 0, NULL, NULL, NULL, NULL, NULL, 0, 80, '', 0
			)`,
		},
		{
			name: "empty session counter state",
			statement: `INSERT INTO session_usage_current VALUES (
				'session-a', 0, NULL, NULL, NULL, NULL, 0, 0, 90, ''
			)`,
		},
	}

	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			err := database.Write(context.Background(), func(ctx context.Context, transaction storesqlite.WriteTx) error {
				_, err := rawExec(ctx, transaction, testCase.statement)
				return err
			})
			if err == nil {
				t.Fatal("constraint write succeeded, want rejection")
			}
		})
	}
}

func openTestDatabase(t testing.TB) *storesqlite.Store {
	t.Helper()

	directory := t.TempDir()
	if err := os.Chmod(directory, 0o700); err != nil {
		t.Fatalf("secure temp directory: %v", err)
	}
	database, err := storesqlite.Open(context.Background(), storesqlite.Config{
		Path: filepath.Join(directory, "codex-pulse-test.db"),
	})
	if err != nil {
		t.Fatalf("sqlite.Open() error = %v", err)
	}
	t.Cleanup(func() {
		if err := database.Close(context.Background()); err != nil {
			t.Errorf("Store.Close() error = %v", err)
		}
	})
	return database
}

func coreTableNames(ctx context.Context, database *storesqlite.Store) ([]string, error) {
	var tables []string
	err := database.View(ctx, func(ctx context.Context, connection storesqlite.ReadConn) error {
		rows, err := rawQueryRows(ctx, connection, `
			SELECT name
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
			if err := rows.Scan(&name); err != nil {
				return err
			}
			tables = append(tables, name)
		}
		return rows.Err()
	})
	return tables, err
}

func equalStrings(got, want []string) bool {
	if len(got) != len(want) {
		return false
	}
	for index := range got {
		if got[index] != want[index] {
			return false
		}
	}
	return true
}

func stringSet(values ...string) map[string]bool {
	set := make(map[string]bool, len(values))
	for _, value := range values {
		set[value] = true
	}
	return set
}

func boolInt(value bool) int {
	if value {
		return 1
	}
	return 0
}
