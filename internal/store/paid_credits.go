package store

import (
	"context"
	"crypto/sha256"
	"fmt"
	"reflect"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"github.com/SisyphusSQ/codex-pulse/internal/creditbalance"
	storeschema "github.com/SisyphusSQ/codex-pulse/internal/store/schema"
)

type paidCreditModel struct {
	ID           string  `gorm:"column:id;primaryKey"`
	RequestID    string  `gorm:"column:request_id"`
	AccountScope string  `gorm:"column:account_scope"`
	LimitID      string  `gorm:"column:limit_id"`
	State        string  `gorm:"column:state"`
	Balance      *string `gorm:"column:balance"`
	HasCredits   *bool   `gorm:"column:has_credits"`
	Unlimited    *bool   `gorm:"column:unlimited"`
	ObservedAtMS int64   `gorm:"column:observed_at_ms"`
}

func (paidCreditModel) TableName() string { return "codex_paid_credits" }

var paidCreditSchemaObjects = []storeschema.Object{
	{ObjectType: "table", Name: "codex_paid_credits", Statement: `CREATE TABLE IF NOT EXISTS codex_paid_credits (
		id TEXT PRIMARY KEY CHECK(length(id) = 64),
		request_id TEXT NOT NULL,
		account_scope TEXT NOT NULL CHECK(length(account_scope) = 64),
		limit_id TEXT NOT NULL CHECK(length(limit_id) BETWEEN 1 AND 512),
		state TEXT NOT NULL CHECK(state IN ('known','unavailable','invalid')),
		balance TEXT CHECK(balance IS NULL OR length(balance) BETWEEN 1 AND 49),
		has_credits INTEGER CHECK(has_credits IS NULL OR has_credits IN (0,1)),
		unlimited INTEGER CHECK(unlimited IS NULL OR unlimited IN (0,1)),
		observed_at_ms INTEGER NOT NULL CHECK(observed_at_ms BETWEEN 0 AND 9007199254740991)
	) STRICT`},
	{ObjectType: "index", Name: "idx_paid_credits_scope_time", Statement: `CREATE INDEX IF NOT EXISTS idx_paid_credits_scope_time ON codex_paid_credits(account_scope, observed_at_ms, limit_id)`},
}

func applicationSchemaV39Checksum() string {
	return fmt.Sprintf("%x", sha256.Sum256([]byte("39/codex-paid-credits/v1\n"+paidCreditSchemaObjects[0].Statement+paidCreditSchemaObjects[1].Statement)))
}

func recordPaidCredits(database *gorm.DB, record QuotaFetchRecord, replay bool) error {
	if replay {
		var count int64
		if err := database.Model(&paidCreditModel{}).Where("request_id = ?", record.Attempt.RequestID).Count(&count).Error; err != nil {
			return err
		}
		if count != int64(len(record.PaidCredits)) {
			return invalidRecord("paid credits replay set conflicts")
		}
	}
	seen := map[string]bool{}
	for _, sample := range record.PaidCredits {
		if record.Attempt.Outcome == SourceAttemptCancelled || seen[sample.LimitID] || !validDerivedCodexAccountScope(record.ScopeKey) || !sample.Valid() || sample.ObservedAtMS < record.Attempt.StartedAtMS || sample.ObservedAtMS > record.Attempt.FinishedAtMS {
			return invalidRecord("paid credits provenance is invalid")
		}
		seen[sample.LimitID] = true
		row := paidCreditModel{ID: SHA256DigestOf([]byte(record.Attempt.RequestID + "\x00" + sample.LimitID)).String(), RequestID: record.Attempt.RequestID, AccountScope: record.ScopeKey, LimitID: sample.LimitID, State: sample.State, Balance: sample.Balance, HasCredits: sample.HasCredits, Unlimited: sample.Unlimited, ObservedAtMS: sample.ObservedAtMS}
		if !replay {
			if err := database.Clauses(clause.OnConflict{DoNothing: true}).Create(&row).Error; err != nil {
				return err
			}
		}
		var existing paidCreditModel
		if err := database.Where("id = ?", row.ID).Take(&existing).Error; err != nil {
			return err
		}
		if !reflect.DeepEqual(row, existing) {
			return invalidRecord("paid credits replay conflicts")
		}
	}
	return nil
}

func paidCreditSnapshot(row paidCreditModel) creditbalance.Snapshot {
	return creditbalance.Snapshot{LimitID: row.LimitID, State: row.State, Balance: row.Balance, HasCredits: row.HasCredits, Unlimited: row.Unlimited, ObservedAtMS: row.ObservedAtMS}
}

func readPaidCredits(ctx context.Context, database *gorm.DB, scope string, now int64) ([]creditbalance.View, error) {
	var latest, recent []paidCreditModel
	ranked := database.Model(&paidCreditModel{}).Where("account_scope = ? AND observed_at_ms <= ?", scope, now).
		Select("*, DENSE_RANK() OVER (PARTITION BY limit_id, state ORDER BY observed_at_ms DESC) AS position")
	if err := database.Table("(?) AS ranked", ranked).Where("position = 1").Find(&latest).Error; err != nil {
		return nil, err
	}
	daily := database.Model(&paidCreditModel{}).Where("account_scope = ? AND state = 'known' AND observed_at_ms >= ? AND observed_at_ms <= ?", scope, max(int64(0), now-30*creditbalance.DayMS), now).
		Select("*, DENSE_RANK() OVER (PARTITION BY limit_id, CAST(observed_at_ms / 86400000 AS INTEGER) ORDER BY observed_at_ms DESC) AS position")
	if err := database.Table("(?) AS ranked", daily).Where("position = 1").Find(&recent).Error; err != nil {
		return nil, err
	}
	samples := make([]creditbalance.Snapshot, 0, len(latest)+len(recent))
	for _, row := range append(latest, recent...) {
		samples = append(samples, paidCreditSnapshot(row))
	}
	state, found, err := sourceStateByID(ctx, database, QuotaSourceInstanceAppServer(scope))
	if err != nil {
		return nil, err
	}
	var attempted int64
	failed := false
	if found && state.LastAttemptAtMS != nil {
		attempted = *state.LastAttemptAtMS
		failed = state.LastFailureCode != nil
	}
	return creditbalance.Views(samples, now, attempted, failed), nil
}
