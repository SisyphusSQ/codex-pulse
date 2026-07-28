package indexer

import (
	logparser "github.com/SisyphusSQ/codex-pulse/internal/codex/logs/parser"
	logsource "github.com/SisyphusSQ/codex-pulse/internal/codex/logs/source"
	"github.com/SisyphusSQ/codex-pulse/internal/store"
)

func parserSeedToCheckpoint(seed *logparser.ParserSeed) *store.ParserSeedCheckpoint {
	if seed == nil {
		return nil
	}
	checkpoint := &store.ParserSeedCheckpoint{
		OpenTurns:    make([]store.CheckpointOpenTurn, len(seed.OpenTurns)),
		PendingTurns: make([]store.CheckpointPendingTurn, len(seed.PendingTurns)),
		ClosedTurns:  make([]store.CheckpointClosedTurn, len(seed.ClosedTurns)),
	}
	if seed.Session != nil {
		checkpoint.Session = checkpointSessionFromParser(seed.Session)
	}
	for index, turn := range seed.OpenTurns {
		checkpoint.OpenTurns[index] = store.CheckpointOpenTurn{
			TurnID: turn.TurnID, StartedAtMS: turn.StartedAtMS,
			ContextWindow: cloneInt64(turn.ContextWindow),
			Context:       checkpointContextFromParser(turn.Context),
			LatestUsage:   checkpointUsageFromParser(turn.LatestUsage),
		}
	}
	for index, turn := range seed.PendingTurns {
		checkpoint.PendingTurns[index] = store.CheckpointPendingTurn{TurnID: turn.TurnID}
		if turn.Context != nil {
			checkpoint.PendingTurns[index].Context = &store.CheckpointPendingContext{
				Position:     checkpointPositionFromParser(turn.Context.Position),
				ObservedAtMS: turn.Context.ObservedAtMS, CWD: turn.Context.CWD,
				Model: turn.Context.Model, Effort: cloneString(turn.Context.Effort),
			}
		}
		if turn.Terminal != nil {
			checkpoint.PendingTurns[index].Terminal = &store.CheckpointPendingTerminal{
				Position:      checkpointPositionFromParser(turn.Terminal.Position),
				CompletedAtMS: turn.Terminal.CompletedAtMS, Outcome: string(turn.Terminal.Outcome),
			}
		}
	}
	for index, turn := range seed.ClosedTurns {
		checkpoint.ClosedTurns[index] = store.CheckpointClosedTurn{
			TurnID: turn.TurnID, StartedAtMS: turn.StartedAtMS,
			ContextWindow: cloneInt64(turn.ContextWindow),
			Terminal:      checkpointEndFromParser(turn.Terminal),
		}
	}
	return checkpoint
}

func parserSeedFromCheckpoint(checkpoint *store.ParserSeedCheckpoint) *logparser.ParserSeed {
	if checkpoint == nil {
		return nil
	}
	seed := &logparser.ParserSeed{
		OpenTurns:    make([]logparser.OpenTurnSeed, len(checkpoint.OpenTurns)),
		PendingTurns: make([]logparser.PendingTurnSeed, len(checkpoint.PendingTurns)),
		ClosedTurns:  make([]logparser.ClosedTurnSeed, len(checkpoint.ClosedTurns)),
	}
	if checkpoint.Session != nil {
		seed.Session = parserSessionFromCheckpoint(checkpoint.Session)
	}
	for index, turn := range checkpoint.OpenTurns {
		seed.OpenTurns[index] = logparser.OpenTurnSeed{
			TurnID: turn.TurnID, StartedAtMS: turn.StartedAtMS,
			ContextWindow: cloneInt64(turn.ContextWindow),
			Context:       parserContextFromCheckpoint(turn.Context),
			LatestUsage:   parserUsageFromCheckpoint(turn.LatestUsage),
		}
	}
	for index, turn := range checkpoint.PendingTurns {
		seed.PendingTurns[index] = logparser.PendingTurnSeed{TurnID: turn.TurnID}
		if turn.Context != nil {
			seed.PendingTurns[index].Context = &logparser.PendingTurnContextSeed{
				Position:     parserPositionFromCheckpoint(turn.Context.Position),
				ObservedAtMS: turn.Context.ObservedAtMS, CWD: turn.Context.CWD,
				Model: turn.Context.Model, Effort: cloneString(turn.Context.Effort),
			}
		}
		if turn.Terminal != nil {
			seed.PendingTurns[index].Terminal = &logparser.PendingTurnTerminalSeed{
				Position:      parserPositionFromCheckpoint(turn.Terminal.Position),
				CompletedAtMS: turn.Terminal.CompletedAtMS,
				Outcome:       logparser.TurnOutcome(turn.Terminal.Outcome),
			}
		}
	}
	for index, turn := range checkpoint.ClosedTurns {
		seed.ClosedTurns[index] = logparser.ClosedTurnSeed{
			TurnID: turn.TurnID, StartedAtMS: turn.StartedAtMS,
			ContextWindow: cloneInt64(turn.ContextWindow),
			Terminal:      parserEndFromCheckpoint(turn.Terminal),
		}
	}
	return seed
}

