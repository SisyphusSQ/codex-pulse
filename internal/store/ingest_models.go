package store

type sourceGenerationModel struct {
	SourceFileID                        string  `gorm:"column:source_file_id;primaryKey"`
	Generation                          int64   `gorm:"column:generation;primaryKey"`
	State                               string  `gorm:"column:state"`
	Provider                            string  `gorm:"column:provider"`
	SourceKind                          string  `gorm:"column:source_kind"`
	CurrentPath                         string  `gorm:"column:current_path"`
	DeviceID                            string  `gorm:"column:device_id"`
	Inode                               int64   `gorm:"column:inode"`
	SizeBytes                           int64   `gorm:"column:size_bytes"`
	MTimeNS                             int64   `gorm:"column:mtime_ns"`
	PrefixBytes                         int64   `gorm:"column:prefix_bytes"`
	PrefixSHA256                        string  `gorm:"column:prefix_sha256"`
	FingerprintSHA256                   string  `gorm:"column:fingerprint_sha256"`
	ParserVersion                       string  `gorm:"column:parser_version"`
	CommittedOffset                     int64   `gorm:"column:committed_offset"`
	SessionID                           *string `gorm:"column:session_id"`
	ReplacesSourceFileID                *string `gorm:"column:replaces_source_file_id"`
	BaseSourceFileID                    *string `gorm:"column:base_source_file_id"`
	BaseGeneration                      *int64  `gorm:"column:base_generation"`
	BaseFingerprintSHA256               *string `gorm:"column:base_fingerprint_sha256"`
	SupersededBuildingSourceFileID      *string `gorm:"column:superseded_building_source_file_id"`
	SupersededBuildingGeneration        *int64  `gorm:"column:superseded_building_generation"`
	SupersededBuildingFingerprintSHA256 *string `gorm:"column:superseded_building_fingerprint_sha256"`
	SupersededBuildingParserVersion     *string `gorm:"column:superseded_building_parser_version"`
	UpdatedAtMS                         int64   `gorm:"column:updated_at_ms"`
}

func (sourceGenerationModel) TableName() string { return "source_generations" }

type parserCheckpointModel struct {
	SourceFileID      string `gorm:"column:source_file_id;primaryKey"`
	Generation        int64  `gorm:"column:generation;primaryKey"`
	CheckpointVersion int    `gorm:"column:checkpoint_version"`
	ParserSeed        []byte `gorm:"column:parser_seed"`
	ProjectorState    []byte `gorm:"column:projector_state"`
	UpdatedAtMS       int64  `gorm:"column:updated_at_ms"`
}

func (parserCheckpointModel) TableName() string { return "parser_checkpoints" }

type sourceGenerationBatchModel struct {
	SourceFileID        string `gorm:"column:source_file_id;primaryKey"`
	Generation          int64  `gorm:"column:generation;primaryKey"`
	FromOffset          int64  `gorm:"column:from_offset;primaryKey"`
	ToOffset            int64  `gorm:"column:to_offset;primaryKey"`
	BatchIdentitySHA256 string `gorm:"column:batch_identity_sha256;primaryKey"`
	Facts               []byte `gorm:"column:facts"`
	EOF                 bool   `gorm:"column:eof"`
	CreatedAtMS         int64  `gorm:"column:created_at_ms"`
}

func (sourceGenerationBatchModel) TableName() string { return "source_generation_batches" }

type parserDiagnosticModel struct {
	SourceFileID        string `gorm:"column:source_file_id;primaryKey"`
	Generation          int64  `gorm:"column:generation;primaryKey"`
	BatchEndOffset      int64  `gorm:"column:batch_end_offset;primaryKey"`
	BatchIdentitySHA256 string `gorm:"column:batch_identity_sha256;primaryKey"`
	Ordinal             int64  `gorm:"column:ordinal;primaryKey"`
	Class               string `gorm:"column:class"`
	Code                string `gorm:"column:code"`
	StartOffset         int64  `gorm:"column:start_offset"`
	EndOffset           int64  `gorm:"column:end_offset"`
	Retryable           bool   `gorm:"column:retryable"`
}

func (parserDiagnosticModel) TableName() string { return "parser_diagnostics" }
