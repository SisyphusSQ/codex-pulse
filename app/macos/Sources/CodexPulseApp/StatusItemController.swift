import AppKit
import Charts
import CodexPulseAppSupport
import Combine
import SwiftUI

@MainActor
final class StatusItemController: NSObject {
    private let statusItem: NSStatusItem
    private let statusBarView = StatusBarQuotaContentView()
    private let popover = NSPopover()
    private let model: AppModel
    private let displayPreferences = StatusBarDisplayPreferences()
    private let captureSource = PopoverCaptureSource()
    private let nativeAcceptanceEnabled: Bool
    private var cancellables: Set<AnyCancellable> = []
    private var smokeFocusedControl: PopoverFocusTarget?
    private var smokeActionResults: [PopoverQuickActionKind: PopoverQuickActionResult] = [:]
    private var smokeOpenedProjectURL: URL?
    private var lastPopoverCaptureFailure = "none"

    init(
        model: AppModel,
        nativeAcceptanceEnabled: Bool = false,
        onOpenOverview: @escaping @MainActor () -> Void,
        onQuit: @escaping @MainActor () -> Void
    ) {
        self.model = model
        self.nativeAcceptanceEnabled = nativeAcceptanceEnabled
        self.statusItem = NSStatusBar.system.statusItem(withLength: NSStatusItem.variableLength)
        super.init()

        if let button = statusItem.button {
            button.image = nil
            button.title = ""
            statusBarView.translatesAutoresizingMaskIntoConstraints = false
            button.addSubview(statusBarView)
            NSLayoutConstraint.activate([
                statusBarView.leadingAnchor.constraint(equalTo: button.leadingAnchor),
                statusBarView.trailingAnchor.constraint(equalTo: button.trailingAnchor),
                statusBarView.topAnchor.constraint(equalTo: button.topAnchor),
                statusBarView.bottomAnchor.constraint(equalTo: button.bottomAnchor),
            ])
            button.target = self
            button.action = #selector(togglePopover(_:))
            button.sendAction(on: [.leftMouseUp])
        }
        updateStatusBarView()

        popover.behavior = .transient
        popover.animates = true
        let popoverContentSize = MenuBarPopoverLayout.contentSize(
            availableScreenHeight: statusItem.button?.window?.screen?.visibleFrame.height
                ?? NSScreen.main?.visibleFrame.height
                ?? 720
        )
        let popoverView = MenuBarPopoverView(
            model: model,
            preferences: displayPreferences,
            captureSource: captureSource,
            contentSize: popoverContentSize,
            capturePopoverPNG: { [weak self] in
                guard let self else { return nil }
                return await self.captureCurrentPopoverPNG()
            },
            openProjectURL: { [weak self] url in
                guard let self else { return false }
                if self.nativeAcceptanceEnabled {
                    self.smokeOpenedProjectURL = url
                    return true
                }
                return NSWorkspace.shared.open(url)
            },
            onQuickActionResult: { [weak self] action, result in
                guard self?.nativeAcceptanceEnabled == true else { return }
                self?.smokeActionResults[action] = result
            },
            onPopoverFocusChanged: { [weak self] control in
                guard self?.nativeAcceptanceEnabled == true else { return }
                self?.smokeFocusedControl = control
            },
            onOpenOverview: {
                self.popover.performClose(nil)
                onOpenOverview()
            },
            onQuit: onQuit
        )
        popover.contentViewController = NSHostingController(rootView: popoverView)
        popover.contentSize = popoverContentSize

        model.$state
            .receive(on: RunLoop.main)
            .sink { [weak self] _ in
                self?.updateStatusBarView()
            }
            .store(in: &cancellables)

        displayPreferences.$style
            .removeDuplicates()
            .receive(on: RunLoop.main)
            .sink { [weak self] _ in self?.updateStatusBarView() }
            .store(in: &cancellables)
    }

    private func updateStatusBarView() {
        let title = model.statusItemTitle
        let summary = model.presentation.flatMap(StatusBarQuotaPresentation.init)
        statusBarView.update(
            summary: summary,
            fallbackText: title,
            style: displayPreferences.style
        )
        statusItem.length = statusBarView.preferredWidth
        let summaryLabel = summary?.accessibilityLabel ?? title
        statusItem.button?.toolTip = "Codex Pulse · \(summaryLabel)"
        statusItem.button?.setAccessibilityLabel("Codex Pulse · \(summaryLabel)")
    }

    @objc private func togglePopover(_ sender: Any?) {
        guard statusItem.button != nil else { return }
        if popover.isShown {
            popover.performClose(sender)
        } else {
            showPopover()
        }
    }

    private func showPopover() {
        guard let button = statusItem.button else { return }
        popover.show(relativeTo: button.bounds, of: button, preferredEdge: .minY)
    }

    private func captureCurrentPopoverPNG() async -> Data? {
        lastPopoverCaptureFailure = "none"
        guard popover.isShown else {
            lastPopoverCaptureFailure = "popover_hidden"
            return nil
        }
        captureSource.setPrivacyHidden(true)
        defer { captureSource.setPrivacyHidden(false) }

        guard await captureSource.waitUntilPrivacyRendered(true) else {
            lastPopoverCaptureFailure = "privacy_render"
            return nil
        }
        guard popover.isShown else {
            lastPopoverCaptureFailure = "popover_closed"
            return nil
        }
        guard let rootView = popover.contentViewController?.view else {
            lastPopoverCaptureFailure = "root_view"
            return nil
        }
        guard let scrollView = captureSource.resolveScrollView(in: rootView) else {
            lastPopoverCaptureFailure = "scroll_view"
            return nil
        }

        rootView.layoutSubtreeIfNeeded()
        rootView.displayIfNeeded()
        guard let png = PopoverFullPageCapture.renderPNG(
            rootView: rootView,
            scrollView: scrollView,
            onFailure: { [weak self] reason in
                self?.lastPopoverCaptureFailure = "bitmap_\(reason)"
            }
        ) else {
            if lastPopoverCaptureFailure == "none" {
                lastPopoverCaptureFailure = "bitmap_unknown"
            }
            return nil
        }
        return png
    }

