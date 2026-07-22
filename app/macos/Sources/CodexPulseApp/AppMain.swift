import AppKit
import CodexPulseAppSupport
import Darwin

@main
@MainActor
struct CodexPulseAppMain {
    static func main() {
        do {
            let configuration = try AppLaunchConfiguration.parse(arguments: CommandLine.arguments)
            let application = NSApplication.shared
            let delegate = AppDelegate(configuration: configuration)
            application.delegate = delegate
            withExtendedLifetime(delegate) {
                application.run()
            }
            Darwin.exit(delegate.exitCode)
        } catch let error as AppLaunchConfigurationError {
            let code: String
            switch error {
            case .missingArgument: code = "missing_argument"
            case .unknownArgument: code = "unknown_argument"
            case .helperUnavailable: code = "helper_unavailable"
            case .runtimeDirectoryUnavailable: code = "runtime_directory_unavailable"
            }
            FileHandle.standardError.write(Data(
                "codex-pulse-app: startup configuration unavailable code=\(code)\n".utf8
            ))
            Darwin.exit(2)
        } catch {
            FileHandle.standardError.write(Data(
                "codex-pulse-app: startup configuration unavailable code=unknown\n".utf8
            ))
            Darwin.exit(2)
        }
    }
}
