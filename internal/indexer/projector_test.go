package indexer

import (
	"errors"
	"reflect"
	"testing"

	"github.com/SisyphusSQ/codex-pulse/internal/codex/logs"
	"github.com/SisyphusSQ/codex-pulse/internal/store"
)

func TestProjectorMapsLifecycleEventsToOrderedFacts(t *testing.T) {
	t.Parallel()

	projector, err := newProjector("source-projector", 3, store.GenerationModeRebuild, nil, store.ProjectorCheckpoint{})
	if err != nil {
		t.Fatalf("newProjector() error = %v", err)
	}
	effort := "high"
	contextWindow := int64(258_000)
	input, output := int64(10), int64(3)
	events := []logs.ParsedEvent{
		{
			Kind: logs.EventSessionMeta, Position: logs.SourcePosition{StartOffset: 0, EndOffset: 10},
			SessionMeta: &logs.SessionMetaFact{
				SessionID: "session-a", RootSessionID: "session-a", SourceKind: logs.SourceKindSession,
				CreatedAtMS: 100, ObservedAtMS: 110, InitialCWD: "/synthetic/project",
				Originator: "codex_cli_rs", CLIVersion: "0.142.3", Source: "cli", ModelProvider: "openai",
			},
		},
		{
			Kind: logs.EventTurnStarted, Position: logs.SourcePosition{StartOffset: 10, EndOffset: 20},
			TurnStart: &logs.TurnStartFact{
				SessionID: "session-a", TurnID: "turn-a", StartedAtMS: 120, ContextWindow: &contextWindow,
			},
		},
		{
			Kind: logs.EventTurnContext, Position: logs.SourcePosition{StartOffset: 20, EndOffset: 30},
			TurnContext: &logs.TurnContextFact{
				SessionID: "session-a", TurnID: "turn-a", ObservedAtMS: 130,
				CWD: "/synthetic/project", Model: "gpt-5", Effort: &effort,
			},
		},
		{
			Kind: logs.EventTurnUsage, Position: logs.SourcePosition{StartOffset: 30, EndOffset: 40},
			TurnUsage: &logs.TurnUsageFact{
				SessionID: "session-a", TurnID: "turn-a", ObservedAtMS: 140,
				Usage:         logs.TokenCounters{InputTokens: &input, OutputTokens: &output},
				ContextWindow: &contextWindow,
			},
		},
		{
			Kind: logs.EventTurnEnded, Position: logs.SourcePosition{StartOffset: 40, EndOffset: 50},
			TurnEnd: &logs.TurnEndFact{
				SessionID: "session-a", TurnID: "turn-a", CompletedAtMS: 150,
				Outcome: logs.TurnOutcomeCompleted,
				FinalUsage: &logs.TurnUsageFact{
					SessionID: "session-a", TurnID: "turn-a", ObservedAtMS: 140,
					Usage:         logs.TokenCounters{InputTokens: &input, OutputTokens: &output},
					ContextWindow: &contextWindow, IsFinal: true,
				},
			},
		},
	}

	facts, checkpoint, err := projector.Project(events)
	if err != nil {
		t.Fatalf("Project() error = %v", err)
	}
	if len(facts) != 5 {
		t.Fatalf("Project() facts = %#v, want five ordered batches", facts)
	}
	if facts[0].Session == nil || facts[0].Session.SessionID != "session-a" ||
		facts[0].Session.SourceKind != "session" || facts[0].Session.InitialCWD == nil ||
		*facts[0].Session.InitialCWD != "/synthetic/project" {
		t.Fatalf("session fact = %#v", facts[0])
	}
	if facts[1].Turn == nil || facts[1].Turn.StartOffset != 10 || facts[1].Turn.SourceGeneration != 3 ||
		facts[1].SessionCurrent == nil || facts[1].SessionCurrent.ActiveTurnID == nil ||
		*facts[1].SessionCurrent.ActiveTurnID != CanonicalTurnID("session-a", "turn-a") {
		t.Fatalf("start fact = %#v", facts[1])
	}
	if facts[2].Turn == nil || facts[2].Turn.Model == nil || *facts[2].Turn.Model != "gpt-5" ||
		facts[2].Turn.ReasoningEffort == nil || *facts[2].Turn.ReasoningEffort != effort {
		t.Fatalf("context fact = %#v", facts[2])
	}
	if facts[3].Usage == nil || facts[3].Usage.IsFinal || facts[3].Usage.SourceOffset != 30 ||
		facts[3].Usage.InputTokens == nil || *facts[3].Usage.InputTokens != input {
		t.Fatalf("provisional usage fact = %#v", facts[3])
	}
	ended := facts[4]
	if ended.Turn == nil || ended.Turn.CompletedAtMS == nil || *ended.Turn.CompletedAtMS != 150 ||
		ended.Turn.CompleteOffset == nil || *ended.Turn.CompleteOffset != 40 ||
		ended.Usage == nil || !ended.Usage.IsFinal || ended.SessionCurrent == nil ||
		ended.SessionCurrent.ActiveTurnID != nil {
		t.Fatalf("terminal fact = %#v", ended)
	}
	if len(checkpoint.OpenTurns) != 0 || checkpoint.Current == nil || checkpoint.Current.ActiveTurnID != nil {
		t.Fatalf("projector checkpoint = %#v, want closed turn and cleared current", checkpoint)
	}
	if facts[4].Session == nil || facts[4].Session.LastSeenAtMS != 150 {
		t.Fatalf("terminal session fact = %#v, want last seen 150", facts[4].Session)
	}
}

