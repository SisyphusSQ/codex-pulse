import Charts
import CodexPulseAppSupport
import CodexPulseProtocolGenerated
import SwiftUI

struct DashboardSummaryView: View {
    @ObservedObject var model: AppModel

    private let ranges: [DateRangePreset] = [.today, .sevenDays, .thirtyDays]

    var body: some View {
        VStack(spacing: 0) {
            pageHeader
            Divider()
            FeatureStateView(
                state: model.dashboardSummaryState,
                emptyTitle: "当前范围暂无跨客户端用量",
                emptySystemImage: "square.grid.2x2"
            ) { response in
                DashboardResponseContent(
                    response: response,
                    selectedRange: model.dashboardRange,
                    trendMode: model.dashboardTrendMode,
                    localization: model.localization,
                    model: model
                ).equatable()
            }
        }
        .frame(maxWidth: .infinity, maxHeight: .infinity, alignment: .topLeading)
        .accessibilityIdentifier("page.dashboard-summary")
    }

    private var pageHeader: some View {
        HStack(alignment: .center, spacing: 16) {
            VStack(alignment: .leading, spacing: 3) {
                Text(AppFeature.dashboardSummary.title(localization: model.localization))
                    .font(.title.bold())
                Text(model.localization.textValue("跨 Codex、Cursor 和 Grok 查看 Token、成本与额度"))
                    .font(.subheadline)
                    .foregroundStyle(.secondary)
            }
            Spacer(minLength: 24)
            HStack(spacing: 8) {
                Text(model.localization.textValue("时间范围"))
                    .font(.subheadline)
                    .foregroundStyle(.secondary)
                    .fixedSize()
                Picker(model.localization.textValue("时间范围"), selection: rangeBinding) {
                    ForEach(ranges) { range in
                        Text(model.localization.textValue(range.title)).tag(range)
                    }
                }
                .labelsHidden()
                .pickerStyle(.segmented)
                .frame(width: 300)
                .accessibilityIdentifier("dashboard.summary.range")
            }
        }
        .padding(.horizontal, 18)
        .padding(.vertical, 14)
    }

    private var rangeBinding: Binding<DateRangePreset> {
        Binding(
            get: { model.dashboardRange },
            set: { model.selectDashboardRange($0) }
        )
    }
}

private enum DashboardDistributionMetric: String, CaseIterable, Identifiable {
    case tokens
    case cost

    var id: String { rawValue }

    func title(localization: AppLocalization) -> String {
        switch self {
        case .tokens: localization.textValue("Token")
        case .cost: localization.textValue("费用")
        }
    }
}

private enum DashboardActivityScope: String, CaseIterable, Identifiable {
    case all
    case codex
    case cursor
    case grok

    var id: String { rawValue }

    var provider: AgentProvider? {
        switch self {
        case .all: nil
        case .codex: .codex
        case .cursor: .cursor
        case .grok: .grok
        }
    }

    func title(localization: AppLocalization) -> String {
        provider?.title ?? localization.textValue("全部客户端")
    }
}

private struct DashboardUsageSegment: Identifiable {
    let id: String
    let title: String
    let value: Int64
    let color: Color
}

private struct DashboardUsageCompositionBar: View {
    let segments: [DashboardUsageSegment]
    let total: Int64
    let localization: AppLocalization
    @State private var hoveredSegmentID: String?

    var body: some View {
        GeometryReader { geometry in
            ZStack(alignment: .leading) {
                RoundedRectangle(cornerRadius: 5, style: .continuous)
                    .fill(Color.secondary.opacity(0.12))
                HStack(spacing: 1) {
                    ForEach(segments) { segment in
                        Rectangle()
                            .fill(segment.color)
                            .frame(width: segmentWidth(segment.value, available: geometry.size.width))
                    }
                }
                .clipShape(RoundedRectangle(cornerRadius: 5, style: .continuous))
                if let hoveredSegment {
                    Text("\(hoveredSegment.title) · \(TokenQuantityFormatter.compactString(hoveredSegment.value, localization: localization))")
                        .font(.caption2.weight(.medium))
                        .lineLimit(1)
                        .padding(.horizontal, 7)
                        .padding(.vertical, 3)
                        .background(.ultraThickMaterial, in: Capsule())
                        .frame(maxWidth: .infinity, alignment: .center)
                        .allowsHitTesting(false)
                }
            }
            .overlay {
                Rectangle()
                    .fill(.clear)
                    .contentShape(Rectangle())
                    .onContinuousHover { phase in
                        switch phase {
                        case .active(let location):
                            hoveredSegmentID = segment(
                                at: location.x,
                                width: geometry.size.width
                            )?.id
                        case .ended:
                            hoveredSegmentID = nil
                        }
                    }
                }
        }
        .frame(height: 28)
        .accessibilityHidden(true)
    }

    private var hoveredSegment: DashboardUsageSegment? {
        guard let hoveredSegmentID else { return nil }
        return segments.first(where: { $0.id == hoveredSegmentID })
    }

    private func segment(at x: CGFloat, width: CGFloat) -> DashboardUsageSegment? {
        guard total > 0, width > 0, x >= 0 else { return nil }
        let target = Double(x / width) * Double(total)
        var cumulative = 0.0
        for segment in segments {
            cumulative += Double(max(segment.value, 0))
            if target <= cumulative { return segment }
        }
        return nil
    }

    private func segmentWidth(_ value: Int64, available: CGFloat) -> CGFloat {
        guard total > 0, value > 0 else { return 0 }
        return available * min(max(CGFloat(value) / CGFloat(total), 0), 1)
    }
}

private struct DashboardDonutItem: Identifiable {
    let id: String
    let title: String
    let value: Int64
    let color: Color
}

private struct DashboardDonutBreakdown: View {
    let items: [DashboardDonutItem]
    let total: Int64
    let metric: DashboardDistributionMetric
    let localization: AppLocalization
    @State private var hoveredItemID: String?

    var body: some View {
        ViewThatFits(in: .horizontal) {
            HStack(alignment: .center, spacing: 22) {
                donut
                legend
            }
            VStack(alignment: .leading, spacing: 16) {
                donut.frame(maxWidth: .infinity)
                legend
            }
        }
        .frame(minHeight: 220)
    }

