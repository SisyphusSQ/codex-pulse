import AppKit
import Charts
import CodexPulseAppSupport
import CodexPulseProtocolGenerated
import SwiftUI

struct QuotaPaceStateView: View {
    let state: FeatureLoadState<Codexpulse_Core_V1_QuotaPaceResponse>
    let currentWindows: [Codexpulse_Core_V1_CurrentWindow]

    var body: some View {
        if let response = state.value {
            QuotaPaceCenterView(response: response, currentWindows: currentWindows)
        } else {
            SectionCard(title: "配额节奏中心") {
                switch state {
                case .loading:
                    HStack(spacing: 10) {
                        ProgressView().controlSize(.small)
                        Text("正在计算本周期消耗节奏…")
                    }
                    .foregroundStyle(.secondary)
                case .unavailable:
                    Label("节奏数据暂时不可用，请稍后重试。", systemImage: "chart.line.downtrend.xyaxis")
                        .foregroundStyle(.orange)
                case .cancelled:
                    Label("节奏数据加载已取消。", systemImage: "xmark.circle")
                        .foregroundStyle(.secondary)
                case .idle, .empty:
                    Text("当前还没有足够的额度观测。")
                        .foregroundStyle(.secondary)
                case .ready, .partial, .stale:
                    EmptyView()
                }
            }
            .accessibilityIdentifier("quota.pace.center")
        }
    }
}

private struct QuotaPaceCenterView: View {
    let response: Codexpulse_Core_V1_QuotaPaceResponse
    let currentWindows: [Codexpulse_Core_V1_CurrentWindow]
    @State private var selectedWindowID: String?

    private var windows: [QuotaPaceWindowPresentation] {
        response.pace.windows.map {
            QuotaPaceWindowPresentation(
                $0,
                evaluatedAtMS: response.pace.evaluatedAtMs
            )
        }
    }

    private var selectedWindow: QuotaPaceWindowPresentation? {
        windows.first { $0.id == selectedWindowID } ?? windows.first
    }

    private var titleByID: [String: String] {
        Dictionary(
            currentWindows.map {
                let presentation = QuotaWindowPresentation($0)
                return (presentation.id, presentation.title)
            },
            uniquingKeysWith: { first, _ in first }
        )
    }

    var body: some View {
        SectionCard(title: "配额节奏中心") {
            VStack(alignment: .leading, spacing: 14) {
                windowPickerHeader

                if let selectedWindow {
                    QuotaPaceWindowView(
                        presentation: selectedWindow,
                        title: title(for: selectedWindow)
                    )
                } else {
                    ContentUnavailableView(
                        "当前还没有节奏数据",
                        systemImage: "chart.line.downtrend.xyaxis"
                    )
                    .frame(maxWidth: .infinity, minHeight: 180)
                }
            }
        }
        .accessibilityIdentifier("quota.pace.center")
        .onAppear { normalizeSelection() }
        .onChange(of: windows.map(\.id)) { _, _ in normalizeSelection() }
    }

    @ViewBuilder
    private var windowPickerHeader: some View {
        if windows.count > 1 {
            VStack(alignment: .leading, spacing: 10) {
                headerDescription
                windowPicker
            }
        } else {
            headerDescription
        }
    }

    private var headerDescription: some View {
        Text("把本周期用量和时间进度放在一起看，并参考最近四个完整周期。")
            .font(.subheadline)
            .foregroundStyle(.secondary)
    }

    private var windowPicker: some View {
        GeometryReader { geometry in
            let width = QuotaPaceWindowPickerLayout.width(
                availableWidth: geometry.size.width,
                segmentedIdealWidth: segmentedPickerIdealWidth
            )
            picker
                .pickerStyle(.segmented)
                .tint(.blue)
                .frame(width: width, height: 26)
                .clipped()
                .frame(maxWidth: .infinity, alignment: .trailing)
                .labelsHidden()
        }
        .frame(height: 26)
        .accessibilityIdentifier("quota.pace.window-picker")
    }

