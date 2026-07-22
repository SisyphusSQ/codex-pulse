package store

import (
	"context"
	"encoding/hex"
	"errors"
	"strings"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"github.com/SisyphusSQ/codex-pulse/internal/attribution"
	storesqlite "github.com/SisyphusSQ/codex-pulse/internal/store/sqlite"
)

var ErrLightTokenConflict = errors.New("light token generation conflict")

type LightRolloutIdentity struct {
	Path              string
	SourceFileID      string
	Home              LightHomeIdentity
	DeviceID          string
	Inode             int64
	SizeBytes         int64
	MTimeNS           int64
	PrefixBytes       int64
	PrefixSHA256      string
	FingerprintSHA256 string
}

type LightTokenCheckpoint struct {
	DurableOffset      int64
	Complete           bool
	InputTokens        int64
	CachedInputTokens  int64
	OutputTokens       int64
	ReasoningTokens    int64
	CurrentModelKey    *string
	CurrentModelSource attribution.Source
	LatestEventAtMS    *int64
	PhysicalBytesRead  int64
	LinesSeen          int64
	CandidateLines     int64
	JSONDecoded        int64
}

type LightTokenDailyDelta struct {
	DayStartMS        int64
	InputTokens       int64
	CachedInputTokens int64
	OutputTokens      int64
	ReasoningTokens   int64
}

type LightTokenBatch struct {
	SessionID   string
	Generation  int64
	Checkpoint  LightTokenCheckpoint
	DailyDeltas []LightTokenDailyDelta
	TimedDeltas []LightTokenTimedDelta
	Activate    bool
	UpdatedAtMS int64
}

type LightTokenTimedDelta struct {
	SourceOffset      int64
	ObservedAtMS      int64
	ModelKey          *string
	ModelSource       attribution.Source
	InputTokens       int64
	CachedInputTokens int64
	OutputTokens      int64
	ReasoningTokens   int64
}

type LightTokenScan struct {
	SessionID     string
	Generation    int64
	Identity      LightRolloutIdentity
	ParserVersion string
	Checkpoint    LightTokenCheckpoint
	State         string
	UpdatedAtMS   int64
}

type LightSessionTokenUsage struct {
	SessionID         string
	Generation        int64
	InputTokens       int64
	CachedInputTokens int64
	OutputTokens      int64
	ReasoningTokens   int64
	LatestEventAtMS   *int64
	Complete          bool
}

type LightSessionTokenDaily struct {
	SessionID         string
	Generation        int64
	DayStartMS        int64
	InputTokens       int64
	CachedInputTokens int64
	OutputTokens      int64
	ReasoningTokens   int64
}

type LightSessionTokenTimed struct {
	SessionID         string
	Generation        int64
	SourceOffset      int64
	ObservedAtMS      int64
	ModelKey          *string
	ModelSource       attribution.Source
	InputTokens       int64
	CachedInputTokens int64
	OutputTokens      int64
	ReasoningTokens   int64
}

type lightTokenScanModel struct {
	SessionID          string  `gorm:"column:session_id;primaryKey"`
	Generation         int64   `gorm:"column:generation;primaryKey"`
	RolloutPath        string  `gorm:"column:rollout_path"`
	SourceFileID       string  `gorm:"column:source_file_id"`
	HomePath           string  `gorm:"column:home_path"`
	HomeDeviceID       string  `gorm:"column:home_device_id"`
	HomeInode          int64   `gorm:"column:home_inode"`
	FileDeviceID       string  `gorm:"column:file_device_id"`
	FileInode          int64   `gorm:"column:file_inode"`
	FileSizeBytes      int64   `gorm:"column:file_size_bytes"`
	FileMTimeNS        int64   `gorm:"column:file_mtime_ns"`
	PrefixBytes        int64   `gorm:"column:prefix_bytes"`
	PrefixSHA256       string  `gorm:"column:prefix_sha256"`
	FingerprintSHA256  string  `gorm:"column:fingerprint_sha256"`
	ParserVersion      string  `gorm:"column:parser_version"`
	DurableOffset      int64   `gorm:"column:durable_offset"`
	Complete           bool    `gorm:"column:complete"`
	InputTokens        int64   `gorm:"column:input_tokens"`
	CachedInputTokens  int64   `gorm:"column:cached_input_tokens"`
	OutputTokens       int64   `gorm:"column:output_tokens"`
	ReasoningTokens    int64   `gorm:"column:reasoning_tokens"`
	CurrentModelKey    *string `gorm:"column:current_model_key;type:TEXT CHECK (current_model_key IS NULL OR (length(current_model_key) BETWEEN 1 AND 128))"`
	CurrentModelSource string  `gorm:"column:current_model_source;type:TEXT NOT NULL DEFAULT 'missing' CHECK (current_model_source IN ('model_canonical','model_alias','missing','invalid_model'))"`
	LatestEventAtMS    *int64  `gorm:"column:latest_event_at_ms"`
	PhysicalBytesRead  int64   `gorm:"column:physical_bytes_read"`
	LinesSeen          int64   `gorm:"column:lines_seen"`
	CandidateLines     int64   `gorm:"column:candidate_lines"`
	JSONDecoded        int64   `gorm:"column:json_decoded"`
	State              string  `gorm:"column:state"`
	UpdatedAtMS        int64   `gorm:"column:updated_at_ms"`
}

