package bootstrap

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/SisyphusSQ/codex-pulse/internal/codex/logs"
	"github.com/SisyphusSQ/codex-pulse/internal/indexer"
	"github.com/SisyphusSQ/codex-pulse/internal/preferences"
	"github.com/SisyphusSQ/codex-pulse/internal/store"
	storesqlite "github.com/SisyphusSQ/codex-pulse/internal/store/sqlite"
)

func TestRuntimeRunsFourStagesAndReplaysCompletedBootstrap(t *testing.T) {
	t.Parallel()

	repository := openBootstrapRepository(t)
	home := t.TempDir()
	recentPath := filepath.Join(home, "sessions", "2026", "07", "recent.jsonl")
	oldPath := filepath.Join(home, "archived_sessions", "old.jsonl")
	writeBootstrapRollout(t, recentPath, completeBootstrapRollout("session-recent", "turn-recent"), time.Now().Add(-time.Hour))
	writeBootstrapRollout(t, oldPath, completeBootstrapRollout("session-old", "turn-old"), time.Now().Add(-60*24*time.Hour))
	request := bootstrapRequest(t, home, "switch-normal", 2)
	runtime := newTestRuntime(t, repository, RuntimeConfig{
		FastMaxFiles: 1, FastMaxBytes: 1 << 20, ReadChunkBytes: 64,
	}, runtimeHooks{})

	if err := runtime.StartBootstrap(context.Background(), request); err != nil {
		t.Fatalf("StartBootstrap() error = %v", err)
	}
	if err := runtime.StartBootstrap(context.Background(), request); err != nil {
		t.Fatalf("StartBootstrap(replay) error = %v", err)
	}
	status, err := runtime.BootstrapStatus(context.Background(), request.SwitchID, request.Generation)
	if err != nil || status != preferences.BootstrapStatusRunning {
		t.Fatalf("BootstrapStatus(planned) = %s, %v", status, err)
	}
	job, facts, err := repository.BootstrapRunByIdentity(
		context.Background(), request.SwitchID, int64(request.Generation),
	)
	if err != nil || facts.PlanState != store.BootstrapPlanReady {
		t.Fatalf("BootstrapRunByIdentity() = %#v %#v, %v", job, facts, err)
	}
	items, err := repository.ListBootstrapPlanItems(
		context.Background(), store.BootstrapPlanItemFilter{JobID: job.JobID},
	)
	if err != nil || len(items) != 2 || items[0].Lane != store.BootstrapLaneFast ||
		items[1].Lane != store.BootstrapLaneBackfill {
		t.Fatalf("initial plan = %#v, %v", items, err)
	}

	report, err := runtime.Run(context.Background(), job.JobID)
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if !report.FirstScreenReady || !report.FullHistoryReady || report.Phase != store.JobPhaseReconcile {
		t.Fatalf("Run() report = %#v", report)
	}
	job, facts, err = repository.BootstrapRun(context.Background(), job.JobID)
	if err != nil || job.State != store.JobSucceeded || facts.ETAState != store.BootstrapETAComplete ||
		facts.FirstScreenReadyAtMS == nil || facts.FullHistoryReadyAtMS == nil ||
		facts.ReconciledAtMS == nil || *facts.FirstScreenReadyAtMS >= *facts.FullHistoryReadyAtMS {
		t.Fatalf("completed run = %#v %#v, %v", job, facts, err)
	}
	for _, sessionID := range []string{"session-recent", "session-old"} {
		if _, err := repository.Session(context.Background(), sessionID); err != nil {
			t.Fatalf("Session(%s) error = %v", sessionID, err)
		}
	}
	if _, err := runtime.Run(context.Background(), job.JobID); err != nil {
		t.Fatalf("Run(completed replay) error = %v", err)
	}
	movedHome := home + "-after-completion"
	if err := os.Rename(home, movedHome); err != nil {
		t.Fatalf("Rename(completed Home) error = %v", err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(movedHome) })
	if err := runtime.StartBootstrap(context.Background(), request); err != nil {
		t.Fatalf("StartBootstrap(completed replay) error = %v", err)
	}
}

func TestRuntimeRunSliceYieldsAtByteBudgetAndResumesFromCommittedOffset(t *testing.T) {
	t.Parallel()

	repository := openBootstrapRepository(t)
	home := t.TempDir()
	content := largeBootstrapRollout("session-slice-byte", "turn-slice-byte")
	path := filepath.Join(home, "sessions", "slice-byte.jsonl")
	writeBootstrapRollout(t, path, content, time.Now().Add(-time.Hour))
	request := bootstrapRequest(t, home, "switch-slice-byte", 101)
	runtime := newTestRuntime(t, repository, RuntimeConfig{ReadChunkBytes: 32}, runtimeHooks{})
	if err := runtime.StartBootstrap(context.Background(), request); err != nil {
		t.Fatalf("StartBootstrap() error = %v", err)
	}
	job, _, _ := repository.BootstrapRunByIdentity(context.Background(), request.SwitchID, 101)
	first, err := runtime.RunSlice(context.Background(), job.JobID, SliceBudget{
		MaxFiles: 1, MaxBytes: logs.PrefixLimitBytes, MaxActive: time.Minute,
	})
	if err != nil {
		t.Fatalf("RunSlice(first) error = %v", err)
	}
	if first.Complete || first.ExhaustedBy != SliceStopByteBudget || first.FilesProcessed != 1 ||
		first.BytesRead != logs.PrefixLimitBytes || first.State != store.JobRunning {
		t.Fatalf("RunSlice(first) = %#v", first)
	}
	items, err := repository.ListBootstrapPlanItems(context.Background(), store.BootstrapPlanItemFilter{
		JobID: job.JobID,
	})
	if err != nil || len(items) != 1 || items[0].State != store.BootstrapItemRunning {
		t.Fatalf("plan items after first slice = %#v, %v", items, err)
	}
	file, err := repository.SourceFile(context.Background(), items[0].Current.SourceFileID)
	if err != nil {
		t.Fatalf("SourceFile() = %#v, %v", file, err)
	}
	cursor, err := repository.BuildingGenerationCursor(context.Background(), file.SourceFileID)
	if err != nil || cursor.Checkpoint.CommittedOffset <= 0 ||
		cursor.Checkpoint.CommittedOffset >= logs.PrefixLimitBytes {
		t.Fatalf("BuildingGenerationCursor() = %#v, %v, want committed offset within prefix", cursor, err)
	}
	committedOffset := cursor.Checkpoint.CommittedOffset

	secondBudget := logs.PrefixLimitBytes + int64(len(content)) - committedOffset
	second, err := runtime.RunSlice(context.Background(), job.JobID, SliceBudget{
		MaxFiles: 1, MaxBytes: secondBudget,
		MaxActive: time.Minute,
	})
	if err != nil {
		t.Fatalf("RunSlice(second) error = %v", err)
	}
	if !second.Complete || second.ExhaustedBy != SliceStopCompleted ||
		second.FilesProcessed != 1 || second.BytesRead <= logs.PrefixLimitBytes ||
		second.BytesRead > secondBudget ||
		second.State != store.JobSucceeded {
		t.Fatalf("RunSlice(second) = %#v", second)
	}
	if _, err := repository.Session(context.Background(), "session-slice-byte"); err != nil {
		t.Fatalf("Session() error = %v", err)
	}
}

func TestRuntimeRunSliceTimeBudgetStillMarksSnapshotDrift(t *testing.T) {
	t.Parallel()

	repository := openBootstrapRepository(t)
	home := t.TempDir()
	path := filepath.Join(home, "sessions", "slice-time-drift.jsonl")
	writeBootstrapRollout(t, path,
		largeBootstrapRollout("session-slice-time-drift", "turn-slice-time-drift"), time.Now())
	request := bootstrapRequest(t, home, "switch-slice-time-drift", 110)
	base := time.Now().Add(time.Minute)
	var clockMu sync.Mutex
	advanced := false
	clock := func() time.Time {
		clockMu.Lock()
		defer clockMu.Unlock()
		if advanced {
			return base.Add(time.Second)
		}
		return base
	}
	var driftOnce sync.Once
	runtime, err := newRuntime(RuntimeConfig{
		Repository: repository, ReadChunkBytes: 32, Clock: clock,
	}, runtimeHooks{afterChunk: func(store.BootstrapPlanItem, int64) {
		driftOnce.Do(func() {
			file, openErr := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0)
			if openErr != nil {
				t.Fatalf("OpenFile(drift) error = %v", openErr)
			}
			_, writeErr := file.WriteString("drift\n")
			closeErr := file.Close()
			if err := errors.Join(writeErr, closeErr); err != nil {
				t.Fatalf("append drift error = %v", err)
			}
			clockMu.Lock()
			advanced = true
			clockMu.Unlock()
		})
	}})
	if err != nil {
		t.Fatalf("newRuntime() error = %v", err)
	}
	if err := runtime.StartBootstrap(context.Background(), request); err != nil {
		t.Fatalf("StartBootstrap() error = %v", err)
	}
	job, _, _ := repository.BootstrapRunByIdentity(context.Background(), request.SwitchID, 110)
	report, err := runtime.RunSlice(context.Background(), job.JobID, SliceBudget{
		MaxFiles: 1, MaxBytes: 1 << 20, MaxActive: 500 * time.Millisecond,
	})
	if err != nil || report.ExhaustedBy != SliceStopTimeBudget {
		t.Fatalf("RunSlice() = %#v, %v, want time budget", report, err)
	}
	items, err := repository.ListBootstrapPlanItems(
		context.Background(), store.BootstrapPlanItemFilter{JobID: job.JobID},
	)
	if err != nil || len(items) < 1 || items[0].State != store.BootstrapItemDrifted {
		t.Fatalf("plan items after time+drift = %#v, %v, want drifted", items, err)
	}
}

