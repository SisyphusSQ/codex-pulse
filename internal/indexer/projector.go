package indexer

import (
	"crypto/sha256"
	"errors"
	"fmt"
	"math"
	"sort"

	"github.com/SisyphusSQ/codex-pulse/internal/codex/logs"
	"github.com/SisyphusSQ/codex-pulse/internal/store"
)

var ErrInvalidProjection = errors.New("invalid rollout fact projection")

type projector struct {
	sourceFileID      string
	generation        int64
	firstCounterState string
	sessionID         string
	sessionSourceKind logs.SourceKind
	session           *logs.SessionMetaFact
	openTurns         map[string]store.ProjectedOpenTurnCheckpoint
	current           *store.SessionCurrent
	sessionUsage      *store.SessionUsageCurrent
}

func newProjector(
	sourceFileID string,
	generation int64,
	mode store.GenerationMode,
	seed *logs.ParserSeed,
	checkpoint store.ProjectorCheckpoint,
) (*projector, error) {
	if sourceFileID == "" || generation < 0 ||
		(mode != store.GenerationModeAppend && mode != store.GenerationModeRebuild) {
		return nil, invalidProjection("generation or mode is invalid")
	}
	result := &projector{
		sourceFileID: sourceFileID, generation: generation, firstCounterState: "live",
		openTurns: make(map[string]store.ProjectedOpenTurnCheckpoint, len(checkpoint.OpenTurns)),
	}
	if checkpoint.SessionSourceKind != "" {
		result.sessionSourceKind = logs.SourceKind(checkpoint.SessionSourceKind)
		if result.sessionSourceKind != logs.SourceKindSession &&
			result.sessionSourceKind != logs.SourceKindArchivedSession {
			return nil, invalidProjection("projector session source kind is invalid")
		}
	}
	if mode == store.GenerationModeRebuild {
		result.firstCounterState = "rebuilt"
	}
	if seed != nil && seed.Session != nil {
		result.sessionID = seed.Session.SessionID
		result.session = cloneParserSession(seed.Session)
		if result.sessionSourceKind == "" {
			result.sessionSourceKind = result.session.SourceKind
		}
		result.session.SourceKind = result.sessionSourceKind
	}
	seedOpen := make(map[string]logs.OpenTurnSeed)
	if seed != nil {
		for _, turn := range seed.OpenTurns {
			if turn.TurnID == "" {
				return nil, invalidProjection("parser seed contains an empty open turn ID")
			}
			seedOpen[turn.TurnID] = turn
		}
	}
	if len(seedOpen) != len(checkpoint.OpenTurns) {
		return nil, invalidProjection("parser and projector open turns differ")
	}
	for _, turn := range checkpoint.OpenTurns {
		seedTurn, found := seedOpen[turn.TurnID]
		if !found || turn.SessionID == "" || turn.SessionID != result.sessionID ||
			turn.StartedAtMS != seedTurn.StartedAtMS || turn.SourceGeneration != generation ||
			turn.StartOffset < 0 {
			return nil, invalidProjection("parser and projector open turn state conflicts")
		}
		if _, duplicate := result.openTurns[turn.TurnID]; duplicate {
			return nil, invalidProjection("projector open turn is duplicated")
		}
		result.openTurns[turn.TurnID] = cloneProjectedOpenTurn(turn)
	}
	cloned := cloneProjectorCheckpoint(checkpoint)
	result.current = cloned.Current
	result.sessionUsage = cloned.SessionUsage
	if result.current != nil {
		if result.sessionID == "" {
			result.sessionID = result.current.SessionID
		}
		if result.current.SessionID != result.sessionID {
			return nil, invalidProjection("current projection belongs to another session")
		}
	}
	if result.sessionUsage != nil {
		if result.sessionID == "" {
			result.sessionID = result.sessionUsage.SessionID
		}
		if result.sessionUsage.SessionID != result.sessionID ||
			result.sessionUsage.SourceGeneration != generation {
			return nil, invalidProjection("usage projection belongs to another session or generation")
		}
	}
	return result, nil
}

