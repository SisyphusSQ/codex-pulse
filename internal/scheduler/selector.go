package scheduler

import (
	"errors"
	"fmt"

	"github.com/SisyphusSQ/codex-pulse/internal/store"
)

var (
	ErrQueueEmpty           = errors.New("scheduler queue is empty")
	ErrInvalidQueueSnapshot = errors.New("invalid scheduler queue snapshot")
)

// QueueSelection 是一次只读队列选择及其当时的公平性观测。
type QueueSelection struct {
	Task                 store.SchedulerTask
	Reason               store.SchedulerSelectionReason
	LiveDepth            int64
	BackfillDepth        int64
	OldestLiveWaitMS     int64
	OldestBackfillWaitMS int64
}

// selectTask 从Store按新到旧返回的recent cycles计算连续live次数，并在lane内按queue_order稳定选择。
func selectTask(
	tasks []store.SchedulerTask,
	recentCycles []store.SchedulerCycle,
	maxLiveBurst int,
	nowMS int64,
) (QueueSelection, error) {
	if maxLiveBurst <= 0 || nowMS < 0 {
		return QueueSelection{}, ErrInvalidQueueSnapshot
	}

	var live *store.SchedulerTask
	var backfill *store.SchedulerTask
	selection := QueueSelection{}
	for index := range tasks {
		task := tasks[index]
		if task.State != store.SchedulerTaskQueued {
			continue
		}
		if task.TaskID == "" || task.QueueOrderMS < 0 || task.EnqueuedAtMS < 0 || task.EnqueuedAtMS > nowMS {
			return QueueSelection{}, fmt.Errorf("%w: task %q", ErrInvalidQueueSnapshot, task.TaskID)
		}
		switch task.Lane {
		case store.SchedulerLaneLive:
			selection.LiveDepth++
			selection.OldestLiveWaitMS = oldestWait(
				selection.OldestLiveWaitMS, selection.LiveDepth, nowMS-task.EnqueuedAtMS,
			)
			if live == nil || schedulerTaskBefore(task, *live) {
				candidate := task
				live = &candidate
			}
		case store.SchedulerLaneBackfill:
			selection.BackfillDepth++
			selection.OldestBackfillWaitMS = oldestWait(
				selection.OldestBackfillWaitMS, selection.BackfillDepth, nowMS-task.EnqueuedAtMS,
			)
			if backfill == nil || schedulerTaskBefore(task, *backfill) {
				candidate := task
				backfill = &candidate
			}
		default:
			return QueueSelection{}, fmt.Errorf("%w: task %q lane %q", ErrInvalidQueueSnapshot, task.TaskID, task.Lane)
		}
	}

	switch {
	case live == nil && backfill == nil:
		return QueueSelection{}, ErrQueueEmpty
	case backfill == nil:
		selection.Task = *live
		selection.Reason = store.SchedulerSelectionLiveOnly
	case live == nil:
		selection.Task = *backfill
		selection.Reason = store.SchedulerSelectionBackfillOnly
	case consecutiveLiveCycles(recentCycles) >= maxLiveBurst:
		selection.Task = *backfill
		selection.Reason = store.SchedulerSelectionBackfillFairness
	default:
		selection.Task = *live
		selection.Reason = store.SchedulerSelectionLivePriority
	}
	return selection, nil
}

func selectQueueSnapshot(
	snapshot store.SchedulerQueueSnapshot,
	recentCycles []store.SchedulerCycle,
	maxLiveBurst int,
	nowMS int64,
) (QueueSelection, error) {
	if maxLiveBurst <= 0 || nowMS < 0 || snapshot.LiveDepth < 0 || snapshot.BackfillDepth < 0 ||
		(snapshot.LiveCandidate == nil) != (snapshot.LiveDepth == 0) ||
		(snapshot.BackfillCandidate == nil) != (snapshot.BackfillDepth == 0) {
		return QueueSelection{}, ErrInvalidQueueSnapshot
	}
	selection := QueueSelection{
		LiveDepth: snapshot.LiveDepth, BackfillDepth: snapshot.BackfillDepth,
	}
	if snapshot.LiveCandidate != nil {
		if err := validateQueueCandidate(*snapshot.LiveCandidate, store.SchedulerLaneLive, nowMS); err != nil {
			return QueueSelection{}, err
		}
		selection.OldestLiveWaitMS = nowMS - snapshot.LiveCandidate.EnqueuedAtMS
	}
	if snapshot.BackfillCandidate != nil {
		if err := validateQueueCandidate(*snapshot.BackfillCandidate, store.SchedulerLaneBackfill, nowMS); err != nil {
			return QueueSelection{}, err
		}
		selection.OldestBackfillWaitMS = nowMS - snapshot.BackfillCandidate.EnqueuedAtMS
	}
	switch {
	case snapshot.LiveCandidate == nil && snapshot.BackfillCandidate == nil:
		return QueueSelection{}, ErrQueueEmpty
	case snapshot.BackfillCandidate == nil:
		selection.Task = *snapshot.LiveCandidate
		selection.Reason = store.SchedulerSelectionLiveOnly
	case snapshot.LiveCandidate == nil:
		selection.Task = *snapshot.BackfillCandidate
		selection.Reason = store.SchedulerSelectionBackfillOnly
	case consecutiveLiveCycles(recentCycles) >= maxLiveBurst:
		selection.Task = *snapshot.BackfillCandidate
		selection.Reason = store.SchedulerSelectionBackfillFairness
	default:
		selection.Task = *snapshot.LiveCandidate
		selection.Reason = store.SchedulerSelectionLivePriority
	}
	return selection, nil
}

func validateQueueCandidate(task store.SchedulerTask, lane store.SchedulerLane, nowMS int64) error {
	if task.State != store.SchedulerTaskQueued || task.Lane != lane || task.TaskID == "" ||
		task.QueueOrderMS < 0 || task.EnqueuedAtMS < 0 || task.EnqueuedAtMS > nowMS {
		return fmt.Errorf("%w: task %q", ErrInvalidQueueSnapshot, task.TaskID)
	}
	return nil
}

func oldestWait(current int64, depth int64, candidate int64) int64 {
	if depth == 1 || candidate > current {
		return candidate
	}
	return current
}

func schedulerTaskBefore(left store.SchedulerTask, right store.SchedulerTask) bool {
	return left.QueueOrderMS < right.QueueOrderMS ||
		(left.QueueOrderMS == right.QueueOrderMS && left.TaskID < right.TaskID)
}

func consecutiveLiveCycles(cycles []store.SchedulerCycle) int {
	count := 0
	for _, cycle := range cycles {
		if cycle.Lane != store.SchedulerLaneLive {
			break
		}
		count++
	}
	return count
}
