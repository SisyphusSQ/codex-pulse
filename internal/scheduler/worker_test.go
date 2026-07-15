package scheduler

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"github.com/SisyphusSQ/codex-pulse/internal/store"
	storesqlite "github.com/SisyphusSQ/codex-pulse/internal/store/sqlite"
)

// 测试 RunCycle 选择live、执行一个有界slice并原子回写yield事实。
func TestServiceRunCycleYieldsSelectedTaskWithDurableFacts(t *testing.T) {
	t.Parallel()

	repository := openSchedulerRepository(t)
	backfill := createSchedulerFixture(t, repository, "backfill", store.SchedulerLaneBackfill, 10)
	live := createSchedulerFixture(t, repository, "live", store.SchedulerLaneLive, 11)
	executor := &recordingExecutor{result: SliceResult{
		FilesProcessed: 1, BytesProcessed: 1024, Active: 12 * time.Millisecond,
		StopReason: store.SchedulerStopByteBudget,
	}}
	service := newSchedulerTestService(t, repository, executor)

	result, err := service.RunCycle(context.Background(), SystemSnapshot{})
	if err != nil {
		t.Fatalf("RunCycle() error = %v", err)
	}
	if len(executor.calls) != 1 || executor.calls[0].TaskID != live.TaskID ||
		executor.calls[0].State != store.SchedulerTaskRunning ||
		executor.budgets[0] != DefaultBudgetPolicy().BackgroundNormal {
		t.Fatalf("executor calls = %#v budgets = %#v", executor.calls, executor.budgets)
	}
	if result.Cycle.TaskID != live.TaskID || result.Cycle.SelectionReason != store.SchedulerSelectionLivePriority ||
		result.Cycle.Outcome != store.SchedulerCycleYielded ||
		result.Cycle.StopReason != store.SchedulerStopByteBudget || result.YieldFor != 150*time.Millisecond {
		t.Fatalf("RunCycle() = %#v", result)
	}
	stored, err := repository.SchedulerTask(context.Background(), live.TaskID)
	if err != nil || stored.State != store.SchedulerTaskQueued || stored.FilesProcessed != 1 ||
		stored.BytesProcessed != 1024 || stored.SliceCount != 1 ||
		stored.QueueOrderMS <= backfill.QueueOrderMS {
		t.Fatalf("SchedulerTask() = %#v, %v", stored, err)
	}
	cycles, err := repository.ListSchedulerCycles(context.Background(), store.SchedulerCycleFilter{
		TaskID: &live.TaskID, Limit: 10,
	})
	if err != nil || len(cycles) != 1 || cycles[0] != result.Cycle {
		t.Fatalf("ListSchedulerCycles() = %#v, %v", cycles, err)
	}
}

// 测试 Store pressure 产生零消耗yield cycle，且不调用target executor。
func TestServiceRunCyclePersistsBlockedBudgetWithoutExecutingTarget(t *testing.T) {
	t.Parallel()

	repository := openSchedulerRepository(t)
	task := createSchedulerFixture(t, repository, "blocked", store.SchedulerLaneBackfill, 10)
	executor := &recordingExecutor{}
	service := newSchedulerTestService(t, repository, executor)

	result, err := service.RunCycle(context.Background(), SystemSnapshot{StorePressure: true})
	if err != nil {
		t.Fatalf("RunCycle() error = %v", err)
	}
	if len(executor.calls) != 0 {
		t.Fatalf("executor calls = %#v, want none", executor.calls)
	}
	if result.Cycle.TaskID != task.TaskID || result.Cycle.StopReason != store.SchedulerStopSystemPressure ||
		result.Cycle.Outcome != store.SchedulerCycleYielded || result.Cycle.ConsumedFiles != 0 ||
		result.Cycle.ConsumedBytes != 0 || result.YieldFor != 500*time.Millisecond {
		t.Fatalf("RunCycle() = %#v", result)
	}
	stored, err := repository.SchedulerTask(context.Background(), task.TaskID)
	if err != nil || stored.State != store.SchedulerTaskQueued || stored.SliceCount != 1 {
		t.Fatalf("SchedulerTask() = %#v, %v", stored, err)
	}
}

