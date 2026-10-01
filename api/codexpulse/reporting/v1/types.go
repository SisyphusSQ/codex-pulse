// Package reportingv1 定义白名单中心同步 contract，不承载 Agent 原始内容或凭据。
package reportingv1

const Version = 1
const MaxBodyBytes = 8 << 20
const MaxContributions = 20000
const MaxTimestampMS int64 = 9007199254740991

// Batch 是不可变的事务单元；身份只来自鉴权凭证，不在 body 中自称设备归属。
type Batch struct {
	Version  int                `json:"version"`
	ID       string             `json:"id"`
	Sessions []SessionSnapshot  `json:"sessions,omitempty"`
	Accounts []Account          `json:"accounts,omitempty"`
	Bindings []AccountBinding   `json:"bindings,omitempty"`
	Quotas   []QuotaObservation `json:"quotas,omitempty"`
	Credits  []ResetCredits     `json:"credits,omitempty"`
	Status   []DeviceStatus     `json:"status,omitempty"`
}

// SessionSnapshot 是同一来源的完整替换快照，revision 跨进程重启单调递增。
type SessionSnapshot struct {
	Throughput       *ThroughputCapsule `json:"throughput,omitempty"`
	Provider         string             `json:"provider"`
	HomeID           string             `json:"home_id"`
	SessionID        string             `json:"session_id"`
	Revision         int64              `json:"revision"`
	CollectedAtMS    int64              `json:"collected_at_ms"`
	Title            string             `json:"title"`
	ProjectID        string             `json:"project_id"`
	ProjectName      string             `json:"project_name"`
	CreatedAtMS      *int64             `json:"created_at_ms"`
	LastActiveAtMS   *int64             `json:"last_active_at_ms"`
	Complete         bool               `json:"complete"`
	Deleted          bool               `json:"deleted"`
	SourceKind       string             `json:"source_kind"`
	SessionKind      string             `json:"session_kind"`
	HistoryStartAtMS int64              `json:"history_start_at_ms"`
	Contributions    []Contribution     `json:"contributions"`
	Invocations      []Invocation       `json:"invocations,omitempty"`
}

// Contribution 保留来源 Token 口径；大整数用十进制字符串，避免浏览器精度损失。
type Contribution struct {
	ID                     string  `json:"id"`
	ObservedAtMS           *int64  `json:"observed_at_ms"`
	Model                  *string `json:"model"`
	InputTokens            *int64  `json:"input_tokens,string"`
	CachedTokens           *int64  `json:"cached_tokens,string"`
	CacheWriteTokens       *int64  `json:"cache_write_tokens,string"`
	OutputTokens           *int64  `json:"output_tokens,string"`
	ReasoningTokens        *int64  `json:"reasoning_tokens,string"`
	TotalTokens            *int64  `json:"total_tokens,string"`
	CostMicroUSD           *int64  `json:"cost_micro_usd,string"`
	ReportedChargeMicroUSD *int64  `json:"reported_charge_micro_usd,string"`
	PricingVersion         *string `json:"pricing_version"`
	CostStatus             string  `json:"cost_status"`
	PricingMode            string  `json:"pricing_mode"`
	Rates                  *Rates  `json:"rates,omitempty"`
}

// Rates 是历史价格证据。Codex 按模型/价格版本汇总后舍入，Cursor 在查询范围内
// 对所有可定价事件求和后舍入；不能用各事件舍入后的金额代替原有口径。
type Rates struct {
	InputMicroUSD      *int64 `json:"input_micro_usd,string"`
	CachedMicroUSD     *int64 `json:"cached_micro_usd,string"`
	CacheWriteMicroUSD *int64 `json:"cache_write_micro_usd,string"`
	OutputMicroUSD     *int64 `json:"output_micro_usd,string"`
}

// Account 只接受受支持公开来源的 confirmed 身份，不根据邮箱合并。
type Account struct {
	Provider      string  `json:"provider"`
	ID            string  `json:"id"`
	Email         *string `json:"email"`
	Plan          *string `json:"plan"`
	CollectedAtMS int64   `json:"collected_at_ms"`
}

// AccountBinding 是同一 confirmed context 证明的本地 scope 关系，不绑定 Home 用量。
type AccountBinding struct {
	Provider      string `json:"provider"`
	LocalScope    string `json:"local_scope"`
	AccountID     string `json:"account_id"`
	ConfirmedAtMS int64  `json:"confirmed_at_ms"`
}

// QuotaObservation 可以缺少账号关系，此时保留待关联历史。
type QuotaObservation struct {
	AssociationScope *string  `json:"association_scope,omitempty"`
	WindowStartAtMS  *int64   `json:"window_start_at_ms,omitempty"`
	Provider         string   `json:"provider"`
	ID               string   `json:"id"`
	AccountID        *string  `json:"account_id"`
	LocalScope       string   `json:"local_scope"`
	LimitID          string   `json:"limit_id"`
	WindowKind       string   `json:"window_kind"`
	WindowMinutes    *int64   `json:"window_minutes"`
	ResetsAtMS       *int64   `json:"resets_at_ms"`
	ObservedAtMS     int64    `json:"observed_at_ms"`
	UsedPercent      *float64 `json:"used_percent"`
	Validity         string   `json:"validity"`
	Source           string   `json:"source"`
	HistoryOrigin    string   `json:"history_origin"`
}

// ResetCredits 不包含原始 credit ID、消费凭据或响应。
type CreditExpiry struct {
	ExpiresAtMS *int64 `json:"expires_at_ms"`
	Count       int64  `json:"count,string"`
}
type ResetCredits struct {
	DetailsStatus   string         `json:"details_status,omitempty"`
	ExpirySchedule  []CreditExpiry `json:"expiry_schedule,omitempty"`
	NextExpiresAtMS *int64         `json:"next_expires_at_ms,omitempty"`
	Provider        string         `json:"provider"`
	ID              string         `json:"id"`
	AccountID       *string        `json:"account_id"`
	LocalScope      string         `json:"local_scope"`
	ObservedAtMS    int64          `json:"observed_at_ms"`
	Inventory       *int64         `json:"inventory,string"`
	Status          string         `json:"status"`
	NextResetAtMS   *int64         `json:"next_reset_at_ms"`
}

// DeviceStatus 的新鲜度与覆盖独立于在线状态，不接收 raw error。
type DeviceStatus struct {
	Provider        string `json:"provider"`
	Version         string `json:"version"`
	CollectedAtMS   *int64 `json:"collected_at_ms"`
	CoverageStartMS *int64 `json:"coverage_start_ms"`
	CoverageEndMS   *int64 `json:"coverage_end_ms"`
	PendingBatches  int64  `json:"pending_batches"`
	Status          string `json:"status"`
}

// Receipt 只确认已提交的同一批次，重复请求返回原始确认时间。
type Receipt struct {
	Version      int    `json:"version"`
	BatchID      string `json:"batch_id"`
	ReceivedAtMS int64  `json:"received_at_ms"`
}

type CollectorPairRequest struct {
	Code string `json:"code"`
	Mode string `json:"mode"`
}
type CollectorPairResponse struct {
	ClientID        string `json:"client_id"`
	Credential      string `json:"credential"`
	ProtocolVersion int    `json:"protocol_version"`
}

// Invocation 是工具/技能结构化统计，不包含参数、输出或任何原始内容。
type Invocation struct {
	ID           string `json:"id"`
	ObservedAtMS int64  `json:"observed_at_ms"`
	Kind         string `json:"kind"`
	Name         string `json:"name"`
	Outcome      string `json:"outcome"`
	DurationMS   *int64 `json:"duration_ms"`
}
