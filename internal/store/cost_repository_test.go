package store

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/SisyphusSQ/codex-pulse/internal/pricing"
	storesqlite "github.com/SisyphusSQ/codex-pulse/internal/store/sqlite"
)

func TestRebuildCostLedgerUsesIANADayBoundariesIncludingDST(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		timezone  string
		observed  []string
		wantStart []string
	}{
		{
			name: "Asia Shanghai midnight", timezone: "Asia/Shanghai",
			observed:  []string{"2026-01-01T15:59:59.999Z", "2026-01-01T16:00:00Z"},
			wantStart: []string{"2025-12-31T16:00:00Z", "2026-01-01T16:00:00Z"},
		},
		{
			name: "New York DST fall day", timezone: "America/New_York",
			observed:  []string{"2026-11-01T03:59:59.999Z", "2026-11-01T04:00:00Z"},
			wantStart: []string{"2026-10-31T04:00:00Z", "2026-11-01T04:00:00Z"},
		},
	}
	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()
			repository := openRuntimeRepository(t)
			ctx := context.Background()
			if err := repository.AddPricingVersion(ctx, pricing.BuiltinOpenAI20260714()); err != nil {
				t.Fatalf("AddPricingVersion() error = %v", err)
			}
			zero := int64(0)
			latest := int64(0)
			for index, value := range testCase.observed {
				atMS := mustParseCostTime(t, value)
				latest = atMS
				seedCostTurn(t, repository, testCase.timezone+string(rune('a'+index)),
					pointerTo("gpt-5.2-codex"), atMS, true, pricing.Usage{
						InputTokens: &zero, CachedInputTokens: &zero,
						OutputTokens: &zero, ReasoningTokens: &zero,
					})
			}
			_, err := repository.RebuildCostLedger(ctx, RebuildCostLedgerRequest{
				GenerationID:      "generation-" + testCase.timezone,
				ReportingTimezone: testCase.timezone, PricingSource: "openai-api", Currency: "USD",
				RollupVersion: 1, CalculatedAtMS: latest + 1,
			})
			if err != nil {
				t.Fatalf("RebuildCostLedger() error = %v", err)
			}
			snapshot, err := repository.ActiveCostLedger(ctx, testCase.timezone)
			if err != nil {
				t.Fatalf("ActiveCostLedger() error = %v", err)
			}
			if len(snapshot.DailyRollups) != len(testCase.wantStart) {
				t.Fatalf("daily rollups = %#v", snapshot.DailyRollups)
			}
			for index, want := range testCase.wantStart {
				if got := time.UnixMilli(snapshot.DailyRollups[index].BucketStartMS).UTC().Format(time.RFC3339Nano); got != want {
					t.Errorf("bucket %d = %s, want %s", index, got, want)
				}
			}
		})
	}
}

