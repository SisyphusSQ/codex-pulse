package store

import (
	"context"
	"encoding/json"
	"errors"
	"reflect"
	"strings"
	"testing"

	"github.com/SisyphusSQ/codex-pulse/internal/attribution"
	storesqlite "github.com/SisyphusSQ/codex-pulse/internal/store/sqlite"
)

func TestRepositoryDerivesSafeSessionTurnProjectAndModelAttribution(t *testing.T) {
	t.Parallel()

	repository := openAttributionRepository(t)
	ctx := context.Background()
	initialCWD := "/Users/alice/work/acme/api"
	if err := repository.UpsertFacts(ctx, FactBatch{Session: &Session{
		SessionID: "session-1", Provider: "codex", SourceKind: "session",
		InitialCWD: &initialCWD, CreatedAtMS: 100, FirstSeenAtMS: 100, LastSeenAtMS: 100,
	}}); err != nil {
		t.Fatalf("UpsertFacts(session) error = %v", err)
	}

	rawModel := "OpenAI/GPT-5.2-Codex"
	turnCWD := "/Users/alice/work/acme/api/internal/store"
	if err := repository.UpsertFacts(ctx, FactBatch{
		Session: &Session{
			SessionID: "session-1", Provider: "codex", SourceKind: "session",
			InitialCWD: &initialCWD, CreatedAtMS: 100, FirstSeenAtMS: 100, LastSeenAtMS: 200,
		},
		Turn: &Turn{
			TurnID: "turn-1", SessionID: "session-1", StartedAtMS: 200,
			Model: &rawModel, CWD: &turnCWD, SourceGeneration: 0, StartOffset: 10,
		},
		SessionCurrent: &SessionCurrent{
			SessionID: "session-1", CurrentModel: &rawModel, CurrentCWD: &turnCWD,
			LastActivityAtMS: int64Pointer254(200), UpdatedAtMS: 200,
		},
	}); err != nil {
		t.Fatalf("UpsertFacts(turn) error = %v", err)
	}

	session, err := repository.SessionAttribution(ctx, "session-1")
	if err != nil {
		t.Fatalf("SessionAttribution() error = %v", err)
	}
	if session.SessionID != "session-1" || !strings.HasPrefix(session.DisplayTitle, "Session ") ||
		session.TitleConfidence != AttributionConfidenceHigh ||
		session.TitleSource != AttributionSourceSessionIDFallback ||
		session.Project.ProjectID == nil || session.Project.DisplayName == nil ||
		*session.Project.DisplayName != "api" ||
		session.Model.ModelKey == nil || *session.Model.ModelKey != "gpt-5.2-codex" ||
		session.Model.DisplayName == nil || *session.Model.DisplayName != "GPT-5.2 Codex" ||
		session.Model.Source != AttributionSourceModelAlias || session.RuleVersion != attribution.RuleVersion {
		t.Fatalf("session attribution = %#v", session)
	}

	turn, err := repository.TurnAttribution(ctx, "turn-1")
	if err != nil {
		t.Fatalf("TurnAttribution() error = %v", err)
	}
	if turn.Project.ProjectID == nil || turn.Project.DisplayName == nil || *turn.Project.DisplayName != "api" ||
		turn.Model.ModelKey == nil || *turn.Model.ModelKey != "gpt-5.2-codex" ||
		turn.Model.Source != AttributionSourceModelAlias || turn.RuleVersion != attribution.RuleVersion {
		t.Fatalf("turn attribution = %#v", turn)
	}

	rawSession, err := repository.Session(ctx, "session-1")
	if err != nil || rawSession.InitialCWD == nil || *rawSession.InitialCWD != initialCWD {
		t.Fatalf("raw session = %#v, err = %v", rawSession, err)
	}
	rawTurn, err := repository.Turn(ctx, "turn-1")
	if err != nil || rawTurn.Model == nil || *rawTurn.Model != rawModel ||
		rawTurn.CWD == nil || *rawTurn.CWD != turnCWD {
		t.Fatalf("raw turn = %#v, err = %v", rawTurn, err)
	}

	encoded, err := json.Marshal(struct {
		Session SessionAttributionSnapshot `json:"session"`
		Turn    TurnAttributionSnapshot    `json:"turn"`
	}{Session: session, Turn: turn})
	if err != nil {
		t.Fatalf("json.Marshal() error = %v", err)
	}
	serialized := string(encoded)
	for _, forbidden := range []string{
		"/Users/alice", "root_path", "initial_cwd", "current_cwd", `"cwd"`, rawModel,
	} {
		if strings.Contains(serialized, forbidden) {
			t.Fatalf("safe attribution leaked %q: %s", forbidden, serialized)
		}
	}
}

