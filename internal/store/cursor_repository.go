package store

import (
	"context"
	"encoding/hex"
	"errors"
	"fmt"
	"math"
	"strings"

	"gorm.io/gorm"
)

type cursorSnapshotModel struct {
	Provider      string `gorm:"column:provider;primaryKey"`
	Generation    int64  `gorm:"column:generation"`
	CollectedAtMS int64  `gorm:"column:collected_at_ms"`
}

func (cursorSnapshotModel) TableName() string { return "agent_provider_snapshots" }

type cursorSourceModel struct {
	Provider        string  `gorm:"column:provider;primaryKey"`
	SourceKey       string  `gorm:"column:source_key;primaryKey"`
	SourceType      string  `gorm:"column:source_type"`
	State           string  `gorm:"column:state"`
	CoverageState   string  `gorm:"column:coverage_state"`
	SchemaVersion   *int64  `gorm:"column:schema_version"`
	CheckpointKind  string  `gorm:"column:checkpoint_kind"`
	CheckpointValue *string `gorm:"column:checkpoint_value"`
	RowCount        int64   `gorm:"column:row_count"`
	LastAttemptAtMS int64   `gorm:"column:last_attempt_at_ms"`
	LastSuccessAtMS *int64  `gorm:"column:last_success_at_ms"`
	FailureCode     *string `gorm:"column:failure_code"`
	UpdatedAtMS     int64   `gorm:"column:updated_at_ms"`
}

func (cursorSourceModel) TableName() string { return "agent_provider_sources" }

type cursorSessionModel struct {
	ID                 int64   `gorm:"column:id;primaryKey;autoIncrement"`
	Provider           string  `gorm:"column:provider"`
	ExternalSessionID  string  `gorm:"column:external_session_id"`
	DisplayTitle       string  `gorm:"column:display_title"`
	TitleSource        string  `gorm:"column:title_source"`
	ProjectKey         string  `gorm:"column:project_key"`
	ProjectDisplayName string  `gorm:"column:project_display_name"`
	CreatedAtMS        int64   `gorm:"column:created_at_ms"`
	LastActivityAtMS   int64   `gorm:"column:last_activity_at_ms"`
	ModelKey           *string `gorm:"column:model_key"`
	RequestCount       int64   `gorm:"column:request_count"`
	ToolCallCount      int64   `gorm:"column:tool_call_count"`
	AIEditCount        int64   `gorm:"column:ai_edit_count"`
	AILinesAdded       *int64  `gorm:"column:ai_lines_added"`
	AILinesRemoved     *int64  `gorm:"column:ai_lines_removed"`
	LineageConflict    bool    `gorm:"column:lineage_conflict"`
	CoverageState      string  `gorm:"column:coverage_state"`
	UpdatedAtMS        int64   `gorm:"column:updated_at_ms"`
}

func (cursorSessionModel) TableName() string { return "cursor_sessions" }

type cursorLineageModel struct {
	SessionID     int64  `gorm:"column:session_id;primaryKey"`
	SourceKey     string `gorm:"column:source_key;primaryKey"`
	LineageKey    string `gorm:"column:lineage_key;primaryKey"`
	ContentDigest string `gorm:"column:content_digest"`
	ObservedAtMS  int64  `gorm:"column:observed_at_ms"`
}

func (cursorLineageModel) TableName() string { return "cursor_session_lineage" }

type cursorUsageModel struct {
	EventID           string  `gorm:"column:event_id;primaryKey"`
	ExternalSessionID string  `gorm:"column:external_session_id"`
	OccurredAtMS      int64   `gorm:"column:occurred_at_ms"`
	ModelKey          *string `gorm:"column:model_key"`
	InputTokens       int64   `gorm:"column:input_tokens"`
	OutputTokens      int64   `gorm:"column:output_tokens"`
	Provenance        string  `gorm:"column:provenance"`
	UpdatedAtMS       int64   `gorm:"column:updated_at_ms"`
}

func (cursorUsageModel) TableName() string { return "cursor_usage_events" }

type cursorRequestModel struct {
	EventID           string `gorm:"column:event_id;primaryKey"`
	ExternalSessionID string `gorm:"column:external_session_id"`
	OccurredAtMS      int64  `gorm:"column:occurred_at_ms"`
	Provenance        string `gorm:"column:provenance"`
	UpdatedAtMS       int64  `gorm:"column:updated_at_ms"`
}

func (cursorRequestModel) TableName() string { return "cursor_request_events" }

type cursorToolModel struct {
	EventID           string `gorm:"column:event_id;primaryKey"`
	ExternalSessionID string `gorm:"column:external_session_id"`
	OccurredAtMS      int64  `gorm:"column:occurred_at_ms"`
	ToolName          string `gorm:"column:tool_name"`
	Outcome           string `gorm:"column:outcome"`
	Provenance        string `gorm:"column:provenance"`
	UpdatedAtMS       int64  `gorm:"column:updated_at_ms"`
}

func (cursorToolModel) TableName() string { return "cursor_tool_events" }

