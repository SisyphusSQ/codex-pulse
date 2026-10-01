package access_srv

import (
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	gormv2 "github.com/SisyphusSQ/codex-pulse/server/internal/lib/gorm"
	access_do "github.com/SisyphusSQ/codex-pulse/server/internal/models/do/mysql/access_do"
	access_dto "github.com/SisyphusSQ/codex-pulse/server/internal/models/dto/access_dto"
	access_repo "github.com/SisyphusSQ/codex-pulse/server/internal/repository/mysql/access_repo"
	centerfixture "github.com/SisyphusSQ/codex-pulse/server/internal/testsupport/center"
	"github.com/SisyphusSQ/codex-pulse/server/utils"
)

func accessFixture(t *testing.T) (*Access, *gormv2.Engine) {
	t.Helper()
	engine := centerfixture.Engine(t)
	return NewAccess(access_repo.NewAccess(engine)), engine
}
func pairedAdmin(t *testing.T, s *Access) access_dto.PairedClient { return centerfixture.Admin(t, s) }

func TestDeviceCodeUnifiedAuthorizationAndRevocation(t *testing.T) {
	s, engine := accessFixture(t)
	code, err := s.Bootstrap(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.Pair(t.Context(), code.Code, "collector", ""); !errors.Is(err, utils.ErrUnauthorized) {
		t.Fatalf("purpose promotion = %v", err)
	}
	admin, err := s.Pair(t.Context(), code.Code, "browser", "https://pulse.example.com")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.Pair(t.Context(), code.Code, "browser", admin.Principal.Origin); !errors.Is(err, utils.ErrUnauthorized) {
		t.Fatalf("code reused = %v", err)
	}
	if _, err := s.Authenticate(t.Context(), admin.Credential, "", "", false, false); !errors.Is(err, utils.ErrForbidden) {
		t.Fatalf("browser credential promoted to header = %v", err)
	}
	if _, err := s.Authenticate(t.Context(), admin.Credential, "http://pulse.example.com", admin.CSRF, true, true); !errors.Is(err, utils.ErrUnauthorized) {
		t.Fatalf("origin downgrade = %v", err)
	}
	if _, err := s.Authenticate(t.Context(), admin.Credential, admin.Principal.Origin, "", true, true); !errors.Is(err, utils.ErrForbidden) {
		t.Fatalf("CSRF missing = %v", err)
	}
	if _, err := s.Authenticate(t.Context(), admin.Credential, admin.Principal.Origin, admin.CSRF, true, true); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Authenticate(t.Context(), admin.Credential, admin.Principal.Origin, "", true, false); err != nil {
		t.Fatal(err)
	}
	collectorCode, err := s.Issue(t.Context(), admin.Principal, access_dto.PurposeCollector, "机器一")
	if err != nil {
		t.Fatal(err)
	}
	collector, err := s.Pair(t.Context(), collectorCode.Code, "collector", "")
	if err != nil {
		t.Fatal(err)
	}
	principal, err := s.Authenticate(t.Context(), collector.Credential, "", "", false, true)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.Issue(t.Context(), principal, access_dto.PurposeAdmin, "越权"); !errors.Is(err, utils.ErrForbidden) {
		t.Fatalf("collector issue admin = %v", err)
	}
	if _, err := s.Clients(t.Context(), principal); !errors.Is(err, utils.ErrForbidden) {
		t.Fatalf("collector list = %v", err)
	}
	if err := s.Revoke(t.Context(), admin.Principal, collector.Principal.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Authenticate(t.Context(), collector.Credential, "", "", false, false); !errors.Is(err, utils.ErrUnauthorized) {
		t.Fatalf("revoked credential accepted = %v", err)
	}
	var clients []access_do.Client
	if err := engine.DB(t.Context()).Find(&clients).Error; err != nil {
		t.Fatal(err)
	}
	if len(clients) != 2 {
		t.Fatalf("revocation deleted history: %d", len(clients))
	}
	for _, client := range clients {
		if client.SecretHash == admin.Credential || client.SecretHash == collector.Credential || len(client.SecretHash) != 64 {
			t.Fatal("reusable credential stored")
		}
	}
}

func TestPairingConcurrentConsumptionIsSingleUse(t *testing.T) {
	s, engine := accessFixture(t)
	code, err := s.Bootstrap(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	var success atomic.Int32
	var group sync.WaitGroup
	for range 8 {
		group.Go(func() {
			if _, err := s.Pair(t.Context(), code.Code, "browser", "https://pulse.example.com"); err == nil {
				success.Add(1)
			} else if !errors.Is(err, utils.ErrUnauthorized) {
				t.Errorf("concurrent pair: %v", err)
			}
		})
	}
	group.Wait()
	if success.Load() != 1 {
		t.Fatalf("successful consumes=%d", success.Load())
	}
	var count int64
	if err := engine.DB(t.Context()).Model(&access_do.Client{}).Count(&count).Error; err != nil || count != 1 {
		t.Fatalf("client count=%d err=%v", count, err)
	}
}

func TestPairingAndBrowserCredentialsExpire(t *testing.T) {
	s, _ := accessFixture(t)
	now := time.Now()
	s.now = func() time.Time { return now }
	code, err := s.Bootstrap(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	now = now.Add(pairingLifetime)
	if _, err := s.Pair(t.Context(), code.Code, "browser", "https://pulse.example.com"); !errors.Is(err, utils.ErrUnauthorized) {
		t.Fatalf("expired code=%v", err)
	}
	admin := pairedAdmin(t, s)
	now = now.Add(browserLifetime)
	if _, err := s.Authenticate(t.Context(), admin.Credential, admin.Principal.Origin, "", true, false); !errors.Is(err, utils.ErrUnauthorized) {
		t.Fatalf("expired browser=%v", err)
	}
}
