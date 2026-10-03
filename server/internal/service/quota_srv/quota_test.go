package quota_srv

import (
	"errors"
	"net/url"
	"testing"
	"time"
	"uuid"

	reportingv1 "github.com/SisyphusSQ/codex-pulse/api/codexpulse/reporting/v1"
	access_dto "github.com/SisyphusSQ/codex-pulse/server/internal/models/dto/access_dto"
	quota_dto "github.com/SisyphusSQ/codex-pulse/server/internal/models/dto/quota_dto"
	quota_vo "github.com/SisyphusSQ/codex-pulse/server/internal/models/vo/quota_vo"
	"github.com/SisyphusSQ/codex-pulse/server/internal/repository/mysql/access_repo"
	"github.com/SisyphusSQ/codex-pulse/server/internal/repository/mysql/quota_repo"
	"github.com/SisyphusSQ/codex-pulse/server/internal/repository/mysql/reporting_repo"
	"github.com/SisyphusSQ/codex-pulse/server/internal/service/access_srv"
	"github.com/SisyphusSQ/codex-pulse/server/internal/service/reporting_srv"
	centerfixture "github.com/SisyphusSQ/codex-pulse/server/internal/testsupport/center"
	"github.com/SisyphusSQ/codex-pulse/server/utils"
)

const quotaNow = int64(1784000000000)

