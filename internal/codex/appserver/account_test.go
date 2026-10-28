package appserver

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/SisyphusSQ/codex-pulse/internal/codex/homeidentity"
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

func TestReadAccountSandwichRequiresMatchingAccountIDs(t *testing.T) {
	t.Parallel()

	rateLimits := `{"accountId":"acct-test-a","rateLimits":{"primary":{"usedPercent":3,"windowDurationMins":5,"resetsAt":1784008800}}}`
	account := `{"account":{"type":"chatgpt","email":"a@example.com","planType":"plus"}}`
	responses := strings.Join([]string{
		`{"jsonrpc":"2.0","id":1,"result":` + rateLimits + `}`,
		`{"jsonrpc":"2.0","id":2,"result":` + account + `}`,
		`{"jsonrpc":"2.0","id":3,"result":` + rateLimits + `}`,
	}, "\n") + "\n"
	writes := &bytes.Buffer{}
	rpc := newJSONLineRPC(nopWriteCloser{writes}, strings.NewReader(responses))
	sandwich, err := readAccountSandwich(context.Background(), rpc)
	if err != nil {
		t.Fatalf("readAccountSandwich() error = %v", err)
	}
	if string(sandwich.BeforeID) != "acct-test-a" || string(sandwich.AfterID) != "acct-test-a" ||
		sandwich.Account == nil || sandwich.Account.Email == nil || *sandwich.Account.Email != "a@example.com" {
		t.Fatalf("readAccountSandwich() = %#v", sandwich)
	}
	written := writes.String()
	if !strings.Contains(written, `"method":"account/rateLimits/read"`) ||
		!strings.Contains(written, `"method":"account/read"`) ||
		strings.Count(written, `"method":"account/rateLimits/read"`) != 2 {
		t.Fatalf("sandwich methods = %q", written)
	}
	if strings.Contains(written, `"refreshToken":true`) {
		t.Fatal("account/read refreshed token")
	}
}

func TestReadAccountSandwichPreservesMismatchedIDsWithoutMixingDisplay(t *testing.T) {
	t.Parallel()

	before := `{"accountId":"acct-test-a","rateLimits":{"primary":{"usedPercent":3,"windowDurationMins":5,"resetsAt":1784008800}}}`
	account := `{"account":{"type":"chatgpt","email":"b@example.com","planType":"plus"}}`
	after := `{"accountId":"acct-test-b","rateLimits":{"primary":{"usedPercent":4,"windowDurationMins":5,"resetsAt":1784008800}}}`
	responses := strings.Join([]string{
		`{"jsonrpc":"2.0","id":1,"result":` + before + `}`,
		`{"jsonrpc":"2.0","id":2,"result":` + account + `}`,
		`{"jsonrpc":"2.0","id":3,"result":` + after + `}`,
	}, "\n") + "\n"
	rpc := newJSONLineRPC(nopWriteCloser{io.Discard}, strings.NewReader(responses))
	sandwich, err := readAccountSandwich(context.Background(), rpc)
	if err != nil {
		t.Fatalf("readAccountSandwich(mismatch) error = %v", err)
	}
	if string(sandwich.BeforeID) != "acct-test-a" || string(sandwich.AfterID) != "acct-test-b" ||
		sandwich.Account == nil || sandwich.Account.Email == nil || *sandwich.Account.Email != "b@example.com" {
		t.Fatalf("mismatch sandwich = %#v", sandwich)
	}
}

func TestReadAccountSandwichCollectsDistinctPlanEvidence(t *testing.T) {
	t.Parallel()

	rateLimits := `{"accountId":"acct-test-a","rateLimits":{"planType":" ProLite ","primary":{"usedPercent":3,"windowDurationMins":5,"resetsAt":1784008800}},"rateLimitsByLimitId":{"codex":{"limitId":"codex","planType":"prolite","primary":{"usedPercent":3,"windowDurationMins":5,"resetsAt":1784008800}},"extra":{"limitId":"extra","planType":"PROLITE","primary":{"usedPercent":4,"windowDurationMins":5,"resetsAt":1784008800}},"empty":{"limitId":"empty","planType":"","primary":{"usedPercent":5,"windowDurationMins":5,"resetsAt":1784008800}}}}`
	account := `{"account":{"type":"chatgpt","email":"a@example.com","planType":"plus"}}`
	responses := strings.Join([]string{
		`{"jsonrpc":"2.0","id":1,"result":` + rateLimits + `}`,
		`{"jsonrpc":"2.0","id":2,"result":` + account + `}`,
		`{"jsonrpc":"2.0","id":3,"result":` + rateLimits + `}`,
	}, "\n") + "\n"
	rpc := newJSONLineRPC(nopWriteCloser{io.Discard}, strings.NewReader(responses))
	sandwich, err := readAccountSandwich(context.Background(), rpc)
	if err != nil {
		t.Fatalf("readAccountSandwich() error = %v", err)
	}
	if !slices.Equal(sandwich.BeforeRateLimitPlanTypes, []string{"prolite"}) ||
		!slices.Equal(sandwich.AfterRateLimitPlanTypes, []string{"prolite"}) {
		t.Fatalf("plan evidence = before %#v after %#v", sandwich.BeforeRateLimitPlanTypes, sandwich.AfterRateLimitPlanTypes)
	}
	if sandwich.Account == nil || sandwich.Account.PlanType == nil || *sandwich.Account.PlanType != "plus" {
		t.Fatalf("account planType = %#v", sandwich.Account)
	}
}

