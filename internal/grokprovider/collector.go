package grokprovider

import (
	"bufio"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/SisyphusSQ/codex-pulse/internal/store"
)

const maxUpdatesLineBytes = 16 << 20

type SnapshotWriter interface {
	ReplaceGrokSnapshot(context.Context, store.GrokSnapshot) error
}

type Collector struct {
	writer         SnapshotWriter
	config         Config
	mu             sync.Mutex
	last           time.Time
	updatesCursors map[string]updatesFileCursor
}

type updatesFileCursor struct {
	size     int64
	modNano  int64
	offset   int64
	device   uint64
	inode    uint64
	usage    []store.GrokUsageEvent
	tools    []store.GrokToolEvent
	turns    map[string]int64
	newTools map[string]int64
}

func NewCollector(writer SnapshotWriter, config Config) (*Collector, error) {
	if writer == nil || config.SessionsRoot == "" {
		return nil, ErrCollector
	}
	if config.Now == nil {
		config.Now = time.Now
	}
	if config.MinimumRefresh < 0 {
		return nil, ErrCollector
	}
	return &Collector{writer: writer, config: config, updatesCursors: map[string]updatesFileCursor{}}, nil
}

func (collector *Collector) Refresh(ctx context.Context) error {
	_, err := collector.RefreshIfDue(ctx)
	return err
}

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
	snapshot, cursors := collectGrokHome(ctx, collector.config, collector.updatesCursors, now.UnixMilli())
	if err := ctx.Err(); err != nil {
		return true, err
	}
	if err := collector.writer.ReplaceGrokSnapshot(ctx, snapshot); err != nil {
		return true, fmt.Errorf("%w: persist snapshot: %w", ErrCollector, err)
	}
	collector.updatesCursors = cursors
	collector.last = now
	return true, nil
}

type collectedSnapshot struct {
	store.GrokSnapshot
	sessions map[string]*store.GrokSession
	lineage  map[string]store.GrokSessionLineage
	usage    map[string]store.GrokUsageEvent
	tools    map[string]store.GrokToolEvent
}

func collectGrokHome(
	ctx context.Context,
	config Config,
	previous map[string]updatesFileCursor,
	atMS int64,
) (store.GrokSnapshot, map[string]updatesFileCursor) {
	snapshot := collectedSnapshot{
		GrokSnapshot: store.GrokSnapshot{Generation: atMS, CollectedAtMS: atMS},
		sessions:     make(map[string]*store.GrokSession),
		lineage:      make(map[string]store.GrokSessionLineage),
		usage:        make(map[string]store.GrokUsageEvent),
		tools:        make(map[string]store.GrokToolEvent),
	}
	summaryState := scanSummaries(ctx, config.SessionsRoot, &snapshot, atMS)
	updatesState, cursors := scanUpdates(ctx, config.SessionsRoot, previous, &snapshot, atMS)
	snapshot.Sources = []store.CursorSourceStatus{
		summaryState,
		updatesState,
		notConfiguredSource(SourceSessionSearch, "sqlite_fts", atMS),
	}
	snapshot.finalize()
	return snapshot.GrokSnapshot, cursors
}

func scanSummaries(ctx context.Context, root string, snapshot *collectedSnapshot, atMS int64) store.CursorSourceStatus {
	status := store.CursorSourceStatus{
		Provider: "grok", SourceKey: SourceSummary, SourceType: "filesystem_scan",
		State: "unavailable", CoverageState: "unknown", CheckpointKind: "filesystem_scan",
		LastAttemptAtMS: atMS, UpdatedAtMS: atMS,
	}
	info, err := os.Stat(root)
	if err != nil {
		status.FailureCode = pointerString(filesystemFailure(err))
		return status
	}
	if !info.IsDir() {
		status.FailureCode = pointerString("missing")
		return status
	}
	count := 0
	walkErr := filepath.WalkDir(root, func(path string, entry fs.DirEntry, walkError error) error {
		if err := ctx.Err(); err != nil {
			return err
		}
		if walkError != nil {
			return nil
		}
		if entry.IsDir() {
			name := entry.Name()
			if name == "session_search.sqlite" || strings.HasPrefix(name, ".") {
				return filepath.SkipDir
			}
			return nil
		}
		if entry.Name() != "summary.json" {
			return nil
		}
		if mergeSummary(path, snapshot, atMS) {
			count++
		}
		return nil
	})
	status.RowCount = int64(count)
	if walkErr != nil && !errors.Is(walkErr, context.Canceled) {
		status.FailureCode = pointerString("read_failed")
		status.State = "partial"
		status.CoverageState = "partial"
		return status
	}
	if count == 0 {
		status.State = "available"
		status.CoverageState = "unknown"
		status.LastSuccessAtMS = &atMS
		return status
	}
	status.State = "available"
	status.CoverageState = "exact"
	status.LastSuccessAtMS = &atMS
	return status
}

