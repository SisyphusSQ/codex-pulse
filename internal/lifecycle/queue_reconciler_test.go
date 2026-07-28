package lifecycle

import (
	"context"
	"reflect"
	"testing"

	logsource "github.com/SisyphusSQ/codex-pulse/internal/codex/logs/source"
	"github.com/SisyphusSQ/codex-pulse/internal/liveindex"
	"github.com/SisyphusSQ/codex-pulse/internal/scheduler"
	"github.com/SisyphusSQ/codex-pulse/internal/store"
)

func TestQueueReconcileRunnerUsesStableTypedLiveQueue(t *testing.T) {
	previous := store.SourceFingerprint{
		SourceFileID: "codex:previous", Provider: logsource.ProviderCodex,
		SourceKind: string(logsource.SourceKindSession), CurrentPath: "/home/sessions/a.jsonl",
		DeviceID: "file-device", Inode: 11, SizeBytes: 10, MTimeNS: 2_000_000,
		PrefixBytes: 10, PrefixSHA256: "prefix-old", FingerprintSHA256: "digest-old",
	}
	current := logsource.Snapshot{
		SourceFileID: previous.SourceFileID, Provider: logsource.ProviderCodex,
		Kind: logsource.SourceKindSession, Path: previous.CurrentPath,
		Fingerprint: logsource.Fingerprint{
			DeviceID: "file-device", Inode: 11, SizeBytes: 20, MTimeNS: 3_000_000,
			PrefixBytes: 10, PrefixSHA256: "prefix-old", Digest: "digest-new",
		},
	}
	old := snapshotFromStoreFingerprint(previous)
	plan := logsource.ReconcilePlan{Actions: []logsource.ReconcileAction{
		{Kind: logsource.ChangeUnchanged, Previous: &old, Current: &old},
		{Kind: logsource.ChangeGrown, Previous: &old, Current: &current},
		{Kind: logsource.ChangeDeleted, Previous: &old},
	}}
	repository := &fakeSnapshotRepository{values: []store.SourceFingerprint{previous}}
	live := &fakeLiveActionStarter{}
	queue := &fakeLiveTaskEnqueuer{}
	var discoveries [][]logsource.Snapshot
	runner, err := NewQueueReconcileRunner(QueueReconcileRunnerConfig{
		Repository: repository, Live: live, Queue: queue, LaneCapacity: 32,
		Discover: func(
			_ context.Context,
			home ConfirmedHome,
			got []logsource.Snapshot,
		) (logsource.ReconcilePlan, error) {
			if home.Path != "/home" || home.DeviceID != "home-device" || home.Inode != 7 {
				t.Fatalf("home = %#v", home)
			}
			discoveries = append(discoveries, got)
			return plan, nil
		},
	})
	if err != nil {
		t.Fatalf("NewQueueReconcileRunner() error = %v", err)
	}
	home := ConfirmedHome{Generation: 4, Path: "/home", DeviceID: "home-device", Inode: 7}
	for attempt := 0; attempt < 2; attempt++ {
		if err := runner.RunReconcile(context.Background(), home, ReconcileSourceChange); err != nil {
			t.Fatalf("RunReconcile() attempt %d error = %v", attempt+1, err)
		}
	}
	if len(discoveries) != 2 || !reflect.DeepEqual(discoveries[0], []logsource.Snapshot{old}) {
		t.Fatalf("discoveries = %#v", discoveries)
	}
	if len(live.requests) != 2 || live.requests[0] != live.requests[1] {
		t.Fatalf("live requests = %#v, want stable replay", live.requests)
	}
	request := live.requests[0]
	if request.HomeGeneration != 4 || request.Action.Kind != logsource.ChangeGrown ||
		request.RequestID == "" || request.RequestedAtMS != 3 {
		t.Fatalf("live request = %#v", request)
	}
	if len(queue.requests) != 2 || queue.requests[0] != queue.requests[1] {
		t.Fatalf("queue requests = %#v, want stable replay", queue.requests)
	}
	task := queue.requests[0]
	if task.TargetID != "live-job-stable" || task.HomeGeneration != 4 ||
		task.Lane != store.SchedulerLaneLive || task.ServiceClass != store.SchedulerServiceBackground ||
		task.LaneCapacity != 32 || task.TaskID == "" || task.DedupeKey == "" {
		t.Fatalf("queue request = %#v", task)
	}
}

type fakeSnapshotRepository struct {
	values []store.SourceFingerprint
}

func (repository *fakeSnapshotRepository) CodexSnapshots(context.Context) ([]store.SourceFingerprint, error) {
	return append([]store.SourceFingerprint(nil), repository.values...), nil
}

type fakeLiveActionStarter struct {
	requests []liveindex.LiveRequest
}

func (starter *fakeLiveActionStarter) Start(
	_ context.Context,
	request liveindex.LiveRequest,
) (store.JobRun, error) {
	starter.requests = append(starter.requests, request)
	return store.JobRun{JobID: "live-job-stable"}, nil
}

type fakeLiveTaskEnqueuer struct {
	requests []scheduler.EnqueueRequest
}

func (queue *fakeLiveTaskEnqueuer) Enqueue(
	_ context.Context,
	request scheduler.EnqueueRequest,
) (store.SchedulerTask, error) {
	queue.requests = append(queue.requests, request)
	return store.SchedulerTask{TaskID: request.TaskID}, nil
}
