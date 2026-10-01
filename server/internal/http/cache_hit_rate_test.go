package http_test

import (
	"encoding/json/v2"
	"net/http"
	"strings"
	"testing"

	reportingv1 "github.com/SisyphusSQ/codex-pulse/api/codexpulse/reporting/v1"
	statistics_vo "github.com/SisyphusSQ/codex-pulse/server/internal/models/vo/statistics_vo"
)

func TestCacheHitRateHTTPAuthorizationStrictCapsuleAndLifetimeDTO(t *testing.T) {
	origin := "http://127.0.0.1:8080"
	server, access := testServer(t, origin)
	administrator := admin(t, access, origin)
	collector := reportingCollector(t, access, administrator.Principal, "合成采集机")
	batch := networkBatch()
	s := &batch.Sessions[0]
	s.Contributions[0].ObservedAtMS = new(int64(2000))
	s.Contributions[0].CachedTokens = s.Contributions[0].InputTokens
	s.Contributions[0].ID = reportingv1.ContributionID(s.Provider, s.SessionID, s.Contributions[0], 0)
	s.CacheUsage = &reportingv1.CacheUsageCapsule{Version: 1, Basis: "lifetime_cached_input", InputTokens: s.Contributions[0].InputTokens, CachedInputTokens: s.Contributions[0].CachedTokens}
	bytes, err := json.Marshal(batch)
	if err != nil {
		t.Fatal(err)
	}
	for range 2 {
		if r := reportingRequest(server, origin, http.MethodPost, "/api/v1/batches", string(bytes), collector); r.Code != 200 {
			t.Fatal("cache capsule accept", r.Code)
		}
	}
	path := "/api/v1/sessions/" + reportingv1.Key(s.Provider, s.SessionID) + "?start_at_ms=0&end_at_ms=10000"
	if r := reportingRequest(server, origin, http.MethodGet, path, "", collector); r.Code != 403 {
		t.Fatal("collector read global cache metric")
	}
	response := request(server, http.MethodGet, origin, path, "", &administrator, false)
	var body struct {
		Data statistics_vo.StatisticsSessionDetail `json:"data"`
	}
	if response.Code != 200 || json.Unmarshal(response.Body.Bytes(), &body) != nil || body.Data.Session.CacheHitRate == nil || body.Data.Session.CacheHitRate.BasisPoints == nil || *body.Data.Session.CacheHitRate.BasisPoints != "10000" || *body.Data.Session.CacheHitRate.InputTokens != "9007199254740993" {
		t.Fatal("cache DTO or precision", response.Code)
	}
	unsafe := strings.Replace(string(bytes), `"basis":"lifetime_cached_input"`, `"basis":"lifetime_cached_input","raw_jsonl":"forbidden-member"`, 1)
	if r := reportingRequest(server, origin, http.MethodPost, "/api/v1/batches", unsafe, collector); r.Code != 400 {
		t.Fatal("unknown cache member accepted")
	}
	s.CacheUsage.Version = 2
	bytes, _ = json.Marshal(batch)
	if r := reportingRequest(server, origin, http.MethodPost, "/api/v1/batches", string(bytes), collector); r.Code != 426 {
		t.Fatal("future cache capsule accepted")
	}
}
