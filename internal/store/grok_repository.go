package store

import (
	"context"
	"errors"
	"fmt"
	"math"

	"gorm.io/gorm"
)

const grokBillingSourceKey = "grok.billing"

type grokSessionModel struct {
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
	LineageConflict    bool    `gorm:"column:lineage_conflict"`
	CoverageState      string  `gorm:"column:coverage_state"`
	UpdatedAtMS        int64   `gorm:"column:updated_at_ms"`
}

func (grokSessionModel) TableName() string { return "grok_sessions" }

type grokLineageModel struct {
	SessionID     int64  `gorm:"column:session_id;primaryKey"`
	SourceKey     string `gorm:"column:source_key;primaryKey"`
	LineageKey    string `gorm:"column:lineage_key;primaryKey"`
	ContentDigest string `gorm:"column:content_digest"`
	ObservedAtMS  int64  `gorm:"column:observed_at_ms"`
}

func (grokLineageModel) TableName() string { return "grok_session_lineage" }

type grokUsageModel struct {
	EventID             string  `gorm:"column:event_id;primaryKey"`
	ExternalSessionID   string  `gorm:"column:external_session_id"`
	OccurredAtMS        int64   `gorm:"column:occurred_at_ms"`
	ModelKey            *string `gorm:"column:model_key"`
	InputTokens         int64   `gorm:"column:input_tokens"`
	OutputTokens        int64   `gorm:"column:output_tokens"`
	CachedReadTokens    int64   `gorm:"column:cached_read_tokens"`
	CacheCreationTokens int64   `gorm:"column:cache_creation_tokens"`
	ReasoningTokens     int64   `gorm:"column:reasoning_tokens"`
	TotalTokens         int64   `gorm:"column:total_tokens"`
	ReportedCostMicros  *int64  `gorm:"column:reported_cost_micros"`
	Provenance          string  `gorm:"column:provenance"`
	UpdatedAtMS         int64   `gorm:"column:updated_at_ms"`
}

func (grokUsageModel) TableName() string { return "grok_usage_events" }

type grokToolModel struct {
	EventID           string `gorm:"column:event_id;primaryKey"`
	ExternalSessionID string `gorm:"column:external_session_id"`
	OccurredAtMS      int64  `gorm:"column:occurred_at_ms"`
	ToolName          string `gorm:"column:tool_name"`
	Outcome           string `gorm:"column:outcome"`
	Provenance        string `gorm:"column:provenance"`
	UpdatedAtMS       int64  `gorm:"column:updated_at_ms"`
}

func (grokToolModel) TableName() string { return "grok_tool_events" }

type grokBillingSnapshotModel struct {
	Provider         string   `gorm:"column:provider;primaryKey"`
	Generation       int64    `gorm:"column:generation"`
	CollectedAtMS    int64    `gorm:"column:collected_at_ms"`
	PeriodType       string   `gorm:"column:period_type"`
	PeriodStartMS    int64    `gorm:"column:period_start_ms"`
	PeriodEndMS      int64    `gorm:"column:period_end_ms"`
	UsedPercent      float64  `gorm:"column:used_percent"`
	OnDemandUsed     *float64 `gorm:"column:on_demand_used"`
	OnDemandCap      *float64 `gorm:"column:on_demand_cap"`
	PrepaidBalance   *float64 `gorm:"column:prepaid_balance"`
	SubscriptionTier *string  `gorm:"column:subscription_tier"`
	IsUnifiedBilling bool     `gorm:"column:is_unified_billing"`
}

func (grokBillingSnapshotModel) TableName() string { return "grok_billing_snapshots" }

type grokBillingQuotaObservationModel struct {
	Provider       string  `gorm:"column:provider;primaryKey"`
	Generation     int64   `gorm:"column:generation;primaryKey"`
	LimitID        string  `gorm:"column:limit_id;primaryKey"`
	UsedPercent    float64 `gorm:"column:used_percent"`
	CycleStartAtMS int64   `gorm:"column:cycle_start_at_ms"`
	CycleEndAtMS   int64   `gorm:"column:cycle_end_at_ms"`
	ObservedAtMS   int64   `gorm:"column:observed_at_ms"`
}

