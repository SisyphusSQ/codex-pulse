package logs

import (
	"sort"
	"unicode/utf8"
)

type StreamParser struct {
	framer    *lineFramer
	lifecycle *lifecycleNormalizer
}

func NewStreamParser(config ParserConfig) (*StreamParser, error) {
	switch config.SourceKind {
	case SourceKindSession, SourceKindArchivedSession:
	case SourceKindSessionIndex:
		return nil, ErrUnsupportedParserSource
	default:
		return nil, ErrUnsupportedParserSource
	}
	if config.StartOffset < 0 || config.MaxLineBytes < 0 || config.MaxLineBytes > MaxSupportedLineBytes {
		return nil, ErrInvalidParserConfig
	}
	if !validParserSeed(config) {
		return nil, ErrInvalidParserSeed
	}
	maxLineBytes := config.MaxLineBytes
	if maxLineBytes == 0 {
		maxLineBytes = DefaultMaxLineBytes
	}
	framer, err := newLineFramer(config.StartOffset, maxLineBytes)
	if err != nil {
		return nil, err
	}
	lifecycle := newLifecycleNormalizer(config.SourceKind)
	lifecycle.hydrate(config.Seed)
	return &StreamParser{framer: framer, lifecycle: lifecycle}, nil
}

func validParserSeed(config ParserConfig) bool {
	if config.StartOffset == 0 {
		return config.Seed == nil
	}
	seed := config.Seed
	if seed == nil || len(seed.OpenTurns) > maxOpenTurnStates ||
		len(seed.PendingTurns) > maxPendingTurnStates || len(seed.ClosedTurns) > maxRetainedClosedTurnStates {
		return false
	}
	if seed.Session == nil {
		return len(seed.OpenTurns) == 0 && len(seed.PendingTurns) == 0 && len(seed.ClosedTurns) == 0
	}
	if !validSeedSession(seed.Session, config.SourceKind) {
		return false
	}
	sessionID := seed.Session.SessionID
	seen := make(map[string]struct{}, len(seed.OpenTurns)+len(seed.PendingTurns)+len(seed.ClosedTurns))
	for _, open := range seed.OpenTurns {
		if !validSeedIdentifier(open.TurnID) || open.StartedAtMS < 0 || !validNonNegative(open.ContextWindow) ||
			!validSeedContext(open.Context, sessionID, open.TurnID, open.StartedAtMS) {
			return false
		}
		if _, exists := seen[open.TurnID]; exists {
			return false
		}
		seen[open.TurnID] = struct{}{}
		usage := open.LatestUsage
		if usage == nil {
			continue
		}
		if usage.SessionID != sessionID || usage.TurnID != open.TurnID || usage.ObservedAtMS < open.StartedAtMS ||
			usage.IsFinal || !validNonNegative(usage.ContextWindow) || !validTokenCounters(usage.Usage) {
			return false
		}
	}
	for _, pending := range seed.PendingTurns {
		if !validSeedIdentifier(pending.TurnID) || (pending.Context == nil && pending.Terminal == nil) {
			return false
		}
		if _, exists := seen[pending.TurnID]; exists {
			return false
		}
		seen[pending.TurnID] = struct{}{}
		if !validPendingContextSeed(pending.Context, config.StartOffset) ||
			!validPendingTerminalSeed(pending.Terminal, config.StartOffset) {
			return false
		}
		if pending.Context != nil && pending.Terminal != nil &&
			pending.Context.Position.EndOffset > pending.Terminal.Position.StartOffset {
			return false
		}
	}
	for _, closed := range seed.ClosedTurns {
		if !validSeedIdentifier(closed.TurnID) || closed.StartedAtMS < 0 || !validNonNegative(closed.ContextWindow) {
			return false
		}
		if _, exists := seen[closed.TurnID]; exists {
			return false
		}
		seen[closed.TurnID] = struct{}{}
		terminal := &closed.Terminal
		if terminal.SessionID != sessionID || terminal.TurnID != closed.TurnID ||
			terminal.CompletedAtMS < closed.StartedAtMS || !validTurnOutcome(terminal.Outcome) ||
			!validFinalUsage(terminal.FinalUsage, sessionID, closed.TurnID, closed.StartedAtMS) {
			return false
		}
	}
	return true
}

func validSeedSession(session *SessionMetaFact, sourceKind SourceKind) bool {
	return session != nil && session.SourceKind == sourceKind &&
		validSeedIdentifier(session.SessionID) && validSeedIdentifier(session.RootSessionID) &&
		session.CreatedAtMS >= 0 && session.ObservedAtMS >= 0 &&
		validSeedString(session.InitialCWD, maxMetadataBytes, true) &&
		validSeedString(session.Originator, maxIdentifierBytes, true) &&
		validSeedString(session.CLIVersion, maxIdentifierBytes, true) &&
		validSeedString(session.Source, maxIdentifierBytes, true) &&
		validSeedString(session.ModelProvider, maxIdentifierBytes, false)
}

func validSeedContext(context *TurnContextFact, sessionID, turnID string, startedAtMS int64) bool {
	if context == nil {
		return true
	}
	return context.SessionID == sessionID && context.TurnID == turnID && context.ObservedAtMS >= startedAtMS &&
		validSeedString(context.CWD, maxMetadataBytes, true) &&
		validSeedString(context.Model, maxIdentifierBytes, true) && validSeedEffort(context.Effort)
}

