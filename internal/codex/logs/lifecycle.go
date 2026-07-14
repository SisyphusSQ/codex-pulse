package logs

import "sort"

const (
	maxOpenTurnStates           = 64
	maxPendingTurnStates        = 1024
	maxRetainedClosedTurnStates = 1024
)

type lifecycleResult struct {
	Events      []ParsedEvent
	Diagnostics []ParserDiagnostic
}

type positionedTurnContext struct {
	position     SourcePosition
	observedAtMS int64
	record       decodedTurnContextRecord
}

type positionedTurnTerminal struct {
	position SourcePosition
	record   decodedTurnTerminalRecord
}

type pendingTurnState struct {
	context  *positionedTurnContext
	terminal *positionedTurnTerminal
}

type turnLifecycleState struct {
	start       TurnStartFact
	context     *TurnContextFact
	latestUsage *TurnUsageFact
	terminal    *TurnEndFact
}

type lifecycleNormalizer struct {
	sourceKind  SourceKind
	session     *SessionMetaFact
	turns       map[string]*turnLifecycleState
	pending     map[string]*pendingTurnState
	closedOrder []string
}

func (normalizer *lifecycleNormalizer) hydrate(seed *ParserSeed) {
	if seed == nil {
		return
	}
	normalizer.session = cloneSessionFact(seed.Session)
	for _, open := range seed.OpenTurns {
		normalizer.turns[open.TurnID] = &turnLifecycleState{
			start: TurnStartFact{
				SessionID: seed.Session.SessionID, TurnID: open.TurnID,
				StartedAtMS: open.StartedAtMS, ContextWindow: cloneInt64(open.ContextWindow),
			},
			context:     cloneTurnContextFact(open.Context),
			latestUsage: cloneTurnUsageFact(open.LatestUsage),
		}
	}
	for _, pending := range seed.PendingTurns {
		state := &pendingTurnState{}
		if pending.Context != nil {
			state.context = &positionedTurnContext{
				position: pending.Context.Position, observedAtMS: pending.Context.ObservedAtMS,
				record: decodedTurnContextRecord{
					TurnID: stringPointer(pending.TurnID), CWD: pending.Context.CWD,
					Model: pending.Context.Model, Effort: cloneString(pending.Context.Effort),
				},
			}
		}
		if pending.Terminal != nil {
			state.terminal = &positionedTurnTerminal{
				position: pending.Terminal.Position,
				record: decodedTurnTerminalRecord{
					TurnID: stringPointer(pending.TurnID), CompletedAtMS: pending.Terminal.CompletedAtMS,
					Outcome: pending.Terminal.Outcome,
				},
			}
		}
		normalizer.pending[pending.TurnID] = state
	}
	for _, closed := range seed.ClosedTurns {
		normalizer.turns[closed.TurnID] = &turnLifecycleState{
			start: TurnStartFact{
				SessionID: seed.Session.SessionID, TurnID: closed.TurnID,
				StartedAtMS: closed.StartedAtMS, ContextWindow: cloneInt64(closed.ContextWindow),
			},
			terminal: cloneTurnEndFact(&closed.Terminal),
		}
		normalizer.closedOrder = append(normalizer.closedOrder, closed.TurnID)
	}
}

func (normalizer *lifecycleNormalizer) checkpoint() *ParserSeed {
	checkpoint := &ParserSeed{Session: cloneSessionFact(normalizer.session)}
	openTurnIDs := normalizer.openTurnIDs()
	sort.Strings(openTurnIDs)
	for _, turnID := range openTurnIDs {
		state := normalizer.turns[turnID]
		checkpoint.OpenTurns = append(checkpoint.OpenTurns, OpenTurnSeed{
			TurnID: turnID, StartedAtMS: state.start.StartedAtMS,
			ContextWindow: cloneInt64(state.start.ContextWindow),
			Context:       cloneTurnContextFact(state.context), LatestUsage: cloneTurnUsageFact(state.latestUsage),
		})
	}
	pendingTurnIDs := make([]string, 0, len(normalizer.pending))
	for turnID := range normalizer.pending {
		pendingTurnIDs = append(pendingTurnIDs, turnID)
	}
	sort.Strings(pendingTurnIDs)
	for _, turnID := range pendingTurnIDs {
		state := normalizer.pending[turnID]
		seed := PendingTurnSeed{TurnID: turnID}
		if state.context != nil {
			seed.Context = &PendingTurnContextSeed{
				Position: state.context.position, ObservedAtMS: state.context.observedAtMS,
				CWD: state.context.record.CWD, Model: state.context.record.Model,
				Effort: cloneString(state.context.record.Effort),
			}
		}
		if state.terminal != nil {
			seed.Terminal = &PendingTurnTerminalSeed{
				Position: state.terminal.position, CompletedAtMS: state.terminal.record.CompletedAtMS,
				Outcome: state.terminal.record.Outcome,
			}
		}
		checkpoint.PendingTurns = append(checkpoint.PendingTurns, seed)
	}
	for _, turnID := range normalizer.closedOrder {
		state := normalizer.turns[turnID]
		if state == nil || state.terminal == nil {
			continue
		}
		checkpoint.ClosedTurns = append(checkpoint.ClosedTurns, ClosedTurnSeed{
			TurnID: turnID, StartedAtMS: state.start.StartedAtMS,
			ContextWindow: cloneInt64(state.start.ContextWindow), Terminal: *cloneTurnEndFact(state.terminal),
		})
	}
	return checkpoint
}

