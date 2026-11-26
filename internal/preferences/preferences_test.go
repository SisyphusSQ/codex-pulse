package preferences

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestDefaultUIPreferencesUsesQuotaWeek(t *testing.T) {
	if got := DefaultUIPreferences().OverviewRange; got != OverviewRange("quota_week") {
		t.Fatalf("default overview range = %q, want quota_week", got)
	}
}

func TestFileStoreConfirmCreatesCurrentTypedPreferences(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "private", "preferences.json")
	store, err := NewFileStore(path)
	if err != nil {
		t.Fatalf("NewFileStore() error = %v", err)
	}
	onboarding := validSnapshot(filepath.Join(t.TempDir(), "codex-home"))
	if err := store.Confirm(context.Background(), onboarding); err != nil {
		t.Fatalf("Confirm() error = %v", err)
	}

	got, err := store.LoadPreferences(context.Background())
	if err != nil {
		t.Fatalf("LoadPreferences() error = %v", err)
	}
	if got.SchemaVersion != CurrentPreferencesSchemaVersion || got.Revision != 1 {
		t.Fatalf("schema/revision = %d/%d, want %d/1", got.SchemaVersion, got.Revision, CurrentPreferencesSchemaVersion)
	}
	if got.Onboarding.Version != CurrentOnboardingVersion || !got.Onboarding.Completed {
		t.Fatalf("onboarding = %#v", got.Onboarding)
	}
	if got.CodexHome == nil || got.CodexHome.Source != onboarding.CodexHome || got.CodexHome.Generation != 1 ||
		got.CodexHome.DataStoreKey != DefaultDataStoreKey {
		t.Fatalf("CodexHome = %#v, want source=%#v generation=1 data-store=%q",
			got.CodexHome, onboarding.CodexHome, DefaultDataStoreKey)
	}
	if got.Providers != DefaultProviderPreferences() {
		t.Fatalf("Providers = %#v", got.Providers)
	}
	if got.CodexAccounts != DefaultCodexAccountPreferences() {
		t.Fatalf("CodexAccounts = %#v", got.CodexAccounts)
	}
	if got.Online != (OnlinePreferences{
		QuotaEnabled: true, ResetCreditsEnabled: false, CursorOnlineEnabled: true,
		GrokQuotaEnabled: true, GrokAutoRefreshEnabled: true, AGYOnlineEnabled: true,
	}) {
		t.Fatalf("Online = %#v", got.Online)
	}
	if got.Refresh != DefaultRefreshPreferences() || got.Updates != DefaultUpdatePreferences() ||
		got.UI != DefaultUIPreferences() || got.PendingSwitch != nil || got.LastSwitch != nil {
		t.Fatalf("defaults = %#v", got)
	}
	if err := validatePreferences(got); err != nil {
		t.Fatalf("validatePreferences(current) error = %v", err)
	}

	content, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("os.ReadFile() error = %v", err)
	}
	var envelope struct {
		SchemaVersion int `json:"schema_version"`
	}
	if err := json.Unmarshal(content, &envelope); err != nil {
		t.Fatalf("json.Unmarshal() error = %v", err)
	}
	if envelope.SchemaVersion != CurrentPreferencesSchemaVersion {
		t.Fatalf("persisted schema = %d, want %d", envelope.SchemaVersion, CurrentPreferencesSchemaVersion)
	}
}