func TestRuntimeRunSliceHonorsFileBudgetAndExactCompletionBoundary(t *testing.T) {
	t.Parallel()

	t.Run("file budget", func(t *testing.T) {
		t.Parallel()
		repository := openBootstrapRepository(t)
		home := t.TempDir()
		for index := 1; index <= 2; index++ {
			content := completeBootstrapRollout(
				fmt.Sprintf("session-slice-file-%d", index), fmt.Sprintf("turn-slice-file-%d", index),
			)
			writeBootstrapRollout(t, filepath.Join(home, "sessions", fmt.Sprintf("%d.jsonl", index)),
				content, time.Now().Add(-time.Duration(index)*time.Hour))
		}
		request := bootstrapRequest(t, home, "switch-slice-files", 102)
		runtime := newTestRuntime(t, repository, RuntimeConfig{
			FastMaxFiles: 2, FastMaxBytes: 1 << 20, ReadChunkBytes: 64,
		}, runtimeHooks{})
		if err := runtime.StartBootstrap(context.Background(), request); err != nil {
			t.Fatalf("StartBootstrap() error = %v", err)
		}
		job, _, _ := repository.BootstrapRunByIdentity(context.Background(), request.SwitchID, 102)

		first, err := runtime.RunSlice(context.Background(), job.JobID, SliceBudget{
			MaxFiles: 1, MaxBytes: 1 << 20, MaxActive: time.Minute,
		})
		if err != nil || first.Complete || first.ExhaustedBy != SliceStopFileBudget ||
			first.FilesProcessed != 1 {
			t.Fatalf("RunSlice(first) = %#v, %v", first, err)
		}
		second, err := runtime.RunSlice(context.Background(), job.JobID, SliceBudget{
			MaxFiles: 1, MaxBytes: 1 << 20, MaxActive: time.Minute,
		})
		if err != nil || !second.Complete || second.ExhaustedBy != SliceStopCompleted ||
			second.FilesProcessed != 1 {
			t.Fatalf("RunSlice(second) = %#v, %v", second, err)
		}
	})

	t.Run("exact EOF", func(t *testing.T) {
		t.Parallel()
		repository := openBootstrapRepository(t)
		home := t.TempDir()
		content := completeBootstrapRollout("session-slice-exact", "turn-slice-exact")
		writeBootstrapRollout(t, filepath.Join(home, "sessions", "exact.jsonl"), content, time.Now())
		request := bootstrapRequest(t, home, "switch-slice-exact", 103)
		runtime := newTestRuntime(t, repository, RuntimeConfig{ReadChunkBytes: 64}, runtimeHooks{})
		if err := runtime.StartBootstrap(context.Background(), request); err != nil {
			t.Fatalf("StartBootstrap() error = %v", err)
		}
		job, _, _ := repository.BootstrapRunByIdentity(context.Background(), request.SwitchID, 103)
		report, err := runtime.RunSlice(context.Background(), job.JobID, SliceBudget{
			MaxFiles: 1, MaxBytes: logs.PrefixLimitBytes, MaxActive: time.Minute,
		})
		if err != nil || !report.Complete || report.ExhaustedBy != SliceStopCompleted ||
			report.BytesRead != int64(len(content)) {
			t.Fatalf("RunSlice(exact EOF) = %#v, %v", report, err)
		}
	})
}

func TestRuntimeRunSliceRejectsInvalidBudgetWithoutChangingJob(t *testing.T) {
	t.Parallel()

	repository := openBootstrapRepository(t)
	home := t.TempDir()
	writeBootstrapRollout(t, filepath.Join(home, "sessions", "invalid.jsonl"),
		completeBootstrapRollout("session-slice-invalid", "turn-slice-invalid"), time.Now())
	request := bootstrapRequest(t, home, "switch-slice-invalid", 104)
	runtime := newTestRuntime(t, repository, RuntimeConfig{}, runtimeHooks{})
	if err := runtime.StartBootstrap(context.Background(), request); err != nil {
		t.Fatalf("StartBootstrap() error = %v", err)
	}
	job, _, _ := repository.BootstrapRunByIdentity(context.Background(), request.SwitchID, 104)
	for _, budget := range []SliceBudget{
		{},
		{MaxFiles: -1, MaxBytes: 1, MaxActive: time.Second},
		{MaxFiles: 1, MaxBytes: -1, MaxActive: time.Second},
		{MaxFiles: 1, MaxBytes: 1, MaxActive: -1},
	} {
		if _, err := runtime.RunSlice(context.Background(), job.JobID, budget); !errors.Is(err, ErrInvalidSliceBudget) {
			t.Fatalf("RunSlice(%#v) error = %v, want ErrInvalidSliceBudget", budget, err)
		}
	}
	stored, _, err := repository.BootstrapRun(context.Background(), job.JobID)
	if err != nil || stored.State != store.JobRunning || stored.Phase != store.JobPhaseDiscover {
		t.Fatalf("BootstrapRun() = %#v, %v", stored, err)
	}
}

func TestRuntimeSchedulerTargetInterruptAndRecoverAreIdempotent(t *testing.T) {
	t.Parallel()

	repository := openBootstrapRepository(t)
	home := t.TempDir()
	writeBootstrapRollout(t, filepath.Join(home, "sessions", "target-recover.jsonl"),
		completeBootstrapRollout("session-target-recover", "turn-target-recover"), time.Now())
	request := bootstrapRequest(t, home, "switch-target-recover", 105)
	runtime := newTestRuntime(t, repository, RuntimeConfig{}, runtimeHooks{})
	if err := runtime.StartBootstrap(context.Background(), request); err != nil {
		t.Fatalf("StartBootstrap() error = %v", err)
	}
	job, _, _ := repository.BootstrapRunByIdentity(context.Background(), request.SwitchID, 105)
	if err := runtime.Interrupt(context.Background(), job.JobID, store.RuntimeErrorUnknown); err != nil {
		t.Fatalf("Interrupt() error = %v", err)
	}
	if err := runtime.Interrupt(context.Background(), job.JobID, store.RuntimeErrorUnknown); err != nil {
		t.Fatalf("Interrupt(replay) error = %v", err)
	}
	resumedJob, err := runtime.Recover(context.Background(), job.JobID)
	if err != nil {
		t.Fatalf("Recover() error = %v", err)
	}
	if replay, err := runtime.Recover(context.Background(), job.JobID); err != nil || replay.JobID != resumedJob.JobID {
		t.Fatalf("Recover(replay) = %#v, %v, want %q", replay, err, resumedJob.JobID)
	}
	resumed, _, err := repository.BootstrapRun(context.Background(), resumedJob.JobID)
	if err != nil || resumed.State != store.JobQueued || resumed.ResumeOfJobID == nil ||
		*resumed.ResumeOfJobID != job.JobID {
		t.Fatalf("BootstrapRun(resumed) = %#v, %v", resumed, err)
	}
}

func TestRuntimeCompletesEmptyHomeWithDistinctReadinessFacts(t *testing.T) {
	t.Parallel()

	repository := openBootstrapRepository(t)
	home := t.TempDir()
	request := bootstrapRequest(t, home, "switch-empty", 3)
	runtime := newTestRuntime(t, repository, RuntimeConfig{}, runtimeHooks{})
	if err := runtime.StartBootstrap(context.Background(), request); err != nil {
		t.Fatalf("StartBootstrap() error = %v", err)
	}
	job, _, _ := repository.BootstrapRunByIdentity(context.Background(), request.SwitchID, 3)
	if _, err := runtime.Run(context.Background(), job.JobID); err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	job, facts, err := repository.BootstrapRun(context.Background(), job.JobID)
	if err != nil || job.State != store.JobSucceeded || facts.FirstScreenReadyAtMS == nil ||
		facts.ReconcilePlanAtMS == nil || facts.FullHistoryReadyAtMS == nil ||
		*facts.FirstScreenReadyAtMS >= *facts.ReconcilePlanAtMS ||
		*facts.ReconcilePlanAtMS > *facts.FullHistoryReadyAtMS {
		t.Fatalf("empty completed run = %#v %#v, %v", job, facts, err)
	}
}

func TestRuntimeInterruptsAndResumesFromDurableSourceCheckpointAfterRestart(t *testing.T) {
	t.Parallel()

	repository := openBootstrapRepository(t)
	home := t.TempDir()
	path := filepath.Join(home, "sessions", "resume.jsonl")
	writeBootstrapRollout(t, path, completeBootstrapRollout("session-resume", "turn-resume"), time.Now().Add(-time.Hour))
	request := bootstrapRequest(t, home, "switch-resume", 4)
	ctx, cancel := context.WithCancel(context.Background())
	var cancelOnce sync.Once
	runtime := newTestRuntime(t, repository, RuntimeConfig{ReadChunkBytes: 32}, runtimeHooks{
		afterChunk: func(store.BootstrapPlanItem, int64) { cancelOnce.Do(cancel) },
	})
	if err := runtime.StartBootstrap(context.Background(), request); err != nil {
		t.Fatalf("StartBootstrap() error = %v", err)
	}
	job, _, _ := repository.BootstrapRunByIdentity(context.Background(), request.SwitchID, 4)
	if _, err := runtime.Run(ctx, job.JobID); !errors.Is(err, context.Canceled) {
		t.Fatalf("Run(cancel) error = %v, want context.Canceled", err)
	}
	status, err := runtime.BootstrapStatus(context.Background(), request.SwitchID, 4)
	if err != nil || status != preferences.BootstrapStatusFailedNeedsResume {
		t.Fatalf("BootstrapStatus(interrupted) = %s, %v", status, err)
	}

	restarted := newTestRuntime(t, repository, RuntimeConfig{ReadChunkBytes: 32}, runtimeHooks{})
	if err := restarted.Resume(context.Background(), 4); err != nil {
		t.Fatalf("Resume() error = %v", err)
	}
	if err := restarted.Resume(context.Background(), 4); err != nil {
		t.Fatalf("Resume(replay) error = %v", err)
	}
	resumed, _, err := repository.BootstrapRunByIdentity(context.Background(), request.SwitchID, 4)
	if err != nil || resumed.ResumeOfJobID == nil || *resumed.ResumeOfJobID != job.JobID {
		t.Fatalf("resumed job = %#v, %v", resumed, err)
	}
	if err := restarted.StartBootstrap(context.Background(), request); err != nil {
		t.Fatalf("StartBootstrap(exact replay after Resume) error = %v", err)
	}
	if _, err := restarted.Run(context.Background(), resumed.JobID); err != nil {
		t.Fatalf("Run(resumed) error = %v", err)
	}
	if _, err := repository.Session(context.Background(), "session-resume"); err != nil {
		t.Fatalf("Session(resumed) error = %v", err)
	}
}

func TestRuntimeCatchesUpItemAfterSourceCommitWinsCancellationRace(t *testing.T) {
	t.Parallel()

	repository := openBootstrapRepository(t)
	home := t.TempDir()
	path := filepath.Join(home, "sessions", "commit-gap.jsonl")
	content := completeBootstrapRollout("session-commit-gap", "turn-commit-gap")
	writeBootstrapRollout(t, path, content, time.Now())
	request := bootstrapRequest(t, home, "switch-commit-gap", 8)
	ctx, cancel := context.WithCancel(context.Background())
	var cancelOnce sync.Once
	runtime := newTestRuntime(t, repository, RuntimeConfig{ReadChunkBytes: 32}, runtimeHooks{
		afterChunk: func(_ store.BootstrapPlanItem, offset int64) {
			if offset == int64(len(content)) {
				cancelOnce.Do(cancel)
			}
		},
	})
	if err := runtime.StartBootstrap(context.Background(), request); err != nil {
		t.Fatalf("StartBootstrap() error = %v", err)
	}
	job, _, _ := repository.BootstrapRunByIdentity(context.Background(), request.SwitchID, 8)
	if _, err := runtime.Run(ctx, job.JobID); !errors.Is(err, context.Canceled) {
		t.Fatalf("Run(cancel after source commit) error = %v", err)
	}
	initialItems, err := repository.ListBootstrapPlanItems(
		context.Background(), store.BootstrapPlanItemFilter{JobID: job.JobID},
	)
	if err != nil || len(initialItems) != 1 || initialItems[0].State != store.BootstrapItemRunning ||
		initialItems[0].ProgressCurrent != 0 {
		t.Fatalf("lagging bootstrap item = %#v, %v", initialItems, err)
	}
	file, err := repository.SourceFile(context.Background(), initialItems[0].Current.SourceFileID)
	if err != nil || file.State != store.SourceFileActive || file.ParsedOffset != int64(len(content)) {
		t.Fatalf("authoritative source = %#v, %v", file, err)
	}

	restarted := newTestRuntime(t, repository, RuntimeConfig{ReadChunkBytes: 32}, runtimeHooks{})
	if err := restarted.Resume(context.Background(), 8); err != nil {
		t.Fatalf("Resume() error = %v", err)
	}
	resumed, _, _ := repository.BootstrapRunByIdentity(context.Background(), request.SwitchID, 8)
	if _, err := restarted.Run(context.Background(), resumed.JobID); err != nil {
		t.Fatalf("Run(resume after source commit) error = %v", err)
	}
	items, err := repository.ListBootstrapPlanItems(
		context.Background(), store.BootstrapPlanItemFilter{JobID: resumed.JobID},
	)
	if err != nil || len(items) != 1 || items[0].State != store.BootstrapItemSucceeded ||
		items[0].ProgressCurrent != int64(len(content)) {
		t.Fatalf("caught-up bootstrap item = %#v, %v", items, err)
	}
}

