package store

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"unicode"
	"unicode/utf8"

	"gorm.io/gorm"

	reportingv1 "github.com/SisyphusSQ/codex-pulse/api/codexpulse/reporting/v1"
)

type dshSessionModel struct {
	Throughput         *string `gorm:"column:throughput"`
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

func (dshSessionModel) TableName() string { return "dsh_sessions" }

type dshLineageModel struct {
	SessionID     int64  `gorm:"column:session_id;primaryKey"`
	SourceKey     string `gorm:"column:source_key;primaryKey"`
	LineageKey    string `gorm:"column:lineage_key;primaryKey"`
	ContentDigest string `gorm:"column:content_digest"`
	ObservedAtMS  int64  `gorm:"column:observed_at_ms"`
}

func (dshLineageModel) TableName() string { return "dsh_session_lineage" }

type dshUsageModel struct {
	ModelProvider   string `gorm:"column:model_provider"`
	CacheReadKnown  bool   `gorm:"column:cache_read_known"`
	CacheWriteKnown bool   `gorm:"column:cache_write_known"`
	ReasoningKnown  bool   `gorm:"column:reasoning_known"`
	TotalKnown      bool   `gorm:"column:total_known"`
	StartedAtMS     *int64 `gorm:"column:started_at_ms"`
	EndedAtMS       *int64 `gorm:"column:ended_at_ms"`

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

func (dshUsageModel) TableName() string { return "dsh_usage_events" }

type dshToolModel struct {
	EventID           string `gorm:"column:event_id;primaryKey"`
	ExternalSessionID string `gorm:"column:external_session_id"`
	OccurredAtMS      int64  `gorm:"column:occurred_at_ms"`
	ToolName          string `gorm:"column:tool_name"`
	Outcome           string `gorm:"column:outcome"`
	Provenance        string `gorm:"column:provenance"`
	UpdatedAtMS       int64  `gorm:"column:updated_at_ms"`
}

func (dshToolModel) TableName() string { return "dsh_tool_events" }

func (repository *Repository) ReplaceDSHSnapshot(ctx context.Context, snapshot DSHSnapshot) error {
	if repository == nil || repository.database == nil || ctx == nil {
		return ErrInvalidRepository
	}
	snapshot.Sessions = append([]DSHSession(nil), snapshot.Sessions...)
	for index := range snapshot.Sessions {
		normalizeDSHSessionPresentation(&snapshot.Sessions[index])
	}
	if err := validateDSHSnapshot(snapshot); err != nil {
		return err
	}
	return repository.database.Write(ctx, func(ctx context.Context, transaction *gorm.DB) error {
		database := transaction.WithContext(ctx)
		// An unchanged canonical file needs no fact rewrites. Source health still advances.
		digests := make(map[string]string, len(snapshot.Lineage))
		for _, lineage := range snapshot.Lineage {
			digests[lineage.ExternalSessionID] = lineage.ContentDigest
		}
		unchanged := make(map[string]bool)
		for _, session := range snapshot.Sessions {
			if digest := digests[session.ExternalSessionID]; digest != "" {
				var count int64
				err := database.Table("dsh_session_lineage AS l").Joins("JOIN dsh_sessions AS s ON s.id = l.session_id").Where("s.external_session_id = ? AND l.content_digest = ? AND s.lineage_conflict = ? AND s.coverage_state = ? AND s.display_title = ? AND s.title_source = ?", session.ExternalSessionID, digest, session.LineageConflict, session.CoverageState, session.DisplayTitle, session.TitleSource).Count(&count).Error
				if err != nil {
					return err
				}
				unchanged[session.ExternalSessionID] = count > 0
			}
		}
		// Keep indexed history when files are moved, unavailable or malformed.
		for _, session := range snapshot.Sessions {
			if unchanged[session.ExternalSessionID] {
				continue
			}
			for _, model := range []any{&dshUsageModel{}, &dshToolModel{}, &dshSessionModel{}} {
				if err := database.Where("external_session_id = ?", session.ExternalSessionID).Delete(model).Error; err != nil {
					return err
				}
			}
		}
		if err := database.Where("provider = ?", "dsh").Delete(&cursorSourceModel{}).Error; err != nil {
			return err
		}
		if err := database.Save(&cursorSnapshotModel{
			Provider: "dsh", Generation: snapshot.Generation, CollectedAtMS: snapshot.CollectedAtMS,
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
			if unchanged[session.ExternalSessionID] {
				continue
			}
			var capsule *string
			if session.Throughput != nil {
				encoded, err := json.Marshal(session.Throughput)
				if err != nil {
					return err
				}
				value := string(encoded)
				capsule = &value
			}
			model := dshSessionModel{Throughput: capsule,
				Provider: "dsh", ExternalSessionID: session.ExternalSessionID,
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
			if unchanged[lineage.ExternalSessionID] {
				continue
			}
			sessionID, ok := sessionIDs[lineage.ExternalSessionID]
			if !ok {
				return invalidRecord("dsh lineage session is missing")
			}
			if err := database.Create(&dshLineageModel{
				SessionID: sessionID, SourceKey: lineage.SourceKey, LineageKey: lineage.LineageKey,
				ContentDigest: lineage.ContentDigest, ObservedAtMS: lineage.ObservedAtMS,
			}).Error; err != nil {
				return err
			}
		}
		for _, event := range snapshot.UsageEvents {
			if unchanged[event.ExternalSessionID] {
				continue
			}
			if err := database.Create(&dshUsageModel{
				ModelProvider: event.ModelProvider, CacheReadKnown: event.CacheReadKnown,
				CacheWriteKnown: event.CacheWriteKnown, ReasoningKnown: event.ReasoningKnown, TotalKnown: event.TotalKnown,
				StartedAtMS: event.StartedAtMS, EndedAtMS: event.EndedAtMS,
				EventID: event.EventID, ExternalSessionID: event.ExternalSessionID, OccurredAtMS: event.OccurredAtMS,
				ModelKey: event.ModelKey, InputTokens: event.InputTokens, OutputTokens: event.OutputTokens,
				CachedReadTokens: event.CachedReadTokens, CacheCreationTokens: event.CacheCreationTokens,
				ReasoningTokens: event.ReasoningTokens, TotalTokens: event.TotalTokens,
				ReportedCostMicros: event.ReportedCostMicros, Provenance: "dsh_assistant_message",
				UpdatedAtMS: event.UpdatedAtMS,
			}).Error; err != nil {
				return err
			}
		}
		for _, event := range snapshot.ToolEvents {
			if unchanged[event.ExternalSessionID] {
				continue
			}
			if err := database.Create(&dshToolModel{
				EventID: event.EventID, ExternalSessionID: event.ExternalSessionID, OccurredAtMS: event.OccurredAtMS,
				ToolName: event.ToolName, Outcome: event.Outcome, Provenance: "dsh_updates",
				UpdatedAtMS: event.UpdatedAtMS,
			}).Error; err != nil {
				return err
			}
		}
		return nil
	})
}

func (repository *Repository) DSHSnapshot(ctx context.Context) (DSHSnapshot, error) {
	if repository == nil || repository.database == nil || ctx == nil {
		return DSHSnapshot{}, ErrInvalidRepository
	}
	var result DSHSnapshot
	err := repository.database.ViewSnapshot(ctx, func(ctx context.Context, database *gorm.DB) error {
		var snapshot cursorSnapshotModel
		if err := database.WithContext(ctx).Where("provider = ?", "dsh").Take(&snapshot).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return ErrNotFound
			}
			return err
		}
		result.Generation, result.CollectedAtMS = snapshot.Generation, snapshot.CollectedAtMS
		var sources []cursorSourceModel
		if err := database.WithContext(ctx).Where("provider = ?", "dsh").Order("source_key").Find(&sources).Error; err != nil {
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

		}
		var sessions []dshSessionModel
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
			result.Sessions = append(result.Sessions, DSHSession{Throughput: capsule,
				ExternalSessionID: session.ExternalSessionID, DisplayTitle: session.DisplayTitle,
				TitleSource: session.TitleSource, ProjectKey: session.ProjectKey,
				ProjectDisplayName: session.ProjectDisplayName, CreatedAtMS: session.CreatedAtMS,
				LastActivityAtMS: session.LastActivityAtMS, ModelKey: session.ModelKey,
				RequestCount: session.RequestCount, ToolCallCount: session.ToolCallCount,
				LineageConflict: session.LineageConflict, CoverageState: session.CoverageState,
				UpdatedAtMS: session.UpdatedAtMS,
			})
		}
		var lineage []dshLineageModel
		if err := database.WithContext(ctx).Find(&lineage).Error; err != nil {
			return err
		}
		for _, item := range lineage {
			externalID := ids[item.SessionID]
			if externalID == "" {
				continue
			}
			result.Lineage = append(result.Lineage, DSHSessionLineage{
				ExternalSessionID: externalID, SourceKey: item.SourceKey, LineageKey: item.LineageKey,
				ContentDigest: item.ContentDigest, ObservedAtMS: item.ObservedAtMS,
			})
		}
		var usage []dshUsageModel
		if err := database.WithContext(ctx).Order("occurred_at_ms, event_id").Find(&usage).Error; err != nil {
			return err
		}
		for _, event := range usage {
			result.UsageEvents = append(result.UsageEvents, DSHUsageEvent{
				ModelProvider: event.ModelProvider, CacheReadKnown: event.CacheReadKnown,
				CacheWriteKnown: event.CacheWriteKnown, ReasoningKnown: event.ReasoningKnown, TotalKnown: event.TotalKnown,
				StartedAtMS: event.StartedAtMS, EndedAtMS: event.EndedAtMS,
				EventID: event.EventID, ExternalSessionID: event.ExternalSessionID, OccurredAtMS: event.OccurredAtMS,
				ModelKey: event.ModelKey, InputTokens: event.InputTokens, OutputTokens: event.OutputTokens,
				CachedReadTokens: event.CachedReadTokens, CacheCreationTokens: event.CacheCreationTokens,
				ReasoningTokens: event.ReasoningTokens, TotalTokens: event.TotalTokens,
				ReportedCostMicros: event.ReportedCostMicros, UpdatedAtMS: event.UpdatedAtMS,
			})
		}
		var tools []dshToolModel
		if err := database.WithContext(ctx).Order("occurred_at_ms, event_id").Find(&tools).Error; err != nil {
			return err
		}
		for _, event := range tools {
			result.ToolEvents = append(result.ToolEvents, DSHToolEvent{
				EventID: event.EventID, ExternalSessionID: event.ExternalSessionID, OccurredAtMS: event.OccurredAtMS,
				ToolName: event.ToolName, Outcome: event.Outcome, UpdatedAtMS: event.UpdatedAtMS,
			})
		}
		return nil
	})
	return result, err
}

func validateDSHSnapshot(snapshot DSHSnapshot) error {
	if snapshot.Generation < 0 || snapshot.CollectedAtMS < 0 {
		return invalidRecord("dsh snapshot identity is invalid")
	}
	sessions := make(map[string]struct{}, len(snapshot.Sessions))
	for _, session := range snapshot.Sessions {
		if !safeCursorID(session.ExternalSessionID) || !validHexDigest(session.ProjectKey) ||
			!safeDSHTitle(session.DisplayTitle) || !validDSHTitleSource(session.TitleSource) ||
			!safeCursorLabel(session.ProjectDisplayName) || session.CreatedAtMS < 0 ||
			session.LastActivityAtMS < session.CreatedAtMS || session.RequestCount < 0 ||
			session.ToolCallCount < 0 ||
			(session.ModelKey != nil && !safeCursorLabel(*session.ModelKey)) ||
			!validCursorCoverage(session.CoverageState) || session.UpdatedAtMS < 0 {
			return invalidRecord("dsh session is invalid")
		}
		if _, exists := sessions[session.ExternalSessionID]; exists {
			return invalidRecord("dsh session identity is duplicated")
		}
		sessions[session.ExternalSessionID] = struct{}{}
	}
	for _, lineage := range snapshot.Lineage {
		if _, ok := sessions[lineage.ExternalSessionID]; !ok || !safeCursorKey(lineage.SourceKey) ||
			!validHexDigest(lineage.LineageKey) || !validHexDigest(lineage.ContentDigest) || lineage.ObservedAtMS < 0 {
			return invalidRecord("dsh lineage is invalid")
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
			return invalidRecord("dsh usage event is invalid")
		}
		if _, exists := seenUsage[event.EventID]; exists {
			return invalidRecord("dsh usage identity is duplicated")
		}
		seenUsage[event.EventID] = struct{}{}
	}
	for _, event := range snapshot.ToolEvents {
		_, sessionExists := sessions[event.ExternalSessionID]
		if !validHexDigest(event.EventID) || !sessionExists || event.OccurredAtMS < 0 || event.UpdatedAtMS < 0 ||
			!safeCursorLabel(event.ToolName) ||
			(event.Outcome != "succeeded" && event.Outcome != "failed" && event.Outcome != "unknown") {
			return invalidRecord("dsh tool event is invalid")
		}
	}
	for _, source := range snapshot.Sources {
		if source.Provider != "dsh" || !safeCursorKey(source.SourceKey) || !safeCursorLabel(source.SourceType) ||
			source.RowCount < 0 || source.LastAttemptAtMS < 0 || source.UpdatedAtMS < 0 ||
			(source.SchemaVersion != nil && *source.SchemaVersion < 0) ||
			(source.LastSuccessAtMS != nil && *source.LastSuccessAtMS < 0) ||
			(source.CheckpointValue != nil && !safeCursorID(*source.CheckpointValue)) ||
			!validCursorSourceState(source.State) || !validCursorCheckpoint(source.CheckpointKind) ||
			!validCursorFailure(source.FailureCode) || !validCursorCoverage(source.CoverageState) {
			return invalidRecord("dsh source status is invalid")
		}
	}
	return nil
}

func normalizeDSHSessionPresentation(session *DSHSession) {
	if session.DisplayTitle == "" {
		session.DisplayTitle = "未命名会话"
	}
	if session.TitleSource == "" {
		session.TitleSource = "fallback"
	}
}

func validDSHTitleSource(value string) bool {
	return value == "dsh_header" || value == "dsh_title_event" || value == "fallback"
}

func safeDSHTitle(value string) bool {
	return utf8.ValidString(value) && strings.TrimSpace(value) != "" &&
		utf8.RuneCountInString(value) <= 512 && !strings.ContainsFunc(value, unicode.IsControl)
}
