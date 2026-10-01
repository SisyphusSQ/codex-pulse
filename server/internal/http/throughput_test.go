package http_test

import (
	"encoding/json/v2"
	"net/http"
	"strings"
	"testing"

	reportingv1 "github.com/SisyphusSQ/codex-pulse/api/codexpulse/reporting/v1"
	statistics_vo "github.com/SisyphusSQ/codex-pulse/server/internal/models/vo/statistics_vo"
)

func TestThroughputHTTPStrictCapsuleAuthReceiptAndNullableDTO(t *testing.T) {
	origin := "http://127.0.0.1:8080"
	server, access := testServer(t, origin)
	administrator := admin(t, access, origin)
	collector := reportingCollector(t, access, administrator.Principal, "合成采集机")
	batch := networkBatch()
	s := &batch.Sessions[0]
	s.Contributions[0].OutputTokens = new(int64(16327))
	s.Contributions[0].ID = reportingv1.ContributionID(s.Provider, s.SessionID, s.Contributions[0], 0)
	s.Throughput = &reportingv1.ThroughputCapsule{Version: 1, Basis: "closed_turn_lifetime_output", TurnsTotal: 1, Measures: reportingv1.ThroughputMeasures{OutputTokens: new(int64(16327)), ActiveDurationMS: new(int64(730364)), IncludedTurns: 1, CoverageKnown: true, Status: "complete", DurationSource: "duration_ms"}}
	bytes, err := json.Marshal(batch)
	if err != nil {
		t.Fatal(err)
	}
	one := reportingRequest(server, origin, http.MethodPost, "/api/v1/batches", string(bytes), collector)
	two := reportingRequest(server, origin, http.MethodPost, "/api/v1/batches", string(bytes), collector)
	if one.Code != 200 || two.Code != 200 {
		t.Fatal("throughput accept", one.Code, two.Code)
	}
	var first, again struct {
		Data reportingv1.Receipt `json:"data"`
	}
	if json.Unmarshal(one.Body.Bytes(), &first) != nil || json.Unmarshal(two.Body.Bytes(), &again) != nil || first.Data != again.Data {
		t.Fatal("TPS receipt changed")
	}
	path := "/api/v1/sessions/" + reportingv1.Key(s.Provider, s.SessionID) + "?start_at_ms=0&end_at_ms=10000&throughput_limit=1"
	if r := reportingRequest(server, origin, http.MethodGet, path, "", collector); r.Code != 403 {
		t.Fatal("collector read center TPS")
	}
	response := request(server, http.MethodGet, origin, path, "", &administrator, false)
	var body struct {
		Data statistics_vo.StatisticsSessionDetail `json:"data"`
	}
	if response.Code != 200 || json.Unmarshal(response.Body.Bytes(), &body) != nil || body.Data.Session.Throughput == nil || *body.Data.Session.Throughput.AverageOutputMilliTPS != "22355" {
		t.Fatal("TPS DTO", response.Code)
	}
	unsafe := strings.Replace(string(bytes), `"basis":"closed_turn_lifetime_output"`, `"basis":"closed_turn_lifetime_output","turn_id":"raw-secret-turn"`, 1)
	if r := reportingRequest(server, origin, http.MethodPost, "/api/v1/batches", unsafe, collector); r.Code != 400 {
		t.Fatal("raw unknown turn member accepted")
	}
	s.Throughput.Version = 2
	bytes, _ = json.Marshal(batch)
	if r := reportingRequest(server, origin, http.MethodPost, "/api/v1/batches", string(bytes), collector); r.Code != 426 {
		t.Fatal("incompatible capsule version accepted")
	}
}