func TestRuntimeResumesIncompleteActiveAppendInsteadOfTrustingFingerprintAlone(t *testing.T) {
	t.Parallel()

	repository := openBootstrapRepository(t)
	home := t.TempDir()
	path := filepath.Join(home, "sessions", "active-append.jsonl")
	initial := []byte(bootstrapSessionMetaLine("session-active-append") + "\n")
	writeBootstrapRollout(t, path, initial, time.Now().Add(-time.Hour))
	initialRequest := bootstrapRequest(t, home, "switch-active-initial", 30)
	initialRuntime := newTestRuntime(t, repository, RuntimeConfig{}, runtimeHooks{})
	if err := initialRuntime.StartBootstrap(context.Background(), initialRequest); err != nil {
		t.Fatalf("StartBootstrap(initial) error = %v", err)
	}
	initialJob, _, _ := repository.BootstrapRunByIdentity(
		context.Background(), initialRequest.SwitchID, 30,
	)
	if _, err := initialRuntime.Run(context.Background(), initialJob.JobID); err != nil {
		t.Fatalf("Run(initial) error = %v", err)
	}

	turnStart := []byte(bootstrapTurnStartLine("turn-active-append") + "\n")
	turnEnd := []byte(bootstrapTurnEndLine("turn-active-append") + "\n")
	grown := append(append(append([]byte(nil), initial...), turnStart...), turnEnd...)
	writeBootstrapRollout(t, path, grown, time.Now())
	request := bootstrapRequest(t, home, "switch-active-partial", 31)
	ctx, cancel := context.WithCancel(context.Background())
	partialOffset := int64(len(initial) + len(turnStart))
	var cancelOnce sync.Once
	runtime := newTestRuntime(t, repository, RuntimeConfig{ReadChunkBytes: len(turnStart)}, runtimeHooks{
		afterChunk: func(_ store.BootstrapPlanItem, offset int64) {
			if offset == partialOffset {
				cancelOnce.Do(cancel)
			}
		},
	})
	if err := runtime.StartBootstrap(context.Background(), request); err != nil {
		t.Fatalf("StartBootstrap(grown) error = %v", err)
	}
	job, _, _ := repository.BootstrapRunByIdentity(context.Background(), request.SwitchID, 31)
	if _, err := runtime.Run(ctx, job.JobID); !errors.Is(err, context.Canceled) {
		t.Fatalf("Run(partial append) error = %v, want context.Canceled", err)
	}
	items, err := repository.ListBootstrapPlanItems(
		context.Background(), store.BootstrapPlanItemFilter{JobID: job.JobID},
	)
	if err != nil || len(items) != 1 || items[0].Current == nil {
		t.Fatalf("partial plan items = %#v, %v", items, err)
	}
	file, err := repository.SourceFile(context.Background(), items[0].Current.SourceFileID)
	if err != nil {
		t.Fatalf("SourceFile(partial) error = %v", err)
	}
	cursor, err := repository.GenerationCursor(
		context.Background(), file.SourceFileID, file.ActiveGeneration,
	)
	if err != nil || cursor.Fingerprint != *items[0].Current ||
		cursor.Checkpoint.CommittedOffset != partialOffset || partialOffset >= int64(len(grown)) {
		t.Fatalf("partial authoritative cursor = %#v, %v", cursor, err)
	}

	restarted := newTestRuntime(t, repository, RuntimeConfig{ReadChunkBytes: len(turnStart)}, runtimeHooks{})
	if err := restarted.Resume(context.Background(), 31); err != nil {
		t.Fatalf("Resume() error = %v", err)
	}
	resumed, _, _ := repository.BootstrapRunByIdentity(context.Background(), request.SwitchID, 31)
	if _, err := restarted.Run(context.Background(), resumed.JobID); err != nil {
		t.Fatalf("Run(resumed partial append) error = %v", err)
	}
	assertCompletedBootstrapTurnAndCursor(
		t, repository, "turn-active-append", items[0].Current.SourceFileID, int64(len(grown)),
	)
}

func TestRuntimeInitialPlanIncludesIncompleteUnchangedActiveSource(t *testing.T) {
	t.Parallel()

	repository := openBootstrapRepository(t)
	home := t.TempDir()
	path := filepath.Join(home, "sessions", "cold-incomplete.jsonl")
	initial := []byte(bootstrapSessionMetaLine("session-cold-incomplete") + "\n")
	writeBootstrapRollout(t, path, initial, time.Now().Add(-time.Hour))
	seedRequest := bootstrapRequest(t, home, "switch-cold-seed", 32)
	seedRuntime := newTestRuntime(t, repository, RuntimeConfig{}, runtimeHooks{})
	if err := seedRuntime.StartBootstrap(context.Background(), seedRequest); err != nil {
		t.Fatalf("StartBootstrap(seed) error = %v", err)
	}
	seedJob, _, _ := repository.BootstrapRunByIdentity(context.Background(), seedRequest.SwitchID, 32)
	if _, err := seedRuntime.Run(context.Background(), seedJob.JobID); err != nil {
		t.Fatalf("Run(seed) error = %v", err)
	}

	turnStart := []byte(bootstrapTurnStartLine("turn-cold-incomplete") + "\n")
	turnEnd := []byte(bootstrapTurnEndLine("turn-cold-incomplete") + "\n")
	grown := append(append(append([]byte(nil), initial...), turnStart...), turnEnd...)
	writeBootstrapRollout(t, path, grown, time.Now())
	partialRequest := bootstrapRequest(t, home, "switch-cold-partial", 33)
	ctx, cancel := context.WithCancel(context.Background())
	partialOffset := int64(len(initial) + len(turnStart))
	partialRuntime := newTestRuntime(t, repository, RuntimeConfig{ReadChunkBytes: len(turnStart)}, runtimeHooks{
		afterChunk: func(_ store.BootstrapPlanItem, offset int64) {
			if offset == partialOffset {
				cancel()
			}
		},
	})
	if err := partialRuntime.StartBootstrap(context.Background(), partialRequest); err != nil {
		t.Fatalf("StartBootstrap(partial) error = %v", err)
	}
	partialJob, _, _ := repository.BootstrapRunByIdentity(context.Background(), partialRequest.SwitchID, 33)
	if _, err := partialRuntime.Run(ctx, partialJob.JobID); !errors.Is(err, context.Canceled) {
		t.Fatalf("Run(partial) error = %v, want context.Canceled", err)
	}

	coldRequest := bootstrapRequest(t, home, "switch-cold-recovery", 34)
	coldRuntime := newTestRuntime(t, repository, RuntimeConfig{ReadChunkBytes: len(turnStart)}, runtimeHooks{})
	if err := coldRuntime.StartBootstrap(context.Background(), coldRequest); err != nil {
		t.Fatalf("StartBootstrap(cold recovery) error = %v", err)
	}
	coldJob, _, _ := repository.BootstrapRunByIdentity(context.Background(), coldRequest.SwitchID, 34)
	items, err := repository.ListBootstrapPlanItems(
		context.Background(), store.BootstrapPlanItemFilter{JobID: coldJob.JobID},
	)
	if err != nil || len(items) != 1 || items[0].ActionKind != store.BootstrapActionUnchanged ||
		items[0].Tier != store.BootstrapTierActiveAppend || items[0].Lane != store.BootstrapLaneFast {
		t.Fatalf("cold incomplete plan = %#v, %v", items, err)
	}
	if _, err := coldRuntime.Run(context.Background(), coldJob.JobID); err != nil {
		t.Fatalf("Run(cold recovery) error = %v", err)
	}
	assertCompletedBootstrapTurnAndCursor(
		t, repository, "turn-cold-incomplete", items[0].Current.SourceFileID, int64(len(grown)),
	)
}

func TestRuntimeFinalReconcileCapturesDirectoryDrift(t *testing.T) {
	t.Parallel()

	repository := openBootstrapRepository(t)
	home := t.TempDir()
	writeBootstrapRollout(t, filepath.Join(home, "sessions", "initial.jsonl"),
		completeBootstrapRollout("session-initial", "turn-initial"), time.Now().Add(-time.Hour))
	request := bootstrapRequest(t, home, "switch-drift", 5)
	var addOnce sync.Once
	runtime := newTestRuntime(t, repository, RuntimeConfig{}, runtimeHooks{
		beforeReconcile: func() {
			addOnce.Do(func() {
				writeBootstrapRollout(t, filepath.Join(home, "sessions", "late.jsonl"),
					completeBootstrapRollout("session-late", "turn-late"), time.Now())
			})
		},
	})
	if err := runtime.StartBootstrap(context.Background(), request); err != nil {
		t.Fatalf("StartBootstrap() error = %v", err)
	}
	job, _, _ := repository.BootstrapRunByIdentity(context.Background(), request.SwitchID, 5)
	if _, err := runtime.Run(context.Background(), job.JobID); err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	_, facts, _ := repository.BootstrapRun(context.Background(), job.JobID)
	items, err := repository.ListBootstrapPlanItems(
		context.Background(), store.BootstrapPlanItemFilter{JobID: job.JobID},
	)
	if err != nil || facts.ReconcileChangeCount != 1 || len(items) != 2 ||
		items[1].Lane != store.BootstrapLaneReconcile || items[1].State != store.BootstrapItemSucceeded {
		t.Fatalf("drift reconcile = %#v %#v, %v", facts, items, err)
	}
	if _, err := repository.Session(context.Background(), "session-late"); err != nil {
		t.Fatalf("Session(late) error = %v", err)
	}
}

