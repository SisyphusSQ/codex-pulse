package apisubscriptions

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestServiceReportsDeepSeekBalanceChangeForNaturalPeriods(t *testing.T) {
	var requestCount atomic.Int64
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.URL.Path != "/user/balance" {
			http.NotFound(writer, request)
			return
		}
		requestNumber := requestCount.Add(1)
		total, granted, toppedUp := "50.00", "10.00", "40.00"
		if requestNumber == 2 {
			total, granted, toppedUp = "60.00", "10.00", "50.00"
		} else if requestNumber > 2 {
			total, granted, toppedUp = "51.60", "8.00", "43.60"
		}
		_, _ = fmt.Fprintf(writer, `{"is_available":true,"balance_infos":[{
			"currency":"CNY","total_balance":%q,"granted_balance":%q,"topped_up_balance":%q
		}]}`, total, granted, toppedUp)
	}))
	t.Cleanup(server.Close)

	credentials := mustMemoryAPIKeys(t, map[string][]byte{ServiceDeepSeek: []byte("deepseek-secret")})
	service := mustService(t, server.URL, credentials)
	monthStartObservation := time.Date(2026, time.August, 1, 12, 0, 0, 0, time.UTC).UnixMilli()
	currentObservation := time.Date(2026, time.August, 19, 12, 0, 0, 0, time.UTC).UnixMilli()
	if _, err := service.Current(t.Context(), monthStartObservation); err != nil {
		t.Fatalf("Current(month start) error = %v", err)
	}
	if _, err := service.Current(t.Context(), monthStartObservation+time.Hour.Milliseconds()); err != nil {
		t.Fatalf("Current(recharge) error = %v", err)
	}
	current, err := service.Current(t.Context(), currentObservation)
	if err != nil {
		t.Fatalf("Current(current) error = %v", err)
	}

	payload, err := json.Marshal(current.DeepSeek)
	if err != nil {
		t.Fatalf("Marshal(DeepSeek) error = %v", err)
	}
	var presentation struct {
		Periods []struct {
			Kind         string `json:"kind"`
			StartsAtMS   int64  `json:"startsAtMs"`
			BaselineAtMS int64  `json:"baselineAtMs"`
			Changes      []struct {
				Currency         string `json:"currency"`
				StartingTotal    string `json:"startingTotal"`
				TotalDelta       string `json:"totalDelta"`
				StartingGranted  string `json:"startingGranted"`
				GrantedDelta     string `json:"grantedDelta"`
				StartingToppedUp string `json:"startingToppedUp"`
				ToppedUpDelta    string `json:"toppedUpDelta"`
				TotalRecharged   string `json:"totalRecharged"`
				TotalConsumed    string `json:"totalConsumed"`
			} `json:"changes"`
		} `json:"periods"`
	}
	if err := json.Unmarshal(payload, &presentation); err != nil {
		t.Fatalf("Unmarshal(period presentation) error = %v", err)
	}
	var month *struct {
		Kind         string `json:"kind"`
		StartsAtMS   int64  `json:"startsAtMs"`
		BaselineAtMS int64  `json:"baselineAtMs"`
		Changes      []struct {
			Currency         string `json:"currency"`
			StartingTotal    string `json:"startingTotal"`
			TotalDelta       string `json:"totalDelta"`
			StartingGranted  string `json:"startingGranted"`
			GrantedDelta     string `json:"grantedDelta"`
			StartingToppedUp string `json:"startingToppedUp"`
			ToppedUpDelta    string `json:"toppedUpDelta"`
			TotalRecharged   string `json:"totalRecharged"`
			TotalConsumed    string `json:"totalConsumed"`
		} `json:"changes"`
	}
	for index := range presentation.Periods {
		if presentation.Periods[index].Kind == "month" {
			month = &presentation.Periods[index]
			break
		}
	}
	if month == nil {
		t.Fatalf("DeepSeek periods = %s, want month balance change", payload)
	}
	if month.BaselineAtMS != monthStartObservation || len(month.Changes) != 1 {
		t.Fatalf("month period = %#v", month)
	}
	change := month.Changes[0]
	if change.Currency != "CNY" || change.StartingTotal != "50.00" || change.TotalDelta != "1.60" ||
		change.StartingGranted != "10.00" || change.GrantedDelta != "-2.00" ||
		change.StartingToppedUp != "40.00" || change.ToppedUpDelta != "3.60" ||
		change.TotalRecharged != "10.00" || change.TotalConsumed != "8.40" {
		t.Fatalf("month change = %#v", change)
	}
}

