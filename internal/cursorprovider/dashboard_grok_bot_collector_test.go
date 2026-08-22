package cursorprovider

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/SisyphusSQ/codex-pulse/internal/store"
)

type grokBotClientFixture struct {
	status GrokBotUsageStatus
	err    error
}

func (fixture *grokBotClientFixture) GetSandUsageStatus(context.Context) (GrokBotUsageStatus, error) {
	return fixture.status, fixture.err
}

type grokBotSnapshotCapture struct {
	mu       sync.Mutex
	commits  []store.CursorGrokBotCommit
	failures []string
}

func (capture *grokBotSnapshotCapture) CommitCursorGrokBotObservation(_ context.Context, commit store.CursorGrokBotCommit) error {
	capture.mu.Lock()
	defer capture.mu.Unlock()
	capture.commits = append(capture.commits, commit)
	return nil
}

func (capture *grokBotSnapshotCapture) RecordCursorGrokBotFailure(_ context.Context, _ int64, failureCode string) error {
	capture.mu.Lock()
	defer capture.mu.Unlock()
	capture.failures = append(capture.failures, failureCode)
	return nil
}

func TestGrokBotCollectorCommitsIncludedPercentOnOfficialWeeklyCycle(t *testing.T) {
	t.Parallel()
	percent := 0.0
	client := &grokBotClientFixture{status: GrokBotUsageStatus{
		Included: true, UsagePercent: &percent,
		PeriodStartMS: 1_000, NextResetAtMS: 9_000,
	}}
	capture := &grokBotSnapshotCapture{}
	collector, err := NewGrokBotCollector(client, capture, GrokBotCollectorConfig{
		MinimumRefresh: 0,
		Now:            func() time.Time { return time.UnixMilli(5_000) },
	})
	if err != nil {
		t.Fatalf("NewGrokBotCollector() error = %v", err)
	}
	if err := collector.Refresh(context.Background()); err != nil {
		t.Fatalf("Refresh() error = %v", err)
	}
	if len(capture.commits) != 1 || len(capture.failures) != 0 {
		t.Fatalf("writes = commits:%d failures:%v", len(capture.commits), capture.failures)
	}
	commit := capture.commits[0]
	if !commit.Included || commit.UsedPercent == nil || *commit.UsedPercent != 0 ||
		commit.CycleStartAtMS != 1_000 || commit.CycleEndAtMS != 9_000 || commit.CycleEndAtMS-commit.CycleStartAtMS == 10_080*60_000 {
		t.Fatalf("grok bot commit = %#v", commit)
	}
}

func TestGrokBotCollectorTreatsNotApplicableAsSuccessWithoutZeroPercent(t *testing.T) {
	t.Parallel()
	client := &grokBotClientFixture{status: GrokBotUsageStatus{
		Included: false, PeriodStartMS: 1_000, NextResetAtMS: 9_000,
	}}
	capture := &grokBotSnapshotCapture{}
	collector, err := NewGrokBotCollector(client, capture, GrokBotCollectorConfig{
		Now: func() time.Time { return time.UnixMilli(5_000) },
	})
	if err != nil {
		t.Fatalf("NewGrokBotCollector() error = %v", err)
	}
	if err := collector.Refresh(context.Background()); err != nil {
		t.Fatalf("Refresh() error = %v", err)
	}
	if len(capture.commits) != 1 || capture.commits[0].Included || capture.commits[0].UsedPercent != nil {
		t.Fatalf("not_applicable commit = %#v failures:%v", capture.commits, capture.failures)
	}
}

func TestGrokBotCollectorMissingPercentIsSchemaIncompatibleNotZero(t *testing.T) {
	t.Parallel()
	client := &grokBotClientFixture{status: GrokBotUsageStatus{
		Included: true, PeriodStartMS: 1_000, NextResetAtMS: 9_000,
	}}
	capture := &grokBotSnapshotCapture{}
	collector, err := NewGrokBotCollector(client, capture, GrokBotCollectorConfig{
		Now: func() time.Time { return time.UnixMilli(5_000) },
	})
	if err != nil {
		t.Fatalf("NewGrokBotCollector() error = %v", err)
	}
	if err := collector.Refresh(context.Background()); err != nil {
		t.Fatalf("Refresh() error = %v", err)
	}
	if len(capture.commits) != 0 || len(capture.failures) != 1 || capture.failures[0] != "schema_incompatible" {
		t.Fatalf("missing percent writes = commits:%#v failures:%v", capture.commits, capture.failures)
	}
}

func TestGrokBotCollectorRecordsAuthFailureOnGrokBotSourceOnly(t *testing.T) {
	t.Parallel()
	client := &grokBotClientFixture{err: ErrDesktopAuthExpired}
	capture := &grokBotSnapshotCapture{}
	collector, err := NewGrokBotCollector(client, capture, GrokBotCollectorConfig{
		Now: func() time.Time { return time.UnixMilli(5_000) },
	})
	if err != nil {
		t.Fatalf("NewGrokBotCollector() error = %v", err)
	}
	if err := collector.Refresh(context.Background()); err != nil {
		t.Fatalf("Refresh() error = %v", err)
	}
	if len(capture.commits) != 0 || len(capture.failures) != 1 || capture.failures[0] != "auth_expired" {
		t.Fatalf("auth failure writes = commits:%#v failures:%v", capture.commits, capture.failures)
	}
}

