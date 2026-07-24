package lightindex

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"gorm.io/gorm"

	"github.com/SisyphusSQ/codex-pulse/internal/codex/appserver"
	"github.com/SisyphusSQ/codex-pulse/internal/codex/logs"
	"github.com/SisyphusSQ/codex-pulse/internal/store"
	storesqlite "github.com/SisyphusSQ/codex-pulse/internal/store/sqlite"
)

func TestRuntimePublishesMetadataBeforeBackgroundTokenScan(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	homePath := t.TempDir()
	rollout := filepath.Join(homePath, "sessions", "2026", "rollout.jsonl")
	if err := os.MkdirAll(filepath.Dir(rollout), 0o700); err != nil {
		t.Fatal(err)
	}
	content := tokenLine("2026-07-19T01:00:00Z", 100, 20, 10, 2) + "\n"
	if err := os.WriteFile(rollout, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	homeMetadata, err := logs.NewHomeProbe().Probe(ctx, homePath)
	if err != nil {
		t.Fatal(err)
	}
	canonicalRollout, err := filepath.EvalSymlinks(rollout)
	if err != nil {
		t.Fatal(err)
	}

	database, repository := openLightRuntimeRepository(t)
	name := "真实标题"
	path := canonicalRollout
	provider := metadataProviderFunc(func(context.Context, string) (appserver.ThreadList, error) {
		return appserver.ThreadList{Threads: []appserver.ThreadMetadata{{
			SessionID: "one", Name: &name, CWD: "/workspace/project", RolloutPath: &path,
			CreatedAtMS: 100, UpdatedAtMS: 200,
		}}}, nil
	})
	releaseScan := make(chan struct{})
	runtime, err := NewRuntime(RuntimeConfig{
		Repository: repository, Metadata: provider, ScanBatchBytes: 8 << 10,
		Clock: func() time.Time { return time.UnixMilli(1_000) },
		BeforeTokenScan: func(ctx context.Context) error {
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-releaseScan:
				return nil
			}
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	run, err := runtime.Start(ctx, store.LightHomeIdentity{
		Path: homeMetadata.Path, DeviceID: homeMetadata.DeviceID, Inode: homeMetadata.Inode,
	})
	if err != nil {
		t.Fatalf("Start() error = %v", err)
	}
	sessions, err := repository.ListLightSessions(ctx)
	if err != nil || len(sessions) != 1 || sessions[0].ThreadName == nil || *sessions[0].ThreadName != name {
		t.Fatalf("metadata was not ready before Start returned: %#v, %v", sessions, err)
	}
	if _, err := repository.LightSessionTokenUsage(ctx, "one"); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("Token scan crossed blocking hook: %v", err)
	}
	close(releaseScan)
	if err := run.Wait(ctx); err != nil {
		t.Fatalf("Wait() error = %v", err)
	}
	usage, err := repository.LightSessionTokenUsage(ctx, "one")
	if err != nil || usage.InputTokens != 100 || usage.CachedInputTokens != 20 ||
		usage.OutputTokens != 10 || usage.ReasoningTokens != 2 || !usage.Complete {
		t.Fatalf("usage = %#v, %v", usage, err)
	}
	active, err := repository.ActiveLightTokenScan(ctx, "one")
	if err != nil || active.Checkpoint.JSONDecoded != 1 || active.Checkpoint.PhysicalBytesRead <= 0 {
		t.Fatalf("active scan = %#v, %v", active, err)
	}
	physicalBytes := active.Checkpoint.PhysicalBytesRead
	refresh, err := runtime.Start(ctx, store.LightHomeIdentity{
		Path: homeMetadata.Path, DeviceID: homeMetadata.DeviceID, Inode: homeMetadata.Inode,
	})
	if err != nil {
		t.Fatalf("Start(no-change refresh) error = %v", err)
	}
	if err := refresh.Wait(ctx); err != nil {
		t.Fatalf("Wait(no-change refresh) error = %v", err)
	}
	active, err = repository.ActiveLightTokenScan(ctx, "one")
	if err != nil || active.Checkpoint.PhysicalBytesRead != physicalBytes || active.Checkpoint.JSONDecoded != 1 {
		t.Fatalf("no-change refresh read content: %#v, %v", active, err)
	}
	assertLightRuntimeDidNotWriteDeepFacts(t, database)
}

func TestRuntimeRebuildsUnchangedRolloutAfterParserVersionBump(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	homePath := t.TempDir()
	rollout := filepath.Join(homePath, "sessions", "parser-bump.jsonl")
	if err := os.MkdirAll(filepath.Dir(rollout), 0o700); err != nil {
		t.Fatal(err)
	}
	content := strings.Join([]string{
		`{"timestamp":"2026-07-19T01:00:00Z","type":"turn_context","payload":{"model":"gpt-5.4-mini"}}`,
		tokenLine("2026-07-19T01:00:01Z", 100, 20, 10, 2),
	}, "\n") + "\n"
	if err := os.WriteFile(rollout, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	homeMetadata, err := logs.NewHomeProbe().Probe(ctx, homePath)
	if err != nil {
		t.Fatal(err)
	}
	canonicalRollout, err := filepath.EvalSymlinks(rollout)
	if err != nil {
		t.Fatal(err)
	}

	database, repository := openLightRuntimeRepository(t)
	provider := metadataProviderFunc(func(context.Context, string) (appserver.ThreadList, error) {
		return appserver.ThreadList{Threads: []appserver.ThreadMetadata{{
			SessionID: "parser-bump", CWD: "/workspace", RolloutPath: &canonicalRollout,
			CreatedAtMS: 100, UpdatedAtMS: 200,
		}}}, nil
	})
	runtime, err := NewRuntime(RuntimeConfig{Repository: repository, Metadata: provider})
	if err != nil {
		t.Fatal(err)
	}
	home := store.LightHomeIdentity{Path: homeMetadata.Path, DeviceID: homeMetadata.DeviceID, Inode: homeMetadata.Inode}
	first, err := runtime.Start(ctx, home)
	if err != nil {
		t.Fatal(err)
	}
	if err := first.Wait(ctx); err != nil {
		t.Fatal(err)
	}
	initial, err := repository.ActiveLightTokenScan(ctx, "parser-bump")
	if err != nil {
		t.Fatal(err)
	}

	if err := database.Write(ctx, func(ctx context.Context, transaction storesqlite.WriteTx) error {
		if err := transaction.WithContext(ctx).Table("light_token_scans").
			Where("session_id = ? AND generation = ?", "parser-bump", initial.Generation).
			Updates(map[string]any{
				"parser_version": "codex-token-count-v1", "current_model_key": nil,
				"current_model_source": "missing",
			}).Error; err != nil {
			return err
		}
		return transaction.WithContext(ctx).Table("light_token_timed").
			Where("session_id = ? AND generation = ?", "parser-bump", initial.Generation).
			Updates(map[string]any{"model_key": nil, "model_source": "missing"}).Error
	}); err != nil {
		t.Fatal(err)
	}

	second, err := runtime.Start(ctx, home)
	if err != nil {
		t.Fatal(err)
	}
	if err := second.Wait(ctx); err != nil {
		t.Fatal(err)
	}
	active, err := repository.ActiveLightTokenScan(ctx, "parser-bump")
	if err != nil {
		t.Fatal(err)
	}
	if active.Generation <= initial.Generation || active.ParserVersion != TokenParserVersion ||
		active.Checkpoint.CurrentModelKey == nil || *active.Checkpoint.CurrentModelKey != "gpt-5.4-mini" {
		t.Fatalf("parser bump did not rebuild unchanged rollout: initial=%#v active=%#v", initial, active)
	}
}

func TestRuntimeCancellationLeavesMetadataReady(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	homePath := t.TempDir()
	if err := os.MkdirAll(filepath.Join(homePath, "sessions"), 0o700); err != nil {
		t.Fatal(err)
	}
	homeMetadata, err := logs.NewHomeProbe().Probe(ctx, homePath)
	if err != nil {
		t.Fatal(err)
	}
	_, repository := openLightRuntimeRepository(t)
	provider := metadataProviderFunc(func(context.Context, string) (appserver.ThreadList, error) {
		return appserver.ThreadList{Threads: []appserver.ThreadMetadata{{
			SessionID: "one", CWD: "/workspace", CreatedAtMS: 100, UpdatedAtMS: 200,
		}}}, nil
	})
	blocked := make(chan struct{})
	runtime, err := NewRuntime(RuntimeConfig{
		Repository: repository, Metadata: provider,
		BeforeTokenScan: func(ctx context.Context) error {
			close(blocked)
			<-ctx.Done()
			return ctx.Err()
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	run, err := runtime.Start(ctx, store.LightHomeIdentity{
		Path: homeMetadata.Path, DeviceID: homeMetadata.DeviceID, Inode: homeMetadata.Inode,
	})
	if err != nil {
		t.Fatal(err)
	}
	<-blocked
	run.Cancel()
	if err := run.Wait(ctx); err != context.Canceled {
		t.Fatalf("Wait() error = %v, want context.Canceled", err)
	}
	if sessions, err := repository.ListLightSessions(ctx); err != nil || len(sessions) != 1 {
		t.Fatalf("cancellation removed metadata: %#v, %v", sessions, err)
	}
}

func TestRuntimeHomeSwitchReplacesOnlyConfirmedOldHome(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	oldPath := t.TempDir()
	newPath := t.TempDir()
	for _, path := range []string{oldPath, newPath} {
		if err := os.MkdirAll(filepath.Join(path, "sessions"), 0o700); err != nil {
			t.Fatal(err)
		}
	}
	oldMetadata, err := logs.NewHomeProbe().Probe(ctx, oldPath)
	if err != nil {
		t.Fatal(err)
	}
	newMetadata, err := logs.NewHomeProbe().Probe(ctx, newPath)
	if err != nil {
		t.Fatal(err)
	}
	oldHome := store.LightHomeIdentity{Path: oldMetadata.Path, DeviceID: oldMetadata.DeviceID, Inode: oldMetadata.Inode}
	newHome := store.LightHomeIdentity{Path: newMetadata.Path, DeviceID: newMetadata.DeviceID, Inode: newMetadata.Inode}
	_, repository := openLightRuntimeRepository(t)
	provider := metadataProviderFunc(func(_ context.Context, home string) (appserver.ThreadList, error) {
		sessionID := "old"
		if home == newHome.Path {
			sessionID = "new"
		}
		return appserver.ThreadList{Threads: []appserver.ThreadMetadata{{
			SessionID: sessionID, CWD: "/workspace/" + sessionID, CreatedAtMS: 100, UpdatedAtMS: 200,
		}}}, nil
	})
	runtime, err := NewRuntime(RuntimeConfig{Repository: repository, Metadata: provider})
	if err != nil {
		t.Fatal(err)
	}
	oldRun, err := runtime.Start(ctx, oldHome)
	if err != nil {
		t.Fatal(err)
	}
	if err := oldRun.Wait(ctx); err != nil {
		t.Fatal(err)
	}
	newRun, err := runtime.StartHomeSwitch(ctx, oldHome, newHome)
	if err != nil {
		t.Fatalf("StartHomeSwitch() error = %v", err)
	}
	if err := newRun.Wait(ctx); err != nil {
		t.Fatal(err)
	}
	state, err := repository.LightIndexState(ctx)
	if err != nil || state.Home != newHome {
		t.Fatalf("LightIndexState() = %#v, %v", state, err)
	}
	sessions, err := repository.ListLightSessions(ctx)
	if err != nil || len(sessions) != 1 || sessions[0].SessionID != "new" {
		t.Fatalf("ListLightSessions() = %#v, %v", sessions, err)
	}
}

func TestRuntimeDeepIndexesOnlyRequestedSessionWithoutQuotaFacts(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	homePath := t.TempDir()
	rollout := filepath.Join(homePath, "sessions", "deep.jsonl")
	if err := os.MkdirAll(filepath.Dir(rollout), 0o700); err != nil {
		t.Fatal(err)
	}
	content := strings.Join([]string{
		`{"timestamp":"2026-07-14T01:00:00Z","type":"session_meta","payload":{"id":"session-deep","timestamp":"2026-07-14T01:00:00Z","cwd":"/tmp/project","originator":"codex_cli_rs","cli_version":"0.142.3","source":"cli","model_provider":"openai"}}`,
		`{"timestamp":"2026-07-14T01:00:01Z","type":"event_msg","payload":{"type":"task_started","turn_id":"turn-deep","started_at":1783990801,"model_context_window":258000}}`,
		`{"timestamp":"2026-07-14T01:00:02Z","type":"turn_context","payload":{"turn_id":"turn-deep","cwd":"/tmp/project","model":"gpt-5","effort":"high"}}`,
		`{"timestamp":"2026-07-14T01:00:03Z","type":"event_msg","payload":{"type":"token_count","info":null,"rate_limits":{"limit_id":"codex","primary":{"used_percent":38,"window_minutes":300,"resets_at":1784008800},"plan_type":"pro"}}}`,
		`{"timestamp":"2026-07-14T01:00:04Z","type":"event_msg","payload":{"type":"task_complete","turn_id":"turn-deep","completed_at":1783990804}}`,
	}, "\n") + "\n"
	if err := os.WriteFile(rollout, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	homeMetadata, err := logs.NewHomeProbe().Probe(ctx, homePath)
	if err != nil {
		t.Fatal(err)
	}
	path, err := filepath.EvalSymlinks(rollout)
	if err != nil {
		t.Fatal(err)
	}
	_, repository := openLightRuntimeRepository(t)
	provider := metadataProviderFunc(func(context.Context, string) (appserver.ThreadList, error) {
		return appserver.ThreadList{Threads: []appserver.ThreadMetadata{{
			SessionID: "session-deep", CWD: "/tmp/project", RolloutPath: &path,
			CreatedAtMS: 100, UpdatedAtMS: 200,
		}}}, nil
	})
	runtime, err := NewRuntime(RuntimeConfig{Repository: repository, Metadata: provider})
	if err != nil {
		t.Fatal(err)
	}
	home := store.LightHomeIdentity{Path: homeMetadata.Path, DeviceID: homeMetadata.DeviceID, Inode: homeMetadata.Inode}
	run, err := runtime.Start(ctx, home)
	if err != nil {
		t.Fatal(err)
	}
	if err := run.Wait(ctx); err != nil {
		t.Fatal(err)
	}
	result, err := runtime.DeepIndexSession(ctx, "session-deep")
	if err != nil || result.LoadedTurnCount != 1 || result.Reused {
		t.Fatalf("DeepIndexSession() = %#v, %v", result, err)
	}
	detail, err := repository.SessionAnalytics(ctx, store.SessionAnalyticsDetailFilter{SessionID: "session-deep", TurnLimit: 20})
	if err != nil || detail.Mode != store.AnalyticsReadLightIndex || len(detail.Turns) != 1 || detail.Turns[0].TurnID == "" {
		t.Fatalf("SessionAnalytics() = %#v, %v", detail, err)
	}
	sessionID := "session-deep"
	observations, err := repository.ListQuotaObservations(ctx, store.QuotaObservationFilter{SessionID: &sessionID, Limit: 10})
	if err != nil || len(observations) != 0 {
		t.Fatalf("quota observations = %#v, %v", observations, err)
	}
	reused, err := runtime.DeepIndexSession(ctx, "session-deep")
	if err != nil || !reused.Reused {
		t.Fatalf("DeepIndexSession(reuse) = %#v, %v", reused, err)
	}
}

func TestRuntimeResumesCommittedOffsetAfterCancellation(t *testing.T) {
	t.Parallel()

	homePath := t.TempDir()
	rollout := filepath.Join(homePath, "sessions", "resume.jsonl")
	if err := os.MkdirAll(filepath.Dir(rollout), 0o700); err != nil {
		t.Fatal(err)
	}
	content := ""
	for index := int64(1); index <= 120; index++ {
		content += tokenLine("2026-07-19T01:00:00Z", index*10, index, index*2, index) + "\n"
	}
	if err := os.WriteFile(rollout, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	homeMetadata, err := logs.NewHomeProbe().Probe(context.Background(), homePath)
	if err != nil {
		t.Fatal(err)
	}
	canonicalRollout, err := filepath.EvalSymlinks(rollout)
	if err != nil {
		t.Fatal(err)
	}
	_, repository := openLightRuntimeRepository(t)
	path := canonicalRollout
	provider := metadataProviderFunc(func(context.Context, string) (appserver.ThreadList, error) {
		return appserver.ThreadList{Threads: []appserver.ThreadMetadata{{
			SessionID: "resume", CWD: "/workspace", RolloutPath: &path, CreatedAtMS: 100, UpdatedAtMS: 200,
		}}}, nil
	})
	firstCtx, cancelFirst := context.WithCancel(context.Background())
	commits := 0
	firstRuntime, err := NewRuntime(RuntimeConfig{
		Repository: repository, Metadata: provider, ScanBatchBytes: 4_600,
		BatchCommitted: func(store.LightTokenScan) {
			commits++
			cancelFirst()
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	firstRun, err := firstRuntime.Start(firstCtx, store.LightHomeIdentity{
		Path: homeMetadata.Path, DeviceID: homeMetadata.DeviceID, Inode: homeMetadata.Inode,
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := firstRun.Wait(context.Background()); !errors.Is(err, context.Canceled) {
		t.Fatalf("Wait(first) error = %v, want canceled", err)
	}
	pending, err := repository.PendingLightTokenScan(context.Background(), "resume")
	if err != nil || commits != 1 || pending.Checkpoint.DurableOffset <= 0 || pending.Checkpoint.DurableOffset >= pending.Identity.SizeBytes {
		t.Fatalf("pending after cancel = %#v commits=%d err=%v", pending, commits, err)
	}
	resumeOffset := pending.Checkpoint.DurableOffset

	secondRuntime, err := NewRuntime(RuntimeConfig{
		Repository: repository, Metadata: provider, ScanBatchBytes: 4_600,
	})
	if err != nil {
		t.Fatal(err)
	}
	secondRun, err := secondRuntime.Start(context.Background(), store.LightHomeIdentity{
		Path: homeMetadata.Path, DeviceID: homeMetadata.DeviceID, Inode: homeMetadata.Inode,
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := secondRun.Wait(context.Background()); err != nil {
		t.Fatalf("Wait(resume) error = %v", err)
	}
	active, err := repository.ActiveLightTokenScan(context.Background(), "resume")
	if err != nil || active.Generation != pending.Generation || active.Checkpoint.DurableOffset != active.Identity.SizeBytes ||
		active.Checkpoint.InputTokens != 1_200 || active.Checkpoint.PhysicalBytesRead <= resumeOffset {
		t.Fatalf("active after resume = %#v, %v", active, err)
	}
}

func TestRuntimeCoalescesPublishedSessionsIntoOneRefreshNotification(t *testing.T) {
	t.Parallel()

	homePath := t.TempDir()
	sessionsDirectory := filepath.Join(homePath, "sessions")
	if err := os.MkdirAll(sessionsDirectory, 0o700); err != nil {
		t.Fatal(err)
	}
	threads := make([]appserver.ThreadMetadata, 0, 2)
	for index, sessionID := range []string{"coalesced-one", "coalesced-two"} {
		rollout := filepath.Join(sessionsDirectory, sessionID+".jsonl")
		if err := os.WriteFile(
			rollout,
			[]byte(tokenLine("2026-07-24T01:00:00Z", int64(10+index), 2, 1, 0)+"\n"),
			0o600,
		); err != nil {
			t.Fatal(err)
		}
		canonicalRollout, err := filepath.EvalSymlinks(rollout)
		if err != nil {
			t.Fatal(err)
		}
		threads = append(threads, appserver.ThreadMetadata{
			SessionID: sessionID, CWD: "/workspace", RolloutPath: &canonicalRollout,
			CreatedAtMS: 100, UpdatedAtMS: 200,
		})
	}
	homeMetadata, err := logs.NewHomeProbe().Probe(context.Background(), homePath)
	if err != nil {
		t.Fatal(err)
	}
	_, repository := openLightRuntimeRepository(t)
	var countsMu sync.Mutex
	batchCommits := 0
	refreshCommits := 0
	runtime, err := NewRuntime(RuntimeConfig{
		Repository: repository,
		Metadata: metadataProviderFunc(func(context.Context, string) (appserver.ThreadList, error) {
			return appserver.ThreadList{Threads: threads}, nil
		}),
		BatchCommitted: func(store.LightTokenScan) {
			countsMu.Lock()
			batchCommits++
			countsMu.Unlock()
		},
		RefreshCommitted: func() {
			countsMu.Lock()
			refreshCommits++
			countsMu.Unlock()
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	run, err := runtime.Start(context.Background(), store.LightHomeIdentity{
		Path: homeMetadata.Path, DeviceID: homeMetadata.DeviceID, Inode: homeMetadata.Inode,
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := run.Wait(context.Background()); err != nil {
		t.Fatal(err)
	}
	countsMu.Lock()
	defer countsMu.Unlock()
	if batchCommits != 2 || refreshCommits != 1 {
		t.Fatalf("commit notifications = batches:%d refreshes:%d, want 2/1", batchCommits, refreshCommits)
	}
}

func TestRuntimePublishesMetadataOnlyRefreshNotification(t *testing.T) {
	t.Parallel()

	homePath := t.TempDir()
	for _, directory := range []string{"sessions", "archived_sessions"} {
		if err := os.Mkdir(filepath.Join(homePath, directory), 0o700); err != nil {
			t.Fatal(err)
		}
	}
	homeMetadata, err := logs.NewHomeProbe().Probe(context.Background(), homePath)
	if err != nil {
		t.Fatal(err)
	}
	_, repository := openLightRuntimeRepository(t)
	metadataCommits := 0
	refreshCommits := 0
	runtime, err := NewRuntime(RuntimeConfig{
		Repository: repository,
		Metadata: metadataProviderFunc(func(context.Context, string) (appserver.ThreadList, error) {
			title := "只有元数据"
			return appserver.ThreadList{Threads: []appserver.ThreadMetadata{{
				SessionID: "metadata-only", Name: &title, CWD: "/workspace",
				CreatedAtMS: 100, UpdatedAtMS: 200,
			}}}, nil
		}),
		MetadataCommitted: func() { metadataCommits++ },
		RefreshCommitted:  func() { refreshCommits++ },
	})
	if err != nil {
		t.Fatal(err)
	}
	run, err := runtime.Start(context.Background(), store.LightHomeIdentity{
		Path: homeMetadata.Path, DeviceID: homeMetadata.DeviceID, Inode: homeMetadata.Inode,
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := run.Wait(context.Background()); err != nil {
		t.Fatal(err)
	}
	if metadataCommits != 1 || refreshCommits != 1 {
		t.Fatalf(
			"metadata-only notifications = metadata:%d refreshes:%d, want 1/1",
			metadataCommits, refreshCommits,
		)
	}
}

func TestRuntimeMonitorRefreshesTitleAndAppendedTokensFromDurableOffset(t *testing.T) {
	t.Parallel()

	homePath := t.TempDir()
	rollout := filepath.Join(homePath, "sessions", "dynamic.jsonl")
	if err := os.MkdirAll(filepath.Dir(rollout), 0o700); err != nil {
		t.Fatal(err)
	}
	initialContent := strings.Repeat("x", 5_000) + "\n" +
		tokenLine("2026-07-19T01:00:00Z", 100, 20, 10, 2) + "\n"
	if err := os.WriteFile(rollout, []byte(initialContent), 0o600); err != nil {
		t.Fatal(err)
	}
	homeMetadata, err := logs.NewHomeProbe().Probe(context.Background(), homePath)
	if err != nil {
		t.Fatal(err)
	}
	canonicalRollout, err := filepath.EvalSymlinks(rollout)
	if err != nil {
		t.Fatal(err)
	}

	_, repository := openLightRuntimeRepository(t)
	var metadataMu sync.Mutex
	title := "初始标题"
	updatedAtMS := int64(200)
	rolloutPath := canonicalRollout
	provider := metadataProviderFunc(func(context.Context, string) (appserver.ThreadList, error) {
		metadataMu.Lock()
		defer metadataMu.Unlock()
		currentTitle := title
		path := rolloutPath
		return appserver.ThreadList{Threads: []appserver.ThreadMetadata{{
			SessionID: "dynamic", Name: &currentTitle, CWD: "/workspace", RolloutPath: &path,
			CreatedAtMS: 100, UpdatedAtMS: updatedAtMS,
		}}}, nil
	})
	metadataCommits := make(chan struct{}, 4)
	batchCommits := make(chan store.LightTokenScan, 4)
	runtime, err := NewRuntime(RuntimeConfig{
		Repository: repository, Metadata: provider, RefreshInterval: time.Hour,
		MetadataCommitted: func() { metadataCommits <- struct{}{} },
		BatchCommitted:    func(scan store.LightTokenScan) { batchCommits <- scan },
	})
	if err != nil {
		t.Fatal(err)
	}
	runContext, cancelRun := context.WithCancel(context.Background())
	run, err := runtime.Start(runContext, store.LightHomeIdentity{
		Path: homeMetadata.Path, DeviceID: homeMetadata.DeviceID, Inode: homeMetadata.Inode,
	})
	if err != nil {
		cancelRun()
		t.Fatal(err)
	}
	waitLightRuntimeSignal(t, metadataCommits, "initial metadata commit")
	initialScan := waitLightRuntimeSignal(t, batchCommits, "initial token scan")
	initialState, err := repository.LightIndexState(context.Background())
	if err != nil {
		cancelRun()
		t.Fatal(err)
	}

	metadataMu.Lock()
	title = "动态标题"
	updatedAtMS = 300
	metadataMu.Unlock()
	appendLine := tokenLine("2026-07-19T01:01:00Z", 130, 30, 15, 4) + "\n"
	file, err := os.OpenFile(rollout, os.O_WRONLY|os.O_APPEND, 0)
	if err != nil {
		cancelRun()
		t.Fatal(err)
	}
	if _, err := file.WriteString(appendLine); err != nil {
		_ = file.Close()
		cancelRun()
		t.Fatal(err)
	}
	if err := file.Close(); err != nil {
		cancelRun()
		t.Fatal(err)
	}
	if !run.Trigger() {
		cancelRun()
		t.Fatal("monitored run rejected refresh trigger")
	}
	waitLightRuntimeSignal(t, metadataCommits, "updated metadata commit")
	updatedScan := waitLightRuntimeSignal(t, batchCommits, "appended token scan")

	sessions, err := repository.ListLightSessions(context.Background())
	if err != nil || len(sessions) != 1 || sessions[0].ThreadName == nil || *sessions[0].ThreadName != "动态标题" {
		cancelRun()
		t.Fatalf("refreshed sessions = %#v, %v", sessions, err)
	}
	updatedState, err := repository.LightIndexState(context.Background())
	if err != nil || updatedState.MetadataGeneration != initialState.MetadataGeneration+1 {
		cancelRun()
		t.Fatalf("metadata state = %#v, %v", updatedState, err)
	}
	fileInfo, err := os.Stat(rollout)
	if err != nil {
		cancelRun()
		t.Fatal(err)
	}
	physicalDelta := updatedScan.Checkpoint.PhysicalBytesRead - initialScan.Checkpoint.PhysicalBytesRead
	if updatedScan.Generation != initialScan.Generation ||
		updatedScan.Checkpoint.DurableOffset != fileInfo.Size() ||
		updatedScan.Checkpoint.InputTokens != 130 || updatedScan.Checkpoint.JSONDecoded != 2 ||
		physicalDelta <= int64(len(appendLine)) || physicalDelta >= int64(len(initialContent)) {
		cancelRun()
		t.Fatalf(
			"append refresh initial=%#v updated=%#v physical_delta=%d file_size=%d",
			initialScan, updatedScan, physicalDelta, fileInfo.Size(),
		)
	}
	archivedRollout := filepath.Join(homePath, "archived_sessions", "dynamic.jsonl")
	if err := os.MkdirAll(filepath.Dir(archivedRollout), 0o700); err != nil {
		cancelRun()
		t.Fatal(err)
	}
	if err := os.Rename(rollout, archivedRollout); err != nil {
		cancelRun()
		t.Fatal(err)
	}
	canonicalArchived, err := filepath.EvalSymlinks(archivedRollout)
	if err != nil {
		cancelRun()
		t.Fatal(err)
	}
	metadataMu.Lock()
	rolloutPath = canonicalArchived
	updatedAtMS = 400
	metadataMu.Unlock()
	if !run.Trigger() {
		cancelRun()
		t.Fatal("monitored run rejected archived-path refresh trigger")
	}
	waitLightRuntimeSignal(t, metadataCommits, "archived metadata commit")
	archivedScan := waitLightRuntimeSignal(t, batchCommits, "archived rollout rebuild")
	if archivedScan.Generation != updatedScan.Generation+1 || archivedScan.Identity.Path != canonicalArchived ||
		archivedScan.Checkpoint.InputTokens != 130 || archivedScan.Checkpoint.DurableOffset != fileInfo.Size() {
		cancelRun()
		t.Fatalf("archived rollout scan = %#v, previous=%#v", archivedScan, updatedScan)
	}

	cancelRun()
	if err := run.Wait(context.Background()); !errors.Is(err, context.Canceled) {
		t.Fatalf("Wait(cancel monitor) error = %v, want context.Canceled", err)
	}
}

func TestRuntimeMonitorAppendsShortRolloutUsingPreviousPrefixProof(t *testing.T) {
	t.Parallel()

	homePath := t.TempDir()
	rollout := filepath.Join(homePath, "sessions", "short.jsonl")
	if err := os.MkdirAll(filepath.Dir(rollout), 0o700); err != nil {
		t.Fatal(err)
	}
	initialContent := tokenLine("2026-07-19T01:00:00Z", 10, 2, 1, 0) + "\n"
	if err := os.WriteFile(rollout, []byte(initialContent), 0o600); err != nil {
		t.Fatal(err)
	}
	homeMetadata, err := logs.NewHomeProbe().Probe(context.Background(), homePath)
	if err != nil {
		t.Fatal(err)
	}
	canonicalRollout, err := filepath.EvalSymlinks(rollout)
	if err != nil {
		t.Fatal(err)
	}
	_, repository := openLightRuntimeRepository(t)
	provider := metadataProviderFunc(func(context.Context, string) (appserver.ThreadList, error) {
		path := canonicalRollout
		return appserver.ThreadList{Threads: []appserver.ThreadMetadata{{
			SessionID: "short", CWD: "/workspace", RolloutPath: &path, CreatedAtMS: 100, UpdatedAtMS: 200,
		}}}, nil
	})
	commits := make(chan store.LightTokenScan, 3)
	runtime, err := NewRuntime(RuntimeConfig{
		Repository: repository, Metadata: provider, RefreshInterval: time.Hour,
		BatchCommitted: func(scan store.LightTokenScan) { commits <- scan },
	})
	if err != nil {
		t.Fatal(err)
	}
	runContext, cancelRun := context.WithCancel(context.Background())
	run, err := runtime.Start(runContext, store.LightHomeIdentity{
		Path: homeMetadata.Path, DeviceID: homeMetadata.DeviceID, Inode: homeMetadata.Inode,
	})
	if err != nil {
		cancelRun()
		t.Fatal(err)
	}
	initialScan := waitLightRuntimeSignal(t, commits, "initial short rollout scan")
	appendLine := tokenLine("2026-07-19T01:01:00Z", 20, 4, 2, 1) + "\n"
	file, err := os.OpenFile(rollout, os.O_WRONLY|os.O_APPEND, 0)
	if err != nil {
		cancelRun()
		t.Fatal(err)
	}
	if _, err := file.WriteString(appendLine); err != nil {
		_ = file.Close()
		cancelRun()
		t.Fatal(err)
	}
	if err := file.Close(); err != nil {
		cancelRun()
		t.Fatal(err)
	}
	if !run.Trigger() {
		cancelRun()
		t.Fatal("short rollout monitor rejected refresh trigger")
	}
	updatedScan := waitLightRuntimeSignal(t, commits, "short rollout append")
	if updatedScan.Generation != initialScan.Generation || updatedScan.Checkpoint.JSONDecoded != 2 ||
		updatedScan.Checkpoint.InputTokens != 20 || updatedScan.Checkpoint.DurableOffset != int64(len(initialContent)+len(appendLine)) {
		cancelRun()
		t.Fatalf("short rollout initial=%#v updated=%#v", initialScan, updatedScan)
	}
	cancelRun()
	if err := run.Wait(context.Background()); !errors.Is(err, context.Canceled) {
		t.Fatalf("Wait(cancel short monitor) error = %v, want context.Canceled", err)
	}
}

func TestRuntimeMonitorPeriodicRefreshRecoversAfterMetadataFailure(t *testing.T) {
	t.Parallel()

	homePath := t.TempDir()
	for _, directory := range []string{"sessions", "archived_sessions"} {
		if err := os.Mkdir(filepath.Join(homePath, directory), 0o700); err != nil {
			t.Fatal(err)
		}
	}
	homeMetadata, err := logs.NewHomeProbe().Probe(context.Background(), homePath)
	if err != nil {
		t.Fatal(err)
	}
	_, repository := openLightRuntimeRepository(t)
	transient := errors.New("transient metadata failure")
	var metadataMu sync.Mutex
	title := "初始标题"
	calls := 0
	provider := metadataProviderFunc(func(context.Context, string) (appserver.ThreadList, error) {
		metadataMu.Lock()
		defer metadataMu.Unlock()
		calls++
		if calls == 2 {
			return appserver.ThreadList{}, transient
		}
		currentTitle := title
		return appserver.ThreadList{Threads: []appserver.ThreadMetadata{{
			SessionID: "periodic", Name: &currentTitle, CWD: "/workspace",
			CreatedAtMS: 100, UpdatedAtMS: int64(calls) * 100,
		}}}, nil
	})
	metadataCommits := make(chan struct{}, 4)
	refreshFailures := make(chan error, 4)
	runtime, err := NewRuntime(RuntimeConfig{
		Repository: repository, Metadata: provider, RefreshInterval: 10 * time.Millisecond,
		MetadataCommitted: func() { metadataCommits <- struct{}{} },
		RefreshFailed:     func(err error) { refreshFailures <- err },
	})
	if err != nil {
		t.Fatal(err)
	}
	runContext, cancelRun := context.WithCancel(context.Background())
	run, err := runtime.Start(runContext, store.LightHomeIdentity{
		Path: homeMetadata.Path, DeviceID: homeMetadata.DeviceID, Inode: homeMetadata.Inode,
	})
	if err != nil {
		cancelRun()
		t.Fatal(err)
	}
	waitLightRuntimeSignal(t, metadataCommits, "initial periodic metadata")
	metadataMu.Lock()
	title = "周期标题"
	metadataMu.Unlock()
	if failure := waitLightRuntimeSignal(t, refreshFailures, "transient refresh failure"); !errors.Is(failure, transient) {
		cancelRun()
		t.Fatalf("refresh failure = %v, want transient error", failure)
	}
	waitLightRuntimeSignal(t, metadataCommits, "recovered periodic metadata")
	sessions, err := repository.ListLightSessions(context.Background())
	if err != nil || len(sessions) != 1 || sessions[0].ThreadName == nil || *sessions[0].ThreadName != "周期标题" {
		cancelRun()
		t.Fatalf("periodic sessions = %#v, %v", sessions, err)
	}
	cancelRun()
	if err := run.Wait(context.Background()); !errors.Is(err, context.Canceled) {
		t.Fatalf("Wait(cancel periodic monitor) error = %v, want context.Canceled", err)
	}
}

func waitLightRuntimeSignal[T any](t *testing.T, values <-chan T, name string) T {
	t.Helper()
	select {
	case value := <-values:
		return value
	case <-time.After(3 * time.Second):
		t.Fatalf("timed out waiting for %s", name)
		var zero T
		return zero
	}
}

type metadataProviderFunc func(context.Context, string) (appserver.ThreadList, error)

func (provider metadataProviderFunc) List(ctx context.Context, home string) (appserver.ThreadList, error) {
	return provider(ctx, home)
}

func openLightRuntimeRepository(t *testing.T) (*storesqlite.Store, *store.Repository) {
	t.Helper()
	directory := t.TempDir()
	if err := os.Chmod(directory, 0o700); err != nil {
		t.Fatal(err)
	}
	database, err := storesqlite.Open(context.Background(), storesqlite.Config{Path: filepath.Join(directory, "light.db")})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = database.Close(context.Background()) })
	repository := store.NewRepository(database)
	if _, err := repository.MigrateApplicationSchema(context.Background()); err != nil {
		t.Fatal(err)
	}
	return database, repository
}

func assertLightRuntimeDidNotWriteDeepFacts(t *testing.T, database *storesqlite.Store) {
	t.Helper()
	if err := database.View(context.Background(), func(ctx context.Context, connection *gorm.DB) error {
		for _, table := range []string{"source_generation_batches", "quota_observation_receipts"} {
			var count int64
			if err := connection.WithContext(ctx).Table(table).Count(&count).Error; err != nil {
				return err
			}
			if count != 0 {
				t.Errorf("%s rows = %d, want 0", table, count)
			}
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
}