func (lightTokenScanModel) TableName() string { return "light_token_scans" }

type lightTokenDailyModel struct {
	SessionID         string `gorm:"column:session_id;primaryKey"`
	Generation        int64  `gorm:"column:generation;primaryKey"`
	DayStartMS        int64  `gorm:"column:day_start_ms;primaryKey"`
	InputTokens       int64  `gorm:"column:input_tokens"`
	CachedInputTokens int64  `gorm:"column:cached_input_tokens"`
	OutputTokens      int64  `gorm:"column:output_tokens"`
	ReasoningTokens   int64  `gorm:"column:reasoning_tokens"`
}

func (lightTokenDailyModel) TableName() string { return "light_token_daily" }

type lightTokenTimedModel struct {
	SessionID         string  `gorm:"column:session_id;primaryKey"`
	Generation        int64   `gorm:"column:generation;primaryKey"`
	SourceOffset      int64   `gorm:"column:source_offset;primaryKey"`
	ObservedAtMS      int64   `gorm:"column:observed_at_ms"`
	ModelKey          *string `gorm:"column:model_key;type:TEXT CHECK (model_key IS NULL OR (length(model_key) BETWEEN 1 AND 128))"`
	ModelSource       string  `gorm:"column:model_source;type:TEXT NOT NULL DEFAULT 'missing' CHECK (model_source IN ('model_canonical','model_alias','missing','invalid_model'))"`
	InputTokens       int64   `gorm:"column:input_tokens"`
	CachedInputTokens int64   `gorm:"column:cached_input_tokens"`
	OutputTokens      int64   `gorm:"column:output_tokens"`
	ReasoningTokens   int64   `gorm:"column:reasoning_tokens"`
}

func (lightTokenTimedModel) TableName() string { return "light_token_timed" }

func (repository *Repository) StartLightTokenRebuild(
	ctx context.Context,
	sessionID string,
	identity LightRolloutIdentity,
	parserVersion string,
	startedAtMS int64,
) (int64, error) {
	if repository == nil || repository.database == nil {
		return 0, ErrInvalidRepository
	}
	if err := validateLightRolloutIdentity(sessionID, identity, parserVersion, startedAtMS); err != nil {
		return 0, err
	}
	var generation int64
	err := repository.database.Write(ctx, func(ctx context.Context, transaction storesqlite.WriteTx) error {
		var state lightIndexStateModel
		if err := transaction.WithContext(ctx).Where("state_id = 1").Take(&state).Error; err != nil {
			return err
		}
		if state.HomePath != identity.Home.Path || state.HomeDeviceID != identity.Home.DeviceID || state.HomeInode != identity.Home.Inode {
			return ErrLightHomeFence
		}
		var session lightSessionModel
		if err := transaction.WithContext(ctx).Where("session_id = ?", sessionID).Take(&session).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return ErrNotFound
			}
			return err
		}
		if session.RolloutPath == nil || *session.RolloutPath != identity.Path || session.PendingGeneration != nil {
			return ErrLightTokenConflict
		}
		generation = session.ActiveTokenGeneration + 1
		if generation <= 0 {
			return invalidRecord("light token generation overflow")
		}
		model := lightTokenScanModel{
			SessionID: sessionID, Generation: generation, RolloutPath: identity.Path, SourceFileID: identity.SourceFileID,
			HomePath: identity.Home.Path, HomeDeviceID: identity.Home.DeviceID, HomeInode: identity.Home.Inode,
			FileDeviceID: identity.DeviceID, FileInode: identity.Inode, FileSizeBytes: identity.SizeBytes,
			FileMTimeNS: identity.MTimeNS, PrefixBytes: identity.PrefixBytes, PrefixSHA256: identity.PrefixSHA256,
			FingerprintSHA256: identity.FingerprintSHA256,
			ParserVersion:     parserVersion, CurrentModelSource: string(attribution.SourceMissing),
			State: "building", UpdatedAtMS: startedAtMS,
		}
		if err := transaction.WithContext(ctx).Create(&model).Error; err != nil {
			return err
		}
		session.PendingGeneration = &generation
		session.ScanState = "scanning"
		session.RowUpdatedAtMS = startedAtMS
		if err := transaction.WithContext(ctx).Save(&session).Error; err != nil {
			return err
		}
		state.TokenScanGeneration++
		state.TokenScanState = "running"
		state.TokenScanStartedAtMS = &startedAtMS
		state.TokenScanFinishedAtMS = nil
		state.UpdatedAtMS = startedAtMS
		return transaction.WithContext(ctx).Save(&state).Error
	})
	return generation, err
}

