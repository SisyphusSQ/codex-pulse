package reporting_repo

import (
	"context"
	"database/sql"

	"gorm.io/gorm"

	reporting_do "github.com/SisyphusSQ/codex-pulse/server/internal/models/do/mysql/reporting_do"
	"github.com/SisyphusSQ/codex-pulse/server/utils"
)

// StreamSourcesForSessions 用于一次性补建索引，读取这些会话的全部来源，包括删除修订。
func (r *Reporting) StreamSourcesForSessions(ctx context.Context, sessionKeys []string, visit func(reporting_do.SessionSource) error) error {
	db := r.engine.DB(ctx).Table("pulse_session_sources AS src").Where("src.session_key IN ?", sessionKeys)
	return StreamSourcePayloads(ctx, db, "", len(sessionKeys)*128, visit)
}

// StreamSourcePayloads 流式解引用一个有序来源查询中的内联正文和发布分片。
func StreamSourcePayloads(ctx context.Context, db *gorm.DB, hint string, maxSources int, visit func(reporting_do.SessionSource) error) error {
	const columns = "src.id,src.session_key,src.client_id,src.home_id,src.source_kind,src.revision,src.collected_at_ms,src.digest,src.payload,part.part_index,part.body"
	rows, err := db.Select(hint + columns).
		Joins("LEFT JOIN pulse_payload_owners owner ON owner.id=CONCAT('source:',src.id)").
		Joins("LEFT JOIN pulse_payload_parts part ON part.payload_key=owner.payload_key").
		Order("src.session_key,src.id,part.part_index").Rows()
	if err != nil {
		return err
	}
	defer rows.Close()
	var current reporting_do.SessionSource
	var parts []reporting_do.PayloadPart
	count := 0
	flush := func() error {
		if current.ID == "" {
			return nil
		}
		payload, err := ResolvePayloadParts(current.Payload, parts)
		if err != nil {
			return err
		}
		current.Payload = payload
		return visit(current)
	}
	for rows.Next() {
		if err := ctx.Err(); err != nil {
			return err
		}
		var row reporting_do.SessionSource
		var index sql.NullInt64
		var body []byte
		if err := rows.Scan(&row.ID, &row.SessionKey, &row.ClientID, &row.HomeID, &row.SourceKind, &row.Revision, &row.CollectedAtMS, &row.Digest, &row.Payload, &index, &body); err != nil {
			return err
		}
		if row.ID != current.ID {
			if err := flush(); err != nil {
				return err
			}
			count++
			if count > maxSources {
				return utils.ErrRequestBudget
			}
			current = row
			parts = nil
		}
		if index.Valid {
			if len(parts) >= 257 {
				return utils.ErrRequestBudget
			}
			parts = append(parts, reporting_do.PayloadPart{PartIndex: int(index.Int64), Body: body})
		}
	}
	if err := rows.Err(); err != nil {
		return err
	}
	return flush()
}
