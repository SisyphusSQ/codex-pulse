import Charts
import CodexPulseAppSupport
import CodexPulseProtocolGenerated
import SwiftUI

struct APISubscriptionsView: View {
    @ObservedObject var model: AppModel
    @State private var selectedBalancePeriod: BalancePeriodSelection = .month
    @State private var selectedBalanceTrendDate: Date?

    private enum BalancePeriodSelection: String, CaseIterable, Identifiable {
        case today
        case week
        case month

        var id: String { rawValue }
    }

    private var localization: AppLocalization { model.localization }

    var body: some View {
        FeatureStateView(
            state: model.apiSubscriptionsState,
            emptyTitle: localization.textValue("API 与订阅暂不可用"),
            emptySystemImage: "creditcard.and.123"
        ) { response in
            ScrollView {
                VStack(alignment: .leading, spacing: 16) {
                    VStack(alignment: .leading, spacing: 3) {
                        Text(AppFeature.apiSubscriptions.title(localization: localization))
                            .font(.largeTitle.bold())
                        Text(localization.textValue("查看独立 API 余额和订阅额度；这些数据不参与客户端用量统计。"))
                            .foregroundStyle(.secondary)
                    }
                    VStack(alignment: .leading, spacing: 16) {
                        APISubscriptionActivityHeatmapView(
                            activity: response.activityCalendar,
                            localization: localization
                        )
                        deepSeekCard(response.deepSeek)
                        openCodeGoCard(response.openCodeGo)
                    }
                    Text(localization.format("更新于 %@", formattedDate(response.evaluatedAtMs)))
                        .font(.caption)
                        .foregroundStyle(.tertiary)
                }
                .padding(20)
            }
        }
        .accessibilityIdentifier("page.api-subscriptions")
    }

    private func deepSeekCard(
        _ snapshot: Codexpulse_Core_V1_DeepSeekAPISubscriptionSnapshot
    ) -> some View {
        SectionCard(title: "DeepSeek API") {
            sourceStatus(snapshot.status)
            if snapshot.hasBalance {
                KeyValueRow(
                    key: localization.textValue("账户状态"),
                    value: snapshot.balance.isAvailable
                        ? localization.textValue("可用")
                        : localization.textValue("暂不可用")
                )
                Divider()
                ForEach(Array(snapshot.balance.balances.enumerated()), id: \.offset) { _, balance in
                    VStack(alignment: .leading, spacing: 8) {
                        Text(balance.currency).font(.caption).foregroundStyle(.secondary)
                        Text("\(balance.total) \(balance.currency)")
                            .font(.title2.bold())
                            .monospacedDigit()
                            .textSelection(.enabled)
                        KeyValueRow(key: localization.textValue("赠金余额"), value: balance.granted)
                        KeyValueRow(key: localization.textValue("充值余额"), value: balance.toppedUp)
                    }
                }
                Divider()
                deepSeekPeriodSection(snapshot)
            } else {
                unavailableSourceCopy(snapshot.status.state)
            }
        }
        .frame(maxWidth: .infinity, alignment: .topLeading)
    }

