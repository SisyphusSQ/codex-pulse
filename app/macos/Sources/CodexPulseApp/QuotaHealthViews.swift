import Charts
import CodexPulseAppSupport
import CodexPulseProtocolGenerated
import SwiftUI

struct QuotaUsageView: View {
    @ObservedObject var model: AppModel

    var body: some View {
        ScrollView {
            VStack(alignment: .leading, spacing: 18) {
                HStack(alignment: .center, spacing: 16) {
                    VStack(alignment: .leading, spacing: 3) {
                        Text("额度与用量").font(.largeTitle.bold())
                        Text("跟踪额度窗口、Token 趋势和 API 折算成本")
                            .foregroundStyle(.secondary)
                    }
                    Spacer()
                }
                HStack {
                    Picker("用量范围", selection: $model.usageRange) {
                        ForEach(DateRangePreset.allCases.filter { $0 != .all && $0 != .quotaWeek }) {
                            Text($0.title).tag($0)
                        }
                    }
                    .frame(width: 150)
                    .onChange(of: model.usageRange) { _, _ in model.loadUsage() }
                    Spacer()
                    if model.quotaState.isLoading || model.usageState.isLoading {
                        ProgressView().controlSize(.small)
                    }
                }
                quotaSection
                usageSection
            }
            .padding(20)
        }
        .accessibilityIdentifier("page.quota-usage")
    }

    private var quotaSection: some View {
        FeatureStateView(
            state: model.quotaState, emptyTitle: "暂时没有额度数据",
            emptySystemImage: "gauge.open.with.lines.needle.33percent"
        ) {
            QuotaContentView(
                response: $0, refreshState: model.quotaRefreshState, refresh: model.requestQuotaRefresh)
        }
    }

    private var usageSection: some View {
        FeatureStateView(
            state: model.usageState, emptyTitle: "当前范围暂无用量记录", emptySystemImage: "chart.xyaxis.line"
        ) {
            UsageContentView(response: $0)
        }
    }
}

private struct QuotaContentView: View {
    let response: Codexpulse_Core_V1_QuotaCurrentResponse
    let refreshState: ActionState
    let refresh: (String) -> Void

    var body: some View {
        VStack(alignment: .leading, spacing: 12) {
            HStack {
                Text("额度窗口").font(.title2.bold())
                Spacer()
                Menu("刷新数据", systemImage: "arrow.clockwise") {
                    Button("刷新额度") { refresh("quota") }
                    Button("刷新重置次数") { refresh("reset_credits") }
                }
                .disabled(isRefreshing)
                .accessibilityIdentifier("quota.refresh")
            }
            switch refreshState {
            case .idle:
                EmptyView()
            case .running:
                Label("正在更新额度…", systemImage: "arrow.clockwise")
                    .font(.caption).foregroundStyle(.secondary)
            case .succeeded:
                Label("额度更新已开始", systemImage: "checkmark.circle")
                    .font(.caption).foregroundStyle(.green)
            case .unavailable:
                Label("暂时无法更新额度", systemImage: "exclamationmark.triangle")
                    .font(.caption).foregroundStyle(.orange)
            }
            LazyVGrid(columns: [GridItem(.adaptive(minimum: 230), spacing: 12)], spacing: 12) {
                ForEach(Array(response.current.windows.enumerated()), id: \.offset) { _, window in
                    let presentation = QuotaWindowPresentation(window)
                    SectionCard(title: presentation.title) {
                        HStack(alignment: .firstTextBaseline) {
                            Text("剩余")
                                .foregroundStyle(.secondary)
                            Spacer()
                            Text(
                                window.hasRemainingPercent
                                    ? String(format: "%.0f%%", window.remainingPercent) : "--"
                            )
                            .font(.system(size: 28, weight: .semibold, design: .rounded))
                            .monospacedDigit()
                        }
                        ProgressView(value: window.hasRemainingPercent ? window.remainingPercent / 100 : 0)
                            .tint(quotaRemainingColor(
                                window.hasRemainingPercent ? window.remainingPercent : nil
                            ))
                        KeyValueRow(
                            key: "距离重置",
                            value: ProductCopy.duration(
                                milliseconds: window.hasResetRemainingMs ? window.resetRemainingMs : nil
                            )
                        )
                        if window.hasUnknownReason {
                            Text("这项额度暂时无法获取。")
                                .font(.caption)
                                .foregroundStyle(.orange)
                        }
                    }
                }
            }
            if response.current.windows.isEmpty {
                Text("没有窗口；这不等于额度充足。")
                    .foregroundStyle(.secondary)
            }
            SectionCard(title: "重置次数") {
                let credits = response.current.resetCredits
                KeyValueRow(
                    key: "可用", value: credits.hasAvailableCount ? credits.availableCount.formatted() : "--")
                KeyValueRow(key: "总量", value: credits.hasTotalCount ? credits.totalCount.formatted() : "--")
                if credits.hasCumulativeRemainingMs {
                    KeyValueRow(
                        key: "累计剩余时间",
                        value: ProductCopy.duration(milliseconds: credits.cumulativeRemainingMs)
                    )
                }
                if credits.hasUnknownReason {
                    Text("重置次数暂时无法获取。").font(.caption).foregroundStyle(.orange)
                }
            }
        }
    }

