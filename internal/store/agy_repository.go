package store

import (
	"context"
	"encoding/json"
	"errors"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	reportingv1 "github.com/SisyphusSQ/codex-pulse/api/codexpulse/reporting/v1"
)

const defaultAGYQuotaObservationRetention = 256

type agySessionModel struct {
	ID                      int64   `gorm:"column:id;primaryKey;autoIncrement"`
	Provider                string  `gorm:"column:provider"`
	ExternalSessionID       string  `gorm:"column:external_session_id"`
	DisplayTitle            string  `gorm:"column:display_title"`
	TitleSource             string  `gorm:"column:title_source"`
	ParentExternalSessionID *string `gorm:"column:parent_external_session_id"`
	SessionRole             string  `gorm:"column:session_role"`
	ProjectKey              string  `gorm:"column:project_key"`
	ProjectDisplayName      string  `gorm:"column:project_display_name"`
	ProjectSource           string  `gorm:"column:project_source"`
	CreatedAtMS             int64   `gorm:"column:created_at_ms"`
	LastActivityAtMS        int64   `gorm:"column:last_activity_at_ms"`
	ModelKey                *string `gorm:"column:model_key"`
	RequestCount            int64   `gorm:"column:request_count"`
	LineageConflict         bool    `gorm:"column:lineage_conflict"`
	CoverageState           string  `gorm:"column:coverage_state"`
	Throughput              *string `gorm:"column:throughput"`
	UpdatedAtMS             int64   `gorm:"column:updated_at_ms"`
}

func (agySessionModel) TableName() string { return "agy_sessions" }

type agyLineageModel struct {
	SessionID     int64  `gorm:"column:session_id;primaryKey"`
	SourceKey     string `gorm:"column:source_key;primaryKey"`
	LineageKey    string `gorm:"column:lineage_key;primaryKey"`
	ContentDigest string `gorm:"column:content_digest"`
	ObservedAtMS  int64  `gorm:"column:observed_at_ms"`
}

func (agyLineageModel) TableName() string { return "agy_session_lineage" }

type agyUsageModel struct {
	EventID             string  `gorm:"column:event_id;primaryKey"`
	ExternalSessionID   string  `gorm:"column:external_session_id"`
	StepIndex           int64   `gorm:"column:step_index"`
	OccurredAtMS        int64   `gorm:"column:occurred_at_ms"`
	ModelKey            *string `gorm:"column:model_key"`
	UncachedInputTokens int64   `gorm:"column:uncached_input_tokens"`
	CacheReadTokens     int64   `gorm:"column:cache_read_tokens"`
	OutputTokens        int64   `gorm:"column:output_tokens"`
	ReasoningTokens     int64   `gorm:"column:reasoning_tokens"`
	TotalTokens         int64   `gorm:"column:total_tokens"`
	CacheReadKnown      bool    `gorm:"column:cache_read_known"`
	ReasoningKnown      bool    `gorm:"column:reasoning_known"`
	TotalKnown          bool    `gorm:"column:total_known"`
	StartedAtMS         *int64  `gorm:"column:started_at_ms"`
	EndedAtMS           *int64  `gorm:"column:ended_at_ms"`
	Provenance          string  `gorm:"column:provenance"`
	UpdatedAtMS         int64   `gorm:"column:updated_at_ms"`
}

func (agyUsageModel) TableName() string { return "agy_usage_events" }

type agyQuotaSnapshotModel struct {
	AccountScope  string  `gorm:"column:account_scope;primaryKey"`
	Generation    int64   `gorm:"column:generation"`
	PlanType      *string `gorm:"column:plan_type"`
	LimitID       string  `gorm:"column:limit_id;primaryKey"`
	LimitName     string  `gorm:"column:limit_name"`
	WindowKind    string  `gorm:"column:window_kind;primaryKey"`
	WindowMinutes int64   `gorm:"column:window_minutes"`
	UsedPercent   *int64  `gorm:"column:used_percent"`
	ResetsAtMS    *int64  `gorm:"column:resets_at_ms"`
	DataAsOfMS    int64   `gorm:"column:data_as_of_ms"`
	UpdatedAtMS   int64   `gorm:"column:updated_at_ms"`
}

