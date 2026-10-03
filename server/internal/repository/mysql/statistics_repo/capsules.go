package statistics_repo

import (
	"context"

	reporting_do "github.com/SisyphusSQ/codex-pulse/server/internal/models/do/mysql/reporting_do"
	statistics_dto "github.com/SisyphusSQ/codex-pulse/server/internal/models/dto/statistics_dto"
)

func (r *Statistics) StreamSourceMetadata(ctx context.Context, q statistics_dto.StatisticsQuery, visit func(statistics_dto.SourceMetadata) error) error {
	db := r.engine.DB(ctx).Table("pulse_session_sources AS src").Select("src.id,src.session_key,src.client_id,src.collected_at_ms,src.revision,src.source_kind,COALESCE(cap.complete,JSON_EXTRACT(src.payload,'$.complete')) AS complete,COALESCE(cap.deleted,JSON_EXTRACT(src.payload,'$.deleted')) AS deleted").Joins("LEFT JOIN pulse_session_capsules cap ON cap.kind='source' AND cap.id=src.id").Where("src.session_key IN (?)", r.sessionQuery(ctx, q).Select("s.id"))
	rows, err := db.Order("src.session_key,src.id").Rows()
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		if err := ctx.Err(); err != nil {
			return err
		}
		var row statistics_dto.SourceMetadata
		if err = rows.Scan(&row.ID, &row.SessionKey, &row.ClientID, &row.CollectedAtMS, &row.Revision, &row.SourceKind, &row.Complete, &row.Deleted); err != nil {
			return err
		}
		if err = visit(row); err != nil {
			return err
		}
	}
	return rows.Err()
}

func (r *Statistics) Capsules(ctx context.Context, ids []string, client string) (out []reporting_do.SessionCapsule, err error) {
	if len(ids) == 0 {
		return []reporting_do.SessionCapsule{}, nil
	}
	db := r.engine.DB(ctx).Where("session_key IN ?", ids)
	if client != "" {
		db = db.Where("kind='source' AND client_id = ?", client)
	}
	err = db.Order("kind,id").Find(&out).Error
	return
}
