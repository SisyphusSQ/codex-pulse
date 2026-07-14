package store

import (
	"context"
	"errors"
	"reflect"
	"testing"
)

// 测试 SourceFile 在 stable identity 下只允许 generation/offset 单调推进。
func TestSourceFileUpsertPreservesIdentityAndMonotonicCursor(t *testing.T) {
	t.Parallel()

	repository := openRuntimeRepository(t)
	initial := SourceFile{
		SourceFileID:     "file-a",
		Provider:         "codex",
		CurrentPath:      "/synthetic/session-a.jsonl",
		DeviceID:         "device-a",
		Inode:            42,
		SizeBytes:        100,
		MTimeNS:          1_000,
		ParsedOffset:     40,
		ParserVersion:    "parser-v1",
		ActiveGeneration: 0,
		State:            SourceFileActive,
		LastScannedAtMS:  pointerTo(int64(10)),
		UpdatedAtMS:      10,
	}
	if err := repository.UpsertSourceFile(context.Background(), initial); err != nil {
		t.Fatalf("UpsertSourceFile(initial) error = %v", err)
	}
	if err := repository.UpsertSourceFile(context.Background(), initial); err != nil {
		t.Fatalf("UpsertSourceFile(replay) error = %v", err)
	}
	got, err := repository.SourceFile(context.Background(), initial.SourceFileID)
	if err != nil {
		t.Fatalf("SourceFile() error = %v", err)
	}
	if !reflect.DeepEqual(got, initial) {
		t.Fatalf("SourceFile() = %#v, want %#v", got, initial)
	}

	advanced := initial
	advanced.SizeBytes = 160
	advanced.MTimeNS = 2_000
	advanced.ParsedOffset = 120
	advanced.LastScannedAtMS = pointerTo(int64(20))
	advanced.UpdatedAtMS = 20
	if err := repository.UpsertSourceFile(context.Background(), advanced); err != nil {
		t.Fatalf("UpsertSourceFile(advanced) error = %v", err)
	}

	rotated := advanced
	rotated.CurrentPath = "/synthetic/session-a.rotated.jsonl"
	rotated.SizeBytes = 20
	rotated.MTimeNS = 3_000
	rotated.ParsedOffset = 0
	rotated.ActiveGeneration = 1
	rotated.State = SourceFileDiscovered
	rotated.LastScannedAtMS = nil
	rotated.UpdatedAtMS = 30
	if err := repository.UpsertSourceFile(context.Background(), rotated); err != nil {
		t.Fatalf("UpsertSourceFile(rotated) error = %v", err)
	}
	got, err = repository.SourceFile(context.Background(), initial.SourceFileID)
	if err != nil {
		t.Fatalf("SourceFile(rotated) error = %v", err)
	}
	if !reflect.DeepEqual(got, rotated) {
		t.Fatalf("SourceFile(rotated) = %#v, want %#v", got, rotated)
	}
	progressed := rotated
	progressed.SizeBytes = 25
	progressed.ParsedOffset = 10
	progressed.UpdatedAtMS = 40
	if err := repository.UpsertSourceFile(context.Background(), progressed); err != nil {
		t.Fatalf("UpsertSourceFile(same generation progress) error = %v", err)
	}

	cases := []struct {
		name string
		file SourceFile
	}{
		{
			name: "same generation offset regression",
			file: func() SourceFile {
				value := progressed
				value.ParsedOffset = 5
				value.UpdatedAtMS = 50
				return value
			}(),
		},
		{
			name: "old generation",
			file: func() SourceFile {
				value := rotated
				value.ActiveGeneration = 0
				value.UpdatedAtMS = 50
				return value
			}(),
		},
		{
			name: "stable identity conflict",
			file: func() SourceFile {
				value := progressed
				value.DeviceID = "device-b"
				value.UpdatedAtMS = 50
				return value
			}(),
		},
		{
			name: "same timestamp payload conflict",
			file: func() SourceFile {
				value := progressed
				value.CurrentPath = "/synthetic/conflict.jsonl"
				return value
			}(),
		},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			if err := repository.UpsertSourceFile(context.Background(), testCase.file); !errors.Is(err, ErrInvalidRecord) {
				t.Fatalf("UpsertSourceFile() error = %v, want ErrInvalidRecord", err)
			}
		})
	}
}

// 测试 session/state 关联查询通过公开 Repository API 返回稳定顺序。
func TestListSourceFilesBySessionState(t *testing.T) {
	t.Parallel()

	repository := openRuntimeRepository(t)
	session := Session{
		SessionID: "source-session", Provider: "codex", SourceKind: "jsonl",
		CreatedAtMS: 1, FirstSeenAtMS: 1, LastSeenAtMS: 1,
	}
	if err := repository.UpsertFacts(context.Background(), FactBatch{Session: &session}); err != nil {
		t.Fatalf("UpsertFacts(session) error = %v", err)
	}
	first := SourceFile{
		SourceFileID: "session-file-a", Provider: "codex", SessionID: pointerTo(session.SessionID),
		CurrentPath: "/synthetic/a.jsonl", DeviceID: "device-session", Inode: 1,
		SizeBytes: 1, MTimeNS: 1, ParsedOffset: 0, ParserVersion: "v1",
		State: SourceFileActive, LastScannedAtMS: pointerTo(int64(10)), UpdatedAtMS: 10,
	}
	second := first
	second.SourceFileID = "session-file-b"
	second.CurrentPath = "/synthetic/b.jsonl"
	second.Inode = 2
	second.LastScannedAtMS = pointerTo(int64(20))
	second.UpdatedAtMS = 20
	completed := first
	completed.SourceFileID = "session-file-complete"
	completed.CurrentPath = "/synthetic/complete.jsonl"
	completed.Inode = 3
	completed.State = SourceFileCompleted
	for _, file := range []SourceFile{second, completed, first} {
		if err := repository.UpsertSourceFile(context.Background(), file); err != nil {
			t.Fatalf("UpsertSourceFile(%s) error = %v", file.SourceFileID, err)
		}
	}
	got, err := repository.ListSourceFilesBySessionState(
		context.Background(), session.SessionID, SourceFileActive, 10,
	)
	if err != nil {
		t.Fatalf("ListSourceFilesBySessionState() error = %v", err)
	}
	if !reflect.DeepEqual(got, []SourceFile{first, second}) {
		t.Fatalf("ListSourceFilesBySessionState() = %#v, want active files in scan order", got)
	}
}

