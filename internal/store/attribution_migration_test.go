package store

import (
	"context"
	"testing"

	"gorm.io/gorm"

	"github.com/SisyphusSQ/codex-pulse/internal/attribution"
	storesqlite "github.com/SisyphusSQ/codex-pulse/internal/store/sqlite"
)

func TestApplicationMigrationAppendsAttributionSchemaToFrozenV3(t *testing.T) {
	t.Parallel()

	database := openTestDatabase(t)
	seedApplicationSchemaV3ForAttribution(t, database)
	repository := NewRepository(database)
	ctx := context.Background()
	initialCWD := "/Users/alice/work/migration-backfill"
	currentCWD := initialCWD + "/internal/store"
	rawModel := "OpenAI/GPT-5.2-Codex"
	if err := repository.UpsertFacts(ctx, FactBatch{
		Session: &Session{
			SessionID: "session-v3-backfill", Provider: "codex", SourceKind: "session",
			InitialCWD: &initialCWD, CreatedAtMS: 100, FirstSeenAtMS: 100, LastSeenAtMS: 200,
		},
		Turn: &Turn{
			TurnID: "turn-v3-backfill", SessionID: "session-v3-backfill", StartedAtMS: 200,
			Model: &rawModel, CWD: &currentCWD, SourceGeneration: 0, StartOffset: 10,
		},
		SessionCurrent: &SessionCurrent{
			SessionID: "session-v3-backfill", CurrentModel: &rawModel, CurrentCWD: &currentCWD,
			LastActivityAtMS: int64Pointer254(200), UpdatedAtMS: 200,
		},
	}); err != nil {
		t.Fatalf("seed v3 facts: %v", err)
	}
	var backupVersions [2]int
	runner := applicationMigrationRunnerForTest(database)
	runner.spaceCheck = func(context.Context, string, int64) error { return nil }
	runner.backup = func(
		_ context.Context,
		fromVersion int,
		targetVersion int,
		_ func(storesqlite.BackupProgress),
	) (string, error) {
		backupVersions = [2]int{fromVersion, targetVersion}
		return "/tmp/application-v3-before-v4.db", nil
	}
	report, err := runner.run(context.Background())
	if err != nil {
		t.Fatalf("run() error = %v", err)
	}
	if report.FromVersion != 3 || report.TargetVersion != 4 ||
		!equalInts(report.AppliedVersions, []int{4}) || report.BackupPath == "" {
		t.Fatalf("run() report = %#v, want v3 to v4 with backup", report)
	}
	if backupVersions != [2]int{3, 4} {
		t.Fatalf("backup versions = %v, want [3 4]", backupVersions)
	}
	assertMigrationVersionAndHistory(t, database, 4, 4)

	err = database.View(context.Background(), func(_ context.Context, connection storesqlite.ReadConn) error {
		for _, table := range []string{"session_attributions", "turn_attributions"} {
			if !connection.Migrator().HasTable(table) {
				t.Errorf("v4 table %q missing", table)
			}
		}
		for table, indexes := range map[string][]string{
			"session_attributions": {"idx_session_attributions_project", "idx_session_attributions_model"},
			"turn_attributions":    {"idx_turn_attributions_project", "idx_turn_attributions_model"},
		} {
			for _, index := range indexes {
				if !connection.Migrator().HasIndex(table, index) {
					t.Errorf("v4 index %q missing", index)
				}
			}
		}
		return nil
	})
	if err != nil {
		t.Fatalf("inspect v4 migration: %v", err)
	}
	session, err := repository.SessionAttribution(ctx, "session-v3-backfill")
	if err != nil || session.Project.ProjectID == nil || session.Model.ModelKey == nil ||
		*session.Model.ModelKey != "gpt-5.2-codex" || session.UpdatedAtMS != 200 {
		t.Fatalf("SessionAttribution(backfill) = %#v, %v", session, err)
	}
	turn, err := repository.TurnAttribution(ctx, "turn-v3-backfill")
	if err != nil || turn.Project.ProjectID == nil || turn.Model.ModelKey == nil ||
		*turn.Model.ModelKey != "gpt-5.2-codex" || turn.UpdatedAtMS != 200 {
		t.Fatalf("TurnAttribution(backfill) = %#v, %v", turn, err)
	}
}