func (projector *projector) Project(events []logs.ParsedEvent) ([]store.FactBatch, store.ProjectorCheckpoint, error) {
	if projector == nil {
		return nil, store.ProjectorCheckpoint{}, invalidProjection("projector is nil")
	}
	working := projector.clone()
	facts := make([]store.FactBatch, 0, len(events))
	for _, event := range events {
		fact, err := working.projectEvent(event)
		if err != nil {
			return nil, store.ProjectorCheckpoint{}, err
		}
		facts = append(facts, fact)
	}
	*projector = *working
	return facts, projector.checkpoint(), nil
}

func (projector *projector) projectEvent(event logs.ParsedEvent) (store.FactBatch, error) {
	if event.Position.StartOffset < 0 || event.Position.EndOffset <= event.Position.StartOffset {
		return store.FactBatch{}, invalidProjection("event source position is invalid")
	}
	switch event.Kind {
	case logs.EventSessionMeta:
		return projector.projectSessionMeta(event)
	case logs.EventTurnStarted:
		return projector.projectTurnStart(event)
	case logs.EventTurnContext:
		return projector.projectTurnContext(event)
	case logs.EventTurnUsage:
		return projector.projectTurnUsage(event)
	case logs.EventSessionUsage:
		return projector.projectSessionUsage(event)
	case logs.EventQuotaObservation:
		return projector.projectQuotaObservation(event)
	case logs.EventTurnEnded:
		return projector.projectTurnEnd(event)
	default:
		return store.FactBatch{}, invalidProjection("event kind is unsupported")
	}
}

func (projector *projector) projectQuotaObservation(event logs.ParsedEvent) (store.FactBatch, error) {
	observation := event.QuotaObservation
	if observation == nil || projector.session == nil || observation.SessionID != projector.sessionID ||
		observation.AccountScope != logs.QuotaAccountScopeDefault ||
		observation.Source != logs.QuotaSourceLocalJSONL ||
		(observation.WindowKind != logs.QuotaWindowPrimary && observation.WindowKind != logs.QuotaWindowSecondary) ||
		math.IsNaN(observation.UsedPercent) || math.IsInf(observation.UsedPercent, 0) ||
		observation.UsedPercent < 0 || observation.UsedPercent > 100 || observation.WindowMinutes <= 0 ||
		observation.ResetsAtMS < 0 || observation.ObservedAtMS < 0 || !validQuotaPlanType(observation.PlanType) {
		return store.FactBatch{}, invalidProjection("quota observation event is invalid")
	}
	validity, reason, err := quotaValidityFromEvent(observation.Validity, observation.RejectionReason)
	if err != nil {
		return store.FactBatch{}, err
	}
	sessionID := observation.SessionID
	sourceFileID := projector.sourceFileID
	projected := &store.QuotaObservationSample{
		ObservationID: quotaObservationID(
			projector.sourceFileID, observation.SessionID, projector.generation,
			event.Position.StartOffset, observation.WindowKind,
		),
		AccountScope:     store.QuotaAccountScopeDefault,
		Source:           store.QuotaSourceLocalJSONL,
		LimitID:          cloneString(observation.LimitID),
		WindowKind:       store.QuotaWindowKind(observation.WindowKind),
		UsedPercent:      observation.UsedPercent,
		WindowMinutes:    observation.WindowMinutes,
		ResetsAtMS:       observation.ResetsAtMS,
		PlanType:         cloneString(observation.PlanType),
		ObservedAtMS:     observation.ObservedAtMS,
		Validity:         validity,
		RejectionReason:  reason,
		SessionID:        &sessionID,
		SourceFileID:     &sourceFileID,
		SourceGeneration: projector.generation,
		SourceOffset:     event.Position.StartOffset,
	}
	lastSeenAtMS := observation.ObservedAtMS
	if lastSeenAtMS < projector.session.ObservedAtMS {
		lastSeenAtMS = projector.session.ObservedAtMS
	}
	return store.FactBatch{
		Session: projector.sessionFactAt(lastSeenAtMS), QuotaObservation: projected,
	}, nil
}

func quotaObservationID(
	sourceFileID string,
	sessionID string,
	generation int64,
	offset int64,
	window logs.QuotaWindowKind,
) string {
	digest := sha256.Sum256([]byte(fmt.Sprintf(
		"%q\n%q\n%d\n%d\n%s", sourceFileID, sessionID, generation, offset, window,
	)))
	return fmt.Sprintf("quota-local-jsonl-%x", digest)
}

