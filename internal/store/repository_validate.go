package store

import (
	"context"
	"errors"
	"fmt"

	"gorm.io/gorm"

	storesqlite "github.com/SisyphusSQ/codex-pulse/internal/store/sqlite"
)

func validateTurnFilter(filter TurnFilter) (int, error) {
	limit := filter.Limit
	if limit == 0 {
		limit = 100
	}
	if limit < 0 || limit > 500 {
		return 0, invalidRecord("turn filter limit must be between 1 and 500")
	}
	for _, value := range []*string{filter.SessionID, filter.ProjectID, filter.Model} {
		if value != nil && *value == "" {
			return 0, invalidRecord("turn filter string must not be empty")
		}
	}
	if filter.SourceGeneration != nil && *filter.SourceGeneration < 0 {
		return 0, invalidRecord("source generation must not be negative")
	}
	if filter.StartOffsetAtOrAfter != nil && *filter.StartOffsetAtOrAfter < 0 {
		return 0, invalidRecord("source offset must not be negative")
	}
	if (filter.SourceGeneration != nil || filter.StartOffsetAtOrAfter != nil) && filter.SessionID == nil {
		return 0, invalidRecord("source position filter requires session ID")
	}
	if filter.StartedAtOrAfterMS != nil && *filter.StartedAtOrAfterMS < 0 {
		return 0, invalidRecord("start time lower bound must not be negative")
	}
	if filter.StartedBeforeMS != nil && *filter.StartedBeforeMS < 0 {
		return 0, invalidRecord("start time upper bound must not be negative")
	}
	if filter.StartedAtOrAfterMS != nil && filter.StartedBeforeMS != nil &&
		*filter.StartedAtOrAfterMS >= *filter.StartedBeforeMS {
		return 0, invalidRecord("start time range is empty")
	}
	return limit, nil
}

