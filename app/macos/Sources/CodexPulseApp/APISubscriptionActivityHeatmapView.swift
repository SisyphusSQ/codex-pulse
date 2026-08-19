import CodexPulseAppSupport
import CodexPulseProtocolGenerated
import SwiftUI

struct APISubscriptionActivityHeatmapView: View {
    let activity: Codexpulse_Core_V1_APISubscriptionActivityCalendar
    let localization: AppLocalization

    @State private var selectedCurrency: String?
    @State private var hoverState = TokenActivityHoverState()

    init(
        activity: Codexpulse_Core_V1_APISubscriptionActivityCalendar,
        localization: AppLocalization
    ) {
        self.activity = activity
        self.localization = localization
        let currencies = Set(activity.days.flatMap { $0.deepSeek.map(\.currency) })
            .filter { !$0.isEmpty }
            .sorted()
        _selectedCurrency = State(initialValue: currencies.first)
    }

    var body: some View {
        if let presentation = APISubscriptionActivityCalendarPresentation(
            activity,
            currency: selectedCurrency,
            localization: localization
        ) {
            SectionCard(title: localization.textValue("API 与订阅活动")) {
                header(presentation)
                metricStrip(presentation)
                Text(localization.textValue("DeepSeek 金额按采样记录估算；方块颜色由当天任一来源的最高档位决定。"))
                    .font(.caption)
                    .foregroundStyle(.secondary)
                Divider()
                heatmap(presentation)
            }
            .accessibilityIdentifier("api-subscriptions.activity-heatmap")
        }
    }

    private func header(
        _ presentation: APISubscriptionActivityCalendarPresentation
    ) -> some View {
        HStack(spacing: 10) {
            Text(localization.textValue("过去 365 天"))
                .font(.subheadline.weight(.semibold))
            if let selectedDay = selectedDay(presentation) {
                Text(selectedDay.id)
                    .font(.caption)
                    .foregroundStyle(.secondary)
            }
            Spacer()
            if presentation.availableCurrencies.count > 1 {
                Picker(localization.textValue("币种"), selection: $selectedCurrency) {
                    ForEach(presentation.availableCurrencies, id: \.self) { currency in
                        Text(currency).tag(Optional(currency))
                    }
                }
                .labelsHidden()
                .frame(width: 110)
            }
        }
    }

    private func metricStrip(
        _ presentation: APISubscriptionActivityCalendarPresentation
    ) -> some View {
        let day = selectedDay(presentation)
        return HStack(alignment: .top, spacing: 14) {
            activityMetric(
                localization.textValue("DeepSeek 总充值"),
                money(day?.deepSeekTotalRecharged, currency: presentation.currency)
            )
            Divider().frame(height: 48)
            activityMetric(
                localization.textValue("DeepSeek 总消耗"),
                money(day?.deepSeekTotalConsumed, currency: presentation.currency)
            )
            Divider().frame(height: 48)
            activityMetric(
                localization.textValue("OpenCode Go 5 小时峰值已用"),
                percent(day?.openCodeGoMaxUsedPercent)
            )
            Divider().frame(height: 48)
            activityMetric(
                localization.textValue("OpenCode Go 5 小时剩余"),
                percent(day?.openCodeGoLatestRemainingPercent)
            )
        }
    }

    private func activityMetric(_ title: String, _ value: String) -> some View {
        VStack(alignment: .leading, spacing: 5) {
            Text(value)
                .font(.system(size: 20, weight: .semibold, design: .rounded))
                .monospacedDigit()
            Text(title)
                .font(.caption)
                .foregroundStyle(.secondary)
                .lineLimit(1)
                .minimumScaleFactor(0.72)
        }
        .frame(maxWidth: .infinity, alignment: .leading)
        .accessibilityElement(children: .combine)
    }

