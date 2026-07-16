package runtimeinfo

import (
	"bytes"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"

	basequery "github.com/SisyphusSQ/codex-pulse/internal/query"
)

type cursorUnsigned struct {
	Version   string                  `json:"version"`
	Endpoint  string                  `json:"endpoint"`
	SortField string                  `json:"sortField"`
	Direction basequery.SortDirection `json:"direction"`
	Value     int64                   `json:"value"`
	Identity  string                  `json:"identity"`
}

type cursorPayload struct {
	cursorUnsigned
	Checksum string `json:"checksum"`
}

func encodeCursor(
	endpoint string,
	sortField string,
	direction basequery.SortDirection,
	value int64,
	identity string,
) (*string, error) {
	if value < 0 || identity == "" {
		return nil, errors.New("runtime cursor is invalid")
	}
	unsigned := cursorUnsigned{
		Version: basequery.ContractVersion, Endpoint: endpoint, SortField: sortField,
		Direction: direction, Value: value, Identity: identity,
	}
	canonical, err := json.Marshal(unsigned)
	if err != nil {
		return nil, err
	}
	digest := sha256.Sum256(canonical)
	payload, err := json.Marshal(cursorPayload{
		cursorUnsigned: unsigned, Checksum: hex.EncodeToString(digest[:]),
	})
	if err != nil {
		return nil, err
	}
	encoded := base64.RawURLEncoding.EncodeToString(payload)
	return &encoded, nil
}

func decodeCursor(
	encoded string,
	endpoint string,
	sortField string,
	direction basequery.SortDirection,
) (int64, string, error) {
	raw, err := base64.RawURLEncoding.DecodeString(encoded)
	if err != nil || len(raw) == 0 || len(raw) > 4096 {
		return 0, "", basequery.NewValidationFailure("page.cursor", err)
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	var payload cursorPayload
	if err := decoder.Decode(&payload); err != nil {
		return 0, "", basequery.NewValidationFailure("page.cursor", err)
	}
	var extra any
	if err := decoder.Decode(&extra); !errors.Is(err, io.EOF) {
		return 0, "", basequery.NewValidationFailure("page.cursor", err)
	}
	canonical, err := json.Marshal(payload.cursorUnsigned)
	if err != nil {
		return 0, "", basequery.NewValidationFailure("page.cursor", err)
	}
	expected := sha256.Sum256(canonical)
	actual, err := hex.DecodeString(payload.Checksum)
	if err != nil || len(actual) != sha256.Size ||
		subtle.ConstantTimeCompare(actual, expected[:]) != 1 ||
		payload.Version != basequery.ContractVersion || payload.Endpoint != endpoint ||
		payload.SortField != sortField || payload.Direction != direction ||
		payload.Value < 0 || payload.Identity == "" {
		return 0, "", basequery.NewValidationFailure("page.cursor", err)
	}
	return payload.Value, payload.Identity, nil
}
