package http_test

import (
	"encoding/json/v2"
	"net/http"
	"testing"
	"uuid"

	reportingv1 "github.com/SisyphusSQ/codex-pulse/api/codexpulse/reporting/v1"
)

func TestCatalogUsageAndSubscriptionHTTPPermissions(t *testing.T) {
	origin := "http://127.0.0.1:8080"
	server, access := testServer(t, origin)
	a := admin(t, access, origin)
	collector := reportingCollector(t, access, a.Principal, "synthetic-device")
	key := reportingv1.Key("codex", "account")
	path := "/api/v1/accounts/" + key + "/subscription"
	for _, route := range []string{"/api/v1/catalog", "/api/v1/statistics/usage", path} {
		if w := request(server, http.MethodGet, origin, route, "", nil, false); w.Code != 401 {
			t.Fatal("anonymous", route, w.Code)
		}
		if w := reportingRequest(server, origin, http.MethodGet, route, "", collector); w.Code != 403 {
			t.Fatal("collector", route, w.Code)
		}
	}
	batch := reportingv1.Batch{Version: 1, ID: uuid.New().String(), Accounts: []reportingv1.Account{{Provider: "codex", ID: "account", Email: new("test@example.invalid"), Plan: new("plus"), CollectedAtMS: 1}}}
	body, err := json.Marshal(batch)
	if err != nil {
		t.Fatal(err)
	}
	if w := reportingRequest(server, origin, http.MethodPost, "/api/v1/batches", string(body), collector); w.Code != 200 {
		t.Fatal(w.Code, w.Body.String())
	}
	update := `{"expected_revision":"0","alias":"test","manual_plan":null,"date_kind":"monthly_renewal","renewal_day":31,"membership_date":null,"time_zone":"UTC"}`
	if w := request(server, http.MethodPost, origin, path, update, &a, false); w.Code != 403 {
		t.Fatal("csrf", w.Code)
	}
	if w := request(server, http.MethodPost, origin, path, update, &a, true); w.Code != 200 {
		t.Fatal("valid update", w.Code, w.Body.String())
	}
	if w := request(server, http.MethodPost, origin, path, update, &a, true); w.Code != 409 {
		t.Fatal("revision conflict", w.Code)
	}
	if w := request(server, http.MethodPost, origin, path, `{"expected_revision":"1","api_key":"forbidden-field"}`, &a, true); w.Code != 400 {
		t.Fatal("unknown fields", w.Code)
	}
	if w := request(server, http.MethodGet, origin, path, "", &a, false); w.Code != 200 {
		t.Fatal("get settings", w.Code)
	}
	if w := request(server, http.MethodGet, origin, "/api/v1/catalog", "", &a, false); w.Code != 200 {
		t.Fatal("catalog", w.Code, w.Body.String())
	}
	if w := request(server, http.MethodGet, origin, "/api/v1/statistics/usage", "", &a, false); w.Code != 200 {
		t.Fatal("usage", w.Code, w.Body.String())
	}
}
