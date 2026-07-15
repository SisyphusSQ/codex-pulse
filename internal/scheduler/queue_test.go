package scheduler

import (
	"context"
	"errors"
	"math"
	"strconv"
	"testing"

	"github.com/SisyphusSQ/codex-pulse/internal/store"
)

func TestServiceEnqueueAndPromoteUseExactDurableTaskIdentity(t *testing.T) {
	t.Parallel()

	repository := openSchedulerRepository(t)
	job := store.JobRun{
		JobID: "job-service-enqueue", JobType: "scheduler-test", RequestedBy: "test", Priority: 1,
		State: store.JobQueued, Phase: store.JobPhaseLive, CreatedAtMS: 10, UpdatedAtMS: 10,
	}
	if err := repository.CreateJobRun(context.Background(), job); err != nil {
		t.Fatalf("CreateJobRun() error = %v", err)
	}
	service := newSchedulerTestService(t, repository, &recordingExecutor{})
	request := EnqueueRequest{
		TaskID: "task-service-enqueue", DedupeKey: "live:service-enqueue",
		TargetKind: store.SchedulerTargetLiveScan, TargetID: job.JobID, HomeGeneration: 1,
		Lane: store.SchedulerLaneLive, ServiceClass: store.SchedulerServiceBackground,
		RequestedAtMS: 11, LaneCapacity: 8,
	}
	created, err := service.Enqueue(context.Background(), request)
	if err != nil || created.State != store.SchedulerTaskQueued || created.QueueOrderMS != 11 {
		t.Fatalf("Enqueue() = %#v, %v", created, err)
	}
	replay, err := service.Enqueue(context.Background(), request)
	if err != nil || replay != created {
		t.Fatalf("Enqueue(replay) = %#v, %v, want %#v", replay, err, created)
	}
	promoted, err := service.Promote(context.Background(), request.DedupeKey)
	if err != nil || promoted.ServiceClass != store.SchedulerServiceInteractive ||
		promoted.TaskID != created.TaskID {
		t.Fatalf("Promote() = %#v, %v", promoted, err)
	}
	replay, err = service.Enqueue(context.Background(), request)
	if err != nil || replay != promoted {
		t.Fatalf("Enqueue(replay after promotion) = %#v, %v, want %#v", replay, err, promoted)
	}
	conflict := request
	conflict.ServiceClass = store.SchedulerServiceInteractive
	if _, err := service.Enqueue(context.Background(), conflict); !errors.Is(err, store.ErrSchedulerConflict) {
		t.Fatalf("Enqueue(conflicting admission class) error = %v, want ErrSchedulerConflict", err)
	}
}

func TestServiceEnqueueRejectsTimestampsWithoutLogicalHeadroom(t *testing.T) {
	t.Parallel()

	repository := openSchedulerRepository(t)
	job := store.JobRun{
		JobID: "job-service-enqueue-overflow", JobType: "scheduler-test", RequestedBy: "test", Priority: 1,
		State: store.JobQueued, Phase: store.JobPhaseLive, CreatedAtMS: 10, UpdatedAtMS: 10,
	}
	if err := repository.CreateJobRun(context.Background(), job); err != nil {
		t.Fatalf("CreateJobRun() error = %v", err)
	}
	service := newSchedulerTestService(t, repository, &recordingExecutor{})
	for _, requestedAtMS := range []int64{math.MaxInt64 - 1, math.MaxInt64} {
		identity := strconv.FormatInt(requestedAtMS, 10)
		request := EnqueueRequest{
			TaskID:     "task-service-enqueue-overflow-" + identity,
			DedupeKey:  "live:service-enqueue-overflow-" + identity,
			TargetKind: store.SchedulerTargetLiveScan, TargetID: job.JobID, HomeGeneration: 1,
			Lane: store.SchedulerLaneLive, ServiceClass: store.SchedulerServiceBackground,
			RequestedAtMS: requestedAtMS, LaneCapacity: 8,
		}
		if _, err := service.Enqueue(context.Background(), request); !errors.Is(err, store.ErrSchedulerTransition) {
			t.Fatalf("Enqueue(RequestedAtMS=%d) error = %v, want ErrSchedulerTransition", requestedAtMS, err)
		}
	}
	if _, err := service.afterMS(math.MaxInt64, store.MaxSchedulerTimestampMS); !errors.Is(err, ErrInvalidService) {
		t.Fatalf("afterMS(math.MaxInt64) error = %v, want ErrInvalidService", err)
	}
}