type cursorAIEditModel struct {
	EventID           string  `gorm:"column:event_id;primaryKey"`
	ExternalSessionID *string `gorm:"column:external_session_id"`
	OccurredAtMS      int64   `gorm:"column:occurred_at_ms"`
	ModelKey          *string `gorm:"column:model_key"`
	EditCount         int64   `gorm:"column:edit_count"`
	Provenance        string  `gorm:"column:provenance"`
	UpdatedAtMS       int64   `gorm:"column:updated_at_ms"`
}

func (cursorAIEditModel) TableName() string { return "cursor_ai_edit_events" }

type cursorDashboardSnapshotModel struct {
	Provider                string `gorm:"column:provider;primaryKey"`
	Generation              int64  `gorm:"column:generation"`
	CollectedAtMS           int64  `gorm:"column:collected_at_ms"`
	WindowStartMS           int64  `gorm:"column:window_start_ms"`
	WindowEndMS             int64  `gorm:"column:window_end_ms"`
	BillingCycleEndMS       int64  `gorm:"column:billing_cycle_end_ms"`
	PlanTotalSpendMicros    *int64 `gorm:"column:plan_total_spend_micros"`
	PlanIncludedSpendMicros *int64 `gorm:"column:plan_included_spend_micros"`
	PlanBonusSpendMicros    *int64 `gorm:"column:plan_bonus_spend_micros"`
	PlanRemainingMicros     *int64 `gorm:"column:plan_remaining_micros"`
	PlanLimitMicros         *int64 `gorm:"column:plan_limit_micros"`
	EventCount              int64  `gorm:"column:event_count"`
}

func (cursorDashboardSnapshotModel) TableName() string { return "cursor_dashboard_snapshots" }

type cursorDashboardQuotaObservationModel struct {
	Provider       string  `gorm:"column:provider;primaryKey"`
	Generation     int64   `gorm:"column:generation;primaryKey"`
	LimitID        string  `gorm:"column:limit_id;primaryKey"`
	UsedPercent    float64 `gorm:"column:used_percent"`
	CycleStartAtMS int64   `gorm:"column:cycle_start_at_ms"`
	CycleEndAtMS   int64   `gorm:"column:cycle_end_at_ms"`
	ObservedAtMS   int64   `gorm:"column:observed_at_ms"`
}

func (cursorDashboardQuotaObservationModel) TableName() string {
	return "cursor_dashboard_quota_observations"
}

type cursorDashboardUsageModel struct {
	EventFingerprint     string  `gorm:"column:event_fingerprint;primaryKey"`
	OccurrenceCount      int64   `gorm:"column:occurrence_count"`
	ExternalSessionID    *string `gorm:"column:external_session_id"`
	OccurredAtMS         int64   `gorm:"column:occurred_at_ms"`
	ModelKey             *string `gorm:"column:model_key"`
	Kind                 int64   `gorm:"column:kind"`
	TokenBased           bool    `gorm:"column:token_based"`
	InputTokens          int64   `gorm:"column:input_tokens"`
	OutputTokens         int64   `gorm:"column:output_tokens"`
	CacheWriteTokens     int64   `gorm:"column:cache_write_tokens"`
	CacheReadTokens      int64   `gorm:"column:cache_read_tokens"`
	ReportedChargeMicros int64   `gorm:"column:reported_charge_micros"`
	CursorTokenFeeMicros int64   `gorm:"column:cursor_token_fee_micros"`
	UpdatedAtMS          int64   `gorm:"column:updated_at_ms"`
}

func (cursorDashboardUsageModel) TableName() string { return "cursor_dashboard_usage_events" }

