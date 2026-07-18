package main

import (
	"archive/zip"
	"crypto/ed25519"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRunWritesAndIndependentlyVerifiesManifest(t *testing.T) {
	dir, cfg := fixtureRelease(t)
	cfg.writeManifest = true
	if err := run(cfg); err != nil {
		t.Fatalf("write manifest: %v", err)
	}
	cfg.writeManifest = false
	if err := run(cfg); err != nil {
		t.Fatalf("verify manifest: %v", err)
	}
	data, err := os.ReadFile(filepath.Join(dir, "manifest.json"))
	if err != nil {
		t.Fatal(err)
	}
	var got manifest
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatal(err)
	}
	if got.SchemaVersion != 1 || got.Architecture != "arm64" || strings.Contains(string(data), "private") {
		t.Fatalf("unsafe manifest=%s", data)
	}
}

func TestRunRejectsTamperedArchive(t *testing.T) {
	dir, cfg := fixtureRelease(t)
	cfg.writeManifest = true
	if err := run(cfg); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "Codex-Pulse-1.2.3-arm64.zip")
	file, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0)
	if err != nil {
		t.Fatal(err)
	}
	_, _ = file.WriteString("tampered")
	_ = file.Close()
	cfg.writeManifest = false
	if err := run(cfg); err == nil {
		t.Fatal("tampered archive verified")
	}
}

func TestRunRejectsTamperedReleaseNotes(t *testing.T) {
	dir, cfg := fixtureRelease(t)
	cfg.writeManifest = true
	if err := run(cfg); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "Codex-Pulse-1.2.3-arm64.txt")
	if err := os.WriteFile(path, []byte("tampered release notes\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg.writeManifest = false
	if err := run(cfg); err == nil {
		t.Fatal("tampered release notes verified")
	}
}

func TestRunRejectsAppcastPlatformDrift(t *testing.T) {
	dir, cfg := fixtureRelease(t)
	path := filepath.Join(dir, "appcast.xml")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	data = []byte(strings.Replace(string(data), ">arm64<", ">x86_64<", 1))
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatal(err)
	}
	cfg.writeManifest = true
	if err := run(cfg); err == nil {
		t.Fatal("appcast architecture drift verified")
	}
}

func TestRunRejectsUnexpectedReleaseFileAndPathLikeVersion(t *testing.T) {
	dir, cfg := fixtureRelease(t)
	if err := os.WriteFile(filepath.Join(dir, "private-key.txt"), []byte("sensitive"), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg.writeManifest = true
	if err := run(cfg); err == nil {
		t.Fatal("unexpected release file accepted")
	}
	os.Remove(filepath.Join(dir, "private-key.txt"))
	cfg.version = "../1.2.3"
	if err := run(cfg); err == nil {
		t.Fatal("path-like version accepted")
	}
}

func TestPlistFromArchiveRejectsUnsafeEntries(t *testing.T) {
	tests := []struct {
		name    string
		entries []zipEntry
	}{
		{name: "traversal", entries: []zipEntry{{name: "Codex Pulse.app/Contents/Info.plist"}, {name: "../escape"}}},
		{name: "case conflict", entries: []zipEntry{{name: "Codex Pulse.app/Contents/Info.plist"}, {name: "Codex Pulse.app/Contents/info.plist"}}},
		{name: "escaping symlink", entries: []zipEntry{{name: "Codex Pulse.app/Contents/Info.plist"}, {name: "Codex Pulse.app/link", mode: os.ModeSymlink | 0o777, content: "../../escape"}}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			archive := filepath.Join(t.TempDir(), "unsafe.zip")
			writeNamedArchive(t, archive, test.entries)
			if _, err := plistFromArchive(archive); err == nil {
				t.Fatal("unsafe ZIP accepted")
			}
		})
	}
}

type zipEntry struct {
	name    string
	mode    os.FileMode
	content string
}

func writeNamedArchive(t *testing.T, path string, entries []zipEntry) {
	t.Helper()
	file, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	w := zip.NewWriter(file)
	for _, item := range entries {
		header := &zip.FileHeader{Name: item.name, Method: zip.Store}
		if item.mode != 0 {
			header.SetMode(item.mode)
		}
		entry, err := w.CreateHeader(header)
		if err != nil {
			t.Fatal(err)
		}
		if item.content != "" {
			_, _ = entry.Write([]byte(item.content))
		} else if item.name == "Codex Pulse.app/Contents/Info.plist" {
			_, _ = entry.Write([]byte(`<?xml version="1.0"?><plist><dict></dict></plist>`))
		}
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	if err := file.Close(); err != nil {
		t.Fatal(err)
	}
}

func fixtureRelease(t *testing.T) (string, config) {
	t.Helper()
	dir := t.TempDir()
	version, build := "1.2.3", "123"
	private := ed25519.NewKeyFromSeed(make([]byte, ed25519.SeedSize))
	publicKey := base64.StdEncoding.EncodeToString(private.Public().(ed25519.PublicKey))
	archiveName := "Codex-Pulse-" + version + "-arm64.zip"
	if err := os.WriteFile(filepath.Join(dir, "Codex-Pulse-"+version+"-arm64.txt"), []byte("Test release notes.\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	writeFixtureArchive(t, filepath.Join(dir, archiveName), map[string]string{
		"CFBundleShortVersionString": version,
		"CFBundleVersion":            build, "LSMinimumSystemVersion": "15.0.0",
		"SUFeedURL": "https://updates.example.test/appcast.xml", "SUPublicEDKey": publicKey,
	})
	archive, err := fileRecord(filepath.Join(dir, archiveName))
	if err != nil {
		t.Fatal(err)
	}
	archiveBytes, err := os.ReadFile(filepath.Join(dir, archiveName))
	if err != nil {
		t.Fatal(err)
	}
	signature := base64.StdEncoding.EncodeToString(ed25519.Sign(private, archiveBytes))
	appcast := fmt.Sprintf(`<?xml version="1.0"?><rss xmlns:sparkle="http://www.andymatuschak.org/xml-namespaces/sparkle"><channel><item><sparkle:version>%s</sparkle:version><sparkle:shortVersionString>%s</sparkle:shortVersionString><sparkle:minimumSystemVersion>15.0.0</sparkle:minimumSystemVersion><sparkle:hardwareRequirements>arm64</sparkle:hardwareRequirements><description sparkle:format="plain-text">Test release notes.
</description><enclosure url="https://downloads.example.test/%s" length="%d" type="application/octet-stream" sparkle:edSignature="%s" /></item></channel></rss>`, build, version, archiveName, archive.Size, signature)
	if err := os.WriteFile(filepath.Join(dir, "appcast.xml"), []byte(appcast), 0o644); err != nil {
		t.Fatal(err)
	}
	return dir, config{dir: dir, version: version, build: build,
		feedURL: "https://updates.example.test/appcast.xml", downloadPrefix: "https://downloads.example.test",
		publicKey: publicKey}
}

func writeFixtureArchive(t *testing.T, path string, values map[string]string) {
	t.Helper()
	file, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	w := zip.NewWriter(file)
	entry, err := w.Create("Codex Pulse.app/Contents/Info.plist")
	if err != nil {
		t.Fatal(err)
	}
	var plist strings.Builder
	plist.WriteString(`<?xml version="1.0"?><plist version="1.0"><dict>`)
	for key, value := range values {
		plist.WriteString("<key>" + key + "</key><string>" + value + "</string>")
	}
	plist.WriteString("</dict></plist>")
	_, _ = entry.Write([]byte(plist.String()))
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	if err := file.Close(); err != nil {
		t.Fatal(err)
	}
}
