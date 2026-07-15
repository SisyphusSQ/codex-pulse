package store

import (
	"context"
	"errors"
	"sort"

	"gorm.io/gorm"
)

var ErrWriteUnitClosed = errors.New("write unit is no longer active")

// WriteUnit 是一个 queue-owned transaction 上的 typed operation 集合。
// 它不暴露 raw transaction、Commit 或 Rollback，且 callback 返回后立即失效。
type WriteUnit struct {
	repository  *Repository
	ctx         context.Context
	transaction *gorm.DB
	active      bool

	attributionSchemaChecked bool
	attributionSchemaPresent bool
	dirtyAttributionSessions map[string]struct{}
}

// WithinWriteUnit 在应用唯一 writer queue 中执行一个 typed 原子工作单元。
func (repository *Repository) WithinWriteUnit(ctx context.Context, write func(*WriteUnit) error) error {
	if repository == nil || repository.database == nil {
		return ErrInvalidRepository
	}
	if write == nil {
		return invalidRecord("write unit callback must not be nil")
	}
	return repository.database.Write(ctx, func(ctx context.Context, transaction *gorm.DB) error {
		unit := &WriteUnit{
			repository: repository, ctx: ctx, transaction: transaction.WithContext(ctx), active: true,
		}
		defer func() { unit.active = false }()
		if err := write(unit); err != nil {
			return err
		}
		return unit.flushAttributions()
	})
}

func (unit *WriteUnit) requireActive() error {
	if unit == nil || !unit.active || unit.repository == nil || unit.transaction == nil {
		return ErrWriteUnitClosed
	}
	return nil
}

// UpsertFacts 在当前工作单元中写入 core facts，不新开 transaction。
func (unit *WriteUnit) UpsertFacts(batch FactBatch) error {
	if err := unit.requireActive(); err != nil {
		return err
	}
	if err := unit.repository.validateBatch(batch); err != nil {
		return err
	}
	expectedSessionID, _ := batchSessionID(batch)
	ctx := unit.ctx
	transaction := unit.transaction
	if batch.Project != nil {
		if err := validateProjectReplay(ctx, transaction, *batch.Project); err != nil {
			return err
		}
		if err := upsertProject(ctx, transaction, *batch.Project); err != nil {
			return err
		}
	}
	if batch.Session != nil {
		if batch.Session.ProjectID != nil {
			if err := requireProject(ctx, transaction, *batch.Session.ProjectID); err != nil {
				return err
			}
		}
		if err := validateSessionIdentity(ctx, transaction, *batch.Session); err != nil {
			return err
		}
		if err := upsertSession(ctx, transaction, *batch.Session); err != nil {
			return err
		}
	}
	if batch.QuotaObservation != nil {
		if batch.QuotaObservation.SessionID != nil {
			if err := requireSession(ctx, transaction, *batch.QuotaObservation.SessionID); err != nil {
				return err
			}
		}
		if err := upsertQuotaObservation(ctx, transaction, *batch.QuotaObservation); err != nil {
			return err
		}
	}
	if batch.Turn != nil {
		if err := requireSession(ctx, transaction, batch.Turn.SessionID); err != nil {
			return err
		}
		if batch.Turn.ProjectID != nil {
			if err := requireProject(ctx, transaction, *batch.Turn.ProjectID); err != nil {
				return err
			}
		}
		if err := validateTurnIdentity(ctx, transaction, *batch.Turn); err != nil {
			return err
		}
		if err := upsertTurn(ctx, transaction, *batch.Turn); err != nil {
			return err
		}
	}
	if batch.Usage != nil {
		if err := requireTurn(ctx, transaction, batch.Usage.TurnID); err != nil {
			return err
		}
		if err := validateTurnUsageReplay(ctx, transaction, *batch.Usage, expectedSessionID); err != nil {
			return err
		}
		if err := upsertTurnUsage(ctx, transaction, *batch.Usage); err != nil {
			return err
		}
	}
	if batch.Turn != nil {
		if err := removeInvalidTurnUsage(ctx, transaction, batch.Turn.TurnID); err != nil {
			return err
		}
	} else if batch.Usage != nil {
		if err := removeInvalidTurnUsage(ctx, transaction, batch.Usage.TurnID); err != nil {
			return err
		}
	}
	if batch.SessionCurrent != nil {
		if err := requireSession(ctx, transaction, batch.SessionCurrent.SessionID); err != nil {
			return err
		}
		if err := validateActiveTurnReference(ctx, transaction, *batch.SessionCurrent); err != nil {
			return err
		}
		if err := validateSessionCurrentReplay(ctx, transaction, *batch.SessionCurrent); err != nil {
			return err
		}
		if err := upsertSessionCurrent(ctx, transaction, *batch.SessionCurrent); err != nil {
			return err
		}
	}
	if batch.SessionUsageCurrent != nil {
		if err := requireSession(ctx, transaction, batch.SessionUsageCurrent.SessionID); err != nil {
			return err
		}
		if err := validateSessionUsageReplay(ctx, transaction, *batch.SessionUsageCurrent); err != nil {
			return err
		}
		if err := upsertSessionUsageCurrent(ctx, transaction, *batch.SessionUsageCurrent); err != nil {
			return err
		}
	}
	if expectedSessionID != "" {
		unit.markAttributionDirty(expectedSessionID)
	}
	return nil
}

