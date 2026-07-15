package store

import (
	"context"
	"errors"
	"math"

	"github.com/SisyphusSQ/codex-pulse/internal/runtimeclock"
	"gorm.io/gorm"

	storesqlite "github.com/SisyphusSQ/codex-pulse/internal/store/sqlite"
)

var (
	ErrLifecycleConflict        = errors.New("scheduler lifecycle conflicts with durable state")
	ErrLifecycleTransition      = errors.New("scheduler lifecycle transition is invalid")
	ErrSchedulerRetryTransition = errors.New("scheduler retry transition is invalid")
)

func (repository *Repository) InitializeSchedulerLifecycle(
	ctx context.Context,
	initial SchedulerLifecycle,
) (SchedulerLifecycle, error) {
	if repository == nil || repository.database == nil {
		return SchedulerLifecycle{}, ErrInvalidRepository
	}
	if err := validateInitialSchedulerLifecycle(initial); err != nil {
		return SchedulerLifecycle{}, err
	}
	var stored SchedulerLifecycle
	err := repository.database.Write(ctx, func(ctx context.Context, transaction storesqlite.WriteTx) error {
		current, found, err := schedulerLifecycleIn(ctx, transaction)
		if err != nil {
			return err
		}
		if found {
			if current == initial {
				stored = current
				return nil
			}
			return ErrLifecycleConflict
		}
		model := schedulerLifecycleModelFromDomain(initial)
		if err := transaction.WithContext(ctx).Create(&model).Error; err != nil {
			return err
		}
		stored = initial
		return nil
	})
	return stored, err
}

func (repository *Repository) SchedulerLifecycle(ctx context.Context) (SchedulerLifecycle, error) {
	if repository == nil || repository.database == nil {
		return SchedulerLifecycle{}, ErrInvalidRepository
	}
	var stored SchedulerLifecycle
	err := repository.database.View(ctx, func(ctx context.Context, connection storesqlite.ReadConn) error {
		value, found, err := schedulerLifecycleIn(ctx, connection)
		if err != nil {
			return err
		}
		if !found {
			return ErrNotFound
		}
		stored = value
		return nil
	})
	return stored, err
}

func (repository *Repository) CompareAndSwapSchedulerLifecycle(
	ctx context.Context,
	expectedRevision int64,
	next SchedulerLifecycle,
) (SchedulerLifecycle, error) {
	if repository == nil || repository.database == nil {
		return SchedulerLifecycle{}, ErrInvalidRepository
	}
	if err := validateSchedulerLifecycle(next); err != nil {
		return SchedulerLifecycle{}, err
	}
	var stored SchedulerLifecycle
	err := repository.database.Write(ctx, func(ctx context.Context, transaction storesqlite.WriteTx) error {
		current, found, err := schedulerLifecycleIn(ctx, transaction)
		if err != nil {
			return err
		}
		if !found {
			return ErrNotFound
		}
		if current == next {
			stored = current
			return nil
		}
		if current.Revision == math.MaxInt64 || current.Revision != expectedRevision ||
			next.Revision != current.Revision+1 ||
			next.UpdatedAtMS <= current.UpdatedAtMS {
			return ErrLifecycleConflict
		}
		model := schedulerLifecycleModelFromDomain(next)
		result := transaction.WithContext(ctx).Model(&schedulerLifecycleModel{}).
			Where("control_key = ? AND revision = ?", int64(1), expectedRevision).
			Updates(map[string]any{
				"home_generation":  model.HomeGeneration,
				"user_pause_scope": model.UserPauseScope,
				"system_state":     model.SystemState,
				"transition":       model.Transition,
				"source_state":     model.SourceState,
				"last_event_id":    model.LastEventID,
				"revision":         model.Revision,
				"updated_at_ms":    model.UpdatedAtMS,
			})
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected != 1 {
			return ErrLifecycleConflict
		}
		stored = next
		return nil
	})
	return stored, err
}

func validateInitialSchedulerLifecycle(value SchedulerLifecycle) error {
	if err := validateSchedulerLifecycle(value); err != nil {
		return err
	}
	if value.Revision != 1 {
		return ErrLifecycleTransition
	}
	return nil
}

