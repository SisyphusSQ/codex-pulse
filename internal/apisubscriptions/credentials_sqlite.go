package apisubscriptions

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	storeschema "github.com/SisyphusSQ/codex-pulse/internal/store/schema"
	storesqlite "github.com/SisyphusSQ/codex-pulse/internal/store/sqlite"
)

const maximumStoredAPIKeyBytes = 4_096

var (
	ErrCredentialStore   = errors.New("API subscription credential store is unavailable")
	ErrInvalidCredential = errors.New("API subscription credential is invalid")
)

// CredentialStatus reports configuration state without exposing credential values.
type CredentialStatus struct {
	DeepSeekConfigured   bool
	OpenCodeGoConfigured bool
}

// SQLiteCredentialStoreConfig configures the Helper-owned private credential database.
type SQLiteCredentialStoreConfig struct {
	Store storesqlite.Config
	Now   func() time.Time
}

// SQLiteCredentialStore owns credential persistence and the Helper's in-memory copies.
type SQLiteCredentialStore struct {
	database *storesqlite.Store
	now      func() time.Time

	mu          sync.RWMutex
	credentials map[string]MemoryAPICredential
	closed      bool
}

type sqliteCredentialModel struct {
	Service         string `gorm:"column:service;primaryKey"`
	Secret          []byte `gorm:"column:secret"`
	CredentialEpoch string `gorm:"column:credential_epoch"`
	UpdatedAtMS     int64  `gorm:"column:updated_at_ms"`
}

func (sqliteCredentialModel) TableName() string { return "api_subscription_credentials" }

var credentialSchemaObjects = []storeschema.Object{
	{
		ObjectType: "table",
		Name:       "api_subscription_credentials",
		Statement: `CREATE TABLE IF NOT EXISTS api_subscription_credentials (
			service TEXT PRIMARY KEY CHECK (service IN ('deepseek', 'opencode_go')),
			secret BLOB NOT NULL CHECK (length(secret) BETWEEN 1 AND 4096),
			credential_epoch TEXT NOT NULL CHECK (length(credential_epoch) BETWEEN 1 AND 128),
			updated_at_ms INTEGER NOT NULL CHECK (updated_at_ms >= 0)
		) STRICT`,
	},
}

// OpenSQLiteCredentialStore opens and verifies the private credential database.
func OpenSQLiteCredentialStore(
	ctx context.Context,
	config SQLiteCredentialStoreConfig,
) (*SQLiteCredentialStore, error) {
	if ctx == nil {
		return nil, ErrCredentialStore
	}
	now := config.Now
	if now == nil {
		now = time.Now
	}
	database, err := storesqlite.Open(ctx, config.Store)
	if err != nil {
		return nil, fmt.Errorf("%w: open database: %w", ErrCredentialStore, err)
	}
	closeOnError := func(cause error) (*SQLiteCredentialStore, error) {
		return nil, errors.Join(cause, database.Close(context.WithoutCancel(ctx)))
	}
	if err := database.Write(ctx, func(ctx context.Context, transaction *gorm.DB) error {
		return storeschema.EnsureObjects(ctx, transaction, credentialSchemaObjects)
	}); err != nil {
		return closeOnError(fmt.Errorf("%w: ensure schema: %w", ErrCredentialStore, err))
	}
	loaded := make(map[string]MemoryAPICredential, 2)
	if err := database.View(ctx, func(ctx context.Context, connection *gorm.DB) error {
		var rows []sqliteCredentialModel
		if err := connection.WithContext(ctx).Order("service").Find(&rows).Error; err != nil {
			return err
		}
		for _, row := range rows {
			if !validService(row.Service) || !validStoredSecret(row.Secret) ||
				!validCredentialEpoch(row.CredentialEpoch) || row.UpdatedAtMS < 0 {
				return ErrInvalidCredential
			}
			loaded[row.Service] = MemoryAPICredential{
				Key: append([]byte(nil), row.Secret...), Epoch: row.CredentialEpoch,
			}
			clear(row.Secret)
		}
		return nil
	}); err != nil {
		for _, credential := range loaded {
			clear(credential.Key)
		}
		return closeOnError(fmt.Errorf("%w: load credentials: %w", ErrCredentialStore, err))
	}
	return &SQLiteCredentialStore{database: database, now: now, credentials: loaded}, nil
}

// APIKey returns a caller-owned credential copy.
func (store *SQLiteCredentialStore) APIKey(service string) ([]byte, bool) {
	if store == nil {
		return nil, false
	}
	store.mu.RLock()
	defer store.mu.RUnlock()
	credential, ok := store.credentials[service]
	if store.closed || !ok || len(credential.Key) == 0 {
		return nil, false
	}
	return append([]byte(nil), credential.Key...), true
}