func TestRebuildCostLedgerSelectsHalfOpenCatalogVersionsAndExactRules(t *testing.T) {
	t.Parallel()

	repository := openRuntimeRepository(t)
	ctx := context.Background()
	baseTime := int64(1_700_000_000_000)
	for _, catalog := range []pricing.CatalogVersion{
		{
			PricingVersion: "test-pricing-v1", Source: "test-pricing", Currency: "USD",
			EffectiveFromMS: baseTime + 200, CreatedAtMS: baseTime + 200,
			Models: []pricing.ModelPrice{
				{
					MatchKind: pricing.ModelMatchExact, ModelPattern: "gpt-test", Priority: 100,
					InputMicrosPerMillion: pointerTo(int64(1_000_000)),
				},
				{
					MatchKind: pricing.ModelMatchPrefix, ModelPattern: "gpt-", Priority: 1,
					InputMicrosPerMillion: pointerTo(int64(999_000_000)),
				},
			},
		},
		{
			PricingVersion: "test-pricing-v2", Source: "test-pricing", Currency: "USD",
			EffectiveFromMS: baseTime + 300, CreatedAtMS: baseTime + 300,
			Models: []pricing.ModelPrice{{
				MatchKind: pricing.ModelMatchExact, ModelPattern: "gpt-test", Priority: 100,
				InputMicrosPerMillion: pointerTo(int64(2_000_000)),
			}},
		},
	} {
		if err := repository.AddPricingVersion(ctx, catalog); err != nil {
			t.Fatalf("AddPricingVersion(%s) error = %v", catalog.PricingVersion, err)
		}
	}
	million, zero := int64(1_000_000), int64(0)
	for _, fixture := range []struct {
		suffix string
		model  string
		atMS   int64
	}{
		{suffix: "before-catalog", model: "gpt-test", atMS: baseTime + 100},
		{suffix: "v1-boundary", model: "gpt-test", atMS: baseTime + 200},
		{suffix: "prefix-rejected", model: "gpt-future", atMS: baseTime + 250},
		{suffix: "v2-boundary", model: "gpt-test", atMS: baseTime + 300},
	} {
		seedCostTurn(t, repository, fixture.suffix, &fixture.model, fixture.atMS, true, pricing.Usage{
			InputTokens: &million, CachedInputTokens: &zero, OutputTokens: &zero, ReasoningTokens: &zero,
		})
	}
	_, err := repository.RebuildCostLedger(ctx, RebuildCostLedgerRequest{
		GenerationID: "generation-boundaries", ReportingTimezone: "UTC",
		PricingSource: "test-pricing", Currency: "USD", RollupVersion: 1,
		CalculatedAtMS: baseTime + 1_000,
	})
	if err != nil {
		t.Fatalf("RebuildCostLedger() error = %v", err)
	}
	snapshot, err := repository.ActiveCostLedger(ctx, "UTC")
	if err != nil {
		t.Fatalf("ActiveCostLedger() error = %v", err)
	}
	byTurn := make(map[string]TurnCost, len(snapshot.TurnCosts))
	for _, cost := range snapshot.TurnCosts {
		byTurn[cost.TurnID] = cost
	}
	if got := byTurn["turn-before-catalog"]; got.Reason != pricing.CostReasonCatalogNotEffective || got.PricingVersion != nil {
		t.Fatalf("before catalog cost = %#v", got)
	}
	if got := byTurn["turn-prefix-rejected"]; got.Reason != pricing.CostReasonModelNotListed ||
		got.PricingVersion == nil || *got.PricingVersion != "test-pricing-v1" {
		t.Fatalf("prefix-only cost = %#v", got)
	}
	for turnID, want := range map[string]struct {
		version string
		micros  int64
	}{
		"turn-v1-boundary": {version: "test-pricing-v1", micros: 1_000_000},
		"turn-v2-boundary": {version: "test-pricing-v2", micros: 2_000_000},
	} {
		got := byTurn[turnID]
		if got.Status != pricing.CostStatusPriced || got.PricingVersion == nil ||
			*got.PricingVersion != want.version || got.EstimatedUSDMicros == nil ||
			*got.EstimatedUSDMicros != want.micros {
			t.Errorf("%s cost = %#v, want version=%s micros=%d", turnID, got, want.version, want.micros)
		}
	}
}

func TestRebuildCostLedgerIsIdempotentAndKeepsOldActiveOnFailure(t *testing.T) {
	t.Parallel()

	repository := openRuntimeRepository(t)
	ctx := context.Background()
	if err := repository.AddPricingVersion(ctx, pricing.BuiltinOpenAI20260714()); err != nil {
		t.Fatalf("AddPricingVersion() error = %v", err)
	}
	baseTime := int64(1_700_000_000_000)
	zero := int64(0)
	seedCostTurn(t, repository, "atomic", pointerTo("gpt-5.2-codex"), baseTime+100, true, pricing.Usage{
		InputTokens: &zero, CachedInputTokens: &zero, OutputTokens: &zero, ReasoningTokens: &zero,
	})
	requestOne := RebuildCostLedgerRequest{
		GenerationID: "generation-atomic-1", ReportingTimezone: "UTC",
		PricingSource: "openai-api", Currency: "USD", RollupVersion: 1,
		CalculatedAtMS: baseTime + 1_000,
	}
	if _, err := repository.RebuildCostLedger(ctx, requestOne); err != nil {
		t.Fatalf("RebuildCostLedger(g1) error = %v", err)
	}
	replayed, err := repository.RebuildCostLedger(ctx, requestOne)
	if err != nil || !replayed.Replayed || replayed.FinalTurns != 1 {
		t.Fatalf("RebuildCostLedger(g1 replay) = %#v, %v", replayed, err)
	}
	conflict := requestOne
	conflict.Currency = "EUR"
	if _, err := repository.RebuildCostLedger(ctx, conflict); !errors.Is(err, ErrInvalidRecord) {
		t.Fatalf("RebuildCostLedger(g1 conflict) error = %v, want ErrInvalidRecord", err)
	}

	if err := repository.database.Write(ctx, func(ctx context.Context, transaction storesqlite.WriteTx) error {
		return transaction.WithContext(ctx).Exec(`CREATE TRIGGER fail_generation_two
			BEFORE INSERT ON turn_costs WHEN NEW.generation_id = 'generation-atomic-2'
			BEGIN SELECT RAISE(ABORT, 'synthetic turn cost failure'); END`).Error
	}); err != nil {
		t.Fatalf("create failure trigger: %v", err)
	}
	requestTwo := requestOne
	requestTwo.GenerationID = "generation-atomic-2"
	requestTwo.CalculatedAtMS = baseTime + 2_000
	if _, err := repository.RebuildCostLedger(ctx, requestTwo); err == nil {
		t.Fatal("RebuildCostLedger(g2 fault) error = nil")
	}
	assertActiveCostGeneration(t, repository, "UTC", requestOne.GenerationID)
	var failedGenerationCount int64
	if err := repository.database.View(ctx, func(ctx context.Context, connection storesqlite.ReadConn) error {
		return connection.WithContext(ctx).Model(&costRollupGenerationModel{}).
			Where("generation_id = ?", requestTwo.GenerationID).Count(&failedGenerationCount).Error
	}); err != nil {
		t.Fatalf("count failed generation: %v", err)
	}
	if failedGenerationCount != 0 {
		t.Fatalf("failed generation rows = %d, want 0", failedGenerationCount)
	}
	if err := repository.database.Write(ctx, func(ctx context.Context, transaction storesqlite.WriteTx) error {
		return transaction.WithContext(ctx).Exec("DROP TRIGGER fail_generation_two").Error
	}); err != nil {
		t.Fatalf("drop failure trigger: %v", err)
	}
	if _, err := repository.RebuildCostLedger(ctx, requestTwo); err != nil {
		t.Fatalf("RebuildCostLedger(g2) error = %v", err)
	}
	assertActiveCostGeneration(t, repository, "UTC", requestTwo.GenerationID)
	var previous costRollupGenerationModel
	if err := repository.database.View(ctx, func(ctx context.Context, connection storesqlite.ReadConn) error {
		return connection.WithContext(ctx).Where("generation_id = ?", requestOne.GenerationID).Take(&previous).Error
	}); err != nil {
		t.Fatalf("read superseded generation: %v", err)
	}
	if previous.State != string(CostRollupGenerationSuperseded) {
		t.Fatalf("previous generation state = %q, want superseded", previous.State)
	}

	cancelled, cancel := context.WithCancel(ctx)
	cancel()
	requestThree := requestTwo
	requestThree.GenerationID = "generation-atomic-3"
	requestThree.CalculatedAtMS++
	if _, err := repository.RebuildCostLedger(cancelled, requestThree); !errors.Is(err, context.Canceled) {
		t.Fatalf("RebuildCostLedger(cancelled) error = %v, want context.Canceled", err)
	}
	assertActiveCostGeneration(t, repository, "UTC", requestTwo.GenerationID)
}

