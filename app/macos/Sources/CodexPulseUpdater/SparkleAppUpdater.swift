import AppKit
import CodexPulseAppSupport
import Foundation
import Sparkle

public struct SparkleBundleConfiguration: Equatable, Sendable {
    public let feedURL: URL
    public let publicEDKey: String

    public init?(feedURLString: String, publicEDKey: String) {
        guard let feedURL = URL(string: feedURLString),
              feedURL.scheme?.lowercased() == "https",
              feedURL.host != nil,
              feedURL.user == nil,
              feedURL.password == nil,
              feedURL.port == nil,
              feedURL.query == nil,
              feedURL.fragment == nil,
              let decodedKey = Data(base64Encoded: publicEDKey),
              decodedKey.count == 32
        else {
            return nil
        }
        self.feedURL = feedURL
        self.publicEDKey = publicEDKey
    }

    public init?(bundle: Bundle) {
        guard let feedURLString = bundle.object(forInfoDictionaryKey: "SUFeedURL") as? String,
              let publicEDKey = bundle.object(forInfoDictionaryKey: "SUPublicEDKey") as? String
        else {
            return nil
        }
        self.init(feedURLString: feedURLString, publicEDKey: publicEDKey)
    }
}

@MainActor
public protocol SparkleUpdaterEngine: AnyObject {
    var automaticallyChecksForUpdates: Bool { get set }
    var automaticallyDownloadsUpdates: Bool { get set }
    var updateCheckInterval: TimeInterval { get set }
    var canCheckForUpdates: Bool { get }

    func checkForUpdates()
    func resetUpdateCycle()
}

extension SPUUpdater: SparkleUpdaterEngine {}

@MainActor
public final class SparkleAppUpdater: NSObject, SPUUpdaterDelegate {
    private var standardController: SPUStandardUpdaterController?
    private var engine: (any SparkleUpdaterEngine)!
    private var policy: AppUpdatePolicy?
    private var isStarted = false

    public private(set) var allowedChannels: Set<String> = []
    public private(set) var installationInProgress = false

    public init(engine: any SparkleUpdaterEngine) {
        self.engine = engine
        isStarted = true
        super.init()
    }

    public init?(bundle: Bundle = .main) {
        guard SparkleBundleConfiguration(bundle: bundle) != nil else {
            return nil
        }
        super.init()
        let controller = SPUStandardUpdaterController(
            startingUpdater: false,
            updaterDelegate: self,
            userDriverDelegate: nil
        )
        standardController = controller
        engine = controller.updater
    }

    public func apply(_ policy: AppUpdatePolicy) {
        guard self.policy != policy else { return }
        self.policy = policy
        allowedChannels = policy.channel.sparkleAllowedChannels
        engine.automaticallyChecksForUpdates = policy.automaticallyChecks
        engine.automaticallyDownloadsUpdates = policy.automaticallyDownloads
        engine.updateCheckInterval = policy.checkIntervalSeconds
        if let standardController, !isStarted {
            standardController.startUpdater()
            isStarted = true
            return
        }
        engine.resetUpdateCycle()
    }

    public func checkForUpdates() {
        guard isStarted, engine.canCheckForUpdates else { return }
        engine.checkForUpdates()
    }

    public func noteUpdateWillInstall() {
        installationInProgress = true
    }

    public func noteUpdateDidAbort() {
        installationInProgress = false
    }

    public func allowedChannels(for updater: SPUUpdater) -> Set<String> {
        allowedChannels
    }

    public func updater(_ updater: SPUUpdater, willInstallUpdate item: SUAppcastItem) {
        noteUpdateWillInstall()
    }

    public func updater(_ updater: SPUUpdater, didAbortWithError error: any Error) {
        noteUpdateDidAbort()
    }

    public func updater(
        _ updater: SPUUpdater,
        willInstallUpdateOnQuit item: SUAppcastItem,
        immediateInstallationBlock immediateInstallHandler: @escaping () -> Void
    ) -> Bool {
        noteUpdateWillInstall()
        return false
    }
}
