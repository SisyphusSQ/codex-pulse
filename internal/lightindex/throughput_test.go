package lightindex

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"

	"github.com/SisyphusSQ/codex-pulse/internal/throughput"
)

func TestThroughputScannerResumesCounterEpochsWithoutReasoningDuplication(t *testing.T) {
	first := `{"timestamp":"2026-10-01T00:00:00Z","type":"event_msg","payload":{"type":"task_started","turn_id":"t1"}}` + "\n" +
		`{"timestamp":"2026-10-01T00:00:01Z","type":"event_msg","payload":{"type":"token_count","info":{"total_token_usage":{"input_tokens":10000,"output_tokens":1000,"reasoning_output_tokens":400},"last_token_usage":{"output_tokens":1000}}}}` + "\n"
	rest := `{"timestamp":"2026-10-01T00:00:02Z","type":"event_msg","payload":{"type":"token_count","info":{"total_token_usage":{"input_tokens":20000,"output_tokens":1500,"reasoning_output_tokens":600},"last_token_usage":{"output_tokens":500}}}}` + "\n" +
		`{"timestamp":"2026-10-01T00:00:03Z","type":"event_msg","payload":{"type":"token_count","info":{"total_token_usage":{"input_tokens":20000,"output_tokens":1500,"reasoning_output_tokens":600}}}}` + "\n" +
		`{"timestamp":"2026-10-01T00:00:10Z","type":"event_msg","payload":{"type":"task_complete","turn_id":"t1","duration_ms":10000,"last_agent_message":"PRIVATE-CONTENT-CANARY"}}` + "\n" +
		`{"timestamp":"2026-10-01T01:00:00Z","type":"event_msg","payload":{"type":"turn_started","turn_id":"t2"}}` + "\n" +
		`{"timestamp":"2026-10-01T01:00:00.500Z","type":"event_msg","payload":{"type":"token_count","info":{"total_token_usage":{"input_tokens":100,"output_tokens":20,"reasoning_output_tokens":10},"last_token_usage":{"output_tokens":20}}}}` + "\n" +
		`{"timestamp":"2026-10-01T01:00:01Z","type":"event_msg","payload":{"type":"turn_aborted","duration_ms":1000}}` + "\n"
	scanner := NewTokenScanner(TokenScannerOptions{ChunkBytes: 37})
	a, err := scanner.Scan(t.Context(), strings.NewReader(first), ScanState{})
	if err != nil {
		t.Fatal(err)
	}
	b, err := scanner.Scan(t.Context(), strings.NewReader(rest), a.State)
	if err != nil {
		t.Fatal(err)
	}
	acc := throughput.NewAccumulator()
	for _, event := range append(a.TurnEvents, b.TurnEvents...) {
		acc.Add(event)
	}
	stats, _ := acc.Result()
	if stats.Status != "complete" || *stats.OutputTokens != 1520 || *stats.ActiveMS != 11000 || *stats.AverageMilliTPS != 138182 || b.State.CounterEpoch != 1 {
		t.Fatalf("stats=%+v epoch=%d", stats, b.State.CounterEpoch)
	}
	encoded, err := json.Marshal(b.TurnEvents)
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(encoded, []byte("PRIVATE-CONTENT-CANARY")) {
		t.Fatal("content persisted in throughput facts")
	}
}

func TestThroughputScannerDoesNotAssignInheritedCumulativeOutputToTurn(t *testing.T) {
	content := `{"timestamp":"2026-10-01T00:00:00Z","type":"event_msg","payload":{"type":"task_started","turn_id":"t"}}` + "\n" +
		`{"timestamp":"2026-10-01T00:00:01Z","type":"event_msg","payload":{"type":"token_count","info":{"total_token_usage":{"output_tokens":9000},"last_token_usage":{"output_tokens":100}}}}` + "\n" +
		`{"timestamp":"2026-10-01T00:00:02Z","type":"event_msg","payload":{"type":"task_complete","turn_id":"t"}}` + "\n"
	result, err := NewTokenScanner(TokenScannerOptions{}).Scan(t.Context(), strings.NewReader(content), ScanState{})
	if err != nil {
		t.Fatal(err)
	}
	acc := throughput.NewAccumulator()
	for _, event := range result.TurnEvents {
		acc.Add(event)
	}
	stats, _ := acc.Result()
	if stats.AverageMilliTPS != nil || stats.ExcludedTurns != 1 {
		t.Fatalf("inherited output = %+v", stats)
	}
}

