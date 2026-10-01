package access_srv

import (
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base32"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"strings"
	"time"
	"unicode/utf8"
	"uuid"

	"gorm.io/gorm"

	access_do "github.com/SisyphusSQ/codex-pulse/server/internal/models/do/mysql/access_do"
	access_dto "github.com/SisyphusSQ/codex-pulse/server/internal/models/dto/access_dto"
	access_vo "github.com/SisyphusSQ/codex-pulse/server/internal/models/vo/access_vo"
	access_repo "github.com/SisyphusSQ/codex-pulse/server/internal/repository/mysql/access_repo"
	"github.com/SisyphusSQ/codex-pulse/server/utils"
)

const pairingLifetime = 10 * time.Minute
const browserLifetime = 14 * 24 * time.Hour

// Access 在所有网络入口统一签发、验证与撤销客户端授权。
type Access struct {
	repository *access_repo.Access
	now        func() time.Time
}

func NewAccess(repository *access_repo.Access) *Access {
	return &Access{repository: repository, now: time.Now}
}

func digest(secret string) string {
	value := sha256.Sum256([]byte(secret))
	return hex.EncodeToString(value[:])
}

// CSRFToken 从持有的随机浏览器凭证和入口派生，重载页面不撤销其他标签页。
func CSRFToken(credential, origin string) string {
	mac := hmac.New(sha256.New, []byte(credential))
	_, _ = mac.Write([]byte("pulse-csrf-v1\x00" + origin))
	return hex.EncodeToString(mac.Sum(nil))
}

func randomSecret() (string, error) {
	var bytes [32]byte
	if _, err := rand.Read(bytes[:]); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(bytes[:]), nil
}

func (s *Access) issue(ctx context.Context, purpose, name string) (access_vo.PairingView, error) {
	if (purpose != access_dto.PurposeAdmin && purpose != access_dto.PurposeCollector) || utf8.RuneCountInString(name) < 1 || utf8.RuneCountInString(name) > 128 {
		return access_vo.PairingView{}, utils.ErrBadParamInput
	}
	var bytes [10]byte
	if _, err := rand.Read(bytes[:]); err != nil {
		return access_vo.PairingView{}, err
	}
	code := base32.StdEncoding.WithPadding(base32.NoPadding).EncodeToString(bytes[:])
	now := s.now().UnixMilli()
	pairing := access_do.Pairing{CodeHash: digest(code), Purpose: purpose, Name: name, CreatedAtMS: now, ExpiresAtMS: now + pairingLifetime.Milliseconds()}
	if err := s.repository.AddPairing(ctx, pairing); err != nil {
		return access_vo.PairingView{}, err
	}
	return access_vo.PairingView{Code: code[:4] + "-" + code[4:8] + "-" + code[8:12] + "-" + code[12:], Purpose: purpose, ExpiresAtMS: pairing.ExpiresAtMS}, nil
}

// Bootstrap 只能由受信任的本机初始化 CLI 调用，不暴露网络路由。
func (s *Access) Bootstrap(ctx context.Context) (access_vo.PairingView, error) {
	return s.issue(ctx, access_dto.PurposeAdmin, "管理浏览器")
}

func RequireAdmin(principal access_dto.Principal) error {
	if principal.ID == "" || principal.Purpose != access_dto.PurposeAdmin {
		return utils.ErrForbidden
	}
	return nil
}

func (s *Access) Issue(ctx context.Context, principal access_dto.Principal, purpose, name string) (access_vo.PairingView, error) {
	if err := RequireAdmin(principal); err != nil {
		return access_vo.PairingView{}, err
	}
	return s.issue(ctx, purpose, strings.TrimSpace(name))
}

