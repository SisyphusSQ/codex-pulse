package logs

import (
	"errors"
	"fmt"
	"reflect"
	"strings"
	"testing"
)

func TestStreamParserParsesCompleteLinesAndRetainsIncompleteTail(t *testing.T) {
	t.Parallel()

	const (
		startOffset   int64 = 0
		privateMarker       = "PRIVATE_RESPONSE_MARKER"
	)
	complete := strings.Join([]string{
		`{"timestamp":"2026-07-14T01:00:00Z","type":"session_meta","payload":{"id":"session-1","timestamp":"2026-07-14T01:00:00Z","cwd":"/tmp/project","originator":"codex_cli_rs","cli_version":"0.142.3","source":"cli","model_provider":"openai"}}`,
		`{"timestamp":"2026-07-14T01:00:01Z","type":"event_msg","payload":{"type":"task_started","turn_id":"turn-1","started_at":1783990801,"model_context_window":258000}}`,
		`{"timestamp":"2026-07-14T01:00:02Z","type":"turn_context","payload":{"turn_id":"turn-1","cwd":"/tmp/project","model":"gpt-5.2-codex","effort":"high"}}`,
		`{"timestamp":"2026-07-14T01:00:03Z","type":"response_item","payload":{"type":"message","content":[{"type":"output_text","text":"` + privateMarker + ` 中文"}]}}`,
		`{"timestamp":`,
		`{"timestamp":"2026-07-14T01:00:04Z","type":"event_msg","payload":{"type":"future_` + privateMarker + `"}}`,
		`{"timestamp":"2026-07-14T01:00:05Z","type":"event_msg","payload":{"type":"token_count","info":{"total_token_usage":{"input_tokens":100,"cached_input_tokens":20,"output_tokens":30,"reasoning_output_tokens":4,"total_tokens":134},"last_token_usage":{"input_tokens":10,"cached_input_tokens":2,"output_tokens":3,"reasoning_output_tokens":1,"total_tokens":14},"model_context_window":258000}}}`,
		`{"timestamp":"2026-07-14T01:00:06Z","type":"event_msg","payload":{"type":"task_complete","turn_id":"turn-1","completed_at":1783990806,"last_agent_message":"` + privateMarker + `"}}`,
	}, "\n") + "\n"
	tail := `{"timestamp":"2026-07-14T01:00:07Z","type":"response_item","payload":{"private":"` + privateMarker + `"}}`
	input := []byte(complete + tail)

	parser, err := NewStreamParser(ParserConfig{
		SourceKind: SourceKindSession, StartOffset: startOffset, MaxLineBytes: 4096,
	})
	if err != nil {
		t.Fatalf("NewStreamParser() error = %v", err)
	}
	first, err := parser.Feed(startOffset, input)
	if err != nil {
		t.Fatalf("Feed() error = %v", err)
	}
	if first.ReadOffset != startOffset+int64(len(input)) ||
		first.CommittableOffset != startOffset+int64(len(complete)) || first.BufferedBytes != len(tail) {
		t.Fatalf("offsets = read %d commit %d buffered %d", first.ReadOffset, first.CommittableOffset, first.BufferedBytes)
	}
	wantStats := ParseStats{
		CompleteLines: 8, ParsedLines: 5, KnownIgnoredLines: 1,
		DiagnosticLines: 2, EventsEmitted: 6,
	}
	if first.Stats != wantStats {
		t.Fatalf("stats = %#v, want %#v", first.Stats, wantStats)
	}
	if len(first.Events) != 6 || first.Events[0].Kind != EventSessionMeta ||
		first.Events[1].Kind != EventTurnStarted || first.Events[2].Kind != EventTurnContext ||
		first.Events[3].Kind != EventSessionUsage || first.Events[4].Kind != EventTurnUsage ||
		first.Events[5].Kind != EventTurnEnded || first.Events[5].TurnEnd == nil ||
		first.Events[5].TurnEnd.FinalUsage == nil || !first.Events[5].TurnEnd.FinalUsage.IsFinal {
		t.Fatalf("events = %#v", first.Events)
	}
	if first.Events[0].SessionMeta.SourceKind != SourceKindSession {
		t.Fatalf("session source kind = %q", first.Events[0].SessionMeta.SourceKind)
	}
	if len(first.Diagnostics) != 2 || first.Diagnostics[0].Code != DiagnosticBadJSON ||
		first.Diagnostics[1].Code != DiagnosticUnknownEventType {
		t.Fatalf("diagnostics = %#v", first.Diagnostics)
	}
	assertMarkerAbsent(t, first, privateMarker)

	second, err := parser.Feed(first.ReadOffset, []byte("\n"))
	if err != nil {
		t.Fatalf("Feed(newline) error = %v", err)
	}
	if second.CommittableOffset != first.ReadOffset+1 || second.BufferedBytes != 0 ||
		second.Stats != (ParseStats{CompleteLines: 1, KnownIgnoredLines: 1}) {
		t.Fatalf("second result = %#v", second)
	}
	assertMarkerAbsent(t, second, privateMarker)
}

