package http_test

import (
	"encoding/json/v2"
	"net/http"
	"testing"

	statistics_vo "github.com/SisyphusSQ/codex-pulse/server/internal/models/vo/statistics_vo"
)

func TestStatisticsHTTPDefaultAuthQueryValidationAndDecimalStrings(t *testing.T) {
	origin := "http://127.0.0.1:8080"
	server, access := testServer(t, origin)
	administrator := admin(t, access, origin)
	collector := reportingCollector(t, access, administrator.Principal, "采集机")
	for _, path := range []string{"/api/v1/statistics/summary", "/api/v1/sessions", "/api/v1/projects", "/api/v1/devices/status"} {
		if response := request(server, http.MethodGet, origin, path, "", nil, false); response.Code != 401 {
			t.Fatal("anonymous query", path, response.Code)
		}
		if response := reportingRequest(server, origin, http.MethodGet, path, "", collector); response.Code != 403 {
			t.Fatal("collector query", path, response.Code)
		}
		if response := request(server, http.MethodGet, origin, path, "", &administrator, false); response.Code != 200 {
			t.Fatal("admin query", path, response.Code, response.Body.String())
		}
	}
	for _, suffix := range []string{"?limit=1000000", "?time_zone=Local", "?sort=evil", "?start_at_ms=0&end_at_ms=0", "?provider=evil"} {
		if response := request(server, http.MethodGet, origin, "/api/v1/sessions"+suffix, "", &administrator, false); response.Code != 400 {
			t.Fatal("invalid query", suffix, response.Code)
		}
	}
	if response := request(server, http.MethodGet, origin, "/api/v1/sessions/bad-key", "", &administrator, false); response.Code != 400 {
		t.Fatal("invalid ID accepted")
	}
	response := request(server, http.MethodGet, origin, "/api/v1/statistics/summary", "", &administrator, false)
	var body struct {
		Data statistics_vo.StatisticsSummary `json:"data"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if body.Data.Totals.TotalTokens != nil || body.Data.Coverage.State != "unknown" {
		t.Fatal("empty center invented zero")
	}
}