func TestActiveCostLedgerSurvivesStoreRestart(t *testing.T) {
	directory := t.TempDir()
	if err := os.Chmod(directory, 0o700); err != nil {
		t.Fatalf("secure temp directory: %v", err)
	}
	config := storesqlite.Config{Path: filepath.Join(directory, "cost-restart.db")}
	ctx := context.Background()
	database, err := storesqlite.Open(ctx, config)
	if err != nil {
		t.Fatalf("sqlite.Open() error = %v", err)
	}
	repository := NewRepository(database)
	if err := repository.EnsureApplicationSchema(ctx); err != nil {
		t.Fatalf("EnsureApplicationSchema() error = %v", err)
	}
	if err := repository.AddPricingVersion(ctx, pricing.BuiltinOpenAI20260714()); err != nil {
		t.Fatalf("AddPricingVersion() error = %v", err)
	}
	baseTime := int64(1_700_000_000_000)
	zero := int64(0)
	seedCostTurn(t, repository, "restart", pointerTo("gpt-5.2-codex"), baseTime+100, true, pricing.Usage{
		InputTokens: &zero, CachedInputTokens: &zero, OutputTokens: &zero, ReasoningTokens: &zero,
	})
	if _, err := repository.RebuildCostLedger(ctx, RebuildCostLedgerRequest{
		GenerationID: "generation-restart", ReportingTimezone: "UTC",
		PricingSource: "openai-api", Currency: "USD", RollupVersion: 1,
		CalculatedAtMS: baseTime + 1_000,
	}); err != nil {
		t.Fatalf("RebuildCostLedger() error = %v", err)
	}
	if err := database.Close(ctx); err != nil {
		t.Fatalf("Close(before restart) error = %v", err)
	}

	reopened, err := storesqlite.Open(ctx, config)
	if err != nil {
		t.Fatalf("sqlite.Open(restart) error = %v", err)
	}
	t.Cleanup(func() { _ = reopened.Close(context.Background()) })
	restartedRepository := NewRepository(reopened)
	if err := restartedRepository.EnsureApplicationSchema(ctx); err != nil {
		t.Fatalf("EnsureApplicationSchema(restart) error = %v", err)
	}
	snapshot, err := restartedRepository.ActiveCostLedger(ctx, "UTC")
	if err != nil || snapshot.Generation.GenerationID != "generation-restart" ||
		len(snapshot.TurnCosts) != 1 || len(snapshot.DailyRollups) != 1 {
		t.Fatalf("ActiveCostLedger(restart) = %#v, %v", snapshot, err)
	}
}

