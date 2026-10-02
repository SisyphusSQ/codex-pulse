package catalog_dto

// ObservedModel 是已接收事实中的公开模型名投影，不读取来源payload或内容。
type ObservedModel struct {
	Provider string
	Model    string
}

// PriceEvidence 投影实际已接受的历史费率，不推断官方核对时间或来源。
type PriceEvidence struct {
	Provider        string
	Model           string
	PricingVersion  string
	InputPrice      *int64
	CachedPrice     *int64
	CacheWritePrice *int64
	OutputPrice     *int64
}