func quotaValidityFromEvent(
	validity logs.QuotaValidity,
	reason *logs.QuotaRejectionReason,
) (store.QuotaValidity, *store.QuotaRejectionReason, error) {
	var projectedValidity store.QuotaValidity
	switch validity {
	case logs.QuotaValidityAccepted:
		projectedValidity = store.QuotaValidityAccepted
	case logs.QuotaValiditySuspicious:
		projectedValidity = store.QuotaValiditySuspicious
	case logs.QuotaValidityRejected:
		projectedValidity = store.QuotaValidityRejected
	default:
		return "", nil, invalidProjection("quota validity is invalid")
	}
	if projectedValidity == store.QuotaValidityAccepted {
		if reason != nil {
			return "", nil, invalidProjection("accepted quota observation has a reason")
		}
		return projectedValidity, nil, nil
	}
	if reason == nil {
		return "", nil, invalidProjection("non-accepted quota observation has no reason")
	}
	projectedReason := store.QuotaRejectionReason(*reason)
	switch projectedReason {
	case store.QuotaReasonMissingLimitID, store.QuotaReasonMissingPrimaryWindow,
		store.QuotaReasonResetNotFuture, store.QuotaReasonUnknownPlanType:
		return projectedValidity, &projectedReason, nil
	default:
		return "", nil, invalidProjection("quota observation reason is invalid")
	}
}

func validQuotaPlanType(planType *string) bool {
	if planType == nil {
		return true
	}
	switch *planType {
	case "free", "go", "plus", "pro", "prolite", "team", "self_serve_business_usage_based",
		"business", "enterprise_cbp_usage_based", "enterprise", "edu", "unknown":
		return true
	default:
		return false
	}
}

func (projector *projector) projectSessionMeta(event logs.ParsedEvent) (store.FactBatch, error) {
	meta := event.SessionMeta
	if meta == nil || meta.SessionID == "" ||
		(meta.SourceKind != logs.SourceKindSession && meta.SourceKind != logs.SourceKindArchivedSession) {
		return store.FactBatch{}, invalidProjection("session metadata event is invalid")
	}
	if projector.sessionID != "" && projector.sessionID != meta.SessionID {
		return store.FactBatch{}, invalidProjection("event belongs to another session")
	}
	projector.sessionID = meta.SessionID
	projector.session = cloneParserSession(meta)
	if projector.sessionSourceKind == "" {
		projector.sessionSourceKind = meta.SourceKind
	}
	projector.session.SourceKind = projector.sessionSourceKind
	current := projector.currentAt(meta.ObservedAtMS)
	if current.CurrentCWD == nil && meta.InitialCWD != "" {
		current.CurrentCWD = stringPointer(meta.InitialCWD)
	}
	projector.current = current
	return store.FactBatch{
		Session:        projector.sessionFactAt(meta.ObservedAtMS),
		SessionCurrent: cloneSessionCurrent(current),
	}, nil
}

func (projector *projector) projectTurnStart(event logs.ParsedEvent) (store.FactBatch, error) {
	start := event.TurnStart
	if start == nil || start.TurnID == "" || start.StartedAtMS < 0 ||
		!projector.matchesSession(start.SessionID) {
		return store.FactBatch{}, invalidProjection("turn start event is invalid")
	}
	if _, exists := projector.openTurns[start.TurnID]; exists {
		return store.FactBatch{}, invalidProjection("turn start is duplicated")
	}
	turn := store.ProjectedOpenTurnCheckpoint{
		TurnID: start.TurnID, SessionID: start.SessionID, StartedAtMS: start.StartedAtMS,
		SourceGeneration: projector.generation, StartOffset: event.Position.StartOffset,
	}
	projector.openTurns[start.TurnID] = turn
	current := projector.currentAt(start.StartedAtMS)
	current.ActiveTurnID = stringPointer(start.TurnID)
	projector.current = current
	return store.FactBatch{
		Session:        projector.sessionFactAt(start.StartedAtMS),
		Turn:           turnFromCheckpoint(turn),
		SessionCurrent: cloneSessionCurrent(current),
	}, nil
}

