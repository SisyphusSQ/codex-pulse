package app

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"golang.org/x/sys/unix"

	quotaonline "github.com/SisyphusSQ/codex-pulse/internal/codex/quota"
	"github.com/SisyphusSQ/codex-pulse/internal/preferences"
)

const maximumAuthFileBytes = int64(1 << 20)

const maximumAuthJSONDepth = 64

type authJSONEnvelope struct {
	Tokens json.RawMessage `json:"tokens"`
}

type authJSONTokens struct {
	AccessToken json.RawMessage `json:"access_token"`
}

type authFileReadHooks struct {
	afterRead func()
}

// authFileCredentialProvider 从当前已确认 Codex Home 的 auth.json 租用 access token。
// provider 不缓存凭证；每次调用都重新读取 Preferences 与文件身份。
type authFileCredentialProvider struct {
	preferences  confirmedPreferencesLoader
	readAuthFile func(context.Context, preferences.ConfirmedSource) ([]byte, error)
}

func newAuthFileCredentialProvider(
	loader confirmedPreferencesLoader,
) (*authFileCredentialProvider, error) {
	if loader == nil {
		return nil, quotaonline.ErrCredentialUnavailable
	}
	return &authFileCredentialProvider{
		preferences:  loader,
		readAuthFile: readConfirmedAuthFile,
	}, nil
}

