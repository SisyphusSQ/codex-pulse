import CodexPulseProtocolGenerated

public enum APISubscriptionCredentialService: String, CaseIterable, Sendable {
    case deepSeek = "deepseek"
    case openCodeGo = "opencode_go"
}

public struct APISubscriptionCredentialStatus: Equatable, Sendable {
    public let deepSeekConfigured: Bool
    public let openCodeGoConfigured: Bool

    public init(deepSeekConfigured: Bool, openCodeGoConfigured: Bool) {
        self.deepSeekConfigured = deepSeekConfigured
        self.openCodeGoConfigured = openCodeGoConfigured
    }

    public init(_ response: Codexpulse_Core_V1_APICredentialStatusResponse) {
        self.init(
            deepSeekConfigured: response.deepSeekConfigured,
            openCodeGoConfigured: response.openCodeGoConfigured
        )
    }
}