func TestRepositoryAttributionReturnsUnknownAndFailsClosedOnPeerConflict(t *testing.T) {
	t.Parallel()

	repository := openAttributionRepository(t)
	ctx := context.Background()
	if err := repository.UpsertFacts(ctx, FactBatch{Session: &Session{
		SessionID: "session-conflict", Provider: "codex", SourceKind: "session",
		CreatedAtMS: 100, FirstSeenAtMS: 100, LastSeenAtMS: 100,
	}}); err != nil {
		t.Fatalf("UpsertFacts(session) error = %v", err)
	}
	missing, err := repository.SessionAttribution(ctx, "session-conflict")
	if err != nil {
		t.Fatalf("SessionAttribution(missing) error = %v", err)
	}
	if missing.Project.ProjectID != nil || missing.Project.Confidence != AttributionConfidenceUnknown ||
		missing.Project.Source != AttributionSourceMissing || missing.Model.ModelKey != nil ||
		missing.Model.Confidence != AttributionConfidenceUnknown || missing.Model.Source != AttributionSourceMissing {
		t.Fatalf("missing attribution = %#v", missing)
	}

	turnModel := "gpt-5.2-codex"
	currentModel := "gpt-5.3-codex"
	turnCWD := "/Users/alice/work/one"
	currentCWD := "/Users/alice/work/two"
	if err := repository.UpsertFacts(ctx, FactBatch{
		Session: &Session{
			SessionID: "session-conflict", Provider: "codex", SourceKind: "session",
			CreatedAtMS: 100, FirstSeenAtMS: 100, LastSeenAtMS: 200,
		},
		Turn: &Turn{
			TurnID: "turn-conflict", SessionID: "session-conflict", StartedAtMS: 200,
			Model: &turnModel, CWD: &turnCWD, SourceGeneration: 0, StartOffset: 10,
		},
		SessionCurrent: &SessionCurrent{
			SessionID: "session-conflict", CurrentModel: &currentModel, CurrentCWD: &currentCWD,
			LastActivityAtMS: int64Pointer254(200), UpdatedAtMS: 200,
		},
	}); err != nil {
		t.Fatalf("UpsertFacts(conflict) error = %v", err)
	}
	conflict, err := repository.SessionAttribution(ctx, "session-conflict")
	if err != nil {
		t.Fatalf("SessionAttribution(conflict) error = %v", err)
	}
	if conflict.Project.ProjectID != nil || conflict.Project.DisplayName != nil ||
		conflict.Project.Confidence != AttributionConfidenceLow ||
		conflict.Project.Source != AttributionSourceConflict ||
		conflict.Model.ModelKey != nil || conflict.Model.DisplayName != nil ||
		conflict.Model.Confidence != AttributionConfidenceLow ||
		conflict.Model.Source != AttributionSourceConflict {
		t.Fatalf("conflict attribution = %#v", conflict)
	}
}

