package reporting_repo

import (
	"context"
	"database/sql"
	"errors"

	reporting_dto "github.com/SisyphusSQ/codex-pulse/server/internal/models/dto/reporting_dto"
)

func (r *Reporting) Metrics(ctx context.Context) (rows []reporting_dto.DeviceHealth, pool sql.DBStats, err error) {
	if r.engine.Connect() == nil {
		return nil, pool, errors.New("database not started")
	}
	db := r.engine.DB(ctx)
	conn, err := db.DB()
	if err != nil {
		return nil, pool, err
	}
	pool = conn.Stats()
	err = db.Table("pulse_device_status AS st").Select("st.client_id,st.provider,st.collected_at_ms,st.received_at_ms,st.pending_batches,sy.sync_state,sy.sync_checked_at_ms").Joins("JOIN pulse_clients AS c ON c.id=st.client_id AND c.revoked_at_ms IS NULL").Joins("LEFT JOIN pulse_device_sync AS sy ON sy.client_id=st.client_id AND sy.provider=st.provider").Order("st.client_id,st.provider").Limit(3001).Find(&rows).Error
	if len(rows) > 3000 {
		return nil, pool, errors.New("metrics device budget exceeded")
	}
	return
}