func TestStreamParserChunkingIsDeterministic(t *testing.T) {
	t.Parallel()

	const startOffset int64 = 0
	input := []byte(strings.Join([]string{
		`{"timestamp":"2026-07-14T01:00:00Z","type":"session_meta","payload":{"id":"session-1","timestamp":"2026-07-14T01:00:00Z","cwd":"/tmp/工程","originator":"codex_cli_rs","cli_version":"0.142.3","source":"cli"}}`,
		`{"timestamp":"2026-07-14T01:00:01Z","type":"event_msg","payload":{"type":"turn_started","turn_id":"turn-1"}}`,
		`{"timestamp":"2026-07-14T01:00:02Z","type":"turn_context","payload":{"turn_id":"turn-1","cwd":"/tmp/工程","model":"gpt-5.2-codex"}}`,
		`{"timestamp":"2026-07-14T01:00:03Z","type":"event_msg","payload":{"type":"turn_complete","turn_id":"turn-1"}}`,
	}, "\r\n") + "\r\n")

	oneChunk := parseWithChunkSizes(t, startOffset, input, []int{len(input)})
	oneByteSizes := make([]int, len(input))
	for index := range oneByteSizes {
		oneByteSizes[index] = 1
	}
	oneByte := parseWithChunkSizes(t, startOffset, input, oneByteSizes)
	boundary := parseWithChunkSizes(t, startOffset, input, []int{1, 2, 3, 5, 8, 13, 21, len(input)})

	if !reflect.DeepEqual(oneChunk, oneByte) {
		t.Fatalf("one-byte result differs:\none=%#v\nbytes=%#v", oneChunk, oneByte)
	}
	if !reflect.DeepEqual(oneChunk, boundary) {
		t.Fatalf("boundary result differs:\none=%#v\nboundary=%#v", oneChunk, boundary)
	}
	if _, err := NewStreamParser(ParserConfig{
		SourceKind: SourceKindSession, StartOffset: oneChunk.CommittableOffset, Seed: oneChunk.NextSeed,
	}); err != nil {
		t.Fatalf("NewStreamParser(generated checkpoint) error = %v seed=%#v", err, oneChunk.NextSeed)
	}
}

func TestStreamParserSkipsOversizeLineAndAdvancesOnlyThroughCompleteLines(t *testing.T) {
	t.Parallel()

	parser, err := NewStreamParser(ParserConfig{
		SourceKind: SourceKindArchivedSession, MaxLineBytes: 8,
	})
	if err != nil {
		t.Fatalf("NewStreamParser() error = %v", err)
	}
	first, err := parser.Feed(0, []byte("123456789"))
	if err != nil {
		t.Fatalf("Feed(first) error = %v", err)
	}
	if first.ReadOffset != 9 || first.CommittableOffset != 0 || first.BufferedBytes != 0 || first.Stats != (ParseStats{}) {
		t.Fatalf("first result = %#v", first)
	}

	second, err := parser.Feed(9, []byte("discarded\n{}\npartial"))
	if err != nil {
		t.Fatalf("Feed(second) error = %v", err)
	}
	if second.ReadOffset != 29 || second.CommittableOffset != 22 || second.BufferedBytes != 7 {
		t.Fatalf("second offsets = %#v", second)
	}
	if second.Stats != (ParseStats{CompleteLines: 2, DiagnosticLines: 2}) ||
		len(second.Diagnostics) != 2 || second.Diagnostics[0].Code != DiagnosticLineTooLong ||
		second.Diagnostics[1].Code != DiagnosticInvalidTimestamp {
		t.Fatalf("second diagnostics = %#v stats=%#v", second.Diagnostics, second.Stats)
	}
}

func TestStreamParserCarriesArchivedSourceKind(t *testing.T) {
	t.Parallel()

	fixture := []byte(`{"timestamp":"2026-07-14T01:00:00Z","type":"session_meta","payload":{"id":"session-1","timestamp":"2026-07-14T01:00:00Z","cwd":"/tmp/project","originator":"codex_cli_rs","cli_version":"0.142.3","source":"cli"}}` + "\n")
	parser, err := NewStreamParser(ParserConfig{SourceKind: SourceKindArchivedSession})
	if err != nil {
		t.Fatalf("NewStreamParser() error = %v", err)
	}
	result, err := parser.Feed(0, fixture)
	if err != nil {
		t.Fatalf("Feed() error = %v", err)
	}
	if len(result.Events) != 1 || result.Events[0].SessionMeta == nil ||
		result.Events[0].SessionMeta.SourceKind != SourceKindArchivedSession {
		t.Fatalf("session event = %#v", result.Events)
	}
}

