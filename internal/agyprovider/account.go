package agyprovider

import (
	"context"
	"strings"
)

// AccountSnapshot 表达本机 Antigravity 当前登录账号的非持久化展示信息。
// 邮箱原文仅用于前端账号胶囊即时展示，严禁写入本地 SQLite、Preferences 或日志。
type AccountSnapshot struct {
	Email    string
	PlanType string
}

// ReadAccountSnapshot 读取当前本机 Antigravity 账号身份与标准化套餐名称；
// 若 agy_online_enabled 关闭或 LS 离线，则安全回退至已持久化的非敏感 PlanType（无邮箱原文）。
func (service *QueryService) ReadAccountSnapshot(ctx context.Context) (AccountSnapshot, error) {
	if service == nil || service.disabled {
		return AccountSnapshot{}, nil
	}
	qc := service.quotaCollector
	if qc != nil && qc.isEnabled() {
		if status, err := qc.client.GetUserStatus(ctx); err == nil {
			email := strings.TrimSpace(status.Email)
			plan := NormalizeAGYPlanType(status.PlanName, status.TierName)
			if email != "" || plan != "" {
				qc.mu.Lock()
				qc.lastAccountEmail = email
				if plan != "" {
					qc.lastAccountPlan = plan
				}
				if id := status.AccountIdentity(); id != "" {
					qc.lastAccountScope = agyAccountScopeDigest(id)
				}
				qc.mu.Unlock()
				return AccountSnapshot{Email: email, PlanType: plan}, nil
			}
		}
		// 离线时若当前进程曾在本轮会话读取过，可复用内存态并回退持久化 PlanType
		qc.mu.Lock()
		cachedEmail := qc.lastAccountEmail
		cachedPlan := qc.lastAccountPlan
		qc.mu.Unlock()
		if cachedEmail != "" || cachedPlan != "" {
			return AccountSnapshot{Email: cachedEmail, PlanType: cachedPlan}, nil
		}
	}

	// agy_online_enabled 关闭或冷启动离线时，仅从持久化配额快照读回非敏感 PlanType（不含邮箱原文）
	if service.quotaStore != nil {
		if snaps, err := service.quotaStore.AGYQuotaSnapshots(ctx, ""); err == nil && len(snaps) > 0 {
			var latestUpdated int64
			var plan string
			for _, s := range snaps {
				if s.UpdatedAtMS >= latestUpdated && s.PlanType != nil && *s.PlanType != "" {
					latestUpdated = s.UpdatedAtMS
					plan = *s.PlanType
				}
			}
			if plan != "" {
				return AccountSnapshot{PlanType: plan}, nil
			}
		}
	}
	return AccountSnapshot{}, nil
}
