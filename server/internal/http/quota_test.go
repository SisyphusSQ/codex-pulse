package http_test

import (
	"encoding/json/v2"
	"net/http"
	"testing"

	quota_vo "github.com/SisyphusSQ/codex-pulse/server/internal/models/vo/quota_vo"
)

func TestQuotaHTTPAuthBoundsAndEmptyUnknown(t *testing.T) {
	origin := "http://127.0.0.1:8080"
	server, access := testServer(t, origin)
	administrator := admin(t, access, origin)
	collector := reportingCollector(t, access, administrator.Principal, "设备")
	if response := request(server, http.MethodGet, origin, "/api/v1/quotas", "", nil, false); response.Code != 401 {
		t.Fatal("anonymous quota", response.Code)
	}
	if response := reportingRequest(server, origin, http.MethodGet, "/api/v1/quotas", "", collector); response.Code != 403 {
		t.Fatal("collector quota", response.Code)
	}
	for _, query := range []string{"?provider=evil", "?client_id=x", "?account_key=x", "?provider=codex&provider=grok", "?limit=1000000"} {
		if response := request(server, http.MethodGet, origin, "/api/v1/quotas"+query, "", &administrator, false); response.Code != 400 {
			t.Fatal("invalid query", query, response.Code)
		}
	}
	response := request(server, http.MethodGet, origin, "/api/v1/quotas", "", &administrator, false)
	var body struct {
		Data quota_vo.Response `json:"data"`
	}
	if response.Code != 200 {
		t.Fatal(response.Code, response.Body.String())
	}
	if err := json.Unmarshal(response.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if len(body.Data.Windows) != 0 || len(body.Data.Credits) != 0 || body.Data.Coverage != "observed_only" {
		t.Fatal("empty center fabricated data")
	}
}