func TestRepositorySessionAttributionPreservesInvalidInputReasons(t *testing.T) {
	t.Parallel()

	repository := openAttributionRepository(t)
	ctx := context.Background()
	relativeCWD := "private/project"
	unsafeModel := "/Users/alice/private/model"
	if err := repository.UpsertFacts(ctx, FactBatch{
		Session: &Session{
			SessionID: "session-invalid", Provider: "codex", SourceKind: "session",
			InitialCWD: &relativeCWD, CreatedAtMS: 100, FirstSeenAtMS: 100, LastSeenAtMS: 100,
		},
		SessionCurrent: &SessionCurrent{
			SessionID: "session-invalid", CurrentModel: &unsafeModel, CurrentCWD: &relativeCWD,
			UpdatedAtMS: 100,
		},
	}); err != nil {
		t.Fatalf("UpsertFacts() error = %v", err)
	}
	derived, err := repository.SessionAttribution(ctx, "session-invalid")
	if err != nil || derived.Project.ProjectID != nil || derived.Project.DisplayName != nil ||
		derived.Project.Confidence != AttributionConfidenceUnknown ||
		derived.Project.Source != AttributionSourceInvalidPath ||
		derived.Project.Reason != AttributionReasonInvalid ||
		derived.Model.ModelKey != nil || derived.Model.DisplayName != nil ||
		derived.Model.Confidence != AttributionConfidenceUnknown ||
		derived.Model.Source != AttributionSourceInvalidModel ||
		derived.Model.Reason != AttributionReasonInvalid {
		t.Fatalf("SessionAttribution() = %#v, %v", derived, err)
	}
}

func TestRepositoryAttributionUsesExplicitRegisteredProjectRoot(t *testing.T) {
	t.Parallel()

	repository := openAttributionRepository(t)
	ctx := context.Background()
	root := "/Users/alice/work/registered"
	nested := root + "/internal/store"
	if err := repository.UpsertFacts(ctx, FactBatch{
		Project: &Project{
			ProjectID: "project-registered", DisplayName: "Registered Project", RootPath: root,
			CreatedAtMS: 100, UpdatedAtMS: 100,
		},
		Session: &Session{
			SessionID: "session-registered", Provider: "codex", SourceKind: "session",
			InitialCWD: &nested, CreatedAtMS: 100, FirstSeenAtMS: 100, LastSeenAtMS: 100,
		},
	}); err != nil {
		t.Fatalf("UpsertFacts() error = %v", err)
	}
	derived, err := repository.SessionAttribution(ctx, "session-registered")
	if err != nil || derived.Project.ProjectID == nil || *derived.Project.ProjectID != "project-registered" ||
		derived.Project.DisplayName == nil || *derived.Project.DisplayName != "Registered Project" ||
		derived.Project.Confidence != AttributionConfidenceHigh ||
		derived.Project.Source != AttributionSourceRegisteredRoot {
		t.Fatalf("SessionAttribution() = %#v, %v", derived, err)
	}
}

func TestRepositoryRecomputeAttributionsDoesNotRewriteCanonicalFacts(t *testing.T) {
	t.Parallel()

	repository := openAttributionRepository(t)
	ctx := context.Background()
	cwd := "/Users/alice/work/recompute"
	model := "gpt-5.2-codex"
	if err := repository.UpsertFacts(ctx, FactBatch{
		Session: &Session{
			SessionID: "session-recompute", Provider: "codex", SourceKind: "session",
			InitialCWD: &cwd, CreatedAtMS: 100, FirstSeenAtMS: 100, LastSeenAtMS: 200,
		},
		Turn: &Turn{
			TurnID: "turn-recompute", SessionID: "session-recompute", StartedAtMS: 200,
			Model: &model, CWD: &cwd, SourceGeneration: 0, StartOffset: 10,
		},
	}); err != nil {
		t.Fatalf("UpsertFacts() error = %v", err)
	}
	beforeSession, _ := repository.Session(ctx, "session-recompute")
	beforeTurn, _ := repository.Turn(ctx, "turn-recompute")

	if err := repository.database.Write(ctx, func(ctx context.Context, transaction storesqlite.WriteTx) error {
		return transaction.WithContext(ctx).Model(&sessionAttributionModel{}).
			Where("session_id = ?", "session-recompute").
			Update("display_title", "corrupted-derived-value").Error
	}); err != nil {
		t.Fatalf("corrupt derived fixture: %v", err)
	}
	report, err := repository.RecomputeAttributions(ctx, RecomputeAttributionsRequest{AtMS: 500})
	if err != nil {
		t.Fatalf("RecomputeAttributions() error = %v", err)
	}
	if report.RuleVersion != attribution.RuleVersion || report.Sessions != 1 || report.Turns != 1 {
		t.Fatalf("recompute report = %#v", report)
	}
	afterSession, _ := repository.Session(ctx, "session-recompute")
	afterTurn, _ := repository.Turn(ctx, "turn-recompute")
	if !reflect.DeepEqual(beforeSession, afterSession) || !reflect.DeepEqual(beforeTurn, afterTurn) {
		t.Fatalf("canonical facts changed:\nbefore=%#v %#v\nafter=%#v %#v", beforeSession, beforeTurn, afterSession, afterTurn)
	}
	derived, err := repository.SessionAttribution(ctx, "session-recompute")
	if err != nil || derived.DisplayTitle == "corrupted-derived-value" || derived.UpdatedAtMS != 500 {
		t.Fatalf("recomputed attribution = %#v, err = %v", derived, err)
	}
	secondReport, err := repository.RecomputeAttributions(ctx, RecomputeAttributionsRequest{AtMS: 500})
	if err != nil || !reflect.DeepEqual(secondReport, report) {
		t.Fatalf("second RecomputeAttributions() = %#v, %v; want %#v", secondReport, err, report)
	}
	secondDerived, err := repository.SessionAttribution(ctx, "session-recompute")
	if err != nil || !reflect.DeepEqual(secondDerived, derived) {
		t.Fatalf("second attribution = %#v, %v; want %#v", secondDerived, err, derived)
	}
}

