package quota_repo

import (
	"fmt"
	"reflect"
	"testing"

	"gorm.io/gorm"

	reportingv1 "github.com/SisyphusSQ/codex-pulse/api/codexpulse/reporting/v1"
	reporting_do "github.com/SisyphusSQ/codex-pulse/server/internal/models/do/mysql/reporting_do"
	quota_dto "github.com/SisyphusSQ/codex-pulse/server/internal/models/dto/quota_dto"
	centerfixture "github.com/SisyphusSQ/codex-pulse/server/internal/testsupport/center"
)

func TestHeadersMatchFullRowsAcrossTiesValidityAndSources(t *testing.T) {
	engine := centerfixture.Engine(t)
	repository := NewQuota(engine)
	db := engine.DB(t.Context())
	const now = int64(1800000000000)
	account := "account"
	rows := make([]reporting_do.QuotaObservation, 2048)
	for i := range rows {
		at := now - int64(i%50)*1000
		if i%7 == 0 {
			at = now + 1000000
		}
		used := new(float64(i % 3))
		if i%11 == 0 {
			used = nil
		}
		reset := new(now + int64(i%4)*100000)
		if i%13 == 0 {
			reset = nil
		}
		rows[i] = reporting_do.QuotaObservation{ID: reportingv1.Key(fmt.Sprint(i)), Provider: "codex", AccountKey: &account, ClientID: fmt.Sprint(i % 3), LocalScope: fmt.Sprint(i / 8), LimitID: "codex", WindowKind: "secondary", WindowMinutes: new(int64(10080)), ResetsAtMS: reset, ObservedAtMS: at, UsedPercent: used, Source: []string{"app_server", "legacy_wham"}[i%2], HistoryOrigin: []string{"confirmed", "linked_history"}[(i/2)%2], Validity: []string{"accepted", "invalid"}[(i/3)%2], ReceivedAtMS: at + 5}
	}
	if err := db.CreateInBatches(rows, 100).Error; err != nil {
		t.Fatal(err)
	}
	queries := 0
	if err := db.Callback().Query().After("gorm:query").Register("test-headers-query-count", func(tx *gorm.DB) {
		if !tx.DryRun {
			queries++
		}
	}); err != nil {
		t.Fatal(err)
	}
	scope := quota_dto.WindowScope{Provider: "codex", AccountKey: &account, LimitID: "codex", WindowKind: "secondary", WindowMinutes: new(int64(10080))}
	for _, client := range []string{"", "1"} {
		t.Run("client-"+client, func(t *testing.T) {
			q := quota_dto.Query{RawHistory: true, ClientID: client}
			partition := "client_id,local_scope,source,history_origin,validity,resets_at_ms,CASE WHEN used_percent > 0 THEN 1 ELSE 0 END,CASE WHEN observed_at_ms <= ? AND resets_at_ms > observed_at_ms AND resets_at_ms-observed_at_ms <= window_minutes*60000+? THEN 1 ELSE 0 END"
			original := fmt.Sprintf("*,ROW_NUMBER() OVER (PARTITION BY %s ORDER BY observed_at_ms,id) AS first_rank,ROW_NUMBER() OVER (PARTITION BY %s ORDER BY observed_at_ms DESC,id DESC) AS last_rank", partition, partition)
			base := repository.windowQuery(t.Context(), q, scope).Select(original, now+300000, 300000, now+300000, 300000)
			var expected []reporting_do.QuotaObservation
			if err := db.Table("(?) AS headers", base).Where("first_rank=1 OR last_rank=1").Order("observed_at_ms,id").Find(&expected).Error; err != nil {
				t.Fatal(err)
			}
			if client == "" && len(expected) <= 1000 {
				t.Fatal("fixture does not cover more than one former endpoint batch")
			}
			queries = 0
			actual, err := repository.Headers(t.Context(), q, scope, now, 300000)
			if err != nil || !reflect.DeepEqual(actual, expected) {
				t.Fatalf("endpoint projection changed: actual=%d expected=%d err=%v", len(actual), len(expected), err)
			}
			if queries != 1 {
				t.Fatalf("headers used %d queries, want one", queries)
			}
		})
	}
}