func TestRebuildCostLedgerOverflowRollsBackToOldActiveGeneration(t *testing.T) {
	t.Parallel()

	repository := openRuntimeRepository(t)
	ctx := context.Background()
	baseTime := int64(1_700_000_000_000)
	million, zero := int64(1_000_000), int64(0)
	for index, suffix := range []string{"overflow-a", "overflow-b"} {
		seedCostTurn(t, repository, suffix, pointerTo("gpt-5.2-codex"),
			baseTime+int64(index+1)*100, true, pricing.Usage{
				InputTokens: &million, CachedInputTokens: &zero,
				OutputTokens: &zero, ReasoningTokens: &zero,
			})
	}
	requestOne := RebuildCostLedgerRequest{
		GenerationID: "generation-before-overflow", ReportingTimezone: "UTC",
		PricingSource: "aggregate-overflow", Currency: "USD", RollupVersion: 1,
		CalculatedAtMS: baseTime + 1_000,
	}
	if _, err := repository.RebuildCostLedger(ctx, requestOne); err != nil {
		t.Fatalf("RebuildCostLedger(before catalog) error = %v", err)
	}
	overflowingRate := maxCostInteger/2 + 1
	if err := repository.AddPricingVersion(ctx, pricing.CatalogVersion{
		PricingVersion: "aggregate-overflow-v1", Source: "aggregate-overflow", Currency: "USD",
		EffectiveFromMS: 0, CreatedAtMS: baseTime + 1_100,
		Models: []pricing.ModelPrice{{
			MatchKind: pricing.ModelMatchExact, ModelPattern: "gpt-5.2-codex", Priority: 100,
			InputMicrosPerMillion: &overflowingRate,
		}},
	}); err != nil {
		t.Fatalf("AddPricingVersion(overflow) error = %v", err)
	}
	requestTwo := requestOne
	requestTwo.GenerationID = "generation-overflow"
	requestTwo.CalculatedAtMS = baseTime + 2_000
	if _, err := repository.RebuildCostLedger(ctx, requestTwo); !errors.Is(err, pricing.ErrCostOverflow) {
		t.Fatalf("RebuildCostLedger(overflow) error = %v, want ErrCostOverflow", err)
	}
	assertActiveCostGeneration(t, repository, "UTC", requestOne.GenerationID)
}

func TestCostRollupsUseSafeDistinctAttributionDimensions(t *testing.T) {
	t.Parallel()

	repository := openRuntimeRepository(t)
	ctx := context.Background()
	if err := repository.AddPricingVersion(ctx, pricing.BuiltinOpenAI20260714()); err != nil {
		t.Fatalf("AddPricingVersion() error = %v", err)
	}
	baseTime := int64(1_700_000_000_000)
	rawModel := "OpenAI/GPT-5.2-Codex"
	roots := []string{
		"/Users/alice/private-root-marker-one/shared-project",
		"/Users/alice/private-root-marker-two/shared-project",
	}
	for index, projectID := range []string{"project-distinct-a", "project-distinct-b"} {
		seedProjectCostTurn(
			t, repository, projectID, "Same Display Name", roots[index], rawModel,
			baseTime+int64(index+1)*100,
		)
	}
	_, err := repository.RebuildCostLedger(ctx, RebuildCostLedgerRequest{
		GenerationID: "generation-safe-dimensions", ReportingTimezone: "UTC",
		PricingSource: "openai-api", Currency: "USD", RollupVersion: 1,
		CalculatedAtMS: baseTime + 1_000,
	})
	if err != nil {
		t.Fatalf("RebuildCostLedger() error = %v", err)
	}
	snapshot, err := repository.ActiveCostLedger(ctx, "UTC")
	if err != nil {
		t.Fatalf("ActiveCostLedger() error = %v", err)
	}
	if len(snapshot.ProjectDaily) != 2 || len(snapshot.ModelDaily) != 1 {
		t.Fatalf("safe dimension cardinality = projects:%#v models:%#v", snapshot.ProjectDaily, snapshot.ModelDaily)
	}
	seenProjects := make(map[string]bool)
	for _, rollup := range snapshot.ProjectDaily {
		if rollup.ProjectID == nil || rollup.ProjectDisplayName == nil ||
			*rollup.ProjectDisplayName != "Same Display Name" || rollup.DimensionKey != *rollup.ProjectID {
			t.Fatalf("project rollup identity = %#v", rollup)
		}
		seenProjects[*rollup.ProjectID] = true
	}
	if !seenProjects["project-distinct-a"] || !seenProjects["project-distinct-b"] {
		t.Fatalf("distinct same-name projects merged: %#v", snapshot.ProjectDaily)
	}
	encoded, err := json.Marshal(snapshot)
	if err != nil {
		t.Fatalf("json.Marshal(snapshot) error = %v", err)
	}
	serialized := string(encoded)
	for _, forbidden := range append(roots, rawModel) {
		if strings.Contains(serialized, forbidden) {
			t.Fatalf("cost ledger leaked raw attribution marker %q: %s", forbidden, serialized)
		}
	}
}

