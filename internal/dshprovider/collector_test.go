package dshprovider

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/klauspost/compress/zstd"

	"github.com/SisyphusSQ/codex-pulse/internal/store"
)

func fixture(seeded bool) string {
	h := map[string]any{"type": "session", "version": 4, "id": "session-test", "createdAt": 1000, "cwd": "/synthetic/demo", "isSeeded": seeded, "delegationDepth": 0}
	b, _ := json.Marshal(h)
	s := string(b) + "\n"
	seq := 0
	add := func(kind string, at int, data any) {
		b, _ := json.Marshal(map[string]any{"seq": seq, "type": kind, "time": at, "data": data})
		s += string(b) + "\n"
		seq++
	}
	add("request/header", 1001, map[string]any{"header": map[string]any{"config": map[string]any{"provider": "deepseek-account", "model": "deepseek-flash"}}})
	if seeded {
		add("assistant/message", 1002, map[string]any{"turn": 0, "step": 0, "usage": map[string]any{"inputTokens": 900, "outputTokens": 900, "totalTokens": 1800}})
		add("session/end-seed", 1003, map[string]any{"inherited": true})
	}
	add("turn/start", 1010, map[string]int{"turn": 1})
	add("step/start", 1011, map[string]int{"turn": 1, "step": 1})
	add("assistant/attempt", 1020, map[string]any{"turn": 1, "step": 1, "stream": []any{map[string]any{"type": "chunk", "time": 1020, "chunk": map[string]any{"type": "usage", "usage": map[string]int{"inputTokens": 2, "outputTokens": 3, "cacheReadTokens": 5, "totalTokens": 10}}}}})
	add("assistant/message", 1030, map[string]any{"turn": 1, "step": 1, "usage": map[string]int{"inputTokens": 10, "outputTokens": 20, "cacheReadTokens": 30, "totalTokens": 60}, "message": map[string]any{"content": []any{map[string]string{"type": "text", "text": "PRIVATE MESSAGE"}}}})
	add("tool/call", 1031, map[string]any{"turn": 1, "step": 1, "callId": "tool-1", "name": "read_file", "arguments": "PRIVATE ARGS"})
	add("tool/result", 1040, map[string]any{"turn": 1, "step": 1, "message": map[string]any{"toolCallId": "tool-1", "isError": true, "content": []any{map[string]string{"type": "text", "text": "PRIVATE RESULT"}}}})
	add("turn/end", 1050, map[string]int{"turn": 1})
	return s
}
func TestDSHUsageAttemptsSeedsAndPrivacy(t *testing.T) {
	for _, seeded := range []bool{false, true} {
		s, err := parseSession(t.Context(), strings.NewReader(fixture(seeded)), 4, 2000)
		if err != nil {
			t.Fatal(err)
		}
		if len(s.usage) != 2 || s.session.RequestCount != 2 || s.usage[1].InputTokens != 10 || s.usage[1].CachedReadTokens != 30 || !s.usage[1].CacheWriteKnown || s.tools[0].Outcome != "failed" {
			t.Fatalf("invalid projection: %+v", s)
		}
		if s.session.Throughput.Measures.IncludedTurns != 1 || *s.session.Throughput.Measures.OutputTokens != 23 {
			t.Fatalf("invalid throughput: %+v", s.session.Throughput)
		}
		b, _ := json.Marshal(store.DSHSnapshot{Sessions: []store.DSHSession{s.session}, UsageEvents: s.usage, ToolEvents: s.tools})
		if strings.Contains(string(b), "PRIVATE") || strings.Contains(string(b), "session-test") || strings.Contains(string(b), "/synthetic/demo") {
			t.Fatal("content retained")
		}
		totals, _ := totalsForUsageEvents(s.usage)
		if *totals.InputTokens.Value != 47 || *totals.TotalTokens.Value != 70 {
			t.Fatal("disjoint input lost")
		}
	}
}
func TestDSHRefusesCorruptionTornTailAndUnknownFormat(t *testing.T) {
	for _, content := range []string{fixture(false) + `{"seq":8`, strings.Replace(fixture(false), `"seq":0`, `"seq":99`, 1), strings.Replace(fixture(false), `"inputTokens":10`, `"inputTokens":10,"inputTokens":5`, 1)} {
		if _, err := parseSession(t.Context(), strings.NewReader(content), 4, 2000); err == nil {
			t.Fatal("accepted corrupt input")
		}
	}
	if _, err := parseSession(t.Context(), strings.NewReader(fixture(false)), 3, 2000); err == nil {
		t.Fatal("version mismatch")
	}
}