func (repository *Repository) validateBatch(batch FactBatch) error {
	if repository == nil || repository.database == nil {
		return ErrInvalidRepository
	}
	if batch.Project == nil && batch.Session == nil && batch.Turn == nil && batch.Usage == nil &&
		batch.SessionCurrent == nil && batch.SessionUsageCurrent == nil && batch.QuotaObservation == nil {
		return invalidRecord("fact batch is empty")
	}
	if batch.Project != nil {
		project := batch.Project
		if project.ProjectID == "" || project.DisplayName == "" || project.RootPath == "" {
			return invalidRecord("project identity is incomplete")
		}
		if project.CreatedAtMS < 0 || project.UpdatedAtMS < project.CreatedAtMS {
			return invalidRecord("project timestamps are invalid")
		}
		if err := validateOptionalStrings(project.GitRemoteSanitized); err != nil {
			return err
		}
	}
	if batch.Session != nil {
		session := batch.Session
		if session.SessionID == "" || session.Provider == "" || session.SourceKind == "" {
			return invalidRecord("session identity is incomplete")
		}
		if session.CreatedAtMS < 0 || session.FirstSeenAtMS < 0 || session.LastSeenAtMS < session.FirstSeenAtMS {
			return invalidRecord("session timestamps are invalid")
		}
		if err := validateOptionalStrings(
			session.Originator,
			session.ModelProvider,
			session.InitialCWD,
			session.ProjectID,
			session.CLIVersion,
		); err != nil {
			return err
		}
		if batch.Project != nil && session.ProjectID != nil && *session.ProjectID != batch.Project.ProjectID {
			return invalidRecord("session project does not match batch project")
		}
	}
	if batch.Turn != nil {
		turn := batch.Turn
		if turn.TurnID == "" || turn.SessionID == "" || turn.StartedAtMS < 0 || turn.SourceGeneration < 0 || turn.StartOffset < 0 {
			return invalidRecord("turn identity or source position is invalid")
		}
		completed := turn.CompletedAtMS != nil || turn.Outcome != nil || turn.CompleteOffset != nil
		if completed && (turn.CompletedAtMS == nil || turn.Outcome == nil || *turn.Outcome == "" || turn.CompleteOffset == nil) {
			return invalidRecord("turn completion fields must be provided together")
		}
		if turn.CompletedAtMS != nil && (*turn.CompletedAtMS < turn.StartedAtMS || *turn.CompleteOffset < 0) {
			return invalidRecord("turn completion time or source offset is invalid")
		}
		if err := validateOptionalStrings(turn.Outcome, turn.Model, turn.ReasoningEffort, turn.CWD, turn.ProjectID); err != nil {
			return err
		}
		if batch.Session != nil && turn.SessionID != batch.Session.SessionID {
			return invalidRecord("turn session does not match batch session")
		}
		if batch.Project != nil && turn.ProjectID != nil && *turn.ProjectID != batch.Project.ProjectID {
			return invalidRecord("turn project does not match batch project")
		}
	}
	if batch.Usage != nil {
		usage := batch.Usage
		if usage.TurnID == "" || usage.ObservedAtMS < 0 || usage.SourceGeneration < 0 ||
			usage.SourceOffset < 0 || usage.Confidence == "" || usage.UpdatedAtMS < usage.ObservedAtMS {
			return invalidRecord("turn usage identity or timestamps are invalid")
		}
		for _, value := range []*int64{usage.InputTokens, usage.CachedInputTokens, usage.OutputTokens, usage.ReasoningTokens, usage.ContextWindow} {
			if value != nil && *value < 0 {
				return invalidRecord("turn usage counters must not be negative")
			}
		}
		if batch.Turn != nil && usage.TurnID != batch.Turn.TurnID {
			return invalidRecord("usage turn does not match batch turn")
		}
		if batch.Turn != nil && usage.SourceGeneration != batch.Turn.SourceGeneration {
			return invalidRecord("usage generation does not match batch turn")
		}
	}
	if batch.SessionCurrent != nil {
		current := batch.SessionCurrent
		if current.SessionID == "" || current.UpdatedAtMS < 0 {
			return invalidRecord("session current identity or timestamp is invalid")
		}
		if (current.ThreadName == nil) != (current.ThreadNameUpdatedAtMS == nil) {
			return invalidRecord("thread name and update timestamp must be provided together")
		}
		if current.ThreadName != nil && (*current.ThreadName == "" || *current.ThreadNameUpdatedAtMS < 0) {
			return invalidRecord("thread name projection is invalid")
		}
		if current.ThreadNameUpdatedAtMS != nil && *current.ThreadNameUpdatedAtMS > current.UpdatedAtMS {
			return invalidRecord("thread name update is newer than current projection")
		}
		if current.LastActivityAtMS != nil && *current.LastActivityAtMS < 0 {
			return invalidRecord("last activity timestamp must not be negative")
		}
		if err := validateOptionalStrings(
			current.ThreadName,
			current.ActiveTurnID,
			current.CurrentModel,
			current.CurrentCWD,
		); err != nil {
			return err
		}
		if batch.Session != nil && current.SessionID != batch.Session.SessionID {
			return invalidRecord("current projection does not match batch session")
		}
		if batch.Turn != nil && current.SessionID != batch.Turn.SessionID {
			return invalidRecord("current projection does not match batch turn session")
		}
		if batch.Turn != nil && current.ActiveTurnID != nil && *current.ActiveTurnID != batch.Turn.TurnID {
			return invalidRecord("active turn does not match batch turn")
		}
	}
	if batch.SessionUsageCurrent != nil {
		usage := batch.SessionUsageCurrent
		if usage.SessionID == "" || usage.CounterEpoch < 0 || usage.ObservedAtMS < 0 ||
			usage.SourceGeneration < 0 || usage.SourceOffset < 0 || usage.CounterState == "" {
			return invalidRecord("session usage current identity or counters are invalid")
		}
		for _, value := range []*int64{usage.TotalInputTokens, usage.TotalCachedTokens, usage.TotalOutputTokens, usage.TotalReasoningTokens} {
			if value != nil && *value < 0 {
				return invalidRecord("session usage current counters must not be negative")
			}
		}
		if batch.Session != nil && usage.SessionID != batch.Session.SessionID {
			return invalidRecord("session usage current does not match batch session")
		}
	}
	if batch.QuotaObservation != nil {
		if err := validateQuotaObservationSample(*batch.QuotaObservation); err != nil {
			return err
		}
		if batch.Session != nil && batch.QuotaObservation.SessionID != nil &&
			*batch.QuotaObservation.SessionID != batch.Session.SessionID {
			return invalidRecord("quota observation does not match batch session")
		}
	}
	_, err := batchSessionID(batch)
	return err
}

