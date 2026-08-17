package cursorprovider

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/SisyphusSQ/codex-pulse/internal/store"
	_ "modernc.org/sqlite"
)

type snapshotCapture struct {
	mu       sync.Mutex
	snapshot store.CursorSnapshot
	writes   int
}

func (capture *snapshotCapture) ReplaceCursorSnapshot(_ context.Context, snapshot store.CursorSnapshot) error {
	capture.mu.Lock()
	defer capture.mu.Unlock()
	capture.snapshot = snapshot
	capture.writes++
	return nil
}

func TestCollectorCancellationDoesNotReplaceLastGoodSnapshot(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	capture := &snapshotCapture{}
	collector, err := NewCollector(capture, Config{
		ProjectsRoot:         filepath.Join(root, "projects"),
		StateDatabase:        filepath.Join(root, "state.vscdb"),
		ConversationDatabase: filepath.Join(root, "conversation.db"),
		AITrackingDatabase:   filepath.Join(root, "tracking.db"),
		MinimumRefresh:       0,
		Now:                  func() time.Time { return time.UnixMilli(2_000) },
	})
	if err != nil {
		t.Fatalf("NewCollector() error = %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := collector.Refresh(ctx); !errors.Is(err, context.Canceled) {
		t.Fatalf("Refresh(cancelled) error = %v", err)
	}
	if capture.writes != 0 {
		t.Fatalf("cancelled collection persisted %d snapshots", capture.writes)
	}
}

func TestCollectorReadsWALSnapshotsDeduplicatesUsageAndDropsContent(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	projects := filepath.Join(root, "projects")
	statePath := filepath.Join(root, "state.vscdb")
	conversationPath := filepath.Join(root, "conversation-search.db")
	aiPath := filepath.Join(root, "ai-code-tracking.db")
	externalID := "11111111-1111-1111-1111-111111111111"
	secret := "SENSITIVE_PROMPT_BODY_NEVER_PERSIST"

	for index, body := range []string{
		`{"role":"user","message":{"role":"user","content":[{"type":"text","text":"` + secret + `"},{"type":"tool_use","name":"Read","input":{"path":"/private/a"}}]}}` + "\n",
		`{"role":"assistant","message":{"role":"assistant","content":[{"type":"text","text":"different response"},{"type":"tool_use","name":"Read","output":"private"}]}}` + "\n",
	} {
		directory := filepath.Join(projects, string(rune('a'+index)), "agent-transcripts")
		if err := os.MkdirAll(directory, 0o700); err != nil {
			t.Fatalf("MkdirAll(transcript) error = %v", err)
		}
		if err := os.WriteFile(filepath.Join(directory, externalID+".jsonl"), []byte(body), 0o600); err != nil {
			t.Fatalf("WriteFile(transcript) error = %v", err)
		}
	}

	stateDB := openFixtureDatabase(t, statePath)
	t.Cleanup(func() { _ = stateDB.Close() })
	mustExec(t, stateDB, `PRAGMA journal_mode=WAL`)
	mustExec(t, stateDB, `PRAGMA user_version=1`)
	mustExec(t, stateDB, `CREATE TABLE composerHeaders (composerId TEXT, createdAt INTEGER, lastUpdatedAt INTEGER, value TEXT)`)
	mustExec(t, stateDB, `CREATE TABLE cursorDiskKV (key TEXT PRIMARY KEY, value TEXT)`)
	mustExec(t, stateDB, `INSERT INTO composerHeaders VALUES (?, ?, ?, ?)`, externalID, int64(1_780_000_000_000), int64(1_780_000_001_000),
		`{"modelId":"cursor-model","name":"Refactor collector","workspaceIdentifier":{"id":"workspace-a","uri":{"fsPath":"/Users/private/secret-project"}},"totalLinesAdded":7,"totalLinesRemoved":2}`)
	bubble := func(id, request string, input, output int64, toolID string) string {
		value, err := json.Marshal(map[string]any{
			"requestId": request, "createdAt": "1780000001000",
			"modelInfo":      map[string]any{"modelName": "cursor-model"},
			"tokenCount":     map[string]any{"inputTokens": input, "outputTokens": output},
			"toolFormerData": map[string]any{"toolCallId": toolID, "name": "Read", "status": "completed"},
			"text":           secret,
		})
		if err != nil {
			t.Fatalf("marshal bubble: %v", err)
		}
		return string(value)
	}
	mustExec(t, stateDB, `INSERT INTO cursorDiskKV VALUES (?, ?)`, "bubbleId:"+externalID+":aaaaaaaa-aaaa-aaaa-aaaa-aaaaaaaaaaaa", bubble("a", "generation-1", 100, 20, "tool-1"))
	mustExec(t, stateDB, `INSERT INTO cursorDiskKV VALUES (?, ?)`, "bubbleId:"+externalID+":bbbbbbbb-bbbb-bbbb-bbbb-bbbbbbbbbbbb", bubble("b", "generation-1", 100, 20, "tool-1"))
	mustExec(t, stateDB, `INSERT INTO cursorDiskKV VALUES (?, ?)`, "bubbleId:"+externalID+":cccccccc-cccc-cccc-cccc-cccccccccccc", bubble("c", "unstable", 1, 2, "tool-2"))
	mustExec(t, stateDB, `INSERT INTO cursorDiskKV VALUES (?, ?)`, "bubbleId:"+externalID+":dddddddd-dddd-dddd-dddd-dddddddddddd", bubble("d", "unstable", 1, 3, "tool-2"))

	conversationDB := openFixtureDatabase(t, conversationPath)
	mustExec(t, conversationDB, `PRAGMA user_version=9`)
	mustExec(t, conversationDB, `CREATE TABLE conversations (id TEXT, source TEXT, updated_at INTEGER, title TEXT, body TEXT)`)
	mustExec(t, conversationDB, `INSERT INTO conversations VALUES (?, 'local', ?, ?, ?)`, externalID, int64(1_780_000_001_000), "Stale search title", secret)
	_ = conversationDB.Close()

	aiDB := openFixtureDatabase(t, aiPath)
	mustExec(t, aiDB, `PRAGMA user_version=0`)
	mustExec(t, aiDB, `CREATE TABLE ai_code_hashes (requestId TEXT, conversationId TEXT, timestamp INTEGER, model TEXT, path TEXT, tracked_file_content TEXT)`)
	mustExec(t, aiDB, `INSERT INTO ai_code_hashes VALUES ('edit-1', ?, ?, 'cursor-model', '/private/file.go', ?)`, externalID, int64(1_780_000_001_500), secret)
	_ = aiDB.Close()

	capture := &snapshotCapture{}
	collector, err := NewCollector(capture, Config{
		ProjectsRoot: projects, StateDatabase: statePath, ConversationDatabase: conversationPath,
		AITrackingDatabase: aiPath, MinimumRefresh: 0,
		Now: func() time.Time { return time.UnixMilli(1_780_000_002_000) },
	})
	if err != nil {
		t.Fatalf("NewCollector() error = %v", err)
	}
	if err := collector.Refresh(context.Background()); err != nil {
		t.Fatalf("Refresh() error = %v", err)
	}
	snapshot := capture.snapshot
	if len(snapshot.RequestEvents) != 2 {
		t.Fatalf("request events = %#v, want two distinct generation IDs", snapshot.RequestEvents)
	}
	if len(snapshot.UsageEvents) != 1 || snapshot.UsageEvents[0].EventID != "generation-1" ||
		snapshot.UsageEvents[0].InputTokens != 100 || snapshot.UsageEvents[0].OutputTokens != 20 {
		t.Fatalf("usage events = %#v", snapshot.UsageEvents)
	}
	if len(snapshot.Sessions) != 1 || !snapshot.Sessions[0].LineageConflict || snapshot.Sessions[0].RequestCount != 2 {
		t.Fatalf("sessions = %#v", snapshot.Sessions)
	}
	if snapshot.Sessions[0].DisplayTitle != "Refactor collector" ||
		snapshot.Sessions[0].TitleSource != "cursor_composer_header" ||
		snapshot.Sessions[0].ProjectDisplayName != "secret-project" {
		t.Fatalf("session metadata = %#v", snapshot.Sessions[0])
	}
	if len(snapshot.Lineage) != 2 {
		t.Fatalf("lineage count = %d, want 2", len(snapshot.Lineage))
	}
	if len(snapshot.ToolEvents) != 2 {
		t.Fatalf("tool events = %#v, want state events deduplicated from transcript", snapshot.ToolEvents)
	}
	if len(snapshot.AIEditEvents) != 1 || snapshot.AIEditEvents[0].EditCount != 1 {
		t.Fatalf("AI edit events = %#v", snapshot.AIEditEvents)
	}
	stateSource := sourceStatus(snapshot.Sources, SourceState)
	if stateSource == nil || stateSource.State != "partial" || stateSource.CoverageState != "partial" {
		t.Fatalf("state source = %#v, want partial after conflicting generation tokens", stateSource)
	}
	encoded, err := json.Marshal(snapshot)
	if err != nil {
		t.Fatalf("Marshal(snapshot) error = %v", err)
	}
	for _, forbidden := range []string{secret, "/Users/private", "/private/file.go", "different response"} {
		if strings.Contains(string(encoded), forbidden) {
			t.Fatalf("snapshot contains forbidden content marker %q", forbidden)
		}
	}
	if _, err := stateDB.Exec(`INSERT INTO cursorDiskKV VALUES ('post-collector', '{}')`); err != nil {
		t.Fatalf("collector modified or locked source database: %v", err)
	}
}

func TestCollectorFiltersTransientHeadersAndKeepsEvidenceBackedSessions(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	projects := filepath.Join(root, "projects")
	statePath := filepath.Join(root, "state.vscdb")
	conversationPath := filepath.Join(root, "conversation-search.db")
	aiPath := filepath.Join(root, "ai-code-tracking.db")
	transcriptID := "11111111-1111-1111-1111-111111111111"
	requestID := "22222222-2222-2222-2222-222222222222"
	formalID := "33333333-3333-3333-3333-333333333333"
	searchID := "44444444-4444-4444-4444-444444444444"
	archivedID := "55555555-5555-5555-5555-555555555555"

	transcriptDirectory := filepath.Join(projects, "redacted-bucket", "agent-transcripts")
	if err := os.MkdirAll(transcriptDirectory, 0o700); err != nil {
		t.Fatalf("MkdirAll(transcript) error = %v", err)
	}
	if err := os.WriteFile(filepath.Join(transcriptDirectory, transcriptID+".jsonl"), []byte("{}\n"), 0o600); err != nil {
		t.Fatalf("WriteFile(transcript) error = %v", err)
	}

	stateDB := openFixtureDatabase(t, statePath)
	t.Cleanup(func() { _ = stateDB.Close() })
	mustExec(t, stateDB, `PRAGMA journal_mode=WAL`)
	mustExec(t, stateDB, `PRAGMA user_version=1`)
	mustExec(t, stateDB, `CREATE TABLE composerHeaders (composerId TEXT, createdAt INTEGER, lastUpdatedAt INTEGER, value TEXT)`)
	mustExec(t, stateDB, `CREATE TABLE cursorDiskKV (key TEXT PRIMARY KEY, value TEXT)`)
	headers := []struct {
		id    string
		value string
	}{
		{"aaaaaaaa-aaaa-aaaa-aaaa-aaaaaaaaaaaa", `{"isDraft":true,"workspaceIdentifier":{"id":"draft"}}`},
		{"bbbbbbbb-bbbb-bbbb-bbbb-bbbbbbbbbbbb", `{"isEphemeral":true,"workspaceIdentifier":{"id":"ephemeral"}}`},
		{transcriptID, `{"isDraft":true,"workspaceIdentifier":{"id":"workspace-transcript","uri":{"fsPath":"/private/one/shared"}}}`},
		{requestID, `{"isEphemeral":true,"workspaceIdentifier":{"id":"workspace-request","uri":{"fsPath":"/private/two/shared"}}}`},
		{formalID, `{"name":"Composer title","workspaceIdentifier":{"id":"workspace-formal","uri":{"fsPath":"/private/three/formal"}}}`},
		{searchID, `{"workspaceIdentifier":{"id":"workspace-search","uri":{"fsPath":"/private/four/search"}}}`},
		{archivedID, `{"name":"Archived title","isArchived":true,"workspaceIdentifier":{"id":"workspace-archived","uri":{"fsPath":"/private/five/archived"}}}`},
	}
	for _, header := range headers {
		mustExec(t, stateDB, `INSERT INTO composerHeaders VALUES (?, 1000, 2000, ?)`, header.id, header.value)
	}
	mustExec(t, stateDB, `INSERT INTO cursorDiskKV VALUES (?, ?)`, "bubbleId:"+requestID+":cccccccc-cccc-cccc-cccc-cccccccccccc",
		`{"requestId":"generation-request","createdAt":"2500","tokenCount":{"inputTokens":1,"outputTokens":2}}`)

	conversationDB := openFixtureDatabase(t, conversationPath)
	mustExec(t, conversationDB, `PRAGMA user_version=9`)
	mustExec(t, conversationDB, `CREATE TABLE conversations (id TEXT, source TEXT, updated_at INTEGER, title TEXT, body TEXT)`)
	mustExec(t, conversationDB, `INSERT INTO conversations VALUES (?, 'local', 3000, 'Stale composer title', 'PRIVATE_BODY')`, formalID)
	mustExec(t, conversationDB, `INSERT INTO conversations VALUES (?, 'local', 3000, 'Search title', 'PRIVATE_BODY')`, searchID)
	_ = conversationDB.Close()

	aiDB := openFixtureDatabase(t, aiPath)
	mustExec(t, aiDB, `PRAGMA user_version=0`)
	mustExec(t, aiDB, `CREATE TABLE ai_code_hashes (requestId TEXT, conversationId TEXT, timestamp INTEGER, model TEXT)`)
	_ = aiDB.Close()

	capture := &snapshotCapture{}
	collector, err := NewCollector(capture, Config{
		ProjectsRoot: projects, StateDatabase: statePath, ConversationDatabase: conversationPath,
		AITrackingDatabase: aiPath, MinimumRefresh: 0,
		Now: func() time.Time { return time.UnixMilli(4_000) },
	})
	if err != nil {
		t.Fatalf("NewCollector() error = %v", err)
	}
	if err := collector.Refresh(context.Background()); err != nil {
		t.Fatalf("Refresh() error = %v", err)
	}

	sessions := make(map[string]store.CursorSession, len(capture.snapshot.Sessions))
	for _, session := range capture.snapshot.Sessions {
		sessions[session.ExternalSessionID] = session
	}
	if len(sessions) != 5 {
		t.Fatalf("sessions = %#v, want five formal or evidence-backed sessions", sessions)
	}
	for _, transientID := range []string{"aaaaaaaa-aaaa-aaaa-aaaa-aaaaaaaaaaaa", "bbbbbbbb-bbbb-bbbb-bbbb-bbbbbbbbbbbb"} {
		if _, exists := sessions[transientID]; exists {
			t.Fatalf("transient session %q was retained", transientID)
		}
	}
	if sessions[formalID].DisplayTitle != "Composer title" || sessions[formalID].TitleSource != "cursor_composer_header" {
		t.Fatalf("formal session = %#v", sessions[formalID])
	}
	if sessions[searchID].DisplayTitle != "Search title" || sessions[searchID].TitleSource != "cursor_conversation_search" {
		t.Fatalf("search session = %#v", sessions[searchID])
	}
	if sessions[archivedID].DisplayTitle != "Archived title" {
		t.Fatalf("archived session = %#v", sessions[archivedID])
	}
	if sessions[transcriptID].TitleSource != "fallback" || sessions[requestID].TitleSource != "fallback" {
		t.Fatalf("evidence fallbacks = transcript %#v request %#v", sessions[transcriptID], sessions[requestID])
	}
	if sessions[transcriptID].ProjectDisplayName != "shared" || sessions[requestID].ProjectDisplayName != "shared" ||
		sessions[transcriptID].ProjectKey == sessions[requestID].ProjectKey {
		t.Fatalf("same-name projects were not kept distinct: transcript %#v request %#v", sessions[transcriptID], sessions[requestID])
	}
	encoded, err := json.Marshal(capture.snapshot)
	if err != nil {
		t.Fatalf("Marshal(snapshot) error = %v", err)
	}
	for _, forbidden := range []string{"/private/", "PRIVATE_BODY", "Stale composer title"} {
		if strings.Contains(string(encoded), forbidden) {
			t.Fatalf("snapshot contains forbidden value %q", forbidden)
		}
	}
}

func TestTranscriptLineageConflictDoesNotArbitrarilyAttributeProject(t *testing.T) {
	t.Parallel()

	snapshot := newSnapshot(4_000)
	session := snapshot.session("11111111-1111-1111-1111-111111111111", 1_000)
	snapshot.setSessionProject(session, "transcript:bucket-b", "未识别项目", 1)
	snapshot.setSessionProject(session, "transcript:bucket-a", "未识别项目", 1)
	snapshot.lineage[session.ExternalSessionID] = map[string]struct{}{
		"content-a": {},
		"content-b": {},
	}
	snapshot.finalize()

	if len(snapshot.Sessions) != 1 {
		t.Fatalf("sessions = %#v", snapshot.Sessions)
	}
	got := snapshot.Sessions[0]
	wantProjectKey := digestString("cursor:unknown-project:" + got.ExternalSessionID)
	if !got.LineageConflict || got.ProjectKey != wantProjectKey || got.ProjectDisplayName != "未识别项目" {
		t.Fatalf("conflicting transcript attribution = %#v", got)
	}
}

func sourceStatus(
	sources []store.CursorSourceStatus,
	key string,
) *store.CursorSourceStatus {
	for index := range sources {
		if sources[index].SourceKey == key {
			return &sources[index]
		}
	}
	return nil
}

func TestCollectorMarksSchemaDriftUnavailableWithoutPersistingBodies(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	statePath := filepath.Join(root, "state.vscdb")
	database := openFixtureDatabase(t, statePath)
	mustExec(t, database, `PRAGMA user_version=99`)
	mustExec(t, database, `CREATE TABLE composerHeaders (composerId TEXT, createdAt INTEGER, lastUpdatedAt INTEGER, value TEXT)`)
	mustExec(t, database, `CREATE TABLE cursorDiskKV (key TEXT, value TEXT)`)
	_ = database.Close()
	capture := &snapshotCapture{}
	collector, err := NewCollector(capture, Config{
		ProjectsRoot: filepath.Join(root, "missing-projects"), StateDatabase: statePath,
		ConversationDatabase: filepath.Join(root, "missing-conversation.db"),
		AITrackingDatabase:   filepath.Join(root, "missing-ai.db"), MinimumRefresh: 0,
		Now: func() time.Time { return time.UnixMilli(1_780_000_002_000) },
	})
	if err != nil {
		t.Fatalf("NewCollector() error = %v", err)
	}
	if err := collector.Refresh(context.Background()); err != nil {
		t.Fatalf("Refresh() error = %v", err)
	}
	for _, source := range capture.snapshot.Sources {
		if source.SourceKey == SourceState {
			if source.State != "unavailable" || source.FailureCode == nil || *source.FailureCode != "schema_incompatible" {
				t.Fatalf("state source = %#v", source)
			}
			return
		}
	}
	t.Fatal("cursor state source status missing")
}

func TestStateZeroTokenPlaceholderKeepsRequestButMarksTokenCoveragePartial(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	statePath := filepath.Join(root, "state.vscdb")
	database := openFixtureDatabase(t, statePath)
	mustExec(t, database, `PRAGMA user_version=1`)
	mustExec(t, database, `CREATE TABLE composerHeaders (composerId TEXT, createdAt INTEGER, lastUpdatedAt INTEGER, value TEXT)`)
	mustExec(t, database, `CREATE TABLE cursorDiskKV (key TEXT PRIMARY KEY, value TEXT)`)
	mustExec(t, database, `INSERT INTO composerHeaders VALUES ('composer-a', 1000, 2000, '{}')`)
	mustExec(t, database, `INSERT INTO cursorDiskKV VALUES ('bubbleId:composer-a:aaaaaaaa-aaaa-aaaa-aaaa-aaaaaaaaaaaa', '{"requestId":"generation-a","createdAt":"2000","tokenCount":{"inputTokens":0,"outputTokens":0}}')`)
	_ = database.Close()

	snapshot := newSnapshot(3_000)
	mergeStateDatabase(context.Background(), statePath, &snapshot)
	snapshot.finalize()
	if len(snapshot.RequestEvents) != 1 || snapshot.RequestEvents[0].EventID != "generation-a" {
		t.Fatalf("request events = %#v, want exact generation identity", snapshot.RequestEvents)
	}
	if len(snapshot.UsageEvents) != 0 {
		t.Fatalf("usage events = %#v, zero placeholder must remain unknown", snapshot.UsageEvents)
	}
	stateSource := sourceStatus(snapshot.Sources, SourceState)
	if stateSource == nil || stateSource.State != "partial" || stateSource.CoverageState != "partial" {
		t.Fatalf("state source = %#v, want partial token coverage", stateSource)
	}
}

func TestStateRequestModelEnrichesSessionWithoutTokenUsage(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	statePath := filepath.Join(root, "state.vscdb")
	database := openFixtureDatabase(t, statePath)
	mustExec(t, database, `PRAGMA user_version=1`)
	mustExec(t, database, `CREATE TABLE composerHeaders (composerId TEXT, createdAt INTEGER, lastUpdatedAt INTEGER, value TEXT)`)
	mustExec(t, database, `CREATE TABLE cursorDiskKV (key TEXT PRIMARY KEY, value TEXT)`)
	composerID := "11111111-1111-1111-1111-111111111111"
	mustExec(t, database, `INSERT INTO composerHeaders VALUES (?, 1000, 2000, '{}')`, composerID)
	mustExec(t, database, `INSERT INTO cursorDiskKV VALUES (
		?,
		'{"requestId":"generation-a","createdAt":"3000","modelInfo":{"modelName":"cursor-request-model"}}'
	)`, "bubbleId:"+composerID+":aaaaaaaa-aaaa-aaaa-aaaa-aaaaaaaaaaaa")
	_ = database.Close()

	snapshot := newSnapshot(4_000)
	mergeStateDatabase(context.Background(), statePath, &snapshot)
	snapshot.finalize()
	if len(snapshot.Sessions) != 1 || snapshot.Sessions[0].ModelKey == nil ||
		*snapshot.Sessions[0].ModelKey != "cursor-request-model" {
		t.Fatalf("session model = %#v", snapshot.Sessions)
	}
	if len(snapshot.RequestEvents) != 1 || len(snapshot.UsageEvents) != 0 {
		t.Fatalf("request/usage evidence = requests %#v usage %#v", snapshot.RequestEvents, snapshot.UsageEvents)
	}
}

func openFixtureDatabase(t *testing.T, path string) *sql.DB {
	t.Helper()
	database, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatalf("open fixture database: %v", err)
	}
	if err := database.Ping(); err != nil {
		database.Close()
		t.Fatalf("ping fixture database: %v", err)
	}
	return database
}

func mustExec(t *testing.T, database *sql.DB, statement string, arguments ...any) {
	t.Helper()
	if _, err := database.Exec(statement, arguments...); err != nil {
		t.Fatalf("exec fixture statement: %v", err)
	}
}