func newLifecycleNormalizer(sourceKinds ...SourceKind) *lifecycleNormalizer {
	sourceKind := SourceKindSession
	if len(sourceKinds) > 0 {
		sourceKind = sourceKinds[0]
	}
	return &lifecycleNormalizer{
		sourceKind: sourceKind,
		turns:      make(map[string]*turnLifecycleState),
		pending:    make(map[string]*pendingTurnState),
	}
}

func (normalizer *lifecycleNormalizer) Apply(position SourcePosition, record *decodedLine) lifecycleResult {
	if normalizer == nil || record == nil || position.StartOffset < 0 || position.EndOffset <= position.StartOffset {
		return lifecycleDiagnostic(position, DiagnosticInvalidTransition)
	}
	if record.Kind == decodedSessionMeta {
		return normalizer.applySession(position, record)
	}
	if normalizer.session == nil {
		return lifecycleDiagnostic(position, DiagnosticMissingSessionMeta)
	}

	switch record.Kind {
	case decodedTurnStart:
		return normalizer.applyTurnStart(position, record.TurnStart)
	case decodedTurnContext:
		return normalizer.applyTurnContext(position, record.ObservedAtMS, record.TurnContext)
	case decodedTokenUsage:
		return normalizer.applyTokenUsage(position, record.TokenUsage)
	case decodedTurnTerminal:
		return normalizer.applyTurnTerminal(position, record.TurnTerminal, true)
	default:
		return lifecycleDiagnostic(position, DiagnosticInvalidTransition)
	}
}

func (normalizer *lifecycleNormalizer) applySession(position SourcePosition, record *decodedLine) lifecycleResult {
	if record.SessionMeta == nil {
		return lifecycleDiagnostic(position, DiagnosticInvalidTransition)
	}
	meta := sessionFact(record, normalizer.sourceKind)
	if normalizer.session != nil {
		if equalSessionFact(normalizer.session, &meta) {
			return lifecycleResult{}
		}
		return lifecycleDiagnostic(position, DiagnosticInvalidTransition)
	}
	normalizer.session = &meta
	return lifecycleResult{Events: []ParsedEvent{{
		Kind: EventSessionMeta, Position: position, SessionMeta: cloneSessionFact(&meta),
	}}}
}

func (normalizer *lifecycleNormalizer) applyTurnStart(
	position SourcePosition,
	record *decodedTurnStartRecord,
) lifecycleResult {
	if record == nil {
		return lifecycleDiagnostic(position, DiagnosticInvalidTransition)
	}
	fact := TurnStartFact{
		SessionID: normalizer.session.SessionID, TurnID: record.TurnID,
		StartedAtMS: record.StartedAtMS, ContextWindow: cloneInt64(record.ContextWindow),
	}
	if current, exists := normalizer.turns[record.TurnID]; exists {
		if equalTurnStartFact(&current.start, &fact) {
			return lifecycleResult{}
		}
		return lifecycleDiagnostic(position, DiagnosticInvalidTransition)
	}
	if len(normalizer.openTurnIDs()) >= maxOpenTurnStates {
		// A previously pending record for this turn can never be resolved while
		// the start is rejected. Drop that bounded safe state so the durable
		// offset is not pinned forever at its earlier source position.
		delete(normalizer.pending, record.TurnID)
		return lifecycleDiagnostic(position, DiagnosticStateLimitExceeded)
	}

	state := &turnLifecycleState{start: fact}
	normalizer.turns[record.TurnID] = state
	result := lifecycleResult{Events: []ParsedEvent{{
		Kind: EventTurnStarted, Position: position, TurnStart: cloneTurnStartFact(&fact),
	}}}

	pending := normalizer.pending[record.TurnID]
	if pending == nil {
		return result
	}
	delete(normalizer.pending, record.TurnID)
	if pending.context != nil {
		resolved := normalizer.applyKnownTurnContext(
			pending.context.position,
			pending.context.observedAtMS,
			pending.context.record,
			record.TurnID,
			state,
		)
		result.append(resolved)
	}
	if pending.terminal != nil {
		terminalRecord := pending.terminal.record
		resolved := normalizer.applyTurnTerminal(pending.terminal.position, &terminalRecord, false)
		result.append(resolved)
	}
	return result
}

