package store

import (
	"context"
	"errors"
	"fmt"
	"math"

	"gorm.io/gorm"

	storesqlite "github.com/SisyphusSQ/codex-pulse/internal/store/sqlite"
)

var (
	ErrSchedulerConflict   = errors.New("scheduler task conflicts with durable identity")
	ErrSchedulerQueueFull  = errors.New("scheduler lane is full")
	ErrSchedulerTransition = errors.New("scheduler task transition is invalid")
	ErrSchedulerBusy       = errors.New("scheduler already has a running task")
	ErrSchedulerPaused     = errors.New("scheduler lifecycle does not permit the task")
)

// EnqueueSchedulerTask 在唯一writer事务中完成exact replay、lane容量检查和插入。
func (repository *Repository) EnqueueSchedulerTask(
	ctx context.Context,
	task SchedulerTask,
	laneCapacity int,
) error {
	if repository == nil || repository.database == nil {
		return ErrInvalidRepository
	}
	if laneCapacity < 1 || laneCapacity > 10000 {
		return fmt.Errorf("%w: lane capacity is invalid", ErrSchedulerTransition)
	}
	if err := validateSchedulerAdmissionTask(task); err != nil {
		return err
	}
	return repository.database.Write(ctx, func(ctx context.Context, transaction storesqlite.WriteTx) error {
		if err := ensureSchedulerLifecycleForAdmission(ctx, transaction, task); err != nil {
			return err
		}
		existing, found, err := schedulerTaskModelByID(ctx, transaction, task.TaskID)
		if err != nil {
			return err
		}
		if found {
			if schedulerTaskAdmissionMatches(existing, task) {
				return nil
			}
			return ErrSchedulerConflict
		}
		if conflict, err := schedulerTaskIdentityExists(ctx, transaction, task); err != nil {
			return err
		} else if conflict {
			return ErrSchedulerConflict
		}
		job, found, err := jobRunByID(ctx, transaction, task.TargetID)
		if err != nil {
			return err
		}
		if !found || !schedulerTargetMatchesJob(task, job) {
			return fmt.Errorf("%w: target job is missing or incompatible", ErrSchedulerConflict)
		}
		var count int64
		result := transaction.WithContext(ctx).Model(&schedulerTaskModel{}).
			Where("lane = ? AND state IN ?", string(task.Lane), schedulerActiveStateStrings()).Count(&count)
		if result.Error != nil {
			return result.Error
		}
		if count >= int64(laneCapacity) {
			return ErrSchedulerQueueFull
		}
		model := schedulerTaskModelFromDomain(task)
		return transaction.WithContext(ctx).Create(&model).Error
	})
}

func (repository *Repository) SchedulerTask(ctx context.Context, taskID string) (SchedulerTask, error) {
	if repository == nil || repository.database == nil || taskID == "" {
		return SchedulerTask{}, ErrInvalidRepository
	}
	var task SchedulerTask
	err := repository.database.View(ctx, func(ctx context.Context, connection storesqlite.ReadConn) error {
		value, found, err := schedulerTaskByID(ctx, connection, taskID)
		if err != nil {
			return err
		}
		if !found {
			return ErrNotFound
		}
		task = value
		return nil
	})
	return task, err
}

// SchedulerTaskByTarget 通过唯一 current target identity 读回 admission。
// scheduler recovery 会保留 TaskID/DedupeKey 并把 TargetID rebind 到 resume job，
// producer 用此读回避免为同一个恢复 attempt 创建第二份 durable task。
func (repository *Repository) SchedulerTaskByTarget(
	ctx context.Context,
	targetID string,
) (SchedulerTask, error) {
	if repository == nil || repository.database == nil || targetID == "" {
		return SchedulerTask{}, ErrInvalidRepository
	}
	var task SchedulerTask
	err := repository.database.View(ctx, func(ctx context.Context, connection storesqlite.ReadConn) error {
		var model schedulerTaskModel
		result := connection.WithContext(ctx).Where("target_id = ?", targetID).Take(&model)
		if errors.Is(result.Error, gorm.ErrRecordNotFound) {
			return ErrNotFound
		}
		if result.Error != nil {
			return result.Error
		}
		value, err := schedulerTaskFromModel(model)
		if err != nil {
			return err
		}
		task = value
		return nil
	})
	return task, err
}

func (repository *Repository) ListSchedulerTasks(
	ctx context.Context,
	filter SchedulerTaskFilter,
) ([]SchedulerTask, error) {
	if repository == nil || repository.database == nil {
		return nil, ErrInvalidRepository
	}
	limit, err := validateRuntimeLimit(filter.Limit)
	if err != nil {
		return nil, err
	}
	var tasks []SchedulerTask
	err = repository.database.View(ctx, func(ctx context.Context, connection storesqlite.ReadConn) error {
		query := connection.WithContext(ctx).Model(&schedulerTaskModel{})
		if filter.State != nil {
			query = query.Where("state = ?", string(*filter.State))
		}
		if filter.Lane != nil {
			query = query.Where("lane = ?", string(*filter.Lane))
		}
		if filter.Active != nil {
			if *filter.Active {
				query = query.Where("state IN ?", schedulerActiveStateStrings())
			} else {
				query = query.Where("state NOT IN ?", schedulerActiveStateStrings())
			}
		}
		var models []schedulerTaskModel
		if err := query.Order("queue_order_ms, task_id").Limit(limit).Find(&models).Error; err != nil {
			return err
		}
		tasks = make([]SchedulerTask, len(models))
		for index, model := range models {
			value, err := schedulerTaskFromModel(model)
			if err != nil {
				return err
			}
			tasks[index] = value
		}
		return nil
	})
	return tasks, err
}

