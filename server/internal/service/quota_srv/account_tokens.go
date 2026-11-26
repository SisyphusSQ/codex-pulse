package quota_srv

import (
	"context"

	quota_dto "github.com/SisyphusSQ/codex-pulse/server/internal/models/dto/quota_dto"
	quota_vo "github.com/SisyphusSQ/codex-pulse/server/internal/models/vo/quota_vo"
)

func activeAccountTokenWindow(window *quota_vo.Window, now int64) bool {
	if window.AccountKey == nil || window.WindowMinutes == nil || *window.WindowMinutes <= 0 ||
		window.Current.WindowStartAtMS == nil || window.Current.ResetsAtMS == nil ||
		*window.Current.WindowStartAtMS > now || *window.Current.ResetsAtMS <= now {
		return false
	}
	if window.Provider == "codex" {
		return window.LimitID == "codex" && *window.WindowMinutes == 10080
	}
	return true
}

func (s *Quota) withAccountTokens(ctx context.Context, q quota_dto.Query, window *quota_vo.Window, now int64) error {
	if !activeAccountTokenWindow(window, now) {
		return nil
	}
	total, available, err := s.repository.AccountTokenTotal(ctx, q, window.Provider, *window.AccountKey, *window.Current.WindowStartAtMS, *window.Current.ResetsAtMS, now)
	if err != nil || !available {
		return err
	}
	window.RecordedTokens = &total
	return nil
}
