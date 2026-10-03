package http_test

import (
	"encoding/json/v2"
	"net/http"
	"strings"
	"testing"

	access_dto "github.com/SisyphusSQ/codex-pulse/server/internal/models/dto/access_dto"
	access_vo "github.com/SisyphusSQ/codex-pulse/server/internal/models/vo/access_vo"
)

func TestDeviceManagementRevokeUnusedCodeAndRename(t *testing.T) {
	const origin = "http://pulse.example"
	server, access := testServer(t, origin)
	browser := admin(t, access, origin)
	code, err := access.Issue(t.Context(), browser.Principal, access_dto.PurposeCollector, "原始机器")
	if err != nil {
		t.Fatal(err)
	}
	body, _ := json.Marshal(access_vo.RevokePairingRequest{Code: code.Code})
	if got := request(server, "POST", origin, "/api/v1/pairings/revoke", string(body), &browser, false); got.Code != 403 {
		t.Fatal("CSRF bypass", got.Code)
	}
	if got := request(server, "POST", origin, "/api/v1/pairings/revoke", string(body), &browser, true); got.Code != 200 {
		t.Fatal(got.Code)
	}
	if _, err := access.Pair(t.Context(), code.Code, "collector", ""); err == nil {
		t.Fatal("revoked code consumed")
	}
	code, err = access.Issue(t.Context(), browser.Principal, access_dto.PurposeCollector, "原始机器")
	if err != nil {
		t.Fatal(err)
	}
	collector, err := access.Pair(t.Context(), code.Code, "collector", "")
	if err != nil {
		t.Fatal(err)
	}
	body, _ = json.Marshal(access_vo.RevokePairingRequest{Code: code.Code})
	if got := request(server, "POST", origin, "/api/v1/pairings/revoke", string(body), &browser, true); got.Code != 409 {
		t.Fatal("consumed code must identify conflict", got.Code)
	}
	path := "/api/v1/clients/" + collector.Principal.ID + "/rename"
	for _, value := range []string{`{"name":"","purpose":"admin"}`, `{"name":""}`, `{"name":"` + strings.Repeat("a", 129) + `"}`} {
		if got := request(server, "POST", origin, path, value, &browser, true); got.Code != 400 {
			t.Fatal("invalid update accepted", got.Code)
		}
	}
	for range 2 {
		if got := request(server, "POST", origin, path, `{"name":"<script>普通名称</script>"}`, &browser, true); got.Code != 200 {
			t.Fatal(got.Code)
		}
	}
	if got := request(server, "POST", origin, path, `{"name":"拒绝"}`, &browser, false); got.Code != 403 {
		t.Fatal(got.Code)
	}
	if _, err := access.Authenticate(t.Context(), collector.Credential, "", "", false, false); err != nil {
		t.Fatal("rename altered credential", err)
	}
	if err := access.Rename(t.Context(), collector.Principal, collector.Principal.ID, "越权"); err == nil {
		t.Fatal("collector management promotion")
	}
	view, err := access.Clients(t.Context(), browser.Principal)
	if err != nil {
		t.Fatal(err)
	}
	for _, client := range view.Clients {
		if client.ID == collector.Principal.ID && (client.Name != "<script>普通名称</script>" || client.Purpose != access_dto.PurposeCollector) {
			t.Fatal("rename changed scope or omitted name")
		}
	}
	if got := request(server, http.MethodGet, origin, "/api/v1/clients", "", &browser, false); strings.Contains(got.Body.String(), collector.Credential) || strings.Contains(got.Body.String(), "secret_hash") {
		t.Fatal("management leaked secret")
	}
}
