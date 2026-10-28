package sqlite

import (
	"context"
	"fmt"
	"os"
	"sync"
	"time"

	"github.com/SisyphusSQ/codex-pulse/internal/platformfs"
)

const schedulerOwnerLockSuffix = ".scheduler-owner.lock"

// SchedulerOwnerLease 是由 OS 持有的进程级 scheduler owner 证明。
// 锁文件不会在 Release 时删除，避免不同 inode 被并发持有。
type SchedulerOwnerLease interface {
	Release()
}

type schedulerOwnerLease struct {
	file *os.File
	once sync.Once
}

// AcquireSchedulerOwnerLease 可取消地等待数据库对应的进程级 owner lease。
func (store *Store) AcquireSchedulerOwnerLease(ctx context.Context) (SchedulerOwnerLease, error) {
	return store.acquireSchedulerOwnerLease(ctx, true)
}

// TryAcquireSchedulerOwnerLease 只尝试一次；已有 owner 时返回 ErrOwnerLeaseBusy。
func (store *Store) TryAcquireSchedulerOwnerLease(ctx context.Context) (SchedulerOwnerLease, error) {
	return store.acquireSchedulerOwnerLease(ctx, false)
}

func (store *Store) acquireSchedulerOwnerLease(
	ctx context.Context,
	wait bool,
) (SchedulerOwnerLease, error) {
	if store == nil {
		return nil, newClassifiedError("acquire scheduler owner lease", ErrInvalidConfig, nil)
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	path := store.config.Path + schedulerOwnerLockSuffix
	file, err := platformfs.OpenFile(path, os.O_RDWR|os.O_CREATE, 0o600)
	if err != nil {
		return nil, newClassifiedError("open scheduler owner lease", ErrPermission, err)
	}
	closeWithError := func(value error) (SchedulerOwnerLease, error) {
		_ = file.Close()
		return nil, value
	}
	opened, err := file.Stat()
	if err != nil {
		return closeWithError(newClassifiedError("inspect scheduler owner lease", ErrIO, err))
	}
	linked, err := os.Lstat(path)
	if err != nil {
		return closeWithError(newClassifiedError("inspect scheduler owner lease path", ErrPermission, err))
	}
	if !opened.Mode().IsRegular() || !platformfs.PrivateMode(path, opened, 0o600) ||
		!linked.Mode().IsRegular() || !platformfs.PrivateMode(path, linked, 0o600) ||
		linked.Mode()&os.ModeSymlink != 0 || !os.SameFile(opened, linked) {
		return closeWithError(newClassifiedError(
			"validate scheduler owner lease", ErrPermission,
			fmt.Errorf("lock path must be one regular 0600 file"),
		))
	}

	for {
		locked, err := platformfs.TryLock(file)
		if locked {
			return &schedulerOwnerLease{file: file}, nil
		}
		if err != nil {
			return closeWithError(newClassifiedError("lock scheduler owner lease", ErrIO, err))
		}
		if !wait {
			return closeWithError(newClassifiedError("lock scheduler owner lease", ErrOwnerLeaseBusy, err))
		}
		timer := time.NewTimer(10 * time.Millisecond)
		select {
		case <-ctx.Done():
			if !timer.Stop() {
				<-timer.C
			}
			return closeWithError(ctx.Err())
		case <-timer.C:
		}
	}
}

func (lease *schedulerOwnerLease) Release() {
	if lease == nil {
		return
	}
	lease.once.Do(func() {
		if lease.file == nil {
			return
		}
		_ = platformfs.Unlock(lease.file)
		_ = lease.file.Close()
	})
}