func TestStreamParserRejectsInvalidConfigAndNonContiguousChunksWithoutMutation(t *testing.T) {
	t.Parallel()

	for _, testCase := range []struct {
		name   string
		config ParserConfig
		err    error
	}{
		{name: "session index", config: ParserConfig{SourceKind: SourceKindSessionIndex}, err: ErrUnsupportedParserSource},
		{name: "unknown source", config: ParserConfig{SourceKind: SourceKind("future")}, err: ErrUnsupportedParserSource},
		{name: "negative offset", config: ParserConfig{SourceKind: SourceKindSession, StartOffset: -1}, err: ErrInvalidParserConfig},
		{name: "negative limit", config: ParserConfig{SourceKind: SourceKindSession, MaxLineBytes: -1}, err: ErrInvalidParserConfig},
		{name: "over hard limit", config: ParserConfig{SourceKind: SourceKindSession, MaxLineBytes: MaxSupportedLineBytes + 1}, err: ErrInvalidParserConfig},
		{name: "nonzero offset without seed", config: ParserConfig{SourceKind: SourceKindSession, StartOffset: 10}, err: ErrInvalidParserSeed},
		{name: "seed at origin", config: ParserConfig{SourceKind: SourceKindSession, Seed: testParserSeed("session-1")}, err: ErrInvalidParserSeed},
		{name: "duplicate seeded turn", config: ParserConfig{
			SourceKind: SourceKindSession, StartOffset: 10,
			Seed: &ParserSeed{Session: testSessionFact("session-1"), OpenTurns: []OpenTurnSeed{
				{TurnID: "turn-1"}, {TurnID: "turn-1"},
			}},
		}, err: ErrInvalidParserSeed},
		{name: "mismatched seeded usage", config: ParserConfig{
			SourceKind: SourceKindSession, StartOffset: 10,
			Seed: &ParserSeed{Session: testSessionFact("session-1"), OpenTurns: []OpenTurnSeed{{
				TurnID: "turn-1", LatestUsage: &TurnUsageFact{SessionID: "other", TurnID: "turn-1"},
			}}},
		}, err: ErrInvalidParserSeed},
		{name: "final seeded usage", config: ParserConfig{
			SourceKind: SourceKindSession, StartOffset: 10,
			Seed: &ParserSeed{Session: testSessionFact("session-1"), OpenTurns: []OpenTurnSeed{{
				TurnID: "turn-1", LatestUsage: &TurnUsageFact{
					SessionID: "session-1", TurnID: "turn-1", IsFinal: true,
				},
			}}},
		}, err: ErrInvalidParserSeed},
		{name: "seeded usage before turn", config: ParserConfig{
			SourceKind: SourceKindSession, StartOffset: 10,
			Seed: &ParserSeed{Session: testSessionFact("session-1"), OpenTurns: []OpenTurnSeed{{
				TurnID: "turn-1", StartedAtMS: 20, LatestUsage: &TurnUsageFact{
					SessionID: "session-1", TurnID: "turn-1", ObservedAtMS: 19,
				},
			}}},
		}, err: ErrInvalidParserSeed},
		{name: "seeded source mismatch", config: ParserConfig{SourceKind: SourceKindArchivedSession, StartOffset: 10, Seed: testParserSeed("session-1")}, err: ErrInvalidParserSeed},
		{name: "pending past checkpoint", config: ParserConfig{
			SourceKind: SourceKindSession, StartOffset: 10,
			Seed: &ParserSeed{Session: testSessionFact("session-1"), PendingTurns: []PendingTurnSeed{{
				TurnID: "turn-1", Terminal: &PendingTurnTerminalSeed{
					Position:      SourcePosition{StartOffset: 9, EndOffset: 11},
					CompletedAtMS: 20, Outcome: TurnOutcomeCompleted,
				},
			}}},
		}, err: ErrInvalidParserSeed},
		{name: "pending context after terminal", config: ParserConfig{
			SourceKind: SourceKindSession, StartOffset: 20,
			Seed: &ParserSeed{Session: testSessionFact("session-1"), PendingTurns: []PendingTurnSeed{{
				TurnID: "turn-1",
				Context: &PendingTurnContextSeed{
					Position:     SourcePosition{StartOffset: 10, EndOffset: 12},
					ObservedAtMS: 20, CWD: "/tmp/project", Model: "gpt-5.2-codex",
				},
				Terminal: &PendingTurnTerminalSeed{
					Position:      SourcePosition{StartOffset: 8, EndOffset: 10},
					CompletedAtMS: 20, Outcome: TurnOutcomeCompleted,
				},
			}}},
		}, err: ErrInvalidParserSeed},
		{name: "pending positions overlap", config: ParserConfig{
			SourceKind: SourceKindSession, StartOffset: 20,
			Seed: &ParserSeed{Session: testSessionFact("session-1"), PendingTurns: []PendingTurnSeed{{
				TurnID: "turn-1",
				Context: &PendingTurnContextSeed{
					Position:     SourcePosition{StartOffset: 8, EndOffset: 12},
					ObservedAtMS: 20, CWD: "/tmp/project", Model: "gpt-5.2-codex",
				},
				Terminal: &PendingTurnTerminalSeed{
					Position:      SourcePosition{StartOffset: 10, EndOffset: 14},
					CompletedAtMS: 20, Outcome: TurnOutcomeCompleted,
				},
			}}},
		}, err: ErrInvalidParserSeed},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()
			if _, err := NewStreamParser(testCase.config); !errors.Is(err, testCase.err) {
				t.Fatalf("NewStreamParser() error = %v, want %v", err, testCase.err)
			}
		})
	}

	tooManyOpenTurns := make([]OpenTurnSeed, maxOpenTurnStates+1)
	for index := range tooManyOpenTurns {
		tooManyOpenTurns[index].TurnID = fmt.Sprintf("turn-%d", index)
	}
	if _, err := NewStreamParser(ParserConfig{
		SourceKind: SourceKindSession, StartOffset: 10,
		Seed: &ParserSeed{Session: testSessionFact("session-1"), OpenTurns: tooManyOpenTurns},
	}); !errors.Is(err, ErrInvalidParserSeed) {
		t.Fatalf("NewStreamParser(too many open turns) error = %v, want %v", err, ErrInvalidParserSeed)
	}

	preSession, err := NewStreamParser(ParserConfig{
		SourceKind: SourceKindSession, StartOffset: 10, Seed: &ParserSeed{},
	})
	if err != nil {
		t.Fatalf("NewStreamParser(empty checkpoint) error = %v", err)
	}
	meta := []byte(`{"timestamp":"2026-07-14T01:00:00Z","type":"session_meta","payload":{"id":"session-1","timestamp":"2026-07-14T01:00:00Z","cwd":"/tmp/project","originator":"codex_cli_rs","cli_version":"0.142.3","source":"cli"}}` + "\n")
	preSessionResult, err := preSession.Feed(10, meta)
	if err != nil || preSessionResult.NextSeed == nil || preSessionResult.NextSeed.Session == nil {
		t.Fatalf("Feed(session after empty checkpoint) result = %#v error=%v", preSessionResult, err)
	}

	parser, err := NewStreamParser(ParserConfig{
		SourceKind: SourceKindSession, StartOffset: 10, MaxLineBytes: 1024,
		Seed: testParserSeed("session-1"),
	})
	if err != nil {
		t.Fatalf("NewStreamParser() error = %v", err)
	}
	if _, err := parser.Feed(11, []byte("ignored\n")); !errors.Is(err, ErrNonContiguousChunk) {
		t.Fatalf("Feed(gap) error = %v", err)
	}
	result, err := parser.Feed(10, []byte("{}\n"))
	if err != nil {
		t.Fatalf("Feed(after gap) error = %v", err)
	}
	if result.ReadOffset != 13 || result.CommittableOffset != 13 || len(result.Diagnostics) != 1 {
		t.Fatalf("Feed(after gap) result = %#v", result)
	}

	var nilParser *StreamParser
	if _, err := nilParser.Feed(0, nil); !errors.Is(err, ErrInvalidParserConfig) {
		t.Fatalf("nil Feed() error = %v", err)
	}
}