func TestCanonicalTurnIDSeparatesSessionLocalTurnIDs(t *testing.T) {
	t.Parallel()

	first := CanonicalTurnID("session-a", "turn-1")
	second := CanonicalTurnID("session-b", "turn-1")
	if first == second || first != CanonicalTurnID("session-a", "turn-1") {
		t.Fatalf("canonical turn IDs are not stable and session-scoped: first=%q second=%q", first, second)
	}
}

func TestProjectorKeepsOverlappingInactiveTurnFactsOutOfSessionCurrent(t *testing.T) {
	t.Parallel()

	projector, err := newProjector("source-overlap", 1, store.GenerationModeRebuild, nil, store.ProjectorCheckpoint{})
	if err != nil {
		t.Fatalf("newProjector() error = %v", err)
	}
	events := []logs.ParsedEvent{
		{
			Kind: logs.EventSessionMeta, Position: logs.SourcePosition{StartOffset: 0, EndOffset: 10},
			SessionMeta: &logs.SessionMetaFact{
				SessionID: "session-overlap", RootSessionID: "session-overlap", SourceKind: logs.SourceKindSession,
				CreatedAtMS: 100, ObservedAtMS: 100,
			},
		},
		{
			Kind: logs.EventTurnStarted, Position: logs.SourcePosition{StartOffset: 10, EndOffset: 20},
			TurnStart: &logs.TurnStartFact{SessionID: "session-overlap", TurnID: "turn-a", StartedAtMS: 110},
		},
		{
			Kind: logs.EventTurnStarted, Position: logs.SourcePosition{StartOffset: 20, EndOffset: 30},
			TurnStart: &logs.TurnStartFact{SessionID: "session-overlap", TurnID: "turn-b", StartedAtMS: 120},
		},
		{
			Kind: logs.EventTurnContext, Position: logs.SourcePosition{StartOffset: 30, EndOffset: 40},
			TurnContext: &logs.TurnContextFact{
				SessionID: "session-overlap", TurnID: "turn-a", ObservedAtMS: 130, Model: "gpt-a",
			},
		},
		{
			Kind: logs.EventTurnEnded, Position: logs.SourcePosition{StartOffset: 40, EndOffset: 50},
			TurnEnd: &logs.TurnEndFact{
				SessionID: "session-overlap", TurnID: "turn-a", CompletedAtMS: 140,
				Outcome: logs.TurnOutcomeCompleted,
			},
		},
	}
	facts, checkpoint, err := projector.Project(events)
	if err != nil {
		t.Fatalf("Project(overlap) error = %v", err)
	}
	if len(facts) != len(events) || facts[3].Turn == nil || facts[3].SessionCurrent != nil ||
		facts[4].Turn == nil || facts[4].SessionCurrent != nil {
		t.Fatalf("inactive overlap facts = %#v", facts)
	}
	if checkpoint.Current == nil || checkpoint.Current.ActiveTurnID == nil ||
		*checkpoint.Current.ActiveTurnID != "turn-b" || len(checkpoint.OpenTurns) != 1 ||
		checkpoint.OpenTurns[0].TurnID != "turn-b" {
		t.Fatalf("overlap checkpoint = %#v", checkpoint)
	}
}