func (grokBillingQuotaObservationModel) TableName() string {
	return "grok_billing_quota_observations"
}

func (repository *Repository) ReplaceGrokSnapshot(ctx context.Context, snapshot GrokSnapshot) error {
	if repository == nil || repository.database == nil || ctx == nil {
		return ErrInvalidRepository
	}
	snapshot.Sessions = append([]GrokSession(nil), snapshot.Sessions...)
	for index := range snapshot.Sessions {
		normalizeGrokSessionPresentation(&snapshot.Sessions[index])
	}
	if err := validateGrokSnapshot(snapshot); err != nil {
		return err
	}
	return repository.database.Write(ctx, func(ctx context.Context, transaction *gorm.DB) error {
		database := transaction.WithContext(ctx)
		for _, table := range []string{
			"grok_session_lineage", "grok_tool_events", "grok_usage_events", "grok_sessions",
		} {
			if err := database.Exec("DELETE FROM " + table).Error; err != nil {
				return err
			}
		}
		if err := database.Where("provider = ? AND source_key <> ?", "grok", grokBillingSourceKey).
			Delete(&cursorSourceModel{}).Error; err != nil {
			return err
		}
		if err := database.Save(&cursorSnapshotModel{
			Provider: "grok", Generation: snapshot.Generation, CollectedAtMS: snapshot.CollectedAtMS,
		}).Error; err != nil {
			return err
		}
		for _, source := range snapshot.Sources {
			if source.SourceKey == grokBillingSourceKey {
				continue
			}
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
			model := grokSessionModel{
				Provider: "grok", ExternalSessionID: session.ExternalSessionID,
				DisplayTitle: session.DisplayTitle, TitleSource: session.TitleSource,
				ProjectKey: session.ProjectKey, ProjectDisplayName: session.ProjectDisplayName,
				CreatedAtMS: session.CreatedAtMS, LastActivityAtMS: session.LastActivityAtMS,
				ModelKey: session.ModelKey, RequestCount: session.RequestCount,
				ToolCallCount: session.ToolCallCount, LineageConflict: session.LineageConflict,
				CoverageState: session.CoverageState, UpdatedAtMS: session.UpdatedAtMS,
			}
			if err := database.Create(&model).Error; err != nil {
				return err
			}
			sessionIDs[session.ExternalSessionID] = model.ID
		}
		for _, lineage := range snapshot.Lineage {
			sessionID, ok := sessionIDs[lineage.ExternalSessionID]
			if !ok {
				return invalidRecord("grok lineage session is missing")
			}
			if err := database.Create(&grokLineageModel{
				SessionID: sessionID, SourceKey: lineage.SourceKey, LineageKey: lineage.LineageKey,
				ContentDigest: lineage.ContentDigest, ObservedAtMS: lineage.ObservedAtMS,
			}).Error; err != nil {
				return err
			}
		}
		for _, event := range snapshot.UsageEvents {
			if err := database.Create(&grokUsageModel{
				EventID: event.EventID, ExternalSessionID: event.ExternalSessionID, OccurredAtMS: event.OccurredAtMS,
				ModelKey: event.ModelKey, InputTokens: event.InputTokens, OutputTokens: event.OutputTokens,
				CachedReadTokens: event.CachedReadTokens, CacheCreationTokens: event.CacheCreationTokens,
				ReasoningTokens: event.ReasoningTokens, TotalTokens: event.TotalTokens,
				ReportedCostMicros: event.ReportedCostMicros, Provenance: "grok_turn_completed",
				UpdatedAtMS: event.UpdatedAtMS,
			}).Error; err != nil {
				return err
			}
		}
		for _, event := range snapshot.ToolEvents {
			if err := database.Create(&grokToolModel{
				EventID: event.EventID, ExternalSessionID: event.ExternalSessionID, OccurredAtMS: event.OccurredAtMS,
				ToolName: event.ToolName, Outcome: event.Outcome, Provenance: "grok_updates",
				UpdatedAtMS: event.UpdatedAtMS,
			}).Error; err != nil {
				return err
			}
		}
		return nil
	})
}