func TestServiceBuildsOneCalendarWithDeepSeekAndOpenCodeGoFiveHourActivity(t *testing.T) {
	var requestCount atomic.Int64
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		writer.Header().Set("Content-Type", "application/json")
		switch request.URL.Path {
		case "/user/balance":
			number := requestCount.Add(1)
			toppedUp := "20.00"
			if number > 1 {
				toppedUp = "17.50"
			}
			_, _ = fmt.Fprintf(writer, `{"is_available":true,"balance_infos":[{
				"currency":"CNY","total_balance":%q,"granted_balance":"0.00","topped_up_balance":%q
			}]}`, toppedUp, toppedUp)
		case "/zen/go/v1/usage":
			_, _ = writer.Write([]byte(`{"usage":{
				"rolling":{"status":"ok","percent":62.5,"resetsAt":"2026-08-19T10:00:00Z"},
				"weekly":{"status":"ok","percent":30,"resetsAt":"2026-08-24T00:00:00Z"},
				"monthly":{"status":"ok","percent":20,"resetsAt":"2026-09-01T00:00:00Z"}
			}}`))
		default:
			http.NotFound(writer, request)
		}
	}))
	t.Cleanup(server.Close)

	credentials := mustMemoryAPIKeys(t, map[string][]byte{
		ServiceDeepSeek: []byte("deepseek-secret"), ServiceOpenCodeGo: []byte("opencode-secret"),
	})
	service := mustService(t, server.URL, credentials)
	firstAt := time.Date(2026, time.August, 19, 8, 0, 0, 0, time.UTC).UnixMilli()
	if _, err := service.Current(t.Context(), firstAt); err != nil {
		t.Fatalf("Current(first) error = %v", err)
	}
	current, err := service.Current(t.Context(), firstAt+time.Hour.Milliseconds())
	if err != nil {
		t.Fatalf("Current(second) error = %v", err)
	}
	payload, err := json.Marshal(current)
	if err != nil {
		t.Fatalf("json.Marshal(current) error = %v", err)
	}
	var decoded struct {
		ActivityCalendar struct {
			ReportingTimeZone string `json:"reportingTimeZone"`
			Days              []struct {
				DateKey  string `json:"dateKey"`
				DeepSeek []struct {
					Currency       string `json:"currency"`
					TotalConsumed  string `json:"totalConsumed"`
					TotalRecharged string `json:"totalRecharged"`
					SampleCount    int    `json:"sampleCount"`
				} `json:"deepSeek"`
				OpenCodeGo *struct {
					MaxFiveHourUsedPercent         float64 `json:"maxFiveHourUsedPercent"`
					LatestFiveHourUsedPercent      float64 `json:"latestFiveHourUsedPercent"`
					LatestFiveHourRemainingPercent float64 `json:"latestFiveHourRemainingPercent"`
					SampleCount                    int     `json:"sampleCount"`
				} `json:"openCodeGo"`
			} `json:"days"`
		} `json:"activityCalendar"`
	}
	if err := json.Unmarshal(payload, &decoded); err != nil {
		t.Fatalf("json.Unmarshal(current) error = %v", err)
	}
	if len(decoded.ActivityCalendar.Days) != 365 {
		t.Fatalf("activity days = %d, want 365; payload=%s", len(decoded.ActivityCalendar.Days), payload)
	}
	day := decoded.ActivityCalendar.Days[len(decoded.ActivityCalendar.Days)-1]
	if day.DateKey != "2026-08-19" || len(day.DeepSeek) != 1 ||
		day.DeepSeek[0].Currency != "CNY" || day.DeepSeek[0].TotalConsumed != "2.50" ||
		day.DeepSeek[0].TotalRecharged != "0.00" || day.DeepSeek[0].SampleCount != 2 ||
		day.OpenCodeGo == nil || day.OpenCodeGo.MaxFiveHourUsedPercent != 62.5 ||
		day.OpenCodeGo.LatestFiveHourUsedPercent != 62.5 ||
		day.OpenCodeGo.LatestFiveHourRemainingPercent != 37.5 || day.OpenCodeGo.SampleCount != 2 {
		t.Fatalf("latest activity day = %#v", day)
	}
}

