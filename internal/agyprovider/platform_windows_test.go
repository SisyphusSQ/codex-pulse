//go:build windows

package agyprovider

import (
	"database/sql"
	"errors"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	_ "modernc.org/sqlite"

	"github.com/SisyphusSQ/codex-pulse/internal/platformfs"
)

func TestPinSourceDatabaseRejectsRelativeAndUnsafePaths(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	cases := []struct {
		name string
		path string
	}{
		{name: "relative path", path: "conversation_summaries.db"},
		{name: "relative nested", path: filepath.Join("sub", "conversation_summaries.db")},
		{name: "alternate data stream colon", path: filepath.Join(dir, "summaries.db:stream")},
		{name: "directory path", path: dir},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			release, err := pinSourceDatabase(tc.path)
			if release != nil {
				release()
			}
			if !errors.Is(err, platformfs.ErrUnsafe) {
				t.Fatalf("pinSourceDatabase(%q) err = %v, want %v", tc.path, err, platformfs.ErrUnsafe)
			}
		})
	}
}

func TestPinSourceDatabaseWALAwareReadOnlySQLite(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	dbPath := filepath.Join(dir, "conversation_summaries.db")

	writer, err := sql.Open("sqlite", dbPath)
	if err != nil {
		t.Fatalf("sql.Open writer: %v", err)
	}
	defer writer.Close()

	if _, err := writer.Exec(`PRAGMA journal_mode=WAL; CREATE TABLE items(id INTEGER PRIMARY KEY, val TEXT); INSERT INTO items(id, val) VALUES (1, 'ok');`); err != nil {
		t.Fatalf("seed WAL database: %v", err)
	}

	// 保持写事务未 checkpoint，验证 -wal / -shm 存在时可钉住并只读查询
	release, err := pinSourceDatabase(dbPath)
	if err != nil {
		t.Fatalf("pinSourceDatabase(%q) err = %v", dbPath, err)
	}
	defer release()

	reader, err := sql.Open("sqlite", "file:"+filepath.ToSlash(dbPath)+"?mode=ro&_pragma=query_only(1)&_pragma=busy_timeout(3000)")
	if err != nil {
		t.Fatalf("sql.Open reader: %v", err)
	}
	defer reader.Close()

	var val string
	if err := reader.QueryRow(`SELECT val FROM items WHERE id = 1`).Scan(&val); err != nil {
		t.Fatalf("read pinned WAL sqlite: %v", err)
	}
	if val != "ok" {
		t.Fatalf("read val = %q, want %q", val, "ok")
	}
}

func TestOpenRegularFileReadOnlyValidationAndRead(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	filePath := filepath.Join(dir, "transcript.jsonl")
	want := `{"type":"assistant"}` + "\n"
	if err := os.WriteFile(filePath, []byte(want), 0o600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	// 拒绝相对路径、ADS 冒号及目录
	for _, bad := range []string{"transcript.jsonl", filepath.Join(dir, "transcript.jsonl:ads"), dir} {
		f, _, err := openRegularFileReadOnly(bad)
		if f != nil {
			_ = f.Close()
		}
		if !errors.Is(err, platformfs.ErrUnsafe) {
			t.Fatalf("openRegularFileReadOnly(%q) err = %v, want %v", bad, err, platformfs.ErrUnsafe)
		}
	}

	f, info, err := openRegularFileReadOnly(filePath)
	if err != nil {
		t.Fatalf("openRegularFileReadOnly(%q) err = %v", filePath, err)
	}
	defer f.Close()

	if !info.Mode().IsRegular() || info.Size() != int64(len(want)) {
		t.Fatalf("unexpected FileInfo mode=%v size=%d", info.Mode(), info.Size())
	}
	got, err := io.ReadAll(f)
	if err != nil {
		t.Fatalf("ReadAll: %v", err)
	}
	if string(got) != want {
		t.Fatalf("content = %q, want %q", string(got), want)
	}
}

func TestParseWindowsProcessJSONSingleAndArray(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name string
		raw  string
		want []osProcess
	}{
		{
			name: "empty",
			raw:  "",
			want: nil,
		},
		{
			name: "malformed json",
			raw:  "{invalid",
			want: nil,
		},
		{
			name: "single object from ConvertTo-Json",
			raw:  `{"ProcessId":4210,"ParentProcessId":1024,"CommandLine":"language_server_windows_x64.exe --csrf_token=tok123 --extension_server_port=51234"}`,
			want: []osProcess{
				{PID: 4210, PPID: 1024, CommandLine: "language_server_windows_x64.exe --csrf_token=tok123 --extension_server_port=51234"},
			},
		},
		{
			name: "array from ConvertTo-Json filters non-positive pid",
			raw: `[
				{"ProcessId":0,"ParentProcessId":1,"CommandLine":"ignored"},
				{"ProcessId":1001,"ParentProcessId":500,"CommandLine":"agy.exe"},
				{"ProcessId":2002,"ParentProcessId":1001,"CommandLine":"language_server_windows_x64.exe --csrf_token=xyz"}
			]`,
			want: []osProcess{
				{PID: 1001, PPID: 500, CommandLine: "agy.exe"},
				{PID: 2002, PPID: 1001, CommandLine: "language_server_windows_x64.exe --csrf_token=xyz"},
			},
		},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := parseWindowsProcessJSON(tc.raw)
			if !reflect.DeepEqual(got, tc.want) {
				t.Fatalf("parseWindowsProcessJSON() = %#v, want %#v", got, tc.want)
			}
		})
	}
}

func TestParseWindowsPortsJSONSingleAndArray(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name string
		raw  string
		want []int
	}{
		{
			name: "empty",
			raw:  "",
			want: nil,
		},
		{
			name: "malformed json",
			raw:  "[not-a-port]",
			want: nil,
		},
		{
			name: "single scalar port from ConvertTo-Json",
			raw:  "43123",
			want: []int{43123},
		},
		{
			name: "single invalid port",
			raw:  "70000",
			want: nil,
		},
		{
			name: "array deduplicates and drops out-of-range ports",
			raw:  "[43123, 0, -5, 65536, 43123, 43124, 65535]",
			want: []int{43123, 43124, 65535},
		},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := parseWindowsPortsJSON(tc.raw)
			if !reflect.DeepEqual(got, tc.want) {
				t.Fatalf("parseWindowsPortsJSON() = %#v, want %#v", got, tc.want)
			}
		})
	}
}
