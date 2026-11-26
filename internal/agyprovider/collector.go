package agyprovider

import (
	"bufio"
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	_ "modernc.org/sqlite"

	reportingv1 "github.com/SisyphusSQ/codex-pulse/api/codexpulse/reporting/v1"
	"github.com/SisyphusSQ/codex-pulse/internal/pricing"
	"github.com/SisyphusSQ/codex-pulse/internal/providercontrol"
	"github.com/SisyphusSQ/codex-pulse/internal/store"
	"github.com/SisyphusSQ/codex-pulse/internal/throughput"
)

const (
	maxAnnotationBytes     = 1 << 20
	maxTranscriptBytes     = 24 << 20
	maxTranscriptFullBytes = 12 << 20
	maxTranscriptLineBytes = 4 << 20
	minSpeedDurationNS     = int64(500_000_000) // 0.5 秒
	unassignedProjectName  = "未归属项目"
)

var (
	uuidPattern      = regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$`)
	safeLabelPattern = regexp.MustCompile(`^[\p{L}\p{N}_.:/ ()-]{1,128}$`)
)

// SnapshotWriter 持久化 AGY 本地采集快照。
type SnapshotWriter interface {
	ReplaceAGYSnapshot(context.Context, store.AGYSnapshot) error
}

type cachedSummaryDB struct {
	walSig  string
	rows    map[string]summaryRecord
	parents map[string]string
}

type cachedTrajectoryDB struct {
	walSig string
	parsed parsedTrajectory
}

// Collector 负责以只读 + WAL 感知方式扫描 Antigravity 4 个产品目录并原子落盘。
type Collector struct {
	writer       SnapshotWriter
	config       Config
	mu           sync.Mutex
	last         time.Time
	hasCommitted bool

	summaryCache    map[string]cachedSummaryDB
	trajectoryCache map[string]cachedTrajectoryDB
}

func NewCollector(writer SnapshotWriter, config Config) (*Collector, error) {
	if writer == nil || !filepath.IsAbs(config.BaseRoot) || config.MinimumRefresh < 0 {
		return nil, ErrCollector
	}
	if config.Now == nil {
		config.Now = time.Now
	}
	return &Collector{
		writer:          writer,
		config:          config,
		summaryCache:    make(map[string]cachedSummaryDB),
		trajectoryCache: make(map[string]cachedTrajectoryDB),
	}, nil
}

func (c *Collector) Refresh(ctx context.Context) error {
	_, err := c.RefreshIfDue(ctx)
	return err
}

func (c *Collector) RefreshIfDue(ctx context.Context) (bool, error) {
	if c == nil || c.writer == nil || ctx == nil {
		return false, ErrCollector
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	now := c.config.Now()
	if !c.last.IsZero() && now.Sub(c.last) < c.config.MinimumRefresh {
		return false, nil
	}
	snapshot, allSourcesLocked, err := c.collect(ctx, now.UnixMilli())
	if err != nil {
		return true, err
	}
	if err := ctx.Err(); err != nil {
		return true, err
	}
	if allSourcesLocked && c.hasCommitted {
		// 宿主锁定或瞬时 I/O 失败时保留上一份已提交快照，不覆写为未知或空。
		return true, fmt.Errorf("%w: all agy databases temporarily locked", ErrCollector)
	}
	if err := providercontrol.WriteWithCommit(ctx, func() error {
		return c.writer.ReplaceAGYSnapshot(ctx, snapshot)
	}); err != nil {
		return true, fmt.Errorf("%w: persist snapshot: %w", ErrCollector, err)
	}
	c.last = now
	c.hasCommitted = true
	return true, nil
}

type summaryRecord struct {
	CascadeID          string
	ProductDir         string
	AppDataDir         string
	IsNativeRoot       bool
	Title              string
	ParentID           string
	RootID             string
	ForkParentID       string
	AgentName          string
	HasSubagentSpec    bool
	SubagentType       string
	SubagentRole       string
	NestingDepth       int32
	SourceEnum         uint64
	TrajectoryType     int32
	StepCount          int64
	CreatedAtMS        int64
	LastModifiedAtMS   int64
	WorkspaceSeed      string
	WorkspaceName      string
	SideQuestionIDs    []string
	SideQuestionTitles map[string]string
}

type parsedStep struct {
	StepIndex           int64
	OccurredAtMS        int64
	StartedNS           int64
	EndedNS             int64
	ModelKey            string
	UncachedInputTokens int64
	CacheReadTokens     int64
	OutputTokens        int64
	ReasoningTokens     int64
	ReasoningKnown      bool
	Provenance          string
}

type parsedTrajectory struct {
	CascadeID       string
	ParentID        string
	RootID          string
	ForkParentID    string
	AgentScriptName string
	HasSubagentSpec bool
	SubagentType    string
	SubagentRole    string
	NestingDepth    int32
	TrajectoryType  int32
	SourceEnum      uint64
	CreatedAtMS     int64
	LastActivityMS  int64
	Steps           []parsedStep
	ContentDigest   string
	Partial         bool
}

type productCandidate struct {
	ProductDir     string
	DBPath         string
	PBPath         string
	TranscriptPath string
	AnnotationPath string
}

func (c *Collector) collect(ctx context.Context, atMS int64) (store.AGYSnapshot, bool, error) {
	snapshot := store.AGYSnapshot{
		Generation:    atMS,
		CollectedAtMS: atMS,
	}
	summaryStatus := store.CursorSourceStatus{
		Provider: "agy", SourceKey: SourceSummaries, SourceType: "sqlite",
		State: "unavailable", CoverageState: "unknown", CheckpointKind: "snapshot",
		LastAttemptAtMS: atMS, UpdatedAtMS: atMS,
	}
	trajStatus := store.CursorSourceStatus{
		Provider: "agy", SourceKey: SourceTrajectories, SourceType: "sqlite",
		State: "unavailable", CoverageState: "unknown", CheckpointKind: "snapshot",
		LastAttemptAtMS: atMS, UpdatedAtMS: atMS,
	}
	transcriptStatus := store.CursorSourceStatus{
		Provider: "agy", SourceKey: SourceTranscripts, SourceType: "jsonl",
		State: "not_configured", CoverageState: "unknown", CheckpointKind: "filesystem_scan",
		LastAttemptAtMS: atMS, UpdatedAtMS: atMS,
	}

	summaries := make(map[string]summaryRecord)
	sideQuestionParents := make(map[string]string)
	sideQuestionTitles := make(map[string]string)
	candidates := make(map[string][]productCandidate)

	var (
		summaryDBFound      int
		summaryDBSucceeded  int
		summaryDBFailed     int
		trajDBFound         int
		trajDBSucceeded     int
		trajDBFailed        int
		transcriptUsedCount int
		encryptedPBOnly     int
	)

	for _, product := range ProductDirectories {
		if err := ctx.Err(); err != nil {
			return store.AGYSnapshot{}, false, err
		}
		productRoot := filepath.Join(c.config.BaseRoot, product)
		info, err := os.Lstat(productRoot)
		if err != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
			continue
		}

		// 1. 扫描 conversation_summaries.db
		sumDBPath := filepath.Join(productRoot, "conversation_summaries.db")
		if sig, exists := sqliteWalSignature(sumDBPath); exists {
			summaryDBFound++
			recs, parents, readErr := c.readSummaryDB(ctx, sumDBPath, product, sig)
			if readErr != nil {
				summaryDBFailed++
				if cached, ok := c.summaryCache[sumDBPath]; ok {
					recs, parents = cached.rows, cached.parents
				}
			} else {
				summaryDBSucceeded++
			}
			for cid, rec := range recs {
				prev, ok := summaries[cid]
				if !ok || (rec.IsNativeRoot && !prev.IsNativeRoot) ||
					(rec.IsNativeRoot == prev.IsNativeRoot && rec.LastModifiedAtMS >= prev.LastModifiedAtMS) {
					summaries[cid] = rec
				}
				if rec.TrajectoryType == protoTrajectoryTypeSQ && rec.ForkParentID != "" {
					if sideQuestionParents[cid] == "" {
						sideQuestionParents[cid] = rec.ForkParentID
					}
				}
				for _, sqID := range rec.SideQuestionIDs {
					sideQuestionParents[sqID] = cid
				}
				for sqID, sqTitle := range rec.SideQuestionTitles {
					if sideQuestionParents[sqID] == "" {
						sideQuestionParents[sqID] = cid
					}
					if sqTitle != "" && sideQuestionTitles[sqID] == "" {
						sideQuestionTitles[sqID] = sqTitle
					}
				}
			}
			for sqID, parentID := range parents {
				if sideQuestionParents[sqID] == "" {
					sideQuestionParents[sqID] = parentID
				}
			}
		}

		// 2. 扫描 conversations/*.db 与 *.pb
		byProduct := make(map[string]*productCandidate)
		ensureCand := func(cid string) *productCandidate {
			if existing := byProduct[cid]; existing != nil {
				return existing
			}
			cand := &productCandidate{ProductDir: product}
			byProduct[cid] = cand
			return cand
		}

		convDir := filepath.Join(productRoot, "conversations")
		if entries, err := readDirSafe(convDir); err == nil {
			for _, entry := range entries {
				if entry.IsDir() || entry.Type()&os.ModeSymlink != 0 {
					continue
				}
				name := strings.ToLower(entry.Name())
				switch {
				case strings.HasSuffix(name, ".db"):
					cid := strings.TrimSuffix(name, ".db")
					if uuidPattern.MatchString(cid) {
						ensureCand(cid).DBPath = filepath.Join(convDir, entry.Name())
					}
				case strings.HasSuffix(name, ".pb"):
					cid := strings.TrimSuffix(name, ".pb")
					if uuidPattern.MatchString(cid) {
						ensureCand(cid).PBPath = filepath.Join(convDir, entry.Name())
					}
				}
			}
		}

		// 3. 扫描 brain/<uuid>/.system_generated/logs/transcript*.jsonl
		brainDir := filepath.Join(productRoot, "brain")
		if entries, err := readDirSafe(brainDir); err == nil {
			for _, entry := range entries {
				if !entry.IsDir() || entry.Type()&os.ModeSymlink != 0 {
					continue
				}
				cid := strings.ToLower(strings.TrimSpace(entry.Name()))
				if !uuidPattern.MatchString(cid) {
					continue
				}
				logsDir := filepath.Join(brainDir, entry.Name(), ".system_generated", "logs")
				if tp := resolveTranscriptFile(logsDir); tp != "" {
					ensureCand(cid).TranscriptPath = tp
				}
			}
		}

		// 4. 扫描 annotations/<uuid>.pbtxt（仅标题）
		annDir := filepath.Join(productRoot, "annotations")
		if entries, err := readDirSafe(annDir); err == nil {
			for _, entry := range entries {
				if entry.IsDir() || entry.Type()&os.ModeSymlink != 0 {
					continue
				}
				name := strings.ToLower(entry.Name())
				if !strings.HasSuffix(name, ".pbtxt") {
					continue
				}
				cid := strings.TrimSuffix(name, ".pbtxt")
				if !uuidPattern.MatchString(cid) {
					continue
				}
				if cand := byProduct[cid]; cand != nil {
					cand.AnnotationPath = filepath.Join(annDir, entry.Name())
				} else if _, hasSum := summaries[cid]; hasSum {
					ensureCand(cid).AnnotationPath = filepath.Join(annDir, entry.Name())
				}
			}
		}

		for cid, cand := range byProduct {
			candidates[cid] = append(candidates[cid], *cand)
		}
	}

	// 汇总全部会话 ID
	allIDs := make(map[string]struct{}, len(summaries)+len(candidates))
	for cid := range summaries {
		allIDs[cid] = struct{}{}
	}
	for cid := range candidates {
		allIDs[cid] = struct{}{}
	}
	sortedIDs := make([]string, 0, len(allIDs))
	for cid := range allIDs {
		sortedIDs = append(sortedIDs, cid)
	}
	sort.Strings(sortedIDs)

	type assembledSession struct {
		session       store.AGYSession
		usage         []store.AGYUsageEvent
		lineage       []store.AGYSessionLineage
		workspaceSeed string
		workspaceName string
	}

	assembled := make(map[string]*assembledSession, len(sortedIDs))
	for _, cid := range sortedIDs {
		if err := ctx.Err(); err != nil {
			return store.AGYSnapshot{}, false, err
		}
		sum, hasSum := summaries[cid]
		cands := candidates[cid]

		var (
			bestTraj        *parsedTrajectory
			bestSourceKey   string
			lineageEntries  []store.AGYSessionLineage
			lineageConflict bool
			annotationTitle string
			hasPBOnly       bool
			anyDBError      bool
		)

		for _, cand := range cands {
			if annotationTitle == "" && cand.AnnotationPath != "" {
				annotationTitle = readAnnotationFileTitle(cand.AnnotationPath)
			}
			if cand.DBPath != "" {
				trajDBFound++
				sig, exists := sqliteWalSignature(cand.DBPath)
				if !exists {
					continue
				}
				parsed, readErr := c.readTrajectoryDB(ctx, cand.DBPath, cid, sig)
				if readErr != nil {
					trajDBFailed++
					anyDBError = true
					if cached, ok := c.trajectoryCache[cand.DBPath]; ok {
						parsed = cached.parsed
					} else {
						continue
					}
				} else {
					trajDBSucceeded++
				}
				lineageEntries = append(lineageEntries, store.AGYSessionLineage{
					ExternalSessionID: cid,
					SourceKey:         SourceTrajectories,
					LineageKey:        digestString("agy:traj:" + cand.ProductDir + ":" + cid),
					ContentDigest:     parsed.ContentDigest,
					ObservedAtMS:      atMS,
				})
				if bestTraj == nil {
					cp := parsed
					bestTraj = &cp
					bestSourceKey = SourceTrajectories
				} else {
					if parsed.ContentDigest != bestTraj.ContentDigest &&
						len(parsed.Steps) > 0 && len(bestTraj.Steps) > 0 {
						lineageConflict = true
					}
					if len(parsed.Steps) > len(bestTraj.Steps) ||
						(len(parsed.Steps) == len(bestTraj.Steps) && parsed.LastActivityMS > bestTraj.LastActivityMS) {
						cp := parsed
						bestTraj = &cp
						bestSourceKey = SourceTrajectories
					}
				}
			}
		}

		// 若 .db 不存在或未产出用量事件，尝试有界 transcript.jsonl 回退
		if bestTraj == nil || len(bestTraj.Steps) == 0 {
			for _, cand := range cands {
				if cand.TranscriptPath == "" {
					continue
				}
				parsed, ok := parseTranscriptFallback(cand.TranscriptPath, cid)
				if !ok {
					continue
				}
				transcriptUsedCount++
				lineageEntries = append(lineageEntries, store.AGYSessionLineage{
					ExternalSessionID: cid,
					SourceKey:         SourceTranscripts,
					LineageKey:        digestString("agy:transcript:" + cand.ProductDir + ":" + cid),
					ContentDigest:     parsed.ContentDigest,
					ObservedAtMS:      atMS,
				})
				if bestTraj == nil || len(parsed.Steps) > len(bestTraj.Steps) {
					cp := parsed
					bestTraj = &cp
					bestSourceKey = SourceTranscripts
				}
				break
			}
		}

		// 检查是否仅存在加密 .pb 文件
		if bestTraj == nil {
			for _, cand := range cands {
				if cand.PBPath != "" {
					hasPBOnly = true
					encryptedPBOnly++
					lineageEntries = append(lineageEntries, store.AGYSessionLineage{
						ExternalSessionID: cid,
						SourceKey:         SourceTrajectories,
						LineageKey:        digestString("agy:pb:" + cand.ProductDir + ":" + cid),
						ContentDigest:     digestString("encrypted-pb:" + cid),
						ObservedAtMS:      atMS,
					})
					break
				}
			}
		}

		if hasSum && len(lineageEntries) == 0 {
			lineageEntries = append(lineageEntries, store.AGYSessionLineage{
				ExternalSessionID: cid,
				SourceKey:         SourceSummaries,
				LineageKey:        digestString("agy:summary:" + sum.ProductDir + ":" + cid),
				ContentDigest:     digestString("summary:" + cid + ":" + strconv.FormatInt(sum.LastModifiedAtMS, 10)),
				ObservedAtMS:      atMS,
			})
		}

		// 标题优先级：annotations/*.pbtxt > conversation_summaries / raw_summary > sideQuestionTitle > fallback
		title, titleSource := "未命名会话", "fallback"
		if annotationTitle != "" {
			title, titleSource = annotationTitle, "agy_annotation"
		} else if hasSum && sum.Title != "" {
			title, titleSource = sum.Title, "agy_summary"
		} else if sqTitle := sanitizeTitle(sideQuestionTitles[cid]); sqTitle != "" {
			title, titleSource = sqTitle, "agy_summary"
		}

		// 父子关系与会话角色
		parentID := firstNonEmpty(
			sideQuestionParents[cid],
			sum.ParentID,
			sum.ForkParentID,
		)
		if bestTraj != nil && parentID == "" {
			parentID = firstNonEmpty(bestTraj.ParentID, bestTraj.ForkParentID)
		}
		if parentID == cid {
			parentID = ""
		}

		role := classifySessionRole(cid, sum, bestTraj, sideQuestionParents, parentID)
		var parentPtr *string
		if parentID != "" {
			p := parentID
			parentPtr = &p
		}

		// 时间边界
		createdAt := positiveMin(sum.CreatedAtMS, trajectoryCreated(bestTraj))
		lastActivity := maxInt64(sum.LastModifiedAtMS, trajectoryLastActivity(bestTraj))
		if createdAt == 0 && lastActivity > 0 {
			createdAt = lastActivity
		}
		if lastActivity < createdAt {
			lastActivity = createdAt
		}

		// 构造 UsageEvents 与 TPS 吞吐胶囊
		var (
			usageEvents []store.AGYUsageEvent
			lastModel   *string
			capsule     *reportingv1.ThroughputCapsule
		)
		if bestTraj != nil && len(bestTraj.Steps) > 0 {
			tpAcc := throughput.NewAccumulator()
			usageEvents = make([]store.AGYUsageEvent, 0, len(bestTraj.Steps))
			for _, step := range bestTraj.Steps {
				var modelPtr *string
				if step.ModelKey != "" {
					m := sanitizeLabel(step.ModelKey, "")
					if m != "" {
						modelPtr = &m
						lastModel = &m
					}
				}
				var startedPtr, endedPtr *int64
				if step.StartedNS > 0 {
					ms := step.StartedNS / 1_000_000
					startedPtr = &ms
					if step.EndedNS >= step.StartedNS {
						ems := step.EndedNS / 1_000_000
						endedPtr = &ems
					}
				}
				totalTokens := step.UncachedInputTokens + step.CacheReadTokens + step.OutputTokens
				ev := store.AGYUsageEvent{
					EventID:             digestString("agy:" + cid + ":" + strconv.FormatInt(step.StepIndex, 10)),
					ExternalSessionID:   cid,
					StepIndex:           step.StepIndex,
					OccurredAtMS:        step.OccurredAtMS,
					ModelKey:            modelPtr,
					UncachedInputTokens: step.UncachedInputTokens,
					CacheReadTokens:     step.CacheReadTokens,
					OutputTokens:        step.OutputTokens,
					ReasoningTokens:     step.ReasoningTokens,
					TotalTokens:         totalTokens,
					CacheReadKnown:      true,
					ReasoningKnown:      step.ReasoningKnown,
					TotalKnown:          true,
					StartedAtMS:         startedPtr,
					EndedAtMS:           endedPtr,
					Provenance:          step.Provenance,
					UpdatedAtMS:         atMS,
				}
				usageEvents = append(usageEvents, ev)
				if step.OccurredAtMS > 0 {
					if createdAt == 0 || step.OccurredAtMS < createdAt {
						createdAt = step.OccurredAtMS
					}
					if step.OccurredAtMS > lastActivity {
						lastActivity = step.OccurredAtMS
					}
				}
				// 精确纳秒差 >= 0.5s 才纳入有效测速（过滤极短步时钟抖动）
				if step.OutputTokens > 0 && step.StartedNS > 0 && step.EndedNS > step.StartedNS &&
					(step.EndedNS-step.StartedNS) >= minSpeedDurationNS {
					turnID := strconv.FormatInt(step.StepIndex, 10)
					sMS := step.StartedNS / 1_000_000
					eMS := step.EndedNS / 1_000_000
					outTok := step.OutputTokens
					tpAcc.Add(throughput.Event{Kind: "start", TurnID: &turnID, AtMS: &sMS, TimeSource: "step_metadata"})
					tpAcc.Add(throughput.Event{Kind: "usage", TurnID: &turnID, AtMS: &eMS, OutputDelta: &outTok, OutputObserved: true})
					tpAcc.Add(throughput.Event{Kind: "complete", TurnID: &turnID, AtMS: &eMS, TimeSource: "step_metadata"})
				}
			}
			stats, turns := tpAcc.Result()
			if stats.IncludedTurns > 0 {
				c := &reportingv1.ThroughputCapsule{
					Version:     1,
					Basis:       "closed_turn_lifetime_output",
					Measures:    throughputMeasures(stats),
					TurnsTotal:  int64(len(turns)),
					RecentTurns: []reportingv1.ThroughputTurn{},
				}
				for _, turn := range turns[:min(len(turns), reportingv1.MaxThroughputTurns)] {
					c.RecentTurns = append(c.RecentTurns, reportingv1.ThroughputTurn{
						Key:         digestString("agy:" + cid + ":turn:" + turn.ID),
						StartedAtMS: turn.StartedAtMS,
						EndedAtMS:   turn.EndedAtMS,
						Measures:    throughputMeasures(turn.Stats),
					})
				}
				capsule = c
			}
		}

		coverage := "exact"
		switch {
		case lineageConflict || anyDBError || hasPBOnly:
			coverage = "partial"
		case bestTraj != nil && bestTraj.Partial:
			coverage = "partial"
		case bestSourceKey == SourceTranscripts:
			coverage = "partial"
		case bestTraj == nil && hasSum && sum.StepCount > 0:
			coverage = "partial"
		}

		sess := store.AGYSession{
			ExternalSessionID:       cid,
			DisplayTitle:            title,
			TitleSource:             titleSource,
			ParentExternalSessionID: parentPtr,
			SessionRole:             role,
			ProjectKey:              digestString("agy:unassigned"),
			ProjectDisplayName:      unassignedProjectName,
			ProjectSource:           "unassigned",
			CreatedAtMS:             createdAt,
			LastActivityAtMS:        lastActivity,
			ModelKey:                lastModel,
			RequestCount:            int64(len(usageEvents)),
			LineageConflict:         lineageConflict,
			CoverageState:           coverage,
			Throughput:              capsule,
			UpdatedAtMS:             atMS,
		}
		if sum.WorkspaceSeed != "" {
			sess.ProjectKey = digestString("agy:" + sum.WorkspaceSeed)
			sess.ProjectDisplayName = sum.WorkspaceName
			sess.ProjectSource = "workspace"
		}

		assembled[cid] = &assembledSession{
			session:       sess,
			usage:         usageEvents,
			lineage:       lineageEntries,
			workspaceSeed: sum.WorkspaceSeed,
			workspaceName: sum.WorkspaceName,
		}
	}

	// 二次遍历：同库可解析父会话时，子会话继承父会话项目归属（带环路保护）
	for _, cid := range sortedIDs {
		item := assembled[cid]
		if item.session.ProjectSource == "workspace" {
			continue
		}
		visited := map[string]struct{}{cid: {}}
		curr := item.session.ParentExternalSessionID
		for curr != nil && *curr != "" {
			pid := *curr
			if _, seen := visited[pid]; seen {
				break
			}
			visited[pid] = struct{}{}
			parent, ok := assembled[pid]
			if !ok {
				break
			}
			if parent.workspaceSeed != "" {
				item.session.ProjectKey = digestString("agy:" + parent.workspaceSeed)
				item.session.ProjectDisplayName = parent.workspaceName
				item.session.ProjectSource = "inherited_parent"
				break
			}
			curr = parent.session.ParentExternalSessionID
		}
	}

	// 组装最终批次
	for _, cid := range sortedIDs {
		item := assembled[cid]
		snapshot.Sessions = append(snapshot.Sessions, item.session)
		snapshot.UsageEvents = append(snapshot.UsageEvents, item.usage...)
		snapshot.Lineage = append(snapshot.Lineage, item.lineage...)
	}

	// 更新各来源状态
	if summaryDBFound == 0 {
		code := "not_configured"
		summaryStatus.State = "not_configured"
		summaryStatus.FailureCode = &code
	} else if summaryDBSucceeded > 0 && summaryDBFailed == 0 {
		summaryStatus.State = "available"
		summaryStatus.CoverageState = "exact"
		summaryStatus.LastSuccessAtMS = &atMS
		summaryStatus.RowCount = int64(len(summaries))
	} else if summaryDBSucceeded > 0 {
		code := "read_failed"
		summaryStatus.State = "partial"
		summaryStatus.CoverageState = "partial"
		summaryStatus.LastSuccessAtMS = &atMS
		summaryStatus.FailureCode = &code
		summaryStatus.RowCount = int64(len(summaries))
	} else {
		code := "read_failed"
		summaryStatus.FailureCode = &code
	}

	if trajDBFound == 0 && encryptedPBOnly == 0 {
		code := "not_configured"
		trajStatus.State = "not_configured"
		trajStatus.FailureCode = &code
	} else if trajDBSucceeded > 0 && trajDBFailed == 0 && encryptedPBOnly == 0 {
		trajStatus.State = "available"
		trajStatus.CoverageState = "exact"
		trajStatus.LastSuccessAtMS = &atMS
		trajStatus.RowCount = int64(len(snapshot.UsageEvents))
	} else if trajDBSucceeded > 0 || encryptedPBOnly > 0 {
		trajStatus.State = "partial"
		trajStatus.CoverageState = "partial"
		if trajDBSucceeded > 0 {
			trajStatus.LastSuccessAtMS = &atMS
		}
		if trajDBFailed > 0 {
			code := "read_failed"
			trajStatus.FailureCode = &code
		}
		trajStatus.RowCount = int64(len(snapshot.UsageEvents))
	} else {
		code := "read_failed"
		trajStatus.FailureCode = &code
	}

	if transcriptUsedCount > 0 {
		transcriptStatus.State = "available"
		transcriptStatus.CoverageState = "partial"
		transcriptStatus.LastSuccessAtMS = &atMS
		transcriptStatus.RowCount = int64(transcriptUsedCount)
	} else {
		code := "not_configured"
		transcriptStatus.FailureCode = &code
	}

	snapshot.Sources = []store.CursorSourceStatus{summaryStatus, trajStatus, transcriptStatus}

	allLocked := (summaryDBFound > 0 || trajDBFound > 0) &&
		summaryDBSucceeded == 0 && trajDBSucceeded == 0 && transcriptUsedCount == 0 &&
		(summaryDBFailed > 0 || trajDBFailed > 0)

	return snapshot, allLocked, nil
}

func (c *Collector) readSummaryDB(
	ctx context.Context,
	dbPath string,
	productDir string,
	walSig string,
) (map[string]summaryRecord, map[string]string, error) {
	if cached, ok := c.summaryCache[dbPath]; ok && cached.walSig == walSig {
		return cached.rows, cached.parents, nil
	}
	db, tx, err := openReadOnlySQLite(ctx, dbPath)
	if err != nil {
		return nil, nil, err
	}
	defer func() {
		_ = tx.Rollback()
		_ = db.Close()
	}()

	cols := tableColumns(ctx, tx, "conversation_summaries")
	if !cols["conversation_id"] {
		return nil, nil, ErrCollector
	}

	// 严格白名单列投影：绝不 SELECT preview 或 conversations_fts！
	selectCols := []string{"conversation_id"}
	optCols := []string{
		"title", "parent_conversation_id", "nesting_depth", "agent_name",
		"step_count", "last_modified_time", "app_data_dir", "workspace_uris", "raw_summary",
	}
	present := make(map[string]int, len(optCols))
	for _, col := range optCols {
		if cols[col] {
			present[col] = len(selectCols)
			selectCols = append(selectCols, col)
		}
	}

	rows, err := tx.QueryContext(ctx, "SELECT "+strings.Join(selectCols, ", ")+" FROM conversation_summaries")
	if err != nil {
		return nil, nil, err
	}
	defer rows.Close()

	records := make(map[string]summaryRecord)
	for rows.Next() {
		vals := make([]any, len(selectCols))
		ptrs := make([]any, len(selectCols))
		for i := range vals {
			ptrs[i] = &vals[i]
		}
		if err := rows.Scan(ptrs...); err != nil {
			continue
		}
		cid := strings.ToLower(strings.TrimSpace(asString(vals[0])))
		if cid == "" {
			continue
		}
		rec := summaryRecord{
			CascadeID:  cid,
			ProductDir: productDir,
		}
		if idx, ok := present["title"]; ok {
			rec.Title = sanitizeTitle(asString(vals[idx]))
		}
		if idx, ok := present["parent_conversation_id"]; ok {
			rec.ParentID = strings.ToLower(strings.TrimSpace(asString(vals[idx])))
		}
		if idx, ok := present["nesting_depth"]; ok {
			rec.NestingDepth = int32(asInt64(vals[idx]))
		}
		if idx, ok := present["agent_name"]; ok {
			rec.AgentName = strings.TrimSpace(asString(vals[idx]))
		}
		if idx, ok := present["step_count"]; ok {
			rec.StepCount = maxInt64(0, asInt64(vals[idx]))
		}
		if idx, ok := present["last_modified_time"]; ok {
			rec.LastModifiedAtMS = parseFlexibleTimeMS(vals[idx])
		}
		if idx, ok := present["app_data_dir"]; ok {
			rec.AppDataDir = strings.ToLower(strings.TrimSpace(asString(vals[idx])))
			rec.IsNativeRoot = rec.AppDataDir != "" && rec.AppDataDir == strings.ToLower(productDir)
		}
		if idx, ok := present["workspace_uris"]; ok {
			rec.WorkspaceSeed, rec.WorkspaceName = parseWorkspaceBasename(asString(vals[idx]))
		}
		if idx, ok := present["raw_summary"]; ok {
			if blob := asBytes(vals[idx]); len(blob) > 0 {
				decoded := decodeCascadeSummary(blob)
				if decoded.AnnotationTitle != "" {
					rec.Title = sanitizeTitle(decoded.AnnotationTitle)
				}
				if decoded.StepCount > 0 {
					rec.StepCount = decoded.StepCount
				}
				if decoded.CreatedNS > 0 {
					rec.CreatedAtMS = decoded.CreatedNS / 1_000_000
				} else if decoded.TrajectoryMeta.CreatedNS > 0 {
					rec.CreatedAtMS = decoded.TrajectoryMeta.CreatedNS / 1_000_000
				}
				if decoded.LastModifiedNS > 0 {
					rec.LastModifiedAtMS = decoded.LastModifiedNS / 1_000_000
				}
				if decoded.TrajectoryMeta.ParentID != "" {
					rec.ParentID = decoded.TrajectoryMeta.ParentID
				}
				if decoded.TrajectoryMeta.RootID != "" {
					rec.RootID = decoded.TrajectoryMeta.RootID
				}
				if decoded.ForkParentID != "" {
					rec.ForkParentID = decoded.ForkParentID
				}
				if decoded.TrajectoryMeta.AgentScriptName != "" {
					rec.AgentName = decoded.TrajectoryMeta.AgentScriptName
				}
				if decoded.TrajectoryMeta.HasSubagentSpec {
					rec.HasSubagentSpec = true
					rec.SubagentType = decoded.TrajectoryMeta.SubagentType
					rec.SubagentRole = decoded.TrajectoryMeta.SubagentRole
				}
				if decoded.TrajectoryMeta.NestingDepth > 0 {
					rec.NestingDepth = decoded.TrajectoryMeta.NestingDepth
				}
				rec.SourceEnum = decoded.SourceEnum
				rec.TrajectoryType = decoded.TrajectoryType
				rec.SideQuestionIDs = decoded.SideQuestionIDs
				rec.SideQuestionTitles = decoded.SideQuestionTitles
			}
		}
		records[cid] = rec
	}

	parents := make(map[string]string)
	if pCols := tableColumns(ctx, tx, "conversation_parents"); pCols["conversation_id"] && pCols["parent_id"] {
		if pRows, err := tx.QueryContext(ctx, "SELECT conversation_id, parent_id FROM conversation_parents"); err == nil {
			defer pRows.Close()
			for pRows.Next() {
				var cID, pID sql.NullString
				if pRows.Scan(&cID, &pID) == nil {
					lc := strings.ToLower(strings.TrimSpace(cID.String))
					lp := strings.ToLower(strings.TrimSpace(pID.String))
					if lc != "" && lp != "" {
						parents[lc] = lp
					}
				}
			}
		}
	}

	c.summaryCache[dbPath] = cachedSummaryDB{walSig: walSig, rows: records, parents: parents}
	return records, parents, nil
}

func (c *Collector) readTrajectoryDB(
	ctx context.Context,
	dbPath string,
	cascadeID string,
	walSig string,
) (parsedTrajectory, error) {
	if cached, ok := c.trajectoryCache[dbPath]; ok && cached.walSig == walSig {
		return cached.parsed, nil
	}
	db, tx, err := openReadOnlySQLite(ctx, dbPath)
	if err != nil {
		return parsedTrajectory{}, err
	}
	defer func() {
		_ = tx.Rollback()
		_ = db.Close()
	}()

	out := parsedTrajectory{CascadeID: cascadeID}

	if cols := tableColumns(ctx, tx, "trajectory_meta"); cols["trajectory_type"] {
		var tType sql.NullInt64
		var src sql.NullInt64
		query := "SELECT trajectory_type, 0 FROM trajectory_meta LIMIT 1"
		if cols["source"] {
			query = "SELECT trajectory_type, source FROM trajectory_meta LIMIT 1"
		}
		if tx.QueryRowContext(ctx, query).Scan(&tType, &src) == nil {
			out.TrajectoryType = int32(tType.Int64)
			if src.Int64 > 0 {
				out.SourceEnum = uint64(src.Int64)
			}
		}
	}

	if cols := tableColumns(ctx, tx, "trajectory_metadata_blob"); cols["data"] {
		var blob []byte
		if tx.QueryRowContext(ctx, "SELECT data FROM trajectory_metadata_blob WHERE id = 'main' LIMIT 1").Scan(&blob) == nil && len(blob) > 0 {
			tm := decodeTrajectoryMetadata(blob)
			out.CreatedAtMS = tm.CreatedNS / 1_000_000
			out.ParentID = tm.ParentID
			out.RootID = tm.RootID
			out.NestingDepth = tm.NestingDepth
			out.HasSubagentSpec = tm.HasSubagentSpec
			out.SubagentType = tm.SubagentType
			out.SubagentRole = tm.SubagentRole
			out.AgentScriptName = tm.AgentScriptName
		}
	}

	if cols := tableColumns(ctx, tx, "parent_references"); cols["data"] {
		if rows, err := tx.QueryContext(ctx, "SELECT data FROM parent_references ORDER BY idx ASC"); err == nil {
			defer rows.Close()
			for rows.Next() {
				var blob []byte
				if rows.Scan(&blob) == nil && len(blob) > 0 {
					if ref := decodeParentReferenceCascadeID(blob); ref != "" && ref != cascadeID {
						out.ForkParentID = ref
					}
				}
			}
		}
	}

	// 读取 gen_metadata（按 origin cascade_id 过滤克隆记录）
	genByStep := make(map[int64]protoGenMeta)
	if cols := tableColumns(ctx, tx, "gen_metadata"); cols["data"] {
		if rows, err := tx.QueryContext(ctx, "SELECT data FROM gen_metadata ORDER BY idx ASC"); err == nil {
			defer rows.Close()
			for rows.Next() {
				var blob []byte
				if rows.Scan(&blob) != nil || len(blob) == 0 {
					continue
				}
				gm := decodeGeneratorMetadata(blob)
				if gm.CascadeID != "" && gm.CascadeID != cascadeID {
					continue
				}
				if out.ParentID == "" && gm.ParentCascadeID != "" {
					out.ParentID = gm.ParentCascadeID
				}
				if out.RootID == "" && gm.RootCascadeID != "" {
					out.RootID = gm.RootCascadeID
				}
				for _, idx := range gm.StepIndices {
					genByStep[idx] = gm
				}
			}
		}
	}

	// 严格白名单读取 steps(idx, step_type, status, metadata) —— 绝不读 step_payload！
	stepCols := tableColumns(ctx, tx, "steps")
	if !stepCols["idx"] || !stepCols["metadata"] {
		return out, ErrCollector
	}
	statusExpr := "0"
	if stepCols["status"] {
		statusExpr = "status"
	}
	stepTypeExpr := "0"
	if stepCols["step_type"] {
		stepTypeExpr = "step_type"
	}
	rows, err := tx.QueryContext(ctx, fmt.Sprintf("SELECT idx, %s, %s, metadata FROM steps ORDER BY idx ASC", stepTypeExpr, statusExpr))
	if err != nil {
		return out, err
	}
	defer rows.Close()

	hasher := sha256.New()
	seenStepIdx := make(map[int64]struct{})
	for rows.Next() {
		var idx, stepType, status int64
		var metaBlob []byte
		if err := rows.Scan(&idx, &stepType, &status, &metaBlob); err != nil {
			continue
		}
		meta := decodeStepMetadata(metaBlob, idx)
		if meta.OriginCascadeID != "" && meta.OriginCascadeID != cascadeID {
			// 过滤分叉/旁路提问克隆自父会话的步骤，避免重复计费
			continue
		}
		stepIdx := meta.StepIndex
		if _, dup := seenStepIdx[stepIdx]; dup {
			continue
		}
		seenStepIdx[stepIdx] = struct{}{}

		gm, hasGM := genByStep[stepIdx]
		var usage *protoModelUsage
		if meta.ModelUsage != nil && meta.ModelUsage.hasAnyTokens() {
			usage = meta.ModelUsage
		} else if hasGM && gm.Usage != nil && gm.Usage.hasAnyTokens() {
			usage = gm.Usage
		}
		if usage == nil {
			continue
		}

		rawModel := firstNonEmpty(usage.Model, meta.GeneratorModel, gm.Model)
		canonicalModel, _ := pricing.ResolveAGYModelID(rawModel, gm.ResponseModel)

		startedNS := meta.CreatedNS
		if startedNS == 0 {
			startedNS = meta.StartedNS
		}
		endedNS := meta.FinishedGeneratingNS
		if endedNS == 0 && status != protoStepStatusCleared {
			endedNS = meta.CompletedNS
		}
		occurredMS := endedNS / 1_000_000
		if occurredMS == 0 {
			occurredMS = startedNS / 1_000_000
		}

		if occurredMS > 0 {
			if out.CreatedAtMS == 0 || occurredMS < out.CreatedAtMS {
				out.CreatedAtMS = occurredMS
			}
			if occurredMS > out.LastActivityMS {
				out.LastActivityMS = occurredMS
			}
		}

		thinking := usage.ThinkingOutputTokens
		reasoningKnown := usage.HasThinking || usage.HasResponse || usage.OutputTokens > 0
		if thinking > usage.OutputTokens && usage.OutputTokens > 0 {
			thinking = usage.OutputTokens
		}

		step := parsedStep{
			StepIndex:           stepIdx,
			OccurredAtMS:        occurredMS,
			StartedNS:           startedNS,
			EndedNS:             endedNS,
			ModelKey:            canonicalModel,
			UncachedInputTokens: usage.InputTokens,
			CacheReadTokens:     usage.CacheReadTokens,
			OutputTokens:        usage.OutputTokens,
			ReasoningTokens:     thinking,
			ReasoningKnown:      reasoningKnown,
			Provenance:          "agy_step_metadata",
		}
		out.Steps = append(out.Steps, step)
		_, _ = fmt.Fprintf(hasher, "%d:%d:%s:%d:%d:%d:%d\n",
			step.StepIndex, step.OccurredAtMS, step.ModelKey,
			step.UncachedInputTokens, step.CacheReadTokens, step.OutputTokens, step.ReasoningTokens,
		)
	}
	out.ContentDigest = hex.EncodeToString(hasher.Sum(nil))
	c.trajectoryCache[dbPath] = cachedTrajectoryDB{walSig: walSig, parsed: out}
	return out, nil
}

func resolveTranscriptFile(logsDir string) string {
	primary := filepath.Join(logsDir, "transcript.jsonl")
	if info, err := os.Lstat(primary); err == nil && info.Mode().IsRegular() && info.Size() <= maxTranscriptBytes {
		return primary
	}
	full := filepath.Join(logsDir, "transcript_full.jsonl")
	if info, err := os.Lstat(full); err == nil && info.Mode().IsRegular() && info.Size() <= maxTranscriptFullBytes {
		return full
	}
	return ""
}

func parseTranscriptFallback(path string, cascadeID string) (parsedTrajectory, bool) {
	file, info, err := openRegularFileReadOnly(path)
	if err != nil {
		return parsedTrajectory{}, false
	}
	defer file.Close()
	limit := int64(maxTranscriptBytes)
	if strings.HasSuffix(strings.ToLower(path), "transcript_full.jsonl") {
		limit = maxTranscriptFullBytes
	}
	if info.Size() > limit {
		return parsedTrajectory{}, false
	}

	out := parsedTrajectory{CascadeID: cascadeID, Partial: true}
	hasher := sha256.New()
	reader := bufio.NewReaderSize(io.LimitReader(file, limit+1), 64<<10)
	var lineIdx int64
	for {
		line, readErr := readBoundedLine(reader, maxTranscriptLineBytes)
		if len(line) > 0 {
			trimmed := strings.TrimSpace(string(line))
			if trimmed != "" {
				// 仅反序列化白名单用量字段，严禁提取 content / thinking / tool_calls
				var row struct {
					StepIndex            *int64 `json:"step_index"`
					CreatedAt            string `json:"created_at"`
					CreatedAtCamel       string `json:"createdAt"`
					FinishedGeneratingAt string `json:"finished_generating_at"`
					FinishedGenCamel     string `json:"finishedGeneratingAt"`
					CompletedAt          string `json:"completed_at"`
					CompletedAtCamel     string `json:"completedAt"`
					Model                string `json:"model"`
					GeneratorModel       string `json:"generator_model"`
					ResponseModel        string `json:"response_model"`
					InputTokens          *int64 `json:"input_tokens"`
					CacheReadTokens      *int64 `json:"cache_read_tokens"`
					OutputTokens         *int64 `json:"output_tokens"`
					ThinkingTokens       *int64 `json:"thinking_tokens"`
				}
				if json.Unmarshal([]byte(trimmed), &row) == nil &&
					(row.InputTokens != nil || row.CacheReadTokens != nil || row.OutputTokens != nil) {
					inp := nonNegativePtr(row.InputTokens)
					cache := nonNegativePtr(row.CacheReadTokens)
					outTok := nonNegativePtr(row.OutputTokens)
					think := nonNegativePtr(row.ThinkingTokens)
					if inp > 0 || cache > 0 || outTok > 0 {
						sIdx := lineIdx
						if row.StepIndex != nil && *row.StepIndex >= 0 {
							sIdx = *row.StepIndex
						}
						startNS := parseISONS(firstNonEmpty(row.CreatedAt, row.CreatedAtCamel))
						endNS := parseISONS(firstNonEmpty(row.FinishedGeneratingAt, row.FinishedGenCamel, row.CompletedAt, row.CompletedAtCamel))
						occMS := endNS / 1_000_000
						if occMS == 0 {
							occMS = startNS / 1_000_000
						}
						rawModel := firstNonEmpty(row.Model, row.GeneratorModel)
						modelKey, _ := pricing.ResolveAGYModelID(rawModel, row.ResponseModel)
						step := parsedStep{
							StepIndex:           sIdx,
							OccurredAtMS:        occMS,
							StartedNS:           startNS,
							EndedNS:             endNS,
							ModelKey:            modelKey,
							UncachedInputTokens: inp,
							CacheReadTokens:     cache,
							OutputTokens:        outTok,
							ReasoningTokens:     think,
							ReasoningKnown:      row.ThinkingTokens != nil,
							Provenance:          "agy_transcript_jsonl",
						}
						out.Steps = append(out.Steps, step)
						if occMS > 0 {
							if out.CreatedAtMS == 0 || occMS < out.CreatedAtMS {
								out.CreatedAtMS = occMS
							}
							if occMS > out.LastActivityMS {
								out.LastActivityMS = occMS
							}
						}
						_, _ = fmt.Fprintf(hasher, "%d:%d:%s:%d:%d:%d\n", sIdx, occMS, modelKey, inp, cache, outTok)
					}
				}
			}
			lineIdx++
		}
		if readErr != nil {
			break
		}
	}
	if len(out.Steps) == 0 {
		return parsedTrajectory{}, false
	}
	out.ContentDigest = hex.EncodeToString(hasher.Sum(nil))
	return out, true
}

func readBoundedLine(r *bufio.Reader, maxBytes int) ([]byte, error) {
	var record []byte
	for {
		piece, err := r.ReadSlice('\n')
		if len(record)+len(piece) > maxBytes {
			return nil, ErrCollector
		}
		record = append(record, piece...)
		if err == bufio.ErrBufferFull {
			continue
		}
		return record, err
	}
}

func readAnnotationFileTitle(path string) string {
	file, info, err := openRegularFileReadOnly(path)
	if err != nil {
		return ""
	}
	defer file.Close()
	if info.Size() > maxAnnotationBytes {
		return ""
	}
	raw, err := io.ReadAll(io.LimitReader(file, maxAnnotationBytes))
	if err != nil {
		return ""
	}
	return parseAnnotationTitle(string(raw))
}

type pinnedSQLite struct {
	*sql.DB
	release func()
}

func (p *pinnedSQLite) Close() error {
	err := p.DB.Close()
	if p.release != nil {
		p.release()
	}
	return err
}

func openReadOnlySQLite(ctx context.Context, path string) (*pinnedSQLite, *sql.Tx, error) {
	release, err := pinSourceDatabase(path)
	if err != nil {
		return nil, nil, err
	}
	uriPath := filepath.ToSlash(path)
	if filepath.VolumeName(path) != "" && !strings.HasPrefix(uriPath, "/") {
		uriPath = "/" + uriPath
	}
	uri := (&url.URL{Scheme: "file", Path: uriPath}).String() + "?mode=ro&_pragma=query_only(1)&_pragma=busy_timeout(3000)"
	db, err := sql.Open("sqlite", uri)
	if err != nil {
		release()
		return nil, nil, err
	}
	pinned := &pinnedSQLite{DB: db, release: release}
	pinned.SetMaxOpenConns(1)
	if err := pinned.PingContext(ctx); err != nil {
		_ = pinned.Close()
		return nil, nil, err
	}
	tx, err := pinned.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		_ = pinned.Close()
		return nil, nil, err
	}
	return pinned, tx, nil
}

func sqliteWalSignature(dbPath string) (string, bool) {
	mainInfo, err := os.Lstat(dbPath)
	if err != nil || !mainInfo.Mode().IsRegular() {
		return "", false
	}
	var walSize int64
	var walMtime int64
	if walInfo, err := os.Lstat(dbPath + "-wal"); err == nil && walInfo.Mode().IsRegular() {
		walSize = walInfo.Size()
		walMtime = walInfo.ModTime().UnixNano()
	}
	return fmt.Sprintf("%d:%d|%d:%d", mainInfo.Size(), mainInfo.ModTime().UnixNano(), walSize, walMtime), true
}

func tableColumns(ctx context.Context, tx *sql.Tx, table string) map[string]bool {
	rows, err := tx.QueryContext(ctx, "PRAGMA table_info("+table+")")
	if err != nil {
		return nil
	}
	defer rows.Close()
	cols := make(map[string]bool)
	for rows.Next() {
		var cid int
		var name, colType string
		var notNull, pk int
		var def any
		if rows.Scan(&cid, &name, &colType, &notNull, &def, &pk) == nil {
			cols[strings.ToLower(name)] = true
		}
	}
	return cols
}

func classifySessionRole(
	cid string,
	sum summaryRecord,
	traj *parsedTrajectory,
	sqParents map[string]string,
	parentID string,
) string {
	if sum.TrajectoryType == protoTrajectoryTypeSQ || (traj != nil && traj.TrajectoryType == protoTrajectoryTypeSQ) || sqParents[cid] != "" {
		return "side_question"
	}
	roleHint := strings.ToLower(strings.Join([]string{
		sum.AgentName, sum.SubagentType, sum.SubagentRole,
		trajectoryRoleHints(traj),
	}, " "))
	if strings.Contains(roleHint, "swarm") || strings.Contains(roleHint, "worker") {
		return "swarm_worker"
	}
	if parentID != "" || sum.HasSubagentSpec || sum.NestingDepth > 0 || sum.SourceEnum == protoSourceSubagent ||
		(traj != nil && (traj.HasSubagentSpec || traj.NestingDepth > 0 || traj.SourceEnum == protoSourceSubagent)) {
		return "subagent"
	}
	return "main"
}

func trajectoryRoleHints(traj *parsedTrajectory) string {
	if traj == nil {
		return ""
	}
	return strings.Join([]string{traj.AgentScriptName, traj.SubagentType, traj.SubagentRole}, " ")
}

func trajectoryCreated(traj *parsedTrajectory) int64 {
	if traj == nil {
		return 0
	}
	return traj.CreatedAtMS
}

func trajectoryLastActivity(traj *parsedTrajectory) int64 {
	if traj == nil {
		return 0
	}
	return traj.LastActivityMS
}

func throughputMeasures(s throughput.Stats) reportingv1.ThroughputMeasures {
	return reportingv1.ThroughputMeasures{
		OutputTokens:       s.OutputTokens,
		ActiveDurationMS:   s.ActiveMS,
		IncludedTurns:      s.IncludedTurns,
		ExcludedTurns:      s.ExcludedTurns,
		OpenTurns:          s.OpenTurns,
		UnattributedEvents: s.UnattributedEvents,
		CoverageKnown: s.Status == "complete" || s.Status == "partial" ||
			s.Reason == "no_closed_turns" || s.Reason == "open_turn" || s.Reason == "missing_facts",
		Status:         s.Status,
		Reason:         s.Reason,
		DurationSource: s.DurationSource,
	}
}

func asString(v any) string {
	switch x := v.(type) {
	case string:
		return x
	case []byte:
		return string(x)
	default:
		return ""
	}
}

func asBytes(v any) []byte {
	switch x := v.(type) {
	case []byte:
		return x
	case string:
		return []byte(x)
	default:
		return nil
	}
}

func asInt64(v any) int64 {
	switch x := v.(type) {
	case int64:
		return x
	case int:
		return int64(x)
	case float64:
		return int64(x)
	case string:
		n, _ := strconv.ParseInt(strings.TrimSpace(x), 10, 64)
		return n
	default:
		return 0
	}
}

func parseFlexibleTimeMS(v any) int64 {
	switch x := v.(type) {
	case int64:
		return normalizeEpochMS(x)
	case float64:
		return normalizeEpochMS(int64(x))
	case string:
		s := strings.TrimSpace(x)
		if s == "" {
			return 0
		}
		if n, err := strconv.ParseInt(s, 10, 64); err == nil {
			return normalizeEpochMS(n)
		}
		return parseISONS(s) / 1_000_000
	case time.Time:
		return x.UnixMilli()
	default:
		return 0
	}
}

func normalizeEpochMS(n int64) int64 {
	if n <= 0 {
		return 0
	}
	if n < 10_000_000_000 {
		return n * 1000
	}
	if n > 100_000_000_000_000 {
		return n / 1_000_000
	}
	return n
}

func parseISONS(raw string) int64 {
	s := strings.TrimSpace(raw)
	if s == "" {
		return 0
	}
	for _, layout := range []string{time.RFC3339Nano, time.RFC3339, "2006-01-02 15:04:05.999999999Z07:00", "2006-01-02 15:04:05"} {
		if parsed, err := time.Parse(layout, s); err == nil {
			return parsed.UnixNano()
		}
	}
	return 0
}

func sanitizeLabel(value, fallback string) string {
	cleaned := strings.TrimSpace(value)
	if cleaned == "" || strings.ContainsAny(cleaned, "/\\\n\r") {
		return fallback
	}
	runes := []rune(cleaned)
	if len(runes) > 128 {
		cleaned = string(runes[:128])
	}
	if !safeLabelPattern.MatchString(cleaned) {
		return fallback
	}
	return cleaned
}

func digestString(value string) string {
	sum := sha256.Sum256([]byte(value))
	return hex.EncodeToString(sum[:])
}

func firstNonEmpty(values ...string) string {
	for _, v := range values {
		if strings.TrimSpace(v) != "" {
			return strings.TrimSpace(v)
		}
	}
	return ""
}

func positiveMin(a, b int64) int64 {
	if a <= 0 {
		return b
	}
	if b <= 0 {
		return a
	}
	if a < b {
		return a
	}
	return b
}

func maxInt64(a, b int64) int64 {
	if a > b {
		return a
	}
	return b
}

func nonNegativePtr(v *int64) int64 {
	if v == nil || *v < 0 {
		return 0
	}
	return *v
}
