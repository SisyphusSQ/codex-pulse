package indexer

import (
	"reflect"
	"testing"

	"github.com/SisyphusSQ/codex-pulse/internal/codex/logs"
)

func TestParserSeedCheckpointMappingRoundTripsAllSafeFields(t *testing.T) {
	t.Parallel()

	zero, ten := int64(0), int64(10)
	effort := "high"
	seed := &logs.ParserSeed{
		Session: &logs.SessionMetaFact{
			SessionID: "session-a", RootSessionID: "root-a", SourceKind: logs.SourceKindArchivedSession,
			CreatedAtMS: 1, ObservedAtMS: 2, InitialCWD: "/synthetic", Originator: "cli",
			CLIVersion: "1", Source: "subagent", ModelProvider: "openai",
		},
		OpenTurns: []logs.OpenTurnSeed{{
			TurnID: "turn-open", StartedAtMS: 3, ContextWindow: &zero,
			Context: &logs.TurnContextFact{
				SessionID: "session-a", TurnID: "turn-open", ObservedAtMS: 4,
				CWD: "/synthetic", Model: "gpt-5", Effort: &effort,
			},
			LatestUsage: &logs.TurnUsageFact{
				SessionID: "session-a", TurnID: "turn-open", ObservedAtMS: 5,
				Usage:         logs.TokenCounters{InputTokens: &zero, OutputTokens: &ten},
				ContextWindow: &ten,
			},
		}},
		PendingTurns: []logs.PendingTurnSeed{{
			TurnID: "turn-pending",
			Context: &logs.PendingTurnContextSeed{
				Position:     logs.SourcePosition{StartOffset: 10, EndOffset: 20},
				ObservedAtMS: 6, CWD: "/synthetic", Model: "gpt-5", Effort: &effort,
			},
			Terminal: &logs.PendingTurnTerminalSeed{
				Position:      logs.SourcePosition{StartOffset: 20, EndOffset: 30},
				CompletedAtMS: 7, Outcome: logs.TurnOutcomeInterrupted,
			},
		}},
		ClosedTurns: []logs.ClosedTurnSeed{{
			TurnID: "turn-closed", StartedAtMS: 8, ContextWindow: &ten,
			Terminal: logs.TurnEndFact{
				SessionID: "session-a", TurnID: "turn-closed", CompletedAtMS: 9,
				Outcome: logs.TurnOutcomeCompleted,
				FinalUsage: &logs.TurnUsageFact{
					SessionID: "session-a", TurnID: "turn-closed", ObservedAtMS: 9,
					Usage:         logs.TokenCounters{InputTokens: &ten, CachedInputTokens: &zero},
					ContextWindow: &ten, IsFinal: true,
				},
			},
		}},
	}
	checkpoint := parserSeedToCheckpoint(seed)
	got := parserSeedFromCheckpoint(checkpoint)
	if !reflect.DeepEqual(got, seed) {
		t.Fatalf("parser seed round trip differs:\ngot:  %#v\nwant: %#v", got, seed)
	}
	*seed.OpenTurns[0].LatestUsage.Usage.InputTokens = 99
	seed.PendingTurns[0].Context.CWD = "/mutated"
	if *got.OpenTurns[0].LatestUsage.Usage.InputTokens != 0 || got.PendingTurns[0].Context.CWD != "/synthetic" {
		t.Fatalf("mapped seed aliases caller memory: %#v", got)
	}
}
