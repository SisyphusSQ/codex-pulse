package cursorprovider

import (
	"bufio"
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/SisyphusSQ/codex-pulse/internal/store"
	_ "modernc.org/sqlite"
)

const (
	SourceTranscripts        = "cursor.transcripts"
	SourceState              = "cursor.state"
	SourceConversationSearch = "cursor.conversation_search"
	SourceAITracking         = "cursor.ai_tracking"
	SourceHooks              = "cursor.hooks"
	SourceDashboard          = "cursor.dashboard"

	maxTranscriptLineBytes = 16 << 20
)

var ErrCollector = errors.New("cursor provider collector is unavailable")

type SnapshotWriter interface {
	ReplaceCursorSnapshot(context.Context, store.CursorSnapshot) error
}

type Config struct {
	ProjectsRoot         string
	StateDatabase        string
	ConversationDatabase string
	AITrackingDatabase   string
	MinimumRefresh       time.Duration
	Now                  func() time.Time
}

func DefaultConfig() (Config, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return Config{}, err
	}
	if override := strings.TrimSpace(os.Getenv("CODEX_PULSE_CURSOR_HOME")); override != "" {
		if !filepath.IsAbs(override) {
			return Config{}, ErrCollector
		}
		home = filepath.Clean(override)
	}
	return Config{
		ProjectsRoot:         filepath.Join(home, ".cursor", "projects"),
		StateDatabase:        filepath.Join(home, "Library", "Application Support", "Cursor", "User", "globalStorage", "state.vscdb"),
		ConversationDatabase: filepath.Join(home, "Library", "Application Support", "Cursor", "User", "globalStorage", "conversation-search.db"),
		AITrackingDatabase:   filepath.Join(home, ".cursor", "ai-tracking", "ai-code-tracking.db"),
		MinimumRefresh:       15 * time.Second,
		Now:                  time.Now,
	}, nil
}

type Collector struct {
	writer SnapshotWriter
	config Config
	mu     sync.Mutex
	last   time.Time
}

func NewCollector(writer SnapshotWriter, config Config) (*Collector, error) {
	if writer == nil || config.ProjectsRoot == "" || config.StateDatabase == "" ||
		config.ConversationDatabase == "" || config.AITrackingDatabase == "" {
		return nil, ErrCollector
	}
	if config.Now == nil {
		config.Now = time.Now
	}
	if config.MinimumRefresh < 0 {
		return nil, ErrCollector
	}
	return &Collector{writer: writer, config: config}, nil
}

func (collector *Collector) Refresh(ctx context.Context) error {
	_, err := collector.RefreshIfDue(ctx)
	return err
}

// RefreshIfDue rebuilds the committed snapshot when the refresh interval has
// elapsed and reports whether a rebuild was attempted.
func (collector *Collector) RefreshIfDue(ctx context.Context) (bool, error) {
	if collector == nil || collector.writer == nil || ctx == nil {
		return false, ErrCollector
	}
	collector.mu.Lock()
	defer collector.mu.Unlock()
	now := collector.config.Now()
	if !collector.last.IsZero() && now.Sub(collector.last) < collector.config.MinimumRefresh {
		return false, nil
	}
	snapshot := newSnapshot(now.UnixMilli())
	mergeTranscripts(ctx, collector.config.ProjectsRoot, &snapshot)
	if err := ctx.Err(); err != nil {
		return true, err
	}
	mergeStateDatabase(ctx, collector.config.StateDatabase, &snapshot)
	if err := ctx.Err(); err != nil {
		return true, err
	}
	mergeConversationDatabase(ctx, collector.config.ConversationDatabase, &snapshot)
	if err := ctx.Err(); err != nil {
		return true, err
	}
	mergeAITrackingDatabase(ctx, collector.config.AITrackingDatabase, &snapshot)
	if err := ctx.Err(); err != nil {
		return true, err
	}
	snapshot.Sources = append(snapshot.Sources, notConfiguredSource(SourceHooks, "hooks", snapshot.CollectedAtMS))
	snapshot.finalize()
	if err := ctx.Err(); err != nil {
		return true, err
	}
	if err := collector.writer.ReplaceCursorSnapshot(ctx, snapshot.CursorSnapshot); err != nil {
		return true, fmt.Errorf("%w: persist snapshot: %w", ErrCollector, err)
	}
	collector.last = now
	return true, nil
}

type collectedSnapshot struct {
	store.CursorSnapshot
	sessions         map[string]*store.CursorSession
	composerMetadata map[string]composerMetadata
	projectRanks     map[string]int
	modelEvidence    map[string]cursorModelEvidence
	lineage          map[string]map[string]struct{}
	usage            map[string]store.CursorUsageEvent
	requests         map[string]store.CursorRequestEvent
	tools            map[string]store.CursorToolEvent
	edits            map[string]store.CursorAIEditEvent
}

