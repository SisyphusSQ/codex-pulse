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
	"strings"
	"testing"
	"time"

	"github.com/SisyphusSQ/codex-pulse/internal/codex/logs"
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
	directory := t.TempDir()
	logPath := filepath.Join(directory, "account.log")
	binary := filepath.Join(directory, "codex")
	script := `#!/bin/sh
printf 'home=%s\n' "$CODEX_HOME" >> "$CODEX_PULSE_ACCOUNT_TEST_LOG"
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
	canonicalHome, err := filepath.EvalSymlinks(confirmedHome)
	if err != nil {
		t.Fatal(err)
	}
	log := string(content)
	if !strings.Contains(log, "home="+canonicalHome) ||
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

func TestReadLocalAccountDiscardsResultAfterSamePathReplacement(t *testing.T) {
	parent := t.TempDir()
	confirmedHome := filepath.Join(parent, "home")
	if err := os.Mkdir(confirmedHome, 0o700); err != nil {
		t.Fatal(err)
	}
	home := confirmedAccountTestHome(t, confirmedHome, 9)
	startedPath := filepath.Join(parent, "started")
	releasePath := filepath.Join(parent, "release")
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
      while [ ! -e "$CODEX_PULSE_ACCOUNT_RELEASE" ]; do sleep 0.01; done
      printf '{"jsonrpc":"2.0","id":%s,"result":{"account":{"type":"chatgpt","email":"wrong@example.com","planType":"pro"}}}\n' "$id"
      ;;
  esac
done
`
	if err := os.WriteFile(binary, []byte(script), 0o700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("CODEX_PULSE_ACCOUNT_STARTED", startedPath)
	t.Setenv("CODEX_PULSE_ACCOUNT_RELEASE", releasePath)

	type result struct {
		account *AccountSnapshot
		err     error
	}
	done := make(chan result, 1)
	go func() {
		account, err := ReadLocalAccount(
			t.Context(),
			home,
			ProcessOptions{CodexBinary: binary},
		)
		done <- result{account: account, err: err}
	}()
	waitForAccountTestPath(t, startedPath)
	if err := os.Rename(confirmedHome, filepath.Join(parent, "replaced-home")); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(confirmedHome, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(releasePath, []byte("release"), 0o600); err != nil {
		t.Fatal(err)
	}

	select {
	case got := <-done:
		if !errors.Is(got.err, ErrConfirmedHomeChanged) || got.account != nil {
			t.Fatalf("ReadLocalAccount(replaced after read) = %#v, %v", got.account, got.err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("ReadLocalAccount(replaced after read) did not finish")
	}
}

func confirmedAccountTestHome(t testing.TB, path string, generation int64) ConfirmedHome {
	t.Helper()
	metadata, err := logs.NewHomeProbe().Probe(context.Background(), path)
	if err != nil {
		t.Fatalf("HomeProbe.Probe() error = %v", err)
	}
	return ConfirmedHome{
		Generation: generation,
		Path:       metadata.Path,
		DeviceID:   metadata.DeviceID,
		Inode:      metadata.Inode,
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