    @ViewBuilder
    private func deepSeekPeriodSection(
        _ snapshot: Codexpulse_Core_V1_DeepSeekAPISubscriptionSnapshot
    ) -> some View {
        VStack(alignment: .leading, spacing: 12) {
            HStack {
                Text(localization.textValue("余额周期"))
                    .font(.headline)
                Spacer()
                Picker(localization.textValue("余额周期"), selection: $selectedBalancePeriod) {
                    ForEach(BalancePeriodSelection.allCases) { period in
                        Text(periodTitle(period)).tag(period)
                    }
                }
                .labelsHidden()
                .pickerStyle(.segmented)
                .frame(width: 240)
            }

            if let period = snapshot.periods.first(where: {
                $0.kind == selectedBalancePeriod.rawValue
            }), period.hasBaselineAtMs, !period.changes.isEmpty {
                Text(localization.format("本周期首次记录于 %@", formattedDate(period.baselineAtMs)))
                    .font(.caption)
                    .foregroundStyle(.secondary)
                ForEach(Array(period.changes.enumerated()), id: \.offset) { _, change in
                    let current = snapshot.balance.balances.first(where: {
                        $0.currency == change.currency
                    })
                    let currentTotal = current?.total ?? "--"
                    VStack(alignment: .leading, spacing: 10) {
                        Text(change.currency)
                            .font(.caption)
                            .foregroundStyle(.secondary)
                        if let series = period.series.first(where: {
                            $0.currency == change.currency
                        }), let presentation = APISubscriptionBalanceTrendPresentation(
                            series: series
                        ) {
                            balanceTrend(presentation)
                        }
                        HStack(alignment: .top, spacing: 12) {
                            balanceMetric(
                                localization.textValue("期初记录"),
                                "\(change.startingTotal) \(change.currency)"
                            )
                            balanceMetric(
                                localization.textValue("当前余额"),
                                "\(currentTotal) \(change.currency)"
                            )
                            balanceMetric(
                                localization.textValue("余额变化"),
                                "\(signedDelta(change.totalDelta)) \(change.currency)",
                                color: deltaColor(change.totalDelta)
                            )
                        }
                        KeyValueRow(
                            key: localization.textValue("赠金余额变化"),
                            value: signedDelta(change.grantedDelta)
                        )
                        KeyValueRow(
                            key: localization.textValue("充值余额变化"),
                            value: signedDelta(change.toppedUpDelta)
                        )
                        KeyValueRow(
                            key: localization.textValue("总充值（采样估算）"),
                            value: "\(change.totalRecharged) \(change.currency)"
                        )
                        KeyValueRow(
                            key: localization.textValue("总消耗（采样估算）"),
                            value: "\(change.totalConsumed) \(change.currency)"
                        )
                    }
                }
            } else {
                Text(localization.textValue("本周期暂无可比较记录"))
                    .font(.callout)
                    .foregroundStyle(.secondary)
            }
        }
        .onChange(of: selectedBalancePeriod) { _, _ in
            selectedBalanceTrendDate = nil
        }
    }