func checkpointSessionFromParser(value *logparser.SessionMetaFact) *store.CheckpointSessionMeta {
	if value == nil {
		return nil
	}
	return &store.CheckpointSessionMeta{
		SessionID: value.SessionID, RootSessionID: value.RootSessionID,
		SourceKind: string(value.SourceKind), CreatedAtMS: value.CreatedAtMS,
		ObservedAtMS: value.ObservedAtMS, InitialCWD: value.InitialCWD,
		Originator: value.Originator, CLIVersion: value.CLIVersion,
		Source: value.Source, ModelProvider: value.ModelProvider,
	}
}

func parserSessionFromCheckpoint(value *store.CheckpointSessionMeta) *logparser.SessionMetaFact {
	if value == nil {
		return nil
	}
	return &logparser.SessionMetaFact{
		SessionID: value.SessionID, RootSessionID: value.RootSessionID,
		SourceKind: logsource.SourceKind(value.SourceKind), CreatedAtMS: value.CreatedAtMS,
		ObservedAtMS: value.ObservedAtMS, InitialCWD: value.InitialCWD,
		Originator: value.Originator, CLIVersion: value.CLIVersion,
		Source: value.Source, ModelProvider: value.ModelProvider,
	}
}

func checkpointContextFromParser(value *logparser.TurnContextFact) *store.CheckpointTurnContext {
	if value == nil {
		return nil
	}
	return &store.CheckpointTurnContext{
		SessionID: value.SessionID, TurnID: value.TurnID, ObservedAtMS: value.ObservedAtMS,
		CWD: value.CWD, Model: value.Model, Effort: cloneString(value.Effort),
	}
}

func parserContextFromCheckpoint(value *store.CheckpointTurnContext) *logparser.TurnContextFact {
	if value == nil {
		return nil
	}
	return &logparser.TurnContextFact{
		SessionID: value.SessionID, TurnID: value.TurnID, ObservedAtMS: value.ObservedAtMS,
		CWD: value.CWD, Model: value.Model, Effort: cloneString(value.Effort),
	}
}

func checkpointUsageFromParser(value *logparser.TurnUsageFact) *store.CheckpointTurnUsage {
	if value == nil {
		return nil
	}
	return &store.CheckpointTurnUsage{
		SessionID: value.SessionID, TurnID: value.TurnID, ObservedAtMS: value.ObservedAtMS,
		InputTokens:       cloneInt64(value.Usage.InputTokens),
		CachedInputTokens: cloneInt64(value.Usage.CachedInputTokens),
		OutputTokens:      cloneInt64(value.Usage.OutputTokens),
		ReasoningTokens:   cloneInt64(value.Usage.ReasoningTokens),
		ContextWindow:     cloneInt64(value.ContextWindow), IsFinal: value.IsFinal,
	}
}

func parserUsageFromCheckpoint(value *store.CheckpointTurnUsage) *logparser.TurnUsageFact {
	if value == nil {
		return nil
	}
	return &logparser.TurnUsageFact{
		SessionID: value.SessionID, TurnID: value.TurnID, ObservedAtMS: value.ObservedAtMS,
		Usage: logparser.TokenCounters{
			InputTokens: cloneInt64(value.InputTokens), CachedInputTokens: cloneInt64(value.CachedInputTokens),
			OutputTokens: cloneInt64(value.OutputTokens), ReasoningTokens: cloneInt64(value.ReasoningTokens),
		},
		ContextWindow: cloneInt64(value.ContextWindow), IsFinal: value.IsFinal,
	}
}

func checkpointEndFromParser(value logparser.TurnEndFact) store.CheckpointTurnEnd {
	return store.CheckpointTurnEnd{
		SessionID: value.SessionID, TurnID: value.TurnID, CompletedAtMS: value.CompletedAtMS,
		Outcome: string(value.Outcome), FinalUsage: checkpointUsageFromParser(value.FinalUsage),
	}
}

func parserEndFromCheckpoint(value store.CheckpointTurnEnd) logparser.TurnEndFact {
	return logparser.TurnEndFact{
		SessionID: value.SessionID, TurnID: value.TurnID, CompletedAtMS: value.CompletedAtMS,
		Outcome: logparser.TurnOutcome(value.Outcome), FinalUsage: parserUsageFromCheckpoint(value.FinalUsage),
	}
}

func checkpointPositionFromParser(value logparser.SourcePosition) store.CheckpointSourcePosition {
	return store.CheckpointSourcePosition{StartOffset: value.StartOffset, EndOffset: value.EndOffset}
}

func parserPositionFromCheckpoint(value store.CheckpointSourcePosition) logparser.SourcePosition {
	return logparser.SourcePosition{StartOffset: value.StartOffset, EndOffset: value.EndOffset}
}
