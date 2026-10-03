package quota_dto

// WindowScope 描述 SQL 范围；未关联观测保留设备及本地 scope 隔离。
type WindowScope struct {
	Provider      string
	AccountKey    *string
	ClientID      string
	LocalScope    string
	LimitID       string
	WindowKind    string
	WindowMinutes *int64
}
