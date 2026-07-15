package store

import (
	"context"
	"time"

	storesqlite "github.com/SisyphusSQ/codex-pulse/internal/store/sqlite"
)

// Repository 通过应用唯一 SQLite Store 持久化和查询结构化事实。
type Repository struct {
	database                   *storesqlite.Store
	schedulerQueueSnapshotHook func(SchedulerLane) error
	quotaProjectionHook        func(string) error
	quotaProjectionReadHook    func(string) error
	quotaNow                   func() time.Time
}

// NewRepository 使用已有 Store 构造事实仓储，不取得连接生命周期所有权。
func NewRepository(database *storesqlite.Store) *Repository {
	return &Repository{database: database, quotaNow: time.Now}
}

func (repository *Repository) quotaEvaluationTimeMS() (int64, error) {
	now := time.Now
	if repository != nil && repository.quotaNow != nil {
		now = repository.quotaNow
	}
	evaluatedAtMS := now().UnixMilli()
	if evaluatedAtMS < 0 {
		return 0, invalidRecord("quota projection trusted clock is invalid")
	}
	return evaluatedAtMS, nil
}

// UpsertFacts 校验并在一个 queue-owned transaction 中按外键依赖顺序写入事实。
func (repository *Repository) UpsertFacts(ctx context.Context, batch FactBatch) error {
	return repository.WithinWriteUnit(ctx, func(unit *WriteUnit) error {
		return unit.UpsertFacts(batch)
	})
}
