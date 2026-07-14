package logs

import (
	"fmt"
	"reflect"
	"testing"
)

func TestLifecycleNormalizerCompletesTurnWithFrozenFinalUsage(t *testing.T) {
	t.Parallel()

	normalizer := newLifecycleNormalizer()
	session := applyDecoded(t, normalizer, 0, decodedLine{
		Kind: decodedSessionMeta,
		SessionMeta: &decodedSessionMetaRecord{
			SessionID: "session-1", RootSessionID: "root-1", CreatedAtMS: 1000,
			InitialCWD: "/tmp/project", Originator: "codex_cli_rs", CLIVersion: "0.142.3",
			Source: "cli", ModelProvider: "openai",
		},
	})
	if len(session.Events) != 1 || session.Events[0].Kind != EventSessionMeta || session.Events[0].SessionMeta == nil {
		t.Fatalf("session result = %#v", session)
	}

	started := applyDecoded(t, normalizer, 10, decodedLine{
		Kind:      decodedTurnStart,
		TurnStart: &decodedTurnStartRecord{TurnID: "turn-1", StartedAtMS: 2000, ContextWindow: int64Pointer(258000)},
	})
	if len(started.Events) != 1 || started.Events[0].Kind != EventTurnStarted ||
		started.Events[0].TurnStart == nil || started.Events[0].TurnStart.SessionID != "session-1" {
		t.Fatalf("start result = %#v", started)
	}

	context := applyDecoded(t, normalizer, 20, decodedLine{
		Kind: decodedTurnContext, ObservedAtMS: 2100,
		TurnContext: &decodedTurnContextRecord{
			TurnID: stringPointer("turn-1"), CWD: "/tmp/project", Model: "gpt-5.2-codex",
			Effort: stringPointer("high"),
		},
	})
	if len(context.Events) != 1 || context.Events[0].Kind != EventTurnContext || context.Events[0].TurnContext == nil ||
		context.Events[0].TurnContext.Model != "gpt-5.2-codex" {
		t.Fatalf("context result = %#v", context)
	}

	usageCounters := TokenCounters{
		InputTokens: int64Pointer(10), CachedInputTokens: int64Pointer(2),
		OutputTokens: int64Pointer(3), ReasoningTokens: int64Pointer(1),
	}
	usage := applyDecoded(t, normalizer, 30, decodedLine{
		Kind: decodedTokenUsage,
		TokenUsage: &decodedTokenUsageRecord{
			ObservedAtMS: 2200,
			Total:        TokenCounters{InputTokens: int64Pointer(100), OutputTokens: int64Pointer(30)},
			Last:         usageCounters, ContextWindow: int64Pointer(258000),
		},
	})
	if len(usage.Events) != 2 || usage.Events[0].Kind != EventSessionUsage ||
		usage.Events[1].Kind != EventTurnUsage || usage.Events[1].TurnUsage == nil || usage.Events[1].TurnUsage.IsFinal {
		t.Fatalf("usage result = %#v", usage)
	}

	completed := applyDecoded(t, normalizer, 40, decodedLine{
		Kind: decodedTurnTerminal,
		TurnTerminal: &decodedTurnTerminalRecord{
			TurnID: stringPointer("turn-1"), CompletedAtMS: 3000, Outcome: TurnOutcomeCompleted,
		},
	})
	if len(completed.Diagnostics) != 0 || len(completed.Events) != 1 || completed.Events[0].Kind != EventTurnEnded {
		t.Fatalf("complete result = %#v", completed)
	}
	end := completed.Events[0].TurnEnd
	if end == nil || end.SessionID != "session-1" || end.TurnID != "turn-1" ||
		end.CompletedAtMS != 3000 || end.Outcome != TurnOutcomeCompleted || end.FinalUsage == nil ||
		!end.FinalUsage.IsFinal || !reflect.DeepEqual(end.FinalUsage.Usage, usageCounters) {
		t.Fatalf("turn end = %#v", end)
	}

	// The terminal snapshot must be detached from the normalizer's provisional state.
	*usageCounters.InputTokens = 999
	if *end.FinalUsage.Usage.InputTokens != 10 {
		t.Fatalf("final usage aliases caller memory: %#v", end.FinalUsage)
	}
}

