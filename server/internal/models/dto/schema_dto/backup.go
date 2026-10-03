package schema_dto

type BackupManifest struct {
	Format        int    `json:"format"`
	Driver        string `json:"driver"`
	Schema        int64  `json:"schema"`
	SHA256        string `json:"sha256"`
	CreatedAtMS   int64  `json:"created_at_ms"`
	ServerVersion string `json:"server_version"`
}