    func verifyNativeSurfacesForSmoke(
        requireSummary: Bool
    ) async -> (passed: Bool, summary: String) {
        updateStatusBarView()
        guard let button = statusItem.button,
              popover.contentViewController != nil,
              statusBarView.superview === button,
              statusBarView.preferredWidth > 0
        else { return (false, "unavailable step=native_surface") }
        if requireSummary && !statusBarView.hasSummary {
            return (false, "unavailable step=status_summary")
        }
        guard nativeAcceptanceEnabled,
              model.presentation != nil
        else { return (false, "unavailable step=acceptance_fixture") }

        smokeFocusedControl = nil
        smokeActionResults.removeAll()
        smokeOpenedProjectURL = nil
        popover.show(relativeTo: button.bounds, of: button, preferredEdge: .minY)
        defer { popover.performClose(nil) }

        guard await waitForNativeSmoke({ self.popover.isShown }) else {
            return (false, "unavailable step=popover_focus")
        }
        guard await moveNativeSmokeFocus(to: .openProject),
              sendNativeSmokeKey("\r", keyCode: 36),
              await waitForNativeSmoke({
                  self.smokeActionResults[.openProject] != nil
              }),
              smokeOpenedProjectURL?.absoluteString
                  == "https://github.com/SisyphusSQ/codex-pulse",
              smokeActionResults[.openProject]?.isFailure == false
        else {
            return (false, "unavailable step=project_keyboard_action")
        }

        guard await moveNativeSmokeFocus(to: .copyPopoverScreenshot) else {
            return (false, "unavailable step=clipboard_keyboard_focus")
        }
        guard sendNativeSmokeKey(" ", keyCode: 49) else {
            return (false, "unavailable step=clipboard_keyboard_event")
        }
        guard await waitForNativeSmoke({
            self.smokeActionResults[.copyPopoverScreenshot] != nil
        }) else {
            return (false, "unavailable step=clipboard_keyboard_result")
        }
        guard let screenshotResult =
            smokeActionResults[.copyPopoverScreenshot]
        else {
            return (false, "unavailable step=clipboard_capture_result")
        }
        guard screenshotResult.isFailure == false else {
            if screenshotResult.message
                == "Popover 截图生成失败，未写入剪贴板。"
            {
                return (
                    false,
                    "unavailable step=clipboard_capture_\(lastPopoverCaptureFailure)"
                )
            }
            return (false, "unavailable step=clipboard_capture_write")
        }
        guard verifyGeneralPasteboardForSmoke() else {
            return (false, "unavailable step=clipboard_payload")
        }

        guard await moveNativeSmokeFocus(to: .resetCredits),
              await moveNativeSmokeFocus(to: .settings),
              await moveNativeSmokeFocus(to: .quit),
              await moveNativeSmokeFocus(to: .settings, backward: true),
              await moveNativeSmokeFocus(to: .resetCredits, backward: true),
              await moveNativeSmokeFocus(to: .copyPopoverScreenshot, backward: true),
              await moveNativeSmokeFocus(to: .openProject, backward: true),
              await moveNativeSmokeFocus(to: .openOverview, backward: true),
              await moveNativeSmokeFocus(to: .refresh, backward: true),
              popover.isShown
        else {
            return (false, "unavailable step=keyboard_focus_chain")
        }

        return (
            true,
            "window+status_item+popover actions=project+copy "
                + "keyboard=tab+shift-tab+return+space "
                + "focus_escape=open-overview+refresh+reset-credits+settings+quit "
                + "clipboard=single_item_string+png"
        )
    }

    private func moveNativeSmokeFocus(
        to target: PopoverFocusTarget,
        backward: Bool = false,
        maximumSteps: Int = 12
    ) async -> Bool {
        let modifierFlags: NSEvent.ModifierFlags = backward ? [.shift] : []
        for _ in 0..<maximumSteps {
            guard sendNativeSmokeKey(
                "\t",
                keyCode: 48,
                modifierFlags: modifierFlags
            ) else { return false }
            try? await Task.sleep(nanoseconds: 20_000_000)
            if smokeFocusedControl == target { return true }
        }
        return false
    }

    private func sendNativeSmokeKey(
        _ characters: String,
        keyCode: UInt16,
        modifierFlags: NSEvent.ModifierFlags = []
    ) -> Bool {
        guard let window = popover.contentViewController?.view.window,
              let keyDown = NSEvent.keyEvent(
                  with: .keyDown,
                  location: .zero,
                  modifierFlags: modifierFlags,
                  timestamp: ProcessInfo.processInfo.systemUptime,
                  windowNumber: window.windowNumber,
                  context: nil,
                  characters: characters,
                  charactersIgnoringModifiers: characters,
                  isARepeat: false,
                  keyCode: keyCode
              ),
              let keyUp = NSEvent.keyEvent(
                  with: .keyUp,
                  location: .zero,
                  modifierFlags: modifierFlags,
                  timestamp: ProcessInfo.processInfo.systemUptime,
                  windowNumber: window.windowNumber,
                  context: nil,
                  characters: characters,
                  charactersIgnoringModifiers: characters,
                  isARepeat: false,
                  keyCode: keyCode
              )
        else { return false }

        window.makeKey()
        window.sendEvent(keyDown)
        window.sendEvent(keyUp)
        return true
    }

    private func waitForNativeSmoke(
        _ predicate: @escaping @MainActor () -> Bool
    ) async -> Bool {
        for _ in 0..<100 {
            if predicate() { return true }
            try? await Task.sleep(nanoseconds: 10_000_000)
        }
        return false
    }

    private func verifyGeneralPasteboardForSmoke() -> Bool {
        guard let items = NSPasteboard.general.pasteboardItems,
              items.count == 1,
              items[0].types.contains(.string),
              items[0].types.contains(.png),
              items[0].string(forType: .string)
                  == PopoverScreenshotClipboardText.plainText,
              let png = items[0].data(forType: .png),
              png.starts(with: Data([0x89, 0x50, 0x4E, 0x47, 0x0D, 0x0A, 0x1A, 0x0A])),
              let bitmap = NSBitmapImageRep(data: png),
              bitmap.pixelsHigh > bitmap.pixelsWide
        else { return false }
        return true
    }
}

@MainActor
private final class PopoverCaptureSource: ObservableObject {
    @Published private(set) var isPrivacyHidden = false
    private weak var documentAnchor: NSView?
    private var renderedPrivacyHidden = false

    func setPrivacyHidden(_ hidden: Bool) {
        isPrivacyHidden = hidden
    }

    func registerDocumentAnchor(_ view: NSView) {
        documentAnchor = view
    }

    func registerPrivacyRendered(_ hidden: Bool) {
        renderedPrivacyHidden = hidden
    }

    func waitUntilPrivacyRendered(_ hidden: Bool) async -> Bool {
        for _ in 0..<50 {
            if renderedPrivacyHidden == hidden { return true }
            try? await Task.sleep(nanoseconds: 1_000_000)
        }
        return false
    }

    func resolveScrollView(in rootView: NSView) -> NSScrollView? {
        var ancestor = documentAnchor
        while let view = ancestor {
            if let scrollView = view as? NSScrollView,
               scrollView.isDescendant(of: rootView)
            {
                return scrollView
            }
            ancestor = view.superview
        }
        return descendantScrollViews(of: rootView)
            .filter { $0.documentView != nil }
            .max {
                ($0.documentView?.bounds.height ?? 0)
                    < ($1.documentView?.bounds.height ?? 0)
            }
    }

    private func descendantScrollViews(of view: NSView) -> [NSScrollView] {
        view.subviews.flatMap { child in
            var matches = descendantScrollViews(of: child)
            if let scrollView = child as? NSScrollView {
                matches.append(scrollView)
            }
            return matches
        }
    }
}

@MainActor
private struct PopoverCaptureDocumentProbe: NSViewRepresentable {
    let source: PopoverCaptureSource

    func makeNSView(context: Context) -> NSView {
        let view = NSView()
        source.registerDocumentAnchor(view)
        return view
    }

    func updateNSView(_ nsView: NSView, context: Context) {
        source.registerDocumentAnchor(nsView)
    }
}

@MainActor
private struct PopoverPrivacyRenderProbe: NSViewRepresentable {
    let source: PopoverCaptureSource
    let isPrivacyHidden: Bool

    func makeNSView(context: Context) -> NSView {
        source.registerPrivacyRendered(isPrivacyHidden)
        return NSView()
    }

    func updateNSView(_ nsView: NSView, context: Context) {
        source.registerPrivacyRendered(isPrivacyHidden)
    }
}

private struct MenuBarPopoverView: View {
    @ObservedObject var model: AppModel
    @ObservedObject var preferences: StatusBarDisplayPreferences
    @ObservedObject var captureSource: PopoverCaptureSource
    let contentSize: NSSize
    let capturePopoverPNG: @MainActor () async -> Data?
    let openProjectURL: @MainActor (URL) -> Bool
    let onQuickActionResult:
        @MainActor (PopoverQuickActionKind, PopoverQuickActionResult) -> Void
    let onPopoverFocusChanged: @MainActor (PopoverFocusTarget?) -> Void
    let onOpenOverview: @MainActor () -> Void
    let onQuit: @MainActor () -> Void
    @State private var route: PopoverRoute = .main
    @State private var selectedDailyTrendKey: String?
    @State private var quickActionResult: PopoverQuickActionResult?
    @State private var screenshotFeedback: PopoverScreenshotFeedback = .idle
    @State private var isCapturingScreenshot = false
    @FocusState private var focusedControl: PopoverFocusTarget?

