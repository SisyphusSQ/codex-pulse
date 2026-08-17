import Charts
import CodexPulseAppSupport
import CodexPulseProtocolGenerated
import SwiftUI

struct InvocationUsageView: View {
    @ObservedObject var model: AppModel
    @State private var mode: InvocationUsageMode = .overview
    @State private var selectedTrendDate: Date?

    private var ranges: [DateRangePreset] {
        model.selectedProvider == .cursor
            ? [.quotaMonth, .today, .sevenDays, .thirtyDays]
            : [.quotaWeek, .today, .sevenDays, .thirtyDays]
    }

    var body: some View {
        VStack(spacing: 0) {
            pageHeader
            Divider()
            FeatureStateView(
                state: model.invocationUsageState,
                emptyTitle: "当前范围没有调用活动",
                emptySystemImage: "wrench.and.screwdriver"
            ) { response in
                content(response)
            }
        }
        .frame(maxWidth: .infinity, maxHeight: .infinity, alignment: .topLeading)
        .accessibilityIdentifier("page.invocationUsage")
    }

    private var pageHeader: some View {
        HStack(alignment: .center, spacing: 16) {
            VStack(alignment: .leading, spacing: 3) {
                Text(AppFeature.invocationUsage.title(localization: model.localization))
                    .font(.title.bold())
                Text(model.localization.textValue("查看 Tool 调用与 Skill 检测活动，不关联 Token 用量"))
                    .font(.subheadline)
                    .foregroundStyle(.secondary)
            }
            Spacer(minLength: 24)
            HStack(spacing: 8) {
                Text(model.localization.textValue("时间范围"))
                    .font(.subheadline)
                    .foregroundStyle(.secondary)
                    .fixedSize()
                Picker(model.localization.textValue("时间范围"), selection: invocationRangeBinding) {
                    ForEach(ranges) { range in
                        Text(model.localization.textValue(range.title)).tag(range)
                    }
                }
                .labelsHidden()
                .pickerStyle(.segmented)
                .frame(width: 320)
                .accessibilityIdentifier("invocation.range")
            }

            HStack(spacing: 8) {
                Text(model.localization.textValue("数据来源"))
                    .font(.subheadline)
                    .foregroundStyle(.secondary)
                    .fixedSize()
                Picker(model.localization.textValue("数据来源"), selection: invocationSourceBinding) {
                    Text(model.localization.textValue("全部来源")).tag("all")
                    Text(model.localization.textValue("结构化事件")).tag("structured")
                    Text(model.localization.textValue("内容检测")).tag("detected")
                }
                .labelsHidden()
                .frame(width: 130)
                .accessibilityIdentifier("invocation.source")
            }
        }
        .padding(.horizontal, 18)
        .padding(.vertical, 14)
    }

    private var invocationRangeBinding: Binding<DateRangePreset> {
        Binding(
            get: { model.invocationRange },
            set: { model.selectInvocationRange($0) }
        )
    }

    private var invocationSourceBinding: Binding<String> {
        Binding(
            get: { model.invocationSourceClass },
            set: { model.selectInvocationSourceClass($0) }
        )
    }

    private func content(_ response: Codexpulse_Core_V1_InvocationUsageResponse) -> some View {
        ScrollView {
            VStack(alignment: .leading, spacing: 16) {
                summaryGrid(response)

                HStack(spacing: 8) {
                    Text(model.localization.textValue("展示内容"))
                        .font(.subheadline)
                        .foregroundStyle(.secondary)
                    Picker(model.localization.textValue("展示内容"), selection: $mode) {
                        ForEach(InvocationUsageMode.allCases) { option in
                            Text(model.localization.textValue(option.title)).tag(option)
                        }
                    }
                    .labelsHidden()
                    .pickerStyle(.segmented)
                    .frame(width: 180)
                    .accessibilityIdentifier("invocation.mode")
                }
                .frame(maxWidth: .infinity, alignment: .leading)

                if model.invocationRangeFellBackFromQuotaWeek { quotaWeekFallbackNotice }
                invocationTrend(response)
                rankingSections(response)
            }
            .padding(18)
        }
    }

