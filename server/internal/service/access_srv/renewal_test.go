package access_srv

import (
	"errors"
	"sync"
	"testing"
	"time"

	"gorm.io/gorm"

	access_do "github.com/SisyphusSQ/codex-pulse/server/internal/models/do/mysql/access_do"
	access_dto "github.com/SisyphusSQ/codex-pulse/server/internal/models/dto/access_dto"
	"github.com/SisyphusSQ/codex-pulse/server/utils"
)

func TestBrowserSlidingWindowAndExpiryBoundary(t *testing.T) {
	s, _ := accessFixture(t)
	now := time.Now().Truncate(time.Millisecond)
	s.now = func() time.Time { return now }
	browser := pairedAdmin(t, s)
	now = now.Add(13 * 24 * time.Hour)
	renewed, err := s.RenewBrowser(t.Context(), browser.Principal)
	if err != nil || renewed.ExpiresAtMS == nil || *renewed.ExpiresAtMS != now.Add(browserLifetime).UnixMilli() {
		t.Fatalf("renewal=%+v err=%v", renewed, err)
	}
	now = now.Add(2 * 24 * time.Hour)
	if _, err := s.Authenticate(t.Context(), browser.Credential, browser.Principal.Origin, browser.CSRF, true, true); err != nil {
		t.Fatalf("original 14-day deadline still applied: %v", err)
	}
	now = time.UnixMilli(*renewed.ExpiresAtMS)
	if _, err := s.RenewBrowser(t.Context(), browser.Principal); !errors.Is(err, utils.ErrUnauthorized) {
		t.Fatalf("expired browser revived: %v", err)
	}
	if _, err := s.Authenticate(t.Context(), browser.Credential, browser.Principal.Origin, "", true, false); !errors.Is(err, utils.ErrUnauthorized) {
		t.Fatalf("idle browser accepted: %v", err)
	}
}

func TestBrowserRenewalRechecksStoredAuthorization(t *testing.T) {
	for _, scenario := range []string{"revoked", "origin", "collector", "missing"} {
		t.Run(scenario, func(t *testing.T) {
			s, engine := accessFixture(t)
			browser := pairedAdmin(t, s)
			principal := browser.Principal
			switch scenario {
			case "revoked":
				if err := s.Revoke(t.Context(), principal, principal.ID); err != nil {
					t.Fatal(err)
				}
			case "origin":
				principal.Origin = "https://other.example"
			case "collector":
				if err := engine.DB(t.Context()).Model(&access_do.Client{}).Where("id = ?", principal.ID).Update("purpose", access_dto.PurposeCollector).Error; err != nil {
					t.Fatal(err)
				}
			case "missing":
				principal.ID = "missing"
			}
			if _, err := s.RenewBrowser(t.Context(), principal); !errors.Is(err, utils.ErrUnauthorized) {
				t.Fatalf("invalid authorization renewed: %v", err)
			}
		})
	}
}

func TestBrowserRenewalWriteFailureKeepsOriginalWindow(t *testing.T) {
	s, engine := accessFixture(t)
	now := time.Now().Truncate(time.Millisecond)
	s.now = func() time.Time { return now }
	browser := pairedAdmin(t, s)
	now = now.Add(24 * time.Hour)
	failure := errors.New("synthetic renewal write failure")
	if err := engine.DB(t.Context()).Callback().Update().Before("gorm:update").Register("test:renewal_failure", func(db *gorm.DB) {
		db.AddError(failure)
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := s.RenewBrowser(t.Context(), browser.Principal); !errors.Is(err, failure) {
		t.Fatalf("write failure hidden: %v", err)
	}
	principal, err := s.Authenticate(t.Context(), browser.Credential, browser.Principal.Origin, "", true, false)
	if err != nil || *principal.ExpiresAtMS != *browser.ExpiresAtMS {
		t.Fatal("failed renewal changed original window", err)
	}
}

func TestConcurrentBrowserRenewalsPreserveWindowAndRevocation(t *testing.T) {
	s, _ := accessFixture(t)
	now := time.Now().Truncate(time.Millisecond)
	s.now = func() time.Time { return now }
	browser := pairedAdmin(t, s)
	now = now.Add(24 * time.Hour)
	var group sync.WaitGroup
	for range 4 {
		group.Go(func() {
			renewed, err := s.RenewBrowser(t.Context(), browser.Principal)
			if err != nil || renewed.ExpiresAtMS == nil || *renewed.ExpiresAtMS != now.Add(browserLifetime).UnixMilli() {
				t.Errorf("concurrent renewal=%+v err=%v", renewed, err)
			}
		})
	}
	group.Wait()
	// 较早的请求不能覆盖已延长的期限。
	now = now.Add(-time.Hour)
	renewed, err := s.RenewBrowser(t.Context(), browser.Principal)
	if err != nil || *renewed.ExpiresAtMS != now.Add(time.Hour+browserLifetime).UnixMilli() {
		t.Fatalf("window shortened: %+v %v", renewed, err)
	}
	group.Go(func() {
		if err := s.Revoke(t.Context(), browser.Principal, browser.Principal.ID); err != nil {
			t.Error(err)
		}
	})
	group.Go(func() {
		_, err := s.RenewBrowser(t.Context(), browser.Principal)
		if err != nil && !errors.Is(err, utils.ErrUnauthorized) {
			t.Error(err)
		}
	})
	group.Wait()
	if _, err := s.Authenticate(t.Context(), browser.Credential, browser.Principal.Origin, "", true, false); !errors.Is(err, utils.ErrUnauthorized) {
		t.Fatalf("revocation lost: %v", err)
	}
}
