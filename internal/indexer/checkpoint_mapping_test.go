package indexer

import (
	"reflect"
	"testing"

	logparser "github.com/SisyphusSQ/codex-pulse/internal/codex/logs/parser"
	logsource "github.com/SisyphusSQ/codex-pulse/internal/codex/logs/source"
)

func TestParserSeedCheckpointMappingRoundTripsAllSafeFields(t *testing.T) {
	t.Parallel()

	zero, ten := int64(0), int64(10)
	effort := "high"
	seed := &logparser.ParserSeed{
		Session: &logparser.SessionMetaFact{
			SessionID: "session-a", RootSessionID: "root-a", SourceKind: logsource.SourceKindArchivedSession,
			CreatedAtMS: 1, ObservedAtMS: 2, InitialCWD: "/synthetic", Originator: "cli",
			CLIVersion: "1", Source: "subagent", ModelProvider: "openai",
		},
		OpenTurns: []logparser.OpenTurnSeed{{
			TurnID: "turn-open", StartedAtMS: 3, ContextWindow: &zero,
			Context: &logparser.TurnContextFact{
				SessionID: "session-a", TurnID: "turn-open", ObservedAtMS: 4,
				CWD: "/synthetic", Model: "gpt-5", Effort: &effort,
			},
			LatestUsage: &logparser.TurnUsageFact{
				SessionID: "session-a", TurnID: "turn-open", ObservedAtMS: 5,
				Usage:         logparser.TokenCounters{InputTokens: &zero, OutputTokens: &ten},
				ContextWindow: &ten,
			},
		}},
		PendingTurns: []logparser.PendingTurnSeed{{
			TurnID: "turn-pending",
			Context: &logparser.PendingTurnContextSeed{
				Position:     logparser.SourcePosition{StartOffset: 10, EndOffset: 20},
				ObservedAtMS: 6, CWD: "/synthetic", Model: "gpt-5", Effort: &effort,
			},
			Terminal: &logparser.PendingTurnTerminalSeed{
				Position:      logparser.SourcePosition{StartOffset: 20, EndOffset: 30},
				CompletedAtMS: 7, Outcome: logparser.TurnOutcomeInterrupted,
			},
		}},
		ClosedTurns: []logparser.ClosedTurnSeed{{
			TurnID: "turn-closed", StartedAtMS: 8, ContextWindow: &ten,
			Terminal: logparser.TurnEndFact{
				SessionID: "session-a", TurnID: "turn-closed", CompletedAtMS: 9,
				Outcome: logparser.TurnOutcomeCompleted,
				FinalUsage: &logparser.TurnUsageFact{
					SessionID: "session-a", TurnID: "turn-closed", ObservedAtMS: 9,
					Usage:         logparser.TokenCounters{InputTokens: &ten, CachedInputTokens: &zero},
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
