package statistics_dto

// SourceMetadata 用于来源说明，查询时不传输 Session 完整正文。
type SourceMetadata struct {
	ID            string
	SessionKey    string
	ClientID      string
	CollectedAtMS int64
	Revision      int64
	SourceKind    string
	Complete      bool
	Deleted       bool
}
