package runtime_vo

// VersionView 仅包含构建标识；不包含环境、路径、仓库地址或秘密。
type VersionView struct {
	Version           string `json:"version"`
	Commit            string `json:"commit"`
	BuiltAt           string `json:"built_at"`
	ReportingProtocol int    `json:"reporting_protocol"`
	ThroughputCapsule int    `json:"throughput_capsule"`
	Schema            int64  `json:"schema"`
}
