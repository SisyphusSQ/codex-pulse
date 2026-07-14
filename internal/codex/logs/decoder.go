package logs

import (
	"encoding/json"
	"errors"
	"io"
	"math"
	"strings"
	"time"
	"unicode/utf8"
)

const (
	maxIdentifierBytes = 512
	maxMetadataBytes   = 4096
)

var errDuplicateJSONKey = errors.New("duplicate JSON key")

type decodedLineKind uint8

const (
	decodedSessionMeta decodedLineKind = iota + 1
	decodedTurnContext
	decodedTurnStart
	decodedTokenUsage
	decodedTurnTerminal
)

type decodedSessionMetaRecord struct {
	SessionID     string
	RootSessionID string
	CreatedAtMS   int64
	InitialCWD    string
	Originator    string
	CLIVersion    string
	Source        string
	ModelProvider string
}

type decodedTurnContextRecord struct {
	TurnID *string
	CWD    string
	Model  string
	Effort *string
}

type decodedTurnStartRecord struct {
	TurnID        string
	StartedAtMS   int64
	ContextWindow *int64
}

type decodedTokenUsageRecord struct {
	ObservedAtMS  int64
	Total         TokenCounters
	Last          TokenCounters
	ContextWindow *int64
}

type decodedTurnTerminalRecord struct {
	TurnID        *string
	CompletedAtMS int64
	Outcome       TurnOutcome
}

type tokenUsagePayload struct {
	InputTokens           *int64 `json:"input_tokens"`
	CachedInputTokens     *int64 `json:"cached_input_tokens"`
	OutputTokens          *int64 `json:"output_tokens"`
	ReasoningOutputTokens *int64 `json:"reasoning_output_tokens"`
	TotalTokens           *int64 `json:"total_tokens"`
}

type decodedLine struct {
	Kind         decodedLineKind
	ObservedAtMS int64
	SessionMeta  *decodedSessionMetaRecord
	TurnContext  *decodedTurnContextRecord
	TurnStart    *decodedTurnStartRecord
	TokenUsage   *decodedTokenUsageRecord
	TurnTerminal *decodedTurnTerminalRecord
}

type decodeResult struct {
	Record       *decodedLine
	Diagnostic   *ParserDiagnostic
	KnownIgnored bool
}

type rolloutEnvelope struct {
	Timestamp json.RawMessage `json:"timestamp"`
	Type      json.RawMessage `json:"type"`
	Payload   json.RawMessage `json:"payload"`
}

func decodeRolloutLine(frame lineFrame) decodeResult {
	if len(strings.TrimSpace(string(frame.Content))) == 0 {
		return decodeDiagnostic(frame, DiagnosticClassFraming, DiagnosticEmptyLine)
	}
	if !utf8.Valid(frame.Content) {
		return decodeDiagnostic(frame, DiagnosticClassFraming, DiagnosticInvalidUTF8)
	}
	if err := validateJSON(frame.Content); err != nil {
		if errors.Is(err, errDuplicateJSONKey) {
			return decodeDiagnostic(frame, DiagnosticClassSyntax, DiagnosticDuplicateJSONKey)
		}
		return decodeDiagnostic(frame, DiagnosticClassSyntax, DiagnosticBadJSON)
	}

	var envelope rolloutEnvelope
	if err := json.Unmarshal(frame.Content, &envelope); err != nil {
		return decodeDiagnostic(frame, DiagnosticClassSyntax, DiagnosticInvalidField)
	}
	observedAtMS, ok := decodeTimestamp(envelope.Timestamp)
	if !ok {
		return decodeDiagnostic(frame, DiagnosticClassSyntax, DiagnosticInvalidTimestamp)
	}
	rolloutType, ok := decodeRequiredString(envelope.Type, maxIdentifierBytes)
	if !ok {
		return decodeDiagnostic(frame, DiagnosticClassSyntax, DiagnosticInvalidField)
	}

	switch rolloutType {
	case "session_meta":
		return decodeSessionMeta(frame, observedAtMS, envelope.Payload)
	case "turn_context":
		return decodeTurnContext(frame, observedAtMS, envelope.Payload)
	case "event_msg":
		return decodeEvent(frame, observedAtMS, envelope.Payload)
	case "response_item", "inter_agent_communication", "compacted":
		return decodeResult{KnownIgnored: true}
	default:
		return decodeDiagnostic(frame, DiagnosticClassCompatibility, DiagnosticUnknownRolloutType)
	}
}