func TestRuntimeRejectsConfirmedHomeReplacementAcrossBootstrapStages(t *testing.T) {
	t.Parallel()

	t.Run("before initial discovery", func(t *testing.T) {
		repository := openBootstrapRepository(t)
		home := filepath.Join(t.TempDir(), "home")
		path := filepath.Join(home, "sessions", "initial.jsonl")
		writeBootstrapRollout(t, path,
			completeBootstrapRollout("session-original", "turn-original"), time.Now())
		request := bootstrapRequest(t, home, "switch-root-initial", 21)
		runtime := newTestRuntime(t, repository, RuntimeConfig{}, runtimeHooks{
			afterHomeProbe: func(context.Context) {
				replaceBootstrapHomeWithHardlinks(t, home, []string{"sessions/initial.jsonl"})
				writeBootstrapRollout(t, filepath.Join(home, "sessions", "replacement.jsonl"),
					completeBootstrapRollout("session-replacement", "turn-replacement"), time.Now())
			},
		})
		if err := runtime.StartBootstrap(context.Background(), request); !errors.Is(err, logs.ErrHomeChanged) {
			t.Fatalf("StartBootstrap(replaced root) error = %v, want ErrHomeChanged", err)
		}
		if _, err := repository.Session(context.Background(), "session-replacement"); !errors.Is(err, store.ErrNotFound) {
			t.Fatalf("Session(replacement) error = %v, want ErrNotFound", err)
		}
	})

	t.Run("before source read", func(t *testing.T) {
		repository := openBootstrapRepository(t)
		home := filepath.Join(t.TempDir(), "home")
		path := filepath.Join(home, "sessions", "initial.jsonl")
		writeBootstrapRollout(t, path,
			completeBootstrapRollout("session-original", "turn-original"), time.Now())
		request := bootstrapRequest(t, home, "switch-root-read", 22)
		runtime := newTestRuntime(t, repository, RuntimeConfig{}, runtimeHooks{})
		if err := runtime.StartBootstrap(context.Background(), request); err != nil {
			t.Fatalf("StartBootstrap() error = %v", err)
		}
		replaceBootstrapHomeWithHardlinks(t, home, []string{"sessions/initial.jsonl"})
		job, _, _ := repository.BootstrapRunByIdentity(context.Background(), request.SwitchID, 22)
		if _, err := runtime.Run(context.Background(), job.JobID); !errors.Is(err, logs.ErrHomeChanged) {
			t.Fatalf("Run(replaced root) error = %v, want ErrHomeChanged", err)
		}
		if _, err := repository.Session(context.Background(), "session-original"); !errors.Is(err, store.ErrNotFound) {
			t.Fatalf("Session(original through replacement root) error = %v, want ErrNotFound", err)
		}
	})

	t.Run("before session index hints", func(t *testing.T) {
		repository := openBootstrapRepository(t)
		home := filepath.Join(t.TempDir(), "home")
		path := filepath.Join(home, "sessions", "initial.jsonl")
		writeBootstrapRollout(t, path,
			completeBootstrapRollout("session-original", "turn-original"), time.Now())
		if err := os.WriteFile(filepath.Join(home, "session_index.jsonl"), []byte("{}\n"), 0o600); err != nil {
			t.Fatalf("WriteFile(session index) error = %v", err)
		}
		request := bootstrapRequest(t, home, "switch-root-hints", 24)
		runtime := newTestRuntime(t, repository, RuntimeConfig{}, runtimeHooks{
			afterInitialDiscovery: func(context.Context) {
				replaceBootstrapHomeWithHardlinks(
					t, home, []string{"sessions/initial.jsonl", "session_index.jsonl"},
				)
			},
		})
		if err := runtime.StartBootstrap(context.Background(), request); !errors.Is(err, logs.ErrHomeChanged) {
			t.Fatalf("StartBootstrap(replaced root before hints) error = %v, want ErrHomeChanged", err)
		}
	})

	t.Run("before final reconcile", func(t *testing.T) {
		repository := openBootstrapRepository(t)
		home := filepath.Join(t.TempDir(), "home")
		path := filepath.Join(home, "sessions", "initial.jsonl")
		writeBootstrapRollout(t, path,
			completeBootstrapRollout("session-original", "turn-original"), time.Now())
		request := bootstrapRequest(t, home, "switch-root-reconcile", 23)
		var replaceOnce sync.Once
		runtime := newTestRuntime(t, repository, RuntimeConfig{}, runtimeHooks{
			beforeReconcile: func() {
				replaceOnce.Do(func() {
					replaceBootstrapHomeWithHardlinks(t, home, []string{"sessions/initial.jsonl"})
					writeBootstrapRollout(t, filepath.Join(home, "sessions", "late.jsonl"),
						completeBootstrapRollout("session-late-root", "turn-late-root"), time.Now())
				})
			},
		})
		if err := runtime.StartBootstrap(context.Background(), request); err != nil {
			t.Fatalf("StartBootstrap() error = %v", err)
		}
		job, _, _ := repository.BootstrapRunByIdentity(context.Background(), request.SwitchID, 23)
		if _, err := runtime.Run(context.Background(), job.JobID); !errors.Is(err, logs.ErrHomeChanged) {
			t.Fatalf("Run(replaced root before reconcile) error = %v, want ErrHomeChanged", err)
		}
		if _, err := repository.Session(context.Background(), "session-late-root"); !errors.Is(err, store.ErrNotFound) {
			t.Fatalf("Session(late replacement) error = %v, want ErrNotFound", err)
		}
	})
}

func TestRuntimeFinalReconcileAppliesGrownDeletedAndUnreadableActions(t *testing.T) {
	t.Parallel()

	t.Run("grown", func(t *testing.T) {
		repository := openBootstrapRepository(t)
		home := t.TempDir()
		path := filepath.Join(home, "sessions", "grown.jsonl")
		initial := []byte(bootstrapSessionMetaLine("session-grown") + "\n")
		writeBootstrapRollout(t, path, initial, time.Now().Add(-time.Hour))
		request := bootstrapRequest(t, home, "switch-grown", 9)
		var growOnce sync.Once
		runtime := newTestRuntime(t, repository, RuntimeConfig{}, runtimeHooks{
			beforeReconcile: func() {
				growOnce.Do(func() {
					grown := append(append([]byte(nil), initial...),
						[]byte(bootstrapTurnStartLine("turn-grown")+"\n"+bootstrapTurnEndLine("turn-grown")+"\n")...)
					writeBootstrapRollout(t, path, grown, time.Now())
				})
			},
		})
		if err := runtime.StartBootstrap(context.Background(), request); err != nil {
			t.Fatalf("StartBootstrap() error = %v", err)
		}
		job, _, _ := repository.BootstrapRunByIdentity(context.Background(), request.SwitchID, 9)
		if _, err := runtime.Run(context.Background(), job.JobID); err != nil {
			t.Fatalf("Run() error = %v", err)
		}
		if _, err := repository.Turn(context.Background(), "turn-grown"); err != nil {
			t.Fatalf("Turn(grown) error = %v", err)
		}
		items, _ := repository.ListBootstrapPlanItems(
			context.Background(), store.BootstrapPlanItemFilter{JobID: job.JobID},
		)
		if len(items) != 2 || items[1].ActionKind != store.BootstrapActionGrown ||
			items[1].State != store.BootstrapItemSucceeded {
			t.Fatalf("grown reconcile items = %#v", items)
		}
	})

	t.Run("deleted", func(t *testing.T) {
		repository := openBootstrapRepository(t)
		home := t.TempDir()
		path := filepath.Join(home, "sessions", "deleted.jsonl")
		writeBootstrapRollout(t, path, completeBootstrapRollout("session-deleted", "turn-deleted"), time.Now())
		request := bootstrapRequest(t, home, "switch-deleted", 10)
		var removeOnce sync.Once
		runtime := newTestRuntime(t, repository, RuntimeConfig{}, runtimeHooks{
			beforeReconcile: func() { removeOnce.Do(func() { _ = os.Remove(path) }) },
		})
		if err := runtime.StartBootstrap(context.Background(), request); err != nil {
			t.Fatalf("StartBootstrap() error = %v", err)
		}
		job, _, _ := repository.BootstrapRunByIdentity(context.Background(), request.SwitchID, 10)
		items, _ := repository.ListBootstrapPlanItems(
			context.Background(), store.BootstrapPlanItemFilter{JobID: job.JobID},
		)
		sourceID := items[0].Current.SourceFileID
		if _, err := runtime.Run(context.Background(), job.JobID); err != nil {
			t.Fatalf("Run() error = %v", err)
		}
		file, err := repository.SourceFile(context.Background(), sourceID)
		if err != nil || file.State != store.SourceFileUnavailable {
			t.Fatalf("SourceFile(deleted) = %#v, %v", file, err)
		}
		if _, err := repository.Session(context.Background(), "session-deleted"); err != nil {
			t.Fatalf("Session(deleted source facts retained) error = %v", err)
		}
	})

	t.Run("unreadable", func(t *testing.T) {
		repository := openBootstrapRepository(t)
		home := t.TempDir()
		path := filepath.Join(home, "sessions", "unreadable.jsonl")
		writeBootstrapRollout(t, path, completeBootstrapRollout("session-unreadable", "turn-unreadable"), time.Now())
		request := bootstrapRequest(t, home, "switch-unreadable", 11)
		var replaceOnce sync.Once
		runtime := newTestRuntime(t, repository, RuntimeConfig{}, runtimeHooks{
			beforeReconcile: func() {
				replaceOnce.Do(func() {
					_ = os.Remove(path)
					_ = os.Symlink(filepath.Join(home, "outside.jsonl"), path)
				})
			},
		})
		if err := runtime.StartBootstrap(context.Background(), request); err != nil {
			t.Fatalf("StartBootstrap() error = %v", err)
		}
		job, _, _ := repository.BootstrapRunByIdentity(context.Background(), request.SwitchID, 11)
		if _, err := runtime.Run(context.Background(), job.JobID); err == nil {
			t.Fatal("Run(unreadable reconcile) error = nil")
		}
		job, facts, err := repository.BootstrapRun(context.Background(), job.JobID)
		if err != nil || job.State != store.JobFailed || facts.ReconcilePass != 1 ||
			facts.ReconcileIssueCount != 1 ||
			facts.FullHistoryReadyAtMS != nil {
			t.Fatalf("unreadable reconcile = %#v %#v, %v", job, facts, err)
		}
		originalItems, err := repository.ListBootstrapPlanItems(
			context.Background(), store.BootstrapPlanItemFilter{JobID: job.JobID},
		)
		if err != nil {
			t.Fatalf("ListBootstrapPlanItems(original unreadable) error = %v", err)
		}
		if err := os.Remove(path); err != nil {
			t.Fatalf("Remove(unreadable symlink) error = %v", err)
		}
		writeBootstrapRollout(t, path,
			completeBootstrapRollout("session-unreadable", "turn-unreadable"), time.Now())
		restarted := newTestRuntime(t, repository, RuntimeConfig{}, runtimeHooks{})
		if err := restarted.Resume(context.Background(), 11); err != nil {
			t.Fatalf("Resume(repaired unreadable) error = %v", err)
		}
		resumed, _, _ := repository.BootstrapRunByIdentity(context.Background(), request.SwitchID, 11)
		if _, err := restarted.Run(context.Background(), resumed.JobID); err != nil {
			t.Fatalf("Run(repaired unreadable) error = %v", err)
		}
		resumedItems, err := repository.ListBootstrapPlanItems(
			context.Background(), store.BootstrapPlanItemFilter{JobID: resumed.JobID},
		)
		_, resumedFacts, _ := repository.BootstrapRun(context.Background(), resumed.JobID)
		if err != nil || resumedFacts.ReconcilePass != 2 || len(resumedItems) <= len(originalItems) ||
			resumedItems[len(resumedItems)-1].Pass <= originalItems[len(originalItems)-1].Pass {
			t.Fatalf("repaired unreadable passes = original %#v resumed %#v, %v", originalItems, resumedItems, err)
		}
	})
}

