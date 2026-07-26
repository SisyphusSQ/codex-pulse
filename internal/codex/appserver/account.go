package appserver

import (
	"context"
	"errors"
	"strings"
	"unicode/utf8"
)

const (
	maxAccountTypeBytes  = 64
	maxAccountEmailBytes = 320
	maxAccountPlanBytes  = 128
)

type AccountSnapshot struct {
	Type     string  `json:"type"`
	Email    *string `json:"email,omitempty"`
	PlanType *string `json:"planType,omitempty"`
}

type accountReadParams struct {
	RefreshToken bool `json:"refreshToken"`
}

type accountReadResult struct {
	Account *AccountSnapshot `json:"account"`
}

func ReadLocalAccount(
	ctx context.Context,
	confirmedHome string,
	options ProcessOptions,
) (*AccountSnapshot, error) {
	return withInitializedLocalRPC(
		ctx,
		confirmedHome,
		options,
		func(ctx context.Context, rpc *jsonLineRPC, _ string) (*AccountSnapshot, error) {
			return readAccount(ctx, rpc)
		},
	)
}

func readAccount(ctx context.Context, rpc RPC) (*AccountSnapshot, error) {
	if ctx == nil || rpc == nil {
		return nil, errors.New("invalid account reader")
	}
	var response accountReadResult
	if err := rpc.Call(
		ctx,
		"account/read",
		accountReadParams{RefreshToken: false},
		&response,
	); err != nil {
		return nil, errors.New("read App Server account")
	}
	if response.Account == nil {
		return nil, nil
	}
	normalized, ok := normalizeAccountSnapshot(*response.Account)
	if !ok {
		return nil, errors.New("invalid App Server account")
	}
	return &normalized, nil
}

func normalizeAccountSnapshot(snapshot AccountSnapshot) (AccountSnapshot, bool) {
	accountType, ok := normalizedAccountField(snapshot.Type, maxAccountTypeBytes, false)
	if !ok {
		return AccountSnapshot{}, false
	}
	email, ok := normalizedOptionalAccountField(snapshot.Email, maxAccountEmailBytes)
	if !ok {
		return AccountSnapshot{}, false
	}
	planType, ok := normalizedOptionalAccountField(snapshot.PlanType, maxAccountPlanBytes)
	if !ok {
		return AccountSnapshot{}, false
	}
	return AccountSnapshot{Type: accountType, Email: email, PlanType: planType}, true
}

func normalizedOptionalAccountField(value *string, maximumBytes int) (*string, bool) {
	if value == nil {
		return nil, true
	}
	normalized, ok := normalizedAccountField(*value, maximumBytes, true)
	if !ok {
		return nil, false
	}
	if normalized == "" {
		return nil, true
	}
	return &normalized, true
}

func normalizedAccountField(value string, maximumBytes int, allowEmpty bool) (string, bool) {
	normalized := strings.TrimSpace(value)
	if (!allowEmpty && normalized == "") || len(normalized) > maximumBytes ||
		!utf8.ValidString(normalized) {
		return "", false
	}
	return normalized, true
}
