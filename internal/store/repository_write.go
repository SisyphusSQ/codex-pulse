package store

import (
	"context"
	"errors"

	"gorm.io/gorm"

	storesqlite "github.com/SisyphusSQ/codex-pulse/internal/store/sqlite"
)

func upsertProject(ctx context.Context, transaction storesqlite.WriteTx, project Project) error {
	database := transaction.WithContext(ctx)
	var existing projectModel
	err := database.Take(&existing, "project_id = ?", project.ProjectID).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return database.Create(&projectModel{
			ProjectID: project.ProjectID, DisplayName: project.DisplayName,
			RootPath: project.RootPath, GitRemoteSanitized: project.GitRemoteSanitized,
			CreatedAtMS: project.CreatedAtMS, UpdatedAtMS: project.UpdatedAtMS,
		}).Error
	}
	if err != nil {
		return err
	}
	updates := map[string]any{"created_at_ms": min(existing.CreatedAtMS, project.CreatedAtMS)}
	if project.UpdatedAtMS > existing.UpdatedAtMS {
		updates["display_name"] = project.DisplayName
		updates["root_path"] = project.RootPath
		updates["updated_at_ms"] = project.UpdatedAtMS
		if project.GitRemoteSanitized != nil {
			updates["git_remote_sanitized"] = *project.GitRemoteSanitized
		}
	}
	return database.Model(&projectModel{}).Where("project_id = ?", project.ProjectID).Updates(updates).Error
}

func upsertSession(ctx context.Context, transaction storesqlite.WriteTx, session Session) error {
	database := transaction.WithContext(ctx)
	var existing sessionModel
	err := database.Take(&existing, "session_id = ?", session.SessionID).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return database.Create(&sessionModel{
			SessionID: session.SessionID, Provider: session.Provider, Originator: session.Originator,
			SourceKind: session.SourceKind, ModelProvider: session.ModelProvider,
			InitialCWD: session.InitialCWD, ProjectID: session.ProjectID, CLIVersion: session.CLIVersion,
			CreatedAtMS: session.CreatedAtMS, FirstSeenAtMS: session.FirstSeenAtMS,
			LastSeenAtMS: session.LastSeenAtMS,
		}).Error
	}
	if err != nil {
		return err
	}
	updates := map[string]any{
		"created_at_ms":    min(existing.CreatedAtMS, session.CreatedAtMS),
		"first_seen_at_ms": min(existing.FirstSeenAtMS, session.FirstSeenAtMS),
		"last_seen_at_ms":  max(existing.LastSeenAtMS, session.LastSeenAtMS),
	}
	fillMissingString(updates, "originator", existing.Originator, session.Originator)
	fillMissingString(updates, "model_provider", existing.ModelProvider, session.ModelProvider)
	fillMissingString(updates, "initial_cwd", existing.InitialCWD, session.InitialCWD)
	fillMissingString(updates, "project_id", existing.ProjectID, session.ProjectID)
	fillMissingString(updates, "cli_version", existing.CLIVersion, session.CLIVersion)
	return database.Model(&sessionModel{}).Where("session_id = ?", session.SessionID).Updates(updates).Error
}

func upsertTurn(ctx context.Context, transaction storesqlite.WriteTx, turn Turn) error {
	database := transaction.WithContext(ctx)
	var existing turnModel
	err := database.Take(&existing, "turn_id = ?", turn.TurnID).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return database.Create(turnModelFromDomain(turn)).Error
	}
	if err != nil {
		return err
	}
	shouldUpdate := (turn.SourceGeneration > existing.SourceGeneration &&
		(existing.CompletedAtMS == nil || turn.CompletedAtMS != nil)) ||
		(turn.SourceGeneration == existing.SourceGeneration && turn.StartOffset == existing.StartOffset &&
			(existing.CompletedAtMS == nil || turn.CompletedAtMS != nil))
	if !shouldUpdate {
		return nil
	}
	updates := map[string]any{
		"started_at_ms":     min(existing.StartedAtMS, turn.StartedAtMS),
		"source_generation": turn.SourceGeneration,
		"start_offset":      turn.StartOffset,
		"completed_at_ms":   coalesceInt64(turn.CompletedAtMS, existing.CompletedAtMS),
		"outcome":           coalesceString(turn.Outcome, existing.Outcome),
		"model":             coalesceString(turn.Model, existing.Model),
		"reasoning_effort":  coalesceString(turn.ReasoningEffort, existing.ReasoningEffort),
		"cwd":               coalesceString(turn.CWD, existing.CWD),
		"project_id":        coalesceString(turn.ProjectID, existing.ProjectID),
		"complete_offset":   coalesceInt64(turn.CompleteOffset, existing.CompleteOffset),
	}
	return database.Model(&turnModel{}).Where("turn_id = ?", turn.TurnID).Updates(updates).Error
}