    private var donut: some View {
        ZStack {
            Chart(items) { item in
                SectorMark(
                    angle: .value(metric.rawValue, item.value),
                    innerRadius: .ratio(0.66),
                    angularInset: 1.2
                )
                .cornerRadius(3)
                .foregroundStyle(item.color)
            }
            .chartLegend(.hidden)
            .chartOverlay { proxy in
                GeometryReader { geometry in
                    Rectangle()
                        .fill(.clear)
                        .contentShape(Rectangle())
                        .onContinuousHover { phase in
                            switch phase {
                            case .active(let location):
                                guard let plotFrame = proxy.plotFrame else {
                                    hoveredItemID = nil
                                    return
                                }
                                hoveredItemID = item(
                                    at: location,
                                    plotFrame: geometry[plotFrame]
                                )?.id
                            case .ended:
                                hoveredItemID = nil
                            }
                        }
                }
            }
            VStack(spacing: 4) {
                Text(hoveredItem?.title ?? metric.title(localization: localization))
                    .font(.caption)
                    .foregroundStyle(.secondary)
                    .lineLimit(1)
                    .frame(maxWidth: 104)
                Text(metricText(hoveredItem?.value ?? total))
                    .font(.system(size: 24, weight: .semibold, design: .rounded))
                    .monospacedDigit()
                if let hoveredItem {
                    Text(localization.percent(percent(hoveredItem.value), fractionDigits: 1))
                        .font(.caption2)
                        .foregroundStyle(.secondary)
                        .monospacedDigit()
                }
            }
            .allowsHitTesting(false)
        }
        .frame(width: 190, height: 190)
        .accessibilityElement(children: .combine)
        .accessibilityLabel(metric.title(localization: localization))
        .accessibilityValue(metricText(total))
    }

    private var hoveredItem: DashboardDonutItem? {
        guard let hoveredItemID else { return nil }
        return items.first(where: { $0.id == hoveredItemID })
    }

    private func item(
        at location: CGPoint,
        plotFrame: CGRect
    ) -> DashboardDonutItem? {
        let center = CGPoint(x: plotFrame.midX, y: plotFrame.midY)
        let deltaX = location.x - center.x
        let deltaY = location.y - center.y
        let radius = hypot(deltaX, deltaY)
        let outerRadius = min(plotFrame.width, plotFrame.height) / 2
        let innerRadius = outerRadius * 0.66
        guard radius >= innerRadius, radius <= outerRadius else { return nil }
        var angle = atan2(deltaX, -deltaY)
        if angle < 0 { angle += .pi * 2 }
        let chartTotal = items.reduce(Int64(0)) { $0 + max($1.value, 0) }
        guard chartTotal > 0 else { return nil }
        let target = Double(chartTotal) * angle / (.pi * 2)
        var cumulative = 0.0
        for item in items {
            cumulative += Double(max(item.value, 0))
            if target <= cumulative { return item }
        }
        return items.last
    }

    private var legend: some View {
        VStack(alignment: .leading, spacing: 11) {
            ForEach(items) { item in
                HStack(alignment: .firstTextBaseline, spacing: 9) {
                    Circle()
                        .fill(item.color)
                        .frame(width: 9, height: 9)
                    Text(item.title)
                        .lineLimit(1)
                    Spacer(minLength: 12)
                    Text(metricText(item.value))
                        .font(.body.weight(.medium))
                        .monospacedDigit()
                    Text(localization.percent(percent(item.value), fractionDigits: 1))
                        .font(.caption)
                        .foregroundStyle(.secondary)
                        .monospacedDigit()
                        .frame(width: 52, alignment: .trailing)
                }
                .accessibilityElement(children: .combine)
            }
        }
        .frame(maxWidth: .infinity, alignment: .leading)
    }

    private func metricText(_ value: Int64) -> String {
        switch metric {
        case .tokens:
            TokenQuantityFormatter.compactString(value, localization: localization)
        case .cost:
            String(format: "$%.2f", locale: localization.locale, Double(value) / 1_000_000)
        }
    }

    private func percent(_ value: Int64) -> Double {
        guard total > 0 else { return 0 }
        return Double(value) / Double(total) * 100
    }
}

private struct DashboardAnnualActivityHeatmap: View {
    let activity: TokenActivityPresentation
    let localization: AppLocalization
    @State private var hoverState = TokenActivityHoverState()

    let card: TokenActivityCardPresentation

    var body: some View {
        VStack(alignment: .leading, spacing: 10) {
            DashboardActivityMetricStrip(metrics: card.metrics)
            Divider()
            HStack(spacing: 10) {
                Text(verbatim: card.scope)
                    .font(.caption.weight(.medium))
                    .foregroundStyle(.secondary)
                Text(activity.reportingTimeZone)
                    .font(.caption2)
                    .foregroundStyle(.tertiary)
                if let notice = card.notice {
                    Label {
                        Text(verbatim: notice)
                    } icon: {
                        Image(systemName: "externaldrive")
                    }
                    .font(.caption)
                    .foregroundStyle(.secondary)
                }
                Spacer()
                intensityLegend
            }
            heatmap
        }
    }

    private var intensityLegend: some View {
        HStack(spacing: 4) {
            Text(localization.textValue("少"))
                .font(.caption2)
                .foregroundStyle(.secondary)
            ForEach([
                TokenActivityIntensity.none,
                .low,
                .medium,
                .high,
                .veryHigh,
            ], id: \.rawValue) { intensity in
                RoundedRectangle(cornerRadius: 2, style: .continuous)
                    .fill(activityHeatmapColor(for: intensity, tint: .blue))
                    .frame(width: 10, height: 10)
            }
            Text(localization.textValue("多"))
                .font(.caption2)
                .foregroundStyle(.secondary)
        }
        .accessibilityElement(children: .combine)
        .accessibilityLabel(localization.textValue("Token 活跃度由少到多"))
    }

