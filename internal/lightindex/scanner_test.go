package lightindex

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"strconv"
	"strings"
	"testing"
)

func TestTokenScannerAttributesTimedDeltasToSafeCurrentModel(t *testing.T) {
	t.Parallel()

	content := strings.Join([]string{
		`{"timestamp":"2026-07-19T01:00:00Z","type":"turn_context","payload":{"model":" OpenAI/GPT-5.4-Mini "}}`,
		tokenLine("2026-07-19T01:00:01Z", 10, 2, 3, 1),
	}, "\n") + "\n"
	result, err := NewTokenScanner(TokenScannerOptions{ChunkBytes: 32}).Scan(
		context.Background(), bytes.NewBufferString(content), ScanState{},
	)
	if err != nil {
		t.Fatalf("Scan() error = %v", err)
	}
	encoded, err := json.Marshal(struct {
		State  ScanState
		Deltas []TimedTokenDelta
	}{State: result.State, Deltas: result.TokenDeltas})
	if err != nil {
		t.Fatalf("marshal scanner result: %v", err)
	}
	if !bytes.Contains(encoded, []byte(`"gpt-5.4-mini"`)) ||
		!bytes.Contains(encoded, []byte(`"model_alias"`)) {
		t.Fatalf("scanner result does not preserve normalized model attribution: %s", encoded)
	}
	if result.State.CurrentModelKey == nil || *result.State.CurrentModelKey != "gpt-5.4-mini" ||
		result.State.CurrentModelSource != "model_alias" || len(result.TokenDeltas) != 1 ||
		result.TokenDeltas[0].ModelKey == nil || *result.TokenDeltas[0].ModelKey != "gpt-5.4-mini" {
		t.Fatalf("typed model attribution = %#v, deltas=%#v", result.State, result.TokenDeltas)
	}
	resumed, err := NewTokenScanner(TokenScannerOptions{ChunkBytes: 32}).Scan(
		context.Background(),
		bytes.NewBufferString(tokenLine("2026-07-19T01:00:02Z", 20, 4, 6, 2)+"\n"),
		result.State,
	)
	if err != nil || len(resumed.TokenDeltas) != 1 || resumed.TokenDeltas[0].ModelKey == nil ||
		*resumed.TokenDeltas[0].ModelKey != "gpt-5.4-mini" || resumed.TokenDeltas[0].ModelSource != "model_alias" {
		t.Fatalf("resumed model attribution = %#v, %v", resumed, err)
	}
}

func TestTokenScannerExtractsPrivacySafeToolInvocations(t *testing.T) {
	t.Parallel()

	content := strings.Join([]string{
		`{"timestamp":"2026-08-07T01:00:00Z","type":"response_item","payload":{"type":"function_call","name":"wait","arguments":"{\"secret\":\"do-not-store\"}"}}`,
		`{"timestamp":"2026-08-07T01:00:01Z","type":"response_item","payload":{"type":"custom_tool_call","name":"exec","input":"await tools.exec_command({cmd: 'private command'}); await tools.mcp__linear__list_issues({team: 'secret'})"}}`,
		`{"timestamp":"2026-08-07T01:00:02Z","type":"event_msg","payload":{"type":"mcp_tool_call_end","duration_ms":42,"invocation":{"server":"linear","tool":"list_issues","arguments":{"team":"secret"}},"result":{"Ok":{"content":[]}}}}`,
	}, "\n") + "\n"

	result, err := NewTokenScanner(TokenScannerOptions{ChunkBytes: 31}).Scan(
		context.Background(), bytes.NewBufferString(content), ScanState{},
	)
	if err != nil {
		t.Fatalf("Scan() error = %v", err)
	}
	want := []InvocationDelta{
		{Kind: InvocationKindTool, Name: "wait", Source: InvocationSourceResponseFunction, Outcome: InvocationOutcomeUnknown},
		{Kind: InvocationKindTool, Name: "exec", Source: InvocationSourceResponseCustom, Outcome: InvocationOutcomeUnknown},
		{Kind: InvocationKindTool, Name: "exec_command", Source: InvocationSourceExecNested, Outcome: InvocationOutcomeUnknown},
		{Kind: InvocationKindTool, Name: "linear.list_issues", Source: InvocationSourceMCP, Outcome: InvocationOutcomeSucceeded},
	}
	if len(result.InvocationDeltas) != len(want) {
		t.Fatalf("InvocationDeltas = %#v, want %d items", result.InvocationDeltas, len(want))
	}
	for index, expected := range want {
		got := result.InvocationDeltas[index]
		if got.Kind != expected.Kind || got.Name != expected.Name || got.Source != expected.Source ||
			got.Outcome != expected.Outcome || got.SourceOffset <= 0 || got.ObservedAtMS <= 0 {
			t.Fatalf("InvocationDeltas[%d] = %#v, want %#v with offsets", index, got, expected)
		}
	}
	if result.InvocationDeltas[3].DurationMS == nil || *result.InvocationDeltas[3].DurationMS != 42 {
		t.Fatalf("MCP duration = %#v, want 42", result.InvocationDeltas[3].DurationMS)
	}
	encoded, err := json.Marshal(result.InvocationDeltas)
	if err != nil {
		t.Fatalf("marshal invocation deltas: %v", err)
	}
	for _, forbidden := range []string{"do-not-store", "private command", "secret", "mcp__linear__list_issues"} {
		if bytes.Contains(encoded, []byte(forbidden)) {
			t.Fatalf("privacy-sensitive value %q leaked into deltas: %s", forbidden, encoded)
		}
	}
}