func TestApplicationMigrationRollsBackV4WhenAttributionBackfillFails(t *testing.T) {
	t.Parallel()

	database := openTestDatabase(t)
	seedApplicationSchemaV3ForAttribution(t, database)
	ctx := context.Background()
	cwd := "/Users/alice/work/backfill-conflict"
	decision := attribution.ResolveProject(attribution.ProjectInput{CWD: cwd})
	conflictingRoot := "/Users/alice/work/different-root"
	if err := NewRepository(database).UpsertFacts(ctx, FactBatch{
		Project: &Project{
			ProjectID: decision.ProjectID, DisplayName: "Conflicting Root", RootPath: conflictingRoot,
			CreatedAtMS: 100, UpdatedAtMS: 100,
		},
		Session: &Session{
			SessionID: "session-v3-conflict", Provider: "codex", SourceKind: "session",
			InitialCWD: &cwd, ProjectID: &decision.ProjectID,
			CreatedAtMS: 100, FirstSeenAtMS: 100, LastSeenAtMS: 100,
		},
	}); err != nil {
		t.Fatalf("seed conflicting v3 facts: %v", err)
	}
	runner := applicationMigrationRunnerForTest(database)
	runner.spaceCheck = func(context.Context, string, int64) error { return nil }
	runner.backup = func(
		context.Context,
		int,
		int,
		func(storesqlite.BackupProgress),
	) (string, error) {
		return "/tmp/application-v3-before-failed-v4.db", nil
	}
	if _, err := runner.run(ctx); err == nil {
		t.Fatal("run() error = nil, want attribution backfill failure")
	}
	assertMigrationVersionAndHistory(t, database, 3, 3)
	if err := database.View(ctx, func(_ context.Context, connection storesqlite.ReadConn) error {
		if connection.Migrator().HasTable("session_attributions") ||
			connection.Migrator().HasTable("turn_attributions") {
			t.Error("failed v4 attribution schema must roll back")
		}
		return nil
	}); err != nil {
		t.Fatalf("inspect failed v4 migration: %v", err)
	}
}

func TestAttributionSchemaColumnsForeignKeysAndStrictContract(t *testing.T) {
	t.Parallel()

	database := openTestDatabase(t)
	if err := NewRepository(database).EnsureApplicationSchema(context.Background()); err != nil {
		t.Fatalf("EnsureApplicationSchema() error = %v", err)
	}

	wantColumns := map[string][]string{
		"session_attributions": {
			"session_id", "display_title", "title_confidence", "title_source", "title_reason",
			"project_id", "project_display_name", "project_confidence", "project_source", "project_reason",
			"model_key", "model_display_name", "model_confidence", "model_source", "model_reason",
			"rule_version", "updated_at_ms",
		},
		"turn_attributions": {
			"turn_id", "project_id", "project_display_name", "project_confidence", "project_source",
			"project_reason", "model_key", "model_display_name", "model_confidence", "model_source",
			"model_reason", "rule_version", "updated_at_ms",
		},
	}

	err := database.View(context.Background(), func(ctx context.Context, connection storesqlite.ReadConn) error {
		for table, expected := range wantColumns {
			var strict int
			if err := rawQueryRow(ctx, connection,
				`SELECT strict FROM pragma_table_list WHERE schema = 'main' AND name = ?`, table,
			).Scan(&strict); err != nil {
				return err
			}
			if strict != 1 {
				t.Errorf("table %q strict = %d", table, strict)
			}
			rows, err := rawQueryRows(ctx, connection,
				`SELECT name FROM pragma_table_info(?) ORDER BY cid`, table,
			)
			if err != nil {
				return err
			}
			var got []string
			for rows.Next() {
				var column string
				if err := rows.Scan(&column); err != nil {
					rows.Close()
					return err
				}
				got = append(got, column)
			}
			if err := rows.Close(); err != nil {
				return err
			}
			if err := rows.Err(); err != nil {
				return err
			}
			if !equalStrings(got, expected) {
				t.Errorf("%s columns = %v, want %v", table, got, expected)
			}
		}

		foreignKeys := map[string]string{
			"session_attributions": "session_id->sessions.session_id/CASCADE",
			"turn_attributions":    "turn_id->turns.turn_id/CASCADE",
		}
		for table, expected := range foreignKeys {
			rows, err := rawQueryRows(ctx, connection,
				`SELECT "from", "table", "to", on_delete FROM pragma_foreign_key_list(?)`, table,
			)
			if err != nil {
				return err
			}
			found := false
			for rows.Next() {
				var from, parent, to, onDelete string
				if err := rows.Scan(&from, &parent, &to, &onDelete); err != nil {
					rows.Close()
					return err
				}
				if from+"->"+parent+"."+to+"/"+onDelete == expected {
					found = true
				}
			}
			if err := rows.Close(); err != nil {
				return err
			}
			if !found {
				t.Errorf("%s foreign key %q missing", table, expected)
			}
		}
		return nil
	})
	if err != nil {
		t.Fatalf("inspect attribution schema: %v", err)
	}
}