func TestV2PreferencesMigrateMissingGrokAutoRefreshToEnabled(t *testing.T) {
	t.Parallel()
	value, err := preferencesFromOnboarding(validSnapshot(filepath.Join(t.TempDir(), "current-home")))
	if err != nil {
		t.Fatalf("preferencesFromOnboarding() error = %v", err)
	}
	v2 := v2Snapshot{
		SchemaVersion: preferencesSchemaV2, Revision: value.Revision, Onboarding: value.Onboarding,
		CodexHome: *value.CodexHome, Online: v2OnlinePreferences{
			QuotaEnabled: value.Online.QuotaEnabled, ResetCreditsEnabled: value.Online.ResetCreditsEnabled,
		},
		Refresh: value.Refresh, Updates: cloneUpdatePreferences(value.Updates), UI: value.UI,
	}
	content, err := json.Marshal(v2)
	if err != nil {
		t.Fatalf("json.Marshal(v2) error = %v", err)
	}
	decoded, migrated, err := decodePreferences(content)
	if err != nil {
		t.Fatalf("decodePreferences() error = %v", err)
	}
	if !migrated {
		t.Fatal("v2 preferences did not report a schema migration")
	}
	if decoded.SchemaVersion != CurrentPreferencesSchemaVersion ||
		decoded.Providers != DefaultProviderPreferences() ||
		decoded.CodexAccounts != DefaultCodexAccountPreferences() ||
		!decoded.Online.GrokAutoRefreshEnabled || !decoded.Online.CursorOnlineEnabled ||
		!decoded.Online.AGYOnlineEnabled {
		t.Fatalf("migrated = %#v", decoded)
	}
}

func TestV3PreferencesMigrateQuotaHistoryRetentionToEnabled(t *testing.T) {
	t.Parallel()
	value, err := preferencesFromOnboarding(validSnapshot(filepath.Join(t.TempDir(), "current-home")))
	if err != nil {
		t.Fatalf("preferencesFromOnboarding() error = %v", err)
	}
	v3 := v3Snapshot{
		SchemaVersion: preferencesSchemaV3,
		Revision:      value.Revision,
		Onboarding:    value.Onboarding,
		CodexHome:     CloneCodexHome(value.CodexHome),
		Providers:     value.Providers,
		Online:        value.Online,
		Refresh:       value.Refresh,
		Updates:       cloneUpdatePreferences(value.Updates),
		UI:            value.UI,
	}
	content, err := json.Marshal(v3)
	if err != nil {
		t.Fatalf("json.Marshal(v3) error = %v", err)
	}
	content = removeNestedJSONField(t, content, "providers", "dsh")
	content = removeNestedJSONField(t, content, "providers", "agy")
	content = removeNestedJSONField(t, content, "online", "agy_online_enabled")
	decoded, migrated, err := decodePreferences(content)
	if err != nil {
		t.Fatalf("decodePreferences() error = %v", err)
	}
	if !migrated || decoded.SchemaVersion != CurrentPreferencesSchemaVersion ||
		decoded.CodexAccounts != DefaultCodexAccountPreferences() ||
		decoded.Providers.DSH.Intent != ProviderIntentAuto ||
		decoded.Providers.AGY.Intent != ProviderIntentAuto ||
		!decoded.Online.AGYOnlineEnabled {
		t.Fatalf("migrated = %#v, didMigrate = %t", decoded, migrated)
	}
}

