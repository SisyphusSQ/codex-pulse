package scheduler

import (
	"errors"
	"testing"

	"github.com/SisyphusSQ/codex-pulse/internal/store"
)

// 测试 selectQueueSnapshot 在live优先与持久8:1公平之间做稳定选择。
func TestSelectQueueSnapshotPrioritizesLiveAndForcesBackfillFairness(t *testing.T) {
	t.Parallel()

	live := schedulerTaskFixture("live-oldest", store.SchedulerLaneLive, 10, 2)
	backfill := schedulerTaskFixture("backfill-oldest", store.SchedulerLaneBackfill, 5, 5)
	snapshot := store.SchedulerQueueSnapshot{
		LiveCandidate: &live, BackfillCandidate: &backfill,
		LiveDepth: 2, BackfillDepth: 1,
	}
	selected, err := selectQueueSnapshot(snapshot, nil, 8, 100)
	if err != nil {
		t.Fatalf("selectQueueSnapshot() error = %v", err)
	}
	if selected.Task.TaskID != "live-oldest" || selected.Reason != store.SchedulerSelectionLivePriority ||
		selected.LiveDepth != 2 || selected.BackfillDepth != 1 ||
		selected.OldestLiveWaitMS != 98 || selected.OldestBackfillWaitMS != 95 {
		t.Fatalf("selectQueueSnapshot() = %#v", selected)
	}

	recent := make([]store.SchedulerCycle, 8)
	for index := range recent {
		recent[index] = store.SchedulerCycle{Lane: store.SchedulerLaneLive}
	}
	selected, err = selectQueueSnapshot(snapshot, recent, 8, 100)
	if err != nil {
		t.Fatalf("selectQueueSnapshot(fairness) error = %v", err)
	}
	if selected.Task.TaskID != "backfill-oldest" ||
		selected.Reason != store.SchedulerSelectionBackfillFairness {
		t.Fatalf("selectQueueSnapshot(fairness) = %#v", selected)
	}

	recent[0].Lane = store.SchedulerLaneBackfill
	selected, err = selectQueueSnapshot(snapshot, recent, 8, 100)
	if err != nil || selected.Task.TaskID != "live-oldest" ||
		selected.Reason != store.SchedulerSelectionLivePriority {
		t.Fatalf("selectQueueSnapshot(after backfill) = %#v, %v", selected, err)
	}
}

// 测试 selectQueueSnapshot 对空队列与单lane返回精确结果。
func TestSelectQueueSnapshotHandlesEmptyAndSingleLaneQueues(t *testing.T) {
	t.Parallel()

	if _, err := selectQueueSnapshot(store.SchedulerQueueSnapshot{}, nil, 8, 100); !errors.Is(err, ErrQueueEmpty) {
		t.Fatalf("selectQueueSnapshot(empty) error = %v, want ErrQueueEmpty", err)
	}
	live := schedulerTaskFixture("live-only", store.SchedulerLaneLive, 10, 4)
	selected, err := selectQueueSnapshot(store.SchedulerQueueSnapshot{
		LiveCandidate: &live, LiveDepth: 1,
	}, nil, 8, 100)
	if err != nil || selected.Reason != store.SchedulerSelectionLiveOnly ||
		selected.Task.TaskID != live.TaskID {
		t.Fatalf("selectQueueSnapshot(live only) = %#v, %v", selected, err)
	}
	backfill := schedulerTaskFixture("backfill-only", store.SchedulerLaneBackfill, 10, 4)
	selected, err = selectQueueSnapshot(store.SchedulerQueueSnapshot{
		BackfillCandidate: &backfill, BackfillDepth: 1,
	}, nil, 8, 100)
	if err != nil || selected.Reason != store.SchedulerSelectionBackfillOnly ||
		selected.Task.TaskID != backfill.TaskID {
		t.Fatalf("selectQueueSnapshot(backfill only) = %#v, %v", selected, err)
	}
}

func schedulerTaskFixture(
	taskID string,
	lane store.SchedulerLane,
	queueOrderMS int64,
	enqueuedAtMS int64,
) store.SchedulerTask {
	return store.SchedulerTask{
		TaskID: taskID, Lane: lane, State: store.SchedulerTaskQueued,
		QueueOrderMS: queueOrderMS, EnqueuedAtMS: enqueuedAtMS,
	}
}