func TestStreamParserHydratesNonzeroOffsetAndFreezesSeededUsage(t *testing.T) {
	t.Parallel()

	seededInput := int64(10)
	seed := &ParserSeed{
		Session: testSessionFact("session-1"),
		OpenTurns: []OpenTurnSeed{{
			TurnID: "turn-1", StartedAtMS: 500,
			ContextWindow: int64Pointer(258000),
			LatestUsage: &TurnUsageFact{
				SessionID: "session-1", TurnID: "turn-1", ObservedAtMS: 900,
				Usage: TokenCounters{InputTokens: &seededInput}, ContextWindow: int64Pointer(258000),
			},
		}},
	}
	parser, err := NewStreamParser(ParserConfig{
		SourceKind: SourceKindSession, StartOffset: 100, MaxLineBytes: 4096, Seed: seed,
	})
	if err != nil {
		t.Fatalf("NewStreamParser() error = %v", err)
	}
	terminal := []byte(`{"timestamp":"2026-07-14T01:00:02Z","type":"event_msg","payload":{"type":"turn_complete","turn_id":"turn-1","completed_at":1}}` + "\n")
	result, err := parser.Feed(100, terminal)
	if err != nil {
		t.Fatalf("Feed() error = %v", err)
	}
	if result.ReadOffset != 100+int64(len(terminal)) || result.CommittableOffset != result.ReadOffset ||
		len(result.Events) != 1 || result.Events[0].TurnEnd == nil || result.Events[0].TurnEnd.FinalUsage == nil ||
		!result.Events[0].TurnEnd.FinalUsage.IsFinal || result.Events[0].TurnEnd.FinalUsage.Usage.InputTokens == nil ||
		*result.Events[0].TurnEnd.FinalUsage.Usage.InputTokens != 10 {
		t.Fatalf("hydrated terminal result = %#v", result)
	}
	seededInput = 999
	if got := *result.Events[0].TurnEnd.FinalUsage.Usage.InputTokens; got != 10 {
		t.Fatalf("final usage aliases seed memory: got %d", got)
	}
}