    private func balanceTrend(
        _ presentation: APISubscriptionBalanceTrendPresentation
    ) -> some View {
        let selectedPoint = presentation.nearest(to: selectedBalanceTrendDate)
        let color: Color = .blue
        return VStack(alignment: .leading, spacing: 8) {
            HStack(alignment: .firstTextBaseline, spacing: 8) {
                Text(localization.textValue("余额趋势"))
                    .font(.subheadline.weight(.semibold))
                Text(localization.textValue("按采样记录显示"))
                    .font(.caption)
                    .foregroundStyle(.secondary)
                Spacer()
                if let selectedPoint {
                    Text("\(selectedPoint.totalText) \(presentation.currency)")
                        .font(.caption.weight(.semibold))
                        .monospacedDigit()
                        .foregroundStyle(color)
                }
            }
            Chart {
                if let first = presentation.points.first {
                    RuleMark(y: .value("期初余额", first.total))
                        .foregroundStyle(Color.secondary.opacity(0.45))
                        .lineStyle(StrokeStyle(lineWidth: 1, dash: [4, 4]))
                }
                ForEach(presentation.points) { point in
                    LineMark(
                        x: .value("采样时间", point.date),
                        y: .value("余额", point.total)
                    )
                    .interpolationMethod(.stepEnd)
                    .foregroundStyle(color)
                    .lineStyle(StrokeStyle(lineWidth: 2))
                    .accessibilityLabel(formattedDate(point.observedAtMs))
                    .accessibilityValue("\(point.totalText) \(presentation.currency)")
                }
                if let last = presentation.points.last {
                    PointMark(
                        x: .value("最新采样时间", last.date),
                        y: .value("最新余额", last.total)
                    )
                    .foregroundStyle(color)
                    .symbolSize(42)
                }
                if let selectedPoint {
                    RuleMark(x: .value("选中采样时间", selectedPoint.date))
                        .foregroundStyle(Color.secondary.opacity(0.55))
                        .lineStyle(StrokeStyle(lineWidth: 1, dash: [4, 4]))
                        .annotation(
                            position: .top,
                            alignment: .leading,
                            spacing: 8,
                            overflowResolution: .init(x: .fit(to: .chart), y: .disabled)
                        ) {
                            balanceTrendDetail(selectedPoint, currency: presentation.currency)
                        }
                }
            }
            .chartYScale(domain: presentation.yDomain)
            .chartXScale(range: .plotDimension(startPadding: 28, endPadding: 44))
            .chartYAxis {
                AxisMarks(position: .leading) { value in
                    AxisGridLine().foregroundStyle(.quaternary)
                    AxisValueLabel {
                        if let amount = value.as(Double.self) {
                            Text(amount.formatted(
                                .number.precision(.fractionLength(0...4))
                            ))
                            .monospacedDigit()
                        }
                    }
                }
            }
            .chartXAxis {
                AxisMarks(values: .automatic(desiredCount: 5)) { value in
                    AxisGridLine().foregroundStyle(.quaternary)
                    AxisTick()
                    AxisValueLabel {
                        if let date = value.as(Date.self) {
                            Text(date.formatted(
                                .dateTime.month(.twoDigits).day(.twoDigits)
                                    .hour(.twoDigits(amPM: .omitted)).minute(.twoDigits)
                            ))
                        }
                    }
                }
            }
            .chartXSelection(value: $selectedBalanceTrendDate)
            .chartOverlay { proxy in
                GeometryReader { geometry in
                    Rectangle()
                        .fill(.clear)
                        .contentShape(Rectangle())
                        .onContinuousHover { phase in
                            switch phase {
                            case .active(let location):
                                guard let plotFrame = proxy.plotFrame else {
                                    selectedBalanceTrendDate = nil
                                    return
                                }
                                let plotRect = geometry[plotFrame]
                                guard plotRect.contains(location) else {
                                    selectedBalanceTrendDate = nil
                                    return
                                }
                                selectedBalanceTrendDate = proxy.value(
                                    atX: location.x - plotRect.origin.x,
                                    as: Date.self
                                )
                            case .ended:
                                selectedBalanceTrendDate = nil
                            }
                        }
                }
            }
            .frame(height: 190)
            if presentation.points.count == 1 {
                Text(localization.textValue("本周期只有 1 条记录，暂不能形成趋势"))
                    .font(.caption)
                    .foregroundStyle(.secondary)
            }
        }
    }

    private func balanceTrendDetail(
        _ point: APISubscriptionBalanceTrendPointPresentation,
        currency: String
    ) -> some View {
        VStack(alignment: .leading, spacing: 3) {
            Text(formattedDate(point.observedAtMs))
                .font(.caption.weight(.semibold))
            Text(localization.format("余额 %@ %@", point.totalText, currency))
            Text(localization.format("赠金 %@", point.grantedText))
            Text(localization.format("充值 %@", point.toppedUpText))
        }
        .font(.caption2)
        .monospacedDigit()
        .padding(7)
        .background(.regularMaterial, in: RoundedRectangle(cornerRadius: 6))
    }

    private func balanceMetric(_ title: String, _ value: String, color: Color = .primary) -> some View {
        VStack(alignment: .leading, spacing: 4) {
            Text(title)
                .font(.caption)
                .foregroundStyle(.secondary)
            Text(value)
                .font(.headline)
                .monospacedDigit()
                .foregroundStyle(color)
                .textSelection(.enabled)
        }
        .frame(maxWidth: .infinity, alignment: .leading)
    }