func decodeSessionMeta(frame lineFrame, observedAtMS int64, payload json.RawMessage) decodeResult {
	var value struct {
		SessionID     json.RawMessage `json:"session_id"`
		ID            json.RawMessage `json:"id"`
		Timestamp     json.RawMessage `json:"timestamp"`
		CWD           json.RawMessage `json:"cwd"`
		Originator    json.RawMessage `json:"originator"`
		CLIVersion    json.RawMessage `json:"cli_version"`
		Source        json.RawMessage `json:"source"`
		ModelProvider json.RawMessage `json:"model_provider"`
	}
	if err := json.Unmarshal(payload, &value); err != nil {
		return decodeDiagnostic(frame, DiagnosticClassSyntax, DiagnosticInvalidField)
	}

	sessionID, sessionOK := decodeRequiredString(value.ID, maxIdentifierBytes)
	createdAtMS, timestampOK := decodeTimestamp(value.Timestamp)
	cwd, cwdOK := decodeRequiredString(value.CWD, maxMetadataBytes)
	originator, originatorOK := decodeRequiredString(value.Originator, maxIdentifierBytes)
	cliVersion, cliOK := decodeRequiredString(value.CLIVersion, maxIdentifierBytes)
	source, sourceOK := decodeSessionSource(value.Source)
	modelProvider, modelOK := decodeOptionalStringValue(value.ModelProvider, maxIdentifierBytes)
	if !sessionOK || !timestampOK || !cwdOK || !originatorOK || !cliOK || !sourceOK || !modelOK {
		return decodeDiagnostic(frame, DiagnosticClassSyntax, DiagnosticInvalidField)
	}

	rootSessionID := sessionID
	if len(value.SessionID) != 0 && string(value.SessionID) != "null" {
		var rootOK bool
		rootSessionID, rootOK = decodeRequiredString(value.SessionID, maxIdentifierBytes)
		if !rootOK {
			return decodeDiagnostic(frame, DiagnosticClassSyntax, DiagnosticInvalidField)
		}
	}

	return decodeResult{Record: &decodedLine{
		Kind:         decodedSessionMeta,
		ObservedAtMS: observedAtMS,
		SessionMeta: &decodedSessionMetaRecord{
			SessionID: sessionID, RootSessionID: rootSessionID, CreatedAtMS: createdAtMS,
			InitialCWD: cwd, Originator: originator, CLIVersion: cliVersion,
			Source: source, ModelProvider: modelProvider,
		},
	}}
}

func decodeTurnContext(frame lineFrame, observedAtMS int64, payload json.RawMessage) decodeResult {
	var value struct {
		TurnID json.RawMessage `json:"turn_id"`
		CWD    json.RawMessage `json:"cwd"`
		Model  json.RawMessage `json:"model"`
		Effort json.RawMessage `json:"effort"`
	}
	if err := json.Unmarshal(payload, &value); err != nil {
		return decodeDiagnostic(frame, DiagnosticClassSyntax, DiagnosticInvalidField)
	}
	cwd, cwdOK := decodeRequiredString(value.CWD, maxMetadataBytes)
	model, modelOK := decodeRequiredString(value.Model, maxIdentifierBytes)
	turnID, turnOK := decodeOptionalString(value.TurnID, maxIdentifierBytes)
	effort, effortOK := decodeEffort(value.Effort)
	if !cwdOK || !modelOK || !turnOK || !effortOK {
		return decodeDiagnostic(frame, DiagnosticClassSyntax, DiagnosticInvalidField)
	}
	return decodeResult{Record: &decodedLine{
		Kind: decodedTurnContext, ObservedAtMS: observedAtMS,
		TurnContext: &decodedTurnContextRecord{TurnID: turnID, CWD: cwd, Model: model, Effort: effort},
	}}
}

