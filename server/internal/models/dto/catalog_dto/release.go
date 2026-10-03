package catalog_dto

// ModelRelease 是独立于费率版本的官方型号发布记录。
type ModelRelease struct {
	Models     []string `json:"models"`
	ReleasedOn string   `json:"released_on"`
	SourceURL  string   `json:"source_url"`
}

// ReleaseInfo 供中心目录为公开、历史与观测模型补充发布时间。
type ReleaseInfo struct {
	ReleasedAtMS int64
	SourceURL    string
}
