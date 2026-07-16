package store

import (
	"context"
	"errors"
	"testing"

	"github.com/SisyphusSQ/codex-pulse/internal/pricing"
	storesqlite "github.com/SisyphusSQ/codex-pulse/internal/store/sqlite"
)

func TestApplicationSchemaV5CreatesStrictCostLedgerContract(t *testing.T) {
	t.Parallel()

	database := openTestDatabase(t)
	if err := NewRepository(database).EnsureApplicationSchema(context.Background()); err != nil {
		t.Fatalf("EnsureApplicationSchema() error = %v", err)
	}
	assertMigrationVersionAndHistory(t, database, 12, 12)

	wantTables := []string{
		"cost_rollup_generations",
		"model_usage_daily",
		"pricing_catalog_metadata",
		"project_usage_daily",
		"session_usage_rollups",
		"turn_costs",
		"usage_daily",
	}
	_, strictByTable, err := applicationTableContract(context.Background(), database)
	if err != nil {
		t.Fatalf("applicationTableContract() error = %v", err)
	}
	for _, table := range wantTables {
		if !strictByTable[table] {
			t.Errorf("cost ledger table %q missing or not STRICT", table)
		}
	}

	err = database.View(context.Background(), func(ctx context.Context, connection storesqlite.ReadConn) error {
		for _, object := range costSchemaObjects {
			valid, err := verifySchemaObject(ctx, connection, object)
			if err != nil {
				return err
			}
			if !valid {
				t.Errorf("cost schema %s %q differs from canonical contract", object.objectType, object.name)
			}
		}
		return nil
	})
	if err != nil {
		t.Fatalf("verify cost schema: %v", err)
	}
}

func TestApplicationMigrationAppendsCostLedgerToFrozenV4(t *testing.T) {
	t.Parallel()

	database := openTestDatabase(t)
	seedApplicationSchemaV4ForCost(t, database)
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
		return "/tmp/application-v4-before-v5.db", nil
	}
	report, err := runner.run(context.Background())
	if err != nil {
		t.Fatalf("run() error = %v", err)
	}
	if report.FromVersion != 4 || report.TargetVersion != 12 ||
		!equalInts(report.AppliedVersions, []int{5, 6, 7, 8, 9, 10, 11, 12}) || report.BackupPath == "" {
		t.Fatalf("run() report = %#v, want v4 to v12", report)
	}
	if backupVersions != [2]int{4, 12} {
		t.Fatalf("backup versions = %v, want [4 12]", backupVersions)
	}
	assertMigrationVersionAndHistory(t, database, 12, 12)
}

func TestPricingCatalogMetadataIsImmutableAndLegacyMetadataRemainsOptional(t *testing.T) {
	t.Parallel()

	repository := openRuntimeRepository(t)
	builtin := pricing.BuiltinOpenAI20260714()
	if err := repository.AddPricingVersion(context.Background(), builtin); err != nil {
		t.Fatalf("AddPricingVersion(builtin) error = %v", err)
	}
	if err := repository.AddPricingVersion(context.Background(), builtin); err != nil {
		t.Fatalf("AddPricingVersion(builtin replay) error = %v", err)
	}
	stored, err := repository.PricingVersion(context.Background(), builtin.PricingVersion)
	if err != nil {
		t.Fatalf("PricingVersion(builtin) error = %v", err)
	}
	if stored.SourceURL != builtin.SourceURL || stored.VerifiedAtMS != builtin.VerifiedAtMS {
		t.Fatalf("stored catalog metadata = %#v, want %#v", stored, builtin)
	}

	mutated := builtin
	mutated.SourceURL = "https://example.invalid/mutated"
	if err := repository.AddPricingVersion(context.Background(), mutated); !errors.Is(err, ErrInvalidRecord) {
		t.Fatalf("AddPricingVersion(metadata mutation) error = %v, want ErrInvalidRecord", err)
	}
	stored, err = repository.PricingVersion(context.Background(), builtin.PricingVersion)
	if err != nil {
		t.Fatalf("PricingVersion(after mutation) error = %v", err)
	}
	if stored.SourceURL != builtin.SourceURL || stored.VerifiedAtMS != builtin.VerifiedAtMS {
		t.Fatalf("metadata changed after rejected mutation: %#v", stored)
	}

	legacy := PricingVersion{
		PricingVersion: "legacy-without-metadata", Source: "test", Currency: "USD",
		EffectiveFromMS: 1, CreatedAtMS: 1,
		Models: []ModelPrice{{
			MatchKind: ModelMatchExact, ModelPattern: "test-model", Priority: 1,
			InputMicrosPerMillion: pointerTo(int64(1)),
		}},
	}
	if err := repository.AddPricingVersion(context.Background(), legacy); err != nil {
		t.Fatalf("AddPricingVersion(legacy) error = %v", err)
	}
	storedLegacy, err := repository.PricingVersion(context.Background(), legacy.PricingVersion)
	if err != nil {
		t.Fatalf("PricingVersion(legacy) error = %v", err)
	}
	if storedLegacy.SourceURL != "" || storedLegacy.VerifiedAtMS != 0 {
		t.Fatalf("legacy optional metadata = %#v", storedLegacy)
	}
}

