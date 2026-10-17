package http_test

import (
	"encoding/json/v2"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	apphttp "github.com/SisyphusSQ/codex-pulse/server/internal/http"
	access_dto "github.com/SisyphusSQ/codex-pulse/server/internal/models/dto/access_dto"
	access_vo "github.com/SisyphusSQ/codex-pulse/server/internal/models/vo/access_vo"
)

func TestBrowserSessionRenewalCookieAndPermissions(t *testing.T) {
	for _, origin := range []string{"http://pulse.example", "https://pulse.example"} {
		t.Run(origin, func(t *testing.T) {
			server, access := testServer(t, origin)
			browser := admin(t, access, origin)
			get := request(server, "GET", origin, "/api/v1/session", "", &browser, false)
			if get.Code != 200 || len(get.Result().Cookies()) != 0 {
				t.Fatal("GET must stay read-only")
			}
			principal, err := access.Authenticate(t.Context(), browser.Credential, origin, "", true, false)
			if err != nil || *principal.ExpiresAtMS != *browser.ExpiresAtMS {
				t.Fatal("GET extended server window")
			}
			if got := request(server, "POST", origin, "/api/v1/session", `{}`, &browser, false); got.Code != 403 || len(got.Result().Cookies()) != 0 {
				t.Fatal("renewal bypassed CSRF")
			}
			got := request(server, "POST", origin, "/api/v1/session", `{}`, &browser, true)
			var response struct {
				Data access_vo.SessionView `json:"data"`
			}
			if err := json.Unmarshal(got.Body.Bytes(), &response); err != nil {
				t.Fatal(err)
			}
			cookies := got.Result().Cookies()
			if got.Code != 200 || len(cookies) != 1 || response.Data.ExpiresAtMS == nil {
				t.Fatalf("renewal status=%d", got.Code)
			}
			cookie := cookies[0]
			if cookie.Name != apphttp.SessionCookieName(origin) || cookie.Value != browser.Credential || cookie.Path != "/" || !cookie.HttpOnly || cookie.SameSite != http.SameSiteStrictMode || cookie.Secure != (origin == "https://pulse.example") {
				t.Fatal("cookie protections changed")
			}
			if cookie.Expires.Unix() != time.UnixMilli(*response.Data.ExpiresAtMS).Unix() || response.Data.CSRF != browser.CSRF {
				t.Fatal("cookie/session disagree")
			}
			principal, err = access.Authenticate(t.Context(), browser.Credential, origin, browser.CSRF, true, true)
			if err != nil || *principal.ExpiresAtMS != *response.Data.ExpiresAtMS {
				t.Fatal("database/session disagree")
			}
			foreign := httptest.NewRequest("POST", origin+"/api/v1/session", nil)
			foreign.Header.Set("Origin", "https://other.example")
			foreign.Header.Set("X-Pulse-CSRF", browser.CSRF)
			foreign.AddCookie(cookie)
			denied := httptest.NewRecorder()
			server.Echo.ServeHTTP(denied, foreign)
			if denied.Code != 403 {
				t.Fatal("foreign renewal accepted")
			}
			code, err := access.Issue(t.Context(), browser.Principal, access_dto.PurposeCollector, "synthetic collector")
			if err != nil {
				t.Fatal(err)
			}
			collector, err := access.Pair(t.Context(), code.Code, "collector", "")
			if err != nil {
				t.Fatal(err)
			}
			collectorRequest := httptest.NewRequest("POST", origin+"/api/v1/session", nil)
			collectorRequest.Header.Set("Authorization", "Bearer "+collector.Credential)
			denied = httptest.NewRecorder()
			server.Echo.ServeHTTP(denied, collectorRequest)
			if denied.Code != 403 {
				t.Fatal("collector renewed browser")
			}
			if out := request(server, "POST", origin, "/api/v1/logout", `{}`, &browser, true); out.Code != 200 {
				t.Fatal("logout failed")
			}
			if out := request(server, "POST", origin, "/api/v1/session", `{}`, &browser, true); out.Code != 401 || len(out.Result().Cookies()) != 0 {
				t.Fatal("logout was undone")
			}
		})
	}
}
