package lightindex

import (
	"encoding/json"
	"time"

	"github.com/SisyphusSQ/codex-pulse/internal/throughput"
)

func safeThroughputID(value *string) *string {
	if value == nil || len(*value) == 0 || len(*value) > 128 {
		return nil
	}
	for _, ch := range *value {
		if ch != '-' && ch != '_' && ch != '.' && !(ch >= 'a' && ch <= 'z') && !(ch >= 'A' && ch <= 'Z') && !(ch >= '0' && ch <= '9') {
			return nil
		}
	}
	return value
}

func decodeThroughputLifecycle(timestamp string, raw json.RawMessage, offset int64) (throughput.Event, bool) {
	var header struct {
		Type string `json:"type"`
	}
	if json.Unmarshal(raw, &header) != nil {
		return throughput.Event{}, false
	}
	kind := ""
	switch header.Type {
	case "task_started", "turn_started":
		kind = "start"
	case "task_complete", "turn_complete":
		kind = "complete"
	case "turn_aborted":
		kind = "abort"
	default:
		return throughput.Event{}, false
	}
	gap := throughput.Event{Kind: "gap", Offset: offset}
	var payload struct {
		TurnID      *string `json:"turn_id"`
		StartedAt   *int64  `json:"started_at"`
		CompletedAt *int64  `json:"completed_at"`
		Duration    *int64  `json:"duration_ms"`
	}
	if json.Unmarshal(raw, &payload) != nil {
		return gap, true
	}
	id := safeThroughputID(payload.TurnID)
	if kind != "abort" && id == nil || payload.TurnID != nil && id == nil {
		return gap, true
	}
	var at *int64
	source := "log_timestamp"
	if parsed, err := time.Parse(time.RFC3339Nano, timestamp); err == nil && parsed.UnixMilli() >= 0 && parsed.UnixMilli() <= throughput.MaxInteger {
		value := parsed.UnixMilli()
		at = &value
	}
	started := throughputSeconds(payload.StartedAt)
	if at == nil {
		source = "source_seconds"
		if kind == "start" {
			at = started
		} else {
			at = throughputSeconds(payload.CompletedAt)
		}
	}
	if payload.Duration != nil && (*payload.Duration < 0 || *payload.Duration > throughput.MaxInteger) {
		return gap, true
	}
	return throughput.Event{Kind: kind, Offset: offset, TurnID: id, AtMS: at, TimeSource: source, StartedAtMS: started, DurationMS: payload.Duration}, true
}

func throughputSeconds(value *int64) *int64 {
	if value == nil || *value < 0 || *value > throughput.MaxInteger/1000 {
		return nil
	}
	ms := *value * 1000
	return &ms
}