    private var heatmap: some View {
        GeometryReader { geometry in
            let weekCount = max(card.calendar.weeks.count, 1)
            let labelWidth: CGFloat = 24
            let labelGap: CGFloat = 8
            let gridOrigin = CGPoint(x: labelWidth + labelGap, y: 18)
            let spacing: CGFloat = geometry.size.width >= 900 ? 3 : 2
            let layout = ActivityHeatmapGridLayout(
                availableWidth: geometry.size.width - gridOrigin.x,
                columnCount: weekCount,
                rowCount: 7,
                horizontalSpacing: spacing,
                verticalSpacing: spacing,
                minimumCellSize: 6
            )

            ZStack(alignment: .topLeading) {
                Canvas { context, _ in
                    drawHeatmap(
                        context: &context,
                        layout: layout,
                        gridOrigin: gridOrigin,
                        labelWidth: labelWidth
                    )
                }

                if let selectedDay,
                   let position = heatmapPosition(for: selectedDay.id),
                   let frame = layout.cellFrame(column: position.column, row: position.row)
                {
                    Text(card.dayDetail(selectedDay))
                        .font(.caption)
                        .fixedSize()
                        .padding(.horizontal, 8)
                        .padding(.vertical, 5)
                        .background(.ultraThickMaterial, in: RoundedRectangle(cornerRadius: 6))
                        .overlay {
                            RoundedRectangle(cornerRadius: 6)
                                .stroke(.separator.opacity(0.55), lineWidth: 0.5)
                        }
                        .shadow(color: .black.opacity(0.14), radius: 5, y: 2)
                        .position(
                            x: min(
                                max(gridOrigin.x + frame.midX, 110),
                                max(geometry.size.width - 110, 110)
                            ),
                            y: max(gridOrigin.y + frame.minY - 18, 0)
                        )
                        .allowsHitTesting(false)
                }
            }
            .contentShape(Rectangle())
            .onContinuousHover { phase in
                switch phase {
                case .active(let location):
                    updateSelection(
                        dayID: heatmapDay(
                            at: CGPoint(
                                x: location.x - gridOrigin.x,
                                y: location.y - gridOrigin.y
                            ),
                            layout: layout
                        )?.id,
                        animated: true
                    )
                case .ended:
                    updateSelection(dayID: nil, animated: true)
                }
            }
            .accessibilityElement(children: .ignore)
            .accessibilityLabel(Text(verbatim: "\(card.title) · \(card.scope)"))
            .accessibilityValue(Text(verbatim: selectedDay.map(card.dayDetail) ?? card.scope))
            .accessibilityAdjustableAction { direction in
                let movement: ActivityHeatmapSelectionDirection
                switch direction {
                case .increment: movement = .increment
                case .decrement: movement = .decrement
                @unknown default: return
                }
                updateSelection(
                    dayID: ActivityHeatmapSelectionResolver.move(
                        from: hoverState.dayID,
                        orderedIDs: orderedDays.map(\.id),
                        direction: movement
                    ),
                    animated: false
                )
            }
        }
        .frame(maxWidth: .infinity)
        .aspectRatio(6.8, contentMode: .fit)
    }

    private var orderedDays: [TokenActivityCalendarDay] {
        card.calendar.weeks.flatMap(\.days).compactMap { $0 }
    }

    private var selectedDay: TokenActivityCalendarDay? {
        guard let dayID = hoverState.dayID else { return nil }
        return orderedDays.first(where: { $0.id == dayID })
    }

    private func drawHeatmap(
        context: inout GraphicsContext,
        layout: ActivityHeatmapGridLayout,
        gridOrigin: CGPoint,
        labelWidth: CGFloat
    ) {
        for row in 0..<7 {
            let label = row == 0 ? "一" : row == 2 ? "三" : row == 4 ? "五" : ""
            guard !label.isEmpty,
                  let frame = layout.cellFrame(column: 0, row: row)
            else { continue }
            context.draw(
                Text(localization.textValue(label))
                    .font(.caption2)
                    .foregroundStyle(.secondary),
                at: CGPoint(x: labelWidth, y: gridOrigin.y + frame.midY),
                anchor: .trailing
            )
        }

        for label in card.calendar.monthLabels {
            guard let frame = layout.cellFrame(column: label.weekIndex, row: 0) else { continue }
            context.draw(
                Text(label.title)
                    .font(.caption2)
                    .foregroundStyle(.secondary),
                at: CGPoint(x: gridOrigin.x + frame.minX, y: 0),
                anchor: .topLeading
            )
        }

        for (column, week) in card.calendar.weeks.enumerated() {
            for (row, day) in week.days.enumerated() {
                guard let day,
                      let frame = layout.cellFrame(column: column, row: row)
                else { continue }
                let resolvedFrame = frame.offsetBy(dx: gridOrigin.x, dy: gridOrigin.y)
                let path = Path(
                    roundedRect: resolvedFrame,
                    cornerRadius: max(2, layout.cellSize * 0.22)
                )
                context.fill(
                    path,
                    with: .color(activityHeatmapColor(for: day.intensity, tint: .blue))
                )
                if day.intensity == .unknown {
                    context.stroke(
                        path,
                        with: .color(.secondary.opacity(0.28)),
                        lineWidth: 0.7
                    )
                }
            }
        }
    }

    private func heatmapDay(
        at point: CGPoint,
        layout: ActivityHeatmapGridLayout
    ) -> TokenActivityCalendarDay? {
        guard let position = layout.position(at: point),
              card.calendar.weeks.indices.contains(position.column),
              card.calendar.weeks[position.column].days.indices.contains(position.row)
        else { return nil }
        return card.calendar.weeks[position.column].days[position.row]
    }

    private func heatmapPosition(for dayID: String) -> ActivityHeatmapGridPosition? {
        for (column, week) in card.calendar.weeks.enumerated() {
            if let row = week.days.firstIndex(where: { $0?.id == dayID }) {
                return ActivityHeatmapGridPosition(column: column, row: row)
            }
        }
        return nil
    }

    private func updateSelection(dayID: String?, animated: Bool) {
        guard hoverState.dayID != dayID else { return }
        let update = { hoverState = TokenActivityHoverState(dayID: dayID) }
        if animated {
            withAnimation(.easeOut(duration: 0.12), update)
        } else {
            update()
        }
    }
}