    private func openCodeGoCard(
        _ snapshot: Codexpulse_Core_V1_OpenCodeGoSubscriptionSnapshot
    ) -> some View {
        SectionCard(title: "OpenCode Go") {
            sourceStatus(snapshot.status)
            if snapshot.hasQuota, !snapshot.quota.windows.isEmpty {
                ForEach(snapshot.quota.windows, id: \.kind) { window in
                    let progress = QuotaProgressPresentation(
                        usedPercent: window.usedPercent,
                        levelOverride: window.status == "rate-limited" ? .critical : nil,
                        localization: localization
                    )
                    VStack(alignment: .leading, spacing: 6) {
                        HStack {
                            Text(windowTitle(window.kind)).font(.headline)
                            Spacer()
                            Text(localization.format("%@ 已用", progress.percentText))
                                .monospacedDigit()
                                .foregroundStyle(quotaLevelColor(progress.level))
                        }
                        ProgressView(value: progress.fraction)
                            .tint(quotaLevelColor(progress.level))
                            .accessibilityLabel(localization.textValue("已使用"))
                            .accessibilityValue(progress.accessibilityValue)
                        HStack {
                            Text(localization.format("剩余 %@", localization.percent(window.remainingPercent)))
                            Spacer()
                            Text(localization.format("重置 %@", formattedDate(window.resetsAtMs)))
                        }
                        .font(.caption)
                        .foregroundStyle(.secondary)
                    }
                    if window.kind != snapshot.quota.windows.last?.kind { Divider() }
                }
            } else {
                unavailableSourceCopy(snapshot.status.state)
            }
        }
        .frame(maxWidth: .infinity, alignment: .topLeading)
    }

    private func sourceStatus(
        _ status: Codexpulse_Core_V1_APISubscriptionSourceStatus
    ) -> some View {
        VStack(alignment: .leading, spacing: 4) {
            HStack(spacing: 6) {
                Circle()
                    .fill(statusColor(status.state))
                    .frame(width: 8, height: 8)
                Text(statusTitle(status.state)).font(.caption).foregroundStyle(.secondary)
                Spacer()
                if status.hasLastSuccessAtMs {
                    Text(formattedDate(status.lastSuccessAtMs))
                        .font(.caption2)
                        .foregroundStyle(.tertiary)
                }
            }
            if status.hasFailureCode {
                Text(failureTitle(status.failureCode))
                    .font(.caption2)
                    .foregroundStyle(.orange)
            }
        }
    }

    private func unavailableSourceCopy(_ state: String) -> some View {
        Text(
            state == "unconfigured"
                ? localization.textValue("请在设置中保存 API key。")
                : localization.textValue("当前无法读取；已有可信数据会继续显示。")
        )
        .font(.callout)
        .foregroundStyle(.secondary)
        .padding(.vertical, 8)
    }

    private func statusTitle(_ state: String) -> String {
        switch state {
        case "current": localization.textValue("已更新")
        case "stale": localization.textValue("显示上次数据")
        case "unconfigured": localization.textValue("未配置")
        default: localization.textValue("暂不可用")
        }
    }

    private func statusColor(_ state: String) -> Color {
        switch state {
        case "current": .green
        case "stale": .orange
        case "unconfigured": .secondary
        default: .red
        }
    }

    private func windowTitle(_ kind: String) -> String {
        switch kind {
        case "five_hour": localization.textValue("5 小时额度")
        case "weekly": localization.textValue("周额度")
        case "monthly": localization.textValue("月额度")
        default: kind
        }
    }

    private func periodTitle(_ period: BalancePeriodSelection) -> String {
        switch period {
        case .today: localization.textValue("今天")
        case .week: localization.textValue("本周")
        case .month: localization.textValue("本月")
        }
    }

    private func signedDelta(_ value: String) -> String {
        if value.hasPrefix("-") || value.allSatisfy({ $0 == "0" || $0 == "." }) {
            return value
        }
        return "+\(value)"
    }

    private func deltaColor(_ value: String) -> Color {
        if value.hasPrefix("-") { return .orange }
        if value.allSatisfy({ $0 == "0" || $0 == "." }) { return .secondary }
        return .green
    }

    private func failureTitle(_ code: String) -> String {
        let key = switch code {
        case "network": "网络连接失败"
        case "timeout": "请求超时"
        case "auth": "API key 无效"
        case "forbidden": "当前账户无权访问"
        case "rate_limit": "请求过于频繁"
        case "server": "远端服务异常"
        default: "接口响应不兼容"
        }
        return localization.textValue(key)
    }

    private func formattedDate(_ milliseconds: Int64) -> String {
        guard milliseconds > 0 else { return "--" }
        return Date(timeIntervalSince1970: Double(milliseconds) / 1_000).formatted(
            date: .abbreviated,
            time: .shortened
        )
    }
}
