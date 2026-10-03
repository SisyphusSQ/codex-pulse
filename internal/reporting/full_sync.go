package reporting

import (
	"context"
	"time"

	"gorm.io/gorm"
)

type fullSync struct {
	Partition           string `gorm:"primaryKey"`
	State               string
	StartedAtMS         int64
	ExportedSessions    int64
	AcknowledgedBatches int64
}

func (fullSync) TableName() string { return "reporting_full_sync" }

// FullSync 强制重读当前范围的已索引事实，保留队列、来源 ID 与单调 revision。
func (r *Runtime) FullSync(ctx context.Context) (Status, error) {
	if r == nil || r.state == nil {
		return Status{}, ErrUnavailable
	}
	r.operations.Lock()
	defer r.operations.Unlock()
	if r.ctx.Err() != nil {
		return Status{}, ErrUnavailable
	}
	status, err := r.state.Status(ctx)
	if err != nil {
		return Status{}, ErrUnavailable
	}
	if !status.Enabled {
		return status, ErrSettings
	}
	if status.State == "reconnect_required" {
		return status, ErrReconnect
	}
	if status.State == "protocol_rejected" {
		return status, ErrProtocol
	}
	if status.FullSyncState == "running" {
		return status, nil
	}
	partition := credentialSettings{Endpoint: status.Endpoint, ClientID: status.ClientID}.partition()
	if err := r.state.startFullSync(ctx, partition); err != nil {
		return Status{}, ErrUnavailable
	}
	r.signal()
	return r.state.Status(ctx)
}
func (s *State) startFullSync(ctx context.Context, partition string) error {
	return s.db.Write(ctx, func(_ context.Context, tx *gorm.DB) error {
		if err := tx.Model(&checkpoint{}).Where("partition = ?", partition).Update("digest", "").Error; err != nil {
			return err
		}
		if err := tx.Where("partition = ?", partition).Delete(&factsCheckpoint{}).Error; err != nil {
			return err
		}
		if err := tx.Model(&exportCursor{}).Where("partition = ?", partition).Updates(map[string]any{"after": "", "authority": true, "next_due_at_ms": 0}).Error; err != nil {
			return err
		}
		return tx.Save(&fullSync{Partition: partition, State: "running", StartedAtMS: time.Now().UnixMilli()}).Error
	})
}
func (s *State) finishFullSync(ctx context.Context, partition string) error {
	return s.db.Write(ctx, func(_ context.Context, tx *gorm.DB) error {
		var pending int64
		if err := tx.Model(&queued{}).Where("partition = ?", partition).Count(&pending).Error; err != nil {
			return err
		}
		if pending > 0 {
			return nil
		}
		result := tx.Model(&fullSync{}).Where("partition = ? AND state = ?", partition, "running").Update("state", "completed")
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected == 0 {
			return nil
		}
		return tx.Model(&exportCursor{}).Where("partition = ? AND provider = ?", partition, "status").Update("next_due_at_ms", 0).Error
	})
}
