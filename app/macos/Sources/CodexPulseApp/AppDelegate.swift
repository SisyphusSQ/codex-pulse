import AppKit
import CodexPulseAppSupport
import Combine
import SwiftUI

@MainActor
final class AppDelegate: NSObject, NSApplicationDelegate, NSWindowDelegate {
    private let configuration: AppLaunchConfiguration
    private let model: AppModel
    private var window: NSWindow?
    private var statusItemController: StatusItemController?
    private var workspaceObservers: [NSObjectProtocol] = []
    private var cancellables: Set<AnyCancellable> = []
    private var terminationInFlight = false
    private var shutdownComplete = false
    private var smokeFinished = false
    private var smokeProbeStarted = false

    private(set) var exitCode: Int32 = 0

    init(configuration: AppLaunchConfiguration) {
        self.configuration = configuration
        self.model = AppModel(configuration: configuration)
        super.init()
    }

    func applicationDidFinishLaunching(_ notification: Notification) {
        if configuration.smokeMode {
            if configuration.nativeSurfaceSmoke {
                NSApp.setActivationPolicy(.regular)
                buildNativeSurfaces()
                NSApp.activate(ignoringOtherApps: true)
            } else {
                NSApp.setActivationPolicy(.prohibited)
            }
            observeSmokeResult()
        } else {
            NSApp.setActivationPolicy(.regular)
            buildNativeSurfaces()
            installWorkspaceObservers()
            NSApp.activate(ignoringOtherApps: true)
        }
        model.start()
    }

    func applicationDidBecomeActive(_ notification: Notification) {
        guard !configuration.smokeMode else { return }
        model.applicationDidBecomeActive()
    }

    func applicationWillResignActive(_ notification: Notification) {
        guard !configuration.smokeMode else { return }
        model.applicationWillResignActive()
    }

    func applicationShouldTerminate(_ sender: NSApplication) -> NSApplication.TerminateReply {
        if shutdownComplete { return .terminateNow }
        guard !terminationInFlight else { return .terminateLater }
        terminationInFlight = true
        Task { @MainActor [weak self] in
            guard let self else {
                sender.reply(toApplicationShouldTerminate: true)
                return
            }
            let outcome = await model.shutdown()
            shutdownComplete = true
            switch outcome {
            case .clean: break
            case .forced: exitCode = max(exitCode, 2)
            case .uncertain: exitCode = max(exitCode, 3)
            }
            sender.reply(toApplicationShouldTerminate: true)
        }
        return .terminateLater
    }

    func applicationWillTerminate(_ notification: Notification) {
        let center = NSWorkspace.shared.notificationCenter
        workspaceObservers.forEach(center.removeObserver)
        workspaceObservers.removeAll()
    }

    func applicationShouldHandleReopen(
        _ sender: NSApplication,
        hasVisibleWindows flag: Bool
    ) -> Bool {
        showOverviewWindow()
        return true
    }

    private func buildNativeSurfaces() {
        let root = RootView(model: model)
        let hosting = NSHostingController(rootView: root)
        let window = NSWindow(
            contentRect: NSRect(x: 0, y: 0, width: 1_080, height: 720),
            styleMask: [.titled, .closable, .miniaturizable, .resizable, .unifiedTitleAndToolbar],
            backing: .buffered,
            defer: false
        )
        window.title = "Codex Pulse"
        window.titlebarAppearsTransparent = true
        window.contentViewController = hosting
        window.contentMinSize = NSSize(width: 820, height: 560)
        sizeInitialWindow(window)
        window.delegate = self
        window.isReleasedWhenClosed = false
        self.window = window
        let statusItemController = StatusItemController(
            model: model,
            onOpenOverview: { [weak self] in self?.showOverviewWindow() },
            onQuit: { NSApp.terminate(nil) }
        )
        self.statusItemController = statusItemController
        window.makeKeyAndOrderFront(nil)
    }

    private func sizeInitialWindow(_ window: NSWindow) {
        guard let screen = NSScreen.main ?? NSScreen.screens.first else {
            window.setContentSize(NSSize(
                width: MainWindowLayout.preferredContentSize.width,
                height: MainWindowLayout.preferredContentSize.height
            ))
            window.center()
            return
        }

        let preferred = MainWindowLayout.preferredContentSize
        let preferredContentRect = NSRect(
            x: 0,
            y: 0,
            width: preferred.width,
            height: preferred.height
        )
        let preferredFrame = window.frameRect(forContentRect: preferredContentRect)
        let contentSize = MainWindowLayout.initialContentSize(
            visibleFrameWidth: screen.visibleFrame.width,
            visibleFrameHeight: screen.visibleFrame.height,
            frameChromeWidth: preferredFrame.width - preferred.width,
            frameChromeHeight: preferredFrame.height - preferred.height
        )
        window.setContentSize(NSSize(width: contentSize.width, height: contentSize.height))
        let frame = window.frame
        window.setFrameOrigin(NSPoint(
            x: screen.visibleFrame.midX - frame.width / 2,
            y: screen.visibleFrame.midY - frame.height / 2
        ))
    }