// 测试 completed slice 将task与cycle写为terminal成功。
func TestServiceRunCycleCompletesTask(t *testing.T) {
	t.Parallel()

	repository := openSchedulerRepository(t)
	task := createSchedulerFixture(t, repository, "complete", store.SchedulerLaneLive, 10)
	executor := &recordingExecutor{result: SliceResult{
		FilesProcessed: 1, BytesProcessed: 256, Active: time.Millisecond,
		StopReason: store.SchedulerStopCompleted,
	}}
	service := newSchedulerTestService(t, repository, executor)

	result, err := service.RunCycle(context.Background(), SystemSnapshot{})
	if err != nil {
		t.Fatalf("RunCycle() error = %v", err)
	}
	if result.Cycle.TaskID != task.TaskID || result.Cycle.Outcome != store.SchedulerCycleCompleted ||
		result.YieldFor != 0 {
		t.Fatalf("RunCycle() = %#v", result)
	}
	stored, err := repository.SchedulerTask(context.Background(), task.TaskID)
	if err != nil || stored.State != store.SchedulerTaskSucceeded || stored.FinishedAtMS == nil {
		t.Fatalf("SchedulerTask() = %#v, %v", stored, err)
	}
}

// 测试父context取消后仍用独立有界context落盘interrupted事实。
func TestServiceRunCyclePersistsCancellationAfterParentContextEnds(t *testing.T) {
	t.Parallel()

	repository := openSchedulerRepository(t)
	task := createSchedulerFixture(t, repository, "cancel", store.SchedulerLaneLive, 10)
	ctx, cancel := context.WithCancel(context.Background())
	executor := &recordingExecutor{execute: func(
		ctx context.Context,
		_ store.SchedulerTask,
		_ ScanBudget,
	) (SliceResult, error) {
		cancel()
		return SliceResult{}, ctx.Err()
	}}
	service := newSchedulerTestService(t, repository, executor)

	result, err := service.RunCycle(ctx, SystemSnapshot{})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("RunCycle() error = %v, want context.Canceled", err)
	}
	if result.Cycle.TaskID != task.TaskID || result.Cycle.Outcome != store.SchedulerCycleInterrupted ||
		result.Cycle.StopReason != store.SchedulerStopCancelled {
		t.Fatalf("RunCycle() = %#v", result)
	}
	if len(executor.interrupts) != 1 || executor.interrupts[0] != store.RuntimeErrorCanceled {
		t.Fatalf("interrupts = %#v", executor.interrupts)
	}
	stored, readErr := repository.SchedulerTask(context.Background(), task.TaskID)
	if readErr != nil || stored.State != store.SchedulerTaskInterrupted || stored.LastErrorClass == nil ||
		*stored.LastErrorClass != store.RuntimeErrorCanceled {
		t.Fatalf("SchedulerTask() = %#v, %v", stored, readErr)
	}
}

// 测试executor panic被降维并持久化；interrupt失败时保守保留running供恢复。
func TestServiceRunCycleContainsExecutorPanic(t *testing.T) {
	t.Parallel()

	t.Run("interrupted", func(t *testing.T) {
		t.Parallel()
		repository := openSchedulerRepository(t)
		task := createSchedulerFixture(t, repository, "panic", store.SchedulerLaneLive, 10)
		executor := &recordingExecutor{execute: func(
			context.Context,
			store.SchedulerTask,
			ScanBudget,
		) (SliceResult, error) {
			panic("sensitive panic text must not be persisted")
		}}
		service := newSchedulerTestService(t, repository, executor)

		result, err := service.RunCycle(context.Background(), SystemSnapshot{})
		if !errors.Is(err, ErrExecutorPanic) || result.Cycle.StopReason != store.SchedulerStopWorkerPanic ||
			result.Cycle.Outcome != store.SchedulerCycleInterrupted {
			t.Fatalf("RunCycle() = %#v, %v", result, err)
		}
		stored, readErr := repository.SchedulerTask(context.Background(), task.TaskID)
		if readErr != nil || stored.State != store.SchedulerTaskInterrupted || stored.LastErrorClass == nil ||
			*stored.LastErrorClass != store.RuntimeErrorUnknown {
			t.Fatalf("SchedulerTask() = %#v, %v", stored, readErr)
		}
	})

	t.Run("interrupt failure", func(t *testing.T) {
		t.Parallel()
		repository := openSchedulerRepository(t)
		task := createSchedulerFixture(t, repository, "panic-interrupt", store.SchedulerLaneLive, 10)
		interruptErr := errors.New("interrupt unavailable")
		executor := &recordingExecutor{
			execute: func(context.Context, store.SchedulerTask, ScanBudget) (SliceResult, error) {
				panic("boom")
			},
			interruptErr: interruptErr,
		}
		service := newSchedulerTestService(t, repository, executor)

		if _, err := service.RunCycle(context.Background(), SystemSnapshot{}); !errors.Is(err, ErrExecutorPanic) ||
			!errors.Is(err, interruptErr) {
			t.Fatalf("RunCycle() error = %v", err)
		}
		stored, readErr := repository.SchedulerTask(context.Background(), task.TaskID)
		if readErr != nil || stored.State != store.SchedulerTaskRunning {
			t.Fatalf("SchedulerTask() = %#v, %v, want conservative running", stored, readErr)
		}
		cycles, listErr := repository.ListSchedulerCycles(context.Background(), store.SchedulerCycleFilter{
			TaskID: &task.TaskID, Limit: 10,
		})
		if listErr != nil || len(cycles) != 0 {
			t.Fatalf("ListSchedulerCycles() = %#v, %v, want none", cycles, listErr)
		}
	})
}