func (agyQuotaSnapshotModel) TableName() string { return "agy_quota_snapshots" }

type agyQuotaObservationModel struct {
	AccountScope string `gorm:"column:account_scope;primaryKey"`
	LimitID      string `gorm:"column:limit_id;primaryKey"`
	WindowKind   string `gorm:"column:window_kind;primaryKey"`
	ObservedAtMS int64  `gorm:"column:observed_at_ms;primaryKey"`
	UsedPercent  int64  `gorm:"column:used_percent"`
	ResetsAtMS   *int64 `gorm:"column:resets_at_ms"`
}

func (agyQuotaObservationModel) TableName() string { return "agy_quota_observations" }

// ReplaceAGYSnapshot 原子写入 AGY 本地快照与来源状态；未出现在本次批次中的历史会话保持保留。
func (repository *Repository) ReplaceAGYSnapshot(ctx context.Context, snapshot AGYSnapshot) error {
	if repository == nil || repository.database == nil || ctx == nil {
		return ErrInvalidRepository
	}
	snapshot.Sessions = append([]AGYSession(nil), snapshot.Sessions...)
	for index := range snapshot.Sessions {
		normalizeAGYSessionPresentation(&snapshot.Sessions[index])
	}
	if err := validateAGYSnapshot(snapshot); err != nil {
		return err
	}
	return repository.database.Write(ctx, func(ctx context.Context, transaction *gorm.DB) error {
		database := transaction.WithContext(ctx)
		for _, session := range snapshot.Sessions {
			for _, model := range []any{&agyUsageModel{}, &agySessionModel{}} {
				if err := database.Where("external_session_id = ?", session.ExternalSessionID).Delete(model).Error; err != nil {
					return err
				}
			}
		}
		if err := database.Where("provider = ?", "agy").Delete(&cursorSourceModel{}).Error; err != nil {
			return err
		}
		if err := database.Save(&cursorSnapshotModel{
			Provider: "agy", Generation: snapshot.Generation, CollectedAtMS: snapshot.CollectedAtMS,
		}).Error; err != nil {
			return err
		}
		for _, source := range snapshot.Sources {
			if err := database.Create(&cursorSourceModel{
				Provider: source.Provider, SourceKey: source.SourceKey, SourceType: source.SourceType,
				State: source.State, CoverageState: source.CoverageState, SchemaVersion: source.SchemaVersion,
				CheckpointKind: source.CheckpointKind, CheckpointValue: source.CheckpointValue,
				RowCount: source.RowCount, LastAttemptAtMS: source.LastAttemptAtMS,
				LastSuccessAtMS: source.LastSuccessAtMS, FailureCode: source.FailureCode, UpdatedAtMS: source.UpdatedAtMS,
			}).Error; err != nil {
				return err
			}
		}
		sessionIDs := make(map[string]int64, len(snapshot.Sessions))
		for _, session := range snapshot.Sessions {
			var capsule *string
			if session.Throughput != nil {
				encoded, err := json.Marshal(session.Throughput)
				if err != nil {
					return err
				}
				value := string(encoded)
				capsule = &value
			}
			model := agySessionModel{
				Provider: "agy", ExternalSessionID: session.ExternalSessionID,
				DisplayTitle: session.DisplayTitle, TitleSource: session.TitleSource,
				ParentExternalSessionID: session.ParentExternalSessionID, SessionRole: session.SessionRole,
				ProjectKey: session.ProjectKey, ProjectDisplayName: session.ProjectDisplayName,
				ProjectSource: session.ProjectSource, CreatedAtMS: session.CreatedAtMS,
				LastActivityAtMS: session.LastActivityAtMS, ModelKey: session.ModelKey,
				RequestCount: session.RequestCount, LineageConflict: session.LineageConflict,
				CoverageState: session.CoverageState, Throughput: capsule, UpdatedAtMS: session.UpdatedAtMS,
			}
			if err := database.Create(&model).Error; err != nil {
				return err
			}
			sessionIDs[session.ExternalSessionID] = model.ID
		}
		for _, lineage := range snapshot.Lineage {
			sessionID, ok := sessionIDs[lineage.ExternalSessionID]
			if !ok {
				return invalidRecord("agy lineage session is missing")
			}
			if err := database.Create(&agyLineageModel{
				SessionID: sessionID, SourceKey: lineage.SourceKey, LineageKey: lineage.LineageKey,
				ContentDigest: lineage.ContentDigest, ObservedAtMS: lineage.ObservedAtMS,
			}).Error; err != nil {
				return err
			}
		}
		for _, event := range snapshot.UsageEvents {
			if err := database.Create(&agyUsageModel{
				EventID: event.EventID, ExternalSessionID: event.ExternalSessionID,
				StepIndex: event.StepIndex, OccurredAtMS: event.OccurredAtMS, ModelKey: event.ModelKey,
				UncachedInputTokens: event.UncachedInputTokens, CacheReadTokens: event.CacheReadTokens,
				OutputTokens: event.OutputTokens, ReasoningTokens: event.ReasoningTokens, TotalTokens: event.TotalTokens,
				CacheReadKnown: event.CacheReadKnown, ReasoningKnown: event.ReasoningKnown, TotalKnown: event.TotalKnown,
				StartedAtMS: event.StartedAtMS, EndedAtMS: event.EndedAtMS,
				Provenance: event.Provenance, UpdatedAtMS: event.UpdatedAtMS,
			}).Error; err != nil {
				return err
			}
		}
		return nil
	})
}