    private var isRefreshing: Bool {
        if case .running = refreshState { return true }
        return false
    }
}

private func quotaRemainingColor(_ value: Double?) -> Color {
    switch QuotaRemainingLevel(remainingPercent: value) {
    case .healthy: .green
    case .warning: .yellow
    case .critical: .red
    case .unavailable: .secondary
    }
}

private struct UsageContentView: View {
    let response: Codexpulse_Core_V1_UsageCostResponse
    @State private var selectedTrendKey: String?

    private var trendBuckets: [UsageModelTrendBucket] {
        UsageModelTrendResolver.buckets(response)
    }

    private var selectedTrendBucket: UsageModelTrendBucket? {
        guard let selectedTrendKey else { return nil }
        return trendBuckets.first { $0.key == selectedTrendKey }
    }

    var body: some View {
        VStack(alignment: .leading, spacing: 12) {
            Text("用量与趋势").font(.title2.bold())
            LazyVGrid(columns: [GridItem(.adaptive(minimum: 210), spacing: 12)], spacing: 12) {
                MetricCard(
                    title: "Token 用量", value: numericText(response.totals.totalTokens),
                    detail: "当前范围总量", systemImage: "number")
                MetricCard(
                    title: "API 折算成本", value: costText(response.totals.estimatedUsdMicros),
                    detail: "按 API 价格折算", systemImage: "dollarsign.circle")
                MetricCard(
                    title: "活动轮次", value: numericText(response.totals.turnCount), detail: "已整理的活动记录",
                    systemImage: "arrow.triangle.2.circlepath")
                MetricCard(
                    title: "暂未折算", value: numericText(response.totals.unpricedTurnCount), detail: "等待价格信息",
                    systemImage: "questionmark.circle")
            }
            SectionCard(title: "按模型统计") {
                if response.models.isEmpty {
                    Text("当前范围没有模型用量。")
                        .foregroundStyle(.secondary)
                }
                ForEach(response.models, id: \.dimensionKey) { item in
                    VStack(alignment: .leading, spacing: 5) {
                        VStack(alignment: .leading, spacing: 2) {
                            Text(attributionText(item.model))
                        }
                        TokenBreakdownView(
                            tokens: TokenBreakdownPresentation(item.totals),
                            style: .compact
                        )
                        Text("API 折算成本 \(costText(item.totals.estimatedUsdMicros))")
                            .font(.caption)
                            .foregroundStyle(.secondary)
                    }
                    .accessibilityIdentifier("usage.model.\(item.dimensionKey)")
                    Divider()
                }
            }
            SectionCard(title: "每日趋势") {
                if trendBuckets.isEmpty {
                    Text("当前范围没有趋势点。")
                        .foregroundStyle(.secondary)
                } else {
                    Chart {
                        ForEach(trendBuckets) { bucket in
                            ForEach(bucket.segments) { segment in
                                BarMark(
                                    x: .value("日期", segment.bucketKey),
                                    y: .value("Token", segment.tokens)
                                )
                                .foregroundStyle(by: .value("模型", segment.modelName))
                                .cornerRadius(3)
                                .accessibilityLabel("\(segment.bucketKey) · \(segment.modelName)")
                                .accessibilityValue("\(TokenQuantityFormatter.string(segment.tokens)) Token")
                            }
                        }
                        if let selected = selectedTrendBucket {
                            RuleMark(x: .value("选中日期", selected.key))
                                .foregroundStyle(Color.secondary.opacity(0.55))
                                .lineStyle(StrokeStyle(lineWidth: 1, dash: [4, 4]))
                                .annotation(
                                    position: .top,
                                    alignment: .leading,
                                    spacing: 8,
                                    overflowResolution: .init(x: .fit(to: .chart), y: .disabled)
                                ) {
                                    selectedTrendDetail(selected)
                                }
                        }
                    }
                    .chartYAxis {
                        AxisMarks(position: .trailing) { value in
                            AxisGridLine().foregroundStyle(.quaternary)
                            AxisValueLabel {
                                if let value = value.as(Int64.self) {
                                    Text(TokenQuantityFormatter.compactString(value))
                                }
                            }
                        }
                    }
                    .chartLegend(position: .bottom, alignment: .leading, spacing: 12)
                    .chartXSelection(value: $selectedTrendKey)
                    .chartOverlay { proxy in
                        GeometryReader { geometry in
                            Rectangle()
                                .fill(.clear)
                                .contentShape(Rectangle())
                                .onContinuousHover { phase in
                                    switch phase {
                                    case .active(let location):
                                        guard let plotFrame = proxy.plotFrame else {
                                            selectedTrendKey = nil
                                            return
                                        }
                                        let plotRect = geometry[plotFrame]
                                        guard plotRect.contains(location) else {
                                            selectedTrendKey = nil
                                            return
                                        }
                                        selectedTrendKey = proxy.value(
                                            atX: location.x - plotRect.origin.x,
                                            as: String.self
                                        )
                                    case .ended:
                                        selectedTrendKey = nil
                                    }
                                }
                        }
                    }
                    .frame(height: 220)
                    .onChange(of: response.range.startAtMs) { _, _ in selectedTrendKey = nil }
                    .onChange(of: response.range.endAtMs) { _, _ in selectedTrendKey = nil }
                }
            }
            if !response.unpricedReasons.isEmpty {
                SectionCard(title: "未计价原因") {
                    ForEach(response.unpricedReasons, id: \.reason) { reason in
                        KeyValueRow(key: "等待价格信息", value: numericText(reason.count))
                    }
                }
            }
        }
    }

