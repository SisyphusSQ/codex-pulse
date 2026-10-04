package statistics_srv

import (
	"context"
	"net/url"
	"reflect"
	"testing"
	"time"

	centerfixture "github.com/SisyphusSQ/codex-pulse/server/internal/testsupport/center"
)

func TestCardQueriesKeepLegacySummaryAndIndependentAnnualRange(t *testing.T) {
	stats, reporting, admin, clients := statisticsFixture(t)
	snapshot := centerfixture.Snapshot()
	centerfixture.SendSnapshot(t, reporting, clients[0], snapshot)
	for _, client := range []string{"", clients[0].ID} {
		q := statisticsTestQuery(t, nil)
		q.ClientID = client
		combined, err := stats.Summary(t.Context(), admin, q)
		if err != nil {
			t.Fatal(err)
		}
		annual, err := stats.Annual(t.Context(), admin, q)
		if err != nil {
			t.Fatal(err)
		}
		activity, err := stats.Activity(t.Context(), admin, q)
		if err != nil {
			t.Fatal(err)
		}
		top, err := stats.Top(t.Context(), admin, q)
		if err != nil {
			t.Fatal(err)
		}
		providers, err := stats.Breakdown(t.Context(), admin, q, false)
		if err != nil {
			t.Fatal(err)
		}
		models, err := stats.Breakdown(t.Context(), admin, q, true)
		if err != nil {
			t.Fatal(err)
		}
		if !reflect.DeepEqual(annual.Heatmap, combined.Heatmap) || !reflect.DeepEqual(annual.HeatmapTotals, combined.HeatmapTotals) || !reflect.DeepEqual(annual.HeatmapCoverage, combined.HeatmapCoverage) || !reflect.DeepEqual(activity.ActivityTimeline, combined.ActivityTimeline) || !reflect.DeepEqual(activity.WeekdayHours, combined.WeekdayHours) || !reflect.DeepEqual(top.TopSessions, combined.TopSessions) || !reflect.DeepEqual(providers.Providers, combined.Providers) || !reflect.DeepEqual(models.Models, combined.Models) {
			t.Fatal("card split changed facts")
		}
		changed := q
		changed.StartAtMS++
		changed.EndAtMS++
		other, err := stats.Annual(t.Context(), admin, changed)
		if err != nil || !reflect.DeepEqual(annual, other) {
			t.Fatal("KPI dates changed annual facts", err)
		}
	}
}

func TestListProjectionsKeepLegacyValuesWithoutUnusedGroups(t *testing.T) {
	stats, reporting, _, clients := statisticsFixture(t)
	centerfixture.SendSnapshot(t, reporting, clients[0], throughputSnapshot())
	for _, values := range []url.Values{{}, {"client_id": {clients[0].ID}}, {"model": {"unknown"}}, {"search": {"no literal match"}}} {
		q := statisticsTestQuery(t, values)
		full, err := stats.read(t.Context(), q)
		if err != nil {
			t.Fatal(err)
		}
		for _, kind := range []string{"sessions", "session", "projects", "project"} {
			read, err := stats.readFor(t.Context(), q, kind)
			if err != nil {
				t.Fatal(err)
			}
			a, _ := full.total.finish(true)
			b, _ := read.total.finish(true)
			if !reflect.DeepEqual(a, b) || !reflect.DeepEqual(full.coverage(stats.now()), read.coverage(stats.now())) {
				t.Fatal("projection changed totals or coverage", kind)
			}
			if kind == "sessions" || kind == "session" || kind == "project" {
				if !reflect.DeepEqual(full.sessionList(), read.sessionList()) {
					t.Fatal("projection changed session list", kind)
				}
			}
			if kind == "projects" || kind == "project" {
				if !reflect.DeepEqual(full.projectViews(), read.projectViews()) {
					t.Fatal("projection changed project list", kind)
				}
			}
			if len(read.hours) != 0 || len(read.timeline) != 0 {
				t.Fatal("unused activity populated", kind)
			}
		}
	}
}

func TestCacheWorkerWarmRefreshAndStop(t *testing.T) {
	stats, _, admin, _ := statisticsFixture(t)
	ctx, cancel := context.WithCancel(t.Context())
	c := newStatisticsCache(ctx, time.Second)
	stats.cache = c
	go c.run(stats)
	q := statisticsTestQuery(t, nil)
	if _, err := stats.Annual(t.Context(), admin, q); err != nil {
		t.Fatal(err)
	}
	if _, err := stats.Activity(t.Context(), admin, q); err != nil {
		t.Fatal(err)
	}
	if _, err := stats.Top(t.Context(), admin, q); err != nil {
		t.Fatal(err)
	}
	c.mu.Lock()
	entryCount := len(c.entries)
	c.mu.Unlock()
	if entryCount > 6 {
		t.Fatal("cards did not share current projection")
	}
	cancel()
	select {
	case <-c.done:
	case <-time.After(2 * time.Second):
		t.Fatal("cache worker did not stop")
	}
}

func TestDefaultCardsStayActiveWithoutBrowserTraffic(t *testing.T) {
	stats, _, _, _ := statisticsFixture(t)
	c := newStatisticsCache(t.Context(), time.Second)
	now := stats.now()
	c.now = func() time.Time { return now }
	c.warm(stats)
	c.mu.Lock()
	if len(c.entries) != 4 {
		t.Fatalf("default projections: %d", len(c.entries))
	}
	for _, e := range c.entries {
		e.ready = nil
		e.used = now.Add(-statisticsIdle - time.Minute)
	}
	idleAt := now.Add(-statisticsIdle - time.Minute)
	other := &statisticsCacheEntry{used: idleAt}
	c.entries[statisticsCacheKey{kind: "summary", zone: "UTC"}] = other
	c.mu.Unlock()
	c.warm(stats)
	c.mu.Lock()
	defer c.mu.Unlock()
	for key, e := range c.entries {
		if key.kind == "summary" {
			if !e.used.Equal(idleAt) {
				t.Fatal("arbitrary filters were kept active")
			}
		} else if !e.used.Equal(now) {
			t.Fatal("default projection went idle", key.kind)
		}
	}
}
