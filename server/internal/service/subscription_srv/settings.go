package subscription_srv

import (
	"context"
	"encoding/hex"
	"fmt"
	"math"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/SisyphusSQ/codex-pulse/internal/codex/subscriptionaccounts"
	reporting_do "github.com/SisyphusSQ/codex-pulse/server/internal/models/do/mysql/reporting_do"
	subscription_do "github.com/SisyphusSQ/codex-pulse/server/internal/models/do/mysql/subscription_do"
	access_dto "github.com/SisyphusSQ/codex-pulse/server/internal/models/dto/access_dto"
	subscription_vo "github.com/SisyphusSQ/codex-pulse/server/internal/models/vo/subscription_vo"
	subscription_repo "github.com/SisyphusSQ/codex-pulse/server/internal/repository/mysql/subscription_repo"
	access_srv "github.com/SisyphusSQ/codex-pulse/server/internal/service/access_srv"
	"github.com/SisyphusSQ/codex-pulse/server/utils"
)

type Subscription struct {
	repository *subscription_repo.Subscription
	now        func() time.Time
}

func NewSubscription(repository *subscription_repo.Subscription) *Subscription {
	return &Subscription{repository: repository, now: time.Now}
}
func validKey(key string) bool {
	b, err := hex.DecodeString(key)
	return err == nil && len(b) == 32 && key == strings.ToLower(key)
}
func (s *Subscription) Get(ctx context.Context, p access_dto.Principal, key string) (subscription_vo.View, error) {
	if err := access_srv.RequireAdmin(p); err != nil {
		return subscription_vo.View{}, err
	}
	if !validKey(key) {
		return subscription_vo.View{}, utils.ErrBadParamInput
	}
	var out subscription_vo.View
	err := s.repository.Snapshot(ctx, func(ctx context.Context) error {
		a, err := s.repository.Account(ctx, key)
		if err != nil {
			return err
		}
		settings, err := s.repository.Settings(ctx, key)
		if err != nil {
			return err
		}
		out, err = s.view(a, settings)
		return err
	})
	return out, err

}
func (s *Subscription) Update(ctx context.Context, p access_dto.Principal, key string, u subscription_vo.Update) (out subscription_vo.View, err error) {
	if err = access_srv.RequireAdmin(p); err != nil {
		return
	}
	if !validKey(key) || u.ExpectedRevision == nil || *u.ExpectedRevision < 0 || *u.ExpectedRevision == math.MaxInt64 {
		return out, utils.ErrBadParamInput
	}
	u.Alias = trim(u.Alias)
	u.ManualPlan = trim(u.ManualPlan)
	if (u.Alias != nil && (!utf8.ValidString(*u.Alias) || utf8.RuneCountInString(*u.Alias) > 128)) || (u.ManualPlan != nil && (!utf8.ValidString(*u.ManualPlan) || utf8.RuneCountInString(*u.ManualPlan) > 64)) {
		return out, utils.ErrBadParamInput
	}
	if len(u.TimeZone) > 64 {
		return out, utils.ErrBadParamInput
	}
	if _, err := subscriptionaccounts.LoadTimeZone(u.TimeZone); err != nil {
		return out, utils.ErrBadParamInput
	}
	if _, err := dateStatus(u.DateKind, u.RenewalDay, u.MembershipDate, s.now().UnixMilli(), u.TimeZone); err != nil {
		return out, utils.ErrBadParamInput
	}
	err = s.repository.Transaction(ctx, func(ctx context.Context) error {
		a, err := s.repository.Account(ctx, key)
		if err != nil {
			return err
		}
		settings := subscription_do.Settings{AccountKey: key, Revision: *u.ExpectedRevision + 1, Alias: u.Alias, ManualPlan: u.ManualPlan, DateKind: u.DateKind, RenewalDay: u.RenewalDay, MembershipDate: u.MembershipDate, TimeZone: u.TimeZone, UpdatedAtMS: s.now().UnixMilli()}
		if err := s.repository.Save(ctx, settings, *u.ExpectedRevision); err != nil {
			return err
		}
		out, err = s.view(a, settings)
		return err
	})
	return
}
func trim(value *string) *string {
	if value == nil {
		return nil
	}
	v := strings.TrimSpace(*value)
	if v == "" {
		return nil
	}
	return &v
}
func dateStatus(kind string, day *int, date *string, at int64, zone string) (subscriptionaccounts.DateStatus, error) {
	var nativeKind *subscriptionaccounts.DateKind
	switch kind {
	case "":
		if day != nil || date != nil {
			return subscriptionaccounts.DateStatus{}, utils.ErrBadParamInput
		}
	case "monthly_renewal":
		if day == nil || *day < 1 || *day > 31 || date != nil {
			return subscriptionaccounts.DateStatus{}, utils.ErrBadParamInput
		}
		d := fmt.Sprintf("2000-01-%02d", *day)
		date = &d
		nativeKind = new(subscriptionaccounts.DateKindNextRenewal)
	case "membership_expiry":
		if day != nil || date == nil {
			return subscriptionaccounts.DateStatus{}, utils.ErrBadParamInput
		}
		nativeKind = new(subscriptionaccounts.DateKindMembershipExpiry)
	default:
		return subscriptionaccounts.DateStatus{}, utils.ErrBadParamInput
	}
	return subscriptionaccounts.ResolveDateStatus(date, nativeKind, at, zone)
}
func (s *Subscription) view(a reporting_do.Account, settings subscription_do.Settings) (subscription_vo.View, error) {
	at := s.now().UnixMilli()
	status, err := dateStatus(settings.DateKind, settings.RenewalDay, settings.MembershipDate, at, settings.TimeZone)
	if err != nil {
		return subscription_vo.View{}, err
	}
	out := subscription_vo.View{AccountKey: a.ID, Revision: settings.Revision, Alias: settings.Alias, AutomaticPlan: a.Plan, ManualPlan: settings.ManualPlan, ResolvedPlan: a.Plan, DateKind: settings.DateKind, RenewalDay: settings.RenewalDay, MembershipDate: settings.MembershipDate, DayDelta: status.Delta, DateState: string(status.State), TimeZone: settings.TimeZone}
	if settings.ManualPlan != nil {
		out.ResolvedPlan = settings.ManualPlan
	}
	if settings.Revision > 0 {
		out.UpdatedAtMS = &settings.UpdatedAtMS
	}
	if status.Delta != nil {
		evaluation, err := subscriptionaccounts.EvaluationCivilDate(at, settings.TimeZone)
		if err != nil {
			return out, err
		}
		target := time.Date(evaluation.Year, time.Month(evaluation.Month), evaluation.Day, 0, 0, 0, 0, time.UTC).AddDate(0, 0, *status.Delta).Format(time.DateOnly)
		out.NextDate = &target
	}
	return out, nil
}