func TestRuntimeFinalReconcileDriftRequiresFreshPassBeforeFullReady(t *testing.T) {
	t.Parallel()

	repository := openBootstrapRepository(t)
	home := t.TempDir()
	writeBootstrapRollout(t, filepath.Join(home, "sessions", "initial.jsonl"),
		completeBootstrapRollout("session-final-drift-initial", "turn-final-drift-initial"), time.Now())
	latePath := filepath.Join(home, "sessions", "late.jsonl")
	lateInitial := []byte(bootstrapSessionMetaLine("session-final-drift-late") + "\n")
	lateGrown := append(append([]byte(nil), lateInitial...),
		[]byte(bootstrapTurnStartLine("turn-final-drift-late")+"\n"+
			bootstrapTurnEndLine("turn-final-drift-late")+"\n")...)
	request := bootstrapRequest(t, home, "switch-final-drift", 40)
	var addOnce sync.Once
	var growOnce sync.Once
	runtime := newTestRuntime(t, repository, RuntimeConfig{}, runtimeHooks{
		beforeReconcile: func() {
			addOnce.Do(func() { writeBootstrapRollout(t, latePath, lateInitial, time.Now()) })
		},
		afterReconcilePlan: func() {
			growOnce.Do(func() { writeBootstrapRollout(t, latePath, lateGrown, time.Now()) })
		},
	})
	if err := runtime.StartBootstrap(context.Background(), request); err != nil {
		t.Fatalf("StartBootstrap() error = %v", err)
	}
	job, _, _ := repository.BootstrapRunByIdentity(context.Background(), request.SwitchID, 40)
	if _, err := runtime.Run(context.Background(), job.JobID); err == nil {
		t.Fatal("Run(final drift) error = nil")
	}
	job, facts, err := repository.BootstrapRun(context.Background(), job.JobID)
	if err != nil || job.State != store.JobFailed || facts.ReconcilePass != 1 ||
		facts.FullHistoryReadyAtMS != nil {
		t.Fatalf("final drift failed run = %#v %#v, %v", job, facts, err)
	}
	originalItems, _ := repository.ListBootstrapPlanItems(
		context.Background(), store.BootstrapPlanItemFilter{JobID: job.JobID},
	)
	if len(originalItems) < 2 || originalItems[len(originalItems)-1].State != store.BootstrapItemDrifted {
		t.Fatalf("final drift items = %#v", originalItems)
	}

	restarted := newTestRuntime(t, repository, RuntimeConfig{}, runtimeHooks{})
	if err := restarted.Resume(context.Background(), 40); err != nil {
		t.Fatalf("Resume(final drift) error = %v", err)
	}
	resumed, _, _ := repository.BootstrapRunByIdentity(context.Background(), request.SwitchID, 40)
	if _, err := restarted.Run(context.Background(), resumed.JobID); err != nil {
		t.Fatalf("Run(fresh final pass) error = %v", err)
	}
	assertCompletedBootstrapTurnAndCursor(
		t, repository, "turn-final-drift-late",
		originalItems[len(originalItems)-1].Current.SourceFileID, int64(len(lateGrown)),
	)
	resumedItems, _ := repository.ListBootstrapPlanItems(
		context.Background(), store.BootstrapPlanItemFilter{JobID: resumed.JobID},
	)
	_, resumedFacts, _ := repository.BootstrapRun(context.Background(), resumed.JobID)
	if resumedFacts.ReconcilePass != 2 ||
		resumedItems[len(resumedItems)-1].Pass <= originalItems[len(originalItems)-1].Pass {
		t.Fatalf("fresh reconcile pass did not advance: original %#v resumed %#v", originalItems, resumedItems)
	}
}

func TestRuntimeInitialUnreadableRecoversThroughFreshReconcilePass(t *testing.T) {
	t.Parallel()

	repository := openBootstrapRepository(t)
	home := t.TempDir()
	path := filepath.Join(home, "sessions", "initial-unreadable.jsonl")
	writeBootstrapRollout(t, path,
		completeBootstrapRollout("session-initial-unreadable", "turn-initial-unreadable"), time.Now())

	seedRequest := bootstrapRequest(t, home, "switch-initial-unreadable-seed", 45)
	seedRuntime := newTestRuntime(t, repository, RuntimeConfig{}, runtimeHooks{})
	if err := seedRuntime.StartBootstrap(context.Background(), seedRequest); err != nil {
		t.Fatalf("StartBootstrap(seed) error = %v", err)
	}
	seedJob, _, _ := repository.BootstrapRunByIdentity(
		context.Background(), seedRequest.SwitchID, int64(seedRequest.Generation),
	)
	if _, err := seedRuntime.Run(context.Background(), seedJob.JobID); err != nil {
		t.Fatalf("Run(seed) error = %v", err)
	}
	request := bootstrapRequest(t, home, "switch-initial-unreadable", 46)
	backupPath := path + ".physical"
	var makeUnreadableOnce sync.Once
	runtime := newTestRuntime(t, repository, RuntimeConfig{}, runtimeHooks{
		afterHomeProbe: func(context.Context) {
			makeUnreadableOnce.Do(func() {
				if err := os.Rename(path, backupPath); err != nil {
					t.Fatalf("Rename(source to backup) error = %v", err)
				}
				if err := os.Symlink(filepath.Join(home, "outside.jsonl"), path); err != nil {
					t.Fatalf("Symlink(unreadable source) error = %v", err)
				}
			})
		},
	})
	if err := runtime.StartBootstrap(context.Background(), request); err != nil {
		t.Fatalf("StartBootstrap(initial unreadable) error = %v", err)
	}
	job, _, _ := repository.BootstrapRunByIdentity(
		context.Background(), request.SwitchID, int64(request.Generation),
	)
	initialItems, err := repository.ListBootstrapPlanItems(
		context.Background(), store.BootstrapPlanItemFilter{JobID: job.JobID},
	)
	if err != nil || len(initialItems) != 1 || initialItems[0].Pass != 0 ||
		initialItems[0].ActionKind != store.BootstrapActionUnreadable {
		t.Fatalf("initial unreadable plan = %#v, %v", initialItems, err)
	}
	if _, err := runtime.Run(context.Background(), job.JobID); !errors.Is(err, ErrSourceUnavailable) {
		t.Fatalf("Run(initial unreadable) error = %v, want ErrSourceUnavailable", err)
	}
	job, facts, err := repository.BootstrapRun(context.Background(), job.JobID)
	initialItems, itemsErr := repository.ListBootstrapPlanItems(
		context.Background(), store.BootstrapPlanItemFilter{JobID: job.JobID},
	)
	if err != nil || itemsErr != nil || job.State != store.JobFailed || facts.ReconcilePass != 1 ||
		facts.FullHistoryReadyAtMS != nil || len(initialItems) < 2 ||
		initialItems[0].State != store.BootstrapItemDrifted {
		t.Fatalf("failed initial unreadable run = %#v %#v items=%#v, errors=%v/%v", job, facts, initialItems, err, itemsErr)
	}

	if err := os.Remove(path); err != nil {
		t.Fatalf("Remove(unreadable symlink) error = %v", err)
	}
	if err := os.Rename(backupPath, path); err != nil {
		t.Fatalf("Restore(physical source) error = %v", err)
	}
	restarted := newTestRuntime(t, repository, RuntimeConfig{}, runtimeHooks{})
	if err := restarted.Resume(context.Background(), request.Generation); err != nil {
		t.Fatalf("Resume(repaired initial unreadable) error = %v", err)
	}
	resumed, _, _ := repository.BootstrapRunByIdentity(
		context.Background(), request.SwitchID, int64(request.Generation),
	)
	report, err := restarted.Run(context.Background(), resumed.JobID)
	if err != nil {
		t.Fatalf("Run(repaired initial unreadable) error = %v", err)
	}
	resumed, resumedFacts, err := repository.BootstrapRun(context.Background(), resumed.JobID)
	if err != nil || resumed.State != store.JobSucceeded || !report.FullHistoryReady ||
		resumedFacts.ReconcilePass != 2 || resumedFacts.FullHistoryReadyAtMS == nil {
		t.Fatalf("repaired initial unreadable run = %#v %#v report=%#v, %v", resumed, resumedFacts, report, err)
	}
}

func TestRuntimeRepairsIssueOnlyEmptyReconcileWithFreshEmptyPass(t *testing.T) {
	t.Parallel()

	repository := openBootstrapRepository(t)
	home := t.TempDir()
	writeBootstrapRollout(t, filepath.Join(home, "sessions", "initial.jsonl"),
		completeBootstrapRollout("session-empty-pass", "turn-empty-pass"), time.Now())
	unsafePath := filepath.Join(home, "sessions", "unsafe.jsonl")
	request := bootstrapRequest(t, home, "switch-empty-pass", 41)
	var issueOnce sync.Once
	runtime := newTestRuntime(t, repository, RuntimeConfig{}, runtimeHooks{
		beforeReconcile: func() {
			issueOnce.Do(func() {
				if err := os.Symlink(filepath.Join(home, "outside.jsonl"), unsafePath); err != nil {
					t.Fatalf("Symlink(issue-only) error = %v", err)
				}
			})
		},
	})
	if err := runtime.StartBootstrap(context.Background(), request); err != nil {
		t.Fatalf("StartBootstrap() error = %v", err)
	}
	job, _, _ := repository.BootstrapRunByIdentity(context.Background(), request.SwitchID, 41)
	if _, err := runtime.Run(context.Background(), job.JobID); !errors.Is(err, ErrDiscoveryIncomplete) {
		t.Fatalf("Run(issue-only pass) error = %v, want ErrDiscoveryIncomplete", err)
	}
	_, oldFacts, _ := repository.BootstrapRun(context.Background(), job.JobID)
	if oldFacts.ReconcilePass != 1 || oldFacts.ReconcilePlanAtMS == nil || oldFacts.ReconcileIssueCount != 1 {
		t.Fatalf("issue-only facts = %#v", oldFacts)
	}
	if err := os.Remove(unsafePath); err != nil {
		t.Fatalf("Remove(issue-only symlink) error = %v", err)
	}
	restarted := newTestRuntime(t, repository, RuntimeConfig{}, runtimeHooks{})
	if err := restarted.Resume(context.Background(), 41); err != nil {
		t.Fatalf("Resume(issue-only) error = %v", err)
	}
	resumed, _, _ := repository.BootstrapRunByIdentity(context.Background(), request.SwitchID, 41)
	if _, err := restarted.Run(context.Background(), resumed.JobID); err != nil {
		t.Fatalf("Run(fresh empty pass) error = %v", err)
	}
	_, resumedFacts, _ := repository.BootstrapRun(context.Background(), resumed.JobID)
	if resumedFacts.ReconcilePass != 2 || resumedFacts.ReconcilePlanAtMS == nil ||
		*resumedFacts.ReconcilePlanAtMS <= *oldFacts.ReconcilePlanAtMS ||
		resumedFacts.ReconcileIssueCount != 0 || resumedFacts.FullHistoryReadyAtMS == nil {
		t.Fatalf("fresh empty pass facts = old %#v resumed %#v", oldFacts, resumedFacts)
	}
}

