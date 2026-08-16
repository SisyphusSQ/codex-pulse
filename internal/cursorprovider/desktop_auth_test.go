package cursorprovider

import (
	"context"
	"database/sql"
	"encoding/base64"
	"fmt"
	"path/filepath"
	"testing"
	"time"

	_ "modernc.org/sqlite"
)

func TestDesktopAuthReaderReadsCommittedWALAccessToken(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "state.vscdb")
	database, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatalf("sql.Open() error = %v", err)
	}
	t.Cleanup(func() { _ = database.Close() })
	if _, err := database.Exec(`PRAGMA journal_mode=WAL`); err != nil {
		t.Fatalf("enable WAL error = %v", err)
	}
	if _, err := database.Exec(`CREATE TABLE ItemTable (key TEXT PRIMARY KEY, value TEXT NOT NULL)`); err != nil {
		t.Fatalf("create ItemTable error = %v", err)
	}

	now := time.Unix(1_800_000_000, 0)
	token := unsignedJWT(t, now.Add(time.Hour).Unix())
	if _, err := database.Exec(`INSERT INTO ItemTable(key, value) VALUES (?, ?)`, "cursorAuth/accessToken", token); err != nil {
		t.Fatalf("insert access token error = %v", err)
	}

	reader, err := NewDesktopAuthReader(path, func() time.Time { return now })
	if err != nil {
		t.Fatalf("NewDesktopAuthReader() error = %v", err)
	}
	credential, err := reader.ReadAccessToken(context.Background())
	if err != nil {
		t.Fatalf("ReadAccessToken() error = %v", err)
	}
	if credential.Token != token {
		t.Fatal("ReadAccessToken() returned a different token")
	}
	if got, want := credential.ExpiresAt.Unix(), now.Add(time.Hour).Unix(); got != want {
		t.Fatalf("ReadAccessToken() expiry = %d, want %d", got, want)
	}
}

func unsignedJWT(t *testing.T, expiresAt int64) string {
	t.Helper()
	header := base64.RawURLEncoding.EncodeToString([]byte(`{"alg":"none","typ":"JWT"}`))
	payload := base64.RawURLEncoding.EncodeToString([]byte(fmt.Sprintf(`{"exp":%d,"sub":"fixture"}`, expiresAt)))
	return header + "." + payload + ".fixture-signature"
}
