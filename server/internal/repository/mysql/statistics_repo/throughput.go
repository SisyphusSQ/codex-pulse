package statistics_repo

import (
	"context"

	reporting_do "github.com/SisyphusSQ/codex-pulse/server/internal/models/do/mysql/reporting_do"
	reporting_repo "github.com/SisyphusSQ/codex-pulse/server/internal/repository/mysql/reporting_repo"
)

func (r *Statistics) StreamCanonicalSessions(ctx context.Context, ids []string, visit func(reporting_do.CanonicalSnapshot) error) error {
	if len(ids) == 0 {
		return nil
	}
	db := r.engine.DB(ctx).Where("session_key IN ?", ids)
	after := ""
	for {
		var page []reporting_do.CanonicalSnapshot
		if err := db.Where("session_key > ?", after).Order("session_key").Limit(8).Find(&page).Error; err != nil {
			return err
		}
		for _, row := range page {
			payload, err := reporting_repo.ResolvePayload(ctx, r.engine.DB(ctx), row.Payload)
			if err != nil {
				return err
			}
			row.Payload = payload
			if err := visit(row); err != nil {
				return err
			}
			after = row.SessionKey
		}
		if len(page) < 8 {
			return nil
		}
	}
}

func (r *Statistics) StreamSessionSources(ctx context.Context, ids []string, client string, visit func(reporting_do.SessionSource) error) error {
	if len(ids) == 0 {
		return nil
	}
	db := r.engine.DB(ctx).Where("session_key IN ?", ids)
	if client != "" {
		db = db.Where("client_id = ?", client)
	}
	after := ""
	for {
		var page []reporting_do.SessionSource
		if err := db.Where("id > ?", after).Order("id").Limit(8).Find(&page).Error; err != nil {
			return err
		}
		for _, row := range page {
			payload, err := reporting_repo.ResolvePayload(ctx, r.engine.DB(ctx), row.Payload)
			if err != nil {
				return err
			}
			row.Payload = payload
			if err := visit(row); err != nil {
				return err
			}
			after = row.ID
		}
		if len(page) < 8 {
			return nil
		}
	}
}