func (normalizer *lifecycleNormalizer) applyTurnContext(
	position SourcePosition,
	observedAtMS int64,
	record *decodedTurnContextRecord,
) lifecycleResult {
	if record == nil {
		return lifecycleDiagnostic(position, DiagnosticInvalidTransition)
	}
	turnID, resolution := normalizer.resolveTurnID(record.TurnID)
	if resolution != "" {
		return lifecycleDiagnostic(position, resolution)
	}
	if turnID == "" {
		return lifecycleDiagnostic(position, DiagnosticMissingTurnStart)
	}
	state, exists := normalizer.turns[turnID]
	if !exists {
		pending, ok := normalizer.pendingState(turnID)
		if !ok {
			return lifecycleDiagnostic(position, DiagnosticStateLimitExceeded)
		}
		if pending.terminal != nil {
			return lifecycleDiagnostic(position, DiagnosticInvalidTransition)
		}
		candidate := positionedTurnContext{
			position: position, observedAtMS: observedAtMS,
			record: decodedTurnContextRecord{
				TurnID: stringPointer(turnID), CWD: record.CWD, Model: record.Model,
				Effort: cloneString(record.Effort),
			},
		}
		if pending.context != nil {
			if equalDecodedTurnContext(&pending.context.record, &candidate.record) {
				return lifecycleResult{}
			}
			return lifecycleDiagnostic(position, DiagnosticInvalidTransition)
		}
		pending.context = &candidate
		return lifecycleDiagnostic(position, DiagnosticMissingTurnStart)
	}
	return normalizer.applyKnownTurnContext(position, observedAtMS, *record, turnID, state)
}

func (normalizer *lifecycleNormalizer) applyKnownTurnContext(
	position SourcePosition,
	observedAtMS int64,
	record decodedTurnContextRecord,
	turnID string,
	state *turnLifecycleState,
) lifecycleResult {
	if state.terminal != nil {
		return lifecycleDiagnostic(position, DiagnosticInvalidTransition)
	}
	if observedAtMS < state.start.StartedAtMS {
		return lifecycleDiagnostic(position, DiagnosticInvalidTransition)
	}
	fact := TurnContextFact{
		SessionID: normalizer.session.SessionID, TurnID: turnID,
		ObservedAtMS: observedAtMS, CWD: record.CWD,
		Model: record.Model, Effort: cloneString(record.Effort),
	}
	if state.context != nil && equalTurnContextFact(state.context, &fact) {
		return lifecycleResult{}
	}
	state.context = cloneTurnContextFact(&fact)
	return lifecycleResult{Events: []ParsedEvent{{
		Kind: EventTurnContext, Position: position, TurnContext: cloneTurnContextFact(&fact),
	}}}
}

func (normalizer *lifecycleNormalizer) applyTokenUsage(
	position SourcePosition,
	record *decodedTokenUsageRecord,
) lifecycleResult {
	if record == nil {
		return lifecycleDiagnostic(position, DiagnosticInvalidTransition)
	}
	result := lifecycleResult{Events: []ParsedEvent{{
		Kind: EventSessionUsage, Position: position,
		SessionUsage: &SessionUsageFact{
			SessionID: normalizer.session.SessionID, ObservedAtMS: record.ObservedAtMS,
			Usage: cloneTokenCounters(record.Total), ContextWindow: cloneInt64(record.ContextWindow),
		},
	}}}
	openTurnIDs := normalizer.openTurnIDs()
	if len(openTurnIDs) == 0 {
		result.Diagnostics = append(result.Diagnostics, newLifecycleDiagnostic(position, DiagnosticOrphanTurnUsage))
		return result
	}
	if len(openTurnIDs) > 1 {
		result.Diagnostics = append(result.Diagnostics, newLifecycleDiagnostic(position, DiagnosticAmbiguousTurn))
		return result
	}

	turnID := openTurnIDs[0]
	if record.ObservedAtMS < normalizer.turns[turnID].start.StartedAtMS {
		result.Diagnostics = append(result.Diagnostics, newLifecycleDiagnostic(position, DiagnosticInvalidTransition))
		return result
	}
	fact := &TurnUsageFact{
		SessionID: normalizer.session.SessionID, TurnID: turnID,
		ObservedAtMS: record.ObservedAtMS, Usage: cloneTokenCounters(record.Last),
		ContextWindow: cloneInt64(record.ContextWindow),
	}
	normalizer.turns[turnID].latestUsage = cloneTurnUsageFact(fact)
	result.Events = append(result.Events, ParsedEvent{
		Kind: EventTurnUsage, Position: position, TurnUsage: cloneTurnUsageFact(fact),
	})
	return result
}