func TestStreamParserPersistsPendingCheckpointAcrossRestart(t *testing.T) {
	t.Parallel()

	const startOffset int64 = 100
	seed := testParserSeed("session-1")
	terminal := []byte(`{"timestamp":"2026-07-14T01:00:02Z","type":"event_msg","payload":{"type":"turn_complete","turn_id":"turn-late","completed_at":2}}` + "\n")
	started := []byte(`{"timestamp":"2026-07-14T01:00:01Z","type":"event_msg","payload":{"type":"turn_started","turn_id":"turn-late","started_at":1}}` + "\n")

	parser, err := NewStreamParser(ParserConfig{SourceKind: SourceKindSession, StartOffset: startOffset, Seed: seed})
	if err != nil {
		t.Fatalf("NewStreamParser() error = %v", err)
	}
	first, err := parser.Feed(startOffset, terminal)
	if err != nil {
		t.Fatalf("Feed(terminal) error = %v", err)
	}
	if first.ReadOffset != startOffset+int64(len(terminal)) || first.CommittableOffset != first.ReadOffset ||
		len(first.Diagnostics) != 1 || first.Diagnostics[0].Code != DiagnosticMissingTurnStart ||
		first.NextSeed == nil || len(first.NextSeed.PendingTurns) != 1 {
		t.Fatalf("pending result = %#v", first)
	}
	second, err := parser.Feed(first.ReadOffset, started)
	if err != nil {
		t.Fatalf("Feed(start) error = %v", err)
	}
	if second.CommittableOffset != second.ReadOffset || len(second.Events) != 2 ||
		second.Events[0].Kind != EventTurnStarted || second.Events[1].Kind != EventTurnEnded {
		t.Fatalf("resolved result = %#v", second)
	}

	restarted, err := NewStreamParser(ParserConfig{
		SourceKind: SourceKindSession, StartOffset: first.ReadOffset, Seed: first.NextSeed,
	})
	if err != nil {
		t.Fatalf("NewStreamParser(restarted) error = %v", err)
	}
	replayed, err := restarted.Feed(first.ReadOffset, started)
	if err != nil {
		t.Fatalf("Feed(replay) error = %v", err)
	}
	if replayed.CommittableOffset != replayed.ReadOffset || !reflect.DeepEqual(second.Events, replayed.Events) ||
		replayed.NextSeed == nil || len(replayed.NextSeed.PendingTurns) != 0 {
		t.Fatalf("restart changed resolved events: incremental=%#v replay=%#v", second.Events, replayed.Events)
	}
	if replayed.Events[0].Position.StartOffset <= replayed.Events[1].Position.StartOffset {
		t.Fatalf("terminal-before-start source truth was reordered: events=%#v", replayed.Events)
	}
}

func TestStreamParserCheckpointKeepsOrphanPendingLiveAcrossRestart(t *testing.T) {
	t.Parallel()

	meta := []byte(`{"timestamp":"2026-07-14T01:00:00Z","type":"session_meta","payload":{"id":"session-1","timestamp":"2026-07-14T01:00:00Z","cwd":"/tmp/project","originator":"codex_cli_rs","cli_version":"0.142.3","source":"cli"}}` + "\n")
	terminal := []byte(`{"timestamp":"2026-07-14T01:00:01Z","type":"event_msg","payload":{"type":"turn_complete","turn_id":"turn-late","completed_at":2}}` + "\n")
	otherStart := []byte(`{"timestamp":"2026-07-14T01:00:02Z","type":"event_msg","payload":{"type":"turn_started","turn_id":"turn-other","started_at":1}}` + "\n")
	input := append(append(append([]byte(nil), meta...), terminal...), otherStart...)

	parser, err := NewStreamParser(ParserConfig{SourceKind: SourceKindSession})
	if err != nil {
		t.Fatalf("NewStreamParser() error = %v", err)
	}
	result, err := parser.Feed(0, input)
	if err != nil {
		t.Fatalf("Feed() error = %v", err)
	}
	if result.CommittableOffset != result.ReadOffset || len(result.Events) != 2 ||
		result.Events[0].Kind != EventSessionMeta || result.Events[1].Kind != EventTurnStarted ||
		len(result.Diagnostics) != 1 || result.Diagnostics[0].Code != DiagnosticMissingTurnStart ||
		result.NextSeed == nil || len(result.NextSeed.PendingTurns) != 1 || len(result.NextSeed.OpenTurns) != 1 {
		t.Fatalf("checkpoint result = %#v", result)
	}

	ignored := []byte(`{"timestamp":"2026-07-14T01:00:03Z","type":"response_item","payload":{"type":"message"}}` + "\n")
	continued, err := parser.Feed(result.ReadOffset, ignored)
	if err != nil {
		t.Fatalf("Feed(ignored) error = %v", err)
	}
	if continued.CommittableOffset != continued.ReadOffset || continued.NextSeed == nil ||
		len(continued.NextSeed.PendingTurns) != 1 {
		t.Fatalf("continued checkpoint = %#v", continued)
	}

	restarted, err := NewStreamParser(ParserConfig{
		SourceKind: SourceKindSession, StartOffset: continued.CommittableOffset,
		Seed: continued.NextSeed,
	})
	if err != nil {
		t.Fatalf("NewStreamParser(restarted) error = %v", err)
	}
	lateStart := []byte(`{"timestamp":"2026-07-14T01:00:04Z","type":"event_msg","payload":{"type":"turn_started","turn_id":"turn-late","started_at":1}}` + "\n")
	replayed, err := restarted.Feed(continued.CommittableOffset, lateStart)
	if err != nil {
		t.Fatalf("Feed(replayed) error = %v", err)
	}
	if replayed.CommittableOffset != replayed.ReadOffset || len(replayed.Events) != 2 ||
		replayed.Events[0].Kind != EventTurnStarted || replayed.Events[1].Kind != EventTurnEnded ||
		replayed.NextSeed == nil || len(replayed.NextSeed.PendingTurns) != 0 || len(replayed.NextSeed.OpenTurns) != 1 {
		t.Fatalf("restarted checkpoint result = %#v", replayed)
	}
}