    private var picker: some View {
        Picker("额度窗口", selection: selectedWindowBinding) {
            ForEach(windows) { window in
                Text(localizedCopy(title(for: window))).tag(window.id)
            }
        }
    }

    private var segmentedPickerIdealWidth: CGFloat {
        let control = NSSegmentedControl(
            labels: windows.map { localizedCopy(title(for: $0)) },
            trackingMode: .selectOne,
            target: nil,
            action: nil
        )
        control.controlSize = .regular
        return control.fittingSize.width
    }

    private var selectedWindowBinding: Binding<String> {
        Binding(
            get: { selectedWindow?.id ?? "" },
            set: { selectedWindowID = $0 }
        )
    }

    private func normalizeSelection() {
        if let selectedWindowID, windows.contains(where: { $0.id == selectedWindowID }) {
            return
        }
        selectedWindowID = windows.first?.id
    }

    private func title(for window: QuotaPaceWindowPresentation) -> String {
        if let title = titleByID[window.id] { return title }
        return localizedCopy(window.windowKind == "primary" ? "主额度窗口" : "次额度窗口")
    }
}

private struct QuotaPaceWindowView: View {
    let presentation: QuotaPaceWindowPresentation
    let title: String
    private let xAxis = QuotaPaceChartXAxisPresentation.percentage
    private var localization: AppLocalization { AppLocalizationRegistry.shared.current }

    var body: some View {
        VStack(alignment: .leading, spacing: 14) {
            HStack(alignment: .firstTextBaseline) {
                Text(localization.textValue(title))
                    .font(.headline)
                Spacer()
                Label(presentation.paceText, systemImage: paceSymbol)
                    .font(.subheadline.weight(.semibold))
                    .foregroundStyle(paceColor)
                    .accessibilityIdentifier("quota.pace.summary.\(presentation.id)")
            }

            LazyVGrid(
                columns: Array(repeating: GridItem(.flexible(), spacing: 10), count: 4),
                spacing: 10
            ) {
                paceMetric(title: "已使用", value: percent(presentation.usedPercent))
                paceMetric(title: "周期进度", value: percent(presentation.elapsedPercent))
                paceMetric(title: "节奏差", value: presentation.paceDeltaMetricText)
                paceMetric(
                    title: "历史基线",
                    value: presentation.historyCycleCount > 0
                        ? localization.quantity(
                            "copy.count.cycle", count: presentation.historyCycleCount
                        ) : "--"
                )
            }

            paceChart

            HStack(alignment: .top, spacing: 12) {
                forecastCard
                comparisonCard
            }
        }
    }