type grokSummaryFile struct {
	GeneratedTitle string `json:"generated_title"`
	CreatedAt      string `json:"created_at"`
	UpdatedAt      string `json:"updated_at"`
	LastActiveAt   string `json:"last_active_at"`
	CurrentModelID string `json:"current_model_id"`
	GitRootDir     string `json:"git_root_dir"`
	Info           struct {
		ID  string `json:"id"`
		CWD string `json:"cwd"`
	} `json:"info"`
}

func mergeSummary(path string, snapshot *collectedSnapshot, atMS int64) bool {
	content, err := os.ReadFile(path)
	if err != nil {
		return false
	}
	var summary grokSummaryFile
	if err := json.Unmarshal(content, &summary); err != nil {
		return false
	}
	sessionID := normalizeID(summary.Info.ID)
	if sessionID == "" {
		sessionID = normalizeID(filepath.Base(filepath.Dir(path)))
	}
	if sessionID == "" {
		return false
	}
	created := parseTimestamp(summary.CreatedAt)
	updated := parseTimestamp(summary.LastActiveAt)
	if updated == 0 {
		updated = parseTimestamp(summary.UpdatedAt)
	}
	if created == 0 {
		created = updated
	}
	if updated == 0 {
		created, updated = atMS, atMS
	}
	session := snapshot.session(sessionID, created, updated)
	if title := normalizeTitle(summary.GeneratedTitle); title != "" {
		session.DisplayTitle = title
		session.TitleSource = "grok_summary"
	}
	projectPath := strings.TrimSpace(summary.GitRootDir)
	if projectPath == "" {
		projectPath = strings.TrimSpace(summary.Info.CWD)
	}
	setSessionProject(session, projectPath)
	if model := normalizeLabel(summary.CurrentModelID); model != "" {
		session.ModelKey = &model
	}
	digest := digestBytes(content)
	snapshot.lineage[sessionID+":"+SourceSummary] = store.GrokSessionLineage{
		ExternalSessionID: sessionID, SourceKey: SourceSummary,
		LineageKey: digest, ContentDigest: digest, ObservedAtMS: atMS,
	}
	return true
}

func scanUpdates(
	ctx context.Context,
	root string,
	previous map[string]updatesFileCursor,
	snapshot *collectedSnapshot,
	atMS int64,
) (store.CursorSourceStatus, map[string]updatesFileCursor) {
	status := store.CursorSourceStatus{
		Provider: "grok", SourceKey: SourceUpdates, SourceType: "filesystem_scan",
		State: "unavailable", CoverageState: "unknown", CheckpointKind: "filesystem_scan",
		LastAttemptAtMS: atMS, UpdatedAtMS: atMS,
	}
	cursors := map[string]updatesFileCursor{}
	if _, err := os.Stat(root); err != nil {
		status.FailureCode = pointerString(filesystemFailure(err))
		return status, cursors
	}
	count := 0
	walkErr := filepath.WalkDir(root, func(path string, entry fs.DirEntry, walkError error) error {
		if err := ctx.Err(); err != nil {
			return err
		}
		if walkError != nil || entry.IsDir() || entry.Name() != "updates.jsonl" {
			return nil
		}
		accepted, cursor := mergeUpdates(path, previous[path], snapshot, atMS)
		cursors[path] = cursor
		count += accepted
		return nil
	})
	status.RowCount = int64(len(snapshot.usage) + len(snapshot.tools))
	if digest := updatesCheckpointDigest(cursors); digest != "" {
		status.CheckpointValue = &digest
	}
	if walkErr != nil && !errors.Is(walkErr, context.Canceled) {
		status.FailureCode = pointerString("read_failed")
		status.State = "partial"
		status.CoverageState = "partial"
		return status, cursors
	}
	if count == 0 && len(snapshot.usage) == 0 && len(snapshot.tools) == 0 {
		status.State = "available"
		status.CoverageState = "unknown"
		status.LastSuccessAtMS = &atMS
		return status, cursors
	}
	status.State = "available"
	status.CoverageState = "exact"
	status.LastSuccessAtMS = &atMS
	return status, cursors
}

