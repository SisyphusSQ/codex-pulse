package main

import (
	"archive/zip"
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"encoding/xml"
	"errors"
	"flag"
	"fmt"
	"io"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
)

const manifestSchemaVersion = 1

type config struct {
	dir, version, build, feedURL, downloadPrefix, publicKey string
	writeManifest                                           bool
}

type manifest struct {
	SchemaVersion  int          `json:"schemaVersion"`
	Version        string       `json:"version"`
	BuildNumber    string       `json:"buildNumber"`
	Architecture   string       `json:"architecture"`
	MinimumMacOS   string       `json:"minimumMacOS"`
	FeedURL        string       `json:"feedURL"`
	PublicKey      string       `json:"publicKey"`
	SparkleVersion string       `json:"sparkleVersion"`
	Archive        manifestFile `json:"archive"`
	Appcast        manifestFile `json:"appcast"`
	ReleaseNotes   manifestFile `json:"releaseNotes"`
}

type manifestFile struct {
	Name   string `json:"name"`
	Size   int64  `json:"size"`
	SHA256 string `json:"sha256"`
}

type rss struct {
	Channel struct {
		Items []appcastItem `xml:"item"`
	} `xml:"channel"`
}

type appcastItem struct {
	Version      string      `xml:"version"`
	ShortVersion string      `xml:"shortVersionString"`
	MinimumMacOS string      `xml:"minimumSystemVersion"`
	Architecture string      `xml:"hardwareRequirements"`
	Description  description `xml:"description"`
	Enclosure    enclosure   `xml:"enclosure"`
}

type description struct {
	Format string `xml:"format,attr"`
	Text   string `xml:",chardata"`
}

type enclosure struct {
	URL       string
	Length    string
	Version   string
	Signature string
	MediaType string
}

func (value *enclosure) UnmarshalXML(decoder *xml.Decoder, start xml.StartElement) error {
	for _, attribute := range start.Attr {
		switch attribute.Name.Local {
		case "url":
			value.URL = attribute.Value
		case "length":
			value.Length = attribute.Value
		case "version":
			value.Version = attribute.Value
		case "edSignature":
			value.Signature = attribute.Value
		case "type":
			value.MediaType = attribute.Value
		}
	}
	return decoder.Skip()
}

func main() {
	var cfg config
	flag.StringVar(&cfg.dir, "dir", "", "release directory")
	flag.StringVar(&cfg.version, "version", "", "semantic version")
	flag.StringVar(&cfg.build, "build", "", "bundle build number")
	flag.StringVar(&cfg.feedURL, "feed-url", "", "expected appcast URL")
	flag.StringVar(&cfg.downloadPrefix, "download-prefix", "", "expected download URL prefix")
	flag.StringVar(&cfg.publicKey, "public-key", "", "expected public Ed25519 key")
	flag.BoolVar(&cfg.writeManifest, "write-manifest", false, "write verified manifest")
	flag.Parse()
	if err := run(cfg); err != nil {
		fmt.Fprintln(os.Stderr, "releaseverify:", err)
		os.Exit(1)
	}
}