// SchedulerQueueSnapshot 聚合全部queued task，并分别读回两个lane最老候选。
func (repository *Repository) SchedulerQueueSnapshot(ctx context.Context) (SchedulerQueueSnapshot, error) {
	if repository == nil || repository.database == nil {
		return SchedulerQueueSnapshot{}, ErrInvalidRepository
	}
	var snapshot SchedulerQueueSnapshot
	err := repository.database.View(ctx, func(ctx context.Context, connection storesqlite.ReadConn) error {
		return connection.WithContext(ctx).Transaction(func(transaction *gorm.DB) error {
			for _, lane := range []SchedulerLane{SchedulerLaneLive, SchedulerLaneBackfill} {
				query := transaction.WithContext(ctx).Model(&schedulerTaskModel{}).
					Where("state = ? AND lane = ?", string(SchedulerTaskQueued), string(lane))
				var depth int64
				if err := query.Count(&depth).Error; err != nil {
					return err
				}
				if repository.schedulerQueueSnapshotHook != nil {
					if err := repository.schedulerQueueSnapshotHook(lane); err != nil {
						return err
					}
				}
				var model schedulerTaskModel
				result := query.Order("queue_order_ms, task_id").Take(&model)
				if result.Error != nil && !errors.Is(result.Error, gorm.ErrRecordNotFound) {
					return result.Error
				}
				var candidate *SchedulerTask
				if result.Error == nil {
					task, err := schedulerTaskFromModel(model)
					if err != nil {
						return err
					}
					candidate = &task
				}
				if lane == SchedulerLaneLive {
					snapshot.LiveDepth = depth
					snapshot.LiveCandidate = candidate
				} else {
					snapshot.BackfillDepth = depth
					snapshot.BackfillCandidate = candidate
				}
			}
			var tail schedulerTaskModel
			result := transaction.WithContext(ctx).Model(&schedulerTaskModel{}).
				Where("state IN ?", schedulerActiveStateStrings()).
				Order("queue_order_ms DESC, task_id DESC").Take(&tail)
			if errors.Is(result.Error, gorm.ErrRecordNotFound) {
				return nil
			}
			if result.Error != nil {
				return result.Error
			}
			snapshot.MaxQueueOrderMS = tail.QueueOrderMS
			return nil
		})
	})
	return snapshot, err
}

// SchedulerRunnableQueueSnapshot 只暴露 active generation 且被持久 lifecycle
// permit 允许的queued task。SchedulerQueueSnapshot仍供审计读取全量队列。
func (repository *Repository) SchedulerRunnableQueueSnapshot(ctx context.Context) (SchedulerQueueSnapshot, error) {
	if repository == nil || repository.database == nil {
		return SchedulerQueueSnapshot{}, ErrInvalidRepository
	}
	var snapshot SchedulerQueueSnapshot
	err := repository.database.View(ctx, func(ctx context.Context, connection storesqlite.ReadConn) error {
		return connection.WithContext(ctx).Transaction(func(transaction *gorm.DB) error {
			lifecycle, found, err := schedulerLifecycleIn(ctx, transaction)
			if err != nil {
				return err
			}
			if !found {
				return ErrSchedulerPaused
			}
			permittedLanes := make([]string, 0, 2)
			for _, lane := range []SchedulerLane{SchedulerLaneLive, SchedulerLaneBackfill} {
				if !lifecyclePermitsTask(lifecycle, lifecycle.HomeGeneration, lane) {
					continue
				}
				permittedLanes = append(permittedLanes, string(lane))
				query := transaction.WithContext(ctx).Model(&schedulerTaskModel{}).
					Where("state = ? AND lane = ? AND home_generation = ?", string(SchedulerTaskQueued), string(lane), lifecycle.HomeGeneration)
				var depth int64
				if err := query.Count(&depth).Error; err != nil {
					return err
				}
				var model schedulerTaskModel
				result := query.Order("queue_order_ms, task_id").Take(&model)
				if result.Error != nil && !errors.Is(result.Error, gorm.ErrRecordNotFound) {
					return result.Error
				}
				var candidate *SchedulerTask
				if result.Error == nil {
					task, err := schedulerTaskFromModel(model)
					if err != nil {
						return err
					}
					candidate = &task
				}
				if lane == SchedulerLaneLive {
					snapshot.LiveDepth, snapshot.LiveCandidate = depth, candidate
				} else {
					snapshot.BackfillDepth, snapshot.BackfillCandidate = depth, candidate
				}
			}
			if len(permittedLanes) == 0 {
				return nil
			}
			var tail schedulerTaskModel
			result := transaction.WithContext(ctx).Model(&schedulerTaskModel{}).
				Where("state IN ? AND home_generation = ? AND lane IN ?",
					schedulerActiveStateStrings(), lifecycle.HomeGeneration, permittedLanes).
				Order("queue_order_ms DESC, task_id DESC").Take(&tail)
			if errors.Is(result.Error, gorm.ErrRecordNotFound) {
				return nil
			}
			if result.Error != nil {
				return result.Error
			}
			snapshot.MaxQueueOrderMS = tail.QueueOrderMS
			return nil
		})
	})
	return snapshot, err
}

// ListRecoverableSchedulerTasks 按稳定keyset分页读取全部running/interrupted task。
func (repository *Repository) ListRecoverableSchedulerTasks(
	ctx context.Context,
	after *SchedulerTaskCursor,
	limit int,
) ([]SchedulerTask, *SchedulerTaskCursor, error) {
	if repository == nil || repository.database == nil {
		return nil, nil, ErrInvalidRepository
	}
	pageLimit, err := validateRuntimeLimit(limit)
	if err != nil {
		return nil, nil, err
	}
	if after != nil && (after.QueueOrderMS < 0 || after.TaskID == "") {
		return nil, nil, ErrSchedulerTransition
	}
	var tasks []SchedulerTask
	err = repository.database.View(ctx, func(ctx context.Context, connection storesqlite.ReadConn) error {
		query := connection.WithContext(ctx).Model(&schedulerTaskModel{}).
			Where("state IN ?", []string{
				string(SchedulerTaskRunning), string(SchedulerTaskInterrupted),
			})
		if after != nil {
			query = query.Where(
				"queue_order_ms > ? OR (queue_order_ms = ? AND task_id > ?)",
				after.QueueOrderMS, after.QueueOrderMS, after.TaskID,
			)
		}
		var models []schedulerTaskModel
		if err := query.Order("queue_order_ms, task_id").Limit(pageLimit).Find(&models).Error; err != nil {
			return err
		}
		tasks = make([]SchedulerTask, len(models))
		for index, model := range models {
			value, err := schedulerTaskFromModel(model)
			if err != nil {
				return err
			}
			tasks[index] = value
		}
		return nil
	})
	if err != nil || len(tasks) == 0 {
		return tasks, nil, err
	}
	last := tasks[len(tasks)-1]
	return tasks, &SchedulerTaskCursor{QueueOrderMS: last.QueueOrderMS, TaskID: last.TaskID}, nil
}