// 测试dependency error只持久化稳定error class，不把原始正文写入scheduler facts。
func TestServiceRunCyclePersistsDependencyFailureClass(t *testing.T) {
	t.Parallel()

	repository := openSchedulerRepository(t)
	task := createSchedulerFixture(t, repository, "dependency", store.SchedulerLaneBackfill, 10)
	dependencyErr := errors.New("private path /Users/example/secret")
	executor := &recordingExecutor{err: dependencyErr}
	service := newSchedulerTestService(t, repository, executor)

	result, err := service.RunCycle(context.Background(), SystemSnapshot{})
	if !errors.Is(err, dependencyErr) || result.Cycle.Outcome != store.SchedulerCycleFailed ||
		result.Cycle.StopReason != store.SchedulerStopDependencyError {
		t.Fatalf("RunCycle() = %#v, %v", result, err)
	}
	stored, readErr := repository.SchedulerTask(context.Background(), task.TaskID)
	if readErr != nil || stored.State != store.SchedulerTaskFailed || stored.LastErrorClass == nil ||
		*stored.LastErrorClass != store.RuntimeErrorUnknown {
		t.Fatalf("SchedulerTask() = %#v, %v", stored, readErr)
	}
}

// 测试持久cycle跨多次RunCycle保持lane内round-robin，并在8个live后强制backfill。
func TestServiceRunCyclePersistsRoundRobinAndEightToOneFairness(t *testing.T) {
	t.Parallel()

	repository := openSchedulerRepository(t)
	backfill := createSchedulerFixture(t, repository, "fair-backfill", store.SchedulerLaneBackfill, 10)
	liveOne := createSchedulerFixture(t, repository, "fair-live-1", store.SchedulerLaneLive, 11)
	liveTwo := createSchedulerFixture(t, repository, "fair-live-2", store.SchedulerLaneLive, 12)
	executor := &recordingExecutor{result: SliceResult{StopReason: store.SchedulerStopFileBudget}}
	service := newSchedulerTestService(t, repository, executor)

	var taskIDs []string
	for index := 0; index < 9; index++ {
		result, err := service.RunCycle(context.Background(), SystemSnapshot{})
		if err != nil {
			t.Fatalf("RunCycle(%d) error = %v", index, err)
		}
		taskIDs = append(taskIDs, result.Cycle.TaskID)
		if index == 8 && result.Cycle.SelectionReason != store.SchedulerSelectionBackfillFairness {
			t.Fatalf("RunCycle(8) reason = %q", result.Cycle.SelectionReason)
		}
	}
	for index := 0; index < 8; index++ {
		want := liveOne.TaskID
		if index%2 == 1 {
			want = liveTwo.TaskID
		}
		if taskIDs[index] != want {
			t.Fatalf("taskIDs = %#v, index %d want %q", taskIDs, index, want)
		}
	}
	if taskIDs[8] != backfill.TaskID {
		t.Fatalf("taskIDs = %#v, want ninth %q", taskIDs, backfill.TaskID)
	}
}

func TestServiceRecoverActiveTasksRebindsLostOwnerAndIsIdempotent(t *testing.T) {
	t.Parallel()

	repository := openSchedulerRepository(t)
	task := createSchedulerFixture(t, repository, "recover-running", store.SchedulerLaneLive, 10)
	if _, err := repository.ClaimSchedulerTask(context.Background(), task.TaskID, 15); err != nil {
		t.Fatalf("ClaimSchedulerTask() error = %v", err)
	}
	executor := &recordingExecutor{recover: func(
		_ context.Context,
		lost store.SchedulerTask,
	) (store.JobRun, error) {
		resumedID := lost.TargetID + "-resume"
		job := store.JobRun{
			JobID: resumedID, JobType: "scheduler-test", RequestedBy: "test", Priority: 1,
			State: store.JobQueued, Phase: store.JobPhaseLive, CreatedAtMS: 16, UpdatedAtMS: 16,
		}
		if err := repository.CreateJobRun(context.Background(), job); err != nil {
			return store.JobRun{}, err
		}
		return job, nil
	}}
	service := newSchedulerTestService(t, repository, executor)

	recovered, err := service.RecoverActiveTasks(context.Background())
	if err != nil || len(recovered) != 1 || recovered[0].TaskID != task.TaskID ||
		recovered[0].TargetID != task.TargetID+"-resume" ||
		recovered[0].State != store.SchedulerTaskQueued || recovered[0].QueueOrderMS <= task.QueueOrderMS {
		t.Fatalf("RecoverActiveTasks() = %#v, %v", recovered, err)
	}
	again, err := service.RecoverActiveTasks(context.Background())
	if err != nil || len(again) != 0 {
		t.Fatalf("RecoverActiveTasks(replay) = %#v, %v", again, err)
	}
}

