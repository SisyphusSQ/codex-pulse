package statistics_srv

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	statistics_dto "github.com/SisyphusSQ/codex-pulse/server/internal/models/dto/statistics_dto"
	statistics_vo "github.com/SisyphusSQ/codex-pulse/server/internal/models/vo/statistics_vo"
	"github.com/SisyphusSQ/codex-pulse/server/utils"
)

func TestStatisticsCacheCoalescesAndCallerCancellationDoesNotCancelRefresh(t *testing.T) {
	c := newStatisticsCache(t.Context(), time.Second)
	key := statisticsCacheKey{kind: "summary"}
	started, release := make(chan struct{}), make(chan struct{})
	var calls atomic.Int64
	load := func(ctx context.Context) ([]byte, error) {
		calls.Add(1)
		close(started)
		select {
		case <-release:
			return []byte(`{"value":1}`), nil
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}
	ctx, cancel := context.WithCancel(t.Context())
	canceled := make(chan error, 1)
	go func() { _, _, err := c.get(ctx, key, load); canceled <- err }()
	<-c.wake
	done := make(chan struct{})
	go func() { c.refresh(); close(done) }()
	<-started
	cancel()
	if err := <-canceled; !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	var wg sync.WaitGroup
	for range 12 {
		wg.Go(func() {
			body, status, err := c.get(t.Context(), key, load)
			if err != nil || string(body) != `{"value":1}` || status == nil {
				t.Errorf("shared load failed: %v", err)
			}
		})
	}
	close(release)
	wg.Wait()
	<-done
	if calls.Load() != 1 {
		t.Fatal("duplicate cold SQL")
	}
	_, _, err := c.get(t.Context(), key, load)
	if err != nil || calls.Load() != 1 {
		t.Fatal("cache hit ran SQL", err)
	}
}

func TestStatisticsCacheFailurePreservesSnapshotAndRetriesOnSchedule(t *testing.T) {
	c := newStatisticsCache(t.Context(), time.Second)
	now := time.Date(2026, 10, 4, 1, 0, 0, 0, time.UTC)
	c.now = func() time.Time { return now }
	key := statisticsCacheKey{kind: "usage"}
	want := errors.New("synthetic database failure")
	c.entries[key] = &statisticsCacheEntry{body: []byte(`{"value":1}`), computed: now, attempted: now, used: now, load: func(context.Context) ([]byte, error) { return nil, want }}
	c.bytes = len(c.entries[key].body)
	now = now.Add(time.Minute)
	if !c.refresh() {
		t.Fatal("background refresh did not become due")
	}
	body, status, err := c.get(t.Context(), key, nil)
	if err != nil || string(body) != `{"value":1}` || status.State != "refresh_failed" || status.Stale {
		t.Fatal("failure replaced last success", status, err)
	}
	if c.refresh() {
		t.Fatal("failed SQL immediately retried")
	}
	now = now.Add(2 * time.Minute)
	_, status, err = c.get(t.Context(), key, nil)
	if err != nil || !status.Stale || status.ComputedAtMS != time.Date(2026, 10, 4, 1, 0, 0, 0, time.UTC).UnixMilli() {
		t.Fatal("old result appeared fresh", status, err)
	}
	c.entries[key].load = func(context.Context) ([]byte, error) { return []byte(`{"value":2}`), nil }
	if !c.refresh() {
		t.Fatal("retry not scheduled")
	}
	body, status, err = c.get(t.Context(), key, nil)
	if err != nil || string(body) != `{"value":2}` || status.Stale || status.State != "ready" {
		t.Fatal("recovery failed", status, err)
	}
}

func TestStatisticsCacheLimitsAndEvictionDoNotLosePendingWaiters(t *testing.T) {
	c := newStatisticsCache(t.Context(), time.Second)
	now := time.Now()
	for i := range statisticsCacheEntries {
		key := statisticsCacheKey{search: fmt.Sprint(i)}
		c.entries[key] = &statisticsCacheEntry{ready: make(chan struct{}), used: now}
	}
	_, _, err := c.get(t.Context(), statisticsCacheKey{search: "overflow"}, nil)
	if !errors.Is(err, utils.ErrRequestBudget) || len(c.entries) != statisticsCacheEntries {
		t.Fatal("pending capacity not bounded", err)
	}
	key := statisticsCacheKey{search: "0"}
	e := c.entries[key]
	e.ready = nil
	e.body = []byte("old")
	c.bytes = 3
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	// 完成条目可以驱逐；已取消的新请求退出等待，不丢失其他冷查询的通知。
	_, _, err = c.get(ctx, statisticsCacheKey{search: "new"}, nil)
	if !errors.Is(err, context.Canceled) || len(c.entries) != statisticsCacheEntries || c.bytes != 0 {
		t.Fatal("eviction lost budget", err)
	}
	if _, ok := c.entries[key]; ok {
		t.Fatal("completed LRU retained")
	}
}

func TestStatisticsCacheRejectsOversizedResultAndStops(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	c := newStatisticsCache(ctx, time.Second)
	key := statisticsCacheKey{}
	c.entries[key] = &statisticsCacheEntry{used: time.Now(), ready: make(chan struct{}), load: func(context.Context) ([]byte, error) { return make([]byte, statisticsCacheBytes+1), nil }}
	c.refresh()
	if c.bytes != 0 || !errors.Is(c.entries[key].err, utils.ErrRequestBudget) {
		t.Fatal("oversized entry retained")
	}
	cancel()
	_, _, err := c.get(t.Context(), key, nil)
	if !errors.Is(err, context.Canceled) {
		t.Fatal("stopped cache accepted request", err)
	}
}

func TestStatisticsCacheKeysIgnorePagingButIsolateFiltersAndAnnualDay(t *testing.T) {
	c := newStatisticsCache(t.Context(), time.Second)
	now := time.Date(2026, 10, 4, 23, 59, 0, 0, time.UTC)
	c.now = func() time.Time { return now }
	q := statisticsTestQuery(t, url.Values{"time_zone": {"UTC"}})
	key := c.key("summary", q)
	changed := q
	changed.Page++
	changed.Sort = "title"
	changed.Direction = "asc"
	changed.ThroughputLimit++
	if key != c.key("summary", changed) {
		t.Fatal("irrelevant paging split aggregate cache")
	}
	for _, apply := range []func(){func() { changed.Model = "unknown" }, func() { changed.ClientID = "client" }, func() { changed.ProjectID = "project" }, func() { changed.Search = "literal" }, func() { changed.Provider = "cursor" }, func() { changed.StartAtMS++ }, func() { changed.TimeZone = "Europe/London" }} {
		changed = q
		apply()
		if key == c.key("summary", changed) {
			t.Fatal("filter shared cache")
		}
	}
	now = now.Add(2 * time.Minute)
	if key == c.key("summary", q) {
		t.Fatal("annual cache crossed local midnight")
	}
}

func TestStatisticsCacheReturnsIndependentValuesAndChecksAdminOnHit(t *testing.T) {
	stats, _, admin, clients := statisticsFixture(t)
	c := newStatisticsCache(t.Context(), time.Second)
	q := statisticsTestQuery(t, nil)
	out, err := stats.Summary(t.Context(), admin, q)
	if err != nil {
		t.Fatal(err)
	}
	key := c.key("summary", q)
	body, err := projectionLoader(q, func(context.Context, statistics_dto.StatisticsQuery) (statistics_vo.StatisticsSummary, error) {
		return out, nil
	})(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	c.entries[key] = &statisticsCacheEntry{body: body, computed: time.Now(), attempted: time.Now()}
	c.bytes = len(body)
	stats.cache = c
	first, err := stats.Summary(t.Context(), admin, q)
	if err != nil {
		t.Fatal(err)
	}
	first.Heatmap[0].Date = "mutated"
	second, err := stats.Summary(t.Context(), admin, q)
	if err != nil || second.Heatmap[0].Date == "mutated" || second.Cache == nil {
		t.Fatal("caller mutated cache", err)
	}
	if _, err := stats.Summary(t.Context(), clients[0], q); !errors.Is(err, utils.ErrForbidden) {
		t.Fatal("cache bypassed admin check", err)
	}
}