    private func installWorkspaceObservers() {
        let center = NSWorkspace.shared.notificationCenter
        workspaceObservers.append(center.addObserver(
            forName: NSWorkspace.willSleepNotification,
            object: nil,
            queue: .main
        ) { [weak self] _ in
            Task { @MainActor in self?.model.prepareForSleep() }
        })
        workspaceObservers.append(center.addObserver(
            forName: NSWorkspace.didWakeNotification,
            object: nil,
            queue: .main
        ) { [weak self] _ in
            Task { @MainActor in self?.model.resumeAfterWake() }
        })
    }

    private func observeSmokeResult() {
        model.$state
            .removeDuplicates()
            .sink { [weak self] state in
                guard let self, !smokeFinished else { return }
                switch state {
                case .overview(let overview), .partial(let overview):
                    guard !smokeProbeStarted else { return }
                    smokeProbeStarted = true
                    let health = overview.health.hasValue
                        ? Self.safeToken(overview.health.level ?? "unknown")
                        : "empty"
                    Task { @MainActor [weak self] in
                        guard let self else { return }
                        let surfaces = nativeSurfaceSmokeSummary(
                            requireStatusSummary: !overview.quotaWindows.isEmpty
                        )
                        do {
                            let renderedPageCount = await renderPrimaryPagesForSmoke()
                            let pages = try await model.runPrimaryPagesSmoke()
                            let pagesStatus = pages.unavailableSteps.isEmpty ? "loaded" : "partial"
                            let rendered = renderedPageCount == AppFeature.allCases.count
                            finishSmoke(
                                success: surfaces.passed && rendered,
                                summary: "app smoke \(surfaces.passed && rendered ? "passed" : "failed"): overview=loaded quota_windows=\(overview.quotaWindows.count) sessions=\(overview.sessions.count) trend_points=\(overview.trend.count) health=\(health) primary_pages=\(pagesStatus) \(pages.stableDescription) ui_pages=\(renderedPageCount) native_surfaces=\(surfaces.summary) lifecycle=not_executed"
                            )
                        } catch {
                            let step = (error as? PrimaryPagesSmokeError)?.step ?? "unknown"
                            finishSmoke(
                                success: false,
                                summary: "app smoke failed: primary_pages=unavailable step=\(Self.safeToken(step)) native_surfaces=\(surfaces.summary) lifecycle=not_executed"
                            )
                        }
                    }
                case .recovery(let phase, _, _):
                    finishSmoke(
                        success: false,
                        summary: "app smoke failed: overview=recovery phase=\(Self.safeToken(phase)) lifecycle=not_executed"
                    )
                case .unavailable(let notice):
                    finishSmoke(
                        success: false,
                        summary: "app smoke failed: code=\(Self.safeToken(notice.code))"
                    )
                default:
                    break
                }
            }
            .store(in: &cancellables)
    }

    private func finishSmoke(success: Bool, summary: String) {
        guard !smokeFinished else { return }
        smokeFinished = true
        Task { @MainActor [weak self] in
            guard let self else { return }
            let outcome = await model.shutdown()
            shutdownComplete = true
            let clean = outcome == .clean
            exitCode = success && clean ? 0 : (outcome == .uncertain ? 3 : 1)
            let finalSummary = success && clean
                ? summary
                : summary.replacingOccurrences(of: "app smoke passed:", with: "app smoke failed:")
            let output = "\(finalSummary) shutdown=\(Self.shutdownToken(outcome))\n"
            FileHandle.standardOutput.write(Data(output.utf8))
            NSApp.terminate(nil)
        }
    }

    private func nativeSurfaceSmokeSummary(
        requireStatusSummary: Bool
    ) -> (passed: Bool, summary: String) {
        guard configuration.nativeSurfaceSmoke else { return (true, "not_executed") }
        let windowVisible = window?.isVisible == true && window?.contentViewController != nil
        let statusReady = statusItemController?.verifyNativeSurfacesForSmoke(
            requireSummary: requireStatusSummary
        ) == true
        let passed = windowVisible && statusReady
        return (passed, passed ? "window+status_item+popover" : "unavailable")
    }

    private func showOverviewWindow() {
        model.navigate(to: .overview)
        window?.makeKeyAndOrderFront(nil)
        NSApp.activate(ignoringOtherApps: true)
    }

    private func renderPrimaryPagesForSmoke() async -> Int {
        guard configuration.nativeSurfaceSmoke else { return 0 }
        for feature in AppFeature.allCases {
            model.navigate(to: feature)
            for _ in 0..<50 where !model.renderedFeatures.contains(feature) {
                try? await Task.sleep(for: .milliseconds(10))
            }
        }
        model.navigate(to: .overview)
        return model.renderedFeatures.count
    }

    private static func safeToken(_ value: String) -> String {
        let allowed = value.unicodeScalars.filter {
            CharacterSet.alphanumerics.union(CharacterSet(charactersIn: "_-.")).contains($0)
        }
        return String(String.UnicodeScalarView(allowed)).prefix(64).description
    }

    private static func shutdownToken(_ outcome: ShutdownOutcome) -> String {
        switch outcome {
        case .clean: "clean"
        case .forced: "forced"
        case .uncertain: "uncertain"
        }
    }
}
