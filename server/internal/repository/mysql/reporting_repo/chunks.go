package reporting_repo

import (
	"context"

	reporting_do "github.com/SisyphusSQ/codex-pulse/server/internal/models/do/mysql/reporting_do"
)

func (r *Reporting) Chunks(ctx context.Context, client, source string, revision int64) (rows []reporting_do.SnapshotChunk, err error) {
	err = r.engine.DB(ctx).Where("client_id = ? AND source_key = ? AND revision = ?", client, source, revision).Order("chunk_index").Limit(257).Find(&rows).Error
	return
}

// ChunkMetadata 不重复读取旧片段正文；完整正文只在收齐后加载一次。
func (r *Reporting) ChunkMetadata(ctx context.Context, client, source string, revision int64) (rows []reporting_do.SnapshotChunk, err error) {
	err = r.engine.DB(ctx).Select("chunk_index,payload_bytes").Where("client_id = ? AND source_key = ? AND revision = ?", client, source, revision).Order("chunk_index").Limit(257).Find(&rows).Error
	return
}
func (r *Reporting) Chunk(ctx context.Context, client, source string, revision int64, index int) (row reporting_do.SnapshotChunk, err error) {
	err = r.engine.DB(ctx).Where("client_id = ? AND source_key = ? AND revision = ? AND chunk_index = ?", client, source, revision, index).Take(&row).Error
	return
}
func (r *Reporting) ChunkBudget(ctx context.Context, client string) (count, size int64, err error) {
	var budget struct{ Count, Size int64 }
	err = r.engine.DB(ctx).Model(&reporting_do.SnapshotChunk{}).Select("COUNT(*) AS count, COALESCE(SUM(payload_bytes),0) AS size").Where("client_id = ?", client).Scan(&budget).Error
	return budget.Count, budget.Size, err
}
func (r *Reporting) SaveChunk(ctx context.Context, row reporting_do.SnapshotChunk) error {
	return r.engine.DB(ctx).Create(&row).Error
}
func (r *Reporting) DeleteChunks(ctx context.Context, client, source string, through int64) error {
	return r.engine.DB(ctx).Where("client_id = ? AND source_key = ? AND revision <= ?", client, source, through).Delete(&reporting_do.SnapshotChunk{}).Error
}
func (r *Reporting) SaveSync(ctx context.Context, row reporting_do.DeviceSync) error {
	var previous reporting_do.DeviceSync
	if err := r.engine.DB(ctx).Where("client_id = ? AND provider = ? AND sync_checked_at_ms > ?", row.ClientID, row.Provider, row.SyncCheckedAtMS).Find(&previous).Error; err != nil {
		return err
	}
	if previous.ClientID != "" {
		return nil
	}
	return r.engine.DB(ctx).Save(&row).Error
}