func TestCostRollupsMergeSafeAttributionMetadataByStableIdentity(t *testing.T) {
	t.Parallel()

	repository := openRuntimeRepository(t)
	ctx := context.Background()
	if err := repository.AddPricingVersion(ctx, pricing.BuiltinOpenAI20260714()); err != nil {
		t.Fatalf("AddPricingVersion() error = %v", err)
	}
	baseTime := int64(1_700_000_000_000)
	seedProjectCostTurn(
		t, repository, "project-stable-a", "Original Safe Project", "/synthetic/stable-a",
		"gpt-5.2-codex", baseTime+100,
	)
	seedProjectCostTurn(
		t, repository, "project-stable-b", "Replacement Safe Project", "/synthetic/stable-b",
		"OpenAI/GPT-5.2-Codex", baseTime+200,
	)
	err := repository.database.Write(ctx, func(ctx context.Context, transaction storesqlite.WriteTx) error {
		stableProjectID := "project-stable-a"
		return transaction.WithContext(ctx).Model(&turnAttributionModel{}).
			Where("turn_id = ?", "turn-project-stable-b").
			Updates(map[string]any{
				"project_id":           stableProjectID,
				"project_display_name": "Replacement Safe Project",
				"project_confidence":   string(AttributionConfidenceMedium),
				"project_source":       string(AttributionSourceCWDPathDigest),
				"project_reason":       string(AttributionReasonPathDerived),
			}).Error
	})
	if err != nil {
		t.Fatalf("set alternate safe project attribution: %v", err)
	}

	_, err = repository.RebuildCostLedger(ctx, RebuildCostLedgerRequest{
		GenerationID: "generation-merged-safe-dimensions", ReportingTimezone: "UTC",
		PricingSource: "openai-api", Currency: "USD", RollupVersion: 1,
		CalculatedAtMS: baseTime + 1_000,
	})
	if err != nil {
		t.Fatalf("RebuildCostLedger() error = %v", err)
	}
	snapshot, err := repository.ActiveCostLedger(ctx, "UTC")
	if err != nil {
		t.Fatalf("ActiveCostLedger() error = %v", err)
	}
	if len(snapshot.ProjectDaily) != 1 || len(snapshot.ModelDaily) != 1 {
		t.Fatalf("merged dimension cardinality = projects:%#v models:%#v", snapshot.ProjectDaily, snapshot.ModelDaily)
	}
	project := snapshot.ProjectDaily[0]
	if project.ProjectID == nil || *project.ProjectID != "project-stable-a" ||
		project.ProjectDisplayName == nil || *project.ProjectDisplayName != "project-stable-a" ||
		project.AttributionConfidence != string(AttributionConfidenceMedium) ||
		project.AttributionSource != "mixed" || project.AttributionReason != "mixed" ||
		project.TurnCount != 2 {
		t.Fatalf("merged project dimension = %#v", project)
	}
	model := snapshot.ModelDaily[0]
	if model.ModelKey == nil || *model.ModelKey != "gpt-5.2-codex" ||
		model.ModelDisplayName == nil || *model.ModelDisplayName != "GPT-5.2 Codex" ||
		model.AttributionConfidence != string(AttributionConfidenceHigh) ||
		model.AttributionSource != "mixed" ||
		model.AttributionReason != string(AttributionReasonObserved) || model.TurnCount != 2 {
		t.Fatalf("merged model dimension = %#v", model)
	}
}