func quotaFixture(t *testing.T) (*Quota, *reporting_srv.Reporting, access_dto.Principal, []access_dto.Principal) {
	t.Helper()
	engine := centerfixture.Engine(t)
	access := access_srv.NewAccess(access_repo.NewAccess(engine))
	admin := centerfixture.Admin(t, access)
	clients := []access_dto.Principal{}
	for _, name := range []string{"一号机", "二号机", "三号机"} {
		clients = append(clients, centerfixture.Collector(t, access, admin.Principal, name))
	}
	s := NewQuota(quota_repo.NewQuota(engine))
	s.now = func() time.Time { return time.UnixMilli(quotaNow) }
	return s, reporting_srv.NewReporting(reporting_repo.NewReporting(engine)), admin.Principal, clients
}
func sendQuota(t *testing.T, s *reporting_srv.Reporting, p access_dto.Principal, b reportingv1.Batch) {
	t.Helper()
	b.ID, b.Version = uuid.New().String(), 1
	if _, err := s.Accept(t.Context(), p, b); err != nil {
		t.Fatal(err)
	}
}
func quotaFact(id, raw string, used float64, at, reset int64) reportingv1.QuotaObservation {
	return reportingv1.QuotaObservation{Provider: "codex", ID: id, LocalScope: "scope-" + raw, AccountID: new(raw), LimitID: "codex", WindowKind: "primary", WindowMinutes: new(int64(300)), ResetsAtMS: new(reset), ObservedAtMS: at, UsedPercent: new(used), Validity: "accepted", Source: "app_server", HistoryOrigin: "confirmed"}
}
func accountFacts(raw string) reportingv1.Batch {
	return reportingv1.Batch{Accounts: []reportingv1.Account{{Provider: "codex", ID: raw, Email: new("same@example.invalid"), CollectedAtMS: quotaNow - 60000}}, Bindings: []reportingv1.AccountBinding{{Provider: "codex", AccountID: raw, LocalScope: "scope-" + raw, ConfirmedAtMS: quotaNow - 60000}}}
}
func readQuota(t *testing.T, s *Quota, p access_dto.Principal, q quota_dto.Query) quota_vo.Response {
	t.Helper()
	out, err := s.Current(t.Context(), p, q)
	if err != nil {
		t.Fatal(err)
	}
	return out
}
func TestMultiMachineQuotaCopiesLateHistoryDecreaseConflictAndIsolation(t *testing.T) {
	s, reporting, admin, clients := quotaFixture(t)
	reset := quotaNow + 7200000
	for i, client := range clients {
		b := accountFacts("raw-a")
		b.Quotas = []reportingv1.QuotaObservation{quotaFact("copy", "raw-a", 40, quotaNow-60000, reset+int64(i)*1000)}
		sendQuota(t, reporting, client, b)
	}
	out := readQuota(t, s, admin, quota_dto.Query{})
	if len(out.Windows) != 1 || *out.Windows[0].Current.UsedPercent != 40 || len(out.Windows[0].Cycles) != 1 || len(out.Windows[0].Observations) != 3 {
		t.Fatal("copies accumulated or reset drift fragmented")
	}
	// 后接收的旧值与真正较新的下降分别按采集时间处理。
	for _, q := range []reportingv1.QuotaObservation{quotaFact("late", "raw-a", 80, quotaNow-120000, reset), quotaFact("decrease", "raw-a", 20, quotaNow-30000, reset)} {
		sendQuota(t, reporting, clients[0], reportingv1.Batch{Quotas: []reportingv1.QuotaObservation{q}})
	}
	out = readQuota(t, s, admin, quota_dto.Query{})
	if *out.Windows[0].Current.UsedPercent != 20 || out.Windows[0].Current.ObservedAtMS == nil || *out.Windows[0].Current.ObservedAtMS != quotaNow-30000 {
		t.Fatal("late receipt or historical peak replaced legal decrease")
	}
	sendQuota(t, reporting, clients[1], reportingv1.Batch{Quotas: []reportingv1.QuotaObservation{quotaFact("contradiction", "raw-a", 70, quotaNow-30000, reset)}})
	out = readQuota(t, s, admin, quota_dto.Query{})
	if !out.Windows[0].Current.Conflict || out.Windows[0].Current.ResetRemainingMS != nil {
		t.Fatal("same-time disagreement hidden")
	}
	b := accountFacts("raw-b")
	b.Quotas = []reportingv1.QuotaObservation{quotaFact("b", "raw-b", 0, quotaNow-10000, reset)}
	sendQuota(t, reporting, clients[2], b)
	out = readQuota(t, s, admin, quota_dto.Query{AccountKey: reportingv1.Key("codex", "raw-b")})
	if len(out.Accounts) != 1 || out.Accounts[0].RawID != "raw-b" || len(out.Windows) != 1 || *out.Windows[0].Current.UsedPercent != 0 {
		t.Fatal("email or device merged separate accounts")
	}
	if _, err := s.Current(t.Context(), clients[0], quota_dto.Query{}); !errors.Is(err, utils.ErrForbidden) {
		t.Fatal("collector read center quota")
	}
}
func TestQuotaNewCyclesExpiredUnknownAndLinkedHistoryCannotRefresh(t *testing.T) {
	s, reporting, admin, clients := quotaFixture(t)
	reset := quotaNow + 3600000
	b := accountFacts("raw-a")
	b.Quotas = []reportingv1.QuotaObservation{quotaFact("old", "raw-a", 50, quotaNow-1200000, reset)}
	sendQuota(t, reporting, clients[0], b)
	legacy := quotaFact("legacy", "raw-a", 90, quotaNow-10000, reset)
	legacy.LocalScope, legacy.AssociationScope, legacy.HistoryOrigin = "default", new("scope-raw-a"), "linked_history"
	sendQuota(t, reporting, clients[0], reportingv1.Batch{Quotas: []reportingv1.QuotaObservation{legacy}})
	out := readQuota(t, s, admin, quota_dto.Query{})
	if *out.Windows[0].Current.UsedPercent != 50 || out.Windows[0].Current.Freshness != "stale" || out.Windows[0].Current.ResetRemainingMS != nil || !out.Windows[0].Cycles[0].LinkedHistory {
		t.Fatal("legacy refreshed current")
	}
	q := quotaFact("new-cycle", "raw-a", 0, quotaNow-1000, reset+3600000)
	sendQuota(t, reporting, clients[0], reportingv1.Batch{Quotas: []reportingv1.QuotaObservation{q}})
	out = readQuota(t, s, admin, quota_dto.Query{})
	if *out.Windows[0].Current.UsedPercent != 0 || len(out.Windows[0].Cycles) != 2 || out.Windows[0].Current.Freshness != "fresh" {
		t.Fatal("actual new reset/zero not accepted")
	}
	s.now = func() time.Time { return time.UnixMilli(reset + 3600001) }
	out = readQuota(t, s, admin, quota_dto.Query{})
	if out.Windows[0].Current.Freshness != "expired_unknown" || out.Windows[0].Current.ResetRemainingMS != nil || *out.Windows[0].Current.ObservedAtMS != q.ObservedAtMS {
		t.Fatal("expiry fabricated current or receipt freshness")
	}
}
func TestUnassignedQuotaUnknownAndCreditsExpiryRemainDistinct(t *testing.T) {
	s, reporting, admin, clients := quotaFixture(t)
	q := quotaFact("unassigned", "unused", 10, quotaNow-1000, quotaNow+3600000)
	q.AccountID = nil
	q.HistoryOrigin = "pending_association"
	sendQuota(t, reporting, clients[0], reportingv1.Batch{Quotas: []reportingv1.QuotaObservation{q}})
	out := readQuota(t, s, admin, quota_dto.Query{})
	if out.Windows[0].IdentityState != "unassigned" || out.Windows[0].Current.Freshness != "unassigned" || out.Windows[0].Current.ResetRemainingMS != nil {
		t.Fatal("unconfirmed account upgraded")
	}
	for _, client := range clients {
		b := accountFacts("raw-a")
		b.Credits = []reportingv1.ResetCredits{{Provider: "codex", ID: "credits", LocalScope: "scope-raw-a", AccountID: new("raw-a"), ObservedAtMS: quotaNow - 60000, Inventory: new(int64(2)), Status: "accepted", DetailsStatus: "complete", ExpirySchedule: []reportingv1.CreditExpiry{{ExpiresAtMS: new(quotaNow - 30000), Count: 1}, {ExpiresAtMS: new(quotaNow + 30000), Count: 1}}, NextExpiresAtMS: new(quotaNow - 30000)}}
		sendQuota(t, reporting, client, b)
	}
	out = readQuota(t, s, admin, quota_dto.Query{})
	if len(out.Credits) != 1 || out.Credits[0].ObservedInventory == nil || *out.Credits[0].ObservedInventory != 2 || out.Credits[0].AvailableInventory == nil || *out.Credits[0].AvailableInventory != 1 || out.Credits[0].NextResetAtMS != nil || *out.Credits[0].NextExpiresAtMS != quotaNow+30000 {
		t.Fatal("credits summed or expiry became reset")
	}
	unknown := reportingv1.ResetCredits{Provider: "codex", ID: "failed-current", LocalScope: "scope-raw-a", AccountID: new("raw-a"), ObservedAtMS: quotaNow - 1000, Status: "unavailable"}
	sendQuota(t, reporting, clients[0], reportingv1.Batch{Credits: []reportingv1.ResetCredits{unknown}})
	out = readQuota(t, s, admin, quota_dto.Query{})
	if *out.Credits[0].ObservedInventory != 2 || out.Credits[0].AvailableInventory != nil || out.Credits[0].Freshness != "stale" {
		t.Fatal("failed refresh erased credits LKG")
	}
}
func TestQuotaQueriesRejectUnknownDuplicateAndUnsafeFilters(t *testing.T) {
	for _, raw := range []string{"provider=evil", "client_id=not-an-id", "account_key='OR1=1", "provider=codex&provider=grok", "token=secret"} {
		values, _ := url.ParseQuery(raw)
		if _, err := ParseQuery(values); !errors.Is(err, utils.ErrBadParamInput) {
			t.Fatal("invalid query accepted", raw)
		}
	}
}
