package scheduler

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/SisyphusSQ/codex-pulse/internal/bootstrap"
	"github.com/SisyphusSQ/codex-pulse/internal/codex/logs"
	"github.com/SisyphusSQ/codex-pulse/internal/liveindex"
	"github.com/SisyphusSQ/codex-pulse/internal/preferences"
	"github.com/SisyphusSQ/codex-pulse/internal/store"
)

func TestSchedulerIntegrationRunsLiveBeforeBackfillAndStillAdvancesHistory(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	repository := openSchedulerRepository(t)
	home := t.TempDir()
	writeSchedulerRollout(t, filepath.Join(home, "archived_sessions", "history.jsonl"),
		schedulerRollout("session-scheduler-history", "turn-scheduler-history"))
	writeSchedulerRollout(t, filepath.Join(home, "sessions", "live.jsonl"),
		schedulerRollout("session-scheduler-live", "turn-scheduler-live"))

	bootstrapRuntime, err := bootstrap.NewRuntime(bootstrap.RuntimeConfig{
		Repository: repository, ReadChunkBytes: 64,
	})
	if err != nil {
		t.Fatalf("bootstrap.NewRuntime() error = %v", err)
	}
	bootstrapRequest := schedulerBootstrapRequest(t, home)
	if err := bootstrapRuntime.StartBootstrap(ctx, bootstrapRequest); err != nil {
		t.Fatalf("StartBootstrap() error = %v", err)
	}
	backfillJob, _, err := repository.BootstrapRunByIdentity(
		ctx, bootstrapRequest.SwitchID, int64(bootstrapRequest.Generation),
	)
	if err != nil {
		t.Fatalf("BootstrapRunByIdentity() error = %v", err)
	}

	liveRuntime, err := liveindex.New(liveindex.Config{Repository: repository, ReadChunkBytes: 64})
	if err != nil {
		t.Fatalf("liveindex.New() error = %v", err)
	}
	liveRequest := schedulerLiveRequest(t, home)
	liveJob, err := liveRuntime.Start(ctx, liveRequest)
	if err != nil {
		t.Fatalf("live.Start() error = %v", err)
	}

	baseMS := maxSchedulerInt64(backfillJob.UpdatedAtMS, liveJob.UpdatedAtMS) + 1
	backfillTask := store.SchedulerTask{
		TaskID: "task-integration-backfill", DedupeKey: "bootstrap:integration",
		TargetKind: store.SchedulerTargetBootstrap, TargetID: backfillJob.JobID,
		HomeGeneration: int64(bootstrapRequest.Generation), Lane: store.SchedulerLaneBackfill,
		ServiceClass: store.SchedulerServiceBackground, State: store.SchedulerTaskQueued,
		QueueOrderMS: baseMS, EnqueuedAtMS: baseMS, UpdatedAtMS: baseMS,
	}
	liveTask := store.SchedulerTask{
		TaskID: "task-integration-live", DedupeKey: "live:integration",
		TargetKind: store.SchedulerTargetLiveScan, TargetID: liveJob.JobID,
		HomeGeneration: liveRequest.HomeGeneration, Lane: store.SchedulerLaneLive,
		ServiceClass: store.SchedulerServiceBackground, State: store.SchedulerTaskQueued,
		QueueOrderMS: baseMS + 1, EnqueuedAtMS: baseMS + 1, UpdatedAtMS: baseMS + 1,
	}
	if err := repository.EnqueueSchedulerTask(ctx, backfillTask, 8); err != nil {
		t.Fatalf("EnqueueSchedulerTask(backfill) error = %v", err)
	}
	if err := repository.EnqueueSchedulerTask(ctx, liveTask, 8); err != nil {
		t.Fatalf("EnqueueSchedulerTask(live) error = %v", err)
	}
	bootstrapExecutor, err := NewBootstrapExecutor(bootstrapRuntime)
	if err != nil {
		t.Fatalf("NewBootstrapExecutor() error = %v", err)
	}
	liveExecutor, err := NewLiveExecutor(liveRuntime)
	if err != nil {
		t.Fatalf("NewLiveExecutor() error = %v", err)
	}
	service, err := NewService(ServiceConfig{
		Repository: repository,
		Executors: map[store.SchedulerTargetKind]Executor{
			store.SchedulerTargetBootstrap: bootstrapExecutor,
			store.SchedulerTargetLiveScan:  liveExecutor,
		},
		BudgetPolicy: DefaultBudgetPolicy(), MaxLiveBurst: 8,
	})
	if err != nil {
		t.Fatalf("NewService() error = %v", err)
	}

	first, err := service.RunCycle(ctx, SystemSnapshot{})
	if err != nil || first.Cycle.TaskID != liveTask.TaskID ||
		first.Cycle.SelectionReason != store.SchedulerSelectionLivePriority ||
		(first.Cycle.Outcome != store.SchedulerCycleCompleted &&
			first.Cycle.Outcome != store.SchedulerCycleYielded) {
		t.Fatalf("RunCycle(live) = %#v, %v", first, err)
	}
	liveComplete := first.Cycle.Outcome == store.SchedulerCycleCompleted
	backfillComplete := false
	for cycle := 0; cycle < 32 && (!liveComplete || !backfillComplete); cycle++ {
		result, err := service.RunCycle(ctx, SystemSnapshot{})
		if err != nil {
			t.Fatalf("RunCycle(%d) = %#v, %v", cycle, result, err)
		}
		if result.Cycle.Outcome != store.SchedulerCycleCompleted &&
			result.Cycle.Outcome != store.SchedulerCycleYielded {
			t.Fatalf("RunCycle(%d) outcome = %q", cycle, result.Cycle.Outcome)
		}
		switch result.Cycle.TaskID {
		case liveTask.TaskID:
			liveComplete = result.Cycle.Outcome == store.SchedulerCycleCompleted
		case backfillTask.TaskID:
			if !liveComplete && result.Cycle.SelectionReason != store.SchedulerSelectionBackfillFairness {
				t.Fatalf("RunCycle(%d) selected early backfill without fairness: %#v", cycle, result)
			}
			backfillComplete = result.Cycle.Outcome == store.SchedulerCycleCompleted
		default:
			t.Fatalf("RunCycle(%d) selected unknown task: %#v", cycle, result)
		}
	}
	if !liveComplete || !backfillComplete {
		t.Fatalf("tasks did not complete within 32 bounded cycles: live=%t backfill=%t",
			liveComplete, backfillComplete)
	}
	for _, sessionID := range []string{"session-scheduler-live", "session-scheduler-history"} {
		if _, err := repository.Session(ctx, sessionID); err != nil {
			t.Fatalf("Session(%q) error = %v", sessionID, err)
		}
	}
}

