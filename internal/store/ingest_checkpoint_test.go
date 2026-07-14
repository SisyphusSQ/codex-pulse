package store

import (
	"errors"
	"fmt"
	"reflect"
	"testing"
)

func TestParserCheckpointCodecRoundTripsCompleteSafeState(t *testing.T) {
	t.Parallel()

	zero := int64(0)
	ten := int64(10)
	effort := "ultra"
	activeTurnID := "turn-open"
	checkpoint := ParserCheckpoint{
		Version: ParserCheckpointVersion, ParserVersion: "codex-rollout-v1", CommittedOffset: 500,
		Seed: &ParserSeedCheckpoint{
			Session: &CheckpointSessionMeta{
				SessionID: "session-1", RootSessionID: "root-1", SourceKind: "session",
				CreatedAtMS: 100, ObservedAtMS: 110, InitialCWD: "/synthetic/project",
				Originator: "codex-cli", CLIVersion: "0.142.3", Source: "cli",
				ModelProvider: "openai",
			},
			OpenTurns: []CheckpointOpenTurn{{
				TurnID: "turn-open", StartedAtMS: 120, ContextWindow: &zero,
				Context: &CheckpointTurnContext{
					SessionID: "session-1", TurnID: "turn-open", ObservedAtMS: 130,
					CWD: "/synthetic/project", Model: "gpt-5", Effort: &effort,
				},
				LatestUsage: &CheckpointTurnUsage{
					SessionID: "session-1", TurnID: "turn-open", ObservedAtMS: 140,
					InputTokens: &zero, CachedInputTokens: nil, OutputTokens: &ten,
					ReasoningTokens: &zero, ContextWindow: &ten, IsFinal: false,
				},
			}},
			PendingTurns: []CheckpointPendingTurn{{
				TurnID: "turn-pending",
				Context: &CheckpointPendingContext{
					Position:     CheckpointSourcePosition{StartOffset: 200, EndOffset: 240},
					ObservedAtMS: 150, CWD: "/synthetic/project", Model: "gpt-5", Effort: &effort,
				},
				Terminal: &CheckpointPendingTerminal{
					Position:      CheckpointSourcePosition{StartOffset: 250, EndOffset: 280},
					CompletedAtMS: 180, Outcome: "interrupted",
				},
			}},
			ClosedTurns: []CheckpointClosedTurn{{
				TurnID: "turn-closed", StartedAtMS: 190, ContextWindow: &ten,
				Terminal: CheckpointTurnEnd{
					SessionID: "session-1", TurnID: "turn-closed", CompletedAtMS: 220,
					Outcome: "completed",
					FinalUsage: &CheckpointTurnUsage{
						SessionID: "session-1", TurnID: "turn-closed", ObservedAtMS: 210,
						InputTokens: &ten, CachedInputTokens: &zero, OutputTokens: &ten,
						ReasoningTokens: nil, ContextWindow: &ten, IsFinal: true,
					},
				},
			}},
		},
		Projector: ProjectorCheckpoint{
			SessionSourceKind: "session",
			OpenTurns: []ProjectedOpenTurnCheckpoint{{
				TurnID: "turn-open", SessionID: "session-1", StartedAtMS: 120,
				SourceGeneration: 3, StartOffset: 100, Model: stringPointerValue("gpt-5"),
				ReasoningEffort: &effort, CWD: stringPointerValue("/synthetic/project"),
			}},
			Current: &SessionCurrent{
				SessionID: "session-1", ActiveTurnID: &activeTurnID, CurrentModel: stringPointerValue("gpt-5"),
				CurrentCWD: stringPointerValue("/synthetic/project"), LastActivityAtMS: int64PointerValue(220), UpdatedAtMS: 220,
			},
			SessionUsage: &SessionUsageCurrent{
				SessionID: "session-1", CounterEpoch: 2, TotalInputTokens: &ten,
				TotalCachedTokens: &zero, TotalOutputTokens: nil, TotalReasoningTokens: &zero,
				ObservedAtMS: 215, SourceGeneration: 3, SourceOffset: 450, CounterState: "live",
			},
		},
	}

	seed, projector, err := marshalParserCheckpoint(checkpoint)
	if err != nil {
		t.Fatalf("marshalParserCheckpoint() error = %v", err)
	}
	got, err := unmarshalParserCheckpoint(
		checkpoint.Version, checkpoint.ParserVersion, checkpoint.CommittedOffset, seed, projector,
	)
	if err != nil {
		t.Fatalf("unmarshalParserCheckpoint() error = %v", err)
	}
	if !reflect.DeepEqual(got, checkpoint) {
		t.Fatalf("checkpoint round trip differs:\ngot:  %#v\nwant: %#v", got, checkpoint)
	}
}

