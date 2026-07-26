package appserver

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"golang.org/x/sys/unix"
)

func TestReadAccountRequestsDisplayFieldsWithoutRefreshingToken(t *testing.T) {
	t.Parallel()

	privateMarker := "secret-token-must-not-survive"
	responses := fmt.Sprintf(
		`{"jsonrpc":"2.0","id":1,"result":{"account":{"type":"chatgpt",`+
			`"email":" person@example.com ","planType":"pro","accessToken":"%s"}}}`+"\n",
		privateMarker,
	)
	writes := &bytes.Buffer{}
	rpc := newJSONLineRPC(nopWriteCloser{writes}, strings.NewReader(responses))

	account, err := readAccount(context.Background(), rpc)
	if err != nil {
		t.Fatalf("readAccount() error = %v", err)
	}
	if account == nil || account.Type != "chatgpt" || account.Email == nil ||
		*account.Email != "person@example.com" || account.PlanType == nil ||
		*account.PlanType != "pro" {
		t.Fatalf("readAccount() = %#v", account)
	}
	written := writes.String()
	if !strings.Contains(written, `"method":"account/read"`) ||
		!strings.Contains(written, `"refreshToken":false`) {
		t.Fatalf("account/read request = %q", written)
	}
	encoded, err := json.Marshal(account)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(encoded), privateMarker) {
		t.Fatalf("account snapshot leaked an undeclared response field: %s", encoded)
	}
}

func TestReadAccountKeepsMissingAccountEmpty(t *testing.T) {
	t.Parallel()

	rpc := newJSONLineRPC(
		nopWriteCloser{io.Discard},
		strings.NewReader(`{"jsonrpc":"2.0","id":1,"result":{"account":null}}`+"\n"),
	)
	account, err := readAccount(context.Background(), rpc)
	if err != nil || account != nil {
		t.Fatalf("readAccount() = %#v, %v", account, err)
	}
}

func TestReadAccountRejectsOversizedDisplayFields(t *testing.T) {
	t.Parallel()

	oversizedPlan := strings.Repeat("p", maxAccountPlanBytes+1)
	response := fmt.Sprintf(
		`{"jsonrpc":"2.0","id":1,"result":{"account":{"type":"chatgpt",`+
			`"email":"person@example.com","planType":"%s"}}}`+"\n",
		oversizedPlan,
	)
	rpc := newJSONLineRPC(nopWriteCloser{io.Discard}, strings.NewReader(response))
	if account, err := readAccount(context.Background(), rpc); err == nil || account != nil {
		t.Fatalf("readAccount() = %#v, %v", account, err)
	}
}

func TestReadLocalAccountUsesConfirmedHomeAndStopsTheProcess(t *testing.T) {
	confirmedHome := t.TempDir()
	if err := os.WriteFile(
		filepath.Join(confirmedHome, "binding-marker"),
		[]byte("confirmed-home\n"),
		0o600,
	); err != nil {
		t.Fatal(err)
	}
	directory := t.TempDir()
	logPath := filepath.Join(directory, "account.log")
	binary := filepath.Join(directory, "codex")
	script := `#!/bin/sh
IFS= read -r binding_marker < "$CODEX_HOME/binding-marker"
printf 'home=%s marker=%s\n' "$CODEX_HOME" "$binding_marker" >> "$CODEX_PULSE_ACCOUNT_TEST_LOG"
while IFS= read -r line; do
  printf '%s\n' "$line" >> "$CODEX_PULSE_ACCOUNT_TEST_LOG"
  id="$(printf '%s\n' "$line" | sed -n 's/.*"id":\([0-9][0-9]*\).*/\1/p')"
  case "$line" in
    *'"method":"initialize"'*)
      printf '{"jsonrpc":"2.0","id":%s,"result":{}}\n' "$id"
      ;;
    *'"method":"account/read"'*)
      printf '{"jsonrpc":"2.0","id":%s,"result":{"account":{"type":"chatgpt","email":"person@example.com","planType":"pro"}}}\n' "$id"
      ;;
  esac
done
`
	if err := os.WriteFile(binary, []byte(script), 0o700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("CODEX_PULSE_ACCOUNT_TEST_LOG", logPath)

	account, err := ReadLocalAccount(
		t.Context(),
		confirmedAccountTestHome(t, confirmedHome, 7),
		ProcessOptions{CodexBinary: binary, ClientName: "account-test", Version: "test"},
	)
	if err != nil {
		t.Fatalf("ReadLocalAccount() error = %v", err)
	}
	if account == nil || account.Email == nil || *account.Email != "person@example.com" {
		t.Fatalf("ReadLocalAccount() = %#v", account)
	}
	content, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatal(err)
	}
	log := string(content)
	if !strings.Contains(log, "home=.") ||
		!strings.Contains(log, "marker=confirmed-home") ||
		!strings.Contains(log, `"method":"account/read"`) ||
		!strings.Contains(log, `"refreshToken":false`) {
		t.Fatalf("App Server account log = %q", log)
	}
}