func TestStreamParserCheckpointPreservesDuplicateSessionMetaDeterminism(t *testing.T) {
	t.Parallel()

	meta := []byte(`{"timestamp":"2026-07-14T01:00:00Z","type":"session_meta","payload":{"id":"session-1","timestamp":"2026-07-14T01:00:00Z","cwd":"/tmp/project","originator":"codex_cli_rs","cli_version":"0.142.3","source":"cli","model_provider":"openai"}}` + "\n")
	parser, err := NewStreamParser(ParserConfig{SourceKind: SourceKindSession})
	if err != nil {
		t.Fatalf("NewStreamParser() error = %v", err)
	}
	first, err := parser.Feed(0, meta)
	if err != nil || first.NextSeed == nil || first.NextSeed.Session == nil {
		t.Fatalf("Feed(meta) result = %#v error=%v", first, err)
	}
	continuous, err := parser.Feed(first.ReadOffset, meta)
	if err != nil {
		t.Fatalf("Feed(duplicate) error = %v", err)
	}

	restarted, err := NewStreamParser(ParserConfig{
		SourceKind: SourceKindSession, StartOffset: first.ReadOffset, Seed: first.NextSeed,
	})
	if err != nil {
		t.Fatalf("NewStreamParser(restarted) error = %v", err)
	}
	replayed, err := restarted.Feed(first.ReadOffset, meta)
	if err != nil {
		t.Fatalf("Feed(replayed duplicate) error = %v", err)
	}
	if !reflect.DeepEqual(continuous, replayed) || len(replayed.Events) != 0 || len(replayed.Diagnostics) != 0 {
		t.Fatalf("duplicate session_meta changed after restart: continuous=%#v replayed=%#v", continuous, replayed)
	}
}

func TestStreamParserCheckpointPreservesOpenContextAndClosedReplay(t *testing.T) {
	t.Parallel()

	prefix := []byte(strings.Join([]string{
		`{"timestamp":"2026-07-14T01:00:00Z","type":"session_meta","payload":{"id":"session-1","timestamp":"2026-07-14T01:00:00Z","cwd":"/tmp/project","originator":"codex_cli_rs","cli_version":"0.142.3","source":"cli"}}`,
		`{"timestamp":"2026-07-14T01:00:01Z","type":"event_msg","payload":{"type":"turn_started","turn_id":"turn-1","started_at":1783990801}}`,
		`{"timestamp":"2026-07-14T01:00:02Z","type":"turn_context","payload":{"turn_id":"turn-1","cwd":"/tmp/project","model":"gpt-5.2-codex","effort":"high"}}`,
	}, "\n") + "\n")
	duplicateContext := []byte(`{"timestamp":"2026-07-14T01:00:02Z","type":"turn_context","payload":{"turn_id":"turn-1","cwd":"/tmp/project","model":"gpt-5.2-codex","effort":"high"}}` + "\n")
	terminal := []byte(`{"timestamp":"2026-07-14T01:00:03Z","type":"event_msg","payload":{"type":"turn_complete","turn_id":"turn-1","completed_at":1783990803}}` + "\n")

	continuousParser, err := NewStreamParser(ParserConfig{SourceKind: SourceKindSession})
	if err != nil {
		t.Fatalf("NewStreamParser() error = %v", err)
	}
	prefixResult, err := continuousParser.Feed(0, prefix)
	if err != nil || prefixResult.NextSeed == nil || len(prefixResult.NextSeed.OpenTurns) != 1 ||
		prefixResult.NextSeed.OpenTurns[0].Context == nil {
		t.Fatalf("Feed(prefix) result = %#v error=%v", prefixResult, err)
	}
	restartedParser, err := NewStreamParser(ParserConfig{
		SourceKind: SourceKindSession, StartOffset: prefixResult.CommittableOffset, Seed: prefixResult.NextSeed,
	})
	if err != nil {
		t.Fatalf("NewStreamParser(restarted) error = %v", err)
	}
	continuousContext, err := continuousParser.Feed(prefixResult.CommittableOffset, duplicateContext)
	if err != nil {
		t.Fatalf("Feed(continuous context) error = %v", err)
	}
	restartedContext, err := restartedParser.Feed(prefixResult.CommittableOffset, duplicateContext)
	if err != nil || !reflect.DeepEqual(continuousContext, restartedContext) || len(restartedContext.Events) != 0 {
		t.Fatalf("context replay differs: continuous=%#v restarted=%#v error=%v", continuousContext, restartedContext, err)
	}

	closed, err := restartedParser.Feed(restartedContext.CommittableOffset, terminal)
	if err != nil || closed.NextSeed == nil || len(closed.NextSeed.OpenTurns) != 0 ||
		len(closed.NextSeed.ClosedTurns) != 1 {
		t.Fatalf("Feed(terminal) result = %#v error=%v", closed, err)
	}
	closedRestart, err := NewStreamParser(ParserConfig{
		SourceKind: SourceKindSession, StartOffset: closed.CommittableOffset, Seed: closed.NextSeed,
	})
	if err != nil {
		t.Fatalf("NewStreamParser(closed) error = %v", err)
	}
	duplicateTerminal, err := closedRestart.Feed(closed.CommittableOffset, terminal)
	if err != nil || len(duplicateTerminal.Events) != 0 || len(duplicateTerminal.Diagnostics) != 0 {
		t.Fatalf("closed replay result = %#v error=%v", duplicateTerminal, err)
	}
}