type updatesEnvelope struct {
	Method    string          `json:"method"`
	Timestamp json.RawMessage `json:"timestamp"`
	Params    struct {
		SessionID string `json:"sessionId"`
		Update    struct {
			SessionUpdate string          `json:"sessionUpdate"`
			PromptID      string          `json:"prompt_id"`
			ToolCallID    string          `json:"toolCallId"`
			Title         string          `json:"title"`
			Kind          string          `json:"kind"`
			Status        string          `json:"status"`
			Usage         *grokUsageDTO   `json:"usage"`
			Meta          json.RawMessage `json:"_meta"`
		} `json:"update"`
	} `json:"params"`
}

type grokUsageDTO struct {
	InputTokens         int64                   `json:"inputTokens"`
	OutputTokens        int64                   `json:"outputTokens"`
	TotalTokens         int64                   `json:"totalTokens"`
	CachedReadTokens    int64                   `json:"cachedReadTokens"`
	CacheCreationTokens int64                   `json:"cacheCreationTokens"`
	ReasoningTokens     int64                   `json:"reasoningTokens"`
	CostUsdTicks        *int64                  `json:"costUsdTicks"`
	CostIsPartial       *bool                   `json:"costIsPartial"`
	NumTurns            int64                   `json:"numTurns"`
	ModelUsage          map[string]grokUsageDTO `json:"modelUsage"`
}

func mergeUpdates(
	path string,
	previous updatesFileCursor,
	snapshot *collectedSnapshot,
	atMS int64,
) (int, updatesFileCursor) {
	info, err := os.Stat(path)
	if err != nil {
		return 0, updatesFileCursor{}
	}
	device, inode, hasIdentity := fileIdentity(info)
	sameFile := hasIdentity && previous.inode != 0 && previous.device == device && previous.inode == inode
	unchanged := sameFile && previous.size == info.Size()
	if unchanged {
		replayUpdatesCursor(previous, snapshot)
		return len(previous.usage) + len(previous.tools), previous
	}
	file, err := os.Open(path)
	if err != nil {
		return 0, updatesFileCursor{}
	}
	defer file.Close()
	canAppend := sameFile && previous.offset > 0 && info.Size() > previous.size && previous.offset <= info.Size()
	if canAppend {
		if _, err := file.Seek(previous.offset, io.SeekStart); err != nil {
			canAppend = false
			if _, seekErr := file.Seek(0, io.SeekStart); seekErr != nil {
				return 0, updatesFileCursor{}
			}
		}
	}
	if canAppend {
		replayUpdatesCursor(previous, snapshot)
	}
	sessionID := normalizeID(filepath.Base(filepath.Dir(path)))
	beforeRequests, beforeTools := sessionCounts(snapshot, sessionID)
	reader := bufio.NewReaderSize(file, 64*1024)
	accepted := 0
	for {
		line, err := readLimitedLine(reader, maxUpdatesLineBytes)
		if len(line) > 0 && mergeUpdatesLine(line, sessionID, snapshot, atMS) {
			accepted++
		}
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			break
		}
	}
	offset, _ := file.Seek(0, io.SeekCurrent)
	afterRequests, afterTools := sessionCounts(snapshot, sessionID)
	cursor := updatesFileCursor{
		size: info.Size(), modNano: info.ModTime().UnixNano(), offset: offset,
		device: device, inode: inode,
		usage: fileUsageEvents(snapshot, sessionID), tools: fileToolEvents(snapshot, sessionID),
		turns:    map[string]int64{},
		newTools: map[string]int64{},
	}
	if canAppend {
		cursor.turns = cloneInt64Map(previous.turns)
		cursor.newTools = cloneInt64Map(previous.newTools)
	}
	if afterRequests > beforeRequests {
		cursor.turns[sessionID] += afterRequests - beforeRequests
	}
	if afterTools > beforeTools {
		cursor.newTools[sessionID] += afterTools - beforeTools
	}
	if canAppend {
		accepted += len(previous.usage) + len(previous.tools)
	}
	return accepted, cursor
}

