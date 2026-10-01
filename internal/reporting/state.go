package reporting

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"

	"github.com/google/uuid"
	"gorm.io/gorm"

	reportingv1 "github.com/SisyphusSQ/codex-pulse/api/codexpulse/reporting/v1"
	storesqlite "github.com/SisyphusSQ/codex-pulse/internal/store/sqlite"
)

type State struct {
	db   *storesqlite.Store
	salt []byte
}
type queued struct {
	Seq                           int64 `gorm:"primaryKey"`
	Partition, BatchID, SourceKey string
	Revision                      int64
	Body                          []byte
}

func (queued) TableName() string { return "reporting_outbox" }

type checkpoint struct {
	Partition, SourceKey                       string `gorm:"primaryKey"`
	Provider, HomeID, SessionID, Digest, Sweep string
	Revision, AcknowledgedRevision             int64
	Deleted                                    bool
	Metadata                                   []byte
}

func (checkpoint) TableName() string { return "reporting_checkpoints" }

// OpenState creates a separate private SQLite store; root source migrations remain untouched.
func OpenState(ctx context.Context, path string) (*State, error) {
	db, err := storesqlite.Open(ctx, storesqlite.Config{Path: path, WriteQueueCapacity: 8, MaxReadConnections: 1})
	if err != nil {
		return nil, ErrUnavailable
	}
	state := &State{db: db}
	err = db.Write(ctx, func(ctx context.Context, tx *gorm.DB) error {

		// Check an existing marker before any schema mutation; an older Helper must not
		// silently modify a newer reporting store.
		var exists int64
		if err := tx.Raw("SELECT COUNT(*) FROM sqlite_schema WHERE type='table' AND name='reporting_schema'").Scan(&exists).Error; err != nil {
			return err
		}
		if exists > 0 {
			var version int
			if err := tx.Raw("SELECT version FROM reporting_schema WHERE id=1").Scan(&version).Error; err != nil {
				return err
			}
			if version != 1 {
				return ErrUnavailable
			}
		}
		for _, sql := range []string{
			`CREATE TABLE IF NOT EXISTS reporting_schema (id INTEGER PRIMARY KEY CHECK(id=1), version INTEGER NOT NULL, salt BLOB NOT NULL CHECK(length(salt)=32)) STRICT`,
			`CREATE TABLE IF NOT EXISTS reporting_settings (id INTEGER PRIMARY KEY CHECK(id=1),endpoint TEXT NOT NULL,client_id TEXT NOT NULL,credential TEXT NOT NULL,enabled INTEGER NOT NULL,allow_http INTEGER NOT NULL,interval_seconds INTEGER NOT NULL,history_start_at_ms INTEGER NOT NULL,state TEXT NOT NULL,last_attempt_at_ms INTEGER,last_success_at_ms INTEGER) STRICT`,
			`CREATE TABLE IF NOT EXISTS reporting_outbox (seq INTEGER PRIMARY KEY AUTOINCREMENT,partition TEXT NOT NULL,batch_id TEXT NOT NULL UNIQUE,source_key TEXT NOT NULL,revision INTEGER NOT NULL,body BLOB NOT NULL) STRICT`,
			`CREATE INDEX IF NOT EXISTS idx_reporting_outbox_partition ON reporting_outbox(partition,seq)`,
			`CREATE TABLE IF NOT EXISTS reporting_cursors (partition TEXT NOT NULL,provider TEXT NOT NULL,home_id TEXT NOT NULL,after TEXT NOT NULL,sweep TEXT NOT NULL,authority INTEGER NOT NULL,next_due_at_ms INTEGER NOT NULL,PRIMARY KEY(partition,provider,home_id)) STRICT`,
			`CREATE TABLE IF NOT EXISTS reporting_checkpoints (partition TEXT NOT NULL,source_key TEXT NOT NULL,provider TEXT NOT NULL,home_id TEXT NOT NULL,session_id TEXT NOT NULL,digest TEXT NOT NULL,sweep TEXT NOT NULL,revision INTEGER NOT NULL,acknowledged_revision INTEGER NOT NULL,deleted INTEGER NOT NULL,metadata BLOB NOT NULL,PRIMARY KEY(partition,source_key)) STRICT`,
		} {
			if err := tx.Exec(sql).Error; err != nil {
				return err
			}
		}
		var schema struct {
			Version int
			Salt    []byte
		}
		result := tx.Table("reporting_schema").Where("id = 1").Take(&schema)
		if errors.Is(result.Error, gorm.ErrRecordNotFound) {
			schema.Version = 1
			schema.Salt = make([]byte, 32)
			if _, err := rand.Read(schema.Salt); err != nil {
				return err
			}
			if err := tx.Exec("INSERT INTO reporting_schema(id,version,salt) VALUES(1,?,?)", 1, schema.Salt).Error; err != nil {
				return err
			}
		} else if result.Error != nil {
			return result.Error
		}
		if schema.Version != 1 || len(schema.Salt) != 32 {
			return ErrUnavailable
		}
		state.salt = append([]byte(nil), schema.Salt...)
		var count int64
		if err := tx.Model(&credentialSettings{}).Count(&count).Error; err != nil {
			return err
		}
		if count == 0 {
			return tx.Create(&credentialSettings{ID: 1, IntervalSeconds: DefaultIntervalSeconds, State: "disabled"}).Error
		}
		return nil
	})
	if err != nil {
		_ = db.Close(context.Background())
		return nil, ErrUnavailable
	}
	return state, nil
}
func (s *State) Close(ctx context.Context) error { return s.db.Close(ctx) }
func (s *State) HomeID(parts ...string) string {
	bytes, _ := json.Marshal(parts)
	sum := hmac.New(sha256.New, s.salt)
	sum.Write(bytes)
	return hex.EncodeToString(sum.Sum(nil))
}
func (s *State) settings(ctx context.Context) (out credentialSettings, err error) {
	err = s.db.View(ctx, func(_ context.Context, db *gorm.DB) error { return db.First(&out, 1).Error })
	return
}
func (s *State) saveSettings(ctx context.Context, value credentialSettings) error {
	return s.db.Write(ctx, func(_ context.Context, tx *gorm.DB) error { return tx.Save(&value).Error })
}
func (s *State) Status(ctx context.Context) (status Status, err error) {
	err = s.db.ViewSnapshot(ctx, func(_ context.Context, db *gorm.DB) error {
		var cfg credentialSettings
		if err := db.First(&cfg, 1).Error; err != nil {
			return err
		}
		status = Status{Endpoint: cfg.Endpoint, ClientID: cfg.ClientID, Enabled: cfg.Enabled, AllowHTTP: cfg.AllowHTTP, IntervalSeconds: cfg.IntervalSeconds, HistoryStartAtMS: cfg.HistoryStartAtMS, State: cfg.State, LastAttemptAtMS: cfg.LastAttemptAtMS, LastSuccessAtMS: cfg.LastSuccessAtMS}
		var counts struct{ Count, Bytes int64 }
		if err := db.Model(&queued{}).Select("COUNT(*) AS count,COALESCE(SUM(length(body)),0) AS bytes").Where("partition = ?", cfg.partition()).Scan(&counts).Error; err != nil {
			return err
		}
		status.PendingBatches = counts.Count
		status.PendingBytes = counts.Bytes
		return db.Model(&queued{}).Where("partition <> ?", cfg.partition()).Count(&status.RetainedBatches).Error
	})
	return
}
func snapshotDigest(value reportingv1.SessionSnapshot) (string, error) {
	value.Revision = 0
	value.CollectedAtMS = 0
	bytes, err := json.Marshal(value)
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(bytes)
	return hex.EncodeToString(sum[:]), nil
}

