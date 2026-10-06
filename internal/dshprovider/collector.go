package dshprovider

import (
	"bufio"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/klauspost/compress/zstd"

	reportingv1 "github.com/SisyphusSQ/codex-pulse/api/codexpulse/reporting/v1"
	"github.com/SisyphusSQ/codex-pulse/internal/jsonshape"
	"github.com/SisyphusSQ/codex-pulse/internal/providercontrol"
	"github.com/SisyphusSQ/codex-pulse/internal/store"
	"github.com/SisyphusSQ/codex-pulse/internal/throughput"
)

// Bounds apply to decoded bytes as well as compressed input. No source text is retained.
const maxLogBytes = 256 << 20
const maxLineBytes = 16 << 20
const maxSessionFiles = 20000

var generationName = regexp.MustCompile(`^session\.v([1-9][0-9]*)\.jsonl(\.zstd)?$`)
var safeLabel = regexp.MustCompile(`^[\p{L}\p{N}_.:/ -]{1,128}$`)

type SnapshotWriter interface {
	ReplaceDSHSnapshot(context.Context, store.DSHSnapshot) error
}
type fileSnapshot struct {
	info    fs.FileInfo
	session store.DSHSession
	usage   []store.DSHUsageEvent
	tools   []store.DSHToolEvent
	digest  string
}
type Collector struct {
	writer SnapshotWriter
	config Config
	mu     sync.Mutex
	last   time.Time
	files  map[string]fileSnapshot
}

