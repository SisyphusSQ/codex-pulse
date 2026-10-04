package statistics_srv

import (
	"context"
	"encoding/json/v2"
	"net/url"
	"sync"
	"time"

	"go.uber.org/fx"

	"github.com/SisyphusSQ/codex-pulse/server/config"
	"github.com/SisyphusSQ/codex-pulse/server/internal/lib/log"
	statistics_dto "github.com/SisyphusSQ/codex-pulse/server/internal/models/dto/statistics_dto"
	statistics_vo "github.com/SisyphusSQ/codex-pulse/server/internal/models/vo/statistics_vo"
	"github.com/SisyphusSQ/codex-pulse/server/utils"
)

const (
	statisticsRefresh      = time.Minute
	statisticsIdle         = 5 * time.Minute
	statisticsCacheEntries = 32
	statisticsCacheBytes   = 64 << 20
)

// 以下结构只保存进程内缓存的私有执行状态；不作为跨层模型或持久化数据。
type statisticsCacheKey struct {
	kind, zone, provider, model, session, client, project, search, annualDay string
	start, end                                                               int64
}
type statisticsCacheEntry struct {
	load                      func(context.Context) ([]byte, error)
	body                      []byte
	ready                     chan struct{}
	running                   bool
	err                       error
	computed, attempted, used time.Time
}
type statisticsCache struct {
	mu      sync.Mutex
	entries map[statisticsCacheKey]*statisticsCacheEntry
	bytes   int
	ctx     context.Context
	wake    chan struct{}
	done    chan struct{}
	now     func() time.Time
	timeout time.Duration
}

func newStatisticsCache(ctx context.Context, timeout time.Duration) *statisticsCache {
	return &statisticsCache{entries: make(map[statisticsCacheKey]*statisticsCacheEntry), ctx: ctx, wake: make(chan struct{}, 1), done: make(chan struct{}), now: time.Now, timeout: timeout}
}

// StartCache 由 Fx 托管，后台一次只计算一个投影；请求取消不会中断其他请求共享的刷新。
func StartCache(lifecycle fx.Lifecycle, cfg config.Config, s *Statistics) {
	ctx, cancel := context.WithCancel(context.Background())
	c := newStatisticsCache(ctx, cfg.ContextTimeout)
	s.cache = c
	lifecycle.Append(fx.Hook{OnStart: func(context.Context) error {
		go c.run(s)
		return nil
	}, OnStop: func(ctx context.Context) error {
		cancel()
		select {
		case <-c.done:
			return nil
		case <-ctx.Done():
			return ctx.Err()
		}
	}})
}

func (c *statisticsCache) key(kind string, q statistics_dto.StatisticsQuery) statisticsCacheKey {
	k := statisticsCacheKey{kind: kind, zone: q.TimeZone, provider: q.Provider, model: q.Model, session: q.SessionKey, client: q.ClientID, project: q.ProjectID, search: q.Search, start: q.StartAtMS, end: q.EndAtMS}
	if kind == "summary" {
		k.annualDay = c.now().In(q.Location).Format(time.DateOnly)
	}
	return k
}

func projectionLoader[T any](q statistics_dto.StatisticsQuery, load func(context.Context, statistics_dto.StatisticsQuery) (T, error)) func(context.Context) ([]byte, error) {
	return func(ctx context.Context) ([]byte, error) {
		v, err := load(ctx, q)
		if err != nil {
			return nil, err
		}
		return json.Marshal(v)
	}
}

func cachedProjection[T any](ctx context.Context, c *statisticsCache, kind string, q statistics_dto.StatisticsQuery, load func(context.Context, statistics_dto.StatisticsQuery) (T, error)) (out T, status *statistics_vo.StatisticsCache, err error) {
	if err = ctx.Err(); err != nil {
		return
	}
	body, status, err := c.get(ctx, c.key(kind, q), projectionLoader(q, load))
	if err != nil {
		return out, status, err
	}
	err = json.Unmarshal(body, &out)
	return
}

func (c *statisticsCache) get(ctx context.Context, key statisticsCacheKey, load func(context.Context) ([]byte, error)) ([]byte, *statistics_vo.StatisticsCache, error) {
	c.mu.Lock()
	if err := c.ctx.Err(); err != nil {
		c.mu.Unlock()
		return nil, nil, err
	}
	e := c.entries[key]
	if e == nil {
		if len(c.entries) >= statisticsCacheEntries && !c.evict(nil) {
			c.mu.Unlock()
			return nil, nil, utils.ErrRequestBudget
		}
		e = &statisticsCacheEntry{load: load, ready: make(chan struct{})}
		c.entries[key] = e
	}
	e.used = c.now()
	if e.body != nil {
		body, status := e.body, c.status(e)
		c.mu.Unlock()
		c.signal()
		return body, status, nil
	}
	if e.ready == nil {
		err := e.err
		c.mu.Unlock()
		c.signal()
		return nil, nil, err
	}
	ready := e.ready
	c.mu.Unlock()
	c.signal()
	select {
	case <-ctx.Done():
		return nil, nil, ctx.Err()
	case <-c.ctx.Done():
		return nil, nil, c.ctx.Err()
	case <-ready:
	}
	c.mu.Lock()
	body, err := e.body, e.err
	var status *statistics_vo.StatisticsCache
	if body != nil {
		status = c.status(e)
	}
	c.mu.Unlock()
	return body, status, err
}

