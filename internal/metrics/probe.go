package metrics

import (
	"context"
	"errors"
	"fmt"
	"math"
	"os"
	"path/filepath"

	"github.com/SisyphusSQ/codex-pulse/internal/store"
	"github.com/shirou/gopsutil/v4/disk"
	"github.com/shirou/gopsutil/v4/process"
)

var ErrProbe = errors.New("runtime metrics probe")

type GopsutilProcessProbe struct {
	pid int32
}

func NewGopsutilProcessProbe(pid int) (*GopsutilProcessProbe, error) {
	if pid <= 0 || int64(pid) > math.MaxInt32 {
		return nil, fmt.Errorf("%w: invalid process id", ErrProbe)
	}
	return &GopsutilProcessProbe{pid: int32(pid)}, nil
}

func (probe *GopsutilProcessProbe) Measure(ctx context.Context) (ProcessMeasurement, error) {
	if probe == nil || probe.pid <= 0 {
		return ProcessMeasurement{}, fmt.Errorf("%w: invalid process probe", ErrProbe)
	}
	currentProcess, err := process.NewProcessWithContext(ctx, probe.pid)
	if err != nil {
		return ProcessMeasurement{}, fmt.Errorf("%w: open process: %w", ErrProbe, err)
	}
	times, err := currentProcess.TimesWithContext(ctx)
	if err != nil {
		return ProcessMeasurement{}, fmt.Errorf("%w: read CPU times: %w", ErrProbe, err)
	}
	memory, err := currentProcess.MemoryInfoWithContext(ctx)
	if err != nil {
		return ProcessMeasurement{}, fmt.Errorf("%w: read memory info: %w", ErrProbe, err)
	}
	userMS, err := secondsToMilliseconds(times.User)
	if err != nil {
		return ProcessMeasurement{}, err
	}
	systemMS, err := secondsToMilliseconds(times.System)
	if err != nil {
		return ProcessMeasurement{}, err
	}
	rssBytes, err := uint64ToInt64(memory.RSS, "RSS bytes")
	if err != nil {
		return ProcessMeasurement{}, err
	}
	return ProcessMeasurement{CPUUserMS: userMS, CPUSystemMS: systemMS, RSSBytes: rssBytes}, nil
}

type QueueSnapshotReader interface {
	SchedulerRunnableQueueSnapshot(context.Context) (store.SchedulerQueueSnapshot, error)
}

type FileStoreProbe struct {
	databasePath string
	queue        QueueSnapshotReader
}

func NewFileStoreProbe(databasePath string, queue QueueSnapshotReader) (*FileStoreProbe, error) {
	if databasePath == "" || queue == nil {
		return nil, fmt.Errorf("%w: database path and queue reader are required", ErrProbe)
	}
	absolutePath, err := filepath.Abs(databasePath)
	if err != nil {
		return nil, fmt.Errorf("%w: resolve database path: %w", ErrProbe, err)
	}
	return &FileStoreProbe{databasePath: filepath.Clean(absolutePath), queue: queue}, nil
}

func (probe *FileStoreProbe) Measure(ctx context.Context, capturedAtMS int64) (StoreMeasurement, error) {
	if probe == nil || probe.databasePath == "" || probe.queue == nil || capturedAtMS < 0 {
		return StoreMeasurement{}, fmt.Errorf("%w: invalid file/store probe", ErrProbe)
	}
	if err := ctx.Err(); err != nil {
		return StoreMeasurement{}, fmt.Errorf("%w: measure cancelled: %w", ErrProbe, err)
	}
	databaseBytes, err := fileSize(probe.databasePath, false)
	if err != nil {
		return StoreMeasurement{}, err
	}
	walBytes, err := fileSize(probe.databasePath+"-wal", true)
	if err != nil {
		return StoreMeasurement{}, err
	}
	usage, err := disk.UsageWithContext(ctx, filepath.Dir(probe.databasePath))
	if err != nil {
		return StoreMeasurement{}, fmt.Errorf("%w: read disk usage: %w", ErrProbe, err)
	}
	diskFreeBytes, err := uint64ToInt64(usage.Free, "disk free bytes")
	if err != nil {
		return StoreMeasurement{}, err
	}
	snapshot, err := probe.queue.SchedulerRunnableQueueSnapshot(ctx)
	if errors.Is(err, store.ErrSchedulerPaused) {
		snapshot = store.SchedulerQueueSnapshot{}
	} else if err != nil {
		return StoreMeasurement{}, fmt.Errorf("%w: read runnable queue: %w", ErrProbe, err)
	}
	liveWaitMS, err := queueWait(snapshot.LiveCandidate, snapshot.LiveDepth, capturedAtMS, "live")
	if err != nil {
		return StoreMeasurement{}, err
	}
	backfillWaitMS, err := queueWait(snapshot.BackfillCandidate, snapshot.BackfillDepth, capturedAtMS, "backfill")
	if err != nil {
		return StoreMeasurement{}, err
	}
	return StoreMeasurement{
		DBBytes: databaseBytes, WALBytes: walBytes, DiskFreeBytes: diskFreeBytes,
		LiveQueueDepth: snapshot.LiveDepth, BackfillQueueDepth: snapshot.BackfillDepth,
		OldestLiveWaitMS: liveWaitMS, OldestBackfillWaitMS: backfillWaitMS,
	}, nil
}

func fileSize(path string, missingAllowed bool) (int64, error) {
	info, err := os.Stat(path)
	if missingAllowed && errors.Is(err, os.ErrNotExist) {
		return 0, nil
	}
	if err != nil {
		return 0, fmt.Errorf("%w: stat %q: %w", ErrProbe, path, err)
	}
	if !info.Mode().IsRegular() || info.Size() < 0 {
		return 0, fmt.Errorf("%w: %q is not a regular file", ErrProbe, path)
	}
	return info.Size(), nil
}

func queueWait(candidate *store.SchedulerTask, depth, capturedAtMS int64, lane string) (int64, error) {
	if depth < 0 || (candidate == nil) != (depth == 0) {
		return 0, fmt.Errorf("%w: inconsistent %s queue snapshot", ErrProbe, lane)
	}
	if candidate == nil {
		return 0, nil
	}
	if candidate.EnqueuedAtMS < 0 || candidate.EnqueuedAtMS > capturedAtMS {
		return 0, fmt.Errorf("%w: invalid %s queue candidate time", ErrProbe, lane)
	}
	return capturedAtMS - candidate.EnqueuedAtMS, nil
}

func secondsToMilliseconds(seconds float64) (int64, error) {
	if math.IsNaN(seconds) || math.IsInf(seconds, 0) || seconds < 0 || seconds > float64(math.MaxInt64)/1_000 {
		return 0, fmt.Errorf("%w: invalid CPU time", ErrProbe)
	}
	return int64(math.Round(seconds * 1_000)), nil
}

func uint64ToInt64(value uint64, field string) (int64, error) {
	if value > math.MaxInt64 {
		return 0, fmt.Errorf("%w: %s exceeds signed range", ErrProbe, field)
	}
	return int64(value), nil
}
