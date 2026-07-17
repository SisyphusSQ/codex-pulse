package usagecost

import (
	"bytes"
	"crypto/aes"
	"crypto/cipher"
	cryptorand "crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"

	basequery "github.com/SisyphusSQ/codex-pulse/internal/query"
	"github.com/SisyphusSQ/codex-pulse/internal/store"
)

const (
	sessionCursorEndpoint        = "sessions"
	sessionTurnCursorEndpoint    = "session-turns"
	projectCursorEndpoint        = "projects"
	projectSessionCursorEndpoint = "project-sessions"
	projectModelCursorEndpoint   = "project-models"
)

var sessionTurnCursorAssociatedData = []byte("query-v1/session-turns/aead-v1")

type sessionTurnCursorKey [32]byte

type projectDetailCursorKey [32]byte

type sessionTurnCursorPayload struct {
	Version     string `json:"version"`
	Endpoint    string `json:"endpoint"`
	SessionID   string `json:"sessionId"`
	TurnID      string `json:"turnId"`
	StartedAtMS int64  `json:"startedAtMs"`
}

type projectDetailCursorPayload struct {
	Version      string `json:"version"`
	Endpoint     string `json:"endpoint"`
	GenerationID string `json:"generationId"`
	DimensionKey string `json:"dimensionKey"`
	TimeZone     string `json:"timeZone"`
	StartAtMS    int64  `json:"startAtMs"`
	EndAtMS      int64  `json:"endAtMs"`
	Identity     string `json:"identity"`
	Null         bool   `json:"null"`
	NumericValue *int64 `json:"numericValue"`
}

type cursorUnsigned struct {
	Version   string                  `json:"version"`
	Endpoint  string                  `json:"endpoint"`
	SortField string                  `json:"sortField"`
	Direction basequery.SortDirection `json:"direction"`
	Null      bool                    `json:"null"`
	Value     *int64                  `json:"value"`
	TextValue *string                 `json:"textValue"`
	Identity  string                  `json:"identity"`
}

type cursorPayload struct {
	cursorUnsigned
	Checksum string `json:"checksum"`
}

func encodeSessionCursor(
	cursor *store.SessionAnalyticsCursor,
	sortField string,
	direction basequery.SortDirection,
) (*string, error) {
	if cursor == nil {
		return nil, nil
	}
	if cursor.SessionID == "" || cursor.Null == (cursor.Value != nil) ||
		(cursor.Value != nil && *cursor.Value < 0) {
		return nil, errors.New("store session cursor is invalid")
	}
	unsigned := cursorUnsigned{
		Version: basequery.ContractVersion, Endpoint: sessionCursorEndpoint,
		SortField: sortField, Direction: direction, Null: cursor.Null,
		Value: cloneInt64(cursor.Value), Identity: cursor.SessionID,
	}
	return encodeCursorUnsigned(unsigned)
}

func decodeSessionCursor(
	encoded string,
	sortField string,
	direction basequery.SortDirection,
) (*store.SessionAnalyticsCursor, error) {
	payload, err := decodeCursorPayload(encoded)
	if err != nil {
		return nil, err
	}
	if payload.Version != basequery.ContractVersion || payload.Endpoint != sessionCursorEndpoint ||
		payload.SortField != sortField || payload.Direction != direction ||
		payload.Identity == "" || payload.Null == (payload.Value != nil) ||
		payload.TextValue != nil || (payload.Value != nil && *payload.Value < 0) {
		return nil, basequery.NewValidationFailure("page.cursor", nil)
	}
	return &store.SessionAnalyticsCursor{
		SessionID: payload.Identity, Null: payload.Null, Value: cloneInt64(payload.Value),
	}, nil
}

func newSessionTurnCursorKey() (sessionTurnCursorKey, error) {
	var key sessionTurnCursorKey
	_, err := io.ReadFull(cryptorand.Reader, key[:])
	return key, err
}

func sessionTurnCursorAEAD(key sessionTurnCursorKey) (cipher.AEAD, error) {
	block, err := aes.NewCipher(key[:])
	if err != nil {
		return nil, err
	}
	return cipher.NewGCM(block)
}

func newProjectDetailCursorKey() (projectDetailCursorKey, error) {
	var key projectDetailCursorKey
	_, err := io.ReadFull(cryptorand.Reader, key[:])
	return key, err
}

func projectDetailCursorAEAD(key projectDetailCursorKey) (cipher.AEAD, error) {
	block, err := aes.NewCipher(key[:])
	if err != nil {
		return nil, err
	}
	return cipher.NewGCM(block)
}