// 测试 SourceState due 查询与 SourceAttempt append-only 重放/冲突语义。
func TestSourceStateDueQueryAndAppendOnlyAttempts(t *testing.T) {
	t.Parallel()

	repository := openRuntimeRepository(t)
	state := SourceState{
		SourceInstanceID:    "source-a",
		SourceType:          "wham_quota",
		ScopeKey:            "default",
		LastAttemptAtMS:     pointerTo(int64(90)),
		LastSuccessAtMS:     pointerTo(int64(80)),
		NextDueAtMS:         pointerTo(int64(100)),
		ConsecutiveFailures: 0,
		FreshnessState:      SourceFreshnessStale,
		CursorVersion:       3,
		UpdatedAtMS:         90,
	}
	if err := repository.UpsertSourceState(context.Background(), state); err != nil {
		t.Fatalf("UpsertSourceState() error = %v", err)
	}
	if err := repository.UpsertSourceState(context.Background(), state); err != nil {
		t.Fatalf("UpsertSourceState(replay) error = %v", err)
	}
	gotState, err := repository.SourceState(context.Background(), state.SourceInstanceID)
	if err != nil {
		t.Fatalf("SourceState() error = %v", err)
	}
	if !reflect.DeepEqual(gotState, state) {
		t.Fatalf("SourceState() = %#v, want %#v", gotState, state)
	}
	due, err := repository.ListDueSources(context.Background(), 100, 10)
	if err != nil {
		t.Fatalf("ListDueSources() error = %v", err)
	}
	if !reflect.DeepEqual(due, []SourceState{state}) {
		t.Fatalf("ListDueSources() = %#v, want state", due)
	}

	conflict := state
	conflict.CursorVersion++
	if err := repository.UpsertSourceState(context.Background(), conflict); !errors.Is(err, ErrInvalidRecord) {
		t.Fatalf("UpsertSourceState(same-time conflict) error = %v, want ErrInvalidRecord", err)
	}
	stale := state
	stale.UpdatedAtMS--
	if err := repository.UpsertSourceState(context.Background(), stale); !errors.Is(err, ErrInvalidRecord) {
		t.Fatalf("UpsertSourceState(stale) error = %v, want ErrInvalidRecord", err)
	}

	attempt := SourceAttempt{
		RequestID:        "request-a",
		SourceInstanceID: state.SourceInstanceID,
		StartedAtMS:      100,
		FinishedAtMS:     110,
		Outcome:          SourceAttemptSucceeded,
		HTTPStatus:       pointerTo(int64(200)),
		PayloadSHA256:    pointerTo(SHA256DigestOf([]byte("synthetic payload"))),
	}
	if err := repository.AppendSourceAttempt(context.Background(), attempt); err != nil {
		t.Fatalf("AppendSourceAttempt() error = %v", err)
	}
	if err := repository.AppendSourceAttempt(context.Background(), attempt); err != nil {
		t.Fatalf("AppendSourceAttempt(replay) error = %v", err)
	}
	attempts, err := repository.ListSourceAttempts(context.Background(), state.SourceInstanceID, 10)
	if err != nil {
		t.Fatalf("ListSourceAttempts() error = %v", err)
	}
	if !reflect.DeepEqual(attempts, []SourceAttempt{attempt}) {
		t.Fatalf("ListSourceAttempts() = %#v, want one exact attempt", attempts)
	}

	attemptConflict := attempt
	attemptConflict.Outcome = SourceAttemptFailed
	attemptConflict.ErrorClass = pointerTo(RuntimeErrorUnavailable)
	if err := repository.AppendSourceAttempt(context.Background(), attemptConflict); !errors.Is(err, ErrInvalidRecord) {
		t.Fatalf("AppendSourceAttempt(conflict) error = %v, want ErrInvalidRecord", err)
	}
	missing := attempt
	missing.RequestID = "request-missing"
	missing.SourceInstanceID = "missing-source"
	if err := repository.AppendSourceAttempt(context.Background(), missing); !errors.Is(err, ErrInvalidRecord) {
		t.Fatalf("AppendSourceAttempt(missing source) error = %v, want ErrInvalidRecord", err)
	}
	rawPayload := attempt
	rawPayload.RequestID = "request-raw"
	var invalidDigest SHA256Digest
	rawPayload.PayloadSHA256 = &invalidDigest
	if err := repository.AppendSourceAttempt(context.Background(), rawPayload); !errors.Is(err, ErrInvalidRecord) {
		t.Fatalf("AppendSourceAttempt(raw payload text) error = %v, want ErrInvalidRecord", err)
	}
}

func openRuntimeRepository(t *testing.T) *Repository {
	t.Helper()
	database := openTestDatabase(t)
	repository := NewRepository(database)
	if err := repository.EnsureApplicationSchema(context.Background()); err != nil {
		t.Fatalf("EnsureApplicationSchema() error = %v", err)
	}
	return repository
}

func pointerTo[T any](value T) *T {
	return &value
}
