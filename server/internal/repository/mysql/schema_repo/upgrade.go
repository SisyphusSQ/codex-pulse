package schema_repo

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/SisyphusSQ/codex-pulse/server/docs/sqls/schema"
	schema_do "github.com/SisyphusSQ/codex-pulse/server/internal/models/do/mysql/schema_do"
)

// Upgrade 只接受已确认 v1/v2/v3/v4 定义，新增已登记结构后读回字段，最后提交版本标记。
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
	v2 := map[string]string{"mysql": "be7938a996a860030d7484aff0a443fc42672e02d0fe35e004efc80ca289899e", "sqlite": "de4c490365d9ba42a38041a5b6b3f7be4f92e5b2c275dc9b6a51ba0c234862ec"}
	v3 := map[string]string{"mysql": "c90a79f74c9d2f31987233f7cb335b3a11e74f01858ac9bf64e3e4ef76cb6e0b", "sqlite": "a796cb171ecdf6edbf40fccc9543c1ed2b0012d95248dcdf9da472b3eaf51565"}
	v4 := map[string]string{"mysql": "c4f4d1e7418f259444a44eadfd9416f41033e6351c94f2db82b37ebf6f7e8450", "sqlite": "ade85380eeb8f7c01b25432092d582d170e6462e3e4f0739f134e6247dae1b9a"}
	if schema.Version != 5 || !((marker.Version == 4 && marker.Checksum == v4[db.Dialector.Name()]) || (marker.Version == 3 && marker.Checksum == v3[db.Dialector.Name()]) || (marker.Version == 1 && marker.Checksum == legacy[db.Dialector.Name()]) || (marker.Version == 2 && marker.Checksum == v2[db.Dialector.Name()])) {
		return ErrSchemaIncompatible
	}
	statements, checksum, _, err := schema.Definition(db.Dialector.Name())
	if err != nil {
		return err
	}
	upgrade := func(ctx context.Context) error {
		for _, ddl := range statements {
			if strings.HasPrefix(ddl, "CREATE TABLE IF NOT EXISTS pulse_account_token_") || strings.HasPrefix(ddl, "CREATE INDEX IF NOT EXISTS idx_account_tokens_") || strings.HasPrefix(ddl, "CREATE TABLE IF NOT EXISTS pulse_account_settings ") || strings.HasPrefix(ddl, "CREATE TABLE IF NOT EXISTS pulse_snapshot_chunks ") || strings.HasPrefix(ddl, "CREATE TABLE IF NOT EXISTS pulse_device_sync ") || strings.HasPrefix(ddl, "CREATE TABLE IF NOT EXISTS pulse_retired_observations ") || strings.HasPrefix(ddl, "CREATE TABLE IF NOT EXISTS pulse_session_capsules ") || strings.HasPrefix(ddl, "CREATE INDEX IF NOT EXISTS idx_capsules_session ") {
				if err := s.engine.DB(ctx).Exec(ddl).Error; err != nil {
					return fmt.Errorf("upgrade center structure: %w", err)
				}
			}
		}
		if err := s.checkColumns(ctx); err != nil {
			return err
		}
		result := s.engine.DB(ctx).Model(&schema_do.SchemaVersion{}).Where("id = ? AND version = ? AND checksum = ?", 1, marker.Version, marker.Checksum).Updates(map[string]any{"version": schema.Version, "checksum": checksum, "initialized_at_ms": time.Now().UnixMilli()})
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
