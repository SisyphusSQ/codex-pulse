package quota_repo

import (
	"errors"
	"fmt"
	"math"
	"math/big"
	"testing"

	reporting_do "github.com/SisyphusSQ/codex-pulse/server/internal/models/do/mysql/reporting_do"
	quota_dto "github.com/SisyphusSQ/codex-pulse/server/internal/models/dto/quota_dto"
	centerfixture "github.com/SisyphusSQ/codex-pulse/server/internal/testsupport/center"
	"github.com/SisyphusSQ/codex-pulse/server/utils"
)

func TestAccountTokenTotalPreservesPrecisionAndFactBudget(t *testing.T) {
	engine := centerfixture.Engine(t)
	db := engine.DB(t.Context())
	repository := NewQuota(engine)
	period := reporting_do.AccountTokenPeriod{ID: "period", AccountKey: "account", Provider: "codex", ClientID: "client", WindowStartAtMS: 0, ResetsAtMS: 1000, CollectedAtMS: 500}
	if err := db.Create(&period).Error; err != nil {
		t.Fatal(err)
	}
	for start := 0; start < accountTokenFactLimit; start += 1000 {
		rows := make([]reporting_do.AccountTokenFact, 1000)
		for i := range rows {
			rows[i] = reporting_do.AccountTokenFact{ID: fmt.Sprint(start + i), AccountKey: "account", Provider: "codex", ObservedAtMS: 100, TotalTokens: math.MaxInt64}
		}
		if err := db.CreateInBatches(rows, 100).Error; err != nil {
			t.Fatal(err)
		}
	}
	want := new(big.Int).Mul(big.NewInt(math.MaxInt64), big.NewInt(accountTokenFactLimit)).String()
	total, available, err := repository.AccountTokenTotal(t.Context(), quota_dto.Query{}, "account", 0, 1000, 500)
	if err != nil || !available || total != want {
		t.Fatalf("exact total at budget: %s %v %v, want %s", total, available, err, want)
	}
	extra := reporting_do.AccountTokenFact{ID: "extra", AccountKey: "account", Provider: "codex", ObservedAtMS: 100, TotalTokens: 1}
	if err := db.Create(&extra).Error; err != nil {
		t.Fatal(err)
	}
	if _, _, err := repository.AccountTokenTotal(t.Context(), quota_dto.Query{}, "account", 0, 1000, 500); !errors.Is(err, utils.ErrRequestBudget) {
		t.Fatalf("fact budget not enforced: %v", err)
	}
}