func (repository *Repository) ReplaceCursorSnapshot(ctx context.Context, snapshot CursorSnapshot) error {
	if repository == nil || repository.database == nil || ctx == nil {
		return ErrInvalidRepository
	}
	snapshot.Sessions = append([]CursorSession(nil), snapshot.Sessions...)
	for index := range snapshot.Sessions {
		normalizeCursorSessionPresentation(&snapshot.Sessions[index])
	}
	if err := validateCursorSnapshot(snapshot); err != nil {
		return err
	}
	return repository.database.Write(ctx, func(ctx context.Context, transaction *gorm.DB) error {
		database := transaction.WithContext(ctx)
		for _, table := range []string{
			"cursor_session_lineage", "cursor_tool_events", "cursor_usage_events", "cursor_request_events", "cursor_ai_edit_events", "cursor_sessions",
		} {
			if err := database.Exec("DELETE FROM " + table).Error; err != nil {
				return err
			}
		}
		if err := database.Where(
			"provider = ? AND source_key <> ? AND source_key <> ?",
			"cursor", "cursor.dashboard", "cursor.dashboard.grok_bot",
		).Delete(&cursorSourceModel{}).Error; err != nil {
			return err
		}
		if err := database.Save(&cursorSnapshotModel{
			Provider: "cursor", Generation: snapshot.Generation, CollectedAtMS: snapshot.CollectedAtMS,
		}).Error; err != nil {
			return err
		}
		for _, source := range snapshot.Sources {
			model := cursorSourceModel{
				Provider: source.Provider, SourceKey: source.SourceKey, SourceType: source.SourceType,
				State: source.State, CoverageState: source.CoverageState, SchemaVersion: source.SchemaVersion,
				CheckpointKind: source.CheckpointKind, CheckpointValue: source.CheckpointValue,
				RowCount: source.RowCount, LastAttemptAtMS: source.LastAttemptAtMS,
				LastSuccessAtMS: source.LastSuccessAtMS, FailureCode: source.FailureCode, UpdatedAtMS: source.UpdatedAtMS,
			}
			if err := database.Create(&model).Error; err != nil {
				return err
			}
		}
		sessionIDs := make(map[string]int64, len(snapshot.Sessions))
		for _, session := range snapshot.Sessions {
			model := cursorSessionModel{
				Provider: "cursor", ExternalSessionID: session.ExternalSessionID,
				DisplayTitle: session.DisplayTitle, TitleSource: session.TitleSource,
				ProjectKey: session.ProjectKey, ProjectDisplayName: session.ProjectDisplayName,
				CreatedAtMS: session.CreatedAtMS, LastActivityAtMS: session.LastActivityAtMS,
				ModelKey: session.ModelKey, RequestCount: session.RequestCount, ToolCallCount: session.ToolCallCount,
				AIEditCount: session.AIEditCount, AILinesAdded: session.AILinesAdded, AILinesRemoved: session.AILinesRemoved,
				LineageConflict: session.LineageConflict, CoverageState: session.CoverageState, UpdatedAtMS: session.UpdatedAtMS,
			}
			if err := database.Create(&model).Error; err != nil {
				return err
			}
			sessionIDs[session.ExternalSessionID] = model.ID
		}
		for _, lineage := range snapshot.Lineage {
			sessionID, ok := sessionIDs[lineage.ExternalSessionID]
			if !ok {
				return invalidRecord("cursor lineage session is missing")
			}
			if err := database.Create(&cursorLineageModel{
				SessionID: sessionID, SourceKey: lineage.SourceKey, LineageKey: lineage.LineageKey,
				ContentDigest: lineage.ContentDigest, ObservedAtMS: lineage.ObservedAtMS,
			}).Error; err != nil {
				return err
			}
		}
		for _, event := range snapshot.UsageEvents {
			if err := database.Create(&cursorUsageModel{
				EventID: event.EventID, ExternalSessionID: event.ExternalSessionID, OccurredAtMS: event.OccurredAtMS,
				ModelKey: event.ModelKey, InputTokens: event.InputTokens, OutputTokens: event.OutputTokens,
				Provenance: "cursor_state_usage", UpdatedAtMS: event.UpdatedAtMS,
			}).Error; err != nil {
				return err
			}
		}
		for _, event := range snapshot.RequestEvents {
			if err := database.Create(&cursorRequestModel{
				EventID: event.EventID, ExternalSessionID: event.ExternalSessionID,
				OccurredAtMS: event.OccurredAtMS, Provenance: "cursor_generation_id", UpdatedAtMS: event.UpdatedAtMS,
			}).Error; err != nil {
				return err
			}
		}
		for _, event := range snapshot.ToolEvents {
			if err := database.Create(&cursorToolModel{
				EventID: event.EventID, ExternalSessionID: event.ExternalSessionID, OccurredAtMS: event.OccurredAtMS,
				ToolName: event.ToolName, Outcome: event.Outcome, Provenance: event.Provenance, UpdatedAtMS: event.UpdatedAtMS,
			}).Error; err != nil {
				return err
			}
		}
		for _, event := range snapshot.AIEditEvents {
			if err := database.Create(&cursorAIEditModel{
				EventID: event.EventID, ExternalSessionID: event.ExternalSessionID, OccurredAtMS: event.OccurredAtMS,
				ModelKey: event.ModelKey, EditCount: event.EditCount,
				Provenance: "cursor_ai_tracking", UpdatedAtMS: event.UpdatedAtMS,
			}).Error; err != nil {
				return err
			}
		}
		return nil
	})
}

