package store

import (
	"bytes"
	"encoding/json"
	"io"
	"unicode/utf8"
)

func marshalParserCheckpoint(checkpoint ParserCheckpoint) ([]byte, []byte, error) {
	if err := validateParserCheckpoint(checkpoint); err != nil {
		return nil, nil, err
	}
	seed, err := json.Marshal(checkpoint.Seed)
	if err != nil {
		return nil, nil, invalidRecord("marshal parser seed")
	}
	projector, err := json.Marshal(checkpoint.Projector)
	if err != nil {
		return nil, nil, invalidRecord("marshal projector checkpoint")
	}
	if len(seed) > maxParserCheckpointBytes || len(projector) > maxParserCheckpointBytes {
		return nil, nil, invalidRecord("parser checkpoint exceeds size limit")
	}
	return seed, projector, nil
}

func unmarshalParserCheckpoint(
	version int,
	parserVersion string,
	committedOffset int64,
	seedBytes []byte,
	projectorBytes []byte,
) (ParserCheckpoint, error) {
	if len(seedBytes) == 0 || len(projectorBytes) == 0 ||
		len(seedBytes) > maxParserCheckpointBytes || len(projectorBytes) > maxParserCheckpointBytes {
		return ParserCheckpoint{}, invalidRecord("stored parser checkpoint size is invalid")
	}
	var seed *ParserSeedCheckpoint
	if err := decodeCheckpointJSON(seedBytes, &seed); err != nil {
		return ParserCheckpoint{}, err
	}
	var projector ProjectorCheckpoint
	if err := decodeCheckpointJSON(projectorBytes, &projector); err != nil {
		return ParserCheckpoint{}, err
	}
	checkpoint := ParserCheckpoint{
		Version: version, ParserVersion: parserVersion, CommittedOffset: committedOffset,
		Seed: seed, Projector: projector,
	}
	if err := validateParserCheckpoint(checkpoint); err != nil {
		return ParserCheckpoint{}, err
	}
	return checkpoint, nil
}

func decodeCheckpointJSON(content []byte, target any) error {
	decoder := json.NewDecoder(bytes.NewReader(content))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		return invalidRecord("stored parser checkpoint is invalid")
	}
	if err := decoder.Decode(&struct{}{}); err != io.EOF {
		return invalidRecord("stored parser checkpoint has trailing data")
	}
	return nil
}

func validateParserCheckpoint(checkpoint ParserCheckpoint) error {
	if checkpoint.Version != ParserCheckpointVersion ||
		!validCheckpointText(checkpoint.ParserVersion, maxCheckpointIdentifier, true) ||
		checkpoint.CommittedOffset < 0 {
		return invalidRecord("parser checkpoint header is invalid")
	}
	if checkpoint.CommittedOffset == 0 {
		if checkpoint.Seed != nil {
			return invalidRecord("offset zero must not have parser seed")
		}
	} else {
		if checkpoint.Seed == nil {
			return invalidRecord("non-zero offset requires parser seed")
		}
		if err := validateParserSeedCheckpoint(*checkpoint.Seed, checkpoint.CommittedOffset); err != nil {
			return err
		}
	}
	return validateProjectorCheckpoint(checkpoint.Projector, checkpoint.Seed, checkpoint.CommittedOffset)
}

func validateParserSeedCheckpoint(seed ParserSeedCheckpoint, committedOffset int64) error {
	if len(seed.OpenTurns) > maxCheckpointOpenTurns || len(seed.PendingTurns) > maxCheckpointPending ||
		len(seed.ClosedTurns) > maxCheckpointClosed {
		return invalidRecord("parser checkpoint state exceeds limit")
	}
	if seed.Session == nil {
		if len(seed.OpenTurns)+len(seed.PendingTurns)+len(seed.ClosedTurns) != 0 {
			return invalidRecord("parser checkpoint turn state requires session")
		}
		return nil
	}
	session := seed.Session
	if !validCheckpointIdentifier(session.SessionID) || !validCheckpointIdentifier(session.RootSessionID) ||
		(session.SourceKind != "session" && session.SourceKind != "archived_session") ||
		session.CreatedAtMS < 0 || session.ObservedAtMS < 0 ||
		!validCheckpointText(session.InitialCWD, maxCheckpointMetadata, true) ||
		!validCheckpointText(session.Originator, maxCheckpointIdentifier, true) ||
		!validCheckpointText(session.CLIVersion, maxCheckpointIdentifier, true) ||
		!validCheckpointText(session.Source, maxCheckpointIdentifier, true) ||
		!validCheckpointText(session.ModelProvider, maxCheckpointIdentifier, false) {
		return invalidRecord("parser checkpoint session is invalid")
	}
	seen := make(map[string]struct{}, len(seed.OpenTurns)+len(seed.PendingTurns)+len(seed.ClosedTurns))
	for _, turn := range seed.OpenTurns {
		if !claimCheckpointTurn(seen, turn.TurnID) || turn.StartedAtMS < 0 || !validCheckpointCounter(turn.ContextWindow) ||
			!validCheckpointContext(turn.Context, session.SessionID, turn.TurnID, turn.StartedAtMS) ||
			!validCheckpointUsage(turn.LatestUsage, session.SessionID, turn.TurnID, turn.StartedAtMS, false) {
			return invalidRecord("parser checkpoint open turn is invalid")
		}
	}
	for _, turn := range seed.PendingTurns {
		if !claimCheckpointTurn(seen, turn.TurnID) || (turn.Context == nil && turn.Terminal == nil) ||
			!validCheckpointPendingContext(turn.Context, committedOffset) ||
			!validCheckpointPendingTerminal(turn.Terminal, committedOffset) ||
			(turn.Context != nil && turn.Terminal != nil && turn.Context.Position.EndOffset > turn.Terminal.Position.StartOffset) {
			return invalidRecord("parser checkpoint pending turn is invalid")
		}
	}
	for _, turn := range seed.ClosedTurns {
		terminal := turn.Terminal
		if !claimCheckpointTurn(seen, turn.TurnID) || turn.StartedAtMS < 0 || !validCheckpointCounter(turn.ContextWindow) ||
			terminal.SessionID != session.SessionID || terminal.TurnID != turn.TurnID ||
			terminal.CompletedAtMS < turn.StartedAtMS || !validCheckpointOutcome(terminal.Outcome) ||
			!validCheckpointUsage(terminal.FinalUsage, session.SessionID, turn.TurnID, turn.StartedAtMS, true) {
			return invalidRecord("parser checkpoint closed turn is invalid")
		}
	}
	return nil
}