func TestReadLocalAccountRejectsSamePathReplacementBeforeStartingProcess(t *testing.T) {
	parent := t.TempDir()
	confirmedHome := filepath.Join(parent, "home")
	if err := os.Mkdir(confirmedHome, 0o700); err != nil {
		t.Fatal(err)
	}
	home := confirmedAccountTestHome(t, confirmedHome, 8)

	startedPath := filepath.Join(parent, "started")
	binary := filepath.Join(parent, "codex")
	script := `#!/bin/sh
: > "$CODEX_PULSE_ACCOUNT_STARTED"
exit 1
`
	if err := os.WriteFile(binary, []byte(script), 0o700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("CODEX_PULSE_ACCOUNT_STARTED", startedPath)

	account, err := ReadLocalAccount(
		t.Context(),
		home,
		ProcessOptions{
			CodexBinary: binary,
			BeforeStart: func(context.Context) error {
				if err := os.Rename(
					confirmedHome,
					filepath.Join(parent, "replaced-home"),
				); err != nil {
					return err
				}
				return os.Mkdir(confirmedHome, 0o700)
			},
		},
	)
	if !errors.Is(err, ErrConfirmedHomeChanged) || account != nil {
		t.Fatalf("ReadLocalAccount(replaced before launch) = %#v, %v", account, err)
	}
	if _, statErr := os.Stat(startedPath); !errors.Is(statErr, os.ErrNotExist) {
		t.Fatalf("App Server unexpectedly started, stat error = %v", statErr)
	}
}

func TestReadLocalAccountRejectsReplacementAfterFinalValidationAndRestoreAfterRead(t *testing.T) {
	parent := t.TempDir()
	confirmedHome := filepath.Join(parent, "home")
	if err := os.Mkdir(confirmedHome, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(
		filepath.Join(confirmedHome, "binding-marker"),
		[]byte("confirmed-home\n"),
		0o600,
	); err != nil {
		t.Fatal(err)
	}
	home := confirmedAccountTestHome(t, confirmedHome, 9)
	startedPath := filepath.Join(parent, "started")
	releaseReadPath := filepath.Join(parent, "release-read")
	readCompletePath := filepath.Join(parent, "read-complete")
	releaseResponsePath := filepath.Join(parent, "release-response")
	boundReadPath := filepath.Join(parent, "bound-read")
	binary := filepath.Join(parent, "codex")
	script := `#!/bin/sh
while IFS= read -r line; do
  id="$(printf '%s\n' "$line" | sed -n 's/.*"id":\([0-9][0-9]*\).*/\1/p')"
  case "$line" in
    *'"method":"initialize"'*)
      printf '{"jsonrpc":"2.0","id":%s,"result":{}}\n' "$id"
      ;;
    *'"method":"account/read"'*)
      : > "$CODEX_PULSE_ACCOUNT_STARTED"
      while [ ! -e "$CODEX_PULSE_ACCOUNT_RELEASE_READ" ]; do sleep 0.01; done
      IFS= read -r binding_marker < "$CODEX_HOME/binding-marker"
      printf '%s\n' "$binding_marker" > "$CODEX_PULSE_ACCOUNT_BOUND_READ"
      : > "$CODEX_PULSE_ACCOUNT_READ_COMPLETE"
      while [ ! -e "$CODEX_PULSE_ACCOUNT_RELEASE_RESPONSE" ]; do sleep 0.01; done
      printf '{"jsonrpc":"2.0","id":%s,"result":{"account":{"type":"chatgpt","email":"%s@example.com","planType":"pro"}}}\n' "$id" "$binding_marker"
      ;;
  esac
done
`
	if err := os.WriteFile(binary, []byte(script), 0o700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("CODEX_PULSE_ACCOUNT_STARTED", startedPath)
	t.Setenv("CODEX_PULSE_ACCOUNT_RELEASE_READ", releaseReadPath)
	t.Setenv("CODEX_PULSE_ACCOUNT_READ_COMPLETE", readCompletePath)
	t.Setenv("CODEX_PULSE_ACCOUNT_RELEASE_RESPONSE", releaseResponsePath)
	t.Setenv("CODEX_PULSE_ACCOUNT_BOUND_READ", boundReadPath)

	type result struct {
		account *AccountSnapshot
		err     error
	}
	done := make(chan result, 1)
	go func() {
		account, err := ReadLocalAccount(
			t.Context(),
			home,
			ProcessOptions{
				CodexBinary: binary,
				afterBeforeStartForTest: func() error {
					if err := os.Rename(
						confirmedHome,
						filepath.Join(parent, "replaced-home"),
					); err != nil {
						return err
					}
					if err := os.Mkdir(confirmedHome, 0o700); err != nil {
						return err
					}
					return os.WriteFile(
						filepath.Join(confirmedHome, "binding-marker"),
						[]byte("replacement-home\n"),
						0o600,
					)
				},
			},
		)
		done <- result{account: account, err: err}
	}()
	waitForAccountTestPath(t, startedPath)
	if err := os.WriteFile(releaseReadPath, []byte("release"), 0o600); err != nil {
		t.Fatal(err)
	}
	waitForAccountTestPath(t, readCompletePath)
	if err := os.Remove(filepath.Join(confirmedHome, "binding-marker")); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(confirmedHome); err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(filepath.Join(parent, "replaced-home"), confirmedHome); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(releaseResponsePath, []byte("release"), 0o600); err != nil {
		t.Fatal(err)
	}

	select {
	case got := <-done:
		if !errors.Is(got.err, ErrConfirmedHomeChanged) || got.account != nil {
			t.Fatalf("ReadLocalAccount(replaced and restored) = %#v, %v", got.account, got.err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("ReadLocalAccount(replaced and restored) did not finish")
	}
	boundRead, err := os.ReadFile(boundReadPath)
	if err != nil {
		t.Fatal(err)
	}
	if strings.TrimSpace(string(boundRead)) != "confirmed-home" {
		t.Fatalf("App Server read physical Home marker = %q", boundRead)
	}
}

func TestReadLocalAccountIgnoresLargeAndActivelyWrittenSessionTree(t *testing.T) {
	t.Parallel()

	confirmedHome := t.TempDir()
	sessions := filepath.Join(confirmedHome, "sessions")
	if err := os.Mkdir(sessions, 0o700); err != nil {
		t.Fatal(err)
	}
	for index := range 2_000 {
		path := filepath.Join(sessions, fmt.Sprintf("session-%04d.jsonl", index))
		if err := os.WriteFile(path, []byte("{}\n"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.Mkdir(filepath.Join(sessions, "unsupported.jsonl"), 0o700); err != nil {
		t.Fatal(err)
	}
	activeSession := filepath.Join(sessions, "active.jsonl")
	if err := os.WriteFile(activeSession, []byte("{}\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	directory := t.TempDir()
	binary := filepath.Join(directory, "codex")
	script := `#!/bin/sh
while IFS= read -r line; do
  id="$(printf '%s\n' "$line" | sed -n 's/.*"id":\([0-9][0-9]*\).*/\1/p')"
  case "$line" in
    *'"method":"initialize"'*)
      printf '{"jsonrpc":"2.0","id":%s,"result":{}}\n' "$id"
      ;;
    *'"method":"account/read"'*)
      printf '{"jsonrpc":"2.0","id":%s,"result":{"account":{"type":"chatgpt","email":"person@example.com","planType":"pro"}}}\n' "$id"
      ;;
  esac
done
`
	if err := os.WriteFile(binary, []byte(script), 0o700); err != nil {
		t.Fatal(err)
	}

	writerContext, stopWriter := context.WithCancel(t.Context())
	writerDone := make(chan error, 1)
	firstWrite := make(chan struct{})
	var firstWriteOnce sync.Once
	go func() {
		for {
			select {
			case <-writerContext.Done():
				writerDone <- nil
				return
			default:
			}
			file, err := os.OpenFile(activeSession, os.O_WRONLY|os.O_APPEND, 0)
			if err != nil {
				writerDone <- err
				return
			}
			_, writeErr := file.WriteString("{}\n")
			closeErr := file.Close()
			if writeErr != nil {
				writerDone <- writeErr
				return
			}
			if closeErr != nil {
				writerDone <- closeErr
				return
			}
			firstWriteOnce.Do(func() { close(firstWrite) })
			time.Sleep(time.Millisecond)
		}
	}()
	<-firstWrite

	readContext, cancelRead := context.WithTimeout(t.Context(), 3*time.Second)
	account, err := ReadLocalAccount(
		readContext,
		confirmedAccountTestHome(t, confirmedHome, 10),
		ProcessOptions{CodexBinary: binary},
	)
	cancelRead()
	stopWriter()
	if writerErr := <-writerDone; writerErr != nil {
		t.Fatalf("concurrent Session writer error = %v", writerErr)
	}
	if err != nil {
		t.Fatalf("ReadLocalAccount(large active Session tree) error = %v", err)
	}
	if account == nil || account.Email == nil || *account.Email != "person@example.com" {
		t.Fatalf("ReadLocalAccount(large active Session tree) = %#v", account)
	}
}

func confirmedAccountTestHome(t testing.TB, path string, generation int64) ConfirmedHome {
	t.Helper()
	canonicalPath, err := filepath.EvalSymlinks(path)
	if err != nil {
		t.Fatalf("filepath.EvalSymlinks() error = %v", err)
	}
	var stat unix.Stat_t
	if err := unix.Stat(canonicalPath, &stat); err != nil {
		t.Fatalf("unix.Stat() error = %v", err)
	}
	return ConfirmedHome{
		Generation: generation,
		Path:       canonicalPath,
		DeviceID:   strconv.FormatUint(uint64(uint32(stat.Dev)), 10),
		Inode:      int64(stat.Ino),
	}
}

func waitForAccountTestPath(t testing.TB, path string) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if _, err := os.Stat(path); err == nil {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for %s", filepath.Base(path))
}