func TestLifecycleNormalizerResolvesPendingContextAndTerminalAfterStart(t *testing.T) {
	t.Parallel()

	normalizer := normalizerWithSession(t)
	context := applyDecoded(t, normalizer, 10, decodedLine{
		Kind: decodedTurnContext, ObservedAtMS: 2100,
		TurnContext: &decodedTurnContextRecord{
			TurnID: stringPointer("turn-late"), CWD: "/tmp/project", Model: "gpt-5.2-codex",
		},
	})
	assertSingleDiagnostic(t, context, DiagnosticMissingTurnStart, 10)

	terminal := applyDecoded(t, normalizer, 20, decodedLine{
		Kind: decodedTurnTerminal,
		TurnTerminal: &decodedTurnTerminalRecord{
			TurnID: stringPointer("turn-late"), CompletedAtMS: 4000, Outcome: TurnOutcomeInterrupted,
		},
	})
	assertSingleDiagnostic(t, terminal, DiagnosticMissingTurnStart, 20)

	started := applyDecoded(t, normalizer, 30, decodedLine{
		Kind:      decodedTurnStart,
		TurnStart: &decodedTurnStartRecord{TurnID: "turn-late", StartedAtMS: 2000},
	})
	if len(started.Diagnostics) != 0 || len(started.Events) != 3 {
		t.Fatalf("resolved result = %#v", started)
	}
	if started.Events[0].Kind != EventTurnStarted || started.Events[0].Position.StartOffset != 30 ||
		started.Events[1].Kind != EventTurnContext || started.Events[1].Position.StartOffset != 10 ||
		started.Events[2].Kind != EventTurnEnded || started.Events[2].Position.StartOffset != 20 {
		t.Fatalf("resolved event order = %#v", started.Events)
	}

	usage := applyDecoded(t, normalizer, 40, decodedLine{
		Kind: decodedTokenUsage,
		TokenUsage: &decodedTokenUsageRecord{
			ObservedAtMS: 5000, Total: TokenCounters{InputTokens: int64Pointer(100)},
			Last: TokenCounters{InputTokens: int64Pointer(10)},
		},
	})
	if len(usage.Events) != 1 || usage.Events[0].Kind != EventSessionUsage {
		t.Fatalf("post-terminal usage events = %#v", usage.Events)
	}
	assertSingleDiagnostic(t, usage, DiagnosticOrphanTurnUsage, 40)
}

func TestLifecycleNormalizerRejectsRegressingPendingTerminal(t *testing.T) {
	t.Parallel()

	normalizer := normalizerWithSession(t)
	terminal := applyDecoded(t, normalizer, 10, decodedLine{
		Kind: decodedTurnTerminal,
		TurnTerminal: &decodedTurnTerminalRecord{
			TurnID: stringPointer("turn-1"), CompletedAtMS: 1000, Outcome: TurnOutcomeCompleted,
		},
	})
	assertSingleDiagnostic(t, terminal, DiagnosticMissingTurnStart, 10)

	started := applyDecoded(t, normalizer, 20, decodedLine{
		Kind:      decodedTurnStart,
		TurnStart: &decodedTurnStartRecord{TurnID: "turn-1", StartedAtMS: 2000},
	})
	if len(started.Events) != 1 || started.Events[0].Kind != EventTurnStarted {
		t.Fatalf("start result = %#v", started)
	}
	assertSingleDiagnostic(t, started, DiagnosticInvalidTransition, 10)
}

func TestLifecycleNormalizerRejectsContextAfterPendingTerminal(t *testing.T) {
	t.Parallel()

	normalizer := normalizerWithSession(t)
	terminal := applyDecoded(t, normalizer, 10, decodedLine{
		Kind: decodedTurnTerminal,
		TurnTerminal: &decodedTurnTerminalRecord{
			TurnID: stringPointer("turn-1"), CompletedAtMS: 2000, Outcome: TurnOutcomeCompleted,
		},
	})
	assertSingleDiagnostic(t, terminal, DiagnosticMissingTurnStart, 10)

	context := applyDecoded(t, normalizer, 20, decodedLine{
		Kind: decodedTurnContext, ObservedAtMS: 1500,
		TurnContext: &decodedTurnContextRecord{
			TurnID: stringPointer("turn-1"), CWD: "/tmp/project", Model: "gpt-5.2-codex",
		},
	})
	assertSingleDiagnostic(t, context, DiagnosticInvalidTransition, 20)

	started := applyDecoded(t, normalizer, 30, decodedLine{
		Kind:      decodedTurnStart,
		TurnStart: &decodedTurnStartRecord{TurnID: "turn-1", StartedAtMS: 1000},
	})
	if len(started.Diagnostics) != 0 || len(started.Events) != 2 ||
		started.Events[0].Kind != EventTurnStarted || started.Events[1].Kind != EventTurnEnded {
		t.Fatalf("resolved result = %#v", started)
	}
}