func TestNaturalPeriodsUseLocalDayMondayAndMonthBoundaries(t *testing.T) {
	t.Parallel()
	location, err := time.LoadLocation("Asia/Shanghai")
	if err != nil {
		t.Fatalf("LoadLocation() error = %v", err)
	}
	observed := time.Date(2026, time.August, 19, 13, 45, 0, 0, location)
	periods := naturalPeriods(observed.UnixMilli(), location)
	want := map[string]int64{
		PeriodToday: time.Date(2026, time.August, 19, 0, 0, 0, 0, location).UnixMilli(),
		PeriodWeek:  time.Date(2026, time.August, 17, 0, 0, 0, 0, location).UnixMilli(),
		PeriodMonth: time.Date(2026, time.August, 1, 0, 0, 0, 0, location).UnixMilli(),
	}
	for _, period := range periods {
		if period.StartsAtMS != want[period.Kind] {
			t.Fatalf("period %q starts at %d, want %d", period.Kind, period.StartsAtMS, want[period.Kind])
		}
	}
}

func TestSubtractDecimalPreservesExactScaleForIncreaseAndZero(t *testing.T) {
	t.Parallel()
	for _, testCase := range []struct {
		name     string
		current  string
		starting string
		want     string
	}{
		{name: "increase", current: "50.5", starting: "43.60", want: "6.90"},
		{name: "zero at different input scales", current: "43.60", starting: "43.6", want: "0.00"},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			got, err := subtractDecimal(testCase.current, testCase.starting)
			if err != nil || got != testCase.want {
				t.Fatalf("subtractDecimal(%q, %q) = %q, %v, want %q", testCase.current, testCase.starting, got, err, testCase.want)
			}
		})
	}
}

func TestDeepSeekClientReturnsExactCurrencyBalances(t *testing.T) {
	t.Parallel()
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.Method != http.MethodGet || request.URL.Path != "/user/balance" {
			t.Errorf("request = %s %s, want GET /user/balance", request.Method, request.URL.Path)
		}
		if request.Header.Get("Authorization") != "Bearer deepseek-secret" {
			t.Errorf("authorization header was not supplied to the fixed endpoint")
		}
		writer.Header().Set("Content-Type", "application/json")
		_, _ = writer.Write([]byte(`{
			"is_available": true,
			"balance_infos": [{
				"currency": "CNY",
				"total_balance": "43.60",
				"granted_balance": "0.00",
				"topped_up_balance": "43.60"
			}]
		}`))
	}))
	t.Cleanup(server.Close)

	credentials := mustMemoryAPIKeys(t, map[string][]byte{ServiceDeepSeek: []byte("deepseek-secret")})
	client := mustDeepSeekClient(t, server.URL, credentials)
	balance, err := client.GetBalance(t.Context())
	if err != nil {
		t.Fatalf("GetBalance() error = %v", err)
	}
	if !balance.IsAvailable || len(balance.Balances) != 1 {
		t.Fatalf("GetBalance() = %#v", balance)
	}
	got := balance.Balances[0]
	if got.Currency != "CNY" || got.Total != "43.60" || got.Granted != "0.00" || got.ToppedUp != "43.60" {
		t.Fatalf("balance = %#v", got)
	}
}

