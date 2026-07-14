package logs

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"time"
)

func TestDecodeRolloutLineSupportedRecords(t *testing.T) {
	t.Parallel()

	t.Run("session metadata keeps only allowlisted fields", func(t *testing.T) {
		t.Parallel()
		const privateMarker = "PRIVATE_BASE_INSTRUCTIONS_MARKER"
		result := decodeFixture(t, `{
			"timestamp":"2026-07-14T01:02:03.004Z",
			"type":"session_meta",
			"payload":{
				"session_id":"root-session",
				"id":"thread-session",
				"timestamp":"2026-07-14T01:02:00Z",
				"cwd":"/tmp/synthetic-project",
				"originator":"codex_cli_rs",
				"cli_version":"0.142.3",
				"source":"cli",
				"model_provider":"openai",
				"base_instructions":{"text":"`+privateMarker+`"}
			}
		}`)
		if result.Diagnostic != nil || result.Record == nil || result.Record.Kind != decodedSessionMeta {
			t.Fatalf("decode result = %#v", result)
		}
		got := result.Record.SessionMeta
		if got == nil || got.SessionID != "thread-session" || got.RootSessionID != "root-session" ||
			got.CreatedAtMS != mustTimestampMS(t, "2026-07-14T01:02:00Z") ||
			got.InitialCWD != "/tmp/synthetic-project" || got.Originator != "codex_cli_rs" ||
			got.CLIVersion != "0.142.3" || got.Source != "cli" || got.ModelProvider != "openai" {
			t.Fatalf("session metadata = %#v", got)
		}
		assertMarkerAbsent(t, result, privateMarker)
	})

	t.Run("legacy session metadata falls back to thread id", func(t *testing.T) {
		t.Parallel()
		result := decodeFixture(t, `{"timestamp":"2026-07-14T01:02:03Z","type":"session_meta","payload":{"id":"legacy-thread","timestamp":"2026-07-14T01:02:00Z","cwd":"/tmp/project","originator":"codex_cli_rs","cli_version":"0.142.3","source":{"subagent":{"thread_spawn":{"parent_thread_id":"private-parent"}}}}}`)
		if result.Diagnostic != nil || result.Record == nil || result.Record.SessionMeta == nil {
			t.Fatalf("decode result = %#v", result)
		}
		if got := result.Record.SessionMeta; got.RootSessionID != "legacy-thread" || got.Source != "subagent" {
			t.Fatalf("legacy session metadata = %#v", got)
		}
	})

	t.Run("turn context", func(t *testing.T) {
		t.Parallel()
		const privateMarker = "PRIVATE_USER_INSTRUCTIONS_MARKER"
		result := decodeFixture(t, `{"timestamp":"2026-07-14T01:02:03Z","type":"turn_context","payload":{"turn_id":"turn-1","cwd":"/tmp/synthetic-project","model":"gpt-5.2-codex","effort":"high","user_instructions":"`+privateMarker+`"}}`)
		if result.Diagnostic != nil || result.Record == nil || result.Record.Kind != decodedTurnContext {
			t.Fatalf("decode result = %#v", result)
		}
		got := result.Record.TurnContext
		if got == nil || got.TurnID == nil || *got.TurnID != "turn-1" || got.CWD != "/tmp/synthetic-project" ||
			got.Model != "gpt-5.2-codex" || got.Effort == nil || *got.Effort != "high" {
			t.Fatalf("turn context = %#v", got)
		}
		assertMarkerAbsent(t, result, privateMarker)
	})

	t.Run("reasoning effort supports ultra and canonicalizes future values", func(t *testing.T) {
		t.Parallel()

		ultra := decodeFixture(t, `{"timestamp":"2026-07-14T01:02:03Z","type":"turn_context","payload":{"turn_id":"turn-1","cwd":"/tmp/project","model":"gpt-5.2-codex","effort":"ultra"}}`)
		if ultra.Diagnostic != nil || ultra.Record == nil || ultra.Record.TurnContext == nil ||
			ultra.Record.TurnContext.Effort == nil || *ultra.Record.TurnContext.Effort != "ultra" {
			t.Fatalf("ultra effort = %#v", ultra)
		}

		const privateMarker = "PRIVATE_FUTURE_EFFORT_MARKER"
		future := decodeFixture(t, `{"timestamp":"2026-07-14T01:02:03Z","type":"turn_context","payload":{"turn_id":"turn-1","cwd":"/tmp/project","model":"gpt-5.2-codex","effort":"`+privateMarker+`"}}`)
		if future.Diagnostic != nil || future.Record == nil || future.Record.TurnContext == nil ||
			future.Record.TurnContext.Effort == nil || *future.Record.TurnContext.Effort != "custom" {
			t.Fatalf("future effort = %#v", future)
		}
		assertMarkerAbsent(t, future, privateMarker)
	})

	t.Run("lifecycle aliases and usage", func(t *testing.T) {
		t.Parallel()

		started := decodeFixture(t, `{"timestamp":"2026-07-14T01:02:03.004Z","type":"event_msg","payload":{"type":"task_started","turn_id":"turn-1","started_at":1783990923,"model_context_window":258000,"trace_id":"private-trace"}}`)
		if started.Diagnostic != nil || started.Record == nil || started.Record.Kind != decodedTurnStart {
			t.Fatalf("started result = %#v", started)
		}
		if got := started.Record.TurnStart; got == nil || got.TurnID != "turn-1" || got.StartedAtMS != 1783990923000 ||
			got.ContextWindow == nil || *got.ContextWindow != 258000 {
			t.Fatalf("turn start = %#v", got)
		}

		startedAlias := decodeFixture(t, `{"timestamp":"2026-07-14T01:02:03.004Z","type":"event_msg","payload":{"type":"turn_started","turn_id":"turn-2","model_context_window":null}}`)
		if got := startedAlias.Record.TurnStart; startedAlias.Diagnostic != nil || got == nil ||
			got.StartedAtMS != mustTimestampMS(t, "2026-07-14T01:02:03.004Z") || got.ContextWindow != nil {
			t.Fatalf("turn_started alias = %#v", startedAlias)
		}

		usage := decodeFixture(t, `{"timestamp":"2026-07-14T01:02:04Z","type":"event_msg","payload":{"type":"token_count","info":{"total_token_usage":{"input_tokens":100,"cached_input_tokens":20,"output_tokens":30,"reasoning_output_tokens":4,"total_tokens":134},"last_token_usage":{"input_tokens":10,"cached_input_tokens":2,"output_tokens":3,"reasoning_output_tokens":1,"total_tokens":14},"model_context_window":258000},"rate_limits":{"private":"ignored"}}}`)
		if usage.Diagnostic != nil || usage.Record == nil || usage.Record.Kind != decodedTokenUsage {
			t.Fatalf("usage result = %#v", usage)
		}
		if got := usage.Record.TokenUsage; got == nil || got.ContextWindow == nil || *got.ContextWindow != 258000 ||
			got.Total.InputTokens == nil || *got.Total.InputTokens != 100 ||
			got.Total.CachedInputTokens == nil || *got.Total.CachedInputTokens != 20 ||
			got.Last.OutputTokens == nil || *got.Last.OutputTokens != 3 ||
			got.Last.ReasoningTokens == nil || *got.Last.ReasoningTokens != 1 {
			t.Fatalf("token usage = %#v", got)
		}

		completed := decodeFixture(t, `{"timestamp":"2026-07-14T01:02:05Z","type":"event_msg","payload":{"type":"turn_complete","turn_id":"turn-1","completed_at":1783990925,"last_agent_message":"private-output"}}`)
		if got := completed.Record.TurnTerminal; completed.Diagnostic != nil || got == nil || got.TurnID == nil ||
			*got.TurnID != "turn-1" || got.CompletedAtMS != 1783990925000 || got.Outcome != TurnOutcomeCompleted {
			t.Fatalf("turn complete = %#v", completed)
		}

		aborted := decodeFixture(t, `{"timestamp":"2026-07-14T01:02:06Z","type":"event_msg","payload":{"type":"turn_aborted","turn_id":null,"reason":"budget_limited","completed_at":1783990926,"duration_ms":1000}}`)
		if got := aborted.Record.TurnTerminal; aborted.Diagnostic != nil || got == nil || got.TurnID != nil ||
			got.CompletedAtMS != 1783990926000 || got.Outcome != TurnOutcomeBudgetLimited {
			t.Fatalf("turn abort = %#v", aborted)
		}
	})
}

