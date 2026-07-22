import CodexPulseCoreClient
import Darwin
import Foundation

public enum AppLaunchConfigurationError: Error, Equatable, Sendable {
    case missingArgument(String)
    case unknownArgument(String)
    case helperUnavailable
    case runtimeDirectoryUnavailable
}

public struct AppLaunchConfiguration: Sendable {
    public let helperExecutablePath: String
    public let runtimeDirectory: String
    public let clientVersion: String
    public let smokeMode: Bool
    public let nativeSurfaceSmoke: Bool
    public let sendLifecycleToHelper: Bool

    public init(
        helperExecutablePath: String,
        runtimeDirectory: String,
        clientVersion: String = "dev",
        smokeMode: Bool = false,
        nativeSurfaceSmoke: Bool = false,
        sendLifecycleToHelper: Bool = true
    ) throws {
        guard FileManager.default.isExecutableFile(atPath: helperExecutablePath) else {
            throw AppLaunchConfigurationError.helperUnavailable
        }
        let runtimeComponents = runtimeDirectory.split(
            separator: "/",
            omittingEmptySubsequences: false
        )
        guard !runtimeComponents.contains("."),
              !runtimeComponents.contains(".."),
              !runtimeDirectory.contains("//"),
              runtimeDirectory.hasPrefix("/private/tmp/cp-") || runtimeDirectory.hasPrefix("/tmp/cp-")
        else {
            throw AppLaunchConfigurationError.runtimeDirectoryUnavailable
        }
        self.helperExecutablePath = helperExecutablePath
        self.runtimeDirectory = runtimeDirectory
        self.clientVersion = clientVersion
        self.smokeMode = smokeMode
        self.nativeSurfaceSmoke = nativeSurfaceSmoke
        self.sendLifecycleToHelper = sendLifecycleToHelper
    }

    public static func parse(
        arguments: [String],
        bundleURL: URL = Bundle.main.bundleURL
    ) throws -> Self {
        var helperPath: String?
        var runtimeDirectory: String?
        var clientVersion = "dev"
        var smokeMode = false
        var nativeSurfaceSmoke = false
        var sendLifecycle = true
        var index = 1
        while index < arguments.count {
            let argument = arguments[index]
            switch argument {
            case "--helper", "--runtime-directory", "--client-version":
                guard index + 1 < arguments.count else {
                    throw AppLaunchConfigurationError.missingArgument(argument)
                }
                let value = arguments[index + 1]
                if argument == "--helper" { helperPath = value }
                if argument == "--runtime-directory" { runtimeDirectory = value }
                if argument == "--client-version" { clientVersion = value }
                index += 2
            case "--smoke":
                smokeMode = true
                index += 1
            case "--ui-smoke":
                smokeMode = true
                nativeSurfaceSmoke = true
                index += 1
            case "--skip-live-lifecycle":
                sendLifecycle = false
                index += 1
            case let launchServicesArgument where launchServicesArgument.hasPrefix("-psn_"):
                index += 1
            default:
                throw AppLaunchConfigurationError.unknownArgument(argument)
            }
        }

        let bundledHelper = bundleURL
            .appendingPathComponent("Contents", isDirectory: true)
            .appendingPathComponent("Helpers", isDirectory: true)
            .appendingPathComponent("codex-pulse", isDirectory: false)
            .path
        let resolvedHelper = helperPath ?? bundledHelper
        let resolvedRuntime = runtimeDirectory ?? defaultRuntimeDirectory()
        return try Self(
            helperExecutablePath: resolvedHelper,
            runtimeDirectory: resolvedRuntime,
            clientVersion: clientVersion,
            smokeMode: smokeMode,
            nativeSurfaceSmoke: nativeSurfaceSmoke,
            sendLifecycleToHelper: sendLifecycle
        )
    }

    private static func defaultRuntimeDirectory() -> String {
        "/private/tmp/cp-app-\(getuid())-\(UUID().uuidString.prefix(12))"
    }
}
