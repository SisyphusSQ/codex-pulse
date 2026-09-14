package accountbinding

import (
	"bytes"
	"strings"
	"testing"
)

func TestDeriveScopeStableForSameKeyAndAccountID(t *testing.T) {
	t.Parallel()

	var key [32]byte
	copy(key[:], bytes.Repeat([]byte{0x11}, 32))
	first, err := DeriveScope(key, []byte("acct-test-a"))
	if err != nil {
		t.Fatalf("DeriveScope() error = %v", err)
	}
	second, err := DeriveScope(key, []byte("acct-test-a"))
	if err != nil {
		t.Fatalf("DeriveScope(repeat) error = %v", err)
	}
	if first != second || !validDerivedScope(first) {
		t.Fatalf("scope = %q / %q, want identical 64-hex", first, second)
	}
}

func TestDeriveScopeDiffersForDifferentAccountOrKey(t *testing.T) {
	t.Parallel()

	var keyA [32]byte
	var keyB [32]byte
	copy(keyA[:], bytes.Repeat([]byte{0x11}, 32))
	copy(keyB[:], bytes.Repeat([]byte{0x22}, 32))
	scopeA, err := DeriveScope(keyA, []byte("acct-test-a"))
	if err != nil {
		t.Fatalf("DeriveScope(A) error = %v", err)
	}
	scopeB, err := DeriveScope(keyA, []byte("acct-test-b"))
	if err != nil {
		t.Fatalf("DeriveScope(B) error = %v", err)
	}
	otherKey, err := DeriveScope(keyB, []byte("acct-test-a"))
	if err != nil {
		t.Fatalf("DeriveScope(other key) error = %v", err)
	}
	if scopeA == scopeB || scopeA == otherKey || scopeB == otherKey {
		t.Fatalf("scopes collided: A=%q B=%q other=%q", scopeA, scopeB, otherKey)
	}
}

func TestDeriveScopeRejectsInvalidAccountIDWithoutLeakingValue(t *testing.T) {
	t.Parallel()

	var key [32]byte
	copy(key[:], bytes.Repeat([]byte{0x33}, 32))
	secret := "acct-test-a"
	cases := [][]byte{
		nil,
		[]byte(""),
		[]byte(" acct-test-a"),
		[]byte("acct-test-a "),
		[]byte("acct-test-a\n"),
		append([]byte("acct-test-a"), 0x80),
		bytes.Repeat([]byte("x"), 257),
	}
	for _, accountID := range cases {
		scope, err := DeriveScope(key, append([]byte(nil), accountID...))
		if err == nil || scope != "" {
			t.Fatalf("DeriveScope(%q) = %q, %v, want error", accountID, scope, err)
		}
		if strings.Contains(err.Error(), secret) {
			t.Fatalf("error leaked account identity: %v", err)
		}
	}
}

func TestDeriveScopeClearsAccountIDBuffer(t *testing.T) {
	t.Parallel()

	var key [32]byte
	accountID := []byte("acct-test-a")
	if _, err := DeriveScope(key, accountID); err != nil {
		t.Fatalf("DeriveScope() error = %v", err)
	}
	if !bytes.Equal(accountID, make([]byte, len(accountID))) {
		t.Fatalf("account ID buffer was not cleared: %q", accountID)
	}
}

func validDerivedScope(value string) bool {
	if len(value) != 64 {
		return false
	}
	for index := 0; index < len(value); index++ {
		character := value[index]
		if character < '0' || character > '9' {
			if character < 'a' || character > 'f' {
				return false
			}
		}
	}
	return true
}