func decodeEvent(frame lineFrame, observedAtMS int64, payload json.RawMessage) decodeResult {
	var envelope struct {
		Type json.RawMessage `json:"type"`
	}
	if err := json.Unmarshal(payload, &envelope); err != nil {
		return decodeDiagnostic(frame, DiagnosticClassSyntax, DiagnosticInvalidField)
	}
	eventType, ok := decodeRequiredString(envelope.Type, maxIdentifierBytes)
	if !ok {
		return decodeDiagnostic(frame, DiagnosticClassSyntax, DiagnosticInvalidField)
	}

	switch eventType {
	case "task_started", "turn_started":
		return decodeTurnStarted(frame, observedAtMS, payload)
	case "task_complete", "turn_complete":
		return decodeTurnComplete(frame, observedAtMS, payload)
	case "turn_aborted":
		return decodeTurnAborted(frame, observedAtMS, payload)
	case "token_count":
		return decodeTokenCount(frame, observedAtMS, payload)
	default:
		if knownIgnoredEventTypes[eventType] {
			return decodeResult{KnownIgnored: true}
		}
		return decodeDiagnostic(frame, DiagnosticClassCompatibility, DiagnosticUnknownEventType)
	}
}

func decodeTurnStarted(frame lineFrame, observedAtMS int64, payload json.RawMessage) decodeResult {
	var value struct {
		TurnID             json.RawMessage `json:"turn_id"`
		StartedAt          *int64          `json:"started_at"`
		ModelContextWindow *int64          `json:"model_context_window"`
	}
	if err := json.Unmarshal(payload, &value); err != nil {
		return decodeDiagnostic(frame, DiagnosticClassSyntax, DiagnosticInvalidField)
	}
	turnID, ok := decodeRequiredString(value.TurnID, maxIdentifierBytes)
	if !ok || !validNonNegative(value.ModelContextWindow) {
		return decodeDiagnostic(frame, DiagnosticClassSyntax, DiagnosticInvalidField)
	}
	startedAtMS := observedAtMS
	if value.StartedAt != nil {
		var timeOK bool
		startedAtMS, timeOK = secondsToMilliseconds(*value.StartedAt)
		if !timeOK {
			return decodeDiagnostic(frame, DiagnosticClassSyntax, DiagnosticInvalidTimestamp)
		}
	}
	return decodeResult{Record: &decodedLine{
		Kind: decodedTurnStart, ObservedAtMS: observedAtMS,
		TurnStart: &decodedTurnStartRecord{
			TurnID: turnID, StartedAtMS: startedAtMS, ContextWindow: cloneInt64(value.ModelContextWindow),
		},
	}}
}

func decodeTurnComplete(frame lineFrame, observedAtMS int64, payload json.RawMessage) decodeResult {
	var value struct {
		TurnID      json.RawMessage `json:"turn_id"`
		CompletedAt *int64          `json:"completed_at"`
	}
	if err := json.Unmarshal(payload, &value); err != nil {
		return decodeDiagnostic(frame, DiagnosticClassSyntax, DiagnosticInvalidField)
	}
	turnID, ok := decodeRequiredString(value.TurnID, maxIdentifierBytes)
	if !ok {
		return decodeDiagnostic(frame, DiagnosticClassSyntax, DiagnosticInvalidField)
	}
	completedAtMS, ok := optionalSecondsOrObserved(value.CompletedAt, observedAtMS)
	if !ok {
		return decodeDiagnostic(frame, DiagnosticClassSyntax, DiagnosticInvalidTimestamp)
	}
	return decodeResult{Record: &decodedLine{
		Kind: decodedTurnTerminal, ObservedAtMS: observedAtMS,
		TurnTerminal: &decodedTurnTerminalRecord{
			TurnID: stringPointer(turnID), CompletedAtMS: completedAtMS, Outcome: TurnOutcomeCompleted,
		},
	}}
}