func TestRuntimeFinalReconcileAppliesMovedAndReplacedActions(t *testing.T) {
	t.Parallel()

	t.Run("moved", func(t *testing.T) {
		repository := openBootstrapRepository(t)
		home := t.TempDir()
		from := filepath.Join(home, "sessions", "moved.jsonl")
		to := filepath.Join(home, "archived_sessions", "moved.jsonl")
		writeBootstrapRollout(t, from,
			completeBootstrapRollout("session-moved", "turn-moved"), time.Now())
		request := bootstrapRequest(t, home, "switch-final-moved", 42)
		var moveOnce sync.Once
		runtime := newTestRuntime(t, repository, RuntimeConfig{}, runtimeHooks{
			beforeReconcile: func() {
				moveOnce.Do(func() {
					if err := os.MkdirAll(filepath.Dir(to), 0o700); err != nil {
						t.Fatalf("MkdirAll(archive) error = %v", err)
					}
					if err := os.Rename(from, to); err != nil {
						t.Fatalf("Rename(moved source) error = %v", err)
					}
				})
			},
		})
		if err := runtime.StartBootstrap(context.Background(), request); err != nil {
			t.Fatalf("StartBootstrap() error = %v", err)
		}
		job, _, _ := repository.BootstrapRunByIdentity(context.Background(), request.SwitchID, 42)
		if _, err := runtime.Run(context.Background(), job.JobID); err != nil {
			t.Fatalf("Run(moved) error = %v", err)
		}
		items, _ := repository.ListBootstrapPlanItems(
			context.Background(), store.BootstrapPlanItemFilter{JobID: job.JobID},
		)
		if len(items) != 2 || items[1].ActionKind != store.BootstrapActionMoved ||
			items[1].State != store.BootstrapItemSucceeded {
			t.Fatalf("moved reconcile items = %#v", items)
		}
		file, err := repository.SourceFile(context.Background(), items[1].Current.SourceFileID)
		canonicalTo, canonicalErr := filepath.EvalSymlinks(to)
		if err != nil || canonicalErr != nil || file.CurrentPath != canonicalTo {
			t.Fatalf("SourceFile(moved) = %#v, %v", file, err)
		}
	})

	t.Run("replaced", func(t *testing.T) {
		repository := openBootstrapRepository(t)
		home := t.TempDir()
		path := filepath.Join(home, "sessions", "replaced.jsonl")
		writeBootstrapRollout(t, path,
			completeBootstrapRollout("session-replaced-old", "turn-replaced-old"), time.Now())
		request := bootstrapRequest(t, home, "switch-final-replaced", 43)
		var replaceOnce sync.Once
		runtime := newTestRuntime(t, repository, RuntimeConfig{}, runtimeHooks{
			beforeReconcile: func() {
				replaceOnce.Do(func() {
					if err := os.Rename(path, filepath.Join(home, "replaced-old.jsonl")); err != nil {
						t.Fatalf("Rename(replaced source) error = %v", err)
					}
					writeBootstrapRollout(t, path,
						completeBootstrapRollout("session-replaced-new", "turn-replaced-new"), time.Now())
				})
			},
		})
		if err := runtime.StartBootstrap(context.Background(), request); err != nil {
			t.Fatalf("StartBootstrap() error = %v", err)
		}
		job, _, _ := repository.BootstrapRunByIdentity(context.Background(), request.SwitchID, 43)
		if _, err := runtime.Run(context.Background(), job.JobID); err != nil {
			t.Fatalf("Run(replaced) error = %v", err)
		}
		items, _ := repository.ListBootstrapPlanItems(
			context.Background(), store.BootstrapPlanItemFilter{JobID: job.JobID},
		)
		if len(items) != 2 || items[1].ActionKind != store.BootstrapActionReplaced ||
			items[1].State != store.BootstrapItemSucceeded {
			t.Fatalf("replaced reconcile items = %#v", items)
		}
		if _, err := repository.Session(context.Background(), "session-replaced-new"); err != nil {
			t.Fatalf("Session(replaced new) error = %v", err)
		}
	})
}

func TestRuntimeStaleDeletedCASRequiresFreshReconcilePass(t *testing.T) {
	t.Parallel()

	repository := openBootstrapRepository(t)
	home := t.TempDir()
	path := filepath.Join(home, "sessions", "stale-delete.jsonl")
	holding := filepath.Join(home, "holding.jsonl")
	initial := completeBootstrapRollout("session-stale-delete", "turn-stale-delete")
	writeBootstrapRollout(t, path, initial, time.Now())
	request := bootstrapRequest(t, home, "switch-stale-delete", 44)
	var removeOnce sync.Once
	var advanceOnce sync.Once
	runtime := newTestRuntime(t, repository, RuntimeConfig{}, runtimeHooks{
		beforeReconcile: func() {
			removeOnce.Do(func() {
				if err := os.Rename(path, holding); err != nil {
					t.Fatalf("Rename(deleted source) error = %v", err)
				}
			})
		},
		afterReconcilePlan: func() {
			advanceOnce.Do(func() {
				if err := os.Rename(holding, path); err != nil {
					t.Fatalf("Rename(restored source) error = %v", err)
				}
				grown := append(append([]byte(nil), initial...),
					[]byte(bootstrapTurnStartLine("turn-stale-delete-new")+"\n"+
						bootstrapTurnEndLine("turn-stale-delete-new")+"\n")...)
				writeBootstrapRollout(t, path, grown, time.Now())
				ingestBootstrapCurrentSource(t, repository, request, path)
			})
		},
	})
	if err := runtime.StartBootstrap(context.Background(), request); err != nil {
		t.Fatalf("StartBootstrap() error = %v", err)
	}
	job, _, _ := repository.BootstrapRunByIdentity(context.Background(), request.SwitchID, 44)
	if _, err := runtime.Run(context.Background(), job.JobID); err == nil {
		t.Fatal("Run(stale deleted CAS) error = nil")
	}
	job, facts, err := repository.BootstrapRun(context.Background(), job.JobID)
	if err != nil || job.State != store.JobFailed || facts.FullHistoryReadyAtMS != nil {
		t.Fatalf("stale deleted run = %#v %#v, %v", job, facts, err)
	}
	items, _ := repository.ListBootstrapPlanItems(
		context.Background(), store.BootstrapPlanItemFilter{JobID: job.JobID},
	)
	file, err := repository.SourceFile(context.Background(), items[0].Current.SourceFileID)
	if err != nil || file.State != store.SourceFileActive {
		t.Fatalf("SourceFile(stale delete) = %#v, %v", file, err)
	}

	restarted := newTestRuntime(t, repository, RuntimeConfig{}, runtimeHooks{})
	if err := restarted.Resume(context.Background(), 44); err != nil {
		t.Fatalf("Resume(stale delete) error = %v", err)
	}
	resumed, _, _ := repository.BootstrapRunByIdentity(context.Background(), request.SwitchID, 44)
	if _, err := restarted.Run(context.Background(), resumed.JobID); err != nil {
		t.Fatalf("Run(fresh pass after stale delete) error = %v", err)
	}
}

func TestRuntimeFailsAfterPlanWhenSourceDependencyBecomesUnsafe(t *testing.T) {
	t.Parallel()

	repository := openBootstrapRepository(t)
	home := t.TempDir()
	path := filepath.Join(home, "sessions", "unsafe.jsonl")
	writeBootstrapRollout(t, path, completeBootstrapRollout("session-unsafe", "turn-unsafe"), time.Now())
	request := bootstrapRequest(t, home, "switch-unsafe", 6)
	runtime := newTestRuntime(t, repository, RuntimeConfig{}, runtimeHooks{})
	if err := runtime.StartBootstrap(context.Background(), request); err != nil {
		t.Fatalf("StartBootstrap() error = %v", err)
	}
	if err := os.Remove(path); err != nil {
		t.Fatalf("Remove(source) error = %v", err)
	}
	if err := os.Symlink(filepath.Join(home, "outside.jsonl"), path); err != nil {
		t.Fatalf("Symlink(source) error = %v", err)
	}
	job, _, _ := repository.BootstrapRunByIdentity(context.Background(), request.SwitchID, 6)
	if _, err := runtime.Run(context.Background(), job.JobID); err == nil {
		t.Fatal("Run(unsafe source) error = nil")
	}
	job, facts, err := repository.BootstrapRun(context.Background(), job.JobID)
	if err != nil || job.State != store.JobFailed || facts.PauseReason == nil ||
		*facts.PauseReason != store.BootstrapPauseSourceUnavailable {
		t.Fatalf("unsafe failed run = %#v %#v, %v", job, facts, err)
	}
	if err := runtime.Resume(context.Background(), 6); err != nil {
		t.Fatalf("Resume(failed plan-ready run) error = %v", err)
	}
	resumed, _, err := repository.BootstrapRunByIdentity(context.Background(), request.SwitchID, 6)
	if err != nil || resumed.JobID == job.JobID || resumed.ResumeOfJobID == nil ||
		*resumed.ResumeOfJobID != job.JobID || resumed.State != store.JobQueued {
		t.Fatalf("resumed failed run = %#v, %v", resumed, err)
	}
}

func TestRuntimeDrainInterruptsAdmissionAndResumeCreatesOneNewAttempt(t *testing.T) {
	t.Parallel()

	repository := openBootstrapRepository(t)
	home := t.TempDir()
	writeBootstrapRollout(t, filepath.Join(home, "sessions", "drain.jsonl"),
		completeBootstrapRollout("session-drain", "turn-drain"), time.Now())
	request := bootstrapRequest(t, home, "switch-drain", 7)
	runtime := newTestRuntime(t, repository, RuntimeConfig{}, runtimeHooks{})
	if err := runtime.StartBootstrap(context.Background(), request); err != nil {
		t.Fatalf("StartBootstrap() error = %v", err)
	}
	job, _, _ := repository.BootstrapRunByIdentity(context.Background(), request.SwitchID, 7)
	if err := runtime.Drain(context.Background(), 7); err != nil {
		t.Fatalf("Drain() error = %v", err)
	}
	job, facts, err := repository.BootstrapRun(context.Background(), job.JobID)
	if err != nil || job.State != store.JobInterrupted || facts.PauseReason == nil ||
		*facts.PauseReason != store.BootstrapPauseApplicationDraining {
		t.Fatalf("drained run = %#v %#v, %v", job, facts, err)
	}
	if err := runtime.StartBootstrap(context.Background(), request); !errors.Is(err, ErrGenerationDraining) {
		t.Fatalf("StartBootstrap(draining) error = %v, want ErrGenerationDraining", err)
	}
	if err := runtime.Resume(context.Background(), 7); err != nil {
		t.Fatalf("Resume() error = %v", err)
	}
	resumed, _, err := repository.BootstrapRunByIdentity(context.Background(), request.SwitchID, 7)
	if err != nil || resumed.ResumeOfJobID == nil || *resumed.ResumeOfJobID != job.JobID {
		t.Fatalf("resumed run = %#v, %v", resumed, err)
	}
	if _, err := runtime.Run(context.Background(), resumed.JobID); err != nil {
		t.Fatalf("Run(after drain resume) error = %v", err)
	}
}