func TestReadAccountSandwichPreservesConflictingBucketPlans(t *testing.T) {
	t.Parallel()

	rateLimits := `{"accountId":"acct-test-a","rateLimits":{"planType":"pro","primary":{"usedPercent":3,"windowDurationMins":5,"resetsAt":1784008800}},"rateLimitsByLimitId":{"codex":{"limitId":"codex","planType":"pro","primary":{"usedPercent":3,"windowDurationMins":5,"resetsAt":1784008800}},"extra":{"limitId":"extra","planType":"prolite","primary":{"usedPercent":9,"windowDurationMins":5,"resetsAt":1784008800}}}}`
	account := `{"account":{"type":"chatgpt","email":"a@example.com","planType":"pro"}}`
	responses := strings.Join([]string{
		`{"jsonrpc":"2.0","id":1,"result":` + rateLimits + `}`,
		`{"jsonrpc":"2.0","id":2,"result":` + account + `}`,
		`{"jsonrpc":"2.0","id":3,"result":` + rateLimits + `}`,
	}, "\n") + "\n"
	rpc := newJSONLineRPC(nopWriteCloser{io.Discard}, strings.NewReader(responses))
	sandwich, err := readAccountSandwich(context.Background(), rpc)
	if err != nil {
		t.Fatalf("readAccountSandwich() error = %v", err)
	}
	want := []string{"pro", "prolite"}
	if !slices.Equal(sandwich.BeforeRateLimitPlanTypes, want) ||
		!slices.Equal(sandwich.AfterRateLimitPlanTypes, want) {
		t.Fatalf("conflicting plan evidence = before %#v after %#v", sandwich.BeforeRateLimitPlanTypes, sandwich.AfterRateLimitPlanTypes)
	}
}

func TestAccountSandwichErrorDoesNotLeakAccountID(t *testing.T) {
	t.Parallel()

	secretID := "acct-secret-sandwich"
	rateLimits := `{"accountId":"` + secretID + `","rateLimits":{"planType":"prolite","primary":{"usedPercent":3,"windowDurationMins":5,"resetsAt":1784008800}}}`
	oversizedPlan := strings.Repeat("p", maxAccountPlanBytes+1)
	account := `{"account":{"type":"chatgpt","email":"a@example.com","planType":"` + oversizedPlan + `"}}`
	responses := strings.Join([]string{
		`{"jsonrpc":"2.0","id":1,"result":` + rateLimits + `}`,
		`{"jsonrpc":"2.0","id":2,"result":` + account + `}`,
	}, "\n") + "\n"
	rpc := newJSONLineRPC(nopWriteCloser{io.Discard}, strings.NewReader(responses))
	sandwich, err := readAccountSandwich(context.Background(), rpc)
	if err == nil {
		t.Fatalf("readAccountSandwich() = %#v, want error", sandwich)
	}
	if len(sandwich.BeforeID) != 0 || len(sandwich.AfterID) != 0 {
		t.Fatal("failed sandwich retained account id bytes")
	}
	if sandwich.BeforeRateLimitPlanTypes != nil || sandwich.AfterRateLimitPlanTypes != nil {
		t.Fatalf("failed sandwich retained plan evidence: %#v", sandwich)
	}
	if strings.Contains(err.Error(), secretID) || strings.Contains(fmt.Sprintf("%#v", err), secretID) {
		t.Fatalf("sandwich error leaked account id: %v", err)
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

func confirmedAccountTestHome(t testing.TB, path string, generation int64) ConfirmedHome {
	t.Helper()
	canonicalPath, err := filepath.EvalSymlinks(path)
	if err != nil {
		t.Fatalf("filepath.EvalSymlinks() error = %v", err)
	}
	directory, err := os.Open(canonicalPath)
	if err != nil {
		t.Fatalf("os.Open() error = %v", err)
	}
	defer directory.Close()
	identity, err := homeidentity.FromDescriptor(int(directory.Fd()))
	if err != nil {
		t.Fatalf("homeidentity.FromDescriptor() error = %v", err)
	}
	return ConfirmedHome{
		Generation: generation,
		Path:       canonicalPath,
		DeviceID:   identity.DeviceID,
		Inode:      identity.Inode,
	}
}

func waitForAccountTestPath(t testing.TB, path string) {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		if _, err := os.Stat(path); err == nil {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for %s", filepath.Base(path))
}