func TestSchedulerIntegrationReconcilesSucceededTargetBeforeMissingCycleCommit(t *testing.T) {
	t.Parallel()

	t.Run("live", func(t *testing.T) {
		t.Parallel()
		ctx := context.Background()
		repository := openSchedulerRepository(t)
		home := t.TempDir()
		writeSchedulerRollout(t, filepath.Join(home, "sessions", "live-crash-gap.jsonl"),
			schedulerRollout("session-live-crash-gap", "turn-live-crash-gap"))
		runtime, err := liveindex.New(liveindex.Config{Repository: repository, ReadChunkBytes: 64})
		if err != nil {
			t.Fatalf("liveindex.New() error = %v", err)
		}
		request := schedulerLiveRequest(t, home)
		request.RequestID = "request-live-crash-gap"
		job, err := runtime.Start(ctx, request)
		if err != nil {
			t.Fatalf("live.Start() error = %v", err)
		}
		atMS := job.UpdatedAtMS + 1
		task := store.SchedulerTask{
			TaskID: "task-live-crash-gap", DedupeKey: "live:crash-gap",
			TargetKind: store.SchedulerTargetLiveScan, TargetID: job.JobID,
			HomeGeneration: request.HomeGeneration, Lane: store.SchedulerLaneLive,
			ServiceClass: store.SchedulerServiceBackground, State: store.SchedulerTaskQueued,
			QueueOrderMS: atMS, EnqueuedAtMS: atMS, UpdatedAtMS: atMS,
		}
		if err := repository.EnqueueSchedulerTask(ctx, task, 8); err != nil {
			t.Fatalf("EnqueueSchedulerTask() error = %v", err)
		}
		if _, err := repository.ClaimSchedulerTask(ctx, task.TaskID, atMS+1); err != nil {
			t.Fatalf("ClaimSchedulerTask() error = %v", err)
		}
		if report, err := runtime.RunSlice(ctx, job.JobID, liveindex.SliceBudget{
			MaxFiles: 1, MaxBytes: 1 << 20, MaxActive: time.Minute,
		}); err != nil || !report.Complete {
			t.Fatalf("live.RunSlice() = %#v, %v", report, err)
		}
		liveExecutor, err := NewLiveExecutor(runtime)
		if err != nil {
			t.Fatalf("NewLiveExecutor() error = %v", err)
		}
		service := schedulerIntegrationRecoveryService(t, repository, store.SchedulerTargetLiveScan, liveExecutor)
		assertRecoveredSucceededTarget(t, ctx, repository, service, task.TaskID)
	})

	t.Run("bootstrap", func(t *testing.T) {
		t.Parallel()
		ctx := context.Background()
		repository := openSchedulerRepository(t)
		home := t.TempDir()
		writeSchedulerRollout(t, filepath.Join(home, "archived_sessions", "bootstrap-crash-gap.jsonl"),
			schedulerRollout("session-bootstrap-crash-gap", "turn-bootstrap-crash-gap"))
		runtime, err := bootstrap.NewRuntime(bootstrap.RuntimeConfig{
			Repository: repository, ReadChunkBytes: 64,
		})
		if err != nil {
			t.Fatalf("bootstrap.NewRuntime() error = %v", err)
		}
		request := schedulerBootstrapRequest(t, home)
		request.SwitchID = "switch-bootstrap-crash-gap"
		if err := runtime.StartBootstrap(ctx, request); err != nil {
			t.Fatalf("StartBootstrap() error = %v", err)
		}
		job, _, err := repository.BootstrapRunByIdentity(ctx, request.SwitchID, int64(request.Generation))
		if err != nil {
			t.Fatalf("BootstrapRunByIdentity() error = %v", err)
		}
		atMS := job.UpdatedAtMS + 1
		task := store.SchedulerTask{
			TaskID: "task-bootstrap-crash-gap", DedupeKey: "bootstrap:crash-gap",
			TargetKind: store.SchedulerTargetBootstrap, TargetID: job.JobID,
			HomeGeneration: int64(request.Generation), Lane: store.SchedulerLaneBackfill,
			ServiceClass: store.SchedulerServiceBackground, State: store.SchedulerTaskQueued,
			QueueOrderMS: atMS, EnqueuedAtMS: atMS, UpdatedAtMS: atMS,
		}
		if err := repository.EnqueueSchedulerTask(ctx, task, 8); err != nil {
			t.Fatalf("EnqueueSchedulerTask() error = %v", err)
		}
		if _, err := repository.ClaimSchedulerTask(ctx, task.TaskID, atMS+1); err != nil {
			t.Fatalf("ClaimSchedulerTask() error = %v", err)
		}
		if report, err := runtime.Run(ctx, job.JobID); err != nil || !report.FullHistoryReady {
			t.Fatalf("bootstrap.Run() = %#v, %v", report, err)
		}
		bootstrapExecutor, err := NewBootstrapExecutor(runtime)
		if err != nil {
			t.Fatalf("NewBootstrapExecutor() error = %v", err)
		}
		service := schedulerIntegrationRecoveryService(
			t, repository, store.SchedulerTargetBootstrap, bootstrapExecutor,
		)
		assertRecoveredSucceededTarget(t, ctx, repository, service, task.TaskID)
	})
}