    private var quotaWeekFallbackNotice: some View {
        Label(
            model.localization.textValue("暂未获取到周额度周期，当前显示最近 7 天。"),
            systemImage: "arrow.trianglehead.2.clockwise.rotate.90"
        )
        .font(.caption.weight(.medium))
        .foregroundStyle(.orange)
        .padding(.horizontal, 12)
        .padding(.vertical, 8)
        .background(Color.orange.opacity(0.1), in: RoundedRectangle(cornerRadius: 9))
    }

    private func summaryGrid(_ response: Codexpulse_Core_V1_InvocationUsageResponse) -> some View {
        LazyVGrid(
            columns: Array(
                repeating: GridItem(.flexible(minimum: 120), spacing: 12),
                count: 4
            ),
            alignment: .leading,
            spacing: 12
        ) {
            InvocationMetricCard(
                title: "Tool 调用",
                value: numericText(response.totals.toolCallCount),
                detail: model.localization.format(
                    "%@ 种 Tool", numericText(response.totals.distinctToolCount)
                ),
                symbol: "wrench.adjustable",
                tint: .blue
            )
            InvocationMetricCard(
                title: "Skill 检测活动",
                value: numericText(response.totals.skillActivityCount),
                detail: model.localization.format(
                    "%@ 种 Skill", numericText(response.totals.distinctSkillCount)
                ),
                symbol: "puzzlepiece.extension",
                tint: .purple
            )
            InvocationMetricCard(
                title: "相关会话",
                value: numericText(response.totals.sessionCount),
                detail: model.localization.textValue("当前筛选范围"),
                symbol: "text.bubble",
                tint: .teal
            )
            InvocationMetricCard(
                title: "明确失败",
                value: numericText(response.totals.toolFailureCount),
                detail: model.localization.textValue("仅带结果的 Tool 事件"),
                symbol: "exclamationmark.triangle",
                tint: response.totals.toolFailureCount.value > 0 ? .orange : .green
            )
        }
    }