func decodeTurnAborted(frame lineFrame, observedAtMS int64, payload json.RawMessage) decodeResult {
	var value struct {
		TurnID      json.RawMessage `json:"turn_id"`
		Reason      json.RawMessage `json:"reason"`
		CompletedAt *int64          `json:"completed_at"`
	}
	if err := json.Unmarshal(payload, &value); err != nil {
		return decodeDiagnostic(frame, DiagnosticClassSyntax, DiagnosticInvalidField)
	}
	turnID, turnOK := decodeOptionalString(value.TurnID, maxIdentifierBytes)
	reason, reasonOK := decodeRequiredString(value.Reason, maxIdentifierBytes)
	outcome, outcomeOK := abortOutcome(reason)
	completedAtMS, timeOK := optionalSecondsOrObserved(value.CompletedAt, observedAtMS)
	if !turnOK || !reasonOK || !outcomeOK || !timeOK {
		return decodeDiagnostic(frame, DiagnosticClassSyntax, DiagnosticInvalidField)
	}
	return decodeResult{Record: &decodedLine{
		Kind: decodedTurnTerminal, ObservedAtMS: observedAtMS,
		TurnTerminal: &decodedTurnTerminalRecord{
			TurnID: turnID, CompletedAtMS: completedAtMS, Outcome: outcome,
		},
	}}
}

func decodeTokenCount(frame lineFrame, observedAtMS int64, payload json.RawMessage) decodeResult {
	var value struct {
		Info *struct {
			Total              *tokenUsagePayload `json:"total_token_usage"`
			Last               *tokenUsagePayload `json:"last_token_usage"`
			ModelContextWindow *int64             `json:"model_context_window"`
		} `json:"info"`
	}
	if err := json.Unmarshal(payload, &value); err != nil {
		return decodeDiagnostic(frame, DiagnosticClassSyntax, DiagnosticInvalidField)
	}
	if value.Info == nil {
		return decodeResult{KnownIgnored: true}
	}
	if !validNonNegative(value.Info.ModelContextWindow) ||
		!validTokenPayload(value.Info.Total) || !validTokenPayload(value.Info.Last) {
		return decodeDiagnostic(frame, DiagnosticClassSyntax, DiagnosticInvalidField)
	}
	return decodeResult{Record: &decodedLine{
		Kind: decodedTokenUsage, ObservedAtMS: observedAtMS,
		TokenUsage: &decodedTokenUsageRecord{
			ObservedAtMS: observedAtMS,
			Total:        tokenCounters(value.Info.Total), Last: tokenCounters(value.Info.Last),
			ContextWindow: cloneInt64(value.Info.ModelContextWindow),
		},
	}}
}