func TestTokenScannerKeepsAmbiguousToolEndOutcomesUnknown(t *testing.T) {
	t.Parallel()

	content := strings.Join([]string{
		`{"timestamp":"2026-08-07T01:00:00Z","type":"event_msg","payload":{"type":"web_search_end","duration_ms":42}}`,
		`{"timestamp":"2026-08-07T01:00:01Z","type":"event_msg","payload":{"type":"image_generation_end","duration_ms":58}}`,
	}, "\n") + "\n"

	result, err := NewTokenScanner(TokenScannerOptions{ChunkBytes: 31}).Scan(
		context.Background(), bytes.NewBufferString(content), ScanState{},
	)
	if err != nil {
		t.Fatalf("Scan() error = %v", err)
	}
	if len(result.InvocationDeltas) != 2 ||
		result.InvocationDeltas[0].Outcome != InvocationOutcomeUnknown ||
		result.InvocationDeltas[1].Outcome != InvocationOutcomeUnknown {
		t.Fatalf("ambiguous end outcomes = %#v, want unknown", result.InvocationDeltas)
	}
}

func TestTokenScannerSkipsInvocationOutsideSafeNumericRange(t *testing.T) {
	t.Parallel()

	content := strings.Join([]string{
		`{"timestamp":"1969-12-31T23:59:59Z","type":"response_item","payload":{"type":"function_call","name":"wait"}}`,
		`{"timestamp":"2026-08-07T01:00:00Z","type":"event_msg","payload":{"type":"web_search_end","duration_ms":9223372036854775807}}`,
	}, "\n") + "\n"

	result, err := NewTokenScanner(TokenScannerOptions{ChunkBytes: 31}).Scan(
		context.Background(), bytes.NewBufferString(content), ScanState{},
	)
	if err != nil {
		t.Fatalf("Scan() error = %v", err)
	}
	if len(result.InvocationDeltas) != 0 {
		t.Fatalf("unsafe invocation values were retained: %#v", result.InvocationDeltas)
	}
	if len(result.Diagnostics) != 1 || result.Diagnostics[0].Code != "candidate_invalid_timestamp" {
		t.Fatalf("diagnostics = %#v, want invalid timestamp", result.Diagnostics)
	}
}

