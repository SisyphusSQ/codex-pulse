package quota

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"net/http"
	"sort"
	"strings"
	"time"

	"github.com/SisyphusSQ/codex-pulse/internal/runtimeclock"
	"github.com/SisyphusSQ/codex-pulse/internal/store"
)

const WhamResetCreditsEndpoint = "https://chatgpt.com/backend-api/wham/rate-limit-reset-credits"

type ResetCreditsClientConfig = ClientConfig

type ResetCreditsResult struct {
	Snapshot      *store.ResetCreditsSnapshot
	Failure       *Failure
	AttemptCount  int64
	HTTPStatus    *int64
	ResponseBytes int64
	PayloadSHA256 *store.SHA256Digest
	StartedAtMS   int64
	FinishedAtMS  int64
}

// ResetCreditsClient reuses the hardened Wham transport configuration while
// keeping Reset Credits decoding and result types independent from quota
// window observations.
type ResetCreditsClient struct {
	base *Client
}

func NewResetCreditsClient(config ResetCreditsClientConfig) (*ResetCreditsClient, error) {
	base, err := NewClient(ClientConfig(config))
	if err != nil {
		return nil, err
	}
	return &ResetCreditsClient{base: base}, nil
}

func (client *ResetCreditsClient) Fetch(ctx context.Context, requestID string) (ResetCreditsResult, error) {
	if client == nil || client.base == nil || requestID == "" || len(requestID) > 512 {
		return ResetCreditsResult{}, ErrInvalidClientConfig
	}
	if ctx == nil {
		ctx = context.Background()
	}
	result := ResetCreditsResult{StartedAtMS: client.base.now().UnixMilli()}
	if err := ctx.Err(); err != nil {
		return client.finish(result, store.SourceFailureCancelled, nil), nil
	}

	for nextAttempt := 1; nextAttempt <= client.base.maxAttempts; nextAttempt++ {
		result.HTTPStatus = nil
		response, requestErr, attempted := client.do(ctx, requestID)
		if attempted {
			result.AttemptCount++
		}
		if !attempted && errors.Is(requestErr, ErrCredentialUnavailable) {
			return client.finish(result, store.SourceFailureAuthRequired, nil), nil
		}
		if requestErr != nil {
			if response != nil {
				result.ResponseBytes += discardResponseBody(response.Body, client.base.maxResponseBytes)
			}
			code := classifyRequestError(ctx, requestErr)
			if code == store.SourceFailureCancelled {
				return client.finish(result, code, nil), nil
			}
			if nextAttempt < client.base.maxAttempts {
				shouldRetry, waitErr := client.base.retry(ctx, nextAttempt)
				if waitErr != nil {
					return client.finish(result, classifyRequestError(ctx, waitErr), nil), nil
				}
				if shouldRetry {
					continue
				}
			}
			return client.finish(result, code, nil), nil
		}
		if response == nil {
			return client.finish(result, store.SourceFailureNetworkUnavailable, nil), nil
		}

		status := int64(response.StatusCode)
		result.HTTPStatus = &status
		if response.StatusCode != http.StatusOK {
			result.ResponseBytes += discardResponseBody(response.Body, client.base.maxResponseBytes)
			finishedAtMS := client.finishedAtMS(result.StartedAtMS)
			code, retryAtMS := classifyHTTPStatus(response.StatusCode, response.Header, time.UnixMilli(finishedAtMS))
			if code == store.SourceFailureServerError && nextAttempt < client.base.maxAttempts {
				shouldRetry, waitErr := client.base.retry(ctx, nextAttempt)
				if waitErr != nil {
					result.HTTPStatus = nil
					return client.finish(result, classifyRequestError(ctx, waitErr), nil), nil
				}
				if shouldRetry {
					continue
				}
			}
			return client.finishAt(result, code, retryAtMS, finishedAtMS), nil
		}
		if !isJSONContentType(response.Header.Get("Content-Type")) {
			result.ResponseBytes += discardResponseBody(response.Body, client.base.maxResponseBytes)
			return client.finish(result, store.SourceFailureSchemaIncompatible, nil), nil
		}
		body, bytesRead, oversized, readErr := readResponseBody(response.Body, client.base.maxResponseBytes)
		result.ResponseBytes += bytesRead
		if readErr != nil {
			result.HTTPStatus = nil
			code := classifyRequestError(ctx, readErr)
			if code == store.SourceFailureCancelled {
				return client.finish(result, code, nil), nil
			}
			if nextAttempt < client.base.maxAttempts {
				shouldRetry, waitErr := client.base.retry(ctx, nextAttempt)
				if waitErr != nil {
					return client.finish(result, classifyRequestError(ctx, waitErr), nil), nil
				}
				if shouldRetry {
					continue
				}
			}
			return client.finish(result, code, nil), nil
		}
		if oversized {
			return client.finish(result, store.SourceFailureSchemaIncompatible, nil), nil
		}
		digest := store.SHA256DigestOf(body)
		result.PayloadSHA256 = &digest
		finishedAtMS := client.finishedAtMS(result.StartedAtMS)
		snapshot, valid := decodeResetCredits(body, requestID, finishedAtMS)
		result.FinishedAtMS = finishedAtMS
		if !valid {
			result.Failure = &Failure{Code: store.SourceFailureSchemaIncompatible}
			return result, nil
		}
		result.Snapshot = &snapshot
		return result, nil
	}
	return client.finish(result, store.SourceFailureNetworkUnavailable, nil), nil
}

