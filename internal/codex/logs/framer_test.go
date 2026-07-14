package logs

import (
	"bytes"
	"errors"
	"testing"
)

func TestLineFramerBuffersSplitUTF8UntilCRLFCompletes(t *testing.T) {
	t.Parallel()

	const startOffset int64 = 37
	framer, err := newLineFramer(startOffset, 1024)
	if err != nil {
		t.Fatalf("newLineFramer() error = %v", err)
	}

	line := []byte("{\"model\":\"工程\"}\r\n")
	split := bytes.Index(line, []byte("程")) + 1
	first, err := framer.Feed(startOffset, line[:split])
	if err != nil {
		t.Fatalf("Feed(first) error = %v", err)
	}
	if len(first.Lines) != 0 || len(first.Diagnostics) != 0 {
		t.Fatalf("Feed(first) emitted complete output = %#v", first)
	}
	if first.ReadOffset != startOffset+int64(split) || first.CommittableOffset != startOffset ||
		first.BufferedBytes != split {
		t.Fatalf("Feed(first) offsets = read %d, commit %d, buffered %d", first.ReadOffset, first.CommittableOffset, first.BufferedBytes)
	}

	second, err := framer.Feed(first.ReadOffset, line[split:])
	if err != nil {
		t.Fatalf("Feed(second) error = %v", err)
	}
	if len(second.Diagnostics) != 0 || len(second.Lines) != 1 {
		t.Fatalf("Feed(second) output = %#v", second)
	}
	got := second.Lines[0]
	if got.StartOffset != startOffset || got.EndOffset != startOffset+int64(len(line)) {
		t.Fatalf("line offsets = [%d,%d)", got.StartOffset, got.EndOffset)
	}
	if want := line[:len(line)-2]; !bytes.Equal(got.Content, want) {
		t.Fatalf("line content = %q, want %q", got.Content, want)
	}
	if second.ReadOffset != got.EndOffset || second.CommittableOffset != got.EndOffset || second.BufferedBytes != 0 {
		t.Fatalf("Feed(second) offsets = read %d, commit %d, buffered %d", second.ReadOffset, second.CommittableOffset, second.BufferedBytes)
	}
}

func TestLineFramerRejectsNonContiguousFeedWithoutMutation(t *testing.T) {
	t.Parallel()

	framer, err := newLineFramer(10, 1024)
	if err != nil {
		t.Fatalf("newLineFramer() error = %v", err)
	}
	if _, err := framer.Feed(11, []byte("ignored")); !errors.Is(err, ErrNonContiguousChunk) {
		t.Fatalf("Feed(gap) error = %v, want ErrNonContiguousChunk", err)
	}

	result, err := framer.Feed(10, []byte("{}\n"))
	if err != nil {
		t.Fatalf("Feed(after gap) error = %v", err)
	}
	if len(result.Lines) != 1 || result.Lines[0].StartOffset != 10 || result.Lines[0].EndOffset != 13 {
		t.Fatalf("Feed(after gap) output = %#v", result)
	}
}

func TestLineFramerDiscardsOversizeLineAndRecoversAtNextNewline(t *testing.T) {
	t.Parallel()

	framer, err := newLineFramer(0, 8)
	if err != nil {
		t.Fatalf("newLineFramer() error = %v", err)
	}
	first, err := framer.Feed(0, []byte("123456789"))
	if err != nil {
		t.Fatalf("Feed(oversize prefix) error = %v", err)
	}
	if len(first.Lines) != 0 || len(first.Diagnostics) != 0 || first.BufferedBytes != 0 || first.CommittableOffset != 0 {
		t.Fatalf("Feed(oversize prefix) output = %#v", first)
	}

	second, err := framer.Feed(first.ReadOffset, []byte("discarded\n{}\n"))
	if err != nil {
		t.Fatalf("Feed(oversize suffix) error = %v", err)
	}
	if len(second.Diagnostics) != 1 || second.Diagnostics[0].Code != DiagnosticLineTooLong ||
		second.Diagnostics[0].StartOffset != 0 || second.Diagnostics[0].EndOffset != 19 {
		t.Fatalf("oversize diagnostic = %#v", second.Diagnostics)
	}
	if len(second.Lines) != 1 || !bytes.Equal(second.Lines[0].Content, []byte("{}")) ||
		second.Lines[0].StartOffset != 19 || second.Lines[0].EndOffset != 22 {
		t.Fatalf("recovered line = %#v", second.Lines)
	}
	if second.ReadOffset != 22 || second.CommittableOffset != 22 || second.BufferedBytes != 0 {
		t.Fatalf("Feed(recovery) offsets = read %d, commit %d, buffered %d", second.ReadOffset, second.CommittableOffset, second.BufferedBytes)
	}
}

func TestLineFramerRejectsInvalidConfiguration(t *testing.T) {
	t.Parallel()

	for _, testCase := range []struct {
		name         string
		startOffset  int64
		maxLineBytes int
	}{
		{name: "negative offset", startOffset: -1, maxLineBytes: 8},
		{name: "zero limit", startOffset: 0, maxLineBytes: 0},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()
			if _, err := newLineFramer(testCase.startOffset, testCase.maxLineBytes); !errors.Is(err, ErrInvalidParserConfig) {
				t.Fatalf("newLineFramer() error = %v, want ErrInvalidParserConfig", err)
			}
		})
	}
}

func TestLineFramerDoesNotCountCRLFCarriageReturnAgainstContentLimit(t *testing.T) {
	t.Parallel()

	framer, err := newLineFramer(0, 8)
	if err != nil {
		t.Fatalf("newLineFramer() error = %v", err)
	}
	first, err := framer.Feed(0, []byte("12345678\r"))
	if err != nil {
		t.Fatalf("Feed(content and CR) error = %v", err)
	}
	if len(first.Diagnostics) != 0 || first.BufferedBytes != 9 || first.CommittableOffset != 0 {
		t.Fatalf("Feed(content and CR) result = %#v", first)
	}
	second, err := framer.Feed(first.ReadOffset, []byte("\n"))
	if err != nil {
		t.Fatalf("Feed(LF) error = %v", err)
	}
	if len(second.Diagnostics) != 0 || len(second.Lines) != 1 ||
		!bytes.Equal(second.Lines[0].Content, []byte("12345678")) {
		t.Fatalf("Feed(LF) result = %#v", second)
	}
}
