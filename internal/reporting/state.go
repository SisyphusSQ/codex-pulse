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
	Priority                      int
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
			if version != 1 && version != 2 && version != 3 {
				return ErrUnavailable
			}
		}
		for _, sql := range []string{
			`CREATE TABLE IF NOT EXISTS reporting_account_identities(provider TEXT NOT NULL,local_scope TEXT NOT NULL,account_id TEXT NOT NULL,email TEXT,plan TEXT,confirmed_at_ms INTEGER NOT NULL,collected_at_ms INTEGER NOT NULL,PRIMARY KEY(provider,local_scope)) STRICT`,
			`CREATE TABLE IF NOT EXISTS reporting_facts_checkpoints(partition TEXT NOT NULL,source_key TEXT NOT NULL,digest TEXT NOT NULL,PRIMARY KEY(partition,source_key)) STRICT`,
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
		if (schema.Version != 1 && schema.Version != 2 && schema.Version != 3) || len(schema.Salt) != 32 {
			return ErrUnavailable
		}
		if !tx.Migrator().HasColumn(&queued{}, "priority") {
			if err := tx.Exec("ALTER TABLE reporting_outbox ADD COLUMN priority INTEGER NOT NULL DEFAULT 2").Error; err != nil {
				return err
			}
			if err := tx.Exec("UPDATE reporting_outbox SET priority=1 WHERE source_key='' ").Error; err != nil {
				return err
			}
		}
		if err := tx.Exec("UPDATE reporting_schema SET version=3 WHERE id=1").Error; err != nil {
			return err
		}
		if err := tx.Exec(`CREATE TABLE IF NOT EXISTS reporting_full_sync(partition TEXT PRIMARY KEY,state TEXT NOT NULL,started_at_ms INTEGER NOT NULL,exported_sessions INTEGER NOT NULL,acknowledged_batches INTEGER NOT NULL) STRICT`).Error; err != nil {
			return err
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
		var task fullSync
		if err := db.Where("partition = ?", cfg.partition()).Find(&task).Error; err != nil {
			return err
		}
		status.FullSyncState = task.State
		if task.StartedAtMS > 0 {
			status.FullSyncStartedAtMS = new(task.StartedAtMS)
		}
		status.FullSyncExportedSessions = task.ExportedSessions
		status.FullSyncAcknowledgedBatches = task.AcknowledgedBatches
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
		parts, err := reportingv1.SplitSnapshot(value)
		if err != nil {
			return ErrProtocol
		}
		rows := make([]queued, 0, len(parts))
		totalBytes := int64(0)
		for _, part := range parts {
			batch := reportingv1.Batch{Version: reportingv1.Version, ID: uuid.NewString(), Sessions: []reportingv1.SessionSnapshot{part}}
			if batch.Validate() != nil {
				return ErrProtocol
			}
			body, err := json.Marshal(batch)
			if err != nil {
				return err
			}
			if len(body) > reportingv1.MaxBodyBytes {
				return ErrQueueFull
			}
			rows = append(rows, queued{Partition: partition, BatchID: batch.ID, SourceKey: key, Revision: value.Revision, Priority: 2, Body: body})
			totalBytes += int64(len(body))
		}
		var budget struct{ Count, Bytes int64 }
		if err := tx.Model(&queued{}).Select("COUNT(*) AS count,COALESCE(SUM(length(body)),0) AS bytes").Scan(&budget).Error; err != nil {
			return err
		}
		if budget.Count+int64(len(rows)) > QueueBatchBudget || budget.Bytes+totalBytes > QueueByteBudget {
			return ErrQueueFull
		}
		metadata := value
		metadata.Contributions = nil
		metadata.Throughput = nil
		metadata.CacheUsage = nil
		metadata.Invocations = nil
		bytes, err := json.Marshal(metadata)
		if err != nil {
			return err
		}
		saved := checkpoint{Partition: partition, SourceKey: key, Provider: value.Provider, HomeID: value.HomeID, SessionID: value.SessionID, Digest: digest, Sweep: sweep, Revision: value.Revision, AcknowledgedRevision: previous.AcknowledgedRevision, Deleted: value.Deleted, Metadata: bytes}
		if err := tx.Save(&saved).Error; err != nil {
			return err
		}
		if err := tx.CreateInBatches(rows, 32).Error; err != nil {
			return err
		}
		return tx.Model(&fullSync{}).Where("partition = ? AND state = ?", partition, "running").Update("exported_sessions", gorm.Expr("exported_sessions+1")).Error
	})
}
func (s *State) next(ctx context.Context, partition string) (out queued, err error) {
	err = s.db.View(ctx, func(_ context.Context, db *gorm.DB) error {
		if err := db.Where("partition = ?", partition).Order("priority, seq").First(&out).Error; err != nil {
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
		var remaining int64
		if err := tx.Model(&queued{}).Where("partition = ? AND source_key = ? AND revision = ? AND seq <> ?", item.Partition, item.SourceKey, item.Revision, item.Seq).Count(&remaining).Error; err != nil {
			return err
		}
		if remaining == 0 {
			if err := tx.Model(&checkpoint{}).Where("partition = ? AND source_key = ? AND acknowledged_revision < ?", item.Partition, item.SourceKey, item.Revision).Update("acknowledged_revision", item.Revision).Error; err != nil {
				return err
			}
		}
		if err := tx.Delete(&actual).Error; err != nil {
			return err
		}
		return tx.Model(&fullSync{}).Where("partition = ? AND state = ?", item.Partition, "running").Update("acknowledged_batches", gorm.Expr("acknowledged_batches+1")).Error
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
				snap.Throughput = nil
				snap.CacheUsage = nil
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
		if err := tx.Where("partition = ?", partition).Delete(&factsCheckpoint{}).Error; err != nil {
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
		if err := db.Model(&checkpoint{}).Where("partition = ?", partition).Count(&count).Error; err != nil {
			return err
		}
		if count > 0 {
			return nil
		}
		return db.Model(&factsCheckpoint{}).Where("partition = ?", partition).Count(&count).Error
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

// EnqueueCheckedFacts 只在允许字段发生变化时入队；确认丢失仍重发原批次。
func (s *State) EnqueueCheckedFacts(ctx context.Context, partition, key string, batch reportingv1.Batch) (changed bool, err error) {
	if !boundedFactsKey(key) || len(batch.Sessions) != 0 {
		return false, ErrProtocol
	}
	batch.Version = reportingv1.Version
	batch.ID = "00000000-0000-0000-0000-000000000000"
	if batch.Validate() != nil {
		return false, ErrProtocol
	}
	bytes, err := json.Marshal(batch)
	if err != nil {
		return false, ErrProtocol
	}
	sum := sha256.Sum256(bytes)
	digest := hex.EncodeToString(sum[:])
	batch.ID = uuid.NewString()
	body, err := json.Marshal(batch)
	if err != nil {
		return false, ErrProtocol
	}
	if len(body) > reportingv1.MaxBodyBytes {
		return false, ErrQueueFull
	}
	err = s.db.Write(ctx, func(_ context.Context, db *gorm.DB) error {
		var previous factsCheckpoint
		result := db.Where("partition = ? AND source_key = ?", partition, key).Take(&previous)
		if result.Error != nil && !errors.Is(result.Error, gorm.ErrRecordNotFound) {
			return result.Error
		}
		if previous.Digest == digest {
			return nil
		}
		var budget struct{ Count, Bytes int64 }
		if err := db.Model(&queued{}).Select("COUNT(*) AS count,COALESCE(SUM(length(body)),0) AS bytes").Scan(&budget).Error; err != nil {
			return err
		}
		if budget.Count >= QueueBatchBudget || budget.Bytes+int64(len(body)) > QueueByteBudget {
			return ErrQueueFull
		}
		if err := db.Create(&queued{Partition: partition, BatchID: batch.ID, Body: body}).Error; err != nil {
			return err
		}
		if err := db.Save(&factsCheckpoint{Partition: partition, SourceKey: key, Digest: digest}).Error; err != nil {
			return err
		}
		changed = true
		return nil
	})
	return
}

// EnqueueFactGroups 将有变化的事实装成有界批次。正文与所有 checkpoint 一次提交，
// 避免历史补传逐条上报的生产速度超过上传速度，也避免半页成功推进游标。
func (s *State) EnqueueFactGroups(ctx context.Context, partition string, groups []FactsGroup, priorities ...int) (changed bool, err error) {
	priority := 1
	if len(priorities) > 0 {
		priority = priorities[0]
	}
	if len(groups) > 1000 {
		return false, ErrQueueFull
	}
	err = s.db.Write(ctx, func(_ context.Context, db *gorm.DB) error {
		checkpoints := []factsCheckpoint{}
		batches := []reportingv1.Batch{}
		current := reportingv1.Batch{Version: 1, ID: "00000000-0000-0000-0000-000000000000"}
		seen := map[string]string{}
		for _, group := range groups {
			if !boundedFactsKey(group.Key) || len(group.Batch.Sessions) != 0 {
				return ErrProtocol
			}
			batch := group.Batch
			batch.Version = 1
			batch.ID = "00000000-0000-0000-0000-000000000000"
			if batch.Validate() != nil {
				return ErrProtocol
			}
			body, err := json.Marshal(batch)
			if err != nil {
				return ErrProtocol
			}
			sum := sha256.Sum256(body)
			digest := hex.EncodeToString(sum[:])
			if earlier, ok := seen[group.Key]; ok {
				if earlier != digest {
					return ErrProtocol
				}
				continue
			}
			seen[group.Key] = digest
			var previous factsCheckpoint
			result := db.Where("partition = ? AND source_key = ?", partition, group.Key).Take(&previous)
			if result.Error != nil && !errors.Is(result.Error, gorm.ErrRecordNotFound) {
				return result.Error
			}
			if previous.Digest == digest {
				continue
			}
			candidate := mergeFactBatch(current, batch)
			encoded, err := json.Marshal(candidate)
			if err != nil {
				return ErrProtocol
			}
			if candidate.Validate() != nil || len(encoded) > reportingv1.MaxBodyBytes {
				if len(current.Accounts)+len(current.Bindings)+len(current.Quotas)+len(current.Credits)+len(current.Status) == 0 {
					return ErrQueueFull
				}
				batches = append(batches, current)
				current = batch
			} else {
				current = candidate
			}
			checkpoints = append(checkpoints, factsCheckpoint{Partition: partition, SourceKey: group.Key, Digest: digest})
		}
		if len(checkpoints) == 0 {
			return nil
		}
		if len(current.Accounts)+len(current.Bindings)+len(current.Quotas)+len(current.Credits)+len(current.Status) > 0 {
			batches = append(batches, current)
		}
		rows := make([]queued, 0, len(batches))
		totalBytes := int64(0)
		for _, batch := range batches {
			batch.ID = uuid.NewString()
			body, err := json.Marshal(batch)
			if err != nil {
				return ErrProtocol
			}
			if len(body) > reportingv1.MaxBodyBytes {
				return ErrQueueFull
			}
			rows = append(rows, queued{Partition: partition, BatchID: batch.ID, Priority: priority, Body: body})
			totalBytes += int64(len(body))
		}
		var budget struct{ Count, Bytes int64 }
		if err := db.Model(&queued{}).Select("COUNT(*) AS count,COALESCE(SUM(length(body)),0) AS bytes").Scan(&budget).Error; err != nil {
			return err
		}
		if budget.Count+int64(len(rows)) > QueueBatchBudget || budget.Bytes+totalBytes > QueueByteBudget {
			return ErrQueueFull
		}
		if err := db.CreateInBatches(rows, 32).Error; err != nil {
			return err
		}
		if err := db.Save(&checkpoints).Error; err != nil {
			return err
		}
		changed = true
		return nil
	})
	return
}
func mergeFactBatch(a, b reportingv1.Batch) reportingv1.Batch {
	a.Accounts = append([]reportingv1.Account(nil), a.Accounts...)
	a.Bindings = append([]reportingv1.AccountBinding(nil), a.Bindings...)
	a.Quotas = append(append([]reportingv1.QuotaObservation(nil), a.Quotas...), b.Quotas...)
	a.Credits = append(append([]reportingv1.ResetCredits(nil), a.Credits...), b.Credits...)
	a.Status = append(append([]reportingv1.DeviceStatus(nil), a.Status...), b.Status...)
	for _, account := range b.Accounts {
		found := false
		for i, old := range a.Accounts {
			if old.Provider == account.Provider && old.ID == account.ID {
				if old.CollectedAtMS <= account.CollectedAtMS {
					a.Accounts[i] = account
				}
				found = true
				break
			}
		}
		if !found {
			a.Accounts = append(a.Accounts, account)
		}
	}
	for _, binding := range b.Bindings {
		found := false
		for i, old := range a.Bindings {
			if old.Provider == binding.Provider && old.LocalScope == binding.LocalScope {
				if old.AccountID != binding.AccountID {
					a.Bindings = append(a.Bindings, binding)
					found = true
					break
				}
				if old.ConfirmedAtMS <= binding.ConfirmedAtMS {
					a.Bindings[i] = binding
				}
				found = true
				break
			}
		}
		if !found {
			a.Bindings = append(a.Bindings, binding)
		}
	}
	return a
}