func replayUpdatesCursor(cursor updatesFileCursor, snapshot *collectedSnapshot) {
	for _, event := range cursor.usage {
		snapshot.usage[event.EventID] = event
		if session := snapshot.existingSession(event.ExternalSessionID); session != nil {
			if event.OccurredAtMS > session.LastActivityAtMS {
				session.LastActivityAtMS = event.OccurredAtMS
			}
			if event.ModelKey != nil {
				session.ModelKey = event.ModelKey
			}
		}
	}
	for _, event := range cursor.tools {
		if _, exists := snapshot.tools[event.EventID]; !exists {
			snapshot.tools[event.EventID] = event
		}
	}
	for sessionID, count := range cursor.turns {
		if session := snapshot.existingSession(sessionID); session != nil {
			session.RequestCount += count
		}
	}
	for sessionID, count := range cursor.newTools {
		if session := snapshot.existingSession(sessionID); session != nil {
			session.ToolCallCount += count
		}
	}
}

func fileIdentity(info os.FileInfo) (uint64, uint64, bool) {
	stat, ok := info.Sys().(*syscall.Stat_t)
	if !ok {
		return 0, 0, false
	}
	return uint64(stat.Dev), uint64(stat.Ino), true
}

func sessionCounts(snapshot *collectedSnapshot, sessionID string) (int64, int64) {
	session := snapshot.sessions[sessionID]
	if session == nil {
		return 0, 0
	}
	return session.RequestCount, session.ToolCallCount
}

func fileUsageEvents(snapshot *collectedSnapshot, sessionID string) []store.GrokUsageEvent {
	events := make([]store.GrokUsageEvent, 0)
	for _, event := range snapshot.usage {
		if event.ExternalSessionID == sessionID {
			events = append(events, event)
		}
	}
	return events
}

func fileToolEvents(snapshot *collectedSnapshot, sessionID string) []store.GrokToolEvent {
	events := make([]store.GrokToolEvent, 0)
	for _, event := range snapshot.tools {
		if event.ExternalSessionID == sessionID {
			events = append(events, event)
		}
	}
	return events
}

func cloneInt64Map(values map[string]int64) map[string]int64 {
	cloned := make(map[string]int64, len(values))
	for key, value := range values {
		cloned[key] = value
	}
	return cloned
}

func updatesCheckpointDigest(cursors map[string]updatesFileCursor) string {
	if len(cursors) == 0 {
		return ""
	}
	paths := make([]string, 0, len(cursors))
	for path := range cursors {
		paths = append(paths, path)
	}
	sort.Strings(paths)
	builder := strings.Builder{}
	for _, path := range paths {
		cursor := cursors[path]
		builder.WriteString(filepath.Base(filepath.Dir(path)))
		builder.WriteByte(':')
		builder.WriteString(strconv.FormatInt(cursor.size, 10))
		builder.WriteByte(':')
		builder.WriteString(strconv.FormatInt(cursor.offset, 10))
		builder.WriteByte(';')
	}
	digest := digestString(builder.String())
	if len(digest) > 128 {
		return digest[:128]
	}
	return digest
}