func newSnapshot(atMS int64) collectedSnapshot {
	return collectedSnapshot{
		CursorSnapshot:   store.CursorSnapshot{Generation: atMS, CollectedAtMS: atMS},
		sessions:         make(map[string]*store.CursorSession),
		composerMetadata: make(map[string]composerMetadata),
		projectRanks:     make(map[string]int),
		modelEvidence:    make(map[string]cursorModelEvidence),
		lineage:          make(map[string]map[string]struct{}),
		usage:            make(map[string]store.CursorUsageEvent),
		requests:         make(map[string]store.CursorRequestEvent),
		tools:            make(map[string]store.CursorToolEvent),
		edits:            make(map[string]store.CursorAIEditEvent),
	}
}

func (snapshot *collectedSnapshot) session(externalID string, atMS int64) *store.CursorSession {
	externalID = normalizeID(externalID)
	if externalID == "" {
		return nil
	}
	if existing := snapshot.sessions[externalID]; existing != nil {
		if atMS > existing.LastActivityAtMS {
			existing.LastActivityAtMS = atMS
		}
		if atMS > 0 && (existing.CreatedAtMS == 0 || atMS < existing.CreatedAtMS) {
			existing.CreatedAtMS = atMS
		}
		snapshot.applyComposerMetadata(existing)
		return existing
	}
	value := &store.CursorSession{
		ExternalSessionID:  externalID,
		DisplayTitle:       fallbackSessionTitle(atMS),
		TitleSource:        "fallback",
		ProjectKey:         digestString("cursor:unknown-project:" + externalID),
		ProjectDisplayName: "未识别项目",
		CreatedAtMS:        maxInt64(atMS, 0),
		LastActivityAtMS:   maxInt64(atMS, 0),
		CoverageState:      "partial",
		UpdatedAtMS:        snapshot.CollectedAtMS,
	}
	snapshot.sessions[externalID] = value
	snapshot.applyComposerMetadata(value)
	return value
}

type composerMetadata struct {
	Name              string
	WorkspaceID       string
	WorkspacePath     string
	RepositoryPath    string
	ModelID           string
	UpdatedAtMS       int64
	TotalLinesAdded   *int64
	TotalLinesRemoved *int64
	IsDraft           bool
	IsEphemeral       bool
}

type cursorModelEvidence struct {
	model        string
	occurredAtMS int64
	rank         int
}

func (snapshot *collectedSnapshot) applyComposerMetadata(session *store.CursorSession) {
	metadata, ok := snapshot.composerMetadata[session.ExternalSessionID]
	if !ok {
		return
	}
	setCursorSessionTitle(session, metadata.Name, "cursor_composer_header")
	projectPath := metadata.WorkspacePath
	if projectPath == "" {
		projectPath = metadata.RepositoryPath
	}
	projectSeed := metadata.WorkspaceID
	if projectSeed == "" {
		projectSeed = projectPath
	}
	snapshot.setSessionProject(session, projectSeed, projectDisplayName(projectPath), 2)
	snapshot.setSessionModel(session, metadata.ModelID, metadata.UpdatedAtMS, 1)
	session.AILinesAdded = nonNegative(metadata.TotalLinesAdded)
	session.AILinesRemoved = nonNegative(metadata.TotalLinesRemoved)
}

func (snapshot *collectedSnapshot) setSessionModel(
	session *store.CursorSession,
	model string,
	occurredAtMS int64,
	rank int,
) {
	model = normalizeLabel(model)
	if session == nil || model == "" {
		return
	}
	previous, exists := snapshot.modelEvidence[session.ExternalSessionID]
	if exists && (rank < previous.rank ||
		(rank == previous.rank && occurredAtMS < previous.occurredAtMS) ||
		(rank == previous.rank && occurredAtMS == previous.occurredAtMS && model >= previous.model)) {
		return
	}
	snapshot.modelEvidence[session.ExternalSessionID] = cursorModelEvidence{
		model: model, occurredAtMS: occurredAtMS, rank: rank,
	}
	session.ModelKey = &model
}

func (snapshot *collectedSnapshot) setSessionProject(
	session *store.CursorSession,
	seed string,
	displayName string,
	rank int,
) {
	seed = strings.TrimSpace(seed)
	currentRank := snapshot.projectRanks[session.ExternalSessionID]
	if seed == "" || rank < currentRank {
		return
	}
	if displayName == "" {
		displayName = "未识别项目"
	}
	projectKey := digestString("cursor:workspace:" + seed)
	if rank == currentRank && currentRank > 0 && projectKey >= session.ProjectKey {
		return
	}
	session.ProjectKey = projectKey
	session.ProjectDisplayName = displayName
	snapshot.projectRanks[session.ExternalSessionID] = rank
}