// AGYSnapshot 在一致性只读快照事务内读取 AGY 本地事实与来源状态。
func (repository *Repository) AGYSnapshot(ctx context.Context) (AGYSnapshot, error) {
	if repository == nil || repository.database == nil || ctx == nil {
		return AGYSnapshot{}, ErrInvalidRepository
	}
	var result AGYSnapshot
	err := repository.database.ViewSnapshot(ctx, func(ctx context.Context, database *gorm.DB) error {
		var snapshot cursorSnapshotModel
		if err := database.WithContext(ctx).Where("provider = ?", "agy").Take(&snapshot).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return ErrNotFound
			}
			return err
		}
		result.Generation, result.CollectedAtMS = snapshot.Generation, snapshot.CollectedAtMS
		var sources []cursorSourceModel
		if err := database.WithContext(ctx).Where("provider = ?", "agy").Order("source_key").Find(&sources).Error; err != nil {
			return err
		}
		for _, source := range sources {
			result.Sources = append(result.Sources, CursorSourceStatus{
				Provider: source.Provider, SourceKey: source.SourceKey, SourceType: source.SourceType,
				State: source.State, CoverageState: source.CoverageState, SchemaVersion: source.SchemaVersion,
				CheckpointKind: source.CheckpointKind, CheckpointValue: source.CheckpointValue,
				RowCount: source.RowCount, LastAttemptAtMS: source.LastAttemptAtMS,
				LastSuccessAtMS: source.LastSuccessAtMS, FailureCode: source.FailureCode, UpdatedAtMS: source.UpdatedAtMS,
			})
		}
		var sessions []agySessionModel
		if err := database.WithContext(ctx).Order("last_activity_at_ms DESC, external_session_id DESC").Find(&sessions).Error; err != nil {
			return err
		}
		ids := make(map[int64]string, len(sessions))
		for _, session := range sessions {
			ids[session.ID] = session.ExternalSessionID
			var capsule *reportingv1.ThroughputCapsule
			if session.Throughput != nil {
				if err := json.Unmarshal([]byte(*session.Throughput), &capsule); err != nil {
					return err
				}
			}
			result.Sessions = append(result.Sessions, AGYSession{
				ExternalSessionID: session.ExternalSessionID, DisplayTitle: session.DisplayTitle,
				TitleSource: session.TitleSource, ParentExternalSessionID: session.ParentExternalSessionID,
				SessionRole: session.SessionRole, ProjectKey: session.ProjectKey,
				ProjectDisplayName: session.ProjectDisplayName, ProjectSource: session.ProjectSource,
				CreatedAtMS: session.CreatedAtMS, LastActivityAtMS: session.LastActivityAtMS,
				ModelKey: session.ModelKey, RequestCount: session.RequestCount,
				LineageConflict: session.LineageConflict, CoverageState: session.CoverageState,
				Throughput: capsule, UpdatedAtMS: session.UpdatedAtMS,
			})
		}
		var lineage []agyLineageModel
		if err := database.WithContext(ctx).Order("session_id, source_key, lineage_key").Find(&lineage).Error; err != nil {
			return err
		}
		for _, item := range lineage {
			externalID := ids[item.SessionID]
			if externalID == "" {
				continue
			}
			result.Lineage = append(result.Lineage, AGYSessionLineage{
				ExternalSessionID: externalID, SourceKey: item.SourceKey, LineageKey: item.LineageKey,
				ContentDigest: item.ContentDigest, ObservedAtMS: item.ObservedAtMS,
			})
		}
		var usage []agyUsageModel
		if err := database.WithContext(ctx).Order("occurred_at_ms, event_id").Find(&usage).Error; err != nil {
			return err
		}
		for _, event := range usage {
			result.UsageEvents = append(result.UsageEvents, AGYUsageEvent{
				EventID: event.EventID, ExternalSessionID: event.ExternalSessionID,
				StepIndex: event.StepIndex, OccurredAtMS: event.OccurredAtMS, ModelKey: event.ModelKey,
				UncachedInputTokens: event.UncachedInputTokens, CacheReadTokens: event.CacheReadTokens,
				OutputTokens: event.OutputTokens, ReasoningTokens: event.ReasoningTokens, TotalTokens: event.TotalTokens,
				CacheReadKnown: event.CacheReadKnown, ReasoningKnown: event.ReasoningKnown, TotalKnown: event.TotalKnown,
				StartedAtMS: event.StartedAtMS, EndedAtMS: event.EndedAtMS,
				Provenance: event.Provenance, UpdatedAtMS: event.UpdatedAtMS,
			})
		}
		return nil
	})
	return result, err
}