func TestTokenScannerDetectsSkillActivityWithoutPersistingContentOrPaths(t *testing.T) {
	t.Parallel()

	content := strings.Join([]string{
		`{"timestamp":"2026-08-07T02:00:00Z","type":"response_item","payload":{"type":"message","role":"user","content":[{"type":"input_text","text":"<skill><name>personal-thin-skills:discuss-first</name><location>/private/skill.md</location></skill>"}]}}`,
		`{"timestamp":"2026-08-07T02:00:01Z","type":"response_item","payload":{"type":"custom_tool_call","name":"exec","input":"sed -n '1,200p' /Users/example/.codex/skills/go-code-style/SKILL.md && sed -n '1,200p' /Users/example/.codex/skills/go-code-style/SKILL.md && sed -n '1,200p' /Users/example/.codex/plugins/cache/sisyphus-private/personal-thin-skills/0.4.0/skills/discuss-first/SKILL.md"}}`,
	}, "\n") + "\n"

	result, err := NewTokenScanner(TokenScannerOptions{ChunkBytes: 29}).Scan(
		context.Background(), bytes.NewBufferString(content), ScanState{},
	)
	if err != nil {
		t.Fatalf("Scan() error = %v", err)
	}
	var skills []InvocationDelta
	for _, delta := range result.InvocationDeltas {
		if delta.Kind == InvocationKindSkill {
			skills = append(skills, delta)
		}
	}
	want := []InvocationDelta{
		{Kind: InvocationKindSkill, Name: "personal-thin-skills:discuss-first", Source: InvocationSourceSkillExplicit, Outcome: InvocationOutcomeUnknown},
		{Kind: InvocationKindSkill, Name: "go-code-style", Source: InvocationSourceSkillFileLoaded, Outcome: InvocationOutcomeUnknown},
		{Kind: InvocationKindSkill, Name: "personal-thin-skills:discuss-first", Source: InvocationSourceSkillFileLoaded, Outcome: InvocationOutcomeUnknown},
	}
	if len(skills) != len(want) {
		t.Fatalf("skill deltas = %#v, want %#v", skills, want)
	}
	for index, expected := range want {
		got := skills[index]
		if got.Kind != expected.Kind || got.Name != expected.Name || got.Source != expected.Source ||
			got.Outcome != expected.Outcome || got.SourceOffset <= 0 || got.ObservedAtMS <= 0 {
			t.Fatalf("skills[%d] = %#v, want %#v with offsets", index, got, expected)
		}
	}
	encoded, err := json.Marshal(skills)
	if err != nil {
		t.Fatalf("marshal skills: %v", err)
	}
	for _, forbidden := range []string{"/Users/example", "sed -n", "private/skill.md", "sisyphus-private"} {
		if bytes.Contains(encoded, []byte(forbidden)) {
			t.Fatalf("privacy-sensitive value %q leaked into skills: %s", forbidden, encoded)
		}
	}
}

func TestTokenScannerPrefiltersBeforeJSONDecode(t *testing.T) {
	t.Parallel()

	content := strings.Join([]string{
		`not-json-and-not-a-token-event`,
		`{"timestamp":"2026-07-19T01:00:00Z","type":"response_item","payload":{"text":"token_count"}}`,
		`{"timestamp":"2026-07-19T01:00:01Z","type":"event_msg","payload":{"type":"token_count","info":{"total_token_usage":{"input_tokens":10,"cached_input_tokens":2,"output_tokens":3,"reasoning_output_tokens":1}}}}`,
	}, "\n") + "\n"

	result, err := NewTokenScanner(TokenScannerOptions{ChunkBytes: 32}).Scan(
		context.Background(), bytes.NewBufferString(content), ScanState{},
	)
	if err != nil {
		t.Fatalf("Scan() error = %v", err)
	}
	if result.LinesSeen != 3 || result.CandidateLines != 2 || result.JSONDecoded != 2 || result.TokenEvents != 1 {
		t.Fatalf("unexpected scan counters: %+v", result)
	}
	if len(result.Diagnostics) != 0 {
		t.Fatalf("non-candidate invalid JSON must not produce diagnostics: %+v", result.Diagnostics)
	}
	if result.DurableOffset != int64(len(content)) || !result.Complete {
		t.Fatalf("unexpected completion: offset=%d complete=%t", result.DurableOffset, result.Complete)
	}
	assertTotals(t, result.State.HighWater, TokenTotals{Input: 10, CachedInput: 2, Output: 3, Reasoning: 1})
}

func TestTokenScannerKeepsTrailingPartialLineBehindDurableOffset(t *testing.T) {
	t.Parallel()

	complete := `{"timestamp":"2026-07-19T01:00:01Z","type":"event_msg","payload":{"type":"token_count","info":{"total_token_usage":{"input_tokens":10}}}}` + "\n"
	partial := `{"timestamp":"2026-07-19T01:00:02Z","type":"event_msg","payload":{"type":"token_count","info":{"total_token_usage":{"input_tokens":99}}}}`
	content := complete + partial

	result, err := NewTokenScanner(TokenScannerOptions{ChunkBytes: 17}).Scan(
		context.Background(), bytes.NewBufferString(content), ScanState{},
	)
	if err != nil {
		t.Fatalf("Scan() error = %v", err)
	}
	if result.DurableOffset != int64(len(complete)) || result.Complete {
		t.Fatalf("partial tail crossed durable boundary: %+v", result)
	}
	if result.BytesRead != int64(len(content)) || result.LinesSeen != 1 || result.TokenEvents != 1 {
		t.Fatalf("unexpected partial-tail counters: %+v", result)
	}
	assertTotals(t, result.State.HighWater, TokenTotals{Input: 10})
}