func (repository *Repository) StartLightTokenAppend(
	ctx context.Context,
	sessionID string,
	identity LightRolloutIdentity,
	parserVersion string,
	startedAtMS int64,
) (int64, error) {
	if repository == nil || repository.database == nil {
		return 0, ErrInvalidRepository
	}
	if err := validateLightRolloutIdentity(sessionID, identity, parserVersion, startedAtMS); err != nil {
		return 0, err
	}
	var generation int64
	err := repository.database.Write(ctx, func(ctx context.Context, transaction storesqlite.WriteTx) error {
		var state lightIndexStateModel
		if err := transaction.WithContext(ctx).Where("state_id = 1").Take(&state).Error; err != nil {
			return err
		}
		if state.HomePath != identity.Home.Path || state.HomeDeviceID != identity.Home.DeviceID || state.HomeInode != identity.Home.Inode {
			return ErrLightHomeFence
		}
		var session lightSessionModel
		if err := transaction.WithContext(ctx).Where("session_id = ?", sessionID).Take(&session).Error; err != nil {
			return err
		}
		if session.ActiveTokenGeneration <= 0 || session.PendingGeneration != nil ||
			session.RolloutPath == nil || *session.RolloutPath != identity.Path {
			return ErrLightTokenConflict
		}
		generation = session.ActiveTokenGeneration
		var scan lightTokenScanModel
		if err := transaction.WithContext(ctx).
			Where("session_id = ? AND generation = ?", sessionID, generation).Take(&scan).Error; err != nil {
			return err
		}
		if scan.State != "active" || scan.ParserVersion != parserVersion || scan.RolloutPath != identity.Path ||
			scan.SourceFileID != identity.SourceFileID || scan.FileDeviceID != identity.DeviceID ||
			scan.FileInode != identity.Inode || identity.SizeBytes <= scan.FileSizeBytes ||
			identity.MTimeNS < scan.FileMTimeNS || scan.PrefixBytes != identity.PrefixBytes ||
			scan.PrefixSHA256 != identity.PrefixSHA256 {
			return ErrLightTokenConflict
		}
		scan.FileSizeBytes = identity.SizeBytes
		scan.FileMTimeNS = identity.MTimeNS
		scan.FingerprintSHA256 = identity.FingerprintSHA256
		scan.Complete = false
		scan.State = "building"
		scan.UpdatedAtMS = startedAtMS
		if err := transaction.WithContext(ctx).Save(&scan).Error; err != nil {
			return err
		}
		session.PendingGeneration = &generation
		session.ScanState = "scanning"
		session.RowUpdatedAtMS = startedAtMS
		if err := transaction.WithContext(ctx).Save(&session).Error; err != nil {
			return err
		}
		state.TokenScanGeneration++
		state.TokenScanState = "running"
		state.TokenScanStartedAtMS = &startedAtMS
		state.TokenScanFinishedAtMS = nil
		state.UpdatedAtMS = startedAtMS
		return transaction.WithContext(ctx).Save(&state).Error
	})
	return generation, err
}