// ReplaceAGYQuotaSnapshots 替换指定账号摘要的全部配额池窗口快照。
func (repository *Repository) ReplaceAGYQuotaSnapshots(
	ctx context.Context,
	accountScope string,
	snapshots []AGYQuotaSnapshot,
) error {
	if repository == nil || repository.database == nil || ctx == nil {
		return ErrInvalidRepository
	}
	if !validHexDigest(accountScope) {
		return invalidRecord("agy quota account scope is invalid")
	}
	for _, item := range snapshots {
		if err := validateAGYQuotaSnapshot(accountScope, item); err != nil {
			return err
		}
	}
	return repository.database.Write(ctx, func(ctx context.Context, transaction *gorm.DB) error {
		database := transaction.WithContext(ctx)
		if err := database.Where("account_scope = ?", accountScope).Delete(&agyQuotaSnapshotModel{}).Error; err != nil {
			return err
		}
		for _, item := range snapshots {
			if err := database.Create(&agyQuotaSnapshotModel{
				AccountScope: item.AccountScope, Generation: item.Generation, PlanType: item.PlanType,
				LimitID: item.LimitID, LimitName: item.LimitName, WindowKind: item.WindowKind,
				WindowMinutes: item.WindowMinutes, UsedPercent: item.UsedPercent, ResetsAtMS: item.ResetsAtMS,
				DataAsOfMS: item.DataAsOfMS, UpdatedAtMS: item.UpdatedAtMS,
			}).Error; err != nil {
				return err
			}
		}
		return nil
	})
}