func TestDecodeRolloutLineDiagnosticsAreStableAndContentFree(t *testing.T) {
	t.Parallel()

	privateMarker := "PRIVATE_UNKNOWN_TYPE_MARKER"
	testCases := []struct {
		name    string
		content []byte
		code    DiagnosticCode
	}{
		{name: "empty", content: []byte(" \t"), code: DiagnosticEmptyLine},
		{name: "invalid utf8", content: []byte{0xff, 0xfe}, code: DiagnosticInvalidUTF8},
		{name: "bad json", content: []byte(`{"timestamp":`), code: DiagnosticBadJSON},
		{name: "duplicate nested key", content: []byte(`{"timestamp":"2026-07-14T01:02:03Z","type":"event_msg","payload":{"type":"task_started","turn_id":"a","turn_id":"b"}}`), code: DiagnosticDuplicateJSONKey},
		{name: "missing timestamp", content: []byte(`{"type":"response_item","payload":{}}`), code: DiagnosticInvalidTimestamp},
		{name: "unknown rollout", content: []byte(`{"timestamp":"2026-07-14T01:02:03Z","type":"` + privateMarker + `","payload":{}}`), code: DiagnosticUnknownRolloutType},
		{name: "unknown event", content: []byte(`{"timestamp":"2026-07-14T01:02:03Z","type":"event_msg","payload":{"type":"` + privateMarker + `"}}`), code: DiagnosticUnknownEventType},
		{name: "negative token", content: []byte(`{"timestamp":"2026-07-14T01:02:03Z","type":"event_msg","payload":{"type":"token_count","info":{"total_token_usage":{"input_tokens":-1},"last_token_usage":{}}}}`), code: DiagnosticInvalidField},
		{name: "unknown abort reason", content: []byte(`{"timestamp":"2026-07-14T01:02:03Z","type":"event_msg","payload":{"type":"turn_aborted","turn_id":"turn-1","reason":"private_reason"}}`), code: DiagnosticInvalidField},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()
			result := decodeRolloutLine(lineFrame{StartOffset: 11, EndOffset: 29, Content: testCase.content})
			if result.Record != nil || result.KnownIgnored || result.Diagnostic == nil ||
				result.Diagnostic.Code != testCase.code || result.Diagnostic.StartOffset != 11 ||
				result.Diagnostic.EndOffset != 29 {
				t.Fatalf("decode result = %#v", result)
			}
			assertMarkerAbsent(t, result, privateMarker)
		})
	}
}