func validateJSON(content []byte) error {
	decoder := json.NewDecoder(strings.NewReader(string(content)))
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
				return errDuplicateJSONKey
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

func decodeTimestamp(raw json.RawMessage) (int64, bool) {
	value, ok := decodeRequiredString(raw, 128)
	if !ok {
		return 0, false
	}
	parsed, err := time.Parse(time.RFC3339Nano, value)
	if err != nil || parsed.UnixMilli() < 0 {
		return 0, false
	}
	return parsed.UnixMilli(), true
}

func decodeRequiredString(raw json.RawMessage, maxBytes int) (string, bool) {
	if len(raw) == 0 || string(raw) == "null" {
		return "", false
	}
	var value string
	if err := json.Unmarshal(raw, &value); err != nil || value == "" || len(value) > maxBytes {
		return "", false
	}
	return value, true
}

func decodeOptionalString(raw json.RawMessage, maxBytes int) (*string, bool) {
	if len(raw) == 0 || string(raw) == "null" {
		return nil, true
	}
	value, ok := decodeRequiredString(raw, maxBytes)
	if !ok {
		return nil, false
	}
	return stringPointer(value), true
}

func decodeOptionalStringValue(raw json.RawMessage, maxBytes int) (string, bool) {
	value, ok := decodeOptionalString(raw, maxBytes)
	if !ok || value == nil {
		return "", ok
	}
	return *value, true
}

func decodeSessionSource(raw json.RawMessage) (string, bool) {
	if len(raw) == 0 || string(raw) == "null" {
		return "vscode", true
	}
	var scalar string
	if err := json.Unmarshal(raw, &scalar); err == nil {
		switch scalar {
		case "cli", "vscode", "exec", "mcp", "unknown":
			return scalar, true
		default:
			return "unknown", true
		}
	}
	var tagged map[string]json.RawMessage
	if err := json.Unmarshal(raw, &tagged); err != nil || len(tagged) != 1 {
		return "", false
	}
	for _, key := range []string{"custom", "internal", "subagent"} {
		if _, ok := tagged[key]; ok {
			return key, true
		}
	}
	return "unknown", true
}

func decodeEffort(raw json.RawMessage) (*string, bool) {
	value, ok := decodeOptionalString(raw, maxIdentifierBytes)
	if !ok || value == nil {
		return value, ok
	}
	switch *value {
	case "none", "minimal", "low", "medium", "high", "xhigh", "ultra":
		return value, true
	default:
		return stringPointer("custom"), true
	}
}

func secondsToMilliseconds(value int64) (int64, bool) {
	if value < 0 || value > math.MaxInt64/1000 {
		return 0, false
	}
	return value * 1000, true
}

func optionalSecondsOrObserved(value *int64, observedAtMS int64) (int64, bool) {
	if value == nil {
		return observedAtMS, true
	}
	return secondsToMilliseconds(*value)
}

func validNonNegative(value *int64) bool {
	return value == nil || *value >= 0
}

func validTokenPayload(value *tokenUsagePayload) bool {
	if value == nil {
		return true
	}
	for _, token := range []*int64{
		value.InputTokens, value.CachedInputTokens, value.OutputTokens,
		value.ReasoningOutputTokens, value.TotalTokens,
	} {
		if token != nil && *token < 0 {
			return false
		}
	}
	return true
}

func tokenCounters(value *tokenUsagePayload) TokenCounters {
	if value == nil {
		return TokenCounters{}
	}
	return TokenCounters{
		InputTokens: cloneInt64(value.InputTokens), CachedInputTokens: cloneInt64(value.CachedInputTokens),
		OutputTokens: cloneInt64(value.OutputTokens), ReasoningTokens: cloneInt64(value.ReasoningOutputTokens),
	}
}

func abortOutcome(reason string) (TurnOutcome, bool) {
	switch reason {
	case "interrupted":
		return TurnOutcomeInterrupted, true
	case "replaced":
		return TurnOutcomeReplaced, true
	case "review_ended":
		return TurnOutcomeReviewEnded, true
	case "budget_limited":
		return TurnOutcomeBudgetLimited, true
	default:
		return "", false
	}
}

func cloneInt64(value *int64) *int64 {
	if value == nil {
		return nil
	}
	cloned := *value
	return &cloned
}

func stringPointer(value string) *string {
	cloned := value
	return &cloned
}

func decodeDiagnostic(frame lineFrame, class DiagnosticClass, code DiagnosticCode) decodeResult {
	return decodeResult{Diagnostic: &ParserDiagnostic{
		Class: class, Code: code, StartOffset: frame.StartOffset, EndOffset: frame.EndOffset,
	}}
}

var knownIgnoredEventTypes = map[string]bool{
	"user_message": true, "agent_message": true, "agent_reasoning": true,
	"agent_reasoning_raw_content": true, "patch_apply_end": true,
	"thread_goal_updated": true, "context_compacted": true,
	"entered_review_mode": true, "exited_review_mode": true,
	"mcp_tool_call_end": true, "thread_rolled_back": true,
	"web_search_end": true, "image_generation_end": true,
	"sub_agent_activity": true, "item_completed": true,
}