func mergeUpdatesLine(line []byte, fallbackSession string, snapshot *collectedSnapshot, atMS int64) bool {
	var envelope updatesEnvelope
	if err := json.Unmarshal(line, &envelope); err != nil {
		return false
	}
	method := strings.TrimSpace(envelope.Method)
	if method != "_x.ai/session/update" && method != "session/update" && method != "" {
		return false
	}
	update := envelope.Params.Update
	sessionID := normalizeID(envelope.Params.SessionID)
	if sessionID == "" {
		sessionID = fallbackSession
	}
	if sessionID == "" {
		return false
	}
	occurred := parseJSONTimestamp(envelope.Timestamp)
	if occurred == 0 {
		occurred = atMS
	}
	switch update.SessionUpdate {
	case "turn_completed":
		return mergeTurnCompleted(sessionID, update.PromptID, occurred, update.Usage, snapshot, atMS)
	case "tool_call", "tool_call_update":
		return mergeToolEvent(sessionID, update.ToolCallID, update.Title, update.Kind, update.Status, update.Meta, occurred, snapshot, atMS)
	default:
		return false
	}
}

func mergeTurnCompleted(
	sessionID, promptID string,
	occurred int64,
	usage *grokUsageDTO,
	snapshot *collectedSnapshot,
	atMS int64,
) bool {
	session := snapshot.existingSession(sessionID)
	if session != nil {
		session.RequestCount++
		if lastAt := occurred; lastAt > session.LastActivityAtMS {
			session.LastActivityAtMS = lastAt
		}
		if usage == nil {
			session.CoverageState = "partial"
		}
	}
	if usage == nil {
		return session != nil
	}
	models := usage.ModelUsage
	if len(models) == 0 {
		models = map[string]grokUsageDTO{"": *usage}
	}
	parentTicksAssigned := false
	for model, item := range models {
		eventID := normalizeID(promptID)
		if eventID == "" {
			eventID = digestString(strings.Join([]string{
				sessionID, strconv.FormatInt(occurred, 10), model,
				strconv.FormatInt(item.InputTokens, 10), strconv.FormatInt(item.OutputTokens, 10),
				strconv.FormatInt(item.CachedReadTokens, 10), strconv.FormatInt(item.CacheCreationTokens, 10),
				strconv.FormatInt(item.ReasoningTokens, 10),
			}, ":"))[:32]
		} else if model != "" {
			eventID = normalizeID(eventID + ":" + model)
			if eventID == "" {
				eventID = digestString(promptID + ":" + model)[:32]
			}
		}
		modelKey := normalizeLabel(model)
		var modelPtr *string
		if modelKey != "" {
			modelPtr = &modelKey
			if session != nil {
				session.ModelKey = modelPtr
			}
		}
		total := item.TotalTokens
		if total == 0 {
			total = item.InputTokens + item.OutputTokens
		}
		event := store.GrokUsageEvent{
			EventID: eventID, ExternalSessionID: sessionID, OccurredAtMS: occurred,
			ModelKey: modelPtr, InputTokens: maxInt64(item.InputTokens, 0),
			OutputTokens: maxInt64(item.OutputTokens, 0), CachedReadTokens: maxInt64(item.CachedReadTokens, 0),
			CacheCreationTokens: maxInt64(item.CacheCreationTokens, 0),
			ReasoningTokens:     maxInt64(item.ReasoningTokens, 0),
			TotalTokens:         maxInt64(total, 0), UpdatedAtMS: atMS,
		}
		if micros, ok := reportedCostMicros(item); ok {
			event.ReportedCostMicros = &micros
		} else if !parentTicksAssigned {
			if micros, ok := reportedCostMicros(*usage); ok {
				event.ReportedCostMicros = &micros
				parentTicksAssigned = true
			}
		}
		snapshot.usage[eventID] = event
	}
	if session != nil && session.CoverageState != "partial" {
		session.CoverageState = "exact"
	}
	return true
}