func TestRebuildCostLedgerRejectsIncompletePersistedGeneration(t *testing.T) {
	t.Parallel()

	const failedGeneration = "generation-persistence-fault-2"
	tests := []struct {
		name    string
		trigger string
	}{
		{
			name: "turn cost insert ignored",
			trigger: `CREATE TRIGGER ignore_turn_cost
				BEFORE INSERT ON turn_costs WHEN NEW.generation_id = 'generation-persistence-fault-2'
				BEGIN SELECT RAISE(IGNORE); END`,
		},
		{
			name: "turn cost removed after insert",
			trigger: `CREATE TRIGGER remove_turn_cost
				AFTER INSERT ON turn_costs WHEN NEW.generation_id = 'generation-persistence-fault-2'
				BEGIN DELETE FROM turn_costs
				WHERE generation_id = NEW.generation_id AND turn_id = NEW.turn_id; END`,
		},
		{
			name: "session rollup removed after insert",
			trigger: `CREATE TRIGGER remove_session_rollup
				AFTER INSERT ON session_usage_rollups WHEN NEW.generation_id = 'generation-persistence-fault-2'
				BEGIN DELETE FROM session_usage_rollups
				WHERE generation_id = NEW.generation_id AND session_id = NEW.session_id; END`,
		},
		{
			name: "daily rollup removed after insert",
			trigger: `CREATE TRIGGER remove_daily_rollup
				AFTER INSERT ON usage_daily WHEN NEW.generation_id = 'generation-persistence-fault-2'
				BEGIN DELETE FROM usage_daily
				WHERE generation_id = NEW.generation_id AND bucket_start_ms = NEW.bucket_start_ms; END`,
		},
		{
			name: "project rollup removed after insert",
			trigger: `CREATE TRIGGER remove_project_rollup
				AFTER INSERT ON project_usage_daily WHEN NEW.generation_id = 'generation-persistence-fault-2'
				BEGIN DELETE FROM project_usage_daily
				WHERE generation_id = NEW.generation_id AND bucket_start_ms = NEW.bucket_start_ms
				AND dimension_key = NEW.dimension_key; END`,
		},
		{
			name: "model rollup removed after insert",
			trigger: `CREATE TRIGGER remove_model_rollup
				AFTER INSERT ON model_usage_daily WHEN NEW.generation_id = 'generation-persistence-fault-2'
				BEGIN DELETE FROM model_usage_daily
				WHERE generation_id = NEW.generation_id AND bucket_start_ms = NEW.bucket_start_ms
				AND dimension_key = NEW.dimension_key; END`,
		},
		{
			name: "supersede update ignored",
			trigger: `CREATE TRIGGER ignore_generation_supersede
				BEFORE UPDATE OF state ON cost_rollup_generations
				WHEN NEW.generation_id = 'generation-persistence-fault-1' AND NEW.state = 'superseded'
				BEGIN SELECT RAISE(IGNORE); END`,
		},
		{
			name: "activation update ignored",
			trigger: `CREATE TRIGGER ignore_generation_activation
				BEFORE UPDATE OF state ON cost_rollup_generations
				WHEN NEW.generation_id = 'generation-persistence-fault-2' AND NEW.state = 'active'
				BEGIN SELECT RAISE(IGNORE); END`,
		},
	}
	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			repository := openRuntimeRepository(t)
			ctx := context.Background()
			if err := repository.AddPricingVersion(ctx, pricing.BuiltinOpenAI20260714()); err != nil {
				t.Fatalf("AddPricingVersion() error = %v", err)
			}
			baseTime := int64(1_700_000_000_000)
			zero := int64(0)
			seedCostTurn(t, repository, "persistence-fault", pointerTo("gpt-5.2-codex"),
				baseTime+100, true, pricing.Usage{
					InputTokens: &zero, CachedInputTokens: &zero,
					OutputTokens: &zero, ReasoningTokens: &zero,
				})
			requestOne := RebuildCostLedgerRequest{
				GenerationID: "generation-persistence-fault-1", ReportingTimezone: "UTC",
				PricingSource: "openai-api", Currency: "USD", RollupVersion: 1,
				CalculatedAtMS: baseTime + 1_000,
			}
			if _, err := repository.RebuildCostLedger(ctx, requestOne); err != nil {
				t.Fatalf("RebuildCostLedger(g1) error = %v", err)
			}
			if err := repository.database.Write(ctx, func(ctx context.Context, transaction storesqlite.WriteTx) error {
				return transaction.WithContext(ctx).Exec(testCase.trigger).Error
			}); err != nil {
				t.Fatalf("create synthetic trigger: %v", err)
			}
			requestTwo := requestOne
			requestTwo.GenerationID = failedGeneration
			requestTwo.CalculatedAtMS = baseTime + 2_000
			if _, err := repository.RebuildCostLedger(ctx, requestTwo); !errors.Is(err, ErrInvalidRecord) {
				t.Fatalf("RebuildCostLedger(g2 fault) error = %v, want ErrInvalidRecord", err)
			}
			assertActiveCostGeneration(t, repository, "UTC", requestOne.GenerationID)
			var failedCount int64
			if err := repository.database.View(ctx, func(ctx context.Context, connection storesqlite.ReadConn) error {
				return connection.WithContext(ctx).Model(&costRollupGenerationModel{}).
					Where("generation_id = ?", failedGeneration).Count(&failedCount).Error
			}); err != nil {
				t.Fatalf("count failed generation: %v", err)
			}
			if failedCount != 0 {
				t.Fatalf("failed generation rows = %d, want 0", failedCount)
			}
		})
	}
}

