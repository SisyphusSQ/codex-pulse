package index

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"
)

type wireEntry struct {
	ID         *string `json:"id"`
	ThreadName *string `json:"thread_name"`
	UpdatedAt  *string `json:"updated_at"`
}

// Parse 按 Codex append-only/latest-wins 语义解析 index；坏行只形成安全诊断。
func Parse(content []byte) (ParsedIndex, error) {
	if len(content) > maxIndexBytes {
		return ParsedIndex{}, ErrIndexTooLarge
	}
	parsed := ParsedIndex{
		latest:  make(map[string]ParsedEntry),
		history: make(map[string]int),
	}
	for lineNumber, start := 1, 0; start <= len(content); lineNumber++ {
		relativeEnd := bytes.IndexByte(content[start:], '\n')
		var line []byte
		if relativeEnd < 0 {
			line = content[start:]
			start = len(content) + 1
		} else {
			line = content[start : start+relativeEnd]
			start += relativeEnd + 1
		}
		trimmed := bytes.TrimSpace(line)
		if len(trimmed) == 0 {
			continue
		}
		if len(trimmed) > maxIndexLineBytes {
			return ParsedIndex{}, ErrIndexLineTooLarge
		}
		if !utf8.Valid(trimmed) {
			parsed.Diagnostics = append(parsed.Diagnostics, Diagnostic{Line: lineNumber, Code: DiagnosticMalformedJSON})
			continue
		}
		if err := validateUniqueJSONKeys(trimmed); err != nil {
			parsed.Diagnostics = append(parsed.Diagnostics, Diagnostic{Line: lineNumber, Code: DiagnosticMalformedJSON})
			continue
		}
		var wire wireEntry
		if err := json.Unmarshal(trimmed, &wire); err != nil {
			parsed.Diagnostics = append(parsed.Diagnostics, Diagnostic{Line: lineNumber, Code: DiagnosticMalformedJSON})
			continue
		}
		if wire.ID == nil {
			parsed.Diagnostics = append(parsed.Diagnostics, Diagnostic{Line: lineNumber, Code: DiagnosticInvalidID})
			continue
		}
		if _, err := uuid.Parse(*wire.ID); err != nil {
			parsed.Diagnostics = append(parsed.Diagnostics, Diagnostic{Line: lineNumber, Code: DiagnosticInvalidID})
			continue
		}
		if wire.ThreadName == nil {
			parsed.Diagnostics = append(parsed.Diagnostics, Diagnostic{Line: lineNumber, Code: DiagnosticInvalidThreadName})
			continue
		}
		if wire.UpdatedAt == nil {
			parsed.Diagnostics = append(parsed.Diagnostics, Diagnostic{Line: lineNumber, Code: DiagnosticInvalidUpdatedAt})
			continue
		}
		if strings.TrimSpace(*wire.ThreadName) == "" || len(*wire.ThreadName) > maxThreadNameBytes {
			return ParsedIndex{}, ErrUnsupportedIndexEntry
		}
		entry := Entry{ID: *wire.ID, ThreadName: *wire.ThreadName, UpdatedAt: *wire.UpdatedAt}
		parsedEntry := ParsedEntry{Entry: entry, Line: lineNumber}
		updatedAt, err := time.Parse(time.RFC3339Nano, entry.UpdatedAt)
		if err != nil {
			parsed.Diagnostics = append(parsed.Diagnostics, Diagnostic{Line: lineNumber, Code: DiagnosticInvalidUpdatedAt})
		} else {
			updatedAtMS := updatedAt.UnixMilli()
			if updatedAtMS < 0 {
				parsed.Diagnostics = append(parsed.Diagnostics, Diagnostic{Line: lineNumber, Code: DiagnosticInvalidUpdatedAt})
			} else {
				parsedEntry.UpdatedAtMS = &updatedAtMS
			}
		}
		parsed.Entries = append(parsed.Entries, parsedEntry)
		parsed.latest[entry.ID] = parsedEntry
		parsed.history[entry.ID]++
	}
	return parsed, nil
}

func validateUniqueJSONKeys(content []byte) error {
	decoder := json.NewDecoder(bytes.NewReader(content))
	decoder.UseNumber()
	if err := validateJSONValue(decoder); err != nil {
		return err
	}
	if _, err := decoder.Token(); !errors.Is(err, io.EOF) {
		if err == nil {
			return errors.New("multiple JSON values")
		}
		return err
	}
	return nil
}

func validateJSONValue(decoder *json.Decoder) error {
	token, err := decoder.Token()
	if err != nil {
		return err
	}
	delimiter, ok := token.(json.Delim)
	if !ok {
		return nil
	}

	switch delimiter {
	case '{':
		seen := make(map[string]struct{})
		for decoder.More() {
			keyToken, err := decoder.Token()
			if err != nil {
				return err
			}
			key, ok := keyToken.(string)
			if !ok {
				return errors.New("invalid object key")
			}
			if _, exists := seen[key]; exists {
				return errors.New("duplicate JSON key")
			}
			seen[key] = struct{}{}
			if err := validateJSONValue(decoder); err != nil {
				return err
			}
		}
		closing, err := decoder.Token()
		if err != nil || closing != json.Delim('}') {
			return errors.New("invalid object closing delimiter")
		}
	case '[':
		for decoder.More() {
			if err := validateJSONValue(decoder); err != nil {
				return err
			}
		}
		closing, err := decoder.Token()
		if err != nil || closing != json.Delim(']') {
			return errors.New("invalid array closing delimiter")
		}
	default:
		return errors.New("unexpected closing delimiter")
	}
	return nil
}
