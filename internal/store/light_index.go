package store

import (
	"context"
	"errors"
	"path/filepath"
	"strings"
	"unicode/utf8"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	storesqlite "github.com/SisyphusSQ/codex-pulse/internal/store/sqlite"
)

var ErrLightHomeFence = errors.New("light index Home identity changed")

type LightHomeIdentity struct {
	Path     string
	DeviceID string
	Inode    int64
}

type LightSessionMetadata struct {
	SessionID          string
	ThreadName         *string
	CWD                string
	RolloutPath        *string
	CreatedAtMS        int64
	UpdatedAtMS        int64
	RecencyAtMS        *int64
	MetadataGeneration int64
	ActiveGeneration   int64
	PendingGeneration  *int64
	ScanState          string
}

type LightMetadataSnapshot struct {
	Home       LightHomeIdentity
	Generation int64
	ReadyAtMS  int64
	Sessions   []LightSessionMetadata
}

type LightIndexState struct {
	Home                  LightHomeIdentity
	MetadataGeneration    int64
	MetadataReadyAtMS     *int64
	TokenScanGeneration   int64
	TokenScanState        string
	TokenScanStartedAtMS  *int64
	TokenScanFinishedAtMS *int64
	UpdatedAtMS           int64
}

type lightIndexStateModel struct {
	StateID               int    `gorm:"column:state_id;primaryKey"`
	HomePath              string `gorm:"column:home_path"`
	HomeDeviceID          string `gorm:"column:home_device_id"`
	HomeInode             int64  `gorm:"column:home_inode"`
	MetadataGeneration    int64  `gorm:"column:metadata_generation"`
	MetadataReadyAtMS     *int64 `gorm:"column:metadata_ready_at_ms"`
	TokenScanGeneration   int64  `gorm:"column:token_scan_generation"`
	TokenScanState        string `gorm:"column:token_scan_state"`
	TokenScanStartedAtMS  *int64 `gorm:"column:token_scan_started_at_ms"`
	TokenScanFinishedAtMS *int64 `gorm:"column:token_scan_finished_at_ms"`
	UpdatedAtMS           int64  `gorm:"column:updated_at_ms"`
}

func (lightIndexStateModel) TableName() string { return "light_index_state" }

type lightSessionModel struct {
	SessionID             string  `gorm:"column:session_id;primaryKey"`
	ThreadName            *string `gorm:"column:thread_name"`
	CWD                   string  `gorm:"column:cwd"`
	RolloutPath           *string `gorm:"column:rollout_path"`
	CreatedAtMS           int64   `gorm:"column:created_at_ms"`
	UpdatedAtMS           int64   `gorm:"column:updated_at_ms"`
	RecencyAtMS           *int64  `gorm:"column:recency_at_ms"`
	MetadataGeneration    int64   `gorm:"column:metadata_generation"`
	ActiveTokenGeneration int64   `gorm:"column:active_token_generation"`
	PendingGeneration     *int64  `gorm:"column:pending_token_generation"`
	ScanState             string  `gorm:"column:scan_state"`
	RowUpdatedAtMS        int64   `gorm:"column:row_updated_at_ms"`
}

func (lightSessionModel) TableName() string { return "light_sessions" }

func (repository *Repository) ReplaceLightMetadata(ctx context.Context, snapshot LightMetadataSnapshot) error {
	return repository.replaceLightMetadata(ctx, snapshot, nil)
}

// ReplaceLightMetadataForHomeSwitch atomically fences the previously confirmed
// Home and replaces only the lightweight derived index. Rollout files are never
// modified; old session/token rows are removed through their foreign-key
// cascades before the new Home becomes visible.
func (repository *Repository) ReplaceLightMetadataForHomeSwitch(
	ctx context.Context,
	expected LightHomeIdentity,
	snapshot LightMetadataSnapshot,
) error {
	if err := validateLightHomeIdentity(expected); err != nil || expected == snapshot.Home {
		return invalidRecord("invalid light Home switch")
	}
	return repository.replaceLightMetadata(ctx, snapshot, &expected)
}

func (repository *Repository) replaceLightMetadata(
	ctx context.Context,
	snapshot LightMetadataSnapshot,
	expected *LightHomeIdentity,
) error {
	if repository == nil || repository.database == nil {
		return ErrInvalidRepository
	}
	if err := validateLightMetadataSnapshot(snapshot); err != nil {
		return err
	}
	return repository.database.Write(ctx, func(ctx context.Context, transaction storesqlite.WriteTx) error {
		var state lightIndexStateModel
		result := transaction.WithContext(ctx).Where("state_id = 1").Take(&state)
		switch {
		case result.Error == nil:
			current := LightHomeIdentity{Path: state.HomePath, DeviceID: state.HomeDeviceID, Inode: state.HomeInode}
			if expected != nil {
				if current != *expected {
					return ErrLightHomeFence
				}
				if err := transaction.WithContext(ctx).Where("1 = 1").Delete(&lightSessionModel{}).Error; err != nil {
					return err
				}
				state = lightIndexStateModel{
					StateID: 1, HomePath: snapshot.Home.Path, HomeDeviceID: snapshot.Home.DeviceID,
					HomeInode: snapshot.Home.Inode, TokenScanState: "idle",
				}
			} else {
				if current != snapshot.Home {
					return ErrLightHomeFence
				}
				if snapshot.Generation <= state.MetadataGeneration {
					return invalidRecord("light metadata generation must advance")
				}
			}
		case errors.Is(result.Error, gorm.ErrRecordNotFound):
			if expected != nil {
				return ErrLightHomeFence
			}
			state = lightIndexStateModel{
				StateID: 1, HomePath: snapshot.Home.Path, HomeDeviceID: snapshot.Home.DeviceID,
				HomeInode: snapshot.Home.Inode, TokenScanState: "idle",
			}
		default:
			return result.Error
		}

		for _, session := range snapshot.Sessions {
			model := lightSessionModel{
				SessionID: session.SessionID, ThreadName: session.ThreadName, CWD: session.CWD,
				RolloutPath: session.RolloutPath, CreatedAtMS: session.CreatedAtMS, UpdatedAtMS: session.UpdatedAtMS,
				RecencyAtMS: session.RecencyAtMS, MetadataGeneration: snapshot.Generation,
				ScanState: "pending", RowUpdatedAtMS: snapshot.ReadyAtMS,
			}
			if err := transaction.WithContext(ctx).Clauses(clause.OnConflict{
				Columns: []clause.Column{{Name: "session_id"}},
				DoUpdates: clause.AssignmentColumns([]string{
					"thread_name", "cwd", "rollout_path", "created_at_ms", "updated_at_ms",
					"recency_at_ms", "metadata_generation", "row_updated_at_ms",
				}),
			}).Create(&model).Error; err != nil {
				return err
			}
		}
		if err := transaction.WithContext(ctx).Where("metadata_generation <> ?", snapshot.Generation).
			Delete(&lightSessionModel{}).Error; err != nil {
			return err
		}
		state.MetadataGeneration = snapshot.Generation
		state.MetadataReadyAtMS = &snapshot.ReadyAtMS
		state.UpdatedAtMS = snapshot.ReadyAtMS
		return transaction.WithContext(ctx).Save(&state).Error
	})
}