func validateSchedulerLifecycle(value SchedulerLifecycle) error {
	if value.HomeGeneration < 0 || value.LastEventID == "" || len(value.LastEventID) > 256 ||
		value.Revision < 1 || value.UpdatedAtMS < 0 || value.UpdatedAtMS > runtimeclock.MaxTimestampMS ||
		!validLifecyclePauseScope(value.UserPauseScope) ||
		!validLifecycleSystemState(value.SystemState) ||
		!validLifecycleTransition(value.Transition) ||
		!validLifecycleSourceState(value.SourceState) {
		return ErrLifecycleTransition
	}
	return nil
}

func schedulerLifecycleIn(
	ctx context.Context,
	database *gorm.DB,
) (SchedulerLifecycle, bool, error) {
	var model schedulerLifecycleModel
	result := database.WithContext(ctx).Where("control_key = ?", int64(1)).Take(&model)
	if errors.Is(result.Error, gorm.ErrRecordNotFound) {
		return SchedulerLifecycle{}, false, nil
	}
	if result.Error != nil {
		return SchedulerLifecycle{}, false, result.Error
	}
	value := schedulerLifecycleFromModel(model)
	if err := validateSchedulerLifecycle(value); err != nil {
		return SchedulerLifecycle{}, false, err
	}
	return value, true, nil
}

func schedulerLifecycleModelFromDomain(value SchedulerLifecycle) schedulerLifecycleModel {
	return schedulerLifecycleModel{
		ControlKey: 1, HomeGeneration: value.HomeGeneration,
		UserPauseScope: string(value.UserPauseScope), SystemState: string(value.SystemState),
		Transition: string(value.Transition), SourceState: string(value.SourceState),
		LastEventID: value.LastEventID, Revision: value.Revision, UpdatedAtMS: value.UpdatedAtMS,
	}
}

func schedulerLifecycleFromModel(model schedulerLifecycleModel) SchedulerLifecycle {
	return SchedulerLifecycle{
		HomeGeneration: model.HomeGeneration,
		UserPauseScope: LifecyclePauseScope(model.UserPauseScope),
		SystemState:    LifecycleSystemState(model.SystemState),
		Transition:     LifecycleTransition(model.Transition),
		SourceState:    LifecycleSourceState(model.SourceState),
		LastEventID:    model.LastEventID, Revision: model.Revision, UpdatedAtMS: model.UpdatedAtMS,
	}
}

func (repository *Repository) SchedulerRetryState(
	ctx context.Context,
	taskID string,
) (SchedulerRetryState, error) {
	if repository == nil || repository.database == nil || taskID == "" {
		return SchedulerRetryState{}, ErrInvalidRepository
	}
	var stored SchedulerRetryState
	err := repository.database.View(ctx, func(ctx context.Context, connection storesqlite.ReadConn) error {
		value, found, err := schedulerRetryStateIn(ctx, connection, taskID)
		if err != nil {
			return err
		}
		if !found {
			return ErrNotFound
		}
		stored = value
		return nil
	})
	return stored, err
}

func (repository *Repository) ListDueSchedulerRetries(
	ctx context.Context,
	homeGeneration int64,
	atMS int64,
	after *SchedulerRetryCursor,
	limit int,
) ([]SchedulerRetryState, *SchedulerRetryCursor, error) {
	if repository == nil || repository.database == nil || homeGeneration < 0 || atMS < 0 {
		return nil, nil, ErrInvalidRepository
	}
	pageLimit, err := validateRuntimeLimit(limit)
	if err != nil {
		return nil, nil, err
	}
	if after != nil && (after.NextRetryAtMS < 0 || after.TaskID == "") {
		return nil, nil, ErrSchedulerRetryTransition
	}
	var states []SchedulerRetryState
	err = repository.database.View(ctx, func(ctx context.Context, connection storesqlite.ReadConn) error {
		query := connection.WithContext(ctx).Model(&schedulerRetryStateModel{}).
			Select("scheduler_retry_states.*").
			Joins("JOIN scheduler_tasks ON scheduler_tasks.task_id = scheduler_retry_states.task_id").
			Where("scheduler_retry_states.disposition = ? AND scheduler_retry_states.next_retry_at_ms <= ?", string(SchedulerRetryWaiting), atMS).
			Where("scheduler_tasks.home_generation = ? AND scheduler_tasks.state = ?", homeGeneration, string(SchedulerTaskFailed))
		if after != nil {
			query = query.Where(
				"scheduler_retry_states.next_retry_at_ms > ? OR (scheduler_retry_states.next_retry_at_ms = ? AND scheduler_retry_states.task_id > ?)",
				after.NextRetryAtMS, after.NextRetryAtMS, after.TaskID,
			)
		}
		var models []schedulerRetryStateModel
		if err := query.Order("scheduler_retry_states.next_retry_at_ms, scheduler_retry_states.task_id").
			Limit(pageLimit).Find(&models).Error; err != nil {
			return err
		}
		states = make([]SchedulerRetryState, len(models))
		for index, model := range models {
			value, err := schedulerRetryStateFromModel(model)
			if err != nil {
				return err
			}
			states[index] = value
		}
		return nil
	})
	if err != nil || len(states) == 0 {
		return states, nil, err
	}
	last := states[len(states)-1]
	return states, &SchedulerRetryCursor{NextRetryAtMS: *last.NextRetryAtMS, TaskID: last.TaskID}, nil
}

