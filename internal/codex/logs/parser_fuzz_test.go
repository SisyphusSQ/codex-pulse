package logs

import (
	"reflect"
	"testing"
)

func FuzzStreamParser(f *testing.F) {
	seeds := [][]byte{
		[]byte(`{"timestamp":"2026-07-14T01:00:00Z","type":"session_meta","payload":{"id":"session-1","timestamp":"2026-07-14T01:00:00Z","cwd":"/tmp/project","originator":"codex_cli_rs","cli_version":"0.142.3","source":"cli"}}` + "\n"),
		[]byte(`{"timestamp":"2026-07-14T01:00:01Z","type":"event_msg","payload":{"type":"future"}}` + "\n"),
		[]byte("{\xff}\n"),
		[]byte("12345678901234567890\r\n{}\npartial"),
	}
	for _, seed := range seeds {
		f.Add(seed, uint16(1))
		f.Add(seed, uint16(17))
	}

	f.Fuzz(func(t *testing.T, input []byte, requestedSplit uint16) {
		const (
			startOffset int64 = 23
			lineLimit         = 256
		)
		oneChunk := fuzzParse(t, startOffset, lineLimit, input, len(input)+1)
		split := int(requestedSplit)%33 + 1
		chunked := fuzzParse(t, startOffset, lineLimit, input, split)
		if !reflect.DeepEqual(oneChunk, chunked) {
			t.Fatalf("chunking changed result: one=%#v chunked=%#v", oneChunk, chunked)
		}
		if oneChunk.ReadOffset != startOffset+int64(len(input)) ||
			oneChunk.CommittableOffset < startOffset || oneChunk.CommittableOffset > oneChunk.ReadOffset ||
			oneChunk.BufferedBytes < 0 || oneChunk.BufferedBytes > lineLimit+1 {
			t.Fatalf("invalid parser bounds: %#v", oneChunk)
		}
		for _, diagnostic := range oneChunk.Diagnostics {
			if diagnostic.StartOffset < startOffset || diagnostic.EndOffset <= diagnostic.StartOffset ||
				diagnostic.EndOffset > oneChunk.CommittableOffset {
				t.Fatalf("invalid diagnostic position: %#v result=%#v", diagnostic, oneChunk)
			}
		}
	})
}

func fuzzParse(t *testing.T, startOffset int64, lineLimit int, input []byte, chunkSize int) collectedParseResult {
	t.Helper()
	parser, err := NewStreamParser(ParserConfig{
		SourceKind: SourceKindSession, StartOffset: startOffset, MaxLineBytes: lineLimit,
		Seed: testParserSeed("fuzz-session"),
	})
	if err != nil {
		t.Fatalf("NewStreamParser() error = %v", err)
	}
	result := collectedParseResult{ReadOffset: startOffset, CommittableOffset: startOffset}
	for consumed := 0; consumed < len(input); {
		size := chunkSize
		if remaining := len(input) - consumed; size > remaining {
			size = remaining
		}
		current, err := parser.Feed(startOffset+int64(consumed), input[consumed:consumed+size])
		if err != nil {
			t.Fatalf("Feed() error = %v", err)
		}
		result.Events = append(result.Events, current.Events...)
		result.Diagnostics = append(result.Diagnostics, current.Diagnostics...)
		result.Stats.add(current.Stats)
		result.ReadOffset = current.ReadOffset
		result.CommittableOffset = current.CommittableOffset
		result.BufferedBytes = current.BufferedBytes
		result.NextSeed = current.NextSeed
		consumed += size
	}
	return result
}