    var body: some View {
        ZStack {
            NativePopoverBackdrop()

            switch route {
            case .main:
                mainContent
            case .resetCredits:
                ResetCreditsDetailView(overview: model.presentation) { route = .main }
            case .displaySettings:
                StatusDisplaySettingsView(
                    preferences: preferences,
                    onBack: { route = .main },
                    onOpenSettings: {
                        model.navigate(to: .settings)
                        onOpenOverview()
                    }
                )
            }
        }
        .foregroundStyle(.primary)
        .frame(width: contentSize.width, height: contentSize.height)
        .alert(
            quickActionResult?.title ?? "",
            isPresented: Binding(
                get: { quickActionResult != nil },
                set: { isPresented in
                    if !isPresented { quickActionResult = nil }
                }
            )
        ) {
            Button("好", role: .cancel) { quickActionResult = nil }
        } message: {
            Text(quickActionResult?.message ?? "")
        }
    }

    private var mainContent: some View {
        VStack(spacing: 0) {
            PopoverHeader(
                title: "Codex Pulse",
                accountSummary: model.presentation?.popoverAccountSummary,
                captureSource: captureSource,
                isPrivacyHidden: captureSource.isPrivacyHidden,
                screenshotFeedback: screenshotFeedback,
                onOpenProject: openProject,
                onCopyPopoverScreenshot: copyPopoverScreenshot,
                focusedControl: $focusedControl,
                onOpen: onOpenOverview,
                onRefresh: model.refreshOrRestart,
                canRefresh: model.canRefreshOrRestart,
                isRefreshing: model.isOverviewRefreshing
            )

            ScrollView {
                if let overview = model.presentation {
                    VStack(alignment: .leading, spacing: 18) {
                        quotaSection(overview)
                        dailyTrendSection(overview)
                        resetCreditsSection(overview)
                        if preferences.showCostSummary { costSection(overview) }
                        if preferences.showProjectRanking { projectRankingSection(overview) }
                    }
                    .padding(.horizontal, 18)
                    .padding(.vertical, 16)
                    .background(
                        PopoverCaptureDocumentProbe(source: captureSource)
                            .allowsHitTesting(false)
                    )
                } else {
                    VStack(spacing: 12) {
                        ProgressView()
                        Text(model.statusItemTitle).foregroundStyle(.secondary)
                    }
                    .frame(maxWidth: .infinity, minHeight: 440)
                }
            }
            .scrollIndicators(.hidden)

            PopoverFooter(
                onSettings: { route = .displaySettings },
                onQuit: onQuit,
                focusedControl: $focusedControl
            )
        }
        .onChange(of: focusedControl) { _, control in
            onPopoverFocusChanged(control)
        }
    }

    private func openProject() {
        let result = PopoverQuickActions.openProject(using: openProjectURL)
        onQuickActionResult(.openProject, result)
        if result.isFailure { quickActionResult = result }
    }

    private func copyPopoverScreenshot() {
        guard !isCapturingScreenshot else { return }
        isCapturingScreenshot = true
        Task { @MainActor in
            let png = model.presentation == nil ? nil : await capturePopoverPNG()
            let result = PopoverQuickActions.copyPopoverScreenshot(
                png: png,
                writeClipboard: writePopoverScreenshotClipboard
            )
            onQuickActionResult(.copyPopoverScreenshot, result)
            screenshotFeedback = result.isFailure ? .failed : .copied
            isCapturingScreenshot = false
            if result.isFailure { quickActionResult = result }
            let feedback = screenshotFeedback
            try? await Task.sleep(nanoseconds: 2_000_000_000)
            if screenshotFeedback == feedback {
                screenshotFeedback = .idle
            }
        }
    }

    private func quotaSection(_ overview: OverviewPresentation) -> some View {
        VStack(alignment: .leading, spacing: 12) {
            PopoverSectionTitle(title: "配额", systemImage: "gauge.with.dots.needle.67percent")
            if overview.quotaWindows.isEmpty {
                PulseCard { Text("尚未取得可信额度数据").foregroundStyle(.secondary) }
            } else {
                ForEach(overview.quotaWindows.prefix(2)) { window in
                    let reset = QuotaResetPresentation(
                        resetsAtMS: window.resetsAtMS,
                        resetRemainingMS: window.resetRemainingMS
                    )
                    VStack(alignment: .leading, spacing: 8) {
                        HStack(alignment: .firstTextBaseline) {
                            Text(window.title).font(.system(size: 15, weight: .semibold))
                            Spacer()
                            Text(percentText(window.remainingPercent))
                                .font(.system(size: 14, weight: .bold, design: .rounded))
                                .monospacedDigit()
                        }
                        GeometryReader { geometry in
                            ZStack(alignment: .leading) {
                                Capsule().fill(.quaternary)
                                Capsule()
                                    .fill(quotaColor(window.remainingPercent))
                                    .frame(width: geometry.size.width * progress(window.remainingPercent))
                            }
                        }
                        .frame(height: 9)
                        HStack(alignment: .top) {
                            Text("\(percentText(window.remainingPercent)) 剩余")
                            Spacer()
                            VStack(alignment: .trailing, spacing: 2) {
                                Text("距离重置：\(reset.remainingText)")
                                Text("重置时间：\(reset.resetTimeText)")
                            }
                            .multilineTextAlignment(.trailing)
                            .accessibilityElement(children: .combine)
                            .accessibilityIdentifier("popover.quota.reset.\(window.id)")
                        }
                        .font(.caption)
                        .foregroundStyle(.secondary)
                    }
                }
            }
        }
    }

    private func resetCreditsSection(_ overview: OverviewPresentation) -> some View {
        let credits = overview.resetCredits
        return VStack(alignment: .leading, spacing: 10) {
            PopoverSectionTitle(title: "重置次数", systemImage: "clock.arrow.circlepath")
            Button { route = .resetCredits } label: {
                PulseCard {
                    VStack(alignment: .leading, spacing: 7) {
                        HStack {
                            Text("\(optionalCount(credits.availableCount)) 可用 / \(optionalCount(credits.totalCount)) 总数")
                                .font(.system(size: 15, weight: .semibold))
                            Spacer()
                            Image(systemName: "chevron.right").foregroundStyle(.tertiary)
                        }
                        HStack {
                            Text("总剩余：\(durationText(credits.cumulativeRemainingMS))")
                            Spacer()
                            Text(nextExpiryText(credits))
                        }
                        .font(.caption)
                        .foregroundStyle(.secondary)
                    }
                }
            }
            .contentShape(RoundedRectangle(cornerRadius: PopoverInteractionMetrics.cardCornerRadius, style: .continuous))
            .buttonStyle(InteractiveCardButtonStyle())
            .accessibilityIdentifier("popover.reset-credits")
            .focusable(true)
            .focused($focusedControl, equals: .resetCredits)
        }
    }