    private func selectedTrendDetail(_ bucket: UsageModelTrendBucket) -> some View {
        VStack(alignment: .leading, spacing: 7) {
            Text(bucket.key)
                .font(.caption.weight(.semibold))
            TokenBreakdownView(tokens: bucket.tokenBreakdown, style: .compact)
            if bucket.breakdownAvailable {
                Divider()
                ForEach(bucket.segments) { segment in
                    HStack(spacing: 10) {
                        Text(segment.modelName)
                            .lineLimit(1)
                        Spacer(minLength: 12)
                        Text("\(TokenQuantityFormatter.string(segment.tokens)) · \(shareText(segment.tokens, total: bucket.totalTokens))")
                            .monospacedDigit()
                            .foregroundStyle(.secondary)
                    }
                    .font(.caption)
                }
            } else {
                Text("模型明细暂不可用")
                    .font(.caption)
                    .foregroundStyle(.orange)
            }
        }
        .padding(10)
        .frame(minWidth: 230, maxWidth: 340, alignment: .leading)
        .background(.regularMaterial, in: RoundedRectangle(cornerRadius: 10, style: .continuous))
        .overlay {
            RoundedRectangle(cornerRadius: 10, style: .continuous)
                .stroke(.quaternary, lineWidth: 1)
        }
    }

    private func shareText(_ tokens: Int64, total: Int64) -> String {
        guard total > 0 else { return "0%" }
        return String(format: "%.1f%%", Double(tokens) / Double(total) * 100)
    }
}

struct LocalStatusView: View {
    @ObservedObject var model: AppModel

    var body: some View {
        ScrollView {
            VStack(alignment: .leading, spacing: 18) {
                VStack(alignment: .leading, spacing: 3) {
                    Text("本机状态").font(.largeTitle.bold())
                    Text("检查数据更新和本地资源使用情况")
                        .foregroundStyle(.secondary)
                }
                FeatureStateView(
                    state: model.healthProjectionState, emptyTitle: "暂时没有状态数据",
                    emptySystemImage: "heart.slash"
                ) {
                    HealthProjectionView(response: $0)
                }
                RuntimeControlPanel(state: model.runtimeActionState, runAction: model.runRuntimeAction)
                FeatureStateView(
                    state: model.dataHealthState, emptyTitle: "暂时没有资源数据",
                    emptySystemImage: "waveform.path.ecg"
                ) {
                    DataHealthView(response: $0)
                }
                healthEvents
            }
            .padding(20)
        }
        .accessibilityIdentifier("page.local-status")
    }