func encodeProjectSessionCursor(
	key projectDetailCursorKey,
	cursor *store.ProjectSessionAnalyticsCursor,
	rangeValue basequery.UTCTimeRange,
) (*string, error) {
	if cursor == nil {
		return nil, nil
	}
	if cursor.GenerationID == "" || cursor.DimensionKey == "" ||
		cursor.SessionID == "" || cursor.LastActivityAtMS < 0 {
		return nil, errors.New("store project session cursor is invalid")
	}
	value := cursor.LastActivityAtMS
	return encodeProjectDetailCursor(key, projectDetailCursorPayload{
		Version: basequery.ContractVersion, Endpoint: projectSessionCursorEndpoint,
		GenerationID: cursor.GenerationID, DimensionKey: cursor.DimensionKey,
		TimeZone:  rangeValue.TimeZone,
		StartAtMS: rangeValue.StartAtMS, EndAtMS: rangeValue.EndAtMS,
		Identity: cursor.SessionID, NumericValue: &value,
	})
}

func decodeProjectSessionCursor(
	key projectDetailCursorKey,
	encoded string,
	dimensionKey string,
	rangeValue basequery.UTCTimeRange,
) (*store.ProjectSessionAnalyticsCursor, error) {
	payload, err := decodeProjectDetailCursor(
		key, encoded, projectSessionCursorEndpoint, "sessionPage.cursor",
	)
	if err != nil {
		return nil, err
	}
	if payload.GenerationID == "" ||
		!validProjectDetailCursorScope(payload, dimensionKey, rangeValue) || payload.Null ||
		payload.Identity == "" || payload.NumericValue == nil || *payload.NumericValue < 0 {
		return nil, basequery.NewValidationFailure("sessionPage.cursor", nil)
	}
	return &store.ProjectSessionAnalyticsCursor{
		GenerationID: payload.GenerationID, DimensionKey: dimensionKey,
		SessionID:        payload.Identity,
		LastActivityAtMS: *payload.NumericValue,
	}, nil
}

func encodeProjectModelCursor(
	key projectDetailCursorKey,
	cursor *store.ProjectModelAnalyticsCursor,
	rangeValue basequery.UTCTimeRange,
) (*string, error) {
	if cursor == nil {
		return nil, nil
	}
	if cursor.GenerationID == "" || cursor.DimensionKey == "" ||
		cursor.ModelDimensionKey == "" ||
		cursor.Null == (cursor.TotalTokens != nil) ||
		(cursor.TotalTokens != nil && *cursor.TotalTokens < 0) {
		return nil, errors.New("store project model cursor is invalid")
	}
	return encodeProjectDetailCursor(key, projectDetailCursorPayload{
		Version: basequery.ContractVersion, Endpoint: projectModelCursorEndpoint,
		GenerationID: cursor.GenerationID, DimensionKey: cursor.DimensionKey,
		TimeZone:  rangeValue.TimeZone,
		StartAtMS: rangeValue.StartAtMS, EndAtMS: rangeValue.EndAtMS,
		Identity: cursor.ModelDimensionKey, Null: cursor.Null,
		NumericValue: cloneInt64(cursor.TotalTokens),
	})
}

func decodeProjectModelCursor(
	key projectDetailCursorKey,
	encoded string,
	dimensionKey string,
	rangeValue basequery.UTCTimeRange,
) (*store.ProjectModelAnalyticsCursor, error) {
	payload, err := decodeProjectDetailCursor(
		key, encoded, projectModelCursorEndpoint, "modelPage.cursor",
	)
	if err != nil {
		return nil, err
	}
	if payload.GenerationID == "" ||
		!validProjectDetailCursorScope(payload, dimensionKey, rangeValue) ||
		payload.Identity == "" || payload.Null == (payload.NumericValue != nil) ||
		(payload.NumericValue != nil && *payload.NumericValue < 0) {
		return nil, basequery.NewValidationFailure("modelPage.cursor", nil)
	}
	return &store.ProjectModelAnalyticsCursor{
		GenerationID: payload.GenerationID, DimensionKey: dimensionKey,
		ModelDimensionKey: payload.Identity,
		Null:              payload.Null, TotalTokens: cloneInt64(payload.NumericValue),
	}, nil
}

func encodeProjectDetailCursor(
	key projectDetailCursorKey,
	payload projectDetailCursorPayload,
) (*string, error) {
	plaintext, err := json.Marshal(payload)
	if err != nil {
		return nil, err
	}
	aead, err := projectDetailCursorAEAD(key)
	if err != nil {
		return nil, err
	}
	nonce := make([]byte, aead.NonceSize())
	if _, err := io.ReadFull(cryptorand.Reader, nonce); err != nil {
		return nil, err
	}
	wire := aead.Seal(nonce, nonce, plaintext, projectDetailCursorAssociatedData(payload.Endpoint))
	encoded := base64.RawURLEncoding.EncodeToString(wire)
	return &encoded, nil
}