    private func dailyTrendSection(_ overview: OverviewPresentation) -> some View {
        let buckets = overview.weeklyUsageModelTrend
        let modelNames = dailyTrendModelNames(buckets)
        let colors = dailyTrendColors(count: modelNames.count)
        let selectedBucket = buckets.first { $0.key == selectedDailyTrendKey }
        return VStack(alignment: .leading, spacing: 10) {
            PopoverSectionTitle(title: "本周每日 Token", systemImage: "chart.bar.fill")
            PulseCard {
                if !overview.weeklyUsageAvailable {
                    Text("本周用量趋势暂时不可用")
                        .foregroundStyle(.secondary)
                } else if buckets.isEmpty {
                    Text("本周还没有每日用量")
                        .foregroundStyle(.secondary)
                } else {
                    VStack(alignment: .leading, spacing: 10) {
                        HStack(alignment: .firstTextBaseline) {
                            Text(overview.weeklyUsageRangeLabel)
                            Spacer()
                            if buckets.contains(where: { !$0.breakdownAvailable }) {
                                Text("部分日期仅有总计")
                            }
                        }
                        .font(.caption2)
                        .foregroundStyle(.secondary)

                        Chart {
                                ForEach(buckets) { bucket in
                                    ForEach(bucket.segments) { segment in
                                        BarMark(
                                            x: .value("日期", segment.bucketKey),
                                            y: .value("Token", segment.tokens)
                                        )
                                        .foregroundStyle(by: .value("模型", segment.modelName))
                                        .opacity(
                                            selectedBucket == nil || selectedBucket?.key == bucket.key
                                                ? 1 : 0.3)
                                        .cornerRadius(2)
                                        .accessibilityLabel(
                                            "\(compactDailyTrendDate(segment.bucketKey)) · \(segment.modelName)"
                                        )
                                        .accessibilityValue(
                                            "\(TokenQuantityFormatter.string(segment.tokens)) Token")
                                    }
                                    if bucket.totalTokens > 0 {
                                        PointMark(
                                            x: .value("日期", bucket.key),
                                            y: .value("总计", bucket.totalTokens)
                                        )
                                        .foregroundStyle(.clear)
                                        .symbolSize(0)
                                        .annotation(position: .top, spacing: 3) {
                                            Text(TokenQuantityFormatter.compactString(bucket.totalTokens))
                                                .font(.system(size: 9, weight: .semibold, design: .rounded))
                                                .monospacedDigit()
                                                .foregroundStyle(.secondary)
                                        }
                                        .accessibilityHidden(true)
                                    }
                                }
                                if let selectedBucket {
                                    RuleMark(x: .value("选中日期", selectedBucket.key))
                                        .foregroundStyle(Color.secondary.opacity(0.55))
                                        .lineStyle(StrokeStyle(lineWidth: 1, dash: [4, 4]))
                                        .accessibilityHidden(true)
                                }
                        }
                        .chartForegroundStyleScale(domain: modelNames, range: colors)
                        .chartYAxis {
                            AxisMarks(position: .trailing, values: .automatic(desiredCount: 3)) { value in
                                AxisGridLine().foregroundStyle(.quaternary)
                                AxisValueLabel {
                                    if let value = value.as(Int64.self) {
                                        Text(TokenQuantityFormatter.compactString(value))
                                    }
                                }
                            }
                        }
                        .chartXAxis {
                            AxisMarks(values: buckets.map(\.key)) { value in
                                AxisValueLabel {
                                    if let value = value.as(String.self) {
                                        Text(compactDailyTrendDate(value))
                                    }
                                }
                            }
                        }
                        .chartLegend(.hidden)
                        .chartXSelection(value: $selectedDailyTrendKey)
                        .chartOverlay { proxy in
                            GeometryReader { geometry in
                                Rectangle()
                                    .fill(.clear)
                                    .contentShape(Rectangle())
                                    .onContinuousHover { phase in
                                        switch phase {
                                        case .active(let location):
                                            guard let plotFrame = proxy.plotFrame else {
                                                selectedDailyTrendKey = nil
                                                return
                                            }
                                            let plotRect = geometry[plotFrame]
                                            guard plotRect.contains(location) else {
                                                selectedDailyTrendKey = nil
                                                return
                                            }
                                            selectedDailyTrendKey = proxy.value(
                                                atX: location.x - plotRect.origin.x,
                                                as: String.self
                                            )
                                        case .ended:
                                            selectedDailyTrendKey = nil
                                        }
                                    }
                            }
                        }
                        .frame(height: 142)

                        if let selectedBucket {
                            dailyTrendHoverDetail(
                                selectedBucket,
                                modelNames: modelNames,
                                colors: colors
                            )
                            .allowsHitTesting(false)
                            .transition(.opacity)
                        } else {
                            LazyVGrid(
                                columns: [GridItem(.adaptive(minimum: 108), spacing: 8)],
                                alignment: .leading,
                                spacing: 6
                            ) {
                                ForEach(Array(modelNames.enumerated()), id: \.element) { index, name in
                                    HStack(spacing: 5) {
                                        Image(systemName: "circle.fill")
                                            .font(.system(size: 7))
                                            .foregroundStyle(colors[index])
                                        Text(name)
                                            .font(.caption2)
                                            .foregroundStyle(.secondary)
                                            .lineLimit(1)
                                    }
                                }
                            }
                        }
                    }
                }
            }
        }
        .accessibilityElement(children: .contain)
        .accessibilityIdentifier("popover.daily-trend")
    }

    private func dailyTrendModelNames(_ buckets: [UsageModelTrendBucket]) -> [String] {
        var names: [String] = []
        var seen = Set<String>()
        for bucket in buckets {
            for segment in bucket.segments where seen.insert(segment.modelName).inserted {
                names.append(segment.modelName)
            }
        }
        return names
    }

    private func dailyTrendColors(count: Int) -> [Color] {
        let palette: [Color] = [.blue, .green, .orange, .purple, .pink, .teal, .indigo, .mint]
        return (0..<count).map { palette[$0 % palette.count] }
    }

    private func dailyTrendHoverDetail(
        _ bucket: UsageModelTrendBucket,
        modelNames: [String],
        colors: [Color]
    ) -> some View {
        let shape = RoundedRectangle(cornerRadius: 10, style: .continuous)
        return VStack(alignment: .leading, spacing: 6) {
            HStack(alignment: .firstTextBaseline, spacing: 8) {
                Text(compactDailyTrendDate(bucket.key))
                    .font(.system(size: 12, weight: .bold))
                Spacer(minLength: 12)
                Text("总计 \(TokenQuantityFormatter.compactString(bucket.totalTokens)) Token")
                    .font(.caption2.monospacedDigit())
                    .foregroundStyle(.secondary)
            }
            Divider()
            TokenBreakdownView(tokens: bucket.tokenBreakdown, style: .compact)
            Divider()
            LazyVGrid(
                columns: [GridItem(.flexible()), GridItem(.flexible())],
                alignment: .leading,
                spacing: 7
            ) {
                ForEach(bucket.segments) { segment in
                    VStack(alignment: .leading, spacing: 2) {
                        HStack(spacing: 5) {
                            Image(systemName: "circle.fill")
                                .font(.system(size: 6))
                                .foregroundStyle(dailyTrendColor(
                                    for: segment.modelName,
                                    modelNames: modelNames,
                                    colors: colors
                                ))
                            Text(segment.modelName)
                                .font(.caption2)
                                .lineLimit(1)
                        }
                        Text(
                            "\(TokenQuantityFormatter.compactString(segment.tokens)) · "
                                + dailyTrendShareText(segment.tokens, total: bucket.totalTokens)
                        )
                        .font(.caption2.monospacedDigit())
                        .foregroundStyle(.secondary)
                        .padding(.leading, 11)
                    }
                }
            }
        }
        .padding(9)
        .frame(maxWidth: .infinity)
        .background(.primary.opacity(0.035), in: shape)
        .overlay(shape.strokeBorder(.primary.opacity(0.1), lineWidth: 1))
        .accessibilityElement(children: .combine)
        .accessibilityLabel(
            "\(compactDailyTrendDate(bucket.key))，总计 "
                + "\(TokenQuantityFormatter.string(bucket.totalTokens)) Token"
        )
    }

    private func dailyTrendColor(
        for modelName: String,
        modelNames: [String],
        colors: [Color]
    ) -> Color {
        guard let index = modelNames.firstIndex(of: modelName), colors.indices.contains(index) else {
            return .secondary
        }
        return colors[index]
    }

    private func dailyTrendShareText(_ tokens: Int64, total: Int64) -> String {
        guard total > 0 else { return "0%" }
        return String(format: "%.0f%%", Double(tokens) / Double(total) * 100)
    }

    private func compactDailyTrendDate(_ key: String) -> String {
        let parts = key.split(separator: "-")
        guard parts.count == 3, let month = Int(parts[1]), let day = Int(parts[2]) else {
            return key
        }
        return "\(month)/\(day)"
    }

