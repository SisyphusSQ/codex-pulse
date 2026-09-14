package accountbinding

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"strings"
	"unicode/utf8"
)

const (
	ScopeDomain       = "codex-pulse/account-scope/v1\x00"
	maxAccountIDBytes = 256
)

var ErrAccountIdentityUnavailable = errors.New("account identity unavailable")

func DeriveScope(key [32]byte, accountID []byte) (string, error) {
	defer clearBytes(accountID)
	if len(accountID) == 0 || len(accountID) > maxAccountIDBytes || !utf8.Valid(accountID) {
		return "", ErrAccountIdentityUnavailable
	}
	if strings.TrimSpace(string(accountID)) != string(accountID) {
		return "", ErrAccountIdentityUnavailable
	}
	mac := hmac.New(sha256.New, key[:])
	_, _ = mac.Write([]byte(ScopeDomain))
	_, _ = mac.Write(accountID)
	return hex.EncodeToString(mac.Sum(nil)), nil
}

func clearBytes(value []byte) {
	for index := range value {
		value[index] = 0
	}
}