func (repository *Repository) CommitLightTokenBatch(ctx context.Context, batch LightTokenBatch) error {
	if repository == nil || repository.database == nil {
		return ErrInvalidRepository
	}
	if err := validateLightTokenBatch(batch); err != nil {
		return err
	}
	return repository.database.Write(ctx, func(ctx context.Context, transaction storesqlite.WriteTx) error {
		var session lightSessionModel
		if err := transaction.WithContext(ctx).Where("session_id = ?", batch.SessionID).Take(&session).Error; err != nil {
			return err
		}
		if session.PendingGeneration == nil || *session.PendingGeneration != batch.Generation {
			return ErrLightTokenConflict
		}
		var scan lightTokenScanModel
		if err := transaction.WithContext(ctx).
			Where("session_id = ? AND generation = ?", batch.SessionID, batch.Generation).Take(&scan).Error; err != nil {
			return err
		}
		if err := validateLightCheckpointAdvance(scan, batch.Checkpoint); err != nil {
			return err
		}
		for _, delta := range batch.DailyDeltas {
			model := lightTokenDailyModel{
				SessionID: batch.SessionID, Generation: batch.Generation, DayStartMS: delta.DayStartMS,
				InputTokens: delta.InputTokens, CachedInputTokens: delta.CachedInputTokens,
				OutputTokens: delta.OutputTokens, ReasoningTokens: delta.ReasoningTokens,
			}
			if err := transaction.WithContext(ctx).Clauses(clause.OnConflict{
				Columns: []clause.Column{{Name: "session_id"}, {Name: "generation"}, {Name: "day_start_ms"}},
				DoUpdates: clause.Assignments(map[string]any{
					"input_tokens":        gorm.Expr("light_token_daily.input_tokens + excluded.input_tokens"),
					"cached_input_tokens": gorm.Expr("light_token_daily.cached_input_tokens + excluded.cached_input_tokens"),
					"output_tokens":       gorm.Expr("light_token_daily.output_tokens + excluded.output_tokens"),
					"reasoning_tokens":    gorm.Expr("light_token_daily.reasoning_tokens + excluded.reasoning_tokens"),
				}),
			}).Create(&model).Error; err != nil {
				return err
			}
		}
		for _, delta := range batch.TimedDeltas {
			model := lightTokenTimedModel{
				SessionID: batch.SessionID, Generation: batch.Generation,
				SourceOffset: delta.SourceOffset, ObservedAtMS: delta.ObservedAtMS,
				ModelKey: cloneLightString(delta.ModelKey), ModelSource: string(lightModelSource(delta.ModelSource)),
				InputTokens: delta.InputTokens, CachedInputTokens: delta.CachedInputTokens,
				OutputTokens: delta.OutputTokens, ReasoningTokens: delta.ReasoningTokens,
			}
			if err := transaction.WithContext(ctx).Clauses(clause.OnConflict{DoNothing: true}).Create(&model).Error; err != nil {
				return err
			}
		}
		applyLightCheckpoint(&scan, batch.Checkpoint, batch.UpdatedAtMS)
		if batch.Activate {
			if !batch.Checkpoint.Complete || batch.Checkpoint.DurableOffset != scan.FileSizeBytes {
				return invalidRecord("active light token generation must be complete")
			}
			scan.State = "active"
		}
		if err := transaction.WithContext(ctx).Save(&scan).Error; err != nil {
			return err
		}
		if !batch.Activate {
			session.ScanState = "partial"
			session.RowUpdatedAtMS = batch.UpdatedAtMS
			return transaction.WithContext(ctx).Save(&session).Error
		}
		session.ActiveTokenGeneration = batch.Generation
		session.PendingGeneration = nil
		session.ScanState = "complete"
		session.RowUpdatedAtMS = batch.UpdatedAtMS
		if err := transaction.WithContext(ctx).Save(&session).Error; err != nil {
			return err
		}
		return transaction.WithContext(ctx).
			Where("session_id = ? AND generation <> ?", batch.SessionID, batch.Generation).
			Delete(&lightTokenScanModel{}).Error
	})
}