func TestServiceRunOwnsSingleLoopAndStopsCancellably(t *testing.T) {
	t.Parallel()

	t.Run("completes then idles", func(t *testing.T) {
		t.Parallel()
		repository := openSchedulerRepository(t)
		createSchedulerFixture(t, repository, "run-loop", store.SchedulerLaneLive, 10)
		called := make(chan struct{})
		executor := &recordingExecutor{execute: func(
			context.Context,
			store.SchedulerTask,
			ScanBudget,
		) (SliceResult, error) {
			close(called)
			return SliceResult{StopReason: store.SchedulerStopCompleted}, nil
		}}
		service := newSchedulerTestService(t, repository, executor)
		ctx, cancel := context.WithCancel(context.Background())
		done := make(chan error, 1)
		go func() { done <- service.Run(ctx) }()
		select {
		case <-called:
		case <-time.After(3 * time.Second):
			t.Fatal("Run() did not execute queued task")
		}
		cancel()
		select {
		case err := <-done:
			if !errors.Is(err, context.Canceled) {
				t.Fatalf("Run() error = %v, want context.Canceled", err)
			}
		case <-time.After(3 * time.Second):
			t.Fatal("Run() did not stop after cancellation")
		}
	})

	t.Run("rejects duplicate owner", func(t *testing.T) {
		t.Parallel()
		repository := openSchedulerRepository(t)
		createSchedulerFixture(t, repository, "run-owner", store.SchedulerLaneLive, 10)
		started := make(chan struct{})
		release := make(chan struct{})
		executor := &recordingExecutor{execute: func(
			context.Context,
			store.SchedulerTask,
			ScanBudget,
		) (SliceResult, error) {
			close(started)
			<-release
			return SliceResult{StopReason: store.SchedulerStopCompleted}, nil
		}}
		service := newSchedulerTestService(t, repository, executor)
		ctx, cancel := context.WithCancel(context.Background())
		done := make(chan error, 1)
		go func() { done <- service.Run(ctx) }()
		select {
		case <-started:
		case <-time.After(3 * time.Second):
			t.Fatal("Run() owner did not start")
		}
		if err := service.Run(context.Background()); !errors.Is(err, ErrRunAlreadyActive) {
			t.Fatalf("second Run() error = %v, want ErrRunAlreadyActive", err)
		}
		cancel()
		close(release)
		select {
		case err := <-done:
			if !errors.Is(err, context.Canceled) {
				t.Fatalf("owner Run() error = %v", err)
			}
		case <-time.After(3 * time.Second):
			t.Fatal("owner Run() did not exit")
		}
	})
}