func TestAttributionSchemaRejectsPartialIdentityDisplayTuples(t *testing.T) {
	t.Parallel()

	database := openTestDatabase(t)
	repository := NewRepository(database)
	ctx := context.Background()
	if err := repository.EnsureApplicationSchema(ctx); err != nil {
		t.Fatalf("EnsureApplicationSchema() error = %v", err)
	}
	cwd := "/Users/alice/work/tuple-check"
	model := "gpt-5.2-codex"
	if err := repository.UpsertFacts(ctx, FactBatch{
		Session: &Session{
			SessionID: "session-tuple-check", Provider: "codex", SourceKind: "session",
			InitialCWD: &cwd, CreatedAtMS: 100, FirstSeenAtMS: 100, LastSeenAtMS: 200,
		},
		Turn: &Turn{
			TurnID: "turn-tuple-check", SessionID: "session-tuple-check", StartedAtMS: 200,
			Model: &model, CWD: &cwd, SourceGeneration: 0, StartOffset: 10,
		},
	}); err != nil {
		t.Fatalf("UpsertFacts() error = %v", err)
	}

	for _, test := range []struct {
		name   string
		model  any
		where  string
		id     string
		column string
	}{
		{name: "session project ID only", model: &sessionAttributionModel{}, where: "session_id = ?", id: "session-tuple-check", column: "project_display_name"},
		{name: "session project display only", model: &sessionAttributionModel{}, where: "session_id = ?", id: "session-tuple-check", column: "project_id"},
		{name: "session model key only", model: &sessionAttributionModel{}, where: "session_id = ?", id: "session-tuple-check", column: "model_display_name"},
		{name: "session model display only", model: &sessionAttributionModel{}, where: "session_id = ?", id: "session-tuple-check", column: "model_key"},
		{name: "turn project ID only", model: &turnAttributionModel{}, where: "turn_id = ?", id: "turn-tuple-check", column: "project_display_name"},
		{name: "turn project display only", model: &turnAttributionModel{}, where: "turn_id = ?", id: "turn-tuple-check", column: "project_id"},
		{name: "turn model key only", model: &turnAttributionModel{}, where: "turn_id = ?", id: "turn-tuple-check", column: "model_display_name"},
		{name: "turn model display only", model: &turnAttributionModel{}, where: "turn_id = ?", id: "turn-tuple-check", column: "model_key"},
	} {
		t.Run(test.name, func(t *testing.T) {
			err := database.Write(ctx, func(ctx context.Context, transaction storesqlite.WriteTx) error {
				return transaction.WithContext(ctx).Model(test.model).
					Where(test.where, test.id).
					Update(test.column, gorm.Expr("NULL")).Error
			})
			if err == nil {
				t.Fatal("partial identity/display tuple update succeeded, want CHECK constraint failure")
			}
		})
	}
}

func seedApplicationSchemaV3ForAttribution(t *testing.T, database *storesqlite.Store) {
	t.Helper()
	err := database.Write(context.Background(), func(ctx context.Context, transaction storesqlite.WriteTx) error {
		if err := ensureSchemaObjects(ctx, transaction, migrationSchemaObjects); err != nil {
			return err
		}
		for _, migration := range applicationMigrations[:3] {
			if err := migration.apply(ctx, transaction); err != nil {
				return err
			}
			if err := transaction.WithContext(ctx).Create(&schemaMigrationModel{
				Version: migration.version, Name: migration.name,
				Checksum: migration.checksum, AppliedAtMS: int64(migration.version),
			}).Error; err != nil {
				return err
			}
		}
		return transaction.WithContext(ctx).Exec("PRAGMA user_version = 3").Error
	})
	if err != nil {
		t.Fatalf("seed application schema v3: %v", err)
	}
}
