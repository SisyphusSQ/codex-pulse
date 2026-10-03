package http_test

import (
	"compress/gzip"
	"encoding/json/v2"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	apphttp "github.com/SisyphusSQ/codex-pulse/server/internal/http"
)

func TestStatisticsCompressionPreservesAuthAndJSON(t *testing.T) {
	origin := "http://127.0.0.1:8080"
	server, access := testServer(t, origin)
	paired := admin(t, access, origin)
	for _, authenticated := range []bool{false, true} {
		r := httptest.NewRequest(http.MethodGet, origin+"/api/v1/statistics/summary", nil)
		r.Header.Set("Accept-Encoding", "gzip")
		if authenticated {
			r.AddCookie(&http.Cookie{Name: apphttp.SessionCookieName(origin), Value: paired.Credential})
		}
		w := httptest.NewRecorder()
		server.Echo.ServeHTTP(w, r)
		if !authenticated {
			if w.Code != 401 {
				t.Fatal("anonymous compressed query allowed")
			}
			continue
		}
		if w.Code != 200 || w.Header().Get("Content-Encoding") != "gzip" {
			t.Fatal("summary compression unavailable")
		}
		reader, err := gzip.NewReader(w.Body)
		if err != nil {
			t.Fatal(err)
		}
		defer reader.Close()
		body, err := io.ReadAll(reader)
		if err != nil {
			t.Fatal(err)
		}
		var envelope map[string]any
		if err := json.Unmarshal(body, &envelope); err != nil {
			t.Fatal(err)
		}
		if envelope["code"] != float64(200) || len(body) <= w.Body.Len() {
			t.Fatal("compressed envelope changed")
		}
	}
}
