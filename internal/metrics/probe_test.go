package metrics

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/SisyphusSQ/codex-pulse/internal/store"
)

// 测试 gopsutil 探针可以在 Pure Go 构建中读取当前进程的累计 CPU 与 RSS。
func TestGopsutilProcessProbeMeasuresCurrentProcess(t *testing.T) {
	t.Parallel()

	probe, err := NewGopsutilProcessProbe(os.Getpid())
	if err != nil {
		t.Fatalf("NewGopsutilProcessProbe() error = %v", err)
	}
	measurement, err := probe.Measure(t.Context())
	if err != nil {
		t.Fatalf("Measure() error = %v", err)
	}
	if measurement.CPUUserMS < 0 || measurement.CPUSystemMS < 0 || measurement.RSSBytes <= 0 {
		t.Fatalf("measurement = %#v", measurement)
	}
}

// 测试文件/队列探针只读取文件元数据与 runnable queue 权威快照。
func TestFileStoreProbeMeasuresFilesAndRunnableQueue(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "codex-pulse.db")
	if err := os.WriteFile(path, []byte("database"), 0o600); err != nil {
		t.Fatalf("write database fixture: %v", err)
	}
	if err := os.WriteFile(path+"-wal", []byte("wal"), 0o600); err != nil {
		t.Fatalf("write WAL fixture: %v", err)
	}
	queue := queueSnapshotStub{snapshot: store.SchedulerQueueSnapshot{
		LiveDepth: 2, BackfillDepth: 3,
		LiveCandidate:     &store.SchedulerTask{EnqueuedAtMS: 800},
		BackfillCandidate: &store.SchedulerTask{EnqueuedAtMS: 700},
	}}
	probe, err := NewFileStoreProbe(path, queue)
	if err != nil {
		t.Fatalf("NewFileStoreProbe() error = %v", err)
	}
	measurement, err := probe.Measure(t.Context(), 1_000)
	if err != nil {
		t.Fatalf("Measure() error = %v", err)
	}
	if measurement.DBBytes != 8 || measurement.WALBytes != 3 || measurement.DiskFreeBytes <= 0 ||
		measurement.LiveQueueDepth != 2 || measurement.BackfillQueueDepth != 3 ||
		measurement.OldestLiveWaitMS != 200 || measurement.OldestBackfillWaitMS != 300 {
		t.Fatalf("measurement = %#v", measurement)
	}
}

// 测试 WAL 尚未创建时按零字节处理，但队列时间倒退会拒绝伪造 wait 指标。
func TestFileStoreProbeHandlesMissingWALAndRejectsFutureQueueCandidate(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "codex-pulse.db")
	if err := os.WriteFile(path, []byte("db"), 0o600); err != nil {
		t.Fatalf("write database fixture: %v", err)
	}
	probe, err := NewFileStoreProbe(path, queueSnapshotStub{})
	if err != nil {
		t.Fatalf("NewFileStoreProbe() error = %v", err)
	}
	measurement, err := probe.Measure(t.Context(), 1_000)
	if err != nil {
		t.Fatalf("Measure(missing WAL) error = %v", err)
	}
	if measurement.WALBytes != 0 {
		t.Fatalf("measurement = %#v", measurement)
	}

	probe, err = NewFileStoreProbe(path, queueSnapshotStub{snapshot: store.SchedulerQueueSnapshot{
		LiveDepth: 1, LiveCandidate: &store.SchedulerTask{EnqueuedAtMS: 1_001},
	}})
	if err != nil {
		t.Fatalf("NewFileStoreProbe(future candidate) error = %v", err)
	}
	if _, err := probe.Measure(t.Context(), 1_000); !errors.Is(err, ErrProbe) {
		t.Fatalf("Measure(future candidate) error = %v, want ErrProbe", err)
	}
}

func TestFileStoreProbeTreatsMissingSchedulerLifecycleAsEmptyQueue(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "codex-pulse.db")
	if err := os.WriteFile(path, []byte("db"), 0o600); err != nil {
		t.Fatalf("write database fixture: %v", err)
	}
	probe, err := NewFileStoreProbe(path, queueSnapshotStub{err: store.ErrSchedulerPaused})
	if err != nil {
		t.Fatalf("NewFileStoreProbe() error = %v", err)
	}
	measurement, err := probe.Measure(t.Context(), 1_000)
	if err != nil {
		t.Fatalf("Measure() error = %v", err)
	}
	if measurement.LiveQueueDepth != 0 || measurement.BackfillQueueDepth != 0 {
		t.Fatalf("measurement = %#v", measurement)
	}
}

type queueSnapshotStub struct {
	snapshot store.SchedulerQueueSnapshot
	err      error
}

func (stub queueSnapshotStub) SchedulerRunnableQueueSnapshot(context.Context) (store.SchedulerQueueSnapshot, error) {
	return stub.snapshot, stub.err
}
