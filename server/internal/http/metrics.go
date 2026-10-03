package http

import (
	"context"
	"time"

	"github.com/prometheus/client_golang/prometheus"

	reporting_repo "github.com/SisyphusSQ/codex-pulse/server/internal/repository/mysql/reporting_repo"
)

// centerMetrics 只读取设备状态表与连接池；采集成本不随用量历史增长。
type centerMetrics struct {
	repository                                    *reporting_repo.Reporting
	up, open, inUse, idle, waitCount, waitSeconds *prometheus.Desc
	received, collected, pending, healthy         *prometheus.Desc
}

func newCenterMetrics(repository *reporting_repo.Reporting) *centerMetrics {
	d := func(name, help string, labels ...string) *prometheus.Desc {
		return prometheus.NewDesc("pulse_"+name, help, labels, nil)
	}
	return &centerMetrics{repository: repository,
		up:   d("metrics_database_up", "Whether the bounded database metrics query succeeded."),
		open: d("database_connections_open", "Open database connections."), inUse: d("database_connections_in_use", "Database connections in use."), idle: d("database_connections_idle", "Idle database connections."),
		waitCount: d("database_wait_total", "Connection pool waits."), waitSeconds: d("database_wait_seconds_total", "Time spent waiting for database connections."),
		received:  d("source_received_timestamp_seconds", "Last received source status, not collection time.", "client_id", "provider"),
		collected: d("source_collected_timestamp_seconds", "Last original collection time; absent means unknown.", "client_id", "provider"),
		pending:   d("source_pending_batches", "Pending batches at the last device status snapshot.", "client_id", "provider"),
		healthy:   d("source_sync_healthy", "One when the latest sync check was ready within 15 minutes.", "client_id", "provider"),
	}
}
func (m *centerMetrics) Describe(ch chan<- *prometheus.Desc) {
	for _, d := range []*prometheus.Desc{m.up, m.open, m.inUse, m.idle, m.waitCount, m.waitSeconds, m.received, m.collected, m.pending, m.healthy} {
		ch <- d
	}
}
func (m *centerMetrics) Collect(ch chan<- prometheus.Metric) {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	rows, pool, err := m.repository.Metrics(ctx)
	if err != nil {
		ch <- prometheus.MustNewConstMetric(m.up, prometheus.GaugeValue, 0)
		return
	}
	ch <- prometheus.MustNewConstMetric(m.up, prometheus.GaugeValue, 1)
	for _, p := range []struct {
		d *prometheus.Desc
		v float64
		t prometheus.ValueType
	}{{m.open, float64(pool.OpenConnections), prometheus.GaugeValue}, {m.inUse, float64(pool.InUse), prometheus.GaugeValue}, {m.idle, float64(pool.Idle), prometheus.GaugeValue}, {m.waitCount, float64(pool.WaitCount), prometheus.CounterValue}, {m.waitSeconds, pool.WaitDuration.Seconds(), prometheus.CounterValue}} {
		ch <- prometheus.MustNewConstMetric(p.d, p.t, p.v)
	}
	now := time.Now().UnixMilli()
	for _, row := range rows {
		labels := []string{row.ClientID, row.Provider}
		ch <- prometheus.MustNewConstMetric(m.received, prometheus.GaugeValue, float64(row.ReceivedAtMS)/1000, labels...)
		if row.CollectedAtMS != nil {
			ch <- prometheus.MustNewConstMetric(m.collected, prometheus.GaugeValue, float64(*row.CollectedAtMS)/1000, labels...)
		}
		ch <- prometheus.MustNewConstMetric(m.pending, prometheus.GaugeValue, float64(row.PendingBatches), labels...)
		value := float64(0)
		if row.SyncState == "ready" && row.SyncCheckedAtMS != nil && now-*row.SyncCheckedAtMS >= 0 && now-*row.SyncCheckedAtMS <= 15*60*1000 {
			value = 1
		}
		ch <- prometheus.MustNewConstMetric(m.healthy, prometheus.GaugeValue, value, labels...)
	}
}