func TestTokenScannerUsesComponentHighWaterAndPositiveDailyDeltas(t *testing.T) {
	t.Parallel()

	content := strings.Join([]string{
		tokenLine("2026-07-18T23:59:59Z", 100, 20, 10, 2),
		tokenLine("2026-07-19T00:00:01Z", 90, 25, 10, 1),
		tokenLine("2026-07-19T00:00:02Z", 130, 25, 15, 4),
	}, "\n") + "\n"

	result, err := NewTokenScanner(TokenScannerOptions{ChunkBytes: 41}).Scan(
		context.Background(), bytes.NewBufferString(content), ScanState{},
	)
	if err != nil {
		t.Fatalf("Scan() error = %v", err)
	}
	assertTotals(t, result.State.HighWater, TokenTotals{Input: 130, CachedInput: 25, Output: 15, Reasoning: 4})
	if len(result.DailyDeltas) != 2 {
		t.Fatalf("daily delta count = %d, want 2: %+v", len(result.DailyDeltas), result.DailyDeltas)
	}
	if len(result.TokenDeltas) != 3 || result.TokenDeltas[0].ObservedAtMS != 1_784_419_199_000 ||
		result.TokenDeltas[0].SourceOffset <= 0 {
		t.Fatalf("timed deltas = %+v", result.TokenDeltas)
	}
	assertDailyDelta(t, result.DailyDeltas[0], "2026-07-18", TokenTotals{Input: 100, CachedInput: 20, Output: 10, Reasoning: 2})
	assertDailyDelta(t, result.DailyDeltas[1], "2026-07-19", TokenTotals{Input: 30, CachedInput: 5, Output: 5, Reasoning: 2})
}

func TestTokenScannerResumesFromSeedOffsetAndHighWater(t *testing.T) {
	t.Parallel()

	appendContent := tokenLine("2026-07-19T01:00:00Z", 130, 30, 15, 4) + "\n"
	seed := ScanState{
		DurableOffset: 4096,
		HighWater:     TokenTotals{Input: 100, CachedInput: 20, Output: 10, Reasoning: 2},
	}

	result, err := NewTokenScanner(TokenScannerOptions{ChunkBytes: 64}).Scan(
		context.Background(), bytes.NewBufferString(appendContent), seed,
	)
	if err != nil {
		t.Fatalf("Scan() error = %v", err)
	}
	if result.DurableOffset != seed.DurableOffset+int64(len(appendContent)) {
		t.Fatalf("durable offset = %d, want %d", result.DurableOffset, seed.DurableOffset+int64(len(appendContent)))
	}
	assertTotals(t, result.State.HighWater, TokenTotals{Input: 130, CachedInput: 30, Output: 15, Reasoning: 4})
	if len(result.DailyDeltas) != 1 {
		t.Fatalf("daily delta count = %d, want 1", len(result.DailyDeltas))
	}
	assertDailyDelta(t, result.DailyDeltas[0], "2026-07-19", TokenTotals{Input: 30, CachedInput: 10, Output: 5, Reasoning: 2})
}

func TestTokenScannerCancellationDoesNotAdvanceOffset(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	seed := ScanState{DurableOffset: 123, HighWater: TokenTotals{Input: 10}}

	result, err := NewTokenScanner(TokenScannerOptions{ChunkBytes: 64}).Scan(
		ctx, bytes.NewBufferString(tokenLine("2026-07-19T01:00:00Z", 20, 0, 0, 0)+"\n"), seed,
	)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("Scan() error = %v, want context.Canceled", err)
	}
	if result.DurableOffset != seed.DurableOffset || result.TokenEvents != 0 {
		t.Fatalf("canceled scan advanced state: %+v", result)
	}
}

func tokenLine(timestamp string, input, cached, output, reasoning int64) string {
	return `{"timestamp":"` + timestamp + `","type":"event_msg","payload":{"type":"token_count","info":{"total_token_usage":{` +
		`"input_tokens":` + intString(input) + `,"cached_input_tokens":` + intString(cached) +
		`,"output_tokens":` + intString(output) + `,"reasoning_output_tokens":` + intString(reasoning) + `}}}}`
}

func intString(value int64) string {
	return strconv.FormatInt(value, 10)
}

func assertTotals(t *testing.T, got, want TokenTotals) {
	t.Helper()
	if got != want {
		t.Fatalf("totals = %+v, want %+v", got, want)
	}
}

func assertDailyDelta(t *testing.T, got DailyTokenDelta, wantDay string, want TokenTotals) {
	t.Helper()
	if got.Day != wantDay || got.Tokens != want {
		t.Fatalf("daily delta = %+v, want day=%s tokens=%+v", got, wantDay, want)
	}
}
