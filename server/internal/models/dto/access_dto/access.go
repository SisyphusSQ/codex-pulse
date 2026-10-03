package access_dto

const PurposeAdmin = "admin"
const PurposeCollector = "collector"

// Principal 只由统一鉴权结果构造，不能从业务 body 或身份 Header 生成。
type Principal struct {
	ID          string
	Purpose     string
	Name        string
	Origin      string
	ExpiresAtMS *int64
}

// PairedClient 是 service 交给可信 controller 的一次性交接，不写入业务事实。
type PairedClient struct {
	Principal   Principal
	Credential  string
	CSRF        string
	ExpiresAtMS *int64
}