func TestProjectorCounterEpochUsesOnlyKnownDecreases(t *testing.T) {
	t.Parallel()

	previousInput, previousOutput := int64(100), int64(50)
	projector, err := newProjector("source-projector", 2, store.GenerationModeAppend, nil, store.ProjectorCheckpoint{
		SessionUsage: &store.SessionUsageCurrent{
			SessionID: "session-a", CounterEpoch: 7, TotalInputTokens: &previousInput,
			TotalOutputTokens: &previousOutput, ObservedAtMS: 100, SourceGeneration: 2,
			SourceOffset: 10, CounterState: "live",
		},
	})
	if err != nil {
		t.Fatalf("newProjector() error = %v", err)
	}
	input110, input90, output60 := int64(110), int64(90), int64(60)
	events := []logs.ParsedEvent{
		{
			Kind: logs.EventSessionUsage, Position: logs.SourcePosition{StartOffset: 20, EndOffset: 21},
			SessionUsage: &logs.SessionUsageFact{
				SessionID: "session-a", ObservedAtMS: 110,
				Usage: logs.TokenCounters{InputTokens: &input110, OutputTokens: &previousOutput},
			},
		},
		{
			Kind: logs.EventSessionUsage, Position: logs.SourcePosition{StartOffset: 30, EndOffset: 31},
			SessionUsage: &logs.SessionUsageFact{
				SessionID: "session-a", ObservedAtMS: 120,
				Usage: logs.TokenCounters{InputTokens: &input90, OutputTokens: &previousOutput},
			},
		},
		{
			Kind: logs.EventSessionUsage, Position: logs.SourcePosition{StartOffset: 40, EndOffset: 41},
			SessionUsage: &logs.SessionUsageFact{
				SessionID: "session-a", ObservedAtMS: 130,
				Usage: logs.TokenCounters{InputTokens: nil, OutputTokens: &output60},
			},
		},
	}
	facts, checkpoint, err := projector.Project(events)
	if err != nil {
		t.Fatalf("Project() error = %v", err)
	}
	want := []struct {
		epoch int64
		state string
	}{{7, "live"}, {8, "reset"}, {8, "live"}}
	for index, expected := range want {
		usage := facts[index].SessionUsageCurrent
		if usage == nil || usage.CounterEpoch != expected.epoch || usage.CounterState != expected.state {
			t.Fatalf("usage[%d] = %#v, want epoch=%d state=%s", index, usage, expected.epoch, expected.state)
		}
	}
	if checkpoint.SessionUsage == nil || checkpoint.SessionUsage.TotalInputTokens != nil ||
		checkpoint.SessionUsage.CounterEpoch != 8 {
		t.Fatalf("checkpoint usage = %#v, want nullable input without fabricated epoch", checkpoint.SessionUsage)
	}
}

func TestProjectorClampsOutOfOrderEventToSessionObservation(t *testing.T) {
	t.Parallel()

	projector, err := newProjector("source-projector", 1, store.GenerationModeRebuild, nil, store.ProjectorCheckpoint{})
	if err != nil {
		t.Fatalf("newProjector() error = %v", err)
	}
	events := []logs.ParsedEvent{
		{
			Kind: logs.EventSessionMeta, Position: logs.SourcePosition{StartOffset: 0, EndOffset: 10},
			SessionMeta: &logs.SessionMetaFact{
				SessionID: "session-a", RootSessionID: "session-a", SourceKind: logs.SourceKindSession,
				CreatedAtMS: 100, ObservedAtMS: 110, InitialCWD: "/synthetic", Originator: "cli",
			},
		},
		{
			Kind: logs.EventTurnStarted, Position: logs.SourcePosition{StartOffset: 10, EndOffset: 20},
			TurnStart: &logs.TurnStartFact{
				SessionID: "session-a", TurnID: "turn-a", StartedAtMS: 105,
			},
		},
	}

	facts, _, err := projector.Project(events)
	if err != nil {
		t.Fatalf("Project() error = %v", err)
	}
	if len(facts) != 2 || facts[1].Session == nil || facts[1].Session.FirstSeenAtMS != 110 ||
		facts[1].Session.LastSeenAtMS != 110 {
		t.Fatalf("out-of-order session fact = %#v, want monotonic interval at 110", facts)
	}
	if facts[1].Turn == nil || facts[1].Turn.StartedAtMS != 105 {
		t.Fatalf("turn fact = %#v, want original event timestamp preserved", facts[1].Turn)
	}
}