func TestRebuildCostLedgerPricesFinalTurnsAndPreservesUnknownReasons(t *testing.T) {
	t.Parallel()

	repository := openRuntimeRepository(t)
	ctx := context.Background()
	if err := repository.AddPricingVersion(ctx, pricing.BuiltinOpenAI20260714()); err != nil {
		t.Fatalf("AddPricingVersion() error = %v", err)
	}

	million := int64(1_000_000)
	zero := int64(0)
	baseTime := int64(1_700_000_000_000)
	seedCostTurn(t, repository, "known", pointerTo("gpt-5.2-codex"), baseTime+100, true, pricing.Usage{
		InputTokens: &million, CachedInputTokens: &million,
		OutputTokens: &million, ReasoningTokens: &million,
	})
	seedCostTurn(t, repository, "not-listed", pointerTo("gpt-5.2-codex-max"), baseTime+200, true, pricing.Usage{
		InputTokens: &zero, CachedInputTokens: &zero, OutputTokens: &zero, ReasoningTokens: &zero,
	})
	seedCostTurn(t, repository, "missing-model", nil, baseTime+300, true, pricing.Usage{
		InputTokens: &zero, CachedInputTokens: &zero, OutputTokens: &zero, ReasoningTokens: &zero,
	})
	seedCostTurn(t, repository, "missing-token", pointerTo("gpt-5.2-codex"), baseTime+400, true, pricing.Usage{
		InputTokens: &zero, CachedInputTokens: nil, OutputTokens: &zero, ReasoningTokens: &zero,
	})
	seedCostTurn(t, repository, "conflict-model", nil, baseTime+500, true, pricing.Usage{
		InputTokens: &zero, CachedInputTokens: &zero, OutputTokens: &zero, ReasoningTokens: &zero,
	})
	setTurnModelAttributionReason(
		t, repository, "turn-conflict-model", AttributionConfidenceLow,
		AttributionSourceConflict, AttributionReasonConflict,
	)
	seedCostTurn(t, repository, "invalid-model", nil, baseTime+600, true, pricing.Usage{
		InputTokens: &zero, CachedInputTokens: &zero, OutputTokens: &zero, ReasoningTokens: &zero,
	})
	setTurnModelAttributionReason(
		t, repository, "turn-invalid-model", AttributionConfidenceUnknown,
		AttributionSourceInvalidModel, AttributionReasonInvalid,
	)
	seedCostTurn(t, repository, "provisional", pointerTo("gpt-5.2-codex"), baseTime+700, false, pricing.Usage{
		InputTokens: &million, CachedInputTokens: &zero, OutputTokens: &zero, ReasoningTokens: &zero,
	})

	report, err := repository.RebuildCostLedger(ctx, RebuildCostLedgerRequest{
		GenerationID: "generation-1", ReportingTimezone: "Asia/Shanghai",
		PricingSource: "openai-api", Currency: "USD", RollupVersion: 1, CalculatedAtMS: baseTime + 1_000,
	})
	if err != nil {
		t.Fatalf("RebuildCostLedger() error = %v", err)
	}
	if report.FinalTurns != 6 || report.PricedTurns != 1 || report.UnpricedTurns != 5 || report.Replayed {
		t.Fatalf("RebuildCostLedger() report = %#v", report)
	}

	snapshot, err := repository.ActiveCostLedger(ctx, "Asia/Shanghai")
	if err != nil {
		t.Fatalf("ActiveCostLedger() error = %v", err)
	}
	if snapshot.Generation.GenerationID != "generation-1" || len(snapshot.TurnCosts) != 6 {
		t.Fatalf("active cost ledger = %#v", snapshot)
	}
	wantReasons := map[string]pricing.CostReason{
		"turn-known":          pricing.CostReasonPriced,
		"turn-not-listed":     pricing.CostReasonModelNotListed,
		"turn-missing-model":  pricing.CostReasonMissingModel,
		"turn-missing-token":  pricing.CostReasonMissingToken,
		"turn-conflict-model": pricing.CostReasonConflictModel,
		"turn-invalid-model":  pricing.CostReasonInvalidModel,
	}
	for _, cost := range snapshot.TurnCosts {
		if cost.Reason != wantReasons[cost.TurnID] {
			t.Errorf("turn %q reason = %q, want %q", cost.TurnID, cost.Reason, wantReasons[cost.TurnID])
		}
		if cost.TurnID == "turn-known" {
			const wantCost = int64(29_925_000)
			if cost.EstimatedUSDMicros == nil || *cost.EstimatedUSDMicros != wantCost {
				t.Errorf("known estimated cost = %v, want %d", cost.EstimatedUSDMicros, wantCost)
			}
		}
	}
	if len(snapshot.SessionRollups) != 6 || len(snapshot.DailyRollups) != 1 ||
		len(snapshot.ProjectDaily) != 1 {
		t.Fatalf("rollup cardinality = sessions:%d daily:%d projects:%d",
			len(snapshot.SessionRollups), len(snapshot.DailyRollups), len(snapshot.ProjectDaily))
	}
	daily := snapshot.DailyRollups[0]
	if daily.TurnCount != 6 || daily.PricedTurnCount != 1 || daily.UnpricedTurnCount != 5 ||
		daily.CachedInputTokens != nil || daily.TotalTokens != nil ||
		daily.EstimatedUSDMicros == nil || *daily.EstimatedUSDMicros != 29_925_000 {
		t.Fatalf("partial daily rollup = %#v", daily)
	}
	modelTurns := int64(0)
	for _, rollup := range snapshot.ModelDaily {
		modelTurns += rollup.TurnCount
	}
	if modelTurns != 6 || snapshot.ProjectDaily[0].TurnCount != 6 {
		t.Fatalf("dimension reconciliation = model turns %d, project %#v", modelTurns, snapshot.ProjectDaily)
	}
	if len(snapshot.ModelDaily) != 5 {
		t.Fatalf("known/missing/conflict/invalid model dimensions merged: %#v", snapshot.ModelDaily)
	}
}

