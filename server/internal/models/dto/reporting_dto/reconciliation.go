package reporting_dto

// CursorReconciliation 是一个已完成分页的结果，Next 可用于中断后续跑。
type CursorReconciliation struct {
	Next      string `json:"next"`
	Processed int    `json:"processed"`
	Changed   int    `json:"changed"`
	Applied   bool   `json:"applied"`
}
