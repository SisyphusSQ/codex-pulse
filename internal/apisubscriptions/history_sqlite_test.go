package apisubscriptions

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/SisyphusSQ/codex-pulse/internal/store"
	storesqlite "github.com/SisyphusSQ/codex-pulse/internal/store/sqlite"
)

func TestSQLiteBalanceHistorySurvivesHelperRestart(t *testing.T) {
	t.Parallel()

	databasePath := filepath.Join(t.TempDir(), "codex-pulse.db")
	firstDatabase := openAPISubscriptionHistoryDatabase(t, databasePath)
	firstHistory, err := NewSQLiteBalanceHistory(firstDatabase)
	if err != nil {
		t.Fatalf("NewSQLiteBalanceHistory(first) error = %v", err)
	}
	monthStart := time.Date(2026, time.August, 1, 12, 0, 0, 0, time.UTC).UnixMilli()
	if err := firstHistory.Record(t.Context(), testBalanceObservation("credential-one", monthStart, "50.00")); err != nil {
		t.Fatalf("Record(month start) error = %v", err)
	}
	if err := firstDatabase.Close(context.Background()); err != nil {
		t.Fatalf("Close(first database) error = %v", err)
	}

	secondDatabase := openAPISubscriptionHistoryDatabase(t, databasePath)
	defer func() { _ = secondDatabase.Close(context.Background()) }()
	secondHistory, err := NewSQLiteBalanceHistory(secondDatabase)
	if err != nil {
		t.Fatalf("NewSQLiteBalanceHistory(second) error = %v", err)
	}
	latest, found, err := secondHistory.Latest(t.Context(), "credential-one", monthStart+time.Hour.Milliseconds())
	if err != nil || !found || latest.ObservedAtMS != monthStart || latest.Balance.Balances[0].Total != "50.00" {
		t.Fatalf("Latest(after restart) = %#v, %t, %v", latest, found, err)
	}
	currentAt := time.Date(2026, time.August, 19, 12, 0, 0, 0, time.UTC).UnixMilli()
	current := testBalanceObservation("credential-one", currentAt, "43.60")
	if err := secondHistory.Record(t.Context(), current); err != nil {
		t.Fatalf("Record(current) error = %v", err)
	}
	periods, err := balancePeriods(t.Context(), secondHistory, "credential-one", current, time.UTC)
	if err != nil {
		t.Fatalf("balancePeriods() error = %v", err)
	}
	month := periodByKind(t, periods, PeriodMonth)
	if month.BaselineAtMS == nil || *month.BaselineAtMS != monthStart || len(month.Changes) != 1 || month.Changes[0].TotalDelta != "-6.40" {
		t.Fatalf("month after restart = %#v", month)
	}
}

func TestSQLiteBalanceHistoryDoesNotCrossCredentialEpoch(t *testing.T) {
	t.Parallel()

	database := openAPISubscriptionHistoryDatabase(t, filepath.Join(t.TempDir(), "codex-pulse.db"))
	defer func() { _ = database.Close(context.Background()) }()
	history, err := NewSQLiteBalanceHistory(database)
	if err != nil {
		t.Fatalf("NewSQLiteBalanceHistory() error = %v", err)
	}
	monthStart := time.Date(2026, time.August, 1, 12, 0, 0, 0, time.UTC).UnixMilli()
	if err := history.Record(t.Context(), testBalanceObservation("credential-one", monthStart, "50.00")); err != nil {
		t.Fatalf("Record(old credential) error = %v", err)
	}
	currentAt := time.Date(2026, time.August, 19, 12, 0, 0, 0, time.UTC).UnixMilli()
	current := testBalanceObservation("credential-two", currentAt, "43.60")
	if err := history.Record(t.Context(), current); err != nil {
		t.Fatalf("Record(new credential) error = %v", err)
	}
	periods, err := balancePeriods(t.Context(), history, "credential-two", current, time.UTC)
	if err != nil {
		t.Fatalf("balancePeriods() error = %v", err)
	}
	month := periodByKind(t, periods, PeriodMonth)
	if month.BaselineAtMS == nil || *month.BaselineAtMS != currentAt || len(month.Changes) != 1 || month.Changes[0].TotalDelta != "0.00" {
		t.Fatalf("new credential month = %#v", month)
	}
}