func batchSessionID(batch FactBatch) (string, error) {
	identifiers := make([]string, 0, 5)
	if batch.Session != nil {
		identifiers = append(identifiers, batch.Session.SessionID)
	}
	if batch.Turn != nil {
		identifiers = append(identifiers, batch.Turn.SessionID)
	}
	if batch.SessionCurrent != nil {
		identifiers = append(identifiers, batch.SessionCurrent.SessionID)
	}
	if batch.SessionUsageCurrent != nil {
		identifiers = append(identifiers, batch.SessionUsageCurrent.SessionID)
	}
	if batch.QuotaObservation != nil && batch.QuotaObservation.SessionID != nil {
		identifiers = append(identifiers, *batch.QuotaObservation.SessionID)
	}
	if len(identifiers) == 0 {
		return "", nil
	}
	expected := identifiers[0]
	for _, identifier := range identifiers[1:] {
		if identifier != expected {
			return "", invalidRecord("fact batch contains multiple session identities")
		}
	}
	return expected, nil
}

func requireProject(ctx context.Context, transaction storesqlite.WriteTx, projectID string) error {
	return requireStoredReference(ctx, transaction, &projectModel{}, "project_id = ?", projectID, "project")
}

func validateProjectReplay(ctx context.Context, transaction storesqlite.WriteTx, project Project) error {
	var existing projectModel
	err := transaction.WithContext(ctx).Take(&existing, "project_id = ?", project.ProjectID).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil
	}
	if err != nil {
		return err
	}
	if project.UpdatedAtMS == existing.UpdatedAtMS &&
		(existing.DisplayName != project.DisplayName || existing.RootPath != project.RootPath ||
			!equalStringPointer(existing.GitRemoteSanitized, project.GitRemoteSanitized)) {
		return invalidRecord("project metadata conflicts at the same update time")
	}
	return nil
}

func requireSession(ctx context.Context, transaction storesqlite.WriteTx, sessionID string) error {
	return requireStoredReference(ctx, transaction, &sessionModel{}, "session_id = ?", sessionID, "session")
}

func requireTurn(ctx context.Context, transaction storesqlite.WriteTx, turnID string) error {
	return requireStoredReference(ctx, transaction, &turnModel{}, "turn_id = ?", turnID, "turn")
}

func requireStoredReference(
	ctx context.Context,
	transaction storesqlite.WriteTx,
	model any,
	query string,
	identifier string,
	recordKind string,
) error {
	var count int64
	err := transaction.WithContext(ctx).Model(model).Where(query, identifier).Count(&count).Error
	if err == nil && count == 0 {
		return invalidRecord(recordKind + " reference does not exist")
	}
	return err
}

func validateSessionIdentity(ctx context.Context, transaction storesqlite.WriteTx, session Session) error {
	var existing sessionModel
	err := transaction.WithContext(ctx).Take(&existing, "session_id = ?", session.SessionID).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil
	}
	if err != nil {
		return err
	}
	if existing.Provider != session.Provider || existing.SourceKind != session.SourceKind {
		return invalidRecord("session provider or source kind conflicts with stable identity")
	}
	stableFields := []struct {
		name     string
		existing *string
		incoming *string
	}{
		{name: "originator", existing: existing.Originator, incoming: session.Originator},
		{name: "model provider", existing: existing.ModelProvider, incoming: session.ModelProvider},
		{name: "initial cwd", existing: existing.InitialCWD, incoming: session.InitialCWD},
		{name: "project", existing: existing.ProjectID, incoming: session.ProjectID},
		{name: "CLI version", existing: existing.CLIVersion, incoming: session.CLIVersion},
	}
	for _, field := range stableFields {
		if field.existing != nil && field.incoming != nil && *field.existing != *field.incoming {
			return invalidRecord("session " + field.name + " conflicts with stable metadata")
		}
	}
	return nil
}