// PromoteSchedulerTask 将既有后台任务原地提升为interactive，不改变lane、target或queue order。
func (repository *Repository) PromoteSchedulerTask(
	ctx context.Context,
	dedupeKey string,
	atMS int64,
) (SchedulerTask, error) {
	if repository == nil || repository.database == nil || dedupeKey == "" {
		return SchedulerTask{}, ErrInvalidRepository
	}
	var promoted SchedulerTask
	err := repository.database.Write(ctx, func(ctx context.Context, transaction storesqlite.WriteTx) error {
		var model schedulerTaskModel
		result := transaction.WithContext(ctx).Where("dedupe_key = ?", dedupeKey).Take(&model)
		if errors.Is(result.Error, gorm.ErrRecordNotFound) {
			return ErrNotFound
		}
		if result.Error != nil {
			return result.Error
		}
		task, err := schedulerTaskFromModel(model)
		if err != nil {
			return err
		}
		if task.ServiceClass == SchedulerServiceInteractive {
			promoted = task
			return nil
		}
		if task.ServiceClass != SchedulerServiceBackground ||
			(task.State != SchedulerTaskQueued && task.State != SchedulerTaskRunning) {
			return ErrSchedulerTransition
		}
		maximum := MaxSchedulerRunningTimestampMS
		if task.State == SchedulerTaskQueued {
			maximum = MaxSchedulerQueuedTimestampMS
		}
		if atMS <= task.UpdatedAtMS {
			if task.UpdatedAtMS >= maximum {
				return ErrSchedulerTransition
			}
			atMS = task.UpdatedAtMS + 1
		}
		if atMS > maximum {
			return ErrSchedulerTransition
		}
		task.ServiceClass = SchedulerServiceInteractive
		task.UpdatedAtMS = atMS
		result = transaction.WithContext(ctx).Model(&schedulerTaskModel{}).
			Where("task_id = ? AND service_class = ? AND state = ?", task.TaskID,
				string(SchedulerServiceBackground), model.State).
			Updates(map[string]any{
				"service_class": string(task.ServiceClass), "updated_at_ms": task.UpdatedAtMS,
			})
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected != 1 {
			return ErrSchedulerTransition
		}
		promoted = task
		return nil
	})
	return promoted, err
}

// RecoverSchedulerTask 将失去owner的task绑定到已创建的resume target并重新排队。
func (repository *Repository) RecoverSchedulerTask(
	ctx context.Context,
	taskID string,
	expectedTargetID string,
	resumedTargetID string,
	queueOrderMS int64,
	atMS int64,
) (SchedulerTask, error) {
	if repository == nil || repository.database == nil || taskID == "" ||
		expectedTargetID == "" || resumedTargetID == "" {
		return SchedulerTask{}, ErrInvalidRepository
	}
	var recovered SchedulerTask
	err := repository.database.Write(ctx, func(ctx context.Context, transaction storesqlite.WriteTx) error {
		task, found, err := schedulerTaskByID(ctx, transaction, taskID)
		if err != nil {
			return err
		}
		if !found {
			return ErrNotFound
		}
		if task.State == SchedulerTaskQueued && task.TargetID == resumedTargetID {
			recovered = task
			return nil
		}
		if task.TargetID != expectedTargetID ||
			(task.State != SchedulerTaskRunning && task.State != SchedulerTaskInterrupted) ||
			atMS <= task.UpdatedAtMS || queueOrderMS <= task.QueueOrderMS || queueOrderMS > atMS {
			return ErrSchedulerTransition
		}
		lifecycle, lifecycleFound, err := schedulerLifecycleIn(ctx, transaction)
		if err != nil {
			return err
		}
		if !lifecycleFound || !lifecyclePermitsTask(lifecycle, task.HomeGeneration, task.Lane) {
			return ErrSchedulerPaused
		}
		job, found, err := jobRunByID(ctx, transaction, resumedTargetID)
		if err != nil {
			return err
		}
		candidate := task
		candidate.TargetID = resumedTargetID
		if !found || !schedulerTargetMatchesJob(candidate, job) {
			return fmt.Errorf("%w: resumed target job is missing or incompatible", ErrSchedulerConflict)
		}
		expectedState := task.State
		task.TargetID = resumedTargetID
		task.State = SchedulerTaskQueued
		task.QueueOrderMS = queueOrderMS
		task.FinishedAtMS = nil
		task.LastErrorClass = nil
		task.UpdatedAtMS = atMS
		if err := validateSchedulerTask(task); err != nil {
			return err
		}
		if err := updateSchedulerTaskCAS(ctx, transaction, task, expectedState); err != nil {
			return err
		}
		recovered = task
		return nil
	})
	return recovered, err
}