func seedCostTurn(
	t *testing.T,
	repository *Repository,
	suffix string,
	model *string,
	observedAtMS int64,
	isFinal bool,
	usage pricing.Usage,
) {
	t.Helper()
	sessionID := "session-" + suffix
	turnID := "turn-" + suffix
	turn := &Turn{
		TurnID: turnID, SessionID: sessionID, StartedAtMS: observedAtMS - 10,
		Model: model, SourceGeneration: 0, StartOffset: 10,
	}
	if isFinal {
		turn.CompletedAtMS = pointerTo(observedAtMS)
		turn.CompleteOffset = pointerTo(int64(20))
		turn.Outcome = pointerTo("completed")
	}
	if err := repository.UpsertFacts(context.Background(), FactBatch{
		Session: &Session{
			SessionID: sessionID, Provider: "codex", SourceKind: "session",
			CreatedAtMS: observedAtMS - 10, FirstSeenAtMS: observedAtMS - 10, LastSeenAtMS: observedAtMS,
		},
		Turn: turn,
		Usage: &TurnUsage{
			TurnID: turnID, ObservedAtMS: observedAtMS, IsFinal: isFinal,
			InputTokens: usage.InputTokens, CachedInputTokens: usage.CachedInputTokens,
			OutputTokens: usage.OutputTokens, ReasoningTokens: usage.ReasoningTokens,
			SourceGeneration: 0, SourceOffset: 20, Confidence: "exact", UpdatedAtMS: observedAtMS,
		},
	}); err != nil {
		t.Fatalf("seed cost turn %q: %v", suffix, err)
	}
}

func mustParseCostTime(t *testing.T, value string) int64 {
	t.Helper()
	parsed, err := time.Parse(time.RFC3339Nano, value)
	if err != nil {
		t.Fatalf("time.Parse(%q): %v", value, err)
	}
	return parsed.UnixMilli()
}

func assertActiveCostGeneration(t *testing.T, repository *Repository, timezone, generationID string) {
	t.Helper()
	snapshot, err := repository.ActiveCostLedger(context.Background(), timezone)
	if err != nil {
		t.Fatalf("ActiveCostLedger(%q) error = %v", timezone, err)
	}
	if snapshot.Generation.GenerationID != generationID {
		t.Fatalf("active generation = %q, want %q", snapshot.Generation.GenerationID, generationID)
	}
}

func setTurnModelAttributionReason(
	t *testing.T,
	repository *Repository,
	turnID string,
	confidence AttributionConfidence,
	source AttributionSource,
	reason AttributionReason,
) {
	t.Helper()
	err := repository.database.Write(context.Background(), func(ctx context.Context, transaction storesqlite.WriteTx) error {
		return transaction.WithContext(ctx).Model(&turnAttributionModel{}).Where("turn_id = ?", turnID).
			Updates(map[string]any{
				"model_confidence": string(confidence),
				"model_source":     string(source),
				"model_reason":     string(reason),
			}).Error
	})
	if err != nil {
		t.Fatalf("set model attribution reason for %q: %v", turnID, err)
	}
}

func seedProjectCostTurn(
	t *testing.T,
	repository *Repository,
	projectID string,
	displayName string,
	root string,
	rawModel string,
	observedAtMS int64,
) {
	t.Helper()
	sessionID := "session-" + projectID
	turnID := "turn-" + projectID
	cwd := root + "/internal/store"
	zero := int64(0)
	if err := repository.UpsertFacts(context.Background(), FactBatch{
		Project: &Project{
			ProjectID: projectID, DisplayName: displayName, RootPath: root,
			CreatedAtMS: observedAtMS - 10, UpdatedAtMS: observedAtMS - 10,
		},
		Session: &Session{
			SessionID: sessionID, Provider: "codex", SourceKind: "session",
			InitialCWD: &cwd, ProjectID: &projectID,
			CreatedAtMS: observedAtMS - 10, FirstSeenAtMS: observedAtMS - 10, LastSeenAtMS: observedAtMS,
		},
		Turn: &Turn{
			TurnID: turnID, SessionID: sessionID, StartedAtMS: observedAtMS - 10,
			CompletedAtMS: &observedAtMS, Outcome: pointerTo("completed"), Model: &rawModel,
			CWD: &cwd, ProjectID: &projectID, SourceGeneration: 0,
			StartOffset: 10, CompleteOffset: pointerTo(int64(20)),
		},
		Usage: &TurnUsage{
			TurnID: turnID, ObservedAtMS: observedAtMS, IsFinal: true,
			InputTokens: &zero, CachedInputTokens: &zero, OutputTokens: &zero, ReasoningTokens: &zero,
			SourceGeneration: 0, SourceOffset: 20, Confidence: "exact", UpdatedAtMS: observedAtMS,
		},
	}); err != nil {
		t.Fatalf("seed project cost turn %q: %v", projectID, err)
	}
}