private struct DashboardActivityMetricStrip: View {
    let metrics: [TokenActivityMetricPresentation]

    var body: some View {
        ViewThatFits(in: .horizontal) {
            HStack(spacing: 0) {
                ForEach(Array(metrics.enumerated()), id: \.element.id) { index, metric in
                    metricCell(metric)
                    if index < metrics.count - 1 {
                        Divider()
                            .frame(height: 54)
                    }
                }
            }
            .frame(minWidth: 660)

            LazyVGrid(
                columns: [GridItem(.adaptive(minimum: 150), spacing: 12)],
                alignment: .leading,
                spacing: 12
            ) {
                ForEach(metrics) { metric in
                    metricCell(metric)
                }
            }
        }
        .accessibilityElement(children: .contain)
        .accessibilityIdentifier("dashboard.summary.activity-metrics")
    }

    private func metricCell(_ metric: TokenActivityMetricPresentation) -> some View {
        VStack(alignment: .leading, spacing: 4) {
            Text(verbatim: metric.value)
                .font(.system(size: 27, weight: .semibold, design: .rounded))
                .monospacedDigit()
                .lineLimit(1)
                .minimumScaleFactor(0.75)
            Text(verbatim: metric.title)
                .font(.caption)
                .foregroundStyle(.secondary)
                .lineLimit(1)
                .minimumScaleFactor(0.8)
        }
        .padding(.horizontal, 14)
        .padding(.vertical, 8)
        .frame(maxWidth: .infinity, alignment: .leading)
        .accessibilityElement(children: .combine)
    }
}

private struct DashboardSummaryContentView: View {
    let presentation: DashboardSummaryPresentation
    let selectedRange: DateRangePreset
    let trendMode: DashboardTrendMode
    let onSelectTrendMode: (DashboardTrendMode) -> Void
    let onOpenProvider: (AgentProvider) -> Void
    let localization: AppLocalization
    @State private var providerDistributionMetric = DashboardDistributionMetric.tokens
    @State private var modelDistributionMetric = DashboardDistributionMetric.tokens
    @State private var activityScope = DashboardActivityScope.all

    var body: some View {
        GeometryReader { geometry in
            ScrollView {
                LazyVStack(alignment: .leading, spacing: 16) {
                    summaryGrid
                    activitySection
                    trendSection
                    distributionSections(availableWidth: geometry.size.width - 36)
                }
                .padding(18)
                .frame(maxWidth: .infinity, alignment: .leading)
            }
        }
        .accessibilityIdentifier("dashboard.summary.content")
    }

    private var summaryGrid: some View {
        LazyVGrid(
            columns: Array(
                repeating: GridItem(.flexible(minimum: 180), spacing: 12),
                count: 3
            ),
            alignment: .leading,
            spacing: 12
        ) {
            MetricCard(
                title: "Token 总量",
                value: metricText(presentation.totals.tokens, localization: localization),
                detail: coverageText(known: presentation.coverage.knownProviderCount),
                systemImage: "number"
            )
            .accessibilityIdentifier("dashboard.summary.kpi.tokens")
            MetricCard(
                title: "API 等价成本",
                value: metricText(
                    presentation.totals.estimatedCost,
                    cost: true,
                    localization: localization
                ),
                detail: presentation.costCoverageCaption(localization: localization),
                systemImage: "dollarsign.circle"
            )
            .accessibilityIdentifier("dashboard.summary.kpi.cost")
            MetricCard(
                title: "活跃客户端",
                value: activeClientsText,
                detail: localization.textValue(selectedRange.title),
                systemImage: "rectangle.3.group"
            )
            .accessibilityIdentifier("dashboard.summary.kpi.clients")
        }
        .accessibilityElement(children: .contain)
    }

    private var trendSection: some View {
        SectionCard(title: "Token 趋势") {
            VStack(alignment: .leading, spacing: 12) {
                HStack(alignment: .center, spacing: 12) {
                    Text(dateRangeText)
                        .font(.caption)
                        .foregroundStyle(.secondary)
                    if trendMode == .stacked {
                        providerLegend
                    }
                    Spacer(minLength: 16)
                    Picker(localization.textValue("趋势视图"), selection: trendBinding) {
                        ForEach(DashboardTrendMode.allCases) { mode in
                            Text(mode.title(localization: localization)).tag(mode)
                        }
                    }
                    .labelsHidden()
                    .pickerStyle(.segmented)
                    .frame(width: 210)
                    .accessibilityIdentifier("dashboard.summary.trend-mode")
                }
                if presentation.trend.isEmpty {
                    ContentUnavailableView(
                        localization.textValue("当前范围暂无用量"),
                        systemImage: "chart.bar.xaxis"
                    )
                    .frame(height: 220)
                } else {
                    DashboardTrendChart(
                        presentation: presentation, selectedRange: selectedRange,
                        trendMode: trendMode, localization: localization
                    )
                }
            }
        }
    }





    private var providerLegend: some View {
        HStack(spacing: 10) {
            ForEach(presentation.providers.map(\.provider)) { provider in
                HStack(spacing: 4) {
                    Circle()
                        .fill(providerColor(provider))
                        .frame(width: 6, height: 6)
                    Text(provider.title)
                        .font(.caption2)
                        .foregroundStyle(.secondary)
                }
            }
        }
    }

    private var trendBinding: Binding<DashboardTrendMode> {
        Binding(get: { trendMode }, set: { onSelectTrendMode($0) })
    }







    private var activitySection: some View {
        SectionCard(title: "Token 活动") {
            VStack(alignment: .leading, spacing: 12) {
                HStack(alignment: .center, spacing: 12) {
                    Text(localization.textValue("过去 365 天的客户端 Token 活跃度"))
                        .font(.subheadline)
                        .foregroundStyle(.secondary)
                    Spacer(minLength: 16)
                    Picker(localization.textValue("客户端"), selection: $activityScope) {
                        ForEach(DashboardActivityScope.allCases) { scope in
                            Text(scope.title(localization: localization)).tag(scope)
                        }
                    }
                    .labelsHidden()
                    .frame(width: 150)
                    .accessibilityIdentifier("dashboard.summary.activity-scope")
                }
                let activity = presentation.tokenActivity(provider: activityScope.provider)
                if activity.availability == .unavailable {
                    ContentUnavailableView(
                        localization.textValue("年度活动暂时不可用"),
                        systemImage: "calendar.badge.exclamationmark"
                    )
                    .frame(height: 150)
                } else {
                    DashboardActivityCard(activity: activity, localization: localization)
                        .equatable()
                }
            }
        }
        .accessibilityIdentifier("dashboard.summary.activity")
    }

