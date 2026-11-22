package schema_repo

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/SisyphusSQ/codex-pulse/server/docs/sqls/schema"
	schema_do "github.com/SisyphusSQ/codex-pulse/server/internal/models/do/mysql/schema_do"
)

// Upgrade 只接受已登记结构，读回目标字段后迁移派生投影并提交版本标记。
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
	v5 := map[string]string{"mysql": "98d2c4f152659193b3233a790ab9971b69140bd0cc664b3e2ba64e9f89aabddd", "sqlite": "1f476adc4fb3cc1d2f6318073589b48042decd39beb8359710db3f38186f8784"}
	// 既有 Windows 构建的 v6 CRLF 摘要已在开发库确认；仅接受这一个已知等价定义。
	v6 := map[string]string{"mysql": "695cb3cbd3fbdfd19f809dfce688e6c0f5352b4bd96e9adeb7945c2f88f90e0c", "sqlite": "65d89e7629f95edc6d80967b2f9839b80a792da91b166720fd247567f2c8f096"}
	v7 := map[string]string{"mysql": "ec2e64d187cb542d1f9c0a7c45212bf9749c3b7b0c13597e6baab2474ba1aca8", "sqlite": "8a862017f2fc8963bce363e6c4f261d155a66671111affb95e412d2864165f7b"}
	v8 := map[string]string{"mysql": "abee32ac651477051dd6354bfcae89075e8a091057965c4a70f6f460fdc61a8f", "sqlite": "f2ed58099b1ebca810f87244ece40540c9eb04895fa892863d8ec6f43a5f3bdd"}
	v9 := map[string]string{"mysql": "05979cf59bcaf6c905e1d20b83e7b438163fa1b5d4b3696218477f374e377aab", "sqlite": "a79125d7ad241f8b4a09a73d65a5c897e50155c4f6f7f9ec9b7c484a14a59a4a"}
	prior := map[int64]map[string]string{1: legacy, 2: v2, 3: v3, 4: v4, 5: v5, 6: v6, 7: v7, 8: v8, 9: v9}
	driver := db.Dialector.Name()
	expected := prior[marker.Version][driver]
	knownV6CRLF := marker.Version == 6 && driver == "mysql" && marker.Checksum == "7f4c7f80de583a8e268c527cf5d3e6404b25edbe723345838607097bf17812d7"
	if (expected == "" || marker.Checksum != expected) && !knownV6CRLF {
		return ErrSchemaIncompatible
	}
	statements, checksum, _, err := schema.Definition(db.Dialector.Name())
	if err != nil {
		return err
	}
	prepare := func(ctx context.Context) error {
		for _, ddl := range statements {
			if strings.HasPrefix(ddl, "CREATE TABLE IF NOT EXISTS pulse_machine_usage_sources ") {
				if err := s.engine.DB(ctx).Exec(ddl).Error; err != nil {
					return fmt.Errorf("create machine usage sources: %w", err)
				}
				continue
			}
			if marker.Version >= 8 || strings.HasPrefix(ddl, "CREATE INDEX idx_") {
				continue
			}
			if strings.Contains(ddl, "pulse_paid_credits ") || strings.Contains(ddl, "idx_paid_credits_") || strings.Contains(ddl, "idx_publication_jobs_ready ") || strings.Contains(ddl, "idx_payload_parts_age ") || strings.Contains(ddl, "pulse_payload_owners ") || strings.Contains(ddl, "idx_payload_owners_key ") || strings.Contains(ddl, "idx_usage_versions_time ") || strings.Contains(ddl, "pulse_publication_jobs ") || strings.Contains(ddl, "pulse_session_publications ") || strings.Contains(ddl, "pulse_payload_parts ") || strings.Contains(ddl, "pulse_usage_versions ") || strings.HasPrefix(ddl, "CREATE TABLE IF NOT EXISTS pulse_account_token_") || strings.HasPrefix(ddl, "CREATE INDEX IF NOT EXISTS idx_account_tokens_") || strings.HasPrefix(ddl, "CREATE TABLE IF NOT EXISTS pulse_account_settings ") || strings.HasPrefix(ddl, "CREATE TABLE IF NOT EXISTS pulse_snapshot_chunks ") || strings.HasPrefix(ddl, "CREATE TABLE IF NOT EXISTS pulse_device_sync ") || strings.HasPrefix(ddl, "CREATE TABLE IF NOT EXISTS pulse_retired_observations ") || strings.HasPrefix(ddl, "CREATE TABLE IF NOT EXISTS pulse_session_capsules ") || strings.HasPrefix(ddl, "CREATE INDEX IF NOT EXISTS idx_capsules_session ") {
				if err := s.engine.DB(ctx).Exec(ddl).Error; err != nil {
					return fmt.Errorf("upgrade center structure: %w", err)
				}
			}
		}
		if err := s.queryIndexes(ctx, marker.Version); err != nil {
			return err
		}
		return s.checkColumns(ctx)
	}
	cutover := func(ctx context.Context) error {
		db := s.engine.DB(ctx)
		if marker.Version < 9 {
			for _, statement := range schema.UsageCutoverStatements() {
				if err := db.Exec(statement).Error; err != nil {
					return fmt.Errorf("migrate published usage: %w", err)
				}
			}
		}
		if err := s.backfillMachineUsage(ctx); err != nil {
			return fmt.Errorf("backfill machine usage sources: %w", err)
		}
		result := db.Model(&schema_do.SchemaVersion{}).Where("id = ? AND version = ? AND checksum = ?", 1, marker.Version, marker.Checksum).Updates(map[string]any{"version": schema.Version, "checksum": checksum, "initialized_at_ms": time.Now().UnixMilli()})
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected != 1 {
			return ErrSchemaIncompatible
		}
		return nil
	}
	if driver == "sqlite" {
		return s.engine.Transaction(ctx, func(ctx context.Context) error {
			if err := prepare(ctx); err != nil {
				return err
			}
			return cutover(ctx)
		})
	}
	if err := prepare(ctx); err != nil {
		return err
	}
	return s.engine.Transaction(ctx, cutover)
}
