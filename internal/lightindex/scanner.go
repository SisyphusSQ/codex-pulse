package lightindex

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"regexp"
	"strings"
	"time"

	"github.com/SisyphusSQ/codex-pulse/internal/attribution"
)

const (
	DefaultTokenScanChunkBytes = 64 << 10
	DefaultTokenScanMaxLine    = 64 << 20
	maximumSafeInvocationInt   = int64(9_007_199_254_740_991)
)

var (
	tokenCountNeedle       = []byte(`"token_count"`)
	turnContextNeedle      = []byte(`"turn_context"`)
	functionCallNeedle     = []byte(`"function_call"`)
	customToolCallNeedle   = []byte(`"custom_tool_call"`)
	mcpToolCallEndNeedle   = []byte(`"mcp_tool_call_end"`)
	webSearchEndNeedle     = []byte(`"web_search_end"`)
	imageGenerationNeedle  = []byte(`"image_generation_end"`)
	explicitSkillNeedle    = []byte(`<skill>`)
	skillFileNeedle        = []byte(`SKILL.md`)
	nestedToolPattern      = regexp.MustCompile(`tools\.([A-Za-z][A-Za-z0-9_]*)[[:space:]]*\(`)
	explicitSkillPattern   = regexp.MustCompile(`(?s)<skill>.*?<name>[[:space:]]*([^<]{1,256})[[:space:]]*</name>`)
	loadedSkillPathPattern = regexp.MustCompile(`(?:^|[/\\])(?:(?:plugins[/\\]cache[/\\][^/\\[:space:]'\"]+[/\\]([^/\\[:space:]'\"]+)[/\\][^/\\[:space:]'\"]+[/\\])?skills[/\\]([^/\\[:space:]'\"]{1,256})[/\\]SKILL\.md)`)
	safeInvocationName     = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_.:/-]{0,127}$`)
)

type InvocationKind string

const (
	InvocationKindTool  InvocationKind = "tool"
	InvocationKindSkill InvocationKind = "skill"
)

type InvocationSource string

const (
	InvocationSourceResponseFunction InvocationSource = "response_function"
	InvocationSourceResponseCustom   InvocationSource = "response_custom"
	InvocationSourceExecNested       InvocationSource = "exec_nested"
	InvocationSourceMCP              InvocationSource = "mcp"
	InvocationSourceWebSearch        InvocationSource = "web_search"
	InvocationSourceImageGeneration  InvocationSource = "image_generation"
	InvocationSourceSkillExplicit    InvocationSource = "skill_explicit"
	InvocationSourceSkillFileLoaded  InvocationSource = "skill_file_loaded"
)

type InvocationOutcome string

const (
	InvocationOutcomeUnknown   InvocationOutcome = "unknown"
	InvocationOutcomeSucceeded InvocationOutcome = "succeeded"
	InvocationOutcomeFailed    InvocationOutcome = "failed"
)

type InvocationDelta struct {
	SourceOffset int64
	Ordinal      int
	ObservedAtMS int64
	Kind         InvocationKind
	Name         string
	Source       InvocationSource
	Outcome      InvocationOutcome
	DurationMS   *int64
}

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
	ModelKey     *string
	ModelSource  attribution.Source
	Tokens       TokenTotals
}

type ScanDiagnostic struct {
	Code        string
	StartOffset int64
	EndOffset   int64
}

type ScanState struct {
	DurableOffset      int64
	HighWater          TokenTotals
	CurrentModelKey    *string
	CurrentModelSource attribution.Source
}

type ScanResult struct {
	State            ScanState
	DurableOffset    int64
	BytesRead        int64
	LinesSeen        int64
	CandidateLines   int64
	JSONDecoded      int64
	TokenEvents      int64
	Complete         bool
	DailyDeltas      []DailyTokenDelta
	TokenDeltas      []TimedTokenDelta
	InvocationDeltas []InvocationDelta
	Diagnostics      []ScanDiagnostic
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
	if seed.CurrentModelSource == "" {
		seed.CurrentModelSource = attribution.SourceMissing
	}
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
	if !candidateLine(line) {
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
	if envelope.Type == "turn_context" {
		var payload struct {
			Model string `json:"model"`
		}
		if err := json.Unmarshal(envelope.Payload, &payload); err != nil {
			result.Diagnostics = append(result.Diagnostics, ScanDiagnostic{
				Code: "candidate_invalid_payload", StartOffset: startOffset, EndOffset: endOffset,
			})
			return
		}
		decision := attribution.NormalizeModel(payload.Model)
		result.State.CurrentModelKey = optionalString(decision.Key)
		result.State.CurrentModelSource = decision.Source
		return
	}
	if envelope.Type == "response_item" {
		scanner.processResponseItem(envelope.Timestamp, envelope.Payload, startOffset, endOffset, result)
		return
	}
	if envelope.Type != "event_msg" {
		return
	}

	var payload struct {
		Type       string          `json:"type"`
		DurationMS *int64          `json:"duration_ms"`
		Invocation json.RawMessage `json:"invocation"`
		Result     json.RawMessage `json:"result"`
		Info       *struct {
			Total *struct {
				Input       int64 `json:"input_tokens"`
				CachedInput int64 `json:"cached_input_tokens"`
				Output      int64 `json:"output_tokens"`
				Reasoning   int64 `json:"reasoning_output_tokens"`
			} `json:"total_token_usage"`
		} `json:"info"`
	}
	if err := json.Unmarshal(envelope.Payload, &payload); err != nil {
		if err != nil {
			result.Diagnostics = append(result.Diagnostics, ScanDiagnostic{
				Code: "candidate_invalid_payload", StartOffset: startOffset, EndOffset: endOffset,
			})
		}
		return
	}
	if payload.Type != "token_count" {
		scanner.processEventInvocation(envelope.Timestamp, payload.Type, payload.DurationMS, payload.Invocation, payload.Result, startOffset, endOffset, result)
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
		SourceOffset: endOffset, ObservedAtMS: observedAt.UnixMilli(),
		ModelKey: cloneString(result.State.CurrentModelKey), ModelSource: result.State.CurrentModelSource,
		Tokens: delta,
	})
	day := observedAt.UTC().Format("2006-01-02")
	if index, ok := dailyIndexes[day]; ok {
		result.DailyDeltas[index].Tokens = addTotals(result.DailyDeltas[index].Tokens, delta)
		return
	}
	dailyIndexes[day] = len(result.DailyDeltas)
	result.DailyDeltas = append(result.DailyDeltas, DailyTokenDelta{Day: day, Tokens: delta})
}

func candidateLine(line []byte) bool {
	for _, needle := range [][]byte{
		tokenCountNeedle, turnContextNeedle, functionCallNeedle, customToolCallNeedle,
		mcpToolCallEndNeedle, webSearchEndNeedle, imageGenerationNeedle,
		explicitSkillNeedle, skillFileNeedle,
	} {
		if bytes.Contains(line, needle) {
			return true
		}
	}
	return false
}

func (scanner *TokenScanner) processResponseItem(
	timestamp string,
	raw json.RawMessage,
	startOffset int64,
	endOffset int64,
	result *ScanResult,
) {
	var payload struct {
		Type    string `json:"type"`
		Name    string `json:"name"`
		Input   string `json:"input"`
		Role    string `json:"role"`
		Content []struct {
			Type string `json:"type"`
			Text string `json:"text"`
		} `json:"content"`
	}
	if err := json.Unmarshal(raw, &payload); err != nil {
		result.Diagnostics = append(result.Diagnostics, ScanDiagnostic{
			Code: "candidate_invalid_payload", StartOffset: startOffset, EndOffset: endOffset,
		})
		return
	}
	observedAtMS, ok := invocationTimestamp(timestamp, startOffset, endOffset, result)
	if !ok {
		return
	}
	switch payload.Type {
	case "function_call":
		appendInvocation(result, endOffset, observedAtMS, InvocationKindTool, payload.Name,
			InvocationSourceResponseFunction, InvocationOutcomeUnknown, nil)
	case "custom_tool_call":
		appendInvocation(result, endOffset, observedAtMS, InvocationKindTool, payload.Name,
			InvocationSourceResponseCustom, InvocationOutcomeUnknown, nil)
		for _, match := range nestedToolPattern.FindAllStringSubmatch(payload.Input, -1) {
			if len(match) != 2 || strings.Contains(match[1], "__") {
				continue
			}
			appendInvocation(result, endOffset, observedAtMS, InvocationKindTool, match[1],
				InvocationSourceExecNested, InvocationOutcomeUnknown, nil)
		}
		appendLoadedSkills(result, payload.Input, endOffset, observedAtMS)
	case "message":
		if payload.Role != "user" {
			return
		}
		seen := make(map[string]struct{})
		for _, content := range payload.Content {
			if content.Type != "input_text" {
				continue
			}
			for _, match := range explicitSkillPattern.FindAllStringSubmatch(content.Text, -1) {
				if len(match) != 2 {
					continue
				}
				name := strings.TrimSpace(match[1])
				if _, duplicated := seen[name]; duplicated {
					continue
				}
				seen[name] = struct{}{}
				appendInvocation(result, endOffset, observedAtMS, InvocationKindSkill, name,
					InvocationSourceSkillExplicit, InvocationOutcomeUnknown, nil)
			}
		}
	}
}

func (scanner *TokenScanner) processEventInvocation(
	timestamp string,
	eventType string,
	durationMS *int64,
	invocationRaw json.RawMessage,
	resultRaw json.RawMessage,
	startOffset int64,
	endOffset int64,
	result *ScanResult,
) {
	if eventType != "mcp_tool_call_end" && eventType != "web_search_end" && eventType != "image_generation_end" {
		return
	}
	observedAtMS, ok := invocationTimestamp(timestamp, startOffset, endOffset, result)
	if !ok {
		return
	}
	switch eventType {
	case "mcp_tool_call_end":
		var invocation struct {
			Server string `json:"server"`
			Tool   string `json:"tool"`
		}
		if err := json.Unmarshal(invocationRaw, &invocation); err != nil {
			result.Diagnostics = append(result.Diagnostics, ScanDiagnostic{
				Code: "candidate_invalid_payload", StartOffset: startOffset, EndOffset: endOffset,
			})
			return
		}
		outcome := InvocationOutcomeUnknown
		var resultFields map[string]json.RawMessage
		if json.Unmarshal(resultRaw, &resultFields) == nil {
			switch {
			case resultFields["Ok"] != nil:
				outcome = InvocationOutcomeSucceeded
			case resultFields["Err"] != nil:
				outcome = InvocationOutcomeFailed
			}
		}
		appendInvocation(result, endOffset, observedAtMS, InvocationKindTool,
			strings.TrimSpace(invocation.Server)+"."+strings.TrimSpace(invocation.Tool),
			InvocationSourceMCP, outcome, durationMS)
	case "web_search_end":
		appendInvocation(result, endOffset, observedAtMS, InvocationKindTool, "web_search",
			InvocationSourceWebSearch, InvocationOutcomeUnknown, durationMS)
	case "image_generation_end":
		appendInvocation(result, endOffset, observedAtMS, InvocationKindTool, "image_generation",
			InvocationSourceImageGeneration, InvocationOutcomeUnknown, durationMS)
	}
}

func appendLoadedSkills(result *ScanResult, input string, sourceOffset int64, observedAtMS int64) {
	seen := make(map[string]struct{})
	for _, match := range loadedSkillPathPattern.FindAllStringSubmatch(input, -1) {
		if len(match) != 3 {
			continue
		}
		name := strings.TrimSpace(match[2])
		if plugin := strings.TrimSpace(match[1]); plugin != "" {
			name = plugin + ":" + name
		}
		if _, duplicated := seen[name]; duplicated {
			continue
		}
		seen[name] = struct{}{}
		appendInvocation(result, sourceOffset, observedAtMS, InvocationKindSkill, name,
			InvocationSourceSkillFileLoaded, InvocationOutcomeUnknown, nil)
	}
}

func invocationTimestamp(
	timestamp string,
	startOffset int64,
	endOffset int64,
	result *ScanResult,
) (int64, bool) {
	observedAt, err := time.Parse(time.RFC3339Nano, timestamp)
	if err != nil {
		result.Diagnostics = append(result.Diagnostics, ScanDiagnostic{
			Code: "candidate_invalid_timestamp", StartOffset: startOffset, EndOffset: endOffset,
		})
		return 0, false
	}
	observedAtMS := observedAt.UnixMilli()
	if observedAtMS < 0 || observedAtMS > maximumSafeInvocationInt {
		result.Diagnostics = append(result.Diagnostics, ScanDiagnostic{
			Code: "candidate_invalid_timestamp", StartOffset: startOffset, EndOffset: endOffset,
		})
		return 0, false
	}
	return observedAtMS, true
}

func appendInvocation(
	result *ScanResult,
	sourceOffset int64,
	observedAtMS int64,
	kind InvocationKind,
	name string,
	source InvocationSource,
	outcome InvocationOutcome,
	durationMS *int64,
) {
	name = strings.TrimSpace(name)
	if !safeInvocationName.MatchString(name) ||
		(durationMS != nil && (*durationMS < 0 || *durationMS > maximumSafeInvocationInt)) {
		return
	}
	ordinal := 0
	for index := len(result.InvocationDeltas) - 1; index >= 0; index-- {
		if result.InvocationDeltas[index].SourceOffset != sourceOffset {
			break
		}
		ordinal++
	}
	result.InvocationDeltas = append(result.InvocationDeltas, InvocationDelta{
		SourceOffset: sourceOffset, Ordinal: ordinal, ObservedAtMS: observedAtMS,
		Kind: kind, Name: name, Source: source, Outcome: outcome, DurationMS: cloneInt64(durationMS),
	})
}

func cloneInt64(value *int64) *int64 {
	if value == nil {
		return nil
	}
	cloned := *value
	return &cloned
}

func optionalString(value string) *string {
	if value == "" {
		return nil
	}
	return &value
}

func cloneString(value *string) *string {
	if value == nil {
		return nil
	}
	cloned := *value
	return &cloned
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
