package http_test

import (
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/SisyphusSQ/codex-pulse/server/config"
	"github.com/SisyphusSQ/codex-pulse/server/internal/controller/runtime_controller"
)

func TestStaticWebRootAssetsAndAPIAuthorization(t *testing.T) {
	directory := t.TempDir()
	if err := os.Mkdir(filepath.Join(directory, "assets"), 0700); err != nil {
		t.Fatal(err)
	}
	for name, content := range map[string]string{"index.html": "<!doctype html><div>synthetic shell</div>", "assets/app-hash.js": "console.log('synthetic')", "assets/.secret.js": "private", "assets/source.map": "private"} {
		if err := os.WriteFile(filepath.Join(directory, name), []byte(content), 0600); err != nil {
			t.Fatal(err)
		}
	}
	outside := filepath.Join(t.TempDir(), "outside.js")
	if err := os.WriteFile(outside, []byte("private"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(directory, "assets", "escape.js")); err != nil {
		t.Fatal(err)
	}
	origin := "http://example.com"
	s, access := testServer(t, origin, func(cfg *config.Config) { cfg.Server.WebDirectory = directory })
	runtime_controller.Register(s.Echo)
	for _, tt := range []struct {
		method, path string
		status       int
	}{{"GET", "/", 200}, {"HEAD", "/", 200}, {"GET", "/assets/app-hash.js", 200}, {"HEAD", "/assets/app-hash.js", 200}, {"GET", "/assets/.secret.js", 404}, {"GET", "/assets/source.map", 404}, {"GET", "/assets/escape.js", 404}, {"GET", "/api/v1/version", 401}, {"GET", "/api/v1/clients", 401}} {
		w := request(s, tt.method, origin, tt.path, "", nil, false)
		if w.Code != tt.status {
			t.Fatalf("%s %s = %d", tt.method, tt.path, w.Code)
		}
		if tt.method == "HEAD" && w.Body.Len() != 0 {
			t.Fatal("HEAD body")
		}
		if w.Header().Get("X-Content-Type-Options") != "nosniff" {
			t.Fatal("security headers missing")
		}
		if tt.path == "/assets/app-hash.js" && !strings.Contains(w.Header().Get("Cache-Control"), "immutable") {
			t.Fatal("hashed asset cache missing")
		}
	}
	paired := admin(t, access, origin)
	w := request(s, http.MethodGet, origin, "/api/v1/version", "", &paired, false)
	if w.Code != 200 || !strings.Contains(w.Body.String(), `"reporting_protocol":1`) || strings.Contains(w.Body.String(), "GitRemote") {
		t.Fatalf("version view = %d", w.Code)
	}
}