    private var healthEvents: some View {
        VStack(alignment: .leading, spacing: 10) {
            HStack {
                Text("状态提醒").font(.title2.bold())
                Spacer()
                Picker("范围", selection: activeFilter) {
                    Text("仅活动中").tag("true")
                    Text("已解决").tag("false")
                    Text("全部").tag("all")
                }
                .frame(width: 140)
                .onChange(of: activeFilter.wrappedValue) { _, _ in model.healthFiltersChanged() }
                Picker("严重性", selection: severityFilter) {
                    Text("全部级别").tag("all")
                    Text("信息").tag("info")
                    Text("警告").tag("warning")
                    Text("错误").tag("error")
                    Text("严重").tag("critical")
                }
                .frame(width: 130)
                .onChange(of: severityFilter.wrappedValue) { _, _ in model.healthFiltersChanged() }
            }
            FeatureStateView(
                state: model.healthState, emptyTitle: "当前条件下没有状态提醒", emptySystemImage: "checkmark.circle"
            ) {
                HealthEventsView(
                    response: $0,
                    selected: Binding(
                        get: { model.selectedHealthEventID }, set: { model.selectHealthEvent($0) }),
                    detailState: model.healthDetailState,
                    isLoading: model.healthState.isLoading,
                    loadMore: { model.loadHealth(reset: false) }
                )
            }
        }
    }

    private var activeFilter: Binding<String> {
        Binding(
            get: { model.healthOptions.firstValues.first ?? "all" },
            set: {
                model.healthOptions.firstField = $0 == "all" ? "" : "active"
                model.healthOptions.firstValues = $0 == "all" ? [] : [$0]
            }
        )
    }

    private var severityFilter: Binding<String> {
        Binding(
            get: { model.healthOptions.secondValues.first ?? "all" },
            set: {
                model.healthOptions.secondField = $0 == "all" ? "" : "severity"
                model.healthOptions.secondValues = $0 == "all" ? [] : [$0]
            }
        )
    }
}

private struct HealthProjectionView: View {
    let response: Codexpulse_Core_V1_HealthProjectionResponse

    var body: some View {
        VStack(alignment: .leading, spacing: 12) {
            HStack {
                Text("本机状态").font(.title2.bold())
                Spacer()
                StatusPill(text: response.hasLevel ? response.level : "unknown")
                if response.stale {
                    Label("正在更新", systemImage: "arrow.clockwise")
                        .font(.caption)
                        .foregroundStyle(.secondary)
                }
            }
            if response.hasPrimary {
                SectionCard(title: ProductCopy.component(response.primary.component)) {
                    KeyValueRow(key: "状态", value: ProductCopy.status(response.primary.level))
                    KeyValueRow(key: "影响", value: ProductCopy.impact(response.primary.impact))
                    KeyValueRow(key: "保护", value: ProductCopy.protection(response.primary.protection))
                    KeyValueRow(
                        key: "建议操作", value: ProductCopy.recoveryAction(response.primary.recoveryAction))
                }
            }
            LazyVGrid(columns: [GridItem(.adaptive(minimum: 230), spacing: 12)], spacing: 12) {
                ForEach(response.components, id: \.component) { component in
                    SectionCard(title: ProductCopy.component(component.component)) {
                        StatusPill(text: component.level)
                        Text(ProductCopy.reason(component.reason))
                            .font(.caption)
                            .foregroundStyle(.secondary)
                        if !component.recoveryAction.isEmpty {
                            Text("建议操作：\(ProductCopy.recoveryAction(component.recoveryAction))")
                                .font(.caption)
                        }
                    }
                }
            }
        }
    }
}

private struct RuntimeControlPanel: View {
    let state: ActionState
    let runAction: (RuntimeControlAction) -> Void