func (snapshot *collectedSnapshot) finalize() {
	for _, session := range snapshot.sessions {
		if session.LastActivityAtMS < session.CreatedAtMS {
			session.LastActivityAtMS = session.CreatedAtMS
		}
		if digests := snapshot.lineage[session.ExternalSessionID]; len(digests) > 1 {
			session.LineageConflict = true
			if snapshot.projectRanks[session.ExternalSessionID] < 2 {
				session.ProjectKey = digestString("cursor:unknown-project:" + session.ExternalSessionID)
				session.ProjectDisplayName = "未识别项目"
			}
		}
		if session.RequestCount > 0 {
			session.CoverageState = "exact"
		}
		if session.TitleSource == "fallback" {
			session.DisplayTitle = fallbackSessionTitle(session.LastActivityAtMS)
		}
		snapshot.Sessions = append(snapshot.Sessions, *session)
	}
	for _, event := range snapshot.usage {
		snapshot.UsageEvents = append(snapshot.UsageEvents, event)
	}
	for _, event := range snapshot.requests {
		snapshot.RequestEvents = append(snapshot.RequestEvents, event)
	}
	for _, event := range snapshot.tools {
		snapshot.ToolEvents = append(snapshot.ToolEvents, event)
	}
	for _, event := range snapshot.edits {
		snapshot.AIEditEvents = append(snapshot.AIEditEvents, event)
	}
	sort.Slice(snapshot.Sessions, func(i, j int) bool {
		if snapshot.Sessions[i].LastActivityAtMS != snapshot.Sessions[j].LastActivityAtMS {
			return snapshot.Sessions[i].LastActivityAtMS > snapshot.Sessions[j].LastActivityAtMS
		}
		return snapshot.Sessions[i].ExternalSessionID > snapshot.Sessions[j].ExternalSessionID
	})
	sort.Slice(snapshot.UsageEvents, func(i, j int) bool { return snapshot.UsageEvents[i].EventID < snapshot.UsageEvents[j].EventID })
	sort.Slice(snapshot.RequestEvents, func(i, j int) bool { return snapshot.RequestEvents[i].EventID < snapshot.RequestEvents[j].EventID })
	sort.Slice(snapshot.ToolEvents, func(i, j int) bool { return snapshot.ToolEvents[i].EventID < snapshot.ToolEvents[j].EventID })
	sort.Slice(snapshot.AIEditEvents, func(i, j int) bool { return snapshot.AIEditEvents[i].EventID < snapshot.AIEditEvents[j].EventID })
}

type transcriptEnvelope struct {
	Role    string `json:"role"`
	Type    string `json:"type"`
	Message struct {
		Role    string `json:"role"`
		Content []struct {
			Type string `json:"type"`
			Name string `json:"name"`
		} `json:"content"`
	} `json:"message"`
}

func mergeTranscripts(ctx context.Context, root string, snapshot *collectedSnapshot) {
	attempt := snapshot.CollectedAtMS
	status := availableSource(SourceTranscripts, "transcript_jsonl", attempt, nil, "filesystem_scan")
	rootInfo, err := os.Stat(root)
	if err != nil || !rootInfo.IsDir() {
		status = failedSource(SourceTranscripts, "transcript_jsonl", attempt, filesystemFailure(err), "filesystem_scan")
		snapshot.Sources = append(snapshot.Sources, status)
		return
	}
	err = filepath.WalkDir(root, func(path string, entry fs.DirEntry, walkErr error) error {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		if walkErr != nil {
			return nil
		}
		if entry.IsDir() || entry.Type()&os.ModeSymlink != 0 || filepath.Ext(entry.Name()) != ".jsonl" ||
			!strings.Contains(filepath.ToSlash(path), "/agent-transcripts/") {
			return nil
		}
		status.RowCount++
		mergeTranscriptFile(path, root, snapshot)
		return nil
	})
	if err != nil {
		status.State, status.CoverageState, status.FailureCode = "partial", "partial", pointer("read_failed")
	}
	checkpoint := digestString(strconv.FormatInt(status.RowCount, 10) + ":" + strconv.FormatInt(attempt, 10))
	status.CheckpointValue = &checkpoint
	snapshot.Sources = append(snapshot.Sources, status)
}