func TestProjectorRestoresOpenTurnSourcePosition(t *testing.T) {
	t.Parallel()

	active := "turn-a"
	seed := &logs.ParserSeed{
		Session: &logs.SessionMetaFact{
			SessionID: "session-a", RootSessionID: "session-a", SourceKind: logs.SourceKindSession,
			CreatedAtMS: 1, ObservedAtMS: 1, InitialCWD: "/synthetic", Originator: "cli",
			CLIVersion: "1", Source: "cli",
		},
		OpenTurns: []logs.OpenTurnSeed{{TurnID: "turn-a", StartedAtMS: 10}},
	}
	checkpoint := store.ProjectorCheckpoint{
		OpenTurns: []store.ProjectedOpenTurnCheckpoint{{
			TurnID: "turn-a", SessionID: "session-a", StartedAtMS: 10,
			SourceGeneration: 4, StartOffset: 90,
		}},
		Current: &store.SessionCurrent{SessionID: "session-a", ActiveTurnID: &active, UpdatedAtMS: 10},
	}
	projector, err := newProjector("source-projector", 4, store.GenerationModeAppend, seed, checkpoint)
	if err != nil {
		t.Fatalf("newProjector() error = %v", err)
	}
	facts, next, err := projector.Project([]logs.ParsedEvent{{
		Kind: logs.EventTurnEnded, Position: logs.SourcePosition{StartOffset: 20, EndOffset: 30},
		TurnEnd: &logs.TurnEndFact{
			SessionID: "session-a", TurnID: "turn-a", CompletedAtMS: 20,
			Outcome: logs.TurnOutcomeInterrupted,
		},
	}})
	if err != nil {
		t.Fatalf("Project() error = %v", err)
	}
	if len(facts) != 1 || facts[0].Turn == nil || facts[0].Turn.StartOffset != 90 ||
		facts[0].Turn.CompleteOffset == nil || *facts[0].Turn.CompleteOffset != 20 {
		t.Fatalf("terminal fact = %#v, want restored start offset 90 and terminal offset 20", facts)
	}
	if len(next.OpenTurns) != 0 {
		t.Fatalf("next open turns = %#v, want empty", next.OpenTurns)
	}

	bad := checkpoint
	bad.OpenTurns[0].TurnID = "other"
	if _, err := newProjector("source-projector", 4, store.GenerationModeAppend, seed, bad); !errors.Is(err, ErrInvalidProjection) {
		t.Fatalf("newProjector(mismatched open state) error = %v, want ErrInvalidProjection", err)
	}
}

func TestCloneProjectorCheckpointDoesNotAliasCaller(t *testing.T) {
	t.Parallel()

	model := "gpt-5"
	checkpoint := store.ProjectorCheckpoint{OpenTurns: []store.ProjectedOpenTurnCheckpoint{{
		TurnID: "turn-a", SessionID: "session-a", StartedAtMS: 1,
		SourceGeneration: 1, StartOffset: 2, Model: &model,
	}}}
	clone := cloneProjectorCheckpoint(checkpoint)
	*checkpoint.OpenTurns[0].Model = "mutated"
	if reflect.DeepEqual(clone, checkpoint) || clone.OpenTurns[0].Model == nil || *clone.OpenTurns[0].Model != "gpt-5" {
		t.Fatalf("cloneProjectorCheckpoint() = %#v, aliases caller %#v", clone, checkpoint)
	}
}