// AGYQuotaSnapshots 按账号摘要读取已持久化的配额池窗口快照；accountScope 为空时返回全部记录。
func (repository *Repository) AGYQuotaSnapshots(ctx context.Context, accountScope string) ([]AGYQuotaSnapshot, error) {
	if repository == nil || repository.database == nil || ctx == nil {
		return nil, ErrInvalidRepository
	}
	if accountScope != "" && !validHexDigest(accountScope) {
		return nil, invalidRecord("agy quota account scope is invalid")
	}
	var result []AGYQuotaSnapshot
	err := repository.database.ViewSnapshot(ctx, func(ctx context.Context, database *gorm.DB) error {
		query := database.WithContext(ctx).Order("account_scope, limit_id, window_kind")
		if accountScope != "" {
			query = query.Where("account_scope = ?", accountScope)
		}
		var rows []agyQuotaSnapshotModel
		if err := query.Find(&rows).Error; err != nil {
			return err
		}
		result = make([]AGYQuotaSnapshot, 0, len(rows))
		for _, row := range rows {
			result = append(result, AGYQuotaSnapshot{
				AccountScope: row.AccountScope, Generation: row.Generation, PlanType: row.PlanType,
				LimitID: row.LimitID, LimitName: row.LimitName, WindowKind: row.WindowKind,
				WindowMinutes: row.WindowMinutes, UsedPercent: row.UsedPercent, ResetsAtMS: row.ResetsAtMS,
				DataAsOfMS: row.DataAsOfMS, UpdatedAtMS: row.UpdatedAtMS,
			})
		}
		return nil
	})
	return result, err
}

// AppendAGYQuotaObservation 追加一条 AGY 配额观测点，并按每窗口最大保留条数裁剪最旧记录。
func (repository *Repository) AppendAGYQuotaObservation(
	ctx context.Context,
	observation AGYQuotaObservation,
	maxPerWindow int,
) error {
	if repository == nil || repository.database == nil || ctx == nil {
		return ErrInvalidRepository
	}
	if err := validateAGYQuotaObservation(observation); err != nil {
		return err
	}
	if maxPerWindow <= 0 {
		maxPerWindow = defaultAGYQuotaObservationRetention
	}
	return repository.database.Write(ctx, func(ctx context.Context, transaction *gorm.DB) error {
		database := transaction.WithContext(ctx)
		row := agyQuotaObservationModel{
			AccountScope: observation.AccountScope, LimitID: observation.LimitID,
			WindowKind: observation.WindowKind, ObservedAtMS: observation.ObservedAtMS,
			UsedPercent: observation.UsedPercent, ResetsAtMS: observation.ResetsAtMS,
		}
		if err := database.Clauses(clause.OnConflict{
			Columns: []clause.Column{
				{Name: "account_scope"}, {Name: "limit_id"}, {Name: "window_kind"}, {Name: "observed_at_ms"},
			},
			DoUpdates: clause.AssignmentColumns([]string{"used_percent", "resets_at_ms"}),
		}).Create(&row).Error; err != nil {
			return err
		}
		var stale []agyQuotaObservationModel
		if err := database.Where(
			"account_scope = ? AND limit_id = ? AND window_kind = ?",
			observation.AccountScope, observation.LimitID, observation.WindowKind,
		).Order("observed_at_ms DESC").Offset(maxPerWindow).Find(&stale).Error; err != nil {
			return err
		}
		for _, item := range stale {
			if err := database.Where(
				"account_scope = ? AND limit_id = ? AND window_kind = ? AND observed_at_ms = ?",
				item.AccountScope, item.LimitID, item.WindowKind, item.ObservedAtMS,
			).Delete(&agyQuotaObservationModel{}).Error; err != nil {
				return err
			}
		}
		return nil
	})
}

