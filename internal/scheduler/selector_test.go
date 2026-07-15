package scheduler

import (
	"errors"
	"testing"

	"github.com/SisyphusSQ/codex-pulse/internal/store"
)

// 测试 selectTask 在live优先与持久8:1公平之间做稳定选择。
func TestSelectTaskPrioritizesLiveAndForcesBackfillFairness(t *testing.T) {
	t.Parallel()

	tasks := []store.SchedulerTask{
		schedulerTaskFixture("live-later", store.SchedulerLaneLive, 20, 10),
		schedulerTaskFixture("backfill-oldest", store.SchedulerLaneBackfill, 5, 5),
		schedulerTaskFixture("live-oldest", store.SchedulerLaneLive, 10, 2),
	}
	selected, err := selectTask(tasks, nil, 8, 100)
	if err != nil {
		t.Fatalf("selectTask() error = %v", err)
	}
	if selected.Task.TaskID != "live-oldest" || selected.Reason != store.SchedulerSelectionLivePriority ||
		selected.LiveDepth != 2 || selected.BackfillDepth != 1 ||
		selected.OldestLiveWaitMS != 98 || selected.OldestBackfillWaitMS != 95 {
		t.Fatalf("selectTask() = %#v", selected)
	}

	recent := make([]store.SchedulerCycle, 8)
	for index := range recent {
		recent[index] = store.SchedulerCycle{Lane: store.SchedulerLaneLive}
	}
	selected, err = selectTask(tasks, recent, 8, 100)
	if err != nil {
		t.Fatalf("selectTask(fairness) error = %v", err)
	}
	if selected.Task.TaskID != "backfill-oldest" ||
		selected.Reason != store.SchedulerSelectionBackfillFairness {
		t.Fatalf("selectTask(fairness) = %#v", selected)
	}

	recent[0].Lane = store.SchedulerLaneBackfill
	selected, err = selectTask(tasks, recent, 8, 100)
	if err != nil || selected.Task.TaskID != "live-oldest" ||
		selected.Reason != store.SchedulerSelectionLivePriority {
		t.Fatalf("selectTask(after backfill) = %#v, %v", selected, err)
	}
}

// 测试 selectTask 对空队列与单lane返回精确结果。
func TestSelectTaskHandlesEmptyAndSingleLaneQueues(t *testing.T) {
	t.Parallel()

	if _, err := selectTask(nil, nil, 8, 100); !errors.Is(err, ErrQueueEmpty) {
		t.Fatalf("selectTask(empty) error = %v, want ErrQueueEmpty", err)
	}
	live := schedulerTaskFixture("live-only", store.SchedulerLaneLive, 10, 4)
	selected, err := selectTask([]store.SchedulerTask{live}, nil, 8, 100)
	if err != nil || selected.Reason != store.SchedulerSelectionLiveOnly ||
		selected.Task.TaskID != live.TaskID {
		t.Fatalf("selectTask(live only) = %#v, %v", selected, err)
	}
	backfill := schedulerTaskFixture("backfill-only", store.SchedulerLaneBackfill, 10, 4)
	selected, err = selectTask([]store.SchedulerTask{backfill}, nil, 8, 100)
	if err != nil || selected.Reason != store.SchedulerSelectionBackfillOnly ||
		selected.Task.TaskID != backfill.TaskID {
		t.Fatalf("selectTask(backfill only) = %#v, %v", selected, err)
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
