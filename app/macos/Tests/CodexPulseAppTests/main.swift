import CodexPulseAppSupport
import CodexPulseCoreClient
import CodexPulseProtocolGenerated
import Foundation

private enum TestFailure: Error, CustomStringConvertible, Sendable {
    case mismatch(String)

    var description: String {
        switch self {
        case .mismatch(let message): message
        }
    }
}

private enum FakeFailure: Error, Sendable {
    case unavailable
}

private struct SessionPagePlan: Sendable {
    let delay: Duration
    let response: Codexpulse_Core_V1_SessionListResponse
    let fails: Bool

    init(
        delay: Duration,
        response: Codexpulse_Core_V1_SessionListResponse,
        fails: Bool = false
    ) {
        self.delay = delay
        self.response = response
        self.fails = fails
    }
}

private func expect(_ condition: @autoclosure () -> Bool, _ message: String) throws {
    guard condition() else { throw TestFailure.mismatch(message) }
}

private func testPrimaryPagesSmokeSummaryIncludesProjectDetailEvidence() throws {
    let summary = PrimaryPagesSmokeSummary(
        sessions: 0,
        projects: 0,
        sources: 0,
        jobs: 0,
        healthEvents: 0,
        usageTrend: 0,
        usageModels: 0,
        usageModelTrend: 0,
        usageModelReconciled: 0,
        usageCostKnown: false,
        quotaWindows: 0,
        detailsRead: 0,
        settingsMutation: "skipped",
        unavailableSteps: []
    )
    try expect(
        summary.stableDescription.contains(
            "project_detail_cost=unknown project_detail_models=0"),
        "primary-page smoke summary must expose project detail cost and model evidence"
    )
}

