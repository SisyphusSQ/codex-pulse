package schema_repo

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"errors"
	"fmt"
	"math"
	"time"

	"gorm.io/gorm"
)

// Migrate 自动初始化空库或执行已登记升级；当前结构只检查，未知版本和漂移拒绝启动。
func (s *Schema) Migrate(ctx context.Context) error {
	return s.withMigrationLock(ctx, func(ctx context.Context) error {
		err := s.Check(ctx)
		switch {
		case err == nil:
			return nil
		case errors.Is(err, ErrSchemaMissing):
			return s.init(ctx)
		case errors.Is(err, ErrSchemaIncompatible):
			return s.upgrade(ctx)
		default:
			return err
		}
	})
}

func (s *Schema) withMigrationLock(ctx context.Context, migrate func(context.Context) error) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if s.engine.DB(ctx).Dialector.Name() != "mysql" {
		return migrate(ctx)
	}
	return s.engine.Connection(ctx, func(ctx context.Context) error {
		return mysqlMigrationLock(ctx, s.engine.DB(ctx), func() error { return migrate(ctx) })
	})
}

// mysqlMigrationLock 与 DDL 持有同一连接；库名只进入锁名摘要，不进入日志。
func mysqlMigrationLock(ctx context.Context, db *gorm.DB, migrate func() error) (err error) {
	var database string
	if err := db.Raw("SELECT DATABASE()").Scan(&database).Error; err != nil {
		return fmt.Errorf("read migration database: %w", err)
	}
	if database == "" {
		return errors.New("migration requires a selected database")
	}
	sum := sha256.Sum256([]byte(database))
	name := fmt.Sprintf("codex-pulse-schema:%x", sum[:16])
	waitSeconds := int64(30)
	if deadline, ok := ctx.Deadline(); ok {
		waitSeconds = int64(math.Ceil(time.Until(deadline).Seconds()))
		if waitSeconds <= 0 {
			return context.DeadlineExceeded
		}
	}
	var acquired sql.NullInt64
	if err := db.Raw("SELECT GET_LOCK(?, ?)", name, waitSeconds).Scan(&acquired).Error; err != nil {
		return fmt.Errorf("acquire schema migration lock: %w", err)
	}
	if !acquired.Valid || acquired.Int64 != 1 {
		return errors.New("schema migration lock was not acquired")
	}
	defer func() {
		// 即使迁移取消也必须释放连接级锁；清理有独立的短截止时间。
		cleanup, cancel := context.WithTimeout(context.WithoutCancel(ctx), 3*time.Second)
		defer cancel()
		var released sql.NullInt64
		releaseErr := db.WithContext(cleanup).Raw("SELECT RELEASE_LOCK(?)", name).Scan(&released).Error
		if releaseErr == nil && (!released.Valid || released.Int64 != 1) {
			releaseErr = errors.New("schema migration lock was not released")
		}
		if releaseErr != nil {
			err = errors.Join(err, fmt.Errorf("release schema migration lock: %w", releaseErr))
		}
	}()
	return migrate()
}