func TestDeepSeekClientRejectsInvalidMoneyInsteadOfRounding(t *testing.T) {
	t.Parallel()
	server := jsonServer(t, "/user/balance", `{
		"is_available": true,
		"balance_infos": [{
			"currency": "CNY",
			"total_balance": "NaN",
			"granted_balance": "0.00",
			"topped_up_balance": "0.00"
		}]
	}`)
	credentials := mustMemoryAPIKeys(t, map[string][]byte{ServiceDeepSeek: []byte("deepseek-secret")})
	client := mustDeepSeekClient(t, server.URL, credentials)
	if _, err := client.GetBalance(t.Context()); !errors.Is(err, ErrProtocol) {
		t.Fatalf("GetBalance() error = %v, want ErrProtocol", err)
	}
}

func TestOpenCodeGoClientMapsAllQuotaWindows(t *testing.T) {
	t.Parallel()
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.Method != http.MethodGet || request.URL.Path != "/zen/go/v1/usage" {
			t.Errorf("request = %s %s, want GET /zen/go/v1/usage", request.Method, request.URL.Path)
		}
		if request.Header.Get("Authorization") != "Bearer opencode-secret" {
			t.Errorf("authorization header was not supplied to the fixed endpoint")
		}
		writer.Header().Set("Content-Type", "application/json")
		_, _ = writer.Write([]byte(`{"usage":{
			"rolling":{"status":"ok","percent":25,"resetsAt":"2026-08-19T10:00:00Z"},
			"weekly":{"status":"ok","percent":40.5,"resetsAt":"2026-08-24T00:00:00Z"},
			"monthly":{"status":"rate-limited","percent":100,"resetsAt":"2026-09-01T00:00:00Z"}
		}}`))
	}))
	t.Cleanup(server.Close)

	credentials := mustMemoryAPIKeys(t, map[string][]byte{ServiceOpenCodeGo: []byte("opencode-secret")})
	client := mustOpenCodeGoClient(t, server.URL, credentials)
	quota, err := client.GetQuota(t.Context())
	if err != nil {
		t.Fatalf("GetQuota() error = %v", err)
	}
	if len(quota.Windows) != 3 {
		t.Fatalf("GetQuota() windows = %#v", quota.Windows)
	}
	wants := []struct {
		kind      string
		status    string
		used      float64
		remaining float64
		resetMS   int64
	}{
		{WindowFiveHour, StatusOK, 25, 75, 1_787_133_600_000},
		{WindowWeekly, StatusOK, 40.5, 59.5, 1_787_529_600_000},
		{WindowMonthly, StatusRateLimited, 100, 0, 1_788_220_800_000},
	}
	for index, want := range wants {
		got := quota.Windows[index]
		if got.Kind != want.kind || got.Status != want.status || got.UsedPercent != want.used ||
			got.RemainingPercent != want.remaining || got.ResetsAtMS != want.resetMS {
			t.Errorf("window[%d] = %#v, want %#v", index, got, want)
		}
	}
}

func TestOpenCodeGoClientRejectsIncompleteQuota(t *testing.T) {
	t.Parallel()
	server := jsonServer(t, "/zen/go/v1/usage", `{"usage":{
		"rolling":{"status":"ok","percent":101,"resetsAt":"2026-08-19T10:00:00Z"},
		"weekly":{"status":"ok","percent":40,"resetsAt":"2026-08-24T00:00:00Z"}
	}}`)
	credentials := mustMemoryAPIKeys(t, map[string][]byte{ServiceOpenCodeGo: []byte("opencode-secret")})
	client := mustOpenCodeGoClient(t, server.URL, credentials)
	if _, err := client.GetQuota(t.Context()); !errors.Is(err, ErrProtocol) {
		t.Fatalf("GetQuota() error = %v, want ErrProtocol", err)
	}
}