    var body: some View {
        SectionCard(title: "数据更新控制") {
            Text("暂停或恢复本机数据更新。高影响操作会再次确认。")
                .font(.caption)
                .foregroundStyle(.secondary)
            HStack {
                ForEach(RuntimeControlAction.allCases, id: \.rawValue) { action in
                    RuntimeActionControl(action: action, state: state, execute: runAction, showsStatus: false)
                }
            }
            switch state {
            case .idle:
                EmptyView()
            case .running:
                Label("正在执行操作…", systemImage: "arrow.clockwise")
                    .font(.caption).foregroundStyle(.secondary)
            case .succeeded:
                Label("操作已完成", systemImage: "checkmark.circle")
                    .font(.caption).foregroundStyle(.green)
            case .unavailable:
                Label("操作暂时不可用", systemImage: "exclamationmark.triangle")
                    .font(.caption).foregroundStyle(.orange)
            }
        }
    }
}

private struct DataHealthView: View {
    let response: Codexpulse_Core_V1_DataHealthResponse

    var body: some View {
        VStack(alignment: .leading, spacing: 12) {
            Text("数据健康").font(.title2.bold())
            if response.hasLatest {
                LazyVGrid(columns: [GridItem(.adaptive(minimum: 190), spacing: 12)], spacing: 12) {
                    MetricCard(
                        title: "CPU", value: String(format: "%.1f%%", response.latest.cpuPercent),
                        detail: "当前使用率", systemImage: "cpu")
                    MetricCard(
                        title: "内存", value: bytesText(response.latest.rssBytes), detail: "当前占用",
                        systemImage: "memorychip")
                    MetricCard(
                        title: "数据库", value: bytesText(response.latest.dbBytes), detail: "本机数据大小",
                        systemImage: "externaldrive")
                    MetricCard(
                        title: "待处理", value: numericText(response.latest.liveQueueDepth), detail: "等待更新的项目",
                        systemImage: "tray.full")
                }
            } else {
                Text("暂时没有最新资源数据。")
                    .foregroundStyle(.secondary)
            }
            SectionCard(title: "数据更新") {
                KeyValueRow(key: "完成更新次数", value: numericText(response.scheduler.completedCycles))
                KeyValueRow(key: "失败更新次数", value: numericText(response.scheduler.failedCycles))
                KeyValueRow(key: "状态正常的数据源", value: numericText(response.sources.current))
                KeyValueRow(key: "需要更新的数据源", value: numericText(response.sources.stale))
                KeyValueRow(key: "暂不可用的数据源", value: numericText(response.sources.unavailable))
            }
        }
    }
}

private struct HealthEventsView: View {
    let response: Codexpulse_Core_V1_HealthListResponse
    @Binding var selected: String?
    let detailState: FeatureLoadState<Codexpulse_Core_V1_HealthDetailResponse>
    let isLoading: Bool
    let loadMore: () -> Void

    var body: some View {
        VStack(spacing: 10) {
            List(response.items, id: \.eventID, selection: $selected) { item in
                HStack {
                    VStack(alignment: .leading) {
                        Text(ProductCopy.eventName(item.code)).font(.headline)
                        Text(ProductCopy.component(item.component)).font(.caption).foregroundStyle(.secondary)
                    }
                    Spacer()
                    StatusPill(text: item.severity)
                    StatusPill(text: item.active ? "active" : "resolved")
                }
                .tag(item.eventID)
                .accessibilityIdentifier("health-event.\(item.eventID)")
            }
            .frame(minHeight: 180, idealHeight: 240)
            .onChange(of: selected) { _, next in
                // The parent binding routes selection through AppModel.
                _ = next
            }
            if pageHasMore(response.meta) {
                Button(isLoading ? "正在加载…" : "加载更多提醒") { loadMore() }
                    .disabled(isLoading)
                    .accessibilityIdentifier("health.load-more")
            }
            FeatureStateView(
                state: detailState, emptyTitle: "选择一个事件查看恢复建议", emptySystemImage: "stethoscope"
            ) { detail in
                SectionCard(title: "状态详情") {
                    KeyValueRow(key: "影响", value: ProductCopy.impact(detail.item.impact))
                    KeyValueRow(key: "保护", value: ProductCopy.protection(detail.item.protection))
                    KeyValueRow(key: "出现次数", value: numericText(detail.item.occurrenceCount))
                    KeyValueRow(
                        key: "建议操作",
                        value: ProductCopy.recoveryAction(detail.item.recoveryAction.kind)
                    )
                }
            }
        }
    }
}
