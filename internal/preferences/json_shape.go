package preferences

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
)

func validateLegacyJSONShape(content []byte) error {
	if err := validateJSONDocument(content); err != nil {
		return err
	}
	root, err := decodeExactObject(content,
		[]string{
			"schema_version", "onboarding_version", "onboarding_completed", "codex_home",
			"online_quota_enabled", "reset_credits_enabled",
		},
		nil,
	)
	if err != nil {
		return err
	}
	_, err = decodeObjectField(root, "codex_home", "path", "device_id", "inode", "confirmed_at_ms")
	return err
}

func validateCurrentJSONShape(content []byte) error {
	if err := validateJSONDocument(content); err != nil {
		return err
	}
	root, err := decodeExactObject(content,
		[]string{
			"schema_version", "revision", "onboarding", "codex_home", "online", "refresh", "updates", "ui",
		},
		[]string{"detached_homes", "pending_switch", "pending_resume", "last_switch"},
	)
	if err != nil {
		return err
	}
	if _, err := decodeObjectField(root, "onboarding", "version", "completed"); err != nil {
		return err
	}
	if err := validateCodexHomeJSON(root["codex_home"]); err != nil {
		return err
	}
	if _, err := decodeObjectField(root, "online", "quota_enabled", "reset_credits_enabled"); err != nil {
		return err
	}
	if _, err := decodeObjectField(root, "refresh",
		"quota_interval_seconds", "reset_credits_interval_seconds", "reconcile_interval_seconds",
		"jsonl_debounce_milliseconds",
	); err != nil {
		return err
	}
	updates, err := decodeObjectFieldExact(root, "updates",
		[]string{"auto_check_enabled", "auto_download_enabled", "channel", "check_interval_seconds"},
		[]string{"skipped_version", "snooze_until_ms", "last_check_at_ms"},
	)
	if err != nil {
		return err
	}
	if hasNullField(updates, "skipped_version", "snooze_until_ms", "last_check_at_ms") {
		return ErrInvalidPreferences
	}
	if _, err := decodeObjectField(root, "ui", "locale", "launch_behavior", "overview_range"); err != nil {
		return err
	}
	if raw, exists := root["detached_homes"]; exists {
		if isJSONNull(raw) {
			return ErrInvalidPreferences
		}
		var homes []json.RawMessage
		if err := json.Unmarshal(raw, &homes); err != nil {
			return ErrInvalidPreferences
		}
		for _, home := range homes {
			if err := validateCodexHomeJSON(home); err != nil {
				return err
			}
		}
	}
	if raw, exists := root["pending_switch"]; exists {
		journal, err := decodeRequiredObject(raw,
			"switch_id", "attempt_id", "previous", "target", "strategy", "started_at_ms",
		)
		if err != nil {
			return err
		}
		if err := validateCodexHomeJSON(journal["previous"]); err != nil {
			return err
		}
		if err := validateCodexHomeJSON(journal["target"]); err != nil {
			return err
		}
	}
	if raw, exists := root["pending_resume"]; exists {
		if _, err := decodeRequiredObject(raw,
			"switch_id", "attempt_id", "generation", "target_generation", "strategy", "started_at_ms",
		); err != nil {
			return err
		}
	}
	if raw, exists := root["last_switch"]; exists {
		if _, err := decodeRequiredObject(raw,
			"switch_id", "from_generation", "to_generation", "strategy", "outcome", "finished_at_ms",
		); err != nil {
			return err
		}
	}
	return nil
}

func validateCodexHomeJSON(raw json.RawMessage) error {
	home, err := decodeRequiredObject(raw, "source", "generation", "data_store_key")
	if err != nil {
		return err
	}
	_, err = decodeObjectField(home, "source", "path", "device_id", "inode", "confirmed_at_ms")
	return err
}

func validateJSONDocument(content []byte) error {
	decoder := json.NewDecoder(bytes.NewReader(content))
	if err := scanJSONValue(decoder); err != nil {
		return ErrInvalidPreferences
	}
	if _, err := decoder.Token(); !errors.Is(err, io.EOF) {
		return ErrInvalidPreferences
	}
	return nil
}

func scanJSONValue(decoder *json.Decoder) error {
	token, err := decoder.Token()
	if err != nil {
		return err
	}
	delimiter, structured := token.(json.Delim)
	if !structured {
		return nil
	}
	switch delimiter {
	case '{':
		seen := make(map[string]struct{})
		for decoder.More() {
			keyToken, err := decoder.Token()
			if err != nil {
				return err
			}
			key, ok := keyToken.(string)
			if !ok {
				return ErrInvalidPreferences
			}
			if _, exists := seen[key]; exists {
				return ErrInvalidPreferences
			}
			seen[key] = struct{}{}
			if err := scanJSONValue(decoder); err != nil {
				return err
			}
		}
		end, err := decoder.Token()
		if err != nil || end != json.Delim('}') {
			return ErrInvalidPreferences
		}
	case '[':
		for decoder.More() {
			if err := scanJSONValue(decoder); err != nil {
				return err
			}
		}
		end, err := decoder.Token()
		if err != nil || end != json.Delim(']') {
			return ErrInvalidPreferences
		}
	default:
		return ErrInvalidPreferences
	}
	return nil
}

func decodeObjectField(
	object map[string]json.RawMessage,
	field string,
	required ...string,
) (map[string]json.RawMessage, error) {
	raw, exists := object[field]
	if !exists {
		return nil, ErrInvalidPreferences
	}
	return decodeRequiredObject(raw, required...)
}

func decodeObjectFieldExact(
	object map[string]json.RawMessage,
	field string,
	required []string,
	optional []string,
) (map[string]json.RawMessage, error) {
	raw, exists := object[field]
	if !exists {
		return nil, ErrInvalidPreferences
	}
	return decodeExactObject(raw, required, optional)
}

func decodeRequiredObject(content []byte, required ...string) (map[string]json.RawMessage, error) {
	return decodeExactObject(content, required, nil)
}

func decodeExactObject(
	content []byte,
	required []string,
	optional []string,
) (map[string]json.RawMessage, error) {
	if isJSONNull(content) {
		return nil, ErrInvalidPreferences
	}
	var object map[string]json.RawMessage
	if err := json.Unmarshal(content, &object); err != nil || object == nil {
		return nil, ErrInvalidPreferences
	}
	allowed := make(map[string]struct{}, len(required)+len(optional))
	for _, field := range required {
		allowed[field] = struct{}{}
		raw, exists := object[field]
		if !exists || isJSONNull(raw) {
			return nil, ErrInvalidPreferences
		}
	}
	for _, field := range optional {
		allowed[field] = struct{}{}
	}
	for field, raw := range object {
		if _, exists := allowed[field]; !exists || isJSONNull(raw) {
			return nil, ErrInvalidPreferences
		}
	}
	return object, nil
}

func hasNullField(object map[string]json.RawMessage, fields ...string) bool {
	for _, field := range fields {
		if raw, exists := object[field]; exists && isJSONNull(raw) {
			return true
		}
	}
	return false
}

func isJSONNull(content []byte) bool {
	return bytes.Equal(bytes.TrimSpace(content), []byte("null"))
}
