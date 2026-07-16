package query

const (
	// ContractVersion 是所有 M6 公共 query DTO 的版本。
	ContractVersion = "query-v1"
	// HardMaxPageLimit 防止 endpoint 把公共查询退化为无限导出。
	HardMaxPageLimit = 500
	// HardMaxRangeDays 是 endpoint 可配置日期范围的绝对上限。
	HardMaxRangeDays = 3660

	maxCursorLength    = 2048
	maxSortTerms       = 4
	maxFilterTerms     = 16
	maxFilterValues    = 32
	maxFilterValueSize = 256
)

// SortDirection 是有限排序方向。
type SortDirection string

const (
	SortAscending  SortDirection = "asc"
	SortDescending SortDirection = "desc"
)

// FilterOperator 是业务字段筛选操作，不映射任意 SQL 操作符。
type FilterOperator string

const (
	FilterEqual       FilterOperator = "eq"
	FilterNotEqual    FilterOperator = "not_eq"
	FilterIn          FilterOperator = "in"
	FilterGreaterThan FilterOperator = "gt"
	FilterAtLeast     FilterOperator = "gte"
	FilterLessThan    FilterOperator = "lt"
	FilterAtMost      FilterOperator = "lte"
	FilterContains    FilterOperator = "contains"
	FilterIsNull      FilterOperator = "is_null"
	FilterIsNotNull   FilterOperator = "is_not_null"
)

// PageRequest 使用有界 limit 和不透明 cursor；cursor 的业务 payload 由具体 endpoint 拥有。
type PageRequest struct {
	Cursor *string `json:"cursor"`
	Limit  int     `json:"limit"`
}

// SortTerm 指向 endpoint specification 中声明的业务字段。
type SortTerm struct {
	Field     string        `json:"field"`
	Direction SortDirection `json:"direction"`
}

// FilterTerm 只携带稳定业务值；具体类型转换由业务 query service 负责。
type FilterTerm struct {
	Field    string         `json:"field"`
	Operator FilterOperator `json:"operator"`
	Values   []string       `json:"values"`
}

// LocalDateRange 使用本地日和 IANA timezone 表达用户选择范围。
type LocalDateRange struct {
	StartDate        string `json:"startDate"`
	EndDateExclusive string `json:"endDateExclusive"`
	TimeZone         string `json:"timeZone"`
}

// UTCTimeRange 是 LocalDateRange 归一化后的 UTC 半开区间。
type UTCTimeRange struct {
	StartAtMS int64  `json:"startAtMs"`
	EndAtMS   int64  `json:"endAtMs"`
	TimeZone  string `json:"timeZone"`
}

// Request 是跨页面公共查询输入 envelope。
type Request struct {
	Page      PageRequest     `json:"page"`
	Sort      []SortTerm      `json:"sort"`
	Filters   []FilterTerm    `json:"filters"`
	TimeRange *LocalDateRange `json:"timeRange"`
}

// ValidatedRequest 只由 Specification 生成，供后续业务 query service 消费。
type ValidatedRequest struct {
	Page      PageRequest   `json:"page"`
	Sort      []SortTerm    `json:"sort"`
	Filters   []FilterTerm  `json:"filters"`
	TimeRange *UTCTimeRange `json:"timeRange"`
}

// FilterField 冻结一个 endpoint 可接受的字段和操作符。
type FilterField struct {
	Field     string
	Operators []FilterOperator
}

// SpecificationConfig 是 endpoint 构造 immutable query contract 的输入。
type SpecificationConfig struct {
	DefaultLimit int
	MaxLimit     int
	MaxRangeDays int
	SortFields   []string
	FilterFields []FilterField
	DefaultSort  []SortTerm
	TieBreaker   SortTerm
}

// ResponseStatus 区分完整、部分和当前不可用，不把任一状态折成空集合。
type ResponseStatus string

const (
	ResponseComplete    ResponseStatus = "complete"
	ResponsePartial     ResponseStatus = "partial"
	ResponseUnavailable ResponseStatus = "unavailable"
)

// PageInfo 描述当前页和不透明下一页 cursor。
type PageInfo struct {
	Limit      int     `json:"limit"`
	HasMore    bool    `json:"hasMore"`
	NextCursor *string `json:"nextCursor"`
}

// Issue 是非 fatal partial/unavailable 说明，不携带底层 error text。
type Issue struct {
	Code       ErrorCode `json:"code"`
	MessageKey string    `json:"messageKey"`
	Retryable  bool      `json:"retryable"`
}

// ResponseMeta 是业务 response 组合的公共版本、状态和分页元数据。
type ResponseMeta struct {
	Version string         `json:"version"`
	Status  ResponseStatus `json:"status"`
	Page    *PageInfo      `json:"page"`
	Issues  []Issue        `json:"issues"`
}