func TestServiceKeepsEachSourceIndependentAndReturnsLastGood(t *testing.T) {
	var mu sync.RWMutex
	openCodeStatus := http.StatusOK
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		writer.Header().Set("Content-Type", "application/json")
		switch request.URL.Path {
		case "/user/balance":
			_, _ = writer.Write([]byte(`{"is_available":true,"balance_infos":[{
				"currency":"CNY","total_balance":"12.34","granted_balance":"2.34","topped_up_balance":"10.00"
			}]}`))
		case "/zen/go/v1/usage":
			mu.RLock()
			status := openCodeStatus
			mu.RUnlock()
			writer.WriteHeader(status)
			if status == http.StatusOK {
				_, _ = writer.Write([]byte(`{"usage":{
					"rolling":{"status":"ok","percent":10,"resetsAt":"2026-08-19T10:00:00Z"},
					"weekly":{"status":"ok","percent":20,"resetsAt":"2026-08-24T00:00:00Z"},
					"monthly":{"status":"ok","percent":30,"resetsAt":"2026-09-01T00:00:00Z"}
				}}`))
			}
		default:
			http.NotFound(writer, request)
		}
	}))
	t.Cleanup(server.Close)

	credentials := mustMemoryAPIKeys(t, map[string][]byte{
		ServiceDeepSeek:   []byte("deepseek-secret"),
		ServiceOpenCodeGo: []byte("opencode-secret"),
	})
	service := mustService(t, server.URL, credentials)
	first, err := service.Current(t.Context(), 1_755_579_600_000)
	if err != nil {
		t.Fatalf("Current(first) error = %v", err)
	}
	if first.DeepSeek.Status.State != StateCurrent || first.OpenCodeGo.Status.State != StateCurrent {
		t.Fatalf("Current(first) states = %q, %q", first.DeepSeek.Status.State, first.OpenCodeGo.Status.State)
	}

	mu.Lock()
	openCodeStatus = http.StatusInternalServerError
	mu.Unlock()
	second, err := service.Current(t.Context(), 1_755_579_660_000)
	if err != nil {
		t.Fatalf("Current(second) error = %v", err)
	}
	if second.DeepSeek.Status.State != StateCurrent || second.DeepSeek.Balance == nil {
		t.Fatalf("DeepSeek after OpenCode failure = %#v", second.DeepSeek)
	}
	if second.OpenCodeGo.Status.State != StateStale || second.OpenCodeGo.Quota == nil ||
		second.OpenCodeGo.Status.FailureCode == nil || *second.OpenCodeGo.Status.FailureCode != FailureServer {
		t.Fatalf("OpenCode after failure = %#v", second.OpenCodeGo)
	}
}

func TestServiceDoesNotRegressLastGoodWhenAnOlderRefreshFinishesLater(t *testing.T) {
	var mu sync.RWMutex
	deepSeekStatus := http.StatusOK
	totalBalance := "20.00"
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.URL.Path != "/user/balance" {
			http.NotFound(writer, request)
			return
		}
		mu.RLock()
		status := deepSeekStatus
		total := totalBalance
		mu.RUnlock()
		writer.WriteHeader(status)
		if status == http.StatusOK {
			_, _ = fmt.Fprintf(writer, `{"is_available":true,"balance_infos":[{
				"currency":"CNY","total_balance":%q,"granted_balance":"0.00","topped_up_balance":%q
			}]}`, total, total)
		}
	}))
	t.Cleanup(server.Close)

	credentials := mustMemoryAPIKeys(t, map[string][]byte{ServiceDeepSeek: []byte("deepseek-secret")})
	service := mustService(t, server.URL, credentials)
	const newerAtMS int64 = 1_755_579_600_200
	const olderAtMS int64 = 1_755_579_600_100
	if _, err := service.Current(t.Context(), newerAtMS); err != nil {
		t.Fatalf("Current(newer) error = %v", err)
	}
	mu.Lock()
	totalBalance = "10.00"
	mu.Unlock()
	if _, err := service.Current(t.Context(), olderAtMS); err != nil {
		t.Fatalf("Current(older) error = %v", err)
	}
	mu.Lock()
	deepSeekStatus = http.StatusInternalServerError
	mu.Unlock()
	current, err := service.Current(t.Context(), 1_755_579_600_300)
	if err != nil {
		t.Fatalf("Current(failed) error = %v", err)
	}
	if current.DeepSeek.Status.LastSuccessAtMS == nil || *current.DeepSeek.Status.LastSuccessAtMS != newerAtMS ||
		current.DeepSeek.Balance == nil || current.DeepSeek.Balance.Balances[0].Total != "20.00" {
		t.Fatalf("stale DeepSeek snapshot regressed = %#v", current.DeepSeek)
	}
}