func TestV4AndV5PreferencesMigrateKeepingExistingSettingsAndAddingAGYDefaults(t *testing.T) {
	t.Parallel()
	base, err := preferencesFromOnboarding(validSnapshot(filepath.Join(t.TempDir(), "legacy-typed-home")))
	if err != nil {
		t.Fatalf("preferencesFromOnboarding() error = %v", err)
	}
	base.Revision = 7
	base.Providers = ProviderPreferences{
		Codex:  ProviderPreference{Intent: ProviderIntentDisabled},
		Cursor: ProviderPreference{Intent: ProviderIntentEnabled},
		Grok:   ProviderPreference{Intent: ProviderIntentDisabled},
		DSH:    ProviderPreference{Intent: ProviderIntentEnabled},
		AGY:    ProviderPreference{Intent: ProviderIntentDisabled},
	}
	base.Online = OnlinePreferences{
		QuotaEnabled:           false,
		ResetCreditsEnabled:    true,
		CursorOnlineEnabled:    false,
		GrokQuotaEnabled:       false,
		GrokAutoRefreshEnabled: false,
		AGYOnlineEnabled:       false,
	}
	v6Bytes, err := marshalPreferences(base)
	if err != nil {
		t.Fatalf("marshalPreferences() error = %v", err)
	}

	// v4 文档：无 dsh、无 agy、无 agy_online_enabled
	v4Bytes := replaceJSONField(t, v6Bytes, "schema_version", preferencesSchemaV4)
	v4Bytes = removeNestedJSONField(t, v4Bytes, "providers", "dsh")
	v4Bytes = removeNestedJSONField(t, v4Bytes, "providers", "agy")
	v4Bytes = removeNestedJSONField(t, v4Bytes, "online", "agy_online_enabled")

	decodedV4, migratedV4, err := decodePreferences(v4Bytes)
	if err != nil {
		t.Fatalf("decodePreferences(v4) error = %v", err)
	}
	if !migratedV4 || decodedV4.SchemaVersion != CurrentPreferencesSchemaVersion || decodedV4.Revision != 7 ||
		decodedV4.Providers.Codex.Intent != ProviderIntentDisabled ||
		decodedV4.Providers.Cursor.Intent != ProviderIntentEnabled ||
		decodedV4.Providers.Grok.Intent != ProviderIntentDisabled ||
		decodedV4.Providers.DSH.Intent != ProviderIntentAuto ||
		decodedV4.Providers.AGY.Intent != ProviderIntentAuto ||
		decodedV4.Online.QuotaEnabled || !decodedV4.Online.ResetCreditsEnabled ||
		decodedV4.Online.CursorOnlineEnabled || decodedV4.Online.GrokQuotaEnabled ||
		decodedV4.Online.GrokAutoRefreshEnabled || !decodedV4.Online.AGYOnlineEnabled {
		t.Fatalf("migrated v4 = %#v, migrated=%t", decodedV4, migratedV4)
	}

	// v5 文档：保留 dsh，无 agy、无 agy_online_enabled
	v5Bytes := replaceJSONField(t, v6Bytes, "schema_version", preferencesSchemaV5)
	v5Bytes = removeNestedJSONField(t, v5Bytes, "providers", "agy")
	v5Bytes = removeNestedJSONField(t, v5Bytes, "online", "agy_online_enabled")

	decodedV5, migratedV5, err := decodePreferences(v5Bytes)
	if err != nil {
		t.Fatalf("decodePreferences(v5) error = %v", err)
	}
	if !migratedV5 || decodedV5.SchemaVersion != CurrentPreferencesSchemaVersion || decodedV5.Revision != 7 ||
		decodedV5.Providers.Codex.Intent != ProviderIntentDisabled ||
		decodedV5.Providers.Cursor.Intent != ProviderIntentEnabled ||
		decodedV5.Providers.Grok.Intent != ProviderIntentDisabled ||
		decodedV5.Providers.DSH.Intent != ProviderIntentEnabled ||
		decodedV5.Providers.AGY.Intent != ProviderIntentAuto ||
		decodedV5.Online.QuotaEnabled || !decodedV5.Online.ResetCreditsEnabled ||
		decodedV5.Online.CursorOnlineEnabled || decodedV5.Online.GrokQuotaEnabled ||
		decodedV5.Online.GrokAutoRefreshEnabled || !decodedV5.Online.AGYOnlineEnabled {
		t.Fatalf("migrated v5 = %#v, migrated=%t", decodedV5, migratedV5)
	}

	// v5 文档若夹带 v6 字段必须拒绝
	v5WithAGYProvider := replaceNestedJSONField(t, v5Bytes, "providers", "agy", map[string]any{"intent": "auto"})
	if _, _, err := decodePreferences(v5WithAGYProvider); !errors.Is(err, ErrInvalidPreferences) {
		t.Fatalf("decodePreferences(v5 with agy provider) error = %v, want ErrInvalidPreferences", err)
	}
	v5WithAGYOnline := replaceNestedJSONField(t, v5Bytes, "online", "agy_online_enabled", true)
	if _, _, err := decodePreferences(v5WithAGYOnline); !errors.Is(err, ErrInvalidPreferences) {
		t.Fatalf("decodePreferences(v5 with agy_online_enabled) error = %v, want ErrInvalidPreferences", err)
	}

	// 磁盘端到端迁移：v5 落盘后 LoadPreferences 原子升至 v6 且重开幂等
	root := t.TempDir()
	if err := os.Chmod(root, 0o700); err != nil {
		t.Fatalf("os.Chmod(root) error = %v", err)
	}
	path := filepath.Join(root, "preferences.json")
	if err := os.WriteFile(path, append(v5Bytes, '\n'), 0o600); err != nil {
		t.Fatalf("os.WriteFile(v5) error = %v", err)
	}
	store, err := NewFileStore(path)
	if err != nil {
		t.Fatalf("NewFileStore() error = %v", err)
	}
	loaded, err := store.LoadPreferences(context.Background())
	if err != nil {
		t.Fatalf("LoadPreferences(v5) error = %v", err)
	}
	if !reflect.DeepEqual(loaded, decodedV5) {
		t.Fatalf("disk migrated v5 = %#v, want %#v", loaded, decodedV5)
	}
	firstBytes, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("os.ReadFile() error = %v", err)
	}
	reopened, err := NewFileStore(path)
	if err != nil {
		t.Fatalf("NewFileStore(reopen) error = %v", err)
	}
	reloaded, err := reopened.LoadPreferences(context.Background())
	if err != nil || !reflect.DeepEqual(reloaded, loaded) {
		t.Fatalf("reloaded = %#v, %v, want %#v", reloaded, err, loaded)
	}
	secondBytes, err := os.ReadFile(path)
	if err != nil || !bytes.Equal(firstBytes, secondBytes) {
		t.Fatalf("reopening migrated v6 rewrote bytes")
	}
}