func (client *ResetCreditsClient) do(
	ctx context.Context,
	requestID string,
) (*http.Response, error, bool) {
	requestContext, cancel := context.WithTimeout(ctx, client.base.timeout)
	request, err := http.NewRequestWithContext(requestContext, http.MethodGet, WhamResetCreditsEndpoint, nil)
	if err != nil {
		cancel()
		return nil, err, false
	}
	request.Header.Set("Accept", "application/json")
	request.Header.Set("Cache-Control", "no-store")
	request.Header.Set("X-Request-ID", requestID)
	var response *http.Response
	attempted := false
	err = client.base.credentials.WithAccessToken(requestContext, func(token []byte) error {
		attempted = true
		request.Header.Set("Authorization", "Bearer "+string(token))
		defer request.Header.Del("Authorization")
		var requestErr error
		response, requestErr = client.base.httpClient.Do(request)
		return requestErr
	})
	if err != nil || response == nil {
		cancel()
		return response, err, attempted
	}
	if response.Body == nil {
		response.Body = http.NoBody
	}
	response.Body = &cancelOnCloseBody{ReadCloser: response.Body, cancel: cancel}
	return response, err, attempted
}

func (client *ResetCreditsClient) finish(
	result ResetCreditsResult,
	code store.SourceFailureCode,
	retryAtMS *int64,
) ResetCreditsResult {
	return client.finishAt(result, code, retryAtMS, client.finishedAtMS(result.StartedAtMS))
}

func (client *ResetCreditsClient) finishAt(
	result ResetCreditsResult,
	code store.SourceFailureCode,
	retryAtMS *int64,
	finishedAtMS int64,
) ResetCreditsResult {
	result.FinishedAtMS = finishedAtMS
	result.Snapshot = nil
	result.Failure = &Failure{Code: code, RetryAtMS: cloneInt64(retryAtMS)}
	return result
}

func (client *ResetCreditsClient) finishedAtMS(startedAtMS int64) int64 {
	finishedAtMS := client.base.now().UnixMilli()
	if finishedAtMS < startedAtMS {
		return startedAtMS
	}
	return finishedAtMS
}

func decodeResetCredits(
	content []byte,
	requestID string,
	observedAtMS int64,
) (store.ResetCreditsSnapshot, bool) {
	if validateUniqueJSONKeys(content) != nil {
		return store.ResetCreditsSnapshot{}, false
	}
	envelope, valid := decodeJSONObject(content)
	if !valid {
		return store.ResetCreditsSnapshot{}, false
	}
	var availableCount int64
	if !decodeRequiredScalar(envelope["available_count"], &availableCount) || availableCount < 0 ||
		availableCount > 100 {
		return store.ResetCreditsSnapshot{}, false
	}
	var rawCredits []json.RawMessage
	if !decodeRequiredScalar(envelope["credits"], &rawCredits) || len(rawCredits) > 100 {
		return store.ResetCreditsSnapshot{}, false
	}
	credits := make([]store.ResetCredit, 0, len(rawCredits))
	seen := make(map[string]struct{}, len(rawCredits))
	available := int64(0)
	for _, raw := range rawCredits {
		credit, ok := decodeResetCredit(raw, observedAtMS)
		if !ok {
			return store.ResetCreditsSnapshot{}, false
		}
		digest := credit.CreditIDHash.String()
		if _, duplicate := seen[digest]; duplicate {
			return store.ResetCreditsSnapshot{}, false
		}
		seen[digest] = struct{}{}
		if credit.Status == store.ResetCreditAvailable {
			available++
		}
		credits = append(credits, credit)
	}
	if available != availableCount {
		return store.ResetCreditsSnapshot{}, false
	}
	sort.Slice(credits, func(left, right int) bool {
		return credits[left].CreditIDHash.String() < credits[right].CreditIDHash.String()
	})
	identity := fmt.Sprintf("reset-credits-wham\x00%s\x00%d", requestID, observedAtMS)
	return store.ResetCreditsSnapshot{
		SnapshotID: "reset-credits-wham-" + store.SHA256DigestOf([]byte(identity)).String(),
		RequestID:  requestID, AccountScope: store.QuotaAccountScopeDefault,
		AvailableCount: availableCount, ObservedAtMS: observedAtMS, Credits: credits,
	}, true
}