func (repository *Repository) CommitCursorDashboardSnapshot(ctx context.Context, snapshot CursorDashboardSnapshot) error {
	if repository == nil || repository.database == nil || ctx == nil {
		return ErrInvalidRepository
	}
	if err := validateCursorDashboardSnapshot(snapshot); err != nil {
		return err
	}
	return repository.database.Write(ctx, func(ctx context.Context, transaction *gorm.DB) error {
		database := transaction.WithContext(ctx)
		if err := database.Exec("DELETE FROM cursor_dashboard_usage_events").Error; err != nil {
			return err
		}
		for _, event := range snapshot.Events {
			model := cursorDashboardUsageModel{
				EventFingerprint: event.EventFingerprint, OccurrenceCount: event.OccurrenceCount,
				ExternalSessionID: event.ExternalSessionID, OccurredAtMS: event.OccurredAtMS,
				ModelKey: event.ModelKey, Kind: event.Kind, TokenBased: event.TokenBased, InputTokens: event.InputTokens,
				OutputTokens: event.OutputTokens, CacheWriteTokens: event.CacheWriteTokens,
				CacheReadTokens: event.CacheReadTokens, ReportedChargeMicros: event.ReportedChargeMicros,
				CursorTokenFeeMicros: event.CursorTokenFeeMicros, UpdatedAtMS: event.UpdatedAtMS,
			}
			if err := database.Create(&model).Error; err != nil {
				return err
			}
		}
		dashboardModel := cursorDashboardSnapshotModel{
			Provider: "cursor", Generation: snapshot.Generation, CollectedAtMS: snapshot.CollectedAtMS,
			WindowStartMS: snapshot.WindowStartMS, WindowEndMS: snapshot.WindowEndMS,
			BillingCycleEndMS: snapshot.BillingCycleEndMS, EventCount: int64(len(snapshot.Events)),
		}
		if snapshot.PlanUsage != nil {
			dashboardModel.PlanTotalSpendMicros = &snapshot.PlanUsage.TotalSpendMicros
			dashboardModel.PlanIncludedSpendMicros = &snapshot.PlanUsage.IncludedSpendMicros
			dashboardModel.PlanBonusSpendMicros = &snapshot.PlanUsage.BonusSpendMicros
			dashboardModel.PlanRemainingMicros = &snapshot.PlanUsage.RemainingMicros
			dashboardModel.PlanLimitMicros = &snapshot.PlanUsage.LimitMicros
		}
		if err := database.Save(&dashboardModel).Error; err != nil {
			return err
		}
		for _, window := range snapshot.QuotaWindows {
			if err := database.Save(&cursorDashboardQuotaObservationModel{
				Provider: "cursor", Generation: snapshot.Generation, LimitID: window.LimitID,
				UsedPercent: window.UsedPercent, CycleStartAtMS: window.CycleStartAtMS,
				CycleEndAtMS: window.CycleEndAtMS, ObservedAtMS: snapshot.CollectedAtMS,
			}).Error; err != nil {
				return err
			}
		}
		checkpoint := fmt.Sprintf("%d", snapshot.Generation)
		lastSuccess := snapshot.CollectedAtMS
		var rowCount int64
		for _, event := range snapshot.Events {
			rowCount += event.OccurrenceCount
		}
		return database.Save(&cursorSourceModel{
			Provider: "cursor", SourceKey: "cursor.dashboard", SourceType: "desktop_authenticated_rpc",
			State: "available", CoverageState: "exact", CheckpointKind: "snapshot",
			CheckpointValue: &checkpoint, RowCount: rowCount,
			LastAttemptAtMS: snapshot.CollectedAtMS, LastSuccessAtMS: &lastSuccess,
			UpdatedAtMS: snapshot.CollectedAtMS,
		}).Error
	})
}

func (repository *Repository) CommitCursorGrokBotObservation(ctx context.Context, commit CursorGrokBotCommit) error {
	if repository == nil || repository.database == nil || ctx == nil {
		return ErrInvalidRepository
	}
	if err := validateCursorGrokBotCommit(commit); err != nil {
		return err
	}
	return repository.database.Write(ctx, func(ctx context.Context, transaction *gorm.DB) error {
		database := transaction.WithContext(ctx)
		if commit.Included {
			if err := database.Save(&cursorDashboardQuotaObservationModel{
				Provider: "cursor", Generation: commit.Generation, LimitID: "cursor.grok_bot",
				UsedPercent: *commit.UsedPercent, CycleStartAtMS: commit.CycleStartAtMS,
				CycleEndAtMS: commit.CycleEndAtMS, ObservedAtMS: commit.CollectedAtMS,
			}).Error; err != nil {
				return err
			}
		}
		checkpoint := fmt.Sprintf("%d", commit.Generation)
		lastSuccess := commit.CollectedAtMS
		var rowCount int64
		if commit.Included {
			rowCount = 1
		}
		return database.Save(&cursorSourceModel{
			Provider: "cursor", SourceKey: "cursor.dashboard.grok_bot", SourceType: "desktop_authenticated_rpc",
			State: "available", CoverageState: "exact", CheckpointKind: "snapshot",
			CheckpointValue: &checkpoint, RowCount: rowCount,
			LastAttemptAtMS: commit.CollectedAtMS, LastSuccessAtMS: &lastSuccess,
			UpdatedAtMS: commit.CollectedAtMS,
		}).Error
	})
}