func TestThroughputScannerRejectsDuplicateKeysAndMarksExplicitFork(t *testing.T) {
	content := `{"type":"session_meta","payload":{"forked_from_id":"PRIVATE-PARENT-CANARY","base_instructions":"PRIVATE-BODY-CANARY"}}` + "\n" +
		`{"timestamp":"2026-10-01T00:00:00Z","type":"event_msg","payload":{"type":"task_started","turn_id":"a","turn_id":"b"}}` + "\n"
	result, err := NewTokenScanner(TokenScannerOptions{}).Scan(t.Context(), strings.NewReader(content), ScanState{})
	if err != nil {
		t.Fatal(err)
	}
	if len(result.TurnEvents) != 2 || result.TurnEvents[0].Kind != "inherited" || result.TurnEvents[1].Kind != "gap" {
		t.Fatalf("events=%+v", result.TurnEvents)
	}
	encoded, _ := json.Marshal(result.TurnEvents)
	if bytes.Contains(encoded, []byte("PRIVATE-")) {
		t.Fatal("source content or parent identity persisted")
	}
	acc := throughput.NewAccumulator()
	for _, e := range result.TurnEvents {
		acc.Add(e)
	}
	stats, _ := acc.Result()
	if stats.Status != "unavailable" || stats.Reason != "inherited_history" {
		t.Fatalf("fork=%+v", stats)
	}
}

func TestThroughputScannerSkippedOversizedLineCannotLookComplete(t *testing.T) {
	content := `{"timestamp":"2026-10-01T00:00:00Z","type":"event_msg","payload":{"type":"task_started","turn_id":"t"}}` + "\n" +
		`{"type":"event_msg","payload":{"type":"token_count","unknown":"` + strings.Repeat("x", 1024) + `"}}` + "\n" +
		`{"timestamp":"2026-10-01T00:00:01Z","type":"event_msg","payload":{"type":"token_count","info":{"total_token_usage":{"output_tokens":100},"last_token_usage":{"output_tokens":100}}}}` + "\n" +
		`{"timestamp":"2026-10-01T00:00:02Z","type":"event_msg","payload":{"type":"task_complete","turn_id":"t"}}` + "\n"
	result, err := NewTokenScanner(TokenScannerOptions{MaxLine: 512}).Scan(t.Context(), strings.NewReader(content), ScanState{})
	if err != nil {
		t.Fatal(err)
	}
	acc := throughput.NewAccumulator()
	for _, event := range result.TurnEvents {
		acc.Add(event)
	}
	stats, _ := acc.Result()
	if stats.AverageMilliTPS != nil || stats.ExcludedTurns != 1 || stats.UnattributedEvents != 1 {
		t.Fatalf("skipped source coverage = %+v", stats)
	}
}

func TestThroughputScannerFallsBackToSourceSeconds(t *testing.T) {
	content := `{"type":"event_msg","payload":{"type":"task_started","turn_id":"t","started_at":1767225600}}` + "\n" +
		`{"timestamp":"2026-01-01T00:00:01Z","type":"event_msg","payload":{"type":"token_count","info":{"total_token_usage":{"output_tokens":100},"last_token_usage":{"output_tokens":100}}}}` + "\n" +
		`{"type":"event_msg","payload":{"type":"task_complete","turn_id":"t","completed_at":1767225602}}` + "\n"
	result, err := NewTokenScanner(TokenScannerOptions{}).Scan(t.Context(), strings.NewReader(content), ScanState{})
	if err != nil {
		t.Fatal(err)
	}
	acc := throughput.NewAccumulator()
	for _, event := range result.TurnEvents {
		acc.Add(event)
	}
	stats, _ := acc.Result()
	if stats.DurationSource != "source_seconds" || stats.AverageMilliTPS == nil || *stats.AverageMilliTPS != 50000 {
		t.Fatalf("source seconds = %+v", stats)
	}
}