func TestFileStoreInitializePreferencesWithoutCodexHome(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "private", "preferences.json")
	store, err := NewFileStore(path)
	if err != nil {
		t.Fatalf("NewFileStore() error = %v", err)
	}
	snapshot, err := NewCurrentSnapshot(nil, DefaultOnlinePreferences())
	if err != nil {
		t.Fatalf("NewCurrentSnapshot() error = %v", err)
	}
	if err := store.InitializePreferences(context.Background(), snapshot); err != nil {
		t.Fatalf("InitializePreferences() error = %v", err)
	}
	got, err := store.LoadPreferences(context.Background())
	if err != nil {
		t.Fatalf("LoadPreferences() error = %v", err)
	}
	if got.Revision != 1 || got.SchemaVersion != CurrentPreferencesSchemaVersion || got.CodexHome != nil ||
		got.Providers != DefaultProviderPreferences() || !got.Onboarding.Completed {
		t.Fatalf("initialized = %#v", got)
	}
	content, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("os.ReadFile() error = %v", err)
	}
	var root map[string]json.RawMessage
	if err := json.Unmarshal(content, &root); err != nil {
		t.Fatalf("json.Unmarshal() error = %v", err)
	}
	if _, exists := root["codex_home"]; exists {
		t.Fatalf("omitted Home was persisted: %s", content)
	}
}

