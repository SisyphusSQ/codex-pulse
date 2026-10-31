package lightindex

import (
	"context"

	"gorm.io/gorm"
)

// SetLightScanState 保留有效事实，标记来源失败或恢复；complete 必须有完整 active 代次。
func (r *Repository) SetLightScanState(ctx context.Context, home LightHomeIdentity, session LightSessionMetadata, state string, at int64) (bool, error) {
	if state != "deferred" && state != "failed" && state != "complete" {
		return false, invalidRecord("invalid scan failure state")
	}
	changed := false
	err := r.database.Write(ctx, func(ctx context.Context, db *gorm.DB) error {
		if err := checkScanHome(ctx, db, home); err != nil {
			return err
		}
		query := db.WithContext(ctx).Model(&lightSessionModel{}).Where("session_id = ? AND metadata_generation = ? AND scan_state <> ?", session.SessionID, session.MetadataGeneration, state)
		if session.RolloutPath == nil {
			query = query.Where("rollout_path IS NULL")
		} else {
			query = query.Where("rollout_path = ?", *session.RolloutPath)
		}
		if state == "complete" {
			query = query.Where("EXISTS (SELECT 1 FROM light_token_scans scan WHERE scan.session_id=light_sessions.session_id AND scan.generation=light_sessions.active_token_generation AND scan.complete=1 AND scan.state='active')")
		}
		result := query.Updates(map[string]any{"scan_state": state, "row_updated_at_ms": at})
		changed = result.RowsAffected > 0
		return result.Error
	})
	return changed, err
}

// FinishLightScanPass 在实际扫描轮次结束时按当前 Session 覆盖收敛状态。
func (r *Repository) FinishLightScanPass(ctx context.Context, home LightHomeIdentity, at int64) (bool, error) {
	changed := false
	err := r.database.Write(ctx, func(ctx context.Context, db *gorm.DB) error {
		if err := checkScanHome(ctx, db, home); err != nil {
			return err
		}
		var incomplete int64
		if err := db.WithContext(ctx).Model(&lightSessionModel{}).Where("scan_state <> 'complete'").Count(&incomplete).Error; err != nil {
			return err
		}
		state := "complete"
		if incomplete != 0 {
			state = "partial"
		}
		result := db.WithContext(ctx).Model(&lightIndexStateModel{}).Where("state_id = 1 AND token_scan_state <> ?", state).
			Updates(map[string]any{"token_scan_state": state, "token_scan_finished_at_ms": at, "updated_at_ms": at})
		changed = result.RowsAffected > 0
		return result.Error
	})
	return changed, err
}

func checkScanHome(ctx context.Context, db *gorm.DB, home LightHomeIdentity) error {
	var state lightIndexStateModel
	if err := db.WithContext(ctx).Where("state_id = 1").Take(&state).Error; err != nil {
		return err
	}
	if state.HomePath != home.Path || state.HomeDeviceID != home.DeviceID || state.HomeInode != home.Inode {
		return ErrLightHomeFence
	}
	return nil
}