    private func invocationTrend(_ response: Codexpulse_Core_V1_InvocationUsageResponse) -> some View {
        let points = invocationTrendPoints(response)
        let bars = InvocationTrendBarPresentation.bars(
            response: response,
            includesTools: mode != .skills,
            includesSkills: mode != .tools
        )
        let selectedPoint = selectedTrendPoint(in: points)
        let presentation = UsageTrendChartPresentation(preset: model.invocationRange, range: response.range)
        return SectionCard(title: "调用趋势") {
            if bars.isEmpty || presentation == nil {
                ContentUnavailableView(
                    model.localization.textValue("当前范围没有调用活动"),
                    systemImage: "chart.bar.xaxis"
                )
                .frame(height: 220)
            } else if let presentation {
                VStack(alignment: .leading, spacing: 10) {
                    HStack(alignment: .firstTextBaseline, spacing: 8) {
                        Text(presentation.sectionTitle)
                            .font(.subheadline.weight(.semibold))
                        Text(presentation.rangeLabel)
                            .font(.caption)
                            .foregroundStyle(.secondary)
                        Spacer()
                        invocationTrendLegend
                    }
                    Chart {
                        ForEach(bars) { bar in
                            BarMark(
                                x: .value("时间", bar.date),
                                y: .value(
                                    bar.series == .tool ? "Tool 调用" : "Skill 活动",
                                    bar.count
                                ),
                                width: .fixed(InvocationTrendBarPresentation.barWidth(
                                    for: model.invocationRange
                                )),
                                stacking: .unstacked
                            )
                            .foregroundStyle(invocationTrendColor(for: bar.series))
                            .cornerRadius(3)
                            .opacity(
                                selectedPoint == nil || selectedPoint?.date == bar.date ? 0.9 : 0.5
                            )
                            .accessibilityLabel(presentation.detailText(for: bar.date))
                            .accessibilityValue(model.localization.format(
                                bar.series == .tool ? "%@ 次调用" : "%@ 次活动",
                                model.localization.number(bar.count)
                            ))
                        }
                        if let selected = selectedPoint {
                            RuleMark(x: .value("选中时间", selected.date))
                                .foregroundStyle(Color.secondary.opacity(0.55))
                                .lineStyle(StrokeStyle(lineWidth: 1, dash: [4, 4]))
                                .annotation(
                                    position: .top,
                                    alignment: .leading,
                                    spacing: 8,
                                    overflowResolution: .init(x: .fit(to: .chart), y: .disabled)
                                ) {
                                    selectedTrendDetail(selected, presentation: presentation)
                                }
                        }
                    }
                    .chartXScale(domain: presentation.domain)
                    .chartYScale(domain: .automatic(includesZero: true))
                    .chartYAxis {
                        AxisMarks(position: .leading) { value in
                            AxisGridLine().foregroundStyle(.quaternary)
                            AxisValueLabel {
                                if let value = value.as(Int64.self) {
                                    Text(TokenQuantityFormatter.compactString(value))
                                }
                            }
                        }
                    }
                    .chartXAxis {
                        AxisMarks(values: presentation.axisTicks) { value in
                            AxisGridLine().foregroundStyle(.quaternary)
                            AxisTick()
                            AxisValueLabel {
                                if let date = value.as(Date.self) {
                                    Text(presentation.axisText(for: date))
                                }
                            }
                        }
                    }
                    .chartXSelection(value: $selectedTrendDate)
                    .chartOverlay { proxy in
                        GeometryReader { geometry in
                            Rectangle()
                                .fill(.clear)
                                .contentShape(Rectangle())
                                .onContinuousHover { phase in
                                    switch phase {
                                    case .active(let location):
                                        guard let plotFrame = proxy.plotFrame else {
                                            selectedTrendDate = nil
                                            return
                                        }
                                        let plotRect = geometry[plotFrame]
                                        guard plotRect.contains(location) else {
                                            selectedTrendDate = nil
                                            return
                                        }
                                        selectedTrendDate = proxy.value(
                                            atX: location.x - plotRect.origin.x,
                                            as: Date.self
                                        )
                                    case .ended:
                                        selectedTrendDate = nil
                                    }
                                }
                        }
                    }
                    .frame(height: 230)
                    .onChange(of: response.range.startAtMs) { _, _ in selectedTrendDate = nil }
                    .onChange(of: response.range.endAtMs) { _, _ in selectedTrendDate = nil }
                }
            }
        }
        .accessibilityIdentifier("invocation.trend")
    }

    private var invocationTrendLegend: some View {
        HStack(spacing: 16) {
            if mode != .skills { invocationLegendItem(color: .blue, title: "Tool") }
            if mode != .tools { invocationLegendItem(color: .purple, title: "Skill") }
        }
        .font(.caption)
        .foregroundStyle(.secondary)
    }

    private func invocationLegendItem(color: Color, title: String) -> some View {
        Label {
            Text(model.localization.textValue(title))
        } icon: {
            RoundedRectangle(cornerRadius: 2).fill(color).frame(width: 9, height: 7)
        }
    }

    private func invocationTrendColor(for series: InvocationTrendSeries) -> Color {
        switch series {
        case .tool: .blue
        case .skill: .purple
        }
    }

    private func selectedTrendDetail(
        _ point: InvocationTrendPoint,
        presentation: UsageTrendChartPresentation
    ) -> some View {
        VStack(alignment: .leading, spacing: 7) {
            Text(presentation.detailText(for: point.date))
                .font(.caption.weight(.semibold))
            if mode != .skills, let count = point.toolCallCount {
                invocationTrendMetric(color: .blue, title: "Tool", count: count)
            }
            if mode != .tools, let count = point.skillActivityCount {
                invocationTrendMetric(color: .purple, title: "Skill", count: count)
            }
        }
        .padding(10)
        .frame(minWidth: 150, alignment: .leading)
        .background(.regularMaterial, in: RoundedRectangle(cornerRadius: 10, style: .continuous))
    }

    private func invocationTrendMetric(color: Color, title: String, count: Int64) -> some View {
        HStack(spacing: 8) {
            Circle().fill(color).frame(width: 7, height: 7)
            Text(model.localization.textValue(title))
            Spacer(minLength: 12)
            Text(model.localization.number(count)).monospacedDigit()
        }
        .font(.caption)
    }