func run(cfg config) error {
	if cfg.dir == "" || cfg.version == "" || cfg.build == "" || cfg.publicKey == "" {
		return errors.New("all verification inputs are required")
	}
	if !validVersion(cfg.version) || !allDigits(cfg.build) {
		return errors.New("version/build format is invalid")
	}
	dirInfo, err := os.Lstat(cfg.dir)
	if err != nil || !dirInfo.IsDir() || dirInfo.Mode()&os.ModeSymlink != 0 {
		return errors.New("release directory must be a real directory")
	}
	feed, err := url.Parse(cfg.feedURL)
	if err != nil || feed.Scheme != "https" || feed.Host == "" || feed.User != nil {
		return errors.New("feed URL must be HTTPS without userinfo")
	}
	prefix, err := url.Parse(cfg.downloadPrefix)
	if err != nil || prefix.Scheme != "https" || prefix.Host == "" || prefix.User != nil || prefix.RawQuery != "" || prefix.Fragment != "" {
		return errors.New("download prefix must be HTTPS without userinfo, query, or fragment")
	}
	archiveName := "Codex-Pulse-" + cfg.version + "-arm64.zip"
	archivePath := filepath.Join(cfg.dir, archiveName)
	appcastPath := filepath.Join(cfg.dir, "appcast.xml")
	notesPath := filepath.Join(cfg.dir, "Codex-Pulse-"+cfg.version+"-arm64.txt")
	manifestPath := filepath.Join(cfg.dir, "manifest.json")
	expectedFiles := []string{archiveName, "appcast.xml", filepath.Base(notesPath)}
	if !cfg.writeManifest {
		expectedFiles = append(expectedFiles, filepath.Base(manifestPath))
	}
	if err := requireExactFiles(cfg.dir, expectedFiles); err != nil {
		return err
	}
	archive, err := fileRecord(archivePath)
	if err != nil {
		return err
	}
	appcast, err := fileRecord(appcastPath)
	if err != nil {
		return err
	}
	releaseNotes, err := fileRecord(notesPath)
	if err != nil {
		return err
	}
	item, err := readEnclosure(appcastPath)
	if err != nil {
		return err
	}
	if item.Version != cfg.build || item.ShortVersion != cfg.version {
		return fmt.Errorf("appcast version mismatch: %s/%s", item.ShortVersion, item.Version)
	}
	if item.MinimumMacOS != "15.0.0" || item.Architecture != "arm64" {
		return fmt.Errorf("appcast platform mismatch: %s/%s", item.MinimumMacOS, item.Architecture)
	}
	notesData, err := readLimited(notesPath, 64*1024)
	if err != nil {
		return err
	}
	if item.Description.Format != "plain-text" || item.Description.Text != string(notesData) {
		return errors.New("appcast embedded plain-text release notes do not match the release notes file")
	}
	if item.Enclosure.MediaType != "application/octet-stream" {
		return fmt.Errorf("appcast enclosure media type mismatch: %s", item.Enclosure.MediaType)
	}
	if item.Enclosure.Length != strconv.FormatInt(archive.Size, 10) {
		return fmt.Errorf("appcast archive length mismatch: %s", item.Enclosure.Length)
	}
	wantURL := strings.TrimSuffix(cfg.downloadPrefix, "/") + "/" + archiveName
	if item.Enclosure.URL != wantURL {
		return fmt.Errorf("appcast enclosure URL mismatch: %s", item.Enclosure.URL)
	}
	if item.Enclosure.Signature == "" {
		return errors.New("appcast EdDSA signature is empty")
	}
	publicKey, err := base64.StdEncoding.DecodeString(cfg.publicKey)
	if err != nil || len(publicKey) != ed25519.PublicKeySize {
		return errors.New("public key must be base64-encoded Ed25519 bytes")
	}
	signature, err := base64.StdEncoding.DecodeString(item.Enclosure.Signature)
	if err != nil || len(signature) != ed25519.SignatureSize {
		return errors.New("appcast signature must be base64-encoded Ed25519 bytes")
	}
	archiveBytes, err := os.ReadFile(archivePath)
	if err != nil {
		return err
	}
	if !ed25519.Verify(ed25519.PublicKey(publicKey), archiveBytes, signature) {
		return errors.New("independent Ed25519 archive verification failed")
	}
	plist, err := plistFromArchive(archivePath)
	if err != nil {
		return err
	}
	for key, expected := range map[string]string{
		"CFBundleShortVersionString": cfg.version,
		"CFBundleVersion":            cfg.build,
		"LSMinimumSystemVersion":     "15.0.0",
		"SUFeedURL":                  cfg.feedURL,
		"SUPublicEDKey":              cfg.publicKey,
	} {
		if plist[key] != expected {
			return fmt.Errorf("bundle %s mismatch", key)
		}
	}
	want := manifest{
		SchemaVersion: manifestSchemaVersion, Version: cfg.version, BuildNumber: cfg.build,
		Architecture: "arm64", MinimumMacOS: "15.0.0", FeedURL: cfg.feedURL,
		PublicKey: cfg.publicKey, SparkleVersion: "2.9.4", Archive: archive, Appcast: appcast,
		ReleaseNotes: releaseNotes,
	}
	if cfg.writeManifest {
		data, err := json.MarshalIndent(want, "", "  ")
		if err != nil {
			return err
		}
		data = append(data, '\n')
		if err := os.WriteFile(manifestPath, data, 0o644); err != nil {
			return err
		}
		return nil
	}
	data, err := readLimited(manifestPath, 1024*1024)
	if err != nil {
		return err
	}
	var got manifest
	if err := json.Unmarshal(data, &got); err != nil {
		return err
	}
	if !reflect.DeepEqual(got, want) {
		return errors.New("manifest does not match independently recomputed release metadata")
	}
	return nil
}

func fileRecord(path string) (manifestFile, error) {
	info, err := os.Lstat(path)
	if err != nil {
		return manifestFile{}, err
	}
	if !info.Mode().IsRegular() {
		return manifestFile{}, fmt.Errorf("release file is not regular: %s", filepath.Base(path))
	}
	file, err := os.Open(path)
	if err != nil {
		return manifestFile{}, err
	}
	defer file.Close()
	hash := sha256.New()
	if _, err := io.Copy(hash, file); err != nil {
		return manifestFile{}, err
	}
	return manifestFile{Name: filepath.Base(path), Size: info.Size(), SHA256: hex.EncodeToString(hash.Sum(nil))}, nil
}

func readEnclosure(path string) (appcastItem, error) {
	data, err := readLimited(path, 1024*1024)
	if err != nil {
		return appcastItem{}, err
	}
	var feed rss
	if err := xml.Unmarshal(data, &feed); err != nil {
		return appcastItem{}, err
	}
	if len(feed.Channel.Items) != 1 {
		return appcastItem{}, fmt.Errorf("appcast must contain exactly one item, got %d", len(feed.Channel.Items))
	}
	return feed.Channel.Items[0], nil
}