func TestParserCheckpointCodecRejectsCrossStateDuplicateTurn(t *testing.T) {
	t.Parallel()

	checkpoint := ParserCheckpoint{
		Version: ParserCheckpointVersion, ParserVersion: "codex-rollout-v1", CommittedOffset: 100,
		Seed: &ParserSeedCheckpoint{
			Session: &CheckpointSessionMeta{
				SessionID: "session-1", RootSessionID: "root-1", SourceKind: "session",
				CreatedAtMS: 1, ObservedAtMS: 1, InitialCWD: "/synthetic",
				Originator: "codex-cli", CLIVersion: "0.142.3", Source: "cli",
			},
			OpenTurns: []CheckpointOpenTurn{{TurnID: "turn-1", StartedAtMS: 2}},
			PendingTurns: []CheckpointPendingTurn{{
				TurnID: "turn-1",
				Terminal: &CheckpointPendingTerminal{
					Position:      CheckpointSourcePosition{StartOffset: 10, EndOffset: 20},
					CompletedAtMS: 3, Outcome: "completed",
				},
			}},
		},
	}
	if _, _, err := marshalParserCheckpoint(checkpoint); !errors.Is(err, ErrInvalidRecord) {
		t.Fatalf("marshalParserCheckpoint(duplicate turn) error = %v, want ErrInvalidRecord", err)
	}
}

func TestParserCheckpointCodecRejectsProjectorOpenTurnDrift(t *testing.T) {
	t.Parallel()

	model := "gpt-5"
	otherModel := "gpt-4"
	checkpoint := ParserCheckpoint{
		Version: ParserCheckpointVersion, ParserVersion: "codex-rollout-v1", CommittedOffset: 100,
		Seed: &ParserSeedCheckpoint{
			Session: &CheckpointSessionMeta{
				SessionID: "session-1", RootSessionID: "root-1", SourceKind: "session",
				CreatedAtMS: 1, ObservedAtMS: 1, InitialCWD: "/synthetic",
				Originator: "codex-cli", CLIVersion: "0.142.3", Source: "cli",
			},
			OpenTurns: []CheckpointOpenTurn{{
				TurnID: "turn-1", StartedAtMS: 2,
				Context: &CheckpointTurnContext{
					SessionID: "session-1", TurnID: "turn-1", ObservedAtMS: 3,
					CWD: "/synthetic", Model: model,
				},
			}},
		},
		Projector: ProjectorCheckpoint{OpenTurns: []ProjectedOpenTurnCheckpoint{{
			TurnID: "turn-1", SessionID: "session-1", StartedAtMS: 2,
			SourceGeneration: 0, StartOffset: 10, Model: &otherModel,
			CWD: stringPointerValue("/synthetic"),
		}}},
	}
	if _, _, err := marshalParserCheckpoint(checkpoint); !errors.Is(err, ErrInvalidRecord) {
		t.Fatalf("marshalParserCheckpoint(drift) error = %v, want ErrInvalidRecord", err)
	}
}