// Enqueue atomically reserves the monotonic revision and immutable retry body. Only Ack
// advances acknowledged_revision; queue capacity errors don't advance the export checkpoint.
func (s *State) Enqueue(ctx context.Context, partition, sweep string, value reportingv1.SessionSnapshot) error {
	digest, err := snapshotDigest(value)
	if err != nil {
		return err
	}
	key := reportingv1.Key(value.Provider, value.HomeID, value.SessionID)
	return s.db.Write(ctx, func(_ context.Context, tx *gorm.DB) error {
		var previous checkpoint
		result := tx.Where("partition = ? AND source_key = ?", partition, key).Take(&previous)
		if result.Error != nil && !errors.Is(result.Error, gorm.ErrRecordNotFound) {
			return result.Error
		}
		if result.Error == nil && previous.Digest == digest {
			return tx.Model(&checkpoint{}).Where("partition = ? AND source_key = ?", partition, key).Update("sweep", sweep).Error
		}
		if previous.Revision == 1<<63-1 {
			return ErrProtocol
		}
		value.Revision = previous.Revision + 1
		batch := reportingv1.Batch{Version: reportingv1.Version, ID: uuid.NewString(), Sessions: []reportingv1.SessionSnapshot{value}}
		if err := batch.Validate(); err != nil {
			return ErrProtocol
		}
		body, err := json.Marshal(batch)
		if err != nil {
			return err
		}
		if len(body) > reportingv1.MaxBodyBytes {
			return ErrQueueFull
		}
		var budget struct{ Count, Bytes int64 }
		if err := tx.Model(&queued{}).Select("COUNT(*) AS count,COALESCE(SUM(length(body)),0) AS bytes").Scan(&budget).Error; err != nil {
			return err
		}
		if budget.Count >= QueueBatchBudget || budget.Bytes+int64(len(body)) > QueueByteBudget {
			return ErrQueueFull
		}
		metadata := value
		metadata.Contributions = nil
		metadata.Invocations = nil
		bytes, err := json.Marshal(metadata)
		if err != nil {
			return err
		}
		saved := checkpoint{Partition: partition, SourceKey: key, Provider: value.Provider, HomeID: value.HomeID, SessionID: value.SessionID, Digest: digest, Sweep: sweep, Revision: value.Revision, AcknowledgedRevision: previous.AcknowledgedRevision, Deleted: value.Deleted, Metadata: bytes}
		if err := tx.Save(&saved).Error; err != nil {
			return err
		}
		return tx.Create(&queued{Partition: partition, BatchID: batch.ID, SourceKey: key, Revision: value.Revision, Body: body}).Error
	})
}
func (s *State) next(ctx context.Context, partition string) (out queued, err error) {
	err = s.db.View(ctx, func(_ context.Context, db *gorm.DB) error {
		if err := db.Where("partition = ?", partition).Order("seq").First(&out).Error; err != nil {
			return err
		}
		if len(out.Body) > reportingv1.MaxBodyBytes {
			return ErrProtocol
		}
		var batch reportingv1.Batch
		decoder := json.NewDecoder(bytes.NewReader(out.Body))
		decoder.DisallowUnknownFields()
		if err := decoder.Decode(&batch); err != nil {
			return ErrProtocol
		}
		if err := decoder.Decode(new(any)); err != io.EOF {
			return ErrProtocol
		}
		if batch.Validate() != nil || batch.ID != out.BatchID {
			return ErrProtocol
		}
		if out.SourceKey != "" && (len(batch.Sessions) != 1 || reportingQueueKey(batch.Sessions[0]) != out.SourceKey || batch.Sessions[0].Revision != out.Revision) {
			return ErrProtocol
		}
		return nil
	})
	return
}
func (s *State) Ack(ctx context.Context, item queued, receipt reportingv1.Receipt) error {
	if receipt.Version != reportingv1.Version || receipt.BatchID != item.BatchID || receipt.ReceivedAtMS < 0 || receipt.ReceivedAtMS > reportingv1.MaxTimestampMS {
		return ErrProtocol
	}
	return s.db.Write(ctx, func(_ context.Context, tx *gorm.DB) error {
		var actual queued
		if err := tx.Where("seq = ? AND partition = ? AND batch_id = ?", item.Seq, item.Partition, item.BatchID).Take(&actual).Error; err != nil {
			return err
		}
		if err := tx.Model(&checkpoint{}).Where("partition = ? AND source_key = ? AND acknowledged_revision < ?", item.Partition, item.SourceKey, item.Revision).Update("acknowledged_revision", item.Revision).Error; err != nil {
			return err
		}
		return tx.Delete(&actual).Error
	})
}
func (s *State) removed(ctx context.Context, partition, provider, homeID, sweep string) (out []reportingv1.SessionSnapshot, err error) {
	err = s.db.View(ctx, func(_ context.Context, db *gorm.DB) error {
		var rows []checkpoint
		if err := db.Where("partition = ? AND provider = ? AND home_id = ? AND sweep <> ? AND deleted = 0", partition, provider, homeID, sweep).Limit(128).Find(&rows).Error; err != nil {
			return err
		}
		for _, r := range rows {
			var snap reportingv1.SessionSnapshot
			if err := json.Unmarshal(r.Metadata, &snap); err != nil {
				return err
			}
			if !snap.Deleted {
				snap.Deleted = true
				snap.Contributions = []reportingv1.Contribution{}
				snap.Invocations = nil
				out = append(out, snap)
			}
		}
		return nil
	})
	return
}

