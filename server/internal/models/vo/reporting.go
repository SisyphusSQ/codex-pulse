package vo

import reportingv1 "github.com/SisyphusSQ/codex-pulse/api/codexpulse/reporting/v1"

// SyncView 只显示当前采集客户端的提交进度，不接受其他客户端ID。
type SyncView struct {
	ProtocolVersion  int    `json:"protocol_version"`
	LastReceivedAtMS *int64 `json:"last_received_at_ms"`
	Batches          int64  `json:"batches"`
}

// ReportingBatchRequest 是网络 contract 的独立请求 VO；controller 显式转换至 DTO。
type ReportingBatchRequest reportingv1.Batch
type ReportingReceiptView struct {
	Version      int    `json:"version"`
	BatchID      string `json:"batch_id"`
	ReceivedAtMS int64  `json:"received_at_ms"`
}

// ProjectAssociationRequest 的标识是中心项目键；空 TargetID 明确解除所选项目的关联。
type ProjectAssociationRequest struct {
	ProjectIDs []string `json:"project_ids"`
	TargetID   string   `json:"target_id"`
}
