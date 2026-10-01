package dto

import "time"

// StatisticsQuery 是规范化后的 UTC/IANA 范围与服务端筛选，不承载凭证。
type StatisticsQuery struct {
	StartAtMS  int64
	EndAtMS    int64
	TimeZone   string
	Location   *time.Location
	Provider   string
	Model      string
	SessionKey string
	ClientID   string
	ProjectID  string
	Search     string
	Sort       string
	Direction  string
	Page       int
	Limit      int
}

// 查询超预算显式失败，不截断结果后伪装全量。
const (
	MaximumStatisticsSessions = 100000
	MaximumStatisticsProjects = 100000
	MaximumStatisticsClients  = 10000
	MaximumStatisticsSources  = 200000
	MaximumStatisticsFacts    = 2000000
)