func TestDecodeRolloutLineKnownContentRecordsAreIgnoredWithoutRetention(t *testing.T) {
	t.Parallel()

	const privateMarker = "PRIVATE_CONTENT_MARKER"
	for _, fixture := range []string{
		`{"timestamp":"2026-07-14T01:02:03Z","type":"response_item","payload":{"type":"message","content":[{"type":"input_text","text":"` + privateMarker + `"}]}}`,
		`{"timestamp":"2026-07-14T01:02:03Z","type":"compacted","payload":{"message":"` + privateMarker + `"}}`,
		`{"timestamp":"2026-07-14T01:02:03Z","type":"event_msg","payload":{"type":"agent_message","message":"` + privateMarker + `"}}`,
	} {
		result := decodeFixture(t, fixture)
		if result.Record != nil || result.Diagnostic != nil || !result.KnownIgnored {
			t.Fatalf("decode result = %#v", result)
		}
		assertMarkerAbsent(t, result, privateMarker)
	}
}

func decodeFixture(t *testing.T, fixture string) decodeResult {
	t.Helper()
	content := []byte(strings.ReplaceAll(fixture, "\n", ""))
	return decodeRolloutLine(lineFrame{StartOffset: 7, EndOffset: 7 + int64(len(content)) + 1, Content: content})
}

func mustTimestampMS(t *testing.T, value string) int64 {
	t.Helper()
	parsed, err := time.Parse(time.RFC3339Nano, value)
	if err != nil {
		t.Fatalf("time.Parse(%q) error = %v", value, err)
	}
	return parsed.UnixMilli()
}

func assertMarkerAbsent(t *testing.T, value any, marker string) {
	t.Helper()
	printed := fmt.Sprintf("%#v", value)
	encoded, err := json.Marshal(value)
	if err != nil {
		t.Fatalf("json.Marshal() error = %v", err)
	}
	if strings.Contains(printed, marker) || strings.Contains(string(encoded), marker) {
		t.Fatalf("private marker leaked: printed=%q encoded=%q", printed, encoded)
	}
}
