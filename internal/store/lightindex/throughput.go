package lightindex

import (
	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	storeschema "github.com/SisyphusSQ/codex-pulse/internal/store/schema"
	"github.com/SisyphusSQ/codex-pulse/internal/throughput"
)

var throughputSchemaObjects = []storeschema.Object{{ObjectType: "table", Name: "light_turn_events", Statement: `CREATE TABLE IF NOT EXISTS light_turn_events (
	session_id TEXT NOT NULL,
	generation INTEGER NOT NULL,
	source_offset INTEGER NOT NULL CHECK (source_offset > 0),
	kind TEXT NOT NULL CHECK (kind IN ('start','complete','abort','usage','gap','inherited')),
	turn_id TEXT CHECK (turn_id IS NULL OR (length(turn_id) BETWEEN 1 AND 128 AND turn_id NOT GLOB '*[^a-zA-Z0-9_.-]*')),
	at_ms INTEGER CHECK (at_ms IS NULL OR at_ms BETWEEN 0 AND 9007199254740991),
	time_source TEXT NOT NULL CHECK (time_source IN ('','log_timestamp','source_seconds')),
	started_at_ms INTEGER CHECK (started_at_ms IS NULL OR started_at_ms BETWEEN 0 AND 9007199254740991),
	duration_ms INTEGER CHECK (duration_ms IS NULL OR duration_ms BETWEEN 0 AND 9007199254740991),
	output_delta INTEGER CHECK (output_delta IS NULL OR output_delta BETWEEN 0 AND 9007199254740991),
	output_observed INTEGER NOT NULL CHECK (output_observed IN (0,1)),
	PRIMARY KEY (session_id,generation,source_offset),
	FOREIGN KEY (session_id,generation) REFERENCES light_token_scans(session_id,generation) ON DELETE CASCADE
) STRICT`}}

func ThroughputSchemaObjects() []storeschema.Object {
	return append([]storeschema.Object(nil), throughputSchemaObjects...)
}

// TurnEventModel is confined to the SQLite adapter; no source content is stored.
type TurnEventModel struct {
	SessionID      string  `gorm:"column:session_id"`
	Generation     int64   `gorm:"column:generation"`
	SourceOffset   int64   `gorm:"column:source_offset"`
	Kind           string  `gorm:"column:kind"`
	TurnID         *string `gorm:"column:turn_id"`
	AtMS           *int64  `gorm:"column:at_ms"`
	TimeSource     string  `gorm:"column:time_source"`
	StartedAtMS    *int64  `gorm:"column:started_at_ms"`
	DurationMS     *int64  `gorm:"column:duration_ms"`
	OutputDelta    *int64  `gorm:"column:output_delta"`
	OutputObserved bool    `gorm:"column:output_observed"`
}

func (TurnEventModel) TableName() string { return "light_turn_events" }
func (m TurnEventModel) Event() throughput.Event {
	return throughput.Event{Offset: m.SourceOffset, Kind: m.Kind, TurnID: m.TurnID, AtMS: m.AtMS, TimeSource: m.TimeSource, StartedAtMS: m.StartedAtMS, DurationMS: m.DurationMS, OutputDelta: m.OutputDelta, OutputObserved: m.OutputObserved}
}

func validateTurnEvent(e throughput.Event, offset int64) error {
	if e.Offset <= 0 || e.Offset > offset {
		return invalidRecord("invalid throughput source position")
	}
	switch e.Kind {
	case "start", "complete", "abort", "usage", "gap", "inherited":
	default:
		return invalidRecord("invalid throughput event kind")
	}
	for _, value := range []*int64{e.AtMS, e.StartedAtMS, e.DurationMS, e.OutputDelta} {
		if value != nil && (*value < 0 || *value > throughput.MaxInteger) {
			return invalidRecord("invalid throughput number")
		}
	}
	if e.TimeSource != "" && e.TimeSource != "log_timestamp" && e.TimeSource != "source_seconds" {
		return invalidRecord("invalid throughput time source")
	}
	if e.TurnID != nil {
		if len(*e.TurnID) == 0 || len(*e.TurnID) > 128 {
			return invalidRecord("invalid throughput identity")
		}
		for _, ch := range *e.TurnID {
			if ch != '-' && ch != '_' && ch != '.' && !(ch >= 'a' && ch <= 'z') && !(ch >= 'A' && ch <= 'Z') && !(ch >= '0' && ch <= '9') {
				return invalidRecord("invalid throughput identity")
			}
		}
	}
	return nil
}

func commitTurnEvents(db *gorm.DB, batch LightTokenBatch) error {
	for _, e := range batch.TurnEvents {
		model := TurnEventModel{SessionID: batch.SessionID, Generation: batch.Generation, SourceOffset: e.Offset, Kind: e.Kind, TurnID: e.TurnID, AtMS: e.AtMS, TimeSource: e.TimeSource, StartedAtMS: e.StartedAtMS, DurationMS: e.DurationMS, OutputDelta: e.OutputDelta, OutputObserved: e.OutputObserved}
		result := db.Clauses(clause.OnConflict{DoNothing: true}).Create(&model)
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected != 1 {
			return ErrLightTokenConflict
		}
	}
	return nil
}
