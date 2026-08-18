package grokprovider

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/SisyphusSQ/codex-pulse/internal/store"
)

type snapshotCapture struct {
	snapshot store.GrokSnapshot
}

func (capture *snapshotCapture) ReplaceGrokSnapshot(_ context.Context, snapshot store.GrokSnapshot) error {
	capture.snapshot = snapshot
	return nil
}

func TestCollectorReadsSummaryAndWhitelistedUpdatesOnly(t *testing.T) {
	t.Parallel()
	home := t.TempDir()
	sessionDir := filepath.Join(home, "sessions", "%2Ftmp%2Fdemo", "session-1")
	if err := os.MkdirAll(sessionDir, 0o700); err != nil {
		t.Fatal(err)
	}
	summary := map[string]any{
		"generated_title":  "Plan the collector",
		"created_at":       "2026-08-18T01:00:00Z",
		"updated_at":       "2026-08-18T02:00:00Z",
		"last_active_at":   "2026-08-18T02:00:00Z",
		"current_model_id": "grok-4.6",
		"git_root_dir":     "/tmp/demo-project/",
		"info":             map[string]any{"id": "session-1", "cwd": "/tmp/demo-project"},
	}
	writeJSON(t, filepath.Join(sessionDir, "summary.json"), summary)
	updates := []map[string]any{
		{
			"method":    "_x.ai/session/update",
			"timestamp": 1787014800,
			"params": map[string]any{
				"sessionId": "session-1",
				"update": map[string]any{
					"sessionUpdate": "turn_completed",
					"prompt_id":     "prompt-1",
					"usage": map[string]any{
						"inputTokens": 100, "outputTokens": 20, "totalTokens": 120,
						"cachedReadTokens": 40, "cacheCreationTokens": 0, "reasoningTokens": 5,
						"costUsdTicks": 1500000,
						"modelUsage": map[string]any{
							"grok-4.6-build": map[string]any{
								"inputTokens": 100, "outputTokens": 20, "totalTokens": 120,
								"cachedReadTokens": 40, "cacheCreationTokens": 0, "reasoningTokens": 5,
								"costUsdTicks": 1500000,
							},
						},
					},
				},
			},
		},
		{
			"method":    "_x.ai/session/update",
			"timestamp": 1787014801,
			"params": map[string]any{
				"sessionId": "session-1",
				"update": map[string]any{
					"sessionUpdate": "tool_call",
					"toolCallId":    "call-1",
					"title":         "read_file /secret/path",
					"kind":          "read",
					"status":        "completed",
					"rawInput":      map[string]any{"path": "/secret/path"},
					"_meta":         map[string]any{"x.ai/tool": map[string]any{"name": "read_file"}},
				},
			},
		},
	}
	writeJSONL(t, filepath.Join(sessionDir, "updates.jsonl"), updates)
	writeJSONL(t, filepath.Join(sessionDir, "chat_history.jsonl"), []map[string]any{{"content": "secret prompt"}})
	if err := os.WriteFile(filepath.Join(sessionDir, "system_prompt.txt"), []byte("secret"), 0o600); err != nil {
		t.Fatal(err)
	}

	capture := &snapshotCapture{}
	collector, err := NewCollector(capture, Config{
		Home: home, SessionsRoot: filepath.Join(home, "sessions"),
		AuthPath: filepath.Join(home, "auth.json"), Now: func() time.Time { return time.UnixMilli(2_000) },
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := collector.Refresh(context.Background()); err != nil {
		t.Fatalf("Refresh() error = %v", err)
	}
	if len(capture.snapshot.Sessions) != 1 {
		t.Fatalf("sessions = %#v", capture.snapshot.Sessions)
	}
	session := capture.snapshot.Sessions[0]
	if session.ExternalSessionID != "session-1" || session.DisplayTitle != "Plan the collector" ||
		session.TitleSource != "grok_summary" || session.ProjectDisplayName != "demo-project" ||
		session.RequestCount != 1 || session.ToolCallCount != 1 {
		t.Fatalf("session = %#v", session)
	}
	if len(capture.snapshot.UsageEvents) != 1 || capture.snapshot.UsageEvents[0].EventID != "prompt-1:grok-4.6-build" {
		t.Fatalf("usage = %#v", capture.snapshot.UsageEvents)
	}
	event := capture.snapshot.UsageEvents[0]
	if event.InputTokens != 100 || event.CachedReadTokens != 40 || event.ReportedCostMicros == nil ||
		*event.ReportedCostMicros != 1500 {
		t.Fatalf("usage event = %#v", event)
	}
	if len(capture.snapshot.ToolEvents) != 1 || capture.snapshot.ToolEvents[0].ToolName != "read_file" ||
		capture.snapshot.ToolEvents[0].Outcome != "succeeded" {
		t.Fatalf("tools = %#v", capture.snapshot.ToolEvents)
	}
	payload, _ := json.Marshal(capture.snapshot)
	if containsAny(string(payload), "/secret/path", "secret prompt", "system_prompt") {
		t.Fatalf("snapshot leaked private content: %s", payload)
	}
}

func TestDefaultConfigUsesIsolatedTestHomeOnly(t *testing.T) {
	home := t.TempDir()
	t.Setenv("CODEX_PULSE_GROK_HOME", home)
	t.Setenv("GROK_HOME", "/tmp/should-not-win")
	config, err := DefaultConfig()
	if err != nil {
		t.Fatal(err)
	}
	if config.Home != home || config.SessionsRoot != filepath.Join(home, "sessions") {
		t.Fatalf("config = %#v", config)
	}
}

func TestAuthReaderReturnsWhitelistAndKeepsTokenInMemory(t *testing.T) {
	t.Parallel()
	home := t.TempDir()
	writeJSON(t, filepath.Join(home, "auth.json"), map[string]any{
		"https://auth.x.ai::acct": map[string]any{
			"email": "person@example.com", "principal_type": "User", "auth_mode": "oidc",
			"user_id": "user-1", "key": "bearer-secret", "refresh_token": "refresh-secret",
			"expires_at": time.UnixMilli(10_000).UTC().Format(time.RFC3339Nano),
		},
	})
	reader, err := NewAuthReader(filepath.Join(home, "auth.json"), func() time.Time { return time.UnixMilli(1_000) })
	if err != nil {
		t.Fatal(err)
	}
	account, err := reader.ReadAccountSnapshot()
	if err != nil || account.Email != "person@example.com" || account.PrincipalType != "User" {
		t.Fatalf("account = %#v, %v", account, err)
	}
	if account.Email == "" {
		t.Fatal("missing email")
	}
	token, err := reader.ReadAccessToken()
	if err != nil || token.Token != "bearer-secret" || token.UserID != "user-1" {
		t.Fatalf("token = %#v, %v", token, err)
	}
}

func writeJSON(t *testing.T, path string, value any) {
	t.Helper()
	content, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, content, 0o600); err != nil {
		t.Fatal(err)
	}
}

func writeJSONL(t *testing.T, path string, values []map[string]any) {
	t.Helper()
	file, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	encoder := json.NewEncoder(file)
	for _, value := range values {
		if err := encoder.Encode(value); err != nil {
			t.Fatal(err)
		}
	}
}

func containsAny(value string, parts ...string) bool {
	for _, part := range parts {
		if filepath.Base(part) != part && len(part) > 0 && (len(value) > 0) {
			if containsString([]string{value}, part) {
				return true
			}
		}
		if containsString([]string{value}, part) {
			return true
		}
	}
	return false
}
