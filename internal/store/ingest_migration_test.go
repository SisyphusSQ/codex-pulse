package store

import (
	"context"
	"testing"

	storesqlite "github.com/SisyphusSQ/codex-pulse/internal/store/sqlite"
)

func TestEnsureCoreSchemaUsesCurrentTurnDDL(t *testing.T) {
	t.Parallel()

	database := openTestDatabase(t)
	if err := NewRepository(database).EnsureCoreSchema(context.Background()); err != nil {
		t.Fatalf("EnsureCoreSchema() error = %v", err)
	}
	err := database.View(context.Background(), func(ctx context.Context, connection storesqlite.ReadConn) error {
		valid, err := verifySchemaObject(ctx, connection, currentTurnsSchemaObject())
		if err != nil {
			return err
		}
		if valid {
			return nil
		}
		var actual string
		if err := connection.WithContext(ctx).Raw(
			`SELECT sql FROM sqlite_schema WHERE type = 'table' AND name = 'turns'`,
		).Scan(&actual).Error; err != nil {
			return err
		}
		t.Fatalf("turns DDL differs:\nactual: %s\nwant: %s", actual, turnsSchemaCurrentStatement)
		return nil
	})
	if err != nil {
		t.Fatalf("inspect current turns DDL: %v", err)
	}
}

func TestHistoricalV1BootstrapAcceptsCurrentCoreTurnDDL(t *testing.T) {
	t.Parallel()

	database := openTestDatabase(t)
	repository := NewRepository(database)
	if err := repository.EnsureCoreSchema(context.Background()); err != nil {
		t.Fatalf("EnsureCoreSchema() error = %v", err)
	}
	if err := database.Write(context.Background(), func(ctx context.Context, transaction storesqlite.WriteTx) error {
		return ensureApplicationSchemaV1(ctx, transaction)
	}); err != nil {
		t.Fatalf("ensureApplicationSchemaV1(current core) error = %v", err)
	}
}

func TestTurnCompletionAllowsTerminalBeforeStartSourceOffset(t *testing.T) {
	t.Parallel()

	repository := openRuntimeRepository(t)
	session := Session{
		SessionID: "late-start-session", Provider: "codex", SourceKind: "session",
		CreatedAtMS: 1_000, FirstSeenAtMS: 1_000, LastSeenAtMS: 2_000,
	}
	if err := repository.UpsertFacts(context.Background(), FactBatch{Session: &session}); err != nil {
		t.Fatalf("UpsertFacts(session) error = %v", err)
	}
	completedAt := int64(2_000)
	completeOffset := int64(20)
	outcome := "completed"
	turn := Turn{
		TurnID: "late-start-turn", SessionID: session.SessionID, StartedAtMS: 1_500,
		CompletedAtMS: &completedAt, Outcome: &outcome, SourceGeneration: 0,
		StartOffset: 100, CompleteOffset: &completeOffset,
	}
	if err := repository.UpsertFacts(context.Background(), FactBatch{Turn: &turn}); err != nil {
		t.Fatalf("UpsertFacts(terminal-before-start turn) error = %v", err)
	}

	got, err := repository.Turn(context.Background(), turn.TurnID)
	if err != nil {
		t.Fatalf("Turn() error = %v", err)
	}
	if got.StartOffset != 100 || got.CompleteOffset == nil || *got.CompleteOffset != 20 {
		t.Fatalf("Turn() source positions = start:%d complete:%v, want 100/20", got.StartOffset, got.CompleteOffset)
	}
}