    private func costSection(_ overview: OverviewPresentation) -> some View {
        VStack(alignment: .leading, spacing: 10) {
            PopoverSectionTitle(title: "API 等价成本", systemImage: "dollarsign.circle")
            PulseCard {
                VStack(alignment: .leading, spacing: 6) {
                    HStack(alignment: .firstTextBaseline) {
                        Text(metricText(overview.estimatedCost, cost: true))
                            .font(.system(size: 20, weight: .bold, design: .rounded))
                            .monospacedDigit()
                        Spacer()
                        Text(overview.usageRangeLabel).font(.caption).foregroundStyle(.secondary)
                    }
                    TokenBreakdownView(tokens: overview.tokenBreakdown, style: .compact)
                    Text("本地会话估算")
                        .font(.caption2)
                        .foregroundStyle(.tertiary)
                }
            }
        }
    }

    private func projectRankingSection(_ overview: OverviewPresentation) -> some View {
        VStack(alignment: .leading, spacing: 10) {
            PopoverSectionTitle(title: "本周项目 Token 排行", systemImage: "chart.bar.xaxis")
            PulseCard(padding: 0) {
                if !overview.weeklyProjectRankingAvailable {
                    Text("周额度项目排行暂时不可用")
                        .foregroundStyle(.secondary)
                        .padding(12)
                } else if overview.weeklyProjectRanking.isEmpty {
                    Text("本周暂无已归类项目用量")
                        .foregroundStyle(.secondary)
                        .padding(12)
                } else {
                    VStack(spacing: 0) {
                        ForEach(Array(overview.weeklyProjectRanking.enumerated()), id: \.element.id) { index, project in
                            HStack(spacing: 10) {
                                Text("\(index + 1)")
                                    .font(.caption.bold())
                                    .foregroundStyle(.secondary)
                                    .frame(width: 18)
                                VStack(alignment: .leading, spacing: 3) {
                                    Text(project.title).font(.system(size: 13, weight: .semibold)).lineLimit(1)
                                    Text("周额度周期")
                                        .font(.caption2)
                                        .foregroundStyle(.secondary)
                                        .lineLimit(1)
                                }
                                Spacer(minLength: 8)
                                Text(metricText(project.tokens))
                                    .font(.caption.weight(.semibold))
                                    .monospacedDigit()
                                    .foregroundStyle(.secondary)
                            }
                            .padding(.horizontal, 12)
                            .padding(.vertical, 9)
                            if index < overview.weeklyProjectRanking.count - 1 {
                                Divider().padding(.leading, 29)
                            }
                        }
                    }
                }
            }
        }
    }

    private func nextExpiryText(_ credits: ResetCreditsPresentation) -> String {
        credits.nextExpiresAtMS == nil ? "无近期到期" : "最近到期 \(minimumRemainingText(credits))"
    }
}

private enum PopoverRoute {
    case main
    case resetCredits
    case displaySettings
}

private enum PopoverFocusTarget: Hashable {
    case openOverview
    case refresh
    case openProject
    case copyPopoverScreenshot
    case resetCredits
    case settings
    case quit
}

private enum PopoverScreenshotFeedback: Equatable {
    case idle
    case copied
    case failed

    var systemImage: String {
        switch self {
        case .idle: "camera"
        case .copied: "checkmark"
        case .failed: "exclamationmark.triangle"
        }
    }

    var title: String {
        switch self {
        case .idle: "复制 Popover 完整截图"
        case .copied: "Popover 完整截图已复制"
        case .failed: "复制失败，请重试"
        }
    }
}

private struct RefreshArrowSymbol: View {
    let isAnimating: Bool

    var body: some View {
        TimelineView(.animation(minimumInterval: 1 / 30, paused: !isAnimating)) { context in
            Image(systemName: "arrow.triangle.2.circlepath")
                .rotationEffect(.degrees(rotationAngle(at: context.date)))
        }
    }

    private func rotationAngle(at date: Date) -> Double {
        guard isAnimating else { return 0 }
        let duration = 0.8
        return date.timeIntervalSinceReferenceDate
            .truncatingRemainder(dividingBy: duration) / duration * 360
    }
}

private struct PopoverHeader: View {
    let title: String
    let accountSummary: PopoverAccountSummaryPresentation?
    let captureSource: PopoverCaptureSource
    let isPrivacyHidden: Bool
    let screenshotFeedback: PopoverScreenshotFeedback
    let onOpenProject: @MainActor () -> Void
    let onCopyPopoverScreenshot: @MainActor () -> Void
    let focusedControl: FocusState<PopoverFocusTarget?>.Binding
    let onOpen: @MainActor () -> Void
    let onRefresh: @MainActor () -> Void
    let canRefresh: Bool
    let isRefreshing: Bool

    var body: some View {
        HStack(alignment: .top, spacing: 0) {
            VStack(alignment: .leading, spacing: 6) {
                Text(title)
                    .font(.headline)
                PopoverAccountCapsule(
                    summary: accountSummary,
                    captureSource: captureSource,
                    isPrivacyHidden: isPrivacyHidden
                )
            }
            Spacer(minLength: 12)
            HStack(spacing: 4) {
                PopoverHeaderButton(
                    title: "打开 GitHub 项目主页",
                    identifier: "popover.open-project",
                    target: .openProject,
                    focusedControl: focusedControl,
                    action: onOpenProject
                ) {
                    GitHubMarkShape().fill(.primary)
                }

                Rectangle()
                    .fill(Color.primary.opacity(0.14))
                    .frame(
                        width: 1,
                        height: PopoverInteractionMetrics.headerDividerHeight
                    )
                    .padding(.horizontal, 2)
                    .accessibilityHidden(true)

                PopoverHeaderButton(
                    title: screenshotFeedback.title,
                    identifier: "popover.copy-screenshot",
                    target: .copyPopoverScreenshot,
                    focusedControl: focusedControl,
                    action: onCopyPopoverScreenshot
                ) {
                    Image(systemName: screenshotFeedback.systemImage)
                }

                PopoverHeaderButton(
                    title: isRefreshing ? "正在刷新本地数据" : "刷新本地数据",
                    identifier: "popover.refresh",
                    target: .refresh,
                    focusedControl: focusedControl,
                    isEnabled: canRefresh,
                    action: onRefresh
                ) {
                    RefreshArrowSymbol(isAnimating: isRefreshing)
                }

                PopoverHeaderButton(
                    title: "打开主窗口",
                    identifier: "popover.open-overview",
                    target: .openOverview,
                    focusedControl: focusedControl,
                    action: onOpen
                ) {
                    Image(systemName: "rectangle.split.2x1")
                }
            }
        }
        .padding(.horizontal, 18)
        .padding(.vertical, 12)
        .nativeGlass(in: Rectangle())
    }
}

private struct PopoverAccountCapsule: View {
    let summary: PopoverAccountSummaryPresentation?
    let captureSource: PopoverCaptureSource
    let isPrivacyHidden: Bool

    var body: some View {
        HStack(spacing: 4) {
            Text(summary?.planText ?? "--")
            Text(summary?.emailText ?? "--")
                .lineLimit(1)
                .truncationMode(.middle)
        }
        .opacity(isPrivacyHidden ? 0 : 1)
        .overlay {
            if isPrivacyHidden {
                Image(systemName: "eye.slash")
                    .accessibilityHidden(true)
            }
        }
        .font(.caption)
        .foregroundStyle(.orange)
        .padding(.horizontal, 8)
        .padding(.vertical, 4)
        .background(Color.orange.opacity(0.14), in: Capsule())
        .accessibilityElement(children: .ignore)
        .accessibilityLabel(
            isPrivacyHidden
                ? "截图中账号与套餐信息已隐藏"
                : summary?.accessibilityLabel ?? "正在读取 Codex 账户与套餐信息"
        )
        .accessibilityIdentifier("popover.account-summary")
        .background(
            PopoverPrivacyRenderProbe(
                source: captureSource,
                isPrivacyHidden: isPrivacyHidden
            )
            .allowsHitTesting(false)
        )
    }
}

