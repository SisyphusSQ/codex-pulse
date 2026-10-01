package vo

type BaseListReq struct {
	Page     int `json:"page" query:"page"`
	PageSize int `json:"pageSize" query:"pageSize"`
}