func upsertTurnUsage(ctx context.Context, transaction storesqlite.WriteTx, usage TurnUsage) error {
	database := transaction.WithContext(ctx)
	var existing turnUsageModel
	err := database.Take(&existing, "turn_id = ?", usage.TurnID).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return database.Create(turnUsageModelFromDomain(usage)).Error
	}
	if err != nil {
		return err
	}
	newerPosition := usage.SourceGeneration > existing.SourceGeneration ||
		(usage.SourceGeneration == existing.SourceGeneration && usage.SourceOffset > existing.SourceOffset)
	if !newerPosition || (existing.IsFinal && !usage.IsFinal) {
		return nil
	}
	return database.Model(&turnUsageModel{}).Where("turn_id = ?", usage.TurnID).Updates(map[string]any{
		"observed_at_ms": usage.ObservedAtMS, "is_final": usage.IsFinal,
		"input_tokens": usage.InputTokens, "cached_input_tokens": usage.CachedInputTokens,
		"output_tokens": usage.OutputTokens, "reasoning_tokens": usage.ReasoningTokens,
		"context_window": usage.ContextWindow, "source_generation": usage.SourceGeneration,
		"source_offset": usage.SourceOffset, "confidence": usage.Confidence,
		"updated_at_ms": usage.UpdatedAtMS,
	}).Error
}

func removeInvalidTurnUsage(ctx context.Context, transaction storesqlite.WriteTx, turnID string) error {
	database := transaction.WithContext(ctx)
	var turn turnModel
	if err := database.Select("turn_id", "source_generation", "completed_at_ms").Take(&turn, "turn_id = ?", turnID).Error; err != nil {
		return err
	}
	var usage turnUsageModel
	err := database.Select("turn_id", "source_generation", "is_final").Take(&usage, "turn_id = ?", turnID).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil
	}
	if err != nil {
		return err
	}
	if usage.SourceGeneration == turn.SourceGeneration && (usage.IsFinal || turn.CompletedAtMS == nil) {
		return nil
	}
	return database.Delete(&turnUsageModel{}, "turn_id = ?", turnID).Error
}

func upsertSessionCurrent(ctx context.Context, transaction storesqlite.WriteTx, current SessionCurrent) error {
	database := transaction.WithContext(ctx)
	var existing sessionCurrentModel
	err := database.Take(&existing, "session_id = ?", current.SessionID).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return database.Create(sessionCurrentModelFromDomain(current)).Error
	}
	if err != nil {
		return err
	}
	updates := make(map[string]any)
	if current.ThreadNameUpdatedAtMS != nil &&
		(existing.ThreadNameUpdatedAtMS == nil || *current.ThreadNameUpdatedAtMS > *existing.ThreadNameUpdatedAtMS) {
		updates["thread_name"] = current.ThreadName
		updates["thread_name_updated_at_ms"] = current.ThreadNameUpdatedAtMS
	}
	if current.UpdatedAtMS > existing.UpdatedAtMS {
		updates["active_turn_id"] = current.ActiveTurnID
		updates["current_model"] = current.CurrentModel
		updates["current_cwd"] = current.CurrentCWD
		updates["last_activity_at_ms"] = current.LastActivityAtMS
		updates["updated_at_ms"] = current.UpdatedAtMS
	}
	if len(updates) == 0 {
		return nil
	}
	return database.Model(&sessionCurrentModel{}).Where("session_id = ?", current.SessionID).Updates(updates).Error
}

