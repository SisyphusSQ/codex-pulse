package controller

import (
	"bytes"
	"encoding/json/v2"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
	"uuid"

	"github.com/labstack/echo/v5"

	reportingv1 "github.com/SisyphusSQ/codex-pulse/api/codexpulse/reporting/v1"
	"github.com/SisyphusSQ/codex-pulse/server/config"
	"github.com/SisyphusSQ/codex-pulse/server/internal/controller/catalog_controller"
	"github.com/SisyphusSQ/codex-pulse/server/internal/controller/quota_controller"
	"github.com/SisyphusSQ/codex-pulse/server/internal/controller/reporting_controller"
	"github.com/SisyphusSQ/codex-pulse/server/internal/controller/statistics_controller"
	apphttp "github.com/SisyphusSQ/codex-pulse/server/internal/http"
	catalog_vo "github.com/SisyphusSQ/codex-pulse/server/internal/models/vo/catalog_vo"
	quota_vo "github.com/SisyphusSQ/codex-pulse/server/internal/models/vo/quota_vo"
	statistics_vo "github.com/SisyphusSQ/codex-pulse/server/internal/models/vo/statistics_vo"
	"github.com/SisyphusSQ/codex-pulse/server/internal/repository/mysql/access_repo"
	"github.com/SisyphusSQ/codex-pulse/server/internal/repository/mysql/catalog_repo"
	"github.com/SisyphusSQ/codex-pulse/server/internal/repository/mysql/quota_repo"
	"github.com/SisyphusSQ/codex-pulse/server/internal/repository/mysql/reporting_repo"
	"github.com/SisyphusSQ/codex-pulse/server/internal/repository/mysql/statistics_repo"
	"github.com/SisyphusSQ/codex-pulse/server/internal/service/access_srv"
	"github.com/SisyphusSQ/codex-pulse/server/internal/service/catalog_srv"
	"github.com/SisyphusSQ/codex-pulse/server/internal/service/quota_srv"
	"github.com/SisyphusSQ/codex-pulse/server/internal/service/reporting_srv"
	"github.com/SisyphusSQ/codex-pulse/server/internal/service/statistics_srv"
	centerfixture "github.com/SisyphusSQ/codex-pulse/server/internal/testsupport/center"
)

