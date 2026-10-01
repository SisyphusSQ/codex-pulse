package store

import (
	"gorm.io/gorm"

	storelight "github.com/SisyphusSQ/codex-pulse/internal/store/lightindex"
	"github.com/SisyphusSQ/codex-pulse/internal/throughput"
)

type throughputSource struct {
	SessionID     string  `gorm:"column:session_id"`
	ParserVersion *string `gorm:"column:parser_version"`
	Complete      bool    `gorm:"column:complete"`
	ScanState     string  `gorm:"column:scan_state"`
}

// loadLightThroughput reads only the returned page's active-generation safe facts.
// Rows are streamed rather than materializing every usage event in memory.
func loadLightThroughput(db *gorm.DB, ids []string) (map[string]throughput.Stats, map[string][]throughput.Turn, error) {
	stats := make(map[string]throughput.Stats, len(ids))
	turns := make(map[string][]throughput.Turn, len(ids))
	if len(ids) == 0 {
		return stats, turns, nil
	}
	var sources []throughputSource
	if err := db.Table("light_sessions AS session").
		Joins("LEFT JOIN light_token_scans AS scan ON scan.session_id = session.session_id AND scan.generation = session.active_token_generation").
		Where("session.session_id IN ?", ids).
		Select("session.session_id, session.scan_state, scan.parser_version, scan.complete").Find(&sources).Error; err != nil {
		return nil, nil, err
	}
	accumulators := make(map[string]*throughput.Accumulator, len(ids))
	for _, source := range sources {
		stats[source.SessionID] = throughput.Stats{Status: "unavailable", Reason: "index_pending"}
		if source.ParserVersion != nil && *source.ParserVersion == throughput.ParserVersion {
			accumulators[source.SessionID] = throughput.NewAccumulator()
		}
	}
	rows, err := db.Table("light_turn_events AS event").
		Joins("JOIN light_sessions AS session ON session.session_id = event.session_id AND session.active_token_generation = event.generation").
		Joins("JOIN light_token_scans AS scan ON scan.session_id = event.session_id AND scan.generation = event.generation").
		Where("event.session_id IN ? AND scan.parser_version = ?", ids, throughput.ParserVersion).
		Select("event.*").Order("event.session_id, event.source_offset").Rows()
	if err != nil {
		return nil, nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var model storelight.TurnEventModel
		if err := db.ScanRows(rows, &model); err != nil {
			return nil, nil, err
		}
		if a := accumulators[model.SessionID]; a != nil {
			a.Add(model.Event())
		}
	}
	if err := rows.Err(); err != nil {
		return nil, nil, err
	}
	for _, source := range sources {
		if a := accumulators[source.SessionID]; a != nil {
			s, items := a.Result()
			if s.AverageMilliTPS != nil && (!source.Complete || source.ScanState != "complete") {
				s.Status, s.Reason = "partial", "index_incomplete"
			}
			stats[source.SessionID], turns[source.SessionID] = s, items
		}
	}
	return stats, turns, nil
}

func attachLightThroughput(db *gorm.DB, records []SessionAnalyticsRecord) (map[string][]throughput.Turn, error) {
	ids := make([]string, 0, len(records))
	for _, record := range records {
		ids = append(ids, record.SessionID)
	}
	stats, turns, err := loadLightThroughput(db, ids)
	if err != nil {
		return nil, err
	}
	for index := range records {
		if value, ok := stats[records[index].SessionID]; ok {
			records[index].Throughput = &value
		}
	}
	return turns, nil
}