func (projector *projector) projectTurnContext(event logs.ParsedEvent) (store.FactBatch, error) {
	context := event.TurnContext
	if context == nil || context.TurnID == "" || !projector.matchesSession(context.SessionID) {
		return store.FactBatch{}, invalidProjection("turn context event is invalid")
	}
	turn, found := projector.openTurns[context.TurnID]
	if !found || context.ObservedAtMS < turn.StartedAtMS {
		return store.FactBatch{}, invalidProjection("turn context has no matching open turn")
	}
	turn.Model = optionalString(context.Model)
	turn.ReasoningEffort = cloneString(context.Effort)
	turn.CWD = optionalString(context.CWD)
	projector.openTurns[context.TurnID] = turn
	current := projector.currentAt(context.ObservedAtMS)
	current.CurrentModel = cloneString(turn.Model)
	current.CurrentCWD = cloneString(turn.CWD)
	projector.current = current
	return store.FactBatch{
		Session:        projector.sessionFactAt(context.ObservedAtMS),
		Turn:           turnFromCheckpoint(turn),
		SessionCurrent: cloneSessionCurrent(current),
	}, nil
}

func (projector *projector) projectTurnUsage(event logs.ParsedEvent) (store.FactBatch, error) {
	usage := event.TurnUsage
	if usage == nil || usage.TurnID == "" || !projector.matchesSession(usage.SessionID) {
		return store.FactBatch{}, invalidProjection("turn usage event is invalid")
	}
	if _, found := projector.openTurns[usage.TurnID]; !found {
		return store.FactBatch{}, invalidProjection("turn usage has no matching open turn")
	}
	return store.FactBatch{
		Session: projector.sessionFactAt(usage.ObservedAtMS),
		Usage:   turnUsageFromEvent(usage, projector.generation, event.Position.StartOffset),
	}, nil
}

func (projector *projector) projectSessionUsage(event logs.ParsedEvent) (store.FactBatch, error) {
	usage := event.SessionUsage
	if usage == nil || !projector.matchesSession(usage.SessionID) {
		return store.FactBatch{}, invalidProjection("session usage event is invalid")
	}
	epoch := int64(0)
	state := projector.firstCounterState
	if projector.sessionUsage != nil {
		epoch = projector.sessionUsage.CounterEpoch
		state = "live"
		if countersDecrease(projector.sessionUsage, usage.Usage) {
			epoch++
			state = "reset"
		}
	}
	projected := &store.SessionUsageCurrent{
		SessionID: usage.SessionID, CounterEpoch: epoch,
		TotalInputTokens:     cloneInt64(usage.Usage.InputTokens),
		TotalCachedTokens:    cloneInt64(usage.Usage.CachedInputTokens),
		TotalOutputTokens:    cloneInt64(usage.Usage.OutputTokens),
		TotalReasoningTokens: cloneInt64(usage.Usage.ReasoningTokens),
		ObservedAtMS:         usage.ObservedAtMS, SourceGeneration: projector.generation,
		SourceOffset: event.Position.StartOffset, CounterState: state,
	}
	projector.sessionUsage = cloneSessionUsage(projected)
	return store.FactBatch{
		Session: projector.sessionFactAt(usage.ObservedAtMS), SessionUsageCurrent: projected,
	}, nil
}

func (projector *projector) projectTurnEnd(event logs.ParsedEvent) (store.FactBatch, error) {
	end := event.TurnEnd
	if end == nil || end.TurnID == "" || !projector.matchesSession(end.SessionID) {
		return store.FactBatch{}, invalidProjection("turn terminal event is invalid")
	}
	open, found := projector.openTurns[end.TurnID]
	if !found || end.CompletedAtMS < open.StartedAtMS {
		return store.FactBatch{}, invalidProjection("turn terminal has no matching open turn")
	}
	turn := turnFromCheckpoint(open)
	outcome := string(end.Outcome)
	completeOffset := event.Position.StartOffset
	turn.CompletedAtMS = int64Pointer(end.CompletedAtMS)
	turn.Outcome = &outcome
	turn.CompleteOffset = &completeOffset
	fact := store.FactBatch{Session: projector.sessionFactAt(end.CompletedAtMS), Turn: turn}
	if end.FinalUsage != nil {
		fact.Usage = turnUsageFromEvent(end.FinalUsage, projector.generation, event.Position.StartOffset)
		fact.Usage.IsFinal = true
	}
	delete(projector.openTurns, end.TurnID)
	current := projector.currentAt(end.CompletedAtMS)
	if current.ActiveTurnID != nil && *current.ActiveTurnID == end.TurnID {
		current.ActiveTurnID = nil
	}
	projector.current = current
	fact.SessionCurrent = cloneSessionCurrent(current)
	return fact, nil
}