func readLimited(path string, maximum int64) ([]byte, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer file.Close()
	data, err := io.ReadAll(io.LimitReader(file, maximum+1))
	if err != nil {
		return nil, err
	}
	if int64(len(data)) > maximum {
		return nil, fmt.Errorf("%s exceeds %d bytes", filepath.Base(path), maximum)
	}
	return data, nil
}

func validVersion(value string) bool {
	parts := strings.Split(value, ".")
	return len(parts) == 3 && allDigits(parts[0]) && allDigits(parts[1]) && allDigits(parts[2])
}

func allDigits(value string) bool {
	if value == "" {
		return false
	}
	for _, char := range value {
		if char < '0' || char > '9' {
			return false
		}
	}
	return true
}

func requireExactFiles(dir string, expected []string) error {
	want := make(map[string]struct{}, len(expected))
	for _, name := range expected {
		want[name] = struct{}{}
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		return err
	}
	if len(entries) != len(want) {
		return fmt.Errorf("release directory file set mismatch: got %d files, want %d", len(entries), len(want))
	}
	for _, entry := range entries {
		if _, ok := want[entry.Name()]; !ok {
			return fmt.Errorf("unexpected release file: %s", entry.Name())
		}
		info, err := entry.Info()
		if err != nil || !info.Mode().IsRegular() {
			return fmt.Errorf("release entry is not a regular file: %s", entry.Name())
		}
	}
	return nil
}

func plistFromArchive(archivePath string) (map[string]string, error) {
	archive, err := zip.OpenReader(archivePath)
	if err != nil {
		return nil, err
	}
	defer archive.Close()
	const root = "Codex Pulse.app"
	const plistName = root + "/Contents/Info.plist"
	seen := make(map[string]struct{}, len(archive.File))
	var plistFile *zip.File
	var total uint64
	for _, file := range archive.File {
		name := strings.TrimSuffix(file.Name, "/")
		clean := path.Clean(name)
		if name == "" || clean != name || path.IsAbs(name) || strings.Contains(name, "\\") || (name != root && !strings.HasPrefix(name, root+"/")) {
			return nil, fmt.Errorf("unsafe or unexpected ZIP entry: %q", file.Name)
		}
		folded := strings.ToLower(clean)
		if _, duplicate := seen[folded]; duplicate {
			return nil, fmt.Errorf("duplicate or case-conflicting ZIP entry: %q", file.Name)
		}
		seen[folded] = struct{}{}
		mode := file.Mode()
		if mode&os.ModeSymlink != 0 {
			target, err := readZIPEntry(file, 4096)
			if err != nil {
				return nil, fmt.Errorf("read ZIP symlink %q: %w", file.Name, err)
			}
			targetName := string(target)
			resolved := path.Clean(path.Join(path.Dir(clean), targetName))
			if targetName == "" || path.IsAbs(targetName) || strings.Contains(targetName, "\\") || (resolved != root && !strings.HasPrefix(resolved, root+"/")) {
				return nil, fmt.Errorf("unsafe ZIP symlink target: %q -> %q", file.Name, targetName)
			}
		} else if !mode.IsRegular() && !mode.IsDir() {
			return nil, fmt.Errorf("unsupported ZIP entry type: %q", file.Name)
		}
		if file.UncompressedSize64 > 1<<30 || total > (2<<30)-file.UncompressedSize64 {
			return nil, errors.New("ZIP uncompressed size exceeds safety limit")
		}
		total += file.UncompressedSize64
		if clean == plistName {
			plistFile = file
		}
	}
	if plistFile == nil {
		return nil, errors.New("archive does not contain exactly one top-level app Info.plist")
	}
	reader, err := plistFile.Open()
	if err != nil {
		return nil, err
	}
	data, readErr := io.ReadAll(io.LimitReader(reader, 1024*1024+1))
	reader.Close()
	if readErr != nil {
		return nil, readErr
	}
	if len(data) > 1024*1024 {
		return nil, errors.New("archive Info.plist exceeds 1 MiB")
	}
	return parseXMLPlist(data)
}

func readZIPEntry(file *zip.File, maximum int64) ([]byte, error) {
	reader, err := file.Open()
	if err != nil {
		return nil, err
	}
	defer reader.Close()
	data, err := io.ReadAll(io.LimitReader(reader, maximum+1))
	if err != nil {
		return nil, err
	}
	if int64(len(data)) > maximum {
		return nil, fmt.Errorf("ZIP entry exceeds %d bytes", maximum)
	}
	return data, nil
}

func parseXMLPlist(data []byte) (map[string]string, error) {
	decoder := xml.NewDecoder(strings.NewReader(string(data)))
	values := make(map[string]string)
	var key string
	for {
		token, err := decoder.Token()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return nil, err
		}
		start, ok := token.(xml.StartElement)
		if !ok || (start.Name.Local != "key" && start.Name.Local != "string") {
			continue
		}
		var text string
		if err := decoder.DecodeElement(&text, &start); err != nil {
			return nil, err
		}
		if start.Name.Local == "key" {
			key = text
		} else if key != "" {
			values[key] = text
			key = ""
		}
	}
	return values, nil
}