// RequeueFailedSchedulerTask 将稳定创建的新target绑定到failed task，并在同一事务
// 把waiting retry fact改为resolved，关闭target-create/task-rebind crash gap。
func (repository *Repository) RequeueFailedSchedulerTask(
	ctx context.Context,
	taskID string,
	expectedTargetID string,
	resumedTargetID string,
	expectedRetryRevision int64,
	queueOrderMS int64,
	atMS int64,
) (SchedulerTask, error) {
	if repository == nil || repository.database == nil || taskID == "" ||
		expectedTargetID == "" || resumedTargetID == "" || expectedRetryRevision < 1 ||
		expectedRetryRevision == math.MaxInt64 {
		return SchedulerTask{}, ErrInvalidRepository
	}
	var requeued SchedulerTask
	err := repository.database.Write(ctx, func(ctx context.Context, transaction storesqlite.WriteTx) error {
		task, found, err := schedulerTaskByID(ctx, transaction, taskID)
		if err != nil {
			return err
		}
		if !found {
			return ErrNotFound
		}
		retryState, retryFound, err := schedulerRetryStateIn(ctx, transaction, taskID)
		if err != nil {
			return err
		}
		if task.State == SchedulerTaskQueued && task.TargetID == resumedTargetID && retryFound &&
			retryState.Disposition == SchedulerRetryResolved && retryState.Revision == expectedRetryRevision+1 {
			requeued = task
			return nil
		}
		if task.State != SchedulerTaskFailed || task.TargetID != expectedTargetID || !retryFound ||
			retryState.Disposition != SchedulerRetryWaiting || retryState.Revision != expectedRetryRevision ||
			retryState.NextRetryAtMS == nil || atMS < *retryState.NextRetryAtMS ||
			queueOrderMS <= task.QueueOrderMS || queueOrderMS > atMS || atMS <= task.UpdatedAtMS ||
			atMS <= retryState.UpdatedAtMS {
			return ErrSchedulerRetryTransition
		}
		lifecycle, lifecycleFound, err := schedulerLifecycleIn(ctx, transaction)
		if err != nil {
			return err
		}
		if !lifecycleFound || !lifecyclePermitsTask(lifecycle, task.HomeGeneration, task.Lane) {
			return ErrSchedulerPaused
		}
		job, jobFound, err := jobRunByID(ctx, transaction, resumedTargetID)
		if err != nil {
			return err
		}
		candidate := task
		candidate.TargetID = resumedTargetID
		if !jobFound || !schedulerTargetMatchesJob(candidate, job) {
			return fmt.Errorf("%w: resumed target job is missing or incompatible", ErrSchedulerConflict)
		}
		task.TargetID = resumedTargetID
		task.State = SchedulerTaskQueued
		task.QueueOrderMS = queueOrderMS
		task.FinishedAtMS = nil
		task.LastErrorClass = nil
		task.UpdatedAtMS = atMS
		if err := validateSchedulerTask(task); err != nil {
			return err
		}
		if err := updateSchedulerTaskCAS(ctx, transaction, task, SchedulerTaskFailed); err != nil {
			return err
		}
		result := transaction.WithContext(ctx).Model(&schedulerRetryStateModel{}).
			Where("task_id = ? AND revision = ? AND disposition = ?", taskID, expectedRetryRevision, string(SchedulerRetryWaiting)).
			Updates(map[string]any{
				"disposition": string(SchedulerRetryResolved), "next_retry_at_ms": nil,
				"recovery_action": string(SchedulerRecoveryNone), "revision": expectedRetryRevision + 1,
				"updated_at_ms": atMS,
			})
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected != 1 {
			return ErrSchedulerRetryTransition
		}
		requeued = task
		return nil
	})
	return requeued, err
}

func (repository *Repository) ClaimSchedulerTask(
	ctx context.Context,
	taskID string,
	atMS int64,
) (SchedulerTask, error) {
	if repository == nil || repository.database == nil || taskID == "" {
		return SchedulerTask{}, ErrInvalidRepository
	}
	var claimed SchedulerTask
	err := repository.database.Write(ctx, func(ctx context.Context, transaction storesqlite.WriteTx) error {
		task, found, err := schedulerTaskByID(ctx, transaction, taskID)
		if err != nil {
			return err
		}
		if !found || task.State != SchedulerTaskQueued || atMS <= task.UpdatedAtMS ||
			atMS > MaxSchedulerRunningTimestampMS {
			return ErrSchedulerTransition
		}
		lifecycle, lifecycleFound, err := schedulerLifecycleIn(ctx, transaction)
		if err != nil {
			return err
		}
		if !lifecycleFound || !lifecyclePermitsTask(lifecycle, task.HomeGeneration, task.Lane) {
			return ErrSchedulerPaused
		}
		var running int64
		result := transaction.WithContext(ctx).Model(&schedulerTaskModel{}).
			Where("state = ? AND task_id <> ?", string(SchedulerTaskRunning), taskID).Count(&running)
		if result.Error != nil {
			return result.Error
		}
		if running > 0 {
			return ErrSchedulerBusy
		}
		task.State = SchedulerTaskRunning
		if task.FirstStartedAtMS == nil {
			task.FirstStartedAtMS = pointerToValue(atMS)
		}
		task.LastStartedAtMS = pointerToValue(atMS)
		task.UpdatedAtMS = atMS
		if err := updateSchedulerTaskCAS(ctx, transaction, task, SchedulerTaskQueued); err != nil {
			return err
		}
		claimed = task
		return nil
	})
	return claimed, err
}

func (repository *Repository) CommitSchedulerCycle(
	ctx context.Context,
	commit SchedulerCycleCommit,
) error {
	if repository == nil || repository.database == nil {
		return ErrInvalidRepository
	}
	if err := validateSchedulerCycleCommit(commit); err != nil {
		return err
	}
	return repository.database.Write(ctx, func(ctx context.Context, transaction storesqlite.WriteTx) error {
		task, found, err := schedulerTaskByID(ctx, transaction, commit.TaskID)
		if err != nil {
			return err
		}
		if !found || task.State != commit.ExpectedState || commit.ExpectedState != SchedulerTaskRunning ||
			commit.AtMS <= task.UpdatedAtMS || task.LastStartedAtMS == nil ||
			commit.Cycle.StartedAtMS != *task.LastStartedAtMS {
			return ErrSchedulerTransition
		}
		if task.FilesProcessed > math.MaxInt64-commit.FilesDelta ||
			task.BytesProcessed > math.MaxInt64-commit.BytesDelta || task.SliceCount == math.MaxInt64 {
			return ErrSchedulerTransition
		}
		task.State = commit.State
		task.FilesProcessed += commit.FilesDelta
		task.BytesProcessed += commit.BytesDelta
		task.SliceCount++
		task.LastErrorClass = cloneRuntimeErrorClass(commit.ErrorClass)
		task.UpdatedAtMS = commit.AtMS
		if commit.State == SchedulerTaskQueued {
			if commit.QueueOrderMS <= task.QueueOrderMS || commit.QueueOrderMS > commit.AtMS {
				return ErrSchedulerTransition
			}
			task.QueueOrderMS = commit.QueueOrderMS
			task.FinishedAtMS = nil
		} else {
			task.FinishedAtMS = pointerToValue(commit.AtMS)
		}
		if err := validateSchedulerTask(task); err != nil {
			return err
		}
		if err := updateSchedulerTaskCAS(ctx, transaction, task, commit.ExpectedState); err != nil {
			return err
		}
		if commit.Retry != nil {
			if err := applySchedulerRetryMutation(ctx, transaction, task.TaskID, commit.AtMS, *commit.Retry); err != nil {
				return err
			}
		}
		model := schedulerCycleModelFromDomain(commit.Cycle)
		return transaction.WithContext(ctx).Create(&model).Error
	})
}

