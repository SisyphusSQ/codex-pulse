package indexer

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/SisyphusSQ/codex-pulse/internal/codex/logs"
	"github.com/SisyphusSQ/codex-pulse/internal/store"
	storesqlite "github.com/SisyphusSQ/codex-pulse/internal/store/sqlite"
)

func TestIngesterPersistsRestartSafePrivacyAttribution(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	repository, database, databasePath := openIndexerRepositoryAt(t)
	ingester, err := New(repository)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	home, rolloutPath := newSyntheticCodexHome(t)
	const privatePath = "/Users/alice/work/acme/api"
	const rawModel = "OpenAI/GPT-5.2-Codex"
	content := []byte(
		rolloutSessionMetaAttributionLine("session-attribution", privatePath) + "\n" +
			rolloutTurnStartLine("turn-attribution") + "\n" +
			rolloutTurnContextAttributionLine("turn-attribution", privatePath+"/internal/store", rawModel) + "\n" +
			rolloutTurnEndLine("turn-attribution") + "\n",
	)
	writeSyntheticRollout(t, rolloutPath, content, time.Unix(10, 0))
	discoverer, err := logs.NewDiscoverer(home)
	if err != nil {
		t.Fatalf("logs.NewDiscoverer() error = %v", err)
	}
	discovery, err := discoverer.Discover(ctx)
	if err != nil {
		t.Fatalf("Discover() error = %v", err)
	}
	plan, err := logs.PlanReconcile(home, nil, discovery)
	if err != nil || len(plan.Actions) != 1 {
		t.Fatalf("PlanReconcile() = %#v, %v", plan, err)
	}
	stream, err := ingester.Open(ctx, OpenRequest{Action: plan.Actions[0], AtMS: 10})
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	result, err := stream.Feed(ctx, content, true, 20)
	if err != nil || !result.Committed || result.Cursor.State != store.GenerationActive {
		t.Fatalf("Feed() = %#v, %v", result, err)
	}

	wantSession, err := repository.SessionAttribution(ctx, "session-attribution")
	if err != nil {
		t.Fatalf("SessionAttribution() error = %v", err)
	}
	wantTurn, err := repository.TurnAttribution(ctx, "turn-attribution")
	if err != nil {
		t.Fatalf("TurnAttribution() error = %v", err)
	}
	assertSafeAttributionJSON(t, wantSession, wantTurn, privatePath, rawModel)

	if err := database.Close(ctx); err != nil {
		t.Fatalf("Store.Close() error = %v", err)
	}
	reopened, err := storesqlite.Open(ctx, storesqlite.Config{Path: databasePath})
	if err != nil {
		t.Fatalf("sqlite.Open(reopen) error = %v", err)
	}
	t.Cleanup(func() { _ = reopened.Close(context.Background()) })
	reopenedRepository := store.NewRepository(reopened)
	if err := reopenedRepository.EnsureApplicationSchema(ctx); err != nil {
		t.Fatalf("EnsureApplicationSchema(reopen) error = %v", err)
	}
	gotSession, err := reopenedRepository.SessionAttribution(ctx, "session-attribution")
	if err != nil {
		t.Fatalf("SessionAttribution(reopen) error = %v", err)
	}
	gotTurn, err := reopenedRepository.TurnAttribution(ctx, "turn-attribution")
	if err != nil {
		t.Fatalf("TurnAttribution(reopen) error = %v", err)
	}
	if encodedAttribution(t, gotSession) != encodedAttribution(t, wantSession) ||
		encodedAttribution(t, gotTurn) != encodedAttribution(t, wantTurn) {
		t.Fatalf("reopened attribution changed:\nwant=%#v %#v\ngot=%#v %#v", wantSession, wantTurn, gotSession, gotTurn)
	}
}

func assertSafeAttributionJSON(
	t *testing.T,
	session store.SessionAttributionSnapshot,
	turn store.TurnAttributionSnapshot,
	privatePath string,
	rawModel string,
) {
	t.Helper()
	encoded := encodedAttribution(t, struct {
		Session store.SessionAttributionSnapshot `json:"session"`
		Turn    store.TurnAttributionSnapshot    `json:"turn"`
	}{Session: session, Turn: turn})
	if session.Project.ProjectID == nil || session.Project.DisplayName == nil ||
		*session.Project.DisplayName != "api" || session.Model.ModelKey == nil ||
		*session.Model.ModelKey != "gpt-5.2-codex" || turn.Model.ModelKey == nil ||
		*turn.Model.ModelKey != "gpt-5.2-codex" {
		t.Fatalf("attribution = %#v %#v", session, turn)
	}
	for _, forbidden := range []string{
		privatePath, "/Users/alice", "root_path", "initial_cwd", "current_cwd", `"cwd"`, rawModel,
	} {
		if strings.Contains(encoded, forbidden) {
			t.Fatalf("safe attribution leaked %q: %s", forbidden, encoded)
		}
	}
}

func encodedAttribution(t *testing.T, value any) string {
	t.Helper()
	encoded, err := json.Marshal(value)
	if err != nil {
		t.Fatalf("json.Marshal() error = %v", err)
	}
	return string(encoded)
}

func rolloutSessionMetaAttributionLine(sessionID, cwd string) string {
	return `{"timestamp":"2026-07-14T01:00:00Z","type":"session_meta","payload":{"id":"` + sessionID +
		`","timestamp":"2026-07-14T01:00:00Z","cwd":"` + cwd +
		`","originator":"codex_cli_rs","cli_version":"0.142.3","source":"cli","model_provider":"openai"}}`
}

func rolloutTurnContextAttributionLine(turnID, cwd, model string) string {
	return `{"timestamp":"2026-07-14T01:00:02Z","type":"turn_context","payload":{"turn_id":"` + turnID +
		`","cwd":"` + cwd + `","model":"` + model + `","effort":"high"}}`
}
