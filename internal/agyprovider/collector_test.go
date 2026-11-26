package agyprovider

import (
	"context"
	"database/sql"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	_ "modernc.org/sqlite"

	"github.com/SisyphusSQ/codex-pulse/internal/store"
	storesqlite "github.com/SisyphusSQ/codex-pulse/internal/store/sqlite"
)

// Protobuf wire encoders (test helper only)
func pbVarint(v uint64) []byte {
	var out []byte
	for {
		b := byte(v & 0x7f)
		v >>= 7
		if v != 0 {
			b |= 0x80
		}
		out = append(out, b)
		if v == 0 {
			break
		}
	}
	return out
}

func pbTag(fn uint64, wt uint64) []byte { return pbVarint((fn << 3) | wt) }

func pbVarintField(fn uint64, v uint64) []byte {
	return append(pbTag(fn, 0), pbVarint(v)...)
}

func pbBytesField(fn uint64, data []byte) []byte {
	out := append(pbTag(fn, 2), pbVarint(uint64(len(data)))...)
	return append(out, data...)
}

func pbStringField(fn uint64, s string) []byte { return pbBytesField(fn, []byte(s)) }

func pbTimestamp(ts time.Time) []byte {
	var out []byte
	out = append(out, pbVarintField(1, uint64(ts.Unix()))...)
	if ts.Nanosecond() > 0 {
		out = append(out, pbVarintField(2, uint64(ts.Nanosecond()))...)
	}
	return out
}

func pbMapEntry(k, v string) []byte {
	var out []byte
	out = append(out, pbStringField(1, k)...)
	out = append(out, pbStringField(2, v)...)
	return out
}

func pbModelUsage(modelEnum uint64, input, output, cacheRead, thinking uint64) []byte {
	var out []byte
	if modelEnum > 0 {
		out = append(out, pbVarintField(1, modelEnum)...)
	}
	if input > 0 {
		out = append(out, pbVarintField(2, input)...)
	}
	if output > 0 {
		out = append(out, pbVarintField(3, output)...)
	}
	if cacheRead > 0 {
		out = append(out, pbVarintField(5, cacheRead)...)
	}
	if thinking > 0 {
		out = append(out, pbVarintField(9, thinking)...)
	}
	return out
}

func pbStepMeta(created, finished, completed time.Time, usage []byte, stepIdx uint64, originCID string) []byte {
	var out []byte
	if !created.IsZero() {
		out = append(out, pbBytesField(1, pbTimestamp(created))...)
	}
	if !finished.IsZero() {
		out = append(out, pbBytesField(7, pbTimestamp(finished))...)
	}
	if !completed.IsZero() {
		out = append(out, pbBytesField(8, pbTimestamp(completed))...)
	}
	if len(usage) > 0 {
		out = append(out, pbBytesField(9, usage)...)
	}
	var stepInfo []byte
	stepInfo = append(stepInfo, pbVarintField(2, stepIdx)...)
	if originCID != "" {
		stepInfo = append(stepInfo, pbStringField(4, originCID)...)
	}
	out = append(out, pbBytesField(20, stepInfo)...)
	// 注入敏感 toolCall(4) 与 toolSummary(30)，验证边界丢弃
	out = append(out, pbStringField(30, "SECRET_TOOL_SUMMARY_MUST_DROP")...)
	return out
}

func openTestDB(t *testing.T, path string) *sql.DB {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("MkdirAll(%s): %v", path, err)
	}
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatalf("sql.Open(%s): %v", path, err)
	}
	if _, err := db.Exec(`PRAGMA journal_mode=WAL;`); err != nil {
		t.Fatalf("enable WAL: %v", err)
	}
	return db
}

func openPulseRepo(t *testing.T) *store.Repository {
	t.Helper()
	dir := filepath.Join(t.TempDir(), "runtime")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	dbPath := filepath.Join(dir, "pulse.sqlite")
	s, err := storesqlite.Open(context.Background(), storesqlite.Config{Path: dbPath})
	if err != nil {
		t.Fatalf("storesqlite.Open: %v", err)
	}
	t.Cleanup(func() { _ = s.Close(context.Background()) })
	repo := store.NewRepository(s)
	if _, err := repo.MigrateApplicationSchema(context.Background()); err != nil {
		t.Fatalf("MigrateApplicationSchema: %v", err)
	}
	return repo
}