func (repository *Repository) PendingLightTokenScan(ctx context.Context, sessionID string) (LightTokenScan, error) {
	if repository == nil || repository.database == nil || sessionID == "" {
		return LightTokenScan{}, ErrInvalidRepository
	}
	var scan lightTokenScanModel
	err := repository.database.View(ctx, func(ctx context.Context, connection storesqlite.ReadConn) error {
		result := connection.WithContext(ctx).Table("light_token_scans AS scans").
			Select("scans.*").
			Joins("JOIN light_sessions AS sessions ON sessions.session_id = scans.session_id AND sessions.pending_token_generation = scans.generation").
			Where("scans.session_id = ?", sessionID).Take(&scan)
		if errors.Is(result.Error, gorm.ErrRecordNotFound) {
			return ErrNotFound
		}
		return result.Error
	})
	return lightTokenScanFromModel(scan), err
}

func (repository *Repository) ActiveLightTokenScan(ctx context.Context, sessionID string) (LightTokenScan, error) {
	if repository == nil || repository.database == nil || sessionID == "" {
		return LightTokenScan{}, ErrInvalidRepository
	}
	var scan lightTokenScanModel
	err := repository.database.View(ctx, func(ctx context.Context, connection storesqlite.ReadConn) error {
		result := connection.WithContext(ctx).Table("light_token_scans AS scans").
			Select("scans.*").
			Joins("JOIN light_sessions AS sessions ON sessions.session_id = scans.session_id AND sessions.active_token_generation = scans.generation").
			Where("scans.session_id = ?", sessionID).Take(&scan)
		if errors.Is(result.Error, gorm.ErrRecordNotFound) {
			return ErrNotFound
		}
		return result.Error
	})
	return lightTokenScanFromModel(scan), err
}

func (repository *Repository) LightSessionTokenUsage(ctx context.Context, sessionID string) (LightSessionTokenUsage, error) {
	if repository == nil || repository.database == nil || sessionID == "" {
		return LightSessionTokenUsage{}, ErrInvalidRepository
	}
	var scan lightTokenScanModel
	err := repository.database.View(ctx, func(ctx context.Context, connection storesqlite.ReadConn) error {
		result := connection.WithContext(ctx).Table("light_token_scans AS scans").
			Select("scans.*").
			Joins("JOIN light_sessions AS sessions ON sessions.session_id = scans.session_id AND sessions.active_token_generation = scans.generation").
			Where("scans.session_id = ?", sessionID).Take(&scan)
		if errors.Is(result.Error, gorm.ErrRecordNotFound) {
			return ErrNotFound
		}
		return result.Error
	})
	if err != nil {
		return LightSessionTokenUsage{}, err
	}
	return LightSessionTokenUsage{
		SessionID: scan.SessionID, Generation: scan.Generation, InputTokens: scan.InputTokens,
		CachedInputTokens: scan.CachedInputTokens, OutputTokens: scan.OutputTokens,
		ReasoningTokens: scan.ReasoningTokens, LatestEventAtMS: scan.LatestEventAtMS, Complete: scan.Complete,
	}, nil
}

func (repository *Repository) LightSessionTokenDaily(ctx context.Context, sessionID string) ([]LightSessionTokenDaily, error) {
	if repository == nil || repository.database == nil || sessionID == "" {
		return nil, ErrInvalidRepository
	}
	var models []lightTokenDailyModel
	err := repository.database.View(ctx, func(ctx context.Context, connection storesqlite.ReadConn) error {
		return connection.WithContext(ctx).Table("light_token_daily AS daily").
			Select("daily.*").
			Joins("JOIN light_sessions AS sessions ON sessions.session_id = daily.session_id AND sessions.active_token_generation = daily.generation").
			Where("daily.session_id = ?", sessionID).Order("daily.day_start_ms").Find(&models).Error
	})
	if err != nil {
		return nil, err
	}
	output := make([]LightSessionTokenDaily, 0, len(models))
	for _, model := range models {
		output = append(output, LightSessionTokenDaily{
			SessionID: model.SessionID, Generation: model.Generation, DayStartMS: model.DayStartMS,
			InputTokens: model.InputTokens, CachedInputTokens: model.CachedInputTokens,
			OutputTokens: model.OutputTokens, ReasoningTokens: model.ReasoningTokens,
		})
	}
	return output, nil
}