func schedulerIntegrationRecoveryService(
	t *testing.T,
	repository *store.Repository,
	targetKind store.SchedulerTargetKind,
	executor Executor,
) *Service {
	t.Helper()
	executors := map[store.SchedulerTargetKind]Executor{
		store.SchedulerTargetLiveScan:  &recordingExecutor{},
		store.SchedulerTargetBootstrap: &recordingExecutor{},
	}
	executors[targetKind] = executor
	service, err := NewService(ServiceConfig{
		Repository: repository, Executors: executors,
		BudgetPolicy: DefaultBudgetPolicy(), MaxLiveBurst: 8,
	})
	if err != nil {
		t.Fatalf("NewService() error = %v", err)
	}
	return service
}

func assertRecoveredSucceededTarget(
	t *testing.T,
	ctx context.Context,
	repository *store.Repository,
	service *Service,
	taskID string,
) {
	t.Helper()
	recovered, err := service.RecoverActiveTasks(ctx)
	if err != nil || len(recovered) != 1 || recovered[0].TaskID != taskID ||
		recovered[0].State != store.SchedulerTaskSucceeded {
		t.Fatalf("RecoverActiveTasks() = %#v, %v", recovered, err)
	}
	cycles, err := repository.ListSchedulerCycles(ctx, store.SchedulerCycleFilter{
		TaskID: &taskID, Limit: 10,
	})
	if err != nil || len(cycles) != 1 || cycles[0].Outcome != store.SchedulerCycleCompleted ||
		cycles[0].StopReason != store.SchedulerStopCompleted {
		t.Fatalf("ListSchedulerCycles() = %#v, %v", cycles, err)
	}
}

