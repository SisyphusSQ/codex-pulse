//go:build windows

package cursorprovider

import (
	"encoding/binary"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"
	"unsafe"

	"golang.org/x/sys/windows"

	"github.com/SisyphusSQ/codex-pulse/internal/platformfs"
	"github.com/SisyphusSQ/codex-pulse/internal/providercontrol"
	"github.com/SisyphusSQ/codex-pulse/internal/store"
	storesqlite "github.com/SisyphusSQ/codex-pulse/internal/store/sqlite"
)

func TestWindowsCursorDefaultCredentialWALToDashboardFixture(t *testing.T) {
	home := t.TempDir()
	t.Setenv("CODEX_PULSE_CURSOR_HOME", home)
	t.Setenv("CODEX_PULSE_CURSOR_USER_DATA_DIR", "")
	config, err := DefaultConfig()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Dir(config.StateDatabase), 0700); err != nil {
		t.Fatal(err)
	}
	now := time.Unix(1800000000, 0)
	token := unsignedJWT(t, now.Add(time.Hour).Unix())
	database := openFixtureDatabase(t, config.StateDatabase)
	defer database.Close()
	mustExec(t, database, `PRAGMA journal_mode=WAL`)
	mustExec(t, database, `CREATE TABLE ItemTable(key TEXT PRIMARY KEY,value TEXT)`)
	mustExec(t, database, `INSERT INTO ItemTable VALUES ('cursorAuth/accessToken',?)`, token)
	reader, err := NewDesktopAuthReader(config.StateDatabase, func() time.Time { return now })
	if err != nil {
		t.Fatal(err)
	}
	var calls atomic.Int64
	var rejected atomic.Bool
	var authenticated atomic.Bool
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		authenticated.Store(r.Header.Get("Authorization") == "Bearer "+token)
		if rejected.Load() {
			w.WriteHeader(http.StatusUnauthorized)
			_, _ = w.Write([]byte("untrusted-body-marker"))
			return
		}
		w.Header().Set("Content-Type", "application/proto")
		_, _ = w.Write(currentPeriodFixture())
	}))
	defer server.Close()
	client, err := NewDashboardClient(DashboardClientConfig{BaseURL: server.URL, HTTPClient: server.Client(), TokenSource: reader})
	if err != nil {
		t.Fatal(err)
	}
	value, err := client.GetCurrentPeriodUsage(t.Context())
	if err != nil || !authenticated.Load() || value.PlanUsage == nil {
		t.Fatal("default Windows WAL credential did not reach the isolated Dashboard")
	}
	rejected.Store(true)
	_, err = client.GetCurrentPeriodUsage(t.Context())
	if !errors.Is(err, ErrDashboardAuthRejected) || strings.Contains(err.Error(), token) || strings.Contains(err.Error(), "untrusted-body-marker") {
		t.Fatal("authentication failure leaked input or changed classification")
	}
	mustExec(t, database, `UPDATE ItemTable SET value=? WHERE key='cursorAuth/accessToken'`, unsignedJWT(t, now.Add(-time.Hour).Unix()))
	before := calls.Load()
	_, err = client.GetCurrentPeriodUsage(t.Context())
	if !errors.Is(err, ErrDesktopAuthExpired) || calls.Load() != before {
		t.Fatal("expired local credential made a remote request")
	}
}