func TestLifecycleNormalizerBoundsOpenPendingAndClosedTurnState(t *testing.T) {
	t.Parallel()

	open := normalizerWithSession(t)
	for index := 0; index < maxOpenTurnStates; index++ {
		turnID := fmt.Sprintf("open-%d", index)
		result := applyDecoded(t, open, int64(10+index), decodedLine{
			Kind:      decodedTurnStart,
			TurnStart: &decodedTurnStartRecord{TurnID: turnID, StartedAtMS: int64(1000 + index)},
		})
		if len(result.Events) != 1 || len(result.Diagnostics) != 0 {
			t.Fatalf("open turn %d = %#v", index, result)
		}
	}
	overflowContext := applyDecoded(t, open, 9999, decodedLine{
		Kind: decodedTurnContext, ObservedAtMS: 1999,
		TurnContext: &decodedTurnContextRecord{
			TurnID: stringPointer("open-overflow"), CWD: "/tmp/project", Model: "gpt-5.2-codex",
		},
	})
	assertSingleDiagnostic(t, overflowContext, DiagnosticMissingTurnStart, 9999)
	overflowOpen := applyDecoded(t, open, 10000, decodedLine{
		Kind:      decodedTurnStart,
		TurnStart: &decodedTurnStartRecord{TurnID: "open-overflow", StartedAtMS: 2000},
	})
	assertSingleDiagnostic(t, overflowOpen, DiagnosticStateLimitExceeded, 10000)
	if len(open.turns) != maxOpenTurnStates {
		t.Fatalf("open state count = %d, want %d", len(open.turns), maxOpenTurnStates)
	}
	if _, exists := open.pending["open-overflow"]; exists {
		t.Fatal("state-limit rejection retained an unresolvable pending turn")
	}

	pending := normalizerWithSession(t)
	for index := 0; index < maxPendingTurnStates; index++ {
		turnID := fmt.Sprintf("pending-%d", index)
		result := applyDecoded(t, pending, int64(10+index), decodedLine{
			Kind: decodedTurnContext, ObservedAtMS: int64(1000 + index),
			TurnContext: &decodedTurnContextRecord{
				TurnID: stringPointer(turnID), CWD: "/tmp/project", Model: "gpt-5.2-codex",
			},
		})
		assertSingleDiagnostic(t, result, DiagnosticMissingTurnStart, int64(10+index))
	}
	overflowPending := applyDecoded(t, pending, 20000, decodedLine{
		Kind: decodedTurnContext, ObservedAtMS: 3000,
		TurnContext: &decodedTurnContextRecord{
			TurnID: stringPointer("pending-overflow"), CWD: "/tmp/project", Model: "gpt-5.2-codex",
		},
	})
	assertSingleDiagnostic(t, overflowPending, DiagnosticStateLimitExceeded, 20000)
	if len(pending.pending) != maxPendingTurnStates {
		t.Fatalf("pending state count = %d, want %d", len(pending.pending), maxPendingTurnStates)
	}

	closed := normalizerWithSession(t)
	for index := 0; index < maxRetainedClosedTurnStates+17; index++ {
		turnID := fmt.Sprintf("closed-%d", index)
		applyDecoded(t, closed, int64(30000+index*2), decodedLine{
			Kind:      decodedTurnStart,
			TurnStart: &decodedTurnStartRecord{TurnID: turnID, StartedAtMS: int64(4000 + index)},
		})
		terminal := applyDecoded(t, closed, int64(30001+index*2), decodedLine{
			Kind: decodedTurnTerminal,
			TurnTerminal: &decodedTurnTerminalRecord{
				TurnID: stringPointer(turnID), CompletedAtMS: int64(5000 + index), Outcome: TurnOutcomeCompleted,
			},
		})
		if len(terminal.Events) != 1 || len(terminal.Diagnostics) != 0 {
			t.Fatalf("closed turn %d = %#v", index, terminal)
		}
	}
	if len(closed.turns) > maxRetainedClosedTurnStates || len(closed.closedOrder) > maxRetainedClosedTurnStates {
		t.Fatalf("closed state grew without bound: turns=%d order=%d", len(closed.turns), len(closed.closedOrder))
	}
}