    private func invocationTrendPoints(
        _ response: Codexpulse_Core_V1_InvocationUsageResponse
    ) -> [InvocationTrendPoint] {
        response.trend.compactMap { point in
            guard point.startAtMs.hasValue else { return nil }
            return InvocationTrendPoint(
                id: "\(point.key):\(point.startAtMs.value)",
                date: invocationDate(point.startAtMs.value),
                toolCallCount: point.toolCallCount.hasValue ? point.toolCallCount.value : nil,
                skillActivityCount: point.skillActivityCount.hasValue
                    ? point.skillActivityCount.value : nil
            )
        }
    }

    private func selectedTrendPoint(in points: [InvocationTrendPoint]) -> InvocationTrendPoint? {
        guard let selectedTrendDate else { return nil }
        return points.min {
            abs($0.date.timeIntervalSince(selectedTrendDate))
                < abs($1.date.timeIntervalSince(selectedTrendDate))
        }
    }

    @ViewBuilder
    private func rankingSections(_ response: Codexpulse_Core_V1_InvocationUsageResponse) -> some View {
        switch mode {
        case .overview:
            ViewThatFits(in: .horizontal) {
                HStack(alignment: .top, spacing: 12) {
                    toolRanking(response.tools)
                    skillRanking(response.skills)
                }
                .frame(minWidth: 680)
                VStack(spacing: 12) {
                    toolRanking(response.tools)
                    skillRanking(response.skills)
                }
            }
        case .tools:
            toolRanking(response.tools)
        case .skills:
            skillRanking(response.skills)
        }
    }

    private func toolRanking(_ items: [Codexpulse_Core_V1_ToolUsageItem]) -> some View {
        let items = Array(items.prefix(10))
        return SectionCard(title: "Tool 使用排行") {
            VStack(spacing: 0) {
                ForEach(Array(items.enumerated()), id: \.element.name) { index, item in
                    ToolUsageRow(item: item, maximum: items.first?.callCount.value ?? 0)
                    if index < items.count - 1 { Divider() }
                }
            }
        }
    }

    private func skillRanking(_ items: [Codexpulse_Core_V1_SkillUsageItem]) -> some View {
        let items = Array(items.prefix(10))
        return SectionCard(title: "Skill 检测排行") {
            VStack(spacing: 0) {
                ForEach(Array(items.enumerated()), id: \.element.name) { index, item in
                    SkillUsageRow(item: item, maximum: items.first?.activityCount.value ?? 0)
                    if index < items.count - 1 { Divider() }
                }
            }
        }
    }

}

private struct InvocationTrendPoint: Identifiable {
    let id: String
    let date: Date
    let toolCallCount: Int64?
    let skillActivityCount: Int64?
}

private enum InvocationUsageMode: String, CaseIterable, Identifiable {
    case overview
    case tools
    case skills

    var id: String { rawValue }

    var title: String {
        switch self {
        case .overview: "总览"
        case .tools: "Tool"
        case .skills: "Skill"
        }
    }
}

private struct InvocationMetricCard: View {
    let title: String
    let value: String
    let detail: String
    let symbol: String
    let tint: Color

    var body: some View {
        VStack(alignment: .leading, spacing: 10) {
            HStack {
                Label(localizedCopy(title), systemImage: symbol)
                    .font(.subheadline.weight(.medium))
                    .foregroundStyle(.secondary)
                Spacer()
            }
            Text(value)
                .font(.system(size: 28, weight: .semibold, design: .rounded))
                .monospacedDigit()
            Text(detail)
                .font(.caption)
                .foregroundStyle(.secondary)
        }
        .padding(14)
        .frame(maxWidth: .infinity, alignment: .leading)
        .background(.regularMaterial, in: RoundedRectangle(cornerRadius: 14, style: .continuous))
        .overlay(alignment: .top) {
            RoundedRectangle(cornerRadius: 14, style: .continuous)
                .stroke(tint.opacity(0.24), lineWidth: 1)
        }
        .accessibilityElement(children: .combine)
    }
}

private struct ToolUsageRow: View {
    let item: Codexpulse_Core_V1_ToolUsageItem
    let maximum: Int64