func (provider *authFileCredentialProvider) WithAccessToken(
	ctx context.Context,
	use func([]byte) error,
) error {
	if provider == nil || provider.preferences == nil || provider.readAuthFile == nil || use == nil {
		return quotaonline.ErrCredentialUnavailable
	}
	if ctx == nil {
		ctx = context.Background()
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	snapshot, err := provider.preferences.LoadPreferences(ctx)
	if err != nil {
		return credentialProviderError(ctx, err)
	}
	content, err := provider.readAuthFile(ctx, snapshot.CodexHome.Source)
	if err != nil {
		return credentialProviderError(ctx, err)
	}
	defer clearCredentialBytes(content)
	current, err := provider.preferences.LoadPreferences(ctx)
	if err != nil {
		return credentialProviderError(ctx, err)
	}
	if current.CodexHome != snapshot.CodexHome {
		return quotaonline.ErrCredentialUnavailable
	}
	token, valid := decodeAuthAccessToken(content)
	if !valid {
		return quotaonline.ErrCredentialUnavailable
	}
	defer clearCredentialBytes(token)
	return use(token)
}

func readConfirmedAuthFile(
	ctx context.Context,
	source preferences.ConfirmedSource,
) ([]byte, error) {
	return readConfirmedAuthFileWithHooks(ctx, source, authFileReadHooks{})
}

func readConfirmedAuthFileWithHooks(
	ctx context.Context,
	source preferences.ConfirmedSource,
	hooks authFileReadHooks,
) ([]byte, error) {
	if ctx == nil || !filepath.IsAbs(source.Path) || filepath.Clean(source.Path) != source.Path ||
		source.DeviceID == "" || source.Inode <= 0 {
		return nil, quotaonline.ErrCredentialUnavailable
	}
	rootDescriptor, err := openAuthHomeNoFollow(source.Path)
	if err != nil {
		return nil, err
	}
	defer func() { _ = unix.Close(rootDescriptor) }()
	var rootStat unix.Stat_t
	if err := unix.Fstat(rootDescriptor, &rootStat); err != nil ||
		rootStat.Mode&unix.S_IFMT != unix.S_IFDIR ||
		strconv.FormatUint(uint64(uint32(rootStat.Dev)), 10) != source.DeviceID ||
		int64(rootStat.Ino) != source.Inode {
		return nil, quotaonline.ErrCredentialUnavailable
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	descriptor, err := unix.Openat(
		rootDescriptor, "auth.json",
		unix.O_RDONLY|unix.O_CLOEXEC|unix.O_NOFOLLOW|unix.O_NONBLOCK,
		0,
	)
	if err != nil {
		return nil, err
	}
	file := os.NewFile(uintptr(descriptor), "auth.json")
	if file == nil {
		_ = unix.Close(descriptor)
		return nil, quotaonline.ErrCredentialUnavailable
	}
	defer func() { _ = file.Close() }()

	var openedBefore unix.Stat_t
	var entryBefore unix.Stat_t
	if err := unix.Fstat(descriptor, &openedBefore); err != nil ||
		openedBefore.Mode&unix.S_IFMT != unix.S_IFREG || openedBefore.Size < 1 ||
		openedBefore.Size > maximumAuthFileBytes {
		return nil, quotaonline.ErrCredentialUnavailable
	}
	if err := unix.Fstatat(rootDescriptor, "auth.json", &entryBefore, unix.AT_SYMLINK_NOFOLLOW); err != nil ||
		!sameAuthFileStat(openedBefore, entryBefore) {
		return nil, quotaonline.ErrCredentialUnavailable
	}
	content, err := io.ReadAll(io.LimitReader(file, maximumAuthFileBytes+1))
	if err != nil || int64(len(content)) > maximumAuthFileBytes {
		clearCredentialBytes(content)
		return nil, quotaonline.ErrCredentialUnavailable
	}
	if err := ctx.Err(); err != nil {
		clearCredentialBytes(content)
		return nil, err
	}
	if hooks.afterRead != nil {
		hooks.afterRead()
	}
	var openedAfter unix.Stat_t
	var entryAfter unix.Stat_t
	if err := unix.Fstat(descriptor, &openedAfter); err != nil ||
		unix.Fstatat(rootDescriptor, "auth.json", &entryAfter, unix.AT_SYMLINK_NOFOLLOW) != nil ||
		!sameAuthFileStat(openedBefore, openedAfter) || !sameAuthFileStat(openedAfter, entryAfter) {
		clearCredentialBytes(content)
		return nil, quotaonline.ErrCredentialUnavailable
	}
	return content, nil
}

func openAuthHomeNoFollow(path string) (int, error) {
	path = filepath.Clean(path)
	if !filepath.IsAbs(path) || path == string(filepath.Separator) {
		return -1, quotaonline.ErrCredentialUnavailable
	}
	current, err := unix.Open(
		string(filepath.Separator),
		unix.O_RDONLY|unix.O_CLOEXEC|unix.O_NOFOLLOW|unix.O_DIRECTORY,
		0,
	)
	if err != nil {
		return -1, err
	}
	components := strings.Split(strings.TrimPrefix(path, string(filepath.Separator)), string(filepath.Separator))
	for _, component := range components {
		if component == "" || component == "." || component == ".." {
			_ = unix.Close(current)
			return -1, quotaonline.ErrCredentialUnavailable
		}
		next, openErr := unix.Openat(
			current, component,
			unix.O_RDONLY|unix.O_CLOEXEC|unix.O_NOFOLLOW|unix.O_DIRECTORY,
			0,
		)
		_ = unix.Close(current)
		if openErr != nil {
			return -1, openErr
		}
		current = next
	}
	return current, nil
}

func sameAuthFileStat(left, right unix.Stat_t) bool {
	return left.Dev == right.Dev && left.Ino == right.Ino && left.Mode == right.Mode &&
		left.Size == right.Size && left.Mtim == right.Mtim && left.Ctim == right.Ctim
}

func decodeAuthAccessToken(content []byte) ([]byte, bool) {
	if validateUniqueAuthJSONKeys(content) != nil {
		return nil, false
	}
	var envelope authJSONEnvelope
	if err := json.Unmarshal(content, &envelope); err != nil {
		return nil, false
	}
	defer clearCredentialBytes(envelope.Tokens)
	if len(envelope.Tokens) == 0 {
		return nil, false
	}
	var tokens authJSONTokens
	if err := json.Unmarshal(envelope.Tokens, &tokens); err != nil {
		return nil, false
	}
	defer clearCredentialBytes(tokens.AccessToken)
	if len(tokens.AccessToken) == 0 {
		return nil, false
	}
	accessToken := bytes.TrimSpace(tokens.AccessToken)
	if len(accessToken) < 3 || accessToken[0] != '"' || accessToken[len(accessToken)-1] != '"' {
		return nil, false
	}
	encoded := accessToken[1 : len(accessToken)-1]
	if len(encoded) == 0 || bytes.IndexByte(encoded, '\\') >= 0 ||
		!bytes.Equal(encoded, bytes.TrimSpace(encoded)) {
		return nil, false
	}
	token := append([]byte(nil), encoded...)
	return token, true
}

func validateUniqueAuthJSONKeys(content []byte) error {
	content = bytes.TrimSpace(content)
	if !json.Valid(content) {
		return quotaonline.ErrCredentialUnavailable
	}
	return validateAuthJSONValue(content, 0)
}

func validateAuthJSONValue(content []byte, depth int) error {
	content = bytes.TrimSpace(content)
	if len(content) == 0 || depth > maximumAuthJSONDepth {
		return quotaonline.ErrCredentialUnavailable
	}
	if content[0] != '{' && content[0] != '[' {
		return nil
	}
	decoder := json.NewDecoder(bytes.NewReader(content))
	opening, err := decoder.Token()
	if err != nil {
		return err
	}
	switch opening {
	case json.Delim('{'):
		seen := make(map[string]struct{})
		for decoder.More() {
			keyToken, err := decoder.Token()
			if err != nil {
				return err
			}
			key, ok := keyToken.(string)
			if !ok {
				return quotaonline.ErrCredentialUnavailable
			}
			if _, duplicate := seen[key]; duplicate {
				return quotaonline.ErrCredentialUnavailable
			}
			seen[key] = struct{}{}
			var raw json.RawMessage
			if err := decoder.Decode(&raw); err != nil {
				clearCredentialBytes(raw)
				return err
			}
			err = validateAuthJSONValue(raw, depth+1)
			clearCredentialBytes(raw)
			if err != nil {
				return err
			}
		}
		closing, err := decoder.Token()
		if err != nil || closing != json.Delim('}') {
			return quotaonline.ErrCredentialUnavailable
		}
		return validateAuthJSONEnd(decoder)
	case json.Delim('['):
		for decoder.More() {
			var raw json.RawMessage
			if err := decoder.Decode(&raw); err != nil {
				clearCredentialBytes(raw)
				return err
			}
			err = validateAuthJSONValue(raw, depth+1)
			clearCredentialBytes(raw)
			if err != nil {
				return err
			}
		}
		closing, err := decoder.Token()
		if err != nil || closing != json.Delim(']') {
			return quotaonline.ErrCredentialUnavailable
		}
		return validateAuthJSONEnd(decoder)
	default:
		return quotaonline.ErrCredentialUnavailable
	}
}

func validateAuthJSONEnd(decoder *json.Decoder) error {
	if _, err := decoder.Token(); !errors.Is(err, io.EOF) {
		if err == nil {
			return quotaonline.ErrCredentialUnavailable
		}
		return err
	}
	return nil
}

func clearCredentialBytes(value []byte) {
	for index := range value {
		value[index] = 0
	}
}

func credentialProviderError(ctx context.Context, err error) error {
	if ctx != nil && ctx.Err() != nil {
		return ctx.Err()
	}
	if errors.Is(err, context.Canceled) {
		return context.Canceled
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return context.DeadlineExceeded
	}
	return quotaonline.ErrCredentialUnavailable
}

var _ quotaonline.CredentialProvider = (*authFileCredentialProvider)(nil)