func TestLifecycleNormalizerAssociatesMissingTurnIDOnlyWhenUnambiguous(t *testing.T) {
	t.Parallel()

	t.Run("one open turn", func(t *testing.T) {
		t.Parallel()
		normalizer := normalizerWithSession(t)
		applyDecoded(t, normalizer, 10, decodedLine{
			Kind:      decodedTurnStart,
			TurnStart: &decodedTurnStartRecord{TurnID: "turn-1", StartedAtMS: 1000},
		})
		result := applyDecoded(t, normalizer, 20, decodedLine{
			Kind: decodedTurnTerminal,
			TurnTerminal: &decodedTurnTerminalRecord{
				CompletedAtMS: 2000, Outcome: TurnOutcomeReviewEnded,
			},
		})
		if len(result.Diagnostics) != 0 || len(result.Events) != 1 || result.Events[0].TurnEnd == nil ||
			result.Events[0].TurnEnd.TurnID != "turn-1" {
			t.Fatalf("terminal result = %#v", result)
		}
	})

	t.Run("multiple open turns", func(t *testing.T) {
		t.Parallel()
		normalizer := normalizerWithSession(t)
		for index, turnID := range []string{"turn-1", "turn-2"} {
			applyDecoded(t, normalizer, int64(10+index*10), decodedLine{
				Kind:      decodedTurnStart,
				TurnStart: &decodedTurnStartRecord{TurnID: turnID, StartedAtMS: int64(1000 + index)},
			})
		}
		result := applyDecoded(t, normalizer, 30, decodedLine{
			Kind:         decodedTurnTerminal,
			TurnTerminal: &decodedTurnTerminalRecord{CompletedAtMS: 2000, Outcome: TurnOutcomeInterrupted},
		})
		assertSingleDiagnostic(t, result, DiagnosticAmbiguousTurn, 30)
	})
}

func TestLifecycleNormalizerDuplicateAndConflictTransitions(t *testing.T) {
	t.Parallel()

	normalizer := normalizerWithSession(t)
	start := decodedLine{
		Kind:      decodedTurnStart,
		TurnStart: &decodedTurnStartRecord{TurnID: "turn-1", StartedAtMS: 1000, ContextWindow: int64Pointer(100)},
	}
	first := applyDecoded(t, normalizer, 10, start)
	if len(first.Events) != 1 {
		t.Fatalf("first start = %#v", first)
	}
	replay := applyDecoded(t, normalizer, 20, start)
	if len(replay.Events) != 0 || len(replay.Diagnostics) != 0 {
		t.Fatalf("exact replay = %#v", replay)
	}

	conflict := start
	conflict.TurnStart = &decodedTurnStartRecord{TurnID: "turn-1", StartedAtMS: 1001, ContextWindow: int64Pointer(100)}
	assertSingleDiagnostic(t, applyDecoded(t, normalizer, 30, conflict), DiagnosticInvalidTransition, 30)

	terminal := decodedLine{
		Kind: decodedTurnTerminal,
		TurnTerminal: &decodedTurnTerminalRecord{
			TurnID: stringPointer("turn-1"), CompletedAtMS: 2000, Outcome: TurnOutcomeCompleted,
		},
	}
	if got := applyDecoded(t, normalizer, 40, terminal); len(got.Events) != 1 || len(got.Diagnostics) != 0 {
		t.Fatalf("first terminal = %#v", got)
	}
	if got := applyDecoded(t, normalizer, 50, terminal); len(got.Events) != 0 || len(got.Diagnostics) != 0 {
		t.Fatalf("terminal replay = %#v", got)
	}
	terminal.TurnTerminal = &decodedTurnTerminalRecord{
		TurnID: stringPointer("turn-1"), CompletedAtMS: 2001, Outcome: TurnOutcomeCompleted,
	}
	assertSingleDiagnostic(t, applyDecoded(t, normalizer, 60, terminal), DiagnosticInvalidTransition, 60)
}

