package store

import (
	"context"
	"errors"
	"strings"

	"gorm.io/gorm"

	"github.com/SisyphusSQ/codex-pulse/internal/attribution"
	"github.com/SisyphusSQ/codex-pulse/internal/projectidentity"
	storesqlite "github.com/SisyphusSQ/codex-pulse/internal/store/sqlite"
)

const (
	currentAttributionPriority  = 100
	fallbackAttributionPriority = 10
	localPathProjectPrefix      = "local-path-v1:"
)

// SessionAttribution 返回不含任何本机路径字段的 Session 派生归因。
func (repository *Repository) SessionAttribution(
	ctx context.Context,
	sessionID string,
) (SessionAttributionSnapshot, error) {
	if repository == nil || repository.database == nil {
		return SessionAttributionSnapshot{}, ErrInvalidRepository
	}
	if sessionID == "" {
		return SessionAttributionSnapshot{}, invalidRecord("session attribution ID must not be empty")
	}
	var result SessionAttributionSnapshot
	err := repository.database.View(ctx, func(ctx context.Context, connection storesqlite.ReadConn) error {
		var model sessionAttributionModel
		if err := connection.WithContext(ctx).Take(&model, "session_id = ?", sessionID).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return ErrNotFound
			}
			return err
		}
		result = sessionAttributionFromModel(model)
		return nil
	})
	return result, err
}

// TurnAttribution 返回不含原始 model/cwd 的 Turn 派生归因。
func (repository *Repository) TurnAttribution(
	ctx context.Context,
	turnID string,
) (TurnAttributionSnapshot, error) {
	if repository == nil || repository.database == nil {
		return TurnAttributionSnapshot{}, ErrInvalidRepository
	}
	if turnID == "" {
		return TurnAttributionSnapshot{}, invalidRecord("turn attribution ID must not be empty")
	}
	var result TurnAttributionSnapshot
	err := repository.database.View(ctx, func(ctx context.Context, connection storesqlite.ReadConn) error {
		var model turnAttributionModel
		if err := connection.WithContext(ctx).Take(&model, "turn_id = ?", turnID).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return ErrNotFound
			}
			return err
		}
		result = turnAttributionFromModel(model)
		return nil
	})
	return result, err
}

// RecomputeAttributions 丢弃并从 canonical Session/Turn facts 重建全部派生归因。
// canonical facts 和 usage 不会被更新；任一步失败时整个 writer transaction 回滚。
func (repository *Repository) RecomputeAttributions(
	ctx context.Context,
	request RecomputeAttributionsRequest,
) (RecomputeAttributionsReport, error) {
	if repository == nil || repository.database == nil {
		return RecomputeAttributionsReport{}, ErrInvalidRepository
	}
	if request.AtMS < 0 {
		return RecomputeAttributionsReport{}, invalidRecord("recompute timestamp must not be negative")
	}
	var report RecomputeAttributionsReport
	err := repository.WithinWriteUnit(ctx, func(unit *WriteUnit) error {
		var err error
		report, err = recomputeAttributionsInTransaction(unit.ctx, unit.transaction, &request.AtMS)
		return err
	})
	return report, err
}

func recomputeAttributionsInTransaction(
	ctx context.Context,
	transaction *gorm.DB,
	atMS *int64,
) (RecomputeAttributionsReport, error) {
	report := RecomputeAttributionsReport{RuleVersion: attribution.RuleVersion}
	database := transaction.WithContext(ctx)
	if err := database.Session(&gorm.Session{AllowGlobalUpdate: true}).Delete(&turnAttributionModel{}).Error; err != nil {
		return RecomputeAttributionsReport{}, err
	}
	if err := database.Session(&gorm.Session{AllowGlobalUpdate: true}).Delete(&sessionAttributionModel{}).Error; err != nil {
		return RecomputeAttributionsReport{}, err
	}
	if err := deleteUnreferencedPathProjects(database); err != nil {
		return RecomputeAttributionsReport{}, err
	}

	var sessionIDs []string
	if err := database.Model(&sessionModel{}).Order("session_id").Pluck("session_id", &sessionIDs).Error; err != nil {
		return RecomputeAttributionsReport{}, err
	}
	writer := attributionWriter{ctx: ctx, transaction: transaction}
	for _, sessionID := range sessionIDs {
		turnCount, err := writer.refreshSessionAttributions(sessionID, atMS)
		if err != nil {
			return RecomputeAttributionsReport{}, err
		}
		report.Sessions++
		report.Turns += turnCount
	}
	return report, nil
}

