package reporting_do

// SnapshotChunk 的主键限定已鉴权设备、来源、版本与片段。
type SnapshotChunk struct {
	ClientID     string `gorm:"primaryKey"`
	SourceKey    string `gorm:"primaryKey"`
	Revision     int64  `gorm:"primaryKey"`
	ChunkIndex   int    `gorm:"primaryKey"`
	Payload      string
	PayloadBytes int64
	ReceivedAtMS int64
}

func (SnapshotChunk) TableName() string { return "pulse_snapshot_chunks" }