    @ViewBuilder
    private func distributionSections(availableWidth: CGFloat) -> some View {
        if availableWidth >= 460 + 400 + 16 {
            VStack(alignment: .leading, spacing: 16) {
                HStack(alignment: .top, spacing: 16) {
                    toolAndModelUsageSection(fillsProposedHeight: true)
                        .frame(minWidth: 460)
                    providerDistributionSection(fillsProposedHeight: true)
                        .frame(minWidth: 400)
                }
                HStack(alignment: .top, spacing: 16) {
                    modelDistributionSection(fillsProposedHeight: true)
                        .frame(minWidth: 460)
                    quotaSection(fillsProposedHeight: true)
                        .frame(minWidth: 400)
                }
            }
        } else {
            VStack(alignment: .leading, spacing: 16) {
                toolAndModelUsageSection(fillsProposedHeight: false)
                providerDistributionSection(fillsProposedHeight: false)
                modelDistributionSection(fillsProposedHeight: false)
                quotaSection(fillsProposedHeight: false)
            }
        }
    }

    private func toolAndModelUsageSection(fillsProposedHeight: Bool) -> some View {
        SectionCard(title: "工具与模型用量", fillsProposedHeight: fillsProposedHeight) {
            Text(localization.textValue("按客户端比较总 Token，并展示模型构成"))
                .font(.subheadline)
                .foregroundStyle(.secondary)
            if presentation.providers.isEmpty {
                Text(localization.textValue("当前范围暂无用量"))
                    .foregroundStyle(.secondary)
            } else {
                VStack(alignment: .leading, spacing: 16) {
                    ForEach(Array(presentation.providers.enumerated()), id: \.element.id) { index, card in
                        toolAndModelUsageRow(card)
                        if index < presentation.providers.count - 1 {
                            Divider()
                        }
                    }
                }
                .padding(.top, 4)
            }
        }
        .accessibilityIdentifier("dashboard.summary.tool-model-usage")
    }

    private func toolAndModelUsageRow(
        _ card: DashboardSummaryPresentation.ProviderCard
    ) -> some View {
        let providerTokens = chartValue(card.tokens)
        let totalTokens = chartValue(presentation.totals.tokens)
        let fraction = totalTokens > 0 ? Double(providerTokens) / Double(totalTokens) : 0
        let costDetails = card.costDetails(localization: localization)
        return Button {
            onOpenProvider(card.provider)
        } label: {
            HStack(alignment: .center, spacing: 12) {
                Image(systemName: providerSymbol(card.provider))
                    .font(.title3.weight(.semibold))
                    .foregroundStyle(providerColor(card.provider))
                    .frame(width: 24)
                VStack(alignment: .leading, spacing: 3) {
                    Text(card.provider.title)
                        .font(.headline)
                    Text(providerModelCaption(card, costDetails: costDetails))
                        .font(.caption)
                        .foregroundStyle(.secondary)
                        .lineLimit(1)
                        .minimumScaleFactor(0.72)
                    if let reportedText = costDetails.reportedText {
                        Text(reportedText)
                            .font(.caption2)
                            .foregroundStyle(.tertiary)
                            .lineLimit(1)
                            .minimumScaleFactor(0.8)
                    }
                }
                .frame(width: 150, alignment: .leading)
                DashboardUsageCompositionBar(
                    segments: modelSegments(for: card),
                    total: totalTokens,
                    localization: localization
                )
                .frame(minWidth: 120, maxWidth: .infinity)
                VStack(alignment: .trailing, spacing: 3) {
                    Text(localization.percent(fraction * 100, fractionDigits: 1))
                        .font(.headline)
                        .monospacedDigit()
                    Text(metricText(card.tokens, localization: localization))
                        .font(.caption)
                        .foregroundStyle(.secondary)
                        .monospacedDigit()
                }
                .frame(width: 76, alignment: .trailing)
                Image(systemName: "chevron.right")
                    .font(.caption.bold())
                    .foregroundStyle(.tertiary)
            }
            .padding(.vertical, 2)
            .contentShape(Rectangle())
        }
        .buttonStyle(.plain)
        .keyboardShortcut(shortcut(for: card.provider), modifiers: [.command, .shift])
        .accessibilityElement(children: .combine)
        .accessibilityIdentifier("dashboard.summary.provider.\(card.provider.rawValue)")
        .accessibilityHint(localization.format("dashboard.open.provider", card.provider.title))
    }

    private func providerDistributionSection(fillsProposedHeight: Bool) -> some View {
        distributionSection(
            title: "工具分布",
            subtitle: "按客户端查看 Token 与费用占比",
            metric: $providerDistributionMetric,
            items: providerDonutItems(metric: providerDistributionMetric),
            total: distributionTotal(metric: providerDistributionMetric),
            fillsProposedHeight: fillsProposedHeight,
            accessibilityID: "dashboard.summary.distribution"
        )
    }

    private func modelDistributionSection(fillsProposedHeight: Bool) -> some View {
        distributionSection(
            title: "模型分布",
            subtitle: "按模型查看 Token 与费用占比",
            metric: $modelDistributionMetric,
            items: modelDonutItems(metric: modelDistributionMetric),
            total: distributionTotal(metric: modelDistributionMetric),
            fillsProposedHeight: fillsProposedHeight,
            accessibilityID: "dashboard.summary.models"
        )
    }

