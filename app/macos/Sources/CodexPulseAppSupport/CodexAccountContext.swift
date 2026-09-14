import Foundation
import CodexPulseProtocolGenerated

public struct CodexAccountContextKey: Equatable, Hashable, Sendable {
    public let scope: String
    public let generation: UInt64

    public init(scope: String, generation: UInt64) {
        self.scope = scope
        self.generation = generation
    }
}

public struct CodexPublishedOverview: Sendable {
    public let responses: OverviewResponses
    public let needsConsistencyRefresh: Bool
}

public enum CodexAccountContext {
    public static func key(
        from binding: Codexpulse_Core_V1_CodexAccountBinding
    ) -> CodexAccountContextKey? {
        guard binding.state == "confirmed" else { return nil }
        let scope = binding.accountScope.trimmingCharacters(in: .whitespacesAndNewlines)
        guard binding.hasAccountScope, !scope.isEmpty, binding.bindingGeneration > 0 else {
            return nil
        }
        return CodexAccountContextKey(scope: scope, generation: binding.bindingGeneration)
    }

    public static func key(
        fromQuota quota: Codexpulse_Core_V1_QuotaCurrentResponse
    ) -> CodexAccountContextKey? {
        guard quota.current.hasBinding else { return nil }
        return key(from: quota.current.binding)
    }

    public static func key(
        fromPace pace: Codexpulse_Core_V1_QuotaPaceResponse
    ) -> CodexAccountContextKey? {
        guard pace.pace.hasBinding else { return nil }
        return key(from: pace.pace.binding)
    }

    public static func key(
        fromAccount account: Codexpulse_Core_V1_AccountSnapshotResponse?
    ) -> CodexAccountContextKey? {
        guard let account, account.hasBinding else { return nil }
        return key(from: account.binding)
    }

    public static func reusableAccount(
        provider: AgentProvider,
        quota: Codexpulse_Core_V1_QuotaCurrentResponse,
        previousAccount: Codexpulse_Core_V1_AccountSnapshotResponse?
    ) -> Codexpulse_Core_V1_AccountSnapshotResponse? {
        guard provider == .codex else { return previousAccount }
        guard key(fromQuota: quota) == key(fromAccount: previousAccount) else {
            return nil
        }
        return previousAccount
    }

    public static func validatePublishedOverview(
        _ responses: OverviewResponses
    ) -> CodexPublishedOverview {
        guard responses.provider == .codex else {
            return CodexPublishedOverview(responses: responses, needsConsistencyRefresh: false)
        }
        var next = responses
        var needsRefresh = false
        let quotaKey = key(fromQuota: responses.quota)
        if key(fromPace: responses.quotaPace) != quotaKey {
            next = next.replacingPace(unknownPace(alignedTo: responses.quota))
            needsRefresh = true
        }
        if let account = responses.account,
           key(fromAccount: account) != quotaKey
        {
            needsRefresh = true
            next = next.replacingAccount(strippedAccount(account))
        }
        return CodexPublishedOverview(responses: next, needsConsistencyRefresh: needsRefresh)
    }

    public static func unknownPace(
        alignedTo quota: Codexpulse_Core_V1_QuotaCurrentResponse
    ) -> Codexpulse_Core_V1_QuotaPaceResponse {
        var pace = Codexpulse_Core_V1_QuotaPaceResponse()
        pace.meta.status = "unavailable"
        pace.pace.version = "quota-pace-v1"
        pace.pace.accountScope = quota.current.accountScope
        pace.pace.evaluatedAtMs = quota.current.evaluatedAtMs
        if quota.current.hasBinding {
            pace.pace.binding = quota.current.binding
        }
        pace.pace.unknownReason = "binding_unavailable"
        pace.providerContext = quota.providerContext
        return pace
    }

    public static func strippedAccount(
        _ incoming: Codexpulse_Core_V1_AccountSnapshotResponse?
    ) -> Codexpulse_Core_V1_AccountSnapshotResponse? {
        guard let incoming else { return nil }
        var stripped = Codexpulse_Core_V1_AccountSnapshotResponse()
        if incoming.hasBinding, key(from: incoming.binding) == nil {
            stripped.binding = incoming.binding
        }
        return stripped
    }
}
