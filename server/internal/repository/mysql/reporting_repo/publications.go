package reporting_repo

import (
	"context"
	"encoding/json/v2"
	"errors"
	"time"
	"uuid"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	reporting_do "github.com/SisyphusSQ/codex-pulse/server/internal/models/do/mysql/reporting_do"
)

func (r *Reporting) ReadSnapshot(ctx context.Context, fn func(context.Context) error) error {
	return r.engine.ReadSnapshot(ctx, fn)
}

// ReadSources 不加写锁，供后台构建的一致只读快照使用。
func (r *Reporting) ReadSources(ctx context.Context, key string) (rows []reporting_do.SessionSource, err error) {
	if err = r.engine.DB(ctx).Where("session_key = ?", key).Order("id").Limit(129).Find(&rows).Error; err != nil {
		return
	}
	for i := range rows {
		rows[i].Payload, err = ResolvePayload(ctx, r.engine.DB(ctx), rows[i].Payload)
		if err != nil {
			return
		}
	}
	return
}
func (r *Reporting) Session(ctx context.Context, key string) (row reporting_do.Session, err error) {
	err = r.engine.DB(ctx).Where("id = ?", key).Take(&row).Error
	return
}

func (r *Reporting) SourceMetadata(ctx context.Context, id string) (row reporting_do.SessionSource, err error) {
	err = r.engine.DB(ctx).Select("id,revision,digest").Where("id = ?", id).Take(&row).Error
	return
}

// HasLargeProjection 同样让小型纠正/部分副本避开已有大投影的同步重写。
func (r *Reporting) HasLargeProjection(ctx context.Context, key string) (bool, error) {
	var n int64
	err := r.engine.DB(ctx).Model(&reporting_do.CanonicalSnapshot{}).Where("session_key = ?", key).Where("LENGTH(payload) >= ? OR EXISTS (SELECT 1 FROM pulse_session_publications p WHERE p.session_key = ?)", 256<<10, key).Count(&n).Error
	return n > 0, err
}
func (r *Reporting) Job(ctx context.Context, id string) (row reporting_do.PublicationJob, err error) {
	err = r.engine.DB(ctx).Where("id = ?", id).Take(&row).Error
	return
}
func (r *Reporting) SaveJob(ctx context.Context, row reporting_do.PublicationJob) error {
	return r.engine.DB(ctx).Save(&row).Error
}

// ClaimPublication 用有期限且受最终提交核对的租约避免多进程重复发布。
func (r *Reporting) ClaimPublication(ctx context.Context, now int64) (job reporting_do.PublicationJob, err error) {
	err = r.Transaction(ctx, func(ctx context.Context) error {
		db := r.engine.DB(ctx)
		if err := db.Clauses(clause.Locking{Strength: "UPDATE"}).Where("state = ? AND next_attempt_at_ms <= ? AND lease_until_ms <= ?", "pending", now, now).Order("next_attempt_at_ms,created_at_ms,id").Take(&job).Error; err != nil {
			return err
		}
		job.LeaseToken = uuid.New().String()
		job.LeaseUntilMS = now + 5*time.Minute.Milliseconds()
		return db.Save(&job).Error
	})
	return
}
func (r *Reporting) FinishJob(ctx context.Context, job reporting_do.PublicationJob, state, reason string, next int64) error {
	result := r.engine.DB(ctx).Model(&reporting_do.PublicationJob{}).Where("id = ? AND lease_token = ?", job.ID, job.LeaseToken).Updates(map[string]any{"state": state, "failure_code": reason, "next_attempt_at_ms": next, "lease_token": "", "lease_until_ms": 0})
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected != 1 {
		return gorm.ErrRecordNotFound
	}
	return nil
}

// SourcesStamp 不读大正文，最终短事务用它确认构建期间没有其他来源修订。
func (r *Reporting) SourcesStamp(ctx context.Context, key string) (string, error) {
	var rows []reporting_do.SessionSource
	if err := r.engine.DB(ctx).Select("id,revision,digest").Where("session_key = ?", key).Order("id").Limit(129).Find(&rows).Error; err != nil {
		return "", err
	}
	b, err := json.Marshal(rows)
	return string(b), err
}
func (r *Reporting) StageUsage(ctx context.Context, generation string, usage []reporting_do.Usage) error {
	for start := 0; start < len(usage); start += 250 {
		rows := make([]reporting_do.UsageVersion, 0, min(250, len(usage)-start))
		for _, row := range usage[start:min(start+250, len(usage))] {
			rows = append(rows, reporting_do.UsageVersion{Generation: generation, Usage: row})
		}
		c, cancel := context.WithTimeout(ctx, 5*time.Second)
		err := r.engine.DB(c).Clauses(clause.OnConflict{DoNothing: true}).Create(&rows).Error
		cancel()
		if err != nil {
			return err
		}
	}
	return nil
}
func (r *Reporting) Publish(ctx context.Context, session reporting_do.Session, generation, manifest string) error {
	db := r.engine.DB(ctx)
	if err := db.Save(&session).Error; err != nil {
		return err
	}
	if err := r.setPayloadOwner(ctx, "canonical:"+session.ID, manifest); err != nil {
		return err
	}
	if err := db.Save(&reporting_do.CanonicalSnapshot{SessionKey: session.ID, Payload: manifest}).Error; err != nil {
		return err
	}
	return db.Save(&reporting_do.SessionPublication{SessionKey: session.ID, Generation: generation}).Error
}

// CleanupPublication 每轮只清理一批已确认任务的片段；不会触碰待确认分片。
func (r *Reporting) CleanupPublication(ctx context.Context) error {
	var job reporting_do.PublicationJob
	err := r.engine.DB(ctx).Where("state = ?", "published").Order("created_at_ms,id").Take(&job).Error
	if err != nil {
		return err
	}
	var part reporting_do.SnapshotChunk
	db := r.engine.DB(ctx)
	err = db.Where("client_id = ? AND source_key = ? AND revision = ?", job.ClientID, job.SourceKey, job.Revision).Order("chunk_index").Take(&part).Error
	if err == nil {
		return db.Delete(&part).Error
	}
	if !errors.Is(err, gorm.ErrRecordNotFound) {
		return err
	}
	return db.Model(&reporting_do.PublicationJob{}).Where("id = ? AND state = ?", job.ID, "published").Updates(map[string]any{"state": "completed", "body": ""}).Error
}