func schedulerRetryStateIn(
	ctx context.Context,
	database *gorm.DB,
	taskID string,
) (SchedulerRetryState, bool, error) {
	var model schedulerRetryStateModel
	result := database.WithContext(ctx).Where("task_id = ?", taskID).Take(&model)
	if errors.Is(result.Error, gorm.ErrRecordNotFound) {
		return SchedulerRetryState{}, false, nil
	}
	if result.Error != nil {
		return SchedulerRetryState{}, false, result.Error
	}
	value, err := schedulerRetryStateFromModel(model)
	return value, err == nil, err
}

func schedulerRetryStateFromModel(model schedulerRetryStateModel) (SchedulerRetryState, error) {
	value := SchedulerRetryState{
		TaskID: model.TaskID, Disposition: SchedulerRetryDisposition(model.Disposition),
		FailureCount: model.FailureCount, LastErrorClass: RuntimeErrorClass(model.LastErrorClass),
		NextRetryAtMS: model.NextRetryAtMS, RecoveryAction: SchedulerRecoveryAction(model.RecoveryAction),
		Revision: model.Revision, UpdatedAtMS: model.UpdatedAtMS,
	}
	if err := validateSchedulerRetryState(value); err != nil {
		return SchedulerRetryState{}, err
	}
	return value, nil
}

func schedulerRetryStateModelFromDomain(value SchedulerRetryState) schedulerRetryStateModel {
	return schedulerRetryStateModel{
		TaskID: value.TaskID, Disposition: string(value.Disposition), FailureCount: value.FailureCount,
		LastErrorClass: string(value.LastErrorClass), NextRetryAtMS: value.NextRetryAtMS,
		RecoveryAction: string(value.RecoveryAction), Revision: value.Revision, UpdatedAtMS: value.UpdatedAtMS,
	}
}

func validateSchedulerRetryState(value SchedulerRetryState) error {
	if value.TaskID == "" || value.FailureCount < 1 || !validRuntimeErrorClass(value.LastErrorClass) ||
		!validSchedulerRetryDisposition(value.Disposition) ||
		!validSchedulerRecoveryAction(value.RecoveryAction) || value.Revision < 1 || value.UpdatedAtMS < 0 ||
		value.UpdatedAtMS > MaxSchedulerTimestampMS ||
		(value.NextRetryAtMS != nil && (*value.NextRetryAtMS < 0 ||
			*value.NextRetryAtMS > MaxSchedulerRetryDueTimestampMS)) {
		return ErrSchedulerRetryTransition
	}
	switch value.Disposition {
	case SchedulerRetryWaiting:
		if value.NextRetryAtMS == nil || *value.NextRetryAtMS <= value.UpdatedAtMS ||
			value.RecoveryAction != SchedulerRecoveryNone {
			return ErrSchedulerRetryTransition
		}
	case SchedulerRetryBlocked:
		if value.NextRetryAtMS != nil || value.RecoveryAction == SchedulerRecoveryNone {
			return ErrSchedulerRetryTransition
		}
	case SchedulerRetryResolved:
		if value.NextRetryAtMS != nil || value.RecoveryAction != SchedulerRecoveryNone {
			return ErrSchedulerRetryTransition
		}
	}
	return nil
}