func (normalizer *lifecycleNormalizer) applyTurnTerminal(
	position SourcePosition,
	record *decodedTurnTerminalRecord,
	allowPending bool,
) lifecycleResult {
	if record == nil {
		return lifecycleDiagnostic(position, DiagnosticInvalidTransition)
	}
	turnID, resolution := normalizer.resolveTurnID(record.TurnID)
	if resolution != "" {
		return lifecycleDiagnostic(position, resolution)
	}
	if turnID == "" {
		return lifecycleDiagnostic(position, DiagnosticMissingTurnStart)
	}
	state, exists := normalizer.turns[turnID]
	if !exists {
		if !allowPending || record.TurnID == nil {
			return lifecycleDiagnostic(position, DiagnosticMissingTurnStart)
		}
		pending, ok := normalizer.pendingState(turnID)
		if !ok {
			return lifecycleDiagnostic(position, DiagnosticStateLimitExceeded)
		}
		candidate := positionedTurnTerminal{
			position: position,
			record: decodedTurnTerminalRecord{
				TurnID: stringPointer(turnID), CompletedAtMS: record.CompletedAtMS, Outcome: record.Outcome,
			},
		}
		if pending.terminal != nil {
			if equalDecodedTerminal(&pending.terminal.record, &candidate.record) {
				return lifecycleResult{}
			}
			return lifecycleDiagnostic(position, DiagnosticInvalidTransition)
		}
		pending.terminal = &candidate
		return lifecycleDiagnostic(position, DiagnosticMissingTurnStart)
	}

	if state.terminal != nil {
		if state.terminal.CompletedAtMS == record.CompletedAtMS && state.terminal.Outcome == record.Outcome {
			return lifecycleResult{}
		}
		return lifecycleDiagnostic(position, DiagnosticInvalidTransition)
	}
	if record.CompletedAtMS < state.start.StartedAtMS {
		return lifecycleDiagnostic(position, DiagnosticInvalidTransition)
	}

	end := &TurnEndFact{
		SessionID: normalizer.session.SessionID, TurnID: turnID,
		CompletedAtMS: record.CompletedAtMS, Outcome: record.Outcome,
	}
	if state.latestUsage != nil {
		end.FinalUsage = cloneTurnUsageFact(state.latestUsage)
		end.FinalUsage.IsFinal = true
	}
	state.terminal = cloneTurnEndFact(end)
	normalizer.retainClosedTurn(turnID)
	return lifecycleResult{Events: []ParsedEvent{{
		Kind: EventTurnEnded, Position: position, TurnEnd: cloneTurnEndFact(end),
	}}}
}

func (normalizer *lifecycleNormalizer) resolveTurnID(explicit *string) (string, DiagnosticCode) {
	if explicit != nil {
		return *explicit, ""
	}
	open := normalizer.openTurnIDs()
	switch len(open) {
	case 0:
		return "", DiagnosticMissingTurnStart
	case 1:
		return open[0], ""
	default:
		return "", DiagnosticAmbiguousTurn
	}
}

func (normalizer *lifecycleNormalizer) openTurnIDs() []string {
	result := make([]string, 0, len(normalizer.turns))
	for turnID, state := range normalizer.turns {
		if state.terminal == nil {
			result = append(result, turnID)
		}
	}
	return result
}

func (normalizer *lifecycleNormalizer) pendingState(turnID string) (*pendingTurnState, bool) {
	state := normalizer.pending[turnID]
	if state == nil {
		if len(normalizer.pending) >= maxPendingTurnStates {
			return nil, false
		}
		state = &pendingTurnState{}
		normalizer.pending[turnID] = state
	}
	return state, true
}

func (normalizer *lifecycleNormalizer) retainClosedTurn(turnID string) {
	normalizer.closedOrder = append(normalizer.closedOrder, turnID)
	if len(normalizer.closedOrder) <= maxRetainedClosedTurnStates {
		return
	}
	oldest := normalizer.closedOrder[0]
	normalizer.closedOrder = normalizer.closedOrder[1:]
	if state := normalizer.turns[oldest]; state != nil && state.terminal != nil {
		delete(normalizer.turns, oldest)
	}
}