func TestCollectorMultiProductDedupInheritancePrivacyAndWAL(t *testing.T) {
	t.Parallel()

	baseRoot := t.TempDir()
	repo := openPulseRepo(t)
	now := time.Date(2026, 11, 25, 16, 0, 0, 0, time.UTC)

	mainID := "11111111-1111-4111-8111-111111111111"
	subID := "22222222-2222-4222-8222-222222222222"
	orphanSubID := "33333333-3333-4333-8333-333333333333"
	transcriptID := "44444444-4444-4444-8444-444444444444"
	pbOnlyID := "55555555-5555-4555-8555-555555555555"

	// 1. antigravity/conversation_summaries.db
	sumDB := openTestDB(t, filepath.Join(baseRoot, "antigravity", "conversation_summaries.db"))
	_, err := sumDB.Exec(`
		CREATE TABLE conversation_summaries (
			conversation_id TEXT PRIMARY KEY,
			title TEXT,
			preview TEXT,
			step_count INTEGER,
			last_modified_time INTEGER,
			workspace_uris TEXT,
			parent_conversation_id TEXT,
			nesting_depth INTEGER,
			agent_name TEXT,
			app_data_dir TEXT,
			raw_summary BLOB
		);
		CREATE TABLE conversation_parents (
			conversation_id TEXT PRIMARY KEY,
			parent_id TEXT NOT NULL
		);
	`)
	if err != nil {
		t.Fatalf("create summary schema: %v", err)
	}
	// raw_summary: field 1 = sensitive prompt preview (must be dropped), field 15 -> 1 = title
	rawMainSummary := append(pbStringField(1, "SECRET_RAW_SUMMARY_PROMPT"), pbBytesField(15, pbStringField(1, "Summary Title"))...)
	_, err = sumDB.Exec(`INSERT INTO conversation_summaries VALUES
		(?, ?, ?, 2, ?, ?, '', 0, '', 'antigravity', ?),
		(?, ?, ?, 1, ?, '', ?, 1, 'swarm-worker', 'antigravity', NULL),
		(?, ?, ?, 1, ?, '', '99999999-9999-4999-8999-999999999999', 1, 'research', 'antigravity', NULL)
	`,
		mainID, "DB Title", "SECRET_PREVIEW_TEXT", now.Add(-time.Hour).UnixMilli(), `["file:///Users/secret/work/alpha-repo"]`, rawMainSummary,
		subID, "Sub Worker", "SECRET_SUB_PREVIEW", now.Add(-30*time.Minute).UnixMilli(), mainID,
		orphanSubID, "Orphan Sub", "SECRET_ORPHAN_PREVIEW", now.Add(-20*time.Minute).UnixMilli(),
	)
	if err != nil {
		t.Fatalf("insert summaries: %v", err)
	}
	_ = sumDB.Close()

	// 2. annotations/<mainID>.pbtxt（优先级高于 summary title，包含 UTF-8 八进制转义）
	annPath := filepath.Join(baseRoot, "antigravity", "annotations", mainID+".pbtxt")
	if err := os.MkdirAll(filepath.Dir(annPath), 0o755); err != nil {
		t.Fatalf("mkdir annotations: %v", err)
	}
	// \344\270\273\344\274\232\350\257\235 = "主会话"
	if err := os.WriteFile(annPath, []byte("title: \"\\344\\270\\273\\344\\274\\232\\350\\257\\235 Alpha\"\n"), 0o644); err != nil {
		t.Fatalf("write pbtxt: %v", err)
	}

	// 3. antigravity/conversations/<mainID>.db（含正常步、短耗时步 <0.5s、CLEARED 步与被克隆父步骤）
	mainDBPath := filepath.Join(baseRoot, "antigravity", "conversations", mainID+".db")
	mainDB := openTestDB(t, mainDBPath)
	_, err = mainDB.Exec(`
		CREATE TABLE steps (
			idx INTEGER PRIMARY KEY,
			step_type INTEGER,
			status INTEGER,
			metadata BLOB,
			step_payload BLOB
		);
		CREATE TABLE gen_metadata (
			idx INTEGER PRIMARY KEY,
			data BLOB
		);
	`)
	if err != nil {
		t.Fatalf("create main conv schema: %v", err)
	}
	t0 := time.Date(2026, 11, 25, 10, 0, 0, 0, time.UTC)
	// Step 1: MODEL_PLACEHOLDER_M429 (enum 1429 -> gemini-4-carbon-high), duration 2s -> 纳入 TPS
	meta1 := pbStepMeta(t0, t0.Add(2*time.Second), t0.Add(2*time.Second), pbModelUsage(1429, 1000, 200, 4000, 50), 1, mainID)
	// Step 2: 克隆自其他会话的步骤 -> 必须过滤，绝不计费
	metaCloned := pbStepMeta(t0.Add(3*time.Second), t0.Add(5*time.Second), t0.Add(5*time.Second), pbModelUsage(1429, 99999, 99999, 99999, 0), 2, "aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa")
	// Step 3: 499.999ms 短步 -> 计入 token，但不得纳入 TPS
	metaShort := pbStepMeta(t0.Add(10*time.Second), t0.Add(10*time.Second+499_999_999*time.Nanosecond), time.Time{}, pbModelUsage(1402, 500, 100, 500, 0), 3, mainID)
	// Step 4: status=CLEARED(5) 且无 finishedGeneratingAt，仅有 completedAt -> 禁止把 completedAt 计入 TPS
	metaCleared := pbStepMeta(t0.Add(20*time.Second), time.Time{}, t0.Add(25*time.Second), pbModelUsage(1429, 200, 40, 0, 0), 4, mainID)

	_, err = mainDB.Exec(`INSERT INTO steps VALUES
		(1, 15, 3, ?, ?),
		(2, 15, 3, ?, ?),
		(3, 15, 3, ?, ?),
		(4, 15, 5, ?, ?)
	`,
		meta1, []byte("SECRET_STEP_PAYLOAD_1"),
		metaCloned, []byte("SECRET_STEP_PAYLOAD_CLONED"),
		metaShort, []byte("SECRET_STEP_PAYLOAD_3"),
		metaCleared, []byte("SECRET_STEP_PAYLOAD_4"),
	)
	if err != nil {
		t.Fatalf("insert main steps: %v", err)
	}
	// 保持 mainDB 打开以在 WAL 模式下验证并发只读与后续追加
	defer mainDB.Close()

	// 4. antigravity-cli/conversations/<subID>.db（跨产品目录 + 子代理继承父项目）
	subDB := openTestDB(t, filepath.Join(baseRoot, "antigravity-cli", "conversations", subID+".db"))
	_, err = subDB.Exec(`CREATE TABLE steps (idx INTEGER PRIMARY KEY, step_type INTEGER, status INTEGER, metadata BLOB, step_payload BLOB)`)
	if err != nil {
		t.Fatalf("create sub schema: %v", err)
	}
	metaSub := pbStepMeta(t0.Add(time.Minute), t0.Add(time.Minute+time.Second), time.Time{}, pbModelUsage(1669, 300, 60, 700, 10), 1, subID)
	if _, err := subDB.Exec(`INSERT INTO steps VALUES (1, 15, 3, ?, ?)`, metaSub, []byte("SECRET_SUB_PAYLOAD")); err != nil {
		t.Fatalf("insert sub step: %v", err)
	}
	_ = subDB.Close()

	// 5. antigravity-ide/brain/<transcriptID>/.system_generated/logs/transcript.jsonl 回退
	tpPath := filepath.Join(baseRoot, "antigravity-ide", "brain", transcriptID, ".system_generated", "logs", "transcript.jsonl")
	if err := os.MkdirAll(filepath.Dir(tpPath), 0o755); err != nil {
		t.Fatalf("mkdir transcript: %v", err)
	}
	tpLine := `{"step_index":1,"created_at":"2026-11-25T11:00:00Z","finished_generating_at":"2026-11-25T11:00:02Z","model":"gemini-3-flash","input_tokens":100,"cache_read_tokens":200,"output_tokens":50,"content":"SECRET_TRANSCRIPT_BODY","thinking":"SECRET_THINKING"}` + "\n"
	if err := os.WriteFile(tpPath, []byte(tpLine), 0o644); err != nil {
		t.Fatalf("write transcript: %v", err)
	}

	// 6. antigravity-idea/conversations/<pbOnlyID>.pb 加密回退（仅标记 partial，不伪造 token）
	pbPath := filepath.Join(baseRoot, "antigravity-idea", "conversations", pbOnlyID+".pb")
	if err := os.MkdirAll(filepath.Dir(pbPath), 0o755); err != nil {
		t.Fatalf("mkdir pb: %v", err)
	}
	if err := os.WriteFile(pbPath, []byte("ENCRYPTED_BINARY"), 0o644); err != nil {
		t.Fatalf("write pb: %v", err)
	}

	clock := now
	collector, err := NewCollector(repo, Config{
		BaseRoot:       baseRoot,
		MinimumRefresh: 0,
		Now:            func() time.Time { return clock },
	})
	if err != nil {
		t.Fatalf("NewCollector: %v", err)
	}
	if err := collector.Refresh(context.Background()); err != nil {
		t.Fatalf("Refresh: %v", err)
	}

	snap, err := repo.AGYSnapshot(context.Background())
	if err != nil {
		t.Fatalf("AGYSnapshot: %v", err)
	}

	// 隐私红线：序列化整个快照，确认任何敏感正文与本机绝对路径均未出现
	rawJSON, err := json.Marshal(snap)
	if err != nil {
		t.Fatalf("json.Marshal: %v", err)
	}
	blob := string(rawJSON)
	for _, secret := range []string{
		"SECRET_PREVIEW_TEXT", "SECRET_RAW_SUMMARY_PROMPT", "SECRET_STEP_PAYLOAD",
		"SECRET_TOOL_SUMMARY", "SECRET_TRANSCRIPT_BODY", "SECRET_THINKING",
		"/Users/secret/work",
	} {
		if strings.Contains(blob, secret) {
			t.Fatalf("privacy leak: %q appeared in AGYSnapshot", secret)
		}
	}

	byID := make(map[string]store.AGYSession, len(snap.Sessions))
	for _, s := range snap.Sessions {
		byID[s.ExternalSessionID] = s
	}

	// 验证主会话标题优先级、角色、项目 basename、克隆去重与 TPS（仅 Step 1 满足 >=0.5s）
	mainSess := byID[mainID]
	if mainSess.DisplayTitle != "主会话 Alpha" || mainSess.TitleSource != "agy_annotation" {
		t.Fatalf("main title = %q (%s), want 主会话 Alpha (agy_annotation)", mainSess.DisplayTitle, mainSess.TitleSource)
	}
	if mainSess.ProjectDisplayName != "alpha-repo" || mainSess.ProjectSource != "workspace" {
		t.Fatalf("main project = %q (%s)", mainSess.ProjectDisplayName, mainSess.ProjectSource)
	}
	if mainSess.RequestCount != 3 {
		t.Fatalf("main RequestCount = %d, want 3 (cloned step 2 must be filtered)", mainSess.RequestCount)
	}
	if mainSess.Throughput == nil || mainSess.Throughput.Measures.IncludedTurns != 1 ||
		mainSess.Throughput.Measures.OutputTokens == nil || *mainSess.Throughput.Measures.OutputTokens != 200 ||
		mainSess.Throughput.Measures.ActiveDurationMS == nil || *mainSess.Throughput.Measures.ActiveDurationMS != 2000 {
		t.Fatalf("unexpected main throughput: %+v", mainSess.Throughput)
	}

	// 验证子代理独立会话与父项目继承
	subSess := byID[subID]
	if subSess.SessionRole != "swarm_worker" || subSess.ProjectSource != "inherited_parent" ||
		subSess.ProjectKey != mainSess.ProjectKey || subSess.ProjectDisplayName != "alpha-repo" {
		t.Fatalf("unexpected sub session: %+v", subSess)
	}

	// 验证孤儿子代理无法解析父会话时落入未归属项目
	orphanSess := byID[orphanSubID]
	if orphanSess.ProjectSource != "unassigned" || orphanSess.ProjectDisplayName != unassignedProjectName {
		t.Fatalf("unexpected orphan session: %+v", orphanSess)
	}

	// 验证 transcript 回退与加密 .pb partial 标记
	tpSess := byID[transcriptID]
	if tpSess.RequestCount != 1 || tpSess.CoverageState != "partial" {
		t.Fatalf("unexpected transcript session: %+v", tpSess)
	}
	pbSess := byID[pbOnlyID]
	if pbSess.RequestCount != 0 || pbSess.CoverageState != "partial" {
		t.Fatalf("unexpected pb-only session: %+v", pbSess)
	}

	// 验证 WAL 写入新步骤后缓存能感知变化并增量刷新
	meta5 := pbStepMeta(t0.Add(30*time.Second), t0.Add(31*time.Second), time.Time{}, pbModelUsage(1429, 100, 50, 0, 0), 5, mainID)
	if _, err := mainDB.Exec(`INSERT INTO steps VALUES (5, 15, 3, ?, NULL)`, meta5); err != nil {
		t.Fatalf("insert WAL step 5: %v", err)
	}
	clock = clock.Add(time.Minute)
	if err := collector.Refresh(context.Background()); err != nil {
		t.Fatalf("second Refresh: %v", err)
	}
	snap2, err := repo.AGYSnapshot(context.Background())
	if err != nil {
		t.Fatalf("second AGYSnapshot: %v", err)
	}
	for _, s := range snap2.Sessions {
		if s.ExternalSessionID == mainID && s.RequestCount != 4 {
			t.Fatalf("WAL update not detected: main RequestCount = %d, want 4", s.RequestCount)
		}
	}
}