func NewCollector(writer SnapshotWriter, config Config) (*Collector, error) {
	if writer == nil || !filepath.IsAbs(config.SessionsRoot) || config.MinimumRefresh < 0 {
		return nil, ErrCollector
	}
	if config.Now == nil {
		config.Now = time.Now
	}
	return &Collector{writer: writer, config: config, files: map[string]fileSnapshot{}}, nil
}
func (c *Collector) Refresh(ctx context.Context) error { _, err := c.RefreshIfDue(ctx); return err }
func (c *Collector) RefreshIfDue(ctx context.Context) (bool, error) {
	if c == nil || ctx == nil {
		return false, ErrCollector
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	now := c.config.Now()
	if !c.last.IsZero() && now.Sub(c.last) < c.config.MinimumRefresh {
		return false, nil
	}
	snapshot, next := c.collect(ctx, now.UnixMilli())
	if err := ctx.Err(); err != nil {
		return true, err
	}
	if err := providercontrol.WriteWithCommit(ctx, func() error { return c.writer.ReplaceDSHSnapshot(ctx, snapshot) }); err != nil {
		return true, err
	}
	c.files = next
	c.last = now
	return true, nil
}
func (c *Collector) collect(ctx context.Context, at int64) (store.DSHSnapshot, map[string]fileSnapshot) {
	result := store.DSHSnapshot{Generation: at, CollectedAtMS: at}
	status := store.CursorSourceStatus{Provider: "dsh", SourceKey: SourceUpdates, SourceType: "jsonl", State: "unavailable", CoverageState: "unknown", CheckpointKind: "filesystem_scan", LastAttemptAtMS: at, UpdatedAtMS: at}
	next := map[string]fileSnapshot{}
	finish := func() (store.DSHSnapshot, map[string]fileSnapshot) {
		result.Sources = []store.CursorSourceStatus{status}
		header := status
		header.SourceKey = SourceSummary
		header.SourceType = "session_header"
		result.Sources = append(result.Sources, header)
		return result, next
	}
	rootInfo, err := os.Lstat(c.config.SessionsRoot)
	if err != nil || !rootInfo.IsDir() || rootInfo.Mode()&os.ModeSymlink != 0 {
		code := "missing"
		if err == nil {
			code = "invalid_path"
		}
		status.FailureCode = &code
		return finish()
	}
	root, err := os.OpenRoot(c.config.SessionsRoot)
	if err != nil {
		code := "read_failed"
		status.FailureCode = &code
		return finish()
	}
	defer root.Close()
	selected := map[string]string{}
	versions := map[string]int{}
	mixed := map[string]bool{}
	err = fs.WalkDir(root.FS(), ".", func(path string, entry fs.DirEntry, walkErr error) error {
		if err := ctx.Err(); err != nil {
			return err
		}
		if walkErr != nil {
			return walkErr
		}
		if entry.Type()&os.ModeSymlink != 0 {
			return nil
		}
		if entry.IsDir() {
			if strings.HasPrefix(entry.Name(), ".") && path != "." {
				return fs.SkipDir
			}
			// The official layout is project/session/file; do not traverse nested content directories.
			if path != "." && len(strings.Split(path, "/")) >= 3 {
				return fs.SkipDir
			}
			return nil
		}
		parts := strings.Split(path, "/")
		if len(parts) != 3 {
			return nil
		}
		match := generationName.FindStringSubmatch(entry.Name())
		if match == nil {
			return nil
		}
		version, e := strconv.Atoi(match[1])
		if e != nil {
			return ErrCollector
		}
		dir := filepath.Dir(path)
		if old, ok := selected[dir]; ok && strings.HasSuffix(old, ".zstd") != strings.HasSuffix(path, ".zstd") {
			mixed[dir] = true
		}
		if version > versions[dir] {
			selected[dir] = path
			versions[dir] = version
		}
		if len(selected) > maxSessionFiles {
			return ErrCollector
		}
		return nil
	})
	status.State = "available"
	status.CoverageState = "exact"
	status.LastSuccessAtMS = &at
	if err != nil {
		status.State = "partial"
		status.CoverageState = "partial"
		code := "scan_failed"
		status.FailureCode = &code
	}
	dirs := make([]string, 0, len(selected))
	for dir := range selected {
		dirs = append(dirs, dir)
	}
	sort.Strings(dirs)
	ids := map[string]int{}
	for _, dir := range dirs {
		if ctx.Err() != nil {
			break
		}
		path := selected[dir]
		info, e := root.Lstat(path)
		if e != nil || !info.Mode().IsRegular() || info.Size() > maxLogBytes || mixed[dir] || (versions[dir] != 3 && versions[dir] != 4) {
			status.State = "partial"
			status.CoverageState = "partial"
			continue
		}
		cached, ok := c.files[path]
		if !ok || !os.SameFile(info, cached.info) || info.Size() != cached.info.Size() || !info.ModTime().Equal(cached.info.ModTime()) {
			file, e := root.Open(path)
			if e != nil {
				status.State = "partial"
				status.CoverageState = "partial"
				continue
			}
			opened, e := file.Stat()
			if e != nil || !os.SameFile(info, opened) {
				file.Close()
				status.State = "partial"
				status.CoverageState = "partial"
				continue
			}
			var reader io.Reader = io.LimitReader(file, maxLogBytes+1)
			var decoder *zstd.Decoder
			if strings.HasSuffix(path, ".zstd") {
				decoder, e = zstd.NewReader(reader, zstd.WithDecoderConcurrency(1), zstd.WithDecoderMaxMemory(maxLogBytes))
				if e == nil {
					reader = decoder
				}
			}
			var parsed fileSnapshot
			if e == nil {
				parsed, e = parseSession(ctx, reader, versions[dir], at)
			}
			if decoder != nil {
				decoder.Close()
			}
			file.Close()
			if e != nil {
				status.State = "partial"
				status.CoverageState = "partial"
				if !ok {
					continue
				}
			} else {
				cached = parsed
				cached.info = info
			}
		}
		if index, duplicate := ids[cached.session.ExternalSessionID]; duplicate {
			next[path] = cached
			if result.Lineage[index].ContentDigest != cached.digest {
				status.State = "partial"
				status.CoverageState = "partial"
				result.Sessions[index].LineageConflict = true
				result.Sessions[index].CoverageState = "partial"
			}
			continue
		}
		if cached.session.CoverageState != "exact" {
			status.State = "partial"
			status.CoverageState = "partial"
		}
		ids[cached.session.ExternalSessionID] = len(result.Sessions)
		next[path] = cached
		result.Sessions = append(result.Sessions, cached.session)
		result.UsageEvents = append(result.UsageEvents, cached.usage...)
		result.ToolEvents = append(result.ToolEvents, cached.tools...)
		result.Lineage = append(result.Lineage, store.DSHSessionLineage{ExternalSessionID: cached.session.ExternalSessionID, SourceKey: SourceUpdates, LineageKey: digest(dir), ContentDigest: cached.digest, ObservedAtMS: at})
	}
	status.RowCount = int64(len(result.Sessions))
	return finish()
}
func digest(value string) string {
	sum := sha256.Sum256([]byte(value))
	return hex.EncodeToString(sum[:])
}
func label(value string) string {
	if !safeLabel.MatchString(value) || strings.Contains(value, "/") {
		return "unknown"
	}
	return value
}

// Narrow decoding drops message bodies, tool arguments/results and request configuration secrets.
type logRow struct {
	Type string          `json:"type"`
	Seq  *int64          `json:"seq"`
	Time int64           `json:"time"`
	Data json.RawMessage `json:"data"`
}
type tokenUsage struct {
	Input     *int64 `json:"inputTokens"`
	Output    *int64 `json:"outputTokens"`
	Cached    *int64 `json:"cacheReadTokens"`
	Written   *int64 `json:"cacheWriteTokens"`
	Reasoning *int64 `json:"reasoningTokens"`
	Total     *int64 `json:"totalTokens"`
}

func parseSession(ctx context.Context, r io.Reader, version int, at int64) (fileSnapshot, error) {
	var out fileSnapshot
	bounded := &io.LimitedReader{R: r, N: maxLogBytes + 1}
	lines := bufio.NewReaderSize(bounded, 64<<10)
	read := func() ([]byte, error) {
		var record []byte
		for {
			piece, err := lines.ReadSlice('\n')
			if len(record)+len(piece) > maxLineBytes {
				return nil, ErrCollector
			}
			record = append(record, piece...)
			if err == bufio.ErrBufferFull {
				continue
			}
			return record, err
		}
	}
	header, err := read()
	if err != nil || jsonshape.ValidateDocument(header) != nil {
		return out, ErrCollector
	}
	var h struct {
		Type       string `json:"type"`
		Version    int    `json:"version"`
		ID         string `json:"id"`
		CreatedAt  int64  `json:"createdAt"`
		Cwd        string `json:"cwd"`
		IsSeeded   bool   `json:"isSeeded"`
		SeedLength int64  `json:"seedLength"`
	}
	if json.Unmarshal(header, &h) != nil || h.Type != "session" || h.Version != version || h.CreatedAt < 0 || h.CreatedAt > at || len(h.ID) == 0 || len(h.ID) > 128 || strings.ContainsAny(h.ID, "/\\\n\r") {
		return out, ErrCollector
	}
	name := "unknown"
	if filepath.IsAbs(h.Cwd) {
		name = label(filepath.Base(filepath.Clean(h.Cwd)))
	}
	out.session = store.DSHSession{ExternalSessionID: digest("dsh-session:" + h.ID), DisplayTitle: "未命名会话", TitleSource: "fallback", ProjectKey: digest(h.Cwd), ProjectDisplayName: name, CreatedAtMS: h.CreatedAt, LastActivityAtMS: h.CreatedAt, CoverageState: "exact", UpdatedAtMS: at}
	hasher := sha256.New()
	hasher.Write(header)
	route, model := "", ""
	seq := int64(0)
	seedSeen := !h.IsSeeded || (version == 3 && h.SeedLength > 0)
	starts := map[string]int64{}
	tools := map[string]int{}
	requests := int64(0)
	throughputState := throughput.NewAccumulator()
	for {
		if ctx.Err() != nil {
			return out, ctx.Err()
		}
		line, e := read()
		if e != nil {
			if errors.Is(e, io.EOF) && len(line) == 0 {
				break
			}
			return out, ErrCollector
		}
		if jsonshape.ValidateDocument(line) != nil {
			return out, ErrCollector
		}
		var row logRow
		if json.Unmarshal(line, &row) != nil || row.Seq == nil || *row.Seq != seq || row.Time < 0 || row.Time > at || len(row.Type) == 0 {
			return out, ErrCollector
		}
		seq++
		if seq > 100000 {
			return out, ErrCollector
		}
		hasher.Write(line)
		var data struct {
			Title     *string `json:"title"`
			Turn      int64   `json:"turn"`
			Step      int64   `json:"step"`
			Inherited bool    `json:"inherited"`
			Header    struct {
				Config struct {
					Provider string `json:"provider"`
					Model    string `json:"model"`
				} `json:"config"`
			} `json:"header"`
			Provider string      `json:"provider"`
			Model    string      `json:"model"`
			Usage    *tokenUsage `json:"usage"`
			Stream   []struct {
				Type  string `json:"type"`
				Time  int64  `json:"time"`
				Chunk struct {
					Type  string      `json:"type"`
					Usage *tokenUsage `json:"usage"`
				} `json:"chunk"`
			} `json:"stream"`
			CallID  string `json:"callId"`
			Name    string `json:"name"`
			Message struct {
				ToolCallID string `json:"toolCallId"`
				IsError    bool   `json:"isError"`
				Content    []struct {
					Type    string `json:"type"`
					CallID  string `json:"callId"`
					IsError bool   `json:"isError"`
				} `json:"content"`
			} `json:"message"`
		}
		if json.Unmarshal(row.Data, &data) != nil {
			return out, ErrCollector
		}
		switch row.Type {
		case "session/title":
			// Titles in the seed are inherited metadata, unlike usage facts.
			if data.Title == nil || !utf8.ValidString(*data.Title) {
				return out, ErrCollector
			}
			title := strings.TrimSpace(*data.Title)
			if title == "" || strings.ContainsFunc(title, unicode.IsControl) {
				return out, ErrCollector
			}
			runes := []rune(title)
			if len(runes) > 512 {
				title = string(runes[:512])
			}
			out.session.DisplayTitle = title
			out.session.TitleSource = "dsh_title_event"
		case "request/header":
			route = data.Header.Config.Provider
			model = data.Header.Config.Model
		case "request/context":
			if data.Provider != "" {
				route = data.Provider
			}
			if data.Model != "" {
				model = data.Model
			}
		case "session/end-seed":
			if data.Inherited {
				out.usage = nil
				out.tools = nil
				tools = map[string]int{}
				starts = map[string]int64{}
				requests = 0
				throughputState = throughput.NewAccumulator()
				seedSeen = true
				out.session.LastActivityAtMS = h.CreatedAt
			}
			continue
		}
		if version == 3 && *row.Seq < h.SeedLength {
			continue
		}
		out.session.LastActivityAtMS = maxInt64(out.session.LastActivityAtMS, row.Time)
		key := strconv.FormatInt(data.Turn, 10) + ":" + strconv.FormatInt(data.Step, 10)
		turnKey := strconv.FormatInt(data.Turn, 10)
		switch row.Type {
		case "turn/start":
			throughputState.Add(throughput.Event{Kind: "start", TurnID: &turnKey, AtMS: &row.Time, TimeSource: "log_timestamp"})
		case "turn/end":
			throughputState.Add(throughput.Event{Kind: "complete", TurnID: &turnKey, AtMS: &row.Time, TimeSource: "log_timestamp"})
		case "step/start":
			starts[key] = row.Time
		case "assistant/message", "assistant/attempt":
			requests++
			requestStart, hasRequestStart := starts[key]
			delete(starts, key)
			u := data.Usage
			if u == nil {
				for _, chunk := range data.Stream {
					if chunk.Type == "chunk" && chunk.Chunk.Type == "usage" {
						u = chunk.Chunk.Usage
					}
				}
			}
			if u == nil {
				out.session.CoverageState = "partial"
				throughputState.Add(throughput.Event{Kind: "gap", TurnID: &turnKey})
				continue
			}
			counts := []*int64{u.Input, u.Output, u.Cached, u.Written, u.Reasoning, u.Total}
			for _, count := range counts {
				if count != nil && (*count < 0 || *count > 1<<40) {
					return out, ErrCollector
				}
			}
			if u.Input == nil || u.Output == nil {
				return out, ErrCollector
			}
			ev := store.DSHUsageEvent{EventID: digest(h.ID + ":" + strconv.FormatInt(*row.Seq, 10)), ExternalSessionID: out.session.ExternalSessionID, OccurredAtMS: row.Time, ModelProvider: label(route), InputTokens: *u.Input, OutputTokens: *u.Output, UpdatedAtMS: at}
			if model != "" {
				v := label(model)
				ev.ModelKey = &v
				out.session.ModelKey = &v
			}
			if u.Cached != nil {
				ev.CacheReadKnown = true
				ev.CachedReadTokens = *u.Cached
			}
			if u.Written != nil {
				ev.CacheWriteKnown = true
				ev.CacheCreationTokens = *u.Written
			}
			// DeepSeek adapter never writes cache; an exact aggregate establishes the absent bucket as zero.
			if u.Total != nil {
				sum := ev.InputTokens + ev.OutputTokens + ev.CachedReadTokens + ev.CacheCreationTokens
				if *u.Total == sum {
					ev.TotalKnown = true
					ev.TotalTokens = *u.Total
					if u.Cached == nil {
						ev.CacheReadKnown = true
					}
					if u.Written == nil {
						ev.CacheWriteKnown = true
					}
				}
			}
			if u.Reasoning != nil {
				ev.ReasoningKnown = true
				ev.ReasoningTokens = *u.Reasoning
			}
			if hasRequestStart && requestStart <= row.Time {
				ev.StartedAtMS = &requestStart
				end := row.Time
				ev.EndedAtMS = &end
			}
			out.usage = append(out.usage, ev)
			throughputState.Add(throughput.Event{Kind: "usage", TurnID: &turnKey, AtMS: &row.Time, OutputDelta: &ev.OutputTokens, OutputObserved: true})
		case "tool/call":
			if data.CallID == "" || tools[data.CallID] != 0 {
				return out, ErrCollector
			}
			out.tools = append(out.tools, store.DSHToolEvent{EventID: digest(h.ID + ":tool:" + data.CallID), ExternalSessionID: out.session.ExternalSessionID, OccurredAtMS: row.Time, ToolName: label(data.Name), Outcome: "unknown", UpdatedAtMS: at})
			tools[data.CallID] = len(out.tools)
		case "tool/result":
			id, failed := data.Message.ToolCallID, data.Message.IsError
			for _, part := range data.Message.Content {
				if part.Type == "tool-result" {
					id, failed = part.CallID, part.IsError
				}
			}
			if index := tools[id]; index > 0 {
				out.tools[index-1].Outcome = "succeeded"
				if failed {
					out.tools[index-1].Outcome = "failed"
				}
			}
		}
	}
	if bounded.N <= 0 || !seedSeen {
		return out, ErrCollector
	}
	stats, turns := throughputState.Result()
	capsule := &reportingv1.ThroughputCapsule{Version: 1, Basis: "closed_turn_lifetime_output", Measures: measures(stats), TurnsTotal: int64(len(turns)), RecentTurns: []reportingv1.ThroughputTurn{}}
	for _, turn := range turns[:min(len(turns), reportingv1.MaxThroughputTurns)] {
		capsule.RecentTurns = append(capsule.RecentTurns, reportingv1.ThroughputTurn{Key: digest(h.ID + ":turn:" + turn.ID), StartedAtMS: turn.StartedAtMS, EndedAtMS: turn.EndedAtMS, Measures: measures(turn.Stats)})
	}
	out.session.Throughput = capsule
	out.session.RequestCount = requests
	out.session.ToolCallCount = int64(len(out.tools))
	out.digest = hex.EncodeToString(hasher.Sum(nil))
	return out, nil
}

func measures(s throughput.Stats) reportingv1.ThroughputMeasures {
	return reportingv1.ThroughputMeasures{OutputTokens: s.OutputTokens, ActiveDurationMS: s.ActiveMS, IncludedTurns: s.IncludedTurns, ExcludedTurns: s.ExcludedTurns, OpenTurns: s.OpenTurns, UnattributedEvents: s.UnattributedEvents, CoverageKnown: s.Status == "complete" || s.Status == "partial" || s.Reason == "no_closed_turns" || s.Reason == "open_turn" || s.Reason == "missing_facts", Status: s.Status, Reason: s.Reason, DurationSource: s.DurationSource}
}
