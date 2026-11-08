package statistics_srv

import (
	"bytes"
	"context"
	"encoding/json/v2"
	"errors"
	"fmt"
	"net/url"
	"reflect"
	"sync"
	"testing"
	"time"

	"gorm.io/gorm"

	reporting_do "github.com/SisyphusSQ/codex-pulse/server/internal/models/do/mysql/reporting_do"
	"github.com/SisyphusSQ/codex-pulse/server/internal/repository/mysql/access_repo"
	"github.com/SisyphusSQ/codex-pulse/server/internal/repository/mysql/reporting_repo"
	"github.com/SisyphusSQ/codex-pulse/server/internal/repository/mysql/statistics_repo"
	"github.com/SisyphusSQ/codex-pulse/server/internal/service/access_srv"
	"github.com/SisyphusSQ/codex-pulse/server/internal/service/reporting_srv"
	centerfixture "github.com/SisyphusSQ/codex-pulse/server/internal/testsupport/center"
)

func TestPairedCacheCancellationAndConcurrentReaders(t *testing.T) {
	c := newStatisticsCache(t.Context(), time.Second)
	started, release := make(chan struct{}), make(chan struct{})
	pair := func(ctx context.Context) map[string]statisticsCacheResult {
		close(started)
		select {
		case <-release:
		case <-ctx.Done():
			t.Error("caller canceled shared refresh")
		}
		return map[string]statisticsCacheResult{"usage": {body: []byte("usage")}, "current": {body: []byte("current")}}
	}
	for _, kind := range []string{"usage", "current"} {
		c.entries[statisticsCacheKey{kind: kind}] = &statisticsCacheEntry{kind: kind, pair: pair, ready: make(chan struct{}), used: c.now()}
	}
	done := make(chan struct{})
	go func() { c.refresh(); close(done) }()
	<-started
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if _, _, err := c.get(ctx, statisticsCacheKey{kind: "usage"}, nil, pair); !errors.Is(err, context.Canceled) {
		t.Fatal("caller cancellation lost", err)
	}
	var wg sync.WaitGroup
	for i := range 12 {
		wg.Go(func() {
			kind := "usage"
			if i%2 == 0 {
				kind = "current"
			}
			body, status, err := c.get(t.Context(), statisticsCacheKey{kind: kind}, nil, pair)
			if err != nil || string(body) != kind || status.State != "ready" {
				t.Error("paired waiter lost", kind, err)
			}
		})
	}
	close(release)
	wg.Wait()
	<-done
}