func TestGrokBotAndDashboardCollectorsRefreshIndependently(t *testing.T) {
	t.Parallel()
	dashboardClient := &dashboardClientFixture{err: ErrDesktopAuthExpired}
	dashboardCapture := &dashboardSnapshotCapture{}
	dashboard, err := NewDashboardCollector(dashboardClient, dashboardCapture, DashboardCollectorConfig{
		Now: func() time.Time { return time.UnixMilli(5_000) },
	})
	if err != nil {
		t.Fatalf("NewDashboardCollector() error = %v", err)
	}
	percent := 40.0
	grokClient := &grokBotClientFixture{status: GrokBotUsageStatus{
		Included: true, UsagePercent: &percent, PeriodStartMS: 1_000, NextResetAtMS: 9_000,
	}}
	grokCapture := &grokBotSnapshotCapture{}
	grokBot, err := NewGrokBotCollector(grokClient, grokCapture, GrokBotCollectorConfig{
		Now: func() time.Time { return time.UnixMilli(5_000) },
	})
	if err != nil {
		t.Fatalf("NewGrokBotCollector() error = %v", err)
	}
	if err := dashboard.Refresh(context.Background()); err != nil {
		t.Fatalf("dashboard Refresh() error = %v", err)
	}
	if err := grokBot.Refresh(context.Background()); err != nil {
		t.Fatalf("grok bot Refresh() error = %v", err)
	}
	if dashboardCapture.commits != 0 || len(dashboardCapture.failures) != 1 || dashboardCapture.failures[0] != "auth_expired" {
		t.Fatalf("dashboard writes = commits:%d failures:%v", dashboardCapture.commits, dashboardCapture.failures)
	}
	if len(grokCapture.commits) != 1 || grokCapture.commits[0].UsedPercent == nil || *grokCapture.commits[0].UsedPercent != 40 {
		t.Fatalf("grok bot writes = %#v failures:%v", grokCapture.commits, grokCapture.failures)
	}
}

func TestGrokBotCollectorRejectsResetThatIsNotInTheFuture(t *testing.T) {
	t.Parallel()
	percent := 10.0
	client := &grokBotClientFixture{status: GrokBotUsageStatus{
		Included: true, UsagePercent: &percent, PeriodStartMS: 1_000, NextResetAtMS: 5_000,
	}}
	capture := &grokBotSnapshotCapture{}
	collector, err := NewGrokBotCollector(client, capture, GrokBotCollectorConfig{
		Now: func() time.Time { return time.UnixMilli(5_000) },
	})
	if err != nil {
		t.Fatalf("NewGrokBotCollector() error = %v", err)
	}
	if err := collector.Refresh(context.Background()); err != nil {
		t.Fatalf("Refresh() error = %v", err)
	}
	if len(capture.commits) != 0 || len(capture.failures) != 1 || capture.failures[0] != "schema_incompatible" {
		t.Fatalf("past reset writes = commits:%#v failures:%v", capture.commits, capture.failures)
	}
	if errors.Is(client.err, ErrDashboardProtocol) {
		t.Fatal("fixture must not rewrite client error")
	}
}

func TestGrokBotCollectorMapsHTTPFailuresThroughRealClient(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name    string
		handler http.HandlerFunc
		timeout time.Duration
		want    string
	}{
		{
			name: "timeout",
			handler: func(_ http.ResponseWriter, request *http.Request) {
				<-request.Context().Done()
			},
			timeout: 40 * time.Millisecond,
			want:    "read_failed",
		},
		{
			name: "server_error",
			handler: func(response http.ResponseWriter, _ *http.Request) {
				response.WriteHeader(http.StatusServiceUnavailable)
			},
			want: "read_failed",
		},
		{
			name: "unauthorized",
			handler: func(response http.ResponseWriter, _ *http.Request) {
				response.WriteHeader(http.StatusUnauthorized)
			},
			want: "auth_expired",
		},
		{
			name: "forbidden",
			handler: func(response http.ResponseWriter, _ *http.Request) {
				response.WriteHeader(http.StatusForbidden)
			},
			want: "auth_expired",
		},
		{
			name: "decode",
			handler: func(response http.ResponseWriter, _ *http.Request) {
				response.Header().Set("Content-Type", "application/proto")
				_, _ = response.Write([]byte{0xff})
			},
			want: "schema_incompatible",
		},
	}
	for _, testCase := range cases {
		testCase := testCase
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()
			server := httptest.NewServer(testCase.handler)
			t.Cleanup(server.Close)
			httpClient := server.Client()
			if testCase.timeout > 0 {
				httpClient.Timeout = testCase.timeout
			}
			client, err := NewDashboardClient(DashboardClientConfig{
				BaseURL:    server.URL,
				HTTPClient: httpClient,
				TokenSource: fixedAccessTokenSource{credential: DesktopAccessToken{
					Token: "fixture-access-token", ExpiresAt: time.Unix(1_800_000_000, 0),
				}},
			})
			if err != nil {
				t.Fatalf("NewDashboardClient() error = %v", err)
			}
			capture := &grokBotSnapshotCapture{}
			collector, err := NewGrokBotCollector(client, capture, GrokBotCollectorConfig{
				Now: func() time.Time { return time.UnixMilli(5_000) },
			})
			if err != nil {
				t.Fatalf("NewGrokBotCollector() error = %v", err)
			}
			if err := collector.Refresh(context.Background()); err != nil {
				t.Fatalf("Refresh() error = %v", err)
			}
			if len(capture.commits) != 0 || len(capture.failures) != 1 || capture.failures[0] != testCase.want {
				t.Fatalf("writes = commits:%#v failures:%v, want %s", capture.commits, capture.failures, testCase.want)
			}
		})
	}
}