// clearPending is an explicit local queue discard, not a deletion of center history.
func (s *State) clearPending(ctx context.Context, partition string) error {
	return s.db.Write(ctx, func(_ context.Context, tx *gorm.DB) error {
		if err := tx.Where("partition = ?", partition).Delete(&queued{}).Error; err != nil {
			return err
		}
		// Keep revision monotonic, but force fresh export after the explicit discard.
		return tx.Model(&checkpoint{}).Where("partition = ?", partition).Update("digest", "").Error
	})
}

type exportCursor struct {
	Partition, Provider, HomeID string `gorm:"primaryKey"`
	After, Sweep                string
	Authority                   bool
	NextDueAtMS                 int64
}

func (exportCursor) TableName() string { return "reporting_cursors" }
func (s *State) cursor(ctx context.Context, partition, provider, homeID string) (value exportCursor, err error) {
	value = exportCursor{Partition: partition, Provider: provider, HomeID: homeID, Sweep: uuid.NewString(), Authority: true}
	err = s.db.View(ctx, func(_ context.Context, db *gorm.DB) error {
		var saved exportCursor
		err := db.Where("partition = ? AND provider = ? AND home_id = ?", partition, provider, homeID).Take(&saved).Error
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil
		}
		if err == nil {
			value = saved
		}
		return err
	})
	return
}
func (s *State) saveCursor(ctx context.Context, value exportCursor) error {
	return s.db.Write(ctx, func(_ context.Context, db *gorm.DB) error { return db.Save(&value).Error })
}