func (repository *Repository) ListSchedulerCycles(
	ctx context.Context,
	filter SchedulerCycleFilter,
) ([]SchedulerCycle, error) {
	if repository == nil || repository.database == nil {
		return nil, ErrInvalidRepository
	}
	limit, err := validateRuntimeLimit(filter.Limit)
	if err != nil {
		return nil, err
	}
	var cycles []SchedulerCycle
	err = repository.database.View(ctx, func(ctx context.Context, connection storesqlite.ReadConn) error {
		query := connection.WithContext(ctx).Model(&schedulerCycleModel{})
		if filter.TaskID != nil {
			query = query.Where("task_id = ?", *filter.TaskID)
		}
		if filter.Lane != nil {
			query = query.Where("lane = ?", string(*filter.Lane))
		}
		var models []schedulerCycleModel
		if err := query.Order("commit_order DESC").Limit(limit).Find(&models).Error; err != nil {
			return err
		}
		cycles = make([]SchedulerCycle, len(models))
		for index, model := range models {
			value, err := schedulerCycleFromModel(model)
			if err != nil {
				return err
			}
			cycles[index] = value
		}
		return nil
	})
	return cycles, err
}

func (repository *Repository) SchedulerCycle(ctx context.Context, cycleID string) (SchedulerCycle, error) {
	if repository == nil || repository.database == nil || cycleID == "" {
		return SchedulerCycle{}, ErrInvalidRepository
	}
	var cycle SchedulerCycle
	err := repository.database.View(ctx, func(ctx context.Context, connection storesqlite.ReadConn) error {
		var model schedulerCycleModel
		result := connection.WithContext(ctx).Where("cycle_id = ?", cycleID).Take(&model)
		if errors.Is(result.Error, gorm.ErrRecordNotFound) {
			return ErrNotFound
		}
		if result.Error != nil {
			return result.Error
		}
		value, err := schedulerCycleFromModel(model)
		if err != nil {
			return err
		}
		cycle = value
		return nil
	})
	return cycle, err
}

func validateSchedulerTask(task SchedulerTask) error {
	if task.TaskID == "" || task.DedupeKey == "" || task.TargetID == "" ||
		!validSchedulerTargetKind(task.TargetKind) || !validSchedulerLane(task.Lane) ||
		!validSchedulerServiceClass(task.ServiceClass) || !validSchedulerTaskState(task.State) ||
		task.HomeGeneration < 0 || task.QueueOrderMS < 0 || task.EnqueuedAtMS < 0 ||
		task.QueueOrderMS < task.EnqueuedAtMS || task.FilesProcessed < 0 || task.BytesProcessed < 0 ||
		task.SliceCount < 0 || task.UpdatedAtMS < task.EnqueuedAtMS ||
		task.QueueOrderMS > MaxSchedulerTimestampMS || task.EnqueuedAtMS > MaxSchedulerTimestampMS ||
		task.UpdatedAtMS > MaxSchedulerTimestampMS ||
		timestampPointerAfter(task.FirstStartedAtMS, MaxSchedulerTimestampMS) ||
		timestampPointerAfter(task.LastStartedAtMS, MaxSchedulerTimestampMS) ||
		timestampPointerAfter(task.FinishedAtMS, MaxSchedulerTimestampMS) {
		return ErrSchedulerTransition
	}
	if task.State == SchedulerTaskQueued &&
		(task.QueueOrderMS > MaxSchedulerQueuedTimestampMS || task.UpdatedAtMS > MaxSchedulerQueuedTimestampMS) {
		return ErrSchedulerTransition
	}
	if (task.State == SchedulerTaskRunning || task.State == SchedulerTaskInterrupted) &&
		task.UpdatedAtMS > MaxSchedulerRunningTimestampMS {
		return ErrSchedulerTransition
	}
	if (task.State == SchedulerTaskQueued || task.State == SchedulerTaskRunning ||
		task.State == SchedulerTaskInterrupted) && task.SliceCount == math.MaxInt64 {
		return ErrSchedulerTransition
	}
	if err := validateRuntimeErrorClass(task.LastErrorClass); err != nil {
		return err
	}
	if (task.FirstStartedAtMS == nil) != (task.LastStartedAtMS == nil) {
		return ErrSchedulerTransition
	}
	if task.FirstStartedAtMS != nil && (*task.FirstStartedAtMS < task.EnqueuedAtMS ||
		*task.LastStartedAtMS < *task.FirstStartedAtMS) {
		return ErrSchedulerTransition
	}
	if task.FinishedAtMS != nil && (*task.FinishedAtMS < task.EnqueuedAtMS ||
		task.LastStartedAtMS != nil && *task.FinishedAtMS < *task.LastStartedAtMS) {
		return ErrSchedulerTransition
	}
	switch task.State {
	case SchedulerTaskQueued:
		if task.FinishedAtMS != nil || task.LastErrorClass != nil {
			return ErrSchedulerTransition
		}
	case SchedulerTaskRunning:
		if task.LastStartedAtMS == nil || task.FinishedAtMS != nil || task.LastErrorClass != nil {
			return ErrSchedulerTransition
		}
	case SchedulerTaskSucceeded:
		if task.LastStartedAtMS == nil || task.FinishedAtMS == nil || task.LastErrorClass != nil {
			return ErrSchedulerTransition
		}
	case SchedulerTaskFailed:
		if task.LastStartedAtMS == nil || task.FinishedAtMS == nil || task.LastErrorClass == nil {
			return ErrSchedulerTransition
		}
	case SchedulerTaskInterrupted:
		if task.LastStartedAtMS == nil || task.FinishedAtMS == nil {
			return ErrSchedulerTransition
		}
	}
	return nil
}