func decodeProjectDetailCursor(
	key projectDetailCursorKey,
	encoded string,
	endpoint string,
	field string,
) (projectDetailCursorPayload, error) {
	wire, err := base64.RawURLEncoding.DecodeString(encoded)
	if err != nil || len(wire) == 0 || len(wire) > 4096 {
		return projectDetailCursorPayload{}, basequery.NewValidationFailure(field, err)
	}
	aead, err := projectDetailCursorAEAD(key)
	if err != nil || len(wire) < aead.NonceSize()+aead.Overhead() {
		return projectDetailCursorPayload{}, basequery.NewValidationFailure(field, err)
	}
	nonce := wire[:aead.NonceSize()]
	plaintext, err := aead.Open(
		nil, nonce, wire[aead.NonceSize():], projectDetailCursorAssociatedData(endpoint),
	)
	if err != nil || len(plaintext) == 0 || len(plaintext) > 2048 {
		return projectDetailCursorPayload{}, basequery.NewValidationFailure(field, err)
	}
	decoder := json.NewDecoder(bytes.NewReader(plaintext))
	decoder.DisallowUnknownFields()
	var payload projectDetailCursorPayload
	if err := decoder.Decode(&payload); err != nil {
		return projectDetailCursorPayload{}, basequery.NewValidationFailure(field, err)
	}
	if err := ensureCursorJSONEnd(decoder); err != nil ||
		payload.Version != basequery.ContractVersion || payload.Endpoint != endpoint {
		return projectDetailCursorPayload{}, basequery.NewValidationFailure(field, err)
	}
	return payload, nil
}

func projectDetailCursorAssociatedData(endpoint string) []byte {
	return []byte("query-v1/" + endpoint + "/aead-v1")
}

func validProjectDetailCursorScope(
	payload projectDetailCursorPayload,
	dimensionKey string,
	rangeValue basequery.UTCTimeRange,
) bool {
	return payload.DimensionKey == dimensionKey && payload.TimeZone == rangeValue.TimeZone &&
		payload.StartAtMS == rangeValue.StartAtMS && payload.EndAtMS == rangeValue.EndAtMS
}

func encodeSessionTurnCursor(
	key sessionTurnCursorKey,
	cursor *store.SessionTurnAnalyticsCursor,
) (*string, error) {
	if cursor == nil {
		return nil, nil
	}
	if cursor.SessionID == "" || cursor.TurnID == "" || cursor.StartedAtMS < 0 {
		return nil, errors.New("store session turn cursor is invalid")
	}
	plaintext, err := json.Marshal(sessionTurnCursorPayload{
		Version: basequery.ContractVersion, Endpoint: sessionTurnCursorEndpoint,
		SessionID: cursor.SessionID, TurnID: cursor.TurnID, StartedAtMS: cursor.StartedAtMS,
	})
	if err != nil {
		return nil, err
	}
	aead, err := sessionTurnCursorAEAD(key)
	if err != nil {
		return nil, err
	}
	nonce := make([]byte, aead.NonceSize())
	if _, err := io.ReadFull(cryptorand.Reader, nonce); err != nil {
		return nil, err
	}
	wire := aead.Seal(nonce, nonce, plaintext, sessionTurnCursorAssociatedData)
	encoded := base64.RawURLEncoding.EncodeToString(wire)
	return &encoded, nil
}

