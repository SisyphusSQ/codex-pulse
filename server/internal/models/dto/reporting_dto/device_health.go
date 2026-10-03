package reporting_dto

// DeviceHealth 仅供有界运维指标，不读取会话、账号或用量历史。
type DeviceHealth struct {
	ClientID        string
	Provider        string
	CollectedAtMS   *int64
	ReceivedAtMS    int64
	PendingBatches  int64
	SyncState       string
	SyncCheckedAtMS *int64
}