func (repository *Repository) LightSessionTokenTimed(
	ctx context.Context,
	sessionID string,
	startAtMS int64,
	endAtMS int64,
) ([]LightSessionTokenTimed, error) {
	if repository == nil || repository.database == nil || sessionID == "" || startAtMS < 0 || endAtMS <= startAtMS {
		return nil, ErrInvalidRepository
	}
	var models []lightTokenTimedModel
	err := repository.database.View(ctx, func(ctx context.Context, connection storesqlite.ReadConn) error {
		return connection.WithContext(ctx).Table("light_token_timed AS timed").
			Select("timed.*").
			Joins("JOIN light_sessions AS sessions ON sessions.session_id = timed.session_id AND sessions.active_token_generation = timed.generation").
			Where("timed.session_id = ? AND timed.observed_at_ms >= ? AND timed.observed_at_ms < ?", sessionID, startAtMS, endAtMS).
			Order("timed.observed_at_ms, timed.source_offset").Find(&models).Error
	})
	if err != nil {
		return nil, err
	}
	output := make([]LightSessionTokenTimed, 0, len(models))
	for _, model := range models {
		output = append(output, LightSessionTokenTimed{
			SessionID: model.SessionID, Generation: model.Generation, SourceOffset: model.SourceOffset,
			ObservedAtMS: model.ObservedAtMS, ModelKey: cloneLightString(model.ModelKey),
			ModelSource: attribution.Source(model.ModelSource), InputTokens: model.InputTokens,
			CachedInputTokens: model.CachedInputTokens, OutputTokens: model.OutputTokens,
			ReasoningTokens: model.ReasoningTokens,
		})
	}
	return output, nil
}

func validateLightRolloutIdentity(sessionID string, identity LightRolloutIdentity, parserVersion string, timestampMS int64) error {
	prefixDigest, prefixDigestErr := hex.DecodeString(identity.PrefixSHA256)
	fingerprintDigest, fingerprintDigestErr := hex.DecodeString(identity.FingerprintSHA256)
	if sessionID == "" || identity.Path == "" || identity.SourceFileID == "" || identity.Home.Path == "" || identity.Home.DeviceID == "" ||
		identity.Home.Inode <= 0 || identity.DeviceID == "" || identity.Inode <= 0 || identity.SizeBytes < 0 ||
		identity.PrefixBytes < 0 || identity.PrefixBytes > identity.SizeBytes || prefixDigestErr != nil || len(prefixDigest) != 32 ||
		fingerprintDigestErr != nil || len(fingerprintDigest) != 32 ||
		strings.TrimSpace(parserVersion) == "" || timestampMS < 0 {
		return invalidRecord("invalid light rollout identity")
	}
	return nil
}

func validateLightTokenBatch(batch LightTokenBatch) error {
	checkpoint := batch.Checkpoint
	if batch.SessionID == "" || batch.Generation <= 0 || batch.UpdatedAtMS < 0 || checkpoint.DurableOffset < 0 ||
		checkpoint.InputTokens < 0 || checkpoint.CachedInputTokens < 0 || checkpoint.OutputTokens < 0 ||
		checkpoint.ReasoningTokens < 0 || checkpoint.PhysicalBytesRead < 0 || checkpoint.LinesSeen < 0 ||
		checkpoint.CandidateLines < 0 || checkpoint.JSONDecoded < 0 ||
		!validLightModelAttribution(checkpoint.CurrentModelKey, checkpoint.CurrentModelSource) ||
		(checkpoint.LatestEventAtMS != nil && *checkpoint.LatestEventAtMS < 0) {
		return invalidRecord("invalid light token batch")
	}
	for _, delta := range batch.DailyDeltas {
		if delta.DayStartMS < 0 || delta.DayStartMS%86_400_000 != 0 || delta.InputTokens < 0 ||
			delta.CachedInputTokens < 0 || delta.OutputTokens < 0 || delta.ReasoningTokens < 0 {
			return invalidRecord("invalid light token daily delta")
		}
	}
	for _, delta := range batch.TimedDeltas {
		if delta.SourceOffset <= 0 || delta.SourceOffset > checkpoint.DurableOffset || delta.ObservedAtMS < 0 ||
			delta.InputTokens < 0 || delta.CachedInputTokens < 0 || delta.OutputTokens < 0 || delta.ReasoningTokens < 0 ||
			!validLightModelAttribution(delta.ModelKey, delta.ModelSource) {
			return invalidRecord("invalid light token timed delta")
		}
	}
	return nil
}

