import AppKit
import Combine
import CodexPulseAppSupport
import CodexPulseCoreClient
import CodexPulseProtocolGenerated
import CodexPulseUpdater
import Foundation
import SwiftUI

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

private struct SessionDetailPlan: Sendable {
    let delay: Duration
    let response: Codexpulse_Core_V1_SessionDetailResponse
    let fails: Bool

    init(
        delay: Duration,
        response: Codexpulse_Core_V1_SessionDetailResponse,
        fails: Bool = false
    ) {
        self.delay = delay
        self.response = response
        self.fails = fails
    }
}

private struct ProjectDetailPlan: Sendable {
    let response: Codexpulse_Core_V1_ProjectDetailResponse
    let fails: Bool
}

private struct SourceDetailPlan: Sendable {
    let response: Codexpulse_Core_V1_SourceDetailResponse
    let fails: Bool
}

private struct JobDetailPlan: Sendable {
    let response: Codexpulse_Core_V1_JobDetailResponse
    let fails: Bool
}

private struct HealthDetailPlan: Sendable {
    let response: Codexpulse_Core_V1_HealthDetailResponse
    let fails: Bool
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

private func updaterSource(_ fileName: String) throws -> String {
    let packageRoot = URL(fileURLWithPath: #filePath)
        .deletingLastPathComponent()
        .deletingLastPathComponent()
        .deletingLastPathComponent()
    let fileURL =
        packageRoot
        .appendingPathComponent("Sources/CodexPulseUpdater", isDirectory: true)
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

private func testReferencePriceFormattingPreservesPrecisionAndUnknown() throws {
    var response = Codexpulse_Core_V1_PricingCatalogCurrentResponse()
    response.currency = "USD"
    response.sourceURL = "https://developers.openai.com/api/docs/pricing"
    var price = Codexpulse_Core_V1_ModelReferencePrice()
    price.modelID = "gpt-5.4-mini"
    price.inputMicros.value = 750_000
    price.inputMicros.unit = "micro_usd"
    price.cachedInputMicros.value = 75_000
    price.cachedInputMicros.unit = "micro_usd"
    price.outputMicros.unknownReason = "unavailable"
    price.outputMicros.unit = "micro_usd"
    response.items = [price]

    try expect(
        ReferencePriceFormatter.rate(price.inputMicros, currency: "USD") == "$0.75",
        "reference input price must retain two-decimal display"
    )
    try expect(
        ReferencePriceFormatter.rate(price.cachedInputMicros, currency: "USD") == "$0.075",
        "reference cached input price must retain three-decimal precision"
    )
    try expect(
        ReferencePriceFormatter.rate(price.outputMicros, currency: "USD") == "暂无",
        "unknown reference price must not become zero"
    )
    var wrongUnit = price.inputMicros
    wrongUnit.unit = "tokens"
    try expect(
        ReferencePriceFormatter.rate(wrongUnit, currency: "USD") == "暂无",
        "non-currency numeric values must fail closed"
    )
    try expect(
        ReferencePriceFormatter.sourceURL(response)?.host == "developers.openai.com",
        "official HTTPS source URL must remain linkable"
    )
    for hiddenModel in [
        "gpt-5",
        "gpt-5-codex",
        "gpt-5.1-codex-max",
        "gpt-5.2-codex",
        "gpt-5.2-codex-max",
        "gpt-5.6",
    ] {
        try expect(
            !ReferencePriceFormatter.shouldDisplay(modelID: hiddenModel),
            "\(hiddenModel) must stay in pricing history without appearing in the reference table"
        )
    }
    for visibleModel in [
        "gpt-5.3-codex",
        "gpt-5.4",
        "gpt-5.6-luna",
        "gpt-5.6-sol",
        "gpt-5.6-terra",
        "gpt-5.10-codex",
    ] {
        try expect(
            ReferencePriceFormatter.shouldDisplay(modelID: visibleModel),
            "\(visibleModel) must remain visible in the reference table"
        )
    }
}

private func testQuotaUsageShowsIndependentReferencePriceCatalogAndBillingBoundary() throws {
    let source = try mainWindowSource("QuotaHealthViews.swift")
    try expect(
        source.contains("pricingSection")
            && source.contains("Table(rows)")
            && source.contains("ReferencePriceFormatter.shouldDisplay(modelID:")
            && source.contains(".tableStyle(.inset(alternatesRowBackgrounds: false))")
            && source.contains(".scrollContentBackground(.hidden)")
            && !source.contains("DisclosureGroup")
            && !source.contains("当前 API 参考价（每 100 万 Token）")
            && source.contains("仅用于 API 等价折算，不是 Codex 订阅账单。")
            && source.contains("长上下文、Batch、Flex、Priority 和区域处理"),
        "quota usage page must expose the independent catalog and its billing boundary"
    )
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
    try expect(
        source.contains("RectangleMark("),
        "current-range activity must render each real time bucket without replacing the Token trend")
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
        source.contains("usageSummary\n                        .fixedSize(horizontal: false, vertical: true)"),
        "overview summary must keep its intrinsic height instead of absorbing project-ranking height")
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
        source.contains("Picker(\"活动指标\"")
            && source.contains("OverviewActivityMetric.allCases"),
        "activity distribution must expose the approved Chinese metric selector")
    try expect(
        source.contains("ViewThatFits(in: .horizontal)")
            && source.contains("activityDistributionCard"),
        "activity distribution and top sessions must adapt between two columns and a vertical stack")
    try expect(
        source.contains("Text(\"时段活动\")")
            && !source.contains("metricColor.gradient")
            && source.contains("activityHeatmapColor(")
            && source.contains("range: .plotDimension(startPadding: 24, endPadding: 32)")
            && source.contains(".fixedSize(horizontal: true, vertical: false)"),
        "activity visuals must use restrained columns and the shared heatmap palette")
    try expect(
        source.contains("maxHeight: fillsProposedHeight ? .infinity : nil")
            && source.contains(".padding(.vertical, 8)"),
        "the five session rows must distribute through the equal-height ranking card")
    try expect(
        source.contains("onSelectProject") && source.contains("onSelectSession"),
        "overview rankings must navigate to their details")
}

private func testToolbarSeparatesCurrentReloadFromGlobalReload() throws {
    let source = try mainWindowSource("RootView.swift")
    try expect(
        source.contains("Label(currentReloadTitle, systemImage: \"arrow.clockwise\")"),
        "toolbar must describe the primary action as reloading the selected page"
    )
    try expect(
        source.contains("Menu {")
            && source.contains("\"重新加载所有页面\",")
            && source.contains("systemImage: \"arrow.triangle.2.circlepath\"")
            && source.contains("Label(\"更多重新加载选项\", systemImage: \"ellipsis.circle\")"),
        "global reload must live in a clearly named secondary menu"
    )
    try expect(
        !source.contains(
            "Label(\"刷新全部页面\", systemImage: \"arrow.triangle.2.circlepath\")"
        ),
        "toolbar must not present two adjacent refresh symbols"
    )
}

private func testWeeklyOverviewTrendUsesDailyAxisAndRangeCopy() throws {
    let weekly = OverviewPresentation(makeResponses())
    try expect(
        weekly.usageRangeLabel == "自 7月14日 · 按天",
        "weekly overview range copy must not expose an hourly boundary"
    )
    try expect(
        weekly.weeklyUsageRangeLabel == "自 7月14日 · 按天",
        "popover weekly range copy must use the same daily unit"
    )

    let base = makeResponses()
    var calendar = Calendar(identifier: .gregorian)
    calendar.timeZone = TimeZone(identifier: base.usage.range.timeZone)!
    let todayResolution = OverviewRequestSet.resolveRange(
        .today,
        quota: base.quota,
        now: Date(timeIntervalSince1970: Double(base.usage.range.endAtMs) / 1_000),
        calendar: calendar
    )
    let today = OverviewPresentation(OverviewResponses(
        usage: base.usage,
        quota: base.quota,
        sessions: base.sessions,
        projects: base.projects,
        health: base.health,
        rangeResolution: todayResolution
    ))
    try expect(
        today.usageRangeLabel == "自 7月14日 09:00",
        "non-week overview range copy must preserve its existing time boundary"
    )
    try expect(
        today.weeklyUsageRangeLabel == "自 7月14日 · 按天",
        "independent popover weekly copy must remain daily when the main range is hourly"
    )

    let source = try mainWindowSource("RootView.swift")
    try expect(
        source.contains("if selectedRange == .quotaWeek")
            && source.contains("AxisMarks(values: .stride(by: .day))")
            && source.contains("trendPointDateText"),
        "weekly overview x-axis must stride and label by date"
    )
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

private func testSessionTrendPresentationAdaptsGranularityAndReportingTimezone() throws {
    let point = Date(timeIntervalSince1970: 1_785_114_000)
    guard let hourly = UsageTrendPresentation(
        granularity: "hour",
        reportingTimeZone: "Asia/Shanghai"
    ) else {
        throw TestFailure.mismatch("hourly trend presentation was rejected")
    }
    try expect(hourly.sectionTitle == "每小时趋势", "same-day sessions must use an hourly title")
    try expect(
        hourly.axisText(for: point) == "09:00",
        "ordinary hourly trend axes must omit the redundant local offset"
    )
    try expect(
        hourly.detailText(for: point) == "2026年7月27日 09:00",
        "ordinary hourly trend details must omit the redundant local offset"
    )

    guard let repeatedHour = UsageTrendPresentation(
        granularity: "hour",
        reportingTimeZone: "America/New_York"
    ) else {
        throw TestFailure.mismatch("DST repeated-hour trend presentation was rejected")
    }
    let firstOneAM = Date(timeIntervalSince1970: 1_793_509_200)
    let secondOneAM = Date(timeIntervalSince1970: 1_793_512_800)
    let ordinaryHour = Date(timeIntervalSince1970: 1_793_516_400)
    try expect(
        repeatedHour.axisText(for: ordinaryHour) == "02:00",
        "ordinary hours in a DST-observing timezone must still omit the offset"
    )
    try expect(
        repeatedHour.axisText(for: firstOneAM) == "01:00 -04:00",
        "first repeated wall-clock hour must retain the daylight offset"
    )
    try expect(
        repeatedHour.axisText(for: secondOneAM) == "01:00 -05:00",
        "second repeated wall-clock hour must retain the standard offset"
    )
    try expect(
        repeatedHour.detailText(for: firstOneAM) == "2026年11月1日 01:00 -04:00",
        "first repeated-hour detail must retain the daylight offset"
    )
    try expect(
        repeatedHour.detailText(for: secondOneAM) == "2026年11月1日 01:00 -05:00",
        "second repeated-hour detail must retain the standard offset"
    )

    guard let daily = UsageTrendPresentation(
        granularity: "day",
        reportingTimeZone: "Asia/Shanghai"
    ) else {
        throw TestFailure.mismatch("daily trend presentation was rejected")
    }
    try expect(daily.sectionTitle == "每日趋势", "cross-day sessions must use a daily title")
    try expect(daily.axisText(for: point) == "7月27日", "daily trend axis must show the local date")
    try expect(
        daily.detailText(for: point) == "2026年7月27日",
        "daily trend detail must omit the hour"
    )
    try expect(
        UsageTrendPresentation(granularity: "week", reportingTimeZone: "Asia/Shanghai") == nil,
        "unknown session trend granularities must fail closed"
    )
    try expect(
        UsageTrendPresentation(granularity: "", reportingTimeZone: "Asia/Shanghai") == nil,
        "missing fallback trend granularity must stay unavailable"
    )
    try expect(
        UsageTrendPresentation(granularity: "hour", reportingTimeZone: "Local") == nil,
        "invalid reporting timezones must fail closed"
    )
}

private func testSessionAndProjectDetailsShareResponsiveThirdWidthSplit() throws {
    let wide = SessionsProjectsSplitLayout.initialDividerPosition(
        availableWidth: 1_200,
        listMinimumWidth: 300,
        dividerThickness: 1
    )
    try expect(
        wide == 799,
        "a wide sessions/projects split must reserve one third for detail"
    )

    let compact = SessionsProjectsSplitLayout.initialDividerPosition(
        availableWidth: 900,
        listMinimumWidth: 320,
        dividerThickness: 1
    )
    try expect(
        compact == 559,
        "a compact sessions/projects split must preserve the readable detail minimum"
    )
    try expect(
        SessionsProjectsSplitLayout.initialDividerPosition(
            availableWidth: 660,
            listMinimumWidth: 320,
            dividerThickness: 1
        ) == nil,
        "an over-constrained split must preserve native narrow-window fallback sizing"
    )

    let source = try mainWindowSource("SessionsProjectsViews.swift")
    try expect(
        source.components(
            separatedBy: "SessionsProjectsSplitView(listMinimumWidth:"
        ).count == 3,
        "sessions and projects must both use the shared split layout"
    )
    try expect(
        source.components(separatedBy: "SessionsProjectsNativeSplitView(").count == 2,
        "sessions and projects must share one native draggable split implementation"
    )
    try expect(
        source.contains("private final class SessionsProjectsNativeSplitView")
            && source.contains("NSSplitViewDelegate")
            && source.contains("private var appliedInitialPosition = false")
            && source.contains("DispatchQueue.main.async")
            && source.contains("setPosition(CGFloat(position), ofDividerAt: 0)")
            && source.contains("constrainMinCoordinate")
            && source.contains("constrainMaxCoordinate")
            && !source.contains("idealWidth: 480"),
        "the shared native split must set a responsive default and retain draggable bounds"
    )
}

@MainActor
private final class NativeContentIdentityRecorder {
    private(set) var views: [NSView] = []

    func record(_ view: NSView) {
        views.append(view)
    }
}

@MainActor
private struct NativeContentIdentityProbe: NSViewRepresentable {
    let recorder: NativeContentIdentityRecorder

    func makeNSView(context: Context) -> NSView {
        let view = NSView()
        recorder.record(view)
        return view
    }

    func updateNSView(_ nsView: NSView, context: Context) {}
}

@MainActor
private func testFeatureRefreshRetainsNativeContentIdentity() async throws {
    let recorder = NativeContentIdentityRecorder()
    let notice = AppNotice(
        code: "content_invalidated",
        messageKey: "app.notice.content_invalidated.index",
        retryable: true
    )
    func stateView(
        _ state: FeatureLoadState<Int>
    ) -> FeatureStateView<Int, NativeContentIdentityProbe> {
        FeatureStateView(
            state: state,
            emptyTitle: "empty",
            emptySystemImage: "tray"
        ) { _ in
            NativeContentIdentityProbe(recorder: recorder)
        }
    }

    let hostingView = NSHostingView(rootView: stateView(.ready(1)))
    hostingView.frame = NSRect(x: 0, y: 0, width: 900, height: 600)
    let window = NSWindow(
        contentRect: hostingView.frame,
        styleMask: [.borderless],
        backing: .buffered,
        defer: false
    )
    window.contentView = hostingView

    for _ in 0..<20 where recorder.views.isEmpty {
        hostingView.layoutSubtreeIfNeeded()
        try await sleepForTest(.milliseconds(1))
    }
    try expect(recorder.views.count == 1, "initial feature content must create one native view")
    let initialView = recorder.views[0]

    let refreshStates: [FeatureLoadState<Int>] = [
        .stale(1, notice: notice),
        .loading(previous: 1),
        .partial(1, notices: [notice]),
        .ready(2),
    ]
    for state in refreshStates {
        hostingView.rootView = stateView(state)
        hostingView.needsLayout = true
        for _ in 0..<3 {
            hostingView.layoutSubtreeIfNeeded()
            try await sleepForTest(.milliseconds(1))
        }
    }

    try expect(
        recorder.views.count == 1 && recorder.views[0] === initialView,
        "content-bearing refresh states must retain one native view identity"
    )
    window.contentView = nil
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

private func testPopoverHeaderMatchesSessionNestLayoutAndNeutralFocus() throws {
    let source = try mainWindowSource("StatusItemController.swift")
    let regularSize = MenuBarPopoverLayout.contentSize(availableScreenHeight: 900)
    let compactSize = MenuBarPopoverLayout.contentSize(availableScreenHeight: 680)
    try expect(
        regularSize.width == 460
            && regularSize.height == 680
            && compactSize.width == 460
            && compactSize.height == 640
            && source.contains("HStack(alignment: .top, spacing: 0)")
            && source.contains("HStack(spacing: 4)"),
        "Popover must gain visible height on regular screens without overflowing compact screens"
    )
    try expect(
        source.contains("GitHubMarkShape().fill(.primary)")
            && source.contains("headerDividerHeight")
            && source.contains("case .idle: \"camera\"")
            && source.contains("Image(systemName: screenshotFeedback.systemImage)")
            && source.contains("Image(systemName: \"arrow.triangle.2.circlepath\")")
            && source.contains("Image(systemName: \"rectangle.split.2x1\")"),
        "Popover header must preserve the SessionNest icon group and ordering"
    )
    try expect(
        source.contains("headerButtonDiameter: CGFloat = 30")
            && source.contains("headerIconSize: CGFloat = 16")
            && source.contains("minimumHitTarget: CGFloat = 44")
            && source.contains(".focusEffectDisabled()")
            && source.contains("isFocused: focusedControl.wrappedValue == target"),
        "Popover header controls must keep 44-point hit targets with neutral visible keyboard focus"
    )
    try expect(
        source.contains("private struct PopoverAccountCapsule")
            && source.contains(".background(Color.orange.opacity(0.14), in: Capsule())")
            && !source.contains("case accountSummary")
            && !source.contains("focusedControl = .accountSummary"),
        "account and plan must be a non-interactive orange capsule without the regressed selection frame"
    )
}

private func testPopoverAccountSummaryShowsSessionNestAccountFieldsOnly() throws {
    let overview = OverviewPresentation(makeResponses(
        accountScope: "account-token-/Users/private/example@example.com"
    ))
    let summary = overview.popoverAccountSummary

    try expect(summary.availability == .available, "account/read facts must be available")
    try expect(summary.planText == "Pro", "account plan type must use SessionNest product copy")
    try expect(summary.emailText == "person@example.com", "account email must remain user-visible")
    try expect(
        !summary.accessibilityLabel.contains("account-token")
            && !summary.accessibilityLabel.contains("/Users/private"),
        "account summary must not fall back to the unrelated quota account scope"
    )
}

private func testPopoverAccountSummaryDistinguishesEmptyAndUnavailableData() throws {
    let empty = OverviewPresentation(makeResponses(includeAccountIdentity: false))
        .popoverAccountSummary
    try expect(empty.availability == .empty, "a successful empty account/read response must remain empty")
    try expect(
        empty.planText == "--" && empty.emailText == "--",
        "an empty account/read response must retain the SessionNest placeholders"
    )

    let unavailable = OverviewPresentation(makeResponses(includeAccountResponse: false))
        .popoverAccountSummary
    try expect(unavailable.availability == .unavailable, "a failed account/read response must stay unavailable")
    try expect(
        unavailable.accessibilityLabel.contains("暂不可用"),
        "an unavailable account/read response must expose a user-facing unavailable state"
    )
}

private func testPopoverScreenshotClipboardTextHidesAccountAndPlan() throws {
    let text = PopoverScreenshotClipboardText.plainText
    try expect(
        text == """
        Codex Pulse Popover 完整截图
        账号与套餐信息已隐藏
        """,
        "Popover screenshot must expose one deterministic privacy notice"
    )
    for canary in ["Pro", "person@example.com", "private-account-marker"] {
        try expect(
            !text.contains(canary),
            "Popover screenshot clipboard text must exclude account canary \(canary)"
        )
    }
}

private func testPopoverProjectActionUsesExactPublicRepositoryURL() throws {
    var openedURL: URL?
    let result = PopoverQuickActions.openProject { url in
        openedURL = url
        return true
    }

    try expect(
        openedURL?.absoluteString == "https://github.com/SisyphusSQ/codex-pulse",
        "project action must open the approved public repository"
    )
    try expect(
        result == .success(title: "已打开项目主页", message: "已交给默认浏览器处理。"),
        "project action must expose a user-visible success result"
    )
}

private func testPopoverProjectActionMakesSystemOpenFailureVisible() throws {
    let result = PopoverQuickActions.openProject { _ in false }

    try expect(
        result == .failure(
            title: "无法打开项目主页",
            message: "系统未能打开 GitHub 项目主页，请稍后再试。"
        ),
        "project action must expose a precise user-visible system open failure"
    )
}

private func testPopoverDoesNotRetainLegacyReleaseLinkUpdater() throws {
    let popoverSource = try mainWindowSource("StatusItemController.swift")
    let mainWindowSource = try mainWindowSource("RootView.swift")

    try expect(
        !popoverSource.contains("model.updateReminder")
            && !popoverSource.contains("PopoverQuickActions.openUpdate")
            && !mainWindowSource.contains("updateReminder"),
        "Sparkle must replace the legacy GitHub release-link reminder"
    )
}

private func testPopoverCopyActionStopsWhenSafeScreenshotIsUnavailable() throws {
    var clipboardWriteCount = 0

    let result = PopoverQuickActions.copyPopoverScreenshot(
        png: nil,
        writeClipboard: { _, _ in
            clipboardWriteCount += 1
            return true
        }
    )

    try expect(clipboardWriteCount == 0, "render failure must not write any clipboard fallback")
    try expect(
        result == .failure(
            title: "无法复制 Popover 完整截图",
            message: "Popover 截图生成失败，未写入剪贴板。"
        ),
        "render failure must be visible and privacy preserving"
    )
}

private func testPopoverCopyActionReportsClipboardFailureWithoutRawFallback() throws {
    let result = PopoverQuickActions.copyPopoverScreenshot(
        png: Data([0x89, 0x50, 0x4E, 0x47]),
        writeClipboard: { _, _ in false }
    )

    try expect(
        result == .failure(
            title: "无法复制 Popover 完整截图",
            message: "剪贴板写入失败，未复制任何数据。"
        ),
        "clipboard failure must not claim success or fall back to raw data"
    )
}

private func testPopoverCopyActionWritesOneSafeImageAndTextPayload() throws {
    let expectedPNG = Data([0x89, 0x50, 0x4E, 0x47])
    var writtenText: String?
    var writtenPNG: Data?

    let result = PopoverQuickActions.copyPopoverScreenshot(
        png: expectedPNG,
        writeClipboard: { text, png in
            writtenText = text
            writtenPNG = png
            return true
        }
    )

    try expect(
        writtenText == PopoverScreenshotClipboardText.plainText,
        "copy action must write the reviewed privacy-safe text"
    )
    try expect(writtenPNG == expectedPNG, "copy action must write the rendered safe screenshot")
    try expect(
        result == .success(
            title: "已复制 Popover 完整截图",
            message: "Popover 全部内容已复制，账号与套餐信息已隐藏。"
        ),
        "copy action must expose a user-visible success result"
    )
}

@MainActor
private func testPopoverPasteboardWriterUsesOneRealItemForTextAndPNG() throws {
    let pasteboard = NSPasteboard(
        name: NSPasteboard.Name(
            "com.sisyphussq.codex-pulse.tests.popover.\(UUID().uuidString)"
        )
    )
    defer { pasteboard.clearContents() }
    let text = PopoverScreenshotClipboardText.plainText
    let png = Data([0x89, 0x50, 0x4E, 0x47, 0x0D, 0x0A, 0x1A, 0x0A])

    try expect(
        PopoverPasteboardPayload.write(text: text, png: png, to: pasteboard),
        "the production pasteboard writer must accept a real macOS pasteboard"
    )
    guard let items = pasteboard.pasteboardItems else {
        throw TestFailure.mismatch("the real pasteboard must expose written items")
    }
    try expect(items.count == 1, "text and PNG must be written as one pasteboard item")
    try expect(
        items[0].types.contains(.string) && items[0].types.contains(.png),
        "the same pasteboard item must advertise both .string and .png"
    )
    try expect(
        items[0].string(forType: .string) == text,
        "the real pasteboard item must retain the reviewed safe text"
    )
    try expect(
        items[0].data(forType: .png) == png,
        "the real pasteboard item must retain the rendered PNG"
    )
}

@MainActor
private final class PopoverCaptureBandView: NSView {
    let colors: [NSColor]

    init(frame: NSRect, colors: [NSColor]) {
        self.colors = colors
        super.init(frame: frame)
    }

    @available(*, unavailable)
    required init?(coder: NSCoder) {
        fatalError("init(coder:) is unavailable")
    }

    override var isFlipped: Bool { true }

    override func draw(_ dirtyRect: NSRect) {
        let bandHeight = bounds.height / CGFloat(colors.count)
        for (index, color) in colors.enumerated() {
            color.setFill()
            NSRect(
                x: bounds.minX,
                y: bounds.minY + CGFloat(index) * bandHeight,
                width: bounds.width,
                height: bandHeight
            ).fill()
        }
    }
}

@MainActor
private func testPopoverFullPageCaptureUsesLiveScrollableViewAndRestoresOffset() throws {
    let root = NSView(frame: NSRect(x: 0, y: 0, width: 120, height: 100))
    let header = PopoverCaptureBandView(
        frame: NSRect(x: 0, y: 80, width: 120, height: 20),
        colors: [.darkGray]
    )
    let footer = PopoverCaptureBandView(
        frame: NSRect(x: 0, y: 0, width: 120, height: 20),
        colors: [.lightGray]
    )
    let scrollView = NSScrollView(frame: NSRect(x: 10, y: 20, width: 100, height: 60))
    scrollView.hasVerticalScroller = false
    scrollView.drawsBackground = false
    let document = PopoverCaptureBandView(
        frame: NSRect(x: 0, y: 0, width: 100, height: 180),
        colors: [.systemRed, .systemGreen, .systemBlue]
    )
    scrollView.documentView = document
    root.addSubview(header)
    root.addSubview(scrollView)
    root.addSubview(footer)
    let window = NSWindow(
        contentRect: root.frame,
        styleMask: [.borderless],
        backing: .buffered,
        defer: false
    )
    window.contentView = root
    window.setFrameOrigin(NSPoint(x: -10_000, y: -10_000))
    window.orderFront(nil)
    defer {
        window.orderOut(nil)
        window.contentView = nil
    }
    root.layoutSubtreeIfNeeded()
    root.displayIfNeeded()

    scrollView.contentView.scroll(to: NSPoint(x: 0, y: 37))
    scrollView.reflectScrolledClipView(scrollView.contentView)
    let originalOrigin = scrollView.contentView.bounds.origin
    guard let directBitmap = root.bitmapImageRepForCachingDisplay(in: root.bounds) else {
        throw TestFailure.mismatch("the live test view must support AppKit bitmap capture")
    }
    root.cacheDisplay(in: root.bounds, to: directBitmap)
    let directColors = stride(
        from: 0,
        to: directBitmap.pixelsHigh,
        by: max(1, directBitmap.pixelsHigh / 40)
    ).compactMap {
        directBitmap.colorAt(
            x: directBitmap.pixelsWide / 2,
            y: $0
        )?.usingColorSpace(.deviceRGB)
    }
    try expect(
        directColors.contains { $0.redComponent > 0 || $0.greenComponent > 0 || $0.blueComponent > 0 },
        "the live test view must draw non-black content before full-page stitching"
    )

    guard let png = PopoverFullPageCapture.renderPNG(
        rootView: root,
        scrollView: scrollView
    ), let bitmap = NSBitmapImageRep(data: png)
    else {
        throw TestFailure.mismatch("live Popover full-page capture must produce PNG data")
    }

    try expect(
        abs(bitmap.size.width - 120) < 1 && abs(bitmap.size.height - 220) < 1,
        "full-page capture must retain the wider Popover frame, one header, "
            + "all 180 points of content, and one footer"
    )
    try expect(
        bitmap.colorAt(x: 0, y: 0)?.alphaComponent == 1,
        "full-page capture must flatten native material transparency onto "
            + "the current system window background"
    )
    try expect(
        abs(scrollView.contentView.bounds.origin.y - originalOrigin.y) < 0.5,
        "full-page capture must restore the user's original scroll offset"
    )

    let sampledColors = stride(from: 0, to: bitmap.pixelsHigh, by: max(1, bitmap.pixelsHigh / 80))
        .compactMap { bitmap.colorAt(x: bitmap.pixelsWide / 2, y: $0)?.usingColorSpace(.deviceRGB) }
    let hasRed = sampledColors.contains { $0.redComponent > 0.7 && $0.greenComponent < 0.45 }
    let hasGreen = sampledColors.contains { $0.greenComponent > 0.7 && $0.blueComponent < 0.45 }
    let hasBlue = sampledColors.contains { $0.blueComponent > 0.7 && $0.redComponent < 0.45 }
    let channelRanges = sampledColors.reduce(
        (red: CGFloat.zero, green: CGFloat.zero, blue: CGFloat.zero)
    ) { ranges, color in
        (
            max(ranges.red, color.redComponent),
            max(ranges.green, color.greenComponent),
            max(ranges.blue, color.blueComponent)
        )
    }
    try expect(
        hasRed && hasGreen && hasBlue,
        "full-page capture must include the top, middle, and bottom of the real document view "
            + "(matched red=\(hasRed) green=\(hasGreen) blue=\(hasBlue); "
            + "max=\(channelRanges))"
    )
    let pixelScale = CGFloat(bitmap.pixelsHigh) / bitmap.size.height
    func color(atPointY y: CGFloat) -> NSColor? {
        bitmap.colorAt(
            x: bitmap.pixelsWide / 2,
            y: min(bitmap.pixelsHigh - 1, max(0, Int(y * pixelScale)))
        )?.usingColorSpace(.deviceRGB)
    }
    let topBand = color(atPointY: 50)
    let middleBand = color(atPointY: 110)
    let bottomBand = color(atPointY: 170)
    try expect(
        topBand.map { $0.redComponent > 0.7 && $0.greenComponent < 0.45 } == true
            && middleBand.map { $0.greenComponent > 0.7 && $0.blueComponent < 0.45 } == true
            && bottomBand.map { $0.blueComponent > 0.7 && $0.redComponent < 0.45 } == true,
        "full-page capture must preserve the document's visual top-to-bottom order "
            + "(top=\(String(describing: topBand)), "
            + "middle=\(String(describing: middleBand)), "
            + "bottom=\(String(describing: bottomBand)))"
    )
}

private func testPopoverScreenshotUsesLiveViewAndRedactsAccountCapsule() throws {
    let source = try mainWindowSource("StatusItemController.swift")
    try expect(
        source.contains("PopoverFullPageCapture.renderPNG(")
            && source.contains("resolveScrollView(in: rootView)")
            && source.contains("PopoverCaptureDocumentProbe(source: captureSource)"),
        "Popover screenshot must capture the currently presented native scroll view"
    )
    try expect(
        source.contains(".opacity(isPrivacyHidden ? 0 : 1)")
            && source.contains("Image(systemName: \"eye.slash\")")
            && source.contains("截图中账号与套餐信息已隐藏")
            && source.contains("waitUntilPrivacyRendered(true)")
            && source.contains("PopoverPrivacyRenderProbe"),
        "Popover screenshot must wait for the live account and plan capsule to be hidden"
    )
    try expect(
        !source.contains("ImageRenderer(content: PopoverPrivacySnapshotView")
            && !source.contains("private struct PopoverPrivacySnapshotView"),
        "Popover screenshot must not reconstruct a separate summary card"
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

private enum FakeLoginItemServiceError: Error {
    case privatePlatformDetails
}

@MainActor
private final class FakeLoginItemService: LoginItemServiceManaging {
    var status: LoginItemRegistrationStatus
    var statusAfterRegister: LoginItemRegistrationStatus = .enabled
    var statusAfterUnregister: LoginItemRegistrationStatus = .notRegistered
    var registerFails = false
    var unregisterFails = false
    private(set) var statusReadCount = 0
    private(set) var registerCount = 0
    private(set) var unregisterCount = 0
    private(set) var openSystemSettingsCount = 0

    init(status: LoginItemRegistrationStatus) {
        self.status = status
    }

    func readStatus() -> LoginItemRegistrationStatus {
        statusReadCount += 1
        return status
    }

    func register() throws {
        registerCount += 1
        status = statusAfterRegister
        if registerFails {
            throw FakeLoginItemServiceError.privatePlatformDetails
        }
    }

    func unregister() throws {
        unregisterCount += 1
        status = statusAfterUnregister
        if unregisterFails {
            throw FakeLoginItemServiceError.privatePlatformDetails
        }
    }

    func openSystemSettings() {
        openSystemSettingsCount += 1
    }
}

@MainActor
private func testLoginItemDefaultsToSystemStatusWithoutRegistration() throws {
    let service = FakeLoginItemService(status: .notRegistered)
    let settings = LoginItemSettingsModel(service: service)

    try expect(!settings.isRequested, "not-registered system status must render the toggle off")
    try expect(service.statusReadCount == 1, "initialization must read the real system status once")
    try expect(service.registerCount == 0, "initialization must never silently register the app")
    try expect(service.unregisterCount == 0, "initialization must never mutate the login item")
}

@MainActor
private func testLoginItemRegistrationAndExternalStatusDrift() throws {
    let service = FakeLoginItemService(status: .notRegistered)
    let settings = LoginItemSettingsModel(service: service)

    settings.setRequested(true)
    try expect(service.registerCount == 1, "turning the toggle on must register exactly once")
    try expect(settings.status == .enabled, "successful registration must use enabled readback")
    try expect(settings.isRequested, "enabled system status must render the toggle on")
    try expect(settings.actionFailure == nil, "successful registration must clear action failures")

    settings.setRequested(false)
    try expect(service.unregisterCount == 1, "turning the toggle off must unregister exactly once")
    try expect(
        settings.status == .notRegistered && !settings.isRequested,
        "successful unregistration must use not-registered readback"
    )

    service.status = .enabled
    settings.refreshStatus()
    try expect(
        settings.status == .enabled && settings.isRequested,
        "an external registration must replace the displayed state with current system status"
    )

    service.status = .notFound
    settings.refreshStatus()
    try expect(
        settings.status == .notFound && !settings.isRequested,
        "not-found system status must stay distinct from successful registration"
    )
}

@MainActor
private func testLoginItemRequiresApprovalUsesRegisteredIntent() throws {
    let service = FakeLoginItemService(status: .notRegistered)
    service.statusAfterRegister = .requiresApproval
    service.registerFails = true
    let settings = LoginItemSettingsModel(service: service)

    settings.setRequested(true)
    try expect(
        settings.status == .requiresApproval && settings.isRequested,
        "requires-approval readback must keep the registered intent visible"
    )
    try expect(
        settings.actionFailure == nil,
        "requires-approval system status must explain the outcome instead of showing a false failure"
    )

    settings.openSystemSettings()
    try expect(
        service.openSystemSettingsCount == 1,
        "approval guidance must use the injected System Settings opener"
    )

    settings.setRequested(false)
    try expect(
        service.unregisterCount == 1
            && settings.status == .notRegistered
            && !settings.isRequested,
        "requires-approval state must remain cancellable through system unregistration"
    )
}

@MainActor
private func testLoginItemFailuresRestoreAuthoritativeState() throws {
    let registrationService = FakeLoginItemService(status: .notRegistered)
    registrationService.statusAfterRegister = .notRegistered
    registrationService.registerFails = true
    let registrationSettings = LoginItemSettingsModel(service: registrationService)

    registrationSettings.setRequested(true)
    try expect(
        registrationSettings.status == .notRegistered && !registrationSettings.isRequested,
        "failed registration must restore the toggle from system readback"
    )
    try expect(
        registrationSettings.actionFailure == .registrationFailed,
        "failed registration must expose only the stable registration failure"
    )

    let unregistrationService = FakeLoginItemService(status: .enabled)
    unregistrationService.statusAfterUnregister = .enabled
    unregistrationService.unregisterFails = true
    let unregistrationSettings = LoginItemSettingsModel(service: unregistrationService)

    unregistrationSettings.setRequested(false)
    try expect(
        unregistrationSettings.status == .enabled && unregistrationSettings.isRequested,
        "failed unregistration must restore the toggle from system readback"
    )
    try expect(
        unregistrationSettings.actionFailure == .unregistrationFailed,
        "failed unregistration must expose only the stable unregistration failure"
    )

    unregistrationService.status = .notRegistered
    unregistrationSettings.refreshStatus()
    try expect(
        unregistrationSettings.actionFailure == nil,
        "a later external status change must clear the obsolete action failure"
    )
}

private func testLoginItemServiceManagementWiringAndCopy() throws {
    let adapterSource = try mainWindowSource("MainAppLoginItemService.swift")
    try expect(
        adapterSource.contains("SMAppService = .mainApp")
            && adapterSource.contains("try service.register()")
            && adapterSource.contains("try service.unregister()"),
        "the platform adapter must use SMAppService.mainApp registration APIs"
    )
    for status in [".notRegistered", ".enabled", ".requiresApproval", ".notFound"] {
        try expect(
            adapterSource.contains(status),
            "the platform adapter must map every SMAppService status: \(status)"
        )
    }

    let delegateSource = try mainWindowSource("AppDelegate.swift")
    try expect(
        delegateSource.contains(
            "func applicationDidBecomeActive(_ notification: Notification) {\n"
                + "        guard !configuration.smokeMode else { return }\n"
                + "        loginItemSettings.refreshStatus()"),
        "application activation must refresh the login-item system status"
    )

    let settingsSource = try mainWindowSource("SourcesJobsSettingsViews.swift")
    try expect(
        settingsSource.contains(".onAppear { loginItemSettings.refreshStatus() }"),
        "re-entering Settings must refresh the login-item system status"
    )
    try expect(
        settingsSource.contains("settings.login-at-launch")
            && settingsSource.contains("等待系统批准")
            && settingsSource.contains("登录项不可用")
            && settingsSource.contains("打开系统登录项设置")
            && settingsSource.contains("无需点击“保存更改”")
            && settingsSource.contains(
                "loginItemSettings.isChanging || loginItemSettings.status == .notFound"
            ),
        "Settings must expose stable controls and copy for every actionable login-item state"
    )
    try expect(
        !settingsSource.contains("localizedDescription")
            && !adapterSource.contains("localizedDescription"),
        "login-item UI must not expose raw platform error text"
    )
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
        source.contains(
            "func verifyNativeSurfacesForSmoke(\n        requireSummary: Bool\n"
                + "    ) async -> (passed: Bool, summary: String) {\n        updateStatusBarView()"
        )
            && source.contains("statusBarView.superview === button")
            && source.contains("statusBarView.preferredWidth > 0")
            && source.contains("if requireSummary && !statusBarView.hasSummary {"),
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
        delegateSource.contains("let surfaces = await nativeSurfaceSmokeSummary(")
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

private func testApplicationMenuRegistersNativeCommands() throws {
    let source = try mainWindowSource("AppDelegate.swift")
    try expect(
        source.contains(
            "NSMenuItem(\n            title: \"关于 Codex Pulse\",\n            action: #selector(showAboutPanel(_:))"
        ),
        "application menu must register the native About command"
    )
    try expect(
        source.contains(
            "NSMenuItem(\n            title: \"设置…\",\n            action: #selector(showSettings(_:)),\n            keyEquivalent: \",\""
        ),
        "application menu must register Settings with the standard Command-comma shortcut"
    )
    try expect(
        source.contains(
            "NSMenuItem(\n            title: \"检查更新…\",\n            action: #selector(checkForUpdates(_:))"
        ),
        "application menu must register the Check for Updates command"
    )
    try expect(
        source.contains("NSApp.orderFrontStandardAboutPanel(sender)"),
        "About must use the native standard About panel"
    )
}

private func testApplicationMenuSettingsShowsTheExistingSettingsPage() throws {
    let source = try mainWindowSource("AppDelegate.swift")
    guard let settingsAction = source.range(of: "@objc private func showSettings"),
          let updateAction = source.range(
              of: "@objc private func checkForUpdates",
              range: settingsAction.upperBound..<source.endIndex
          )
    else {
        throw TestFailure.mismatch("application menu settings action was unavailable")
    }
    let settingsSource = source[settingsAction.lowerBound..<updateAction.lowerBound]
    try expect(
        settingsSource.contains("model.navigate(to: .settings)")
            && settingsSource.contains("showMainWindow(sender)"),
        "Settings must navigate through AppModel and reveal the existing main window"
    )
}

private func testApplicationMenuUpdateUsesSparkleAndObservedHelperPolicy() throws {
    let source = try mainWindowSource("AppDelegate.swift")
    try expect(
        source.contains("import CodexPulseUpdater")
            && source.contains("private var updater: SparkleAppUpdater?")
            && source.contains("updater.checkForUpdates()"),
        "Check for Updates must use the embedded Sparkle updater"
    )
    try expect(
        source.contains("model.$updatePolicy")
            && source.contains("self?.updater?.apply(policy)"),
        "Sparkle must observe the Helper-owned update policy"
    )
    try expect(
        !source.contains("releasesURLString")
            && !source.contains("openExternalURL(releasesURL)"),
        "the legacy browser-only update path must be removed"
    )
}

private func testSparkleStartsWithoutRacingTheHelperOwnedPolicy() throws {
    let source = try updaterSource("SparkleAppUpdater.swift")
    guard let disableChecks = source.range(
        of: "engine.automaticallyChecksForUpdates = policy.automaticallyChecks"
    ),
          let startUpdater = source.range(
              of: "standardController.startUpdater()"
          )
    else {
        throw TestFailure.mismatch(
            "Sparkle startup policy ordering was unavailable"
        )
    }
    try expect(
        disableChecks.lowerBound < startUpdater.lowerBound,
        "Sparkle must stay offline until the Helper-owned policy is applied"
    )
}

private func testUpdateInstallationBlocksTerminationAfterUncleanHelperShutdown() throws {
    let source = try mainWindowSource("AppDelegate.swift")
    try expect(
        source.contains("updater?.installationInProgress == true")
            && source.contains("await model.prepareForUpdateInstallation()")
            && source.contains("case .blocked(let outcome):")
            && source.contains("sender.reply(toApplicationShouldTerminate: false)"),
        "Sparkle installation must not replace the bundle after a forced or uncertain Helper stop"
    )
    try expect(
        !source.contains("updater?.resetBlockedInstallation()")
            && source.contains("model.start()")
            && source.contains("presentUpdateInstallationBlocked(outcome)"),
        "a blocked install must retain Sparkle's scheduled intent, restart the model, and explain the failure"
    )
}

private func testApplicationMenuIsSkippedForSmokeLaunches() throws {
    let source = try mainWindowSource("AppDelegate.swift")
    guard let normalLaunch = source.range(
        of: "        } else {\n            NSApp.setActivationPolicy(.regular)"
    ),
          let modelStart = source.range(
              of: "        model.start()",
              range: normalLaunch.upperBound..<source.endIndex
          )
    else {
        throw TestFailure.mismatch("application launch branches were unavailable")
    }
    let smokeLaunchSource = source[..<normalLaunch.lowerBound]
    let normalLaunchSource = source[normalLaunch.lowerBound..<modelStart.lowerBound]
    try expect(
        !smokeLaunchSource.contains("installApplicationMenu()")
            && normalLaunchSource.contains("installApplicationMenu()"),
        "application menu must install only on the normal App launch path"
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
        try await sleepForTest(.milliseconds(10))
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

private actor LoadingStateBarrier {
    private var isBlockingLoading = false
    private var loadingReleased = false
    private var loadingEntryCount = 0
    private var loadingWaiters: [CheckedContinuation<Void, Never>] = []

    func accept(_ state: CoreConnectionState) async {
        guard case .loadingOverview = state else { return }
        loadingEntryCount += 1
        isBlockingLoading = true
        guard !loadingReleased else { return }
        await withCheckedContinuation { continuation in
            loadingWaiters.append(continuation)
        }
    }

    func isBlocking() -> Bool {
        isBlockingLoading && !loadingReleased
    }

    func entryCount() -> Int {
        loadingEntryCount
    }

    func release() {
        loadingReleased = true
        let waiters = loadingWaiters
        loadingWaiters.removeAll()
        waiters.forEach { $0.resume() }
    }
}

private actor CompletionFlag {
    private var completed = false

    func markCompleted() {
        completed = true
    }

    func isCompleted() -> Bool {
        completed
    }
}

private actor ForegroundSurfaceRecorder {
    private var overviewTimestamps: [Int64] = []
    private var unavailableCount = 0

    func append(_ state: AppViewState) {
        switch state {
        case .overview(let overview), .partial(let overview), .stale(let overview, _):
            overviewTimestamps.append(overview.evaluatedAtMS)
        case .unavailable:
            unavailableCount += 1
        default:
            break
        }
    }

    func observedUnavailable() -> Bool {
        unavailableCount > 0
    }

    func unavailableCountValue() -> Int {
        unavailableCount
    }

    func latestOverviewTimestamp() -> Int64? {
        overviewTimestamps.last
    }
}

private func sleepForTest(_ duration: Duration) async throws {
    let components = duration.components
    let seconds = max(
        0,
        Double(components.seconds) + Double(components.attoseconds) / 1e18
    )
    try await Task.sleep(nanoseconds: UInt64(seconds * 1e9))
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
        if startDelay != .zero { try await sleepForTest(startDelay) }
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
    private var failTokenActivity = false
    private var failAccount = false
    private var handshakeFailure = false
    private var handshakeError: CoreClientError?
    private var overviewDelay: Duration = .zero
    private var accountDelay: Duration = .zero
    private var handshakeDelay: Duration = .zero
    private var bootstrapDelay: Duration = .zero
    private var shutdownDelay: Duration = .zero
    private var calls: [String] = []
    private var usageRequests: [Codexpulse_Core_V1_UsageCostRequest] = []
    private var tokenActivityRequests: [Codexpulse_Core_V1_UsageCostRequest] = []
    private var sessionRequests: [Codexpulse_Core_V1_ListSessionsRequest] = []
    private var projectRequests: [Codexpulse_Core_V1_ListProjectsRequest] = []
    private var featureSessionPlans: [SessionPagePlan] = []
    private var featureSessionDetailPlans: [SessionDetailPlan] = []
    private var featureProjectDetailPlans: [ProjectDetailPlan] = []
    private var featureSourceDetailPlans: [SourceDetailPlan] = []
    private var featureJobDetailPlans: [JobDetailPlan] = []
    private var featureHealthDetailPlans: [HealthDetailPlan] = []
    private var invalidationDomain: String?
    private var invalidationDelay: Duration = .zero
    private var quotaRefreshDelay: Duration = .zero
    private var settingsResponses: [Codexpulse_Core_V1_SettingsResponse] = []
    private var pricingCatalogResponse = Codexpulse_Core_V1_PricingCatalogCurrentResponse()
    private var pricingCatalogCalls = 0
    private var settingsUpdateFailure = false
    private var settingsReadDelay: Duration = .zero
    private var settingsUpdateDelay: Duration = .zero
    private var overviewBarrierClosed = false
    private var overviewBarrierWaiters: [CheckedContinuation<Void, Never>] = []
    private var invalidationHandler:
        (@Sendable (Codexpulse_Core_V1_QueryInvalidationEvent) async throws -> Void)?
    private var nextInvalidationSequence: UInt64 = 1
    private var activeUsageCalls = 0
    private var completedUsageCalls = 0
    private var maximumConcurrentUsageCalls = 0
    private var invalidationStreamCalls = 0
    private var holdInitialStreamReadiness = false
    private var initialStreamReadinessReleased = false
    private var initialStreamReadinessWaiters: [CheckedContinuation<Void, Never>] = []
    private var disconnectInitialStreamAfterReady = false
    private var initialStreamDisconnectReleased = false
    private var initialStreamDisconnectWaiters: [CheckedContinuation<Void, Never>] = []
    private var holdReconnectStreamReadiness = false
    private var reconnectStreamReadinessReleased = false
    private var reconnectStreamReadinessWaiters: [CheckedContinuation<Void, Never>] = []
    private var reconnectReadySignalCount = 1
    private var blockSystemWillSleep = false
    private var systemWillSleepReleased = false
    private var systemWillSleepWaiters: [CheckedContinuation<Void, Never>] = []
    private var readyInvalidationStreamCalls = 0
    private var completedAccountCalls = 0

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
    func setTokenActivityFailure(_ value: Bool) { failTokenActivity = value }
    func setAccountFailure(_ value: Bool) { failAccount = value }
    func setHandshakeFailure(_ value: Bool) { handshakeFailure = value }
    func setHandshakeError(_ value: CoreClientError?) { handshakeError = value }
    func setOverviewDelay(_ value: Duration) { overviewDelay = value }
    func setAccountDelay(_ value: Duration) { accountDelay = value }
    func setHandshakeDelay(_ value: Duration) { handshakeDelay = value }
    func setBootstrapDelay(_ value: Duration) { bootstrapDelay = value }
    func setShutdownDelay(_ value: Duration) { shutdownDelay = value }
    func setFeatureSessionPlans(_ plans: [SessionPagePlan]) { featureSessionPlans = plans }
    func setFeatureSessionDetailPlans(_ plans: [SessionDetailPlan]) {
        featureSessionDetailPlans = plans
    }
    func setFeatureProjectDetailPlans(_ plans: [ProjectDetailPlan]) {
        featureProjectDetailPlans = plans
    }
    func setFeatureSourceDetailPlans(_ plans: [SourceDetailPlan]) {
        featureSourceDetailPlans = plans
    }
    func setFeatureJobDetailPlans(_ plans: [JobDetailPlan]) {
        featureJobDetailPlans = plans
    }
    func setFeatureHealthDetailPlans(_ plans: [HealthDetailPlan]) {
        featureHealthDetailPlans = plans
    }
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
    func setPricingCatalogResponse(_ value: Codexpulse_Core_V1_PricingCatalogCurrentResponse) {
        pricingCatalogResponse = value
    }
    func prepareUnavailableOverviewRecovery() {
        failOverview = true
        overviewBarrierClosed = true
    }
    func prepareOverviewBarrier() {
        overviewBarrierClosed = true
    }
    func prepareInitialStreamReadinessBarrier() {
        holdInitialStreamReadiness = true
        initialStreamReadinessReleased = false
    }
    func releaseInitialStreamReadinessBarrier() {
        initialStreamReadinessReleased = true
        let waiters = initialStreamReadinessWaiters
        initialStreamReadinessWaiters.removeAll()
        waiters.forEach { $0.resume() }
    }
    func prepareReconnectWithoutInvalidationReplay() {
        disconnectInitialStreamAfterReady = true
        initialStreamDisconnectReleased = false
        holdReconnectStreamReadiness = true
        reconnectStreamReadinessReleased = false
    }
    func disconnectInitialInvalidationStream() {
        initialStreamDisconnectReleased = true
        let waiters = initialStreamDisconnectWaiters
        initialStreamDisconnectWaiters.removeAll()
        waiters.forEach { $0.resume() }
    }
    func releaseReconnectStreamReadiness() {
        reconnectStreamReadinessReleased = true
        let waiters = reconnectStreamReadinessWaiters
        reconnectStreamReadinessWaiters.removeAll()
        waiters.forEach { $0.resume() }
    }
    func setReconnectReadySignalCount(_ count: Int) {
        reconnectReadySignalCount = max(1, count)
    }
    func prepareSystemWillSleepBarrier() {
        blockSystemWillSleep = true
        systemWillSleepReleased = false
    }
    func releaseSystemWillSleepBarrier() {
        systemWillSleepReleased = true
        let waiters = systemWillSleepWaiters
        systemWillSleepWaiters.removeAll()
        waiters.forEach { $0.resume() }
    }
    func publishOverviewInvalidations(
        count: Int,
        recovered: Bool
    ) async {
        if recovered { failOverview = false }
        guard let invalidationHandler else { return }
        for _ in 0..<count {
            var event = Codexpulse_Core_V1_QueryInvalidationEvent()
            event.version = CodexPulseTransportContract.invalidationVersion
            event.domain = "index"
            event.sequence = nextInvalidationSequence
            nextInvalidationSequence += 1
            try? await invalidationHandler(event)
        }
    }
    func releaseOverviewBarrier() {
        overviewBarrierClosed = false
        let waiters = overviewBarrierWaiters
        overviewBarrierWaiters.removeAll()
        waiters.forEach { $0.resume() }
    }
    func releaseNextOverviewBarrierWaiter() {
        guard !overviewBarrierWaiters.isEmpty else { return }
        overviewBarrierWaiters.removeFirst().resume()
    }
    func overviewBarrierWaiterCount() -> Int { overviewBarrierWaiters.count }
    func overviewRecoveryStats() -> (
        usageCalls: Int,
        completedUsageCalls: Int,
        maximumConcurrentUsageCalls: Int
    ) {
        (usageRequests.count, completedUsageCalls, maximumConcurrentUsageCalls)
    }
    func invalidationStreamCallCount() -> Int { invalidationStreamCalls }
    func initialStreamReadinessWaiterCount() -> Int { initialStreamReadinessWaiters.count }
    func initialStreamDisconnectWaiterCount() -> Int { initialStreamDisconnectWaiters.count }
    func reconnectStreamReadinessWaiterCount() -> Int { reconnectStreamReadinessWaiters.count }
    func systemWillSleepWaiterCount() -> Int { systemWillSleepWaiters.count }
    func readyInvalidationStreamCallCount() -> Int { readyInvalidationStreamCalls }
    func overviewCoreCallCount() -> Int {
        calls.filter {
            $0 == "quota"
                || $0 == "usage"
                || $0 == "sessions"
                || $0 == "projects"
                || $0 == "health"
        }.count
    }
    func overviewBatchCallCount() -> Int {
        calls.filter { $0 == "quota" }.count
    }
    func recordedCalls() -> [String] { calls }
    func recordedUsageRequests() -> [Codexpulse_Core_V1_UsageCostRequest] { usageRequests }
    func recordedTokenActivityRequests() -> [Codexpulse_Core_V1_UsageCostRequest] {
        tokenActivityRequests
    }
    func recordedSessionRequests() -> [Codexpulse_Core_V1_ListSessionsRequest] { sessionRequests }
    func recordedProjectRequests() -> [Codexpulse_Core_V1_ListProjectsRequest] { projectRequests }
    func recordedCompletedAccountCalls() -> Int { completedAccountCalls }
    func recordedPricingCatalogCalls() -> Int { pricingCatalogCalls }

    func handshake(
        clientName: String,
        clientVersion: String,
        retryPolicy: ReadRetryPolicy
    ) async throws -> Codexpulse_Core_V1_HandshakeResponse {
        calls.append("handshake")
        if handshakeDelay != .zero { try await sleepForTest(handshakeDelay) }
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
        if bootstrapDelay != .zero { try await sleepForTest(bootstrapDelay) }
        return bootstrapResponse
    }

    func usageCost(
        _ request: Codexpulse_Core_V1_UsageCostRequest,
        retryPolicy: ReadRetryPolicy
    ) async throws -> Codexpulse_Core_V1_UsageCostResponse {
        if request.hasExactRange,
           request.exactRange.endAtMs - request.exactRange.startAtMs > 300 * 86_400_000 {
            calls.append("token-activity")
            tokenActivityRequests.append(request)
            let shouldFail = failOverview || failTokenActivity
            await waitForOverviewBarrier()
            if overviewDelay != .zero { try await sleepForTest(overviewDelay) }
            if shouldFail { throw FakeFailure.unavailable }
            return responses.tokenActivityUsage
        }
        calls.append("usage")
        usageRequests.append(request)
        let shouldFail = failOverview
        activeUsageCalls += 1
        maximumConcurrentUsageCalls = max(maximumConcurrentUsageCalls, activeUsageCalls)
        defer {
            activeUsageCalls -= 1
            completedUsageCalls += 1
        }
        await waitForOverviewBarrier()
        if overviewDelay != .zero { try await sleepForTest(overviewDelay) }
        if shouldFail { throw FakeFailure.unavailable }
        return responses.usage
    }

    func pricingCatalogCurrent(
        retryPolicy: ReadRetryPolicy
    ) async throws -> Codexpulse_Core_V1_PricingCatalogCurrentResponse {
        pricingCatalogCalls += 1
        return pricingCatalogResponse
    }

    func quotaCurrent(
        _ request: Codexpulse_Core_V1_QuotaCurrentRequest,
        retryPolicy: ReadRetryPolicy
    ) async throws -> Codexpulse_Core_V1_QuotaCurrentResponse {
        calls.append("quota")
        let shouldFail = failOverview
        await waitForOverviewBarrier()
        if overviewDelay != .zero { try await sleepForTest(overviewDelay) }
        if shouldFail { throw FakeFailure.unavailable }
        return responses.quota
    }

    func accountSnapshot(
        retryPolicy: ReadRetryPolicy
    ) async throws -> Codexpulse_Core_V1_AccountSnapshotResponse {
        calls.append("account")
        defer { completedAccountCalls += 1 }
        if accountDelay != .zero { try await sleepForTest(accountDelay) }
        if failAccount { throw FakeFailure.unavailable }
        return responses.account ?? .init()
    }

    func listSessions(
        _ request: Codexpulse_Core_V1_ListSessionsRequest,
        retryPolicy: ReadRetryPolicy
    ) async throws -> Codexpulse_Core_V1_SessionListResponse {
        calls.append("sessions")
        sessionRequests.append(request)
        let isOverviewRequest = request.query.page.limit == 5
            && request.query.sort.first?.field == "totalTokens"
            && request.query.hasExactTimeRange
        if !isOverviewRequest, !featureSessionPlans.isEmpty {
            let plan = featureSessionPlans.removeFirst()
            if plan.delay != .zero { try await sleepForTest(plan.delay) }
            if plan.fails { throw FakeFailure.unavailable }
            return plan.response
        }
        let shouldFail = failOverview
        await waitForOverviewBarrier()
        if overviewDelay != .zero { try await sleepForTest(overviewDelay) }
        if shouldFail { throw FakeFailure.unavailable }
        return responses.sessions
    }

    func sessionDetail(
        _ request: Codexpulse_Core_V1_SessionDetailRequest,
        retryPolicy: ReadRetryPolicy
    ) async throws -> Codexpulse_Core_V1_SessionDetailResponse {
        calls.append("session-detail:\(request.sessionID)")
        guard !featureSessionDetailPlans.isEmpty else { throw FakeFailure.unavailable }
        let plan = featureSessionDetailPlans.removeFirst()
        if plan.delay != .zero { try await sleepForTest(plan.delay) }
        if plan.fails { throw FakeFailure.unavailable }
        return plan.response
    }

    func listProjects(
        _ request: Codexpulse_Core_V1_ListProjectsRequest,
        retryPolicy: ReadRetryPolicy
    ) async throws -> Codexpulse_Core_V1_ProjectListResponse {
        calls.append("projects")
        projectRequests.append(request)
        let shouldFail = failOverview
        await waitForOverviewBarrier()
        if overviewDelay != .zero { try await sleepForTest(overviewDelay) }
        if request.query.page.limit == 5, failOverviewProjects { throw FakeFailure.unavailable }
        if shouldFail { throw FakeFailure.unavailable }
        if request.query.filters.contains(where: { $0.field == "confidence" }) {
            return responses.weeklyProjects
        }
        var response = Codexpulse_Core_V1_ProjectListResponse()
        response.meta = completeMeta()
        return response
    }

    func projectDetail(
        _ request: Codexpulse_Core_V1_ProjectDetailRequest,
        retryPolicy: ReadRetryPolicy
    ) async throws -> Codexpulse_Core_V1_ProjectDetailResponse {
        calls.append("project-detail:\(request.dimensionKey)")
        guard !featureProjectDetailPlans.isEmpty else { throw FakeFailure.unavailable }
        let plan = featureProjectDetailPlans.removeFirst()
        if plan.fails { throw FakeFailure.unavailable }
        return plan.response
    }

    func source(
        _ request: Codexpulse_Core_V1_SourceRequest,
        retryPolicy: ReadRetryPolicy
    ) async throws -> Codexpulse_Core_V1_SourceDetailResponse {
        calls.append("source-detail:\(request.sourceKey)")
        guard !featureSourceDetailPlans.isEmpty else { throw FakeFailure.unavailable }
        let plan = featureSourceDetailPlans.removeFirst()
        if plan.fails { throw FakeFailure.unavailable }
        return plan.response
    }

    func job(
        _ request: Codexpulse_Core_V1_JobRequest,
        retryPolicy: ReadRetryPolicy
    ) async throws -> Codexpulse_Core_V1_JobDetailResponse {
        calls.append("job-detail:\(request.jobID)")
        guard !featureJobDetailPlans.isEmpty else { throw FakeFailure.unavailable }
        let plan = featureJobDetailPlans.removeFirst()
        if plan.fails { throw FakeFailure.unavailable }
        return plan.response
    }

    func health(
        _ request: Codexpulse_Core_V1_HealthRequest,
        retryPolicy: ReadRetryPolicy
    ) async throws -> Codexpulse_Core_V1_HealthDetailResponse {
        calls.append("health-detail:\(request.eventID)")
        guard !featureHealthDetailPlans.isEmpty else { throw FakeFailure.unavailable }
        let plan = featureHealthDetailPlans.removeFirst()
        if plan.fails { throw FakeFailure.unavailable }
        return plan.response
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
        if quotaRefreshDelay != .zero { try await sleepForTest(quotaRefreshDelay) }
        var receipt = Codexpulse_Core_V1_QuotaRefreshReceipt()
        receipt.source = request.source
        receipt.reason = "accepted"
        return receipt
    }

    func settings(
        retryPolicy: ReadRetryPolicy
    ) async throws -> Codexpulse_Core_V1_SettingsResponse {
        calls.append("settings")
        if settingsReadDelay != .zero { try await sleepForTest(settingsReadDelay) }
        guard !settingsResponses.isEmpty else { throw FakeFailure.unavailable }
        if settingsResponses.count == 1 { return settingsResponses[0] }
        return settingsResponses.removeFirst()
    }

    func updateSettings(
        _ request: Codexpulse_Core_V1_UpdateSettingsRequest
    ) async throws -> Codexpulse_Core_V1_SettingsUpdateReceipt {
        calls.append("settings_update:\(request.expectedRevision)")
        if settingsUpdateDelay != .zero { try await sleepForTest(settingsUpdateDelay) }
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
        let shouldFail = failOverview
        await waitForOverviewBarrier()
        if overviewDelay != .zero { try await sleepForTest(overviewDelay) }
        if shouldFail { throw FakeFailure.unavailable }
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
        if event == .systemWillSleep, blockSystemWillSleep, !systemWillSleepReleased {
            await withCheckedContinuation { continuation in
                systemWillSleepWaiters.append(continuation)
            }
        }
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
        invalidationStreamCalls += 1
        let streamCall = invalidationStreamCalls
        invalidationHandler = onEvent
        defer { invalidationHandler = nil }
        if streamCall == 1, holdInitialStreamReadiness, !initialStreamReadinessReleased {
            await withCheckedContinuation { continuation in
                initialStreamReadinessWaiters.append(continuation)
            }
            try Task.checkCancellation()
        }
        if streamCall == 2, holdReconnectStreamReadiness, !reconnectStreamReadinessReleased {
            await withCheckedContinuation { continuation in
                reconnectStreamReadinessWaiters.append(continuation)
            }
            try Task.checkCancellation()
        }
        let readySignalCount = streamCall == 2 ? reconnectReadySignalCount : 1
        for _ in 0..<readySignalCount {
            await onReady()
            readyInvalidationStreamCalls += 1
        }
        if streamCall == 1,
           disconnectInitialStreamAfterReady,
           !initialStreamDisconnectReleased
        {
            await withCheckedContinuation { continuation in
                initialStreamDisconnectWaiters.append(continuation)
            }
            throw FakeFailure.unavailable
        }
        if let invalidationDomain {
            if invalidationDelay != .zero { try await sleepForTest(invalidationDelay) }
            var event = Codexpulse_Core_V1_QueryInvalidationEvent()
            event.version = CodexPulseTransportContract.invalidationVersion
            event.domain = invalidationDomain
            event.sequence = 1
            try await onEvent(event)
        }
        let idleStream = AsyncStream<Void>.makeStream()
        for await _ in idleStream.stream {}
        try Task.checkCancellation()
    }

    private func waitForOverviewBarrier() async {
        guard overviewBarrierClosed else { return }
        await withCheckedContinuation { continuation in
            overviewBarrierWaiters.append(continuation)
        }
    }

    func shutdown(reason: String) async throws {
        calls.append("shutdown:\(reason)")
        if shutdownDelay != .zero { try await sleepForTest(shutdownDelay) }
    }
    func closeTransport() async { calls.append("close_transport") }
}

private func completeMeta() -> Codexpulse_Core_V1_ResponseMeta {
    var meta = Codexpulse_Core_V1_ResponseMeta()
    meta.version = "test-v1"
    meta.status = "complete"
    return meta
}

private func makePricingCatalogResponse() -> Codexpulse_Core_V1_PricingCatalogCurrentResponse {
    var response = Codexpulse_Core_V1_PricingCatalogCurrentResponse()
    response.meta = completeMeta()
    response.pricingVersion = "openai-api-test"
    response.source = "openai-api"
    response.currency = "USD"
    response.basis = "openai_api_standard_short_context_text"
    response.unitTokens.value = 1_000_000
    response.unitTokens.unit = "tokens"
    response.verifiedAtMs.value = 1_785_254_400_000
    response.verifiedAtMs.unit = "milliseconds"
    response.sourceURL = "https://developers.openai.com/api/docs/pricing"
    var item = Codexpulse_Core_V1_ModelReferencePrice()
    item.modelID = "gpt-5.4-mini"
    item.inputMicros.value = 750_000
    item.inputMicros.unit = "micro_usd"
    item.cachedInputMicros.value = 75_000
    item.cachedInputMicros.unit = "micro_usd"
    item.outputMicros.value = 4_500_000
    item.outputMicros.unit = "micro_usd"
    response.items = [item]
    return response
}

private func makeTokenActivityResponse() -> Codexpulse_Core_V1_UsageCostResponse {
    var response = Codexpulse_Core_V1_UsageCostResponse()
    response.meta = completeMeta()
    response.reportingTimeZone = "Asia/Shanghai"
    response.range.startAtMs = 1_753_632_000_000
    response.range.endAtMs = 1_785_137_400_000
    response.range.timeZone = "Asia/Shanghai"
    return response
}

private func makeTokenActivityPoint(
    date: String,
    tokens: Int64,
    turns: Int64
) -> Codexpulse_Core_V1_TrendPoint {
    var point = Codexpulse_Core_V1_TrendPoint()
    point.key = date
    point.totals.totalTokens.value = tokens
    point.totals.totalTokens.unit = "tokens"
    point.totals.turnCount.value = turns
    point.totals.turnCount.unit = "count"
    return point
}

private func makeNormalBootstrap() -> Codexpulse_Core_V1_BootstrapResponse {
    var response = Codexpulse_Core_V1_BootstrapResponse()
    response.mode = "normal"
    return response
}

private func makeResponses(
    partial: Bool = false,
    includeWeeklyQuota: Bool = true,
    accountScope: String = "default",
    quotaStatus: String = "complete",
    includeAccountResponse: Bool = true,
    includeAccountIdentity: Bool = true,
    accountType: String = "chatgpt",
    accountEmail: String? = "person@example.com",
    accountPlanType: String? = "pro"
) -> OverviewResponses {
    var accountResponse: Codexpulse_Core_V1_AccountSnapshotResponse?
    if includeAccountResponse {
        var response = Codexpulse_Core_V1_AccountSnapshotResponse()
        if includeAccountIdentity {
            var account = Codexpulse_Core_V1_CodexAccountIdentity()
            account.type = accountType
            if let accountEmail { account.email = accountEmail }
            if let accountPlanType { account.planType = accountPlanType }
            response.account = account
        }
        accountResponse = response
    }
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
    quota.meta.status = quotaStatus
    quota.current.accountScope = accountScope
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

    var tokenActivity = makeTokenActivityResponse()
    tokenActivity.totals.totalTokens.value = 0
    tokenActivity.totals.totalTokens.unit = "tokens"

    return OverviewResponses(
        usage: usage,
        quota: quota,
        account: accountResponse,
        sessions: sessions,
        projects: projects,
        health: health,
        tokenActivityUsage: tokenActivity
    )
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

private func makeSessionDetail(
    id: String,
    title: String
) -> Codexpulse_Core_V1_SessionDetailResponse {
    var response = Codexpulse_Core_V1_SessionDetailResponse()
    response.meta = completeMeta()
    response.item.sessionID = id
    response.item.displayTitle = title
    return response
}

private func makeProjectDetail(
    key: String
) -> Codexpulse_Core_V1_ProjectDetailResponse {
    var response = Codexpulse_Core_V1_ProjectDetailResponse()
    response.meta = completeMeta()
    response.item.dimensionKey = key
    return response
}

private func makeSourceDetail(
    key: String
) -> Codexpulse_Core_V1_SourceDetailResponse {
    var response = Codexpulse_Core_V1_SourceDetailResponse()
    response.meta = completeMeta()
    response.item.sourceKey = key
    return response
}

private func makeJobDetail(
    id: String
) -> Codexpulse_Core_V1_JobDetailResponse {
    var response = Codexpulse_Core_V1_JobDetailResponse()
    response.meta = completeMeta()
    response.item.jobID = id
    return response
}

private func makeHealthDetail(
    id: String
) -> Codexpulse_Core_V1_HealthDetailResponse {
    var response = Codexpulse_Core_V1_HealthDetailResponse()
    response.meta = completeMeta()
    response.item.eventID = id
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
private func testPricingCatalogLoadsWithoutUsageModels() async throws {
    let core = FakeCore(bootstrap: makeNormalBootstrap(), responses: makeResponses())
    await core.setPricingCatalogResponse(makePricingCatalogResponse())
    let model = AppModel(
        runtime: AppRuntime(supervisor: FakeSupervisor(), clientFactory: { _ in core })
    )
    model.start()
    try await waitUntil("pricing catalog overview") {
        await MainActor.run { model.presentation != nil }
    }
    model.navigate(to: .quotaUsage)
    try await waitUntil("pricing catalog current response") {
        await MainActor.run {
            model.pricingCatalogState.value?.items.first?.modelID == "gpt-5.4-mini"
        }
    }
    try expect(
        model.usageState.value?.models.isEmpty == true,
        "pricing catalog test requires an empty usage-model result"
    )
    let pricingCatalogCalls = await core.recordedPricingCatalogCalls()
    try expect(
        pricingCatalogCalls == 1,
        "quota usage navigation must load the current pricing catalog once"
    )
    _ = await model.shutdown()
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

@MainActor
private func testIndexInvalidationRefreshesSelectedSessionDetail() async throws {
    let core = FakeCore(bootstrap: makeNormalBootstrap(), responses: makeResponses())
    await core.setFeatureSessionPlans([
        SessionPagePlan(
            delay: .zero,
            response: makeSessionPage(id: "selected", title: "before")
        ),
        SessionPagePlan(
            delay: .zero,
            response: makeSessionPage(id: "selected", title: "after")
        ),
    ])
    await core.setFeatureSessionDetailPlans([
        SessionDetailPlan(
            delay: .milliseconds(500),
            response: makeSessionDetail(id: "selected", title: "too late")
        ),
        SessionDetailPlan(
            delay: .zero,
            response: makeSessionDetail(id: "selected", title: "recovered")
        ),
    ])
    await core.setInvalidation(domain: "index", delay: .milliseconds(250))
    let model = AppModel(
        runtime: AppRuntime(supervisor: FakeSupervisor(), clientFactory: { _ in core })
    )

    model.start()
    try await waitUntil("selected-detail overview") {
        await MainActor.run { model.presentation != nil }
    }
    model.navigate(to: .sessions)
    try await waitUntil("selected-detail initial page") {
        await MainActor.run {
            model.sessionsState.value?.items.first?.sessionID == "selected"
        }
    }
    model.selectSession("selected")
    try await waitUntil("selected-detail initial request") {
        await core.recordedCalls().filter { $0 == "session-detail:selected" }.count == 1
    }

    try await waitUntil("selected-detail list recovers after invalidation") {
        await MainActor.run {
            model.sessionsState.value?.items.first?.displayTitle == "after"
        }
    }
    try await waitUntil("selected detail recovers without another click") {
        await MainActor.run {
            model.sessionDetailState.value?.item.displayTitle == "recovered"
        }
    }
    try expect(
        model.selectedSessionID == "selected",
        "successful recovery must preserve the selected Session"
    )
    let detailCalls = await core.recordedCalls().filter { $0 == "session-detail:selected" }
    try expect(
        detailCalls.count == 2,
        "one invalidation must replace the cancelled detail request exactly once"
    )
    _ = await model.shutdown()
}

@MainActor
private func testForegroundRecoveryRefreshesSelectedSessionOnce() async throws {
    let core = FakeCore(bootstrap: makeNormalBootstrap(), responses: makeResponses())
    await core.setFeatureSessionPlans([
        SessionPagePlan(
            delay: .zero,
            response: makeSessionPage(id: "selected", title: "before")
        ),
        SessionPagePlan(
            delay: .zero,
            response: makeSessionPage(id: "selected", title: "after")
        ),
        SessionPagePlan(
            delay: .zero,
            response: makeSessionPage(id: "selected", title: "duplicate")
        ),
    ])
    await core.setFeatureSessionDetailPlans([
        SessionDetailPlan(
            delay: .zero,
            response: makeSessionDetail(id: "selected", title: "before")
        ),
        SessionDetailPlan(
            delay: .zero,
            response: makeSessionDetail(id: "selected", title: "after")
        ),
    ])
    let model = AppModel(
        runtime: AppRuntime(supervisor: FakeSupervisor(), clientFactory: { _ in core })
    )

    model.start()
    try await waitUntil("foreground overview") {
        await MainActor.run { model.presentation != nil }
    }
    model.navigate(to: .sessions)
    try await waitUntil("foreground initial page") {
        await MainActor.run {
            model.sessionsState.value?.items.first?.displayTitle == "before"
        }
    }
    model.selectSession("selected")
    try await waitUntil("foreground initial detail") {
        await MainActor.run {
            model.sessionDetailState.value?.item.displayTitle == "before"
        }
    }
    let sessionRequestsBefore = await core.recordedSessionRequests().count

    model.applicationDidBecomeActive()

    try expect(
        model.sessionDetailState.value?.item.displayTitle == "before",
        "foreground activation must not clear the selected detail before lifecycle recovery begins"
    )
    try await waitUntil("foreground lifecycle reaches Core") {
        await core.recordedCalls().contains("lifecycle:application_did_become_active")
    }
    try await waitUntil("foreground selected detail recovers") {
        await MainActor.run {
            model.sessionDetailState.value?.item.displayTitle == "after"
        }
    }
    let sessionRequestsAfter = await core.recordedSessionRequests().count
    try expect(
        sessionRequestsAfter == sessionRequestsBefore + 1,
        "foreground recovery must refresh the Sessions page through lifecycle exactly once"
    )
    try expect(
        model.selectedSessionID == "selected",
        "foreground recovery must preserve the selected Session"
    )
    _ = await model.shutdown()
}

@MainActor
private func testLoadingActiveSessionsRetriesUnavailableSelectedDetail() async throws {
    let core = FakeCore(bootstrap: makeNormalBootstrap(), responses: makeResponses())
    await core.setFeatureSessionPlans([
        SessionPagePlan(
            delay: .zero,
            response: makeSessionPage(id: "selected", title: "selected")
        )
    ])
    await core.setFeatureSessionDetailPlans([
        SessionDetailPlan(
            delay: .zero,
            response: makeSessionDetail(id: "selected", title: "unavailable"),
            fails: true
        ),
        SessionDetailPlan(
            delay: .zero,
            response: makeSessionDetail(id: "selected", title: "recovered")
        ),
    ])
    let model = AppModel(
        runtime: AppRuntime(supervisor: FakeSupervisor(), clientFactory: { _ in core })
    )

    model.start()
    try await waitUntil("active-load overview") {
        await MainActor.run { model.presentation != nil }
    }
    model.navigate(to: .sessions)
    try await waitUntil("active-load sessions") {
        await MainActor.run {
            model.sessionsState.value?.items.first?.sessionID == "selected"
        }
    }
    model.selectSession("selected")
    try await waitUntil("active-load unavailable detail") {
        await MainActor.run {
            if case .unavailable = model.sessionDetailState { return true }
            return false
        }
    }

    model.load(.sessions)

    try await waitUntil("active load retries selected detail without reselection") {
        await MainActor.run {
            model.sessionDetailState.value?.item.displayTitle == "recovered"
        }
    }
    let detailCalls = await core.recordedCalls().filter { $0 == "session-detail:selected" }
    try expect(
        detailCalls.count == 2,
        "loading the active page must retry only the unavailable selected detail"
    )
    _ = await model.shutdown()
}

@MainActor
private func testLoadingActiveProjectsRetriesUnavailableSelectedDetail() async throws {
    let core = FakeCore(bootstrap: makeNormalBootstrap(), responses: makeResponses())
    await core.setFeatureProjectDetailPlans([
        ProjectDetailPlan(response: makeProjectDetail(key: "project"), fails: true),
        ProjectDetailPlan(response: makeProjectDetail(key: "project"), fails: false),
    ])
    let model = AppModel(
        runtime: AppRuntime(supervisor: FakeSupervisor(), clientFactory: { _ in core })
    )

    model.start()
    try await waitUntil("active-project overview") {
        await MainActor.run { model.presentation != nil }
    }
    model.selectProject("project")
    try await waitUntil("active-project unavailable detail") {
        await MainActor.run {
            if case .unavailable = model.projectDetailState { return true }
            return false
        }
    }

    model.load(.projects)

    try await waitUntil("active project load retries selected detail") {
        await MainActor.run {
            model.projectDetailState.value?.item.dimensionKey == "project"
        }
    }
    let detailCalls = await core.recordedCalls().filter { $0 == "project-detail:project" }
    try expect(
        detailCalls.count == 2,
        "loading the active Projects page must retry only the unavailable selected detail"
    )
    _ = await model.shutdown()
}

@MainActor
private func testRefreshingProjectsRetriesUnavailableSelectedDetail() async throws {
    let core = FakeCore(bootstrap: makeNormalBootstrap(), responses: makeResponses())
    await core.setFeatureProjectDetailPlans([
        ProjectDetailPlan(response: makeProjectDetail(key: "project"), fails: true),
        ProjectDetailPlan(response: makeProjectDetail(key: "project"), fails: false),
    ])
    let model = AppModel(
        runtime: AppRuntime(supervisor: FakeSupervisor(), clientFactory: { _ in core })
    )

    model.start()
    try await waitUntil("refresh-project overview") {
        await MainActor.run { model.presentation != nil }
    }
    model.selectProject("project")
    try await waitUntil("refresh-project unavailable detail") {
        await MainActor.run {
            if case .unavailable = model.projectDetailState { return true }
            return false
        }
    }

    model.refresh(.projects)

    try await waitUntil("project refresh retries selected detail") {
        await MainActor.run {
            model.projectDetailState.value?.item.dimensionKey == "project"
        }
    }
    let detailCalls = await core.recordedCalls().filter { $0 == "project-detail:project" }
    try expect(
        detailCalls.count == 2,
        "refreshing Projects must retry the selected detail exactly once"
    )
    _ = await model.shutdown()
}

@MainActor
private func testRefreshingSourcesAndJobsRetriesUnavailableSelectedDetails() async throws {
    let core = FakeCore(bootstrap: makeNormalBootstrap(), responses: makeResponses())
    await core.setFeatureSourceDetailPlans([
        SourceDetailPlan(response: makeSourceDetail(key: "source"), fails: true),
        SourceDetailPlan(response: makeSourceDetail(key: "source"), fails: false),
    ])
    await core.setFeatureJobDetailPlans([
        JobDetailPlan(response: makeJobDetail(id: "job"), fails: true),
        JobDetailPlan(response: makeJobDetail(id: "job"), fails: false),
    ])
    let model = AppModel(
        runtime: AppRuntime(supervisor: FakeSupervisor(), clientFactory: { _ in core })
    )

    model.start()
    try await waitUntil("refresh-sources-jobs overview") {
        await MainActor.run { model.presentation != nil }
    }
    model.selectSource("source")
    model.selectJob("job")
    try await waitUntil("refresh-sources-jobs unavailable details") {
        await MainActor.run {
            guard case .unavailable = model.sourceDetailState else { return false }
            guard case .unavailable = model.jobDetailState else { return false }
            return true
        }
    }

    model.refresh(.sourcesJobs)

    try await waitUntil("sources and jobs refresh selected details") {
        await MainActor.run {
            model.sourceDetailState.value?.item.sourceKey == "source"
                && model.jobDetailState.value?.item.jobID == "job"
        }
    }
    let calls = await core.recordedCalls()
    try expect(
        calls.filter { $0 == "source-detail:source" }.count == 2,
        "refreshing Sources and Jobs must retry the selected Source exactly once"
    )
    try expect(
        calls.filter { $0 == "job-detail:job" }.count == 2,
        "refreshing Sources and Jobs must retry the selected Job exactly once"
    )
    _ = await model.shutdown()
}

@MainActor
private func testLoadingActiveSourcesAndJobsRetriesUnavailableSelectedDetails() async throws {
    let core = FakeCore(bootstrap: makeNormalBootstrap(), responses: makeResponses())
    await core.setFeatureSourceDetailPlans([
        SourceDetailPlan(response: makeSourceDetail(key: "source"), fails: true),
        SourceDetailPlan(response: makeSourceDetail(key: "source"), fails: false),
    ])
    await core.setFeatureJobDetailPlans([
        JobDetailPlan(response: makeJobDetail(id: "job"), fails: true),
        JobDetailPlan(response: makeJobDetail(id: "job"), fails: false),
    ])
    let model = AppModel(
        runtime: AppRuntime(supervisor: FakeSupervisor(), clientFactory: { _ in core })
    )

    model.start()
    try await waitUntil("load-sources-jobs overview") {
        await MainActor.run { model.presentation != nil }
    }
    model.selectSource("source")
    model.selectJob("job")
    try await waitUntil("load-sources-jobs unavailable details") {
        await MainActor.run {
            guard case .unavailable = model.sourceDetailState else { return false }
            guard case .unavailable = model.jobDetailState else { return false }
            return true
        }
    }

    model.load(.sourcesJobs)

    try await waitUntil("active sources and jobs load selected details") {
        await MainActor.run {
            model.sourceDetailState.value?.item.sourceKey == "source"
                && model.jobDetailState.value?.item.jobID == "job"
        }
    }
    let calls = await core.recordedCalls()
    try expect(
        calls.filter { $0 == "source-detail:source" }.count == 2,
        "loading active Sources and Jobs must retry the selected Source exactly once"
    )
    try expect(
        calls.filter { $0 == "job-detail:job" }.count == 2,
        "loading active Sources and Jobs must retry the selected Job exactly once"
    )
    _ = await model.shutdown()
}

@MainActor
private func testRefreshingLocalStatusRetriesUnavailableSelectedHealthDetail() async throws {
    let core = FakeCore(bootstrap: makeNormalBootstrap(), responses: makeResponses())
    await core.setFeatureHealthDetailPlans([
        HealthDetailPlan(response: makeHealthDetail(id: "health"), fails: true),
        HealthDetailPlan(response: makeHealthDetail(id: "health"), fails: false),
    ])
    let model = AppModel(
        runtime: AppRuntime(supervisor: FakeSupervisor(), clientFactory: { _ in core })
    )

    model.start()
    try await waitUntil("refresh-health overview") {
        await MainActor.run { model.presentation != nil }
    }
    model.selectHealthEvent("health")
    try await waitUntil("refresh-health unavailable detail") {
        await MainActor.run {
            if case .unavailable = model.healthDetailState { return true }
            return false
        }
    }

    model.refresh(.localStatus)

    try await waitUntil("local status refresh retries selected health detail") {
        await MainActor.run {
            model.healthDetailState.value?.item.eventID == "health"
        }
    }
    let detailCalls = await core.recordedCalls().filter { $0 == "health-detail:health" }
    try expect(
        detailCalls.count == 2,
        "refreshing Local Status must retry the selected Health event exactly once"
    )
    _ = await model.shutdown()
}

@MainActor
private func testLoadingActiveLocalStatusRetriesUnavailableSelectedHealthDetail() async throws {
    let core = FakeCore(bootstrap: makeNormalBootstrap(), responses: makeResponses())
    await core.setFeatureHealthDetailPlans([
        HealthDetailPlan(response: makeHealthDetail(id: "health"), fails: true),
        HealthDetailPlan(response: makeHealthDetail(id: "health"), fails: false),
    ])
    let model = AppModel(
        runtime: AppRuntime(supervisor: FakeSupervisor(), clientFactory: { _ in core })
    )

    model.start()
    try await waitUntil("load-health overview") {
        await MainActor.run { model.presentation != nil }
    }
    model.selectHealthEvent("health")
    try await waitUntil("load-health unavailable detail") {
        await MainActor.run {
            if case .unavailable = model.healthDetailState { return true }
            return false
        }
    }

    model.load(.localStatus)

    try await waitUntil("active local status load retries selected health detail") {
        await MainActor.run {
            model.healthDetailState.value?.item.eventID == "health"
        }
    }
    let detailCalls = await core.recordedCalls().filter { $0 == "health-detail:health" }
    try expect(
        detailCalls.count == 2,
        "loading active Local Status must retry the selected Health event exactly once"
    )
    _ = await model.shutdown()
}

@MainActor
private func testRefreshAllRetriesEveryUnavailableSelectedDetail() async throws {
    let core = FakeCore(bootstrap: makeNormalBootstrap(), responses: makeResponses())
    await core.setFeatureSessionDetailPlans([
        SessionDetailPlan(
            delay: .zero,
            response: makeSessionDetail(id: "session", title: "unavailable"),
            fails: true
        ),
        SessionDetailPlan(
            delay: .zero,
            response: makeSessionDetail(id: "session", title: "recovered")
        ),
    ])
    await core.setFeatureProjectDetailPlans([
        ProjectDetailPlan(response: makeProjectDetail(key: "project"), fails: true),
        ProjectDetailPlan(response: makeProjectDetail(key: "project"), fails: false),
    ])
    await core.setFeatureSourceDetailPlans([
        SourceDetailPlan(response: makeSourceDetail(key: "source"), fails: true),
        SourceDetailPlan(response: makeSourceDetail(key: "source"), fails: false),
    ])
    await core.setFeatureJobDetailPlans([
        JobDetailPlan(response: makeJobDetail(id: "job"), fails: true),
        JobDetailPlan(response: makeJobDetail(id: "job"), fails: false),
    ])
    await core.setFeatureHealthDetailPlans([
        HealthDetailPlan(response: makeHealthDetail(id: "health"), fails: true),
        HealthDetailPlan(response: makeHealthDetail(id: "health"), fails: false),
    ])
    let model = AppModel(
        runtime: AppRuntime(supervisor: FakeSupervisor(), clientFactory: { _ in core })
    )

    model.start()
    try await waitUntil("refresh-all overview") {
        await MainActor.run { model.presentation != nil }
    }
    model.selectSession("session")
    model.selectProject("project")
    model.selectSource("source")
    model.selectJob("job")
    model.selectHealthEvent("health")
    try await waitUntil("refresh-all unavailable details") {
        await MainActor.run {
            guard case .unavailable = model.sessionDetailState else { return false }
            guard case .unavailable = model.projectDetailState else { return false }
            guard case .unavailable = model.sourceDetailState else { return false }
            guard case .unavailable = model.jobDetailState else { return false }
            guard case .unavailable = model.healthDetailState else { return false }
            return true
        }
    }

    model.refreshAllFeatures()

    try await waitUntil("refresh all retries every selected detail") {
        await MainActor.run {
            model.sessionDetailState.value?.item.sessionID == "session"
                && model.projectDetailState.value?.item.dimensionKey == "project"
                && model.sourceDetailState.value?.item.sourceKey == "source"
                && model.jobDetailState.value?.item.jobID == "job"
                && model.healthDetailState.value?.item.eventID == "health"
        }
    }
    let calls = await core.recordedCalls()
    try expect(
        calls.filter { $0 == "session-detail:session" }.count == 2,
        "Refresh All must retry the selected Session exactly once"
    )
    try expect(
        calls.filter { $0 == "project-detail:project" }.count == 2,
        "Refresh All must retry the selected Project exactly once"
    )
    try expect(
        calls.filter { $0 == "source-detail:source" }.count == 2,
        "Refresh All must retry the selected Source exactly once"
    )
    try expect(
        calls.filter { $0 == "job-detail:job" }.count == 2,
        "Refresh All must retry the selected Job exactly once"
    )
    try expect(
        calls.filter { $0 == "health-detail:health" }.count == 2,
        "Refresh All must retry the selected Health event exactly once"
    )
    _ = await model.shutdown()
}

@MainActor
private func testRefreshAllReportsGlobalProgressUntilEveryReadCompletes() async throws {
    let core = FakeCore(bootstrap: makeNormalBootstrap(), responses: makeResponses())
    await core.setFeatureSessionPlans([
        SessionPagePlan(
            delay: .milliseconds(150),
            response: makeSessionPage(id: "session", title: "refreshed")
        )
    ])
    let model = AppModel(
        runtime: AppRuntime(supervisor: FakeSupervisor(), clientFactory: { _ in core })
    )

    model.start()
    try await waitUntil("refresh-all progress overview") {
        await MainActor.run { model.presentation != nil }
    }

    model.refreshAllFeatures()

    try expect(model.isRefreshingAll, "Refresh All must expose global progress immediately")
    try expect(
        model.sessionsState.isLoading,
        "Refresh All progress must begin while the delayed page read is still running"
    )
    try await waitUntil("refresh-all progress completion") {
        await MainActor.run { !model.isRefreshingAll }
    }
    try expect(
        model.sessionsState.value?.items.first?.sessionID == "session",
        "Refresh All must clear global progress only after the last page read completes"
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
    try await sleepForTest(.milliseconds(20))
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
    response.snapshot.updates.channel = "stable"
    response.snapshot.ui.launchBehavior = "main_window"
    response.snapshot.ui.overviewRange = "7d"
    var editable = Codexpulse_Core_V1_EditableField()
    editable.key = "online.quotaEnabled"
    editable.editable = true
    var updateChannelEditable = Codexpulse_Core_V1_EditableField()
    updateChannelEditable.key = "updates.channel"
    updateChannelEditable.editable = true
    updateChannelEditable.options = ["stable", "prerelease"]
    response.editableFields = [editable, updateChannelEditable]

    var draft = SettingsDraft(response)
    draft.quotaEnabled = true
    draft.resetCreditsEnabled = false
    draft.quotaIntervalSeconds = 1
    draft.updateChannel = "prerelease"
    let request = draft.makeRequest(authoritative: response)
    try expect(
        request.expectedRevision == "revision-1", "settings write must carry authoritative revision")
    try expect(request.online.quotaEnabled, "editable field must carry the draft")
    try expect(
        request.online.resetCreditsEnabled, "non-editable field must preserve authoritative truth")
    try expect(
        request.refresh.quotaIntervalSeconds == 300,
        "non-editable numeric field must not be shadow-edited")
    try expect(
        request.updates.channel == "prerelease",
        "editable update channel must carry the user's stable or prerelease selection")
}

private func testUpdateChannelsMapToSparkleAllowedChannels() throws {
    try expect(
        AppUpdateChannel.stable.sparkleAllowedChannels.isEmpty,
        "stable updates must use Sparkle's default channel"
    )
    try expect(
        AppUpdateChannel.prerelease.sparkleAllowedChannels == ["prerelease"],
        "prerelease updates must opt into prerelease while retaining Sparkle's default channel"
    )
}

private func testUpdateInstallationRequiresCleanClientRestartShutdown() async throws {
    let cleanCore = FakeCore(bootstrap: makeNormalBootstrap(), responses: makeResponses())
    let cleanRuntime = AppRuntime(
        supervisor: FakeSupervisor(),
        clientFactory: { _ in cleanCore }
    )
    await cleanRuntime.start()
    let cleanOutcome = await cleanRuntime.shutdown(reason: .updateInstallation)
    try expect(
        AppUpdateInstallPreparation(shutdownOutcome: cleanOutcome) == .ready,
        "an update may replace the bundle only after a clean Helper shutdown"
    )
    let cleanCalls = await cleanCore.recordedCalls()
    try expect(
        cleanCalls.contains("shutdown:client_restart"),
        "update installation must identify the shutdown as a client restart"
    )

    let delayedCore = FakeCore(bootstrap: makeNormalBootstrap(), responses: makeResponses())
    await delayedCore.setShutdownDelay(.seconds(60))
    let forcedRuntime = AppRuntime(
        supervisor: FakeSupervisor(),
        shutdownRequestTimeout: .milliseconds(40),
        clientFactory: { _ in delayedCore }
    )
    await forcedRuntime.start()
    let forcedOutcome = await forcedRuntime.shutdown(reason: .updateInstallation)
    try expect(
        AppUpdateInstallPreparation(shutdownOutcome: forcedOutcome) == .blocked(.forced),
        "a forced or uncertain Helper shutdown must block bundle replacement"
    )
}

@MainActor
private final class FakeSparkleUpdaterEngine: SparkleUpdaterEngine {
    var automaticallyChecksForUpdates = false
    var automaticallyDownloadsUpdates = false
    var updateCheckInterval: TimeInterval = 0
    var canCheckForUpdates = true
    private(set) var checkCount = 0
    private(set) var resetCount = 0

    func checkForUpdates() {
        checkCount += 1
    }

    func resetUpdateCycle() {
        resetCount += 1
    }
}

@MainActor
private func testSparkleUpdaterAppliesPolicyAndTracksInstallation() throws {
    let engine = FakeSparkleUpdaterEngine()
    let updater = SparkleAppUpdater(engine: engine)
    let policy = AppUpdatePolicy(
        automaticallyChecks: true,
        automaticallyDownloads: false,
        channel: .prerelease,
        checkIntervalSeconds: 7_200
    )

    updater.apply(policy)
    try expect(engine.automaticallyChecksForUpdates, "Sparkle must inherit automatic checks")
    try expect(!engine.automaticallyDownloadsUpdates, "Sparkle must preserve check-only policy")
    try expect(engine.updateCheckInterval == 7_200, "Sparkle must inherit the check interval")
    try expect(
        updater.allowedChannels == ["prerelease"],
        "Sparkle must receive the configured prerelease channel"
    )
    try expect(engine.resetCount == 1, "a policy change must reset Sparkle's update cycle")

    updater.checkForUpdates()
    try expect(engine.checkCount == 1, "manual checks must be delegated to Sparkle")

    updater.noteUpdateWillInstall()
    try expect(updater.installationInProgress, "Sparkle install intent must be observable")
    updater.noteUpdateDidAbort()
    try expect(!updater.installationInProgress, "an aborted install must clear stale intent")

    updater.noteUpdateWillInstall()
    try expect(
        updater.installationInProgress,
        "a scheduled install must remain observable until Sparkle aborts it"
    )
}

private func testSparkleBundleConfigurationRequiresHTTPSAndEd25519Key() throws {
    let publicKey = Data(repeating: 7, count: 32).base64EncodedString()
    try expect(
        SparkleBundleConfiguration(
            feedURLString: "https://updates.example.com/codex-pulse/appcast.xml",
            publicEDKey: publicKey
        ) != nil,
        "a release bundle must accept an HTTPS appcast and a 32-byte Ed25519 public key"
    )
    try expect(
        SparkleBundleConfiguration(
            feedURLString: "http://updates.example.com/appcast.xml",
            publicEDKey: publicKey
        ) == nil,
        "an insecure appcast URL must not enable the updater"
    )
    try expect(
        SparkleBundleConfiguration(
            feedURLString: "https://user@updates.example.com/appcast.xml",
            publicEDKey: publicKey
        ) == nil,
        "a feed URL containing credentials must not enable the updater"
    )
    try expect(
        SparkleBundleConfiguration(
            feedURLString: "https://updates.example.com/appcast.xml",
            publicEDKey: "not-a-key"
        ) == nil,
        "an invalid Ed25519 public key must not enable the updater"
    )
}

@MainActor
private func testAppModelPublishesConfiguredUpdatePolicyForSparkle() async throws {
    let core = FakeCore(bootstrap: makeNormalBootstrap(), responses: makeResponses())
    var settings = makeSettingsResponse(revision: "revision-1", quotaEnabled: true)
    settings.snapshot.updates.autoCheckEnabled = true
    settings.snapshot.updates.autoDownloadEnabled = true
    settings.snapshot.updates.channel = "prerelease"
    settings.snapshot.updates.checkIntervalSeconds = 7_200
    await core.setSettingsResponses([settings], updateFailure: false)
    let model = AppModel(
        runtime: AppRuntime(supervisor: FakeSupervisor(), clientFactory: { _ in core }),
        observesUpdatePolicy: true
    )

    model.start()
    let expected = AppUpdatePolicy(
        automaticallyChecks: true,
        automaticallyDownloads: true,
        channel: .prerelease,
        checkIntervalSeconds: 7_200
    )
    try await waitUntil("configured Sparkle update policy") {
        await MainActor.run { model.updatePolicy == expected }
    }
    try expect(
        model.updatePolicy == expected,
        "normal startup must publish the Helper-owned update policy for Sparkle")
    _ = await model.shutdown()
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
    try await sleepForTest(.milliseconds(20))
    model.sessionOptions.projectID = "replacement"
    model.sessionFiltersChanged()
    try await waitUntil("replacement feature result") {
        await MainActor.run { model.sessionsState.value?.items.first?.sessionID == "new" }
    }
    try await sleepForTest(.milliseconds(140))
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
        presentation.usageRangeLabel == "自 7月14日 · 按天",
        "usage range label must show the daily weekly start")
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

private func testTokenActivityRequestUsesAnIndependentRollingYear() throws {
    var calendar = Calendar(identifier: .gregorian)
    calendar.timeZone = TimeZone(identifier: "Asia/Shanghai")!
    let now = Date(timeIntervalSince1970: 1_785_137_400)

    let request = OverviewRequestSet.tokenActivityRequest(now: now, calendar: calendar)

    try expect(request.granularity == "day", "Token activity must request daily buckets")
    try expect(request.hasExactRange, "Token activity must use one exact rolling-year range")
    try expect(
        request.exactRange.startAtMs == 1_753_632_000_000,
        "Token activity must include 365 local calendar days including today")
    try expect(
        request.exactRange.endAtMs == 1_785_137_400_000,
        "Token activity must end at its own evaluation time")
    try expect(
        request.exactRange.timeZone == "Asia/Shanghai",
        "Token activity must preserve the reporting timezone")
    try expect(
        !request.includeActivityDistribution,
        "the independent annual request must not scan the current-range activity distribution")
}

private func testOverviewActivityPresentationBuildsTimelineAndCompleteHeatmap() throws {
    let rangeStart: Int64 = 1_753_056_000_000
    let firstStart = rangeStart + 3_600_000
    let secondStart = rangeStart + 7_200_000
    let rangeEnd = rangeStart + 86_400_000
    var response = Codexpulse_Core_V1_UsageCostResponse()
    response.meta = completeMeta()
    response.range.startAtMs = rangeStart
    response.range.endAtMs = rangeEnd
    response.range.timeZone = "UTC"
    response.reportingTimeZone = "UTC"
    response.totals.totalTokens.value = 100
    response.totals.totalTokens.unit = "tokens"
    response.activityDistribution.timelineGranularity = "hour"
    response.activityDistribution.timelineBucketMinutes = 60

    func metrics(tokens: Int64, sessions: Int64) -> Codexpulse_Core_V1_ActivityMetrics {
        var value = Codexpulse_Core_V1_ActivityMetrics()
        value.totalTokens.value = tokens
        value.totalTokens.unit = "tokens"
        value.sessionCount.value = sessions
        value.sessionCount.unit = "count"
        return value
    }
    func timeline(
        start: Int64,
        tokens: Int64,
        sessions: Int64
    ) -> Codexpulse_Core_V1_ActivityTimelinePoint {
        var point = Codexpulse_Core_V1_ActivityTimelinePoint()
        point.startAtMs.value = start
        point.startAtMs.unit = "milliseconds"
        point.endAtMs.value = start + 3_600_000
        point.endAtMs.unit = "milliseconds"
        point.metrics = metrics(tokens: tokens, sessions: sessions)
        return point
    }
    func weekdayHour(
        weekday: Int32,
        hour: Int32,
        tokens: Int64,
        sessions: Int64
    ) -> Codexpulse_Core_V1_ActivityWeekdayHourPoint {
        var point = Codexpulse_Core_V1_ActivityWeekdayHourPoint()
        point.weekday = weekday
        point.hour = hour
        point.metrics = metrics(tokens: tokens, sessions: sessions)
        return point
    }
    response.activityDistribution.timeline = [
        timeline(start: firstStart, tokens: 40, sessions: 2),
        timeline(start: secondStart, tokens: 60, sessions: 1),
    ]
    response.activityDistribution.weekdayHours = [
        weekdayHour(weekday: 1, hour: 1, tokens: 40, sessions: 2),
        weekdayHour(weekday: 1, hour: 2, tokens: 60, sessions: 1),
    ]

    let presentation = OverviewActivityPresentation(response)

    try expect(
        OverviewActivityMetric.allCases.map(\.title) == ["Token 消耗", "会话数量"],
        "activity metric options must use the approved Chinese wording")
    try expect(
        presentation.availability == .available
            && presentation.timelineGranularity == .hour
            && presentation.timelineBucketMinutes == 60
            && presentation.timeline.count == 2,
        "a reconciled activity response must expose its ordered hourly timeline")
    try expect(
        presentation.heatmap.count == 7 * 24,
        "a complete activity response must expose every weekday-hour cell")
    try expect(
        presentation.heatmap.first(where: { $0.weekday == 1 && $0.hour == 1 })?.totalTokens
            == .known(40, unit: "tokens"),
        "an observed weekday-hour cell must preserve Token consumption")
    try expect(
        presentation.heatmap.first(where: { $0.weekday == 7 && $0.hour == 23 })?.sessionCount
            == .known(0, unit: "count"),
        "a missing cell in reconciled facts must be a known zero")
    try expect(
        presentation.timeline.first?.sessionCount == .known(2, unit: "count"),
        "timeline buckets must preserve distinct session counts")
}

private func testOverviewActivityAxisTicksKeepDateOnlyAtDayBoundaries() throws {
    let hour: Int64 = 3_600_000
    var calendar = Calendar(identifier: .gregorian)
    calendar.timeZone = TimeZone(identifier: "Asia/Shanghai")!
    let dayOne = Int64(calendar.date(
        from: DateComponents(year: 2026, month: 7, day: 26)
    )!.timeIntervalSince1970 * 1_000)
    let starts = [0, 6, 12, 18, 24, 30, 36, 42].map { dayOne + Int64($0) * hour }
    let points = starts.map { start in
        OverviewActivityTimelinePoint(
            id: start,
            startAtMS: start,
            endAtMS: start + hour,
            totalTokens: .known(1, unit: "tokens"),
            sessionCount: .known(1, unit: "count")
        )
    }

    let ticks = OverviewActivityTimelineResolver.axisTicks(
        points: points,
        granularity: .hour,
        timeZoneID: "Asia/Shanghai",
        maximumCount: 8
    )

    try expect(
        ticks.map(\.label) == [
            "7月26日 0时", "6时", "12时", "18时",
            "7月27日 0时", "6时", "12时", "18时",
        ],
        "hourly activity axis must show the date once per local day instead of repeating it")
}

private func testOverviewActivityTimelineResolverSelectsContainingBucketThenNearest() throws {
    let hour: Int64 = 3_600_000
    let start: Int64 = 1_753_488_000_000
    let points = [0, 1, 2].map { offset in
        let bucketStart = start + Int64(offset) * hour
        return OverviewActivityTimelinePoint(
            id: bucketStart,
            startAtMS: bucketStart,
            endAtMS: bucketStart + hour,
            totalTokens: .known(Int64(offset + 1), unit: "tokens"),
            sessionCount: .known(1, unit: "count")
        )
    }

    let insideSecond = Date(
        timeIntervalSince1970: Double(start + hour + 15 * 60_000) / 1_000
    )
    let afterLast = Date(timeIntervalSince1970: Double(start + 4 * hour) / 1_000)

    try expect(
        OverviewActivityTimelineResolver.nearest(to: insideSecond, in: points)?.id
            == start + hour,
        "hovering anywhere inside an activity bucket must select that bucket")
    try expect(
        OverviewActivityTimelineResolver.nearest(to: afterLast, in: points)?.id
            == start + 2 * hour,
        "hovering outside the buckets must fall back to the nearest bucket center")
    try expect(
        OverviewActivityTimelineResolver.nearest(to: nil, in: points) == nil,
        "ending chart hover must clear the selected activity bucket")
}

private func testOverviewActivityTimelineResolverInsetsBarsSymmetrically() throws {
    let point = OverviewActivityTimelinePoint(
        id: 0,
        startAtMS: 0,
        endAtMS: 1_000,
        totalTokens: .known(1, unit: "tokens"),
        sessionCount: .known(1, unit: "count")
    )

    let range = OverviewActivityTimelineResolver.visibleRange(for: point)

    try expect(
        range.map {
            abs($0.lowerBound.timeIntervalSince1970 - 0.28) < 0.000_001
                && abs($0.upperBound.timeIntervalSince1970 - 0.72) < 0.000_001
        } == true,
        "activity bars must reserve a stable symmetric gap between adjacent time buckets")
}

private func testTokenActivityPresentationBuildsCalendarAndStreakStatistics() throws {
    var response = makeTokenActivityResponse()
    response.trend = [
        makeTokenActivityPoint(date: "2026-07-22", tokens: 10, turns: 1),
        makeTokenActivityPoint(date: "2026-07-23", tokens: 20, turns: 2),
        makeTokenActivityPoint(date: "2026-07-25", tokens: 40, turns: 4),
        makeTokenActivityPoint(date: "2026-07-26", tokens: 50, turns: 5),
    ]
    response.totals.totalTokens.value = 120
    response.totals.totalTokens.unit = "tokens"

    let presentation = TokenActivityPresentation(
        response,
        now: Date(timeIntervalSince1970: 1_785_137_400)
    )

    try expect(
        presentation.availability == .available,
        "a complete annual response must remain available")
    try expect(
        presentation.days.count == 365
            && presentation.days.first?.dateKey == "2025-07-28"
            && presentation.days.last?.dateKey == "2026-07-27",
        "Token activity must expose exactly 365 ordered local calendar days")
    try expect(
        presentation.days.first(where: { $0.dateKey == "2026-07-24" })?.tokens == 0,
        "a missing day in a complete response must mean known zero activity")
    try expect(
        presentation.totalTokens == 120
            && presentation.peakDailyTokens == 50
            && presentation.activeDays == 4,
        "Token activity statistics must use the complete daily facts")
    try expect(
        presentation.currentStreakDays == 2,
        "an inactive in-progress today must preserve the streak ending yesterday")
    try expect(
        presentation.longestStreakDays == 2,
        "Token activity must find the longest consecutive active-day run")
}

private func testTokenActivityPresentationSummarizesKnownLocalFactsFromPartialResponse() throws {
    var response = makeTokenActivityResponse()
    response.meta.status = "partial"
    response.trend = [makeTokenActivityPoint(date: "2026-07-26", tokens: 50, turns: 5)]
    response.totals.totalTokens.value = 50
    response.totals.totalTokens.unit = "tokens"

    let presentation = TokenActivityPresentation(
        response,
        now: Date(timeIntervalSince1970: 1_785_137_400)
    )

    try expect(
        presentation.availability == .partial,
        "a partial annual response must not look complete")
    try expect(
        presentation.days.first(where: { $0.dateKey == "2026-07-24" })?.tokens == nil,
        "a missing day in a partial response must stay unknown instead of becoming zero")
    try expect(
        presentation.totalTokens == 50
            && presentation.peakDailyTokens == 50
            && presentation.activeDays == 1
            && presentation.currentStreakDays == 1
            && presentation.longestStreakDays == 1,
        "a partial response with reconciled Token facts must summarize the local data it knows")
}

private func testTokenActivityPresentationAnchorsToTheResponseRange() throws {
    var response = makeTokenActivityResponse()
    response.range.endAtMs = 1_767_544_200_000
    response.totals.totalTokens.value = 0
    response.totals.totalTokens.unit = "tokens"

    let presentation = TokenActivityPresentation(response)

    try expect(
        presentation.days.last?.dateKey == "2026-01-05",
        "retained annual data must stay anchored to its response range instead of drifting with wall time")
}

private func testTokenActivityPresentationRejectsFactsOutsideItsVisibleYear() throws {
    var response = makeTokenActivityResponse()
    response.trend = [makeTokenActivityPoint(date: "2025-07-27", tokens: 10, turns: 1)]
    response.totals.totalTokens.value = 10
    response.totals.totalTokens.unit = "tokens"

    let presentation = TokenActivityPresentation(
        response,
        now: Date(timeIntervalSince1970: 1_785_137_400)
    )

    try expect(
        presentation.availability == .partial
            && presentation.totalTokens == nil
            && presentation.activeDays == nil,
        "out-of-range daily facts must not produce complete summary values that the heatmap cannot show")
}

private func testTokenActivityCalendarAlignsWeeksAndMonthLabels() throws {
    var response = makeTokenActivityResponse()
    response.totals.totalTokens.value = 0
    response.totals.totalTokens.unit = "tokens"
    let activity = TokenActivityPresentation(
        response,
        now: Date(timeIntervalSince1970: 1_785_137_400)
    )

    let calendar = TokenActivityCalendarPresentation(activity)

    try expect(calendar.weeks.count == 53, "365 daily cells must align into 53 calendar weeks")
    try expect(
        calendar.weeks.first?.days.first??.day.dateKey == "2025-07-28",
        "the annual calendar must align its first Monday to the first row")
    try expect(
        calendar.weeks.last?.days.first??.day.dateKey == "2026-07-27"
            && calendar.weeks.last?.days.dropFirst().allSatisfy { $0 == nil } == true,
        "the current partial week must end with non-data padding cells")
    try expect(
        calendar.monthLabels.first
            == TokenActivityMonthLabel(id: "2025-08", title: "8月", weekIndex: 0)
            && calendar.monthLabels.last?.title == "7月",
        "month labels must follow visible month boundaries without labeling a partial prior month")
}

private func testTokenActivityCalendarSeparatesUnknownZeroAndRelativeIntensity() throws {
    let thresholds = ActivityIntensityScale.thresholds(for: [10, 20, 40, 80])
    try expect(
        ActivityIntensityScale.intensity(for: nil, thresholds: thresholds) == .unknown
            && ActivityIntensityScale.intensity(for: 0, thresholds: thresholds) == .none
            && ActivityIntensityScale.intensity(for: 10, thresholds: thresholds) == .low
            && ActivityIntensityScale.intensity(for: 20, thresholds: thresholds) == .medium
            && ActivityIntensityScale.intensity(for: 40, thresholds: thresholds) == .high
            && ActivityIntensityScale.intensity(for: 80, thresholds: thresholds) == .veryHigh,
        "shared activity scale must keep unknown, zero, and four non-zero levels distinct")

    var response = makeTokenActivityResponse()
    response.trend = [
        makeTokenActivityPoint(date: "2026-07-22", tokens: 10, turns: 1),
        makeTokenActivityPoint(date: "2026-07-23", tokens: 20, turns: 2),
        makeTokenActivityPoint(date: "2026-07-24", tokens: 40, turns: 4),
        makeTokenActivityPoint(date: "2026-07-25", tokens: 80, turns: 8),
    ]
    response.totals.totalTokens.value = 150
    response.totals.totalTokens.unit = "tokens"
    let complete = TokenActivityCalendarPresentation(TokenActivityPresentation(
        response,
        now: Date(timeIntervalSince1970: 1_785_137_400)
    ))

    let levels = Dictionary(uniqueKeysWithValues: complete.weeks.flatMap(\.days).compactMap {
        $0.map { ($0.day.dateKey, $0.intensity) }
    })
    try expect(
        levels["2026-07-21"] == TokenActivityIntensity.none
            && levels["2026-07-22"] == .low
            && levels["2026-07-23"] == .medium
            && levels["2026-07-24"] == .high
            && levels["2026-07-25"] == .veryHigh,
        "the heatmap must reserve zero for no activity and order four non-zero intensity levels")

    response.meta.status = "partial"
    let partial = TokenActivityCalendarPresentation(TokenActivityPresentation(
        response,
        now: Date(timeIntervalSince1970: 1_785_137_400)
    ))
    let partialLevels = Dictionary(uniqueKeysWithValues: partial.weeks.flatMap(\.days).compactMap {
        $0.map { ($0.day.dateKey, $0.intensity) }
    })
    try expect(
        partialLevels["2026-07-21"] == .unknown
            && partialLevels["2026-07-25"] == .veryHigh,
        "partial heatmaps must distinguish missing unknown days from known activity")
}

private func testTokenActivityCardPresentsFiveBoundedMetricsAndDayDetails() throws {
    var response = makeTokenActivityResponse()
    response.trend = [
        makeTokenActivityPoint(date: "2026-07-22", tokens: 10, turns: 1),
        makeTokenActivityPoint(date: "2026-07-23", tokens: 20, turns: 2),
        makeTokenActivityPoint(date: "2026-07-25", tokens: 40, turns: 4),
        makeTokenActivityPoint(date: "2026-07-26", tokens: 23_032_305, turns: 5),
    ]
    response.totals.totalTokens.value = 23_032_375
    response.totals.totalTokens.unit = "tokens"
    let activity = TokenActivityPresentation(
        response,
        now: Date(timeIntervalSince1970: 1_785_137_400)
    )

    let card = TokenActivityCardPresentation(activity)

    try expect(
        card.title == "Token 活动" && card.scope == "过去 365 天",
        "the annual card must state its independent scope")
    try expect(
        card.metrics.map(\.title) == [
            "近 365 天 Token", "峰值日 Token", "活跃天数", "当前连续天数", "最长连续天数",
        ] && card.metrics.map(\.value) == ["2.3千万", "2.3千万", "4 天", "2 天", "2 天"],
        "the annual card must present the five approved bounded metrics in order")
    guard let peakDay = card.calendar.weeks.flatMap(\.days).compactMap({ $0 }).first(where: {
        $0.day.dateKey == "2026-07-26"
    }) else {
        throw TestFailure.mismatch("annual card omitted a known peak day")
    }
    try expect(
        card.dayDetail(peakDay) == "2026年7月26日 · 2.3千万 Token · 5 轮",
        "a heatmap cell must expose an exact localized hover and accessibility description")
}

private func testTokenActivityCardLabelsReconciledPartialFactsAsLocalData() throws {
    var response = makeTokenActivityResponse()
    response.meta.status = "partial"
    response.trend = [makeTokenActivityPoint(date: "2026-07-26", tokens: 50, turns: 5)]
    response.totals.totalTokens.value = 50
    response.totals.totalTokens.unit = "tokens"

    let card = TokenActivityCardPresentation(TokenActivityPresentation(
        response,
        now: Date(timeIntervalSince1970: 1_785_137_400)
    ))

    try expect(
        card.notice == "仅统计本机现有数据"
            && card.metrics.map(\.value) == ["50", "50", "1 天", "1 天", "1 天"],
        "reconciled partial facts must show local summary values with an honest scope notice")
}

private func testTokenActivityHoverStateTracksOnlyTheCurrentCell() throws {
    var hover = TokenActivityHoverState()

    hover.update(isHovering: true, dayID: "2026-07-27")
    try expect(
        hover.isHovered(dayID: "2026-07-27"),
        "entering an activity cell must immediately expose its explicit hover state")

    hover.update(isHovering: false, dayID: "2026-07-26")
    try expect(
        hover.isHovered(dayID: "2026-07-27"),
        "leaving a stale cell must not dismiss the currently hovered day")

    hover.update(isHovering: false, dayID: "2026-07-27")
    try expect(
        !hover.isHovered(dayID: "2026-07-27"),
        "leaving the current activity cell must dismiss its hover detail")
}

private func testTokenActivityHeatmapUsesTheAvailableCardWidth() throws {
    let source = try mainWindowSource("RootView.swift")
    try expect(
        source.contains("let cellSize = max(6, availableWidth / CGFloat(weekCount))")
            && source.contains(".aspectRatio(6.8, contentMode: .fit)")
            && !source.contains("let cellSize = min(14"),
        "the annual heatmap must grow with the card instead of leaving a fixed-size empty tail")
}

@MainActor
private func testAppRuntimeLoadsTokenActivityThroughAnIndependentAnnualRequest() async throws {
    let core = FakeCore(bootstrap: makeNormalBootstrap(), responses: makeResponses())
    let model = AppModel(
        runtime: AppRuntime(supervisor: FakeSupervisor(), clientFactory: { _ in core }))

    model.start()
    try await waitUntil("annual Token activity overview") {
        await MainActor.run {
            model.presentation?.tokenActivity.availability == .available
        }
    }

    let requests = await core.recordedTokenActivityRequests()
    try expect(requests.count == 1, "overview must issue one independent annual activity request")
    try expect(
        requests[0].granularity == "day"
            && requests[0].exactRange.endAtMs - requests[0].exactRange.startAtMs
                > 300 * 86_400_000,
        "the annual activity request must stay daily and independent of the selected overview range")
    _ = await model.shutdown()
}

@MainActor
private func testAppRuntimeKeepsTokenActivityFailureLocal() async throws {
    let core = FakeCore(bootstrap: makeNormalBootstrap(), responses: makeResponses())
    await core.setTokenActivityFailure(true)
    let model = AppModel(
        runtime: AppRuntime(supervisor: FakeSupervisor(), clientFactory: { _ in core }))

    model.start()
    try await waitUntil("locally unavailable Token activity") {
        await MainActor.run { model.presentation != nil }
    }

    try expect(
        model.presentation?.tokenActivity.availability == .unavailable,
        "a failed annual activity request must stay unavailable in its own card")
    try expect(
        model.presentation?.usageAvailable == true
            && model.presentation?.quotaAvailable == true
            && model.presentation?.isPartial == false,
        "annual activity failure must not downgrade quota or current-range usage")
    _ = await model.shutdown()
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

private func testOverviewLimitsMergedProjectRowsToFive() throws {
    let base = makeResponses()
    var projects = base.projects
    projects.items = (0..<5).map { index in
        var project = Codexpulse_Core_V1_ProjectItem()
        project.project.id = "project-\(index + 1)"
        project.project.displayName = "Project \(index + 1)"
        project.dimensionKey = "project-\(index + 1)"
        project.totals.totalTokens.value = 100 - Int64(index * 10)
        project.totals.totalTokens.unit = "tokens"
        return project
    }
    projects.matchedTotals.totalTokens.value = 465
    projects.matchedTotals.totalTokens.unit = "tokens"

    let presentation = OverviewPresentation(OverviewResponses(
        usage: base.usage,
        quota: base.quota,
        sessions: base.sessions,
        projects: projects,
        health: base.health
    ))

    try expect(
        presentation.projects.map(\.title)
            == ["Project 1", "Project 2", "Project 3", "Project 4", "其他"],
        "overview must rank the merged Other row and then keep at most five project rows")
    try expect(
        presentation.projects.last?.tokens == .known(65, unit: "tokens"),
        "the visible Other row must preserve the full remainder after the five-project query")
}

private func testOverviewLimitsHighConsumptionSessionsToFive() throws {
    let base = makeResponses()
    var sessions = base.sessions
    sessions.items = (0..<7).map { index in
        var session = Codexpulse_Core_V1_SessionItem()
        session.sessionID = "session-\(index + 1)"
        session.displayTitle = "Session \(index + 1)"
        session.totals.totalTokens.value = 700 - Int64(index * 100)
        session.totals.totalTokens.unit = "tokens"
        return session
    }

    let presentation = OverviewPresentation(OverviewResponses(
        usage: base.usage,
        quota: base.quota,
        sessions: sessions,
        projects: base.projects,
        health: base.health
    ))

    try expect(
        presentation.sessions.map(\.id)
            == ["session-1", "session-2", "session-3", "session-4", "session-5"],
        "overview must preserve Token order and expose at most five high-consumption sessions")
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
        usageRequests[0].includeActivityDistribution,
        "overview usage must include current-range activity distribution")
    try expect(
        sessionRequests[0].query.hasExactTimeRange,
        "overview sessions must use weekly quota exact range")
    try expect(
        sessionRequests[0].query.page.limit == 5
            && sessionRequests[0].query.sort.first?.field == "totalTokens"
            && sessionRequests[0].query.sort.first?.direction == "desc",
        "overview sessions must be a bounded Token-descending ranking")
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
private func testAppRuntimeKeepsAccountReadOptionalAndRetainsLastSuccess() async throws {
    let hangingCore = FakeCore(
        bootstrap: makeNormalBootstrap(),
        responses: makeResponses()
    )
    await hangingCore.setAccountDelay(.seconds(60))
    await hangingCore.setInvalidation(domain: "index", delay: .milliseconds(50))
    let hangingModel = AppModel(runtime: AppRuntime(
        supervisor: FakeSupervisor(),
        clientFactory: { _ in hangingCore }
    ))
    hangingModel.start()
    try await waitUntil("hanging account/read initial Overview") {
        await MainActor.run {
            hangingModel.presentation != nil && !hangingModel.isOverviewRefreshing
        }
    }
    try expect(
        hangingModel.presentation?.account.availability == .unavailable,
        "a hanging initial account/read must publish Overview with unavailable account semantics"
    )
    try await waitUntil("hanging account/read invalidation Overview") {
        let usageCalls = await hangingCore.recordedUsageRequests().count
        let accountCalls = await hangingCore.recordedCalls().filter { $0 == "account" }.count
        let isRefreshing = await MainActor.run { hangingModel.isOverviewRefreshing }
        return usageCalls >= 2 && accountCalls >= 2 && !isRefreshing
    }
    hangingModel.refreshOrRestart()
    try await waitUntil("hanging account/read manual Overview") {
        let usageCalls = await hangingCore.recordedUsageRequests().count
        let accountCalls = await hangingCore.recordedCalls().filter { $0 == "account" }.count
        let isRefreshing = await MainActor.run { hangingModel.isOverviewRefreshing }
        return usageCalls >= 3 && accountCalls >= 3 && !isRefreshing
    }
    _ = await hangingModel.shutdown()

    let failingCore = FakeCore(
        bootstrap: makeNormalBootstrap(),
        responses: makeResponses()
    )
    await failingCore.setAccountFailure(true)
    let failingRuntime = AppRuntime(
        supervisor: FakeSupervisor(),
        clientFactory: { _ in failingCore }
    )
    let failingModel = AppModel(runtime: failingRuntime)
    failingModel.start()
    try await waitUntil("optional account/read failure") {
        let completed = await failingCore.recordedCompletedAccountCalls()
        return await MainActor.run {
            completed >= 1 && failingModel.presentation != nil
                && !failingModel.isOverviewRefreshing
        }
    }
    try expect(
        failingModel.presentation?.account.availability == .unavailable,
        "an initial account/read failure must not downgrade the rest of Overview"
    )
    _ = await failingModel.shutdown()

    let core = FakeCore(
        bootstrap: makeNormalBootstrap(),
        responses: makeResponses()
    )
    let runtime = AppRuntime(
        supervisor: FakeSupervisor(),
        clientFactory: { _ in core }
    )
    let model = AppModel(runtime: runtime)
    model.start()
    try await waitUntil("initial account/read success") {
        await MainActor.run { model.presentation?.account.planText == "Pro" }
    }
    await core.setAccountFailure(true)
    await core.setAccountDelay(.milliseconds(100))
    model.refreshOrRestart()
    try await waitUntil("failed account/read does not block manual Overview") {
        let accountCalls = await core.recordedCalls().filter { $0 == "account" }.count
        let isRefreshing = await MainActor.run { model.isOverviewRefreshing }
        return accountCalls >= 2 && !isRefreshing
    }
    try expect(
        model.presentation?.account.planText == "Pro",
        "an in-flight optional account refresh must retain the last successful account"
    )
    try await waitUntil("failed account/read completes independently") {
        await core.recordedCompletedAccountCalls() >= 2
    }
    try expect(
        model.presentation?.account.planText == "Pro"
            && model.presentation?.account.emailText == "person@example.com",
        "a later account/read failure must retain the last successful display fields"
    )
    _ = await model.shutdown()
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

private func testQuotaWindowDisplayResolverDeduplicatesOnlyEquivalentWindows() throws {
    func window(
        kind: String,
        limitID: String,
        name: String? = nil,
        minutes: Int64,
        freshness: String,
        resetRemainingMS: Int64? = nil
    ) -> Codexpulse_Core_V1_CurrentWindow {
        var value = Codexpulse_Core_V1_CurrentWindow()
        value.windowKind = kind
        value.limitID = limitID
        if let name { value.limitName = name }
        value.windowMinutes = minutes
        value.remainingPercent = 50
        value.freshness = freshness
        if let resetRemainingMS { value.resetRemainingMs = resetRemainingMS }
        return value
    }

    let generalPrimary = window(
        kind: "primary", limitID: "codex", minutes: 10_080,
        freshness: "fresh", resetRemainingMS: 6 * 24 * 60 * 60 * 1_000
    )
    let sparkPrimary = window(
        kind: "primary", limitID: "codex_spark", name: "GPT-5.3-Codex-Spark",
        minutes: 10_080, freshness: "fresh",
        resetRemainingMS: 6 * 24 * 60 * 60 * 1_000
    )
    let generalSecondary = window(
        kind: "secondary", limitID: "codex", minutes: 10_080,
        freshness: "expired_unknown"
    )
    let sparkSecondary = window(
        kind: "secondary", limitID: "codex_spark", minutes: 10_080,
        freshness: "expired_unknown"
    )
    let deduplicated = QuotaWindowDisplayResolver.displayWindows([
        generalPrimary, sparkPrimary, generalSecondary, sparkSecondary,
    ])
    try expect(
        deduplicated.count == 2
            && deduplicated[0].windowKind == "primary"
            && deduplicated[0].limitID == "codex"
            && deduplicated[1].windowKind == "primary"
            && deduplicated[1].limitID == "codex_spark",
        "same quota and duration must resolve to the two trustworthy primary windows"
    )

    let fiveHour = window(
        kind: "primary", limitID: "codex", minutes: 300,
        freshness: "fresh", resetRemainingMS: 3_600_000
    )
    let sevenDay = window(
        kind: "secondary", limitID: "codex", minutes: 10_080,
        freshness: "fresh", resetRemainingMS: 6 * 24 * 60 * 60 * 1_000
    )
    let distinctPeriods = QuotaWindowDisplayResolver.displayWindows([fiveHour, sevenDay])
    try expect(
        distinctPeriods.count == 2
            && distinctPeriods.map(\.windowMinutes) == [300, 10_080],
        "different periods for the same quota must remain independently visible"
    )

    let weakPrimary = window(
        kind: "primary", limitID: "codex", minutes: 10_080,
        freshness: "suspicious"
    )
    let trustedSecondary = window(
        kind: "secondary", limitID: "codex", minutes: 10_080,
        freshness: "fresh", resetRemainingMS: 3_600_000
    )
    let preferred = QuotaWindowDisplayResolver.displayWindows([weakPrimary, trustedSecondary])
    try expect(
        preferred.count == 1 && preferred[0].windowKind == "secondary",
        "a trustworthy secondary window must beat an untrusted primary duplicate"
    )

    let secondaryOnly = QuotaWindowDisplayResolver.displayWindows([sevenDay])
    try expect(
        secondaryOnly.count == 1 && secondaryOnly[0].windowKind == "secondary",
        "a secondary window without an equivalent sibling must remain visible"
    )
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
        "status bar styles must preserve their persisted raw values"
    )
    try expect(
        StatusBarStyle.allCases.map(\.title)
            == ["基准圆环", "缺口圆环", "仪表弧"],
        "status bar style options must omit internal letter prefixes"
    )
    try expect(
        StatusBarStyle.resolve(storedValue: "open_ring_summary") == .openRingSummary,
        "approved status bar preference must survive reload"
    )
    for legacy in ["countdown", "battery", "meters", "rings", "unsupported", nil] as [String?] {
        try expect(
            StatusBarStyle.resolve(storedValue: legacy) == .ringSummary,
            "legacy or unknown status bar preference must use the baseline ring"
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

private func testStatusBarQuotaDataStateSeparatesRemainingColorFromTrust() throws {
    let fresh = StatusBarQuotaDataState(freshness: "fresh")
    let stale = StatusBarQuotaDataState(freshness: "stale")
    let suspicious = StatusBarQuotaDataState(freshness: "suspicious")
    let unavailable = StatusBarQuotaDataState(freshness: "expired_unknown")

    try expect(
        fresh.preservesRemainingColor
            && stale.preservesRemainingColor
            && suspicious.preservesRemainingColor,
        "known last-good quota values must retain their green/yellow/red remaining-level color"
    )
    try expect(
        stale.accessibilitySuffix == "，显示上次可信额度"
            && suspicious.accessibilitySuffix == "，新额度数据异常，显示上次可信额度",
        "stale and suspicious quota values must expose their independent trust state"
    )
    try expect(
        !unavailable.preservesRemainingColor
            && unavailable.accessibilitySuffix == "，额度数据可信度不可用",
        "expired or unknown quota values must remain fully unavailable"
    )
}

private func testStatusBarQuotaPresentationDescribesLastKnownGoodState() throws {
    let base = makeResponses()
    var quota = base.quota
    quota.current.windows[0].remainingPercent = 73
    quota.current.windows[0].freshness = "stale"
    let overview = OverviewPresentation(OverviewResponses(
        usage: base.usage,
        quota: quota,
        sessions: base.sessions,
        projects: base.projects,
        health: base.health
    ))
    guard let summary = StatusBarQuotaPresentation(overview) else {
        throw TestFailure.mismatch("stale last-known-good status bar summary was unavailable")
    }

    try expect(
        summary.dataState == .stale
            && summary.accessibilityLabel.hasSuffix("，显示上次可信额度"),
        "status bar summary must retain the value while naming its stale trust state"
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
    try expect(
        requests.usage.includeActivityDistribution,
        "overview usage must request the current-range activity distribution")
    try expect(requests.sessions.query.exactTimeRange.startAtMs == weekly.startAtMS, "session range start")
    try expect(requests.projects.query.exactTimeRange.startAtMs == weekly.startAtMS, "project range start")
    try expect(requests.sessions.query.page.limit == 5, "overview sessions must stay bounded")
    try expect(requests.projects.query.page.limit == 5, "overview projects must stay bounded")
    try expect(requests.sessions.query.sort.first?.field == "totalTokens", "sessions sort by Token")
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
private func testUnavailableRecoveryRefreshesOpenForegroundSurfaces() async throws {
    let core = FakeCore(bootstrap: makeNormalBootstrap(), responses: makeResponses())
    await core.prepareUnavailableOverviewRecovery()
    await core.setOverviewDelay(.milliseconds(80))
    let model = AppModel(runtime: AppRuntime(
        supervisor: FakeSupervisor(),
        clientFactory: { _ in core }
    ))
    let mainWindow = ForegroundSurfaceRecorder()
    let statusPopover = ForegroundSurfaceRecorder()
    var cancellables: Set<AnyCancellable> = []
    model.$state.sink { state in
        Task { await mainWindow.append(state) }
    }.store(in: &cancellables)
    model.$state.sink { state in
        Task { await statusPopover.append(state) }
    }.store(in: &cancellables)

    model.start()
    try await waitUntil("initial unavailable quota is in flight") {
        await core.overviewBarrierWaiterCount() == 1
    }
    await core.releaseNextOverviewBarrierWaiter()
    try await waitUntil("initial unavailable Overview sections are in flight") {
        await core.overviewBarrierWaiterCount() == 5
    }
    await core.publishOverviewInvalidations(count: 3, recovered: true)
    await core.releaseOverviewBarrier()

    try await waitUntil("main window observes unavailable before recovery") {
        await mainWindow.observedUnavailable()
    }
    try await waitUntil("status Popover observes unavailable before recovery") {
        await statusPopover.observedUnavailable()
    }
    try await waitUntil("main window receives recovered Overview") {
        await mainWindow.latestOverviewTimestamp() != nil
    }
    try await waitUntil("status Popover receives recovered Overview") {
        await statusPopover.latestOverviewTimestamp() != nil
    }
    let stats = await core.overviewRecoveryStats()
    try expect(
        stats.usageCalls == 2,
        "coalesced recovery invalidations must schedule exactly one follow-up Overview refresh"
    )
    try expect(
        stats.completedUsageCalls == 2,
        "recovered Overview follow-up must complete before foreground success is asserted"
    )
    try expect(
        stats.maximumConcurrentUsageCalls == 1,
        "recovery refresh must not overlap the unavailable Overview request"
    )
    try expect(model.presentation != nil, "recovered Overview must become the shared AppModel truth")
    withExtendedLifetime(cancellables) {}
    _ = await model.shutdown()
}

@MainActor
private func testRecoveryDuringDisconnectedStreamRefreshesAfterReconnectWithoutReplay() async throws {
    let core = FakeCore(bootstrap: makeNormalBootstrap(), responses: makeResponses())
    await core.prepareUnavailableOverviewRecovery()
    await core.prepareReconnectWithoutInvalidationReplay()
    let model = AppModel(runtime: AppRuntime(
        supervisor: FakeSupervisor(),
        clientFactory: { _ in core }
    ))
    let mainWindow = ForegroundSurfaceRecorder()
    let statusPopover = ForegroundSurfaceRecorder()
    var cancellables: Set<AnyCancellable> = []
    model.$state.sink { state in
        Task { await mainWindow.append(state) }
    }.store(in: &cancellables)
    model.$state.sink { state in
        Task { await statusPopover.append(state) }
    }.store(in: &cancellables)

    model.start()
    try await waitUntil("disconnected recovery quota is in flight") {
        await core.overviewBarrierWaiterCount() == 1
    }
    await core.releaseNextOverviewBarrierWaiter()
    try await waitUntil("disconnected recovery Overview sections are in flight") {
        await core.overviewBarrierWaiterCount() == 5
    }
    await core.releaseOverviewBarrier()
    try await waitUntil("disconnected recovery starts unavailable") {
        let stats = await core.overviewRecoveryStats()
        let mainUnavailable = await mainWindow.observedUnavailable()
        let popoverUnavailable = await statusPopover.observedUnavailable()
        return stats.completedUsageCalls == 1 && mainUnavailable && popoverUnavailable
    }
    try await waitUntil("initial invalidation stream can disconnect") {
        await core.initialStreamDisconnectWaiterCount() == 1
    }

    await core.disconnectInitialInvalidationStream()
    try await waitUntil("replacement invalidation stream waits before ready") {
        let streamCalls = await core.invalidationStreamCallCount()
        let readinessWaiters = await core.reconnectStreamReadinessWaiterCount()
        return streamCalls == 2 && readinessWaiters == 1
    }
    await core.setOverviewFailure(false)
    await core.releaseReconnectStreamReadiness()

    try await waitUntil("main window refreshes after reconnect without replay") {
        await mainWindow.latestOverviewTimestamp() != nil
    }
    try await waitUntil("status Popover refreshes after reconnect without replay") {
        await statusPopover.latestOverviewTimestamp() != nil
    }
    let stats = await core.overviewRecoveryStats()
    try expect(
        stats.usageCalls == 2 && stats.completedUsageCalls == 2,
        "stream ready after a disconnected recovery must schedule one completed authoritative refresh"
    )
    try expect(
        stats.maximumConcurrentUsageCalls == 1,
        "reconnect recovery refresh must remain serial"
    )
    try expect(model.presentation != nil, "reconnect recovery must update shared AppModel truth")
    withExtendedLifetime(cancellables) {}
    _ = await model.shutdown()
}

@MainActor
private func testInitialInFlightReconnectQueuesOneRecoveryRefresh() async throws {
    let core = FakeCore(bootstrap: makeNormalBootstrap(), responses: makeResponses())
    await core.prepareUnavailableOverviewRecovery()
    await core.prepareReconnectWithoutInvalidationReplay()
    let model = AppModel(runtime: AppRuntime(
        supervisor: FakeSupervisor(),
        clientFactory: { _ in core }
    ))
    let mainWindow = ForegroundSurfaceRecorder()
    let statusPopover = ForegroundSurfaceRecorder()
    var cancellables: Set<AnyCancellable> = []
    model.$state.sink { state in
        Task { await mainWindow.append(state) }
    }.store(in: &cancellables)
    model.$state.sink { state in
        Task { await statusPopover.append(state) }
    }.store(in: &cancellables)

    model.start()
    try await waitUntil("initial unavailable quota is in flight before reconnect") {
        await core.overviewBarrierWaiterCount() == 1
    }
    await core.releaseNextOverviewBarrierWaiter()
    try await waitUntil("initial unavailable Overview sections are in flight before reconnect") {
        await core.overviewBarrierWaiterCount() == 5
    }
    try await waitUntil("initial stream can disconnect during the first Overview") {
        await core.initialStreamDisconnectWaiterCount() == 1
    }
    await core.disconnectInitialInvalidationStream()
    try await waitUntil("replacement stream waits while the first Overview remains in flight") {
        await core.reconnectStreamReadinessWaiterCount() == 1
    }
    await core.setOverviewFailure(false)
    await core.releaseReconnectStreamReadiness()
    try await waitUntil("replacement stream becomes ready before the first Overview completes") {
        await core.readyInvalidationStreamCallCount() == 2
    }
    await core.releaseOverviewBarrier()

    try await waitUntil("first captured Overview failure reaches the main window") {
        await mainWindow.observedUnavailable()
    }
    try await waitUntil("first captured Overview failure reaches the status Popover") {
        await statusPopover.observedUnavailable()
    }
    try await waitUntil("reconnect follow-up updates the main window without replay") {
        await mainWindow.latestOverviewTimestamp() != nil
    }
    try await waitUntil("reconnect follow-up updates the status Popover without replay") {
        await statusPopover.latestOverviewTimestamp() != nil
    }
    let stats = await core.overviewRecoveryStats()
    try expect(
        stats.usageCalls == 2 && stats.completedUsageCalls == 2,
        "reconnect after the first Overview starts must complete one serial authoritative follow-up"
    )
    try expect(
        stats.maximumConcurrentUsageCalls == 1,
        "reconnect recovery must not overlap the first Overview request"
    )
    try expect(
        model.presentation != nil,
        "the reconnect follow-up must publish successful shared AppModel truth"
    )
    withExtendedLifetime(cancellables) {}
    _ = await model.shutdown()
}

private func testReadyJitterBeforeInitialCoreQueryCompletesOneOverview() async throws {
    let core = FakeCore(bootstrap: makeNormalBootstrap(), responses: makeResponses())
    await core.prepareReconnectWithoutInvalidationReplay()
    await core.setReconnectReadySignalCount(2)
    let loadingBarrier = LoadingStateBarrier()
    let runtime = AppRuntime(supervisor: FakeSupervisor(), clientFactory: { _ in core })
    await runtime.setStateSink { state in
        await loadingBarrier.accept(state)
    }

    let start = Task { await runtime.start() }
    try await waitUntil("loading state sink blocks before the first Core query") {
        await loadingBarrier.isBlocking()
    }
    var stats = await core.overviewRecoveryStats()
    var overviewCoreCalls = await core.overviewCoreCallCount()
    try expect(
        stats.usageCalls == 0 && overviewCoreCalls == 0,
        "blocking the loading sink must keep the first Core Overview query unstarted"
    )
    try await waitUntil("initial stream can disconnect during the loading-state window") {
        await core.initialStreamDisconnectWaiterCount() == 1
    }
    await core.disconnectInitialInvalidationStream()
    try await waitUntil("replacement stream waits before repeated ready signals") {
        await core.reconnectStreamReadinessWaiterCount() == 1
    }
    await core.releaseReconnectStreamReadiness()
    try await waitUntil("replacement and repeated ready signals arrive before the first Core query") {
        await core.readyInvalidationStreamCallCount() == 3
    }
    stats = await core.overviewRecoveryStats()
    overviewCoreCalls = await core.overviewCoreCallCount()
    try expect(
        stats.usageCalls == 0 && overviewCoreCalls == 0,
        "ready jitter while loading is blocked must not start a Core query through reentrancy"
    )

    await loadingBarrier.release()
    await start.value
    stats = await core.overviewRecoveryStats()
    try expect(
        stats.usageCalls == 1 && stats.completedUsageCalls == 1,
        "ready jitter before Core admission must complete exactly one Overview"
    )
    try expect(
        stats.maximumConcurrentUsageCalls == 1,
        "ready jitter before Core admission must preserve Overview single-flight"
    )
    _ = await runtime.shutdown()
}

private func testInvalidationDuringLoadingAdmissionCompletesOneOverview() async throws {
    let core = FakeCore(bootstrap: makeNormalBootstrap(), responses: makeResponses())
    let loadingBarrier = LoadingStateBarrier()
    let runtime = AppRuntime(supervisor: FakeSupervisor(), clientFactory: { _ in core })
    await runtime.setStateSink { state in await loadingBarrier.accept(state) }

    let start = Task { await runtime.start() }
    try await waitUntil("loading blocks before invalidation admission") {
        await loadingBarrier.isBlocking()
    }
    var overviewCoreCalls = await core.overviewCoreCallCount()
    try expect(
        overviewCoreCalls == 0,
        "loading barrier must precede every Overview Core query"
    )

    let invalidationCompleted = CompletionFlag()
    let invalidation = Task {
        await core.publishOverviewInvalidations(count: 1, recovered: false)
        await invalidationCompleted.markCompleted()
    }
    try await waitUntil("invalidation either coalesces or reaches the blocked loading sink") {
        let entryCount = await loadingBarrier.entryCount()
        let completed = await invalidationCompleted.isCompleted()
        return entryCount == 2 || completed
    }
    overviewCoreCalls = await core.overviewCoreCallCount()
    try expect(
        overviewCoreCalls == 0,
        "invalidation during loading admission must not start a Core query early"
    )

    await loadingBarrier.release()
    await invalidation.value
    await start.value
    let stats = await core.overviewRecoveryStats()
    try expect(
        stats.usageCalls == 1 && stats.completedUsageCalls == 1,
        "invalidation before Core admission must share exactly one Overview"
    )
    try expect(
        stats.maximumConcurrentUsageCalls == 1,
        "invalidation before Core admission must preserve single-flight"
    )
    _ = await runtime.shutdown()
}

private func testSecondManualRefreshDuringLoadingAdmissionCompletesOneOverview() async throws {
    let core = FakeCore(bootstrap: makeNormalBootstrap(), responses: makeResponses())
    let loadingBarrier = LoadingStateBarrier()
    let runtime = AppRuntime(supervisor: FakeSupervisor(), clientFactory: { _ in core })
    await runtime.setStateSink { state in await loadingBarrier.accept(state) }

    let start = Task { await runtime.start() }
    try await waitUntil("loading blocks before the second manual refresh") {
        await loadingBarrier.isBlocking()
    }
    var overviewCoreCalls = await core.overviewCoreCallCount()
    try expect(
        overviewCoreCalls == 0,
        "manual refresh race must begin before every Overview Core query"
    )

    let secondRefreshCompleted = CompletionFlag()
    let secondRefresh = Task {
        await runtime.refresh()
        await secondRefreshCompleted.markCompleted()
    }
    try await waitUntil("second refresh either coalesces or reaches the blocked loading sink") {
        let entryCount = await loadingBarrier.entryCount()
        let completed = await secondRefreshCompleted.isCompleted()
        return entryCount == 2 || completed
    }
    overviewCoreCalls = await core.overviewCoreCallCount()
    try expect(
        overviewCoreCalls == 0,
        "second refresh during loading admission must not start a Core query early"
    )

    await loadingBarrier.release()
    await secondRefresh.value
    await start.value
    let stats = await core.overviewRecoveryStats()
    try expect(
        stats.usageCalls == 1 && stats.completedUsageCalls == 1,
        "concurrent manual refresh before Core admission must share exactly one Overview"
    )
    try expect(
        stats.maximumConcurrentUsageCalls == 1,
        "concurrent manual refresh before Core admission must preserve single-flight"
    )
    _ = await runtime.shutdown()
}

private func testRangeChangeDuringLoadingAdmissionReplacesOldRefresh() async throws {
    let core = FakeCore(bootstrap: makeNormalBootstrap(), responses: makeResponses())
    let loadingBarrier = LoadingStateBarrier()
    let runtime = AppRuntime(supervisor: FakeSupervisor(), clientFactory: { _ in core })
    await runtime.setStateSink { state in await loadingBarrier.accept(state) }

    let start = Task { await runtime.start() }
    try await waitUntil("loading blocks before the range change") {
        await loadingBarrier.isBlocking()
    }
    let rangeRefreshCompleted = CompletionFlag()
    let rangeRefresh = Task {
        await runtime.refresh(range: .sevenDays)
        await rangeRefreshCompleted.markCompleted()
    }
    try await waitUntil("range refresh either reserves or reaches the blocked loading sink") {
        let entryCount = await loadingBarrier.entryCount()
        let completed = await rangeRefreshCompleted.isCompleted()
        return entryCount == 2 || completed
    }
    let overviewCoreCalls = await core.overviewCoreCallCount()
    try expect(
        overviewCoreCalls == 0,
        "range change during loading admission must cancel before Core query"
    )

    await loadingBarrier.release()
    await rangeRefresh.value
    await start.value
    let stats = await core.overviewRecoveryStats()
    let overviewBatches = await core.overviewBatchCallCount()
    try expect(
        overviewBatches == 1 && stats.usageCalls == 2 && stats.completedUsageCalls == 2,
        "range change must replace the old admission with exactly one Overview"
    )
    _ = await runtime.shutdown()
}

private func testCancelDuringLoadingAdmissionPreventsCoreQuery() async throws {
    let core = FakeCore(bootstrap: makeNormalBootstrap(), responses: makeResponses())
    let loadingBarrier = LoadingStateBarrier()
    let recorder = StateRecorder()
    let runtime = AppRuntime(supervisor: FakeSupervisor(), clientFactory: { _ in core })
    await runtime.setStateSink { state in
        await recorder.append(state)
        await loadingBarrier.accept(state)
    }

    let start = Task { await runtime.start() }
    try await waitUntil("loading blocks before cancel") {
        await loadingBarrier.isBlocking()
    }
    await runtime.cancelRefresh()
    var overviewCoreCalls = await core.overviewCoreCallCount()
    try expect(
        overviewCoreCalls == 0,
        "cancel during loading admission must happen before every Core query"
    )

    await loadingBarrier.release()
    await start.value
    overviewCoreCalls = await core.overviewCoreCallCount()
    try expect(
        overviewCoreCalls == 0,
        "cancelled loading admission must never start an Overview query"
    )
    let phases = await recorder.snapshot()
    try expect(phases.last == "cancelled", "late loading completion must not overwrite cancel")
    try expect(
        !phases.contains("normal") && !phases.contains("partial") && !phases.contains("unavailable"),
        "cancelled loading admission must not publish fresh or unavailable data"
    )
    _ = await runtime.shutdown()
}

private func testSleepDuringLoadingAdmissionPreventsCoreQuery() async throws {
    let supervisor = FakeSupervisor()
    let core = FakeCore(bootstrap: makeNormalBootstrap(), responses: makeResponses())
    let loadingBarrier = LoadingStateBarrier()
    let recorder = StateRecorder()
    let runtime = AppRuntime(supervisor: supervisor, clientFactory: { _ in core })
    await runtime.setStateSink { state in
        await recorder.append(state)
        await loadingBarrier.accept(state)
    }

    let start = Task { await runtime.start() }
    try await waitUntil("loading blocks before sleep") {
        await loadingBarrier.isBlocking()
    }
    await runtime.prepareForSleep()
    var overviewCoreCalls = await core.overviewCoreCallCount()
    try expect(
        overviewCoreCalls == 0,
        "sleep during loading admission must happen before every Core query"
    )

    await loadingBarrier.release()
    await start.value
    overviewCoreCalls = await core.overviewCoreCallCount()
    try expect(
        overviewCoreCalls == 0,
        "sleeping runtime must not admit the stale loading refresh"
    )
    let phases = await recorder.snapshot()
    try expect(
        !phases.contains("normal") && !phases.contains("partial") && !phases.contains("unavailable"),
        "sleeping runtime must not publish a late Overview result"
    )
    let counts = await supervisor.counts()
    try expect(counts == (1, 0), "sleep admission cancellation must preserve the Helper")
    _ = await runtime.shutdown()
}

private func testShutdownDuringLoadingAdmissionKeepsStoppedTerminal() async throws {
    let core = FakeCore(bootstrap: makeNormalBootstrap(), responses: makeResponses())
    let loadingBarrier = LoadingStateBarrier()
    let recorder = StateRecorder()
    let runtime = AppRuntime(supervisor: FakeSupervisor(), clientFactory: { _ in core })
    await runtime.setStateSink { state in
        await recorder.append(state)
        await loadingBarrier.accept(state)
    }

    let start = Task { await runtime.start() }
    try await waitUntil("loading blocks before shutdown") {
        await loadingBarrier.isBlocking()
    }
    let shutdownOutcome = await runtime.shutdown()
    try expect(shutdownOutcome == .clean, "loading admission shutdown must remain clean")
    var overviewCoreCalls = await core.overviewCoreCallCount()
    try expect(
        overviewCoreCalls == 0,
        "shutdown during loading admission must happen before every Core query"
    )
    var phases = await recorder.snapshot()
    try expect(phases.last == "stopped", "shutdown must publish stopped before releasing loading")

    await loadingBarrier.release()
    await start.value
    overviewCoreCalls = await core.overviewCoreCallCount()
    try expect(
        overviewCoreCalls == 0,
        "stopped runtime must not start a query from stale loading admission"
    )
    phases = await recorder.snapshot()
    try expect(phases.last == "stopped", "late loading completion must not overwrite stopped")
}

@MainActor
private func testUnavailableRecoveryFailureDoesNotPublishSuccess() async throws {
    let core = FakeCore(bootstrap: makeNormalBootstrap(), responses: makeResponses())
    await core.prepareUnavailableOverviewRecovery()
    let model = AppModel(runtime: AppRuntime(
        supervisor: FakeSupervisor(),
        clientFactory: { _ in core }
    ))
    let foreground = ForegroundSurfaceRecorder()
    var cancellables: Set<AnyCancellable> = []
    model.$state.sink { state in
        Task { await foreground.append(state) }
    }.store(in: &cancellables)

    model.start()
    try await waitUntil("failed recovery quota is in flight") {
        await core.overviewBarrierWaiterCount() == 1
    }
    await core.releaseNextOverviewBarrierWaiter()
    try await waitUntil("failed recovery Overview sections are in flight") {
        await core.overviewBarrierWaiterCount() == 5
    }
    await core.publishOverviewInvalidations(count: 2, recovered: false)
    await core.releaseOverviewBarrier()

    try await waitUntil("failed recovery follow-up completes") {
        let stats = await core.overviewRecoveryStats()
        return stats.completedUsageCalls == 2
    }
    try await waitUntil("failed recovery publishes its second unavailable result") {
        await foreground.unavailableCountValue() == 2
    }
    try await sleepForTest(.milliseconds(100))
    let stats = await core.overviewRecoveryStats()
    try expect(
        stats.usageCalls == 2 && stats.completedUsageCalls == 2,
        "failed recovery must stop after exactly one completed follow-up Overview refresh"
    )
    try expect(
        stats.maximumConcurrentUsageCalls == 1,
        "failed recovery follow-up must remain serial"
    )
    let foregroundOverviewTimestamp = await foreground.latestOverviewTimestamp()
    try expect(
        model.presentation == nil && foregroundOverviewTimestamp == nil,
        "failed recovery must not publish an Overview or reuse unavailable data as fresh"
    )
    withExtendedLifetime(cancellables) {}
    _ = await model.shutdown()
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
    try await sleepForTest(.milliseconds(40))
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
    try await sleepForTest(.milliseconds(20))
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
    try await sleepForTest(.milliseconds(10))
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
    try await sleepForTest(.milliseconds(20))
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

private func testSleepDuringInvalidationReadinessPreservesRuntimeUntilWake() async throws {
    let supervisor = FakeSupervisor()
    let core = FakeCore(bootstrap: makeNormalBootstrap(), responses: makeResponses())
    await core.prepareInitialStreamReadinessBarrier()
    let recorder = StateRecorder()
    let runtime = AppRuntime(supervisor: supervisor, clientFactory: { _ in core })
    await runtime.setStateSink { state in await recorder.append(state) }

    let start = Task { await runtime.start() }
    try await waitUntil("initial stream readiness is blocked") {
        await core.initialStreamReadinessWaiterCount() == 1
    }
    await runtime.prepareForSleep()
    await core.releaseInitialStreamReadinessBarrier()
    await start.value

    let sleepingCounts = await supervisor.counts()
    try expect(
        sleepingCounts == (1, 0),
        "sleep during initial stream readiness must preserve the existing Helper runtime"
    )
    var calls = await core.recordedCalls()
    try expect(
        calls.contains("lifecycle:system_will_sleep"),
        "readiness-window sleep must notify the existing Helper"
    )
    try expect(!calls.contains("usage"), "readiness-window sleep must defer the first Overview")

    await runtime.resumeAfterWake()
    try await waitUntil("wake after readiness-window sleep completes first Overview") {
        let stats = await core.overviewRecoveryStats()
        return stats.completedUsageCalls == 1
    }
    calls = await core.recordedCalls()
    try expect(
        calls.contains("lifecycle:system_did_wake"),
        "wake must resume the preserved stream lifecycle"
    )
    let phases = await recorder.snapshot()
    try expect(phases.last == "normal", "wake must resume the stream before the deferred first read")
    let awakeCounts = await supervisor.counts()
    try expect(awakeCounts == (1, 0), "wake must reuse the original Helper runtime")
    _ = await runtime.shutdown()
}

private func testSuspendingReadinessPastTimeoutPreservesRuntimeUntilWake() async throws {
    let supervisor = FakeSupervisor()
    let core = FakeCore(bootstrap: makeNormalBootstrap(), responses: makeResponses())
    await core.prepareInitialStreamReadinessBarrier()
    await core.prepareSystemWillSleepBarrier()
    let recorder = StateRecorder()
    let runtime = AppRuntime(supervisor: supervisor, clientFactory: { _ in core })
    await runtime.setStateSink { state in await recorder.append(state) }

    let start = Task { await runtime.start() }
    try await waitUntil("initial stream readiness waits before suspending") {
        await core.initialStreamReadinessWaiterCount() == 1
    }
    let sleep = Task { await runtime.prepareForSleep() }
    try await waitUntil("systemWillSleep lifecycle remains in flight") {
        await core.systemWillSleepWaiterCount() == 1
    }
    await core.releaseInitialStreamReadinessBarrier()
    try await sleepForTest(.milliseconds(5_200))
    let countsPastReadinessTimeout = await supervisor.counts()

    await core.releaseSystemWillSleepBarrier()
    await sleep.value
    await start.value
    try expect(
        countsPastReadinessTimeout == (1, 0),
        "suspending past stream readiness timeout must preserve the existing Helper"
    )
    let sleepingCounts = await supervisor.counts()
    try expect(sleepingCounts == (1, 0), "completed sleep transition must not stop the Helper")

    await runtime.resumeAfterWake()
    try await waitUntil("wake after prolonged suspending completes the first Overview") {
        let stats = await core.overviewRecoveryStats()
        return stats.completedUsageCalls == 1
    }
    let calls = await core.recordedCalls()
    try expect(
        calls.contains("lifecycle:system_did_wake"),
        "wake must resume the preserved stream after prolonged suspending"
    )
    let phases = await recorder.snapshot()
    try expect(phases.last == "normal", "wake must resume the stream before the first Overview read")
    let awakeCounts = await supervisor.counts()
    try expect(awakeCounts == (1, 0), "wake must reuse the original Helper runtime")
    _ = await runtime.shutdown()
}

private func testWakeBeforeSleepLifecycleCompletionResumesRuntime() async throws {
    let supervisor = FakeSupervisor()
    let core = FakeCore(bootstrap: makeNormalBootstrap(), responses: makeResponses())
    await core.prepareInitialStreamReadinessBarrier()
    await core.prepareSystemWillSleepBarrier()
    let recorder = StateRecorder()
    let runtime = AppRuntime(supervisor: supervisor, clientFactory: { _ in core })
    await runtime.setStateSink { state in await recorder.append(state) }

    let start = Task { await runtime.start() }
    try await waitUntil("initial stream readiness is blocked before the sleep race") {
        await core.initialStreamReadinessWaiterCount() == 1
    }
    let sleep = Task { await runtime.prepareForSleep() }
    try await waitUntil("systemWillSleep lifecycle is blocked") {
        await core.systemWillSleepWaiterCount() == 1
    }

    await runtime.resumeAfterWake()
    var calls = await core.recordedCalls()
    try expect(
        !calls.contains("lifecycle:system_did_wake"),
        "wake must wait until the in-flight systemWillSleep transition reaches sleeping"
    )
    let statsBeforeWakeRecovery = await core.overviewRecoveryStats()
    try expect(
        statsBeforeWakeRecovery.usageCalls == 0,
        "the first Overview must remain deferred while sleep transition is incomplete"
    )

    await core.releaseInitialStreamReadinessBarrier()
    await start.value
    await core.releaseSystemWillSleepBarrier()
    await sleep.value

    try await waitUntil("deferred wake reconnects the invalidation stream") {
        await core.invalidationStreamCallCount() == 2
    }
    try await waitUntil("deferred wake completes one authoritative Overview") {
        let stats = await core.overviewRecoveryStats()
        return stats.completedUsageCalls == 1
    }
    calls = await core.recordedCalls()
    guard
        let willSleepIndex = calls.firstIndex(of: "lifecycle:system_will_sleep"),
        let didWakeIndex = calls.firstIndex(of: "lifecycle:system_did_wake")
    else {
        throw TestFailure.mismatch("sleep/wake lifecycle calls were not both delivered")
    }
    try expect(
        willSleepIndex < didWakeIndex,
        "an early wake must be delivered after the blocked sleep lifecycle RPC completes"
    )
    let stats = await core.overviewRecoveryStats()
    try expect(
        stats.usageCalls == 1 && stats.completedUsageCalls == 1,
        "the deferred wake must perform the first Overview exactly once"
    )
    let counts = await supervisor.counts()
    try expect(counts == (1, 0), "the sleep race must preserve the original Helper runtime")
    let phases = await recorder.snapshot()
    try expect(phases.last == "normal", "the deferred wake must not leave runtime sleeping")
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
    try await sleepForTest(.milliseconds(20))
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
        try testReferencePriceFormattingPreservesPrecisionAndUnknown()
        try testQuotaUsageShowsIndependentReferencePriceCatalogAndBillingBoundary()
        try testOverviewMergesAllOtherProjectUsage()
        try testOverviewLimitsMergedProjectRowsToFive()
        try testOverviewLimitsHighConsumptionSessionsToFive()
        try testWeeklyProjectRankingFailureStaysLocal()
        try testOverviewUsesOneNavigationAndARealTrendChart()
        try testToolbarSeparatesCurrentReloadFromGlobalReload()
        try testWeeklyOverviewTrendUsesDailyAxisAndRangeCopy()
        try testUsageChartStacksModelsWithLocalizedHoverDetails()
        try testSessionTrendPresentationAdaptsGranularityAndReportingTimezone()
        try testSessionAndProjectDetailsShareResponsiveThirdWidthSplit()
        try await testFeatureRefreshRetainsNativeContentIdentity()
        try testEveryTokenChartUsesLocalizedAxisAndAccessibilityUnits()
        try testEveryTokenSurfaceUsesInputOutputBreakdown()
        try testStatusPopoverShowsLocalizedModelDailyTrend()
        try testPopoverHeaderMatchesSessionNestLayoutAndNeutralFocus()
        try testPopoverAccountSummaryShowsSessionNestAccountFieldsOnly()
        try testPopoverAccountSummaryDistinguishesEmptyAndUnavailableData()
        try testPopoverScreenshotClipboardTextHidesAccountAndPlan()
        try testPopoverProjectActionUsesExactPublicRepositoryURL()
        try testPopoverProjectActionMakesSystemOpenFailureVisible()
        try testPopoverDoesNotRetainLegacyReleaseLinkUpdater()
        try testPopoverCopyActionStopsWhenSafeScreenshotIsUnavailable()
        try testPopoverCopyActionReportsClipboardFailureWithoutRawFallback()
        try testPopoverCopyActionWritesOneSafeImageAndTextPayload()
        try testPopoverPasteboardWriterUsesOneRealItemForTextAndPNG()
        try testPopoverFullPageCaptureUsesLiveScrollableViewAndRestoresOffset()
        try testPopoverScreenshotUsesLiveViewAndRedactsAccountCapsule()
        try testPopoverWeeklyTrendDoesNotFollowOverviewRange()
        try testTrendSelectionSnapsToNearestRealPoint()
        try testSidebarSettingsUsesSystemRowSpacing()
        try testSettingsOverviewRangeFallbackMatchesProductOptions()
        try testSettingsExplainsAutomaticDefaultHome()
        try testLoginItemDefaultsToSystemStatusWithoutRegistration()
        try testLoginItemRegistrationAndExternalStatusDrift()
        try testLoginItemRequiresApprovalUsesRegisteredIntent()
        try testLoginItemFailuresRestoreAuthoritativeState()
        try testLoginItemServiceManagementWiringAndCopy()
        try testStatusPillUsesProductCopy()
        try testStatusItemRefreshReadsCommittedState()
        try testApplicationMenuRegistersNativeCommands()
        try testApplicationMenuSettingsShowsTheExistingSettingsPage()
        try testApplicationMenuUpdateUsesSparkleAndObservedHelperPolicy()
        try testSparkleStartsWithoutRacingTheHelperOwnedPolicy()
        try testUpdateInstallationBlocksTerminationAfterUncleanHelperShutdown()
        try testApplicationMenuIsSkippedForSmokeLaunches()
        try testInitialWindowUsesScreenAwarePreferredLayout()
        try testNativeSmokeForcesOverviewTransitionLast()
        try testPopoverUsesWeeklyProjectTokenRanking()
        try testSettingsIntervalsUseAuthoritativeBounds()
        try testOverviewRangeIncludesQuotaWeek()
        try testRequestFactoryAndPresentation()
        try testTokenActivityRequestUsesAnIndependentRollingYear()
        try testQuotaWindowDisplayResolverDeduplicatesOnlyEquivalentWindows()
        try testOverviewActivityPresentationBuildsTimelineAndCompleteHeatmap()
        try testOverviewActivityAxisTicksKeepDateOnlyAtDayBoundaries()
        try testOverviewActivityTimelineResolverSelectsContainingBucketThenNearest()
        try testOverviewActivityTimelineResolverInsetsBarsSymmetrically()
        try testTokenActivityPresentationBuildsCalendarAndStreakStatistics()
        try testTokenActivityPresentationSummarizesKnownLocalFactsFromPartialResponse()
        try testTokenActivityPresentationAnchorsToTheResponseRange()
        try testTokenActivityPresentationRejectsFactsOutsideItsVisibleYear()
        try testTokenActivityCalendarAlignsWeeksAndMonthLabels()
        try testTokenActivityCalendarSeparatesUnknownZeroAndRelativeIntensity()
        try testTokenActivityCardPresentsFiveBoundedMetricsAndDayDetails()
        try testTokenActivityCardLabelsReconciledPartialFactsAsLocalData()
        try testTokenActivityHoverStateTracksOnlyTheCurrentCell()
        try testTokenActivityHeatmapUsesTheAvailableCardWidth()
        try await testAppRuntimeLoadsTokenActivityThroughAnIndependentAnnualRequest()
        try await testAppRuntimeKeepsTokenActivityFailureLocal()
        try await testAppRuntimeUsesWeeklyQuotaRangeForOverview()
        try await testAppRuntimeKeepsAccountReadOptionalAndRetainsLastSuccess()
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
        try testStatusBarQuotaDataStateSeparatesRemainingColorFromTrust()
        try testStatusBarQuotaPresentationDescribesLastKnownGoodState()
        try testMainWindowLayoutPrefersFullOverviewWithoutLeavingTheScreen()
        try testWeeklyQuotaUsageRequestUsesExactWindowStart()
        try testOverviewRangeResolutionDrivesEveryContentRequest()
        try testFeatureRequestsStateAndMerge()
        try testSettingsRevisionRequest()
        try testUpdateChannelsMapToSparkleAllowedChannels()
        try testSparkleBundleConfigurationRequiresHTTPSAndEd25519Key()
        try await MainActor.run { try testSparkleUpdaterAppliesPolicyAndTracksInstallation() }
        try await testUpdateInstallationRequiresCleanClientRestartShutdown()
        try await testAppModelPublishesConfiguredUpdatePolicyForSparkle()
        try testLaunchConfigurationBoundaries()
        try testLaunchConfigurationUsesPersistentProductDefaults()
        try await testPricingCatalogLoadsWithoutUsageModels()
        try await testFeatureGenerationPreventsStaleOverwrite()
        try await testInvalidationRefreshesActivePage()
        try await testIndexInvalidationRefreshesSelectedSessionDetail()
        try await testForegroundRecoveryRefreshesSelectedSessionOnce()
        try await testLoadingActiveSessionsRetriesUnavailableSelectedDetail()
        try await testLoadingActiveProjectsRetriesUnavailableSelectedDetail()
        try await testRefreshingProjectsRetriesUnavailableSelectedDetail()
        try await testRefreshingSourcesAndJobsRetriesUnavailableSelectedDetails()
        try await testLoadingActiveSourcesAndJobsRetriesUnavailableSelectedDetails()
        try await testRefreshingLocalStatusRetriesUnavailableSelectedHealthDetail()
        try await testLoadingActiveLocalStatusRetriesUnavailableSelectedHealthDetail()
        try await testRefreshAllRetriesEveryUnavailableSelectedDetail()
        try await testRefreshAllReportsGlobalProgressUntilEveryReadCompletes()
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
        try await testUnavailableRecoveryRefreshesOpenForegroundSurfaces()
        try await testSleepDuringInvalidationReadinessPreservesRuntimeUntilWake()
        try await testWakeBeforeSleepLifecycleCompletionResumesRuntime()
        try await testReadyJitterBeforeInitialCoreQueryCompletesOneOverview()
        try await testInvalidationDuringLoadingAdmissionCompletesOneOverview()
        try await testSecondManualRefreshDuringLoadingAdmissionCompletesOneOverview()
        try await testRangeChangeDuringLoadingAdmissionReplacesOldRefresh()
        try await testCancelDuringLoadingAdmissionPreventsCoreQuery()
        try await testSleepDuringLoadingAdmissionPreventsCoreQuery()
        try await testShutdownDuringLoadingAdmissionKeepsStoppedTerminal()
        try await testInitialInFlightReconnectQueuesOneRecoveryRefresh()
        try await testSuspendingReadinessPastTimeoutPreservesRuntimeUntilWake()
        try await testRecoveryDuringDisconnectedStreamRefreshesAfterReconnectWithoutReplay()
        try await testUnavailableRecoveryFailureDoesNotPublishSuccess()
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
