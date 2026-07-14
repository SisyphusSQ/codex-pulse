package index

import (
	"bytes"
	"encoding/json"
	"errors"
	"runtime"
	"strings"
	"testing"
)

func TestParseUsesLatestValidAppendEntryAndKeepsContentFreeDiagnostics(t *testing.T) {
	t.Parallel()

	content := []byte(
		`{"id":"019d4b69-25d1-7be2-b5f4-8c4234ed5dee","thread_name":"first","updated_at":"2026-04-01T23:39:24Z"}` + "\n" +
			`not-json-with-private-content` + "\n" +
			`{"id":"019d4b69-25d1-7be2-b5f4-8c4234ed5dee","thread_name":"latest","updated_at":"unknown"}`,
	)

	parsed, err := Parse(content)
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}
	if len(parsed.Entries) != 2 {
		t.Fatalf("valid entries = %d, want 2", len(parsed.Entries))
	}
	latest, found := parsed.Latest("019d4b69-25d1-7be2-b5f4-8c4234ed5dee")
	if !found || latest.ThreadName != "latest" || latest.Line != 3 || latest.UpdatedAtMS != nil {
		t.Fatalf("Latest() = %#v, %v", latest, found)
	}
	if got := parsed.HistoryCount("019d4b69-25d1-7be2-b5f4-8c4234ed5dee"); got != 2 {
		t.Fatalf("HistoryCount() = %d, want 2", got)
	}
	want := []Diagnostic{
		{Line: 2, Code: DiagnosticMalformedJSON},
		{Line: 3, Code: DiagnosticInvalidUpdatedAt},
	}
	if !equalDiagnostics(parsed.Diagnostics, want) {
		t.Fatalf("diagnostics = %#v, want %#v", parsed.Diagnostics, want)
	}
	for _, diagnostic := range parsed.Diagnostics {
		if diagnostic.Raw != "" {
			t.Fatalf("diagnostic leaked raw content: %#v", diagnostic)
		}
	}
}

func TestParseRejectsDuplicateJSONKeys(t *testing.T) {
	t.Parallel()

	parsed, err := Parse([]byte(
		`{"id":"019d4b69-25d1-7be2-b5f4-8c4234ed5dee","thread_name":"safe","thread_name":"shadowed","updated_at":"2026-04-01T23:39:24Z"}`,
	))
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}
	if len(parsed.Entries) != 0 {
		t.Fatalf("entries = %#v, want none", parsed.Entries)
	}
	want := []Diagnostic{{Line: 1, Code: DiagnosticMalformedJSON}}
	if !equalDiagnostics(parsed.Diagnostics, want) {
		t.Fatalf("diagnostics = %#v, want %#v", parsed.Diagnostics, want)
	}
}

func TestParseFailsClosedForUpstreamValidUnsupportedLatestEntry(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		threadName string
		wantErr    error
	}{
		{name: "blank thread name", threadName: " ", wantErr: ErrUnsupportedIndexEntry},
		{name: "thread name above local output limit", threadName: strings.Repeat("n", maxThreadNameBytes+1), wantErr: ErrUnsupportedIndexEntry},
		{name: "schema valid line above parser limit", threadName: strings.Repeat("n", maxIndexLineBytes), wantErr: ErrIndexLineTooLarge},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			content := encodeEntryForTest(t, Entry{
				ID: "019d4b69-25d1-7be2-b5f4-8c4234ed5dee", ThreadName: "older",
				UpdatedAt: "2026-04-01T00:00:00Z",
			})
			content = append(content, '\n')
			content = append(content, encodeEntryForTest(t, Entry{
				ID: "019d4b69-25d1-7be2-b5f4-8c4234ed5dee", ThreadName: test.threadName,
				UpdatedAt: "2026-04-04T00:00:00Z",
			})...)

			parsed, err := Parse(content)
			if !errors.Is(err, test.wantErr) {
				t.Fatalf("Parse(upstream-valid unsupported latest) = %#v, %v, want %v", parsed, err, test.wantErr)
			}
		})
	}
}

func TestParseSkipsRowsMissingRequiredUpstreamStringFields(t *testing.T) {
	t.Parallel()

	parsed, err := Parse([]byte(
		`{"id":"019d4b69-25d1-7be2-b5f4-8c4234ed5dee","thread_name":null,"updated_at":"2026-04-01T00:00:00Z"}` + "\n" +
			`{"id":"019d4b69-25d1-7be2-b5f4-8c4234ed5def","thread_name":"missing-time"}`,
	))
	if err != nil {
		t.Fatalf("Parse(missing required strings) error = %v", err)
	}
	if len(parsed.Entries) != 0 {
		t.Fatalf("entries = %#v, want none", parsed.Entries)
	}
	want := []Diagnostic{
		{Line: 1, Code: DiagnosticInvalidThreadName},
		{Line: 2, Code: DiagnosticInvalidUpdatedAt},
	}
	if !equalDiagnostics(parsed.Diagnostics, want) {
		t.Fatalf("diagnostics = %#v, want %#v", parsed.Diagnostics, want)
	}
}

func TestParseDenseEmptyLinesHasBoundedAllocation(t *testing.T) {
	content := bytes.Repeat([]byte{'\n'}, 1<<20)
	runtime.GC()
	var before runtime.MemStats
	runtime.ReadMemStats(&before)
	parsed, err := Parse(content)
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}
	var after runtime.MemStats
	runtime.ReadMemStats(&after)
	if len(parsed.Entries) != 0 || len(parsed.Diagnostics) != 0 {
		t.Fatalf("dense empty parse = %#v", parsed)
	}
	allocated := after.TotalAlloc - before.TotalAlloc
	if allocated > uint64(len(content))*4 {
		t.Fatalf("Parse() allocated %d bytes for %d bytes of empty lines", allocated, len(content))
	}
}

func equalDiagnostics(left, right []Diagnostic) bool {
	if len(left) != len(right) {
		return false
	}
	for index := range left {
		if left[index] != right[index] {
			return false
		}
	}
	return true
}

func encodeEntryForTest(t *testing.T, entry Entry) []byte {
	t.Helper()
	encoded, err := json.Marshal(entry)
	if err != nil {
		t.Fatalf("json.Marshal(%#v): %v", entry, err)
	}
	return encoded
}