func (projector *projector) matchesSession(sessionID string) bool {
	if sessionID == "" {
		return false
	}
	if projector.sessionID == "" {
		projector.sessionID = sessionID
	}
	return projector.sessionID == sessionID
}

func (projector *projector) currentAt(atMS int64) *store.SessionCurrent {
	current := cloneSessionCurrent(projector.current)
	if current == nil {
		return &store.SessionCurrent{
			SessionID: projector.sessionID, LastActivityAtMS: int64Pointer(atMS), UpdatedAtMS: atMS,
		}
	}
	if current.LastActivityAtMS == nil || atMS > *current.LastActivityAtMS {
		current.LastActivityAtMS = int64Pointer(atMS)
	}
	if atMS > current.UpdatedAtMS {
		current.UpdatedAtMS = atMS
	} else {
		current.UpdatedAtMS++
	}
	return current
}

func (projector *projector) checkpoint() store.ProjectorCheckpoint {
	checkpoint := store.ProjectorCheckpoint{
		OpenTurns: make([]store.ProjectedOpenTurnCheckpoint, 0, len(projector.openTurns)),
		Current:   cloneSessionCurrent(projector.current), SessionUsage: cloneSessionUsage(projector.sessionUsage),
	}
	if projector.session != nil {
		checkpoint.SessionSourceKind = string(projector.session.SourceKind)
	}
	// Parser checkpoint orders open turns by ID; use the same deterministic order.
	for _, turnID := range sortedKeys(projector.openTurns) {
		checkpoint.OpenTurns = append(checkpoint.OpenTurns, cloneProjectedOpenTurn(projector.openTurns[turnID]))
	}
	return checkpoint
}

func (value *projector) clone() *projector {
	copy := &projector{
		sourceFileID: value.sourceFileID,
		generation:   value.generation, firstCounterState: value.firstCounterState,
		sessionID: value.sessionID, sessionSourceKind: value.sessionSourceKind,
		session:   cloneParserSession(value.session),
		openTurns: make(map[string]store.ProjectedOpenTurnCheckpoint, len(value.openTurns)),
		current:   cloneSessionCurrent(value.current), sessionUsage: cloneSessionUsage(value.sessionUsage),
	}
	for turnID, turn := range value.openTurns {
		copy.openTurns[turnID] = cloneProjectedOpenTurn(turn)
	}
	return copy
}

func (projector *projector) sessionFactAt(lastSeenAtMS int64) *store.Session {
	meta := projector.session
	if meta == nil {
		return nil
	}
	return &store.Session{
		SessionID: meta.SessionID, Provider: logs.ProviderCodex,
		Originator: optionalString(meta.Originator), SourceKind: string(meta.SourceKind),
		ModelProvider: optionalString(meta.ModelProvider), InitialCWD: optionalString(meta.InitialCWD),
		CLIVersion: optionalString(meta.CLIVersion), CreatedAtMS: meta.CreatedAtMS,
		FirstSeenAtMS: meta.ObservedAtMS, LastSeenAtMS: lastSeenAtMS,
	}
}

func cloneParserSession(value *logs.SessionMetaFact) *logs.SessionMetaFact {
	if value == nil {
		return nil
	}
	clone := *value
	return &clone
}

func cloneProjectorCheckpoint(value store.ProjectorCheckpoint) store.ProjectorCheckpoint {
	clone := store.ProjectorCheckpoint{
		SessionSourceKind: value.SessionSourceKind,
		OpenTurns:         make([]store.ProjectedOpenTurnCheckpoint, len(value.OpenTurns)),
		Current:           cloneSessionCurrent(value.Current), SessionUsage: cloneSessionUsage(value.SessionUsage),
	}
	for index, turn := range value.OpenTurns {
		clone.OpenTurns[index] = cloneProjectedOpenTurn(turn)
	}
	return clone
}