func TestServiceRestoresPersistedDeepSeekBalanceAfterRestart(t *testing.T) {
	t.Parallel()
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.URL.Path == "/user/balance" {
			http.Error(writer, "unavailable", http.StatusInternalServerError)
			return
		}
		http.NotFound(writer, request)
	}))
	t.Cleanup(server.Close)

	credentials := mustMemoryAPIKeys(t, map[string][]byte{ServiceDeepSeek: []byte("deepseek-secret")})
	history := newMemoryBalanceHistory()
	observedAtMS := time.Date(2026, time.August, 1, 12, 0, 0, 0, time.UTC).UnixMilli()
	if err := history.Record(t.Context(), testBalanceObservation("legacy-deepseek", observedAtMS, "50.00")); err != nil {
		t.Fatalf("Record() error = %v", err)
	}
	service, err := NewService(ServiceConfig{
		DeepSeek:   mustDeepSeekClient(t, server.URL, credentials),
		OpenCodeGo: mustOpenCodeGoClient(t, server.URL, credentials),
		History:    history,
		Location:   time.UTC,
	})
	if err != nil {
		t.Fatalf("NewService() error = %v", err)
	}

	evaluatedAtMS := time.Date(2026, time.September, 1, 1, 0, 0, 0, time.UTC).UnixMilli()
	current, err := service.Current(t.Context(), evaluatedAtMS)
	if err != nil {
		t.Fatalf("Current() error = %v", err)
	}
	if current.DeepSeek.Status.State != StateStale ||
		current.DeepSeek.Status.LastSuccessAtMS == nil || *current.DeepSeek.Status.LastSuccessAtMS != observedAtMS ||
		current.DeepSeek.Balance == nil || current.DeepSeek.Balance.Balances[0].Total != "50.00" ||
		len(current.DeepSeek.Periods) != 3 {
		t.Fatalf("restored DeepSeek snapshot = %#v", current.DeepSeek)
	}
	month := periodByKind(t, current.DeepSeek.Periods, PeriodMonth)
	if month.BaselineAtMS != nil || len(month.Changes) != 0 {
		t.Fatalf("current-month period reused prior-month balance = %#v", month)
	}
}

func TestServiceReturnsLocalHistoryFailure(t *testing.T) {
	t.Parallel()
	server := jsonServer(t, "/user/balance", `{"is_available":true,"balance_infos":[{
		"currency":"CNY","total_balance":"50.00","granted_balance":"0.00","topped_up_balance":"50.00"
	}]}`)
	credentials := mustMemoryAPIKeys(t, map[string][]byte{ServiceDeepSeek: []byte("deepseek-secret")})
	service, err := NewService(ServiceConfig{
		DeepSeek:   mustDeepSeekClient(t, server.URL, credentials),
		OpenCodeGo: mustOpenCodeGoClient(t, server.URL, credentials),
		History:    failingBalanceHistory{err: errors.New("disk failed")},
	})
	if err != nil {
		t.Fatalf("NewService() error = %v", err)
	}
	if _, err := service.Current(t.Context(), 1_755_579_600_000); err == nil || !strings.Contains(err.Error(), "disk failed") {
		t.Fatalf("Current() error = %v, want local history failure", err)
	}
}