func deleteUnreferencedPathProjects(database *gorm.DB) error {
	sessionProjects := database.Model(&sessionModel{}).Select("project_id").Where("project_id IS NOT NULL")
	turnProjects := database.Model(&turnModel{}).Select("project_id").Where("project_id IS NOT NULL")
	return database.Where("project_id LIKE ?", localPathProjectPrefix+"%").
		Where("project_id NOT IN (?)", sessionProjects).
		Where("project_id NOT IN (?)", turnProjects).
		Delete(&projectModel{}).Error
}

type attributionWriter struct {
	ctx         context.Context
	transaction *gorm.DB
}

func (writer attributionWriter) refreshSessionAttributions(sessionID string, atMS *int64) (int, error) {
	database := writer.transaction.WithContext(writer.ctx)
	var session sessionModel
	if err := database.Take(&session, "session_id = ?", sessionID).Error; err != nil {
		return 0, err
	}
	var current sessionCurrentModel
	hasCurrent := true
	if err := database.Take(&current, "session_id = ?", sessionID).Error; err != nil {
		if !errors.Is(err, gorm.ErrRecordNotFound) {
			return 0, err
		}
		hasCurrent = false
	}
	var turns []turnModel
	if err := database.Where("session_id = ?", sessionID).
		Order("started_at_ms DESC").Order("turn_id DESC").Find(&turns).Error; err != nil {
		return 0, err
	}

	roots, err := projectRoots(database)
	if err != nil {
		return 0, err
	}
	projectResolver := projectidentity.NewResolver(projectAttributionPaths(session, current, hasCurrent, turns, roots))
	updatedAt := attributionTimestamp(session, current, hasCurrent, turns, atMS)

	initialProject, err := writer.resolveAndRegisterProject(session.InitialCWD, &roots, projectResolver, updatedAt)
	if err != nil {
		return 0, err
	}

	turnProjects := make(map[string]attribution.ProjectDecision, len(turns))
	turnModels := make(map[string]attribution.ModelDecision, len(turns))
	for _, turn := range turns {
		decisionRoots := append([]attribution.ProjectRoot(nil), roots...)
		project, err := writer.resolveAndRegisterProject(turn.CWD, &decisionRoots, projectResolver, updatedAt)
		if err != nil {
			return 0, err
		}
		model := attribution.NormalizeModel(valueOrEmpty(turn.Model))
		turnProjects[turn.TurnID] = project
		turnModels[turn.TurnID] = model
		attributionModel := turnAttributionPersistence(turn.TurnID, project, model, updatedAt)
		if err := upsertTurnAttribution(database, attributionModel); err != nil {
			return 0, err
		}
	}

	currentProject := attribution.ProjectDecision{
		Confidence: attribution.ConfidenceUnknown,
		Source:     attribution.SourceMissing,
		Reason:     attribution.ReasonMissing,
	}
	currentModel := attribution.NormalizeModel("")
	if hasCurrent {
		decisionRoots := append([]attribution.ProjectRoot(nil), roots...)
		currentProject, err = writer.resolveAndRegisterProject(current.CurrentCWD, &decisionRoots, projectResolver, updatedAt)
		if err != nil {
			return 0, err
		}
		currentModel = attribution.NormalizeModel(valueOrEmpty(current.CurrentModel))
	}

	projectCandidates := make([]attribution.Candidate, 0, 4)
	modelCandidates := make([]attribution.Candidate, 0, 3)
	appendProjectCandidate(&projectCandidates, currentProject, currentAttributionPriority)
	appendModelCandidate(&modelCandidates, currentModel, currentAttributionPriority)
	if len(turns) > 0 {
		latestStartedAt := turns[0].StartedAtMS
		for _, turn := range turns {
			if turn.StartedAtMS != latestStartedAt {
				break
			}
			appendProjectCandidate(&projectCandidates, turnProjects[turn.TurnID], currentAttributionPriority)
			appendModelCandidate(&modelCandidates, turnModels[turn.TurnID], currentAttributionPriority)
		}
	}
	appendProjectCandidate(&projectCandidates, initialProject, fallbackAttributionPriority)

	project := attribution.Arbitrate(projectCandidates)
	model := attribution.Arbitrate(modelCandidates)
	title := attribution.NormalizeSessionTitle(session.SessionID)
	sessionAttribution := sessionAttributionPersistence(session.SessionID, title, project, model, updatedAt)
	if err := upsertSessionAttribution(database, sessionAttribution); err != nil {
		return 0, err
	}
	return len(turns), nil
}