func (repository *Repository) RecordCursorGrokBotFailure(ctx context.Context, atMS int64, failureCode string) error {
	if repository == nil || repository.database == nil || ctx == nil || atMS < 0 ||
		(failureCode != "auth_expired" && failureCode != "schema_incompatible" && failureCode != "read_failed") {
		return ErrInvalidRepository
	}
	return repository.database.Write(ctx, func(ctx context.Context, transaction *gorm.DB) error {
		database := transaction.WithContext(ctx)
		var source cursorSourceModel
		err := database.Where("provider = ? AND source_key = ?", "cursor", "cursor.dashboard.grok_bot").Take(&source).Error
		if err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
			return err
		}
		if errors.Is(err, gorm.ErrRecordNotFound) {
			source = cursorSourceModel{
				Provider: "cursor", SourceKey: "cursor.dashboard.grok_bot", SourceType: "desktop_authenticated_rpc",
				CheckpointKind: "snapshot",
			}
		}
		source.State = "unavailable"
		source.CoverageState = "unknown"
		source.LastAttemptAtMS = atMS
		source.FailureCode = &failureCode
		source.UpdatedAtMS = atMS
		return database.Save(&source).Error
	})
}

func (repository *Repository) RecordCursorDashboardFailure(ctx context.Context, atMS int64, failureCode string) error {
	if repository == nil || repository.database == nil || ctx == nil || atMS < 0 ||
		(failureCode != "auth_expired" && failureCode != "schema_incompatible" && failureCode != "read_failed") {
		return ErrInvalidRepository
	}
	return repository.database.Write(ctx, func(ctx context.Context, transaction *gorm.DB) error {
		database := transaction.WithContext(ctx)
		var source cursorSourceModel
		err := database.Where("provider = ? AND source_key = ?", "cursor", "cursor.dashboard").Take(&source).Error
		if err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
			return err
		}
		if errors.Is(err, gorm.ErrRecordNotFound) {
			source = cursorSourceModel{
				Provider: "cursor", SourceKey: "cursor.dashboard", SourceType: "desktop_authenticated_rpc",
				CheckpointKind: "snapshot",
			}
		}
		source.State = "unavailable"
		source.CoverageState = "unknown"
		source.LastAttemptAtMS = atMS
		source.FailureCode = &failureCode
		source.UpdatedAtMS = atMS
		return database.Save(&source).Error
	})
}