func validateTurnIdentity(ctx context.Context, transaction storesqlite.WriteTx, turn Turn) error {
	var existing turnModel
	err := transaction.WithContext(ctx).Take(&existing, "turn_id = ?", turn.TurnID).Error
	switch {
	case errors.Is(err, gorm.ErrRecordNotFound):
	case err != nil:
		return err
	case existing.SessionID != turn.SessionID:
		return invalidRecord("turn belongs to another session")
	case turn.SourceGeneration == existing.SourceGeneration && turn.StartOffset != existing.StartOffset:
		return invalidRecord("turn source offset conflicts within the same generation")
	case turn.SourceGeneration == existing.SourceGeneration && turn.StartOffset == existing.StartOffset &&
		(existing.StartedAtMS != turn.StartedAtMS ||
			optionalStringConflicts(existing.Model, turn.Model) ||
			optionalStringConflicts(existing.ReasoningEffort, turn.ReasoningEffort) ||
			optionalStringConflicts(existing.CWD, turn.CWD) ||
			optionalStringConflicts(existing.ProjectID, turn.ProjectID)):
		return invalidRecord("turn facts conflict at the same source identity")
	case turn.SourceGeneration == existing.SourceGeneration && existing.CompletedAtMS != nil && turn.CompletedAtMS != nil &&
		(!equalInt64Pointer(existing.CompletedAtMS, turn.CompletedAtMS) ||
			!equalStringPointer(existing.Outcome, turn.Outcome) ||
			!equalInt64Pointer(existing.CompleteOffset, turn.CompleteOffset)):
		return invalidRecord("turn completion conflicts at the same source identity")
	}

	var positioned turnModel
	err = transaction.WithContext(ctx).Select("turn_id").Take(
		&positioned,
		"session_id = ? AND source_generation = ? AND start_offset = ?",
		turn.SessionID, turn.SourceGeneration, turn.StartOffset,
	).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil
	}
	if err != nil {
		return err
	}
	if positioned.TurnID != turn.TurnID {
		return invalidRecord("source position already belongs to another turn")
	}
	return nil
}

func validateActiveTurnReference(ctx context.Context, transaction storesqlite.WriteTx, current SessionCurrent) error {
	if current.ActiveTurnID == nil {
		return nil
	}
	var turn turnModel
	err := transaction.WithContext(ctx).Select("turn_id", "session_id").Take(&turn, "turn_id = ?", *current.ActiveTurnID).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return invalidRecord("active turn does not exist")
	}
	if err != nil {
		return err
	}
	if turn.SessionID != current.SessionID {
		return invalidRecord("active turn belongs to another session")
	}
	return nil
}

func validateTurnUsageReplay(
	ctx context.Context,
	transaction storesqlite.WriteTx,
	usage TurnUsage,
	expectedSessionID string,
) error {
	var turn turnModel
	err := transaction.WithContext(ctx).Select("turn_id", "session_id", "source_generation", "completed_at_ms").Take(
		&turn, "turn_id = ?", usage.TurnID,
	).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return invalidRecord("turn reference does not exist")
	}
	if err != nil {
		return err
	}
	if expectedSessionID != "" && turn.SessionID != expectedSessionID {
		return invalidRecord("turn usage belongs to another session")
	}
	if usage.SourceGeneration != turn.SourceGeneration {
		return invalidRecord("turn usage generation does not match stored turn")
	}
	if turn.CompletedAtMS != nil && !usage.IsFinal {
		return invalidRecord("completed turn requires final usage")
	}

	var stored turnUsageModel
	err = transaction.WithContext(ctx).Take(&stored, "turn_id = ?", usage.TurnID).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil
	}
	if err != nil {
		return err
	}
	existing := TurnUsage{
		TurnID: stored.TurnID, ObservedAtMS: stored.ObservedAtMS, IsFinal: stored.IsFinal,
		InputTokens: stored.InputTokens, CachedInputTokens: stored.CachedInputTokens,
		OutputTokens: stored.OutputTokens, ReasoningTokens: stored.ReasoningTokens,
		ContextWindow: stored.ContextWindow, SourceGeneration: stored.SourceGeneration,
		SourceOffset: stored.SourceOffset, Confidence: stored.Confidence, UpdatedAtMS: stored.UpdatedAtMS,
	}
	if usage.SourceGeneration == existing.SourceGeneration && usage.SourceOffset < existing.SourceOffset {
		return invalidRecord("turn usage source position regresses within the same generation")
	}
	if usage.SourceGeneration == existing.SourceGeneration && usage.SourceOffset == existing.SourceOffset &&
		!equalTurnUsage(existing, usage) {
		return invalidRecord("turn usage conflicts at the same source position")
	}
	return nil
}