func validateProjectorCheckpoint(
	projector ProjectorCheckpoint,
	seed *ParserSeedCheckpoint,
	committedOffset int64,
) error {
	var sessionID string
	if seed != nil && seed.Session != nil {
		sessionID = seed.Session.SessionID
	}
	if projector.SessionSourceKind != "" && projector.SessionSourceKind != "session" &&
		projector.SessionSourceKind != "archived_session" {
		return invalidRecord("projector session source kind is invalid")
	}
	if sessionID != "" && projector.SessionSourceKind == "" {
		return invalidRecord("projector canonical session source kind is missing")
	}
	if sessionID == "" && projector.SessionSourceKind != "" {
		return invalidRecord("projector session source kind has no session")
	}
	if len(projector.OpenTurns) > maxCheckpointOpenTurns || len(projector.OpenTurns) != checkpointSeedOpenTurnCount(seed) {
		return invalidRecord("projector open turn checkpoint is invalid")
	}
	seenOpen := make(map[string]struct{}, len(projector.OpenTurns))
	for _, turn := range projector.OpenTurns {
		seedTurn, found := checkpointSeedOpenTurn(seed, turn.TurnID)
		if sessionID == "" || turn.SessionID != sessionID || !claimCheckpointTurn(seenOpen, turn.TurnID) ||
			!found || turn.StartedAtMS != seedTurn.StartedAtMS ||
			turn.SourceGeneration < 0 || turn.StartOffset < 0 || turn.StartOffset > committedOffset ||
			!validCheckpointOptionalText(turn.Model, maxCheckpointIdentifier) ||
			!validCheckpointOptionalText(turn.ReasoningEffort, maxCheckpointIdentifier) ||
			!validCheckpointOptionalText(turn.CWD, maxCheckpointMetadata) ||
			!validCheckpointEffort(turn.ReasoningEffort) ||
			!projectedTurnMatchesSeedContext(turn, seedTurn.Context) {
			return invalidRecord("projector open turn checkpoint is invalid")
		}
	}
	if projector.Current != nil {
		current := projector.Current
		if sessionID == "" || current.SessionID != sessionID || current.UpdatedAtMS < 0 ||
			(current.ThreadName == nil) != (current.ThreadNameUpdatedAtMS == nil) ||
			!validCheckpointOptionalText(current.ThreadName, maxCheckpointIdentifier) ||
			!validCheckpointOptionalText(current.ActiveTurnID, maxCheckpointIdentifier) ||
			!validCheckpointOptionalText(current.CurrentModel, maxCheckpointIdentifier) ||
			!validCheckpointOptionalText(current.CurrentCWD, maxCheckpointMetadata) ||
			!validCheckpointCounter(current.LastActivityAtMS) {
			return invalidRecord("projector current checkpoint is invalid")
		}
		if current.ThreadNameUpdatedAtMS != nil &&
			(*current.ThreadNameUpdatedAtMS < 0 || *current.ThreadNameUpdatedAtMS > current.UpdatedAtMS) {
			return invalidRecord("projector thread name checkpoint is invalid")
		}
		if current.ActiveTurnID != nil && !checkpointSeedHasOpenTurn(seed, *current.ActiveTurnID) {
			return invalidRecord("projector active turn is not open in parser seed")
		}
	}
	if projector.SessionUsage != nil {
		usage := projector.SessionUsage
		if sessionID == "" || usage.SessionID != sessionID || usage.CounterEpoch < 0 ||
			usage.ObservedAtMS < 0 || usage.SourceGeneration < 0 || usage.SourceOffset < 0 ||
			usage.SourceOffset > committedOffset || usage.CounterState == "" ||
			!validCheckpointCounter(usage.TotalInputTokens) || !validCheckpointCounter(usage.TotalCachedTokens) ||
			!validCheckpointCounter(usage.TotalOutputTokens) || !validCheckpointCounter(usage.TotalReasoningTokens) {
			return invalidRecord("projector usage checkpoint is invalid")
		}
	}
	return nil
}