func (s *Access) Pair(ctx context.Context, code, mode, origin string) (access_dto.PairedClient, error) {
	code = strings.ReplaceAll(strings.ToUpper(strings.TrimSpace(code)), "-", "")
	if len(code) != 16 || (mode != "browser" && mode != "collector") {
		return access_dto.PairedClient{}, utils.ErrBadParamInput
	}
	if mode == "browser" && origin == "" {
		return access_dto.PairedClient{}, utils.ErrForbidden
	}
	credential, err := randomSecret()
	if err != nil {
		return access_dto.PairedClient{}, err
	}
	now := s.now().UnixMilli()
	var paired access_dto.PairedClient
	err = s.repository.Transaction(ctx, func(ctx context.Context) error {
		pairing, err := s.repository.Pairing(ctx, digest(code))
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return utils.ErrUnauthorized
		}
		if err != nil {
			return err
		}
		purpose := access_dto.PurposeCollector
		if mode == "browser" {
			purpose = access_dto.PurposeAdmin
		}
		if pairing.Purpose != purpose || pairing.ConsumedAtMS != nil || pairing.ExpiresAtMS <= now {
			return utils.ErrUnauthorized
		}
		consumed, err := s.repository.Consume(ctx, pairing.CodeHash, now)
		if err != nil {
			return err
		}
		if !consumed {
			return utils.ErrUnauthorized
		}
		client := access_do.Client{ID: uuid.New().String(), Purpose: purpose, Name: pairing.Name, SecretHash: digest(credential), CreatedAtMS: now}
		if purpose == access_dto.PurposeAdmin {
			client.Origin = origin
			client.ExpiresAtMS = new(now + browserLifetime.Milliseconds())
			paired.CSRF = CSRFToken(credential, origin)
			client.CSRFHash = digest(paired.CSRF)
		}
		if err := s.repository.AddClient(ctx, client); err != nil {
			return err
		}
		paired.Principal = access_dto.Principal{ID: client.ID, Purpose: client.Purpose, Name: client.Name, Origin: client.Origin, ExpiresAtMS: client.ExpiresAtMS}
		paired.Credential = credential
		paired.ExpiresAtMS = client.ExpiresAtMS
		return nil
	})
	if err != nil {
		return access_dto.PairedClient{}, err
	}
	return paired, nil
}

func (s *Access) Authenticate(ctx context.Context, credential, origin, csrf string, browser, mutation bool) (access_dto.Principal, error) {
	if len(credential) != 43 {
		return access_dto.Principal{}, utils.ErrUnauthorized
	}
	client, err := s.repository.ClientBySecret(ctx, digest(credential))
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return access_dto.Principal{}, utils.ErrUnauthorized
	}
	if err != nil {
		return access_dto.Principal{}, err
	}
	if client.RevokedAtMS != nil || (client.ExpiresAtMS != nil && *client.ExpiresAtMS <= s.now().UnixMilli()) {
		return access_dto.Principal{}, utils.ErrUnauthorized
	}
	if browser {
		if client.Purpose != access_dto.PurposeAdmin || origin == "" || client.Origin != origin {
			return access_dto.Principal{}, utils.ErrUnauthorized
		}
		if mutation && subtle.ConstantTimeCompare([]byte(client.CSRFHash), []byte(digest(csrf))) != 1 {
			return access_dto.Principal{}, utils.ErrForbidden
		}
	} else if client.Purpose != access_dto.PurposeCollector {
		return access_dto.Principal{}, utils.ErrForbidden
	}
	return access_dto.Principal{ID: client.ID, Purpose: client.Purpose, Name: client.Name, Origin: client.Origin, ExpiresAtMS: client.ExpiresAtMS}, nil
}

func (s *Access) Clients(ctx context.Context, principal access_dto.Principal) (access_vo.ClientListView, error) {
	if err := RequireAdmin(principal); err != nil {
		return access_vo.ClientListView{}, err
	}
	clients, err := s.repository.Clients(ctx)
	if err != nil {
		return access_vo.ClientListView{}, err
	}
	view := access_vo.ClientListView{Clients: make([]access_vo.ClientView, 0, len(clients))}
	for _, client := range clients {
		view.Clients = append(view.Clients, access_vo.ClientView{ID: client.ID, Purpose: client.Purpose, Name: client.Name, CreatedAtMS: client.CreatedAtMS, ExpiresAtMS: client.ExpiresAtMS, RevokedAtMS: client.RevokedAtMS, LastReceivedAtMS: client.LastReceivedAtMS})
	}
	return view, nil
}

func (s *Access) Revoke(ctx context.Context, principal access_dto.Principal, id string) error {
	if err := RequireAdmin(principal); err != nil {
		return err
	}
	if len(id) != 36 {
		return utils.ErrBadParamInput
	}
	found, err := s.repository.Revoke(ctx, id, s.now().UnixMilli())
	if err != nil {
		return err
	}
	if !found {
		return utils.ErrNotFound
	}
	return nil
}