func TestServiceDoesNotPersistConfiguredKeyWithoutCredentialEpoch(t *testing.T) {
	t.Parallel()
	server := jsonServer(t, "/user/balance", `{"is_available":true,"balance_infos":[{
		"currency":"CNY","total_balance":"50.00","granted_balance":"0.00","topped_up_balance":"50.00"
	}]}`)
	credentials := keyOnlyProvider{ServiceDeepSeek: []byte("deepseek-secret")}
	service, err := NewService(ServiceConfig{
		DeepSeek:   mustDeepSeekClient(t, server.URL, credentials),
		OpenCodeGo: mustOpenCodeGoClient(t, server.URL, credentials),
	})
	if err != nil {
		t.Fatalf("NewService() error = %v", err)
	}
	if _, err := service.Current(t.Context(), 1_755_579_600_000); err == nil ||
		!strings.Contains(err.Error(), "credential epoch") {
		t.Fatalf("Current() error = %v, want missing credential epoch", err)
	}
}

type keyOnlyProvider map[string][]byte

func (provider keyOnlyProvider) APIKey(service string) ([]byte, bool) {
	key, ok := provider[service]
	return append([]byte(nil), key...), ok
}

type failingBalanceHistory struct{ err error }

func (history failingBalanceHistory) Record(context.Context, BalanceObservation) error {
	return history.err
}

func (history failingBalanceHistory) Observations(context.Context, string, int64, int64) ([]BalanceObservation, error) {
	return nil, history.err
}

func TestServiceDoesNotCallNetworkWithoutConfiguredKeys(t *testing.T) {
	t.Parallel()
	var requests atomic.Int64
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		requests.Add(1)
		http.Error(writer, "unexpected", http.StatusInternalServerError)
	}))
	t.Cleanup(server.Close)
	credentials := mustMemoryAPIKeys(t, nil)
	service := mustService(t, server.URL, credentials)
	current, err := service.Current(t.Context(), 1_755_579_600_000)
	if err != nil {
		t.Fatalf("Current() error = %v", err)
	}
	if requests.Load() != 0 {
		t.Fatalf("network requests = %d, want 0", requests.Load())
	}
	if current.DeepSeek.Status.State != StateUnconfigured || current.OpenCodeGo.Status.State != StateUnconfigured {
		t.Fatalf("states = %q, %q", current.DeepSeek.Status.State, current.OpenCodeGo.Status.State)
	}
}

func TestServiceRefreshesConfiguredSourceWhenOtherSourceIsUnconfigured(t *testing.T) {
	t.Parallel()
	var requests atomic.Int64
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		requests.Add(1)
		if request.URL.Path != "/user/balance" {
			http.NotFound(writer, request)
			return
		}
		_, _ = writer.Write([]byte(`{"is_available":true,"balance_infos":[{
			"currency":"CNY","total_balance":"1.00","granted_balance":"0.00","topped_up_balance":"1.00"
		}]}`))
	}))
	t.Cleanup(server.Close)
	credentials := mustMemoryAPIKeys(t, map[string][]byte{ServiceDeepSeek: []byte("deepseek-secret")})
	service := mustService(t, server.URL, credentials)
	current, err := service.Current(t.Context(), 1_755_579_600_000)
	if err != nil {
		t.Fatalf("Current() error = %v", err)
	}
	if requests.Load() != 1 || current.DeepSeek.Status.State != StateCurrent ||
		current.OpenCodeGo.Status.State != StateUnconfigured {
		t.Fatalf("requests = %d, snapshot = %#v", requests.Load(), current)
	}
}

func TestClientDoesNotFollowCredentialedRedirect(t *testing.T) {
	t.Parallel()
	var sinkRequests atomic.Int64
	sink := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		sinkRequests.Add(1)
	}))
	t.Cleanup(sink.Close)
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		http.Redirect(writer, request, sink.URL, http.StatusTemporaryRedirect)
	}))
	t.Cleanup(server.Close)
	credentials := mustMemoryAPIKeys(t, map[string][]byte{ServiceDeepSeek: []byte("deepseek-secret")})
	client := mustDeepSeekClient(t, server.URL, credentials)
	if _, err := client.GetBalance(t.Context()); !errors.Is(err, ErrProtocol) {
		t.Fatalf("GetBalance() error = %v, want ErrProtocol", err)
	}
	if sinkRequests.Load() != 0 {
		t.Fatalf("redirect sink requests = %d, want 0", sinkRequests.Load())
	}
}