func TestWindowsCursorDiscoveryNeedsNoCredentialBodyPermission(t *testing.T) {
	home := t.TempDir()
	t.Setenv("CODEX_PULSE_CURSOR_HOME", home)
	t.Setenv("CODEX_PULSE_CURSOR_USER_DATA_DIR", "")
	config, err := DefaultConfig()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Dir(config.StateDatabase), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(config.StateDatabase, []byte("body-read-forbidden"), 0600); err != nil {
		t.Fatal(err)
	}
	user, err := windows.GetCurrentProcessToken().GetTokenUser()
	if err != nil {
		t.Fatal(err)
	}
	sd, err := windows.SecurityDescriptorFromString("D:P(D;;0x1;;;" + user.User.Sid.String() + ")(A;;FA;;;" + user.User.Sid.String() + ")(A;;FA;;;SY)")
	if err != nil {
		t.Fatal(err)
	}
	acl, _, err := sd.DACL()
	if err != nil {
		t.Fatal(err)
	}
	if err := windows.SetNamedSecurityInfo(config.StateDatabase, windows.SE_FILE_OBJECT, windows.DACL_SECURITY_INFORMATION|windows.PROTECTED_DACL_SECURITY_INFORMATION, nil, nil, acl, nil); err != nil {
		t.Fatal(err)
	}
	probe := providercontrol.DefaultProbes(nil, nil).Cursor(t.Context())
	if probe.State != providercontrol.DiscoveryAvailable {
		t.Fatalf("metadata-only discovery failed: %#v", probe)
	}
	file, err := os.Open(config.StateDatabase)
	if err == nil {
		defer file.Close()
		t.Fatal("fixture did not deny credential body reads")
	}
}

func TestWindowsCursorSnapshotURIWALAndReadOnly(t *testing.T) {
	t.Parallel()
	root := filepath.Join(t.TempDir(), strings.Repeat("a", 110), strings.Repeat("b", 110), "中文 空格 # % &")
	if err := os.MkdirAll(root, 0700); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(root, "state.vscdb")
	writer := openFixtureDatabase(t, path)
	t.Cleanup(func() { _ = writer.Close() })
	mustExec(t, writer, `PRAGMA journal_mode=WAL`)
	mustExec(t, writer, `CREATE TABLE fixture(value INTEGER)`)
	mustExec(t, writer, `INSERT INTO fixture VALUES (1)`)
	database, tx, _, err := openReadSnapshot(t.Context(), path)
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	defer tx.Rollback()
	var count int
	if err := tx.QueryRowContext(t.Context(), `SELECT COUNT(*) FROM fixture`).Scan(&count); err != nil || count != 1 {
		t.Fatal("initial WAL snapshot unavailable")
	}
	mustExec(t, writer, `INSERT INTO fixture VALUES (2)`)
	if err := tx.QueryRowContext(t.Context(), `SELECT COUNT(*) FROM fixture`).Scan(&count); err != nil || count != 1 {
		t.Fatal("read transaction changed under concurrent WAL commit")
	}
	if _, err := tx.ExecContext(t.Context(), `INSERT INTO fixture VALUES (3)`); err == nil {
		t.Fatal("source snapshot allowed writes")
	}
	_ = tx.Rollback()
	_ = database.Close()
	database, tx, _, err = openReadSnapshot(t.Context(), path)
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	defer tx.Rollback()
	if err := tx.QueryRowContext(t.Context(), `SELECT COUNT(*) FROM fixture`).Scan(&count); err != nil || count != 2 {
		t.Fatal("new snapshot did not see committed WAL")
	}
}