private func mainWindowSource(_ fileName: String) throws -> String {
    let packageRoot = URL(fileURLWithPath: #filePath)
        .deletingLastPathComponent()
        .deletingLastPathComponent()
        .deletingLastPathComponent()
    let fileURL =
        packageRoot
        .appendingPathComponent("Sources/CodexPulseApp", isDirectory: true)
        .appendingPathComponent(fileName)
    return try String(contentsOf: fileURL, encoding: .utf8)
}

private func testMainWindowCopyDoesNotExposeImplementationLanguage() throws {
    let files = [
        "RootView.swift",
        "FeatureViewSupport.swift",
        "SessionsProjectsViews.swift",
        "QuotaHealthViews.swift",
        "SourcesJobsSettingsViews.swift",
    ]
    let forbiddenFragments = [
        "\"估算值",
        "\"来自 Helper",
        "\"Helper ",
        "\"Helper未",
        "\"Core ",
        "\"CoreService",
        "\"RPC",
        "\"Revision",
        "\"revision conflict",
        "\"reconcile_required",
        "\"provider",
        "\"JSONL",
        "\"SQLite",
        "\"contract",
        "KeyValueRow(key: \"新鲜度\"",
        "? \"unknown\"",
        "StatusPill(text: \"stale\")",
        "核心组件",
        "可信",
        "事实",
        "采样",
        "已索引",
        "退化",
        "错误代码",
        "恢复入口",
        "归属 key",
        "任务 ID",
        "会话 ID",
        "契约",
        "归因",
        "调度器",
        "\"调度",
        "诊断",
        "项目标识",
        "模型标识",
        "归属准确度",
    ]

    for file in files {
        let source = try mainWindowSource(file)
        for fragment in forbiddenFragments {
            try expect(
                !source.contains(fragment),
                "main-window copy in \(file) still exposes implementation fragment: \(fragment)"
            )
        }
    }
}

private func testOverviewUsesOneNavigationAndARealTrendChart() throws {
    let source = try mainWindowSource("RootView.swift")
    try expect(
        !source.contains("private var navigationSection"),
        "overview must not duplicate sidebar navigation")
    try expect(
        !source.contains(".frame(maxWidth: 1_240)"),
        "overview must expand with a wide detail pane instead of leaving a fixed-width blank column")
    try expect(source.contains("import Charts"), "overview trend must use Swift Charts")
    try expect(source.contains("AreaMark("), "overview trend must use a quiet area chart")
    try expect(source.contains("LineMark("), "overview trend must retain a readable trend line")
    try expect(!source.contains("BarMark("), "overview must not fall back to the old bar chart")
    try expect(source.contains("PointMark("), "overview trend must expose each selectable point")
    try expect(source.contains("RuleMark("), "overview trend must highlight the selected time")
    try expect(
        source.contains("overflowResolution: .init(x: .fit(to: .chart), y: .disabled)"),
        "overview detail must stay inside the chart without padding either scale")
    try expect(
        source.contains(".chartXSelection(value: $selectedTrendDate)"),
        "overview trend must bind pointer selection to its horizontal domain")
    try expect(
        !source.contains(".chartGesture { proxy in"),
        "overview trend must preserve the macOS hover selection gesture")
    try expect(
        !source.contains("DragGesture(minimumDistance: 0)"),
        "overview trend must not require a click before showing details")
    try expect(
        source.contains("TrendSelectionResolver.nearest"),
        "overview trend must snap a continuous chart selection to the nearest real point")
    try expect(
        source.contains("AxisValueLabel {\n                            if let value = value.as(Int64.self)"),
        "overview Token axis must use the same Chinese magnitude units as project metrics")
    try expect(
        source.contains("Picker(\"概览范围\""),
        "overview must expose the approved range selector")
    try expect(
        source.contains("Text(\"Token 总量\")"),
        "overview summary must promote total Token usage as the primary metric")
    try expect(
        source.contains("private func usageBreakdownMetric(")
            && source.contains("title: \"输入\"")
            && source.contains("title: \"输出\""),
        "overview summary must place input and output breakdowns below the total")
    try expect(
        !source.contains("TokenBreakdownView(tokens: overview.tokenBreakdown)"),
        "overview summary must not render input, output, and total as equal columns")
    try expect(
        source.contains(
            ".frame(maxWidth: .infinity, alignment: .leading)\n\n            Divider()\n\n            VStack(alignment: .leading"
        ),
        "overview summary must separate Token usage and place API equivalent cost at the top-left")
    try expect(
        !source.contains("按公开 API 价格换算，不代表实际账单"),
        "overview summary must keep API equivalent cost free of redundant explanatory copy")
    try expect(source.contains("项目消耗"), "overview must explain where usage went by project")
    try expect(
        !source.contains("if let other = overview.otherProjectTokens"),
        "overview must render the already-merged project breakdown instead of appending another Other row")
    try expect(
        source.contains("if project.isOther"),
        "the merged Other row must render as a summary instead of a project link")
    try expect(
        !source.contains(".disabled(project.id.isEmpty)"),
        "the merged Other row must not be dimmed as a disabled project button")
    try expect(source.contains("高消耗会话"), "overview must rank high-consumption sessions")
    try expect(
        source.contains("onSelectProject") && source.contains("onSelectSession"),
        "overview rankings must navigate to their details")
}

private func testOverviewParallelReadsAvoidAsyncLetReleaseCrash() throws {
    let source = try String(
        contentsOf: URL(fileURLWithPath: #filePath)
            .deletingLastPathComponent()
            .deletingLastPathComponent()
            .deletingLastPathComponent()
            .appendingPathComponent(
                "Sources/CodexPulseAppSupport/AppRuntime.swift",
                isDirectory: false
            ),
        encoding: .utf8
    )
    try expect(
        !source.contains("async let"),
        "release overview reads must avoid the Swift task-stack deallocation crash")
    try expect(
        source.contains("withTaskCancellationHandler")
            && source.contains("let usageTask = Task")
            && source.contains("usageTask.cancel()"),
        "parallel overview reads must preserve explicit cancellation")
}

private func testUsageChartStacksModelsWithLocalizedHoverDetails() throws {
    let source = try mainWindowSource("QuotaHealthViews.swift")
    try expect(
        source.contains("UsageModelTrendResolver.buckets")
            && source.contains("foregroundStyle(by: .value(\"模型\"")
            && source.contains("response.models"),
        "usage chart must stack real model buckets instead of repeating the overall total")
    try expect(
        source.contains("TokenQuantityFormatter.compactString")
            && source.contains("AxisValueLabel {")
            && !source.contains("AxisValueLabel(format:"),
        "usage chart axis must use localized Token units instead of scientific notation")
    try expect(
        source.contains(".chartXSelection(value: $selectedTrendKey)")
            && source.contains("selectedTrendDetail")
            && source.contains("RuleMark("),
        "usage chart must expose pointer-selected per-model details")
}

private func testSessionAndProjectDailyTrendsShowSelectionRuleAndDateDetail() throws {
    let source = try mainWindowSource("SessionsProjectsViews.swift")
    try expect(
        source.contains("private struct DailyTokenTrendView")
            && source.contains("RuleMark(x: .value(\"选中日期\"")
            && source.contains(".chartXSelection(value: $selectedDate)"),
        "session and project daily trends must share a selectable vertical rule"
    )
    try expect(
        source.contains("DailyTokenTrendView(points: response.daily)")
            && source.components(separatedBy: "DailyTokenTrendView(points: response.daily)").count == 3,
        "session and project details must both use the shared daily trend"
    )
    try expect(
        source.contains("Text(Self.detailDateFormatter.string(from: selected.date))"),
        "daily trend detail must show the selected date"
    )
    try expect(
        source.components(
            separatedBy: "_selectedDate = State(initialValue: mapped.last?.date)"
        ).count == 3,
        "daily trends must select the latest day before pointer interaction"
    )
}

private func testEveryTokenChartUsesLocalizedAxisAndAccessibilityUnits() throws {
    let chartSources = [
        ("RootView.swift", try mainWindowSource("RootView.swift")),
        ("QuotaHealthViews.swift", try mainWindowSource("QuotaHealthViews.swift")),
        ("SessionsProjectsViews.swift", try mainWindowSource("SessionsProjectsViews.swift")),
    ]
    for (file, source) in chartSources {
        try expect(
            source.contains(".chartYAxis {") && source.contains("TokenQuantityFormatter."),
            "\(file) Token chart must use localized Chinese magnitude units on its Y axis"
        )
        try expect(
            source.contains(".accessibilityValue(")
                && source.contains("TokenQuantityFormatter.string"),
            "\(file) Token chart must expose its unit through accessibility values"
        )
    }
}

private func testEveryTokenSurfaceUsesInputOutputBreakdown() throws {
    let support = try mainWindowSource("FeatureViewSupport.swift")
    try expect(
        support.contains("struct TokenBreakdownView")
            && support.contains("Text(\"输入\")")
            && support.contains("Text(\"缓存")
            && support.contains("Text(\"输出\")")
            && support.contains("Text(\"推理")
            && support.contains("Text(\"总量\")"),
        "the shared Token component must expose input, cached input, output, reasoning, and total"
    )

    for file in [
        "RootView.swift",
        "SessionsProjectsViews.swift",
        "QuotaHealthViews.swift",
        "StatusItemController.swift",
    ] {
        let source = try mainWindowSource(file)
        try expect(
            source.contains("TokenBreakdownView"),
            "\(file) must use the shared input/output Token breakdown"
        )
    }

    let statusItem = try mainWindowSource("StatusItemController.swift")
    try expect(
        statusItem.contains("Text(metricText(project.tokens))"),
        "the narrow status popover ranking must keep its original total-only value"
    )

    let quotaUsage = try mainWindowSource("QuotaHealthViews.swift")
    guard let usageSummaryStart = quotaUsage.range(of: "title: \"Token 用量\""),
          let costSummaryStart = quotaUsage.range(
              of: "title: \"API 折算成本\"",
              range: usageSummaryStart.upperBound..<quotaUsage.endIndex
          )
    else {
        throw TestFailure.mismatch("quota usage summary cards were unavailable")
    }
    let tokenSummary = quotaUsage[usageSummaryStart.lowerBound..<costSummaryStart.lowerBound]
    try expect(
        tokenSummary.contains("numericText(response.totals.totalTokens)")
            && !tokenSummary.contains("TokenBreakdownView"),
        "quota usage summary must show only the total Token value"
    )
}

private func testStatusPopoverShowsLocalizedModelDailyTrend() throws {
    let source = try mainWindowSource("StatusItemController.swift")
    guard let quota = source.range(of: "quotaSection(overview)"),
          let trend = source.range(of: "dailyTrendSection(overview)"),
          let credits = source.range(of: "resetCreditsSection(overview)")
    else {
        throw TestFailure.mismatch("popover sections were unavailable")
    }
    try expect(
        quota.lowerBound < trend.lowerBound && trend.lowerBound < credits.lowerBound,
        "popover daily trend must sit between quota and reset credits"
    )
    try expect(
        source.contains("import Charts")
            && source.contains("BarMark(")
            && source.contains("foregroundStyle(by: .value(\"模型\"")
            && source.contains("overview.weeklyUsageModelTrend")
            && source.contains("overview.weeklyUsageRangeLabel"),
        "popover daily trend must render the reconciled per-model buckets with Swift Charts"
    )
    try expect(
        source.contains("TokenQuantityFormatter.compactString")
            && source.contains(".accessibilityValue(")
            && source.contains(" Token\")"),
        "popover daily trend must localize visible and accessibility Token units"
    )
    try expect(
        source.contains(".accessibilityIdentifier(\"popover.daily-trend\")"),
        "popover daily trend must expose a stable native accessibility surface"
    )
    if let toggleStart = source.range(of: "@objc private func togglePopover"),
       let showStart = source.range(
           of: "private func showPopover",
           range: toggleStart.upperBound..<source.endIndex
       ) {
        let toggleSource = source[toggleStart.lowerBound..<showStart.lowerBound]
        try expect(
            !toggleSource.contains("refreshOrRestart"),
            "opening the status popover must not trigger a redundant Overview refresh"
        )
    } else {
        throw TestFailure.mismatch("popover toggle source was unavailable")
    }
    try expect(
        source.contains("@State private var selectedDailyTrendKey")
            && source.contains(".chartXSelection(value: $selectedDailyTrendKey)")
            && source.contains(".onContinuousHover")
            && source.contains("RuleMark(")
            && source.contains("dailyTrendHoverDetail"),
        "popover daily trend must expose hover selection with a visible per-model detail"
    )
}

private func testPopoverWeeklyTrendDoesNotFollowOverviewRange() throws {
    func trendPoint(_ key: String, tokens: Int64) -> Codexpulse_Core_V1_TrendPoint {
        var point = Codexpulse_Core_V1_TrendPoint()
        point.key = key
        point.totals.totalTokens.value = tokens
        point.totals.totalTokens.unit = "tokens"
        return point
    }

    let base = makeResponses()
    var todayUsage = base.usage
    todayUsage.trend = [trendPoint("2026-07-24T14", tokens: 25)]
    var todayModel = Codexpulse_Core_V1_UsageModelItem()
    todayModel.dimensionKey = "gpt-5.6"
    todayModel.model.displayName = "GPT-5.6"
    todayModel.trend = [trendPoint("2026-07-24T14", tokens: 25)]
    todayUsage.models = [todayModel]

    var weeklyUsage = base.usage
    weeklyUsage.trend = [
        trendPoint("2026-07-23", tokens: 60),
        trendPoint("2026-07-24", tokens: 100),
    ]
    var weeklyModel = Codexpulse_Core_V1_UsageModelItem()
    weeklyModel.dimensionKey = "gpt-5.6"
    weeklyModel.model.displayName = "GPT-5.6"
    weeklyModel.trend = [
        trendPoint("2026-07-23", tokens: 60),
        trendPoint("2026-07-24", tokens: 100),
    ]
    weeklyUsage.models = [weeklyModel]

    let overview = OverviewPresentation(OverviewResponses(
        usage: todayUsage,
        quota: base.quota,
        sessions: base.sessions,
        projects: base.projects,
        health: base.health,
        weeklyUsage: weeklyUsage,
        weeklyProjects: base.weeklyProjects
    ))
    try expect(
        overview.usageModelTrend.map(\.key) == ["2026-07-24T14"],
        "main overview must preserve its selected hourly range"
    )
    try expect(
        overview.weeklyUsageModelTrend.map(\.key) == ["2026-07-23", "2026-07-24"]
            && overview.weeklyUsageModelTrend.allSatisfy(\.breakdownAvailable)
            && overview.weeklyUsageModelTrend[0].segments.map(\.modelName) == ["GPT-5.6"],
        "status popover must preserve the independent weekly daily trend"
    )
}

private func testTrendSelectionSnapsToNearestRealPoint() throws {
    func presentation(_ id: String, startAtMS: Int64) -> TrendPresentation {
        var point = Codexpulse_Core_V1_TrendPoint()
        point.key = id
        point.startAtMs.value = startAtMS
        point.startAtMs.unit = "unix_ms"
        point.totals.totalTokens.value = startAtMS
        point.totals.totalTokens.unit = "tokens"
        return TrendPresentation(point)
    }

    let points = [
        presentation("early", startAtMS: 1_000),
        presentation("middle", startAtMS: 2_000),
        presentation("late", startAtMS: 4_000),
    ]
    try expect(
        TrendSelectionResolver.nearest(to: Date(timeIntervalSince1970: 3.1), in: points)?.id
            == "late",
        "chart selection must choose the nearest real point")
    try expect(
        TrendSelectionResolver.nearest(to: Date(timeIntervalSince1970: 3.0), in: points)?.id
            == "middle",
        "equal-distance chart selection must deterministically prefer the earlier point")
    try expect(
        TrendSelectionResolver.nearest(to: nil, in: points) == nil,
        "clearing chart selection must clear point details")
}

private func testSidebarSettingsUsesSystemRowSpacing() throws {
    let source = try mainWindowSource("RootView.swift")
    try expect(
        source.contains("ForEach([AppFeature.localStatus, .sourcesJobs, .settings])"),
        "settings must share the system section's native sidebar row spacing"
    )
}

private func testSettingsOverviewRangeFallbackMatchesProductOptions() throws {
    let source = try mainWindowSource("SourcesJobsSettingsViews.swift")
    try expect(
        source.contains(
            "fallback: [\"quota_week\", \"today\", \"seven_days\", \"thirty_days\"]"),
        "settings fallback must expose the same overview ranges as the product")
}

private func testSettingsExplainsAutomaticDefaultHome() throws {
    let source = try mainWindowSource("SourcesJobsSettingsViews.swift")
    try expect(
        source.contains(
            "response.snapshot.home.configured ? \"已配置\" : \"默认 Codex Home 不可用\""),
        "settings must explain why a first launch can remain unconfigured")
    try expect(
        source.contains("首次启动会自动使用默认 Codex Home，无需手动确认。"),
        "settings must describe automatic first-launch binding")
}

private func testStatusPillUsesProductCopy() throws {
    let source = try mainWindowSource("FeatureViewSupport.swift")
    try expect(
        source.contains("ProductCopy.status"), "status pills must map raw states to product copy")
    try expect(
        !source.contains("if value.hasID"),
        "attribution copy must not fall back to raw backend identifiers")
    try expect(
        source.contains("return \"其他\"") && !source.contains("return \"暂未归类\""),
        "unclassified project usage must use the approved Other label")
    try expect(ProductCopy.status("not_configured") == "未配置", "setup states must use product copy")
    try expect(
        ProductCopy.settingOption("future_channel") == "其他选项",
        "unknown setting options must not expose raw backend values")
    try expect(ProductCopy.unknownMetric("not_computed") == "未计算", "uncomputed metric copy")
    try expect(ProductCopy.unknownMetric("unavailable") == "暂不可用", "unavailable metric copy")

    let healthSource = try mainWindowSource("QuotaHealthViews.swift")
    try expect(
        healthSource.contains("ProductCopy.impact(response.primary.impact)"),
        "health impact must map raw values to product copy")
    try expect(
        healthSource.contains("ProductCopy.protection(response.primary.protection)"),
        "health protection must map raw values to product copy")
    try expect(
        healthSource.contains("ProductCopy.reason(component.reason)"),
        "health reasons must map raw values to product copy")
}

private func testStatusItemRefreshReadsCommittedState() throws {
    let source = try mainWindowSource("StatusItemController.swift")
    try expect(
        source.contains("model.$state\n            .receive(on: RunLoop.main)\n            .sink"),
        "status item refresh must run after Published state has committed"
    )
    try expect(
        source.contains(
            "displayPreferences.$style\n            .removeDuplicates()\n            .receive(on: RunLoop.main)\n            .sink"),
        "status item style refresh must run after Published preference has committed"
    )
    try expect(
        source.contains("func verifyNativeSurfacesForSmoke(requireSummary: Bool) -> Bool {\n        updateStatusBarView()")
            && source.contains("statusBarView.superview === button")
            && source.contains("statusBarView.preferredWidth > 0")
            && source.contains("if requireSummary && !statusBarView.hasSummary { return false }"),
        "native surface smoke must accept empty Home fallback but require summary when quota data exists"
    )
    let contentSource = try mainWindowSource("StatusBarQuotaContentView.swift")
    try expect(
        contentSource.contains("var hasSummary: Bool { summary != nil }")
            && contentSource.contains("guard let summary else {")
            && contentSource.contains("textWidth(fallbackText, font: fallbackFont)"),
        "status bar view must distinguish summary readiness while keeping a deterministic fallback width"
    )
    let delegateSource = try mainWindowSource("AppDelegate.swift")
    try expect(
        delegateSource.contains("let surfaces = nativeSurfaceSmokeSummary(")
            && delegateSource.contains("requireStatusSummary: !overview.quotaWindows.isEmpty"),
        "native surface smoke must derive summary strictness from authoritative overview quota data"
    )
    try expect(
        contentSource.contains("QuotaRemainingLevel(remainingPercent: summary.remainingPercent)")
            && contentSource.contains("case .healthy: return NSColor.systemGreen")
            && contentSource.contains("case .warning: return NSColor.systemYellow")
            && contentSource.contains("case .critical: return NSColor.systemRed"),
        "status bar progress must use semantic green, yellow, and red remaining-quota colors"
    )
}

private func testInitialWindowUsesScreenAwarePreferredLayout() throws {
    let source = try mainWindowSource("AppDelegate.swift")
    try expect(
        source.contains("MainWindowLayout.initialContentSize(")
            && source.contains("screen.visibleFrame.width")
            && source.contains("screen.visibleFrame.height"),
        "initial window must fit the preferred layout inside the active screen"
    )
    try expect(
        !source.contains("window.setContentSize(NSSize(width: 1_080, height: 720))"),
        "initial window must not retain the clipped 1080x720 fixed size"
    )
}

private func testNativeSmokeForcesOverviewTransitionLast() throws {
    let source = try mainWindowSource("AppDelegate.swift")
    try expect(
        source.contains(
            "let renderOrder = AppFeature.allCases.filter { $0 != .overview } + [.overview]"
        ),
        "native smoke must finish with a real overview route transition"
    )
    try expect(
        source.contains("for feature in renderOrder"),
        "native smoke must render every route in the transition-safe order"
    )
}

private func testPopoverUsesWeeklyProjectTokenRanking() throws {
    let source = try mainWindowSource("StatusItemController.swift")
    try expect(
        source.contains("PopoverSectionTitle(title: \"本周项目 Token 排行\"")
            && source.contains("overview.weeklyProjectRanking")
            && source.contains("Text(metricText(project.tokens))")
            && !source.contains("tokens: project.tokenBreakdown"),
        "popover must render the weekly project Token ranking with total-only values"
    )
    try expect(
        !source.contains("PopoverSectionTitle(title: \"最近会话\"")
            && !source.contains("overview.sessions.prefix(5)"),
        "popover must replace recent sessions instead of adding a second long list"
    )

    let preferences = try mainWindowSource("StatusBarDisplayPreferences.swift")
    try expect(
        preferences.contains("showProjectRanking")
            && !preferences.contains("showRecentSessions")
            && !preferences.contains("showSessionCost"),
        "popover settings must describe the project ranking instead of retired session controls"
    )
}

private func testSettingsIntervalsUseAuthoritativeBounds() throws {
    let source = try mainWindowSource("SourcesJobsSettingsViews.swift")
    try expect(source.contains("hasMinimum"), "settings intervals must read their minimum from editable fields")
    try expect(source.contains("hasMaximum"), "settings intervals must read their maximum from editable fields")
}

private func testOverviewRangeIncludesQuotaWeek() throws {
    guard let quotaWeek = DateRangePreset.allCases.first(where: { $0.rawValue == "quota_week" }) else {
        throw TestFailure.mismatch("overview ranges must include quota_week")
    }
    try expect(quotaWeek.title == "周额度", "quota_week title must be user-facing")
    try expect(
        ProductCopy.settingOption("quota_week") == "周额度",
        "settings must use the same quota-week copy as overview"
    )
}

private func waitUntil(
    _ context: String,
    timeout: Duration = .seconds(2),
    condition: @escaping @Sendable () async -> Bool
) async throws {
    let clock = ContinuousClock()
    let deadline = clock.now.advanced(by: timeout)
    while clock.now < deadline {
        if await condition() { return }
        try await Task.sleep(for: .milliseconds(10))
    }
    throw TestFailure.mismatch("timed out: \(context)")
}

private actor StateRecorder {
    private var phases: [String] = []

    func append(_ state: CoreConnectionState) {
        switch state {
        case .idle: phases.append("idle")
        case .starting: phases.append("starting")
        case .handshaking: phases.append("handshaking")
        case .loadingOverview: phases.append("loading_overview")
        case .normal: phases.append("normal")
        case .partial: phases.append("partial")
        case .recovery: phases.append("recovery")
        case .restartRequired: phases.append("restart_required")
        case .stale: phases.append("stale")
        case .unavailable: phases.append("unavailable")
        case .cancelled: phases.append("cancelled")
        case .shuttingDown: phases.append("shutting_down")
        case .stopped: phases.append("stopped")
        }
    }

    func snapshot() -> [String] { phases }
}

private actor FakeSupervisor: HelperSupervising {
    private var starts = 0
    private var stops = 0
    private let startFailure: Bool
    private let startDelay: Duration

    init(startFailure: Bool = false, startDelay: Duration = .zero) {
        self.startFailure = startFailure
        self.startDelay = startDelay
    }

    func start() async throws -> RunningHelper {
        starts += 1
        if startDelay != .zero { try await Task.sleep(for: startDelay) }
        if startFailure { throw FakeFailure.unavailable }
        return RunningHelper(
            processID: 42,
            socketPath: "/private/tmp/cp-test/core.sock",
            databasePath: "/private/tmp/cp-test/data/test.db",
            preferencesPath: "/private/tmp/cp-test/preferences.json",
            bearerToken: "test-only"
        )
    }

    func waitForExit(timeout: Duration) async throws -> Int32 { 0 }

    func stop(mode: HelperStopMode) async { stops += 1 }

    func counts() -> (Int, Int) { (starts, stops) }
}

private actor FakeCore: AppCoreServing {
    private var bootstrapResponse: Codexpulse_Core_V1_BootstrapResponse
    private var recoveryReceipt: Codexpulse_Core_V1_MigrationRecoveryReceipt
    private var responses: OverviewResponses
    private var failOverview = false
    private var failOverviewProjects = false
    private var handshakeFailure = false
    private var handshakeError: CoreClientError?
    private var overviewDelay: Duration = .zero
    private var handshakeDelay: Duration = .zero
    private var bootstrapDelay: Duration = .zero
    private var shutdownDelay: Duration = .zero
    private var calls: [String] = []
    private var usageRequests: [Codexpulse_Core_V1_UsageCostRequest] = []
    private var sessionRequests: [Codexpulse_Core_V1_ListSessionsRequest] = []
    private var projectRequests: [Codexpulse_Core_V1_ListProjectsRequest] = []
    private var featureSessionPlans: [SessionPagePlan] = []
    private var invalidationDomain: String?
    private var invalidationDelay: Duration = .zero
    private var quotaRefreshDelay: Duration = .zero
    private var settingsResponses: [Codexpulse_Core_V1_SettingsResponse] = []
    private var settingsUpdateFailure = false
    private var settingsReadDelay: Duration = .zero
    private var settingsUpdateDelay: Duration = .zero

    init(
        bootstrap: Codexpulse_Core_V1_BootstrapResponse,
        recoveryReceipt: Codexpulse_Core_V1_MigrationRecoveryReceipt = .init(),
        responses: OverviewResponses
    ) {
        self.bootstrapResponse = bootstrap
        self.recoveryReceipt = recoveryReceipt
        self.responses = responses
    }

    func setOverviewFailure(_ value: Bool) { failOverview = value }
    func setOverviewProjectFailure(_ value: Bool) { failOverviewProjects = value }
    func setHandshakeFailure(_ value: Bool) { handshakeFailure = value }
    func setHandshakeError(_ value: CoreClientError?) { handshakeError = value }
    func setOverviewDelay(_ value: Duration) { overviewDelay = value }
    func setHandshakeDelay(_ value: Duration) { handshakeDelay = value }
    func setBootstrapDelay(_ value: Duration) { bootstrapDelay = value }
    func setShutdownDelay(_ value: Duration) { shutdownDelay = value }
    func setFeatureSessionPlans(_ plans: [SessionPagePlan]) { featureSessionPlans = plans }
    func setInvalidation(domain: String?, delay: Duration = .zero) {
        invalidationDomain = domain
        invalidationDelay = delay
    }
    func setQuotaRefreshDelay(_ value: Duration) { quotaRefreshDelay = value }
    func setSettingsReadDelay(_ value: Duration) { settingsReadDelay = value }
    func setSettingsUpdateDelay(_ value: Duration) { settingsUpdateDelay = value }
    func setSettingsResponses(_ values: [Codexpulse_Core_V1_SettingsResponse], updateFailure: Bool) {
        settingsResponses = values
        settingsUpdateFailure = updateFailure
    }
    func recordedCalls() -> [String] { calls }
    func recordedUsageRequests() -> [Codexpulse_Core_V1_UsageCostRequest] { usageRequests }
    func recordedSessionRequests() -> [Codexpulse_Core_V1_ListSessionsRequest] { sessionRequests }
    func recordedProjectRequests() -> [Codexpulse_Core_V1_ListProjectsRequest] { projectRequests }

    func handshake(
        clientName: String,
        clientVersion: String,
        retryPolicy: ReadRetryPolicy
    ) async throws -> Codexpulse_Core_V1_HandshakeResponse {
        calls.append("handshake")
        if handshakeDelay != .zero { try await Task.sleep(for: handshakeDelay) }
        if let handshakeError { throw handshakeError }
        if handshakeFailure { throw FakeFailure.unavailable }
        var response = Codexpulse_Core_V1_HandshakeResponse()
        response.contractVersion = CodexPulseTransportContract.version
        response.transport = CodexPulseTransportContract.transport
        return response
    }

    func bootstrap(
        retryPolicy: ReadRetryPolicy
    ) async throws -> Codexpulse_Core_V1_BootstrapResponse {
        calls.append("bootstrap")
        if bootstrapDelay != .zero { try await Task.sleep(for: bootstrapDelay) }
        return bootstrapResponse
    }

    func usageCost(
        _ request: Codexpulse_Core_V1_UsageCostRequest,
        retryPolicy: ReadRetryPolicy
    ) async throws -> Codexpulse_Core_V1_UsageCostResponse {
        calls.append("usage")
        usageRequests.append(request)
        if overviewDelay != .zero { try await Task.sleep(for: overviewDelay) }
        if failOverview { throw FakeFailure.unavailable }
        return responses.usage
    }

    func quotaCurrent(
        _ request: Codexpulse_Core_V1_QuotaCurrentRequest,
        retryPolicy: ReadRetryPolicy
    ) async throws -> Codexpulse_Core_V1_QuotaCurrentResponse {
        calls.append("quota")
        if overviewDelay != .zero { try await Task.sleep(for: overviewDelay) }
        if failOverview { throw FakeFailure.unavailable }
        return responses.quota
    }

    func listSessions(
        _ request: Codexpulse_Core_V1_ListSessionsRequest,
        retryPolicy: ReadRetryPolicy
    ) async throws -> Codexpulse_Core_V1_SessionListResponse {
        calls.append("sessions")
        sessionRequests.append(request)
        if request.query.page.limit != 5, !featureSessionPlans.isEmpty {
            let plan = featureSessionPlans.removeFirst()
            if plan.delay != .zero { try await Task.sleep(for: plan.delay) }
            if plan.fails { throw FakeFailure.unavailable }
            return plan.response
        }
        if overviewDelay != .zero { try await Task.sleep(for: overviewDelay) }
        if failOverview { throw FakeFailure.unavailable }
        return responses.sessions
    }

    func listProjects(
        _ request: Codexpulse_Core_V1_ListProjectsRequest,
        retryPolicy: ReadRetryPolicy
    ) async throws -> Codexpulse_Core_V1_ProjectListResponse {
        calls.append("projects")
        projectRequests.append(request)
        if overviewDelay != .zero { try await Task.sleep(for: overviewDelay) }
        if request.query.page.limit == 5, failOverviewProjects { throw FakeFailure.unavailable }
        if failOverview { throw FakeFailure.unavailable }
        if request.query.filters.contains(where: { $0.field == "confidence" }) {
            return responses.weeklyProjects
        }
        var response = Codexpulse_Core_V1_ProjectListResponse()
        response.meta = completeMeta()
        return response
    }

    func runRuntimeAction(
        _ request: Codexpulse_Core_V1_RuntimeActionRequest
    ) async throws -> Codexpulse_Core_V1_RuntimeActionReceipt {
        calls.append("runtime_action:\(request.action)")
        var receipt = Codexpulse_Core_V1_RuntimeActionReceipt()
        receipt.action = request.action
        receipt.pauseScope = request.action == "pause_all" ? "all" : "none"
        receipt.sourceState = "ready"
        receipt.transition = "applied"
        return receipt
    }

    func requestQuotaRefresh(
        _ request: Codexpulse_Core_V1_QuotaRefreshRequest
    ) async throws -> Codexpulse_Core_V1_QuotaRefreshReceipt {
        calls.append("quota_refresh:\(request.source)")
        if quotaRefreshDelay != .zero { try await Task.sleep(for: quotaRefreshDelay) }
        var receipt = Codexpulse_Core_V1_QuotaRefreshReceipt()
        receipt.source = request.source
        receipt.reason = "accepted"
        return receipt
    }

    func settings(
        retryPolicy: ReadRetryPolicy
    ) async throws -> Codexpulse_Core_V1_SettingsResponse {
        calls.append("settings")
        if settingsReadDelay != .zero { try await Task.sleep(for: settingsReadDelay) }
        guard !settingsResponses.isEmpty else { throw FakeFailure.unavailable }
        if settingsResponses.count == 1 { return settingsResponses[0] }
        return settingsResponses.removeFirst()
    }

    func updateSettings(
        _ request: Codexpulse_Core_V1_UpdateSettingsRequest
    ) async throws -> Codexpulse_Core_V1_SettingsUpdateReceipt {
        calls.append("settings_update:\(request.expectedRevision)")
        if settingsUpdateDelay != .zero { try await Task.sleep(for: settingsUpdateDelay) }
        if settingsUpdateFailure { throw FakeFailure.unavailable }
        var receipt = Codexpulse_Core_V1_SettingsUpdateReceipt()
        receipt.result = "applied"
        receipt.revision = settingsResponses.last?.snapshot.revision ?? request.expectedRevision
        return receipt
    }

    func healthProjection(
        retryPolicy: ReadRetryPolicy
    ) async throws -> Codexpulse_Core_V1_HealthProjectionResponse {
        calls.append("health")
        if overviewDelay != .zero { try await Task.sleep(for: overviewDelay) }
        if failOverview { throw FakeFailure.unavailable }
        return responses.health
    }

    func migrationRecoveryRetry() async throws -> Codexpulse_Core_V1_MigrationRecoveryReceipt {
        calls.append("recovery_retry")
        return recoveryReceipt
    }

    func notifyLifecycle(
        _ event: LifecycleEvent
    ) async throws -> Codexpulse_Core_V1_LifecycleNotificationReceipt {
        calls.append("lifecycle:\(event.rawValue)")
        var response = Codexpulse_Core_V1_LifecycleNotificationReceipt()
        response.event = event.rawValue
        response.accepted = true
        return response
    }

    func consumeInvalidations(
        domains: [String],
        afterSequence: UInt64,
        onReady: @Sendable @escaping () async -> Void,
        onEvent: @Sendable @escaping (Codexpulse_Core_V1_QueryInvalidationEvent) async throws -> Void
    ) async throws {
        calls.append("stream:\(domains.joined(separator: ","))")
        await onReady()
        if let invalidationDomain {
            if invalidationDelay != .zero { try await Task.sleep(for: invalidationDelay) }
            var event = Codexpulse_Core_V1_QueryInvalidationEvent()
            event.version = CodexPulseTransportContract.invalidationVersion
            event.domain = invalidationDomain
            event.sequence = 1
            try await onEvent(event)
        }
        try await Task.sleep(for: .seconds(60))
    }

    func shutdown(reason: String) async throws {
        calls.append("shutdown:\(reason)")
        if shutdownDelay != .zero { try await Task.sleep(for: shutdownDelay) }
    }
    func closeTransport() async { calls.append("close_transport") }
}

private func completeMeta() -> Codexpulse_Core_V1_ResponseMeta {
    var meta = Codexpulse_Core_V1_ResponseMeta()
    meta.version = "test-v1"
    meta.status = "complete"
    return meta
}

private func makeNormalBootstrap() -> Codexpulse_Core_V1_BootstrapResponse {
    var response = Codexpulse_Core_V1_BootstrapResponse()
    response.mode = "normal"
    return response
}

private func makeResponses(
    partial: Bool = false,
    includeWeeklyQuota: Bool = true
) -> OverviewResponses {
    var usage = Codexpulse_Core_V1_UsageCostResponse()
    usage.meta = completeMeta()
    usage.totals.totalTokens.value = 0
    usage.totals.totalTokens.unit = "tokens"
    usage.totals.estimatedUsdMicros.unknownReason = "pricing_missing"
    usage.totals.estimatedUsdMicros.unit = "usd_micros"
    usage.range.startAtMs = 1_753_059_600_000 - 10_080 * 60_000
    usage.range.endAtMs = 1_753_056_000_000
    usage.range.timeZone = "Asia/Shanghai"
    var point = Codexpulse_Core_V1_TrendPoint()
    point.key = "2026-07-21"
    point.startAtMs.value = 1_753_046_400_000
    point.startAtMs.unit = "unix_ms"
    point.totals.totalTokens.value = 0
    point.totals.totalTokens.unit = "tokens"
    usage.trend = [point]

    var primary = Codexpulse_Core_V1_CurrentWindow()
    primary.windowKind = "primary"
    primary.limitID = "codex"
    primary.windowMinutes = 10_080
    primary.remainingPercent = 0
    primary.resetsAtMs = 1_753_059_600_000
    primary.resetRemainingMs = 3_600_000
    primary.freshness = "fresh"
    var secondary = Codexpulse_Core_V1_CurrentWindow()
    secondary.windowKind = "secondary"
    secondary.limitID = "codex"
    secondary.freshness = "unknown"
    secondary.unknownReason = "not_observed"
    var quota = Codexpulse_Core_V1_QuotaCurrentResponse()
    quota.meta = completeMeta()
    quota.current.windows = includeWeeklyQuota ? [primary, secondary] : []
    quota.current.evaluatedAtMs = 1_753_056_000_000
    quota.current.resetCredits.availableCount = 1
    quota.current.resetCredits.totalCount = 2
    quota.current.resetCredits.redeemedCount = 1
    quota.current.resetCredits.cumulativeRemainingMs = 3_600_000
    quota.current.resetCredits.nextExpiresAtMs = 1_753_059_600_000
    quota.current.resetCredits.freshness = "fresh"
    var availableCredit = Codexpulse_Core_V1_CurrentResetCreditItem()
    availableCredit.status = "available"
    availableCredit.type = "codex_rate_limits"
    availableCredit.grantedAtMs = 1_753_050_000_000
    availableCredit.expiresAtMs = 1_753_059_600_000
    availableCredit.remainingMs = 3_600_000
    quota.current.resetCredits.items = [availableCredit]

    var sessions = Codexpulse_Core_V1_SessionListResponse()
    sessions.meta = completeMeta()
    var session = Codexpulse_Core_V1_SessionItem()
    session.sessionID = "session-test"
    session.displayTitle = "真实生成类型会话"
    session.activity = "completed"
    session.totals.totalTokens.value = 0
    session.totals.totalTokens.unit = "tokens"
    session.totals.estimatedUsdMicros.value = 1_250_000
    session.totals.estimatedUsdMicros.unit = "usd_micros"
    session.project.displayName = "Codex Pulse"
    sessions.items = [session]
    if partial {
        sessions.meta.status = "partial"
        var issue = Codexpulse_Core_V1_Issue()
        issue.code = "index_incomplete"
        issue.messageKey = "core.issue.index_incomplete"
        issue.retryable = true
        sessions.meta.issues = [issue]
    }

    var projects = Codexpulse_Core_V1_ProjectListResponse()
    projects.meta = completeMeta()
    var project = Codexpulse_Core_V1_ProjectItem()
    project.project.id = "project-test"
    project.project.displayName = "Codex Pulse"
    project.dimensionKey = "project-test"
    project.totals.totalTokens.value = 60
    project.totals.totalTokens.unit = "tokens"
    projects.items = [project]
    projects.matchedTotals.totalTokens.value = 100
    projects.matchedTotals.totalTokens.unit = "tokens"

    var health = Codexpulse_Core_V1_HealthProjectionResponse()
    health.hasValue_p = true
    health.level = "healthy"

    return OverviewResponses(
        usage: usage, quota: quota, sessions: sessions, projects: projects, health: health)
}

private func makeSessionPage(
    id: String,
    title: String,
    nextCursor: String? = nil
) -> Codexpulse_Core_V1_SessionListResponse {
    var response = Codexpulse_Core_V1_SessionListResponse()
    response.meta = completeMeta()
    var item = Codexpulse_Core_V1_SessionItem()
    item.sessionID = id
    item.displayTitle = title
    item.totals.totalTokens.value = 0
    response.items = [item]
    response.meta.page.limit = 50
    if let nextCursor {
        response.meta.page.hasMore_p = true
        response.meta.page.nextCursor = nextCursor
    }
    return response
}

private func makeSettingsResponse(revision: String, quotaEnabled: Bool)
    -> Codexpulse_Core_V1_SettingsResponse
{
    var response = Codexpulse_Core_V1_SettingsResponse()
    response.meta = completeMeta()
    response.snapshot.revision = revision
    response.snapshot.online.quotaEnabled = quotaEnabled
    response.snapshot.online.resetCreditsEnabled = true
    response.snapshot.refresh.quotaIntervalSeconds = 300
    response.snapshot.refresh.resetCreditsIntervalSeconds = 600
    response.snapshot.refresh.reconcileIntervalSeconds = 900
    response.snapshot.refresh.jsonlDebounceMilliseconds = 250
    response.snapshot.updates.autoCheckEnabled = true
    response.snapshot.updates.checkIntervalSeconds = 3_600
    response.snapshot.ui.launchBehavior = "main_window"
    response.snapshot.ui.overviewRange = "7d"
    var editable = Codexpulse_Core_V1_EditableField()
    editable.key = "online.quotaEnabled"
    editable.editable = true
    response.editableFields = [editable]
    return response
}

private func testFeatureRequestsStateAndMerge() throws {
    var calendar = Calendar(identifier: .buddhist)
    calendar.timeZone = TimeZone(secondsFromGMT: 0)!
    let now = Date(timeIntervalSince1970: 1_753_056_000)
    var options = SessionQueryOptions()
    options.range = .sevenDays
    options.activity = "active"
    options.projectID = "project-1"
    options.modelKey = "model-1"
    options.sortField = "totalTokens"
    options.sortDirection = "asc"
    let request = FeatureRequestFactory.sessions(
        options: options,
        cursor: "opaque-cursor",
        limit: 500,
        now: now,
        calendar: calendar
    )
    try expect(
        request.query.page.limit == 100, "feature page limit must be bounded by provider maximum")
    try expect(
        request.query.page.cursor == "opaque-cursor", "opaque cursor must pass through unchanged")
    try expect(
        request.query.sort.first?.field == "totalTokens", "provider sort field must remain explicit")
    try expect(
        request.query.sort.first?.direction == "asc", "provider sort direction must remain explicit")
    try expect(
        request.query.filters.count == 3, "session filters must be composed, not shadow-searched")
    try expect(
        request.query.filters.allSatisfy { $0.operator == "eq" }, "single-value filters must use eq")
    try expect(
        request.query.timeRange.startDate == "2025-07-15", "feature dates must remain Gregorian")

    var exactRange = Codexpulse_Core_V1_UTCTimeRange()
    exactRange.startAtMs = 3_600_000
    exactRange.endAtMs = 7_200_000
    exactRange.timeZone = "UTC"
    var exactSessionOptions = SessionQueryOptions()
    exactSessionOptions.range = .quotaWeek
    exactSessionOptions.exactRange = exactRange
    let exactSessions = FeatureRequestFactory.sessions(options: exactSessionOptions)
    try expect(
        exactSessions.query.hasExactTimeRange && !exactSessions.query.hasTimeRange,
        "overview session navigation must retain the exact quota range")
    var exactProjectOptions = ProjectQueryOptions()
    exactProjectOptions.range = .quotaWeek
    exactProjectOptions.exactRange = exactRange
    let exactProjects = FeatureRequestFactory.projects(options: exactProjectOptions)
    try expect(
        exactProjects.query.hasExactTimeRange && !exactProjects.query.hasTimeRange,
        "overview project navigation must retain the exact quota range")
    let exactProjectDetail = FeatureRequestFactory.projectDetail(
        dimensionKey: "project-a", range: .quotaWeek, exactRange: exactRange)
    try expect(
        exactProjectDetail.hasExactRange && !exactProjectDetail.hasRange,
        "project detail must retain the exact quota range")
    try expect(
        RuntimeControlAction.allCases.map(\.rawValue) == [
            "pause_backfill", "pause_all", "resume", "reconcile",
        ],
        "runtime controls must expose only the RunRuntimeAction allowlist"
    )
    try expect(
        RuntimeControlAction(commandKey: "repair") == nil,
        "high-risk repair must not become a runtime action")

    let first = makeSessionPage(id: "session-a", title: "first", nextCursor: "cursor-2")
    var second = makeSessionPage(id: "session-b", title: "second")
    second.items.insert(first.items[0], at: 0)
    let merged = FeatureResponseMerge.sessions(first, second, append: true)
    try expect(
        merged.items.map(\.sessionID) == ["session-a", "session-b"],
        "pagination merge must be stable and deduplicated")
    try expect(pageHasMore(first.meta), "has_more requires an opaque next cursor")
    try expect(!pageHasMore(second.meta), "missing cursor must stop pagination")

    var previousProject = Codexpulse_Core_V1_ProjectDetailResponse()
    var previousSession = Codexpulse_Core_V1_ProjectSessionItem()
    previousSession.sessionID = "session-complete"
    previousProject.sessions = [previousSession]
    previousProject.sessionPage.hasMore_p = false
    var previousModel = Codexpulse_Core_V1_ProjectModelItem()
    previousModel.dimensionKey = "model-a"
    previousProject.models = [previousModel]
    previousProject.modelPage.hasMore_p = true
    previousProject.modelPage.nextCursor = "model-cursor"
    var nextProject = Codexpulse_Core_V1_ProjectDetailResponse()
    var repeatedFirstPageSession = Codexpulse_Core_V1_ProjectSessionItem()
    repeatedFirstPageSession.sessionID = "session-first-page-again"
    nextProject.sessions = [repeatedFirstPageSession]
    var nextModel = Codexpulse_Core_V1_ProjectModelItem()
    nextModel.dimensionKey = "model-b"
    nextProject.models = [nextModel]
    let mergedProject = FeatureResponseMerge.projectDetail(
        previousProject,
        nextProject,
        append: true,
        appendSessions: false,
        appendModels: true
    )
    try expect(
        mergedProject.sessions.map(\.sessionID) == ["session-complete"],
        "completed Project session page must not reset while models continue"
    )
    try expect(
        mergedProject.models.map(\.dimensionKey) == ["model-a", "model-b"],
        "independent Project model page must append"
    )

    var partialMeta = completeMeta()
    partialMeta.status = "partial"
    var issue = Codexpulse_Core_V1_Issue()
    issue.code = "bounded_partial"
    issue.retryable = true
    partialMeta.issues = [issue]
    if case .partial(_, let notices) = loadState(value: first, meta: partialMeta, isEmpty: false) {
        try expect(
            notices.first?.code == "bounded_partial", "partial issue semantics must remain recursive")
    } else {
        throw TestFailure.mismatch("partial response was normalized to ready")
    }
    var unavailableMeta = completeMeta()
    unavailableMeta.status = "unavailable"
    if case .unavailable = loadState(value: first, meta: unavailableMeta, isEmpty: false) {
        // Provider explicitly declined to provide a trustworthy value.
    } else {
        throw TestFailure.mismatch("unavailable response was presented as partial value")
    }
    var unknownMeta = completeMeta()
    unknownMeta.status = "future_status"
    if case .unavailable(let notice) = loadState(value: first, meta: unknownMeta, isEmpty: false) {
        try expect(!notice.retryable, "unknown response status must fail closed")
    } else {
        throw TestFailure.mismatch("unknown response status did not fail closed")
    }
    if case .empty = loadState(value: second, meta: completeMeta(), isEmpty: true) {
        // Expected zero state.
    } else {
        throw TestFailure.mismatch("complete zero result must become empty")
    }
    if case .stale(let previous, _) = failedLoadState(previous: first, error: FakeFailure.unavailable) {
        try expect(
            previous.items.first?.sessionID == "session-a",
            "failed refresh must retain the previous page as stale")
    } else {
        throw TestFailure.mismatch("failed refresh discarded its previous value")
    }
    if case .cancelled(let previous) = failedLoadState(previous: first, error: CancellationError()) {
        try expect(
            previous?.items.first?.sessionID == "session-a",
            "cancelled refresh must retain its bounded previous value")
    } else {
        throw TestFailure.mismatch("cancellation was presented as unavailable")
    }
}

@MainActor
private func testInvalidationRefreshesActivePage() async throws {
    let core = FakeCore(bootstrap: makeNormalBootstrap(), responses: makeResponses())
    await core.setFeatureSessionPlans([
        SessionPagePlan(delay: .zero, response: makeSessionPage(id: "before", title: "before")),
        SessionPagePlan(delay: .zero, response: makeSessionPage(id: "after", title: "after")),
    ])
    await core.setInvalidation(domain: "index", delay: .milliseconds(150))
    let runtime = AppRuntime(supervisor: FakeSupervisor(), clientFactory: { _ in core })
    let model = AppModel(runtime: runtime)
    model.start()
    try await waitUntil("overview before invalidation") {
        await MainActor.run { model.presentation != nil }
    }
    model.navigate(to: .sessions)
    try await waitUntil("initial sessions page") {
        await MainActor.run { model.sessionsState.value?.items.first?.sessionID == "before" }
    }
    try await waitUntil("index invalidation reloads active sessions") {
        await MainActor.run { model.sessionsState.value?.items.first?.sessionID == "after" }
    }
    if case .idle = model.sessionDetailState {
        // No detail was selected, so an index invalidation must not invent a detail error.
    } else {
        throw TestFailure.mismatch("unselected session detail became unavailable after invalidation")
    }
    let calls = await core.recordedCalls()
    try expect(
        calls.contains(where: { $0 == "stream:index,quota,health,settings" }),
        "invalidation stream must subscribe to settings as well as data domains"
    )
    _ = await model.shutdown()
}

private func testIndexInvalidationRefreshesStatusWhileApplicationIsInactive() async throws {
    let core = FakeCore(bootstrap: makeNormalBootstrap(), responses: makeResponses())
    await core.setInvalidation(domain: "index", delay: .milliseconds(250))
    let runtime = AppRuntime(supervisor: FakeSupervisor(), clientFactory: { _ in core })

    await runtime.start()
    try await waitUntil("initial status overview") {
        await core.recordedCalls().filter { $0 == "usage" }.count == 1
    }
    await runtime.applicationWillResignActive()

    try await waitUntil("inactive index invalidation refreshes status overview") {
        await core.recordedCalls().filter { $0 == "usage" }.count >= 2
    }
    _ = await runtime.shutdown()
}

@MainActor
private func testRepeatedCursorStopsPagination() async throws {
    let core = FakeCore(bootstrap: makeNormalBootstrap(), responses: makeResponses())
    await core.setFeatureSessionPlans([
        SessionPagePlan(
            delay: .zero, response: makeSessionPage(id: "page-1", title: "page 1", nextCursor: "repeat")),
        SessionPagePlan(
            delay: .zero, response: makeSessionPage(id: "page-2", title: "page 2", nextCursor: "repeat")),
    ])
    let model = AppModel(
        runtime: AppRuntime(supervisor: FakeSupervisor(), clientFactory: { _ in core }))
    model.start()
    try await waitUntil("pagination overview") { await MainActor.run { model.presentation != nil } }
    model.loadSessions(reset: true)
    try await waitUntil("pagination first page") {
        await MainActor.run { model.sessionsState.value?.items.first?.sessionID == "page-1" }
    }
    model.loadSessions(reset: false)
    try await waitUntil("pagination second page") {
        await MainActor.run { model.sessionsState.value?.items.count == 2 }
    }
    model.loadSessions(reset: false)
    try expect(
        model.sessionsState.value?.meta.page.hasMore_p == false,
        "repeated cursor must terminate pagination")
    if case .partial(_, let notices) = model.sessionsState {
        try expect(
            notices.first?.code == "pagination_cursor_repeated", "cursor loop must remain visible")
    } else {
        throw TestFailure.mismatch("cursor loop did not produce a bounded partial state")
    }
    _ = await model.shutdown()
}

@MainActor
private func testTransientCursorFailureCanRetry() async throws {
    let core = FakeCore(bootstrap: makeNormalBootstrap(), responses: makeResponses())
    await core.setFeatureSessionPlans([
        SessionPagePlan(
            delay: .zero, response: makeSessionPage(id: "page-1", title: "page 1", nextCursor: "retry")),
        SessionPagePlan(
            delay: .zero, response: makeSessionPage(id: "failed", title: "failed"), fails: true),
        SessionPagePlan(delay: .zero, response: makeSessionPage(id: "page-2", title: "page 2")),
    ])
    let model = AppModel(
        runtime: AppRuntime(supervisor: FakeSupervisor(), clientFactory: { _ in core }))
    model.start()
    try await waitUntil("cursor retry overview") { await MainActor.run { model.presentation != nil } }
    model.loadSessions(reset: true)
    try await waitUntil("cursor retry first page") {
        await MainActor.run { model.sessionsState.value?.items.first?.sessionID == "page-1" }
    }
    model.loadSessions(reset: false)
    try await waitUntil("cursor retry transient failure") {
        await MainActor.run {
            if case .stale = model.sessionsState { return true }
            return false
        }
    }
    model.loadSessions(reset: false)
    try await waitUntil("cursor retry succeeds") {
        await MainActor.run {
            model.sessionsState.value?.items.map(\.sessionID) == ["page-1", "page-2"]
        }
    }
    _ = await model.shutdown()
}

@MainActor
private func testQuotaMutationIsSingleFlight() async throws {
    let core = FakeCore(bootstrap: makeNormalBootstrap(), responses: makeResponses())
    await core.setQuotaRefreshDelay(.milliseconds(100))
    let model = AppModel(
        runtime: AppRuntime(supervisor: FakeSupervisor(), clientFactory: { _ in core }))
    model.start()
    try await waitUntil("quota singleflight overview") {
        await MainActor.run { model.presentation != nil }
    }
    model.requestQuotaRefresh(source: "quota")
    model.requestQuotaRefresh(source: "quota")
    try await waitUntil("quota singleflight receipt") {
        await MainActor.run {
            if case .succeeded = model.quotaRefreshState { return true }
            return false
        }
    }
    let calls = await core.recordedCalls().filter { $0 == "quota_refresh:quota" }
    try expect(calls.count == 1, "quota mutation must not be cancelled and replayed")
    _ = await model.shutdown()
}

@MainActor
private func testLifecycleInvalidationPreservesMutation() async throws {
    let core = FakeCore(bootstrap: makeNormalBootstrap(), responses: makeResponses())
    await core.setQuotaRefreshDelay(.milliseconds(300))
    await core.setInvalidation(domain: "lifecycle", delay: .milliseconds(120))
    let model = AppModel(
        runtime: AppRuntime(supervisor: FakeSupervisor(), clientFactory: { _ in core }))
    model.start()
    try await waitUntil("lifecycle mutation overview") {
        await MainActor.run { model.presentation != nil }
    }
    model.requestQuotaRefresh(source: "quota")
    try await waitUntil("lifecycle mutation receipt") {
        await MainActor.run {
            if case .succeeded = model.quotaRefreshState { return true }
            return false
        }
    }
    let calls = await core.recordedCalls().filter { $0 == "quota_refresh:quota" }
    try expect(calls.count == 1, "active/wake invalidation must not cancel an in-flight mutation")
    _ = await model.shutdown()
}

@MainActor
private func testSettingsConflictPreservesDraft() async throws {
    let core = FakeCore(bootstrap: makeNormalBootstrap(), responses: makeResponses())
    await core.setSettingsResponses(
        [
            makeSettingsResponse(revision: "revision-1", quotaEnabled: false),
            makeSettingsResponse(revision: "revision-2", quotaEnabled: false),
        ], updateFailure: true)
    let model = AppModel(
        runtime: AppRuntime(supervisor: FakeSupervisor(), clientFactory: { _ in core }))
    model.start()
    try await waitUntil("settings conflict overview") {
        await MainActor.run { model.presentation != nil }
    }
    model.loadSettings()
    try await waitUntil("settings authoritative load") {
        await MainActor.run { model.settingsState.value?.snapshot.revision == "revision-1" }
    }
    guard var draft = model.settingsDraft else {
        throw TestFailure.mismatch("settings draft missing")
    }
    draft.quotaEnabled = true
    model.settingsDraft = draft
    model.saveSettings()
    model.saveSettings()
    try await waitUntil("settings revision conflict") {
        await MainActor.run {
            if case .conflict = model.settingsSaveState { return true }
            return false
        }
    }
    try expect(
        model.settingsState.value?.snapshot.revision == "revision-2",
        "conflict must retain authoritative readback")
    try expect(
        model.settingsDraft?.quotaEnabled == true, "conflict must preserve the user's pending draft")
    let updates = await core.recordedCalls().filter { $0 == "settings_update:revision-1" }
    try expect(updates.count == 1, "Settings mutation must remain single-flight")
    _ = await model.shutdown()
}

@MainActor
private func testSettingsEditDuringSaveIsPreserved() async throws {
    let core = FakeCore(bootstrap: makeNormalBootstrap(), responses: makeResponses())
    await core.setSettingsResponses(
        [
            makeSettingsResponse(revision: "revision-1", quotaEnabled: false),
            makeSettingsResponse(revision: "revision-2", quotaEnabled: true),
        ], updateFailure: false)
    await core.setSettingsUpdateDelay(.milliseconds(120))
    let model = AppModel(
        runtime: AppRuntime(supervisor: FakeSupervisor(), clientFactory: { _ in core }))
    model.start()
    try await waitUntil("settings edit overview") {
        await MainActor.run { model.presentation != nil }
    }
    model.loadSettings()
    try await waitUntil("settings edit authoritative load") {
        await MainActor.run { model.settingsState.value?.snapshot.revision == "revision-1" }
    }
    guard var submitted = model.settingsDraft else {
        throw TestFailure.mismatch("settings draft missing")
    }
    submitted.quotaEnabled = true
    model.settingsDraft = submitted
    model.saveSettings()
    try await waitUntil("settings update starts") {
        await core.recordedCalls().contains("settings_update:revision-1")
    }
    var edited = submitted
    edited.quotaEnabled = false
    model.settingsDraft = edited
    try await waitUntil("settings save readback") {
        await MainActor.run { model.settingsState.value?.snapshot.revision == "revision-2" }
    }
    try expect(
        model.settingsDraft?.quotaEnabled == false,
        "an edit made during save must survive receipt/readback")
    if case .idle = model.settingsSaveState {
        // The preserved edit remains pending against the authoritative readback.
    } else {
        throw TestFailure.mismatch("a pending post-submit edit must return Settings to idle")
    }
    _ = await model.shutdown()
}

@MainActor
private func testSettingsEditDuringRefreshIsPreserved() async throws {
    let core = FakeCore(bootstrap: makeNormalBootstrap(), responses: makeResponses())
    await core.setSettingsResponses(
        [
            makeSettingsResponse(revision: "revision-1", quotaEnabled: false),
            makeSettingsResponse(revision: "revision-1", quotaEnabled: false),
        ], updateFailure: false)
    let model = AppModel(
        runtime: AppRuntime(supervisor: FakeSupervisor(), clientFactory: { _ in core }))
    model.start()
    try await waitUntil("settings refresh overview") {
        await MainActor.run { model.presentation != nil }
    }
    model.loadSettings()
    try await waitUntil("settings refresh initial load") {
        await MainActor.run { model.settingsState.value?.snapshot.revision == "revision-1" }
    }
    await core.setSettingsReadDelay(.milliseconds(120))
    model.loadSettings()
    try await Task.sleep(for: .milliseconds(20))
    guard var edited = model.settingsDraft else {
        throw TestFailure.mismatch("settings draft missing")
    }
    edited.quotaEnabled = true
    model.settingsDraft = edited
    try await waitUntil("settings refresh completes") {
        await MainActor.run { !model.settingsState.isLoading }
    }
    try expect(
        model.settingsDraft?.quotaEnabled == true, "an edit made during refresh must not be overwritten"
    )
    _ = await model.shutdown()
}

private func testSettingsRevisionRequest() throws {
    var response = Codexpulse_Core_V1_SettingsResponse()
    response.meta = completeMeta()
    response.snapshot.revision = "revision-1"
    response.snapshot.online.quotaEnabled = false
    response.snapshot.online.resetCreditsEnabled = true
    response.snapshot.refresh.quotaIntervalSeconds = 300
    response.snapshot.refresh.resetCreditsIntervalSeconds = 600
    response.snapshot.refresh.reconcileIntervalSeconds = 900
    response.snapshot.refresh.jsonlDebounceMilliseconds = 250
    response.snapshot.updates.autoCheckEnabled = true
    response.snapshot.updates.checkIntervalSeconds = 3_600
    response.snapshot.ui.launchBehavior = "main_window"
    response.snapshot.ui.overviewRange = "7d"
    var editable = Codexpulse_Core_V1_EditableField()
    editable.key = "online.quotaEnabled"
    editable.editable = true
    response.editableFields = [editable]

    var draft = SettingsDraft(response)
    draft.quotaEnabled = true
    draft.resetCreditsEnabled = false
    draft.quotaIntervalSeconds = 1
    let request = draft.makeRequest(authoritative: response)
    try expect(
        request.expectedRevision == "revision-1", "settings write must carry authoritative revision")
    try expect(request.online.quotaEnabled, "editable field must carry the draft")
    try expect(
        request.online.resetCreditsEnabled, "non-editable field must preserve authoritative truth")
    try expect(
        request.refresh.quotaIntervalSeconds == 300,
        "non-editable numeric field must not be shadow-edited")
}

@MainActor
private func testFeatureGenerationPreventsStaleOverwrite() async throws {
    let supervisor = FakeSupervisor()
    let core = FakeCore(bootstrap: makeNormalBootstrap(), responses: makeResponses())
    await core.setFeatureSessionPlans([
        SessionPagePlan(
            delay: .milliseconds(120), response: makeSessionPage(id: "old", title: "old request")),
        SessionPagePlan(delay: .zero, response: makeSessionPage(id: "new", title: "new request")),
    ])
    let runtime = AppRuntime(supervisor: supervisor, clientFactory: { _ in core })
    let model = AppModel(runtime: runtime)
    model.start()
    try await waitUntil("AppModel reaches overview") {
        await MainActor.run { model.presentation != nil }
    }
    model.loadSessions(reset: true)
    try await Task.sleep(for: .milliseconds(20))
    model.sessionOptions.projectID = "replacement"
    model.sessionFiltersChanged()
    try await waitUntil("replacement feature result") {
        await MainActor.run { model.sessionsState.value?.items.first?.sessionID == "new" }
    }
    try await Task.sleep(for: .milliseconds(140))
    try expect(
        model.sessionsState.value?.items.first?.sessionID == "new",
        "cancelled old generation overwrote replacement")
    _ = await model.shutdown()
}

private func testRequestFactoryAndPresentation() throws {
    let now = Date(timeIntervalSince1970: 1_753_056_000)
    let requests = OverviewRequestSet.make(now: now)
    try expect(requests.sessions.query.page.limit == 5, "overview sessions must stay bounded")
    try expect(
        requests.quota.evaluatedAtMs == Int64(now.timeIntervalSince1970 * 1_000), "quota clock")

    let presentation = OverviewPresentation(makeResponses(partial: true))
    try expect(presentation.isPartial, "partial response meta must remain partial")
    try expect(
        presentation.trend.first?.startAtMS == 1_753_046_400_000,
        "trend presentation must preserve its actual bucket time")
    try expect(
        presentation.projects.first?.id == "project-test",
        "overview must preserve the project detail key")
    try expect(
        presentation.otherProjectTokens == .known(40, unit: "tokens"),
        "overview must expose the remainder outside the top projects")

    var unclassifiedProject = Codexpulse_Core_V1_ProjectItem()
    unclassifiedProject.dimensionKey = "unknown|unknown|missing|missing"
    try expect(
        ProjectPresentation(unclassifiedProject).title == "其他",
        "scratch and unclassified usage must use the product-facing Other label")

    let base = makeResponses()
    var unavailableProjects = base.projects
    unavailableProjects.meta.status = "unavailable"
    let degraded = OverviewPresentation(OverviewResponses(
        usage: base.usage,
        quota: base.quota,
        sessions: base.sessions,
        projects: unavailableProjects,
        health: base.health
    ))
    try expect(!degraded.projectsAvailable, "project query failure must not look like empty data")
    try expect(
        presentation.notices.first?.code == "index_incomplete", "stable issue must survive mapping")
    try expect(presentation.quotaWindows[0].remainingPercent == 0, "real zero remaining must survive")
    try expect(
        presentation.quotaWindows[1].remainingPercent == nil, "unknown quota must not become zero")
    try expect(
        presentation.quotaWindows[0].resetRemainingMS == 3_600_000, "quota reset countdown must survive"
    )
    try expect(presentation.resetCredits.availableCount == 1, "reset credit summary must survive")
    try expect(
        presentation.resetCredits.items.first?.remainingMS == 3_600_000,
        "reset credit detail must survive")
    try expect(
        presentation.usageRangeLabel == "自 7月14日 09:00", "usage range label must show the weekly start")
    try expect(presentation.sessions.first?.project == "Codex Pulse", "session project must survive")
    try expect(
        presentation.sessions.first?.estimatedCost == .known(1_250_000, unit: "usd_micros"),
        "session cost must survive"
    )
    try expect(presentation.totalTokens == .known(0, unit: "tokens"), "real zero token total")
    try expect(
        presentation.estimatedCost == .unknown(reason: "pricing_missing", unit: "usd_micros"),
        "unknown cost must keep reason"
    )
    if case .partial = AppViewState(.normal(makeResponses(partial: true))) {
        // Expected: state mapper refuses to present partial data as fully normal.
    } else {
        throw TestFailure.mismatch(
            "normal runtime response with partial meta was presented as complete")
    }
}

private func testOverviewMergesAllOtherProjectUsage() throws {
    let base = makeResponses()
    var projects = base.projects

    var firstOther = Codexpulse_Core_V1_ProjectItem()
    firstOther.dimensionKey = "unknown|unknown|missing|first"
    firstOther.totals.totalTokens.value = 30
    firstOther.totals.totalTokens.unit = "tokens"

    var secondOther = Codexpulse_Core_V1_ProjectItem()
    secondOther.dimensionKey = "unknown|unknown|missing|second"
    secondOther.totals.totalTokens.value = 10
    secondOther.totals.totalTokens.unit = "tokens"

    projects.items.append(contentsOf: [firstOther, secondOther])
    projects.matchedTotals.totalTokens.value = 150

    let presentation = OverviewPresentation(OverviewResponses(
        usage: base.usage,
        quota: base.quota,
        sessions: base.sessions,
        projects: projects,
        health: base.health
    ))

    try expect(
        presentation.projects.filter { $0.title == "其他" }.count == 1,
        "overview must expose exactly one Other row")
    try expect(
        presentation.projects.first(where: { $0.title == "其他" })?.tokens
            == .known(90, unit: "tokens"),
        "explicit unclassified rows and the page remainder must be combined")
    try expect(
        presentation.otherProjectTokens == .known(90, unit: "tokens"),
        "overview must preserve the combined Other total")
    try expect(
        presentation.projects.first?.title == "其他",
        "the merged Other row must participate in Token ranking")
    try expect(
        presentation.weeklyProjectRanking.map(\.title) == ["Codex Pulse"],
        "popover weekly ranking must exclude every merged Other row")
}

private func testWeeklyProjectRankingFailureStaysLocal() throws {
    let base = makeResponses()
    var unavailableWeeklyProjects = base.weeklyProjects
    unavailableWeeklyProjects.meta.status = "unavailable"
    let presentation = OverviewPresentation(OverviewResponses(
        usage: base.usage,
        quota: base.quota,
        sessions: base.sessions,
        projects: base.projects,
        health: base.health,
        weeklyProjects: unavailableWeeklyProjects
    ))

    try expect(
        !presentation.weeklyProjectRankingAvailable,
        "failed weekly project ranking must render its local unavailable state")
    try expect(
        !presentation.isPartial,
        "optional popover ranking failure must not downgrade the whole overview")
}

@MainActor
private func testAppRuntimeUsesWeeklyQuotaRangeForOverview() async throws {
    let core = FakeCore(bootstrap: makeNormalBootstrap(), responses: makeResponses())
    let runtime = AppRuntime(supervisor: FakeSupervisor(), clientFactory: { _ in core })
    let recorder = StateRecorder()
    await runtime.setStateSink { state in await recorder.append(state) }
    await runtime.start()
    try await waitUntil("weekly usage overview") {
        await recorder.snapshot().contains("normal")
    }
    let usageRequests = await core.recordedUsageRequests()
    let sessionRequests = await core.recordedSessionRequests()
    let projectRequests = await core.recordedProjectRequests()
    try expect(usageRequests.count == 1, "overview must issue one usage request")
    try expect(sessionRequests.count == 1, "overview must issue one session request")
    try expect(
        projectRequests.count == 2,
        "overview must issue one content project request and one weekly ranking request")
    try expect(usageRequests[0].hasExactRange, "overview usage must use weekly quota exact range")
    try expect(
        sessionRequests[0].query.hasExactTimeRange,
        "overview sessions must use weekly quota exact range")
    guard let contentProjectRequest = projectRequests.first(where: { $0.query.filters.isEmpty }),
          let rankingProjectRequest = projectRequests.first(where: {
              $0.query.filters.contains { $0.field == "confidence" }
          })
    else {
        throw TestFailure.mismatch("overview project requests were not separated by purpose")
    }
    try expect(
        contentProjectRequest.query.hasExactTimeRange,
        "overview projects must use weekly quota exact range")
    try expect(
        rankingProjectRequest.query.hasExactTimeRange
            && rankingProjectRequest.query.page.limit == 5
            && rankingProjectRequest.query.sort.first?.field == "totalTokens"
            && rankingProjectRequest.query.sort.first?.direction == "desc",
        "weekly project ranking must be a bounded Token-descending query")
    guard let rankingFilter = rankingProjectRequest.query.filters.first(where: {
        $0.field == "confidence"
    }) else {
        throw TestFailure.mismatch("weekly project ranking omitted the classified-project filter")
    }
    try expect(
        rankingFilter.operator == "in"
            && rankingFilter.values == ["high", "medium", "low"],
        "weekly project ranking must exclude the unclassified Other bucket before pagination")
    try expect(
        usageRequests[0].exactRange.startAtMs == 1_753_059_600_000 - 10_080 * 60_000,
        "overview usage must start at the weekly quota boundary"
    )
    try expect(
        usageRequests[0].exactRange.endAtMs == 1_753_056_000_000,
        "overview usage must end at the quota evaluation time"
    )
    try expect(
        sessionRequests[0].query.exactTimeRange.startAtMs == usageRequests[0].exactRange.startAtMs
            && sessionRequests[0].query.exactTimeRange.endAtMs == usageRequests[0].exactRange.endAtMs,
        "overview sessions must share the usage range"
    )
    try expect(
        contentProjectRequest.query.exactTimeRange.startAtMs == usageRequests[0].exactRange.startAtMs
            && contentProjectRequest.query.exactTimeRange.endAtMs == usageRequests[0].exactRange.endAtMs
            && rankingProjectRequest.query.exactTimeRange.startAtMs == usageRequests[0].exactRange.startAtMs
            && rankingProjectRequest.query.exactTimeRange.endAtMs == usageRequests[0].exactRange.endAtMs,
        "overview projects must share the usage range"
    )
    let calls = await core.recordedCalls()
    guard let quotaIndex = calls.firstIndex(of: "quota"),
        let usageIndex = calls.firstIndex(of: "usage")
    else {
        throw TestFailure.mismatch("overview did not issue quota and usage calls")
    }
    try expect(quotaIndex < usageIndex, "quota must be observed before the weekly usage request")
    _ = await runtime.shutdown()
}

@MainActor
private func testAppRuntimeFallsBackWhenWeeklyQuotaIsUnavailable() async throws {
    let core = FakeCore(
        bootstrap: makeNormalBootstrap(),
        responses: makeResponses(includeWeeklyQuota: false))
    let runtime = AppRuntime(supervisor: FakeSupervisor(), clientFactory: { _ in core })
    let recorder = StateRecorder()
    await runtime.setStateSink { state in await recorder.append(state) }
    await runtime.start()
    try await waitUntil("weekly quota fallback") {
        await recorder.snapshot().contains("partial")
    }
    let usageRequests = await core.recordedUsageRequests()
    try expect(usageRequests.count == 1, "fallback must still query overview usage")
    try expect(usageRequests[0].hasExactRange, "fallback usage must keep an exact range")
    try expect(
        usageRequests[0].granularity == "day", "weekly quota fallback must use a daily trend")
    let phases = await recorder.snapshot()
    try expect(
        !phases.contains("unavailable"),
        "missing weekly quota must not collapse the whole overview")
    _ = await runtime.shutdown()
}

@MainActor
private func testOverviewRangeSelectionRefreshesAllContent() async throws {
    let core = FakeCore(bootstrap: makeNormalBootstrap(), responses: makeResponses())
    let model = AppModel(
        runtime: AppRuntime(supervisor: FakeSupervisor(), clientFactory: { _ in core }))
    try expect(model.overviewRange == .quotaWeek, "overview must default to the quota week")
    model.start()
    try await waitUntil("initial quota-week overview") {
        await core.recordedUsageRequests().count == 1
    }

    model.selectOverviewRange(.today)
    try await waitUntil("today overview") {
        await core.recordedUsageRequests().count == 3
    }

    let refreshedUsageRequests = Array(await core.recordedUsageRequests().suffix(2))
    guard let usage = refreshedUsageRequests.first(where: { $0.granularity == "hour" }),
          let weeklyUsage = refreshedUsageRequests.first(where: { $0.granularity == "day" })
    else {
        throw TestFailure.mismatch("today and status-popover usage requests were not separated")
    }
    let sessions = await core.recordedSessionRequests()[1]
    let projectRequests = await core.recordedProjectRequests()
    try expect(
        projectRequests.count == 4,
        "each overview refresh must keep a separate current-range project request and weekly ranking request")
    guard let projects = projectRequests.first(where: {
        $0.query.filters.isEmpty
            && $0.query.exactTimeRange.startAtMs == usage.exactRange.startAtMs
    }), let weeklyRanking = projectRequests.last(where: {
        $0.query.filters.contains { $0.field == "confidence" }
    }) else {
        throw TestFailure.mismatch("range refresh project requests were not distinguishable")
    }
    try expect(model.overviewRange == .today, "overview selection must remain visible")
    try expect(usage.granularity == "hour", "today overview must request hourly trend")
    try expect(
        weeklyUsage.exactRange.startAtMs == 1_753_059_600_000 - 10_080 * 60_000
            && weeklyUsage.exactRange.endAtMs == 1_753_056_000_000,
        "status popover usage must remain bound to the quota week after the main range changes")
    try expect(
        sessions.query.exactTimeRange.startAtMs == usage.exactRange.startAtMs
            && projects.query.exactTimeRange.startAtMs == usage.exactRange.startAtMs,
        "range selection must refresh usage, sessions, and projects together")
    try expect(
        weeklyRanking.query.exactTimeRange.startAtMs
            == 1_753_059_600_000 - 10_080 * 60_000,
        "popover project ranking must remain bound to the quota week after the main range changes")
    _ = await model.shutdown()
}

@MainActor
private func testOverviewProjectFailureDoesNotHideUsageAndSessions() async throws {
    let core = FakeCore(bootstrap: makeNormalBootstrap(), responses: makeResponses())
    await core.setOverviewProjectFailure(true)
    let runtime = AppRuntime(supervisor: FakeSupervisor(), clientFactory: { _ in core })
    let recorder = StateRecorder()
    await runtime.setStateSink { state in await recorder.append(state) }
    await runtime.start()
    try await waitUntil("project section partial overview") {
        await recorder.snapshot().contains("partial")
    }
    let phases = await recorder.snapshot()
    try expect(
        !phases.contains("unavailable"),
        "one failed overview section must not hide the successful sections")
    let usageCount = await core.recordedUsageRequests().count
    let sessionCount = await core.recordedSessionRequests().count
    try expect(
        usageCount == 1 && sessionCount == 1,
        "usage and sessions must remain independently queryable")
    _ = await runtime.shutdown()
}

private func testQuotaWindowPresentationUsesActualDuration() throws {
    let cases: [(kind: String, id: String, name: String?, minutes: Int64?, expected: String)] = [
        ("primary", "codex", nil, 10_080, "通用额度 · 7 天"),
        ("primary", "codex", "  ", 10_080, "通用额度 · 7 天"),
        ("primary", "codex_spark", "GPT-5.3-Codex-Spark", 10_080,
         "GPT-5.3-Codex-Spark · 7 天"),
        ("primary", "codex", nil, 300, "通用额度 · 5 小时"),
        ("secondary", "codex", nil, 1_440, "通用额度 · 24 小时"),
        ("secondary", "codex", nil, 90, "通用额度 · 90 分钟"),
        ("primary", "codex", nil, nil, "通用额度"),
        ("primary", "codex_bengalfox", nil, 10_080, "模型专属额度 · 7 天"),
    ]

    for item in cases {
        var window = Codexpulse_Core_V1_CurrentWindow()
        window.windowKind = item.kind
        window.limitID = item.id
        if let name = item.name { window.limitName = name }
        if let minutes = item.minutes { window.windowMinutes = minutes }
        window.remainingPercent = 42
        window.freshness = "fresh"

        let presentation = QuotaWindowPresentation(window)
        try expect(
            presentation.title == item.expected,
            "quota window \(item.id)/\(item.minutes.map(String.init) ?? "nil") must be titled \(item.expected), got \(presentation.title)"
        )
    }
}

private func testTokenQuantityFormatterUsesChineseMagnitudeUnits() throws {
    let cases: [(value: Int64, expected: String)] = [
        (0, "0.00 百万"),
        (999_999, "1.00 百万"),
        (1_000_000, "1.00 百万"),
        (9_999_999, "10.00 百万"),
        (10_000_000, "1.00 千万"),
        (99_999_999, "10.00 千万"),
        (100_000_000, "1.00 亿"),
        (1_360_518_381, "13.61 亿"),
        (4_002_161_268, "40.02 亿"),
    ]

    for item in cases {
        try expect(
            TokenQuantityFormatter.string(item.value) == item.expected,
            "token quantity \(item.value) must be formatted as \(item.expected)"
        )
    }

    let compactCases: [(value: Int64, expected: String)] = [
        (0, "0"),
        (9_999, "9999"),
        (10_000, "1万"),
        (1_000_000, "1百万"),
        (10_000_000, "1千万"),
        (100_000_000, "1亿"),
        (790_000_000, "7.9亿"),
    ]
    for item in compactCases {
        try expect(
            TokenQuantityFormatter.compactString(item.value) == item.expected,
            "compact token quantity \(item.value) must be formatted as \(item.expected)"
        )
    }
}

private func testTokenBreakdownPresentationPreservesInputOutputSemantics() throws {
    var totals = Codexpulse_Core_V1_UsageTotals()
    totals.inputTokens.value = 801_000_000
    totals.inputTokens.unit = "tokens"
    totals.cachedInputTokens.value = 782_000_000
    totals.cachedInputTokens.unit = "tokens"
    totals.outputTokens.value = 270_000_000
    totals.outputTokens.unit = "tokens"
    totals.reasoningTokens.value = 70_870_000
    totals.reasoningTokens.unit = "tokens"
    totals.totalTokens.value = 1_071_000_000
    totals.totalTokens.unit = "tokens"

    let presentation = TokenBreakdownPresentation(totals)
    try expect(
        presentation.input == .known(801_000_000, unit: "tokens"),
        "Token breakdown must preserve input tokens"
    )
    try expect(
        presentation.cachedInput == .known(782_000_000, unit: "tokens"),
        "cached input must remain a subset detail of input"
    )
    try expect(
        presentation.output == .known(270_000_000, unit: "tokens"),
        "Token breakdown must preserve output tokens"
    )
    try expect(
        presentation.reasoning == .known(70_870_000, unit: "tokens"),
        "reasoning must remain a subset detail of output"
    )
    try expect(
        presentation.total == .known(1_071_000_000, unit: "tokens"),
        "Token breakdown must preserve the authoritative total"
    )
}

private func testUsageModelTrendResolverUsesOnlyReconciledDailyFacts() throws {
    func point(_ key: String, tokens: Int64) -> Codexpulse_Core_V1_TrendPoint {
        var point = Codexpulse_Core_V1_TrendPoint()
        point.key = key
        point.totals.totalTokens.value = tokens
        point.totals.totalTokens.unit = "tokens"
        return point
    }

    var response = Codexpulse_Core_V1_UsageCostResponse()
    response.trend = [
        point("2026-07-22", tokens: 100),
        point("2026-07-23", tokens: 100),
        point("2026-07-24", tokens: 100),
    ]

    var first = Codexpulse_Core_V1_UsageModelItem()
    first.dimensionKey = "gpt-5.6"
    first.model.displayName = "GPT-5.6"
    first.trend = [
        point("2026-07-22", tokens: 60),
        point("2026-07-23", tokens: 70),
        point("2026-07-24", tokens: 100),
    ]
    var second = Codexpulse_Core_V1_UsageModelItem()
    second.dimensionKey = "private-unknown-key"
    var unknownPoint = Codexpulse_Core_V1_TrendPoint()
    unknownPoint.key = "2026-07-24"
    unknownPoint.totals.totalTokens.unknownReason = "source_incomplete"
    unknownPoint.totals.totalTokens.unit = "tokens"
    second.trend = [point("2026-07-22", tokens: 40), unknownPoint]
    response.models = [first, second]

    let buckets = UsageModelTrendResolver.buckets(response)
    try expect(buckets.count == 3, "usage model resolver must preserve every known total bucket")
    try expect(
        buckets[0].breakdownAvailable && buckets[0].segments.map(\.tokens) == [60, 40],
        "an exactly reconciled day must retain its real per-model segments"
    )
    try expect(
        buckets[0].segments.map(\.modelName) == ["GPT-5.6", "其他模型"],
        "missing model display names must not expose internal dimension keys"
    )
    try expect(
        !buckets[1].breakdownAvailable
            && buckets[1].segments.count == 1
            && buckets[1].segments[0].modelName == "全部模型"
            && buckets[1].segments[0].tokens == 100,
        "an unreconciled day must fall back to its known total instead of inventing a model split"
    )
    try expect(
        !buckets[2].breakdownAvailable && buckets[2].segments[0].tokens == 100,
        "an unknown model bucket must invalidate the split even when the remaining known values match the total"
    )
}

private func testStatusBarQuotaPresentationUsesOnlyMatchingPeriodUsage() throws {
    let base = makeResponses()
    var usage = base.usage
    usage.totals.inputTokens.value = 780_000_000
    usage.totals.inputTokens.unit = "tokens"
    usage.totals.cachedInputTokens.value = 750_000_000
    usage.totals.cachedInputTokens.unit = "tokens"
    usage.totals.outputTokens.value = 10_000_000
    usage.totals.outputTokens.unit = "tokens"
    usage.totals.reasoningTokens.value = 3_000_000
    usage.totals.reasoningTokens.unit = "tokens"
    usage.totals.totalTokens.value = 790_000_000

    var quota = base.quota
    var shortWindow = Codexpulse_Core_V1_CurrentWindow()
    shortWindow.windowKind = "primary"
    shortWindow.limitID = "short"
    shortWindow.windowMinutes = 300
    shortWindow.remainingPercent = 80
    shortWindow.resetsAtMs = quota.current.evaluatedAtMs + 3_600_000
    shortWindow.freshness = "fresh"
    quota.current.windows.insert(shortWindow, at: 0)

    var modelSpecificWindow = Codexpulse_Core_V1_CurrentWindow()
    modelSpecificWindow.windowKind = "primary"
    modelSpecificWindow.limitID = "codex_spark"
    modelSpecificWindow.limitName = "GPT-5.3-Codex-Spark"
    modelSpecificWindow.windowMinutes = 10_080
    modelSpecificWindow.remainingPercent = 100
    modelSpecificWindow.resetsAtMs = quota.current.evaluatedAtMs + 86_400_000
    modelSpecificWindow.freshness = "fresh"
    quota.current.windows.insert(modelSpecificWindow, at: 0)

    let matchingOverview = OverviewPresentation(OverviewResponses(
        usage: usage,
        quota: quota,
        sessions: base.sessions,
        projects: base.projects,
        health: base.health
    ))
    guard let matching = StatusBarQuotaPresentation(matchingOverview) else {
        throw TestFailure.mismatch("matching weekly status bar summary was unavailable")
    }
    try expect(matching.periodLabel == "周剩", "status bar must prefer the general weekly window")
    try expect(matching.remainingText == "周剩 0%", "status bar must preserve confirmed zero remaining")
    try expect(
        matching.usageText == "已用 7.9亿",
        "status bar must retain the compact total-only text"
    )
    try expect(
        matching.accessibilityLabel.contains("已用 7.9亿 Token")
            && !matching.accessibilityLabel.contains("输入"),
        "status bar accessibility must retain the original total-only wording and Token unit"
    )

    let plannedRange = OverviewRequestSet.resolveRange(
        .quotaWeek,
        quota: quota,
        now: Date(timeIntervalSince1970: TimeInterval(quota.current.evaluatedAtMs) / 1_000)
    )
    var todayUsage = usage
    todayUsage.range.startAtMs += 60_000
    let independentWeeklyOverview = OverviewPresentation(OverviewResponses(
        usage: todayUsage,
        quota: quota,
        sessions: base.sessions,
        projects: base.projects,
        health: base.health,
        rangeResolution: plannedRange,
        weeklyUsage: usage
    ))
    guard let independentWeekly = StatusBarQuotaPresentation(independentWeeklyOverview) else {
        throw TestFailure.mismatch("quota summary disappeared when the main usage range changed")
    }
    try expect(
        independentWeekly.usageText == "已用 7.9亿",
        "status bar must use its weekly response instead of the main overview range"
    )
}

private func testStatusBarStyleSelectionAndLegacyFallback() throws {
    try expect(
        StatusBarStyle.allCases.map(\.rawValue)
            == ["ring_summary", "open_ring_summary", "gauge_summary"],
        "status bar must expose only the approved A/B/D styles"
    )
    try expect(
        StatusBarStyle.resolve(storedValue: "open_ring_summary") == .openRingSummary,
        "approved status bar preference must survive reload"
    )
    for legacy in ["countdown", "battery", "meters", "rings", "unsupported", nil] as [String?] {
        try expect(
            StatusBarStyle.resolve(storedValue: legacy) == .ringSummary,
            "legacy or unknown status bar preference must migrate to A"
        )
    }
}

private func testQuotaRemainingLevelUsesGreenYellowRedThresholds() throws {
    try expect(
        QuotaRemainingLevel(remainingPercent: 98) == .healthy,
        "98% remaining must be healthy/green"
    )
    try expect(
        QuotaRemainingLevel(remainingPercent: 40) == .warning,
        "40% remaining must be warning/yellow"
    )
    try expect(
        QuotaRemainingLevel(remainingPercent: 20) == .critical,
        "20% remaining must be critical/red"
    )
    try expect(
        QuotaRemainingLevel(remainingPercent: nil) == .unavailable
            && QuotaRemainingLevel(remainingPercent: .nan) == .unavailable,
        "missing or invalid remaining quota must stay unavailable"
    )
}

private func testMainWindowLayoutPrefersFullOverviewWithoutLeavingTheScreen() throws {
    let spacious = MainWindowLayout.initialContentSize(
        visibleFrameWidth: 1_920,
        visibleFrameHeight: 1_080,
        frameChromeWidth: 0,
        frameChromeHeight: 52
    )
    try expect(
        spacious == MainWindowContentSize(width: 1_440, height: 900),
        "a spacious screen must open the complete 1440x900 overview"
    )

    let compact = MainWindowLayout.initialContentSize(
        visibleFrameWidth: 1_280,
        visibleFrameHeight: 800,
        frameChromeWidth: 0,
        frameChromeHeight: 52
    )
    try expect(
        compact == MainWindowContentSize(width: 1_280, height: 748),
        "a compact screen must constrain the window to its visible frame"
    )
}

private func testWeeklyQuotaUsageRequestUsesExactWindowStart() throws {
    var calendar = Calendar(identifier: .gregorian)
    calendar.timeZone = TimeZone(identifier: "Asia/Shanghai")!
    let evaluatedAtMS: Int64 = 1_753_056_000_000
    let resetAtMS: Int64 = 1_753_059_600_000

    var shortWindow = Codexpulse_Core_V1_CurrentWindow()
    shortWindow.windowKind = "primary"
    shortWindow.windowMinutes = 300
    shortWindow.resetsAtMs = evaluatedAtMS + 60_000
    var weeklyWindow = Codexpulse_Core_V1_CurrentWindow()
    weeklyWindow.windowKind = "secondary"
    weeklyWindow.limitID = "codex"
    weeklyWindow.windowMinutes = 10_080
    weeklyWindow.resetsAtMs = resetAtMS
    var modelWeeklyWindow = weeklyWindow
    modelWeeklyWindow.limitID = "codex_spark"
    modelWeeklyWindow.limitName = "GPT-5.3-Codex-Spark"
    modelWeeklyWindow.resetsAtMs = resetAtMS + 86_400_000
    var quota = Codexpulse_Core_V1_QuotaCurrentResponse()
    quota.current.evaluatedAtMs = evaluatedAtMS
    quota.current.windows = [shortWindow, modelWeeklyWindow, weeklyWindow]

    guard let request = OverviewRequestSet.weeklyUsageRequest(quota: quota, calendar: calendar) else {
        throw TestFailure.mismatch("weekly quota usage request was unavailable")
    }
    try expect(request.hasExactRange, "weekly usage request must use an exact UTC range")
    try expect(
        request.exactRange.startAtMs == resetAtMS - 10_080 * 60_000,
        "weekly usage must prefer the general limit when several seven-day windows exist"
    )
    try expect(
        request.exactRange.endAtMs == evaluatedAtMS, "weekly usage end must use quota evaluation time")
    try expect(
        request.exactRange.timeZone == "Asia/Shanghai", "weekly usage must keep reporting timezone")
    try expect(request.granularity == "day", "weekly usage trend must remain daily")

    weeklyWindow.clearResetsAtMs()
    quota.current.windows = [weeklyWindow]
    try expect(
        OverviewRequestSet.weeklyUsageRequest(quota: quota, calendar: calendar) == nil,
        "weekly usage must not invent a range when reset time is unknown"
    )
}

private func testOverviewRangeResolutionDrivesEveryContentRequest() throws {
    var calendar = Calendar(identifier: .gregorian)
    calendar.timeZone = TimeZone(identifier: "Asia/Shanghai")!
    let now = Date(timeIntervalSince1970: 1_753_056_000)
    let evaluatedAtMS = Int64(now.timeIntervalSince1970 * 1_000)
    let resetAtMS = evaluatedAtMS + 3_600_000
    var weeklyWindow = Codexpulse_Core_V1_CurrentWindow()
    weeklyWindow.windowMinutes = 10_080
    weeklyWindow.resetsAtMs = resetAtMS
    var quota = Codexpulse_Core_V1_QuotaCurrentResponse()
    quota.current.evaluatedAtMs = evaluatedAtMS
    quota.current.windows = [weeklyWindow]

    let weekly = OverviewRequestSet.resolveRange(
        .quotaWeek, quota: quota, now: now, calendar: calendar)
    try expect(weekly.effectivePreset == .quotaWeek, "weekly quota range must remain selected")
    try expect(!weekly.fellBackFromQuotaWeek, "valid weekly quota must not fall back")
    try expect(
        weekly.startAtMS == resetAtMS - 10_080 * 60_000,
        "weekly quota range must start at its authoritative boundary")
    try expect(weekly.endAtMS == evaluatedAtMS, "weekly quota range must end at evaluation time")
    try expect(weekly.granularity == "day", "weekly quota trend must stay daily")

    let requests = OverviewRequestSet.content(range: weekly)
    try expect(requests.usage.exactRange.startAtMs == weekly.startAtMS, "usage range start")
    try expect(requests.sessions.query.exactTimeRange.startAtMs == weekly.startAtMS, "session range start")
    try expect(requests.projects.query.exactTimeRange.startAtMs == weekly.startAtMS, "project range start")
    try expect(requests.sessions.query.page.limit == 5, "overview sessions must stay bounded")
    try expect(requests.projects.query.page.limit == 5, "overview projects must stay bounded")
    try expect(requests.sessions.query.sort.first?.field == "lastActivityAt", "sessions sort by recent activity")
    try expect(requests.projects.query.sort.first?.field == "totalTokens", "projects sort by Token")

    let today = OverviewRequestSet.resolveRange(.today, quota: quota, now: now, calendar: calendar)
    try expect(today.granularity == "hour", "today trend must use hourly aggregation")
    try expect(today.endAtMS == evaluatedAtMS, "today range must end now")

    let fallback = OverviewRequestSet.resolveRange(
        .quotaWeek, quota: .init(), now: now, calendar: calendar)
    try expect(fallback.effectivePreset == .sevenDays, "missing quota week must fall back to seven days")
    try expect(fallback.fellBackFromQuotaWeek, "weekly fallback must remain explicit")
}

private func testLaunchConfigurationBoundaries() throws {
    do {
        _ = try AppLaunchConfiguration(
            helperExecutablePath: "/usr/bin/true",
            runtimeDirectory: "/private/tmp/cp-safe/../escape"
        )
        throw TestFailure.mismatch("runtime path traversal was accepted")
    } catch AppLaunchConfigurationError.runtimeDirectoryUnavailable {
        // Expected.
    }
    let parsed = try AppLaunchConfiguration.parse(
        arguments: [
            "codex-pulse-app",
            "-psn_0_12345",
            "--helper", "/usr/bin/true",
            "--runtime-directory", "/private/tmp/cp-test-launch",
        ]
    )
    try expect(
        parsed.helperExecutablePath == "/usr/bin/true", "LaunchServices argument must be ignored")
}

private func testLaunchConfigurationUsesPersistentProductDefaults() throws {
    let expectedRuntime = FileManager.default.homeDirectoryForCurrentUser
        .appendingPathComponent("Library", isDirectory: true)
        .appendingPathComponent("Application Support", isDirectory: true)
        .appendingPathComponent("Codex Pulse", isDirectory: true)
        .appendingPathComponent("runtime", isDirectory: true)
        .path
    let parsed = try AppLaunchConfiguration.parse(
        arguments: [
            "codex-pulse-app",
            "--helper", "/usr/bin/true",
        ]
    )

    try expect(
        parsed.runtimeDirectory == expectedRuntime,
        "ordinary App launch must reuse the private persistent runtime")
    try expect(
        !parsed.clientVersion.isEmpty && parsed.clientVersion != "dev",
        "ordinary App launch must send product metadata instead of dev")
    _ = try AppLaunchConfiguration(
        helperExecutablePath: "/usr/bin/true",
        runtimeDirectory: expectedRuntime
    )
}

private func testNormalLifecycleAndShutdown() async throws {
    let supervisor = FakeSupervisor()
    let core = FakeCore(bootstrap: makeNormalBootstrap(), responses: makeResponses())
    let recorder = StateRecorder()
    let runtime = AppRuntime(supervisor: supervisor, clientFactory: { _ in core })
    await runtime.setStateSink { state in await recorder.append(state) }

    await runtime.start()
    var phases = await recorder.snapshot()
    try expect(phases.contains("starting"), "runtime must expose starting")
    try expect(phases.contains("handshaking"), "runtime must expose handshaking")
    try expect(phases.contains("loading_overview"), "runtime must expose overview loading")
    try expect(phases.last == "normal", "runtime must reach normal")

    let actionReceipt = try await runtime.runRuntimeAction(.reconcile)
    try expect(
        actionReceipt.action == "reconcile", "runtime action must use the generated Core receipt")

    await runtime.applicationWillResignActive()
    let usageCallsBeforeActivation = await core.recordedCalls().filter { $0 == "usage" }.count
    await runtime.applicationDidBecomeActive()
    let usageCallsAfterActivation = await core.recordedCalls().filter { $0 == "usage" }.count
    try expect(
        usageCallsAfterActivation == usageCallsBeforeActivation,
        "application activation must reuse the invalidation stream instead of refreshing Overview"
    )
    await runtime.prepareForSleep()
    await runtime.applicationDidBecomeActive()
    await runtime.resumeAfterWake()
    let calls = await core.recordedCalls()
    try expect(
        calls.contains("lifecycle:application_did_become_active"), "active lifecycle must reach Core")
    try expect(
        calls.filter { $0 == "lifecycle:application_did_become_active" }.count == 2,
        "active-before-wake must be replayed after stream recovery"
    )
    try expect(calls.contains("lifecycle:system_will_sleep"), "sleep lifecycle must reach Core")
    try expect(calls.contains("lifecycle:system_did_wake"), "wake lifecycle must reach Core")
    try expect(calls.contains("runtime_action:reconcile"), "confirmed runtime action must reach Core")

    let outcome = await runtime.shutdown()
    try expect(outcome == .clean, "normal shutdown must read back clean Helper exit")
    phases = await recorder.snapshot()
    try expect(phases.suffix(2) == ["shutting_down", "stopped"], "shutdown state order")
    let shutdownCalls = await core.recordedCalls()
    try expect(shutdownCalls.contains("shutdown:client_exit"), "Shutdown RPC must run")
}

private func testRecoveryAndRestartRequired() async throws {
    var bootstrap = Codexpulse_Core_V1_BootstrapResponse()
    bootstrap.mode = "recovery"
    bootstrap.recovery.version = "migration-recovery-v1"
    bootstrap.recovery.phase = "failed"
    bootstrap.recovery.stage = "migrate"
    bootstrap.recovery.code = "schema_future"
    var receipt = Codexpulse_Core_V1_MigrationRecoveryReceipt()
    receipt.restartRequired = true
    let supervisor = FakeSupervisor()
    let core = FakeCore(bootstrap: bootstrap, recoveryReceipt: receipt, responses: makeResponses())
    let recorder = StateRecorder()
    let runtime = AppRuntime(supervisor: supervisor, clientFactory: { _ in core })
    await runtime.setStateSink { state in await recorder.append(state) }

    await core.setBootstrapDelay(.milliseconds(80))
    let start = Task { await runtime.start() }
    try await waitUntil("recovery bootstrap in flight") {
        await core.recordedCalls().contains("bootstrap")
    }
    await runtime.applicationDidBecomeActive()
    await start.value
    let recoveryPhases = await recorder.snapshot()
    try expect(recoveryPhases.last == "recovery", "recovery Bootstrap must block Overview")
    let callsBeforeRetry = await core.recordedCalls()
    try expect(!callsBeforeRetry.contains("usage"), "recovery must not issue normal Overview RPCs")
    try expect(
        !callsBeforeRetry.contains("lifecycle:application_did_become_active"),
        "pending active must not bypass recovery Bootstrap"
    )
    await runtime.retryRecovery()
    let retryPhases = await recorder.snapshot()
    try expect(
        retryPhases.last == "restart_required", "recovery receipt must expose restart required")
    _ = await runtime.shutdown()
}

private func testStaleAndUnavailable() async throws {
    let supervisor = FakeSupervisor()
    let core = FakeCore(bootstrap: makeNormalBootstrap(), responses: makeResponses())
    let recorder = StateRecorder()
    let runtime = AppRuntime(supervisor: supervisor, clientFactory: { _ in core })
    await runtime.setStateSink { state in await recorder.append(state) }
    await runtime.start()
    await core.setOverviewFailure(true)
    await runtime.refresh()
    let stalePhases = await recorder.snapshot()
    try expect(stalePhases.last == "stale", "refresh failure with snapshot must be stale")
    _ = await runtime.shutdown()

    let failedSupervisor = FakeSupervisor(startFailure: true)
    let failedRecorder = StateRecorder()
    let failedRuntime = AppRuntime(supervisor: failedSupervisor, clientFactory: { _ in core })
    await failedRuntime.setStateSink { state in await failedRecorder.append(state) }
    await failedRuntime.start()
    let unavailablePhases = await failedRecorder.snapshot()
    try expect(unavailablePhases.last == "unavailable", "startup failure must be unavailable")
}

@MainActor
private func testContractUnavailableCannotRestartLoop() async throws {
    let supervisor = FakeSupervisor()
    let core = FakeCore(bootstrap: makeNormalBootstrap(), responses: makeResponses())
    await core.setHandshakeError(.incompatibleContract(expected: "expected", actual: "actual"))
    let model = AppModel(runtime: AppRuntime(supervisor: supervisor, clientFactory: { _ in core }))
    model.start()
    try await waitUntil("contract unavailable") {
        await MainActor.run {
            if case .unavailable(let notice) = model.state { return !notice.retryable }
            return false
        }
    }
    try expect(
        !model.requiresCoreRestart, "non-retryable contract failure must not be labeled restartable")
    try expect(
        !model.canRefreshOrRestart,
        "non-retryable contract failure must disable refresh/restart actions")
    model.refreshOrRestart()
    try await Task.sleep(for: .milliseconds(40))
    let counts = await supervisor.counts()
    try expect(counts.0 == 1, "non-retryable contract failure must not start a reconnect loop")
    _ = await model.shutdown()
}

@MainActor
private func testManualOverviewRefreshExposesBusyState() async throws {
    let supervisor = FakeSupervisor()
    let core = FakeCore(bootstrap: makeNormalBootstrap(), responses: makeResponses())
    let model = AppModel(runtime: AppRuntime(supervisor: supervisor, clientFactory: { _ in core }))
    model.start()
    try await waitUntil("manual refresh initial overview") {
        await MainActor.run { model.presentation != nil }
    }

    await core.setOverviewDelay(.milliseconds(120))
    model.refreshOrRestart()

    try expect(
        !model.canRefreshOrRestart, "manual overview refresh must expose a busy state immediately")
    try await waitUntil("manual refresh completes") {
        await MainActor.run { model.canRefreshOrRestart }
    }
    _ = await model.shutdown()
}

private func testShutdownDuringStartup() async throws {
    let supervisor = FakeSupervisor(startDelay: .milliseconds(100))
    let core = FakeCore(bootstrap: makeNormalBootstrap(), responses: makeResponses())
    let recorder = StateRecorder()
    let runtime = AppRuntime(supervisor: supervisor, clientFactory: { _ in core })
    await runtime.setStateSink { state in await recorder.append(state) }

    let start = Task { await runtime.start() }
    try await Task.sleep(for: .milliseconds(20))
    let outcome = await runtime.shutdown()
    await start.value

    try expect(outcome == .clean, "startup shutdown without connected Core must be clean")
    let phases = await recorder.snapshot()
    try expect(phases.last == "stopped", "stale startup callback must not overwrite stopped")
    if let stopped = phases.lastIndex(of: "stopped") {
        try expect(
            !phases.suffix(from: phases.index(after: stopped)).contains("cancelled"),
            "no cancelled after stopped")
        try expect(
            !phases.suffix(from: phases.index(after: stopped)).contains("unavailable"),
            "no unavailable after stopped")
    }
}

private func testConcurrentStartIsCoalesced() async throws {
    let supervisor = FakeSupervisor(startDelay: .milliseconds(80))
    let core = FakeCore(bootstrap: makeNormalBootstrap(), responses: makeResponses())
    let runtime = AppRuntime(supervisor: supervisor, clientFactory: { _ in core })

    let first = Task { await runtime.start() }
    try await Task.sleep(for: .milliseconds(10))
    await runtime.start()
    await first.value

    let counts = await supervisor.counts()
    try expect(counts.0 == 1, "concurrent start must spawn exactly one Helper")
    _ = await runtime.shutdown()
}

private func testCancelledRefreshCannotOverwriteReplacement() async throws {
    let supervisor = FakeSupervisor()
    let core = FakeCore(bootstrap: makeNormalBootstrap(), responses: makeResponses())
    let recorder = StateRecorder()
    let runtime = AppRuntime(supervisor: supervisor, clientFactory: { _ in core })
    await runtime.setStateSink { state in await recorder.append(state) }
    await runtime.start()

    await core.setOverviewDelay(.milliseconds(100))
    let first = Task { await runtime.refresh() }
    try await Task.sleep(for: .milliseconds(20))
    await runtime.cancelRefresh()
    await core.setOverviewDelay(.zero)
    await runtime.refresh()
    await first.value

    let phases = await recorder.snapshot()
    try expect(phases.last == "normal", "cancelled refresh must not overwrite its replacement")
    _ = await runtime.shutdown()
}

private func testPendingActiveWaitsForBootstrap() async throws {
    let supervisor = FakeSupervisor()
    let core = FakeCore(bootstrap: makeNormalBootstrap(), responses: makeResponses())
    await core.setHandshakeDelay(.milliseconds(80))
    let runtime = AppRuntime(supervisor: supervisor, clientFactory: { _ in core })

    let start = Task { await runtime.start() }
    try await waitUntil("handshake in flight") {
        await core.recordedCalls().contains("handshake")
    }
    await runtime.applicationDidBecomeActive()
    await start.value

    let calls = await core.recordedCalls()
    guard let bootstrap = calls.firstIndex(of: "bootstrap"),
        let lifecycle = calls.firstIndex(of: "lifecycle:application_did_become_active"),
        let usage = calls.firstIndex(of: "usage")
    else { throw TestFailure.mismatch("pending active calls were not delivered") }
    try expect(bootstrap < usage, "Bootstrap must precede Overview RPCs")
    try expect(bootstrap < lifecycle, "Bootstrap must precede pending active lifecycle")
    _ = await runtime.shutdown()
}

private func testSleepDuringStartupDefersOverviewUntilWake() async throws {
    let supervisor = FakeSupervisor()
    let core = FakeCore(bootstrap: makeNormalBootstrap(), responses: makeResponses())
    await core.setHandshakeDelay(.milliseconds(80))
    let recorder = StateRecorder()
    let runtime = AppRuntime(supervisor: supervisor, clientFactory: { _ in core })
    await runtime.setStateSink { state in await recorder.append(state) }

    let start = Task { await runtime.start() }
    try await waitUntil("sleep startup handshake") {
        await core.recordedCalls().contains("handshake")
    }
    await runtime.prepareForSleep()
    await start.value

    var calls = await core.recordedCalls()
    try expect(
        calls.contains("lifecycle:system_will_sleep"), "startup sleep must reach Core after Bootstrap")
    try expect(!calls.contains("usage"), "startup sleep must defer Overview queries")
    await runtime.resumeAfterWake()
    calls = await core.recordedCalls()
    try expect(calls.contains("lifecycle:system_did_wake"), "startup wake must reach Core")
    try expect(calls.contains("usage"), "wake must load the deferred Overview")
    let phases = await recorder.snapshot()
    try expect(phases.last == "normal", "wake after startup sleep must reach normal")
    _ = await runtime.shutdown()
}

private final class FakeProcessMonitor: HelperProcessMonitoring, @unchecked Sendable {
    private let lock = NSLock()
    private var cancelled = false

    func cancel() {
        lock.lock()
        cancelled = true
        lock.unlock()
    }

    func isCancelled() -> Bool {
        lock.lock()
        defer { lock.unlock() }
        return cancelled
    }
}

private final class ProcessExitHarness: @unchecked Sendable {
    private let lock = NSLock()
    private var callback: (@Sendable () -> Void)?
    private var monitor: FakeProcessMonitor?

    func makeMonitor(
        processID: Int32,
        onExit: @escaping @Sendable () -> Void
    ) -> any HelperProcessMonitoring {
        let monitor = FakeProcessMonitor()
        lock.lock()
        callback = onExit
        self.monitor = monitor
        lock.unlock()
        return monitor
    }

    func triggerExit() {
        lock.lock()
        let callback = callback
        lock.unlock()
        callback?()
    }
}

private func testHelperExitBecomesStale() async throws {
    let supervisor = FakeSupervisor()
    let core = FakeCore(bootstrap: makeNormalBootstrap(), responses: makeResponses())
    let recorder = StateRecorder()
    let exit = ProcessExitHarness()
    let runtime = AppRuntime(
        supervisor: supervisor,
        processMonitorFactory: { processID, onExit in
            exit.makeMonitor(processID: processID, onExit: onExit)
        },
        clientFactory: { _ in core }
    )
    await runtime.setStateSink { state in await recorder.append(state) }
    await runtime.start()
    exit.triggerExit()
    try await waitUntil("Helper exit state") {
        await recorder.snapshot().last == "stale"
    }
    let counts = await supervisor.counts()
    try expect(counts.1 == 1, "Helper exit must stop the supervised process")
    await runtime.restart()
    try await waitUntil("Helper exit recovery") {
        await recorder.snapshot().last == "normal"
    }
    let recoveredCounts = await supervisor.counts()
    try expect(recoveredCounts.0 == 2, "Helper exit recovery must start a fresh Helper")
    _ = await runtime.shutdown()
}

@MainActor
private func testHelperExitCannotBecomeFeatureCancelled() async throws {
    let core = FakeCore(bootstrap: makeNormalBootstrap(), responses: makeResponses())
    await core.setFeatureSessionPlans([
        SessionPagePlan(delay: .zero, response: makeSessionPage(id: "last-good", title: "last good")),
        SessionPagePlan(
            delay: .milliseconds(250), response: makeSessionPage(id: "too-late", title: "too late")),
    ])
    let exit = ProcessExitHarness()
    let runtime = AppRuntime(
        supervisor: FakeSupervisor(),
        processMonitorFactory: { processID, onExit in
            exit.makeMonitor(processID: processID, onExit: onExit)
        },
        clientFactory: { _ in core }
    )
    let model = AppModel(runtime: runtime)
    model.start()
    try await waitUntil("feature terminal overview") {
        await MainActor.run { model.presentation != nil }
    }
    model.navigate(to: .sessions)
    try await waitUntil("feature terminal last good") {
        await MainActor.run { model.sessionsState.value?.items.first?.sessionID == "last-good" }
    }
    model.loadSessions(reset: true)
    try await Task.sleep(for: .milliseconds(20))
    exit.triggerExit()
    try await waitUntil("feature terminal stale state") {
        await MainActor.run {
            if case .stale = model.sessionsState { return true }
            return false
        }
    }
    try expect(
        model.sessionsState.value?.items.first?.sessionID == "last-good",
        "Helper exit must preserve the last good feature snapshot"
    )
    _ = await model.shutdown()
}

private func testShutdownDeadlineForcesHelperStop() async throws {
    let supervisor = FakeSupervisor()
    let core = FakeCore(bootstrap: makeNormalBootstrap(), responses: makeResponses())
    await core.setShutdownDelay(.seconds(60))
    let runtime = AppRuntime(
        supervisor: supervisor,
        shutdownRequestTimeout: .milliseconds(40),
        clientFactory: { _ in core }
    )
    await runtime.start()
    let clock = ContinuousClock()
    let started = clock.now
    let outcome = await runtime.shutdown()
    try expect(outcome == .forced, "Shutdown deadline must use forced Helper stop")
    try expect(started.duration(to: clock.now) < .seconds(1), "Shutdown deadline must stay bounded")
}

@main
struct CodexPulseAppTestMain {
    static func main() async throws {
        try testPrimaryPagesSmokeSummaryIncludesProjectDetailEvidence()
        try testMainWindowCopyDoesNotExposeImplementationLanguage()
        try testOverviewMergesAllOtherProjectUsage()
        try testWeeklyProjectRankingFailureStaysLocal()
        try testOverviewUsesOneNavigationAndARealTrendChart()
        try testOverviewParallelReadsAvoidAsyncLetReleaseCrash()
        try testUsageChartStacksModelsWithLocalizedHoverDetails()
        try testSessionAndProjectDailyTrendsShowSelectionRuleAndDateDetail()
        try testEveryTokenChartUsesLocalizedAxisAndAccessibilityUnits()
        try testEveryTokenSurfaceUsesInputOutputBreakdown()
        try testStatusPopoverShowsLocalizedModelDailyTrend()
        try testPopoverWeeklyTrendDoesNotFollowOverviewRange()
        try testTrendSelectionSnapsToNearestRealPoint()
        try testSidebarSettingsUsesSystemRowSpacing()
        try testSettingsOverviewRangeFallbackMatchesProductOptions()
        try testSettingsExplainsAutomaticDefaultHome()
        try testStatusPillUsesProductCopy()
        try testStatusItemRefreshReadsCommittedState()
        try testInitialWindowUsesScreenAwarePreferredLayout()
        try testNativeSmokeForcesOverviewTransitionLast()
        try testPopoverUsesWeeklyProjectTokenRanking()
        try testSettingsIntervalsUseAuthoritativeBounds()
        try testOverviewRangeIncludesQuotaWeek()
        try testRequestFactoryAndPresentation()
        try await testAppRuntimeUsesWeeklyQuotaRangeForOverview()
        try await testAppRuntimeFallsBackWhenWeeklyQuotaIsUnavailable()
        try await testOverviewRangeSelectionRefreshesAllContent()
        try await testOverviewProjectFailureDoesNotHideUsageAndSessions()
        try testQuotaWindowPresentationUsesActualDuration()
        try testTokenQuantityFormatterUsesChineseMagnitudeUnits()
        try testTokenBreakdownPresentationPreservesInputOutputSemantics()
        try testUsageModelTrendResolverUsesOnlyReconciledDailyFacts()
        try testStatusBarQuotaPresentationUsesOnlyMatchingPeriodUsage()
        try testStatusBarStyleSelectionAndLegacyFallback()
        try testQuotaRemainingLevelUsesGreenYellowRedThresholds()
        try testMainWindowLayoutPrefersFullOverviewWithoutLeavingTheScreen()
        try testWeeklyQuotaUsageRequestUsesExactWindowStart()
        try testOverviewRangeResolutionDrivesEveryContentRequest()
        try testFeatureRequestsStateAndMerge()
        try testSettingsRevisionRequest()
        try testLaunchConfigurationBoundaries()
        try testLaunchConfigurationUsesPersistentProductDefaults()
        try await testFeatureGenerationPreventsStaleOverwrite()
        try await testInvalidationRefreshesActivePage()
        try await testIndexInvalidationRefreshesStatusWhileApplicationIsInactive()
        try await testRepeatedCursorStopsPagination()
        try await testTransientCursorFailureCanRetry()
        try await testQuotaMutationIsSingleFlight()
        try await testLifecycleInvalidationPreservesMutation()
        try await testSettingsConflictPreservesDraft()
        try await testSettingsEditDuringSaveIsPreserved()
        try await testSettingsEditDuringRefreshIsPreserved()
        try await testNormalLifecycleAndShutdown()
        try await testRecoveryAndRestartRequired()
        try await testStaleAndUnavailable()
        try await testContractUnavailableCannotRestartLoop()
        try await testManualOverviewRefreshExposesBusyState()
        try await testShutdownDuringStartup()
        try await testConcurrentStartIsCoalesced()
        try await testCancelledRefreshCannotOverwriteReplacement()
        try await testPendingActiveWaitsForBootstrap()
        try await testSleepDuringStartupDefersOverviewUntilWake()
        try await testHelperExitBecomesStale()
        try await testHelperExitCannotBecomeFeatureCancelled()
        try await testShutdownDeadlineForcesHelperStop()
        print("CodexPulseApp deterministic tests passed")
    }
}