func TestFileStoreMigratesV2AtomicallyKeepingExplicitSettings(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	if err := os.Chmod(root, 0o700); err != nil {
		t.Fatalf("os.Chmod(root) error = %v", err)
	}
	path := filepath.Join(root, "preferences.json")
	onboarding := validSnapshot(filepath.Join(t.TempDir(), "v2-home"))
	current, err := preferencesFromOnboarding(onboarding)
	if err != nil {
		t.Fatalf("preferencesFromOnboarding() error = %v", err)
	}
	v2 := v2Snapshot{
		SchemaVersion: preferencesSchemaV2, Revision: 4, Onboarding: current.Onboarding,
		CodexHome: *current.CodexHome,
		Online: v2OnlinePreferences{
			QuotaEnabled: false, ResetCreditsEnabled: true,
			GrokQuotaEnabled: boolPointer(false), GrokAutoRefreshEnabled: boolPointer(false),
		},
		Refresh: current.Refresh, Updates: cloneUpdatePreferences(current.Updates), UI: current.UI,
	}
	content, err := json.MarshalIndent(v2, "", "  ")
	if err != nil {
		t.Fatalf("json.MarshalIndent() error = %v", err)
	}
	if err := os.WriteFile(path, append(content, '\n'), 0o600); err != nil {
		t.Fatalf("os.WriteFile(v2) error = %v", err)
	}
	store, err := NewFileStore(path)
	if err != nil {
		t.Fatalf("NewFileStore() error = %v", err)
	}
	migrated, err := store.LoadPreferences(context.Background())
	if err != nil {
		t.Fatalf("LoadPreferences(v2) error = %v", err)
	}
	if migrated.SchemaVersion != CurrentPreferencesSchemaVersion || migrated.Revision != 4 ||
		migrated.CodexHome == nil || *migrated.CodexHome != v2.CodexHome ||
		migrated.Providers != DefaultProviderPreferences() ||
		migrated.Online.QuotaEnabled || !migrated.Online.ResetCreditsEnabled ||
		migrated.Online.GrokQuotaEnabled || migrated.Online.GrokAutoRefreshEnabled ||
		!migrated.Online.CursorOnlineEnabled {
		t.Fatalf("migrated = %#v", migrated)
	}
}

func TestDecodePreferencesRejectsExplicitNullCodexHomeAndUnknownProviderIntent(t *testing.T) {
	t.Parallel()

	value, err := preferencesFromOnboarding(validSnapshot(filepath.Join(t.TempDir(), "home")))
	if err != nil {
		t.Fatalf("preferencesFromOnboarding() error = %v", err)
	}
	content, err := marshalPreferences(value)
	if err != nil {
		t.Fatalf("marshalPreferences() error = %v", err)
	}
	tests := map[string][]byte{
		"null home":        replaceJSONField(t, content, "codex_home", nil),
		"unknown provider": replaceNestedJSONField(t, content, "providers", "claude", map[string]any{"intent": "auto"}),
		"unknown intent": mutateNestedJSONField(t, content, "providers", func(object map[string]any) {
			object["codex"] = map[string]any{"intent": "maybe"}
		}),
		"missing providers": removeJSONField(t, content, "providers"),
	}
	for name, encoded := range tests {
		name, encoded := name, encoded
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			if _, _, err := decodePreferences(encoded); !errors.Is(err, ErrInvalidPreferences) {
				t.Fatalf("decodePreferences(%s) error = %v, want ErrInvalidPreferences", name, err)
			}
		})
	}
}

func TestExplicitProviderIntentRoundTripSurvivesReload(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "private", "preferences.json")
	store, err := NewFileStore(path)
	if err != nil {
		t.Fatalf("NewFileStore() error = %v", err)
	}
	if err := store.Confirm(context.Background(), validSnapshot(filepath.Join(t.TempDir(), "home"))); err != nil {
		t.Fatalf("Confirm() error = %v", err)
	}
	current, err := store.LoadPreferences(context.Background())
	if err != nil {
		t.Fatalf("LoadPreferences() error = %v", err)
	}
	updated := cloneSnapshot(current)
	updated.Revision++
	updated.Providers = ProviderPreferences{
		Codex:  ProviderPreference{Intent: ProviderIntentDisabled},
		Cursor: ProviderPreference{Intent: ProviderIntentEnabled},
		Grok:   ProviderPreference{Intent: ProviderIntentAuto},
		DSH:    ProviderPreference{Intent: ProviderIntentAuto},
		AGY:    ProviderPreference{Intent: ProviderIntentDisabled},
	}
	if err := store.CompareAndSwap(context.Background(), current.Revision, updated); err != nil {
		t.Fatalf("CompareAndSwap() error = %v", err)
	}
	reopened, err := NewFileStore(path)
	if err != nil {
		t.Fatalf("NewFileStore(reopen) error = %v", err)
	}
	got, err := reopened.LoadPreferences(context.Background())
	if err != nil || got.Providers != updated.Providers {
		t.Fatalf("reload providers = %#v, %v, want %#v", got.Providers, err, updated.Providers)
	}
}