func validateLightCheckpointAdvance(previous lightTokenScanModel, current LightTokenCheckpoint) error {
	if current.DurableOffset < previous.DurableOffset || current.DurableOffset > previous.FileSizeBytes ||
		current.InputTokens < previous.InputTokens || current.CachedInputTokens < previous.CachedInputTokens ||
		current.OutputTokens < previous.OutputTokens || current.ReasoningTokens < previous.ReasoningTokens ||
		current.PhysicalBytesRead < previous.PhysicalBytesRead || current.LinesSeen < previous.LinesSeen ||
		current.CandidateLines < previous.CandidateLines || current.JSONDecoded < previous.JSONDecoded {
		return ErrLightTokenConflict
	}
	return nil
}

func applyLightCheckpoint(model *lightTokenScanModel, checkpoint LightTokenCheckpoint, updatedAtMS int64) {
	model.DurableOffset = checkpoint.DurableOffset
	model.Complete = checkpoint.Complete
	model.InputTokens = checkpoint.InputTokens
	model.CachedInputTokens = checkpoint.CachedInputTokens
	model.OutputTokens = checkpoint.OutputTokens
	model.ReasoningTokens = checkpoint.ReasoningTokens
	model.CurrentModelKey = cloneLightString(checkpoint.CurrentModelKey)
	model.CurrentModelSource = string(lightModelSource(checkpoint.CurrentModelSource))
	model.LatestEventAtMS = checkpoint.LatestEventAtMS
	model.PhysicalBytesRead = checkpoint.PhysicalBytesRead
	model.LinesSeen = checkpoint.LinesSeen
	model.CandidateLines = checkpoint.CandidateLines
	model.JSONDecoded = checkpoint.JSONDecoded
	model.UpdatedAtMS = updatedAtMS
}

func lightTokenScanFromModel(model lightTokenScanModel) LightTokenScan {
	return LightTokenScan{
		SessionID: model.SessionID, Generation: model.Generation,
		Identity: LightRolloutIdentity{
			Path: model.RolloutPath, SourceFileID: model.SourceFileID,
			Home:     LightHomeIdentity{Path: model.HomePath, DeviceID: model.HomeDeviceID, Inode: model.HomeInode},
			DeviceID: model.FileDeviceID, Inode: model.FileInode, SizeBytes: model.FileSizeBytes,
			MTimeNS: model.FileMTimeNS, PrefixBytes: model.PrefixBytes, PrefixSHA256: model.PrefixSHA256,
			FingerprintSHA256: model.FingerprintSHA256,
		},
		ParserVersion: model.ParserVersion,
		Checkpoint: LightTokenCheckpoint{
			DurableOffset: model.DurableOffset, Complete: model.Complete,
			InputTokens: model.InputTokens, CachedInputTokens: model.CachedInputTokens,
			OutputTokens: model.OutputTokens, ReasoningTokens: model.ReasoningTokens,
			CurrentModelKey:    cloneLightString(model.CurrentModelKey),
			CurrentModelSource: attribution.Source(model.CurrentModelSource),
			LatestEventAtMS:    model.LatestEventAtMS, PhysicalBytesRead: model.PhysicalBytesRead,
			LinesSeen: model.LinesSeen, CandidateLines: model.CandidateLines, JSONDecoded: model.JSONDecoded,
		},
		State: model.State, UpdatedAtMS: model.UpdatedAtMS,
	}
}

func lightModelSource(source attribution.Source) attribution.Source {
	if source == "" {
		return attribution.SourceMissing
	}
	return source
}

func validLightModelAttribution(modelKey *string, source attribution.Source) bool {
	source = lightModelSource(source)
	switch source {
	case attribution.SourceModelCanonical, attribution.SourceModelAlias:
		return modelKey != nil && attribution.NormalizeModel(*modelKey).Key == *modelKey
	case attribution.SourceMissing, attribution.SourceInvalidModel:
		return modelKey == nil
	default:
		return false
	}
}

func cloneLightString(value *string) *string {
	if value == nil {
		return nil
	}
	cloned := *value
	return &cloned
}
