package quota_repo

import (
	"fmt"
	"math"
	"reflect"
	"strings"
	"testing"

	"gorm.io/gorm"

	reporting_do "github.com/SisyphusSQ/codex-pulse/server/internal/models/do/mysql/reporting_do"
	quota_dto "github.com/SisyphusSQ/codex-pulse/server/internal/models/dto/quota_dto"
	centerfixture "github.com/SisyphusSQ/codex-pulse/server/internal/testsupport/center"
)

func TestCreditsMatchFullRowsAcrossRanksTiesAndFilters(t *testing.T) {
	engine := centerfixture.Engine(t)
	db := engine.DB(t.Context())
	repository := NewQuota(engine)
	const now = int64(1800000000000)
	rows := []reporting_do.ResetCredits{}
	for _, provider := range []string{"codex", "cursor"} {
		for _, account := range []*string{nil, new("account-a"), new("account-b")} {
			for _, client := range []string{"client-a", "client-b", "client-c"} {
				for i, status := range []string{"accepted", "fresh", "stale", "unknown", "unavailable"} {
					for j, at := range []int64{now - 3000, now - 2000, now - 1000, now, now + 1, now + 600000} {
						inventory := []*int64{nil, new(int64(0)), new(int64(9007199254740993)), new(int64(math.MaxInt64))}[(i+j)%4]
						rows = append(rows, reporting_do.ResetCredits{ID: fmt.Sprintf("credit-%04d", len(rows)), Provider: provider, AccountKey: account, ClientID: client, LocalScope: "scope-" + client, ObservedAtMS: at, Inventory: inventory, Status: status, NextResetAtMS: new(now + 10000), NextExpiresAtMS: new(now + 20000), DetailsStatus: "partial", ExpirySchedule: `[{"count":"1","expires_at_ms":null}]` + strings.Repeat(" ", 2048)})
					}
				}
			}
		}
	}
	if err := db.CreateInBatches(rows, 100).Error; err != nil {
		t.Fatal(err)
	}
	queries := 0
	var generated string
	if err := db.Callback().Query().After("gorm:query").Register("test-credits-query", func(tx *gorm.DB) {
		if !tx.DryRun {
			queries++
			generated = tx.Statement.SQL.String()
		}
	}); err != nil {
		t.Fatal(err)
	}
	for _, q := range []quota_dto.Query{{}, {Provider: "codex"}, {AccountKey: "account-a"}, {ClientID: "client-b"}, {Provider: "cursor", AccountKey: "account-b", ClientID: "client-c"}, {AccountKey: "missing"}} {
		t.Run(fmt.Sprintf("%s/%s/%s", q.Provider, q.AccountKey, q.ClientID), func(t *testing.T) {
			base := db.Model(&reporting_do.ResetCredits{}).Where("account_key IS NOT NULL")
			if q.Provider != "" {
				base = base.Where("provider = ?", q.Provider)
			}
			if q.AccountKey != "" {
				base = base.Where("account_key = ?", q.AccountKey)
			}
			if q.ClientID != "" {
				base = base.Where("client_id = ?", q.ClientID)
			}
			base = base.Select("*,DENSE_RANK() OVER (PARTITION BY provider,account_key,CASE WHEN account_key IS NULL THEN client_id ELSE '' END,CASE WHEN account_key IS NULL THEN local_scope ELSE '' END,CASE WHEN inventory IS NOT NULL AND status IN ('accepted','fresh','stale') AND observed_at_ms <= ? THEN 1 ELSE 0 END ORDER BY observed_at_ms DESC) AS recency", now)
			var expected []reporting_do.ResetCredits
			if err := db.Table("(?) AS credits", base).Where("recency <= 2").Order("observed_at_ms,id").Find(&expected).Error; err != nil {
				t.Fatal(err)
			}
			queries = 0
			actual, err := repository.Credits(t.Context(), q, now)
			if err != nil || !reflect.DeepEqual(actual, expected) {
				t.Fatalf("credit evidence changed: actual=%d expected=%d err=%v", len(actual), len(expected), err)
			}
			if queries != 1 || !strings.Contains(generated, "SELECT id,DENSE_RANK()") || !strings.Contains(generated, "USE_NL(credits)") {
				t.Fatalf("expected one narrow ranking and primary-key read query, count=%d SQL=%s", queries, generated)
			}
			if q == (quota_dto.Query{}) && len(actual) <= 8 {
				t.Fatal("same-time evidence was reduced to two physical rows per account/provider")
			}
		})
	}
}
