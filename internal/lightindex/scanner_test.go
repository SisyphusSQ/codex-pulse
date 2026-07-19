package lightindex

import (
	"bytes"
	"context"
	"errors"
	"strconv"
	"strings"
	"testing"
)

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