func (c *statisticsCache) status(e *statisticsCacheEntry) *statistics_vo.StatisticsCache {
	age := max(int64(0), c.now().Sub(e.computed).Milliseconds())
	state := "ready"
	if e.running {
		state = "refreshing"
	} else if e.err != nil {
		state = "refresh_failed"
	}
	return &statistics_vo.StatisticsCache{ComputedAtMS: e.computed.UnixMilli(), RefreshAfterMS: statisticsRefresh.Milliseconds(), AgeMS: age, Stale: age > int64(2*time.Minute/time.Millisecond), State: state}
}

func (c *statisticsCache) signal() {
	select {
	case c.wake <- struct{}{}:
	default:
	}
}

// evict 只移除已完成条目；冷查询等待者始终能收到 ready 的关闭通知。
func (c *statisticsCache) evict(skip *statisticsCacheEntry) bool {
	var oldest *statisticsCacheEntry
	var key statisticsCacheKey
	for k, e := range c.entries {
		if e == skip || e.running || e.ready != nil {
			continue
		}
		if oldest == nil || e.used.Before(oldest.used) {
			oldest = e
			key = k
		}
	}
	if oldest == nil {
		return false
	}
	c.bytes -= len(oldest.body)
	delete(c.entries, key)
	return true
}

func (c *statisticsCache) refresh() bool {
	c.mu.Lock()
	now := c.now()
	var chosen *statisticsCacheEntry
	for _, e := range c.entries {
		if e.running || (e.ready == nil && now.Sub(e.used) > statisticsIdle) || (!e.attempted.IsZero() && now.Sub(e.attempted) < statisticsRefresh) {
			continue
		}
		if chosen == nil || (e.attempted.IsZero() && !chosen.attempted.IsZero()) || (e.attempted.IsZero() == chosen.attempted.IsZero() && e.attempted.Before(chosen.attempted)) {
			chosen = e
		}
	}
	if chosen == nil {
		c.mu.Unlock()
		return false
	}
	e := chosen
	e.running = true
	e.attempted = now
	c.mu.Unlock()
	ctx, cancel := context.WithTimeout(c.ctx, c.timeout)
	body, err := e.load(ctx)
	cancel()
	c.mu.Lock()
	if err == nil && len(body) > statisticsCacheBytes {
		err = utils.ErrRequestBudget
	}
	if err == nil {
		for c.bytes-len(e.body)+len(body) > statisticsCacheBytes {
			if !c.evict(e) {
				err = utils.ErrRequestBudget
				break
			}
		}
	}
	if err == nil {
		c.bytes += len(body) - len(e.body)
		e.body = body
		e.computed = now
	}
	e.err = err
	e.running = false
	if e.ready != nil {
		close(e.ready)
		e.ready = nil
	}
	c.mu.Unlock()
	if err != nil && c.ctx.Err() == nil && log.Logger != nil {
		log.Logger.Errorf("statistics cache refresh failed: %T", err)
	}
	return true
}

func (c *statisticsCache) warm(s *Statistics) {
	now := c.now()
	zone, _ := time.LoadLocation("Asia/Shanghai")
	local := now.In(zone)
	start := time.Date(local.Year(), local.Month(), local.Day(), 0, 0, 0, 0, zone)
	q, err := ParseStatisticsQuery(url.Values{"start_date": {start.Format(time.DateOnly)}, "end_date_exclusive": {start.AddDate(0, 0, 1).Format(time.DateOnly)}}, now)
	if err != nil {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	for kind, load := range map[string]func(context.Context) ([]byte, error){"annual": projectionLoader(s.annualQuery(q), s.annual), "usage": projectionLoader(q, s.usage), "current": projectionLoader(q, s.current), "source-usage": projectionLoader(q, s.sourceUsage)} {
		query := q
		if kind == "annual" {
			query = s.annualQuery(q)
		}
		key := c.key(kind, query)
		if c.entries[key] != nil {
			continue
		}
		if len(c.entries) >= statisticsCacheEntries && !c.evict(nil) {
			continue
		}
		c.entries[key] = &statisticsCacheEntry{load: load, ready: make(chan struct{}), used: now}
	}
}

func (c *statisticsCache) run(s *Statistics) {
	defer close(c.done)
	c.warm(s)
	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()
	for {
		for c.ctx.Err() == nil && c.refresh() {
		}
		select {
		case <-c.ctx.Done():
			return
		case <-c.wake:
		case <-ticker.C:
			c.warm(s)
		}
	}
}