    var body: some View {
        DisclosureGroup {
            VStack(alignment: .leading, spacing: 5) {
                Text(
                    AppLocalizationRegistry.shared.current.format(
                        "成功 %@ · 失败 %@ · 结果未知 %@",
                        numericText(item.succeededCount),
                        numericText(item.failedCount),
                        numericText(item.unknownCount)
                    )
                )
                Text(AppLocalizationRegistry.shared.current.format(
                    "来源：%@",
                    item.sources.map(invocationSourceTitle).joined(
                        separator: AppLocalizationRegistry.shared.current.textValue("、")
                    )
                ))
                HStack {
                    Text(AppLocalizationRegistry.shared.current.format(
                        "最近：%@", timestampText(item.lastSeenAtMs)
                    ))
                    Spacer()
                    Text(AppLocalizationRegistry.shared.current.format(
                        "平均耗时：%@", durationText(item.averageDurationMs)
                    ))
                }
            }
            .font(.caption)
            .foregroundStyle(.secondary)
            .padding(.top, 5)
        } label: {
            VStack(alignment: .leading, spacing: 6) {
                HStack(alignment: .firstTextBaseline) {
                    Text(item.name)
                        .font(.body.weight(.medium))
                        .lineLimit(1)
                    Spacer()
                    Text(AppLocalizationRegistry.shared.current.format(
                        "%@ 次调用", numericText(item.callCount)
                    ))
                        .font(.headline)
                        .monospacedDigit()
                }
                ProgressView(value: Double(item.callCount.value), total: Double(max(1, maximum)))
                    .tint(.blue)
            }
        }
        .padding(.vertical, 10)
    }
}

private struct SkillUsageRow: View {
    let item: Codexpulse_Core_V1_SkillUsageItem
    let maximum: Int64

    var body: some View {
        DisclosureGroup {
            VStack(alignment: .leading, spacing: 5) {
                Text(
                    AppLocalizationRegistry.shared.current.format(
                        "显式引用 %@ · 文件加载 %@",
                        numericText(item.explicitCount),
                        numericText(item.fileLoadedCount)
                    )
                )
                HStack {
                    Text(AppLocalizationRegistry.shared.current.format(
                        "相关会话：%@", numericText(item.sessionCount)
                    ))
                    Spacer()
                    Text(AppLocalizationRegistry.shared.current.format(
                        "最近：%@", timestampText(item.lastSeenAtMs)
                    ))
                }
            }
            .font(.caption)
            .foregroundStyle(.secondary)
            .padding(.top, 5)
        } label: {
            VStack(alignment: .leading, spacing: 6) {
                HStack(alignment: .firstTextBaseline) {
                    Text(item.name)
                        .font(.body.weight(.medium))
                        .lineLimit(1)
                    Spacer()
                    Text(AppLocalizationRegistry.shared.current.format(
                        "%@ 次活动", numericText(item.activityCount)
                    ))
                        .font(.headline)
                        .monospacedDigit()
                }
                ProgressView(value: Double(item.activityCount.value), total: Double(max(1, maximum)))
                    .tint(.purple)
            }
        }
        .padding(.vertical, 10)
    }
}

private func invocationDate(_ milliseconds: Int64) -> Date {
    Date(timeIntervalSince1970: TimeInterval(milliseconds) / 1_000)
}

private func durationText(_ value: Codexpulse_Core_V1_NumericValue) -> String {
    guard value.hasValue else { return localizedCopy("暂无") }
    if value.value < 1_000 { return "\(value.value) ms" }
    return String(format: "%.2f s", locale: AppLocalizationRegistry.shared.current.locale, Double(value.value) / 1_000)
}

private func invocationSourceTitle(_ source: String) -> String {
    let value = switch source {
    case "response_function": "函数调用"
    case "response_custom": "编排调用"
    case "exec_nested": "内部 Tool"
    case "mcp": "MCP"
    case "web_search": "网页搜索"
    case "image_generation": "图像生成"
    case "skill_explicit": "显式引用"
    case "skill_file_loaded": "文件加载"
    default: source
    }
    return localizedCopy(value)
}