func TestServiceRunOwnerLeasePreventsLiveRecoveryAndAllowsTakeoverAfterRelease(t *testing.T) {
	t.Parallel()

	repository := openSchedulerRepository(t)
	task := createSchedulerFixture(t, repository, "owner-lease", store.SchedulerLaneLive, 10)
	ownerStarted := make(chan struct{})
	ownerExecutor := &recordingExecutor{execute: func(
		ctx context.Context,
		_ store.SchedulerTask,
		_ ScanBudget,
	) (SliceResult, error) {
		close(ownerStarted)
		<-ctx.Done()
		return SliceResult{}, ctx.Err()
	}}
	owner := newSchedulerTestService(t, repository, ownerExecutor)
	ownerCtx, cancelOwner := context.WithCancel(context.Background())
	t.Cleanup(cancelOwner)
	ownerDone := make(chan error, 1)
	go func() { ownerDone <- owner.Run(ownerCtx) }()
	select {
	case <-ownerStarted:
	case <-time.After(3 * time.Second):
		t.Fatal("owner Run() did not enter executor")
	}

	recoverCalled := make(chan struct{})
	executed := make(chan struct{})
	contenderExecutor := &recordingExecutor{
		recover: func(
			_ context.Context,
			lost store.SchedulerTask,
		) (store.JobRun, error) {
			close(recoverCalled)
			return store.JobRun{JobID: lost.TargetID, State: store.JobQueued}, nil
		},
		execute: func(context.Context, store.SchedulerTask, ScanBudget) (SliceResult, error) {
			close(executed)
			return SliceResult{StopReason: store.SchedulerStopCompleted}, nil
		},
	}
	contender := newSchedulerTestService(t, repository, contenderExecutor)
	waitCtx, cancelWait := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancelWait()
	if err := contender.Run(waitCtx); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("contender Run(active owner) error = %v, want deadline", err)
	}
	select {
	case <-recoverCalled:
		t.Fatal("contender recovered a target whose owner was still alive")
	default:
	}
	if len(contenderExecutor.calls) != 0 {
		t.Fatalf("contender executor calls = %#v, want none", contenderExecutor.calls)
	}

	cancelOwner()
	if err := <-ownerDone; !errors.Is(err, context.Canceled) {
		t.Fatalf("owner Run() error = %v, want context.Canceled", err)
	}
	takeoverCtx, cancelTakeover := context.WithCancel(context.Background())
	takeoverDone := make(chan error, 1)
	go func() { takeoverDone <- contender.Run(takeoverCtx) }()
	select {
	case <-recoverCalled:
	case <-time.After(3 * time.Second):
		cancelTakeover()
		t.Fatal("contender did not recover after owner lease release")
	}
	select {
	case <-executed:
	case err := <-takeoverDone:
		cancelTakeover()
		stored, readErr := repository.SchedulerTask(context.Background(), task.TaskID)
		t.Fatalf("contender exited before executing recovered task: %v; task=%#v read=%v", err, stored, readErr)
	case <-time.After(3 * time.Second):
		cancelTakeover()
		t.Fatal("contender did not execute recovered task")
	}
	deadline := time.Now().Add(3 * time.Second)
	for {
		stored, err := repository.SchedulerTask(context.Background(), task.TaskID)
		if err == nil && stored.State == store.SchedulerTaskSucceeded {
			break
		}
		if time.Now().After(deadline) {
			cancelTakeover()
			t.Fatalf("recovered task did not succeed: %#v, %v", stored, err)
		}
		time.Sleep(time.Millisecond)
	}
	cancelTakeover()
	if err := <-takeoverDone; !errors.Is(err, context.Canceled) {
		t.Fatalf("contender Run(after takeover) error = %v", err)
	}
}

func TestServiceRunCycleRecordsLiveArrivalAtBackfillBoundary(t *testing.T) {
	t.Parallel()

	repository := openSchedulerRepository(t)
	backfill := createSchedulerFixture(t, repository, "preempt-backfill", store.SchedulerLaneBackfill, 10)
	var live store.SchedulerTask
	executor := &recordingExecutor{execute: func(
		_ context.Context,
		task store.SchedulerTask,
		_ ScanBudget,
	) (SliceResult, error) {
		if task.TaskID == backfill.TaskID {
			live = createSchedulerFixture(t, repository, "preempt-live", store.SchedulerLaneLive, 30)
		}
		return SliceResult{StopReason: store.SchedulerStopTimeBudget}, nil
	}}
	service := newSchedulerTestService(t, repository, executor)

	result, err := service.RunCycle(context.Background(), SystemSnapshot{})
	if err != nil || result.Cycle.TaskID != backfill.TaskID ||
		result.Cycle.StopReason != store.SchedulerStopLivePreempted ||
		result.Cycle.Outcome != store.SchedulerCycleYielded {
		t.Fatalf("RunCycle(backfill boundary) = %#v, %v", result, err)
	}
	next, err := service.RunCycle(context.Background(), SystemSnapshot{})
	if err != nil || next.Cycle.TaskID != live.TaskID {
		t.Fatalf("RunCycle(next live) = %#v, %v", next, err)
	}
}

