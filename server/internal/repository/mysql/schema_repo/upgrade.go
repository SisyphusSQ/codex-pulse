package schema_repo

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/SisyphusSQ/codex-pulse/server/docs/sqls/schema"
	schema_do "github.com/SisyphusSQ/codex-pulse/server/internal/models/do/mysql/schema_do"
)

// Upgrade 只接受已确认 v1 定义，增加设置表后读回全部字段，最后提交版本标记。
func (s *Schema) Upgrade(ctx context.Context) error {
	return s.withMigrationLock(ctx, s.upgrade)
}

func (s *Schema) upgrade(ctx context.Context) error {
	db := s.engine.DB(ctx)
	var marker schema_do.SchemaVersion
	if err := db.Where("id = ?", 1).Take(&marker).Error; err != nil {
		return fmt.Errorf("read upgrade marker: %w", err)
	}
	if marker.Version == schema.Version {
		return s.Check(ctx)
	}
	legacy := map[string]string{"mysql": "0acd2ecebfc06e0fb6b22a552055adbad95033e228eeb58d8148164c9cce9c7c", "sqlite": "e5e94cb539d004969cd009c78c12be71bbb3dcbd30e2a69dcb7b310b428e8568"}
	// 当前登记的是 v1→v2；未来提升目标版本时必须实现对应步骤，不能跳过迁移直接改标记。
	if marker.Version != 1 || schema.Version != 2 || marker.Checksum != legacy[db.Dialector.Name()] {
		return ErrSchemaIncompatible
	}
	statements, checksum, _, err := schema.Definition(db.Dialector.Name())
	if err != nil {
		return err
	}
	upgrade := func(ctx context.Context) error {
		for _, ddl := range statements {
			if strings.HasPrefix(ddl, "CREATE TABLE IF NOT EXISTS pulse_account_settings ") {
				if err := s.engine.DB(ctx).Exec(ddl).Error; err != nil {
					return fmt.Errorf("upgrade subscription structure: %w", err)
				}
			}
		}
		if err := s.checkColumns(ctx); err != nil {
			return err
		}
		result := s.engine.DB(ctx).Model(&schema_do.SchemaVersion{}).Where("id = ? AND version = ? AND checksum = ?", 1, 1, marker.Checksum).Updates(map[string]any{"version": schema.Version, "checksum": checksum, "initialized_at_ms": time.Now().UnixMilli()})
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected != 1 {
			return ErrSchemaIncompatible
		}
		return nil
	}
	if db.Dialector.Name() == "sqlite" {
		return s.engine.Transaction(ctx, upgrade)
	}
	return upgrade(ctx)
}