func boolPointer(value bool) *bool {
	copied := value
	return &copied
}

func TestFileStoreMigratesLegacyV1AtomicallyAndIdempotently(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	if err := os.Chmod(root, 0o700); err != nil {
		t.Fatalf("os.Chmod(root) error = %v", err)
	}
	path := filepath.Join(root, "preferences.json")
	legacy := validSnapshot(filepath.Join(t.TempDir(), "legacy-home"))
	legacyContent, err := marshalSnapshot(legacy)
	if err != nil {
		t.Fatalf("marshalSnapshot() error = %v", err)
	}
	if err := os.WriteFile(path, legacyContent, 0o600); err != nil {
		t.Fatalf("os.WriteFile(legacy) error = %v", err)
	}

	store, err := NewFileStore(path)
	if err != nil {
		t.Fatalf("NewFileStore() error = %v", err)
	}
	migrated, err := store.LoadPreferences(context.Background())
	if err != nil {
		t.Fatalf("LoadPreferences(migrate) error = %v", err)
	}
	want, err := preferencesFromOnboarding(legacy)
	if err != nil {
		t.Fatalf("preferencesFromOnboarding() error = %v", err)
	}
	if !reflect.DeepEqual(migrated, want) {
		t.Fatalf("migrated = %#v, want %#v", migrated, want)
	}
	firstContent, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("os.ReadFile(migrated) error = %v", err)
	}
	if string(firstContent) == string(legacyContent) {
		t.Fatal("legacy bytes were not migrated")
	}

	reopened, err := NewFileStore(path)
	if err != nil {
		t.Fatalf("NewFileStore(reopen) error = %v", err)
	}
	second, err := reopened.LoadPreferences(context.Background())
	if err != nil || !reflect.DeepEqual(second, migrated) {
		t.Fatalf("LoadPreferences(reopen) = %#v, %v, want %#v", second, err, migrated)
	}
	secondContent, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("os.ReadFile(reopen) error = %v", err)
	}
	if string(secondContent) != string(firstContent) {
		t.Fatal("reopening current preferences rewrote bytes")
	}
}

func TestFileStoreCompareAndSwapRejectsStaleRevision(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "private", "preferences.json")
	first, err := NewFileStore(path)
	if err != nil {
		t.Fatalf("NewFileStore(first) error = %v", err)
	}
	second, err := NewFileStore(path)
	if err != nil {
		t.Fatalf("NewFileStore(second) error = %v", err)
	}
	if err := first.Confirm(context.Background(), validSnapshot(filepath.Join(t.TempDir(), "home"))); err != nil {
		t.Fatalf("Confirm() error = %v", err)
	}

	base, err := first.LoadPreferences(context.Background())
	if err != nil {
		t.Fatalf("LoadPreferences() error = %v", err)
	}
	updated := base
	updated.Revision++
	updated.Online.QuotaEnabled = false
	if err := first.CompareAndSwap(context.Background(), base.Revision, updated); err != nil {
		t.Fatalf("CompareAndSwap(first) error = %v", err)
	}
	stale := base
	stale.Revision++
	stale.UI.OverviewRange = OverviewRangeThirtyDays
	if err := second.CompareAndSwap(context.Background(), base.Revision, stale); !errors.Is(err, ErrPreferencesConflict) {
		t.Fatalf("CompareAndSwap(stale) error = %v, want ErrPreferencesConflict", err)
	}

	got, err := second.LoadPreferences(context.Background())
	if err != nil {
		t.Fatalf("LoadPreferences(after conflict) error = %v", err)
	}
	if !reflect.DeepEqual(got, updated) {
		t.Fatalf("preferences after conflict = %#v, want %#v", got, updated)
	}
}