func decodeResetCredit(raw json.RawMessage, observedAtMS int64) (store.ResetCredit, bool) {
	wire, valid := decodeJSONObject(raw)
	if !valid {
		return store.ResetCredit{}, false
	}
	var id, statusValue string
	if !decodeRequiredScalar(wire["id"], &id) || !decodeRequiredScalar(wire["status"], &statusValue) {
		return store.ResetCredit{}, false
	}
	id = strings.TrimSpace(id)
	if id == "" || len(id) > 512 {
		return store.ResetCredit{}, false
	}
	status := store.ResetCreditStatus(strings.ToLower(strings.TrimSpace(statusValue)))
	if status != store.ResetCreditAvailable && status != store.ResetCreditRedeemed &&
		status != store.ResetCreditExpired && status != store.ResetCreditUsed {
		return store.ResetCredit{}, false
	}
	resetType := store.ResetCreditTypeUnknown
	typeRaw := wire["reset_type"]
	if len(typeRaw) == 0 || isNullJSON(typeRaw) {
		typeRaw = wire["type"]
	}
	if len(typeRaw) > 0 && !isNullJSON(typeRaw) {
		var value string
		if !decodeRequiredScalar(typeRaw, &value) {
			return store.ResetCredit{}, false
		}
		if strings.EqualFold(strings.TrimSpace(value), string(store.ResetCreditTypeCodexRateLimits)) {
			resetType = store.ResetCreditTypeCodexRateLimits
		}
	}
	grantedRaw := wire["granted_at"]
	if len(grantedRaw) == 0 || isNullJSON(grantedRaw) {
		grantedRaw = wire["created_at"]
	}
	grantedAtMS, ok := decodeResetCreditTime(grantedRaw)
	if !ok {
		return store.ResetCredit{}, false
	}
	expiresAtMS, ok := decodeResetCreditTime(wire["expires_at"])
	if !ok || expiresAtMS < grantedAtMS {
		return store.ResetCredit{}, false
	}
	var redeemedAtMS *int64
	if rawRedeemed := wire["redeemed_at"]; len(rawRedeemed) > 0 && !isNullJSON(rawRedeemed) {
		value, valid := decodeResetCreditTime(rawRedeemed)
		if !valid || value < grantedAtMS || value > expiresAtMS {
			return store.ResetCredit{}, false
		}
		redeemedAtMS = &value
	}
	if status == store.ResetCreditAvailable && (redeemedAtMS != nil || expiresAtMS <= observedAtMS) {
		return store.ResetCredit{}, false
	}
	if (status == store.ResetCreditRedeemed || status == store.ResetCreditUsed) && redeemedAtMS == nil {
		return store.ResetCredit{}, false
	}
	return store.ResetCredit{
		CreditIDHash: store.SHA256DigestOf([]byte(id)), Status: status, Type: resetType,
		GrantedAtMS: grantedAtMS, ExpiresAtMS: expiresAtMS, RedeemedAtMS: redeemedAtMS,
	}, true
}

func decodeResetCreditTime(raw json.RawMessage) (int64, bool) {
	var value string
	if !decodeRequiredScalar(raw, &value) {
		return 0, false
	}
	parsed, err := time.Parse(time.RFC3339Nano, strings.TrimSpace(value))
	if err != nil {
		return 0, false
	}
	result := parsed.UnixMilli()
	return result, result >= 0 && result <= runtimeclock.MaxTimestampMS && result != math.MaxInt64
}
