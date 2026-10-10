package reporting_srv

import (
	"context"
	"encoding/hex"

	reporting_dto "github.com/SisyphusSQ/codex-pulse/server/internal/models/dto/reporting_dto"
	"github.com/SisyphusSQ/codex-pulse/server/utils"
)

// ReconcileCursor 对已收到的 Cursor 来源重新仲裁。每个会话独立事务，和实时
// 上报持有相同 session 锁；失败会话回滚，Next 只推进到已完成的会话。
func (s *Reporting) ReconcileCursor(ctx context.Context, after string, limit int, apply bool) (result reporting_dto.CursorReconciliation, err error) {
	result.Next, result.Applied = after, apply
	if limit < 1 || limit > 128 {
		return result, utils.ErrBadParamInput
	}
	if after != "" {
		if len(after) != 64 {
			return result, utils.ErrBadParamInput
		}
		if _, err := hex.DecodeString(after); err != nil {
			return result, utils.ErrBadParamInput
		}
	}
	keys, err := s.repository.CursorSessionKeys(ctx, after, limit)
	if err != nil {
		return result, err
	}
	for _, key := range keys {
		var changed bool
		err = s.repository.Transaction(ctx, func(ctx context.Context) error {
			current, err := s.repository.SessionForUpdate(ctx, key)
			if err != nil {
				return err
			}
			changed, err = s.rebuildSession(ctx, current, apply)
			return err
		})
		if err != nil {
			return result, err
		}
		result.Processed++
		if changed {
			result.Changed++
		}
		result.Next = key
	}
	return result, nil
}
