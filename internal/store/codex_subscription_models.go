package store

import "github.com/SisyphusSQ/codex-pulse/internal/codex/subscriptionaccounts"

type codexSubscriptionDetectedAccountModel subscriptionaccounts.DetectedAccount

func (codexSubscriptionDetectedAccountModel) TableName() string {
	return "codex_subscription_detected_accounts"
}

type codexSubscriptionManualEntryModel subscriptionaccounts.ManualEntry

func (codexSubscriptionManualEntryModel) TableName() string {
	return "codex_subscription_manual_entries"
}

type codexSubscriptionLinkModel subscriptionaccounts.Link

func (codexSubscriptionLinkModel) TableName() string {
	return "codex_subscription_links"
}