func (repository *Repository) CursorSnapshot(ctx context.Context) (CursorSnapshot, error) {
	if repository == nil || repository.database == nil || ctx == nil {
		return CursorSnapshot{}, ErrInvalidRepository
	}
	var result CursorSnapshot
	err := repository.database.ViewSnapshot(ctx, func(ctx context.Context, database *gorm.DB) error {
		var snapshot cursorSnapshotModel
		if err := database.WithContext(ctx).Where("provider = ?", "cursor").Take(&snapshot).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return ErrNotFound
			}
			return err
		}
		result.Generation, result.CollectedAtMS = snapshot.Generation, snapshot.CollectedAtMS
		var sources []cursorSourceModel
		if err := database.WithContext(ctx).Where("provider = ?", "cursor").Order("source_key").Find(&sources).Error; err != nil {
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
		var sessions []cursorSessionModel
		if err := database.WithContext(ctx).Order("last_activity_at_ms DESC, external_session_id DESC").Find(&sessions).Error; err != nil {
			return err
		}
		ids := make(map[int64]string, len(sessions))
		for _, session := range sessions {
			ids[session.ID] = session.ExternalSessionID
			result.Sessions = append(result.Sessions, CursorSession{
				ExternalSessionID: session.ExternalSessionID,
				DisplayTitle:      session.DisplayTitle, TitleSource: session.TitleSource,
				ProjectKey:         session.ProjectKey,
				ProjectDisplayName: session.ProjectDisplayName, CreatedAtMS: session.CreatedAtMS,
				LastActivityAtMS: session.LastActivityAtMS, ModelKey: session.ModelKey,
				RequestCount: session.RequestCount, ToolCallCount: session.ToolCallCount,
				AIEditCount: session.AIEditCount, AILinesAdded: session.AILinesAdded,
				AILinesRemoved: session.AILinesRemoved, LineageConflict: session.LineageConflict,
				CoverageState: session.CoverageState, UpdatedAtMS: session.UpdatedAtMS,
			})
		}
		var lineage []cursorLineageModel
		if err := database.WithContext(ctx).Find(&lineage).Error; err != nil {
			return err
		}
		for _, item := range lineage {
			result.Lineage = append(result.Lineage, CursorSessionLineage{
				ExternalSessionID: ids[item.SessionID], SourceKey: item.SourceKey,
				LineageKey: item.LineageKey, ContentDigest: item.ContentDigest, ObservedAtMS: item.ObservedAtMS,
			})
		}
		var usage []cursorUsageModel
		if err := database.WithContext(ctx).Find(&usage).Error; err != nil {
			return err
		}
		var dashboardSnapshot cursorDashboardSnapshotModel
		if err := database.WithContext(ctx).Where("provider = ?", "cursor").Take(&dashboardSnapshot).Error; err == nil {
			result.DashboardGeneration = dashboardSnapshot.Generation
			result.DashboardCollectedAtMS = dashboardSnapshot.CollectedAtMS
			result.DashboardWindowStartMS = dashboardSnapshot.WindowStartMS
			result.DashboardWindowEndMS = dashboardSnapshot.WindowEndMS
			result.DashboardBillingCycleEndMS = dashboardSnapshot.BillingCycleEndMS
			if dashboardSnapshot.PlanTotalSpendMicros != nil && dashboardSnapshot.PlanIncludedSpendMicros != nil &&
				dashboardSnapshot.PlanBonusSpendMicros != nil && dashboardSnapshot.PlanRemainingMicros != nil &&
				dashboardSnapshot.PlanLimitMicros != nil {
				result.DashboardPlanUsage = &CursorDashboardPlanUsage{
					TotalSpendMicros:    *dashboardSnapshot.PlanTotalSpendMicros,
					IncludedSpendMicros: *dashboardSnapshot.PlanIncludedSpendMicros,
					BonusSpendMicros:    *dashboardSnapshot.PlanBonusSpendMicros,
					RemainingMicros:     *dashboardSnapshot.PlanRemainingMicros,
					LimitMicros:         *dashboardSnapshot.PlanLimitMicros,
				}
			}
		} else if !errors.Is(err, gorm.ErrRecordNotFound) {
			return err
		}
		var dashboardQuota []cursorDashboardQuotaObservationModel
		if err := database.WithContext(ctx).
			Order("generation, limit_id").Find(&dashboardQuota).Error; err != nil {
			return err
		}
		for _, observation := range dashboardQuota {
			result.DashboardQuotaObservations = append(
				result.DashboardQuotaObservations,
				CursorDashboardQuotaObservation{
					Generation: observation.Generation, LimitID: observation.LimitID,
					UsedPercent: observation.UsedPercent, CycleStartAtMS: observation.CycleStartAtMS,
					CycleEndAtMS: observation.CycleEndAtMS, ObservedAtMS: observation.ObservedAtMS,
				},
			)
		}
		var dashboardUsage []cursorDashboardUsageModel
		if err := database.WithContext(ctx).Order("occurred_at_ms, event_fingerprint").Find(&dashboardUsage).Error; err != nil {
			return err
		}
		for _, event := range dashboardUsage {
			result.DashboardUsageEvents = append(result.DashboardUsageEvents, CursorDashboardUsageEvent{
				EventFingerprint: event.EventFingerprint, OccurrenceCount: event.OccurrenceCount,
				ExternalSessionID: event.ExternalSessionID, OccurredAtMS: event.OccurredAtMS,
				ModelKey: event.ModelKey, Kind: event.Kind, TokenBased: event.TokenBased, InputTokens: event.InputTokens,
				OutputTokens: event.OutputTokens, CacheWriteTokens: event.CacheWriteTokens,
				CacheReadTokens: event.CacheReadTokens, ReportedChargeMicros: event.ReportedChargeMicros,
				CursorTokenFeeMicros: event.CursorTokenFeeMicros, UpdatedAtMS: event.UpdatedAtMS,
			})
		}
		for _, event := range usage {
			result.UsageEvents = append(result.UsageEvents, CursorUsageEvent{
				EventID: event.EventID, ExternalSessionID: event.ExternalSessionID,
				OccurredAtMS: event.OccurredAtMS, ModelKey: event.ModelKey,
				InputTokens: event.InputTokens, OutputTokens: event.OutputTokens, UpdatedAtMS: event.UpdatedAtMS,
			})
		}
		var requests []cursorRequestModel
		if err := database.WithContext(ctx).Find(&requests).Error; err != nil {
			return err
		}
		for _, event := range requests {
			result.RequestEvents = append(result.RequestEvents, CursorRequestEvent{
				EventID: event.EventID, ExternalSessionID: event.ExternalSessionID,
				OccurredAtMS: event.OccurredAtMS, UpdatedAtMS: event.UpdatedAtMS,
			})
		}
		var tools []cursorToolModel
		if err := database.WithContext(ctx).Find(&tools).Error; err != nil {
			return err
		}
		for _, event := range tools {
			result.ToolEvents = append(result.ToolEvents, CursorToolEvent{
				EventID: event.EventID, ExternalSessionID: event.ExternalSessionID, OccurredAtMS: event.OccurredAtMS,
				ToolName: event.ToolName, Outcome: event.Outcome, Provenance: event.Provenance, UpdatedAtMS: event.UpdatedAtMS,
			})
		}
		var edits []cursorAIEditModel
		if err := database.WithContext(ctx).Find(&edits).Error; err != nil {
			return err
		}
		for _, event := range edits {
			result.AIEditEvents = append(result.AIEditEvents, CursorAIEditEvent{
				EventID: event.EventID, ExternalSessionID: event.ExternalSessionID,
				OccurredAtMS: event.OccurredAtMS, ModelKey: event.ModelKey,
				EditCount: event.EditCount, UpdatedAtMS: event.UpdatedAtMS,
			})
		}
		return nil
	})
	return result, err
}

