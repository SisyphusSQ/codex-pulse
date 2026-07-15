package liveindex

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/SisyphusSQ/codex-pulse/internal/codex/logs"
	"github.com/SisyphusSQ/codex-pulse/internal/store"
	storesqlite "github.com/SisyphusSQ/codex-pulse/internal/store/sqlite"
)

func TestRuntimeStartsExactTypedLiveJobAndRunsBoundedSlices(t *testing.T) {
	t.Parallel()

	repository := openLiveRepository(t)
	home := t.TempDir()
	content := largeLiveRollout("session-live-slice", "turn-live-slice")
	path := filepath.Join(home, "sessions", "live.jsonl")
	writeLiveFile(t, path, content)
	request := liveRequest(t, home, "request-live-slice", 7)
	runtime := newLiveRuntime(t, repository, Config{ReadChunkBytes: 32})

	job, err := runtime.Start(context.Background(), request)
	if err != nil {
		t.Fatalf("Start() error = %v", err)
	}
	replay, err := runtime.Start(context.Background(), request)
	if err != nil || replay != job {
		t.Fatalf("Start(replay) = %#v, %v, want %#v", replay, err, job)
	}
	storedJob, facts, err := repository.LiveScanRun(context.Background(), job.JobID)
	if err != nil || storedJob != job || facts.RequestID != request.RequestID ||
		facts.ActionKind != store.LiveScanActionAdded || facts.Current.CurrentPath != request.Action.Current.Path {
		t.Fatalf("LiveScanRun() = %#v %#v, %v", storedJob, facts, err)
	}

	first, err := runtime.RunSlice(context.Background(), job.JobID, SliceBudget{
		MaxFiles: 1, MaxBytes: logs.PrefixLimitBytes, MaxActive: time.Minute,
	})
	if err != nil || first.Complete || first.ExhaustedBy != SliceStopByteBudget ||
		first.FilesProcessed != 1 || first.BytesRead != logs.PrefixLimitBytes || first.State != store.JobRunning {
		t.Fatalf("RunSlice(first) = %#v, %v", first, err)
	}
	cursor, err := repository.BuildingGenerationCursor(context.Background(), facts.Current.SourceFileID)
	if err != nil || cursor.Checkpoint.CommittedOffset <= 0 ||
		cursor.Checkpoint.CommittedOffset >= logs.PrefixLimitBytes {
		t.Fatalf("BuildingGenerationCursor() = %#v, %v", cursor, err)
	}
	committedOffset := cursor.Checkpoint.CommittedOffset

	secondBudget := logs.PrefixLimitBytes + int64(len(content)) - committedOffset
	second, err := runtime.RunSlice(context.Background(), job.JobID, SliceBudget{
		MaxFiles: 1, MaxBytes: secondBudget,
		MaxActive: time.Minute,
	})
	if err != nil || !second.Complete || second.ExhaustedBy != SliceStopCompleted ||
		second.BytesRead <= logs.PrefixLimitBytes || second.BytesRead > secondBudget ||
		second.State != store.JobSucceeded {
		t.Fatalf("RunSlice(second) = %#v, %v", second, err)
	}
	if _, err := repository.Session(context.Background(), "session-live-slice"); err != nil {
		t.Fatalf("Session() error = %v", err)
	}
	movedHome := home + "-after-completion"
	if err := os.Rename(home, movedHome); err != nil {
		t.Fatalf("Rename(Home) error = %v", err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(movedHome) })
	if replay, err := runtime.Start(context.Background(), request); err != nil || replay.JobID != job.JobID {
		t.Fatalf("Start(durable replay after Home moved) = %#v, %v", replay, err)
	}
}