func TestCostSchemaRejectsDuplicateActiveAndInconsistentNullableTotals(t *testing.T) {
	t.Parallel()

	repository := openRuntimeRepository(t)
	ctx := context.Background()
	if err := repository.UpsertFacts(ctx, FactBatch{Session: &Session{
		SessionID: "session-cost-check", Provider: "codex", SourceKind: "session",
		CreatedAtMS: 100, FirstSeenAtMS: 100, LastSeenAtMS: 100,
	}}); err != nil {
		t.Fatalf("UpsertFacts(session) error = %v", err)
	}
	active := costRollupGenerationModel{
		GenerationID: "generation-check-1", ReportingTimezone: "UTC",
		PricingSource: "test", Currency: "USD", RollupVersion: 1,
		State: string(CostRollupGenerationActive), CreatedAtMS: 100,
		CompletedAtMS: pointerTo(int64(100)), UpdatedAtMS: 100,
	}
	if err := repository.database.Write(ctx, func(ctx context.Context, transaction storesqlite.WriteTx) error {
		return transaction.WithContext(ctx).Create(&active).Error
	}); err != nil {
		t.Fatalf("create active generation: %v", err)
	}
	duplicate := active
	duplicate.GenerationID = "generation-check-2"
	if err := repository.database.Write(ctx, func(ctx context.Context, transaction storesqlite.WriteTx) error {
		return transaction.WithContext(ctx).Create(&duplicate).Error
	}); err == nil {
		t.Fatal("duplicate active generation unexpectedly succeeded")
	}

	zero := int64(0)
	for _, testCase := range []struct {
		name   string
		totals rollupTotalsModel
	}{
		{
			name: "missing component with forged total",
			totals: rollupTotalsModel{
				TurnCount: 1, InputTokens: nil, CachedInputTokens: &zero,
				OutputTokens: &zero, ReasoningTokens: &zero, TotalTokens: &zero,
				UnpricedTurnCount: 1, FirstActivityAtMS: 100, LastActivityAtMS: 100, UpdatedAtMS: 100,
			},
		},
		{
			name: "complete components with missing total",
			totals: rollupTotalsModel{
				TurnCount: 1, InputTokens: &zero, CachedInputTokens: &zero,
				OutputTokens: &zero, ReasoningTokens: &zero, TotalTokens: nil,
				UnpricedTurnCount: 1, FirstActivityAtMS: 100, LastActivityAtMS: 100, UpdatedAtMS: 100,
			},
		},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			model := sessionUsageRollupModel{
				GenerationID: active.GenerationID, SessionID: "session-cost-check", Totals: testCase.totals,
			}
			if err := repository.database.Write(ctx, func(ctx context.Context, transaction storesqlite.WriteTx) error {
				return transaction.WithContext(ctx).Create(&model).Error
			}); err == nil {
				t.Fatal("inconsistent nullable totals unexpectedly succeeded")
			}
		})
	}
}

func seedApplicationSchemaV4ForCost(t *testing.T, database *storesqlite.Store) {
	t.Helper()
	err := database.Write(context.Background(), func(ctx context.Context, transaction storesqlite.WriteTx) error {
		if err := ensureSchemaObjects(ctx, transaction, migrationSchemaObjects); err != nil {
			return err
		}
		for _, migration := range applicationMigrations[:4] {
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
		return transaction.WithContext(ctx).Exec("PRAGMA user_version = 4").Error
	})
	if err != nil {
		t.Fatalf("seed application schema v4: %v", err)
	}
}
