package quota

import (
	"context"
	"fmt"
	"sort"
	"strings"

	"github.com/SisyphusSQ/codex-pulse/internal/codex/appserver"
	"github.com/SisyphusSQ/codex-pulse/internal/store"
)

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

type ResetCreditsClient struct {
	base *Client
}

func NewResetCreditsClient(config ClientConfig) (*ResetCreditsClient, error) {
	base, err := newRateLimitsClient(config, resetReadExcludeDetails)
	if err != nil {
		return nil, err
	}
	return &ResetCreditsClient{base: base}, nil
}

func (client *ResetCreditsClient) Fetch(ctx context.Context, request BoundRefreshRequest) (ResetCreditsResult, error) {
	if client == nil || client.base == nil || !validBoundRefreshRequest(request) {
		return ResetCreditsResult{}, ErrInvalidClientConfig
	}
	if ctx == nil {
		ctx = context.Background()
	}
	result := ResetCreditsResult{StartedAtMS: client.base.now().UnixMilli()}
	if err := ctx.Err(); err != nil {
		return client.finish(result, store.SourceFailureCancelled), nil
	}
	quotaResult := Result{StartedAtMS: result.StartedAtMS}
	snapshot, err := client.base.readBoundSnapshot(ctx, request, &quotaResult)
	result.AttemptCount = quotaResult.AttemptCount
	result.FinishedAtMS = quotaResult.FinishedAtMS
	if err != nil {
		return ResetCreditsResult{}, err
	}
	if quotaResult.Failure != nil {
		result.Failure = quotaResult.Failure
		if result.FinishedAtMS == 0 {
			result.FinishedAtMS = client.base.finishedAtMS(result.StartedAtMS)
		}
		return result, nil
	}
	finishedAtMS := client.base.finishedAtMS(result.StartedAtMS)
	decoded, ok := snapshotFromAppServerResetCredits(snapshot, request, finishedAtMS)
	result.FinishedAtMS = finishedAtMS
	if !ok {
		result.Failure = &Failure{Code: store.SourceFailureSchemaIncompatible}
		return result, nil
	}
	result.Snapshot = &decoded
	digest := resetCreditsTypedDigest(decoded)
	result.PayloadSHA256 = &digest
	return result, nil
}

func (client *ResetCreditsClient) finish(result ResetCreditsResult, code store.SourceFailureCode) ResetCreditsResult {
	result.FinishedAtMS = client.base.finishedAtMS(result.StartedAtMS)
	result.Snapshot = nil
	result.Failure = &Failure{Code: code}
	return result
}

func snapshotFromAppServerResetCredits(
	snapshot appserver.AccountRateLimitsSnapshot,
	request BoundRefreshRequest,
	observedAtMS int64,
) (store.ResetCreditsSnapshot, bool) {
	if snapshot.RateLimitResetCredits == nil {
		return store.ResetCreditsSnapshot{}, false
	}
	summary := snapshot.RateLimitResetCredits
	if summary.AvailableCount < 0 || summary.AvailableCount > storeMaxResetCreditsAvailableCount() {
		return store.ResetCreditsSnapshot{}, false
	}
	decoded := store.ResetCreditsSnapshot{
		RequestID: request.RequestID, AccountScope: request.Binding.AccountScope,
		AvailableCount: summary.AvailableCount, ObservedAtMS: observedAtMS,
	}
	if summary.Credits == nil {
		decoded.DetailsStatus = store.ResetCreditDetailsUnavailable
	} else {
		if len(summary.Credits) > 100 {
			return store.ResetCreditsSnapshot{}, false
		}
		credits := make([]store.ResetCredit, 0, len(summary.Credits))
		seen := make(map[string]struct{}, len(summary.Credits))
		for _, credit := range summary.Credits {
			item, ok := storeResetCreditFromAppServer(credit)
			if !ok {
				return store.ResetCreditsSnapshot{}, false
			}
			digest := item.CreditIDHash.String()
			if _, duplicate := seen[digest]; duplicate {
				return store.ResetCreditsSnapshot{}, false
			}
			seen[digest] = struct{}{}
			credits = append(credits, item)
		}
		sort.Slice(credits, func(left, right int) bool {
			return credits[left].CreditIDHash.String() < credits[right].CreditIDHash.String()
		})
		decoded.Credits = credits
		available := int64(0)
		for _, credit := range credits {
			if credit.Status == store.ResetCreditAvailable {
				available++
			}
		}
		if available < summary.AvailableCount {
			decoded.DetailsStatus = store.ResetCreditDetailsPartial
		} else {
			decoded.DetailsStatus = store.ResetCreditDetailsComplete
		}
	}
	identity := fmt.Sprintf(
		"reset-credits-app-server\x00%s\x00%d\x00%s\x00%d",
		request.Binding.AccountScope, request.Binding.BindingGeneration, request.RequestID, observedAtMS,
	)
	decoded.SnapshotID = "reset-credits-app-server-" + store.SHA256DigestOf([]byte(identity)).String()
	return decoded, true
}

func storeResetCreditFromAppServer(credit appserver.RateLimitResetCredit) (store.ResetCredit, bool) {
	if credit.ID == "" {
		return store.ResetCredit{}, false
	}
	status := store.ResetCreditStatus(credit.Status)
	switch status {
	case store.ResetCreditAvailable, store.ResetCreditRedeeming, store.ResetCreditUnknown:
	case store.ResetCreditRedeemed, store.ResetCreditUsed:
		status = store.ResetCreditUnknown
	default:
		status = store.ResetCreditUnknown
	}
	resetType := store.ResetCreditTypeUnknown
	if credit.ResetType == "codexRateLimits" {
		resetType = store.ResetCreditTypeCodexRateLimits
	}
	grantedAtMS := credit.GrantedAtSeconds * 1000
	var expiresAtMS *int64
	if credit.ExpiresAtSeconds != nil {
		value := *credit.ExpiresAtSeconds * 1000
		expiresAtMS = &value
	}
	hashed := store.SHA256DigestOf([]byte(credit.ID))
	return store.ResetCredit{
		CreditIDHash: hashed, Status: status, Type: resetType,
		GrantedAtMS: grantedAtMS, ExpiresAtMS: expiresAtMS,
	}, true
}

func resetCreditsTypedDigest(snapshot store.ResetCreditsSnapshot) store.SHA256Digest {
	var builder strings.Builder
	fmt.Fprintf(
		&builder, "reset-credits\x00%s\x00%s\x00%d\x00%s\x00%d",
		snapshot.AccountScope, snapshot.RequestID, snapshot.AvailableCount, snapshot.DetailsStatus, snapshot.ObservedAtMS,
	)
	for _, credit := range snapshot.Credits {
		fmt.Fprintf(
			&builder, "\x00%s\x00%s\x00%s\x00%d",
			credit.CreditIDHash.String(), credit.Status, credit.Type, credit.GrantedAtMS,
		)
	}
	return store.SHA256DigestOf([]byte(builder.String()))
}

func storeMaxResetCreditsAvailableCount() int64 {
	return 1_000_000
}