func (result *lifecycleResult) append(other lifecycleResult) {
	result.Events = append(result.Events, other.Events...)
	result.Diagnostics = append(result.Diagnostics, other.Diagnostics...)
}

func lifecycleDiagnostic(position SourcePosition, code DiagnosticCode) lifecycleResult {
	return lifecycleResult{Diagnostics: []ParserDiagnostic{newLifecycleDiagnostic(position, code)}}
}

func newLifecycleDiagnostic(position SourcePosition, code DiagnosticCode) ParserDiagnostic {
	return ParserDiagnostic{
		Class: DiagnosticClassLifecycle, Code: code,
		StartOffset: position.StartOffset, EndOffset: position.EndOffset,
	}
}

func sessionFact(record *decodedLine, sourceKind SourceKind) SessionMetaFact {
	meta := record.SessionMeta
	return SessionMetaFact{
		SessionID: meta.SessionID, RootSessionID: meta.RootSessionID, SourceKind: sourceKind,
		CreatedAtMS: meta.CreatedAtMS, ObservedAtMS: record.ObservedAtMS,
		InitialCWD: meta.InitialCWD, Originator: meta.Originator,
		CLIVersion: meta.CLIVersion, Source: meta.Source, ModelProvider: meta.ModelProvider,
	}
}

func equalSessionFact(left, right *SessionMetaFact) bool {
	return *left == *right
}

func equalTurnStartFact(left, right *TurnStartFact) bool {
	return left.SessionID == right.SessionID && left.TurnID == right.TurnID &&
		left.StartedAtMS == right.StartedAtMS && equalOptionalInt64(left.ContextWindow, right.ContextWindow)
}

func equalTurnContextFact(left, right *TurnContextFact) bool {
	return left.SessionID == right.SessionID && left.TurnID == right.TurnID &&
		left.ObservedAtMS == right.ObservedAtMS && left.CWD == right.CWD && left.Model == right.Model &&
		equalOptionalString(left.Effort, right.Effort)
}

func equalDecodedTurnContext(left, right *decodedTurnContextRecord) bool {
	return equalOptionalString(left.TurnID, right.TurnID) && left.CWD == right.CWD && left.Model == right.Model &&
		equalOptionalString(left.Effort, right.Effort)
}

func equalDecodedTerminal(left, right *decodedTurnTerminalRecord) bool {
	return equalOptionalString(left.TurnID, right.TurnID) && left.CompletedAtMS == right.CompletedAtMS &&
		left.Outcome == right.Outcome
}

func equalOptionalInt64(left, right *int64) bool {
	return (left == nil && right == nil) || (left != nil && right != nil && *left == *right)
}

func equalOptionalString(left, right *string) bool {
	return (left == nil && right == nil) || (left != nil && right != nil && *left == *right)
}

func cloneTokenCounters(value TokenCounters) TokenCounters {
	return TokenCounters{
		InputTokens: cloneInt64(value.InputTokens), CachedInputTokens: cloneInt64(value.CachedInputTokens),
		OutputTokens: cloneInt64(value.OutputTokens), ReasoningTokens: cloneInt64(value.ReasoningTokens),
	}
}

func cloneString(value *string) *string {
	if value == nil {
		return nil
	}
	return stringPointer(*value)
}

func cloneSessionFact(value *SessionMetaFact) *SessionMetaFact {
	if value == nil {
		return nil
	}
	cloned := *value
	return &cloned
}

func cloneTurnStartFact(value *TurnStartFact) *TurnStartFact {
	if value == nil {
		return nil
	}
	cloned := *value
	cloned.ContextWindow = cloneInt64(value.ContextWindow)
	return &cloned
}

func cloneTurnContextFact(value *TurnContextFact) *TurnContextFact {
	if value == nil {
		return nil
	}
	cloned := *value
	cloned.Effort = cloneString(value.Effort)
	return &cloned
}

func cloneTurnUsageFact(value *TurnUsageFact) *TurnUsageFact {
	if value == nil {
		return nil
	}
	cloned := *value
	cloned.Usage = cloneTokenCounters(value.Usage)
	cloned.ContextWindow = cloneInt64(value.ContextWindow)
	return &cloned
}

func cloneTurnEndFact(value *TurnEndFact) *TurnEndFact {
	if value == nil {
		return nil
	}
	cloned := *value
	cloned.FinalUsage = cloneTurnUsageFact(value.FinalUsage)
	return &cloned
}