func schedulerBootstrapRequest(t *testing.T, home string) preferences.BootstrapRequest {
	t.Helper()
	metadata, err := logs.NewHomeProbe().Probe(context.Background(), home)
	if err != nil {
		t.Fatalf("HomeProbe(backfill) error = %v", err)
	}
	return preferences.BootstrapRequest{
		SwitchID: "switch-scheduler-integration", Generation: 201,
		Source: preferences.ConfirmedSource{
			Path: metadata.Path, DeviceID: metadata.DeviceID, Inode: metadata.Inode,
			ConfirmedAtMS: time.Now().UnixMilli(),
		},
		DataStoreKey: "store-scheduler-integration",
		Strategy:     preferences.HomeSwitchIndependentDatabase,
	}
}

func schedulerLiveRequest(t *testing.T, home string) liveindex.LiveRequest {
	t.Helper()
	metadata, err := logs.NewHomeProbe().Probe(context.Background(), home)
	if err != nil {
		t.Fatalf("HomeProbe(live) error = %v", err)
	}
	discoverer, err := logs.NewConfirmedDiscoverer(metadata.Path, metadata.DeviceID, metadata.Inode)
	if err != nil {
		t.Fatalf("NewConfirmedDiscoverer() error = %v", err)
	}
	discovery, err := discoverer.Discover(context.Background())
	if err != nil {
		t.Fatalf("Discover() error = %v", err)
	}
	plan, err := logs.PlanReconcile(metadata.Path, nil, discovery)
	if err != nil {
		t.Fatalf("PlanReconcile() = %#v, %v", plan, err)
	}
	var liveAction *logs.ReconcileAction
	for index := range plan.Actions {
		if plan.Actions[index].Current != nil && plan.Actions[index].Current.Kind == logs.SourceKindSession {
			liveAction = &plan.Actions[index]
			break
		}
	}
	if liveAction == nil {
		t.Fatalf("PlanReconcile() = %#v, want session action", plan)
	}
	return liveindex.LiveRequest{
		RequestID: "request-scheduler-integration", HomeGeneration: 202,
		HomePath: metadata.Path, HomeDeviceID: metadata.DeviceID, HomeInode: metadata.Inode,
		Action: *liveAction, RequestedAtMS: time.Now().UnixMilli(),
	}
}

func writeSchedulerRollout(t *testing.T, path string, content []byte) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatalf("MkdirAll() error = %v", err)
	}
	if err := os.WriteFile(path, content, 0o600); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}
}

func schedulerRollout(sessionID, turnID string) []byte {
	return []byte(`{"timestamp":"2026-07-14T01:00:00Z","type":"session_meta","payload":{"id":"` + sessionID +
		`","timestamp":"2026-07-14T01:00:00Z","cwd":"/tmp/project","originator":"codex_cli_rs","cli_version":"0.142.3","source":"cli","model_provider":"openai"}}` + "\n" +
		`{"timestamp":"2026-07-14T01:00:01Z","type":"event_msg","payload":{"type":"task_started","turn_id":"` + turnID +
		`","started_at":1783990801,"model_context_window":258000}}` + "\n" +
		`{"timestamp":"2026-07-14T01:00:02Z","type":"event_msg","payload":{"type":"task_complete","turn_id":"` + turnID +
		`","completed_at":1783990802}}` + "\n")
}

func maxSchedulerInt64(left, right int64) int64 {
	if left > right {
		return left
	}
	return right
}