func (repository *Repository) LightIndexState(ctx context.Context) (LightIndexState, error) {
	if repository == nil || repository.database == nil {
		return LightIndexState{}, ErrInvalidRepository
	}
	var model lightIndexStateModel
	err := repository.database.View(ctx, func(ctx context.Context, connection storesqlite.ReadConn) error {
		result := connection.WithContext(ctx).Where("state_id = 1").Take(&model)
		if errors.Is(result.Error, gorm.ErrRecordNotFound) {
			return ErrNotFound
		}
		return result.Error
	})
	if err != nil {
		return LightIndexState{}, err
	}
	return lightIndexStateFromModel(model), nil
}

func (repository *Repository) ListLightSessions(ctx context.Context) ([]LightSessionMetadata, error) {
	if repository == nil || repository.database == nil {
		return nil, ErrInvalidRepository
	}
	var models []lightSessionModel
	err := repository.database.View(ctx, func(ctx context.Context, connection storesqlite.ReadConn) error {
		return connection.WithContext(ctx).Order("updated_at_ms DESC, session_id").Find(&models).Error
	})
	if err != nil {
		return nil, err
	}
	output := make([]LightSessionMetadata, 0, len(models))
	for _, model := range models {
		output = append(output, lightSessionFromModel(model))
	}
	return output, nil
}

func validateLightMetadataSnapshot(snapshot LightMetadataSnapshot) error {
	if snapshot.Generation <= 0 || snapshot.ReadyAtMS < 0 || validateLightHomeIdentity(snapshot.Home) != nil {
		return invalidRecord("invalid light metadata snapshot")
	}
	seen := make(map[string]struct{}, len(snapshot.Sessions))
	for _, session := range snapshot.Sessions {
		if session.SessionID == "" || len(session.SessionID) > 4096 || !utf8.ValidString(session.SessionID) ||
			session.CreatedAtMS <= 0 || session.UpdatedAtMS <= 0 {
			return invalidRecord("invalid light session metadata")
		}
		if _, exists := seen[session.SessionID]; exists {
			return invalidRecord("duplicate light session metadata")
		}
		seen[session.SessionID] = struct{}{}
		if session.ThreadName != nil && (strings.TrimSpace(*session.ThreadName) == "" || !utf8.ValidString(*session.ThreadName)) {
			return invalidRecord("invalid light session name")
		}
		if session.RolloutPath != nil && !filepath.IsAbs(*session.RolloutPath) {
			return invalidRecord("invalid light rollout path")
		}
		if session.RecencyAtMS != nil && *session.RecencyAtMS <= 0 {
			return invalidRecord("invalid light session recency")
		}
	}
	return nil
}

func validateLightHomeIdentity(home LightHomeIdentity) error {
	if home.Path == "" || !filepath.IsAbs(home.Path) || home.DeviceID == "" || home.Inode <= 0 {
		return invalidRecord("invalid light Home identity")
	}
	return nil
}

func lightIndexStateFromModel(model lightIndexStateModel) LightIndexState {
	return LightIndexState{
		Home:               LightHomeIdentity{Path: model.HomePath, DeviceID: model.HomeDeviceID, Inode: model.HomeInode},
		MetadataGeneration: model.MetadataGeneration, MetadataReadyAtMS: model.MetadataReadyAtMS,
		TokenScanGeneration: model.TokenScanGeneration, TokenScanState: model.TokenScanState,
		TokenScanStartedAtMS: model.TokenScanStartedAtMS, TokenScanFinishedAtMS: model.TokenScanFinishedAtMS,
		UpdatedAtMS: model.UpdatedAtMS,
	}
}

func lightSessionFromModel(model lightSessionModel) LightSessionMetadata {
	return LightSessionMetadata{
		SessionID: model.SessionID, ThreadName: model.ThreadName, CWD: model.CWD, RolloutPath: model.RolloutPath,
		CreatedAtMS: model.CreatedAtMS, UpdatedAtMS: model.UpdatedAtMS, RecencyAtMS: model.RecencyAtMS,
		MetadataGeneration: model.MetadataGeneration, ActiveGeneration: model.ActiveTokenGeneration,
		PendingGeneration: model.PendingGeneration, ScanState: model.ScanState,
	}
}