func TestAGYControllersEndToEndSQLite(t *testing.T) {
	engine := centerfixture.Engine(t)
	origin := "http://127.0.0.1:8080"
	cfg := config.Config{ContextTimeout: 5 * time.Second}
	cfg.Server.Origins = []string{origin}
	cfg.Server.AllowHTTP = true

	access := access_srv.NewAccess(access_repo.NewAccess(engine))
	adminPair, err := access.Bootstrap(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	adminSession, err := access.Pair(t.Context(), adminPair.Code, "browser", origin)
	if err != nil {
		t.Fatal(err)
	}
	collectorPair, err := access.Issue(t.Context(), adminSession.Principal, "collector", "mac-1")
	if err != nil {
		t.Fatal(err)
	}
	collectorSession, err := access.Pair(t.Context(), collectorPair.Code, "collector", origin)
	if err != nil {
		t.Fatal(err)
	}

	e := echo.New()
	mw := apphttp.InitMiddleware(cfg, access)
	e.HTTPErrorHandler = mw.ErrorHandler
	e.Use(mw.Auth)
	reporting_controller.NewReporting(reporting_srv.NewReporting(reporting_repo.NewReporting(engine))).Register(e)
	quota_controller.NewQuota(quota_srv.NewQuota(quota_repo.NewQuota(engine))).Register(e)
	statistics_controller.NewStatistics(statistics_srv.NewStatistics(statistics_repo.NewStatistics(engine))).Register(e)
	catalog_controller.NewCatalog(catalog_srv.NewCatalog(catalog_repo.NewCatalog(engine))).Register(e)

	now := time.Now().UTC().UnixMilli()
	evAt := now - 60_000
	reset5h := now + 3*3600_000
	scope := reportingv1.Key("agy-account", "controller-test-user")
	publicScope := reportingv1.Key("agy-public", collectorSession.Principal.ID, scope)
	c := reportingv1.Contribution{
		ObservedAtMS:    &evAt,
		Model:           new("gemini-4-carbon"),
		InputTokens:     new(int64(2000)),
		CachedTokens:    new(int64(1000)),
		OutputTokens:    new(int64(400)),
		ReasoningTokens: new(int64(100)),
		TotalTokens:     new(int64(2400)),
		CostMicroUSD:    new(int64(6100)),
		PricingVersion:  new("agy-api-2026-11-25:phase1"),
		CostStatus:      "known",
		PricingMode:     "event_cost",
	}
	c.ID = reportingv1.ContributionID("agy", "agy-http-sess", c, 0)
	batch := reportingv1.Batch{
		Version: 1,
		ID:      uuid.New().String(),
		Sessions: []reportingv1.SessionSnapshot{{
			Provider:       "agy",
			HomeID:         "home-agy",
			SessionID:      "agy-http-sess",
			Revision:       1,
			CollectedAtMS:  now,
			Title:          "AGY Controller 测试",
			ProjectID:      reportingv1.Key("agy", "proj"),
			ProjectName:    "proj",
			CreatedAtMS:    new(evAt - 2000),
			LastActiveAtMS: &evAt,
			SourceKind:     "agy_local",
			SessionKind:    "session",
			Complete:       true,
			Contributions:  []reportingv1.Contribution{c},
			CacheUsage: &reportingv1.CacheUsageCapsule{
				Version:           1,
				Basis:             "lifetime_cached_input",
				InputTokens:       new(int64(2000)),
				CachedInputTokens: new(int64(1000)),
			},
			Throughput: &reportingv1.ThroughputCapsule{
				Version: 1,
				Basis:   "closed_turn_lifetime_output",
				Measures: reportingv1.ThroughputMeasures{
					OutputTokens:     new(int64(400)),
					ActiveDurationMS: new(int64(2000)),
					IncludedTurns:    1,
					CoverageKnown:    true,
					Status:           "complete",
					DurationSource:   "step_metadata",
				},
				TurnsTotal: 1,
			},
		}},
		Accounts: []reportingv1.Account{{
			Provider:      "agy",
			ID:            scope,
			Plan:          new("Google AI Ultra 20×"),
			CollectedAtMS: now,
		}},
		Bindings: []reportingv1.AccountBinding{{
			Provider:      "agy",
			LocalScope:    publicScope,
			AccountID:     scope,
			ConfirmedAtMS: now,
		}},
		Quotas: []reportingv1.QuotaObservation{{
			Provider:        "agy",
			ID:              reportingv1.Key("q", "gemini"),
			AccountID:       new(scope),
			LocalScope:      publicScope,
			LimitID:         "agy.gemini",
			WindowKind:      "primary",
			WindowMinutes:   new(int64(300)),
			WindowStartAtMS: new(reset5h - 300*60000),
			ResetsAtMS:      new(reset5h),
			ObservedAtMS:    evAt,
			UsedPercent:     new(float64(35)),
			Validity:        "accepted",
			Source:          "agy_ls_rpc",
			HistoryOrigin:   "confirmed",
		}},
	}
	payload, err := json.Marshal(batch)
	if err != nil {
		t.Fatal(err)
	}

	// 1. Collector POSTs AGY batch.
	postReq := httptest.NewRequest(http.MethodPost, origin+"/api/v1/batches", bytes.NewReader(payload))
	postReq.Header.Set("Content-Type", "application/json")
	postReq.Header.Set("Authorization", "Bearer "+collectorSession.Credential)
	postRec := httptest.NewRecorder()
	e.ServeHTTP(postRec, postReq)
	if postRec.Code != http.StatusOK {
		t.Fatalf("POST /api/v1/batches failed: status=%d body=%s", postRec.Code, postRec.Body.String())
	}

	adminGet := func(path string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(http.MethodGet, origin+path, nil)
		req.AddCookie(&http.Cookie{Name: apphttp.SessionCookieName(origin), Value: adminSession.Credential})
		rec := httptest.NewRecorder()
		e.ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("GET %s failed: status=%d body=%s", path, rec.Code, rec.Body.String())
		}
		return rec
	}

	// 2. GET /api/v1/quotas?provider=agy
	qRec := adminGet("/api/v1/quotas?provider=agy")
	var qResp struct {
		Data quota_vo.Response `json:"data"`
	}
	if err := json.Unmarshal(qRec.Body.Bytes(), &qResp); err != nil {
		t.Fatal(err)
	}
	if len(qResp.Data.Windows) != 1 || qResp.Data.Windows[0].LimitID != "agy.gemini" || *qResp.Data.Windows[0].Current.UsedPercent != 35 {
		t.Fatalf("unexpected AGY quota HTTP response: %+v", qResp.Data)
	}

	// 3. GET /api/v1/statistics/usage?provider=agy
	uRec := adminGet("/api/v1/statistics/usage?provider=agy")
	var uResp struct {
		Data statistics_vo.UsageResponse `json:"data"`
	}
	if err := json.Unmarshal(uRec.Body.Bytes(), &uResp); err != nil {
		t.Fatal(err)
	}
	if uResp.Data.Totals.TotalTokens == nil || *uResp.Data.Totals.TotalTokens != "2400" || uResp.Data.CacheHitRate == nil || *uResp.Data.CacheHitRate.BasisPoints != "5000" {
		t.Fatalf("unexpected AGY usage HTTP response: %+v", uResp.Data)
	}

	// 4. GET /api/v1/catalog includes AGY models.
	cRec := adminGet("/api/v1/catalog")
	var cResp struct {
		Data catalog_vo.Response `json:"data"`
	}
	if err := json.Unmarshal(cRec.Body.Bytes(), &cResp); err != nil {
		t.Fatal(err)
	}
	foundAGYModel := false
	for _, m := range cResp.Data.Models {
		if m.Provider == "agy" && m.Model == "gemini-4-carbon" {
			foundAGYModel = true
			break
		}
	}
	if !foundAGYModel {
		t.Fatal("GET /api/v1/catalog missing AGY models")
	}
}
