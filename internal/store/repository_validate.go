package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

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
		batch.SessionCurrent == nil && batch.SessionUsageCurrent == nil {
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
		if turn.CompletedAtMS != nil && (*turn.CompletedAtMS < turn.StartedAtMS || *turn.CompleteOffset < turn.StartOffset) {
			return invalidRecord("turn completion precedes start")
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
	_, err := batchSessionID(batch)
	return err
}

func batchSessionID(batch FactBatch) (string, error) {
	identifiers := make([]string, 0, 4)
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
	return requireStoredReference(ctx, transaction, `SELECT 1 FROM projects WHERE project_id = ?`, projectID, "project")
}

func validateProjectReplay(ctx context.Context, transaction storesqlite.WriteTx, project Project) error {
	var displayName, rootPath string
	var remote sql.NullString
	var updatedAt int64
	err := transaction.QueryRowContext(ctx, `
		SELECT display_name, root_path, git_remote_sanitized, updated_at_ms
		FROM projects WHERE project_id = ?
	`, project.ProjectID).Scan(&displayName, &rootPath, &remote, &updatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return nil
	}
	if err != nil {
		return err
	}
	if project.UpdatedAtMS == updatedAt &&
		(displayName != project.DisplayName || rootPath != project.RootPath ||
			!equalStringPointer(stringPointer(remote), project.GitRemoteSanitized)) {
		return invalidRecord("project metadata conflicts at the same update time")
	}
	return nil
}

func requireSession(ctx context.Context, transaction storesqlite.WriteTx, sessionID string) error {
	return requireStoredReference(ctx, transaction, `SELECT 1 FROM sessions WHERE session_id = ?`, sessionID, "session")
}

func requireTurn(ctx context.Context, transaction storesqlite.WriteTx, turnID string) error {
	return requireStoredReference(ctx, transaction, `SELECT 1 FROM turns WHERE turn_id = ?`, turnID, "turn")
}

func requireStoredReference(
	ctx context.Context,
	transaction storesqlite.WriteTx,
	query string,
	identifier string,
	recordKind string,
) error {
	var exists int
	err := transaction.QueryRowContext(ctx, query, identifier).Scan(&exists)
	if errors.Is(err, sql.ErrNoRows) {
		return invalidRecord(recordKind + " reference does not exist")
	}
	return err
}

func validateSessionIdentity(ctx context.Context, transaction storesqlite.WriteTx, session Session) error {
	var provider, sourceKind string
	var originator, modelProvider, initialCWD, projectID, cliVersion sql.NullString
	err := transaction.QueryRowContext(
		ctx,
		`SELECT provider, source_kind, originator, model_provider, initial_cwd, project_id, cli_version
		 FROM sessions WHERE session_id = ?`,
		session.SessionID,
	).Scan(&provider, &sourceKind, &originator, &modelProvider, &initialCWD, &projectID, &cliVersion)
	if errors.Is(err, sql.ErrNoRows) {
		return nil
	}
	if err != nil {
		return err
	}
	if provider != session.Provider || sourceKind != session.SourceKind {
		return invalidRecord("session provider or source kind conflicts with stable identity")
	}
	stableFields := []struct {
		name     string
		existing *string
		incoming *string
	}{
		{name: "originator", existing: stringPointer(originator), incoming: session.Originator},
		{name: "model provider", existing: stringPointer(modelProvider), incoming: session.ModelProvider},
		{name: "initial cwd", existing: stringPointer(initialCWD), incoming: session.InitialCWD},
		{name: "project", existing: stringPointer(projectID), incoming: session.ProjectID},
		{name: "CLI version", existing: stringPointer(cliVersion), incoming: session.CLIVersion},
	}
	for _, field := range stableFields {
		if field.existing != nil && field.incoming != nil && *field.existing != *field.incoming {
			return invalidRecord("session " + field.name + " conflicts with stable metadata")
		}
	}
	return nil
}

func validateTurnIdentity(ctx context.Context, transaction storesqlite.WriteTx, turn Turn) error {
	var existingSessionID string
	var existingStartedAt, existingGeneration, existingOffset int64
	var existingCompletedAt, existingCompleteOffset sql.NullInt64
	var existingOutcome, existingModel, existingReasoning, existingCWD, existingProjectID sql.NullString
	err := transaction.QueryRowContext(
		ctx,
		`SELECT session_id, started_at_ms, completed_at_ms, outcome, model, reasoning_effort,
		        cwd, project_id, source_generation, start_offset, complete_offset
		 FROM turns WHERE turn_id = ?`,
		turn.TurnID,
	).Scan(
		&existingSessionID,
		&existingStartedAt,
		&existingCompletedAt,
		&existingOutcome,
		&existingModel,
		&existingReasoning,
		&existingCWD,
		&existingProjectID,
		&existingGeneration,
		&existingOffset,
		&existingCompleteOffset,
	)
	switch {
	case errors.Is(err, sql.ErrNoRows):
	case err != nil:
		return err
	case existingSessionID != turn.SessionID:
		return invalidRecord("turn belongs to another session")
	case turn.SourceGeneration == existingGeneration && turn.StartOffset != existingOffset:
		return invalidRecord("turn source offset conflicts within the same generation")
	case turn.SourceGeneration == existingGeneration && turn.StartOffset == existingOffset &&
		(existingStartedAt != turn.StartedAtMS ||
			optionalStringConflicts(existingModel, turn.Model) ||
			optionalStringConflicts(existingReasoning, turn.ReasoningEffort) ||
			optionalStringConflicts(existingCWD, turn.CWD) ||
			optionalStringConflicts(existingProjectID, turn.ProjectID)):
		return invalidRecord("turn facts conflict at the same source identity")
	case turn.SourceGeneration == existingGeneration && existingCompletedAt.Valid && turn.CompletedAtMS != nil &&
		(!equalInt64Pointer(int64Pointer(existingCompletedAt), turn.CompletedAtMS) ||
			!equalStringPointer(stringPointer(existingOutcome), turn.Outcome) ||
			!equalInt64Pointer(int64Pointer(existingCompleteOffset), turn.CompleteOffset)):
		return invalidRecord("turn completion conflicts at the same source identity")
	}

	var existingTurnID string
	err = transaction.QueryRowContext(ctx, `
		SELECT turn_id FROM turns
		WHERE session_id = ? AND source_generation = ? AND start_offset = ?
	`, turn.SessionID, turn.SourceGeneration, turn.StartOffset).Scan(&existingTurnID)
	if errors.Is(err, sql.ErrNoRows) {
		return nil
	}
	if err != nil {
		return err
	}
	if existingTurnID != turn.TurnID {
		return invalidRecord("source position already belongs to another turn")
	}
	return nil
}

func validateActiveTurnReference(ctx context.Context, transaction storesqlite.WriteTx, current SessionCurrent) error {
	if current.ActiveTurnID == nil {
		return nil
	}
	var sessionID string
	err := transaction.QueryRowContext(ctx, `SELECT session_id FROM turns WHERE turn_id = ?`, *current.ActiveTurnID).Scan(&sessionID)
	if errors.Is(err, sql.ErrNoRows) {
		return invalidRecord("active turn does not exist")
	}
	if err != nil {
		return err
	}
	if sessionID != current.SessionID {
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
	var turnSessionID string
	var turnGeneration int64
	var turnCompletedAt sql.NullInt64
	err := transaction.QueryRowContext(
		ctx,
		`SELECT session_id, source_generation, completed_at_ms FROM turns WHERE turn_id = ?`,
		usage.TurnID,
	).Scan(&turnSessionID, &turnGeneration, &turnCompletedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return invalidRecord("turn reference does not exist")
	}
	if err != nil {
		return err
	}
	if expectedSessionID != "" && turnSessionID != expectedSessionID {
		return invalidRecord("turn usage belongs to another session")
	}
	if usage.SourceGeneration != turnGeneration {
		return invalidRecord("turn usage generation does not match stored turn")
	}
	if turnCompletedAt.Valid && !usage.IsFinal {
		return invalidRecord("completed turn requires final usage")
	}

	var existing TurnUsage
	var isFinal int
	var input, cached, output, reasoning, contextWindow sql.NullInt64
	err = transaction.QueryRowContext(ctx, `
		SELECT turn_id, observed_at_ms, is_final, input_tokens, cached_input_tokens,
		       output_tokens, reasoning_tokens, context_window, source_generation, source_offset,
		       confidence, updated_at_ms
		FROM turn_usage WHERE turn_id = ?
	`, usage.TurnID).Scan(
		&existing.TurnID,
		&existing.ObservedAtMS,
		&isFinal,
		&input,
		&cached,
		&output,
		&reasoning,
		&contextWindow,
		&existing.SourceGeneration,
		&existing.SourceOffset,
		&existing.Confidence,
		&existing.UpdatedAtMS,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return nil
	}
	if err != nil {
		return err
	}
	existing.IsFinal = isFinal == 1
	existing.InputTokens = int64Pointer(input)
	existing.CachedInputTokens = int64Pointer(cached)
	existing.OutputTokens = int64Pointer(output)
	existing.ReasoningTokens = int64Pointer(reasoning)
	existing.ContextWindow = int64Pointer(contextWindow)
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
	var existing SessionCurrent
	var threadName, activeTurnID, currentModel, currentCWD sql.NullString
	var threadNameUpdatedAt, lastActivityAt sql.NullInt64
	err := transaction.QueryRowContext(ctx, `
		SELECT session_id, thread_name, thread_name_updated_at_ms, active_turn_id,
		       current_model, current_cwd, last_activity_at_ms, updated_at_ms
		FROM session_current WHERE session_id = ?
	`, current.SessionID).Scan(
		&existing.SessionID,
		&threadName,
		&threadNameUpdatedAt,
		&activeTurnID,
		&currentModel,
		&currentCWD,
		&lastActivityAt,
		&existing.UpdatedAtMS,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return nil
	}
	if err != nil {
		return err
	}
	existing.ThreadName = stringPointer(threadName)
	existing.ThreadNameUpdatedAtMS = int64Pointer(threadNameUpdatedAt)
	existing.ActiveTurnID = stringPointer(activeTurnID)
	existing.CurrentModel = stringPointer(currentModel)
	existing.CurrentCWD = stringPointer(currentCWD)
	existing.LastActivityAtMS = int64Pointer(lastActivityAt)

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
	var existing SessionUsageCurrent
	var input, cached, output, reasoning sql.NullInt64
	err := transaction.QueryRowContext(ctx, `
		SELECT session_id, counter_epoch, total_input_tokens, total_cached_tokens,
		       total_output_tokens, total_reasoning_tokens, observed_at_ms,
		       source_generation, source_offset, counter_state
		FROM session_usage_current WHERE session_id = ?
	`, usage.SessionID).Scan(
		&existing.SessionID,
		&existing.CounterEpoch,
		&input,
		&cached,
		&output,
		&reasoning,
		&existing.ObservedAtMS,
		&existing.SourceGeneration,
		&existing.SourceOffset,
		&existing.CounterState,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return nil
	}
	if err != nil {
		return err
	}
	existing.TotalInputTokens = int64Pointer(input)
	existing.TotalCachedTokens = int64Pointer(cached)
	existing.TotalOutputTokens = int64Pointer(output)
	existing.TotalReasoningTokens = int64Pointer(reasoning)
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

func optionalStringConflicts(existing sql.NullString, incoming *string) bool {
	return existing.Valid && incoming != nil && existing.String != *incoming
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

func validateOptionalStrings(values ...*string) error {
	for _, value := range values {
		if value != nil && *value == "" {
			return invalidRecord("optional string must be nil or non-empty")
		}
	}
	return nil
}
