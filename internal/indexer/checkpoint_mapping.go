package indexer

import (
	"github.com/SisyphusSQ/codex-pulse/internal/codex/logs"
	"github.com/SisyphusSQ/codex-pulse/internal/store"
)

func parserSeedToCheckpoint(seed *logs.ParserSeed) *store.ParserSeedCheckpoint {
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

func parserSeedFromCheckpoint(checkpoint *store.ParserSeedCheckpoint) *logs.ParserSeed {
	if checkpoint == nil {
		return nil
	}
	seed := &logs.ParserSeed{
		OpenTurns:    make([]logs.OpenTurnSeed, len(checkpoint.OpenTurns)),
		PendingTurns: make([]logs.PendingTurnSeed, len(checkpoint.PendingTurns)),
		ClosedTurns:  make([]logs.ClosedTurnSeed, len(checkpoint.ClosedTurns)),
	}
	if checkpoint.Session != nil {
		seed.Session = parserSessionFromCheckpoint(checkpoint.Session)
	}
	for index, turn := range checkpoint.OpenTurns {
		seed.OpenTurns[index] = logs.OpenTurnSeed{
			TurnID: turn.TurnID, StartedAtMS: turn.StartedAtMS,
			ContextWindow: cloneInt64(turn.ContextWindow),
			Context:       parserContextFromCheckpoint(turn.Context),
			LatestUsage:   parserUsageFromCheckpoint(turn.LatestUsage),
		}
	}
	for index, turn := range checkpoint.PendingTurns {
		seed.PendingTurns[index] = logs.PendingTurnSeed{TurnID: turn.TurnID}
		if turn.Context != nil {
			seed.PendingTurns[index].Context = &logs.PendingTurnContextSeed{
				Position:     parserPositionFromCheckpoint(turn.Context.Position),
				ObservedAtMS: turn.Context.ObservedAtMS, CWD: turn.Context.CWD,
				Model: turn.Context.Model, Effort: cloneString(turn.Context.Effort),
			}
		}
		if turn.Terminal != nil {
			seed.PendingTurns[index].Terminal = &logs.PendingTurnTerminalSeed{
				Position:      parserPositionFromCheckpoint(turn.Terminal.Position),
				CompletedAtMS: turn.Terminal.CompletedAtMS,
				Outcome:       logs.TurnOutcome(turn.Terminal.Outcome),
			}
		}
	}
	for index, turn := range checkpoint.ClosedTurns {
		seed.ClosedTurns[index] = logs.ClosedTurnSeed{
			TurnID: turn.TurnID, StartedAtMS: turn.StartedAtMS,
			ContextWindow: cloneInt64(turn.ContextWindow),
			Terminal:      parserEndFromCheckpoint(turn.Terminal),
		}
	}
	return seed
}

func checkpointSessionFromParser(value *logs.SessionMetaFact) *store.CheckpointSessionMeta {
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

func parserSessionFromCheckpoint(value *store.CheckpointSessionMeta) *logs.SessionMetaFact {
	if value == nil {
		return nil
	}
	return &logs.SessionMetaFact{
		SessionID: value.SessionID, RootSessionID: value.RootSessionID,
		SourceKind: logs.SourceKind(value.SourceKind), CreatedAtMS: value.CreatedAtMS,
		ObservedAtMS: value.ObservedAtMS, InitialCWD: value.InitialCWD,
		Originator: value.Originator, CLIVersion: value.CLIVersion,
		Source: value.Source, ModelProvider: value.ModelProvider,
	}
}

func checkpointContextFromParser(value *logs.TurnContextFact) *store.CheckpointTurnContext {
	if value == nil {
		return nil
	}
	return &store.CheckpointTurnContext{
		SessionID: value.SessionID, TurnID: value.TurnID, ObservedAtMS: value.ObservedAtMS,
		CWD: value.CWD, Model: value.Model, Effort: cloneString(value.Effort),
	}
}

func parserContextFromCheckpoint(value *store.CheckpointTurnContext) *logs.TurnContextFact {
	if value == nil {
		return nil
	}
	return &logs.TurnContextFact{
		SessionID: value.SessionID, TurnID: value.TurnID, ObservedAtMS: value.ObservedAtMS,
		CWD: value.CWD, Model: value.Model, Effort: cloneString(value.Effort),
	}
}

func checkpointUsageFromParser(value *logs.TurnUsageFact) *store.CheckpointTurnUsage {
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

func parserUsageFromCheckpoint(value *store.CheckpointTurnUsage) *logs.TurnUsageFact {
	if value == nil {
		return nil
	}
	return &logs.TurnUsageFact{
		SessionID: value.SessionID, TurnID: value.TurnID, ObservedAtMS: value.ObservedAtMS,
		Usage: logs.TokenCounters{
			InputTokens: cloneInt64(value.InputTokens), CachedInputTokens: cloneInt64(value.CachedInputTokens),
			OutputTokens: cloneInt64(value.OutputTokens), ReasoningTokens: cloneInt64(value.ReasoningTokens),
		},
		ContextWindow: cloneInt64(value.ContextWindow), IsFinal: value.IsFinal,
	}
}

func checkpointEndFromParser(value logs.TurnEndFact) store.CheckpointTurnEnd {
	return store.CheckpointTurnEnd{
		SessionID: value.SessionID, TurnID: value.TurnID, CompletedAtMS: value.CompletedAtMS,
		Outcome: string(value.Outcome), FinalUsage: checkpointUsageFromParser(value.FinalUsage),
	}
}

func parserEndFromCheckpoint(value store.CheckpointTurnEnd) logs.TurnEndFact {
	return logs.TurnEndFact{
		SessionID: value.SessionID, TurnID: value.TurnID, CompletedAtMS: value.CompletedAtMS,
		Outcome: logs.TurnOutcome(value.Outcome), FinalUsage: parserUsageFromCheckpoint(value.FinalUsage),
	}
}

func checkpointPositionFromParser(value logs.SourcePosition) store.CheckpointSourcePosition {
	return store.CheckpointSourcePosition{StartOffset: value.StartOffset, EndOffset: value.EndOffset}
}

func parserPositionFromCheckpoint(value store.CheckpointSourcePosition) logs.SourcePosition {
	return logs.SourcePosition{StartOffset: value.StartOffset, EndOffset: value.EndOffset}
}