func TestValidatePreferencesRejectsInvalidTypedFields(t *testing.T) {
	t.Parallel()

	base, err := preferencesFromOnboarding(validSnapshot(filepath.Join(t.TempDir(), "home")))
	if err != nil {
		t.Fatalf("preferencesFromOnboarding() error = %v", err)
	}
	tests := map[string]func(*Snapshot){
		"schema":      func(value *Snapshot) { value.SchemaVersion++ },
		"revision":    func(value *Snapshot) { value.Revision = 0 },
		"generation":  func(value *Snapshot) { value.CodexHome.Generation = 0 },
		"data store":  func(value *Snapshot) { value.CodexHome.DataStoreKey = "../unsafe" },
		"long device": func(value *Snapshot) { value.CodexHome.Source.DeviceID = strings.Repeat("d", 257) },
		"quota range": func(value *Snapshot) { value.Refresh.QuotaIntervalSeconds = 59 },
		"debounce":    func(value *Snapshot) { value.Refresh.JSONLDebounceMilliseconds = 5001 },
		"download":    func(value *Snapshot) { value.Updates.AutoDownloadEnabled = true },
		"channel":     func(value *Snapshot) { value.Updates.Channel = "nightly" },
		"locale":      func(value *Snapshot) { value.UI.Locale = "fr-FR" },
		"launch":      func(value *Snapshot) { value.UI.LaunchBehavior = "hidden" },
		"range":       func(value *Snapshot) { value.UI.OverviewRange = "year" },
	}
	for name, mutate := range tests {
		name, mutate := name, mutate
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			value := cloneSnapshot(base)
			mutate(&value)
			if err := validatePreferences(value); !errors.Is(err, ErrInvalidPreferences) {
				t.Fatalf("validatePreferences(%s) error = %v, want ErrInvalidPreferences", name, err)
			}
		})
	}
}

func TestMarshalPreferencesRejectsSnapshotOverFileLimit(t *testing.T) {
	t.Parallel()

	value, err := preferencesFromOnboarding(validSnapshot(filepath.Join(t.TempDir(), "home")))
	if err != nil {
		t.Fatalf("preferencesFromOnboarding() error = %v", err)
	}
	value = cloneSnapshot(value)
	value.Updates.SkippedVersion = nil
	value.CodexHome.Source.Path = string(filepath.Separator) + strings.Repeat("a", maximumPreferencesBytes)
	if _, err := marshalPreferences(value); !errors.Is(err, ErrInvalidPreferences) {
		t.Fatalf("marshalPreferences(oversized) error = %v, want ErrInvalidPreferences", err)
	}
}

func TestDecodePreferencesRejectsUnknownSchemaVersion(t *testing.T) {
	t.Parallel()

	for _, content := range []string{
		`{"schema_version":0}`,
		`{"schema_version":4}`,
		`{"schema_version":7}`,
	} {
		if _, _, err := decodePreferences([]byte(content)); !errors.Is(err, ErrInvalidPreferences) {
			t.Fatalf("decodePreferences(%s) error = %v, want ErrInvalidPreferences", content, err)
		}
	}
}

