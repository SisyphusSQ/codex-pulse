package reporting_repo

import (
	"context"

	"gorm.io/gorm/clause"

	reporting_do "github.com/SisyphusSQ/codex-pulse/server/internal/models/do/mysql/reporting_do"
)

func (r *Reporting) RetiredObservation(ctx context.Context, id string) (row reporting_do.RetiredObservation, err error) {
	err = r.engine.DB(ctx).Where("id = ?", id).Take(&row).Error
	return
}
func (r *Reporting) SaveCapsule(ctx context.Context, row reporting_do.SessionCapsule) error {
	return r.engine.DB(ctx).Save(&row).Error
}
func (r *Reporting) WarmCapsule(ctx context.Context, row reporting_do.SessionCapsule) error {
	return r.engine.DB(ctx).Clauses(clause.OnConflict{DoNothing: true}).Create(&row).Error
}
func (r *Reporting) MissingSourceCapsule(ctx context.Context) (row reporting_do.SessionSource, err error) {
	err = r.engine.DB(ctx).Where("NOT EXISTS (SELECT 1 FROM pulse_session_capsules c WHERE c.kind='source' AND c.id=pulse_session_sources.id)").Order("id").Take(&row).Error
	return
}
func (r *Reporting) MissingCanonicalCapsule(ctx context.Context) (row reporting_do.CanonicalSnapshot, err error) {
	err = r.engine.DB(ctx).Where("NOT EXISTS (SELECT 1 FROM pulse_session_capsules c WHERE c.kind='canonical' AND c.id=pulse_session_canonical.session_key)").Order("session_key").Take(&row).Error
	return
}