func reportedCostMicros(item grokUsageDTO) (int64, bool) {
	if item.CostUsdTicks == nil || *item.CostUsdTicks < 0 || (item.CostIsPartial != nil && *item.CostIsPartial) {
		return 0, false
	}
	const ticksPerMicro int64 = 10_000
	if *item.CostUsdTicks > (1<<62)/1 && *item.CostUsdTicks/ticksPerMicro > (1<<62) {
		return 0, false
	}
	return *item.CostUsdTicks / ticksPerMicro, true
}

func mergeToolEvent(
	sessionID, toolCallID, title, kind, status string,
	meta json.RawMessage,
	occurred int64,
	snapshot *collectedSnapshot,
	atMS int64,
) bool {
	session := snapshot.existingSession(sessionID)
	if session != nil && occurred > session.LastActivityAtMS {
		session.LastActivityAtMS = occurred
	}
	name := toolNameFromMeta(meta)
	if name == "" {
		name = firstToken(title)
	}
	name = foldToolName(name)
	if name == "" {
		name = "unknown"
	}
	if kind != "" && name == "unknown" {
		if folded := foldToolName(kind); folded != "" {
			name = folded
		}
	}
	idSeed := toolCallID
	if idSeed == "" {
		idSeed = strings.Join([]string{sessionID, name, strconv.FormatInt(occurred, 10)}, ":")
	}
	eventID := digestString("grok-tool:" + sessionID + ":" + idSeed)
	existing, seen := snapshot.tools[eventID]
	outcome := normalizeToolOutcome(status)
	if seen && rankOutcome(existing.Outcome) > rankOutcome(outcome) {
		outcome = existing.Outcome
	}
	if !seen && session != nil {
		session.ToolCallCount++
	}
	snapshot.tools[eventID] = store.GrokToolEvent{
		EventID: eventID, ExternalSessionID: sessionID, OccurredAtMS: occurred,
		ToolName: name, Outcome: outcome, UpdatedAtMS: atMS,
	}
	return true
}

func (snapshot *collectedSnapshot) existingSession(externalID string) *store.GrokSession {
	return snapshot.sessions[externalID]
}

func (snapshot *collectedSnapshot) session(externalID string, createdAtMS, lastAtMS int64) *store.GrokSession {
	if existing := snapshot.sessions[externalID]; existing != nil {
		if lastAtMS > existing.LastActivityAtMS {
			existing.LastActivityAtMS = lastAtMS
		}
		if createdAtMS > 0 && (existing.CreatedAtMS == 0 || createdAtMS < existing.CreatedAtMS) {
			existing.CreatedAtMS = createdAtMS
		}
		return existing
	}
	value := &store.GrokSession{
		ExternalSessionID:  externalID,
		DisplayTitle:       fallbackSessionTitle(lastAtMS),
		TitleSource:        "fallback",
		ProjectKey:         digestString("grok:unknown-project:" + externalID),
		ProjectDisplayName: "未识别项目",
		CreatedAtMS:        maxInt64(createdAtMS, 0),
		LastActivityAtMS:   maxInt64(lastAtMS, 0),
		CoverageState:      "partial",
		UpdatedAtMS:        snapshot.CollectedAtMS,
	}
	snapshot.sessions[externalID] = value
	return value
}

func (snapshot *collectedSnapshot) finalize() {
	sessions := make([]store.GrokSession, 0, len(snapshot.sessions))
	for _, session := range snapshot.sessions {
		sessions = append(sessions, *session)
	}
	sort.Slice(sessions, func(i, j int) bool {
		if sessions[i].LastActivityAtMS != sessions[j].LastActivityAtMS {
			return sessions[i].LastActivityAtMS > sessions[j].LastActivityAtMS
		}
		return sessions[i].ExternalSessionID > sessions[j].ExternalSessionID
	})
	snapshot.Sessions = sessions
	snapshot.Lineage = valuesOf(snapshot.lineage)
	snapshot.UsageEvents = valuesOf(snapshot.usage)
	snapshot.ToolEvents = valuesOf(snapshot.tools)
}