func TestDecodePreferencesRejectsDuplicateMissingAndNullRequiredFields(t *testing.T) {
	t.Parallel()

	legacy, err := marshalSnapshot(validSnapshot(filepath.Join(t.TempDir(), "legacy-home")))
	if err != nil {
		t.Fatalf("marshalSnapshot() error = %v", err)
	}
	currentValue, err := preferencesFromOnboarding(validSnapshot(filepath.Join(t.TempDir(), "current-home")))
	if err != nil {
		t.Fatalf("preferencesFromOnboarding() error = %v", err)
	}
	current, err := marshalPreferences(currentValue)
	if err != nil {
		t.Fatalf("marshalPreferences() error = %v", err)
	}

	tests := map[string][]byte{
		"legacy duplicate": bytes.Replace(legacy, []byte(`"schema_version": 1,`),
			[]byte(`"schema_version": 1, "schema_version": 1,`), 1),
		"legacy case alias":   replaceJSONField(t, legacy, "Online_Quota_Enabled", false),
		"legacy missing bool": removeJSONField(t, legacy, "online_quota_enabled"),
		"legacy null bool":    replaceJSONField(t, legacy, "online_quota_enabled", nil),
		"current duplicate": bytes.Replace(current, []byte(`"schema_version": 6,`),
			[]byte(`"schema_version": 6, "schema_version": 6,`), 1),
		"current root case alias": replaceJSONField(t, current, "Online", map[string]any{
			"quota_enabled": true, "reset_credits_enabled": false,
		}),
		"current missing object":       removeJSONField(t, current, "online"),
		"current null object":          replaceJSONField(t, current, "online", nil),
		"current nested case alias":    replaceNestedJSONField(t, current, "online", "Quota_Enabled", false),
		"current missing bool":         removeNestedJSONField(t, current, "online", "quota_enabled"),
		"current missing agy online":   removeNestedJSONField(t, current, "online", "agy_online_enabled"),
		"current null agy online":      replaceNestedJSONField(t, current, "online", "agy_online_enabled", nil),
		"current missing agy provider": removeNestedJSONField(t, current, "providers", "agy"),
		"current null bool":            replaceNestedJSONField(t, current, "online", "quota_enabled", nil),
		"current optional root null":   replaceJSONField(t, current, "detached_homes", nil),
		"current optional nested null": replaceNestedJSONField(t, current, "updates", "skipped_version", nil),
	}
	for name, content := range tests {
		name, content := name, content
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			if _, _, err := decodePreferences(content); !errors.Is(err, ErrInvalidPreferences) {
				t.Fatalf("decodePreferences() error = %v, want ErrInvalidPreferences; content=%s", err, content)
			}
		})
	}
}

func removeJSONField(t *testing.T, content []byte, field string) []byte {
	t.Helper()
	var object map[string]any
	if err := json.Unmarshal(content, &object); err != nil {
		t.Fatalf("json.Unmarshal() error = %v", err)
	}
	delete(object, field)
	result, err := json.Marshal(object)
	if err != nil {
		t.Fatalf("json.Marshal() error = %v", err)
	}
	return result
}

func replaceJSONField(t *testing.T, content []byte, field string, value any) []byte {
	t.Helper()
	var object map[string]any
	if err := json.Unmarshal(content, &object); err != nil {
		t.Fatalf("json.Unmarshal() error = %v", err)
	}
	object[field] = value
	result, err := json.Marshal(object)
	if err != nil {
		t.Fatalf("json.Marshal() error = %v", err)
	}
	return result
}

func removeNestedJSONField(t *testing.T, content []byte, objectField, field string) []byte {
	t.Helper()
	return mutateNestedJSONField(t, content, objectField, func(object map[string]any) {
		delete(object, field)
	})
}

func replaceNestedJSONField(t *testing.T, content []byte, objectField, field string, value any) []byte {
	t.Helper()
	return mutateNestedJSONField(t, content, objectField, func(object map[string]any) {
		object[field] = value
	})
}

func mutateNestedJSONField(
	t *testing.T,
	content []byte,
	objectField string,
	mutate func(map[string]any),
) []byte {
	t.Helper()
	var root map[string]any
	if err := json.Unmarshal(content, &root); err != nil {
		t.Fatalf("json.Unmarshal() error = %v", err)
	}
	object, ok := root[objectField].(map[string]any)
	if !ok {
		t.Fatalf("field %q is not an object", objectField)
	}
	mutate(object)
	result, err := json.Marshal(root)
	if err != nil {
		t.Fatalf("json.Marshal() error = %v", err)
	}
	return result
}