    private func distributionSection(
        title: String,
        subtitle: String,
        metric: Binding<DashboardDistributionMetric>,
        items: [DashboardDonutItem],
        total: Int64,
        fillsProposedHeight: Bool,
        accessibilityID: String
    ) -> some View {
        SectionCard(title: title, fillsProposedHeight: fillsProposedHeight) {
            VStack(alignment: .leading, spacing: 14) {
                HStack(alignment: .center, spacing: 12) {
                    Text(localization.textValue(subtitle))
                        .font(.subheadline)
                        .foregroundStyle(.secondary)
                    Spacer(minLength: 12)
                    Picker(localization.textValue("统计口径"), selection: metric) {
                        ForEach(DashboardDistributionMetric.allCases) { option in
                            Text(option.title(localization: localization)).tag(option)
                        }
                    }
                    .labelsHidden()
                    .pickerStyle(.segmented)
                    .frame(width: 138)
                }
                if items.isEmpty || total <= 0 {
                    ContentUnavailableView(
                        localization.textValue("当前范围暂无用量"),
                        systemImage: "chart.pie"
                    )
                    .frame(height: 210)
                } else {
                    DashboardDonutBreakdown(
                        items: items,
                        total: total,
                        metric: metric.wrappedValue,
                        localization: localization
                    )
                }
            }
        }
        .accessibilityIdentifier(accessibilityID)
    }

    private func providerDonutItems(
        metric: DashboardDistributionMetric
    ) -> [DashboardDonutItem] {
        presentation.distribution.compactMap { share in
            let value = metricValue(metric, tokens: share.tokens, cost: share.estimatedCost)
            guard let value, value > 0 else { return nil }
            return DashboardDonutItem(
                id: share.provider.rawValue,
                title: share.provider.title,
                value: value,
                color: providerColor(share.provider)
            )
        }
    }

    private func modelDonutItems(
        metric: DashboardDistributionMetric
    ) -> [DashboardDonutItem] {
        let candidates = presentation.models.compactMap { row -> DashboardDonutItem? in
            let value = metricValue(metric, tokens: row.tokens, cost: row.estimatedCost)
            guard let value, value > 0 else { return nil }
            return DashboardDonutItem(
                id: row.dimensionKey,
                title: row.displayName,
                value: value,
                color: modelColor(row.dimensionKey)
            )
        }
        let visible = Array(candidates.prefix(5))
        let visibleTotal = visible.reduce(Int64(0)) { $0 + $1.value }
        let remainder = max(distributionTotal(metric: metric) - visibleTotal, 0)
        guard remainder > 0 else { return visible }
        return visible + [DashboardDonutItem(
            id: "other",
            title: localization.textValue("其他"),
            value: remainder,
            color: .secondary.opacity(0.3)
        )]
    }

    private func distributionTotal(metric: DashboardDistributionMetric) -> Int64 {
        let metricValue = switch metric {
        case .tokens: presentation.totals.tokens
        case .cost: presentation.totals.estimatedCost
        }
        let global = chartValue(metricValue)
        let knownSubtotal: Int64 = switch metric {
        case .tokens:
            presentation.models.reduce(0) { $0 + max(chartValue($1.tokens), 0) }
        case .cost:
            presentation.models.reduce(0) { $0 + max(chartValue($1.estimatedCost), 0) }
        }
        return max(global, knownSubtotal)
    }

    private func metricValue(
        _ metric: DashboardDistributionMetric,
        tokens: DisplayMetric,
        cost: DisplayMetric
    ) -> Int64? {
        switch metric {
        case .tokens: knownChartValue(tokens)
        case .cost: knownChartValue(cost)
        }
    }

    private func modelSegments(
        for card: DashboardSummaryPresentation.ProviderCard
    ) -> [DashboardUsageSegment] {
        let models = presentation.models.filter { $0.provider == card.provider }
        var segments = models.compactMap { row -> DashboardUsageSegment? in
            guard let value = knownChartValue(row.tokens), value > 0 else { return nil }
            return DashboardUsageSegment(
                id: row.dimensionKey,
                title: row.displayName,
                value: value,
                color: modelColor(row.dimensionKey)
            )
        }
        let knownModels = segments.reduce(Int64(0)) { $0 + $1.value }
        let remainder = max(chartValue(card.tokens) - knownModels, 0)
        if remainder > 0 {
            segments.append(DashboardUsageSegment(
                id: "\(card.provider.rawValue):other",
                title: localization.textValue("其他"),
                value: remainder,
                color: providerColor(card.provider).opacity(0.45)
            ))
        }
        return segments
    }

    private func providerModelCaption(
        _ card: DashboardSummaryPresentation.ProviderCard,
        costDetails: DashboardSummaryPresentation.ProviderCard.CostDetails
    ) -> String {
        let count = localization.quantity("copy.count.model", count: Int64(card.modelCount))
        return "\(count) · \(costDetails.estimatedText)"
    }

    private func providerSymbol(_ provider: AgentProvider) -> String {
        switch provider {
        case .codex: "terminal.fill"
        case .cursor: "cursorarrow.rays"
        case .grok: "sparkles"
        }
    }

    private func modelColor(_ dimensionKey: String) -> Color {
        let palette: [Color] = [.blue, .mint, .orange, .purple, .red, .cyan, .pink, .indigo]
        guard let index = presentation.models.firstIndex(where: { $0.dimensionKey == dimensionKey }) else {
            return .secondary
        }
        return palette[index % palette.count]
    }

    private func quotaSection(fillsProposedHeight: Bool) -> some View {
        SectionCard(title: "各客户端额度", fillsProposedHeight: fillsProposedHeight) {
            if presentation.quotas.isEmpty {
                Text(localization.textValue("暂无额度数据"))
                    .foregroundStyle(.secondary)
            } else {
                VStack(alignment: .leading, spacing: 0) {
                    ForEach(Array(presentation.quotas.enumerated()), id: \.element.id) { index, card in
                        quotaRow(card)
                        if index < presentation.quotas.count - 1 {
                            Divider().padding(.vertical, 10)
                        }
                    }
                }
            }
        }
    }

