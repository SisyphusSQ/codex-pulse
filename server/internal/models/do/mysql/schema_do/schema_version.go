package schema_do

// SchemaVersion 是单例结构标记，只有完成 DDL 和读回后才写入。
type SchemaVersion struct {
	ID              int64
	Version         int64
	Checksum        string
	InitializedAtMS int64
}

func (SchemaVersion) TableName() string { return "pulse_schema" }