    private func heatmap(
        _ presentation: APISubscriptionActivityCalendarPresentation
    ) -> some View {
        GeometryReader { geometry in
            let weekCount = max(presentation.calendar.weeks.count, 1)
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
                    draw(
                        context: &context,
                        presentation: presentation,
                        layout: layout,
                        gridOrigin: gridOrigin,
                        labelWidth: labelWidth
                    )
                }
                if let day = selectedDay(presentation), hoverState.dayID != nil,
                   let position = position(for: day.id, presentation: presentation),
                   let frame = layout.cellFrame(column: position.column, row: position.row)
                {
                    Text(day.id)
                        .font(.caption)
                        .padding(.horizontal, 8)
                        .padding(.vertical, 5)
                        .background(.ultraThickMaterial, in: RoundedRectangle(cornerRadius: 6))
                        .shadow(color: .black.opacity(0.14), radius: 5, y: 2)
                        .position(
                            x: min(max(gridOrigin.x + frame.midX, 70), max(geometry.size.width - 70, 70)),
                            y: max(gridOrigin.y + frame.minY - 18, 0)
                        )
                        .allowsHitTesting(false)
                }
            }
            .contentShape(Rectangle())
            .onContinuousHover { phase in
                switch phase {
                case .active(let location):
                    updateSelection(dayID: day(
                        at: CGPoint(x: location.x - gridOrigin.x, y: location.y - gridOrigin.y),
                        presentation: presentation,
                        layout: layout
                    )?.id)
                case .ended:
                    updateSelection(dayID: nil)
                }
            }
            .accessibilityElement(children: .ignore)
            .accessibilityLabel(localization.text("API 与订阅活动"))
            .accessibilityValue(Text(verbatim: selectedDay(presentation)?.id ?? "--"))
            .accessibilityAdjustableAction { direction in
                let movement: ActivityHeatmapSelectionDirection
                switch direction {
                case .increment: movement = .increment
                case .decrement: movement = .decrement
                @unknown default: return
                }
                updateSelection(dayID: ActivityHeatmapSelectionResolver.move(
                    from: hoverState.dayID,
                    orderedIDs: presentation.days.map(\.id),
                    direction: movement
                ))
            }
        }
        .frame(maxWidth: .infinity)
        .aspectRatio(6.8, contentMode: .fit)
    }

    private func draw(
        context: inout GraphicsContext,
        presentation: APISubscriptionActivityCalendarPresentation,
        layout: ActivityHeatmapGridLayout,
        gridOrigin: CGPoint,
        labelWidth: CGFloat
    ) {
        for row in 0..<7 {
            let label = row == 0 ? "一" : row == 2 ? "三" : row == 4 ? "五" : ""
            guard !label.isEmpty, let frame = layout.cellFrame(column: 0, row: row) else { continue }
            context.draw(
                Text(verbatim: localization.textValue(label)).font(.caption2).foregroundStyle(.secondary),
                at: CGPoint(x: labelWidth, y: gridOrigin.y + frame.midY),
                anchor: .trailing
            )
        }
        for label in presentation.calendar.monthLabels {
            guard let frame = layout.cellFrame(column: label.weekIndex, row: 0) else { continue }
            context.draw(
                Text(label.title).font(.caption2).foregroundStyle(.secondary),
                at: CGPoint(x: gridOrigin.x + frame.minX, y: 0),
                anchor: .topLeading
            )
        }
        for (column, week) in presentation.calendar.weeks.enumerated() {
            for (row, day) in week.days.enumerated() {
                guard let day, let frame = layout.cellFrame(column: column, row: row) else { continue }
                let resolvedFrame = frame.offsetBy(dx: gridOrigin.x, dy: gridOrigin.y)
                let path = Path(roundedRect: resolvedFrame, cornerRadius: max(2, layout.cellSize * 0.22))
                context.fill(
                    path,
                    with: .color(activityHeatmapColor(for: day.intensity, tint: .blue))
                )
                if day.intensity == .unknown {
                    context.stroke(path, with: .color(.secondary.opacity(0.28)), lineWidth: 0.7)
                }
            }
        }
    }

    private func selectedDay(
        _ presentation: APISubscriptionActivityCalendarPresentation
    ) -> APISubscriptionActivityDayPresentation? {
        hoverState.dayID.flatMap { id in presentation.days.first(where: { $0.id == id }) }
            ?? presentation.days.last
    }

    private func day(
        at point: CGPoint,
        presentation: APISubscriptionActivityCalendarPresentation,
        layout: ActivityHeatmapGridLayout
    ) -> TokenActivityCalendarDay? {
        guard let position = layout.position(at: point),
              presentation.calendar.weeks.indices.contains(position.column),
              presentation.calendar.weeks[position.column].days.indices.contains(position.row)
        else { return nil }
        return presentation.calendar.weeks[position.column].days[position.row]
    }

    private func position(
        for dayID: String,
        presentation: APISubscriptionActivityCalendarPresentation
    ) -> ActivityHeatmapGridPosition? {
        for (column, week) in presentation.calendar.weeks.enumerated() {
            if let row = week.days.firstIndex(where: { $0?.id == dayID }) {
                return ActivityHeatmapGridPosition(column: column, row: row)
            }
        }
        return nil
    }

    private func updateSelection(dayID: String?) {
        guard hoverState.dayID != dayID else { return }
        withAnimation(.easeOut(duration: 0.12)) {
            hoverState = TokenActivityHoverState(dayID: dayID)
        }
    }

    private func money(_ value: String?, currency: String?) -> String {
        guard let value, let currency else { return "--" }
        return "\(value) \(currency)"
    }

    private func percent(_ value: Double?) -> String {
        guard let value else { return "--" }
        return localization.percent(value)
    }
}
