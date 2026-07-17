package store

type appRuntimeSampleModel struct {
	CapturedAtMS            int64   `gorm:"column:captured_at_ms;primaryKey;autoIncrement:false"`
	CPUPercent              float64 `gorm:"column:cpu_percent"`
	CPUUserMS               int64   `gorm:"column:cpu_user_ms"`
	CPUSystemMS             int64   `gorm:"column:cpu_system_ms"`
	RSSBytes                int64   `gorm:"column:rss_bytes"`
	PeakRSSBytes            int64   `gorm:"column:peak_rss_bytes"`
	GoroutineCount          int64   `gorm:"column:goroutine_count"`
	DBBytes                 int64   `gorm:"column:db_bytes"`
	WALBytes                int64   `gorm:"column:wal_bytes"`
	DiskFreeBytes           int64   `gorm:"column:disk_free_bytes"`
	LiveQueueDepth          int64   `gorm:"column:live_queue_depth"`
	BackfillQueueDepth      int64   `gorm:"column:backfill_queue_depth"`
	OldestLiveWaitMS        int64   `gorm:"column:oldest_live_wait_ms"`
	OldestBackfillWaitMS    int64   `gorm:"column:oldest_backfill_wait_ms"`
	QueryCount              int64   `gorm:"column:query_count"`
	QueryTotalMicros        int64   `gorm:"column:query_total_micros"`
	QueryMaxMicros          int64   `gorm:"column:query_max_micros"`
	CollectorDurationMicros int64   `gorm:"column:collector_duration_micros"`
	DroppedSamples          int64   `gorm:"column:dropped_samples"`
}

func (appRuntimeSampleModel) TableName() string { return "app_runtime_samples" }

func appRuntimeSampleModelFromDomain(value AppRuntimeSample) appRuntimeSampleModel {
	return appRuntimeSampleModel{
		CapturedAtMS: value.CapturedAtMS, CPUPercent: value.CPUPercent,
		CPUUserMS: value.CPUUserMS, CPUSystemMS: value.CPUSystemMS,
		RSSBytes: value.RSSBytes, PeakRSSBytes: value.PeakRSSBytes,
		GoroutineCount: value.GoroutineCount, DBBytes: value.DBBytes,
		WALBytes: value.WALBytes, DiskFreeBytes: value.DiskFreeBytes,
		LiveQueueDepth: value.LiveQueueDepth, BackfillQueueDepth: value.BackfillQueueDepth,
		OldestLiveWaitMS:     value.OldestLiveWaitMS,
		OldestBackfillWaitMS: value.OldestBackfillWaitMS,
		QueryCount:           value.QueryCount, QueryTotalMicros: value.QueryTotalMicros,
		QueryMaxMicros:          value.QueryMaxMicros,
		CollectorDurationMicros: value.CollectorDurationMicros,
		DroppedSamples:          value.DroppedSamples,
	}
}

func appRuntimeSampleFromModel(model appRuntimeSampleModel) AppRuntimeSample {
	return AppRuntimeSample{
		CapturedAtMS: model.CapturedAtMS, CPUPercent: model.CPUPercent,
		CPUUserMS: model.CPUUserMS, CPUSystemMS: model.CPUSystemMS,
		RSSBytes: model.RSSBytes, PeakRSSBytes: model.PeakRSSBytes,
		GoroutineCount: model.GoroutineCount, DBBytes: model.DBBytes,
		WALBytes: model.WALBytes, DiskFreeBytes: model.DiskFreeBytes,
		LiveQueueDepth: model.LiveQueueDepth, BackfillQueueDepth: model.BackfillQueueDepth,
		OldestLiveWaitMS:     model.OldestLiveWaitMS,
		OldestBackfillWaitMS: model.OldestBackfillWaitMS,
		QueryCount:           model.QueryCount, QueryTotalMicros: model.QueryTotalMicros,
		QueryMaxMicros:          model.QueryMaxMicros,
		CollectorDurationMicros: model.CollectorDurationMicros,
		DroppedSamples:          model.DroppedSamples,
	}
}