// AGYQuotaObservations 读取指定账号与配额窗口的历史观测点，按时间升序排列。
func (repository *Repository) AGYQuotaObservations(
	ctx context.Context,
	accountScope, limitID, windowKind string,
) ([]AGYQuotaObservation, error) {
	if repository == nil || repository.database == nil || ctx == nil {
		return nil, ErrInvalidRepository
	}
	if !validHexDigest(accountScope) || !safeCursorKey(limitID) || !validAGYWindowKind(windowKind) {
		return nil, invalidRecord("agy quota observation key is invalid")
	}
	var result []AGYQuotaObservation
	err := repository.database.ViewSnapshot(ctx, func(ctx context.Context, database *gorm.DB) error {
		var rows []agyQuotaObservationModel
		if err := database.WithContext(ctx).Where(
			"account_scope = ? AND limit_id = ? AND window_kind = ?",
			accountScope, limitID, windowKind,
		).Order("observed_at_ms ASC").Find(&rows).Error; err != nil {
			return err
		}
		result = make([]AGYQuotaObservation, 0, len(rows))
		for _, row := range rows {
			result = append(result, AGYQuotaObservation{
				AccountScope: row.AccountScope, LimitID: row.LimitID, WindowKind: row.WindowKind,
				ObservedAtMS: row.ObservedAtMS, UsedPercent: row.UsedPercent, ResetsAtMS: row.ResetsAtMS,
			})
		}
		return nil
	})
	return result, err
}

func validateAGYSnapshot(snapshot AGYSnapshot) error {
	if snapshot.Generation < 0 || snapshot.CollectedAtMS < 0 {
		return invalidRecord("agy snapshot identity is invalid")
	}
	sessions := make(map[string]struct{}, len(snapshot.Sessions))
	for _, session := range snapshot.Sessions {
		if !safeCursorID(session.ExternalSessionID) || !validHexDigest(session.ProjectKey) ||
			!safeDSHTitle(session.DisplayTitle) || !validAGYTitleSource(session.TitleSource) ||
			(session.ParentExternalSessionID != nil && !safeCursorID(*session.ParentExternalSessionID)) ||
			!validAGYSessionRole(session.SessionRole) ||
			!safeCursorLabel(session.ProjectDisplayName) || !validAGYProjectSource(session.ProjectSource) ||
			session.CreatedAtMS < 0 || session.LastActivityAtMS < session.CreatedAtMS ||
			session.RequestCount < 0 ||
			(session.ModelKey != nil && !safeCursorLabel(*session.ModelKey)) ||
			!validCursorCoverage(session.CoverageState) || session.UpdatedAtMS < 0 {
			return invalidRecord("agy session is invalid")
		}
		if _, exists := sessions[session.ExternalSessionID]; exists {
			return invalidRecord("agy session identity is duplicated")
		}
		sessions[session.ExternalSessionID] = struct{}{}
	}
	for _, lineage := range snapshot.Lineage {
		if _, ok := sessions[lineage.ExternalSessionID]; !ok || !safeCursorKey(lineage.SourceKey) ||
			!validHexDigest(lineage.LineageKey) || !validHexDigest(lineage.ContentDigest) || lineage.ObservedAtMS < 0 {
			return invalidRecord("agy lineage is invalid")
		}
	}
	seenUsage := make(map[string]struct{}, len(snapshot.UsageEvents))
	for _, event := range snapshot.UsageEvents {
		_, sessionExists := sessions[event.ExternalSessionID]
		if !validHexDigest(event.EventID) || !sessionExists || event.StepIndex < 0 || event.OccurredAtMS < 0 ||
			event.UncachedInputTokens < 0 || event.CacheReadTokens < 0 ||
			event.OutputTokens < 0 || event.ReasoningTokens < 0 || event.TotalTokens < 0 ||
			event.UpdatedAtMS < 0 ||
			(event.ModelKey != nil && !safeCursorLabel(*event.ModelKey)) ||
			(event.StartedAtMS != nil && *event.StartedAtMS < 0) ||
			(event.EndedAtMS != nil && (event.StartedAtMS == nil || *event.EndedAtMS < *event.StartedAtMS)) ||
			!validAGYUsageProvenance(event.Provenance) {
			return invalidRecord("agy usage event is invalid")
		}
		if _, exists := seenUsage[event.EventID]; exists {
			return invalidRecord("agy usage identity is duplicated")
		}
		seenUsage[event.EventID] = struct{}{}
	}
	for _, source := range snapshot.Sources {
		if source.Provider != "agy" || !safeCursorKey(source.SourceKey) || !safeCursorLabel(source.SourceType) ||
			source.RowCount < 0 || source.LastAttemptAtMS < 0 || source.UpdatedAtMS < 0 ||
			(source.SchemaVersion != nil && *source.SchemaVersion < 0) ||
			(source.LastSuccessAtMS != nil && *source.LastSuccessAtMS < 0) ||
			(source.CheckpointValue != nil && !safeCursorID(*source.CheckpointValue)) ||
			!validCursorSourceState(source.State) || !validCursorCheckpoint(source.CheckpointKind) ||
			!validCursorFailure(source.FailureCode) || !validCursorCoverage(source.CoverageState) {
			return invalidRecord("agy source status is invalid")
		}
	}
	return nil
}