func TestServiceRunCycleEnforcesSingleHeavyOwnerAcrossServices(t *testing.T) {
	t.Parallel()

	repository := openSchedulerRepository(t)
	createSchedulerFixture(t, repository, "owner-first", store.SchedulerLaneLive, 10)
	createSchedulerFixture(t, repository, "owner-second", store.SchedulerLaneLive, 11)
	started := make(chan struct{})
	release := make(chan struct{})
	firstExecutor := &recordingExecutor{execute: func(
		context.Context,
		store.SchedulerTask,
		ScanBudget,
	) (SliceResult, error) {
		close(started)
		<-release
		return SliceResult{StopReason: store.SchedulerStopCompleted}, nil
	}}
	secondExecutor := &recordingExecutor{result: SliceResult{StopReason: store.SchedulerStopCompleted}}
	firstService := newSchedulerTestService(t, repository, firstExecutor)
	secondService := newSchedulerTestService(t, repository, secondExecutor)
	firstDone := make(chan error, 1)
	go func() {
		_, err := firstService.RunCycle(context.Background(), SystemSnapshot{})
		firstDone <- err
	}()
	select {
	case <-started:
	case <-time.After(3 * time.Second):
		t.Fatal("first RunCycle() did not enter executor")
	}
	if _, err := secondService.RunCycle(context.Background(), SystemSnapshot{}); !errors.Is(err, ErrSchedulerRetry) {
		t.Fatalf("second RunCycle() error = %v, want ErrSchedulerRetry", err)
	}
	if len(secondExecutor.calls) != 0 {
		t.Fatalf("second executor calls = %#v, want none", secondExecutor.calls)
	}
	close(release)
	if err := <-firstDone; err != nil {
		t.Fatalf("first RunCycle() error = %v", err)
	}
	second, err := secondService.RunCycle(context.Background(), SystemSnapshot{})
	if err != nil || second.Cycle.Outcome != store.SchedulerCycleCompleted || len(secondExecutor.calls) != 1 {
		t.Fatalf("second RunCycle(retry) = %#v calls=%#v err=%v", second, secondExecutor.calls, err)
	}
}

func TestServiceRunCycleTreatsPromotionCASAsRetryWithoutLosingPromotion(t *testing.T) {
	t.Parallel()

	t.Run("promotion before claim", func(t *testing.T) {
		t.Parallel()
		repository := openSchedulerRepository(t)
		task := createSchedulerFixture(t, repository, "promote-before-claim", store.SchedulerLaneLive, 10)
		executor := &recordingExecutor{result: SliceResult{StopReason: store.SchedulerStopCompleted}}
		service := newSchedulerTestService(t, repository, executor)
		service.newCycleID = func() (string, error) {
			_, err := repository.PromoteSchedulerTask(context.Background(), task.DedupeKey, 100)
			return "cycle-promote-before-claim", err
		}
		if _, err := service.RunCycle(context.Background(), SystemSnapshot{}); !errors.Is(err, ErrSchedulerRetry) {
			t.Fatalf("RunCycle(stale claim) error = %v, want ErrSchedulerRetry", err)
		}
		if len(executor.calls) != 0 {
			t.Fatalf("executor calls = %#v, want none before fresh claim", executor.calls)
		}
		service.newCycleID = func() (string, error) { return "cycle-promote-fresh", nil }
		result, err := service.RunCycle(context.Background(), SystemSnapshot{})
		if err != nil || result.Cycle.Outcome != store.SchedulerCycleCompleted {
			t.Fatalf("RunCycle(fresh) = %#v, %v", result, err)
		}
		stored, err := repository.SchedulerTask(context.Background(), task.TaskID)
		if err != nil || stored.ServiceClass != store.SchedulerServiceInteractive ||
			stored.State != store.SchedulerTaskSucceeded {
			t.Fatalf("SchedulerTask() = %#v, %v", stored, err)
		}
	})

	t.Run("promotion before commit", func(t *testing.T) {
		t.Parallel()
		repository := openSchedulerRepository(t)
		task := createSchedulerFixture(t, repository, "promote-before-commit", store.SchedulerLaneLive, 10)
		executor := &recordingExecutor{}
		executor.execute = func(
			context.Context,
			store.SchedulerTask,
			ScanBudget,
		) (SliceResult, error) {
			if _, err := repository.PromoteSchedulerTask(context.Background(), task.DedupeKey, 100); err != nil {
				return SliceResult{}, err
			}
			return SliceResult{StopReason: store.SchedulerStopFileBudget}, nil
		}
		service := newSchedulerTestService(t, repository, executor)
		result, err := service.RunCycle(context.Background(), SystemSnapshot{})
		if err != nil || result.Cycle.Outcome != store.SchedulerCycleYielded {
			t.Fatalf("RunCycle() = %#v, %v", result, err)
		}
		stored, err := repository.SchedulerTask(context.Background(), task.TaskID)
		if err != nil || stored.ServiceClass != store.SchedulerServiceInteractive ||
			stored.State != store.SchedulerTaskQueued || stored.SliceCount != 1 {
			t.Fatalf("SchedulerTask() = %#v, %v", stored, err)
		}
	})
}

