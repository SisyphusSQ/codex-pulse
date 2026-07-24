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
	if got.CodexHome.Source != onboarding.CodexHome || got.CodexHome.Generation != 1 ||
		got.CodexHome.DataStoreKey != DefaultDataStoreKey {
		t.Fatalf("CodexHome = %#v, want source=%#v generation=1 data-store=%q",
			got.CodexHome, onboarding.CodexHome, DefaultDataStoreKey)
	}
	if got.Online != (OnlinePreferences{QuotaEnabled: true, ResetCreditsEnabled: false}) {
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
		"locale":      func(value *Snapshot) { value.UI.Locale = "en-US" },
		"launch":      func(value *Snapshot) { value.UI.LaunchBehavior = "hidden" },
		"range":       func(value *Snapshot) { value.UI.OverviewRange = "year" },
	}
	for name, mutate := range tests {
		name, mutate := name, mutate
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			value := base
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
		`{"schema_version":3}`,
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
		"current duplicate": bytes.Replace(current, []byte(`"schema_version": 2,`),
			[]byte(`"schema_version": 2, "schema_version": 2,`), 1),
		"current root case alias": replaceJSONField(t, current, "Online", map[string]any{
			"quota_enabled": true, "reset_credits_enabled": false,
		}),
		"current missing object":       removeJSONField(t, current, "online"),
		"current null object":          replaceJSONField(t, current, "online", nil),
		"current nested case alias":    replaceNestedJSONField(t, current, "online", "Quota_Enabled", false),
		"current missing bool":         removeNestedJSONField(t, current, "online", "quota_enabled"),
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