private struct ResetCreditsDetailView: View {
    let overview: OverviewPresentation?
    let onBack: () -> Void

    var body: some View {
        ScrollView {
            VStack(alignment: .leading, spacing: 18) {
                PopoverBackButton(onBack: onBack)
                if let credits = overview?.resetCredits {
                    HStack(alignment: .firstTextBaseline) {
                        VStack(alignment: .leading, spacing: 3) {
                            Text("重置次数").font(.title2.bold())
                            Text("更新于 \(credits.lastSuccessAtMS.map(relativeTimestamp) ?? "--")")
                                .font(.caption).foregroundStyle(.secondary)
                        }
                        Spacer()
                        Text("\(optionalCount(credits.availableCount))/\(optionalCount(credits.totalCount))")
                            .font(.title2.bold().monospacedDigit())
                    }
                    LazyVGrid(columns: [GridItem(.flexible()), GridItem(.flexible())], spacing: 10) {
                        SummaryTile(label: "可用", value: optionalCount(credits.availableCount), color: .green)
                        SummaryTile(label: "已使用", value: optionalCount(credits.redeemedCount), color: .orange)
                        SummaryTile(label: "总剩余", value: durationText(credits.cumulativeRemainingMS), color: .blue)
                        SummaryTile(label: "最近到期", value: minimumRemainingText(credits), color: .orange)
                    }
                    VStack(alignment: .leading, spacing: 8) {
                        Text("到期风险").font(.headline)
                        GeometryReader { geometry in
                            Capsule().fill(.quaternary)
                                .overlay(alignment: .leading) {
                                    Capsule().fill(Color.green).frame(width: geometry.size.width * availabilityRatio(credits))
                                }
                        }
                        .frame(height: 10)
                        Text("可用 \(optionalCount(credits.availableCount)) · 已使用或过期 \(unavailableCount(credits))")
                            .font(.caption).foregroundStyle(.secondary)
                    }
                    VStack(alignment: .leading, spacing: 10) {
                        Text("次数").font(.headline)
                        if credits.items.isEmpty {
                            PulseCard { Text("当前没有逐条次数事实").foregroundStyle(.secondary) }
                        } else {
                            ForEach(Array(credits.items.enumerated()), id: \.element.id) { index, item in
                                PulseCard {
                                    HStack(alignment: .top) {
                                        VStack(alignment: .leading, spacing: 4) {
                                            Text("次数 \(index + 1)").font(.system(size: 13, weight: .semibold))
                                            Text("到期：\(absoluteTimestamp(item.expiresAtMS))")
                                                .font(.caption).foregroundStyle(.secondary)
                                        }
                                        Spacer()
                                        VStack(alignment: .trailing, spacing: 4) {
                                            StatusCapsule(status: item.status)
                                            Text(item.remainingMS.map(durationText) ?? "--")
                                                .font(.caption.monospacedDigit()).foregroundStyle(.secondary)
                                        }
                                    }
                                }
                            }
                        }
                    }
                } else {
                    Text("重置次数").font(.title2.bold())
                    ProgressView().frame(maxWidth: .infinity, minHeight: 480)
                }
            }
            .padding(18)
        }
        .scrollIndicators(.hidden)
    }
}

private struct StatusDisplaySettingsView: View {
    @ObservedObject var preferences: StatusBarDisplayPreferences
    let onBack: () -> Void
    let onOpenSettings: () -> Void

    var body: some View {
        ScrollView {
            VStack(alignment: .leading, spacing: 20) {
                PopoverBackButton(onBack: onBack)
                VStack(alignment: .leading, spacing: 3) {
                    Text("显示设置").font(.title2.bold())
                    Text("状态栏与弹窗内容").font(.caption).foregroundStyle(.secondary)
                }
                VStack(alignment: .leading, spacing: 10) {
                    Text("状态栏样式").font(.headline)
                    Picker("状态栏样式", selection: $preferences.style) {
                        ForEach(StatusBarStyle.allCases) { style in Text(style.title).tag(style) }
                    }
                    .pickerStyle(.menu)
                    .labelsHidden()
                    .frame(maxWidth: .infinity, minHeight: PopoverInteractionMetrics.minimumHitTarget, alignment: .leading)
                    .contentShape(Rectangle())
                    .accessibilityIdentifier("popover.status-style")
                    Text("三种样式均显示真实额度周期剩余与同周期 Token；范围无法对齐时用量显示 --。")
                        .font(.caption).foregroundStyle(.secondary)
                }
                PulseCard {
                    VStack(alignment: .leading, spacing: 14) {
                        SettingsToggle(identifier: "cost-summary", title: "显示 API 成本摘要", subtitle: "当前周额度周期的本地 API 等价估算", value: $preferences.showCostSummary)
                        Divider()
                        SettingsToggle(identifier: "project-ranking", title: "显示项目排行", subtitle: "按周额度周期展示 Token 前 5 的已归类项目", value: $preferences.showProjectRanking)
                    }
                }
                Button(action: onOpenSettings) {
                    Label("打开完整设置", systemImage: "gearshape")
                        .frame(maxWidth: .infinity)
                }
                .buttonStyle(NativeGlassButtonStyle())
                .accessibilityIdentifier("popover.open-settings")
            }
            .padding(18)
        }
        .scrollIndicators(.hidden)
    }
}

private struct SettingsToggle: View {
    let identifier: String
    let title: String
    let subtitle: String
    @Binding var value: Bool

    var body: some View {
        Toggle(isOn: $value) {
            VStack(alignment: .leading, spacing: 3) {
                Text(title).font(.system(size: 13, weight: .semibold))
                Text(subtitle).font(.caption).foregroundStyle(.secondary)
            }
        }
        .toggleStyle(.switch)
        .padding(.vertical, 4)
        .frame(maxWidth: .infinity, minHeight: PopoverInteractionMetrics.minimumHitTarget, alignment: .leading)
        .contentShape(Rectangle())
        .accessibilityIdentifier("popover.toggle.\(identifier)")
    }
}

private struct PopoverBackButton: View {
    let onBack: () -> Void

    var body: some View {
        Button(action: onBack) { Label("返回", systemImage: "chevron.left") }
            .buttonStyle(NativeGlassButtonStyle())
            .accessibilityIdentifier("popover.back")
    }
}

private struct PopoverFooter: View {
    let onSettings: () -> Void
    let onQuit: @MainActor () -> Void
    let focusedControl: FocusState<PopoverFocusTarget?>.Binding

    var body: some View {
        HStack {
            Button(action: onSettings) { Label("设置", systemImage: "slider.horizontal.3") }
                .accessibilityIdentifier("popover.settings")
                .focusable(true)
                .focused(focusedControl, equals: .settings)
            Spacer()
            Button(action: onQuit) { Label("退出", systemImage: "power") }
                .accessibilityIdentifier("popover.quit")
                .focusable(true)
                .focused(focusedControl, equals: .quit)
        }
        .buttonStyle(NativeGlassButtonStyle())
        .padding(.horizontal, 18)
        .padding(.vertical, 12)
        .nativeGlass(in: Rectangle())
    }
}

private struct PopoverSectionTitle: View {
    let title: String
    let systemImage: String

    var body: some View {
        Label(title, systemImage: systemImage).font(.system(size: 14, weight: .bold))
    }
}

private struct PulseCard<Content: View>: View {
    @Environment(\.controlActiveState) private var controlActiveState
    let padding: CGFloat
    @ViewBuilder let content: Content

    init(padding: CGFloat = 12, @ViewBuilder content: () -> Content) {
        self.padding = padding
        self.content = content()
    }