func setSessionProject(session *store.GrokSession, path string) {
	path = strings.TrimSpace(path)
	if path == "" {
		return
	}
	session.ProjectKey = digestString("grok:" + filepath.Clean(path))
	name := filepath.Base(filepath.Clean(path))
	if name == "." || name == string(filepath.Separator) || name == "" {
		name = "未识别项目"
	}
	if label := normalizeLabel(name); label != "" {
		session.ProjectDisplayName = label
	}
}

func toolNameFromMeta(raw json.RawMessage) string {
	if len(raw) == 0 {
		return ""
	}
	var meta map[string]json.RawMessage
	if err := json.Unmarshal(raw, &meta); err != nil {
		return ""
	}
	toolRaw, ok := meta["x.ai/tool"]
	if !ok {
		return ""
	}
	var tool struct {
		Name string `json:"name"`
	}
	if err := json.Unmarshal(toolRaw, &tool); err != nil {
		var name string
		if json.Unmarshal(toolRaw, &name) == nil {
			return normalizeLabel(name)
		}
		return ""
	}
	return normalizeLabel(tool.Name)
}

func foldToolName(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return ""
	}
	if strings.Contains(value, "/") {
		value = value[strings.LastIndex(value, "/")+1:]
	}
	if strings.Contains(value, ":") {
		value = value[strings.LastIndex(value, ":")+1:]
	}
	return normalizeLabel(value)
}

func firstToken(value string) string {
	fields := strings.Fields(strings.TrimSpace(value))
	if len(fields) == 0 {
		return ""
	}
	return strings.TrimRight(fields[0], ":")
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

func rankOutcome(value string) int {
	switch value {
	case "failed":
		return 2
	case "succeeded":
		return 1
	default:
		return 0
	}
}

func notConfiguredSource(key, sourceType string, atMS int64) store.CursorSourceStatus {
	failure := "not_configured"
	return store.CursorSourceStatus{
		Provider: "grok", SourceKey: key, SourceType: sourceType, State: "not_configured",
		CoverageState: "unknown", CheckpointKind: "not_configured", RowCount: 0,
		LastAttemptAtMS: atMS, FailureCode: &failure, UpdatedAtMS: atMS,
	}
}

func fallbackSessionTitle(atMS int64) string {
	if atMS <= 0 {
		return "未命名会话"
	}
	return "未命名会话 · " + time.UnixMilli(atMS).UTC().Format("2006-01-02")
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

func parseTimestamp(value string) int64 {
	value = strings.TrimSpace(value)
	if value == "" {
		return 0
	}
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

func parseJSONTimestamp(raw json.RawMessage) int64 {
	if len(raw) == 0 {
		return 0
	}
	var number float64
	if json.Unmarshal(raw, &number) == nil {
		return parseTimestamp(strconv.FormatInt(int64(number), 10))
	}
	var text string
	if json.Unmarshal(raw, &text) == nil {
		return parseTimestamp(text)
	}
	return 0
}

func filesystemFailure(err error) string {
	if errors.Is(err, os.ErrPermission) {
		return "permission"
	}
	return "missing"
}

func digestString(value string) string { return digestBytes([]byte(value)) }

func digestBytes(value []byte) string {
	sum := sha256.Sum256(value)
	return hex.EncodeToString(sum[:])
}

func pointerString(value string) *string { return &value }

func maxInt64(left, right int64) int64 {
	if left > right {
		return left
	}
	return right
}

func readLimitedLine(reader *bufio.Reader, limit int) ([]byte, error) {
	var builder []byte
	for {
		chunk, isPrefix, err := reader.ReadLine()
		if len(chunk) > 0 {
			if len(builder)+len(chunk) > limit {
				return nil, io.ErrUnexpectedEOF
			}
			builder = append(builder, chunk...)
		}
		if err != nil {
			return builder, err
		}
		if !isPrefix {
			return builder, nil
		}
	}
}

func valuesOf[K comparable, V any](items map[K]V) []V {
	result := make([]V, 0, len(items))
	for _, value := range items {
		result = append(result, value)
	}
	return result
}