func validateCursorSnapshot(snapshot CursorSnapshot) error {
	if snapshot.Generation < 0 || snapshot.CollectedAtMS < 0 {
		return invalidRecord("cursor snapshot identity is invalid")
	}
	sessions := make(map[string]struct{}, len(snapshot.Sessions))
	for _, session := range snapshot.Sessions {
		if !safeCursorID(session.ExternalSessionID) || !validHexDigest(session.ProjectKey) ||
			!safeCursorTitle(session.DisplayTitle) || !validCursorTitleSource(session.TitleSource) ||
			!safeCursorLabel(session.ProjectDisplayName) || session.CreatedAtMS < 0 ||
			session.LastActivityAtMS < session.CreatedAtMS || session.RequestCount < 0 ||
			session.ToolCallCount < 0 || session.AIEditCount < 0 ||
			(session.ModelKey != nil && !safeCursorLabel(*session.ModelKey)) ||
			(session.AILinesAdded != nil && *session.AILinesAdded < 0) ||
			(session.AILinesRemoved != nil && *session.AILinesRemoved < 0) ||
			!validCursorCoverage(session.CoverageState) || session.UpdatedAtMS < 0 {
			return invalidRecord("cursor session is invalid")
		}
		if _, exists := sessions[session.ExternalSessionID]; exists {
			return invalidRecord("cursor session identity is duplicated")
		}
		sessions[session.ExternalSessionID] = struct{}{}
	}
	for _, lineage := range snapshot.Lineage {
		if _, ok := sessions[lineage.ExternalSessionID]; !ok || !safeCursorKey(lineage.SourceKey) ||
			!validHexDigest(lineage.LineageKey) || !validHexDigest(lineage.ContentDigest) || lineage.ObservedAtMS < 0 {
			return invalidRecord("cursor lineage is invalid")
		}
	}
	for _, event := range snapshot.UsageEvents {
		_, sessionExists := sessions[event.ExternalSessionID]
		if !safeCursorID(event.EventID) || !sessionExists || event.OccurredAtMS < 0 ||
			event.InputTokens < 0 || event.OutputTokens < 0 || event.UpdatedAtMS < 0 ||
			(event.ModelKey != nil && !safeCursorLabel(*event.ModelKey)) {
			return invalidRecord("cursor usage event is invalid")
		}
	}
	for _, event := range snapshot.RequestEvents {
		_, sessionExists := sessions[event.ExternalSessionID]
		if !safeCursorID(event.EventID) || !sessionExists || event.OccurredAtMS < 0 || event.UpdatedAtMS < 0 {
			return invalidRecord("cursor request event is invalid")
		}
	}
	for _, event := range snapshot.ToolEvents {
		_, sessionExists := sessions[event.ExternalSessionID]
		if !validHexDigest(event.EventID) || !sessionExists || event.OccurredAtMS < 0 || event.UpdatedAtMS < 0 ||
			!safeCursorLabel(event.ToolName) || (event.Outcome != "succeeded" && event.Outcome != "failed" && event.Outcome != "unknown") ||
			(event.Provenance != "cursor_transcript" && event.Provenance != "cursor_state") {
			return invalidRecord("cursor tool event is invalid")
		}
	}
	for _, event := range snapshot.AIEditEvents {
		sessionExists := event.ExternalSessionID == nil
		if event.ExternalSessionID != nil {
			_, sessionExists = sessions[*event.ExternalSessionID]
		}
		if !validHexDigest(event.EventID) || !sessionExists || event.OccurredAtMS < 0 ||
			event.EditCount <= 0 || event.UpdatedAtMS < 0 ||
			(event.ModelKey != nil && !safeCursorLabel(*event.ModelKey)) {
			return invalidRecord("cursor AI edit event is invalid")
		}
	}
	for _, source := range snapshot.Sources {
		if source.Provider != "cursor" || !safeCursorKey(source.SourceKey) || !safeCursorLabel(source.SourceType) ||
			source.RowCount < 0 || source.LastAttemptAtMS < 0 || source.UpdatedAtMS < 0 ||
			(source.SchemaVersion != nil && *source.SchemaVersion < 0) ||
			(source.LastSuccessAtMS != nil && *source.LastSuccessAtMS < 0) ||
			(source.CheckpointValue != nil && !safeCursorID(*source.CheckpointValue)) ||
			!validCursorSourceState(source.State) || !validCursorCheckpoint(source.CheckpointKind) ||
			!validCursorFailure(source.FailureCode) || !validCursorCoverage(source.CoverageState) {
			return invalidRecord("cursor source status is invalid")
		}
	}
	return nil
}

func validateCursorGrokBotCommit(commit CursorGrokBotCommit) error {
	if commit.Generation < 0 || commit.CollectedAtMS < 0 || commit.CycleStartAtMS < 0 ||
		commit.CycleEndAtMS <= commit.CycleStartAtMS ||
		commit.CollectedAtMS < commit.CycleStartAtMS || commit.CollectedAtMS > commit.CycleEndAtMS {
		return invalidRecord("cursor grok bot observation is invalid")
	}
	if commit.Included {
		if commit.UsedPercent == nil || math.IsNaN(*commit.UsedPercent) || math.IsInf(*commit.UsedPercent, 0) ||
			*commit.UsedPercent < 0 || *commit.UsedPercent > 100 {
			return invalidRecord("cursor grok bot percent is invalid")
		}
		return nil
	}
	if commit.UsedPercent != nil {
		return invalidRecord("cursor grok bot not-applicable observation must omit percent")
	}
	return nil
}