func TestRuntimeRejectsConflictingAndUnsupportedLiveRequests(t *testing.T) {
	t.Parallel()

	repository := openLiveRepository(t)
	home := t.TempDir()
	writeLiveFile(t, filepath.Join(home, "sessions", "conflict.jsonl"),
		liveRollout("session-live-conflict", "turn-live-conflict"))
	request := liveRequest(t, home, "request-live-conflict", 8)
	runtime := newLiveRuntime(t, repository, Config{})
	if _, err := runtime.Start(context.Background(), request); err != nil {
		t.Fatalf("Start() error = %v", err)
	}
	conflict := request
	conflict.HomeGeneration++
	if _, err := runtime.Start(context.Background(), conflict); !errors.Is(err, store.ErrLiveScanConflict) {
		t.Fatalf("Start(conflict) error = %v, want ErrLiveScanConflict", err)
	}
	unsupported := request
	unsupported.RequestID = "request-live-deleted"
	unsupported.Action = logs.ReconcileAction{Kind: logs.ChangeDeleted, Previous: request.Action.Current}
	if _, err := runtime.Start(context.Background(), unsupported); !errors.Is(err, ErrInvalidRequest) {
		t.Fatalf("Start(deleted) error = %v, want ErrInvalidRequest", err)
	}
}

func TestRuntimeInterruptsAndRecoversLiveJobFromAuthoritativeCheckpoint(t *testing.T) {
	t.Parallel()

	repository := openLiveRepository(t)
	home := t.TempDir()
	content := largeLiveRollout("session-live-resume", "turn-live-resume")
	writeLiveFile(t, filepath.Join(home, "sessions", "resume.jsonl"), content)
	request := liveRequest(t, home, "request-live-resume", 9)
	runtime := newLiveRuntime(t, repository, Config{ReadChunkBytes: 32})
	job, err := runtime.Start(context.Background(), request)
	if err != nil {
		t.Fatalf("Start() error = %v", err)
	}
	if _, err := runtime.RunSlice(context.Background(), job.JobID, SliceBudget{
		MaxFiles: 1, MaxBytes: logs.PrefixLimitBytes, MaxActive: time.Minute,
	}); err != nil {
		t.Fatalf("RunSlice(first) error = %v", err)
	}
	if err := runtime.Interrupt(context.Background(), job.JobID, store.RuntimeErrorCanceled); err != nil {
		t.Fatalf("Interrupt() error = %v", err)
	}
	resumedJob, err := runtime.Recover(context.Background(), job.JobID)
	if err != nil {
		t.Fatalf("Recover() error = %v", err)
	}
	if replay, err := runtime.Recover(context.Background(), job.JobID); err != nil || replay.JobID != resumedJob.JobID {
		t.Fatalf("Recover(replay) = %#v, %v, want %q", replay, err, resumedJob.JobID)
	}
	resumed, facts, err := repository.LiveScanRun(context.Background(), resumedJob.JobID)
	if err != nil || resumed.ResumeOfJobID == nil || *resumed.ResumeOfJobID != job.JobID ||
		facts.RequestID == request.RequestID {
		t.Fatalf("LiveScanRun(resumed) = %#v %#v, %v", resumed, facts, err)
	}
	report, err := runtime.RunSlice(context.Background(), resumedJob.JobID, SliceBudget{
		MaxFiles: 1, MaxBytes: logs.PrefixLimitBytes + int64(len(content)), MaxActive: time.Minute,
	})
	if err != nil || !report.Complete {
		t.Fatalf("RunSlice(resumed) = %#v, %v", report, err)
	}
}