func projectRoots(database *gorm.DB) ([]attribution.ProjectRoot, error) {
	var models []projectModel
	if err := database.Order("root_path").Order("project_id").Find(&models).Error; err != nil {
		return nil, err
	}
	roots := make([]attribution.ProjectRoot, 0, len(models))
	for _, model := range models {
		if strings.HasPrefix(model.ProjectID, localPathProjectPrefix) {
			continue
		}
		roots = append(roots, attribution.ProjectRoot{
			ProjectID: model.ProjectID, RootPath: model.RootPath,
			DisplayName: model.DisplayName, Confidence: attribution.ConfidenceHigh,
		})
	}
	return roots, nil
}

func (writer attributionWriter) resolveAndRegisterProject(
	rawPath *string,
	roots *[]attribution.ProjectRoot,
	resolver projectidentity.Resolver,
	atMS int64,
) (attribution.ProjectDecision, error) {
	decision := resolveProjectDecision(valueOrEmpty(rawPath), *roots, resolver)
	if decision.ProjectID == "" || decision.Source != attribution.SourceCWDPathDigest {
		return decision, nil
	}
	project := Project{
		ProjectID: decision.ProjectID, DisplayName: decision.DisplayName, RootPath: decision.RootPath,
		CreatedAtMS: atMS, UpdatedAtMS: atMS,
	}
	if err := validateProjectReplay(writer.ctx, writer.transaction, project); err != nil {
		return attribution.ProjectDecision{}, err
	}
	if err := upsertProject(writer.ctx, writer.transaction, project); err != nil {
		return attribution.ProjectDecision{}, err
	}
	*roots = append(*roots, attribution.ProjectRoot{
		ProjectID: project.ProjectID, RootPath: project.RootPath,
		DisplayName: project.DisplayName, Confidence: attribution.ConfidenceMedium,
	})
	return decision, nil
}

func projectAttributionPaths(
	session sessionModel,
	current sessionCurrentModel,
	hasCurrent bool,
	turns []turnModel,
	roots []attribution.ProjectRoot,
) []string {
	paths := make([]string, 0, len(roots)+len(turns)+2)
	for _, root := range roots {
		paths = append(paths, root.RootPath)
	}
	if session.InitialCWD != nil {
		paths = append(paths, *session.InitialCWD)
	}
	if hasCurrent && current.CurrentCWD != nil {
		paths = append(paths, *current.CurrentCWD)
	}
	for _, turn := range turns {
		if turn.CWD != nil {
			paths = append(paths, *turn.CWD)
		}
	}
	return paths
}

func resolveProjectDecision(
	rawPath string,
	roots []attribution.ProjectRoot,
	resolver projectidentity.Resolver,
) attribution.ProjectDecision {
	raw := attribution.ResolveProject(attribution.ProjectInput{CWD: rawPath, Roots: roots})
	if raw.Source != attribution.SourceCWDPathDigest {
		return raw
	}
	resolution := resolver.Resolve(rawPath)
	if resolution.Other {
		return attribution.ResolveProject(attribution.ProjectInput{})
	}
	if resolution.CanonicalPath == rawPath {
		return raw
	}
	return attribution.ResolveProject(attribution.ProjectInput{
		CWD: resolution.CanonicalPath, Roots: roots,
	})
}