func validateSessionCurrentReplay(
	ctx context.Context,
	transaction storesqlite.WriteTx,
	current SessionCurrent,
) error {
	var stored sessionCurrentModel
	err := transaction.WithContext(ctx).Take(&stored, "session_id = ?", current.SessionID).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil
	}
	if err != nil {
		return err
	}
	existing := sessionCurrentFromModel(stored)

	if current.ThreadNameUpdatedAtMS != nil && existing.ThreadNameUpdatedAtMS != nil &&
		*current.ThreadNameUpdatedAtMS == *existing.ThreadNameUpdatedAtMS &&
		!equalStringPointer(current.ThreadName, existing.ThreadName) {
		return invalidRecord("thread name conflicts at the same update time")
	}
	if current.UpdatedAtMS == existing.UpdatedAtMS && !equalSessionCurrentNonThread(existing, current) {
		return invalidRecord("session current conflicts at the same update time")
	}
	return nil
}

func validateSessionUsageReplay(
	ctx context.Context,
	transaction storesqlite.WriteTx,
	usage SessionUsageCurrent,
) error {
	var stored sessionUsageCurrentModel
	err := transaction.WithContext(ctx).Take(&stored, "session_id = ?", usage.SessionID).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil
	}
	if err != nil {
		return err
	}
	existing := sessionUsageCurrentFromModel(stored)
	if usage.SourceGeneration == existing.SourceGeneration && usage.CounterEpoch == existing.CounterEpoch &&
		usage.SourceOffset == existing.SourceOffset &&
		!equalSessionUsage(existing, usage) {
		return invalidRecord("session usage conflicts at the same counter position")
	}
	return nil
}

func equalTurnUsage(left, right TurnUsage) bool {
	return left.TurnID == right.TurnID && left.ObservedAtMS == right.ObservedAtMS &&
		left.IsFinal == right.IsFinal && equalInt64Pointer(left.InputTokens, right.InputTokens) &&
		equalInt64Pointer(left.CachedInputTokens, right.CachedInputTokens) &&
		equalInt64Pointer(left.OutputTokens, right.OutputTokens) &&
		equalInt64Pointer(left.ReasoningTokens, right.ReasoningTokens) &&
		equalInt64Pointer(left.ContextWindow, right.ContextWindow) &&
		left.SourceGeneration == right.SourceGeneration && left.SourceOffset == right.SourceOffset &&
		left.Confidence == right.Confidence &&
		left.UpdatedAtMS == right.UpdatedAtMS
}

func equalSessionCurrentNonThread(left, right SessionCurrent) bool {
	return left.SessionID == right.SessionID &&
		equalStringPointer(left.ActiveTurnID, right.ActiveTurnID) &&
		equalStringPointer(left.CurrentModel, right.CurrentModel) &&
		equalStringPointer(left.CurrentCWD, right.CurrentCWD) &&
		equalInt64Pointer(left.LastActivityAtMS, right.LastActivityAtMS) &&
		left.UpdatedAtMS == right.UpdatedAtMS
}

func equalSessionUsage(left, right SessionUsageCurrent) bool {
	return left.SessionID == right.SessionID && left.CounterEpoch == right.CounterEpoch &&
		equalInt64Pointer(left.TotalInputTokens, right.TotalInputTokens) &&
		equalInt64Pointer(left.TotalCachedTokens, right.TotalCachedTokens) &&
		equalInt64Pointer(left.TotalOutputTokens, right.TotalOutputTokens) &&
		equalInt64Pointer(left.TotalReasoningTokens, right.TotalReasoningTokens) &&
		left.ObservedAtMS == right.ObservedAtMS && left.SourceGeneration == right.SourceGeneration &&
		left.SourceOffset == right.SourceOffset &&
		left.CounterState == right.CounterState
}

func equalStringPointer(left, right *string) bool {
	if left == nil || right == nil {
		return left == nil && right == nil
	}
	return *left == *right
}

func optionalStringConflicts(existing, incoming *string) bool {
	return existing != nil && incoming != nil && *existing != *incoming
}

func equalInt64Pointer(left, right *int64) bool {
	if left == nil || right == nil {
		return left == nil && right == nil
	}
	return *left == *right
}

func invalidRecord(message string) error {
	return fmt.Errorf("%w: %s", ErrInvalidRecord, message)
}

func sourceRefreshConflict(message string) error {
	return fmt.Errorf("%w: %s", ErrSourceRefreshConflict, message)
}

func validateOptionalStrings(values ...*string) error {
	for _, value := range values {
		if value != nil && *value == "" {
			return invalidRecord("optional string must be nil or non-empty")
		}
	}
	return nil
}
