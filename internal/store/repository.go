package store

import (
	"context"

	storesqlite "github.com/SisyphusSQ/codex-pulse/internal/store/sqlite"
)

// Repository 通过应用唯一 SQLite Store 持久化和查询结构化事实。
type Repository struct {
	database *storesqlite.Store
}

// NewRepository 使用已有 Store 构造事实仓储，不取得连接生命周期所有权。
func NewRepository(database *storesqlite.Store) *Repository {
	return &Repository{database: database}
}

// UpsertFacts 校验并在一个 queue-owned transaction 中按外键依赖顺序写入事实。
func (repository *Repository) UpsertFacts(ctx context.Context, batch FactBatch) error {
	if err := repository.validateBatch(batch); err != nil {
		return err
	}
	expectedSessionID, _ := batchSessionID(batch)
	return repository.database.Write(ctx, func(ctx context.Context, transaction storesqlite.WriteTx) error {
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
		return nil
	})
}
