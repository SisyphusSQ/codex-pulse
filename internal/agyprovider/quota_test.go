package agyprovider

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/SisyphusSQ/codex-pulse/internal/store"
)

func floatPtr(v float64) *float64 { return &v }

func TestLSDiscoveryHubPriorityCorruptIgnoreRedirectAndProcessFallback(t *testing.T) {
	t.Parallel()

	const expectedCSRF = "csrf-secret-token-xyz"

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("x-codeium-csrf-token") != expectedCSRF {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		if r.URL.Path == lsServicePath+lsMethodGetUserStatus {
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"userStatus":{"email":"dev@example.com"}}`))
			return
		}
		w.WriteHeader(http.StatusNotFound)
	}))
	defer srv.Close()

	parsedURL, err := url.Parse(srv.URL)
	if err != nil {
		t.Fatalf("url.Parse: %v", err)
	}
	port, err := strconv.Atoi(parsedURL.Port())
	if err != nil {
		t.Fatalf("strconv.Atoi port: %v", err)
	}

	daemonDir := t.TempDir()
	// 坏 JSON 文件必须被安全忽略
	if err := os.WriteFile(filepath.Join(daemonDir, "hub_corrupt.json"), []byte("{not-json"), 0o644); err != nil {
		t.Fatalf("write corrupt hub: %v", err)
	}
	// 死 PID 文件必须被跳过
	if err := os.WriteFile(filepath.Join(daemonDir, "hub_dead.json"), []byte(`{"pid":99999,"httpPort":12345,"csrfToken":"dead"}`), 0o644); err != nil {
		t.Fatalf("write dead hub: %v", err)
	}
	// 合法存活 hub_*.json
	validHub := `{"pid":4242,"httpPort":` + strconv.Itoa(port) + `,"csrfToken":"` + expectedCSRF + `"}`
	if err := os.WriteFile(filepath.Join(daemonDir, "hub_live.json"), []byte(validHub), 0o644); err != nil {
		t.Fatalf("write live hub: %v", err)
	}

	disc := &LSDiscoverer{
		DaemonDirs: []string{daemonDir},
		HTTPClient: newLoopbackHTTPClient(2 * time.Second),
		IsPIDAlive: func(pid int) bool { return pid == 4242 },
		ListProcesses: func(context.Context) ([]osProcess, error) {
			t.Fatal("process fallback should not run when hub_*.json succeeds")
			return nil, nil
		},
	}
	ep, err := disc.Discover(context.Background())
	if err != nil {
		t.Fatalf("Discover(hub): %v", err)
	}
	if ep.PID != 4242 || ep.CSRFToken != expectedCSRF || !isStrictLoopbackURL(ep.BaseURL) {
		t.Fatalf("unexpected endpoint: %+v", ep)
	}

	// 进程回退发现测试
	discFallback := &LSDiscoverer{
		DaemonDirs: []string{t.TempDir()},
		HTTPClient: newLoopbackHTTPClient(2 * time.Second),
		ListProcesses: func(context.Context) ([]osProcess, error) {
			return []osProcess{{
				PID:         7777,
				CommandLine: "/usr/local/bin/agy --hub --csrf_token=" + expectedCSRF,
			}}, nil
		},
		ListListeningPorts: func(_ context.Context, pid int) ([]int, error) {
			if pid == 7777 {
				return []int{port}, nil
			}
			return nil, nil
		},
	}
	ep2, err := discFallback.Discover(context.Background())
	if err != nil || ep2.PID != 7777 {
		t.Fatalf("Discover(process fallback) = %+v, %v", ep2, err)
	}

	// 严格 loopback 校验与重定向拦截
	if isStrictLoopbackURL("https://cloudcode-pa.googleapis.com") || isStrictLoopbackURL("http://192.168.1.5:8080") {
		t.Fatal("non-loopback URL must be rejected")
	}
	redirectSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "https://example.com", http.StatusFound)
	}))
	defer redirectSrv.Close()
	client := NewLSClient(nil, nil)
	client.endpoint = &LSEndpoint{BaseURL: redirectSrv.URL, CSRFToken: "tok"}
	if _, err := client.GetUserStatus(context.Background()); !errors.Is(err, ErrLSRedirectBlocked) {
		t.Fatalf("expected ErrLSRedirectBlocked, got %v", err)
	}
	// 非白名单轨迹方法必须拒绝
	if _, err := client.call(context.Background(), "GetCascadeTrajectory", map[string]any{}); !errors.Is(err, ErrLSMethodRejected) {
		t.Fatalf("expected ErrLSMethodRejected, got %v", err)
	}
}

func TestNormalizeOfficialQuotaPoolsUltraProAndProto3Zero(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 11, 25, 12, 0, 0, 0, time.UTC)
	nowMS := now.UnixMilli()
	reset5h := now.Add(3 * time.Hour).Format(time.RFC3339)
	resetWeekly := now.Add(5 * 24 * time.Hour).Format(time.RFC3339)
	expiredReset := now.Add(-time.Hour).Format(time.RFC3339)

	// 1. Pro 账号：独立 Argon 组、Proto3 零值省略耗尽（未来 resetTime -> 100% used）、
	// 已过期省略桶不得判 0% 剩余、clientModelConfigs 推断 Carbon 独立池 + Claude & GPT 池
	summary := LSQuotaSummary{
		Groups: []LSQuotaGroup{
			{
				DisplayName: "Gemini Models",
				Buckets: []LSQuotaBucket{
					{BucketID: "gemini-5h", Window: "5h", RemainingFraction: floatPtr(0.65), ResetTime: reset5h},
					{BucketID: "gemini-weekly", Window: "weekly", RemainingFraction: nil, ResetTime: resetWeekly}, // Proto3 0.0 耗尽
					{BucketID: "gemini-expired-zero", Window: "weekly", RemainingFraction: nil, ResetTime: expiredReset},
				},
			},
			{
				DisplayName: "Gemini 4 Argon",
				Buckets: []LSQuotaBucket{
					{BucketID: "argon-weekly", Window: "weekly", RemainingFraction: floatPtr(0.40), ResetTime: resetWeekly},
				},
			},
			{
				DisplayName: "Third-Party Partner Models",
				Description: "Claude and GPT models, separate from Gemini",
				Buckets: []LSQuotaBucket{
					{BucketID: "claude-5h", Window: "5h", RemainingFraction: floatPtr(0.80), ResetTime: reset5h},
				},
			},
		},
	}
	clientConfigs := []LSClientModelConfig{
		{
			Label:        "Gemini 4 Carbon (High)",
			ModelOrAlias: &LSModelOrAlias{Model: "MODEL_PLACEHOLDER_M429"},
			QuotaInfo:    &LSQuotaInfo{RemainingFraction: floatPtr(0.25), ResetTime: resetWeekly},
		},
		{
			Label:        "Gemini 3.8 Flash",
			ModelOrAlias: &LSModelOrAlias{Model: "MODEL_PLACEHOLDER_M318"},
			QuotaInfo:    &LSQuotaInfo{RemainingFraction: floatPtr(0.65), ResetTime: reset5h},
		},
	}

	windows := NormalizeOfficialQuotaPools(summary, clientConfigs, nowMS)
	byKey := make(map[string]NormalizedQuotaWindow, len(windows))
	for _, w := range windows {
		byKey[w.LimitID+":"+w.WindowKind] = w
	}

	if g5 := byKey[AGYLimitGemini+":primary"]; g5.UsedPercent != 35 || g5.WindowMinutes != 300 {
		t.Fatalf("unexpected gemini 5h: %+v", g5)
	}
	if gw := byKey[AGYLimitGemini+":secondary"]; gw.UsedPercent != 100 || gw.WindowMinutes != 10080 {
		t.Fatalf("expected Proto3 omitted future reset to be 100%% used, got %+v", gw)
	}
	if aw := byKey[AGYLimitArgon+":secondary"]; aw.UsedPercent != 60 {
		t.Fatalf("unexpected argon weekly: %+v", aw)
	}
	if cw := byKey[AGYLimitCarbon+":secondary"]; cw.UsedPercent != 75 {
		t.Fatalf("expected inferred Pro Carbon weekly pool 75%% used, got %+v", cw)
	}
	if c5 := byKey[AGYLimitClaudeGPT+":primary"]; c5.UsedPercent != 20 {
		t.Fatalf("unexpected claude_gpt 5h: %+v", c5)
	}

	// 2. Ultra 共享池：Carbon 与标准 Gemini 比例一致，不拆出伪独立 Carbon 池
	ultraConfigs := []LSClientModelConfig{
		{
			Label:        "Gemini 4 Carbon (High)",
			ModelOrAlias: &LSModelOrAlias{Model: "MODEL_PLACEHOLDER_M429"},
			QuotaInfo:    &LSQuotaInfo{RemainingFraction: floatPtr(0.65), ResetTime: reset5h},
		},
	}
	ultraSummary := LSQuotaSummary{
		Groups: []LSQuotaGroup{{
			DisplayName: "Gemini Models",
			Buckets:     []LSQuotaBucket{{BucketID: "gemini-5h", Window: "5h", RemainingFraction: floatPtr(0.65), ResetTime: reset5h}},
		}},
	}
	ultraWins := NormalizeOfficialQuotaPools(ultraSummary, ultraConfigs, nowMS)
	for _, w := range ultraWins {
		if w.LimitID == AGYLimitCarbon || w.LimitID == AGYLimitArgon {
			t.Fatalf("Ultra shared pool should not synthesize separate %s pool", w.LimitID)
		}
	}
}

func TestAccountSandwichOfflineStaleExpiryOnlineToggleAndPrivacy(t *testing.T) {
	t.Parallel()

	repo := openPulseRepo(t)
	now := time.Date(2026, 11, 25, 14, 0, 0, 0, time.UTC)
	reset5h := now.Add(2 * time.Hour)
	resetWeekly := now.Add(4 * 24 * time.Hour)

	const secretCSRF = "SUPER_SECRET_CSRF_TOKEN_999"
	const userEmail = "alice.secret@example.com"

	var statusCalls atomic.Int32
	var rpcTotal atomic.Int32
	var mismatchMode atomic.Bool
	var offlineMode atomic.Bool

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		rpcTotal.Add(1)
		if offlineMode.Load() {
			w.WriteHeader(http.StatusServiceUnavailable)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case lsServicePath + lsMethodGetUserStatus:
			call := statusCalls.Add(1)
			email := userEmail
			if mismatchMode.Load() && call%2 == 0 {
				email = "bob.switched@example.com"
			}
			_, _ = w.Write([]byte(`{"userStatus":{"email":"` + email + `","planStatus":{"planInfo":{"planName":"Google AI Ultra 20x"}}}}`))
		case lsServicePath + lsMethodRetrieveQuota:
			_, _ = w.Write([]byte(`{"groups":[{"displayName":"Gemini Models","buckets":[{"bucketId":"g5","window":"5h","remainingFraction":0.70,"resetTime":"` + reset5h.Format(time.RFC3339) + `"},{"bucketId":"gw","window":"weekly","remainingFraction":0.50,"resetTime":"` + resetWeekly.Format(time.RFC3339) + `"}]}]}`))
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer srv.Close()

	lsClient := NewLSClient(nil, newLoopbackHTTPClient(2*time.Second))
	lsClient.endpoint = &LSEndpoint{BaseURL: srv.URL, CSRFToken: secretCSRF}

	var onlineEnabled atomic.Bool
	onlineEnabled.Store(true)
	clock := now

	qCollector, err := NewQuotaCollector(lsClient, repo, QuotaCollectorConfig{
		MinimumRefresh: 0,
		Now:            func() time.Time { return clock },
		Enabled:        func() bool { return onlineEnabled.Load() },
	})
	if err != nil {
		t.Fatalf("NewQuotaCollector: %v", err)
	}
	localCollector, err := NewCollector(repo, Config{
		BaseRoot:       t.TempDir(),
		MinimumRefresh: time.Hour,
		Now:            func() time.Time { return clock },
	})
	if err != nil {
		t.Fatalf("NewCollector: %v", err)
	}
	service, err := NewQueryService(localCollector, repo)
	if err != nil {
		t.Fatalf("NewQueryService: %v", err)
	}
	service.BindQuota(qCollector, repo)

	// 1. 账号夹心切号竞态：必须丢弃脏观测，绝不落盘
	mismatchMode.Store(true)
	if err := service.RefreshQuota(context.Background()); !errors.Is(err, ErrAccountMismatch) {
		t.Fatalf("expected ErrAccountMismatch during account switch race, got %v", err)
	}
	snapsAfterMismatch, err := repo.AGYQuotaSnapshots(context.Background(), "")
	if err != nil || len(snapsAfterMismatch) != 0 {
		t.Fatalf("dirty observation must not persist: len=%d err=%v", len(snapsAfterMismatch), err)
	}

	// 2. 正常采集 + 隐私断言（无明文邮箱、无 CSRF token）
	mismatchMode.Store(false)
	if err := service.RefreshQuota(context.Background()); err != nil {
		t.Fatalf("RefreshQuota: %v", err)
	}
	snaps, err := repo.AGYQuotaSnapshots(context.Background(), "")
	if err != nil || len(snaps) != 2 {
		t.Fatalf("expected 2 persisted windows, got len=%d err=%v", len(snaps), err)
	}
	rawSnapJSON, _ := json.Marshal(snaps)
	if strings.Contains(string(rawSnapJSON), userEmail) || strings.Contains(string(rawSnapJSON), secretCSRF) {
		t.Fatalf("privacy leak in persisted agy_quota_snapshots: %s", string(rawSnapJSON))
	}

	// AccountSnapshot 正常返回即时 email 与标准套餐名
	acct, err := service.ReadAccountSnapshot(context.Background())
	if err != nil || acct.Email != userEmail || acct.PlanType != "Google AI Ultra 20×" {
		t.Fatalf("unexpected AccountSnapshot: %+v err=%v", acct, err)
	}

	// QuotaCurrent & QuotaPace 正常返回
	qCur, err := service.QuotaCurrent(context.Background(), now.UnixMilli())
	if err != nil || len(qCur.Current.Windows) != 2 || qCur.Current.Windows[0].Freshness != store.QuotaCurrentFresh ||
		*qCur.Current.Windows[0].UsedPercent != 30 {
		t.Fatalf("unexpected QuotaCurrent: %+v err=%v", qCur, err)
	}
	qPace, err := service.QuotaPace(context.Background(), now.UnixMilli())
	if err != nil || len(qPace.Pace.Windows) != 2 {
		t.Fatalf("unexpected QuotaPace: %+v err=%v", qPace, err)
	}

	// 3. 离线/RPC 失败：未过期保留 last-known 并标 stale，跨过 5h resetTime 变为 expired_unknown
	offlineMode.Store(true)
	_ = service.RefreshQuota(context.Background())
	qStale, err := service.QuotaCurrent(context.Background(), now.Add(30*time.Minute).UnixMilli())
	if err != nil || qStale.Current.Windows[0].Freshness != store.QuotaCurrentStale || *qStale.Current.Windows[0].UsedPercent != 30 {
		t.Fatalf("expected stale last-known before reset, got %+v err=%v", qStale, err)
	}
	// 跨过 5h resetTime，但 weekly 未过期
	qExpired5h, err := service.QuotaCurrent(context.Background(), reset5h.Add(time.Minute).UnixMilli())
	if err != nil {
		t.Fatalf("QuotaCurrent after 5h reset: %v", err)
	}
	if qExpired5h.Current.Windows[0].Freshness != store.QuotaCurrentExpiredUnknown || qExpired5h.Current.Windows[0].UsedPercent != nil {
		t.Fatalf("expired 5h window must become unknown, got %+v", qExpired5h.Current.Windows[0])
	}
	if qExpired5h.Current.Windows[1].Freshness != store.QuotaCurrentStale || *qExpired5h.Current.Windows[1].UsedPercent != 50 {
		t.Fatalf("unexpired weekly window must remain stale 50%%, got %+v", qExpired5h.Current.Windows[1])
	}

	// 4. 关闭 agy_online_enabled：完全停止所有 LS RPC 请求
	onlineEnabled.Store(false)
	offlineMode.Store(false)
	beforeCalls := rpcTotal.Load()
	res := qCollector.RefreshIfDue(context.Background(), true)
	if !res.Disabled {
		t.Fatalf("expected Disabled=true when agy_online_enabled=false, got %+v", res)
	}
	if _, err := service.ReadAccountSnapshot(context.Background()); err != nil {
		t.Fatalf("ReadAccountSnapshot when disabled: %v", err)
	}
	if rpcTotal.Load() != beforeCalls {
		t.Fatalf("LS RPC must not be called when agy_online_enabled=false: before=%d after=%d", beforeCalls, rpcTotal.Load())
	}
}

func TestQuotaCollectorForceRefreshAndAutoInterval(t *testing.T) {
	t.Parallel()

	if DefaultQuotaMinRefresh != 5*time.Minute {
		t.Fatalf("DefaultQuotaMinRefresh = %v, want 5m", DefaultQuotaMinRefresh)
	}

	repo := openPulseRepo(t)
	now := time.Date(2026, 11, 26, 10, 0, 0, 0, time.UTC)
	reset5h := now.Add(3 * time.Hour)

	var recordedForce []bool
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case lsServicePath + lsMethodGetUserStatus:
			_, _ = w.Write([]byte(`{"userStatus":{"email":"user@example.com","planStatus":{"planInfo":{"planName":"Google AI Pro"}}}}`))
		case lsServicePath + lsMethodRetrieveQuota:
			var reqBody struct {
				ForceRefresh bool `json:"forceRefresh"`
			}
			if err := json.NewDecoder(r.Body).Decode(&reqBody); err != nil {
				t.Errorf("decode RetrieveUserQuotaSummary body: %v", err)
			}
			recordedForce = append(recordedForce, reqBody.ForceRefresh)
			_, _ = w.Write([]byte(`{"groups":[{"displayName":"Gemini Models","buckets":[{"bucketId":"g5","window":"5h","remainingFraction":0.80,"resetTime":"` + reset5h.Format(time.RFC3339) + `"}]}]}`))
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer srv.Close()

	lsClient := NewLSClient(nil, newLoopbackHTTPClient(2*time.Second))
	lsClient.endpoint = &LSEndpoint{BaseURL: srv.URL, CSRFToken: "tok"}

	clock := now
	qCollector, err := NewQuotaCollector(lsClient, repo, QuotaCollectorConfig{
		MinimumRefresh: DefaultQuotaMinRefresh,
		Now:            func() time.Time { return clock },
		Enabled:        func() bool { return true },
	})
	if err != nil {
		t.Fatalf("NewQuotaCollector: %v", err)
	}

	// 1. 首次自动刷新：执行 RPC，且必须传 forceRefresh=false
	res1 := qCollector.RefreshIfDue(context.Background(), false)
	if !res1.Attempted || res1.Err != nil {
		t.Fatalf("first auto refresh = %+v, want attempted without error", res1)
	}
	if len(recordedForce) != 1 || recordedForce[0] {
		t.Fatalf("first auto refresh forceRefresh = %v, want [false]", recordedForce)
	}

	// 2. 5m 到期前再次自动刷新：必须节流跳过，不发 RPC
	clock = clock.Add(5*time.Minute - time.Millisecond)
	res2 := qCollector.RefreshIfDue(context.Background(), false)
	if res2.Attempted || res2.Err != nil {
		t.Fatalf("throttled auto refresh = %+v, want not attempted", res2)
	}
	if len(recordedForce) != 1 {
		t.Fatalf("throttled auto refresh should not call RPC: recorded=%v", recordedForce)
	}

	// 3. 间隔内显式手动刷新（force=true）：绕过节流，且传 forceRefresh=true
	res3 := qCollector.RefreshIfDue(context.Background(), true)
	if !res3.Attempted || res3.Err != nil {
		t.Fatalf("manual refresh = %+v, want attempted without error", res3)
	}
	if len(recordedForce) != 2 || !recordedForce[1] {
		t.Fatalf("manual refresh forceRefresh = %v, want [false true]", recordedForce)
	}

	// 4. 手动刷新后满 5m 再次自动刷新：重新触发，且传 forceRefresh=false
	clock = clock.Add(DefaultQuotaMinRefresh)
	res4 := qCollector.RefreshIfDue(context.Background(), false)
	if !res4.Attempted || res4.Err != nil {
		t.Fatalf("due auto refresh = %+v, want attempted without error", res4)
	}
	if len(recordedForce) != 3 || recordedForce[2] {
		t.Fatalf("due auto refresh forceRefresh = %v, want [false true false]", recordedForce)
	}
}

func TestLSPlanTierIdentityAndUnknownMultiplier(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct{ name, plan, tierName, tierID, want string }{
		{"ultra lite ID", "Pro", "Google AI Ultra", "g1-ultra-lite-tier", "Google AI Ultra 5×"},
		{"ultra ID", "Pro", "Google AI Ultra", "g1-ultra-tier", "Google AI Ultra 20×"},
		{"unknown ultra multiplier", "Pro", "Google AI Ultra", "", "Google AI Ultra"},
		{"unrecognized ultra ID", "Pro", "Google AI Ultra", "future-ultra-tier", "Google AI Ultra"},
		{"explicit multiplier", "Google AI Ultra 5x", "", "", "Google AI Ultra 5×"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				if r.URL.Path == lsServicePath+lsMethodGetUserStatus {
					_ = json.NewEncoder(w).Encode(map[string]any{"userStatus": map[string]any{
						"email": "test@example.com", "planStatus": map[string]any{"planInfo": map[string]any{"planName": tc.plan}},
						"userTier": map[string]any{"name": tc.tierName, "id": tc.tierID},
					}})
				} else {
					_, _ = w.Write([]byte(`{}`))
				}
			}))
			defer srv.Close()
			client := NewLSClient(nil, newLoopbackHTTPClient(time.Second))
			client.endpoint = &LSEndpoint{BaseURL: srv.URL, CSRFToken: "test"}
			result, err := client.FetchAccountAndQuota(context.Background(), false)
			if err != nil {
				t.Fatal(err)
			}
			if result.PlanType != tc.want {
				t.Fatalf("plan = %q, want %q", result.PlanType, tc.want)
			}
		})
	}
}
