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