    private func quotaRow(_ card: DashboardSummaryPresentation.QuotaCard) -> some View {
        let window = card.windows.first
        let progress = QuotaProgressPresentation(
            remainingPercent: window?.remainingPercent,
            localization: localization
        )
        return Button {
            onOpenProvider(card.provider)
        } label: {
            VStack(alignment: .leading, spacing: 7) {
                HStack(spacing: 8) {
                    Circle()
                        .fill(providerColor(card.provider))
                        .frame(width: 8, height: 8)
                    Text(card.provider.title)
                        .font(.headline)
                    Spacer(minLength: 10)
                    Text(progress.percentText)
                        .font(.title3.weight(.semibold))
                        .monospacedDigit()
                        .foregroundStyle(quotaLevelColor(progress.level))
                    Image(systemName: "chevron.right")
                        .font(.caption.bold())
                        .foregroundStyle(.tertiary)
                }
                ProgressView(value: progress.fraction)
                    .tint(quotaLevelColor(progress.level))
                    .opacity(window?.remainingPercent == nil ? 0.35 : 1)
                    .accessibilityLabel(localization.textValue("剩余"))
                    .accessibilityValue(progress.accessibilityValue)
                HStack(alignment: .firstTextBaseline, spacing: 8) {
                    Text(window?.title ?? localization.textValue("额度暂不可用"))
                        .font(.caption)
                        .foregroundStyle(.secondary)
                        .lineLimit(1)
                    Spacer(minLength: 8)
                    Text(quotaDetail(card: card, window: window))
                        .font(.caption2)
                        .foregroundStyle(.tertiary)
                        .lineLimit(1)
                }
            }
            .contentShape(Rectangle())
        }
        .buttonStyle(.plain)
        .accessibilityElement(children: .combine)
        .accessibilityIdentifier("dashboard.summary.quota.\(card.provider.rawValue)")
        .accessibilityHint(localization.format("dashboard.open.provider", card.provider.title))
    }

    private var activeClientsText: String {
        switch presentation.totals.activeProviders {
        case .known(let value, _):
            localization.quantity("copy.count.client", count: value)
        case .unknown, .absent:
            "--"
        }
    }

    private var dateRangeText: String {
        let zone = presentation.reportingTimeZone.isEmpty
            ? localization.textValue("本地时区")
            : presentation.reportingTimeZone
        guard let start = presentation.rangeStart, let end = presentation.rangeEnd else {
            return zone
        }
        var calendar = Calendar(identifier: .gregorian)
        calendar.timeZone = reportingTimeZone
        if calendar.isDate(start, inSameDayAs: end) {
            return "\(formatted(start)) · \(zone)"
        }
        return "\(formatted(start)) – \(formatted(end)) · \(zone)"
    }

    private var reportingTimeZone: TimeZone {
        TimeZone(identifier: presentation.reportingTimeZone) ?? .current
    }

    private func coverageText(known: Int32) -> String {
        localization.format(
            "dashboard.coverage.clients",
            localization.number(Int64(known)),
            localization.quantity("copy.count.client", count: Int64(presentation.coverage.totalProviderCount))
        )
    }

    private func quotaDetail(
        card: DashboardSummaryPresentation.QuotaCard,
        window: QuotaWindowPresentation?
    ) -> String {
        guard let window else { return quotaCoverageLabel(card.coverage) }
        return localization.format(
            "dashboard.quota.freshness",
            ProductCopy.status(window.freshness, localization: localization)
        )
    }

    private func coverageLabel(_ state: DashboardSummaryPresentation.Coverage.State) -> String {
        switch state {
        case .complete: localization.textValue("完整")
        case .partial: localization.textValue("部分数据")
        case .empty: localization.textValue("无用量")
        case .unavailable: localization.textValue("暂不可用")
        case .unknown: localization.textValue("暂时未知")
        }
    }

    private func quotaCoverageLabel(_ state: DashboardSummaryPresentation.Coverage.State) -> String {
        state == .empty ? localization.textValue("暂无额度数据") : coverageLabel(state)
    }



    private func shortcut(for provider: AgentProvider) -> KeyEquivalent {
        switch provider {
        case .codex: "1"
        case .cursor: "2"
        case .grok: "3"
        }
    }







    private func formatted(_ date: Date) -> String {
        let formatter = DateFormatter()
        formatter.locale = localization.locale
        formatter.timeZone = reportingTimeZone
        formatter.dateStyle = .medium
        formatter.timeStyle = .none
        return formatter.string(from: date)
    }
}

private struct DashboardActivityCard: View, Equatable {
    let activity: TokenActivityPresentation
    let localization: AppLocalization

    var body: some View {
        DashboardAnnualActivityHeatmap(
            activity: activity,
            localization: localization,
            card: TokenActivityCardPresentation(activity, localization: localization)
        )
    }
}

private func providerColor(_ provider: AgentProvider) -> Color {
        switch provider {
        case .codex: .blue
        case .cursor: .orange
        case .grok: .purple
        }
    }

private func chartValue(_ metric: DisplayMetric) -> Int64 {
        knownChartValue(metric) ?? 0
    }

private func knownChartValue(_ metric: DisplayMetric) -> Int64? {
        if case .known(let value, _) = metric { return value }
        return nil
    }