func validateAGYQuotaSnapshot(accountScope string, item AGYQuotaSnapshot) error {
	if item.AccountScope != accountScope || item.Generation < 0 ||
		(item.PlanType != nil && !safeCursorLabel(*item.PlanType)) ||
		!safeCursorKey(item.LimitID) || !safeCursorLabel(item.LimitName) ||
		!validAGYWindowKind(item.WindowKind) || item.WindowMinutes <= 0 ||
		(item.UsedPercent != nil && (*item.UsedPercent < 0 || *item.UsedPercent > 100)) ||
		(item.ResetsAtMS != nil && *item.ResetsAtMS < 0) ||
		item.DataAsOfMS < 0 || item.UpdatedAtMS < 0 {
		return invalidRecord("agy quota snapshot is invalid")
	}
	return nil
}

func validateAGYQuotaObservation(item AGYQuotaObservation) error {
	if !validHexDigest(item.AccountScope) || !safeCursorKey(item.LimitID) ||
		!validAGYWindowKind(item.WindowKind) || item.ObservedAtMS < 0 ||
		item.UsedPercent < 0 || item.UsedPercent > 100 ||
		(item.ResetsAtMS != nil && *item.ResetsAtMS < 0) {
		return invalidRecord("agy quota observation is invalid")
	}
	return nil
}

func normalizeAGYSessionPresentation(session *AGYSession) {
	if session.DisplayTitle == "" {
		session.DisplayTitle = "未命名会话"
	}
	if session.TitleSource == "" {
		session.TitleSource = "fallback"
	}
	if session.SessionRole == "" {
		session.SessionRole = "unknown"
	}
	if session.ProjectSource == "" {
		session.ProjectSource = "unassigned"
	}
}

func validAGYTitleSource(value string) bool {
	return value == "agy_annotation" || value == "agy_summary" || value == "fallback"
}

func validAGYSessionRole(value string) bool {
	return value == "main" || value == "subagent" || value == "swarm_worker" || value == "side_question" || value == "unknown"
}

func validAGYProjectSource(value string) bool {
	return value == "workspace" || value == "inherited_parent" || value == "unassigned"
}

func validAGYUsageProvenance(value string) bool {
	return value == "agy_step_metadata" || value == "agy_transcript_jsonl"
}

func validAGYWindowKind(value string) bool {
	return value == "primary" || value == "secondary"
}
