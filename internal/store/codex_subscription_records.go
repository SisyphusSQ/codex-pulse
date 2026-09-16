package store

import "github.com/SisyphusSQ/codex-pulse/internal/codex/subscriptionaccounts"

type CodexAccountFence struct {
	AccountScope      string
	BindingGeneration int64
}

type CodexSubscriptionDetectedAccount = subscriptionaccounts.DetectedAccount
type CodexSubscriptionRecords = subscriptionaccounts.Records
type CodexSubscriptionMutation = subscriptionaccounts.Mutation

type DetectedCodexSubscriptionProfile struct {
	Email        *string
	Automatic    subscriptionaccounts.AutomaticPlanFact
	ObservedAtMS int64
}

type CodexSubscriptionManualCreate = subscriptionaccounts.CreateRequest
type CodexSubscriptionManualUpdate = subscriptionaccounts.UpdateRequest
type CodexSubscriptionDeleteRequest = subscriptionaccounts.DeleteRequest
type CodexSubscriptionLinkRequest = subscriptionaccounts.LinkRequest
type CodexSubscriptionUnlinkRequest = subscriptionaccounts.UnlinkRequest
