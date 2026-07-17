package health

import "github.com/SisyphusSQ/codex-pulse/internal/store"

type EventDescriptor struct {
	Component      Component
	Rule           Reason
	Impact         Impact
	Protection     Protection
	RecoveryAction RecoveryAction
}

// DescribeEvent 将持久层有限 code 映射为稳定、content-free 的产品语义。
func DescribeEvent(domain store.HealthDomain, code store.HealthCode) (EventDescriptor, bool) {
	for _, value := range managedRules {
		if value.domain == domain && value.code == code {
			return EventDescriptor{
				Component: value.component, Rule: value.reason, Impact: value.impact,
				Protection: value.protection, RecoveryAction: value.action,
			}, true
		}
	}
	descriptor := func(
		component Component,
		rule Reason,
		impact Impact,
		protection Protection,
		action RecoveryAction,
	) (EventDescriptor, bool) {
		return EventDescriptor{
			Component: component, Rule: rule, Impact: impact,
			Protection: protection, RecoveryAction: action,
		}, true
	}
	switch domain {
	case store.HealthDomainSource:
		switch code {
		case store.HealthCodeSourceTimeout:
			return descriptor(ComponentOnlineQuota, ReasonSourceTimeout, ImpactOnlineQuotaUnavailable, ProtectionRetryBackoff, RecoveryRetry)
		case store.HealthCodeSourcePermission:
			return descriptor(ComponentOnlineQuota, ReasonSourcePermission, ImpactOnlineQuotaUnavailable, ProtectionRetryBackoff, RecoveryGrantPermission)
		case store.HealthCodeSourceCorrupt:
			return descriptor(ComponentLocalIndex, ReasonSourceCorrupt, ImpactIndexingStopped, ProtectionWritesStopped, RecoveryRepairStore)
		case store.HealthCodeSourceStale:
			return descriptor(ComponentHistoryBackfill, ReasonSourceStale, ImpactHistoryIncomplete, ProtectionObservationOnly, RecoveryCheckSource)
		}
	case store.HealthDomainJob:
		switch code {
		case store.HealthCodeJobInterrupted:
			return descriptor(ComponentHistoryBackfill, ReasonJobInterrupted, ImpactHistoryIncomplete, ProtectionRetryBackoff, RecoveryRetry)
		case store.HealthCodeJobFailed:
			return descriptor(ComponentHistoryBackfill, ReasonJobFailed, ImpactHistoryIncomplete, ProtectionRetryBackoff, RecoveryRetry)
		case store.HealthCodeJobCancelled:
			return descriptor(ComponentHistoryBackfill, ReasonJobCancelled, ImpactHistoryIncomplete, ProtectionNone, RecoveryNone)
		}
	case store.HealthDomainStore:
		switch code {
		case store.HealthCodeStoreBusy:
			return descriptor(ComponentStorage, ReasonStoreBusy, ImpactStorageAtRisk, ProtectionRetryBackoff, RecoveryRetry)
		case store.HealthCodeStoreDiskFull:
			return descriptor(ComponentStorage, ReasonStoreDiskFull, ImpactStorageAtRisk, ProtectionWritesStopped, RecoveryFreeSpace)
		case store.HealthCodeStoreReadOnly:
			return descriptor(ComponentStorage, ReasonStoreReadOnly, ImpactStorageAtRisk, ProtectionWritesStopped, RecoveryGrantPermission)
		case store.HealthCodeStorePermission:
			return descriptor(ComponentStorage, ReasonStorePermission, ImpactStorageAtRisk, ProtectionWritesStopped, RecoveryGrantPermission)
		case store.HealthCodeStoreIO:
			return descriptor(ComponentStorage, ReasonStoreIO, ImpactStorageAtRisk, ProtectionRetryBackoff, RecoveryRetry)
		case store.HealthCodeStoreCorrupt:
			return descriptor(ComponentStorage, ReasonStoreCorrupt, ImpactStorageAtRisk, ProtectionWritesStopped, RecoveryRepairStore)
		case store.HealthCodeStoreUnavailable:
			return descriptor(ComponentStorage, ReasonStoreUnavailable, ImpactStorageAtRisk, ProtectionRetryBackoff, RecoveryRetry)
		case store.HealthCodeStoreUnknown:
			return descriptor(ComponentStorage, ReasonStoreUnknown, ImpactStorageAtRisk, ProtectionObservationOnly, RecoveryRetry)
		case store.HealthCodeStoreWALPressure:
			return descriptor(ComponentStorage, ReasonWALPressure, ImpactStorageAtRisk, ProtectionObservationOnly, RecoveryRetry)
		}
	case store.HealthDomainPricing:
		switch code {
		case store.HealthCodePricingUnavailable:
			return descriptor(ComponentOnlineQuota, ReasonPricingUnavailable, ImpactOnlineQuotaUnavailable, ProtectionRetryBackoff, RecoveryRetry)
		case store.HealthCodePricingInvalid:
			return descriptor(ComponentOnlineQuota, ReasonPricingInvalid, ImpactOnlineQuotaUnavailable, ProtectionObservationOnly, RecoveryRepairStore)
		}
	case store.HealthDomainRuntime:
		if code == store.HealthCodeRuntimeUnknown {
			return descriptor(ComponentRuntime, ReasonRuntimeUnknown, ImpactRuntimeAtRisk, ProtectionObservationOnly, RecoveryRetry)
		}
	}
	return EventDescriptor{}, false
}