func TestSQLiteBalanceHistoryBuildsLosslessCompressedPeriodSeries(t *testing.T) {
	t.Parallel()

	database := openAPISubscriptionHistoryDatabase(t, filepath.Join(t.TempDir(), "codex-pulse.db"))
	defer func() { _ = database.Close(context.Background()) }()
	history, err := NewSQLiteBalanceHistory(database)
	if err != nil {
		t.Fatalf("NewSQLiteBalanceHistory() error = %v", err)
	}
	monthStart := time.Date(2026, time.August, 1, 0, 15, 0, 0, time.UTC).UnixMilli()
	unchangedAt := monthStart + 15*time.Minute.Milliseconds()
	changedAt := monthStart + 60*time.Minute.Milliseconds()
	currentAt := monthStart + 75*time.Minute.Milliseconds()
	for _, observation := range []BalanceObservation{
		testBalanceObservation("credential-one", monthStart, "50.00"),
		testBalanceObservation("credential-one", unchangedAt, "50.00"),
		testBalanceObservation("credential-one", changedAt, "43.60"),
		testBalanceObservation("credential-one", currentAt, "43.60"),
		testBalanceObservation("credential-two", monthStart, "999.00"),
	} {
		if err := history.Record(t.Context(), observation); err != nil {
			t.Fatalf("Record(%d) error = %v", observation.ObservedAtMS, err)
		}
	}
	current := testBalanceObservation("credential-one", currentAt, "43.60")
	periods, err := balancePeriods(t.Context(), history, "credential-one", current, time.UTC)
	if err != nil {
		t.Fatalf("balancePeriods() error = %v", err)
	}
	month := periodByKind(t, periods, PeriodMonth)
	payload, err := json.Marshal(month)
	if err != nil {
		t.Fatalf("json.Marshal(month) error = %v", err)
	}
	var decoded struct {
		Series []struct {
			Currency string `json:"currency"`
			Points   []struct {
				ObservedAtMS int64  `json:"observedAtMs"`
				Total        string `json:"total"`
				Granted      string `json:"granted"`
				ToppedUp     string `json:"toppedUp"`
			} `json:"points"`
		} `json:"series"`
	}
	if err := json.Unmarshal(payload, &decoded); err != nil {
		t.Fatalf("json.Unmarshal(month) error = %v", err)
	}
	if len(decoded.Series) != 1 || decoded.Series[0].Currency != "CNY" {
		t.Fatalf("month series = %#v", decoded.Series)
	}
	points := decoded.Series[0].Points
	if len(points) != 3 ||
		points[0].ObservedAtMS != monthStart || points[0].Total != "50.00" ||
		points[1].ObservedAtMS != changedAt || points[1].Total != "43.60" ||
		points[2].ObservedAtMS != currentAt || points[2].Total != "43.60" ||
		points[0].Granted != "0.00" || points[2].ToppedUp != "43.60" {
		t.Fatalf("month points = %#v", points)
	}
}

func TestSQLiteQuotaHistorySurvivesHelperRestartAndKeepsCredentialEpoch(t *testing.T) {
	t.Parallel()

	databasePath := filepath.Join(t.TempDir(), "codex-pulse.db")
	firstDatabase := openAPISubscriptionHistoryDatabase(t, databasePath)
	firstHistory, err := NewSQLiteBalanceHistory(firstDatabase)
	if err != nil {
		t.Fatalf("NewSQLiteBalanceHistory(first) error = %v", err)
	}
	observedAtMS := time.Date(2026, time.August, 19, 8, 0, 0, 0, time.UTC).UnixMilli()
	observation := QuotaObservation{
		CredentialEpoch: "credential-one", ObservedAtMS: observedAtMS,
		Quota: Quota{Windows: []QuotaWindow{{
			Kind: WindowFiveHour, Status: StatusOK, UsedPercent: 62.5,
			RemainingPercent: 37.5, ResetsAtMS: observedAtMS + time.Hour.Milliseconds(),
		}}},
	}
	if err := firstHistory.RecordQuota(t.Context(), observation); err != nil {
		t.Fatalf("RecordQuota() error = %v", err)
	}
	if err := firstDatabase.Close(context.Background()); err != nil {
		t.Fatalf("Close(first database) error = %v", err)
	}

	secondDatabase := openAPISubscriptionHistoryDatabase(t, databasePath)
	defer func() { _ = secondDatabase.Close(context.Background()) }()
	secondHistory, err := NewSQLiteBalanceHistory(secondDatabase)
	if err != nil {
		t.Fatalf("NewSQLiteBalanceHistory(second) error = %v", err)
	}
	rows, err := secondHistory.QuotaObservations(
		t.Context(), "credential-one", observedAtMS-time.Minute.Milliseconds(), observedAtMS+time.Minute.Milliseconds(),
	)
	if err != nil || len(rows) != 1 || len(rows[0].Quota.Windows) != 1 ||
		rows[0].Quota.Windows[0].UsedPercent != 62.5 {
		t.Fatalf("QuotaObservations(after restart) = %#v, %v", rows, err)
	}
	otherRows, err := secondHistory.QuotaObservations(
		t.Context(), "credential-two", observedAtMS-time.Minute.Milliseconds(), observedAtMS+time.Minute.Milliseconds(),
	)
	if err != nil || len(otherRows) != 0 {
		t.Fatalf("QuotaObservations(other credential) = %#v, %v", otherRows, err)
	}
}

func openAPISubscriptionHistoryDatabase(t testing.TB, path string) *storesqlite.Store {
	t.Helper()
	if err := os.Chmod(filepath.Dir(path), 0o700); err != nil {
		t.Fatalf("chmod SQLite directory: %v", err)
	}
	database, err := storesqlite.Open(context.Background(), storesqlite.Config{Path: path})
	if err != nil {
		t.Fatalf("sqlite.Open() error = %v", err)
	}
	if _, err := store.NewRepository(database).MigrateApplicationSchema(context.Background()); err != nil {
		_ = database.Close(context.Background())
		t.Fatalf("MigrateApplicationSchema() error = %v", err)
	}
	return database
}

func testBalanceObservation(epoch string, observedAtMS int64, total string) BalanceObservation {
	return BalanceObservation{
		CredentialEpoch: epoch,
		ObservedAtMS:    observedAtMS,
		Balance: Balance{IsAvailable: true, Balances: []CurrencyBalance{{
			Currency: "CNY", Total: total, Granted: "0.00", ToppedUp: total,
		}}},
	}
}

func periodByKind(t testing.TB, periods []BalancePeriod, kind string) BalancePeriod {
	t.Helper()
	for _, period := range periods {
		if period.Kind == kind {
			return period
		}
	}
	t.Fatalf("period %q is missing from %#v", kind, periods)
	return BalancePeriod{}
}