func (repository *Repository) CommitGrokBillingSnapshot(ctx context.Context, snapshot GrokBillingSnapshot) error {
	if repository == nil || repository.database == nil || ctx == nil {
		return ErrInvalidRepository
	}
	if err := validateGrokBillingSnapshot(snapshot); err != nil {
		return err
	}
	return repository.database.Write(ctx, func(ctx context.Context, transaction *gorm.DB) error {
		database := transaction.WithContext(ctx)
		if err := database.Exec("DELETE FROM grok_billing_quota_observations").Error; err != nil {
			return err
		}
		if err := database.Save(&grokBillingSnapshotModel{
			Provider: "grok", Generation: snapshot.Generation, CollectedAtMS: snapshot.CollectedAtMS,
			PeriodType: snapshot.PeriodType, PeriodStartMS: snapshot.PeriodStartMS, PeriodEndMS: snapshot.PeriodEndMS,
			UsedPercent: snapshot.UsedPercent, OnDemandUsed: snapshot.OnDemandUsed, OnDemandCap: snapshot.OnDemandCap,
			PrepaidBalance: snapshot.PrepaidBalance, SubscriptionTier: snapshot.SubscriptionTier,
			IsUnifiedBilling: snapshot.IsUnifiedBilling,
		}).Error; err != nil {
			return err
		}
		for _, observation := range snapshot.QuotaObservations {
			if err := database.Save(&grokBillingQuotaObservationModel{
				Provider: "grok", Generation: observation.Generation, LimitID: observation.LimitID,
				UsedPercent: observation.UsedPercent, CycleStartAtMS: observation.CycleStartAtMS,
				CycleEndAtMS: observation.CycleEndAtMS, ObservedAtMS: observation.ObservedAtMS,
			}).Error; err != nil {
				return err
			}
		}
		checkpoint := fmt.Sprintf("%d", snapshot.Generation)
		lastSuccess := snapshot.CollectedAtMS
		return database.Save(&cursorSourceModel{
			Provider: "grok", SourceKey: grokBillingSourceKey, SourceType: "cli_authenticated_rpc",
			State: "available", CoverageState: "exact", CheckpointKind: "snapshot",
			CheckpointValue: &checkpoint, RowCount: 1,
			LastAttemptAtMS: snapshot.CollectedAtMS, LastSuccessAtMS: &lastSuccess,
			UpdatedAtMS: snapshot.CollectedAtMS,
		}).Error
	})
}