func TestRuntimeConcurrentExactStartWaitsForAuthoritativeReadback(t *testing.T) {
	repository := openBootstrapRepository(t)
	home := t.TempDir()
	writeBootstrapRollout(t, filepath.Join(home, "sessions", "concurrent-start.jsonl"),
		completeBootstrapRollout("session-concurrent-start", "turn-concurrent-start"), time.Now())
	request := bootstrapRequest(t, home, "switch-concurrent-start", 61)
	entered := make(chan struct{})
	release := make(chan struct{})
	var enteredOnce sync.Once
	runtime := newTestRuntime(t, repository, RuntimeConfig{}, runtimeHooks{
		afterHomeProbe: func(ctx context.Context) {
			enteredOnce.Do(func() { close(entered) })
			select {
			case <-ctx.Done():
			case <-release:
			}
		},
	})
	firstDone := make(chan error, 1)
	go func() { firstDone <- runtime.StartBootstrap(context.Background(), request) }()
	select {
	case <-entered:
	case <-time.After(2 * time.Second):
		close(release)
		t.Fatal("first StartBootstrap did not reach owner hook")
	}
	secondDone := make(chan error, 1)
	go func() { secondDone <- runtime.StartBootstrap(context.Background(), request) }()
	select {
	case err := <-secondDone:
		close(release)
		<-firstDone
		t.Fatalf("concurrent exact StartBootstrap returned before owner readback: %v", err)
	case <-time.After(100 * time.Millisecond):
	}
	close(release)
	if err := <-firstDone; err != nil {
		t.Fatalf("first StartBootstrap error = %v", err)
	}
	if err := <-secondDone; err != nil {
		t.Fatalf("concurrent exact StartBootstrap error = %v", err)
	}
	job, facts, err := repository.BootstrapRunByIdentity(
		context.Background(), request.SwitchID, int64(request.Generation),
	)
	if err != nil || job.State != store.JobRunning || facts.PlanState != store.BootstrapPlanReady {
		t.Fatalf("authoritative concurrent Start readback = %#v %#v, %v", job, facts, err)
	}
}

func TestRuntimeConcurrentResumeWaitsForAuthoritativeReadback(t *testing.T) {
	repository := openBootstrapRepository(t)
	home := t.TempDir()
	writeBootstrapRollout(t, filepath.Join(home, "sessions", "concurrent-resume.jsonl"),
		completeBootstrapRollout("session-concurrent-resume", "turn-concurrent-resume"), time.Now())
	request := bootstrapRequest(t, home, "switch-concurrent-resume", 62)
	entered := make(chan struct{})
	release := make(chan struct{})
	var enteredOnce sync.Once
	runtime := newTestRuntime(t, repository, RuntimeConfig{}, runtimeHooks{
		afterResumeLookup: func(ctx context.Context) {
			enteredOnce.Do(func() { close(entered) })
			select {
			case <-ctx.Done():
			case <-release:
			}
		},
	})
	if err := runtime.StartBootstrap(context.Background(), request); err != nil {
		t.Fatalf("StartBootstrap() error = %v", err)
	}
	if err := runtime.Drain(context.Background(), request.Generation); err != nil {
		t.Fatalf("Drain() error = %v", err)
	}
	firstDone := make(chan error, 1)
	go func() { firstDone <- runtime.Resume(context.Background(), request.Generation) }()
	select {
	case <-entered:
	case <-time.After(2 * time.Second):
		close(release)
		t.Fatal("first Resume did not reach owner hook")
	}
	secondDone := make(chan error, 1)
	go func() { secondDone <- runtime.Resume(context.Background(), request.Generation) }()
	select {
	case err := <-secondDone:
		close(release)
		<-firstDone
		t.Fatalf("concurrent exact Resume returned before owner readback: %v", err)
	case <-time.After(100 * time.Millisecond):
	}
	close(release)
	if err := <-firstDone; err != nil {
		t.Fatalf("first Resume error = %v", err)
	}
	if err := <-secondDone; err != nil {
		t.Fatalf("concurrent exact Resume error = %v", err)
	}
	resumed, _, err := repository.BootstrapRunByIdentity(
		context.Background(), request.SwitchID, int64(request.Generation),
	)
	if err != nil || resumed.ResumeOfJobID == nil || resumed.State != store.JobQueued {
		t.Fatalf("authoritative concurrent Resume readback = %#v, %v", resumed, err)
	}
}

func TestRuntimeExactStartIsReadOnlyNoOpWhileRunIsActive(t *testing.T) {
	repository := openBootstrapRepository(t)
	home := t.TempDir()
	writeBootstrapRollout(t, filepath.Join(home, "sessions", "run-active-start.jsonl"),
		completeBootstrapRollout("session-run-active-start", "turn-run-active-start"), time.Now())
	request := bootstrapRequest(t, home, "switch-run-active-start", 63)
	entered := make(chan struct{})
	release := make(chan struct{})
	var enteredOnce sync.Once
	runtime := newTestRuntime(t, repository, RuntimeConfig{ReadChunkBytes: 32}, runtimeHooks{
		afterChunk: func(store.BootstrapPlanItem, int64) {
			enteredOnce.Do(func() { close(entered) })
			<-release
		},
	})
	if err := runtime.StartBootstrap(context.Background(), request); err != nil {
		t.Fatalf("StartBootstrap() error = %v", err)
	}
	job, _, _ := repository.BootstrapRunByIdentity(
		context.Background(), request.SwitchID, int64(request.Generation),
	)
	runDone := make(chan error, 1)
	go func() {
		_, err := runtime.Run(context.Background(), job.JobID)
		runDone <- err
	}()
	select {
	case <-entered:
	case <-time.After(2 * time.Second):
		close(release)
		t.Fatal("Run did not reach active chunk hook")
	}
	if err := runtime.StartBootstrap(context.Background(), request); err != nil {
		close(release)
		<-runDone
		t.Fatalf("StartBootstrap(exact replay during Run) error = %v", err)
	}
	close(release)
	if err := <-runDone; err != nil {
		t.Fatalf("Run() error = %v", err)
	}
}

func TestRuntimeDrainLinearizesAllStartBootstrapAdmissionWindows(t *testing.T) {
	stages := []struct {
		name  string
		apply func(*runtimeHooks, func(context.Context))
	}{
		{name: "after probe", apply: func(hooks *runtimeHooks, block func(context.Context)) {
			hooks.afterHomeProbe = block
		}},
		{name: "after create", apply: func(hooks *runtimeHooks, block func(context.Context)) {
			hooks.afterBootstrapCreate = block
		}},
		{name: "before freeze", apply: func(hooks *runtimeHooks, block func(context.Context)) {
			hooks.beforeInitialFreeze = block
		}},
	}
	for index, stage := range stages {
		stage := stage
		t.Run(stage.name, func(t *testing.T) {
			repository := openBootstrapRepository(t)
			home := t.TempDir()
			writeBootstrapRollout(t, filepath.Join(home, "sessions", "admission.jsonl"),
				completeBootstrapRollout("session-admission", "turn-admission"), time.Now())
			request := bootstrapRequest(t, home, "switch-admission-"+stage.name, uint64(50+index))
			entered := make(chan struct{})
			release := make(chan struct{})
			var enteredOnce sync.Once
			block := func(ctx context.Context) {
				enteredOnce.Do(func() { close(entered) })
				select {
				case <-ctx.Done():
				case <-release:
				}
			}
			hooks := runtimeHooks{}
			stage.apply(&hooks, block)
			runtime := newTestRuntime(t, repository, RuntimeConfig{}, hooks)
			startDone := make(chan error, 1)
			go func() { startDone <- runtime.StartBootstrap(context.Background(), request) }()
			select {
			case <-entered:
			case <-time.After(2 * time.Second):
				close(release)
				t.Fatal("StartBootstrap did not reach admission hook")
			}
			drainDone := make(chan error, 1)
			go func() { drainDone <- runtime.Drain(context.Background(), request.Generation) }()
			select {
			case drainErr := <-drainDone:
				select {
				case startErr := <-startDone:
					if drainErr != nil || startErr == nil {
						t.Fatalf("Drain/StartBootstrap errors = %v / %v", drainErr, startErr)
					}
				default:
					close(release)
					startErr := <-startDone
					t.Fatalf("Drain returned before admitted StartBootstrap exited; start error after release = %v", startErr)
				}
			case <-time.After(2 * time.Second):
				close(release)
				t.Fatal("Drain did not cancel and wait for admitted StartBootstrap")
			}
			job, _, err := repository.BootstrapRunByIdentity(
				context.Background(), request.SwitchID, int64(request.Generation),
			)
			if err == nil && (job.State == store.JobQueued || job.State == store.JobRunning) {
				t.Fatalf("job remained admitted after Drain: %#v", job)
			}
			if err != nil && !errors.Is(err, store.ErrNotFound) {
				t.Fatalf("BootstrapRunByIdentity() error = %v", err)
			}
		})
	}
}

func TestRuntimeDrainLinearizesConcurrentResumeAdmission(t *testing.T) {
	repository := openBootstrapRepository(t)
	home := t.TempDir()
	writeBootstrapRollout(t, filepath.Join(home, "sessions", "resume-admission.jsonl"),
		completeBootstrapRollout("session-resume-admission", "turn-resume-admission"), time.Now())
	request := bootstrapRequest(t, home, "switch-resume-admission", 60)
	entered := make(chan struct{})
	release := make(chan struct{})
	var enteredOnce sync.Once
	runtime := newTestRuntime(t, repository, RuntimeConfig{}, runtimeHooks{
		afterResumeLookup: func(ctx context.Context) {
			enteredOnce.Do(func() { close(entered) })
			select {
			case <-ctx.Done():
			case <-release:
			}
		},
	})
	if err := runtime.StartBootstrap(context.Background(), request); err != nil {
		t.Fatalf("StartBootstrap() error = %v", err)
	}
	if err := runtime.Drain(context.Background(), 60); err != nil {
		t.Fatalf("Drain(initial) error = %v", err)
	}
	resumeDone := make(chan error, 1)
	go func() { resumeDone <- runtime.Resume(context.Background(), 60) }()
	select {
	case <-entered:
	case <-time.After(2 * time.Second):
		close(release)
		t.Fatal("Resume did not reach latest-attempt hook")
	}
	drainDone := make(chan error, 1)
	go func() { drainDone <- runtime.Drain(context.Background(), 60) }()
	select {
	case drainErr := <-drainDone:
		select {
		case resumeErr := <-resumeDone:
			if drainErr != nil || resumeErr == nil {
				t.Fatalf("Drain/Resume errors = %v / %v", drainErr, resumeErr)
			}
		default:
			close(release)
			resumeErr := <-resumeDone
			t.Fatalf("Drain returned before admitted Resume exited; resume error after release = %v", resumeErr)
		}
	case <-time.After(2 * time.Second):
		close(release)
		t.Fatal("Drain did not cancel and wait for admitted Resume")
	}
	latest, _, err := repository.BootstrapRunByIdentity(context.Background(), request.SwitchID, 60)
	if err != nil || latest.State == store.JobQueued || latest.State == store.JobRunning {
		t.Fatalf("latest attempt after Drain = %#v, %v", latest, err)
	}
}