func cloneProjectedOpenTurn(value store.ProjectedOpenTurnCheckpoint) store.ProjectedOpenTurnCheckpoint {
	value.Model = cloneString(value.Model)
	value.ReasoningEffort = cloneString(value.ReasoningEffort)
	value.CWD = cloneString(value.CWD)
	return value
}

func turnFromCheckpoint(value store.ProjectedOpenTurnCheckpoint) *store.Turn {
	return &store.Turn{
		TurnID: value.TurnID, SessionID: value.SessionID, StartedAtMS: value.StartedAtMS,
		Model: cloneString(value.Model), ReasoningEffort: cloneString(value.ReasoningEffort),
		CWD: cloneString(value.CWD), SourceGeneration: value.SourceGeneration,
		StartOffset: value.StartOffset,
	}
}

func turnUsageFromEvent(value *logs.TurnUsageFact, generation, offset int64) *store.TurnUsage {
	return &store.TurnUsage{
		TurnID: value.TurnID, ObservedAtMS: value.ObservedAtMS, IsFinal: value.IsFinal,
		InputTokens:       cloneInt64(value.Usage.InputTokens),
		CachedInputTokens: cloneInt64(value.Usage.CachedInputTokens),
		OutputTokens:      cloneInt64(value.Usage.OutputTokens),
		ReasoningTokens:   cloneInt64(value.Usage.ReasoningTokens),
		ContextWindow:     cloneInt64(value.ContextWindow), SourceGeneration: generation,
		SourceOffset: offset, Confidence: "observed", UpdatedAtMS: value.ObservedAtMS,
	}
}

func countersDecrease(previous *store.SessionUsageCurrent, current logs.TokenCounters) bool {
	return knownCounterDecreases(previous.TotalInputTokens, current.InputTokens) ||
		knownCounterDecreases(previous.TotalCachedTokens, current.CachedInputTokens) ||
		knownCounterDecreases(previous.TotalOutputTokens, current.OutputTokens) ||
		knownCounterDecreases(previous.TotalReasoningTokens, current.ReasoningTokens)
}

func knownCounterDecreases(previous, current *int64) bool {
	return previous != nil && current != nil && *current < *previous
}

func cloneSessionCurrent(value *store.SessionCurrent) *store.SessionCurrent {
	if value == nil {
		return nil
	}
	clone := *value
	clone.ThreadName = cloneString(value.ThreadName)
	clone.ThreadNameUpdatedAtMS = cloneInt64(value.ThreadNameUpdatedAtMS)
	clone.ActiveTurnID = cloneString(value.ActiveTurnID)
	clone.CurrentModel = cloneString(value.CurrentModel)
	clone.CurrentCWD = cloneString(value.CurrentCWD)
	clone.LastActivityAtMS = cloneInt64(value.LastActivityAtMS)
	return &clone
}

func cloneSessionUsage(value *store.SessionUsageCurrent) *store.SessionUsageCurrent {
	if value == nil {
		return nil
	}
	clone := *value
	clone.TotalInputTokens = cloneInt64(value.TotalInputTokens)
	clone.TotalCachedTokens = cloneInt64(value.TotalCachedTokens)
	clone.TotalOutputTokens = cloneInt64(value.TotalOutputTokens)
	clone.TotalReasoningTokens = cloneInt64(value.TotalReasoningTokens)
	return &clone
}

func optionalString(value string) *string {
	if value == "" {
		return nil
	}
	return stringPointer(value)
}

func stringPointer(value string) *string { return &value }

func int64Pointer(value int64) *int64 { return &value }

func cloneString(value *string) *string {
	if value == nil {
		return nil
	}
	return stringPointer(*value)
}

func cloneInt64(value *int64) *int64 {
	if value == nil {
		return nil
	}
	return int64Pointer(*value)
}

func sortedKeys(values map[string]store.ProjectedOpenTurnCheckpoint) []string {
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}

func invalidProjection(message string) error {
	return fmt.Errorf("%w: %s", ErrInvalidProjection, message)
}
