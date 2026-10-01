package schema_repo

import (
	"context"
	"fmt"
	"time"

	access_do "github.com/SisyphusSQ/codex-pulse/server/internal/models/do/mysql/access_do"
)

// Snapshot 使用 SQLite 的一致性备份，不复制活跃 WAL 文件。
func (s *Schema) Snapshot(ctx context.Context, destination string) error {
	if s.engine.DB(ctx).Dialector.Name() != "sqlite" {
		return fmt.Errorf("use the documented MySQL backup entry")
	}
	if err := s.Check(ctx); err != nil {
		return err
	}
	return s.engine.DB(ctx).Exec("VACUUM INTO ?", destination).Error
}
func (s *Schema) Integrity(ctx context.Context) error {
	if err := s.Check(ctx); err != nil {
		return err
	}
	var result string
	if err := s.engine.DB(ctx).Raw("PRAGMA integrity_check").Scan(&result).Error; err != nil {
		return err
	}
	if result != "ok" {
		return fmt.Errorf("SQLite integrity check failed")
	}
	return nil
}

// InvalidateRestoredAccess 防止旧备份复活后来撤销的授权；事实和 receipts 原样保留。
func (s *Schema) InvalidateRestoredAccess(ctx context.Context) error {
	return s.engine.Transaction(ctx, func(ctx context.Context) error {
		now := time.Now().UnixMilli()
		if err := s.engine.DB(ctx).Model(&access_do.Client{}).Where("revoked_at_ms IS NULL").Update("revoked_at_ms", now).Error; err != nil {
			return err
		}
		return s.engine.DB(ctx).Model(&access_do.Pairing{}).Where("consumed_at_ms IS NULL").Update("consumed_at_ms", now).Error
	})
}
func (s *Schema) Checkpoint(ctx context.Context) error {
	return s.engine.DB(ctx).Exec("PRAGMA wal_checkpoint(TRUNCATE)").Error
}