func TestLoadSessionIndexHintsUsesSafeLatestEntryAndDropsFutureTime(t *testing.T) {
	t.Parallel()

	home := t.TempDir()
	validID := "550e8400-e29b-41d4-a716-446655440000"
	futureID := "550e8400-e29b-41d4-a716-446655440001"
	validPath := filepath.Join(home, "sessions", "rollout-"+validID+".jsonl")
	futurePath := filepath.Join(home, "sessions", "rollout-"+futureID+".jsonl")
	writeBootstrapRollout(t, validPath, completeBootstrapRollout("session-hint", "turn-hint"), time.Now())
	writeBootstrapRollout(t, futurePath, completeBootstrapRollout("session-future", "turn-future"), time.Now())
	indexContent := []byte(
		`{"id":"` + validID + `","thread_name":"old","updated_at":"2026-07-14T01:00:00Z"}` + "\n" +
			`{"id":"` + validID + `","thread_name":"latest","updated_at":"2026-07-14T02:00:00Z"}` + "\n" +
			`{"id":"` + futureID + `","thread_name":"future","updated_at":"2030-07-14T02:00:00Z"}` + "\n",
	)
	if err := os.WriteFile(filepath.Join(home, "session_index.jsonl"), indexContent, 0o600); err != nil {
		t.Fatalf("WriteFile(session_index) error = %v", err)
	}
	discoverer, err := logs.NewDiscoverer(home)
	if err != nil {
		t.Fatalf("NewDiscoverer() error = %v", err)
	}
	discovery, err := discoverer.Discover(context.Background())
	if err != nil {
		t.Fatalf("Discover() error = %v", err)
	}
	maximum := time.Date(2026, 7, 15, 0, 0, 0, 0, time.UTC).UnixMilli()
	metadata, err := logs.NewHomeProbe().Probe(context.Background(), home)
	if err != nil {
		t.Fatalf("HomeProbe.Probe() error = %v", err)
	}
	hints, err := loadSessionIndexHints(
		context.Background(), home, metadata.DeviceID, metadata.Inode, discovery.Snapshots, maximum,
	)
	if err != nil {
		t.Fatalf("loadSessionIndexHints() error = %v", err)
	}
	var validSourceID, futureSourceID string
	for _, snapshot := range discovery.Snapshots {
		switch snapshot.Path {
		case validPath:
			validSourceID = snapshot.SourceFileID
		case futurePath:
			futureSourceID = snapshot.SourceFileID
		}
	}
	want := time.Date(2026, 7, 14, 2, 0, 0, 0, time.UTC).UnixMilli()
	if hints[validSourceID] != want {
		t.Fatalf("valid hint = %d, want %d", hints[validSourceID], want)
	}
	if _, found := hints[futureSourceID]; found {
		t.Fatalf("future hint unexpectedly accepted: %#v", hints)
	}
}

func openBootstrapRepository(t *testing.T) *store.Repository {
	t.Helper()
	directory := t.TempDir()
	if err := os.Chmod(directory, 0o700); err != nil {
		t.Fatalf("Chmod(temp) error = %v", err)
	}
	database, err := storesqlite.Open(context.Background(), storesqlite.Config{
		Path: filepath.Join(directory, "bootstrap.db"),
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

func newTestRuntime(
	t *testing.T,
	repository *store.Repository,
	config RuntimeConfig,
	hooks runtimeHooks,
) *Runtime {
	t.Helper()
	config.Repository = repository
	var mu sync.Mutex
	now := time.Now().UnixMilli()
	config.Clock = func() time.Time {
		mu.Lock()
		defer mu.Unlock()
		now++
		return time.UnixMilli(now)
	}
	runtime, err := newRuntime(config, hooks)
	if err != nil {
		t.Fatalf("newRuntime() error = %v", err)
	}
	return runtime
}

func bootstrapRequest(t *testing.T, home, switchID string, generation uint64) preferences.BootstrapRequest {
	t.Helper()
	metadata, err := logs.NewHomeProbe().Probe(context.Background(), home)
	if err != nil {
		t.Fatalf("HomeProbe.Probe() error = %v", err)
	}
	return preferences.BootstrapRequest{
		SwitchID: switchID, Generation: generation,
		Source: preferences.ConfirmedSource{
			Path: metadata.Path, DeviceID: metadata.DeviceID, Inode: metadata.Inode,
			ConfirmedAtMS: time.Now().UnixMilli(),
		},
		DataStoreKey: "store-" + switchID, Strategy: preferences.HomeSwitchIndependentDatabase,
	}
}

func writeBootstrapRollout(t *testing.T, path string, content []byte, modified time.Time) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatalf("MkdirAll(%s) error = %v", path, err)
	}
	if err := os.WriteFile(path, content, 0o600); err != nil {
		t.Fatalf("WriteFile(%s) error = %v", path, err)
	}
	if err := os.Chtimes(path, modified, modified); err != nil {
		t.Fatalf("Chtimes(%s) error = %v", path, err)
	}
}

func replaceBootstrapHomeWithHardlinks(t *testing.T, home string, relativePaths []string) {
	t.Helper()
	previous := home + "-previous"
	if err := os.Rename(home, previous); err != nil {
		t.Fatalf("Rename(Home) error = %v", err)
	}
	if err := os.MkdirAll(home, 0o700); err != nil {
		t.Fatalf("MkdirAll(replacement Home) error = %v", err)
	}
	for _, relativePath := range relativePaths {
		destination := filepath.Join(home, relativePath)
		if err := os.MkdirAll(filepath.Dir(destination), 0o700); err != nil {
			t.Fatalf("MkdirAll(replacement source parent) error = %v", err)
		}
		if err := os.Link(filepath.Join(previous, relativePath), destination); err != nil {
			t.Fatalf("Link(replacement source) error = %v", err)
		}
	}
}

func assertCompletedBootstrapTurnAndCursor(
	t *testing.T,
	repository *store.Repository,
	turnID string,
	sourceFileID string,
	wantOffset int64,
) {
	t.Helper()
	turn, err := repository.Turn(context.Background(), turnID)
	if err != nil || turn.CompletedAtMS == nil || turn.Outcome == nil || *turn.Outcome != "completed" {
		t.Fatalf("Turn(%s) = %#v, %v, want completed", turnID, turn, err)
	}
	file, err := repository.SourceFile(context.Background(), sourceFileID)
	if err != nil {
		t.Fatalf("SourceFile(%s) error = %v", sourceFileID, err)
	}
	cursor, err := repository.GenerationCursor(
		context.Background(), sourceFileID, file.ActiveGeneration,
	)
	if err != nil || cursor.Checkpoint.CommittedOffset != wantOffset ||
		cursor.Fingerprint.SizeBytes != wantOffset {
		t.Fatalf("completed cursor = %#v, %v, want offset=%d", cursor, err, wantOffset)
	}
}

func ingestBootstrapCurrentSource(
	t *testing.T,
	repository *store.Repository,
	request preferences.BootstrapRequest,
	path string,
) {
	t.Helper()
	previous, err := repository.CodexSnapshots(context.Background())
	if err != nil {
		t.Fatalf("CodexSnapshots() error = %v", err)
	}
	discoverer, err := logs.NewConfirmedDiscoverer(
		request.Source.Path, request.Source.DeviceID, request.Source.Inode,
	)
	if err != nil {
		t.Fatalf("NewConfirmedDiscoverer() error = %v", err)
	}
	discovery, err := discoverer.DiscoverAgainst(
		context.Background(), snapshotsFromFingerprints(previous),
	)
	if err != nil {
		t.Fatalf("DiscoverAgainst() error = %v", err)
	}
	plan, err := logs.PlanReconcile(
		request.Source.Path, snapshotsFromFingerprints(previous), discovery,
	)
	if err != nil {
		t.Fatalf("PlanReconcile() error = %v", err)
	}
	var action *logs.ReconcileAction
	for index := range plan.Actions {
		if plan.Actions[index].Kind == logs.ChangeGrown && plan.Actions[index].Current != nil {
			action = &plan.Actions[index]
			break
		}
	}
	if action == nil || action.Kind != logs.ChangeGrown {
		t.Fatalf("current source reconcile action = %#v, want grown", action)
	}
	ingester, err := indexer.New(repository)
	if err != nil {
		t.Fatalf("indexer.New() error = %v", err)
	}
	atMS := time.Now().Add(time.Hour).UnixMilli()
	stream, err := ingester.Open(context.Background(), indexer.OpenRequest{Action: *action, AtMS: atMS})
	if err != nil {
		t.Fatalf("Ingester.Open() error = %v", err)
	}
	cursor, err := stream.Cursor()
	if err != nil {
		t.Fatalf("Stream.Cursor() error = %v", err)
	}
	reader, err := logs.NewConfirmedSnapshotReader(
		request.Source.Path, request.Source.DeviceID, request.Source.Inode, defaultReadChunkBytes,
	)
	if err != nil {
		t.Fatalf("NewConfirmedSnapshotReader() error = %v", err)
	}
	if _, err := reader.Read(
		context.Background(), *action.Current, cursor.CommittedOffset,
		func(chunk []byte, eof bool) error {
			atMS++
			_, err := stream.Feed(context.Background(), chunk, eof, atMS)
			return err
		},
	); err != nil {
		t.Fatalf("SnapshotReader.Read() error = %v", err)
	}
}

func completeBootstrapRollout(sessionID, turnID string) []byte {
	return []byte(bootstrapSessionMetaLine(sessionID) + "\n" + bootstrapTurnStartLine(turnID) + "\n" +
		bootstrapTurnEndLine(turnID) + "\n")
}

func largeBootstrapRollout(sessionID, turnID string) []byte {
	return append(completeBootstrapRollout(sessionID, turnID), []byte(
		`{"timestamp":"2026-07-14T01:00:03Z","type":"response_item","payload":{"type":"message","content":[{"type":"output_text","text":"`+
			strings.Repeat("x", int(logs.PrefixLimitBytes))+`"}]}}`+"\n",
	)...)
}

func bootstrapSessionMetaLine(sessionID string) string {
	return `{"timestamp":"2026-07-14T01:00:00Z","type":"session_meta","payload":{"id":"` + sessionID +
		`","timestamp":"2026-07-14T01:00:00Z","cwd":"/tmp/project","originator":"codex_cli_rs","cli_version":"0.142.3","source":"cli","model_provider":"openai"}}`
}

func bootstrapTurnStartLine(turnID string) string {
	return `{"timestamp":"2026-07-14T01:00:01Z","type":"event_msg","payload":{"type":"task_started","turn_id":"` + turnID +
		`","started_at":1783990801,"model_context_window":258000}}`
}

func bootstrapTurnEndLine(turnID string) string {
	return `{"timestamp":"2026-07-14T01:00:02Z","type":"event_msg","payload":{"type":"task_complete","turn_id":"` + turnID +
		`","completed_at":1783990802}}`
}
