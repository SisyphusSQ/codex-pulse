package appserver

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
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
		confirmedHome,
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