func upsertSessionUsageCurrent(ctx context.Context, transaction storesqlite.WriteTx, usage SessionUsageCurrent) error {
	database := transaction.WithContext(ctx)
	var existing sessionUsageCurrentModel
	err := database.Take(&existing, "session_id = ?", usage.SessionID).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return database.Create(sessionUsageCurrentModelFromDomain(usage)).Error
	}
	if err != nil {
		return err
	}
	newer := usage.SourceGeneration > existing.SourceGeneration ||
		(usage.SourceGeneration == existing.SourceGeneration &&
			(usage.CounterEpoch > existing.CounterEpoch ||
				(usage.CounterEpoch == existing.CounterEpoch && usage.SourceOffset > existing.SourceOffset)))
	if !newer {
		return nil
	}
	return database.Model(&sessionUsageCurrentModel{}).Where("session_id = ?", usage.SessionID).Updates(map[string]any{
		"counter_epoch": usage.CounterEpoch, "total_input_tokens": usage.TotalInputTokens,
		"total_cached_tokens": usage.TotalCachedTokens, "total_output_tokens": usage.TotalOutputTokens,
		"total_reasoning_tokens": usage.TotalReasoningTokens, "observed_at_ms": usage.ObservedAtMS,
		"source_generation": usage.SourceGeneration, "source_offset": usage.SourceOffset,
		"counter_state": usage.CounterState,
	}).Error
}

func turnModelFromDomain(turn Turn) *turnModel {
	return &turnModel{
		TurnID: turn.TurnID, SessionID: turn.SessionID, StartedAtMS: turn.StartedAtMS,
		CompletedAtMS: turn.CompletedAtMS, Outcome: turn.Outcome, Model: turn.Model,
		ReasoningEffort: turn.ReasoningEffort, CWD: turn.CWD, ProjectID: turn.ProjectID,
		SourceGeneration: turn.SourceGeneration, StartOffset: turn.StartOffset,
		CompleteOffset: turn.CompleteOffset,
	}
}

func turnUsageModelFromDomain(usage TurnUsage) *turnUsageModel {
	return &turnUsageModel{
		TurnID: usage.TurnID, ObservedAtMS: usage.ObservedAtMS, IsFinal: usage.IsFinal,
		InputTokens: usage.InputTokens, CachedInputTokens: usage.CachedInputTokens,
		OutputTokens: usage.OutputTokens, ReasoningTokens: usage.ReasoningTokens,
		ContextWindow: usage.ContextWindow, SourceGeneration: usage.SourceGeneration,
		SourceOffset: usage.SourceOffset, Confidence: usage.Confidence, UpdatedAtMS: usage.UpdatedAtMS,
	}
}

func sessionCurrentModelFromDomain(current SessionCurrent) *sessionCurrentModel {
	return &sessionCurrentModel{
		SessionID: current.SessionID, ThreadName: current.ThreadName,
		ThreadNameUpdatedAtMS: current.ThreadNameUpdatedAtMS, ActiveTurnID: current.ActiveTurnID,
		CurrentModel: current.CurrentModel, CurrentCWD: current.CurrentCWD,
		LastActivityAtMS: current.LastActivityAtMS, UpdatedAtMS: current.UpdatedAtMS,
	}
}

func sessionUsageCurrentModelFromDomain(usage SessionUsageCurrent) *sessionUsageCurrentModel {
	return &sessionUsageCurrentModel{
		SessionID: usage.SessionID, CounterEpoch: usage.CounterEpoch,
		TotalInputTokens: usage.TotalInputTokens, TotalCachedTokens: usage.TotalCachedTokens,
		TotalOutputTokens: usage.TotalOutputTokens, TotalReasoningTokens: usage.TotalReasoningTokens,
		ObservedAtMS: usage.ObservedAtMS, SourceGeneration: usage.SourceGeneration,
		SourceOffset: usage.SourceOffset, CounterState: usage.CounterState,
	}
}

func fillMissingString(updates map[string]any, column string, existing, incoming *string) {
	if existing == nil && incoming != nil {
		updates[column] = *incoming
	}
}

func coalesceString(preferred, fallback *string) *string {
	if preferred != nil {
		return preferred
	}
	return fallback
}

func coalesceInt64(preferred, fallback *int64) *int64 {
	if preferred != nil {
		return preferred
	}
	return fallback
}

func nullableString(value *string) any {
	if value == nil {
		return nil
	}
	return *value
}

func nullableInt64(value *int64) any {
	if value == nil {
		return nil
	}
	return *value
}

func boolInteger(value bool) int {
	if value {
		return 1
	}
	return 0
}