// CredentialEpoch returns the persisted generation associated with a credential.
func (store *SQLiteCredentialStore) CredentialEpoch(service string) (string, bool) {
	if store == nil {
		return "", false
	}
	store.mu.RLock()
	defer store.mu.RUnlock()
	credential, ok := store.credentials[service]
	if store.closed || !ok || len(credential.Key) == 0 || credential.Epoch == "" {
		return "", false
	}
	return credential.Epoch, true
}

// Status returns configuration state without reading credential values from SQLite again.
func (store *SQLiteCredentialStore) Status(ctx context.Context) (CredentialStatus, error) {
	if store == nil || ctx == nil {
		return CredentialStatus{}, ErrCredentialStore
	}
	if err := ctx.Err(); err != nil {
		return CredentialStatus{}, err
	}
	store.mu.RLock()
	defer store.mu.RUnlock()
	if store.closed {
		return CredentialStatus{}, ErrCredentialStore
	}
	return store.statusLocked(), nil
}

// Save replaces one service credential and rotates its persisted epoch.
func (store *SQLiteCredentialStore) Save(ctx context.Context, service string, secret []byte) error {
	if store == nil || ctx == nil || !validService(service) {
		return ErrInvalidCredential
	}
	normalized := append([]byte(nil), bytes.TrimSpace(secret)...)
	defer clear(normalized)
	if !validStoredSecret(normalized) {
		return ErrInvalidCredential
	}
	updatedAtMS := store.now().UnixMilli()
	if updatedAtMS < 0 {
		return ErrCredentialStore
	}
	credential := MemoryAPICredential{
		Key: append([]byte(nil), normalized...), Epoch: "db:" + uuid.NewString(),
	}
	store.mu.Lock()
	defer store.mu.Unlock()
	if store.closed {
		clear(credential.Key)
		return ErrCredentialStore
	}
	model := sqliteCredentialModel{
		Service: service, Secret: normalized,
		CredentialEpoch: credential.Epoch, UpdatedAtMS: updatedAtMS,
	}
	if err := store.database.Write(ctx, func(ctx context.Context, transaction *gorm.DB) error {
		return transaction.WithContext(ctx).Clauses(clause.OnConflict{
			Columns: []clause.Column{{Name: "service"}},
			DoUpdates: clause.AssignmentColumns([]string{
				"secret", "credential_epoch", "updated_at_ms",
			}),
		}).Create(&model).Error
	}); err != nil {
		clear(credential.Key)
		return fmt.Errorf("%w: save credential: %w", ErrCredentialStore, err)
	}
	if previous, ok := store.credentials[service]; ok {
		clear(previous.Key)
	}
	store.credentials[service] = credential
	return nil
}

// Delete removes one service credential. Repeated deletion succeeds.
func (store *SQLiteCredentialStore) Delete(ctx context.Context, service string) error {
	if store == nil || ctx == nil || !validService(service) {
		return ErrInvalidCredential
	}
	store.mu.Lock()
	defer store.mu.Unlock()
	if store.closed {
		return ErrCredentialStore
	}
	if err := store.database.Write(ctx, func(ctx context.Context, transaction *gorm.DB) error {
		return transaction.WithContext(ctx).
			Where("service = ?", service).
			Delete(&sqliteCredentialModel{}).Error
	}); err != nil {
		return fmt.Errorf("%w: delete credential: %w", ErrCredentialStore, err)
	}
	if previous, ok := store.credentials[service]; ok {
		clear(previous.Key)
		delete(store.credentials, service)
	}
	return nil
}

// Close clears in-memory secrets after closing the dedicated database.
func (store *SQLiteCredentialStore) Close(ctx context.Context) error {
	if store == nil || ctx == nil {
		return ErrCredentialStore
	}
	store.mu.Lock()
	defer store.mu.Unlock()
	if store.closed {
		return nil
	}
	if err := store.database.Close(ctx); err != nil {
		return fmt.Errorf("%w: close database: %w", ErrCredentialStore, err)
	}
	for service, credential := range store.credentials {
		clear(credential.Key)
		delete(store.credentials, service)
	}
	store.closed = true
	return nil
}

func (store *SQLiteCredentialStore) statusLocked() CredentialStatus {
	_, deepSeek := store.credentials[ServiceDeepSeek]
	_, openCodeGo := store.credentials[ServiceOpenCodeGo]
	return CredentialStatus{
		DeepSeekConfigured: deepSeek, OpenCodeGoConfigured: openCodeGo,
	}
}

func validStoredSecret(secret []byte) bool {
	return len(secret) > 0 && len(secret) <= maximumStoredAPIKeyBytes
}