func mergeTranscriptFile(path, root string, snapshot *collectedSnapshot) {
	file, err := os.Open(path)
	if err != nil {
		return
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil || info.Size() > 256<<20 {
		return
	}
	externalID := normalizeID(strings.TrimSuffix(filepath.Base(path), filepath.Ext(path)))
	if externalID == "" {
		externalID = normalizeID(filepath.Base(filepath.Dir(path)))
	}
	if externalID == "" {
		return
	}
	relative, _ := filepath.Rel(root, path)
	parts := strings.Split(filepath.ToSlash(relative), "/")
	projectSeed := ""
	if len(parts) > 0 {
		projectSeed = parts[0]
	}
	session := snapshot.session(externalID, info.ModTime().UnixMilli())
	if session == nil {
		return
	}
	snapshot.setSessionProject(session, "transcript:"+projectSeed, "未识别项目", 1)
	hasher := sha256.New()
	reader := io.TeeReader(file, hasher)
	scanner := bufio.NewScanner(reader)
	scanner.Buffer(make([]byte, 64<<10), maxTranscriptLineBytes)
	lineNumber := 0
	for scanner.Scan() {
		lineNumber++
		var envelope transcriptEnvelope
		if json.Unmarshal(scanner.Bytes(), &envelope) != nil {
			continue
		}
		for index, content := range envelope.Message.Content {
			name := normalizeLabel(content.Name)
			if content.Type != "tool_use" || name == "" {
				continue
			}
			eventID := digestString(externalID + ":transcript:" + strconv.Itoa(lineNumber) + ":" + strconv.Itoa(index) + ":" + name)
			snapshot.tools[eventID] = store.CursorToolEvent{
				EventID: eventID, ExternalSessionID: externalID, OccurredAtMS: info.ModTime().UnixMilli(),
				ToolName: name, Outcome: "unknown", Provenance: "cursor_transcript", UpdatedAtMS: snapshot.CollectedAtMS,
			}
		}
	}
	contentDigest := hex.EncodeToString(hasher.Sum(nil))
	lineageKey := digestString(filepath.ToSlash(relative))
	snapshot.Lineage = append(snapshot.Lineage, store.CursorSessionLineage{
		ExternalSessionID: externalID, SourceKey: SourceTranscripts, LineageKey: lineageKey,
		ContentDigest: contentDigest, ObservedAtMS: info.ModTime().UnixMilli(),
	})
	if snapshot.lineage[externalID] == nil {
		snapshot.lineage[externalID] = make(map[string]struct{})
	}
	snapshot.lineage[externalID][contentDigest] = struct{}{}
}

type usageAggregate struct {
	composerID string
	requestID  string
	occurredAt int64
	model      string
	input      int64
	output     int64
	stable     bool
}

func mergeStateDatabase(ctx context.Context, path string, snapshot *collectedSnapshot) {
	status := availableSource(SourceState, "sqlite_snapshot", snapshot.CollectedAtMS, pointerInt64(1), "snapshot")
	database, transaction, version, err := openReadSnapshot(ctx, path)
	if err != nil {
		status = failedSource(SourceState, "sqlite_snapshot", snapshot.CollectedAtMS, sqliteFailure(err), "snapshot")
		snapshot.Sources = append(snapshot.Sources, status)
		return
	}
	defer database.Close()
	defer transaction.Rollback()
	status.SchemaVersion = &version
	if version != 1 || !hasColumns(ctx, transaction, "composerHeaders", "composerId", "createdAt", "lastUpdatedAt", "value") ||
		!hasColumns(ctx, transaction, "cursorDiskKV", "key", "value") {
		status = failedSource(SourceState, "sqlite_snapshot", snapshot.CollectedAtMS, "schema_incompatible", "snapshot")
		status.SchemaVersion = &version
		snapshot.Sources = append(snapshot.Sources, status)
		return
	}
	if err := mergeComposerHeaders(ctx, transaction, snapshot); err != nil {
		status.State, status.CoverageState, status.FailureCode = "partial", "partial", pointer("read_failed")
	}
	usageCount, exactUsage, err := mergeStateUsageAndTools(ctx, transaction, snapshot)
	if err != nil {
		status.State, status.CoverageState, status.FailureCode = "partial", "partial", pointer("read_failed")
	} else if !exactUsage {
		status.State, status.CoverageState = "partial", "partial"
	}
	status.RowCount = usageCount
	checkpoint := digestString(strconv.FormatInt(version, 10) + ":" + strconv.FormatInt(usageCount, 10))
	status.CheckpointValue = &checkpoint
	snapshot.Sources = append(snapshot.Sources, status)
}

func mergeComposerHeaders(ctx context.Context, transaction *sql.Tx, snapshot *collectedSnapshot) error {
	rows, err := transaction.QueryContext(ctx, `SELECT composerId, createdAt, lastUpdatedAt, CAST(value AS TEXT) FROM composerHeaders`)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var composerID string
		var createdAt, updatedAt int64
		var value string
		if rows.Scan(&composerID, &createdAt, &updatedAt, &value) != nil {
			continue
		}
		var raw struct {
			Name              string `json:"name"`
			IsDraft           bool   `json:"isDraft"`
			IsEphemeral       bool   `json:"isEphemeral"`
			ModelID           string `json:"modelId"`
			TotalLinesAdded   *int64 `json:"totalLinesAdded"`
			TotalLinesRemoved *int64 `json:"totalLinesRemoved"`
			TrackedGitRepos   []struct {
				RepoPath string `json:"repoPath"`
			} `json:"trackedGitRepos"`
			Workspace struct {
				ID  string `json:"id"`
				URI struct {
					FSPath string `json:"fsPath"`
				} `json:"uri"`
			} `json:"workspaceIdentifier"`
		}
		_ = json.Unmarshal([]byte(value), &raw)
		metadata := composerMetadata{
			Name: normalizeTitle(raw.Name), WorkspaceID: normalizeID(raw.Workspace.ID),
			WorkspacePath: raw.Workspace.URI.FSPath, ModelID: raw.ModelID,
			UpdatedAtMS:     updatedAt,
			TotalLinesAdded: raw.TotalLinesAdded, TotalLinesRemoved: raw.TotalLinesRemoved,
			IsDraft: raw.IsDraft, IsEphemeral: raw.IsEphemeral,
		}
		if len(raw.TrackedGitRepos) > 0 {
			metadata.RepositoryPath = raw.TrackedGitRepos[0].RepoPath
		}
		composerID = normalizeID(composerID)
		if composerID == "" {
			continue
		}
		snapshot.composerMetadata[composerID] = metadata
		if metadata.IsDraft || metadata.IsEphemeral {
			if existing := snapshot.sessions[composerID]; existing != nil {
				snapshot.applyComposerMetadata(existing)
			}
			continue
		}
		session := snapshot.session(composerID, maxInt64(createdAt, updatedAt))
		if session == nil {
			continue
		}
		session.CreatedAtMS = minPositive(session.CreatedAtMS, createdAt)
		session.LastActivityAtMS = maxInt64(session.LastActivityAtMS, updatedAt)
	}
	return rows.Err()
}