func TestWindowsCursorDatabasePinAndSharingFailure(t *testing.T) {
	t.Parallel()
	path := filepath.Join(t.TempDir(), "state.vscdb")
	database := openFixtureDatabase(t, path)
	mustExec(t, database, `CREATE TABLE ItemTable(key TEXT,value TEXT)`)
	_ = database.Close()
	release, err := pinSourceDatabase(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(path, path+".moved"); err == nil {
		release()
		t.Fatal("pinned source database was replaceable")
	}
	release()
	if err := os.Rename(path, path+".moved"); err != nil {
		t.Fatal("source pin was not released")
	}
	if err := os.Rename(path+".moved", path); err != nil {
		t.Fatal(err)
	}
	name, _ := windows.UTF16PtrFromString(path)
	lock, err := windows.CreateFile(name, windows.GENERIC_READ|windows.GENERIC_WRITE, 0, nil, windows.OPEN_EXISTING, 0, 0)
	if err != nil {
		t.Fatal(err)
	}
	_, _, _, readErr := openReadSnapshot(t.Context(), path)
	_ = windows.CloseHandle(lock)
	if readErr == nil {
		t.Fatal("exclusive source lock was ignored")
	}
	opened, tx, _, err := openReadSnapshot(t.Context(), path)
	if err != nil {
		t.Fatal("source did not recover after lock release")
	}
	_ = tx.Rollback()
	_ = opened.Close()
}

func TestWindowsCursorRejectsJunctionCompanionsAndAlternateStreams(t *testing.T) {
	home := t.TempDir()
	outside := t.TempDir()
	storage := filepath.Join(outside, "User", "globalStorage")
	if err := os.MkdirAll(storage, 0700); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(storage, "state.vscdb")
	database := openFixtureDatabase(t, path)
	mustExec(t, database, `CREATE TABLE ItemTable(key TEXT,value TEXT)`)
	_ = database.Close()
	link := filepath.Join(home, "linked-data")
	cursorTestJunction(t, link, outside)
	t.Setenv("CODEX_PULSE_CURSOR_HOME", home)
	t.Setenv("CODEX_PULSE_CURSOR_USER_DATA_DIR", link)
	config, err := DefaultConfig()
	if err != nil {
		t.Fatal(err)
	}
	reader, err := NewDesktopAuthReader(config.StateDatabase, time.Now)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = reader.ReadAccessToken(t.Context()); !errors.Is(err, ErrDesktopAuthUnavailable) {
		t.Fatal("credential reader followed a directory junction")
	}
	probe := providercontrol.DefaultProbes(nil, nil).Cursor(t.Context())
	if probe.State != providercontrol.DiscoveryInvalid || probe.ReasonCode != providercontrol.ReasonUnsafePath {
		t.Fatalf("unsafe source discovery: %#v", probe)
	}
	if _, _, _, err := openReadSnapshot(t.Context(), path+":stream"); err == nil {
		t.Fatal("alternate data stream accepted")
	}
	cursorTestJunction(t, path+"-wal", t.TempDir())
	if _, _, _, err := openReadSnapshot(t.Context(), path); err == nil {
		t.Fatal("SQLite reparse companion accepted")
	}
	projects := filepath.Join(home, "projects")
	if err := os.Mkdir(projects, 0700); err != nil {
		t.Fatal(err)
	}
	transcripts := t.TempDir()
	if err := os.WriteFile(filepath.Join(transcripts, "fake.jsonl"), []byte(`{"message":{"content":[{"type":"tool_use","name":"private"}]}}`), 0600); err != nil {
		t.Fatal(err)
	}
	cursorTestJunction(t, filepath.Join(projects, "agent-transcripts"), transcripts)
	visits := 0
	_, err = walkTranscriptFiles(t.Context(), projects, func(string, *os.File) error { visits++; return nil })
	if visits != 0 || err == nil {
		t.Fatal("transcript traversal followed a directory junction")
	}
}

func TestWindowsCursorLockedCollectorRetainsCommittedGeneration(t *testing.T) {
	t.Parallel()
	ctx := t.Context()
	root := t.TempDir()
	private := filepath.Join(root, "private")
	if err := platformfs.Mkdir(private, 0700); err != nil {
		t.Fatal(err)
	}
	db, err := storesqlite.Open(ctx, storesqlite.Config{Path: filepath.Join(private, "facts.db")})
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close(ctx)
	repository := store.NewRepository(db)
	if err := repository.EnsureApplicationSchema(ctx); err != nil {
		t.Fatal(err)
	}
	state := filepath.Join(root, "state.vscdb")
	source := openFixtureDatabase(t, state)
	mustExec(t, source, `PRAGMA user_version=1`)
	mustExec(t, source, `CREATE TABLE composerHeaders(composerId TEXT,createdAt INTEGER,lastUpdatedAt INTEGER,value TEXT)`)
	mustExec(t, source, `CREATE TABLE cursorDiskKV(key TEXT,value TEXT)`)
	mustExec(t, source, `INSERT INTO composerHeaders VALUES ('stable',1000,1500,'{}')`)
	_ = source.Close()
	clock := time.UnixMilli(2000)
	collector, err := NewCollector(repository, Config{ProjectsRoot: filepath.Join(root, "projects"), StateDatabase: state, ConversationDatabase: filepath.Join(root, "conversation.db"), AITrackingDatabase: filepath.Join(root, "tracking.db"), Now: func() time.Time { return clock }})
	if err != nil {
		t.Fatal(err)
	}
	if err := collector.Refresh(ctx); err != nil {
		t.Fatal(err)
	}
	name, _ := windows.UTF16PtrFromString(state)
	lock, err := windows.CreateFile(name, windows.GENERIC_READ|windows.GENERIC_WRITE, 0, nil, windows.OPEN_EXISTING, 0, 0)
	if err != nil {
		t.Fatal(err)
	}
	clock = time.UnixMilli(3000)
	collector.config.MinimumRefresh = time.Second
	refreshErr := collector.Refresh(ctx)
	clock = time.UnixMilli(3500)
	if attempted, err := collector.RefreshIfDue(ctx); attempted || err != nil {
		_ = windows.CloseHandle(lock)
		t.Fatal("known source failure caused a rapid retry instead of honoring the refresh interval")
	}
	_ = windows.CloseHandle(lock)
	if !errors.Is(refreshErr, store.ErrCursorSourceUnavailable) {
		t.Fatalf("locked collector outcome: %v", refreshErr)
	}
	value, err := repository.CursorSnapshot(ctx)
	if err != nil || value.Generation != 2000 || len(value.Sessions) != 1 {
		t.Fatal("lock replaced the last successful generation")
	}
	stateStatus := sourceStatus(value.Sources, SourceState)
	if stateStatus == nil || stateStatus.LastSuccessAtMS == nil || *stateStatus.LastSuccessAtMS != 2000 || stateStatus.LastAttemptAtMS != 3000 || stateStatus.FailureCode == nil || *stateStatus.FailureCode != "read_failed" {
		t.Fatal("lock did not retain finite failure and success evidence")
	}
	clock = time.UnixMilli(4000)
	if err := collector.Refresh(ctx); err != nil {
		t.Fatal(err)
	}
	value, err = repository.CursorSnapshot(ctx)
	if err != nil || value.Generation != 4000 || len(value.Sessions) != 1 {
		t.Fatal("collector did not recover after lock release")
	}
}

func cursorTestJunction(t *testing.T, link, target string) {
	t.Helper()
	if err := os.Mkdir(link, 0700); err != nil {
		t.Fatal(err)
	}
	name, _ := windows.UTF16PtrFromString(link)
	handle, err := windows.CreateFile(name, windows.GENERIC_WRITE, 0, nil, windows.OPEN_EXISTING, windows.FILE_FLAG_OPEN_REPARSE_POINT|windows.FILE_FLAG_BACKUP_SEMANTICS, 0)
	if err != nil {
		t.Fatal(err)
	}
	defer windows.CloseHandle(handle)
	substitute, _ := windows.UTF16FromString(`\??\` + target)
	printName, _ := windows.UTF16FromString(target)
	data := make([]byte, 16+2*(len(substitute)+len(printName)))
	binary.LittleEndian.PutUint32(data, windows.IO_REPARSE_TAG_MOUNT_POINT)
	binary.LittleEndian.PutUint16(data[4:], uint16(len(data)-8))
	binary.LittleEndian.PutUint16(data[10:], uint16(2*(len(substitute)-1)))
	binary.LittleEndian.PutUint16(data[12:], uint16(2*len(substitute)))
	binary.LittleEndian.PutUint16(data[14:], uint16(2*(len(printName)-1)))
	for index, value := range append(substitute, printName...) {
		binary.LittleEndian.PutUint16(data[16+2*index:], value)
	}
	var returned uint32
	if err := windows.DeviceIoControl(handle, windows.FSCTL_SET_REPARSE_POINT, (*byte)(unsafe.Pointer(&data[0])), uint32(len(data)), nil, 0, &returned, nil); err != nil {
		t.Fatal(err)
	}
}