func TestRepositoryRecomputeAttributionsIsIndependentOfSessionWriteOrder(t *testing.T) {
	t.Parallel()

	repository := openAttributionRepository(t)
	ctx := context.Background()
	parentCWD := "/Users/alice/work/order-independent"
	childCWD := parentCWD + "/nested"
	for _, fixture := range []struct {
		sessionID string
		cwd       string
		atMS      int64
	}{
		{sessionID: "session-z-parent-first", cwd: parentCWD, atMS: 100},
		{sessionID: "session-a-child-second", cwd: childCWD, atMS: 200},
	} {
		if err := repository.UpsertFacts(ctx, FactBatch{Session: &Session{
			SessionID: fixture.sessionID, Provider: "codex", SourceKind: "session",
			InitialCWD: &fixture.cwd, CreatedAtMS: fixture.atMS,
			FirstSeenAtMS: fixture.atMS, LastSeenAtMS: fixture.atMS,
		}}); err != nil {
			t.Fatalf("UpsertFacts(%s) error = %v", fixture.sessionID, err)
		}
	}

	before := make(map[string]string)
	for _, sessionID := range []string{"session-z-parent-first", "session-a-child-second"} {
		derived, err := repository.SessionAttribution(ctx, sessionID)
		if err != nil || derived.Project.ProjectID == nil {
			t.Fatalf("SessionAttribution(%s) = %#v, %v", sessionID, derived, err)
		}
		before[sessionID] = *derived.Project.ProjectID
	}
	if before["session-z-parent-first"] == before["session-a-child-second"] {
		t.Fatalf("distinct path-derived roots collapsed before recompute: %v", before)
	}

	if _, err := repository.RecomputeAttributions(ctx, RecomputeAttributionsRequest{AtMS: 300}); err != nil {
		t.Fatalf("RecomputeAttributions() error = %v", err)
	}
	for sessionID, wantProjectID := range before {
		derived, err := repository.SessionAttribution(ctx, sessionID)
		if err != nil || derived.Project.ProjectID == nil || *derived.Project.ProjectID != wantProjectID {
			t.Fatalf("SessionAttribution(%s) after recompute = %#v, %v; want project %q", sessionID, derived, err, wantProjectID)
		}
	}
}

func TestRepositoryAttributionFailureRollsBackCanonicalFacts(t *testing.T) {
	t.Parallel()

	repository := openAttributionRepository(t)
	ctx := context.Background()
	if err := repository.database.Write(ctx, func(ctx context.Context, transaction storesqlite.WriteTx) error {
		return transaction.WithContext(ctx).Exec(`CREATE TRIGGER fail_attribution_insert
			BEFORE INSERT ON session_attributions
			BEGIN SELECT RAISE(ABORT, 'injected attribution failure'); END`).Error
	}); err != nil {
		t.Fatalf("create failure trigger: %v", err)
	}

	err := repository.UpsertFacts(ctx, FactBatch{Session: &Session{
		SessionID: "session-rollback", Provider: "codex", SourceKind: "session",
		CreatedAtMS: 100, FirstSeenAtMS: 100, LastSeenAtMS: 100,
	}})
	if err == nil {
		t.Fatal("UpsertFacts() error = nil, want injected failure")
	}
	if _, err := repository.Session(ctx, "session-rollback"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("Session() error = %v, want ErrNotFound", err)
	}
}