func TestRuntimeRecoverReturnsInterruptedTerminalWhenResumeTimeIsExhausted(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	repository := openLiveRepository(t)
	home := t.TempDir()
	writeLiveFile(t, filepath.Join(home, "sessions", "recover-time-boundary.jsonl"),
		liveRollout("session-live-recover-time-boundary", "turn-live-recover-time-boundary"))
	runtime := newLiveRuntime(t, repository, Config{})
	job, err := runtime.Start(ctx, liveRequest(t, home, "request-live-recover-time-boundary", 13))
	if err != nil {
		t.Fatalf("Start() error = %v", err)
	}
	if err := repository.TransitionJobRun(ctx, store.JobTransition{
		JobID: job.JobID, ExpectedState: store.JobQueued, State: store.JobRunning,
		Phase: store.JobPhaseLive, AtMS: store.MaxSchedulerRunningTimestampMS,
	}); err != nil {
		t.Fatalf("TransitionJobRun(running) error = %v", err)
	}
	recovered, err := runtime.Recover(ctx, job.JobID)
	if err != nil || recovered.State != store.JobInterrupted ||
		recovered.UpdatedAtMS != store.MaxSchedulerTimestampMS {
		t.Fatalf("Recover(exhausted resume time) = %#v, %v", recovered, err)
	}
	if replay, err := runtime.Recover(ctx, job.JobID); err != nil || replay.JobID != recovered.JobID ||
		replay.State != recovered.State || replay.UpdatedAtMS != recovered.UpdatedAtMS {
		t.Fatalf("Recover(exhausted replay) = %#v, %v, want %#v", replay, err, recovered)
	}
}

func TestRuntimeFailsLiveJobWhenSnapshotDriftsBeforeRead(t *testing.T) {
	t.Parallel()

	repository := openLiveRepository(t)
	home := t.TempDir()
	path := filepath.Join(home, "sessions", "drift.jsonl")
	writeLiveFile(t, path, liveRollout("session-live-drift", "turn-live-drift"))
	request := liveRequest(t, home, "request-live-drift", 10)
	runtime := newLiveRuntime(t, repository, Config{})
	job, err := runtime.Start(context.Background(), request)
	if err != nil {
		t.Fatalf("Start() error = %v", err)
	}
	file, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0)
	if err != nil {
		t.Fatalf("OpenFile() error = %v", err)
	}
	_, writeErr := file.WriteString("drift\n")
	closeErr := file.Close()
	if err := errors.Join(writeErr, closeErr); err != nil {
		t.Fatalf("append drift: %v", err)
	}
	if _, err := runtime.RunSlice(context.Background(), job.JobID, SliceBudget{
		MaxFiles: 1, MaxBytes: 1 << 20, MaxActive: time.Minute,
	}); !errors.Is(err, logs.ErrChangedDuringScan) {
		t.Fatalf("RunSlice(drift) error = %v, want ErrChangedDuringScan", err)
	}
	failed, _, err := repository.LiveScanRun(context.Background(), job.JobID)
	if err != nil || failed.State != store.JobFailed || failed.ErrorClass == nil ||
		*failed.ErrorClass != store.RuntimeErrorUnavailable {
		t.Fatalf("LiveScanRun(failed) = %#v, %v", failed, err)
	}
	resumed, err := runtime.Retry(context.Background(), job.JobID)
	if err != nil || resumed.State != store.JobQueued || resumed.ResumeOfJobID == nil ||
		*resumed.ResumeOfJobID != job.JobID {
		t.Fatalf("Retry(failed) = %#v, %v", resumed, err)
	}
	if replay, err := runtime.Retry(context.Background(), job.JobID); err != nil || replay.JobID != resumed.JobID {
		t.Fatalf("Retry(failed replay) = %#v, %v, want %q", replay, err, resumed.JobID)
	}
}

