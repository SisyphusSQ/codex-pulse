package lightindex

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"time"
)

const (
	DefaultTokenScanChunkBytes = 64 << 10
	DefaultTokenScanMaxLine    = 64 << 20
)

var tokenCountNeedle = []byte(`"token_count"`)

type TokenTotals struct {
	Input       int64
	CachedInput int64
	Output      int64
	Reasoning   int64
}

type DailyTokenDelta struct {
	Day    string
	Tokens TokenTotals
}

type TimedTokenDelta struct {
	SourceOffset int64
	ObservedAtMS int64
	Tokens       TokenTotals
}

type ScanDiagnostic struct {
	Code        string
	StartOffset int64
	EndOffset   int64
}

type ScanState struct {
	DurableOffset int64
	HighWater     TokenTotals
}

type ScanResult struct {
	State          ScanState
	DurableOffset  int64
	BytesRead      int64
	LinesSeen      int64
	CandidateLines int64
	JSONDecoded    int64
	TokenEvents    int64
	Complete       bool
	DailyDeltas    []DailyTokenDelta
	TokenDeltas    []TimedTokenDelta
	Diagnostics    []ScanDiagnostic
}

type TokenScannerOptions struct {
	ChunkBytes int
	MaxLine    int
}

type TokenScanner struct {
	chunkBytes int
	maxLine    int
}

func NewTokenScanner(options TokenScannerOptions) *TokenScanner {
	chunkBytes := options.ChunkBytes
	if chunkBytes <= 0 {
		chunkBytes = DefaultTokenScanChunkBytes
	}
	maxLine := options.MaxLine
	if maxLine <= 0 {
		maxLine = DefaultTokenScanMaxLine
	}
	return &TokenScanner{chunkBytes: chunkBytes, maxLine: maxLine}
}

func (scanner *TokenScanner) Scan(ctx context.Context, reader io.Reader, seed ScanState) (ScanResult, error) {
	result := ScanResult{State: seed, DurableOffset: seed.DurableOffset}
	if scanner == nil || scanner.chunkBytes <= 0 || scanner.maxLine <= 0 || reader == nil || seed.DurableOffset < 0 {
		return result, errors.New("invalid token scanner input")
	}
	if err := ctx.Err(); err != nil {
		return result, err
	}

	buffer := make([]byte, scanner.chunkBytes)
	pending := make([]byte, 0, scanner.chunkBytes)
	dailyIndexes := make(map[string]int)

	for {
		if err := ctx.Err(); err != nil {
			return result, err
		}
		readBytes, readErr := reader.Read(buffer)
		if readBytes > 0 {
			result.BytesRead += int64(readBytes)
			pending = append(pending, buffer[:readBytes]...)
			for {
				newline := bytes.IndexByte(pending, '\n')
				if newline < 0 {
					break
				}
				if err := ctx.Err(); err != nil {
					return result, err
				}
				lineStart := result.DurableOffset
				lineEnd := lineStart + int64(newline+1)
				line := pending[:newline]
				result.LinesSeen++
				if len(line) > scanner.maxLine {
					result.Diagnostics = append(result.Diagnostics, ScanDiagnostic{
						Code: "candidate_line_too_long", StartOffset: lineStart, EndOffset: lineEnd,
					})
				} else {
					scanner.processLine(line, lineStart, lineEnd, dailyIndexes, &result)
				}
				result.DurableOffset = lineEnd
				result.State.DurableOffset = lineEnd
				pending = pending[newline+1:]
			}
		}

		if readErr != nil {
			if errors.Is(readErr, io.EOF) {
				result.Complete = len(pending) == 0
				return result, nil
			}
			return result, fmt.Errorf("read token source: %w", readErr)
		}
		if readBytes == 0 {
			return result, io.ErrNoProgress
		}
	}
}