func TestWriteUnitRefreshesAttributionOncePerDirtySession(t *testing.T) {
	t.Parallel()

	repository := openAttributionRepository(t)
	ctx := context.Background()
	cwd := "/Users/alice/work/dirty-session"
	if err := repository.UpsertFacts(ctx, FactBatch{Session: &Session{
		SessionID: "session-dirty", Provider: "codex", SourceKind: "session",
		InitialCWD: &cwd, CreatedAtMS: 100, FirstSeenAtMS: 100, LastSeenAtMS: 100,
	}}); err != nil {
		t.Fatalf("UpsertFacts(session) error = %v", err)
	}
	for index := 0; index < 3; index++ {
		turnID := "turn-dirty-" + string(rune('a'+index))
		model := "gpt-5.2-codex"
		startedAt := int64(200 + index)
		if err := repository.UpsertFacts(ctx, FactBatch{
			Session: &Session{
				SessionID: "session-dirty", Provider: "codex", SourceKind: "session",
				InitialCWD: &cwd, CreatedAtMS: 100, FirstSeenAtMS: 100, LastSeenAtMS: startedAt,
			},
			Turn: &Turn{
				TurnID: turnID, SessionID: "session-dirty", StartedAtMS: startedAt,
				Model: &model, CWD: &cwd, SourceGeneration: 0, StartOffset: int64(index + 1),
			},
		}); err != nil {
			t.Fatalf("UpsertFacts(%s) error = %v", turnID, err)
		}
	}
	if err := repository.database.Write(ctx, func(ctx context.Context, transaction storesqlite.WriteTx) error {
		if err := transaction.WithContext(ctx).Exec(`CREATE TABLE attribution_refresh_audit (
			id INTEGER PRIMARY KEY
		) STRICT`).Error; err != nil {
			return err
		}
		return transaction.WithContext(ctx).Exec(`CREATE TRIGGER audit_turn_attribution_refresh
			AFTER UPDATE ON turn_attributions
			BEGIN INSERT INTO attribution_refresh_audit (id) VALUES (NULL); END`).Error
	}); err != nil {
		t.Fatalf("create attribution refresh audit: %v", err)
	}

	if err := repository.WithinWriteUnit(ctx, func(unit *WriteUnit) error {
		for index := 0; index < 3; index++ {
			lastSeen := int64(300 + index)
			if err := unit.UpsertFacts(FactBatch{Session: &Session{
				SessionID: "session-dirty", Provider: "codex", SourceKind: "session",
				InitialCWD: &cwd, CreatedAtMS: 100, FirstSeenAtMS: 100, LastSeenAtMS: lastSeen,
			}}); err != nil {
				return err
			}
		}
		return nil
	}); err != nil {
		t.Fatalf("WithinWriteUnit() error = %v", err)
	}

	var refreshes int64
	if err := repository.database.View(ctx, func(ctx context.Context, connection storesqlite.ReadConn) error {
		return connection.WithContext(ctx).Table("attribution_refresh_audit").Count(&refreshes).Error
	}); err != nil {
		t.Fatalf("count attribution refreshes: %v", err)
	}
	if refreshes != 3 {
		t.Fatalf("turn attribution refreshes = %d, want one refresh of 3 turns", refreshes)
	}
}

func openAttributionRepository(t *testing.T) *Repository {
	t.Helper()
	database := openTestDatabase(t)
	repository := NewRepository(database)
	if err := repository.EnsureApplicationSchema(context.Background()); err != nil {
		t.Fatalf("EnsureApplicationSchema() error = %v", err)
	}
	return repository
}

func int64Pointer254(value int64) *int64 { return &value }