func TestClientClassifiesRemoteFailures(t *testing.T) {
	t.Parallel()
	for _, testCase := range []struct {
		name   string
		status int
		want   error
	}{
		{"authentication", http.StatusUnauthorized, ErrAuth},
		{"subscription", http.StatusForbidden, ErrForbidden},
		{"rate limit", http.StatusTooManyRequests, ErrRateLimit},
		{"server", http.StatusBadGateway, ErrServer},
		{"unexpected", http.StatusTeapot, ErrProtocol},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()
			server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
				writer.WriteHeader(testCase.status)
			}))
			t.Cleanup(server.Close)
			credentials := mustMemoryAPIKeys(t, map[string][]byte{ServiceDeepSeek: []byte("deepseek-secret")})
			client := mustDeepSeekClient(t, server.URL, credentials)
			if _, err := client.GetBalance(t.Context()); !errors.Is(err, testCase.want) {
				t.Fatalf("GetBalance() error = %v, want %v", err, testCase.want)
			}
		})
	}
}

func TestMemoryAPIKeysReturnCopiesAndClearOnClose(t *testing.T) {
	t.Parallel()
	credentials, err := NewMemoryAPIKeys(map[string][]byte{ServiceDeepSeek: []byte("secret")})
	if err != nil {
		t.Fatal(err)
	}
	copyOfKey, ok := credentials.APIKey(ServiceDeepSeek)
	if !ok {
		t.Fatal("APIKey() did not return configured key")
	}
	clear(copyOfKey)
	secondCopy, ok := credentials.APIKey(ServiceDeepSeek)
	if !ok || string(secondCopy) != "secret" {
		t.Fatalf("APIKey() did not isolate caller mutation")
	}
	clear(secondCopy)
	credentials.Close()
	if _, ok := credentials.APIKey(ServiceDeepSeek); ok || len(credentials.keys) != 0 {
		t.Fatal("Close() retained a reusable key")
	}
}

func mustMemoryAPIKeys(t testing.TB, keys map[string][]byte) *MemoryAPIKeys {
	t.Helper()
	credentials, err := NewMemoryAPIKeys(keys)
	if err != nil {
		t.Fatalf("NewMemoryAPIKeys() error = %v", err)
	}
	t.Cleanup(credentials.Close)
	return credentials
}

func mustDeepSeekClient(t testing.TB, baseURL string, credentials APIKeyProvider) *DeepSeekClient {
	t.Helper()
	client, err := NewDeepSeekClient(ClientConfig{
		BaseURL: baseURL, Transport: http.DefaultTransport, Credentials: credentials,
		Timeout: time.Second,
	})
	if err != nil {
		t.Fatalf("NewDeepSeekClient() error = %v", err)
	}
	return client
}

func mustOpenCodeGoClient(t testing.TB, baseURL string, credentials APIKeyProvider) *OpenCodeGoClient {
	t.Helper()
	client, err := NewOpenCodeGoClient(ClientConfig{
		BaseURL: baseURL, Transport: http.DefaultTransport, Credentials: credentials,
		Timeout: time.Second,
	})
	if err != nil {
		t.Fatalf("NewOpenCodeGoClient() error = %v", err)
	}
	return client
}

func mustService(t testing.TB, baseURL string, credentials APIKeyProvider) *Service {
	t.Helper()
	service, err := NewService(ServiceConfig{
		DeepSeek:   mustDeepSeekClient(t, baseURL, credentials),
		OpenCodeGo: mustOpenCodeGoClient(t, baseURL, credentials),
	})
	if err != nil {
		t.Fatalf("NewService() error = %v", err)
	}
	return service
}

func jsonServer(t testing.TB, path string, payload string) *httptest.Server {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.URL.Path != path {
			http.NotFound(writer, request)
			return
		}
		writer.Header().Set("Content-Type", "application/json")
		_, _ = fmt.Fprint(writer, payload)
	}))
	t.Cleanup(server.Close)
	return server
}