func TestStreamParserGeneratedCheckpointFreezesUsageAcrossRestart(t *testing.T) {
	t.Parallel()

	prefix := []byte(strings.Join([]string{
		`{"timestamp":"2026-07-14T01:00:00Z","type":"session_meta","payload":{"id":"session-1","timestamp":"2026-07-14T01:00:00Z","cwd":"/tmp/project","originator":"codex_cli_rs","cli_version":"0.142.3","source":"cli"}}`,
		`{"timestamp":"2026-07-14T01:00:01Z","type":"event_msg","payload":{"type":"turn_started","turn_id":"turn-1","started_at":1783990801}}`,
		`{"timestamp":"2026-07-14T01:00:02Z","type":"event_msg","payload":{"type":"token_count","info":{"total_token_usage":{"input_tokens":100},"last_token_usage":{"input_tokens":10}}}}`,
		`{"timestamp":"2026-07-14T01:00:03Z","type":"event_msg","payload":{"type":"turn_complete","turn_id":"turn-late","completed_at":1783990805}}`,
	}, "\n") + "\n")
	parser, err := NewStreamParser(ParserConfig{SourceKind: SourceKindSession})
	if err != nil {
		t.Fatalf("NewStreamParser() error = %v", err)
	}
	first, err := parser.Feed(0, prefix)
	if err != nil || first.NextSeed == nil || len(first.NextSeed.OpenTurns) != 1 ||
		first.NextSeed.OpenTurns[0].LatestUsage == nil || len(first.NextSeed.PendingTurns) != 1 {
		t.Fatalf("Feed(prefix) result = %#v error=%v", first, err)
	}

	restarted, err := NewStreamParser(ParserConfig{
		SourceKind: SourceKindSession, StartOffset: first.CommittableOffset, Seed: first.NextSeed,
	})
	if err != nil {
		t.Fatalf("NewStreamParser(restarted) error = %v", err)
	}
	terminal := []byte(`{"timestamp":"2026-07-14T01:00:04Z","type":"event_msg","payload":{"type":"turn_complete","turn_id":"turn-1","completed_at":1783990804}}` + "\n")
	result, err := restarted.Feed(first.CommittableOffset, terminal)
	if err != nil || len(result.Events) != 1 || result.Events[0].TurnEnd == nil ||
		result.Events[0].TurnEnd.FinalUsage == nil || result.Events[0].TurnEnd.FinalUsage.Usage.InputTokens == nil ||
		*result.Events[0].TurnEnd.FinalUsage.Usage.InputTokens != 10 {
		t.Fatalf("Feed(terminal) result = %#v error=%v", result, err)
	}
}

func TestStreamParserCheckpointDoesNotAliasParserState(t *testing.T) {
	t.Parallel()

	input := []byte(strings.Join([]string{
		`{"timestamp":"2026-07-14T01:00:00Z","type":"session_meta","payload":{"id":"session-1","timestamp":"2026-07-14T01:00:00Z","cwd":"/tmp/project","originator":"codex_cli_rs","cli_version":"0.142.3","source":"cli"}}`,
		`{"timestamp":"2026-07-14T01:00:01Z","type":"event_msg","payload":{"type":"turn_complete","turn_id":"turn-late","completed_at":1783990802}}`,
	}, "\n") + "\n")
	parser, err := NewStreamParser(ParserConfig{SourceKind: SourceKindSession})
	if err != nil {
		t.Fatalf("NewStreamParser() error = %v", err)
	}
	first, err := parser.Feed(0, input)
	if err != nil || first.NextSeed == nil || first.NextSeed.Session == nil ||
		len(first.NextSeed.PendingTurns) != 1 || first.NextSeed.PendingTurns[0].Terminal == nil {
		t.Fatalf("Feed(checkpoint) result = %#v error=%v", first, err)
	}
	first.NextSeed.Session.SessionID = "mutated"
	first.NextSeed.PendingTurns[0].Terminal.CompletedAtMS = 9999999999999

	started := []byte(`{"timestamp":"2026-07-14T01:00:02Z","type":"event_msg","payload":{"type":"turn_started","turn_id":"turn-late","started_at":1783990801}}` + "\n")
	result, err := parser.Feed(first.ReadOffset, started)
	if err != nil || len(result.Events) != 2 || result.Events[1].TurnEnd == nil ||
		result.Events[1].TurnEnd.SessionID != "session-1" || result.Events[1].TurnEnd.CompletedAtMS != 1783990802000 {
		t.Fatalf("checkpoint mutation changed parser state: result=%#v error=%v", result, err)
	}
}

