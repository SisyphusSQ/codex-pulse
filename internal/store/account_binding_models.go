package store

type codexAccountScopeKeyModel struct {
	SingletonID int64  `gorm:"column:singleton_id;primaryKey"`
	KeyBytes    []byte `gorm:"column:key_bytes"`
	CreatedAtMS int64  `gorm:"column:created_at_ms"`
}

func (codexAccountScopeKeyModel) TableName() string { return "codex_account_scope_key" }

type codexAccountScopeModel struct {
	AccountScope  string `gorm:"column:account_scope;primaryKey"`
	FirstSeenAtMS int64  `gorm:"column:first_seen_at_ms"`
	LastSeenAtMS  int64  `gorm:"column:last_seen_at_ms"`
}

func (codexAccountScopeModel) TableName() string { return "codex_account_scopes" }

type codexAccountBindingModel struct {
	SingletonID        int64   `gorm:"column:singleton_id;primaryKey"`
	State              string  `gorm:"column:state"`
	AccountScope       *string `gorm:"column:account_scope"`
	LastConfirmedScope *string `gorm:"column:last_confirmed_scope"`
	BindingGeneration  int64   `gorm:"column:binding_generation"`
	ObservedAtMS       int64   `gorm:"column:observed_at_ms"`
	Reason             string  `gorm:"column:reason"`
}

func (codexAccountBindingModel) TableName() string { return "codex_account_binding" }

type sourceRefreshGlobalFenceModel struct {
	SourceGroup string `gorm:"column:source_group;primaryKey"`
	NotBeforeMS int64  `gorm:"column:not_before_ms"`
	Reason      string `gorm:"column:reason"`
	UpdatedAtMS int64  `gorm:"column:updated_at_ms"`
}

func (sourceRefreshGlobalFenceModel) TableName() string { return "source_refresh_global_fences" }

type storedCodexAccountBinding struct {
	CodexAccountBinding
	LastConfirmedScope *string
	Present            bool
}
