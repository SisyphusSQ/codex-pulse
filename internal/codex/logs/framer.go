package logs

import (
	"bytes"
	"fmt"
	"math"
)

type lineFrame struct {
	StartOffset int64
	EndOffset   int64
	Content     []byte
}

type lineFrameResult struct {
	Lines             []lineFrame
	Diagnostics       []ParserDiagnostic
	ReadOffset        int64
	CommittableOffset int64
	BufferedBytes     int
}

type lineFramer struct {
	readOffset        int64
	committableOffset int64
	lineStartOffset   int64
	maxLineBytes      int
	buffer            []byte
	discarding        bool
}

func newLineFramer(startOffset int64, maxLineBytes int) (*lineFramer, error) {
	if startOffset < 0 || maxLineBytes <= 0 {
		return nil, ErrInvalidParserConfig
	}

	return &lineFramer{
		readOffset:        startOffset,
		committableOffset: startOffset,
		lineStartOffset:   startOffset,
		maxLineBytes:      maxLineBytes,
	}, nil
}

func (framer *lineFramer) Feed(startOffset int64, chunk []byte) (lineFrameResult, error) {
	if framer == nil {
		return lineFrameResult{}, ErrInvalidParserConfig
	}
	if startOffset != framer.readOffset {
		return lineFrameResult{}, fmt.Errorf(
			"%w: expected offset %d, got %d",
			ErrNonContiguousChunk,
			framer.readOffset,
			startOffset,
		)
	}
	if int64(len(chunk)) > math.MaxInt64-startOffset {
		return lineFrameResult{}, ErrInvalidParserConfig
	}

	result := lineFrameResult{}
	for cursor := 0; cursor < len(chunk); {
		if framer.discarding {
			newline := bytes.IndexByte(chunk[cursor:], '\n')
			if newline < 0 {
				break
			}

			endOffset := startOffset + int64(cursor+newline+1)
			result.Diagnostics = append(result.Diagnostics, ParserDiagnostic{
				Class:       DiagnosticClassFraming,
				Code:        DiagnosticLineTooLong,
				StartOffset: framer.lineStartOffset,
				EndOffset:   endOffset,
			})
			framer.discarding = false
			framer.committableOffset = endOffset
			framer.lineStartOffset = endOffset
			cursor += newline + 1
			continue
		}

		newline := bytes.IndexByte(chunk[cursor:], '\n')
		if newline < 0 {
			remaining := chunk[cursor:]
			combinedLength := len(framer.buffer) + len(remaining)
			endsWithCarriageReturn := len(remaining) > 0 && remaining[len(remaining)-1] == '\r'
			if len(remaining) == 0 && len(framer.buffer) > 0 {
				endsWithCarriageReturn = framer.buffer[len(framer.buffer)-1] == '\r'
			}
			if combinedLength > framer.maxLineBytes &&
				!(combinedLength == framer.maxLineBytes+1 && endsWithCarriageReturn) {
				framer.buffer = nil
				framer.discarding = true
			} else {
				framer.buffer = append(framer.buffer, remaining...)
			}
			break
		}

		linePart := chunk[cursor : cursor+newline]
		endOffset := startOffset + int64(cursor+newline+1)
		combinedLength := len(framer.buffer) + len(linePart)
		endsWithCarriageReturn := len(linePart) > 0 && linePart[len(linePart)-1] == '\r'
		if len(linePart) == 0 && len(framer.buffer) > 0 {
			endsWithCarriageReturn = framer.buffer[len(framer.buffer)-1] == '\r'
		}
		contentLength := combinedLength
		if endsWithCarriageReturn {
			contentLength--
		}
		if contentLength > framer.maxLineBytes {
			framer.buffer = nil
			result.Diagnostics = append(result.Diagnostics, ParserDiagnostic{
				Class:       DiagnosticClassFraming,
				Code:        DiagnosticLineTooLong,
				StartOffset: framer.lineStartOffset,
				EndOffset:   endOffset,
			})
		} else {
			content := make([]byte, 0, len(framer.buffer)+len(linePart))
			content = append(content, framer.buffer...)
			content = append(content, linePart...)
			if len(content) > 0 && content[len(content)-1] == '\r' {
				content = content[:len(content)-1]
			}
			result.Lines = append(result.Lines, lineFrame{
				StartOffset: framer.lineStartOffset,
				EndOffset:   endOffset,
				Content:     content,
			})
			framer.buffer = nil
		}
		framer.committableOffset = endOffset
		framer.lineStartOffset = endOffset
		cursor += newline + 1
	}

	framer.readOffset = startOffset + int64(len(chunk))
	result.ReadOffset = framer.readOffset
	result.CommittableOffset = framer.committableOffset
	result.BufferedBytes = len(framer.buffer)
	return result, nil
}
