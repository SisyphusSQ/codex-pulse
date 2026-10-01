package statistics_repo

import (
	"context"

	reporting_do "github.com/SisyphusSQ/codex-pulse/server/internal/models/do/mysql/reporting_do"
)

func (r *Statistics) StreamCanonicalSessions(ctx context.Context, ids []string, visit func(reporting_do.CanonicalSnapshot) error) error {
	if len(ids) == 0 {
		return nil
	}
	db := r.engine.DB(ctx).Where("session_key IN ?", ids)
	rows, err := db.Model(&reporting_do.CanonicalSnapshot{}).Order("session_key").Rows()
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		if err := ctx.Err(); err != nil {
			return err
		}
		var row reporting_do.CanonicalSnapshot
		if err := db.ScanRows(rows, &row); err != nil {
			return err
		}
		if err := visit(row); err != nil {
			return err
		}
	}
	return rows.Err()
}

func (r *Statistics) StreamSessionSources(ctx context.Context, ids []string, client string, visit func(reporting_do.SessionSource) error) error {
	if len(ids) == 0 {
		return nil
	}
	db := r.engine.DB(ctx).Where("session_key IN ?", ids)
	if client != "" {
		db = db.Where("client_id = ?", client)
	}
	rows, err := db.Model(&reporting_do.SessionSource{}).Order("session_key,id").Rows()
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		if err := ctx.Err(); err != nil {
			return err
		}
		var row reporting_do.SessionSource
		if err := db.ScanRows(rows, &row); err != nil {
			return err
		}
		if err := visit(row); err != nil {
			return err
		}
	}
	return rows.Err()
}