func appendProjectCandidate(
	candidates *[]attribution.Candidate,
	decision attribution.ProjectDecision,
	priority int,
) {
	if decision.ProjectID == "" &&
		(decision.Source == "" || decision.Source == attribution.SourceMissing) {
		return
	}
	*candidates = append(*candidates, attribution.Candidate{
		Key: decision.ProjectID, DisplayName: decision.DisplayName, Priority: priority,
		Confidence: decision.Confidence, Source: decision.Source, Reason: decision.Reason,
	})
}

func appendModelCandidate(
	candidates *[]attribution.Candidate,
	decision attribution.ModelDecision,
	priority int,
) {
	if decision.Key == "" &&
		(decision.Source == "" || decision.Source == attribution.SourceMissing) {
		return
	}
	*candidates = append(*candidates, attribution.Candidate{
		Key: decision.Key, DisplayName: decision.DisplayName, Priority: priority,
		Confidence: decision.Confidence, Source: decision.Source, Reason: decision.Reason,
	})
}

func attributionTimestamp(
	session sessionModel,
	current sessionCurrentModel,
	hasCurrent bool,
	turns []turnModel,
	override *int64,
) int64 {
	if override != nil {
		return *override
	}
	result := session.LastSeenAtMS
	if hasCurrent {
		result = max(result, current.UpdatedAtMS)
	}
	for _, turn := range turns {
		result = max(result, turn.StartedAtMS)
		if turn.CompletedAtMS != nil {
			result = max(result, *turn.CompletedAtMS)
		}
	}
	return result
}

func turnAttributionPersistence(
	turnID string,
	project attribution.ProjectDecision,
	model attribution.ModelDecision,
	updatedAt int64,
) turnAttributionModel {
	return turnAttributionModel{
		TurnID:            turnID,
		ProjectID:         optionalAttributionString(project.ProjectID),
		ProjectDisplay:    optionalAttributionString(project.DisplayName),
		ProjectConfidence: string(project.Confidence), ProjectSource: string(project.Source),
		ProjectReason: string(project.Reason), ModelKey: optionalAttributionString(model.Key),
		ModelDisplay: optionalAttributionString(model.DisplayName), ModelConfidence: string(model.Confidence),
		ModelSource: string(model.Source), ModelReason: string(model.Reason),
		RuleVersion: attribution.RuleVersion, UpdatedAtMS: updatedAt,
	}
}

func sessionAttributionPersistence(
	sessionID string,
	title attribution.SessionTitleDecision,
	project attribution.Decision,
	model attribution.Decision,
	updatedAt int64,
) sessionAttributionModel {
	return sessionAttributionModel{
		SessionID: sessionID, DisplayTitle: title.DisplayTitle,
		TitleConfidence: string(title.Confidence), TitleSource: string(title.Source),
		TitleReason: string(title.Reason), ProjectID: optionalAttributionString(project.Key),
		ProjectDisplay:    optionalAttributionString(project.DisplayName),
		ProjectConfidence: string(project.Confidence), ProjectSource: string(project.Source),
		ProjectReason: string(project.Reason), ModelKey: optionalAttributionString(model.Key),
		ModelDisplay: optionalAttributionString(model.DisplayName), ModelConfidence: string(model.Confidence),
		ModelSource: string(model.Source), ModelReason: string(model.Reason),
		RuleVersion: attribution.RuleVersion, UpdatedAtMS: updatedAt,
	}
}