func validateSchedulerAdmissionTask(task SchedulerTask) error {
	if err := validateSchedulerTask(task); err != nil {
		return err
	}
	if task.State != SchedulerTaskQueued || task.EnqueuedAtMS > MaxSchedulerAdmissionTimestampMS ||
		task.QueueOrderMS != task.EnqueuedAtMS ||
		task.UpdatedAtMS != task.EnqueuedAtMS || task.FirstStartedAtMS != nil ||
		task.LastStartedAtMS != nil || task.FinishedAtMS != nil || task.FilesProcessed != 0 ||
		task.BytesProcessed != 0 || task.SliceCount != 0 || task.LastErrorClass != nil {
		return ErrSchedulerTransition
	}
	return nil
}

func validateSchedulerCycleCommit(commit SchedulerCycleCommit) error {
	cycle := commit.Cycle
	if commit.TaskID == "" || commit.TaskID != cycle.TaskID || commit.FilesDelta < 0 ||
		commit.BytesDelta < 0 || commit.AtMS < 0 || commit.AtMS > MaxSchedulerTimestampMS ||
		commit.QueueOrderMS < 0 || commit.QueueOrderMS > MaxSchedulerTimestampMS ||
		!validSchedulerTaskState(commit.ExpectedState) || !validSchedulerTaskState(commit.State) ||
		cycle.CycleID == "" || !validSchedulerLane(cycle.Lane) ||
		!validSchedulerSelectionReason(cycle.SelectionReason) ||
		!validSchedulerStopReason(cycle.StopReason) || !validSchedulerCycleOutcome(cycle.Outcome) ||
		cycle.BudgetFiles < 0 || cycle.BudgetBytes < 0 || cycle.BudgetActiveMS < 0 ||
		cycle.ConsumedFiles < 0 || cycle.ConsumedFiles > cycle.BudgetFiles ||
		cycle.ConsumedBytes < 0 || cycle.ConsumedBytes > cycle.BudgetBytes || cycle.ActiveMS < 0 ||
		cycle.ActiveMS > cycle.BudgetActiveMS ||
		cycle.LiveDepth < 0 || cycle.BackfillDepth < 0 || cycle.OldestLiveWaitMS < 0 ||
		cycle.OldestBackfillWaitMS < 0 || cycle.StartedAtMS < 0 ||
		cycle.StartedAtMS > MaxSchedulerTimestampMS || cycle.FinishedAtMS > MaxSchedulerTimestampMS ||
		cycle.FinishedAtMS < cycle.StartedAtMS || cycle.FinishedAtMS > commit.AtMS ||
		cycle.ConsumedFiles != commit.FilesDelta || cycle.ConsumedBytes != commit.BytesDelta {
		return ErrSchedulerTransition
	}
	if !schedulerCycleMatchesTaskState(cycle, commit.State) {
		return ErrSchedulerTransition
	}
	if commit.State == SchedulerTaskFailed && commit.ErrorClass == nil ||
		(commit.State == SchedulerTaskQueued || commit.State == SchedulerTaskSucceeded) && commit.ErrorClass != nil {
		return ErrSchedulerTransition
	}
	if commit.State == SchedulerTaskFailed {
		if commit.Retry == nil || commit.ErrorClass == nil ||
			commit.Retry.LastErrorClass != *commit.ErrorClass ||
			(commit.Retry.Disposition != SchedulerRetryWaiting &&
				commit.Retry.Disposition != SchedulerRetryBlocked) ||
			commit.Retry.ExpectedRevision < 0 || commit.Retry.ExpectedRevision == math.MaxInt64 {
			return ErrSchedulerRetryTransition
		}
		candidate := SchedulerRetryState{
			TaskID: commit.TaskID, Disposition: commit.Retry.Disposition,
			FailureCount: commit.Retry.FailureCount, LastErrorClass: commit.Retry.LastErrorClass,
			NextRetryAtMS: commit.Retry.NextRetryAtMS, RecoveryAction: commit.Retry.RecoveryAction,
			Revision: commit.Retry.ExpectedRevision + 1, UpdatedAtMS: commit.AtMS,
		}
		if err := validateSchedulerRetryState(candidate); err != nil {
			return err
		}
	} else if commit.Retry != nil {
		return ErrSchedulerRetryTransition
	}
	return validateRuntimeErrorClass(commit.ErrorClass)
}

func timestampPointerAfter(value *int64, maximum int64) bool {
	return value != nil && *value > maximum
}

func applySchedulerRetryMutation(
	ctx context.Context,
	database *gorm.DB,
	taskID string,
	atMS int64,
	mutation SchedulerRetryMutation,
) error {
	if mutation.ExpectedRevision == math.MaxInt64 || mutation.FailureCount == math.MaxInt64 {
		return ErrSchedulerRetryTransition
	}
	current, found, err := schedulerRetryStateIn(ctx, database, taskID)
	if err != nil {
		return err
	}
	next := SchedulerRetryState{
		TaskID: taskID, Disposition: mutation.Disposition, FailureCount: mutation.FailureCount,
		LastErrorClass: mutation.LastErrorClass, NextRetryAtMS: mutation.NextRetryAtMS,
		RecoveryAction: mutation.RecoveryAction, Revision: mutation.ExpectedRevision + 1, UpdatedAtMS: atMS,
	}
	if err := validateSchedulerRetryState(next); err != nil {
		return err
	}
	if !found {
		if mutation.ExpectedRevision != 0 || mutation.FailureCount != 1 {
			return ErrSchedulerRetryTransition
		}
		model := schedulerRetryStateModelFromDomain(next)
		return database.WithContext(ctx).Create(&model).Error
	}
	if current.Revision == math.MaxInt64 || current.FailureCount == math.MaxInt64 ||
		current.Revision != mutation.ExpectedRevision || mutation.FailureCount != current.FailureCount+1 ||
		atMS <= current.UpdatedAtMS {
		return ErrSchedulerRetryTransition
	}
	model := schedulerRetryStateModelFromDomain(next)
	result := database.WithContext(ctx).Model(&schedulerRetryStateModel{}).
		Where("task_id = ? AND revision = ?", taskID, mutation.ExpectedRevision).
		Updates(map[string]any{
			"disposition": model.Disposition, "failure_count": model.FailureCount,
			"last_error_class": model.LastErrorClass, "next_retry_at_ms": model.NextRetryAtMS,
			"recovery_action": model.RecoveryAction, "revision": model.Revision,
			"updated_at_ms": model.UpdatedAtMS,
		})
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected != 1 {
		return ErrSchedulerRetryTransition
	}
	return nil
}