    var body: some View {
        let shape = RoundedRectangle(cornerRadius: PopoverInteractionMetrics.cardCornerRadius, style: .continuous)
        content
            .padding(padding)
            .frame(maxWidth: .infinity, alignment: .leading)
            .background(cardFill, in: shape)
            .nativeGlass(in: shape)
            .overlay(shape.strokeBorder(cardBorder, lineWidth: 1))
            .contentShape(shape)
    }

    private var cardFill: Color {
        .primary.opacity(controlActiveState == .inactive ? 0.065 : 0.03)
    }

    private var cardBorder: Color {
        .primary.opacity(controlActiveState == .inactive ? 0.18 : 0.1)
    }
}

private struct SummaryTile: View {
    let label: String
    let value: String
    let color: Color

    var body: some View {
        PulseCard {
            VStack(alignment: .leading, spacing: 6) {
                HStack(spacing: 6) { Circle().fill(color).frame(width: 7, height: 7); Text(label) }
                    .font(.caption).foregroundStyle(.secondary)
                Text(value).font(.system(size: 15, weight: .bold, design: .rounded)).monospacedDigit()
            }
        }
    }
}

private struct StatusCapsule: View {
    let status: String

    var body: some View {
        Text(statusTitle)
            .font(.caption2.bold())
            .padding(.horizontal, 7)
            .padding(.vertical, 3)
            .background(statusColor.opacity(0.2), in: Capsule())
            .foregroundStyle(statusColor)
    }

    private var statusTitle: String {
        switch status {
        case "available": "可用"
        case "redeemed", "used": "已使用"
        case "expired": "已过期"
        default: "未知"
        }
    }

    private var statusColor: Color {
        status == "available" ? .green : (status == "expired" ? .secondary : .orange)
    }
}

private struct PopoverHeaderButton<Label: View>: View {
    let title: String
    let identifier: String
    let target: PopoverFocusTarget
    let focusedControl: FocusState<PopoverFocusTarget?>.Binding
    let isEnabled: Bool
    let action: @MainActor () -> Void
    let label: Label
    @State private var isHovered = false

    init(
        title: String,
        identifier: String,
        target: PopoverFocusTarget,
        focusedControl: FocusState<PopoverFocusTarget?>.Binding,
        isEnabled: Bool = true,
        action: @escaping @MainActor () -> Void,
        @ViewBuilder label: () -> Label
    ) {
        self.title = title
        self.identifier = identifier
        self.target = target
        self.focusedControl = focusedControl
        self.isEnabled = isEnabled
        self.action = action
        self.label = label()
    }

    var body: some View {
        Button(action: action) {
            label
                .frame(
                    width: PopoverInteractionMetrics.headerIconSize,
                    height: PopoverInteractionMetrics.headerIconSize
                )
        }
        .buttonStyle(PopoverHeaderButtonStyle(
            isHovered: isHovered,
            isFocused: focusedControl.wrappedValue == target
        ))
        .disabled(!isEnabled)
        .opacity(isEnabled ? 1 : 0.35)
        .frame(
            width: PopoverInteractionMetrics.minimumHitTarget,
            height: PopoverInteractionMetrics.minimumHitTarget
        )
        .contentShape(Circle())
        .padding(-PopoverInteractionMetrics.headerHitSlop)
        .onHover { isHovered = $0 }
        .help(title)
        .accessibilityLabel(title)
        .accessibilityIdentifier(identifier)
        .focusable(true)
        .focused(focusedControl, equals: target)
        .focusEffectDisabled()
        .onKeyPress(.return) {
            guard isEnabled else { return .ignored }
            action()
            return .handled
        }
        .onKeyPress(.space) {
            guard isEnabled else { return .ignored }
            action()
            return .handled
        }
    }
}

private struct PopoverHeaderButtonStyle: ButtonStyle {
    let isHovered: Bool
    let isFocused: Bool

    func makeBody(configuration: Configuration) -> some View {
        configuration.label
            .frame(
                width: PopoverInteractionMetrics.headerButtonDiameter,
                height: PopoverInteractionMetrics.headerButtonDiameter
            )
            .background {
                Circle()
                    .fill(Color.primary.opacity(backgroundOpacity(
                        pressed: configuration.isPressed
                    )))
            }
            .overlay {
                if isFocused {
                    Circle()
                        .strokeBorder(Color.primary.opacity(0.42), lineWidth: 1)
                }
            }
            .contentShape(Circle())
    }

    private func backgroundOpacity(pressed: Bool) -> Double {
        if pressed { return 0.16 }
        if isFocused { return 0.1 }
        return isHovered ? 0.08 : 0
    }
}

private struct GitHubMarkShape: Shape {
    func path(in rect: CGRect) -> Path {
        let scale = min(rect.width / 98, rect.height / 96)
        let origin = CGPoint(
            x: rect.midX - 49 * scale,
            y: rect.midY - 48 * scale
        )
        func point(_ x: CGFloat, _ y: CGFloat) -> CGPoint {
            CGPoint(x: origin.x + x * scale, y: origin.y + y * scale)
        }

        var path = Path()
        path.move(to: point(41.4395, 69.3848))
        path.addCurve(
            to: point(19.9062, 46.9902),
            control1: point(28.8066, 67.8535),
            control2: point(19.9062, 58.7617)
        )
        path.addCurve(
            to: point(24.5, 33.5918),
            control1: point(19.9062, 42.2051),
            control2: point(21.6289, 37.0371)
        )
        path.addCurve(
            to: point(24.8828, 20.959),
            control1: point(23.2559, 30.4336),
            control2: point(23.4473, 23.7344)
        )
        path.addCurve(
            to: point(36.9414, 25.2656),
            control1: point(28.7109, 20.4805),
            control2: point(33.8789, 22.4902)
        )
        path.addCurve(
            to: point(49.0957, 23.543),
            control1: point(40.5781, 24.1172),
            control2: point(44.4062, 23.543)
        )
        path.addCurve(
            to: point(61.0586, 25.1699),
            control1: point(53.7852, 23.543),
            control2: point(57.6133, 24.1172)
        )
        path.addCurve(
            to: point(73.1172, 20.959),
            control1: point(64.0254, 22.4902),
            control2: point(69.2891, 20.4805)
        )
        path.addCurve(
            to: point(73.4043, 33.4961),
            control1: point(74.457, 23.543),
            control2: point(74.6484, 30.2422)
        )
        path.addCurve(
            to: point(78.0937, 46.9902),
            control1: point(76.4668, 37.1328),
            control2: point(78.0937, 42.0137)
        )
        path.addCurve(
            to: point(56.3691, 69.2891),
            control1: point(78.0937, 58.7617),
            control2: point(69.1934, 67.6621)
        )
        path.addCurve(
            to: point(61.8242, 81.252),
            control1: point(59.623, 71.3945),
            control2: point(61.8242, 75.9883)
        )
        path.addLine(to: point(61.8242, 91.2051))
        path.addCurve(
            to: point(67.0879, 94.5547),
            control1: point(61.8242, 94.0762),
            control2: point(64.2168, 95.7031)
        )
        path.addCurve(
            to: point(98, 49.1914),
            control1: point(84.4102, 87.9512),
            control2: point(98, 70.6289)
        )
        path.addCurve(
            to: point(48.9043, 0),
            control1: point(98, 22.1074),
            control2: point(75.9883, 0)
        )
        path.addCurve(
            to: point(0, 49.1914),
            control1: point(21.8203, 0),
            control2: point(0, 22.1074)
        )
        path.addCurve(
            to: point(31.6777, 94.6504),
            control1: point(0, 70.4375),
            control2: point(13.4941, 88.0469)
        )
        path.addCurve(
            to: point(36.75, 91.3008),
            control1: point(34.2617, 95.6074),
            control2: point(36.75, 93.8848)
        )
        path.addLine(to: point(36.75, 83.6445))
        path.addCurve(
            to: point(32.1562, 84.6016),
            control1: point(35.4102, 84.2188),
            control2: point(33.6875, 84.6016)
        )
        path.addCurve(
            to: point(19.4277, 74.7441),
            control1: point(25.8398, 84.6016),
            control2: point(22.1074, 81.1563)
        )
        path.addCurve(
            to: point(15.0254, 70.3418),
            control1: point(18.375, 72.1602),
            control2: point(17.2266, 70.6289)
        )
        path.addCurve(
            to: point(13.4941, 69.1934),
            control1: point(13.877, 70.2461),
            control2: point(13.4941, 69.7676)
        )
        path.addCurve(
            to: point(17.3223, 67.1836),
            control1: point(13.4941, 68.0449),
            control2: point(15.4082, 67.1836)
        )
        path.addCurve(
            to: point(24.9785, 72.4473),
            control1: point(20.0977, 67.1836),
            control2: point(22.4902, 68.9063)
        )
        path.addCurve(
            to: point(31.2949, 76.4668),
            control1: point(26.8926, 75.2227),
            control2: point(28.9023, 76.4668)
        )
        path.addCurve(
            to: point(37.4199, 73.4043),
            control1: point(33.6875, 76.4668),
            control2: point(35.2187, 75.6055)
        )
        path.addCurve(
            to: point(41.4395, 69.3848),
            control1: point(39.0469, 71.7773),
            control2: point(40.291, 70.3418)
        )
        path.closeSubpath()
        return path
    }
}