func mergeStateUsageAndTools(
	ctx context.Context,
	transaction *sql.Tx,
	snapshot *collectedSnapshot,
) (int64, bool, error) {
	rows, err := transaction.QueryContext(ctx, `SELECT
		substr(key, 10, 36),
		COALESCE(json_extract(CAST(value AS TEXT), '$.requestId'), ''),
		COALESCE(json_extract(CAST(value AS TEXT), '$.createdAt'), ''),
		COALESCE(json_extract(CAST(value AS TEXT), '$.modelInfo.modelName'), ''),
		json_extract(CAST(value AS TEXT), '$.tokenCount.inputTokens'),
		json_extract(CAST(value AS TEXT), '$.tokenCount.outputTokens'),
		COALESCE(json_extract(CAST(value AS TEXT), '$.toolFormerData.toolCallId'), ''),
		COALESCE(json_extract(CAST(value AS TEXT), '$.toolFormerData.name'), ''),
		COALESCE(json_extract(CAST(value AS TEXT), '$.toolFormerData.status'), '')
		FROM cursorDiskKV WHERE key LIKE 'bubbleId:%' AND json_valid(CAST(value AS TEXT))`)
	if err != nil {
		return 0, false, err
	}
	defer rows.Close()
	usageByRequest := make(map[string]usageAggregate)
	observedRequests := make(map[string]struct{})
	sessionsWithStateTools := make(map[string]struct{})
	var rowCount int64
	for rows.Next() {
		rowCount++
		var composerID, requestID, created, model, toolCallID, toolName, toolStatus string
		var input, output sql.NullInt64
		if rows.Scan(&composerID, &requestID, &created, &model, &input, &output, &toolCallID, &toolName, &toolStatus) != nil {
			continue
		}
		composerID, requestID = normalizeID(composerID), normalizeID(requestID)
		occurredAt := parseTimestamp(created)
		session := snapshot.session(composerID, occurredAt)
		snapshot.setSessionModel(session, model, occurredAt, 2)
		if requestID != "" {
			observedRequests[requestID] = struct{}{}
		}
		if requestID != "" && composerID != "" {
			candidate := store.CursorRequestEvent{
				EventID: requestID, ExternalSessionID: composerID, OccurredAtMS: occurredAt,
				UpdatedAtMS: snapshot.CollectedAtMS,
			}
			if previous, exists := snapshot.requests[requestID]; exists {
				candidate.OccurredAtMS = minPositive(previous.OccurredAtMS, candidate.OccurredAtMS)
				if candidate.ExternalSessionID == "" {
					candidate.ExternalSessionID = previous.ExternalSessionID
				}
			}
			snapshot.requests[requestID] = candidate
		}
		if requestID != "" && input.Valid && output.Valid && input.Int64 >= 0 && output.Int64 >= 0 &&
			(input.Int64 > 0 || output.Int64 > 0) {
			candidate := usageAggregate{
				composerID: composerID, requestID: requestID, occurredAt: occurredAt,
				model: normalizeLabel(model), input: input.Int64, output: output.Int64, stable: true,
			}
			if previous, exists := usageByRequest[requestID]; exists {
				candidate.stable = previous.stable && previous.input == candidate.input && previous.output == candidate.output
				candidate.occurredAt = minPositive(previous.occurredAt, candidate.occurredAt)
				if candidate.composerID == "" {
					candidate.composerID = previous.composerID
				}
				if candidate.model == "" {
					candidate.model = previous.model
				}
			}
			usageByRequest[requestID] = candidate
		}
		name := normalizeLabel(toolName)
		if name != "" && composerID != "" {
			eventSeed := normalizeID(toolCallID)
			if eventSeed == "" {
				eventSeed = composerID + ":" + name + ":" + created
			}
			eventID := digestString("state:" + eventSeed)
			snapshot.tools[eventID] = store.CursorToolEvent{
				EventID: eventID, ExternalSessionID: composerID, OccurredAtMS: occurredAt,
				ToolName: name, Outcome: normalizeToolOutcome(toolStatus), Provenance: "cursor_state",
				UpdatedAtMS: snapshot.CollectedAtMS,
			}
			sessionsWithStateTools[composerID] = struct{}{}
		}
		_ = session
	}
	if err := rows.Err(); err != nil {
		return rowCount, false, err
	}
	exactUsage := true
	for requestID, aggregate := range usageByRequest {
		if !aggregate.stable || aggregate.composerID == "" {
			exactUsage = false
			continue
		}
		var model *string
		if aggregate.model != "" {
			model = &aggregate.model
		}
		snapshot.usage[requestID] = store.CursorUsageEvent{
			EventID: requestID, ExternalSessionID: aggregate.composerID, OccurredAtMS: aggregate.occurredAt,
			ModelKey: model, InputTokens: aggregate.input, OutputTokens: aggregate.output,
			UpdatedAtMS: snapshot.CollectedAtMS,
		}
	}
	for requestID := range observedRequests {
		if aggregate, ok := usageByRequest[requestID]; !ok || !aggregate.stable || aggregate.composerID == "" {
			exactUsage = false
		}
	}
	for _, event := range snapshot.requests {
		if session := snapshot.sessions[event.ExternalSessionID]; session != nil {
			session.RequestCount++
		}
	}
	for sessionID := range sessionsWithStateTools {
		for eventID, event := range snapshot.tools {
			if event.ExternalSessionID == sessionID && event.Provenance == "cursor_transcript" {
				delete(snapshot.tools, eventID)
			}
		}
	}
	for _, event := range snapshot.tools {
		if session := snapshot.sessions[event.ExternalSessionID]; session != nil {
			session.ToolCallCount++
		}
	}
	return rowCount, exactUsage, nil
}