func (scanner *TokenScanner) processLine(
	line []byte,
	startOffset int64,
	endOffset int64,
	dailyIndexes map[string]int,
	result *ScanResult,
) {
	if !bytes.Contains(line, tokenCountNeedle) {
		return
	}
	result.CandidateLines++
	result.JSONDecoded++

	var envelope struct {
		Timestamp string          `json:"timestamp"`
		Type      string          `json:"type"`
		Payload   json.RawMessage `json:"payload"`
	}
	if err := json.Unmarshal(line, &envelope); err != nil {
		result.Diagnostics = append(result.Diagnostics, ScanDiagnostic{
			Code: "candidate_bad_json", StartOffset: startOffset, EndOffset: endOffset,
		})
		return
	}
	if envelope.Type != "event_msg" {
		return
	}

	var payload struct {
		Type string `json:"type"`
		Info *struct {
			Total *struct {
				Input       int64 `json:"input_tokens"`
				CachedInput int64 `json:"cached_input_tokens"`
				Output      int64 `json:"output_tokens"`
				Reasoning   int64 `json:"reasoning_output_tokens"`
			} `json:"total_token_usage"`
		} `json:"info"`
	}
	if err := json.Unmarshal(envelope.Payload, &payload); err != nil || payload.Type != "token_count" {
		if err != nil {
			result.Diagnostics = append(result.Diagnostics, ScanDiagnostic{
				Code: "candidate_invalid_payload", StartOffset: startOffset, EndOffset: endOffset,
			})
		}
		return
	}
	if payload.Info == nil || payload.Info.Total == nil {
		return
	}
	observedAt, err := time.Parse(time.RFC3339Nano, envelope.Timestamp)
	if err != nil {
		result.Diagnostics = append(result.Diagnostics, ScanDiagnostic{
			Code: "candidate_invalid_timestamp", StartOffset: startOffset, EndOffset: endOffset,
		})
		return
	}
	current := TokenTotals{
		Input:       payload.Info.Total.Input,
		CachedInput: payload.Info.Total.CachedInput,
		Output:      payload.Info.Total.Output,
		Reasoning:   payload.Info.Total.Reasoning,
	}
	if current.Input < 0 || current.CachedInput < 0 || current.Output < 0 || current.Reasoning < 0 {
		result.Diagnostics = append(result.Diagnostics, ScanDiagnostic{
			Code: "candidate_invalid_counter", StartOffset: startOffset, EndOffset: endOffset,
		})
		return
	}

	delta := positiveDelta(result.State.HighWater, current)
	result.State.HighWater = componentMaximum(result.State.HighWater, current)
	result.TokenEvents++
	if delta == (TokenTotals{}) {
		return
	}
	result.TokenDeltas = append(result.TokenDeltas, TimedTokenDelta{
		SourceOffset: endOffset, ObservedAtMS: observedAt.UnixMilli(), Tokens: delta,
	})
	day := observedAt.UTC().Format("2006-01-02")
	if index, ok := dailyIndexes[day]; ok {
		result.DailyDeltas[index].Tokens = addTotals(result.DailyDeltas[index].Tokens, delta)
		return
	}
	dailyIndexes[day] = len(result.DailyDeltas)
	result.DailyDeltas = append(result.DailyDeltas, DailyTokenDelta{Day: day, Tokens: delta})
}

func positiveDelta(previous, current TokenTotals) TokenTotals {
	return TokenTotals{
		Input:       positiveDifference(previous.Input, current.Input),
		CachedInput: positiveDifference(previous.CachedInput, current.CachedInput),
		Output:      positiveDifference(previous.Output, current.Output),
		Reasoning:   positiveDifference(previous.Reasoning, current.Reasoning),
	}
}

func positiveDifference(previous, current int64) int64 {
	if current <= previous {
		return 0
	}
	return current - previous
}

func componentMaximum(left, right TokenTotals) TokenTotals {
	return TokenTotals{
		Input:       max(left.Input, right.Input),
		CachedInput: max(left.CachedInput, right.CachedInput),
		Output:      max(left.Output, right.Output),
		Reasoning:   max(left.Reasoning, right.Reasoning),
	}
}

func addTotals(left, right TokenTotals) TokenTotals {
	return TokenTotals{
		Input:       left.Input + right.Input,
		CachedInput: left.CachedInput + right.CachedInput,
		Output:      left.Output + right.Output,
		Reasoning:   left.Reasoning + right.Reasoning,
	}
}