func TestServiceRunContinuesAfterPersistedDependencyFailure(t *testing.T) {
	t.Parallel()

	repository := openSchedulerRepository(t)
	failed := createSchedulerFixture(t, repository, "run-dependency", store.SchedulerLaneLive, 10)
	succeeded := createSchedulerFixture(t, repository, "run-after-dependency", store.SchedulerLaneLive, 11)
	secondCalled := make(chan struct{})
	executor := &recordingExecutor{execute: func(
		_ context.Context,
		task store.SchedulerTask,
		_ ScanBudget,
	) (SliceResult, error) {
		if task.TaskID == failed.TaskID {
			return SliceResult{}, errors.New("dependency unavailable")
		}
		close(secondCalled)
		return SliceResult{StopReason: store.SchedulerStopCompleted}, nil
	}}
	service := newSchedulerTestService(t, repository, executor)
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- service.Run(ctx) }()
	select {
	case <-secondCalled:
	case err := <-done:
		t.Fatalf("Run() exited after dependency failure: %v", err)
	case <-time.After(3 * time.Second):
		t.Fatal("Run() did not continue to the next task")
	}
	deadline := time.Now().Add(3 * time.Second)
	for {
		stored, err := repository.SchedulerTask(context.Background(), succeeded.TaskID)
		if err == nil && stored.State == store.SchedulerTaskSucceeded {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("second task did not succeed: %#v, %v", stored, err)
		}
		time.Sleep(time.Millisecond)
	}
	cancel()
	if err := <-done; !errors.Is(err, context.Canceled) {
		t.Fatalf("Run() final error = %v, want context.Canceled", err)
	}
}

func TestServiceRecoverActiveTasksReconcilesSucceededTargetCrashGap(t *testing.T) {
	t.Parallel()

	repository := openSchedulerRepository(t)
	task := createSchedulerFixture(t, repository, "recover-succeeded", store.SchedulerLaneLive, 10)
	if _, err := repository.ClaimSchedulerTask(context.Background(), task.TaskID, 15); err != nil {
		t.Fatalf("ClaimSchedulerTask() error = %v", err)
	}
	if err := repository.TransitionJobRun(context.Background(), store.JobTransition{
		JobID: task.TargetID, ExpectedState: store.JobQueued, State: store.JobRunning,
		Phase: store.JobPhaseLive, AtMS: 16,
	}); err != nil {
		t.Fatalf("TransitionJobRun(running) error = %v", err)
	}
	if err := repository.TransitionJobRun(context.Background(), store.JobTransition{
		JobID: task.TargetID, ExpectedState: store.JobRunning, State: store.JobSucceeded,
		Phase: store.JobPhaseLive, AtMS: 17,
	}); err != nil {
		t.Fatalf("TransitionJobRun(succeeded) error = %v", err)
	}
	executor := &recordingExecutor{recover: func(
		ctx context.Context,
		lost store.SchedulerTask,
	) (store.JobRun, error) {
		return repository.JobRun(ctx, lost.TargetID)
	}}
	service := newSchedulerTestService(t, repository, executor)
	recovered, err := service.RecoverActiveTasks(context.Background())
	if err != nil || len(recovered) != 1 || recovered[0].State != store.SchedulerTaskSucceeded {
		t.Fatalf("RecoverActiveTasks() = %#v, %v", recovered, err)
	}
	cycles, err := repository.ListSchedulerCycles(context.Background(), store.SchedulerCycleFilter{
		TaskID: &task.TaskID, Limit: 10,
	})
	if err != nil || len(cycles) != 1 || cycles[0].Outcome != store.SchedulerCycleCompleted {
		t.Fatalf("ListSchedulerCycles() = %#v, %v", cycles, err)
	}
}

func TestServiceRunCycleReadsBackCommitThatSucceededWithUnknownResponse(t *testing.T) {
	t.Parallel()

	repository := openSchedulerRepository(t)
	task := createSchedulerFixture(t, repository, "commit-readback", store.SchedulerLaneLive, 10)
	executor := &recordingExecutor{result: SliceResult{StopReason: store.SchedulerStopCompleted}}
	service := newSchedulerTestService(t, repository, executor)
	unknown := errors.New("commit response unavailable")
	service.commitCycle = func(ctx context.Context, commit store.SchedulerCycleCommit) error {
		if err := repository.CommitSchedulerCycle(ctx, commit); err != nil {
			return err
		}
		return unknown
	}

	result, err := service.RunCycle(context.Background(), SystemSnapshot{})
	if err != nil || result.Cycle.TaskID != task.TaskID || result.Cycle.Outcome != store.SchedulerCycleCompleted {
		t.Fatalf("RunCycle() = %#v, %v", result, err)
	}
	cycles, err := repository.ListSchedulerCycles(context.Background(), store.SchedulerCycleFilter{
		TaskID: &task.TaskID, Limit: 10,
	})
	if err != nil || len(cycles) != 1 || cycles[0] != result.Cycle {
		t.Fatalf("ListSchedulerCycles() = %#v, %v", cycles, err)
	}
}