func mergeConversationDatabase(ctx context.Context, path string, snapshot *collectedSnapshot) {
	status := availableSource(SourceConversationSearch, "sqlite_snapshot", snapshot.CollectedAtMS, pointerInt64(9), "snapshot")
	database, transaction, version, err := openReadSnapshot(ctx, path)
	if err != nil {
		status = failedSource(SourceConversationSearch, "sqlite_snapshot", snapshot.CollectedAtMS, sqliteFailure(err), "snapshot")
		snapshot.Sources = append(snapshot.Sources, status)
		return
	}
	defer database.Close()
	defer transaction.Rollback()
	status.SchemaVersion = &version
	if version != 9 || !hasColumns(ctx, transaction, "conversations", "id", "updated_at", "title") {
		status = failedSource(SourceConversationSearch, "sqlite_snapshot", snapshot.CollectedAtMS, "schema_incompatible", "snapshot")
		status.SchemaVersion = &version
		snapshot.Sources = append(snapshot.Sources, status)
		return
	}
	rows, err := transaction.QueryContext(ctx, `SELECT id, updated_at, title FROM conversations WHERE source = 'local'`)
	if err != nil {
		status.State, status.CoverageState, status.FailureCode = "partial", "partial", pointer("read_failed")
		snapshot.Sources = append(snapshot.Sources, status)
		return
	}
	defer rows.Close()
	for rows.Next() {
		var id, title string
		var updatedAt int64
		if rows.Scan(&id, &updatedAt, &title) == nil {
			session := snapshot.session(id, updatedAt)
			if session != nil {
				setCursorSessionTitle(session, title, "cursor_conversation_search")
			}
			status.RowCount++
		}
	}
	checkpoint := digestString(strconv.FormatInt(version, 10) + ":" + strconv.FormatInt(status.RowCount, 10))
	status.CheckpointValue = &checkpoint
	snapshot.Sources = append(snapshot.Sources, status)
}