    private var paceChart: some View {
        VStack(alignment: .leading, spacing: 8) {
            HStack(spacing: 14) {
                legend(
                    color: .secondary,
                    title: "本周期",
                    dashed: true,
                    endpointColor: .accentColor
                )
                legend(
                    color: .secondary,
                    title: "上一周期",
                    dashed: true,
                    endpointColor: .secondary
                )
                legend(color: .purple, title: "近四周期中位数", dashed: true)
                legend(color: .gray, title: "理想节奏", dashed: true)
                Spacer()
            }
            .font(.caption)
            .foregroundStyle(.secondary)

            Chart {
                ForEach(presentation.historyBand) { point in
                    AreaMark(
                        x: .value("周期进度", point.elapsedPercent),
                        yStart: .value("历史下界", point.minimumRemaining),
                        yEnd: .value("历史上界", point.maximumRemaining)
                    )
                    .foregroundStyle(Color.purple.opacity(0.10))
                }
                ForEach(presentation.historyBand) { point in
                    LineMark(
                        x: .value("周期进度", point.elapsedPercent),
                        y: .value("历史中位数", point.medianRemaining)
                    )
                    .foregroundStyle(Color.purple.opacity(0.75))
                    .lineStyle(StrokeStyle(lineWidth: 1.5, dash: [6, 5]))
                    .interpolationMethod(.monotone)
                }
                ForEach(presentation.previousPoints) { point in
                    LineMark(
                        x: .value("周期进度", point.elapsedPercent),
                        y: .value("上一周期剩余", point.remainingPercent),
                        series: .value("趋势系列", point.series)
                    )
                    .foregroundStyle(Color.secondary.opacity(0.75))
                    .lineStyle(StrokeStyle(lineWidth: 1.5, dash: [6, 5]))
                    .interpolationMethod(.stepEnd)
                }
                ForEach(presentation.currentPoints) { point in
                    LineMark(
                        x: .value("周期进度", point.elapsedPercent),
                        y: .value("本周期剩余", point.remainingPercent),
                        series: .value("趋势系列", point.series)
                    )
                    .foregroundStyle(Color.secondary.opacity(0.75))
                    .lineStyle(StrokeStyle(lineWidth: 1.5, dash: [6, 5]))
                    .interpolationMethod(.stepEnd)
                }
                if let point = presentation.previousPoints.last {
                    PointMark(
                        x: .value("上一周期结束进度", point.elapsedPercent),
                        y: .value("上一周期结束剩余", point.remainingPercent)
                    )
                    .foregroundStyle(Color.secondary)
                    .symbolSize(36)
                }
                if let point = presentation.currentPoints.last {
                    PointMark(
                        x: .value("当前周期进度", point.elapsedPercent),
                        y: .value("当前剩余", point.remainingPercent)
                    )
                    .foregroundStyle(Color.accentColor)
                    .symbolSize(52)
                }
            }
            .chartXScale(domain: 0...100)
            .chartYScale(domain: 0...100)
            .chartPlotStyle { plotArea in
                plotArea
                    .background {
                        QuotaPaceIdealReferenceShape()
                            .stroke(
                                Color.gray.opacity(0.7),
                                style: StrokeStyle(lineWidth: 1.5, dash: [3, 4])
                            )
                            .allowsHitTesting(false)
                    }
                    .overlay {
                        QuotaPacePlotBorderShape()
                            .strokeBorder(
                                Color.secondary.opacity(0.65),
                                style: StrokeStyle(lineWidth: 1.5, dash: [6, 5])
                            )
                            .allowsHitTesting(false)
                    }
            }
            .chartXAxis {
                AxisMarks(values: xAxis.gridLineValues) { _ in
                    AxisGridLine().foregroundStyle(Color.secondary.opacity(0.10))
                }
                AxisMarks(values: xAxis.labelValues) { value in
                    AxisValueLabel {
                        if let value = value.as(Double.self) {
                            Text("\(Int(value))%")
                        }
                    }
                }
            }
            .chartYAxis {
                AxisMarks(position: .leading, values: [0, 25, 50, 75, 100]) { value in
                    AxisGridLine().foregroundStyle(Color.secondary.opacity(0.10))
                    AxisValueLabel {
                        if let value = value.as(Double.self) {
                            Text("\(Int(value))%")
                        }
                    }
                }
            }
            .frame(height: 250)
            .accessibilityIdentifier("quota.pace.chart.\(presentation.id)")

            HStack {
                Text("横轴：周期进度")
                Spacer()
                Text("纵轴：剩余额度")
            }
            .font(.caption2)
            .foregroundStyle(.secondary)
        }
    }

    private var forecastCard: some View {
        VStack(alignment: .leading, spacing: 8) {
            Label("保守耗尽推算", systemImage: forecastSymbol)
                .font(.subheadline.weight(.semibold))
                .foregroundStyle(forecastColor)
            Text(presentation.forecastText)
                .font(.headline)
            if let evidenceText = presentation.evidenceText {
                Text(evidenceText)
                    .font(.caption)
                    .foregroundStyle(.secondary)
            }
        }
        .frame(maxWidth: .infinity, alignment: .leading)
        .padding(12)
        .background(forecastColor.opacity(0.08), in: RoundedRectangle(cornerRadius: 10))
        .accessibilityIdentifier("quota.pace.forecast.\(presentation.id)")
    }

