package subscription_srv

import (
	"errors"
	"testing"
	"time"

	reportingv1 "github.com/SisyphusSQ/codex-pulse/api/codexpulse/reporting/v1"
	reporting_do "github.com/SisyphusSQ/codex-pulse/server/internal/models/do/mysql/reporting_do"
	access_dto "github.com/SisyphusSQ/codex-pulse/server/internal/models/dto/access_dto"
	subscription_vo "github.com/SisyphusSQ/codex-pulse/server/internal/models/vo/subscription_vo"
	subscription_repo "github.com/SisyphusSQ/codex-pulse/server/internal/repository/mysql/subscription_repo"
	centerfixture "github.com/SisyphusSQ/codex-pulse/server/internal/testsupport/center"
	"github.com/SisyphusSQ/codex-pulse/server/utils"
)

func TestSettingsPersistConflictAndAccountIsolation(t *testing.T) {
	engine := centerfixture.Engine(t)
	repo := subscription_repo.NewSubscription(engine)
	s := NewSubscription(repo)
	admin := access_dto.Principal{ID: "synthetic-admin", Purpose: "admin"}
	a := reporting_do.Account{ID: reportingv1.Key("codex", "one"), Provider: "codex", AccountID: "one", Email: new("same@example.invalid"), Plan: new("plus")}
	b := a
	b.ID = reportingv1.Key("codex", "two")
	b.AccountID = "two"
	if err := engine.DB(t.Context()).Create(&[]reporting_do.Account{a, b}).Error; err != nil {
		t.Fatal(err)
	}
	s.now = func() time.Time { return time.Date(2026, 2, 27, 16, 0, 0, 0, time.UTC) }
	u := subscription_vo.Update{ExpectedRevision: new(int64(0)), Alias: new(" 主账号 "), ManualPlan: new("pro"), DateKind: "monthly_renewal", RenewalDay: new(31), TimeZone: "Asia/Shanghai"}
	v, err := s.Update(t.Context(), admin, a.ID, u)
	if err != nil {
		t.Fatal(err)
	}
	if v.Revision != 1 || v.NextDate == nil || *v.NextDate != "2026-02-28" || v.DayDelta == nil || *v.DayDelta != 0 || v.DateState != "today" || *v.Alias != "主账号" || *v.ResolvedPlan != "pro" {
		t.Fatalf("monthly or manual values: %+v", v)
	}
	if _, err := s.Update(t.Context(), admin, a.ID, u); !errors.Is(err, utils.ErrConflict) {
		t.Fatalf("initial conflict: %v", err)
	}
	other, err := s.Get(t.Context(), admin, b.ID)
	if err != nil || other.Revision != 0 || other.NextDate != nil {
		t.Fatal("same email account polluted", err)
	}
	if err := engine.DB(t.Context()).Model(&a).Update("plan", "business").Error; err != nil {
		t.Fatal(err)
	}
	reopened, err := NewSubscription(subscription_repo.NewSubscription(engine)).Get(t.Context(), admin, a.ID)
	if err != nil || *reopened.ResolvedPlan != "pro" || *reopened.AutomaticPlan != "business" {
		t.Fatal("reported plan overwrote manual", err)
	}
	cleared, err := s.Update(t.Context(), admin, a.ID, subscription_vo.Update{ExpectedRevision: new(int64(1)), TimeZone: "UTC"})
	if err != nil || cleared.Revision != 2 || cleared.NextDate != nil || *cleared.ResolvedPlan != "business" {
		t.Fatal("clear failed", err)
	}
	if _, err := s.Update(t.Context(), admin, a.ID, subscription_vo.Update{ExpectedRevision: new(int64(1)), TimeZone: "UTC"}); !errors.Is(err, utils.ErrConflict) {
		t.Fatal("stale update accepted", err)
	}
	for _, bad := range []subscription_vo.Update{{TimeZone: "Local"}, {TimeZone: "UTC", DateKind: "monthly_renewal", RenewalDay: new(32)}, {TimeZone: "UTC", DateKind: "membership_expiry", MembershipDate: new("2026-02-30")}, {TimeZone: "UTC", DateKind: "", RenewalDay: new(1)}} {
		if _, err := s.Update(t.Context(), admin, a.ID, func() subscription_vo.Update { bad.ExpectedRevision = new(int64(0)); return bad }()); !errors.Is(err, utils.ErrBadParamInput) {
			t.Fatal("invalid settings accepted", bad, err)
		}
	}
	if _, err := s.Get(t.Context(), access_dto.Principal{Purpose: "collector"}, a.ID); !errors.Is(err, utils.ErrForbidden) {
		t.Fatal("collector read", err)
	}
	if _, err := s.Get(t.Context(), admin, reportingv1.Key("missing")); !errors.Is(err, utils.ErrNotFound) {
		t.Fatal("missing account", err)
	}
}
func TestSubscriptionDateRulesAcrossMonthsAndTimeZones(t *testing.T) {
	for _, tc := range []struct {
		at    string
		zone  string
		day   int
		delta int
	}{{"2028-02-28T23:00:00Z", "UTC", 31, 1}, {"2026-02-28T16:00:00Z", "Asia/Shanghai", 31, 30}, {"2026-03-01T00:00:00Z", "America/New_York", 31, 0}} {
		at, err := time.Parse(time.RFC3339, tc.at)
		if err != nil {
			t.Fatal(err)
		}
		v, err := dateStatus("monthly_renewal", new(tc.day), nil, at.UnixMilli(), tc.zone)
		if err != nil || v.Delta == nil || *v.Delta != tc.delta {
			t.Fatalf("case %+v status=%+v err=%v", tc, v, err)
		}
	}
	at := time.Date(2026, 10, 2, 0, 0, 0, 0, time.UTC).UnixMilli()
	v, err := dateStatus("membership_expiry", nil, new("2026-10-01"), at, "UTC")
	if err != nil || v.Delta == nil || *v.Delta != -1 || v.State != "needs_update" {
		t.Fatal("expired membership", v, err)
	}
}
