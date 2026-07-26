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

// ErrConfirmedHomeChanged 阻止账户字段跨越已确认的物理 Home 边界。
var ErrConfirmedHomeChanged = errors.New("confirmed Codex Home changed")

// ConfirmedHome 保存 Preferences 已确认的逻辑代际与物理身份。
type ConfirmedHome struct {
	Generation int64
	Path       string
	DeviceID   string
	Inode      int64
}

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
	confirmedHome ConfirmedHome,
	options ProcessOptions,
) (*AccountSnapshot, error) {
	binding, err := openConfirmedHomeBinding(ctx, confirmedHome)
	if err != nil {
		return nil, err
	}
	defer func() { _ = binding.close() }()
	beforeStart := options.BeforeStart
	options.BeforeStart = func(startContext context.Context) error {
		if beforeStart != nil {
			if err := beforeStart(startContext); err != nil {
				return err
			}
		}
		return binding.validate(startContext)
	}
	options.homeBinding = binding
	account, err := withInitializedLocalRPC(
		ctx,
		confirmedHome.Path,
		options,
		func(ctx context.Context, rpc *jsonLineRPC, _ string) (*AccountSnapshot, error) {
			return readAccount(ctx, rpc)
		},
	)
	if err != nil {
		return nil, err
	}
	if err := binding.validate(ctx); err != nil {
		return nil, err
	}
	return account, nil
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
