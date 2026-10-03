package reporting

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"testing"

	"gorm.io/gorm"

	reportingv1 "github.com/SisyphusSQ/codex-pulse/api/codexpulse/reporting/v1"
)

func TestReportingIdentityMappingAndFactsCheckpointSurviveReopen(t *testing.T) {
	state, path := testState(t)
	ctx := t.Context()
	a := AccountIdentity{Provider: "codex", LocalScope: "scope-a", AccountID: "raw-account-a", Email: new("synthetic@example.invalid"), ConfirmedAtMS: 1000, CollectedAtMS: 1000}
	if err := state.RememberIdentities(ctx, []AccountIdentity{a}); err != nil {
		t.Fatal(err)
	}
	bad := a
	bad.AccountID = "raw-account-b"
	if err := state.RememberIdentities(ctx, []AccountIdentity{bad}); !errors.Is(err, ErrProtocol) {
		t.Fatal("scope rebound to B", err)
	}
	b := a
	b.LocalScope = "scope-b"
	b.AccountID = "raw-account-b"
	b.Email = nil
	if err := state.RememberIdentities(ctx, []AccountIdentity{b}); err != nil {
		t.Fatal(err)
	}
	key := reportingv1.Key("quota", "a")
	facts := reportingv1.Batch{Accounts: []reportingv1.Account{{Provider: "codex", ID: a.AccountID, Email: a.Email, CollectedAtMS: 1000}}, Bindings: []reportingv1.AccountBinding{{Provider: "codex", LocalScope: state.PublicScope("codex", a.LocalScope), AccountID: a.AccountID, ConfirmedAtMS: 1000}}}
	changed, err := state.EnqueueCheckedFacts(ctx, "center", key, facts)
	if err != nil || !changed {
		t.Fatal("enqueue", err)
	}
	first, err := state.next(ctx, "center")
	if err != nil {
		t.Fatal(err)
	}
	if changed, err = state.EnqueueCheckedFacts(ctx, "center", key, facts); err != nil || changed {
		t.Fatal("same facts duplicated", err)
	}
	if err := state.Close(ctx); err != nil {
		t.Fatal(err)
	}
	reopened, err := OpenState(ctx, path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = reopened.Close(context.Background()) })
	second, err := reopened.next(ctx, "center")
	if err != nil || string(first.Body) != string(second.Body) || first.BatchID != second.BatchID {
		t.Fatal("retry body changed after reopening", err)
	}
	identities, err := reopened.Identities(ctx)
	if err != nil || len(identities) != 2 || identities[0].AccountID != a.AccountID || identities[1].AccountID != b.AccountID {
		t.Fatal("identity isolation lost", err)
	}
	if err := reopened.clearPending(ctx, "center"); err != nil {
		t.Fatal(err)
	}
	if changed, err = reopened.EnqueueCheckedFacts(ctx, "center", key, facts); err != nil || !changed {
		t.Fatal("explicit discard suppressed resubmission", err)
	}
}
func TestReportingV1MigrationPreservesQueueAndIdentitySalt(t *testing.T) {
	state, path := testState(t)
	ctx := t.Context()
	salt := state.HomeID("scope")
	if err := state.Enqueue(ctx, "center", "sweep", testSnapshot()); err != nil {
		t.Fatal(err)
	}
	before, _ := state.next(ctx, "center")
	if err := state.db.Write(ctx, func(_ context.Context, db *gorm.DB) error {
		if err := db.Exec("DROP TABLE reporting_account_identities").Error; err != nil {
			return err
		}
		if err := db.Exec("DROP TABLE reporting_facts_checkpoints").Error; err != nil {
			return err
		}
		return db.Exec("UPDATE reporting_schema SET version=1").Error
	}); err != nil {
		t.Fatal(err)
	}
	_ = state.Close(ctx)
	reopened, err := OpenState(ctx, path)
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close(context.Background())
	after, err := reopened.next(ctx, "center")
	if err != nil || reopened.HomeID("scope") != salt || string(before.Body) != string(after.Body) {
		t.Fatal("migration replaced salt or pending facts", err)
	}
	identities, err := reopened.Identities(ctx)
	if err != nil || len(identities) != 0 {
		t.Fatal("migration inferred account IDs", err)
	}
}

func TestReportingFactPageIsAtomicBatchedAndReplaySafe(t *testing.T) {
	state, _ := testState(t)
	groups := []FactsGroup{}
	for i := range 200 {
		q := reportingv1.QuotaObservation{Provider: "codex", ID: fmt.Sprintf("q-%d", i), LocalScope: "scope", LimitID: "codex", WindowKind: "primary", ObservedAtMS: int64(i), UsedPercent: new(0.0), WindowMinutes: new(int64(300)), ResetsAtMS: new(int64(9999999)), Source: "app_server", Validity: "accepted", HistoryOrigin: "pending_association"}
		groups = append(groups, FactsGroup{Key: reportingv1.Key(q.ID), Batch: reportingv1.Batch{Quotas: []reportingv1.QuotaObservation{q}}})
	}
	changed, err := state.EnqueueFactGroups(t.Context(), "center", groups)
	if err != nil || !changed {
		t.Fatal(err)
	}
	first, err := state.next(t.Context(), "center")
	if err != nil {
		t.Fatal(err)
	}
	var batch reportingv1.Batch
	if err := json.Unmarshal(first.Body, &batch); err != nil {
		t.Fatal(err)
	}
	if len(batch.Quotas) != 200 {
		t.Fatal("not batched")
	}
	var count int64
	_ = state.db.View(t.Context(), func(_ context.Context, db *gorm.DB) error { return db.Model(&queued{}).Count(&count).Error })
	if count != 1 {
		t.Fatal("produced per-fact queue burst", count)
	}
	if changed, err := state.EnqueueFactGroups(t.Context(), "center", groups); err != nil || changed {
		t.Fatal("same page duplicated", err)
	}
	next := groups[0]
	next.Batch.Quotas = append([]reportingv1.QuotaObservation(nil), next.Batch.Quotas...)
	next.Batch.Quotas[0].HistoryOrigin = "confirmed"
	next.Batch.Quotas[0].AccountID = new("raw-account-a")
	if err := state.db.Write(t.Context(), func(_ context.Context, db *gorm.DB) error {
		rows := []queued{}
		for i := range QueueBatchBudget - 1 {
			rows = append(rows, queued{Partition: "other", BatchID: fmt.Sprintf("fill-%d", i), Body: []byte("x")})
		}
		return db.CreateInBatches(rows, 100).Error
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := state.EnqueueFactGroups(t.Context(), "center", []FactsGroup{next}); !errors.Is(err, ErrQueueFull) {
		t.Fatal("full page accepted", err)
	}
	var cp factsCheckpoint
	_ = state.db.View(t.Context(), func(_ context.Context, db *gorm.DB) error {
		return db.Where("partition = ? AND source_key = ?", "center", next.Key).Take(&cp).Error
	})
	if err := state.db.Write(t.Context(), func(_ context.Context, db *gorm.DB) error {
		return db.Where("partition = ?", "other").Delete(&queued{}).Error
	}); err != nil {
		t.Fatal(err)
	}
	if changed, err := state.EnqueueFactGroups(t.Context(), "center", []FactsGroup{next}); err != nil || !changed {
		t.Fatal("failed page advanced checkpoint", err)
	}
}