func normalizeCursorSessionPresentation(session *CursorSession) {
	if session.DisplayTitle == "" {
		session.DisplayTitle = "未命名会话"
	}
	if session.TitleSource == "" {
		session.TitleSource = "fallback"
	}
}

func validateCursorDashboardSnapshot(snapshot CursorDashboardSnapshot) error {
	if snapshot.Generation < 0 || snapshot.CollectedAtMS < 0 || snapshot.WindowStartMS < 0 ||
		snapshot.WindowEndMS <= snapshot.WindowStartMS || snapshot.BillingCycleEndMS <= snapshot.WindowStartMS {
		return invalidRecord("cursor dashboard snapshot is invalid")
	}
	if snapshot.PlanUsage != nil && (snapshot.PlanUsage.TotalSpendMicros < 0 || snapshot.PlanUsage.IncludedSpendMicros < 0 ||
		snapshot.PlanUsage.BonusSpendMicros < 0 || snapshot.PlanUsage.RemainingMicros < 0 || snapshot.PlanUsage.LimitMicros < 0) {
		return invalidRecord("cursor dashboard plan usage is invalid")
	}
	quotaWindows := make(map[string]struct{}, len(snapshot.QuotaWindows))
	for _, window := range snapshot.QuotaWindows {
		if (window.LimitID != "cursor.models" && window.LimitID != "cursor.other_models") ||
			math.IsNaN(window.UsedPercent) || math.IsInf(window.UsedPercent, 0) ||
			window.UsedPercent < 0 || window.UsedPercent > 100 ||
			window.CycleStartAtMS < 0 || window.CycleEndAtMS <= window.CycleStartAtMS ||
			snapshot.CollectedAtMS < window.CycleStartAtMS || snapshot.CollectedAtMS > window.CycleEndAtMS {
			return invalidRecord("cursor dashboard quota window is invalid")
		}
		if _, exists := quotaWindows[window.LimitID]; exists {
			return invalidRecord("cursor dashboard quota window is duplicated")
		}
		quotaWindows[window.LimitID] = struct{}{}
	}
	seen := make(map[string]struct{}, len(snapshot.Events))
	for _, event := range snapshot.Events {
		if !validHexDigest(event.EventFingerprint) || event.OccurrenceCount <= 0 || event.OccurredAtMS < snapshot.WindowStartMS ||
			event.OccurredAtMS > snapshot.WindowEndMS || event.Kind < 0 || event.InputTokens < 0 || event.OutputTokens < 0 ||
			event.CacheWriteTokens < 0 || event.CacheReadTokens < 0 || event.ReportedChargeMicros < 0 ||
			event.CursorTokenFeeMicros < 0 || event.UpdatedAtMS < 0 ||
			(event.ExternalSessionID != nil && !safeCursorID(*event.ExternalSessionID)) ||
			(event.ModelKey != nil && !safeCursorLabel(*event.ModelKey)) {
			return invalidRecord("cursor dashboard usage event is invalid")
		}
		if _, exists := seen[event.EventFingerprint]; exists {
			return invalidRecord("cursor dashboard usage identity is duplicated")
		}
		seen[event.EventFingerprint] = struct{}{}
	}
	return nil
}

func safeCursorID(value string) bool {
	return len(value) >= 1 && len(value) <= 128 && !strings.ContainsAny(value, "\r\n\x00")
}
func safeCursorKey(value string) bool { return safeCursorID(value) }
func safeCursorLabel(value string) bool {
	return safeCursorID(value) && !strings.Contains(value, "/") && !strings.Contains(value, "\\")
}
func safeCursorTitle(value string) bool { return safeCursorID(value) }
func validCursorTitleSource(value string) bool {
	return value == "cursor_composer_header" || value == "cursor_conversation_search" || value == "fallback"
}
func validCursorCoverage(value string) bool {
	return value == "exact" || value == "partial" || value == "unknown"
}

func validCursorSourceState(value string) bool {
	return value == "available" || value == "partial" || value == "unavailable" || value == "not_configured"
}

func validCursorCheckpoint(value string) bool {
	return value == "snapshot" || value == "filesystem_scan" || value == "not_configured"
}

func validCursorFailure(value *string) bool {
	if value == nil {
		return true
	}
	switch *value {
	case "missing", "permission", "schema_incompatible", "busy", "corrupt", "read_failed", "not_configured", "auth_expired":
		return true
	default:
		return false
	}
}

func validHexDigest(value string) bool {
	if len(value) != 64 {
		return false
	}
	_, err := hex.DecodeString(value)
	return err == nil
}