func decodeSessionTurnCursor(
	key sessionTurnCursorKey,
	encoded string,
	sessionID string,
) (*store.SessionTurnAnalyticsCursor, error) {
	wire, err := base64.RawURLEncoding.DecodeString(encoded)
	if err != nil || len(wire) == 0 || len(wire) > 4096 {
		return nil, basequery.NewValidationFailure("turnPage.cursor", err)
	}
	aead, err := sessionTurnCursorAEAD(key)
	if err != nil || len(wire) < aead.NonceSize()+aead.Overhead() {
		return nil, basequery.NewValidationFailure("turnPage.cursor", err)
	}
	nonce := wire[:aead.NonceSize()]
	plaintext, err := aead.Open(
		nil, nonce, wire[aead.NonceSize():], sessionTurnCursorAssociatedData,
	)
	if err != nil || len(plaintext) == 0 || len(plaintext) > 2048 {
		return nil, basequery.NewValidationFailure("turnPage.cursor", err)
	}
	decoder := json.NewDecoder(bytes.NewReader(plaintext))
	decoder.DisallowUnknownFields()
	var payload sessionTurnCursorPayload
	if err := decoder.Decode(&payload); err != nil {
		return nil, basequery.NewValidationFailure("turnPage.cursor", err)
	}
	if err := ensureCursorJSONEnd(decoder); err != nil {
		return nil, basequery.NewValidationFailure("turnPage.cursor", err)
	}
	if payload.Version != basequery.ContractVersion ||
		payload.Endpoint != sessionTurnCursorEndpoint || payload.SessionID != sessionID ||
		payload.TurnID == "" || payload.StartedAtMS < 0 {
		return nil, basequery.NewValidationFailure("turnPage.cursor", nil)
	}
	return &store.SessionTurnAnalyticsCursor{
		SessionID: sessionID, TurnID: payload.TurnID, StartedAtMS: payload.StartedAtMS,
	}, nil
}

func encodeProjectCursor(
	cursor *store.ProjectAnalyticsCursor,
	sortField string,
	direction basequery.SortDirection,
) (*string, error) {
	if cursor == nil {
		return nil, nil
	}
	if cursor.DimensionKey == "" || !validProjectCursorValue(cursor, sortField) {
		return nil, errors.New("store project cursor is invalid")
	}
	return encodeCursorUnsigned(cursorUnsigned{
		Version: basequery.ContractVersion, Endpoint: projectCursorEndpoint,
		SortField: sortField, Direction: direction, Null: cursor.Null,
		Value: cloneInt64(cursor.NumericValue), TextValue: cloneStringPointer(cursor.TextValue),
		Identity: cursor.DimensionKey,
	})
}

func decodeProjectCursor(
	encoded string,
	sortField string,
	direction basequery.SortDirection,
) (*store.ProjectAnalyticsCursor, error) {
	payload, err := decodeCursorPayload(encoded)
	if err != nil {
		return nil, err
	}
	cursor := &store.ProjectAnalyticsCursor{
		DimensionKey: payload.Identity, Null: payload.Null,
		NumericValue: cloneInt64(payload.Value), TextValue: cloneStringPointer(payload.TextValue),
	}
	if payload.Version != basequery.ContractVersion || payload.Endpoint != projectCursorEndpoint ||
		payload.SortField != sortField || payload.Direction != direction ||
		payload.Identity == "" || !validProjectCursorValue(cursor, sortField) {
		return nil, basequery.NewValidationFailure("page.cursor", nil)
	}
	return cursor, nil
}

func validProjectCursorValue(cursor *store.ProjectAnalyticsCursor, sortField string) bool {
	if cursor.Null {
		return cursor.NumericValue == nil && cursor.TextValue == nil
	}
	if sortField == "displayName" {
		return cursor.NumericValue == nil && cursor.TextValue != nil && *cursor.TextValue != ""
	}
	return cursor.NumericValue != nil && *cursor.NumericValue >= 0 && cursor.TextValue == nil
}

func encodeCursorUnsigned(unsigned cursorUnsigned) (*string, error) {
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

func decodeCursorPayload(encoded string) (cursorPayload, error) {
	raw, err := base64.RawURLEncoding.DecodeString(encoded)
	if err != nil || len(raw) == 0 || len(raw) > 4096 {
		return cursorPayload{}, basequery.NewValidationFailure("page.cursor", err)
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	var payload cursorPayload
	if err := decoder.Decode(&payload); err != nil {
		return cursorPayload{}, basequery.NewValidationFailure("page.cursor", err)
	}
	if err := ensureCursorJSONEnd(decoder); err != nil {
		return cursorPayload{}, basequery.NewValidationFailure("page.cursor", err)
	}
	canonical, err := json.Marshal(payload.cursorUnsigned)
	if err != nil {
		return cursorPayload{}, basequery.NewValidationFailure("page.cursor", err)
	}
	expected := sha256.Sum256(canonical)
	actual, err := hex.DecodeString(payload.Checksum)
	if err != nil || len(actual) != sha256.Size ||
		subtle.ConstantTimeCompare(actual, expected[:]) != 1 {
		return cursorPayload{}, basequery.NewValidationFailure("page.cursor", err)
	}
	return payload, nil
}

func ensureCursorJSONEnd(decoder *json.Decoder) error {
	var extra any
	if err := decoder.Decode(&extra); !errors.Is(err, io.EOF) {
		if err == nil {
			return errors.New("cursor contains trailing JSON")
		}
		return err
	}
	return nil
}
