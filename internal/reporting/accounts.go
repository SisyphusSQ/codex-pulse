package reporting

import (
	"context"
	"encoding/hex"
	"errors"

	"gorm.io/gorm"

	reportingv1 "github.com/SisyphusSQ/codex-pulse/api/codexpulse/reporting/v1"
)

// AccountIdentity 来自已有可信读取及 confirmed scope，不能从凭据或当前邮箱猜测。
// 它只进入 Go 上报边界与私有 reporting.db，不通过 CoreService 返回 Swift。
type AccountIdentity struct {
	Provider      string `gorm:"primaryKey"`
	LocalScope    string `gorm:"primaryKey"`
	AccountID     string
	Email         *string
	Plan          *string
	ConfirmedAtMS int64
	CollectedAtMS int64
}

func (AccountIdentity) TableName() string { return "reporting_account_identities" }

type AccountIdentitySource interface {
	ReportingAccountIdentities(context.Context) ([]AccountIdentity, error)
}

func (s *State) RememberIdentities(ctx context.Context, identities []AccountIdentity) error {
	if len(identities) > 10000 {
		return ErrQueueFull
	}
	for _, value := range identities {
		if value.LocalScope == "default" || value.LocalScope == "" || value.ConfirmedAtMS < 0 || value.CollectedAtMS < value.ConfirmedAtMS {
			return ErrProtocol
		}
		if (reportingv1.Batch{Version: 1, ID: "00000000-0000-0000-0000-000000000000", Accounts: []reportingv1.Account{{Provider: value.Provider, ID: value.AccountID, Email: value.Email, Plan: value.Plan, CollectedAtMS: value.CollectedAtMS}}, Bindings: []reportingv1.AccountBinding{{Provider: value.Provider, LocalScope: value.LocalScope, AccountID: value.AccountID, ConfirmedAtMS: value.ConfirmedAtMS}}}).Validate() != nil {
			return ErrProtocol
		}
	}
	return s.db.Write(ctx, func(_ context.Context, db *gorm.DB) error {
		for _, value := range identities {
			var previous AccountIdentity
			err := db.Where("provider = ? AND local_scope = ?", value.Provider, value.LocalScope).Take(&previous).Error
			if err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
				return err
			}
			if previous.AccountID != "" && previous.AccountID != value.AccountID {
				return ErrProtocol
			}
			if previous.CollectedAtMS > value.CollectedAtMS {
				continue
			}
			if value.Email == nil {
				value.Email = previous.Email
			}
			if value.Plan == nil {
				value.Plan = previous.Plan
			}
			if previous.ConfirmedAtMS > 0 {
				value.ConfirmedAtMS = min(value.ConfirmedAtMS, previous.ConfirmedAtMS)
			}
			if err := db.Save(&value).Error; err != nil {
				return err
			}
		}
		return nil
	})
}
func (s *State) Identities(ctx context.Context) (out []AccountIdentity, err error) {
	err = s.db.View(ctx, func(_ context.Context, db *gorm.DB) error {
		return db.Order("provider,local_scope").Limit(10001).Find(&out).Error
	})
	if len(out) > 10000 {
		return nil, ErrQueueFull
	}
	return
}

// PublicScope 不暴露安装级 HMAC key，也不把整个 Home 绑定到一个账号。
func (s *State) PublicScope(provider, scope string) string {
	if scope == "default" {
		return "default"
	}
	return s.HomeID("account-scope", provider, scope)
}

// FactsCheckpoint 与不可变批次在一个 writer 事务提交，重启/丢确认不改变 body。
type factsCheckpoint struct {
	Partition, SourceKey string `gorm:"primaryKey"`
	Digest               string
}

func (factsCheckpoint) TableName() string { return "reporting_facts_checkpoints" }
func boundedFactsKey(key string) bool {
	if len(key) != 64 {
		return false
	}
	_, err := hex.DecodeString(key)
	return err == nil
}