type captureWriter struct{ snapshot store.DSHSnapshot }

func (w *captureWriter) ReplaceDSHSnapshot(_ context.Context, s store.DSHSnapshot) error {
	w.snapshot = s
	return nil
}
func TestDSHZstdCanonicalGenerationAndLastGood(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, "project", "session")
	if err := os.MkdirAll(dir, 0700); err != nil {
		t.Fatal(err)
	}
	encoder, err := zstd.NewWriter(nil)
	if err != nil {
		t.Fatal(err)
	}
	defer encoder.Close()
	path := filepath.Join(dir, "session.v4.jsonl.zstd")
	b := encoder.EncodeAll([]byte(fixture(false)), nil)
	if err := os.WriteFile(path, b, 0600); err != nil {
		t.Fatal(err)
	}
	// The older generation must never contribute a second copy.
	if err := os.WriteFile(filepath.Join(dir, "session.v3.jsonl.zstd"), b, 0600); err != nil {
		t.Fatal(err)
	}
	writer := &captureWriter{}
	c, err := NewCollector(writer, Config{SessionsRoot: root, Now: func() time.Time { return time.UnixMilli(2000) }})
	if err != nil {
		t.Fatal(err)
	}
	if err := c.Refresh(t.Context()); err != nil {
		t.Fatal(err)
	}
	if len(writer.snapshot.Sessions) != 1 || len(writer.snapshot.UsageEvents) != 2 {
		t.Fatal("canonical generation duplicated")
	}
	if err := os.WriteFile(path, b[:len(b)-4], 0600); err != nil {
		t.Fatal(err)
	}
	if err := c.Refresh(t.Context()); err != nil {
		t.Fatal(err)
	}
	if len(writer.snapshot.UsageEvents) != 2 || writer.snapshot.Sources[0].State != "partial" {
		t.Fatal("last good lost")
	}
}

func TestDSHDuplicateFilesAndDivergentLineage(t *testing.T) {
	root := t.TempDir()
	paths := make([]string, 0, 2)
	for _, name := range []string{"first", "second"} {
		dir := filepath.Join(root, "project", name)
		if err := os.MkdirAll(dir, 0700); err != nil {
			t.Fatal(err)
		}
		path := filepath.Join(dir, "session.v4.jsonl")
		if err := os.WriteFile(path, []byte(fixture(false)), 0600); err != nil {
			t.Fatal(err)
		}
		paths = append(paths, path)
	}
	writer := &captureWriter{}
	c, err := NewCollector(writer, Config{SessionsRoot: root, Now: func() time.Time { return time.UnixMilli(2000) }})
	if err != nil {
		t.Fatal(err)
	}
	if err := c.Refresh(t.Context()); err != nil {
		t.Fatal(err)
	}
	if len(writer.snapshot.Sessions) != 1 || len(writer.snapshot.UsageEvents) != 2 || writer.snapshot.Sources[0].State != "available" {
		t.Fatal("identical copies changed usage or coverage")
	}
	divergent := strings.Replace(fixture(false), `"inputTokens":10`, `"inputTokens":11`, 1)
	if err := os.WriteFile(paths[1], []byte(divergent), 0600); err != nil {
		t.Fatal(err)
	}
	if err := c.Refresh(t.Context()); err != nil {
		t.Fatal(err)
	}
	if !writer.snapshot.Sessions[0].LineageConflict || writer.snapshot.Sessions[0].CoverageState != "partial" || writer.snapshot.Sources[0].State != "partial" || len(writer.snapshot.UsageEvents) != 2 {
		t.Fatal("divergent copies were presented as complete or counted twice")
	}
}