func TestRuntimeRetryRejectsTimestampSuccessorPastRuntimeBoundary(t *testing.T) {
	t.Parallel()

	repository := openLiveRepository(t)
	home := t.TempDir()
	writeLiveFile(t, filepath.Join(home, "sessions", "retry-boundary.jsonl"),
		liveRollout("session-live-retry-boundary", "turn-live-retry-boundary"))
	runtime := newLiveRuntime(t, repository, Config{})
	job, err := runtime.Start(context.Background(), liveRequest(t, home, "request-live-retry-boundary", 12))
	if err != nil {
		t.Fatalf("Start() error = %v", err)
	}
	if err := repository.TransitionJobRun(context.Background(), store.JobTransition{
		JobID: job.JobID, ExpectedState: store.JobQueued, State: store.JobRunning,
		Phase: store.JobPhaseLive, AtMS: store.MaxSchedulerTimestampMS - 1,
	}); err != nil {
		t.Fatalf("TransitionJobRun(running) error = %v", err)
	}
	class := store.RuntimeErrorTimeout
	if err := repository.TransitionJobRun(context.Background(), store.JobTransition{
		JobID: job.JobID, ExpectedState: store.JobRunning, State: store.JobFailed,
		Phase: store.JobPhaseLive, ErrorClass: &class, AtMS: store.MaxSchedulerTimestampMS,
	}); err != nil {
		t.Fatalf("TransitionJobRun(failed) error = %v", err)
	}
	if _, err := runtime.Retry(context.Background(), job.JobID); !errors.Is(err, ErrInvalidRuntime) {
		t.Fatalf("Retry(runtime timestamp exhausted) error = %v, want ErrInvalidRuntime", err)
	}
	stored, _, err := repository.LiveScanRun(context.Background(), job.JobID)
	if err != nil || stored.State != store.JobFailed || stored.UpdatedAtMS != store.MaxSchedulerTimestampMS {
		t.Fatalf("LiveScanRun() = %#v, %v", stored, err)
	}
}

func TestRuntimeTimeBudgetStillFailsWhenSnapshotDriftsAfterChunk(t *testing.T) {
	t.Parallel()

	repository := openLiveRepository(t)
	home := t.TempDir()
	path := filepath.Join(home, "sessions", "time-drift.jsonl")
	writeLiveFile(t, path, largeLiveRollout("session-live-time-drift", "turn-live-time-drift"))
	request := liveRequest(t, home, "request-live-time-drift", 11)
	base := time.Now().Add(time.Minute)
	var clockMu sync.Mutex
	runPhase := false
	runCalls := 0
	var mutateErr error
	clock := func() time.Time {
		clockMu.Lock()
		defer clockMu.Unlock()
		if !runPhase {
			return base
		}
		runCalls++
		if runCalls >= 4 {
			if runCalls == 4 {
				file, openErr := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0)
				if openErr != nil {
					mutateErr = openErr
				} else {
					_, writeErr := file.WriteString("drift\n")
					mutateErr = errors.Join(writeErr, file.Close())
				}
			}
			return base.Add(time.Second)
		}
		return base
	}
	runtime, err := New(Config{Repository: repository, ReadChunkBytes: 32, Clock: clock})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	job, err := runtime.Start(context.Background(), request)
	if err != nil {
		t.Fatalf("Start() error = %v", err)
	}
	clockMu.Lock()
	runPhase = true
	runCalls = 0
	clockMu.Unlock()
	report, err := runtime.RunSlice(context.Background(), job.JobID, SliceBudget{
		MaxFiles: 1, MaxBytes: 1 << 20, MaxActive: 500 * time.Millisecond,
	})
	clockMu.Lock()
	mutationErr := mutateErr
	clockMu.Unlock()
	if mutationErr != nil {
		t.Fatalf("append drift error = %v", mutationErr)
	}
	if !errors.Is(err, logs.ErrChangedDuringScan) || report.ExhaustedBy == SliceStopTimeBudget {
		t.Fatalf("RunSlice(time+drift) = %#v, %v, want ErrChangedDuringScan", report, err)
	}
	failed, _, readErr := repository.LiveScanRun(context.Background(), job.JobID)
	if readErr != nil || failed.State != store.JobFailed || failed.ErrorClass == nil ||
		*failed.ErrorClass != store.RuntimeErrorUnavailable {
		t.Fatalf("LiveScanRun(failed) = %#v, %v", failed, readErr)
	}
}