func (unit *WriteUnit) markAttributionDirty(sessionID string) {
	if !unit.attributionSchemaChecked {
		unit.attributionSchemaPresent = unit.transaction.Migrator().HasTable(&sessionAttributionModel{})
		unit.attributionSchemaChecked = true
	}
	if !unit.attributionSchemaPresent {
		return
	}
	if unit.dirtyAttributionSessions == nil {
		unit.dirtyAttributionSessions = make(map[string]struct{})
	}
	unit.dirtyAttributionSessions[sessionID] = struct{}{}
}

func (unit *WriteUnit) flushAttributions() error {
	if len(unit.dirtyAttributionSessions) == 0 {
		return nil
	}
	sessionIDs := make([]string, 0, len(unit.dirtyAttributionSessions))
	for sessionID := range unit.dirtyAttributionSessions {
		sessionIDs = append(sessionIDs, sessionID)
	}
	sort.Strings(sessionIDs)
	writer := attributionWriter{ctx: unit.ctx, transaction: unit.transaction}
	for _, sessionID := range sessionIDs {
		if _, err := writer.refreshSessionAttributions(sessionID, nil); err != nil {
			return err
		}
	}
	return nil
}

// UpsertSourceFile 在当前工作单元中推进 source cursor，不新开 transaction。
func (unit *WriteUnit) UpsertSourceFile(file SourceFile) error {
	if err := unit.requireActive(); err != nil {
		return err
	}
	if err := validateSourceFile(file); err != nil {
		return err
	}
	ctx := unit.ctx
	transaction := unit.transaction
	if file.SessionID != nil {
		if err := requireSession(ctx, transaction, *file.SessionID); err != nil {
			return err
		}
	}
	existing, found, err := sourceFileByID(ctx, transaction, file.SourceFileID)
	if err != nil {
		return err
	}
	if found {
		if err := validateSourceFileProgression(existing, file); err != nil {
			return err
		}
		if sourceFilesEqual(existing, file) {
			return nil
		}
	} else {
		var count int64
		err := transaction.WithContext(ctx).Model(&sourceFileModel{}).
			Where("provider = ? AND device_id = ? AND inode = ?", file.Provider, file.DeviceID, file.Inode).
			Count(&count).Error
		if err != nil {
			return err
		}
		if count > 0 {
			return invalidRecord("source physical identity belongs to another source file")
		}
	}
	model := sourceFileModelFromDomain(file)
	if !found {
		return transaction.WithContext(ctx).Create(&model).Error
	}
	return transaction.WithContext(ctx).Model(&sourceFileModel{}).
		Where("source_file_id = ?", file.SourceFileID).Updates(sourceFileUpdates(model)).Error
}

// TransitionJobRun 在当前工作单元中推进 job/progress/cursor，不新开 transaction。
func (unit *WriteUnit) TransitionJobRun(transition JobTransition) error {
	if err := unit.requireActive(); err != nil {
		return err
	}
	if err := validateJobTransition(transition); err != nil {
		return err
	}
	existing, found, err := jobRunByID(unit.ctx, unit.transaction, transition.JobID)
	if err != nil {
		return err
	}
	if !found {
		return invalidRecord("job run does not exist")
	}
	projected, err := projectJobTransition(existing, transition)
	if err != nil {
		return err
	}
	if jobRunsEqual(existing, projected) {
		return nil
	}
	return updateJobRun(unit.ctx, unit.transaction, projected)
}
