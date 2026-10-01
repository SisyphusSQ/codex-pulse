package service

import (
	"errors"
	"net/url"
	"strings"
	"testing"
	"time"
	"uuid"

	reportingv1 "github.com/SisyphusSQ/codex-pulse/api/codexpulse/reporting/v1"
	"github.com/SisyphusSQ/codex-pulse/server/internal/models/dto"
	"github.com/SisyphusSQ/codex-pulse/server/internal/models/vo"
	"github.com/SisyphusSQ/codex-pulse/server/internal/repository"
	"github.com/SisyphusSQ/codex-pulse/server/utils"
)

func statisticsFixture(t *testing.T) (*Statistics, *Reporting, dto.Principal, []dto.Principal) {
	t.Helper()
	access, engine := accessFixture(t)
	admin := pairedAdmin(t, access).Principal
	clients := []dto.Principal{collectorForReporting(t, access, admin, "一"), collectorForReporting(t, access, admin, "二"), collectorForReporting(t, access, admin, "三")}
	stats := NewStatistics(repository.NewStatistics(engine))
	stats.now = func() time.Time { return time.Date(2026, 10, 1, 12, 0, 0, 0, time.UTC) }
	return stats, NewReporting(repository.NewReporting(engine)), admin, clients
}
func statisticsTestQuery(t *testing.T, values url.Values) dto.StatisticsQuery {
	t.Helper()
	if values == nil {
		values = url.Values{}
	}
	if values.Get("start_at_ms") == "" && values.Get("start_date") == "" {
		values.Set("start_at_ms", "0")
		values.Set("end_at_ms", "10000")
	}
	if values.Get("time_zone") == "" {
		values.Set("time_zone", "UTC")
	}
	q, err := ParseStatisticsQuery(values, time.Date(2026, 10, 1, 12, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatal(err)
	}
	return q
}
func decimal(t *testing.T, value *string, want string) {
	t.Helper()
	if value == nil || *value != want {
		t.Fatalf("decimal got=%v want=%s", value, want)
	}
}
func TestStatisticsGlobalDedupAndActualCollectorScope(t *testing.T) {
	stats, r, admin, clients := statisticsFixture(t)
	snap := reportingSnapshot()
	snap.LastActiveAtMS = new(int64(2000))
	for _, p := range clients {
		sendSnapshot(t, r, p, snap)
	}
	snap.Revision = 2
	snap.Contributions = append(snap.Contributions, reportingContribution(25, 2000))
	sendSnapshot(t, r, clients[1], snap)
	q := statisticsTestQuery(t, nil)
	global, err := stats.Summary(t.Context(), admin, q)
	if err != nil {
		t.Fatal(err)
	}
	decimal(t, global.Totals.TotalTokens, "125")
	if global.Totals.Sessions != 1 || len(global.Devices) != 1 || global.Devices[0].Key != "execution_unknown" {
		t.Fatal("copies claimed execution devices")
	}
	decimal(t, global.Devices[0].Totals.TotalTokens, "125")
	decimal(t, global.Providers[0].Totals.TotalTokens, "125")
	decimal(t, global.Models[0].Totals.TotalTokens, "125")
	if global.Totals.CostMicroUSD != nil || global.Coverage.UnpricedFacts != 2 || global.Coverage.State != "partial" {
		t.Fatal("unpriced or absent coverage disguised")
	}
	if global.HeatmapRange == global.Range || len(global.Heatmap) != 365 || *global.Heatmap[0].Totals.TotalTokens != "0" {
		t.Fatal("annual/current range mixed")
	}
	q.ClientID = clients[0].ID
	own, err := stats.Summary(t.Context(), admin, q)
	if err != nil {
		t.Fatal(err)
	}
	decimal(t, own.Totals.TotalTokens, "100")
	q.ProjectID = reportingv1.Key(clients[0].ID, "codex", "project")
	own, err = stats.Summary(t.Context(), admin, q)
	if err != nil {
		t.Fatal(err)
	}
	decimal(t, own.Totals.TotalTokens, "100")
	q.ClientID = ""
	filtered, err := stats.Summary(t.Context(), admin, q)
	if err != nil {
		t.Fatal(err)
	}
	decimal(t, filtered.Totals.TotalTokens, "0")
	list, err := stats.Sessions(t.Context(), admin, statisticsTestQuery(t, nil))
	if err != nil {
		t.Fatal(err)
	}
	if len(list.Items) != 1 || len(list.Items[0].Sources) != 3 || list.Items[0].SessionID == nil || *list.Items[0].SessionID != "session" {
		t.Fatal("provenance or raw identity lost")
	}
	if _, err = stats.Summary(t.Context(), clients[0], q); !errors.Is(err, utils.ErrForbidden) {
		t.Fatal("collector queried center")
	}
}
func fixStatisticsIDs(snap *reportingv1.SessionSnapshot) {
	seen := map[string]int64{}
	for i, c := range snap.Contributions {
		key := reportingv1.ContributionID(snap.Provider, snap.SessionID, c, 0)
		c.ID = reportingv1.ContributionID(snap.Provider, snap.SessionID, c, seen[key])
		seen[key]++
		snap.Contributions[i] = c
	}
}
func TestStatisticsFullRangeSearchPaginationModelAndProjectAssociation(t *testing.T) {
	stats, r, admin, clients := statisticsFixture(t)
	for i, title := range []string{"甲 needle", "乙 needle", "丙其他"} {
		snap := reportingSnapshot()
		snap.SessionID = uuid.New().String()
		snap.Title = title
		snap.ProjectID = "p" + title
		snap.Contributions[0].InputTokens = new(int64(100 * (i + 1)))
		snap.Contributions[0].TotalTokens = new(int64(100 * (i + 1)))
		snap.Contributions[0].Model = new("model-a")
		snap.LastActiveAtMS = new(int64(1000 + i))
		fixStatisticsIDs(&snap)
		sendSnapshot(t, r, clients[0], snap)
	}
	q := statisticsTestQuery(t, url.Values{"search": {"needle"}, "page": {"2"}, "limit": {"1"}, "sort": {"tokens"}})
	list, err := stats.Sessions(t.Context(), admin, q)
	if err != nil {
		t.Fatal(err)
	}
	if list.Page.Total != 2 || len(list.Items) != 1 || list.Items[0].Title != "甲 needle" {
		t.Fatal("search/sort/page not full scope")
	}
	decimal(t, list.Totals.TotalTokens, "300")
	q.Page = 1
	q.Model = "nonexistent"
	none, err := stats.Sessions(t.Context(), admin, q)
	if err != nil {
		t.Fatal(err)
	}
	if none.Page.Total != 0 {
		t.Fatal("model ignored")
	}
	decimal(t, none.Totals.TotalTokens, "0")
	q = statisticsTestQuery(t, url.Values{"sort": {"name"}, "limit": {"1"}})
	projects, err := stats.Projects(t.Context(), admin, q)
	if err != nil {
		t.Fatal(err)
	}
	if projects.Page.Total != 3 {
		t.Fatal("same name projects merged")
	}
	decimal(t, projects.Totals.TotalTokens, "600")
	q.Limit = 100
	projects, err = stats.Projects(t.Context(), admin, q)
	if err != nil {
		t.Fatal(err)
	}
	ids := []string{}
	for _, p := range projects.Items {
		ids = append(ids, p.ID)
	}
	if _, err = r.AssociateProjects(t.Context(), admin, vo.ProjectAssociationRequest{ProjectIDs: ids[1:], TargetID: ids[0]}); err != nil {
		t.Fatal(err)
	}
	project, err := stats.Project(t.Context(), admin, q, ids[0])
	if err != nil {
		t.Fatal(err)
	}
	if len(project.Project.Members) != 3 || project.Sessions.Page.Total != 3 {
		t.Fatal("project association detail incomplete")
	}
	decimal(t, project.Project.Totals.TotalTokens, "600")
	detail, err := stats.Session(t.Context(), admin, q, project.Sessions.Items[0].ID)
	if err != nil || detail.Session.SessionID == nil {
		t.Fatal("session drilldown failed", err)
	}
}
func TestStatisticsExactHistoricPriceGroupedCachedDeltaAndUnknownTime(t *testing.T) {
	stats, r, admin, clients := statisticsFixture(t)
	snap := reportingSnapshot()
	rate := int64(1000000)
	// A cached delta can exceed an input delta; only the grouped decomposition decides priceability.
	snap.Contributions = []reportingv1.Contribution{}
	for i, pair := range [][2]int64{{400, 500}, {600, 100}} {
		c := reportingContribution(pair[0], 1000+int64(i))
		c.CachedTokens = new(pair[1])
		c.Model = new("model")
		c.PricingMode = "codex_model_sum"
		c.PricingVersion = new("historical-v1")
		c.CostStatus = "known"
		c.Rates = &reportingv1.Rates{InputMicroUSD: &rate, CachedMicroUSD: new(int64(500000)), OutputMicroUSD: &rate}
		snap.Contributions = append(snap.Contributions, c)
	}
	fixStatisticsIDs(&snap)
	sendSnapshot(t, r, clients[0], snap)
	q := statisticsTestQuery(t, nil)
	result, err := stats.Summary(t.Context(), admin, q)
	if err != nil {
		t.Fatal(err)
	}
	decimal(t, result.Totals.CostMicroUSD, "700")
	if result.Coverage.UnpricedFacts != 0 {
		t.Fatal("per-delta cache decomposition rejected")
	}
	snap.SessionID = "huge"
	snap.Contributions = []reportingv1.Contribution{}
	for i := range 2 {
		c := reportingContribution(9223372036854775807, 2000+int64(i))
		c.CostStatus = "known"
		c.PricingMode = "codex_model_sum"
		c.PricingVersion = new("historical-huge")
		c.Model = new("huge-model")
		c.Rates = &reportingv1.Rates{InputMicroUSD: &rate, CachedMicroUSD: &rate, OutputMicroUSD: &rate}
		snap.Contributions = append(snap.Contributions, c)
	}
	fixStatisticsIDs(&snap)
	sendSnapshot(t, r, clients[0], snap)
	q.Model = "huge-model"
	result, err = stats.Summary(t.Context(), admin, q)
	if err != nil {
		t.Fatal(err)
	}
	decimal(t, result.Totals.TotalTokens, "18446744073709551614")
	decimal(t, result.Totals.CostMicroUSD, "18446744073709551614")
	unknown := reportingSnapshot()
	unknown.SessionID = "unknown"
	unknown.Contributions[0].ObservedAtMS = nil
	unknown.Contributions[0].TotalTokens = nil
	unknown.LastActiveAtMS = new(int64(1000))
	fixStatisticsIDs(&unknown)
	sendSnapshot(t, r, clients[0], unknown)
	q.Model = ""
	q.Search = "unknown"
	result, err = stats.Summary(t.Context(), admin, q)
	if err != nil {
		t.Fatal(err)
	}
	if result.Coverage.UntimedFacts != 1 || result.Coverage.State != "partial" {
		t.Fatal("unknown time disappeared")
	}
	decimal(t, result.Totals.TotalTokens, "0")
	// Known empty range is zero; a never-observed provider stays unknown.
	q.Provider = "grok"
	result, err = stats.Summary(t.Context(), admin, q)
	if err != nil {
		t.Fatal(err)
	}
	if result.Totals.TotalTokens != nil || result.Coverage.State != "unknown" {
		t.Fatal("never collected became zero")
	}
}
func TestStatisticsDSTNaturalDaysAndInputBounds(t *testing.T) {
	q := statisticsTestQuery(t, url.Values{"start_date": {"2026-03-08"}, "end_date_exclusive": {"2026-03-10"}, "time_zone": {"America/New_York"}})
	if q.EndAtMS-q.StartAtMS != int64(47*time.Hour/time.Millisecond) {
		t.Fatal("DST used 24-hour days")
	}
	o := &statisticsRead{q: q, providerSeen: map[string]bool{}, days: map[string]*statisticsAggregate{}}
	days := o.trend()
	if len(days) != 2 || days[1].StartAtMS-days[0].StartAtMS != int64(23*time.Hour/time.Millisecond) {
		t.Fatal("DST trend boundaries")
	}
	for _, v := range []url.Values{{"time_zone": {"Local"}}, {"start_at_ms": {"1"}}, {"start_at_ms": {"1"}, "end_at_ms": {"1"}}, {"start_at_ms": {"-1"}, "end_at_ms": {"100"}}, {"limit": {"101"}}, {"page": {"0"}}, {"provider": {"injected"}}, {"sort": {"tokens;drop table"}}, {"client_id": {"other"}}, {"project_id": {"wrong"}}, {"search": {strings.Repeat("a", 257)}}} {
		if _, err := ParseStatisticsQuery(v, time.Now()); !errors.Is(err, utils.ErrBadParamInput) {
			t.Fatal("bad query accepted", v)
		}
	}
}
func TestStatisticsCursorRangeRoundingChargeAndComposition(t *testing.T) {
	stats, r, admin, clients := statisticsFixture(t)
	snap := reportingSnapshot()
	snap.Provider = "cursor"
	snap.SourceKind = "cursor_dashboard"
	snap.Contributions = nil
	for i := range 2 {
		c := reportingv1.Contribution{ObservedAtMS: new(int64(1000 + i)), Model: new("model-" + string(rune('a'+i))), InputTokens: new(int64(1)), CachedTokens: new(int64(0)), CacheWriteTokens: new(int64(0)), OutputTokens: new(int64(0)), TotalTokens: new(int64(1)), ReportedChargeMicroUSD: new(int64(2)), PricingMode: "cursor_range_sum", PricingVersion: new("historical-cursor"), CostStatus: "known", Rates: &reportingv1.Rates{InputMicroUSD: new(int64(500000))}}
		snap.Contributions = append(snap.Contributions, c)
	}
	fixStatisticsIDs(&snap)
	sendSnapshot(t, r, clients[0], snap)
	result, err := stats.Summary(t.Context(), admin, statisticsTestQuery(t, nil))
	if err != nil {
		t.Fatal(err)
	}
	decimal(t, result.Totals.CostMicroUSD, "1")
	decimal(t, result.Totals.ReportedChargeMicroUSD, "4")
	decimal(t, result.Providers[0].Totals.CostMicroUSD, "1")
	if len(result.Models) != 3 || result.Models[2].Key != "rounding_adjustment" {
		t.Fatal("rounded components mis-reconcile")
	}
	decimal(t, result.Models[2].Totals.CostMicroUSD, "-1")
	devices, err := stats.Devices(t.Context(), admin)
	if err != nil || len(devices) != 3 {
		t.Fatal("device status missing", err)
	}
	if devices[0].Providers == nil {
		t.Fatal("empty status is not explicit")
	}
}

func TestStatisticsSessionCostRetainsNativeRangeRoundingAndToolWhitelist(t *testing.T) {
	stats, r, admin, clients := statisticsFixture(t)
	snap := reportingSnapshot()
	snap.Contributions = nil
	for _, at := range []int64{1000, 86401000} {
		c := reportingContribution(1, at)
		c.CostStatus = "known"
		c.PricingMode = "codex_model_sum"
		c.PricingVersion = new("half")
		c.Model = new("small")
		c.Rates = &reportingv1.Rates{InputMicroUSD: new(int64(500000))}
		snap.Contributions = append(snap.Contributions, c)
	}
	snap.Invocations = []reportingv1.Invocation{{ObservedAtMS: 1000, Kind: "tool", Name: "exec_command", Outcome: "succeeded"}, {ObservedAtMS: 1001, Kind: "skill", Name: "go-workflow", Outcome: "succeeded"}}
	for i, c := range snap.Invocations {
		snap.Invocations[i].ID = reportingv1.InvocationID("codex", "session", c, 0)
	}
	fixStatisticsIDs(&snap)
	sendSnapshot(t, r, clients[0], snap)
	q := statisticsTestQuery(t, url.Values{"start_at_ms": {"0"}, "end_at_ms": {"172800000"}})
	summary, err := stats.Summary(t.Context(), admin, q)
	if err != nil {
		t.Fatal(err)
	}
	decimal(t, summary.Totals.CostMicroUSD, "2")
	list, err := stats.Sessions(t.Context(), admin, q)
	if err != nil {
		t.Fatal(err)
	}
	decimal(t, list.Items[0].Totals.CostMicroUSD, "1")
	if !strings.HasPrefix(list.Items[0].Totals.CostBasis, "codex_model_version;") {
		t.Fatal("session uses day rounding")
	}
	detail, err := stats.Session(t.Context(), admin, q, list.Items[0].ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(detail.Tools) != 1 || detail.Tools[0].Totals.Invocations != 1 || len(detail.Skills) != 1 || detail.Skills[0].Totals.Invocations != 1 {
		t.Fatal("tool/skill counts lost")
	}
	q.Model = "small"
	filtered, err := stats.Summary(t.Context(), admin, q)
	if err != nil {
		t.Fatal(err)
	}
	if filtered.Totals.Invocations != 0 || len(filtered.Tools) != 0 {
		t.Fatal("model filter guessed invocation attribution")
	}
}