func upsertSessionAttribution(database *gorm.DB, model sessionAttributionModel) error {
	var existing sessionAttributionModel
	err := database.Take(&existing, "session_id = ?", model.SessionID).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return database.Create(&model).Error
	}
	if err != nil {
		return err
	}
	return database.Model(&sessionAttributionModel{}).Where("session_id = ?", model.SessionID).Updates(map[string]any{
		"display_title": model.DisplayTitle, "title_confidence": model.TitleConfidence,
		"title_source": model.TitleSource, "title_reason": model.TitleReason,
		"project_id": model.ProjectID, "project_display_name": model.ProjectDisplay,
		"project_confidence": model.ProjectConfidence, "project_source": model.ProjectSource,
		"project_reason": model.ProjectReason, "model_key": model.ModelKey,
		"model_display_name": model.ModelDisplay, "model_confidence": model.ModelConfidence,
		"model_source": model.ModelSource, "model_reason": model.ModelReason,
		"rule_version": model.RuleVersion, "updated_at_ms": model.UpdatedAtMS,
	}).Error
}

func upsertTurnAttribution(database *gorm.DB, model turnAttributionModel) error {
	var existing turnAttributionModel
	err := database.Take(&existing, "turn_id = ?", model.TurnID).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return database.Create(&model).Error
	}
	if err != nil {
		return err
	}
	return database.Model(&turnAttributionModel{}).Where("turn_id = ?", model.TurnID).Updates(map[string]any{
		"project_id": model.ProjectID, "project_display_name": model.ProjectDisplay,
		"project_confidence": model.ProjectConfidence, "project_source": model.ProjectSource,
		"project_reason": model.ProjectReason, "model_key": model.ModelKey,
		"model_display_name": model.ModelDisplay, "model_confidence": model.ModelConfidence,
		"model_source": model.ModelSource, "model_reason": model.ModelReason,
		"rule_version": model.RuleVersion, "updated_at_ms": model.UpdatedAtMS,
	}).Error
}

func sessionAttributionFromModel(model sessionAttributionModel) SessionAttributionSnapshot {
	return SessionAttributionSnapshot{
		SessionID: model.SessionID, DisplayTitle: model.DisplayTitle,
		TitleConfidence: AttributionConfidence(model.TitleConfidence),
		TitleSource:     AttributionSource(model.TitleSource), TitleReason: AttributionReason(model.TitleReason),
		Project: ProjectAttribution{
			ProjectID: cloneAttributionString(model.ProjectID), DisplayName: cloneAttributionString(model.ProjectDisplay),
			Confidence: AttributionConfidence(model.ProjectConfidence),
			Source:     AttributionSource(model.ProjectSource), Reason: AttributionReason(model.ProjectReason),
		},
		Model: ModelAttribution{
			ModelKey: cloneAttributionString(model.ModelKey), DisplayName: cloneAttributionString(model.ModelDisplay),
			Confidence: AttributionConfidence(model.ModelConfidence),
			Source:     AttributionSource(model.ModelSource), Reason: AttributionReason(model.ModelReason),
		},
		RuleVersion: model.RuleVersion, UpdatedAtMS: model.UpdatedAtMS,
	}
}

func turnAttributionFromModel(model turnAttributionModel) TurnAttributionSnapshot {
	return TurnAttributionSnapshot{
		TurnID: model.TurnID,
		Project: ProjectAttribution{
			ProjectID: cloneAttributionString(model.ProjectID), DisplayName: cloneAttributionString(model.ProjectDisplay),
			Confidence: AttributionConfidence(model.ProjectConfidence),
			Source:     AttributionSource(model.ProjectSource), Reason: AttributionReason(model.ProjectReason),
		},
		Model: ModelAttribution{
			ModelKey: cloneAttributionString(model.ModelKey), DisplayName: cloneAttributionString(model.ModelDisplay),
			Confidence: AttributionConfidence(model.ModelConfidence),
			Source:     AttributionSource(model.ModelSource), Reason: AttributionReason(model.ModelReason),
		},
		RuleVersion: model.RuleVersion, UpdatedAtMS: model.UpdatedAtMS,
	}
}

func optionalAttributionString(value string) *string {
	if value == "" {
		return nil
	}
	return &value
}

func cloneAttributionString(value *string) *string {
	if value == nil {
		return nil
	}
	copy := *value
	return &copy
}

func valueOrEmpty(value *string) string {
	if value == nil {
		return ""
	}
	return *value
}