func TestSharedOverviewMatchesIndependentReadsAndPageSources(t *testing.T) {
	engine := centerfixture.Engine(t)
	db := engine.DB(t.Context())
	access := access_srv.NewAccess(access_repo.NewAccess(engine))
	admin := centerfixture.Admin(t, access).Principal
	client := centerfixture.Collector(t, access, admin, "synthetic")
	reporting := reporting_srv.NewReporting(reporting_repo.NewReporting(engine))
	stats := NewStatistics(statistics_repo.NewStatistics(engine))
	stats.now = func() time.Time { return time.UnixMilli(10000) }
	for i := range 9 {
		snapshot := throughputSnapshot()
		snapshot.SessionID = fmt.Sprintf("session-%d", i)
		snapshot.Title = fmt.Sprintf("title-%d", i)
		if i == 0 {
			snapshot.Contributions[0].InputTokens = new(int64(9007199254740993))
			snapshot.Contributions[0].TotalTokens = new(int64(9007199254740993))
		}
		fixStatisticsIDs(&snapshot)
		centerfixture.SendSnapshot(t, reporting, client, snapshot)
	}
	queries := 0
	count := func(tx *gorm.DB) {
		if !tx.DryRun {
			queries++
		}
	}
	if err := db.Callback().Query().After("gorm:query").Register("count-overview", count); err != nil {
		t.Fatal(err)
	}
	if err := db.Callback().Row().After("gorm:row").Register("count-overview", count); err != nil {
		t.Fatal(err)
	}
	for _, values := range []url.Values{{}, {"client_id": {client.ID}}, {"search": {"title-1"}}, {"model": {"unknown"}}, {"provider": {"cursor"}}, {"time_zone": {"America/New_York"}}} {
		q := statisticsTestQuery(t, values)
		queries = 0
		usage, err := stats.usage(t.Context(), q)
		if err != nil {
			t.Fatal(err)
		}
		current, err := stats.current(t.Context(), q)
		if err != nil {
			t.Fatal(err)
		}
		oldQueries := queries
		queries = 0
		results := stats.overviewLoader(q)(t.Context())
		if queries >= oldQueries {
			t.Fatalf("shared query count %d >= %d", queries, oldQueries)
		}
		if results["usage"].err != nil || results["current"].err != nil {
			t.Fatal(results)
		}
		oldUsage, err := json.Marshal(usage)
		if err != nil {
			t.Fatal(err)
		}
		oldCurrent, err := json.Marshal(current)
		if err != nil {
			t.Fatal(err)
		}
		if !bytes.Equal(oldUsage, results["usage"].body) || !bytes.Equal(oldCurrent, results["current"].body) {
			t.Fatalf("wire values changed for %v", values)
		}

		q.Limit = 2
		q.Page = 2
		full, err := stats.read(t.Context(), q)
		if err != nil {
			t.Fatal(err)
		}
		want := full.sessionList()
		want.Coverage = full.coverage(stats.now())
		if _, err := stats.attachThroughput(t.Context(), q, want.Items, full.metadata, false); err != nil {
			t.Fatal(err)
		}
		got, err := stats.Sessions(t.Context(), admin, q)
		if err != nil || !reflect.DeepEqual(want, got) {
			t.Fatal("paged result changed", err)
		}
	}
	if err := db.Callback().Row().Before("gorm:row").Register("fail-display-sources", func(tx *gorm.DB) {
		if tx.Statement.TableExpr != nil && tx.Statement.TableExpr.SQL == "pulse_session_sources AS src" {
			tx.AddError(errors.New("synthetic source failure"))
		}
	}); err != nil {
		t.Fatal(err)
	}
	results := stats.overviewLoader(statisticsTestQuery(t, nil))(t.Context())
	if results["usage"].err != nil || results["current"].err == nil {
		t.Fatal("display source failure crossed into usage")
	}
}

func TestSharedOverviewModelBudgetKeepsCurrentAggregation(t *testing.T) {
	stats, _, _, _ := statisticsFixture(t)
	read, err := stats.readProjection(t.Context(), statisticsTestQuery(t, nil), true, false, "overview")
	if err != nil {
		t.Fatal(err)
	}
	read.metadata["session"] = reporting_do.Session{Provider: "codex"}
	for i := range 20000 {
		read.modelDays[fmt.Sprint(i)] = nil
	}
	read.usage(reporting_do.Usage{SessionKey: "session", Model: new("overflow"), ObservedAtMS: new(int64(1000)), TotalTokens: new(int64(123))})
	if !read.modelTrendExceeded || read.modelDays != nil || read.total.tokens.value.String() != "123" || read.sessions["session"].tokens.value.String() != "123" {
		t.Fatal("model budget stopped current card")
	}
}

func TestPairedCacheRetainsIndependentFailuresAndSingleCardLoads(t *testing.T) {
	c := newStatisticsCache(t.Context(), time.Second)
	now := time.Now()
	c.now = func() time.Time { return now }
	calls, singles := 0, 0
	pair := func(context.Context) map[string]statisticsCacheResult {
		calls++
		return map[string]statisticsCacheResult{"usage": {err: errors.New("synthetic budget")}, "current": {body: []byte("new-current")}}
	}
	for _, kind := range []string{"usage", "current"} {
		body := []byte("old-" + kind)
		c.entries[statisticsCacheKey{kind: kind}] = &statisticsCacheEntry{kind: kind, pair: pair, body: body, used: now, computed: now.Add(-time.Minute), load: func(context.Context) ([]byte, error) { singles++; return []byte("single"), nil }}
		c.bytes += len(body)
	}
	if !c.refresh() || calls != 1 || singles != 0 || c.refresh() {
		t.Fatal("pair schedule failed")
	}
	usage := c.entries[statisticsCacheKey{kind: "usage"}]
	current := c.entries[statisticsCacheKey{kind: "current"}]
	if string(usage.body) != "old-usage" || usage.err == nil || !usage.computed.Equal(now.Add(-time.Minute)) || string(current.body) != "new-current" || current.err != nil {
		t.Fatal("independent cache failure changed")
	}
	now = now.Add(time.Minute)
	current.attempted = now
	if !c.refresh() || singles != 1 || calls != 1 {
		t.Fatal("single due card unnecessarily recomputed peer")
	}
}