func TestParserCheckpointCodecRejectsInvalidBoundsAndOpaqueFields(t *testing.T) {
	t.Parallel()

	valid := func() ParserCheckpoint {
		return ParserCheckpoint{
			Version: ParserCheckpointVersion, ParserVersion: "codex-rollout-v1", CommittedOffset: 100,
			Seed: &ParserSeedCheckpoint{Session: &CheckpointSessionMeta{
				SessionID: "session-1", RootSessionID: "root-1", SourceKind: "session",
				CreatedAtMS: 1, ObservedAtMS: 1, InitialCWD: "/synthetic",
				Originator: "cli", CLIVersion: "1", Source: "cli",
			}},
		}
	}
	cases := []struct {
		name   string
		mutate func(*ParserCheckpoint)
	}{
		{name: "version", mutate: func(value *ParserCheckpoint) { value.Version++ }},
		{name: "source kind", mutate: func(value *ParserCheckpoint) { value.Seed.Session.SourceKind = "session_index" }},
		{name: "offset zero with seed", mutate: func(value *ParserCheckpoint) { value.CommittedOffset = 0 }},
		{name: "pending offset beyond checkpoint", mutate: func(value *ParserCheckpoint) {
			value.Seed.PendingTurns = []CheckpointPendingTurn{{
				TurnID: "turn-pending", Terminal: &CheckpointPendingTerminal{
					Position:      CheckpointSourcePosition{StartOffset: 90, EndOffset: 101},
					CompletedAtMS: 2, Outcome: "completed",
				},
			}}
		}},
		{name: "open state limit", mutate: func(value *ParserCheckpoint) {
			for index := 0; index <= maxCheckpointOpenTurns; index++ {
				turnID := fmt.Sprintf("turn-%d", index)
				value.Seed.OpenTurns = append(value.Seed.OpenTurns, CheckpointOpenTurn{
					TurnID: turnID, StartedAtMS: int64(index + 2),
				})
				value.Projector.OpenTurns = append(value.Projector.OpenTurns, ProjectedOpenTurnCheckpoint{
					TurnID: turnID, SessionID: "session-1", StartedAtMS: int64(index + 2),
					SourceGeneration: 0, StartOffset: int64(index),
				})
			}
		}},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			checkpoint := valid()
			testCase.mutate(&checkpoint)
			if _, _, err := marshalParserCheckpoint(checkpoint); !errors.Is(err, ErrInvalidRecord) {
				t.Fatalf("marshalParserCheckpoint() error = %v, want ErrInvalidRecord", err)
			}
		})
	}

	projector := []byte(`{"open_turns":null,"current":null,"session_usage":null}`)
	seedWithOpaqueField := []byte(`{"session":null,"open_turns":[],"pending_turns":[],"closed_turns":[],"raw_json":"secret"}`)
	if _, err := unmarshalParserCheckpoint(
		ParserCheckpointVersion, "codex-rollout-v1", 1, seedWithOpaqueField, projector,
	); !errors.Is(err, ErrInvalidRecord) {
		t.Fatalf("unmarshalParserCheckpoint(opaque field) error = %v, want ErrInvalidRecord", err)
	}
	if _, err := unmarshalParserCheckpoint(
		ParserCheckpointVersion, "codex-rollout-v1", 1,
		make([]byte, maxParserCheckpointBytes+1), projector,
	); !errors.Is(err, ErrInvalidRecord) {
		t.Fatalf("unmarshalParserCheckpoint(oversized) error = %v, want ErrInvalidRecord", err)
	}
}

func TestParserCheckpointRequiresCanonicalSessionSourceKind(t *testing.T) {
	t.Parallel()

	checkpoint := ParserCheckpoint{
		Version: ParserCheckpointVersion, ParserVersion: "codex-rollout-v1", CommittedOffset: 10,
		Seed: &ParserSeedCheckpoint{Session: &CheckpointSessionMeta{
			SessionID: "session-a", RootSessionID: "session-a", SourceKind: "session",
			CreatedAtMS: 1, ObservedAtMS: 1, InitialCWD: "/synthetic",
			Originator: "cli", CLIVersion: "1", Source: "cli",
		}},
	}
	if _, _, err := marshalParserCheckpoint(checkpoint); !errors.Is(err, ErrInvalidRecord) {
		t.Fatalf("marshalParserCheckpoint(missing canonical source kind) error = %v, want ErrInvalidRecord", err)
	}
	checkpoint.Projector.SessionSourceKind = "session"
	if _, _, err := marshalParserCheckpoint(checkpoint); err != nil {
		t.Fatalf("marshalParserCheckpoint(canonical source kind) error = %v", err)
	}
}

func stringPointerValue(value string) *string { return &value }

func int64PointerValue(value int64) *int64 { return &value }
