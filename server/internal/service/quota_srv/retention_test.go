package quota_srv

import (
	"errors"
	"fmt"
	"math/rand/v2"
	"reflect"
	"testing"
	"time"
	"uuid"

	reportingv1 "github.com/SisyphusSQ/codex-pulse/api/codexpulse/reporting/v1"
	reporting_do "github.com/SisyphusSQ/codex-pulse/server/internal/models/do/mysql/reporting_do"
	quota_dto "github.com/SisyphusSQ/codex-pulse/server/internal/models/dto/quota_dto"
	"github.com/SisyphusSQ/codex-pulse/server/internal/repository/mysql/access_repo"
	"github.com/SisyphusSQ/codex-pulse/server/internal/repository/mysql/quota_repo"
	"github.com/SisyphusSQ/codex-pulse/server/internal/repository/mysql/reporting_repo"
	"github.com/SisyphusSQ/codex-pulse/server/internal/service/access_srv"
	"github.com/SisyphusSQ/codex-pulse/server/internal/service/reporting_srv"
	centerfixture "github.com/SisyphusSQ/codex-pulse/server/internal/testsupport/center"
	"github.com/SisyphusSQ/codex-pulse/server/utils"
)

func TestFourCyclesCompactionKeepsEndpointsCurrentAndReplayIdentity(t *testing.T) {
	engine := centerfixture.Engine(t)
	access := access_srv.NewAccess(access_repo.NewAccess(engine))
	admin := centerfixture.Admin(t, access).Principal
	client := centerfixture.Collector(t, access, admin, "synthetic")
	reporting := reporting_srv.NewReporting(reporting_repo.NewReporting(engine))
	quota := NewQuota(quota_repo.NewQuota(engine))
	quota.now = func() time.Time { return time.UnixMilli(quotaNow) }
	b := accountFacts("retention")
	facts := []reportingv1.QuotaObservation{}
	for cycle := 0; cycle < 6; cycle++ {
		reset := quotaNow + 3600000 - int64(5-cycle)*18000000
		for i := 0; i < 8; i++ {
			facts = append(facts, quotaFact(fmt.Sprintf("c%d-%d", cycle, i), "retention", 20, reset-3600000+int64(i)*1000, reset))
		}
	}
	b.Quotas = facts
	sendQuota(t, reporting, client, b)
	before := readQuota(t, quota, admin, quota_dto.Query{}).Windows[0]
	if len(before.Cycles) != 4 {
		t.Fatalf("cycles=%d", len(before.Cycles))
	}
	retired, err := quota.Compact(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if retired != 34 {
		t.Fatalf("retired=%d want34", retired)
	} // 两旧周期16 + 三结束周期各6；当前8完整保留。
	after := readQuota(t, quota, admin, quota_dto.Query{}).Windows[0]
	if !reflect.DeepEqual(before.Current, after.Current) || len(after.Cycles) != 4 || len(after.Observations) != 14 {
		t.Fatal("compaction changed current/cycles/endpoints")
	}
	var removed reporting_do.RetiredObservation
	if err := engine.DB(t.Context()).First(&removed).Error; err != nil {
		t.Fatal(err)
	}
	var replay reportingv1.QuotaObservation
	for _, f := range facts {
		if reportingv1.Key(client.ID, f.Provider, f.ID) == removed.ID {
			replay = f
		}
	}
	if replay.ID == "" {
		t.Fatal("retired identity mismatch")
	}
	sendQuota(t, reporting, client, reportingv1.Batch{Quotas: []reportingv1.QuotaObservation{replay}})
	var count int64
	engine.DB(t.Context()).Model(&reporting_do.QuotaObservation{}).Count(&count)
	if count != 14 {
		t.Fatalf("replay resurrected %d", count)
	}
	replay.UsedPercent = new(float64(99))
	_, err = reporting.Accept(t.Context(), client, reportingv1.Batch{ID: uuid.New().String(), Version: 1, Quotas: []reportingv1.QuotaObservation{replay}})
	if !errors.Is(err, utils.ErrConflict) {
		t.Fatalf("changed retired fact accepted: %v", err)
	}
	if n, err := quota.Compact(t.Context()); err != nil || n != 0 {
		t.Fatalf("not idempotent: %d %v", n, err)
	}
}

func TestSummaryKeepsValidPastBehindFutureAndInvalidObservations(t *testing.T) {
	s, reporting, admin, clients := quotaFixture(t)
	b := accountFacts("summary")
	reset := quotaNow + 7200000
	b.Quotas = []reportingv1.QuotaObservation{quotaFact("first", "summary", 10, quotaNow-60000, reset), quotaFact("last-valid", "summary", 30, quotaNow-10000, reset), quotaFact("future1", "summary", 40, quotaNow+300000, reset), quotaFact("future2", "summary", 50, quotaNow+400000, reset), quotaFact("future3", "summary", 60, quotaNow+500000, reset)}
	sendQuota(t, reporting, clients[0], b)
	full := readQuota(t, s, admin, quota_dto.Query{}).Windows[0]
	summary := readQuota(t, s, admin, quota_dto.Query{View: "summary"}).Windows[0]
	if !reflect.DeepEqual(full.Current, summary.Current) || len(summary.Observations) != 0 || len(summary.Cycles) != 0 {
		t.Fatal("summary lost last valid fact")
	}
	b = reportingv1.Batch{Credits: []reportingv1.ResetCredits{}}
	for i := 0; i < 4; i++ {
		at := quotaNow - 10000
		if i > 0 {
			at = quotaNow + int64(i)*300000
		}
		b.Credits = append(b.Credits, reportingv1.ResetCredits{Provider: "codex", ID: fmt.Sprint(i), LocalScope: "scope-summary", AccountID: new("summary"), ObservedAtMS: at, Inventory: new(int64(i + 3)), Status: "accepted", DetailsStatus: "unavailable"})
	}
	sendQuota(t, reporting, clients[0], b)
	out := readQuota(t, s, admin, quota_dto.Query{View: "summary"})
	if len(out.Credits) != 1 || out.Credits[0].ObservedInventory == nil || *out.Credits[0].ObservedInventory != 3 {
		t.Fatal("future Credits displaced valid inventory")
	}
}

func TestSummaryAboveOldGlobalObservationBudget(t *testing.T) {
	engine := centerfixture.Engine(t)
	access := access_srv.NewAccess(access_repo.NewAccess(engine))
	admin := centerfixture.Admin(t, access).Principal
	s := NewQuota(quota_repo.NewQuota(engine))
	s.now = func() time.Time { return time.UnixMilli(quotaNow) }
	if err := engine.DB(t.Context()).Create(&reporting_do.Account{ID: "account", Provider: "codex", Plan: new("plus")}).Error; err != nil {
		t.Fatal(err)
	}
	rows := make([]reporting_do.QuotaObservation, 100005)
	for i := range rows {
		rows[i] = reporting_do.QuotaObservation{ID: reportingv1.Key("large", fmt.Sprint(i)), ObservationID: fmt.Sprint(i), ClientID: "synthetic", Provider: "codex", LocalScope: "scope", AccountKey: new("account"), LimitID: "codex", WindowKind: "primary", WindowMinutes: new(int64(300)), ResetsAtMS: new(quotaNow + 7200000), ObservedAtMS: quotaNow - 2000000 + int64(i), UsedPercent: new(float64(30)), Validity: "accepted", Source: "app_server", HistoryOrigin: "confirmed"}
	}
	if err := engine.DB(t.Context()).CreateInBatches(rows, 250).Error; err != nil {
		t.Fatal(err)
	}
	out := readQuota(t, s, admin, quota_dto.Query{View: "summary"})
	if len(out.Windows) != 1 || out.Windows[0].ObservationCount != 0 || len(out.Windows[0].Observations) != 0 || *out.Windows[0].Current.UsedPercent != 30 {
		t.Fatal("large history rejected or leaked into summary")
	}
}

func TestSummaryMatchesFullArbitrationAcrossMixedResetEvidence(t *testing.T) {
	engine := centerfixture.Engine(t)
	s := NewQuota(quota_repo.NewQuota(engine))
	s.now = func() time.Time { return time.UnixMilli(quotaNow) }
	access := access_srv.NewAccess(access_repo.NewAccess(engine))
	admin := centerfixture.Admin(t, access).Principal
	if err := engine.DB(t.Context()).Create(&reporting_do.Account{ID: "account", Provider: "codex", Plan: new("plus")}).Error; err != nil {
		t.Fatal(err)
	}
	random := rand.New(rand.NewPCG(41, 53))
	for trial := 0; trial < 100; trial++ {
		rows := []reporting_do.QuotaObservation{}
		for i := 0; i < 80; i++ {
			at := quotaNow - 18000000 + int64(i)*240000
			reset := quotaNow - 7200000 + int64(random.IntN(4))*7200000
			source := []string{"app_server", "local_jsonl", "wham"}[random.IntN(3)]
			used := float64(random.IntN(3) * 20)
			rows = append(rows, reporting_do.QuotaObservation{ID: reportingv1.Key(fmt.Sprint(trial), fmt.Sprint(i)), ClientID: fmt.Sprint(random.IntN(2)), Provider: "codex", AccountKey: new("account"), LocalScope: "scope", LimitID: "codex", WindowKind: "primary", WindowMinutes: new(int64(300)), ResetsAtMS: new(reset), ObservedAtMS: at, UsedPercent: new(used), Validity: "accepted", Source: source, HistoryOrigin: "confirmed"})
		}
		if err := engine.DB(t.Context()).Exec("DELETE FROM pulse_quota_observations").Error; err != nil {
			t.Fatal(err)
		}
		if err := engine.DB(t.Context()).CreateInBatches(rows, 250).Error; err != nil {
			t.Fatal(err)
		}
		full, err := buildWindow(windowKey(rows[0]), rows, map[string]string{}, quotaNow)
		if err != nil {
			t.Fatal(err)
		}
		summary := readQuota(t, s, admin, quota_dto.Query{View: "summary"}).Windows[0]
		if !reflect.DeepEqual(full.Current, summary.Current) {
			t.Fatalf("summary arbitration differs at trial %d: full=%+v summary=%+v", trial, full.Current, summary.Current)
		}
	}
}