    private var comparisonCard: some View {
        VStack(alignment: .leading, spacing: 8) {
            Label("同进度对比", systemImage: "arrow.left.and.right")
                .font(.subheadline.weight(.semibold))
            comparisonRow("上一周期", presentation.previousComparisonText)
            comparisonRow(
                presentation.historyCycleCount > 0
                    ? localization.format(
                        "quota.pace.historyTarget",
                        localization.quantity(
                            "copy.count.cycle", count: presentation.historyCycleCount
                        )
                    ) : localization.textValue("最近周期"),
                presentation.historyComparisonText
            )
        }
        .frame(maxWidth: .infinity, alignment: .leading)
        .padding(12)
        .background(Color.secondary.opacity(0.07), in: RoundedRectangle(cornerRadius: 10))
        .accessibilityIdentifier("quota.pace.comparison.\(presentation.id)")
    }

    private var paceColor: Color {
        guard let delta = presentation.paceDeltaPP else { return .secondary }
        if delta > 5 { return .orange }
        if delta < -5 { return .green }
        return .secondary
    }

    private var paceSymbol: String {
        guard let delta = presentation.paceDeltaPP else { return "questionmark.circle" }
        if delta > 5 { return "speedometer" }
        if delta < -5 { return "leaf" }
        return "equal.circle"
    }

    private var forecastColor: Color {
        switch presentation.forecastState {
        case "at_risk", "exhausted": .orange
        case "on_track": .green
        default: .secondary
        }
    }

    private var forecastSymbol: String {
        switch presentation.forecastState {
        case "at_risk": "clock.badge.exclamationmark"
        case "exhausted": "exclamationmark.octagon"
        case "on_track": "checkmark.circle"
        default: "ellipsis.circle"
        }
    }

    private func paceMetric(title: String, value: String) -> some View {
        let localization = AppLocalizationRegistry.shared.current
        return VStack(alignment: .leading, spacing: 4) {
            Text(localization.textValue(title))
                .font(.caption)
                .foregroundStyle(.secondary)
            Text(value)
                .font(.title3.weight(.semibold))
                .monospacedDigit()
        }
        .frame(maxWidth: .infinity, alignment: .leading)
        .padding(10)
        .background(Color.secondary.opacity(0.06), in: RoundedRectangle(cornerRadius: 9))
    }

    private func comparisonRow(_ title: String, _ value: String?) -> some View {
        let localization = AppLocalizationRegistry.shared.current
        return HStack {
            Text(localization.textValue(title)).foregroundStyle(.secondary)
            Spacer()
            Text(value ?? localization.textValue("暂无完整对比"))
                .fontWeight(.medium)
                .multilineTextAlignment(.trailing)
        }
        .font(.caption)
    }

    private func legend(
        color: Color,
        title: String,
        dashed: Bool = false,
        endpointColor: Color? = nil
    ) -> some View {
        let localization = AppLocalizationRegistry.shared.current
        return HStack(spacing: 5) {
            ZStack(alignment: .trailing) {
                Capsule()
                    .fill(color)
                    .frame(width: 18, height: dashed ? 2 : 3)
                    .overlay {
                        if dashed {
                            HStack(spacing: 3) {
                                Rectangle().fill(color).frame(width: 5)
                                Rectangle().fill(.clear).frame(width: 3)
                                Rectangle().fill(color).frame(width: 5)
                            }
                        }
                    }
                if let endpointColor {
                    Circle()
                        .fill(endpointColor)
                        .frame(width: 6, height: 6)
                }
            }
            .frame(width: 18, height: 6)
            Text(localization.textValue(title))
        }
    }

    private func percent(_ value: Double?) -> String {
        guard let value else { return "--" }
        return AppLocalizationRegistry.shared.current.percent(value)
    }

}
