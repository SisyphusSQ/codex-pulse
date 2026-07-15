package bootstrap

import (
	"context"
	"errors"
	"time"

	"github.com/SisyphusSQ/codex-pulse/internal/codex/logs"
	"github.com/SisyphusSQ/codex-pulse/internal/store"
)

const (
	maxSliceFiles  = int64(1_000_000)
	maxSliceBytes  = int64(1 << 50)
	maxSliceActive = 24 * time.Hour
)

var (
	ErrInvalidSliceBudget = errors.New("invalid bootstrap slice budget")
	errSliceTimeBudget    = errors.New("bootstrap slice time budget exhausted")
)

type SliceStopReason string

const (
	SliceStopNone       SliceStopReason = ""
	SliceStopCompleted  SliceStopReason = "completed"
	SliceStopFileBudget SliceStopReason = "file_budget"
	SliceStopByteBudget SliceStopReason = "byte_budget"
	SliceStopTimeBudget SliceStopReason = "time_budget"
)

type SliceBudget struct {
	MaxFiles  int64
	MaxBytes  int64
	MaxActive time.Duration
}

type SliceReport struct {
	RunReport
	FilesProcessed int64
	BytesRead      int64
	Active         time.Duration
	Complete       bool
	ExhaustedBy    SliceStopReason
}

func (runtime *Runtime) RunSlice(
	ctx context.Context,
	jobID string,
	budget SliceBudget,
) (SliceReport, error) {
	if runtime == nil || runtime.repository == nil || jobID == "" {
		return SliceReport{}, ErrInvalidRuntime
	}
	if !validSliceBudget(budget) {
		return SliceReport{}, ErrInvalidSliceBudget
	}
	job, facts, err := runtime.repository.BootstrapRun(ctx, jobID)
	if err != nil {
		return SliceReport{}, err
	}
	if job.State == store.JobSucceeded {
		return SliceReport{
			RunReport: reportFromRun(job, facts), Complete: true, ExhaustedBy: SliceStopCompleted,
		}, nil
	}
	if job.State != store.JobQueued && job.State != store.JobRunning {
		return SliceReport{RunReport: reportFromRun(job, facts)}, ErrSourceUnavailable
	}
	runCtx, release, err := runtime.registerRun(ctx, facts.HomeGeneration, jobID)
	if err != nil {
		return SliceReport{RunReport: reportFromRun(job, facts)}, err
	}
	defer release()

	tracker := newSliceTracker(budget, runtime.clock())
	complete, executeErr := runtime.executeSlice(runCtx, jobID, tracker)
	if executeErr != nil {
		writeCtx := context.WithoutCancel(ctx)
		var terminalErr error
		if errors.Is(executeErr, context.Canceled) || errors.Is(executeErr, context.DeadlineExceeded) {
			var pause *store.BootstrapPauseReason
			if runtime.isDraining(facts.HomeGeneration) {
				value := store.BootstrapPauseApplicationDraining
				pause = &value
			}
			terminalErr = runtime.terminate(writeCtx, jobID, store.JobInterrupted, executeErr, pause)
		} else {
			terminalErr = runtime.terminate(
				writeCtx, jobID, store.JobFailed, executeErr, sourcePauseReason(executeErr),
			)
		}
		executeErr = errors.Join(executeErr, terminalErr)
	}
	job, facts, readErr := runtime.repository.BootstrapRun(context.WithoutCancel(ctx), jobID)
	if readErr != nil {
		return SliceReport{}, errors.Join(executeErr, readErr)
	}
	active := tracker.elapsed(runtime.clock())
	stop := tracker.stop
	if complete && job.State == store.JobSucceeded {
		stop = SliceStopCompleted
	}
	return SliceReport{
		RunReport: reportFromRun(job, facts), FilesProcessed: tracker.files,
		BytesRead: tracker.bytes, Active: active, Complete: complete && job.State == store.JobSucceeded,
		ExhaustedBy: stop,
	}, executeErr
}

func validSliceBudget(budget SliceBudget) bool {
	return budget.MaxFiles > 0 && budget.MaxFiles <= maxSliceFiles &&
		budget.MaxBytes >= logs.PrefixLimitBytes && budget.MaxBytes <= maxSliceBytes &&
		budget.MaxActive > 0 && budget.MaxActive <= maxSliceActive
}

type sliceTracker struct {
	budget SliceBudget
	start  time.Time
	files  int64
	bytes  int64
	stop   SliceStopReason
}

func newSliceTracker(budget SliceBudget, started time.Time) *sliceTracker {
	return &sliceTracker{budget: budget, start: started}
}

func (tracker *sliceTracker) beginItem(now time.Time) bool {
	if tracker.exhausted(now) {
		return false
	}
	tracker.files++
	return true
}

func (tracker *sliceTracker) exhausted(now time.Time) bool {
	if tracker.stop != SliceStopNone {
		return true
	}
	if tracker.elapsed(now) >= tracker.budget.MaxActive {
		tracker.stop = SliceStopTimeBudget
	} else if tracker.bytes >= tracker.budget.MaxBytes {
		tracker.stop = SliceStopByteBudget
	} else if tracker.files >= tracker.budget.MaxFiles {
		tracker.stop = SliceStopFileBudget
	}
	return tracker.stop != SliceStopNone
}

func (tracker *sliceTracker) remainingBytes() int64 {
	return tracker.budget.MaxBytes - tracker.bytes
}

func (tracker *sliceTracker) addBytes(value int64) {
	tracker.bytes += value
}

func (tracker *sliceTracker) stopForPartialRead() {
	if tracker.stop == SliceStopNone && tracker.bytes >= tracker.budget.MaxBytes {
		tracker.stop = SliceStopByteBudget
	}
}

func (tracker *sliceTracker) stopForTime(now time.Time) bool {
	if tracker.elapsed(now) < tracker.budget.MaxActive {
		return false
	}
	tracker.stop = SliceStopTimeBudget
	return true
}

func (tracker *sliceTracker) elapsed(now time.Time) time.Duration {
	elapsed := now.Sub(tracker.start)
	if elapsed < 0 {
		return 0
	}
	return elapsed
}