type recordingExecutor struct {
	result       SliceResult
	err          error
	execute      func(context.Context, store.SchedulerTask, ScanBudget) (SliceResult, error)
	calls        []store.SchedulerTask
	budgets      []ScanBudget
	interruptErr error
	interrupts   []store.RuntimeErrorClass
	recover      func(context.Context, store.SchedulerTask) (store.JobRun, error)
}

func (executor *recordingExecutor) ExecuteSlice(
	ctx context.Context,
	task store.SchedulerTask,
	budget ScanBudget,
) (SliceResult, error) {
	executor.calls = append(executor.calls, task)
	executor.budgets = append(executor.budgets, budget)
	if executor.execute != nil {
		return executor.execute(ctx, task, budget)
	}
	return executor.result, executor.err
}

func (executor *recordingExecutor) Interrupt(
	_ context.Context,
	_ store.SchedulerTask,
	class store.RuntimeErrorClass,
) error {
	executor.interrupts = append(executor.interrupts, class)
	return executor.interruptErr
}

func (executor *recordingExecutor) Recover(
	ctx context.Context,
	task store.SchedulerTask,
) (store.JobRun, error) {
	if executor.recover != nil {
		return executor.recover(ctx, task)
	}
	return store.JobRun{JobID: task.TargetID, State: store.JobQueued}, nil
}

func newSchedulerTestService(
	t *testing.T,
	repository *store.Repository,
	executor Executor,
) *Service {
	t.Helper()
	serviceID := schedulerTestServiceSequence.Add(1)
	clock := int64(20)
	cycle := 0
	service, err := NewService(ServiceConfig{
		Repository: repository,
		Executors: map[store.SchedulerTargetKind]Executor{
			store.SchedulerTargetLiveScan:  executor,
			store.SchedulerTargetBootstrap: executor,
		},
		BudgetPolicy: DefaultBudgetPolicy(), MaxLiveBurst: 8,
		Clock: func() time.Time {
			value := clock
			clock++
			return time.UnixMilli(value)
		},
		NewCycleID: func() (string, error) {
			cycle++
			return fmt.Sprintf("cycle-test-%d-%d", serviceID, cycle), nil
		},
	})
	if err != nil {
		t.Fatalf("NewService() error = %v", err)
	}
	return service
}

var schedulerTestServiceSequence atomic.Uint64

func createSchedulerFixture(
	t *testing.T,
	repository *store.Repository,
	suffix string,
	lane store.SchedulerLane,
	atMS int64,
) store.SchedulerTask {
	t.Helper()
	phase := store.JobPhaseLive
	targetKind := store.SchedulerTargetLiveScan
	if lane == store.SchedulerLaneBackfill {
		phase = store.JobPhaseHistoryBackfill
		targetKind = store.SchedulerTargetBootstrap
	}
	job := store.JobRun{
		JobID: "job-" + suffix, JobType: "scheduler-test", RequestedBy: "test", Priority: 1,
		State: store.JobQueued, Phase: phase, CreatedAtMS: atMS, UpdatedAtMS: atMS,
	}
	if err := repository.CreateJobRun(context.Background(), job); err != nil {
		t.Fatalf("CreateJobRun() error = %v", err)
	}
	task := store.SchedulerTask{
		TaskID: "task-" + suffix, DedupeKey: "dedupe-" + suffix,
		TargetKind: targetKind, TargetID: job.JobID, HomeGeneration: 1,
		Lane: lane, ServiceClass: store.SchedulerServiceBackground,
		State: store.SchedulerTaskQueued, QueueOrderMS: atMS, EnqueuedAtMS: atMS, UpdatedAtMS: atMS,
	}
	if err := repository.EnqueueSchedulerTask(context.Background(), task, 16); err != nil {
		t.Fatalf("EnqueueSchedulerTask() error = %v", err)
	}
	return task
}

func openSchedulerRepository(t *testing.T) *store.Repository {
	t.Helper()
	directory := t.TempDir()
	if err := os.Chmod(directory, 0o700); err != nil {
		t.Fatalf("secure temp directory: %v", err)
	}
	database, err := storesqlite.Open(context.Background(), storesqlite.Config{
		Path: filepath.Join(directory, "scheduler.db"),
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