private struct NativeGlassButtonStyle: ButtonStyle {
    @Environment(\.controlActiveState) private var controlActiveState
    @Environment(\.isEnabled) private var isEnabled

    func makeBody(configuration: Configuration) -> some View {
        let shape = RoundedRectangle(cornerRadius: 8, style: .continuous)
        configuration.label
            .font(.system(size: 13, weight: .medium))
            .padding(.horizontal, 10)
            .frame(height: PopoverInteractionMetrics.compactButtonVisualHeight)
            .foregroundStyle(.primary)
            .background(buttonFill, in: shape)
            .nativeGlass(in: shape)
            .overlay(shape.fill(configuration.isPressed ? Color.primary.opacity(0.08) : .clear))
            .overlay(shape.strokeBorder(buttonBorder, lineWidth: 1))
            .scaleEffect(configuration.isPressed ? 0.97 : 1)
            .opacity(isEnabled ? (configuration.isPressed ? 0.76 : 1) : 0.5)
            .frame(minHeight: PopoverInteractionMetrics.minimumHitTarget)
            .contentShape(Rectangle())
            .padding(.vertical, -PopoverInteractionMetrics.compactButtonHitSlop)
            .animation(.easeOut(duration: 0.08), value: configuration.isPressed)
    }

    private var buttonFill: Color {
        .primary.opacity(controlActiveState == .inactive ? 0.075 : 0.04)
    }

    private var buttonBorder: Color {
        .primary.opacity(controlActiveState == .inactive ? 0.22 : 0.12)
    }
}

private struct InteractiveCardButtonStyle: ButtonStyle {
    func makeBody(configuration: Configuration) -> some View {
        configuration.label
            .modifier(InteractiveFeedbackModifier(
                shape: RoundedRectangle(cornerRadius: PopoverInteractionMetrics.cardCornerRadius, style: .continuous),
                isPressed: configuration.isPressed
            ))
    }
}

private struct InteractiveFeedbackModifier<S: InsettableShape>: ViewModifier {
    @Environment(\.isEnabled) private var isEnabled
    let shape: S
    let isPressed: Bool

    func body(content: Content) -> some View {
        content
            .overlay(shape.fill(isPressed ? Color.primary.opacity(0.08) : .clear))
            .contentShape(shape)
            .scaleEffect(isPressed ? 0.995 : 1)
            .opacity(isEnabled ? 1 : 0.55)
            .animation(.easeOut(duration: 0.08), value: isPressed)
    }
}

private struct NativePopoverBackdrop: View {
    @Environment(\.controlActiveState) private var controlActiveState

    var body: some View {
        Rectangle()
            .fill(Color.primary.opacity(controlActiveState == .inactive ? 0.025 : 0))
            .nativeGlass(in: Rectangle())
            .ignoresSafeArea()
    }
}

private enum PopoverInteractionMetrics {
    static let minimumHitTarget: CGFloat = 44
    static let headerButtonDiameter: CGFloat = 30
    static let headerIconSize: CGFloat = 16
    static let headerDividerHeight: CGFloat = 18
    static let headerHitSlop = (minimumHitTarget - headerButtonDiameter) / 2
    static let compactButtonVisualHeight: CGFloat = 28
    static let compactButtonHitSlop = (minimumHitTarget - compactButtonVisualHeight) / 2
    static let cardCornerRadius: CGFloat = 12
}

private struct NativeGlassModifier<S: Shape>: ViewModifier {
    let shape: S

    @ViewBuilder
    func body(content: Content) -> some View {
        if #available(macOS 26.0, *) {
            content.glassEffect(.regular, in: shape)
        } else {
            content.background(.ultraThinMaterial, in: shape)
        }
    }
}

private extension View {
    func nativeGlass<S: Shape>(in shape: S) -> some View {
        modifier(NativeGlassModifier(shape: shape))
    }
}

@MainActor
private func writePopoverScreenshotClipboard(_ text: String, _ png: Data) -> Bool {
    PopoverPasteboardPayload.write(text: text, png: png, to: .general)
}

private func percentText(_ value: Double?) -> String {
    value.map { String(format: "%.0f%%", $0) } ?? "--"
}

private func progress(_ value: Double?) -> CGFloat {
    CGFloat(max(0, min(100, value ?? 0))) / 100
}

private func quotaColor(_ value: Double?) -> Color {
    switch QuotaRemainingLevel(remainingPercent: value) {
    case .healthy: .green
    case .warning: .yellow
    case .critical: .red
    case .unavailable: .gray
    }
}

private func optionalCount(_ value: Int64?) -> String {
    value?.formatted() ?? "--"
}

private func durationText(_ milliseconds: Int64?) -> String {
    guard let milliseconds, milliseconds >= 0 else { return "--" }
    let totalMinutes = milliseconds / 60_000
    let days = totalMinutes / 1_440
    let hours = totalMinutes % 1_440 / 60
    let minutes = totalMinutes % 60
    if days > 0 { return "\(days)天 \(hours)小时" }
    if hours > 0 { return "\(hours)小时 \(minutes)分钟" }
    return "\(minutes)分钟"
}

private func relativeTimestamp(_ milliseconds: Int64) -> String {
    guard milliseconds > 0 else { return "时间未知" }
    let formatter = RelativeDateTimeFormatter()
    formatter.unitsStyle = .short
    return formatter.localizedString(for: Date(timeIntervalSince1970: Double(milliseconds) / 1_000), relativeTo: Date())
}

private func absoluteTimestamp(_ milliseconds: Int64) -> String {
    guard milliseconds > 0 else { return "--" }
    return Date(timeIntervalSince1970: Double(milliseconds) / 1_000).formatted(
        .dateTime.year().month().day().hour().minute()
    )
}

private func availabilityRatio(_ credits: ResetCreditsPresentation) -> CGFloat {
    guard let available = credits.availableCount, let total = credits.totalCount, total > 0 else { return 0 }
    return CGFloat(max(0, min(total, available))) / CGFloat(total)
}

private func unavailableCount(_ credits: ResetCreditsPresentation) -> String {
    guard let available = credits.availableCount, let total = credits.totalCount else { return "--" }
    return max(0, total - available).formatted()
}

private func minimumRemainingText(_ credits: ResetCreditsPresentation) -> String {
    credits.items.compactMap(\.remainingMS).min().map(durationText) ?? "--"
}
