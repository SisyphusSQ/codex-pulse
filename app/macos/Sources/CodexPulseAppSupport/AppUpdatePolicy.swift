import Foundation

public enum AppUpdateChannel: String, Equatable, Sendable {
    case stable
    case prerelease

    public var sparkleAllowedChannels: Set<String> {
        switch self {
        case .stable: []
        case .prerelease: ["prerelease"]
        }
    }
}

public struct AppUpdatePolicy: Equatable, Sendable {
    public let automaticallyChecks: Bool
    public let automaticallyDownloads: Bool
    public let channel: AppUpdateChannel
    public let checkIntervalSeconds: TimeInterval

    public init(
        automaticallyChecks: Bool,
        automaticallyDownloads: Bool,
        channel: AppUpdateChannel,
        checkIntervalSeconds: Int64
    ) {
        self.automaticallyChecks = automaticallyChecks
        self.automaticallyDownloads = automaticallyDownloads
        self.channel = channel
        self.checkIntervalSeconds = TimeInterval(
            min(max(checkIntervalSeconds, 3_600), 86_400)
        )
    }

    public static let disabled = AppUpdatePolicy(
        automaticallyChecks: false,
        automaticallyDownloads: false,
        channel: .stable,
        checkIntervalSeconds: 3_600
    )
}