func schedulerCycleMatchesTaskState(cycle SchedulerCycle, state SchedulerTaskState) bool {
	switch cycle.Outcome {
	case SchedulerCycleCompleted:
		return state == SchedulerTaskSucceeded && cycle.StopReason == SchedulerStopCompleted
	case SchedulerCycleYielded:
		return state == SchedulerTaskQueued && (cycle.StopReason == SchedulerStopFileBudget ||
			cycle.StopReason == SchedulerStopByteBudget || cycle.StopReason == SchedulerStopTimeBudget ||
			cycle.StopReason == SchedulerStopSystemPressure || cycle.StopReason == SchedulerStopLivePreempted)
	case SchedulerCycleFailed:
		return state == SchedulerTaskFailed && cycle.StopReason == SchedulerStopDependencyError
	case SchedulerCycleInterrupted:
		return state == SchedulerTaskInterrupted && (cycle.StopReason == SchedulerStopCancelled ||
			cycle.StopReason == SchedulerStopWorkerPanic)
	default:
		return false
	}
}

func schedulerTaskByID(
	ctx context.Context,
	database *gorm.DB,
	taskID string,
) (SchedulerTask, bool, error) {
	model, found, err := schedulerTaskModelByID(ctx, database, taskID)
	if err != nil || !found {
		return SchedulerTask{}, found, err
	}
	task, err := schedulerTaskFromModel(model)
	return task, err == nil, err
}

func schedulerTaskModelByID(
	ctx context.Context,
	database *gorm.DB,
	taskID string,
) (schedulerTaskModel, bool, error) {
	var model schedulerTaskModel
	result := database.WithContext(ctx).Where("task_id = ?", taskID).Take(&model)
	if errors.Is(result.Error, gorm.ErrRecordNotFound) {
		return schedulerTaskModel{}, false, nil
	}
	if result.Error != nil {
		return schedulerTaskModel{}, false, result.Error
	}
	return model, true, nil
}

func schedulerTaskIdentityExists(ctx context.Context, database *gorm.DB, task SchedulerTask) (bool, error) {
	var count int64
	result := database.WithContext(ctx).Model(&schedulerTaskModel{}).
		Where("dedupe_key = ? OR target_id = ?", task.DedupeKey, task.TargetID).Count(&count)
	return count > 0, result.Error
}

func schedulerTargetMatchesJob(task SchedulerTask, job JobRun) bool {
	if job.State != JobQueued && job.State != JobRunning {
		return false
	}
	return task.TargetKind == SchedulerTargetLiveScan && task.Lane == SchedulerLaneLive && job.Phase == JobPhaseLive ||
		task.TargetKind == SchedulerTargetBootstrap && task.Lane == SchedulerLaneBackfill &&
			(job.Phase == JobPhaseDiscover || job.Phase == JobPhaseFastBootstrap ||
				job.Phase == JobPhaseHistoryBackfill || job.Phase == JobPhaseReconcile)
}

func ensureSchedulerLifecycleForAdmission(
	ctx context.Context,
	database *gorm.DB,
	task SchedulerTask,
) error {
	_, found, err := schedulerLifecycleIn(ctx, database)
	if err != nil || found {
		return err
	}
	initial := SchedulerLifecycle{
		HomeGeneration: task.HomeGeneration, UserPauseScope: LifecyclePauseNone,
		SystemState: LifecycleSystemAwake, Transition: LifecycleTransitionSteady,
		SourceState: LifecycleSourceAvailable,
		LastEventID: "scheduler-admission",
		Revision:    1, UpdatedAtMS: task.EnqueuedAtMS,
	}
	if err := validateInitialSchedulerLifecycle(initial); err != nil {
		return err
	}
	model := schedulerLifecycleModelFromDomain(initial)
	return database.WithContext(ctx).Create(&model).Error
}

func lifecyclePermitsTask(
	lifecycle SchedulerLifecycle,
	homeGeneration int64,
	lane SchedulerLane,
) bool {
	if lifecycle.HomeGeneration != homeGeneration || lifecycle.SystemState != LifecycleSystemAwake ||
		lifecycle.Transition != LifecycleTransitionSteady ||
		lifecycle.SourceState != LifecycleSourceAvailable || lifecycle.UserPauseScope == LifecyclePauseAll {
		return false
	}
	return lane == SchedulerLaneLive ||
		lane == SchedulerLaneBackfill && lifecycle.UserPauseScope != LifecyclePauseBackfill
}

func schedulerActiveStateStrings() []string {
	return []string{string(SchedulerTaskQueued), string(SchedulerTaskRunning), string(SchedulerTaskInterrupted)}
}

func updateSchedulerTaskCAS(
	ctx context.Context,
	database *gorm.DB,
	task SchedulerTask,
	expected SchedulerTaskState,
) error {
	model := schedulerTaskModelFromDomain(task)
	result := database.WithContext(ctx).Model(&schedulerTaskModel{}).
		Where("task_id = ? AND state = ?", task.TaskID, string(expected)).Updates(map[string]any{
		"target_id": model.TargetID, "service_class": model.ServiceClass, "state": model.State,
		"queue_order_ms": model.QueueOrderMS, "first_started_at_ms": model.FirstStartedAtMS,
		"last_started_at_ms": model.LastStartedAtMS, "finished_at_ms": model.FinishedAtMS,
		"files_processed": model.FilesProcessed, "bytes_processed": model.BytesProcessed,
		"slice_count": model.SliceCount, "last_error_class": model.LastErrorClass,
		"updated_at_ms": model.UpdatedAtMS,
	})
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected != 1 {
		return ErrSchedulerTransition
	}
	return nil
}