func validPendingContextSeed(context *PendingTurnContextSeed, checkpointOffset int64) bool {
	if context == nil {
		return true
	}
	return validCheckpointPosition(context.Position, checkpointOffset) && context.ObservedAtMS >= 0 &&
		validSeedString(context.CWD, maxMetadataBytes, true) &&
		validSeedString(context.Model, maxIdentifierBytes, true) && validSeedEffort(context.Effort)
}

func validPendingTerminalSeed(terminal *PendingTurnTerminalSeed, checkpointOffset int64) bool {
	return terminal == nil || (validCheckpointPosition(terminal.Position, checkpointOffset) &&
		terminal.CompletedAtMS >= 0 && validTurnOutcome(terminal.Outcome))
}

func validCheckpointPosition(position SourcePosition, checkpointOffset int64) bool {
	return position.StartOffset >= 0 && position.EndOffset > position.StartOffset &&
		position.EndOffset <= checkpointOffset
}

func validSeedEffort(effort *string) bool {
	if effort == nil {
		return true
	}
	switch *effort {
	case "none", "minimal", "low", "medium", "high", "xhigh", "ultra", "custom":
		return true
	default:
		return false
	}
}

func validTurnOutcome(outcome TurnOutcome) bool {
	switch outcome {
	case TurnOutcomeCompleted, TurnOutcomeInterrupted, TurnOutcomeReplaced,
		TurnOutcomeReviewEnded, TurnOutcomeBudgetLimited:
		return true
	default:
		return false
	}
}

func validFinalUsage(usage *TurnUsageFact, sessionID, turnID string, startedAtMS int64) bool {
	return usage == nil || (usage.SessionID == sessionID && usage.TurnID == turnID && usage.IsFinal &&
		usage.ObservedAtMS >= startedAtMS && validNonNegative(usage.ContextWindow) && validTokenCounters(usage.Usage))
}

func validSeedIdentifier(value string) bool {
	return value != "" && len(value) <= maxIdentifierBytes && utf8.ValidString(value)
}

func validSeedString(value string, maxBytes int, required bool) bool {
	return (!required || value != "") && len(value) <= maxBytes && utf8.ValidString(value)
}

func validTokenCounters(value TokenCounters) bool {
	return validNonNegative(value.InputTokens) && validNonNegative(value.CachedInputTokens) &&
		validNonNegative(value.OutputTokens) && validNonNegative(value.ReasoningTokens)
}

type parserInputItem struct {
	frame      *lineFrame
	diagnostic *ParserDiagnostic
}

func (item parserInputItem) position() SourcePosition {
	if item.frame != nil {
		return SourcePosition{StartOffset: item.frame.StartOffset, EndOffset: item.frame.EndOffset}
	}
	return SourcePosition{StartOffset: item.diagnostic.StartOffset, EndOffset: item.diagnostic.EndOffset}
}

func (parser *StreamParser) Feed(startOffset int64, chunk []byte) (ParseResult, error) {
	if parser == nil || parser.framer == nil || parser.lifecycle == nil {
		return ParseResult{}, ErrInvalidParserConfig
	}
	framed, err := parser.framer.Feed(startOffset, chunk)
	if err != nil {
		return ParseResult{}, err
	}
	result := ParseResult{
		ReadOffset: framed.ReadOffset, CommittableOffset: framed.CommittableOffset,
		BufferedBytes: framed.BufferedBytes,
	}
	result.Stats.CompleteLines = uint64(len(framed.Lines) + len(framed.Diagnostics))
	items := make([]parserInputItem, 0, len(framed.Lines)+len(framed.Diagnostics))
	for index := range framed.Lines {
		items = append(items, parserInputItem{frame: &framed.Lines[index]})
	}
	for index := range framed.Diagnostics {
		items = append(items, parserInputItem{diagnostic: &framed.Diagnostics[index]})
	}
	sort.SliceStable(items, func(left, right int) bool {
		leftPosition, rightPosition := items[left].position(), items[right].position()
		if leftPosition.StartOffset != rightPosition.StartOffset {
			return leftPosition.StartOffset < rightPosition.StartOffset
		}
		return leftPosition.EndOffset < rightPosition.EndOffset
	})

	for _, item := range items {
		if item.diagnostic != nil {
			result.Diagnostics = append(result.Diagnostics, *item.diagnostic)
			result.Stats.DiagnosticLines++
			continue
		}
		frame := *item.frame
		decoded := decodeRolloutLine(frame)
		switch {
		case decoded.Diagnostic != nil:
			result.Diagnostics = append(result.Diagnostics, *decoded.Diagnostic)
			result.Stats.DiagnosticLines++
		case decoded.KnownIgnored:
			result.Stats.KnownIgnoredLines++
		case decoded.Record != nil:
			result.Stats.ParsedLines++
			normalized := parser.lifecycle.Apply(
				SourcePosition{StartOffset: frame.StartOffset, EndOffset: frame.EndOffset},
				decoded.Record,
			)
			result.Events = append(result.Events, normalized.Events...)
			result.Diagnostics = append(result.Diagnostics, normalized.Diagnostics...)
			result.Stats.DiagnosticLines += uint64(len(normalized.Diagnostics))
		}
	}
	result.Stats.EventsEmitted = uint64(len(result.Events))
	result.NextSeed = parser.lifecycle.checkpoint()
	return result, nil
}