func (s *State) resetCursors(ctx context.Context, partition string) error {
	return s.db.Write(ctx, func(_ context.Context, db *gorm.DB) error {
		return db.Model(&exportCursor{}).Where("partition = ?", partition).Update("next_due_at_ms", 0).Error
	})
}

func reportingQueueKey(s reportingv1.SessionSnapshot) string {
	return reportingv1.Key(s.Provider, s.HomeID, s.SessionID)
}

func (s *State) hasExport(ctx context.Context, partition string) (present bool, err error) {
	var count int64
	err = s.db.View(ctx, func(_ context.Context, db *gorm.DB) error {
		return db.Model(&checkpoint{}).Where("partition = ?", partition).Count(&count).Error
	})
	return count > 0, err
}

// EnqueueFacts handles whitelisted account/quota/status batches without a Session checkpoint.
func (s *State) EnqueueFacts(ctx context.Context, partition string, batch reportingv1.Batch) error {
	batch.Version = reportingv1.Version
	batch.ID = uuid.NewString()
	if len(batch.Sessions) != 0 || batch.Validate() != nil {
		return ErrProtocol
	}
	body, err := json.Marshal(batch)
	if err != nil {
		return ErrProtocol
	}
	if len(body) > reportingv1.MaxBodyBytes {
		return ErrQueueFull
	}
	return s.db.Write(ctx, func(_ context.Context, db *gorm.DB) error {
		var budget struct{ Count, Bytes int64 }
		if err := db.Model(&queued{}).Select("COUNT(*) AS count,COALESCE(SUM(length(body)),0) AS bytes").Scan(&budget).Error; err != nil {
			return err
		}
		if budget.Count >= QueueBatchBudget || budget.Bytes+int64(len(body)) > QueueByteBudget {
			return ErrQueueFull
		}
		return db.Create(&queued{Partition: partition, BatchID: batch.ID, Body: body}).Error
	})
}