func mergeAITrackingDatabase(ctx context.Context, path string, snapshot *collectedSnapshot) {
	status := availableSource(SourceAITracking, "sqlite_snapshot", snapshot.CollectedAtMS, pointerInt64(0), "snapshot")
	database, transaction, version, err := openReadSnapshot(ctx, path)
	if err != nil {
		status = failedSource(SourceAITracking, "sqlite_snapshot", snapshot.CollectedAtMS, sqliteFailure(err), "snapshot")
		snapshot.Sources = append(snapshot.Sources, status)
		return
	}
	defer database.Close()
	defer transaction.Rollback()
	status.SchemaVersion = &version
	if version != 0 || !hasColumns(ctx, transaction, "ai_code_hashes", "requestId", "conversationId", "timestamp", "model") {
		status = failedSource(SourceAITracking, "sqlite_snapshot", snapshot.CollectedAtMS, "schema_incompatible", "snapshot")
		status.SchemaVersion = &version
		snapshot.Sources = append(snapshot.Sources, status)
		return
	}
	rows, err := transaction.QueryContext(ctx, `SELECT
		requestId,
		CASE WHEN COUNT(DISTINCT conversationId) = 1 THEN MIN(conversationId) ELSE '' END,
		MIN(timestamp),
		CASE WHEN COUNT(DISTINCT model) = 1 THEN MIN(model) ELSE '' END,
		COUNT(*)
		FROM ai_code_hashes
		WHERE requestId IS NOT NULL AND requestId <> '' AND timestamp >= 0
		GROUP BY requestId`)
	if err != nil {
		status.State, status.CoverageState, status.FailureCode = "partial", "partial", pointer("read_failed")
		snapshot.Sources = append(snapshot.Sources, status)
		return
	}
	defer rows.Close()
	for rows.Next() {
		var requestID, conversationID, modelValue string
		var occurredAt, count int64
		if rows.Scan(&requestID, &conversationID, &occurredAt, &modelValue, &count) != nil || count <= 0 {
			continue
		}
		conversationID = normalizeID(conversationID)
		var externalID *string
		if conversationID != "" {
			externalID = &conversationID
			if snapshot.sessions[conversationID] == nil {
				externalID = nil
			}
		}
		modelValue = normalizeLabel(modelValue)
		var model *string
		if modelValue != "" {
			model = &modelValue
		}
		eventID := digestString(requestID)
		snapshot.edits[eventID] = store.CursorAIEditEvent{
			EventID: eventID, ExternalSessionID: externalID, OccurredAtMS: occurredAt,
			ModelKey: model, EditCount: 1, UpdatedAtMS: snapshot.CollectedAtMS,
		}
		status.RowCount++
		if session := snapshot.sessions[conversationID]; session != nil {
			session.AIEditCount++
			snapshot.setSessionModel(session, modelValue, occurredAt, 2)
		}
	}
	checkpoint := digestString(strconv.FormatInt(version, 10) + ":" + strconv.FormatInt(status.RowCount, 10))
	status.CheckpointValue = &checkpoint
	snapshot.Sources = append(snapshot.Sources, status)
}

func openReadSnapshot(ctx context.Context, path string) (*sql.DB, *sql.Tx, int64, error) {
	if _, err := os.Stat(path); err != nil {
		return nil, nil, 0, err
	}
	uri := (&url.URL{Scheme: "file", Path: path}).String() + "?mode=ro&_pragma=query_only(1)&_pragma=busy_timeout(3000)"
	database, err := sql.Open("sqlite", uri)
	if err != nil {
		return nil, nil, 0, err
	}
	database.SetMaxOpenConns(1)
	if err := database.PingContext(ctx); err != nil {
		database.Close()
		return nil, nil, 0, err
	}
	transaction, err := database.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		database.Close()
		return nil, nil, 0, err
	}
	var version int64
	if err := transaction.QueryRowContext(ctx, "PRAGMA user_version").Scan(&version); err != nil {
		transaction.Rollback()
		database.Close()
		return nil, nil, 0, err
	}
	return database, transaction, version, nil
}

func hasColumns(ctx context.Context, transaction *sql.Tx, table string, columns ...string) bool {
	rows, err := transaction.QueryContext(ctx, "PRAGMA table_info("+table+")")
	if err != nil {
		return false
	}
	defer rows.Close()
	found := make(map[string]bool, len(columns))
	for rows.Next() {
		var cid int
		var name, columnType string
		var notNull, primaryKey int
		var defaultValue any
		if rows.Scan(&cid, &name, &columnType, &notNull, &defaultValue, &primaryKey) == nil {
			found[name] = true
		}
	}
	for _, column := range columns {
		if !found[column] {
			return false
		}
	}
	return true
}

