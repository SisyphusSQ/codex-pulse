package http_test

import (
	"encoding/json/v2"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"uuid"

	reportingv1 "github.com/SisyphusSQ/codex-pulse/api/codexpulse/reporting/v1"
	apphttp "github.com/SisyphusSQ/codex-pulse/server/internal/http"
	access_dto "github.com/SisyphusSQ/codex-pulse/server/internal/models/dto/access_dto"
	reporting_vo "github.com/SisyphusSQ/codex-pulse/server/internal/models/vo/reporting_vo"
	access_srv "github.com/SisyphusSQ/codex-pulse/server/internal/service/access_srv"
)

func reportingCollector(t *testing.T, access *access_srv.Access, administrator access_dto.Principal, name string) access_dto.PairedClient {
	t.Helper()
	code, err := access.Issue(t.Context(), administrator, access_dto.PurposeCollector, name)
	if err != nil {
		t.Fatal(err)
	}
	paired, err := access.Pair(t.Context(), code.Code, "collector", "")
	if err != nil {
		t.Fatal(err)
	}
	return paired
}
func reportingRequest(server *apphttp.Server, origin, method, path, body string, p access_dto.PairedClient) *httptest.ResponseRecorder {
	r := httptest.NewRequest(method, origin+path, strings.NewReader(body))
	r.Header.Set("Content-Type", "application/json")
	r.Header.Set("Authorization", "Bearer "+p.Credential)
	w := httptest.NewRecorder()
	server.Echo.ServeHTTP(w, r)
	return w
}
func networkBatch() reportingv1.Batch {
	c := reportingv1.Contribution{InputTokens: new(int64(9007199254740993)), TotalTokens: new(int64(9007199254740993)), CostStatus: "unpriced"}
	c.ID = reportingv1.ContributionID("codex", "original-session", c, 0)
	return reportingv1.Batch{Version: 1, ID: uuid.New().String(), Sessions: []reportingv1.SessionSnapshot{{Provider: "codex", HomeID: "home-key", SessionID: "original-session", Revision: 1, CollectedAtMS: 3000, Title: "允许的会话标题", ProjectID: "project", ProjectName: "允许的项目名", SourceKind: "light_index", SessionKind: "session", Complete: false, Contributions: []reportingv1.Contribution{c}}}}
}
func TestReportingHTTPReceiptsPermissionsBudgetsAndClientScope(t *testing.T) {
	origin := "http://127.0.0.1:8080"
	server, access := testServer(t, origin)
	administrator := admin(t, access, origin)
	a := reportingCollector(t, access, administrator.Principal, "机器一")
	b := reportingCollector(t, access, administrator.Principal, "机器二")
	batch := networkBatch()
	bytes, err := json.Marshal(batch)
	if err != nil {
		t.Fatal(err)
	}
	first := reportingRequest(server, origin, http.MethodPost, "/api/v1/batches", string(bytes), a)
	if first.Code != 200 {
		t.Fatalf("accept=%d %s", first.Code, first.Body.String())
	}
	again := reportingRequest(server, origin, http.MethodPost, "/api/v1/batches", string(bytes), a)
	var one, two struct {
		Data reportingv1.Receipt `json:"data"`
	}
	if json.Unmarshal(first.Body.Bytes(), &one) != nil || json.Unmarshal(again.Body.Bytes(), &two) != nil || one.Data != two.Data {
		t.Fatal("HTTP receipt was not stable")
	}
	own := reportingRequest(server, origin, http.MethodGet, "/api/v1/sync?client_id="+a.Principal.ID, "", b)
	var view struct {
		Data reporting_vo.SyncView `json:"data"`
	}
	if own.Code != 200 || json.Unmarshal(own.Body.Bytes(), &view) != nil || view.Data.Batches != 0 || view.Data.LastReceivedAtMS != nil {
		t.Fatal("collector read another client progress")
	}
	promoted := request(server, http.MethodPost, origin, "/api/v1/batches", string(bytes), &administrator, true)
	if promoted.Code != 403 {
		t.Fatal("admin browser wrote collector facts")
	}
	unknown := strings.TrimSuffix(string(bytes), "}") + `,"role":"admin"}`
	if r := reportingRequest(server, origin, http.MethodPost, "/api/v1/batches", unknown, a); r.Code != 400 {
		t.Fatal("unknown identity field accepted")
	}
	duplicate := strings.TrimSuffix(string(bytes), "}") + `,"version":1}`
	if r := reportingRequest(server, origin, http.MethodPost, "/api/v1/batches", duplicate, a); r.Code != 400 {
		t.Fatal("duplicate member accepted")
	}
	batch.Version = 2
	bytes, _ = json.Marshal(batch)
	if r := reportingRequest(server, origin, http.MethodPost, "/api/v1/batches", string(bytes), a); r.Code != 426 {
		t.Fatal("unknown version not distinguished")
	}
	if r := reportingRequest(server, origin, http.MethodPost, "/api/v1/batches", strings.Repeat("x", reportingv1.MaxBodyBytes+1), a); r.Code != 413 {
		t.Fatal("oversized body accepted")
	}
	if err := access.Revoke(t.Context(), administrator.Principal, a.Principal.ID); err != nil {
		t.Fatal(err)
	}
	if r := reportingRequest(server, origin, http.MethodGet, "/api/v1/sync", "", a); r.Code != 401 {
		t.Fatal("revocation did not stop sync")
	}
}
func TestReportingActualTLSUsesSameCollectorContract(t *testing.T) {
	tlsServer := httptest.NewUnstartedServer(nil)
	tlsServer.StartTLS()
	defer tlsServer.Close()
	server, access := testServer(t, tlsServer.URL)
	tlsServer.Config.Handler = server.Echo
	administrator := admin(t, access, tlsServer.URL)
	collector := reportingCollector(t, access, administrator.Principal, "HTTPS机器")
	batch := networkBatch()
	bytes, _ := json.Marshal(batch)
	request, err := http.NewRequestWithContext(t.Context(), http.MethodPost, tlsServer.URL+"/api/v1/batches", strings.NewReader(string(bytes)))
	if err != nil {
		t.Fatal(err)
	}
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Authorization", "Bearer "+collector.Credential)
	response, err := tlsServer.Client().Do(request)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	if response.StatusCode != 200 {
		t.Fatalf("TLS collector=%d", response.StatusCode)
	}
}