func schedulerTaskModelFromDomain(task SchedulerTask) schedulerTaskModel {
	return schedulerTaskModel{
		TaskID: task.TaskID, DedupeKey: task.DedupeKey, TargetKind: string(task.TargetKind),
		AdmissionTargetID: task.TargetID, TargetID: task.TargetID,
		HomeGeneration: task.HomeGeneration, Lane: string(task.Lane),
		AdmissionServiceClass: string(task.ServiceClass), ServiceClass: string(task.ServiceClass),
		State:        string(task.State),
		QueueOrderMS: task.QueueOrderMS, EnqueuedAtMS: task.EnqueuedAtMS,
		FirstStartedAtMS: task.FirstStartedAtMS, LastStartedAtMS: task.LastStartedAtMS,
		FinishedAtMS: task.FinishedAtMS, FilesProcessed: task.FilesProcessed,
		BytesProcessed: task.BytesProcessed, SliceCount: task.SliceCount,
		LastErrorClass: runtimeErrorStringPointer(task.LastErrorClass), UpdatedAtMS: task.UpdatedAtMS,
	}
}

func schedulerTaskFromModel(model schedulerTaskModel) (SchedulerTask, error) {
	if model.AdmissionTargetID == "" || !validSchedulerServiceClass(
		SchedulerServiceClass(model.AdmissionServiceClass),
	) {
		return SchedulerTask{}, ErrSchedulerTransition
	}
	task := SchedulerTask{
		TaskID: model.TaskID, DedupeKey: model.DedupeKey, TargetKind: SchedulerTargetKind(model.TargetKind),
		TargetID: model.TargetID, HomeGeneration: model.HomeGeneration, Lane: SchedulerLane(model.Lane),
		ServiceClass: SchedulerServiceClass(model.ServiceClass), State: SchedulerTaskState(model.State),
		QueueOrderMS: model.QueueOrderMS, EnqueuedAtMS: model.EnqueuedAtMS,
		FirstStartedAtMS: model.FirstStartedAtMS, LastStartedAtMS: model.LastStartedAtMS,
		FinishedAtMS: model.FinishedAtMS, FilesProcessed: model.FilesProcessed,
		BytesProcessed: model.BytesProcessed, SliceCount: model.SliceCount,
		LastErrorClass: runtimeErrorClassFromString(model.LastErrorClass), UpdatedAtMS: model.UpdatedAtMS,
	}
	return task, validateSchedulerTask(task)
}

func schedulerCycleModelFromDomain(cycle SchedulerCycle) schedulerCycleModel {
	return schedulerCycleModel{
		CycleID: cycle.CycleID, TaskID: cycle.TaskID, Lane: string(cycle.Lane),
		SelectionReason: string(cycle.SelectionReason), StopReason: string(cycle.StopReason),
		Outcome: string(cycle.Outcome), BudgetFiles: cycle.BudgetFiles,
		BudgetBytes: cycle.BudgetBytes, BudgetActiveMS: cycle.BudgetActiveMS,
		ConsumedFiles: cycle.ConsumedFiles, ConsumedBytes: cycle.ConsumedBytes,
		ActiveMS: cycle.ActiveMS, LiveDepth: cycle.LiveDepth, BackfillDepth: cycle.BackfillDepth,
		OldestLiveWaitMS: cycle.OldestLiveWaitMS, OldestBackfillWaitMS: cycle.OldestBackfillWaitMS,
		StartedAtMS: cycle.StartedAtMS, FinishedAtMS: cycle.FinishedAtMS,
	}
}

func schedulerCycleFromModel(model schedulerCycleModel) (SchedulerCycle, error) {
	cycle := SchedulerCycle{
		CycleID: model.CycleID, TaskID: model.TaskID, Lane: SchedulerLane(model.Lane),
		SelectionReason: SchedulerSelectionReason(model.SelectionReason),
		StopReason:      modelStopReason(model.StopReason), Outcome: SchedulerCycleOutcome(model.Outcome),
		BudgetFiles: model.BudgetFiles, BudgetBytes: model.BudgetBytes,
		BudgetActiveMS: model.BudgetActiveMS, ConsumedFiles: model.ConsumedFiles,
		ConsumedBytes: model.ConsumedBytes, ActiveMS: model.ActiveMS,
		LiveDepth: model.LiveDepth, BackfillDepth: model.BackfillDepth,
		OldestLiveWaitMS: model.OldestLiveWaitMS, OldestBackfillWaitMS: model.OldestBackfillWaitMS,
		StartedAtMS: model.StartedAtMS, FinishedAtMS: model.FinishedAtMS,
	}
	if !validSchedulerLane(cycle.Lane) || !validSchedulerSelectionReason(cycle.SelectionReason) ||
		!validSchedulerStopReason(cycle.StopReason) || !validSchedulerCycleOutcome(cycle.Outcome) {
		return SchedulerCycle{}, ErrSchedulerTransition
	}
	return cycle, nil
}

func modelStopReason(value string) SchedulerStopReason { return SchedulerStopReason(value) }

func schedulerTaskAdmissionMatches(model schedulerTaskModel, task SchedulerTask) bool {
	return model.TaskID == task.TaskID && model.DedupeKey == task.DedupeKey &&
		model.TargetKind == string(task.TargetKind) && model.AdmissionTargetID == task.TargetID &&
		model.HomeGeneration == task.HomeGeneration && model.Lane == string(task.Lane) &&
		model.AdmissionServiceClass == string(task.ServiceClass) &&
		model.EnqueuedAtMS == task.EnqueuedAtMS
}

func cloneRuntimeErrorClass(value *RuntimeErrorClass) *RuntimeErrorClass {
	if value == nil {
		return nil
	}
	copy := *value
	return &copy
}
