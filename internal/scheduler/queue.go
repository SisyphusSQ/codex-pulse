package scheduler

import (
	"context"

	"github.com/SisyphusSQ/codex-pulse/internal/store"
)

type EnqueueRequest struct {
	TaskID         string
	DedupeKey      string
	TargetKind     store.SchedulerTargetKind
	TargetID       string
	HomeGeneration int64
	Lane           store.SchedulerLane
	ServiceClass   store.SchedulerServiceClass
	RequestedAtMS  int64
	LaneCapacity   int
}

// Enqueue 只让producer提供稳定identity与调度意图，状态与时间字段由service固定构造。
func (service *Service) Enqueue(
	ctx context.Context,
	request EnqueueRequest,
) (store.SchedulerTask, error) {
	if service == nil || service.repository == nil {
		return store.SchedulerTask{}, ErrInvalidService
	}
	if err := ctx.Err(); err != nil {
		return store.SchedulerTask{}, err
	}
	task := store.SchedulerTask{
		TaskID: request.TaskID, DedupeKey: request.DedupeKey,
		TargetKind: request.TargetKind, TargetID: request.TargetID,
		HomeGeneration: request.HomeGeneration, Lane: request.Lane,
		ServiceClass: request.ServiceClass, State: store.SchedulerTaskQueued,
		QueueOrderMS: request.RequestedAtMS, EnqueuedAtMS: request.RequestedAtMS,
		UpdatedAtMS: request.RequestedAtMS,
	}
	if err := service.repository.EnqueueSchedulerTask(ctx, task, request.LaneCapacity); err != nil {
		return store.SchedulerTask{}, err
	}
	return service.repository.SchedulerTask(ctx, task.TaskID)
}

func (service *Service) Promote(
	ctx context.Context,
	dedupeKey string,
) (store.SchedulerTask, error) {
	if service == nil || service.repository == nil || dedupeKey == "" {
		return store.SchedulerTask{}, ErrInvalidService
	}
	atMS, err := service.afterMS(0, store.MaxSchedulerTimestampMS)
	if err != nil {
		return store.SchedulerTask{}, err
	}
	return service.repository.PromoteSchedulerTask(ctx, dedupeKey, atMS)
}
