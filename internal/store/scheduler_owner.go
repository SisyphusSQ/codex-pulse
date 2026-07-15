package store

import (
	"context"
	"errors"

	storesqlite "github.com/SisyphusSQ/codex-pulse/internal/store/sqlite"
)

var ErrSchedulerOwnerBusy = errors.New("scheduler owner lease is busy")

// SchedulerOwnerLease 证明当前进程是数据库对应 scheduler 的唯一 heavy-work owner。
type SchedulerOwnerLease interface {
	Release()
}

// AcquireSchedulerOwner 可取消地等待 owner lease，供长生命周期 worker 与恢复流程使用。
func (repository *Repository) AcquireSchedulerOwner(ctx context.Context) (SchedulerOwnerLease, error) {
	if repository == nil || repository.database == nil {
		return nil, ErrInvalidRepository
	}
	return repository.database.AcquireSchedulerOwnerLease(ctx)
}

// TryAcquireSchedulerOwner 只尝试一次，供独立 RunCycle 避免阻塞调用方。
func (repository *Repository) TryAcquireSchedulerOwner(ctx context.Context) (SchedulerOwnerLease, error) {
	if repository == nil || repository.database == nil {
		return nil, ErrInvalidRepository
	}
	lease, err := repository.database.TryAcquireSchedulerOwnerLease(ctx)
	if errors.Is(err, storesqlite.ErrOwnerLeaseBusy) {
		return nil, errors.Join(ErrSchedulerOwnerBusy, err)
	}
	return lease, err
}