func availableSource(key, sourceType string, atMS int64, version *int64, checkpointKind string) store.CursorSourceStatus {
	return store.CursorSourceStatus{
		Provider: "cursor", SourceKey: key, SourceType: sourceType, State: "available", CoverageState: "exact",
		SchemaVersion: version, CheckpointKind: checkpointKind, LastAttemptAtMS: atMS,
		LastSuccessAtMS: &atMS, UpdatedAtMS: atMS,
	}
}

func failedSource(key, sourceType string, atMS int64, code, checkpointKind string) store.CursorSourceStatus {
	return store.CursorSourceStatus{
		Provider: "cursor", SourceKey: key, SourceType: sourceType, State: "unavailable", CoverageState: "unknown",
		CheckpointKind: checkpointKind, LastAttemptAtMS: atMS, FailureCode: &code, UpdatedAtMS: atMS,
	}
}

func notConfiguredSource(key, sourceType string, atMS int64) store.CursorSourceStatus {
	code := "not_configured"
	return store.CursorSourceStatus{
		Provider: "cursor", SourceKey: key, SourceType: sourceType, State: "not_configured", CoverageState: "unknown",
		CheckpointKind: "not_configured", LastAttemptAtMS: atMS, FailureCode: &code, UpdatedAtMS: atMS,
	}
}

func sqliteFailure(err error) string {
	if errors.Is(err, os.ErrNotExist) {
		return "missing"
	}
	if errors.Is(err, os.ErrPermission) {
		return "permission"
	}
	return "read_failed"
}

func filesystemFailure(err error) string {
	if errors.Is(err, os.ErrPermission) {
		return "permission"
	}
	return "missing"
}

func normalizeID(value string) string {
	value = strings.TrimSpace(value)
	if len(value) == 0 || len(value) > 128 || strings.ContainsAny(value, "\x00\r\n") {
		return ""
	}
	return value
}

func normalizeLabel(value string) string {
	value = strings.TrimSpace(value)
	if len(value) == 0 || len(value) > 128 || strings.ContainsAny(value, "/\\\x00\r\n") {
		return ""
	}
	return value
}

func normalizeTitle(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return ""
	}
	value = strings.Map(func(character rune) rune {
		if character == '\x00' || character == '\r' || character == '\n' || character < 0x20 {
			return ' '
		}
		return character
	}, value)
	value = strings.Join(strings.Fields(value), " ")
	runes := []rune(value)
	if len(runes) > 128 {
		value = string(runes[:128])
	}
	return value
}

func setCursorSessionTitle(session *store.CursorSession, value, source string) {
	title := normalizeTitle(value)
	if title == "" {
		return
	}
	if session.TitleSource == "cursor_composer_header" ||
		(session.TitleSource == "cursor_conversation_search" && source != "cursor_composer_header") {
		return
	}
	session.DisplayTitle = title
	session.TitleSource = source
}

func fallbackSessionTitle(atMS int64) string {
	if atMS <= 0 {
		return "未命名会话"
	}
	return "未命名会话 · " + time.UnixMilli(atMS).UTC().Format("2006-01-02")
}

func projectDisplayName(path string) string {
	path = strings.TrimSpace(path)
	if path == "" {
		return ""
	}
	name := filepath.Base(filepath.Clean(path))
	if strings.EqualFold(filepath.Ext(name), ".code-workspace") {
		name = strings.TrimSuffix(name, filepath.Ext(name))
	}
	return normalizeLabel(name)
}

func normalizeToolOutcome(value string) string {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "completed", "success", "succeeded":
		return "succeeded"
	case "error", "failed", "cancelled", "canceled":
		return "failed"
	default:
		return "unknown"
	}
}

func parseTimestamp(value string) int64 {
	if numeric, err := strconv.ParseInt(value, 10, 64); err == nil {
		if numeric > 0 && numeric < 10_000_000_000 {
			return numeric * 1_000
		}
		return maxInt64(numeric, 0)
	}
	if parsed, err := time.Parse(time.RFC3339Nano, value); err == nil {
		return parsed.UnixMilli()
	}
	return 0
}

func digestString(value string) string {
	digest := sha256.Sum256([]byte(value))
	return hex.EncodeToString(digest[:])
}

func pointer(value string) *string    { return &value }
func pointerInt64(value int64) *int64 { return &value }
func maxInt64(left, right int64) int64 {
	if left > right {
		return left
	}
	return right
}
func minPositive(left, right int64) int64 {
	if left <= 0 {
		return maxInt64(right, 0)
	}
	if right <= 0 {
		return left
	}
	if left < right {
		return left
	}
	return right
}
func nonNegative(value *int64) *int64 {
	if value == nil || *value < 0 {
		return nil
	}
	copy := *value
	return &copy
}
