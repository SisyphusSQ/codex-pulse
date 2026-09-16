import CodexPulseProtocolGenerated
import Foundation

public struct ProviderRuntimeState: Equatable, Sendable, Identifiable {
    public var id: AgentProvider { provider }
    public var provider: AgentProvider
    public var intent: Codexpulse_Core_V1_ProviderIntent
    public var discovery: Codexpulse_Core_V1_ProviderDiscoveryState
    public var effective: Codexpulse_Core_V1_ProviderEffectiveState
    public var reasonCode: String
    public var generation: String

    public var isEnabled: Bool { effective == .enabled }

    public var masterSwitchOn: Bool {
        switch intent {
        case .enabled:
            return true
        case .disabled:
            return false
        case .auto:
            return effective == .enabled || effective == .disabling
        default:
            return false
        }
    }
}

public struct ProviderCatalog: Equatable, Sendable {
    public var revision: String
    public var states: [ProviderRuntimeState]

    public static let empty = ProviderCatalog(revision: "", states: [])

    public var enabledProviders: [AgentProvider] {
        AgentProvider.allCases.filter { provider in
            states.contains { $0.provider == provider && $0.isEnabled }
        }
    }

    public var generationToken: String {
        states.map(\.generation).joined(separator: ":")
    }

    public init(revision: String, states: [ProviderRuntimeState]) {
        self.revision = revision
        self.states = states
    }

    public init(_ response: Codexpulse_Core_V1_SettingsResponse) {
        revision = response.snapshot.revision
        states = AgentProvider.allCases.compactMap { provider in
            response.snapshot.providers.first { $0.provider == provider.rawValue }.map { snapshot in
                ProviderRuntimeState(
                    provider: provider,
                    intent: snapshot.intent,
                    discovery: snapshot.discoveryState,
                    effective: snapshot.effectiveState,
                    reasonCode: snapshot.reasonCode,
                    generation: snapshot.generation
                )
            }
        }
    }

    public func state(for provider: AgentProvider) -> ProviderRuntimeState? {
        states.first { $0.provider == provider }
    }

    public func resolvedSelection(preferred: AgentProvider?) -> AgentProvider? {
        let enabled = enabledProviders
        if let preferred, enabled.contains(preferred) {
            return preferred
        }
        return enabled.first
    }
}

public enum ProviderStatusCopy {
    public static func statusLabel(_ state: ProviderRuntimeState) -> String {
        let value: String = {
            switch state.effective {
            case .disabling:
                return "正在停止"
            case .disabled:
                if state.discovery == .available {
                    return "已发现 · 已关闭"
                }
                return "已停用"
            case .unavailable:
                if state.intent == .enabled {
                    return "已开启，等待客户端数据源"
                }
                if state.intent == .auto {
                    return autoDiscoveryLabel(state.discovery)
                }
                return reasonLabel(state.reasonCode)
            case .enabled:
                if state.intent == .auto {
                    return "自动发现 · 已发现"
                }
                return "已发现"
            default:
                return reasonLabel(state.reasonCode)
            }
        }()
        return AppLocalizationRegistry.shared.current.textValue(value)
    }

    public static func autoDiscoveryLabel(_ discovery: Codexpulse_Core_V1_ProviderDiscoveryState) -> String {
        let value: String = switch discovery {
        case .available: "自动发现 · 已发现"
        case .missing: "自动发现 · 未发现"
        case .inaccessible: "自动发现 · 无权限"
        case .invalid: "自动发现 · 数据源无效"
        default: "自动发现"
        }
        return AppLocalizationRegistry.shared.current.textValue(value)
    }

    public static func reasonLabel(_ code: String) -> String {
        let value: String = switch code {
        case "available": "已发现"
        case "not_found": "未发现"
        case "permission_denied": "无权限"
        case "unsafe_path", "invalid_type", "probe_failed": "数据源无效"
        case "disabled": "已停用"
        case "disabling": "正在停止"
        case "unchecked": "尚未检查"
        default: "暂时未知"
        }
        return AppLocalizationRegistry.shared.current.textValue(value)
    }
}