func TestLifecycleNormalizerRequiresSessionAndUniqueOpenTurnForUsage(t *testing.T) {
	t.Parallel()

	withoutSession := newLifecycleNormalizer()
	missing := applyDecoded(t, withoutSession, 0, decodedLine{
		Kind: decodedTurnStart, TurnStart: &decodedTurnStartRecord{TurnID: "turn-1", StartedAtMS: 1000},
	})
	assertSingleDiagnostic(t, missing, DiagnosticMissingSessionMeta, 0)

	normalizer := normalizerWithSession(t)
	usage := decodedLine{
		Kind: decodedTokenUsage,
		TokenUsage: &decodedTokenUsageRecord{
			ObservedAtMS: 2000, Total: TokenCounters{InputTokens: int64Pointer(100)},
			Last: TokenCounters{InputTokens: int64Pointer(10)},
		},
	}
	noOpen := applyDecoded(t, normalizer, 10, usage)
	if len(noOpen.Events) != 1 || noOpen.Events[0].Kind != EventSessionUsage {
		t.Fatalf("no-open events = %#v", noOpen.Events)
	}
	assertSingleDiagnostic(t, noOpen, DiagnosticOrphanTurnUsage, 10)

	for index, turnID := range []string{"turn-1", "turn-2"} {
		applyDecoded(t, normalizer, int64(20+index*10), decodedLine{
			Kind:      decodedTurnStart,
			TurnStart: &decodedTurnStartRecord{TurnID: turnID, StartedAtMS: int64(3000 + index)},
		})
	}
	ambiguous := applyDecoded(t, normalizer, 40, usage)
	if len(ambiguous.Events) != 1 || ambiguous.Events[0].Kind != EventSessionUsage {
		t.Fatalf("ambiguous events = %#v", ambiguous.Events)
	}
	assertSingleDiagnostic(t, ambiguous, DiagnosticAmbiguousTurn, 40)
}

func TestLifecycleNormalizerRejectsContextAndTurnUsageBeforeStart(t *testing.T) {
	t.Parallel()

	normalizer := normalizerWithSession(t)
	applyDecoded(t, normalizer, 10, decodedLine{
		Kind:      decodedTurnStart,
		TurnStart: &decodedTurnStartRecord{TurnID: "turn-1", StartedAtMS: 2000},
	})
	context := applyDecoded(t, normalizer, 20, decodedLine{
		Kind: decodedTurnContext, ObservedAtMS: 1999,
		TurnContext: &decodedTurnContextRecord{
			TurnID: stringPointer("turn-1"), CWD: "/tmp/project", Model: "gpt-5.2-codex",
		},
	})
	assertSingleDiagnostic(t, context, DiagnosticInvalidTransition, 20)

	usage := applyDecoded(t, normalizer, 30, decodedLine{
		Kind: decodedTokenUsage,
		TokenUsage: &decodedTokenUsageRecord{
			ObservedAtMS: 1999, Total: TokenCounters{InputTokens: int64Pointer(100)},
			Last: TokenCounters{InputTokens: int64Pointer(10)},
		},
	})
	if len(usage.Events) != 1 || usage.Events[0].Kind != EventSessionUsage {
		t.Fatalf("regressing usage events = %#v", usage.Events)
	}
	assertSingleDiagnostic(t, usage, DiagnosticInvalidTransition, 30)
	if state := normalizer.turns["turn-1"]; state.context != nil || state.latestUsage != nil {
		t.Fatalf("regressing facts mutated checkpoint state: %#v", state)
	}
}

func normalizerWithSession(t *testing.T) *lifecycleNormalizer {
	t.Helper()
	normalizer := newLifecycleNormalizer()
	result := applyDecoded(t, normalizer, 0, decodedLine{
		Kind: decodedSessionMeta,
		SessionMeta: &decodedSessionMetaRecord{
			SessionID: "session-1", RootSessionID: "root-1", CreatedAtMS: 500,
			InitialCWD: "/tmp/project", Originator: "codex_cli_rs", CLIVersion: "0.142.3",
			Source: "cli", ModelProvider: "openai",
		},
	})
	if len(result.Events) != 1 || len(result.Diagnostics) != 0 {
		t.Fatalf("session setup = %#v", result)
	}
	return normalizer
}

func applyDecoded(t *testing.T, normalizer *lifecycleNormalizer, startOffset int64, record decodedLine) lifecycleResult {
	t.Helper()
	return normalizer.Apply(
		SourcePosition{StartOffset: startOffset, EndOffset: startOffset + 7},
		&record,
	)
}

func assertSingleDiagnostic(t *testing.T, result lifecycleResult, code DiagnosticCode, startOffset int64) {
	t.Helper()
	if len(result.Diagnostics) != 1 || result.Diagnostics[0].Code != code ||
		result.Diagnostics[0].StartOffset != startOffset {
		t.Fatalf("lifecycle result = %#v, want diagnostic %s at %d", result, code, startOffset)
	}
}

func int64Pointer(value int64) *int64 {
	return &value
}