func checkpointSeedOpenTurnCount(seed *ParserSeedCheckpoint) int {
	if seed == nil {
		return 0
	}
	return len(seed.OpenTurns)
}

func validCheckpointContext(
	context *CheckpointTurnContext,
	sessionID string,
	turnID string,
	startedAtMS int64,
) bool {
	return context == nil || (context.SessionID == sessionID && context.TurnID == turnID &&
		context.ObservedAtMS >= startedAtMS && validCheckpointText(context.CWD, maxCheckpointMetadata, true) &&
		validCheckpointText(context.Model, maxCheckpointIdentifier, true) && validCheckpointEffort(context.Effort))
}

func validCheckpointUsage(
	usage *CheckpointTurnUsage,
	sessionID string,
	turnID string,
	startedAtMS int64,
	wantFinal bool,
) bool {
	return usage == nil || (usage.SessionID == sessionID && usage.TurnID == turnID &&
		usage.ObservedAtMS >= startedAtMS && usage.IsFinal == wantFinal &&
		validCheckpointCounter(usage.InputTokens) && validCheckpointCounter(usage.CachedInputTokens) &&
		validCheckpointCounter(usage.OutputTokens) && validCheckpointCounter(usage.ReasoningTokens) &&
		validCheckpointCounter(usage.ContextWindow))
}

func validCheckpointPendingContext(context *CheckpointPendingContext, committedOffset int64) bool {
	return context == nil || (validCheckpointPosition(context.Position, committedOffset) && context.ObservedAtMS >= 0 &&
		validCheckpointText(context.CWD, maxCheckpointMetadata, true) &&
		validCheckpointText(context.Model, maxCheckpointIdentifier, true) && validCheckpointEffort(context.Effort))
}

func validCheckpointPendingTerminal(terminal *CheckpointPendingTerminal, committedOffset int64) bool {
	return terminal == nil || (validCheckpointPosition(terminal.Position, committedOffset) &&
		terminal.CompletedAtMS >= 0 && validCheckpointOutcome(terminal.Outcome))
}

func validCheckpointPosition(position CheckpointSourcePosition, committedOffset int64) bool {
	return position.StartOffset >= 0 && position.EndOffset > position.StartOffset &&
		position.EndOffset <= committedOffset
}

func claimCheckpointTurn(seen map[string]struct{}, turnID string) bool {
	if !validCheckpointIdentifier(turnID) {
		return false
	}
	if _, exists := seen[turnID]; exists {
		return false
	}
	seen[turnID] = struct{}{}
	return true
}

func checkpointSeedHasOpenTurn(seed *ParserSeedCheckpoint, turnID string) bool {
	_, found := checkpointSeedOpenTurn(seed, turnID)
	return found
}

func checkpointSeedOpenTurn(seed *ParserSeedCheckpoint, turnID string) (CheckpointOpenTurn, bool) {
	if seed == nil {
		return CheckpointOpenTurn{}, false
	}
	for _, turn := range seed.OpenTurns {
		if turn.TurnID == turnID {
			return turn, true
		}
	}
	return CheckpointOpenTurn{}, false
}

func projectedTurnMatchesSeedContext(
	projected ProjectedOpenTurnCheckpoint,
	context *CheckpointTurnContext,
) bool {
	if context == nil {
		return projected.Model == nil && projected.ReasoningEffort == nil && projected.CWD == nil
	}
	return equalStringPointer(projected.Model, &context.Model) &&
		equalStringPointer(projected.ReasoningEffort, context.Effort) &&
		equalStringPointer(projected.CWD, &context.CWD)
}

func validCheckpointIdentifier(value string) bool {
	return validCheckpointText(value, maxCheckpointIdentifier, true)
}

func validCheckpointText(value string, limit int, required bool) bool {
	return (!required || value != "") && len(value) <= limit && utf8.ValidString(value)
}

func validCheckpointOptionalText(value *string, limit int) bool {
	return value == nil || validCheckpointText(*value, limit, true)
}

func validCheckpointCounter(value *int64) bool {
	return value == nil || *value >= 0
}

func validCheckpointEffort(value *string) bool {
	if value == nil {
		return true
	}
	switch *value {
	case "none", "minimal", "low", "medium", "high", "xhigh", "ultra", "custom":
		return true
	default:
		return false
	}
}

func validCheckpointOutcome(value string) bool {
	switch value {
	case "completed", "interrupted", "replaced", "review_ended", "budget_limited":
		return true
	default:
		return false
	}
}