private struct DashboardTrendChart: View {
    let presentation: DashboardSummaryPresentation
    let selectedRange: DateRangePreset
    let trendMode: DashboardTrendMode
    let localization: AppLocalization
    @State private var hoveredTrendKey: String?
    private var reportingTimeZone: TimeZone {
        TimeZone(identifier: presentation.reportingTimeZone) ?? .current
    }
    var body: some View {
        Chart {
            if trendMode == .total {
                ForEach(presentation.trend) { point in
                    AreaMark(
                        x: .value(localization.textValue("时间"), point.date),
                        y: .value("Token", chartValue(point.tokens))
                    )
                    .interpolationMethod(.monotone)
                    .foregroundStyle(
                        LinearGradient(
                            colors: [
                                Color.accentColor.opacity(0.28),
                                Color.accentColor.opacity(0.03),
                            ],
                            startPoint: .top,
                            endPoint: .bottom
                        )
                    )
                    LineMark(
                        x: .value(localization.textValue("时间"), point.date),
                        y: .value("Token", chartValue(point.tokens))
                    )
                    .interpolationMethod(.monotone)
                    .foregroundStyle(Color.accentColor)
                    .lineStyle(StrokeStyle(lineWidth: 2.2, lineCap: .round, lineJoin: .round))
                    PointMark(
                        x: .value(localization.textValue("时间"), point.date),
                        y: .value("Token", chartValue(point.tokens))
                    )
                    .foregroundStyle(Color.accentColor)
                    .symbolSize(hoveredTrendKey == point.key ? 70 : 28)
                    .accessibilityLabel(axisText(for: point.date))
                    .accessibilityValue(metricText(point.tokens, localization: localization))
                }
            } else {
                ForEach(presentation.trend) { point in
                    ForEach(point.shares) { share in
                        BarMark(
                            x: .value(localization.textValue("时间"), point.date),
                            y: .value("Token", chartValue(share.tokens)),
                            width: .fixed(trendBarWidth)
                        )
                        .foregroundStyle(by: .value(
                            localization.textValue("客户端"),
                            share.provider.title
                        ))
                        .cornerRadius(3)
                    }
                }
            }
            if let selected = presentation.trend.first(where: { $0.key == hoveredTrendKey }) {
                RuleMark(x: .value(localization.textValue("时间"), selected.date))
                    .foregroundStyle(.secondary.opacity(0.45))
                    .lineStyle(StrokeStyle(lineWidth: 1, dash: [3, 3]))
                    .annotation(
                        position: .top,
                        alignment: .leading,
                        spacing: 8,
                        overflowResolution: .init(x: .fit(to: .chart), y: .disabled)
                    ) {
                        trendTooltip(selected)
                    }
            }
        }
        .chartForegroundStyleScale([
            AgentProvider.codex.title: providerColor(.codex),
            AgentProvider.cursor.title: providerColor(.cursor),
            AgentProvider.grok.title: providerColor(.grok),
        ])
        .chartLegend(.hidden)
        .chartXScale(
            range: .plotDimension(
                startPadding: CGFloat(trendLayout.horizontalPlotPadding),
                endPadding: CGFloat(trendLayout.horizontalPlotPadding)
            )
        )
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
            AxisMarks(values: trendAxisTicks) { value in
                AxisTick()
                AxisValueLabel(anchor: .top) {
                    if let date = value.as(Date.self) {
                        Text(axisText(for: date))
                    }
                }
            }
        }
        .chartOverlay { proxy in
            GeometryReader { geometry in
                Rectangle()
                    .fill(.clear)
                    .contentShape(Rectangle())
                    .onContinuousHover { phase in
                        switch phase {
                        case .active(let location):
                            guard let plotFrame = proxy.plotFrame else {
                                hoveredTrendKey = nil
                                return
                            }
                            let frame = geometry[plotFrame]
                            let plotX = location.x - frame.minX
                            guard plotX >= 0, plotX <= frame.width,
                                  let date: Date = proxy.value(atX: plotX)
                            else {
                                hoveredTrendKey = nil
                                return
                            }
                            hoveredTrendKey = presentation.trend.min(by: {
                                abs($0.date.timeIntervalSince(date))
                                    < abs($1.date.timeIntervalSince(date))
                            })?.key
                        case .ended:
                            hoveredTrendKey = nil
                        }
                    }
            }
        }
        .frame(height: 240)
        .accessibilityLabel(localization.textValue("Token 趋势"))
    }
    private func trendTooltip(
        _ point: DashboardSummaryPresentation.TrendPoint
    ) -> some View {
        VStack(alignment: .leading, spacing: 4) {
            Text(axisText(for: point.date))
                .font(.caption.weight(.semibold))
            Text(metricText(point.tokens, localization: localization))
                .font(.caption)
                .monospacedDigit()
            if trendMode == .stacked {
                ForEach(point.shares) { share in
                    HStack(spacing: 5) {
                        Circle()
                            .fill(providerColor(share.provider))
                            .frame(width: 6, height: 6)
                        Text(share.provider.title)
                        Text(metricText(share.tokens, localization: localization))
                            .monospacedDigit()
                    }
                    .font(.caption2)
                }
            }
        }
        .padding(.horizontal, 9)
        .padding(.vertical, 7)
        .background(.ultraThickMaterial, in: RoundedRectangle(cornerRadius: 7))
        .overlay {
            RoundedRectangle(cornerRadius: 7)
                .stroke(.separator.opacity(0.5), lineWidth: 0.5)
        }
        .shadow(color: .black.opacity(0.12), radius: 4, y: 2)
    }
    private var trendLayout: DashboardSummaryTrendChartLayout {
        DashboardSummaryTrendChartLayout(range: selectedRange)
    }
    private var trendBarWidth: CGFloat {
        CGFloat(trendLayout.barWidth)
    }
    private var trendAxisTicks: [Date] {
        trendLayout.axisTickIndices(pointCount: presentation.trend.count).map {
            presentation.trend[$0].date
        }
    }
    private func axisText(for date: Date) -> String {
        let formatter = DateFormatter()
        formatter.locale = localization.locale
        formatter.timeZone = reportingTimeZone
        formatter.setLocalizedDateFormatFromTemplate(selectedRange == .today ? "HHmm" : "Md")
        return formatter.string(from: date)
    }
}

// AppModel also publishes unrelated job, status-menu and detail state.
// Only actual dashboard inputs should rebuild the presentation and page.
private struct DashboardResponseContent: View, Equatable {
    let response: Codexpulse_Core_V1_DashboardSummaryResponse
    let selectedRange: DateRangePreset
    let trendMode: DashboardTrendMode
    let localization: AppLocalization
    let model: AppModel

    nonisolated static func == (lhs: Self, rhs: Self) -> Bool {
        lhs.model === rhs.model && lhs.response == rhs.response
            && lhs.selectedRange == rhs.selectedRange && lhs.trendMode == rhs.trendMode
            && lhs.localization == rhs.localization
    }

    var body: some View {
        DashboardSummaryContentView(
            presentation: DashboardSummaryPresentation(response),
            selectedRange: selectedRange, trendMode: trendMode,
            onSelectTrendMode: model.selectDashboardTrendMode,
            onOpenProvider: model.openMainWindowProvider,
            localization: localization
        )
    }
}