func (repository *Repository) RecordGrokBillingFailure(ctx context.Context, atMS int64, failureCode string) error {
	if repository == nil || repository.database == nil || ctx == nil || atMS < 0 ||
		(failureCode != "auth_expired" && failureCode != "schema_incompatible" && failureCode != "read_failed") {
		return ErrInvalidRepository
	}
	return repository.database.Write(ctx, func(ctx context.Context, transaction *gorm.DB) error {
		database := transaction.WithContext(ctx)
		var source cursorSourceModel
		err := database.Where("provider = ? AND source_key = ?", "grok", grokBillingSourceKey).Take(&source).Error
		if err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
			return err
		}
		if errors.Is(err, gorm.ErrRecordNotFound) {
			source = cursorSourceModel{
				Provider: "grok", SourceKey: grokBillingSourceKey, SourceType: "cli_authenticated_rpc",
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

func (repository *Repository) GrokSnapshot(ctx context.Context) (GrokSnapshot, error) {
	if repository == nil || repository.database == nil || ctx == nil {
		return GrokSnapshot{}, ErrInvalidRepository
	}
	var result GrokSnapshot
	err := repository.database.ViewSnapshot(ctx, func(ctx context.Context, database *gorm.DB) error {
		var snapshot cursorSnapshotModel
		if err := database.WithContext(ctx).Where("provider = ?", "grok").Take(&snapshot).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return ErrNotFound
			}
			return err
		}
		result.Generation, result.CollectedAtMS = snapshot.Generation, snapshot.CollectedAtMS
		var sources []cursorSourceModel
		if err := database.WithContext(ctx).Where("provider = ?", "grok").Order("source_key").Find(&sources).Error; err != nil {
			return err
		}
		for _, source := range sources {
			status := CursorSourceStatus{
				Provider: source.Provider, SourceKey: source.SourceKey, SourceType: source.SourceType,
				State: source.State, CoverageState: source.CoverageState, SchemaVersion: source.SchemaVersion,
				CheckpointKind: source.CheckpointKind, CheckpointValue: source.CheckpointValue,
				RowCount: source.RowCount, LastAttemptAtMS: source.LastAttemptAtMS,
				LastSuccessAtMS: source.LastSuccessAtMS, FailureCode: source.FailureCode, UpdatedAtMS: source.UpdatedAtMS,
			}
			result.Sources = append(result.Sources, status)
			if source.SourceKey == grokBillingSourceKey && source.State != "available" {
				result.BillingStale = source.LastSuccessAtMS != nil
				result.BillingFailureCode = source.FailureCode
			}
		}
		var sessions []grokSessionModel
		if err := database.WithContext(ctx).Order("last_activity_at_ms DESC, external_session_id DESC").Find(&sessions).Error; err != nil {
			return err
		}
		ids := make(map[int64]string, len(sessions))
		for _, session := range sessions {
			ids[session.ID] = session.ExternalSessionID
			result.Sessions = append(result.Sessions, GrokSession{
				ExternalSessionID: session.ExternalSessionID, DisplayTitle: session.DisplayTitle,
				TitleSource: session.TitleSource, ProjectKey: session.ProjectKey,
				ProjectDisplayName: session.ProjectDisplayName, CreatedAtMS: session.CreatedAtMS,
				LastActivityAtMS: session.LastActivityAtMS, ModelKey: session.ModelKey,
				RequestCount: session.RequestCount, ToolCallCount: session.ToolCallCount,
				LineageConflict: session.LineageConflict, CoverageState: session.CoverageState,
				UpdatedAtMS: session.UpdatedAtMS,
			})
		}
		var lineage []grokLineageModel
		if err := database.WithContext(ctx).Find(&lineage).Error; err != nil {
			return err
		}
		for _, item := range lineage {
			externalID := ids[item.SessionID]
			if externalID == "" {
				continue
			}
			result.Lineage = append(result.Lineage, GrokSessionLineage{
				ExternalSessionID: externalID, SourceKey: item.SourceKey, LineageKey: item.LineageKey,
				ContentDigest: item.ContentDigest, ObservedAtMS: item.ObservedAtMS,
			})
		}
		var usage []grokUsageModel
		if err := database.WithContext(ctx).Order("occurred_at_ms, event_id").Find(&usage).Error; err != nil {
			return err
		}
		for _, event := range usage {
			result.UsageEvents = append(result.UsageEvents, GrokUsageEvent{
				EventID: event.EventID, ExternalSessionID: event.ExternalSessionID, OccurredAtMS: event.OccurredAtMS,
				ModelKey: event.ModelKey, InputTokens: event.InputTokens, OutputTokens: event.OutputTokens,
				CachedReadTokens: event.CachedReadTokens, CacheCreationTokens: event.CacheCreationTokens,
				ReasoningTokens: event.ReasoningTokens, TotalTokens: event.TotalTokens,
				ReportedCostMicros: event.ReportedCostMicros, UpdatedAtMS: event.UpdatedAtMS,
			})
		}
		var tools []grokToolModel
		if err := database.WithContext(ctx).Order("occurred_at_ms, event_id").Find(&tools).Error; err != nil {
			return err
		}
		for _, event := range tools {
			result.ToolEvents = append(result.ToolEvents, GrokToolEvent{
				EventID: event.EventID, ExternalSessionID: event.ExternalSessionID, OccurredAtMS: event.OccurredAtMS,
				ToolName: event.ToolName, Outcome: event.Outcome, UpdatedAtMS: event.UpdatedAtMS,
			})
		}
		var billing grokBillingSnapshotModel
		err := database.WithContext(ctx).Where("provider = ?", "grok").Take(&billing).Error
		if err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
			return err
		}
		if err == nil {
			value := GrokBillingSnapshot{
				Generation: billing.Generation, CollectedAtMS: billing.CollectedAtMS,
				PeriodType: billing.PeriodType, PeriodStartMS: billing.PeriodStartMS, PeriodEndMS: billing.PeriodEndMS,
				UsedPercent: billing.UsedPercent, OnDemandUsed: billing.OnDemandUsed, OnDemandCap: billing.OnDemandCap,
				PrepaidBalance: billing.PrepaidBalance, SubscriptionTier: billing.SubscriptionTier,
				IsUnifiedBilling: billing.IsUnifiedBilling,
			}
			var observations []grokBillingQuotaObservationModel
			if err := database.WithContext(ctx).Where("provider = ?", "grok").
				Order("generation, limit_id").Find(&observations).Error; err != nil {
				return err
			}
			for _, observation := range observations {
				value.QuotaObservations = append(value.QuotaObservations, GrokBillingQuotaObservation{
					Generation: observation.Generation, LimitID: observation.LimitID,
					UsedPercent: observation.UsedPercent, CycleStartAtMS: observation.CycleStartAtMS,
					CycleEndAtMS: observation.CycleEndAtMS, ObservedAtMS: observation.ObservedAtMS,
				})
			}
			result.Billing = &value
		}
		return nil
	})
	return result, err
}

func validateGrokSnapshot(snapshot GrokSnapshot) error {
	if snapshot.Generation < 0 || snapshot.CollectedAtMS < 0 {
		return invalidRecord("grok snapshot identity is invalid")
	}
	sessions := make(map[string]struct{}, len(snapshot.Sessions))
	for _, session := range snapshot.Sessions {
		if !safeCursorID(session.ExternalSessionID) || !validHexDigest(session.ProjectKey) ||
			!safeCursorTitle(session.DisplayTitle) || !validGrokTitleSource(session.TitleSource) ||
			!safeCursorLabel(session.ProjectDisplayName) || session.CreatedAtMS < 0 ||
			session.LastActivityAtMS < session.CreatedAtMS || session.RequestCount < 0 ||
			session.ToolCallCount < 0 ||
			(session.ModelKey != nil && !safeCursorLabel(*session.ModelKey)) ||
			!validCursorCoverage(session.CoverageState) || session.UpdatedAtMS < 0 {
			return invalidRecord("grok session is invalid")
		}
		if _, exists := sessions[session.ExternalSessionID]; exists {
			return invalidRecord("grok session identity is duplicated")
		}
		sessions[session.ExternalSessionID] = struct{}{}
	}
	for _, lineage := range snapshot.Lineage {
		if _, ok := sessions[lineage.ExternalSessionID]; !ok || !safeCursorKey(lineage.SourceKey) ||
			!validHexDigest(lineage.LineageKey) || !validHexDigest(lineage.ContentDigest) || lineage.ObservedAtMS < 0 {
			return invalidRecord("grok lineage is invalid")
		}
	}
	seenUsage := make(map[string]struct{}, len(snapshot.UsageEvents))
	for _, event := range snapshot.UsageEvents {
		_, sessionExists := sessions[event.ExternalSessionID]
		if !safeCursorID(event.EventID) || !sessionExists || event.OccurredAtMS < 0 ||
			event.InputTokens < 0 || event.OutputTokens < 0 || event.CachedReadTokens < 0 ||
			event.CacheCreationTokens < 0 || event.ReasoningTokens < 0 || event.TotalTokens < 0 ||
			event.UpdatedAtMS < 0 ||
			(event.ModelKey != nil && !safeCursorLabel(*event.ModelKey)) ||
			(event.ReportedCostMicros != nil && *event.ReportedCostMicros < 0) {
			return invalidRecord("grok usage event is invalid")
		}
		if _, exists := seenUsage[event.EventID]; exists {
			return invalidRecord("grok usage identity is duplicated")
		}
		seenUsage[event.EventID] = struct{}{}
	}
	for _, event := range snapshot.ToolEvents {
		_, sessionExists := sessions[event.ExternalSessionID]
		if !validHexDigest(event.EventID) || !sessionExists || event.OccurredAtMS < 0 || event.UpdatedAtMS < 0 ||
			!safeCursorLabel(event.ToolName) ||
			(event.Outcome != "succeeded" && event.Outcome != "failed" && event.Outcome != "unknown") {
			return invalidRecord("grok tool event is invalid")
		}
	}
	for _, source := range snapshot.Sources {
		if source.Provider != "grok" || !safeCursorKey(source.SourceKey) || !safeCursorLabel(source.SourceType) ||
			source.RowCount < 0 || source.LastAttemptAtMS < 0 || source.UpdatedAtMS < 0 ||
			(source.SchemaVersion != nil && *source.SchemaVersion < 0) ||
			(source.LastSuccessAtMS != nil && *source.LastSuccessAtMS < 0) ||
			(source.CheckpointValue != nil && !safeCursorID(*source.CheckpointValue)) ||
			!validCursorSourceState(source.State) || !validCursorCheckpoint(source.CheckpointKind) ||
			!validCursorFailure(source.FailureCode) || !validCursorCoverage(source.CoverageState) {
			return invalidRecord("grok source status is invalid")
		}
	}
	return nil
}

func validateGrokBillingSnapshot(snapshot GrokBillingSnapshot) error {
	if snapshot.Generation < 0 || snapshot.CollectedAtMS < 0 ||
		(snapshot.PeriodType != "weekly" && snapshot.PeriodType != "monthly") ||
		snapshot.PeriodStartMS < 0 || snapshot.PeriodEndMS <= snapshot.PeriodStartMS ||
		math.IsNaN(snapshot.UsedPercent) || math.IsInf(snapshot.UsedPercent, 0) ||
		snapshot.UsedPercent < 0 || snapshot.UsedPercent > 100 {
		return invalidRecord("grok billing snapshot is invalid")
	}
	if snapshot.OnDemandUsed != nil && (math.IsNaN(*snapshot.OnDemandUsed) || math.IsInf(*snapshot.OnDemandUsed, 0) || *snapshot.OnDemandUsed < 0) {
		return invalidRecord("grok billing on-demand used is invalid")
	}
	if snapshot.OnDemandCap != nil && (math.IsNaN(*snapshot.OnDemandCap) || math.IsInf(*snapshot.OnDemandCap, 0) || *snapshot.OnDemandCap < 0) {
		return invalidRecord("grok billing on-demand cap is invalid")
	}
	if snapshot.PrepaidBalance != nil && (math.IsNaN(*snapshot.PrepaidBalance) || math.IsInf(*snapshot.PrepaidBalance, 0) || *snapshot.PrepaidBalance < 0) {
		return invalidRecord("grok billing prepaid balance is invalid")
	}
	if snapshot.SubscriptionTier != nil && !safeCursorLabel(*snapshot.SubscriptionTier) {
		return invalidRecord("grok billing subscription tier is invalid")
	}
	seen := make(map[string]struct{}, len(snapshot.QuotaObservations))
	for _, observation := range snapshot.QuotaObservations {
		if !safeCursorID(observation.LimitID) || observation.Generation < 0 ||
			math.IsNaN(observation.UsedPercent) || math.IsInf(observation.UsedPercent, 0) ||
			observation.UsedPercent < 0 || observation.UsedPercent > 100 ||
			observation.CycleStartAtMS < 0 || observation.CycleEndAtMS <= observation.CycleStartAtMS ||
			observation.ObservedAtMS < 0 {
			return invalidRecord("grok billing quota observation is invalid")
		}
		if _, exists := seen[observation.LimitID]; exists {
			return invalidRecord("grok billing quota observation is duplicated")
		}
		seen[observation.LimitID] = struct{}{}
	}
	return nil
}

func normalizeGrokSessionPresentation(session *GrokSession) {
	if session.DisplayTitle == "" {
		session.DisplayTitle = "未命名会话"
	}
	if session.TitleSource == "" {
		session.TitleSource = "fallback"
	}
}

func validGrokTitleSource(value string) bool {
	return value == "grok_summary" || value == "fallback"
}
