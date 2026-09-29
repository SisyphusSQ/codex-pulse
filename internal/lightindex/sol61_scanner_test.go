package lightindex

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"
)

func TestTokenScannerRestoresGPT61SolAndSwitchesModels(t *testing.T) {
	t.Parallel()
	scanner := NewTokenScanner(TokenScannerOptions{ChunkBytes: 31})
	content := strings.Join([]string{
		`{"timestamp":"2026-09-29T01:00:00Z","type":"turn_context","payload":{"model":"gpt-6-sol"}}`,
		tokenLine("2026-09-29T01:00:01Z", 10, 2, 3, 1),
		`{"timestamp":"2026-09-29T01:00:02Z","type":"turn_context","payload":{"model":" OpenAI/GPT-6.1-Sol "}}`,
		tokenLine("2026-09-29T01:00:03Z", 20, 4, 6, 2),
	}, "\n") + "\n"
	first, err := scanner.Scan(t.Context(), bytes.NewBufferString(content), ScanState{})
	if err != nil || len(first.TokenDeltas) != 2 {
		t.Fatalf("first scan = %#v, %v", first, err)
	}
	for i, model := range []string{"gpt-6-sol", "gpt-6.1-sol"} {
		delta := first.TokenDeltas[i]
		if delta.ModelKey == nil || *delta.ModelKey != model ||
			delta.Tokens != (TokenTotals{Input: 10, CachedInput: 2, Output: 3, Reasoning: 1}) {
			t.Fatalf("model delta %d = %#v", i, delta)
		}
	}
	encoded, err := json.Marshal(first.State)
	if err != nil {
		t.Fatal(err)
	}
	var restored ScanState
	if err := json.Unmarshal(encoded, &restored); err != nil {
		t.Fatal(err)
	}
	appendContent := strings.Join([]string{
		tokenLine("2026-09-29T01:00:04Z", 30, 6, 9, 3),
		`{"timestamp":"2026-09-29T01:00:05Z","type":"turn_context","payload":{"model":"gpt-6-sol"}}`,
		tokenLine("2026-09-29T01:00:06Z", 40, 8, 12, 4),
	}, "\n") + "\n"
	resumed, err := scanner.Scan(t.Context(), bytes.NewBufferString(appendContent), restored)
	if err != nil || len(resumed.TokenDeltas) != 2 {
		t.Fatalf("resumed scan = %#v, %v", resumed, err)
	}
	for i, model := range []string{"gpt-6.1-sol", "gpt-6-sol"} {
		delta := resumed.TokenDeltas[i]
		if delta.ModelKey == nil || *delta.ModelKey != model ||
			delta.Tokens != (TokenTotals{Input: 10, CachedInput: 2, Output: 3, Reasoning: 1}) ||
			delta.SourceOffset < first.State.DurableOffset {
			t.Fatalf("resumed model delta %d = %#v", i, delta)
		}
	}
	if resumed.TokenDeltas[0].ModelSource != "model_alias" ||
		resumed.TokenDeltas[1].ModelSource != "model_canonical" ||
		resumed.State.Aggregates != (TokenTotals{Input: 40, CachedInput: 8, Output: 12, Reasoning: 4}) ||
		resumed.State.DurableOffset != int64(len(content)+len(appendContent)) {
		t.Fatalf("restored checkpoint did not preserve provenance and totals: %#v", resumed)
	}
}
