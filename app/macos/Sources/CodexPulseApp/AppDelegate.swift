import AppKit
import CodexPulseAppSupport
import CodexPulseUpdater
import Combine
import SwiftUI

@MainActor
final class AppDelegate: NSObject, NSApplicationDelegate, NSWindowDelegate {
    private let configuration: AppLaunchConfiguration
    private let model: AppModel
    private let loginItemSettings: LoginItemSettingsModel
    private var updater: SparkleAppUpdater?
    private var window: NSWindow?
    private var statusItemController: StatusItemController?
    private var workspaceObservers: [NSObjectProtocol] = []
    private var cancellables: Set<AnyCancellable> = []
    private var terminationInFlight = false
    private var shutdownComplete = false
    private var smokeFinished = false
    private var smokeProbeStarted = false

    private(set) var exitCode: Int32 = 0

    init(
        configuration: AppLaunchConfiguration,
        loginItemService: any LoginItemServiceManaging = MainAppLoginItemService()
    ) {
        self.configuration = configuration
        self.model = AppModel(configuration: configuration)
        self.loginItemSettings = LoginItemSettingsModel(service: loginItemService)
        super.init()
        model.$localization
            .removeDuplicates()
            .receive(on: RunLoop.main)
            .sink { [weak self] localization in
                AppLocalizationRegistry.shared.update(localization)
                guard let self else { return }
                window?.title = "Codex Pulse"
                if !configuration.smokeMode, NSApp.mainMenu != nil {
                    refreshApplicationMenu()
                }
            }
            .store(in: &cancellables)
        if !configuration.smokeMode {
            updater = SparkleAppUpdater()
        }
        model.$updatePolicy
            .removeDuplicates()
            .receive(on: RunLoop.main)
            .sink { [weak self] policy in
                self?.updater?.apply(policy)
            }
            .store(in: &cancellables)
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
            installApplicationMenu()
            installWorkspaceObservers()
            NSApp.activate(ignoringOtherApps: true)
        }
        model.start()
    }

    func applicationDidBecomeActive(_ notification: Notification) {
        guard !configuration.smokeMode else { return }
        loginItemSettings.refreshStatus()
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
            if updater?.installationInProgress == true {
                switch await model.prepareForUpdateInstallation() {
                case .ready:
                    shutdownComplete = true
                    sender.reply(toApplicationShouldTerminate: true)
                case .blocked(let outcome):
                    terminationInFlight = false
                    sender.reply(toApplicationShouldTerminate: false)
                    model.start()
                    presentUpdateInstallationBlocked(outcome)
                }
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
        presentMainWindow(.revealCurrent, sender: sender)
        return true
    }

    private func installApplicationMenu() {
        let localization = model.localization
        let mainMenu = NSMenu()
        let applicationMenuItem = NSMenuItem()
        mainMenu.addItem(applicationMenuItem)

        let applicationMenu = NSMenu(title: localization.textValue("Codex Pulse"))
        applicationMenuItem.submenu = applicationMenu

        let aboutItem = NSMenuItem(
            title: "关于 Codex Pulse",
            action: #selector(showAboutPanel(_:)),
            keyEquivalent: ""
        )
        aboutItem.title = localization.textValue("关于 Codex Pulse")
        aboutItem.target = self
        applicationMenu.addItem(aboutItem)
        applicationMenu.addItem(.separator())

        let settingsItem = NSMenuItem(
            title: "设置…",
            action: #selector(showSettings(_:)),
            keyEquivalent: ","
        )
        settingsItem.title = localization.textValue("设置…")
        settingsItem.target = self
        settingsItem.keyEquivalentModifierMask = .command
        applicationMenu.addItem(settingsItem)

        let updateItem = NSMenuItem(
            title: "检查更新…",
            action: #selector(checkForUpdates(_:)),
            keyEquivalent: ""
        )
        updateItem.title = localization.textValue("检查更新…")
        updateItem.target = self
        applicationMenu.addItem(updateItem)
        applicationMenu.addItem(.separator())

        let servicesMenu = NSMenu(title: localization.textValue("服务"))
        let servicesItem = NSMenuItem(title: localization.textValue("服务"), action: nil, keyEquivalent: "")
        servicesItem.submenu = servicesMenu
        applicationMenu.addItem(servicesItem)
        NSApp.servicesMenu = servicesMenu
        applicationMenu.addItem(.separator())

        let hideItem = NSMenuItem(
            title: localization.textValue("隐藏 Codex Pulse"),
            action: #selector(NSApplication.hide(_:)),
            keyEquivalent: "h"
        )
        hideItem.target = NSApp
        applicationMenu.addItem(hideItem)

        let hideOthersItem = NSMenuItem(
            title: localization.textValue("隐藏其他"),
            action: #selector(NSApplication.hideOtherApplications(_:)),
            keyEquivalent: "h"
        )
        hideOthersItem.target = NSApp
        hideOthersItem.keyEquivalentModifierMask = [.command, .option]
        applicationMenu.addItem(hideOthersItem)

        let showAllItem = NSMenuItem(
            title: localization.textValue("全部显示"),
            action: #selector(NSApplication.unhideAllApplications(_:)),
            keyEquivalent: ""
        )
        showAllItem.target = NSApp
        applicationMenu.addItem(showAllItem)
        applicationMenu.addItem(.separator())

        let quitItem = NSMenuItem(
            title: localization.textValue("退出 Codex Pulse"),
            action: #selector(NSApplication.terminate(_:)),
            keyEquivalent: "q"
        )
        quitItem.target = NSApp
        applicationMenu.addItem(quitItem)

        NSApp.mainMenu = mainMenu
    }

    private func refreshApplicationMenu() {
        installApplicationMenu()
    }

    @objc private func showAboutPanel(_ sender: Any?) {
        NSApp.orderFrontStandardAboutPanel(sender)
    }

    @objc private func showSettings(_ sender: Any?) {
        presentMainWindow(.open(.settings), sender: sender)
    }

    @objc private func checkForUpdates(_ sender: Any?) {
        guard let updater else {
            presentUpdaterUnavailable()
            return
        }
        updater.checkForUpdates()
    }

    private func presentUpdaterUnavailable() {
        let localization = AppLocalizationRegistry.shared.current
        let alert = NSAlert()
        alert.alertStyle = .warning
        alert.messageText = localization.textValue("此构建未配置应用内更新")
        alert.informativeText = localization.textValue("正式发布包需要同时包含 HTTPS 更新源和 Ed25519 公钥。")
        alert.addButton(withTitle: localization.textValue("好"))
        if let window, window.isVisible {
            alert.beginSheetModal(for: window)
        } else {
            alert.runModal()
        }
    }

    private func presentUpdateInstallationBlocked(_ outcome: ShutdownOutcome) {
        let localization = AppLocalizationRegistry.shared.current
        let alert = NSAlert()
        alert.alertStyle = .warning
        alert.messageText = localization.textValue("暂时无法安装更新")
        switch outcome {
        case .forced:
            alert.informativeText = localization.textValue(
                "后台服务未能正常退出，已阻止替换应用。请稍后再次检查更新。"
            )
        case .uncertain:
            alert.informativeText = localization.textValue(
                "无法确认后台服务已经退出，已阻止替换应用。请稍后再次检查更新。"
            )
        case .clean:
            alert.informativeText = localization.textValue("更新准备状态异常，请稍后再次检查更新。")
        }
        alert.addButton(withTitle: localization.textValue("好"))
        if let window, window.isVisible {
            alert.beginSheetModal(for: window)
        } else {
            alert.runModal()
        }
    }

    private func buildNativeSurfaces() {
        let root = RootView(model: model, loginItemSettings: loginItemSettings)
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
            nativeAcceptanceEnabled: configuration.nativeSurfaceSmoke,
            onOpenOverview: { [weak self] in self?.showOverviewWindow() },
            onOpenSettings: { [weak self] in self?.showSettings(nil) },
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
                    let activityAvailability: String
                    switch overview.activityDistribution.availability {
                    case .available:
                        activityAvailability = "available"
                    case .partial:
                        activityAvailability = "partial"
                    case .unavailable:
                        activityAvailability = "unavailable"
                    }
                    Task { @MainActor [weak self] in
                        guard let self else { return }
                        let surfaces = await nativeSurfaceSmokeSummary(
                            requireStatusSummary: !overview.quotaWindows.isEmpty
                        )
                        do {
                            let renderedPageCount = await renderPrimaryPagesForSmoke()
                            let pages = try await model.runPrimaryPagesSmoke()
                            let pagesStatus = pages.unavailableSteps.isEmpty ? "loaded" : "partial"
                            let rendered = renderedPageCount == AppFeature.allCases.count
                            finishSmoke(
                                success: surfaces.passed && rendered,
                                summary: "app smoke \(surfaces.passed && rendered ? "passed" : "failed"): overview=loaded quota_windows=\(overview.quotaWindows.count) sessions=\(overview.sessions.count) trend_points=\(overview.trend.count) activity=\(activityAvailability) activity_timeline=\(overview.activityDistribution.timeline.count) activity_heatmap=\(overview.activityDistribution.heatmap.count) health=\(health) primary_pages=\(pagesStatus) \(pages.stableDescription) ui_pages=\(renderedPageCount) native_surfaces=\(surfaces.summary) lifecycle=not_executed"
                            )
                        } catch {
                            let step = (error as? PrimaryPagesSmokeError)?.step ?? "unknown"
							let code = AppNotice.from(error).code
                            finishSmoke(
                                success: false,
								summary: "app smoke failed: primary_pages=unavailable step=\(Self.safeToken(step)) code=\(Self.safeToken(code)) native_surfaces=\(surfaces.summary) lifecycle=not_executed"
                            )
                        }
                    }
                case .recovery(let phase, let stage, let code):
                    finishSmoke(
                        success: false,
                        summary: "app smoke failed: overview=recovery phase=\(Self.safeToken(phase)) "
                            + "stage=\(Self.safeToken(stage)) code=\(Self.safeToken(code)) "
                            + "lifecycle=not_executed"
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
    ) async -> (passed: Bool, summary: String) {
        guard configuration.nativeSurfaceSmoke else { return (true, "not_executed") }
        let windowVisible = window?.isVisible == true && window?.contentViewController != nil
        let status = await statusItemController?.verifyNativeSurfacesForSmoke(
            requireSummary: requireStatusSummary
        ) ?? (passed: false, summary: "unavailable step=status_item")
        let passed = windowVisible && status.passed
        return (
            passed,
            passed ? status.summary : (windowVisible ? status.summary : "unavailable step=window")
        )
    }

    private func showOverviewWindow() {
        presentMainWindow(.open(.overview), sender: nil)
    }

    private func presentMainWindow(
        _ request: MainWindowNavigationRequest,
        sender: Any?
    ) {
        if let feature = request.navigationTarget {
            model.navigate(to: feature)
        }
        showMainWindow(sender)
    }

    private func showMainWindow(_ sender: Any?) {
        window?.makeKeyAndOrderFront(sender)
        NSApp.activate(ignoringOtherApps: true)
    }

    private func renderPrimaryPagesForSmoke() async -> Int {
        guard configuration.nativeSurfaceSmoke else { return 0 }
        let renderOrder = AppFeature.allCases.filter { $0 != .overview } + [.overview]
        for feature in renderOrder {
            model.navigate(to: feature)
            for _ in 0..<50 where !model.renderedFeatures.contains(feature) {
                try? await Task.sleep(nanoseconds: 10_000_000)
            }
        }
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