func TestStreamParserDiagnosticOrderIsChunkInvariantWithDelayedLifecycleFailure(t *testing.T) {
	t.Parallel()

	input := []byte(strings.Join([]string{
		`{"timestamp":"2026-07-14T01:00:00Z","type":"session_meta","payload":{"id":"session-1","timestamp":"2026-07-14T01:00:00Z","cwd":"/tmp/project","originator":"codex_cli_rs","cli_version":"0.142.3","source":"cli"}}`,
		`{"timestamp":"2026-07-14T01:00:02Z","type":"event_msg","payload":{"type":"turn_complete","turn_id":"turn-late","completed_at":1}}`,
		`{"timestamp":`,
		`{"timestamp":"2026-07-14T01:00:01Z","type":"event_msg","payload":{"type":"turn_started","turn_id":"turn-late","started_at":2}}`,
	}, "\n") + "\n")
	oneChunk := parseWithChunkSizes(t, 0, input, []int{len(input)})
	oneByteSizes := make([]int, len(input))
	for index := range oneByteSizes {
		oneByteSizes[index] = 1
	}
	oneByte := parseWithChunkSizes(t, 0, input, oneByteSizes)
	if !reflect.DeepEqual(oneChunk, oneByte) {
		t.Fatalf("diagnostic emission order depends on chunking:\none=%#v\nbytes=%#v", oneChunk, oneByte)
	}
	wantCodes := []DiagnosticCode{DiagnosticMissingTurnStart, DiagnosticBadJSON, DiagnosticInvalidTransition}
	gotCodes := make([]DiagnosticCode, 0, len(oneChunk.Diagnostics))
	for _, diagnostic := range oneChunk.Diagnostics {
		gotCodes = append(gotCodes, diagnostic.Code)
	}
	if !reflect.DeepEqual(gotCodes, wantCodes) {
		t.Fatalf("diagnostic codes = %#v, want %#v", gotCodes, wantCodes)
	}
}

type collectedParseResult struct {
	Events            []ParsedEvent
	Diagnostics       []ParserDiagnostic
	Stats             ParseStats
	ReadOffset        int64
	CommittableOffset int64
	BufferedBytes     int
	NextSeed          *ParserSeed
}

func parseWithChunkSizes(t *testing.T, startOffset int64, input []byte, sizes []int) collectedParseResult {
	t.Helper()
	parser, err := NewStreamParser(ParserConfig{
		SourceKind: SourceKindSession, StartOffset: startOffset, MaxLineBytes: 4096,
	})
	if err != nil {
		t.Fatalf("NewStreamParser() error = %v", err)
	}
	result := collectedParseResult{ReadOffset: startOffset, CommittableOffset: startOffset}
	consumed := 0
	for _, requestedSize := range sizes {
		if consumed == len(input) {
			break
		}
		size := requestedSize
		if remaining := len(input) - consumed; size > remaining {
			size = remaining
		}
		if size <= 0 {
			continue
		}
		current, err := parser.Feed(startOffset+int64(consumed), input[consumed:consumed+size])
		if err != nil {
			t.Fatalf("Feed(offset=%d,size=%d) error = %v", consumed, size, err)
		}
		result.Events = append(result.Events, current.Events...)
		result.Diagnostics = append(result.Diagnostics, current.Diagnostics...)
		result.Stats.add(current.Stats)
		result.ReadOffset = current.ReadOffset
		result.CommittableOffset = current.CommittableOffset
		result.BufferedBytes = current.BufferedBytes
		result.NextSeed = current.NextSeed
		consumed += size
	}
	if consumed < len(input) {
		current, err := parser.Feed(startOffset+int64(consumed), input[consumed:])
		if err != nil {
			t.Fatalf("Feed(remainder) error = %v", err)
		}
		result.Events = append(result.Events, current.Events...)
		result.Diagnostics = append(result.Diagnostics, current.Diagnostics...)
		result.Stats.add(current.Stats)
		result.ReadOffset = current.ReadOffset
		result.CommittableOffset = current.CommittableOffset
		result.BufferedBytes = current.BufferedBytes
		result.NextSeed = current.NextSeed
	}
	if result.ReadOffset != startOffset+int64(len(input)) {
		t.Fatalf("read offset = %d, want %d", result.ReadOffset, startOffset+int64(len(input)))
	}
	return result
}

func (result collectedParseResult) String() string {
	return fmt.Sprintf("events=%d diagnostics=%d stats=%#v", len(result.Events), len(result.Diagnostics), result.Stats)
}

func testParserSeed(sessionID string) *ParserSeed {
	return &ParserSeed{Session: testSessionFact(sessionID)}
}

func testSessionFact(sessionID string) *SessionMetaFact {
	return &SessionMetaFact{
		SessionID: sessionID, RootSessionID: sessionID, SourceKind: SourceKindSession,
		CreatedAtMS: 500, ObservedAtMS: 500, InitialCWD: "/tmp/project",
		Originator: "codex_cli_rs", CLIVersion: "0.142.3", Source: "cli",
	}
}