func openLiveRepository(t *testing.T) *store.Repository {
	t.Helper()
	directory := t.TempDir()
	if err := os.Chmod(directory, 0o700); err != nil {
		t.Fatalf("Chmod(temp) error = %v", err)
	}
	database, err := storesqlite.Open(context.Background(), storesqlite.Config{
		Path: filepath.Join(directory, "live.db"),
	})
	if err != nil {
		t.Fatalf("sqlite.Open() error = %v", err)
	}
	t.Cleanup(func() { _ = database.Close(context.Background()) })
	repository := store.NewRepository(database)
	if err := repository.EnsureApplicationSchema(context.Background()); err != nil {
		t.Fatalf("EnsureApplicationSchema() error = %v", err)
	}
	return repository
}

func newLiveRuntime(t *testing.T, repository *store.Repository, config Config) *Runtime {
	t.Helper()
	config.Repository = repository
	var mu sync.Mutex
	now := time.Now().Add(time.Minute).UnixMilli()
	config.Clock = func() time.Time {
		mu.Lock()
		defer mu.Unlock()
		now++
		return time.UnixMilli(now)
	}
	runtime, err := New(config)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	return runtime
}

func liveRequest(t *testing.T, home, requestID string, generation int64) LiveRequest {
	t.Helper()
	metadata, err := logs.NewHomeProbe().Probe(context.Background(), home)
	if err != nil {
		t.Fatalf("HomeProbe() error = %v", err)
	}
	discoverer, err := logs.NewConfirmedDiscoverer(metadata.Path, metadata.DeviceID, metadata.Inode)
	if err != nil {
		t.Fatalf("NewDiscoverer() error = %v", err)
	}
	discovery, err := discoverer.Discover(context.Background())
	if err != nil || len(discovery.Snapshots) != 1 {
		t.Fatalf("Discover() = %#v, %v", discovery, err)
	}
	plan, err := logs.PlanReconcile(metadata.Path, nil, discovery)
	if err != nil || len(plan.Actions) != 1 {
		t.Fatalf("PlanReconcile() = %#v, %v", plan, err)
	}
	return LiveRequest{
		RequestID: requestID, HomeGeneration: generation,
		HomePath: metadata.Path, HomeDeviceID: metadata.DeviceID, HomeInode: metadata.Inode,
		Action: plan.Actions[0], RequestedAtMS: time.Now().UnixMilli(),
	}
}

func writeLiveFile(t *testing.T, path string, content []byte) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatalf("MkdirAll() error = %v", err)
	}
	if err := os.WriteFile(path, content, 0o600); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}
}

func liveRollout(sessionID, turnID string) []byte {
	return []byte(liveSessionMetaLine(sessionID) + "\n" +
		`{"timestamp":"2026-07-14T01:00:01Z","type":"event_msg","payload":{"type":"task_started","turn_id":"` + turnID +
		`","started_at":1783990801,"model_context_window":258000}}` + "\n" +
		`{"timestamp":"2026-07-14T01:00:02Z","type":"event_msg","payload":{"type":"task_complete","turn_id":"` + turnID +
		`","completed_at":1783990802}}` + "\n")
}

func largeLiveRollout(sessionID, turnID string) []byte {
	return append(liveRollout(sessionID, turnID), []byte(
		`{"timestamp":"2026-07-14T01:00:03Z","type":"response_item","payload":{"type":"message","content":[{"type":"output_text","text":"`+
			strings.Repeat("x", int(logs.PrefixLimitBytes))+`"}]}}`+"\n",
	)...)
}

func liveSessionMetaLine(sessionID string) string {
	return `{"timestamp":"2026-07-14T01:00:00Z","type":"session_meta","payload":{"id":"` + sessionID +
		`","timestamp":"2026-07-14T01:00:00Z","cwd":"/tmp/project","originator":"codex_cli_rs","cli_version":"0.142.3","source":"cli","model_provider":"openai"}}`
}
