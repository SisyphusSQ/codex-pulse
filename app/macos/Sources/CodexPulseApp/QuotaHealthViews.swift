import CodexPulseAppSupport
import CodexPulseProtocolGenerated
import SwiftUI

struct QuotaUsageView: View {
    @ObservedObject var model: AppModel

    var body: some View {
        ScrollView {
            VStack(alignment: .leading, spacing: 18) {
                HStack {
                    Picker("用量范围", selection: $model.usageRange) {
                        ForEach(DateRangePreset.allCases.filter { $0 != .all }) { Text($0.title).tag($0) }
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
        FeatureStateView(state: model.quotaState, emptyTitle: "没有可信额度事实", emptySystemImage: "gauge.open.with.lines.needle.33percent") {
            QuotaContentView(response: $0, refreshState: model.quotaRefreshState, refresh: model.requestQuotaRefresh)
        }
    }

    private var usageSection: some View {
        FeatureStateView(state: model.usageState, emptyTitle: "当前范围没有已索引用量", emptySystemImage: "chart.xyaxis.line") {
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
                Button("刷新额度") { refresh("quota") }
                    .disabled(isRefreshing)
                    .accessibilityIdentifier("quota.refresh")
                Button("刷新重置额度") { refresh("reset_credits") }
                    .disabled(isRefreshing)
                    .accessibilityIdentifier("quota.refresh-reset-credits")
            }
            switch refreshState {
            case .idle:
                EmptyView()
            case .running:
                Label("已提交刷新请求…", systemImage: "arrow.clockwise")
                    .font(.caption).foregroundStyle(.secondary)
            case .succeeded(let reason):
                Label("刷新请求已受理：\(reason)", systemImage: "checkmark.circle")
                    .font(.caption).foregroundStyle(.green)
            case .unavailable(let notice):
                Label("刷新请求不可用：\(notice.code)", systemImage: "exclamationmark.triangle")
                    .font(.caption).foregroundStyle(.orange)
            }
            LazyVGrid(columns: [GridItem(.adaptive(minimum: 230), spacing: 12)], spacing: 12) {
                ForEach(Array(response.current.windows.enumerated()), id: \.offset) { _, window in
                    SectionCard(title: window.windowKind.isEmpty ? "未命名窗口" : window.windowKind) {
                        KeyValueRow(
                            key: "剩余",
                            value: window.hasRemainingPercent ? String(format: "%.0f%%", window.remainingPercent) : "--"
                        )
                        KeyValueRow(
                            key: "已用",
                            value: window.hasUsedPercent ? String(format: "%.0f%%", window.usedPercent) : "--"
                        )
                        KeyValueRow(key: "新鲜度", value: window.freshness.isEmpty ? "unknown" : window.freshness)
                        KeyValueRow(key: "来源", value: window.hasSelectedSource ? window.selectedSource : "--")
                        KeyValueRow(key: "重置时间", value: window.hasResetsAtMs ? timestampText(window.resetsAtMs) : "--")
                        if window.hasUnknownReason {
                            Text("未知原因：\(window.unknownReason)")
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
            SectionCard(title: "数据来源") {
                if response.current.sources.isEmpty {
                    Text("没有来源事实。")
                        .foregroundStyle(.secondary)
                }
                ForEach(response.current.sources, id: \.source) { source in
                    HStack {
                        Text(source.source)
                        Spacer()
                        StatusPill(text: source.freshness)
                        Text("采用 \(source.selectedWindowCount) 个窗口")
                            .font(.caption)
                            .foregroundStyle(.secondary)
                    }
                    if source.hasFailureCode || source.hasUnknownReason {
                        Text(source.hasFailureCode ? source.failureCode : source.unknownReason)
                            .font(.caption)
                            .foregroundStyle(.orange)
                    }
                    Divider()
                }
            }
            SectionCard(title: "重置额度") {
                let credits = response.current.resetCredits
                KeyValueRow(key: "可用", value: credits.hasAvailableCount ? credits.availableCount.formatted() : "--")
                KeyValueRow(key: "总量", value: credits.hasTotalCount ? credits.totalCount.formatted() : "--")
                KeyValueRow(key: "新鲜度", value: credits.freshness.isEmpty ? "unknown" : credits.freshness)
                if credits.hasUnknownReason {
                    Text("未知原因：\(credits.unknownReason)").font(.caption).foregroundStyle(.orange)
                }
            }
        }
    }

    private var isRefreshing: Bool {
        if case .running = refreshState { return true }
        return false
    }
}

private struct UsageContentView: View {
    let response: Codexpulse_Core_V1_UsageCostResponse

    var body: some View {
        VStack(alignment: .leading, spacing: 12) {
            Text("用量与趋势").font(.title2.bold())
            LazyVGrid(columns: [GridItem(.adaptive(minimum: 210), spacing: 12)], spacing: 12) {
                MetricCard(title: "Token 总量", value: numericText(response.totals.totalTokens), detail: "Helper 聚合事实")
                MetricCard(title: "API 等价成本", value: costText(response.totals.estimatedUsdMicros), detail: "估算值，不代表实际支出")
                MetricCard(title: "Turn", value: numericText(response.totals.turnCount), detail: "已索引 turn")
                MetricCard(title: "未计价 Turn", value: numericText(response.totals.unpricedTurnCount), detail: "保留 unknown/partial")
            }
            SectionCard(title: "按模型统计") {
                if response.models.isEmpty {
                    Text("当前范围没有可归因的模型用量。")
                        .foregroundStyle(.secondary)
                }
                ForEach(response.models, id: \.dimensionKey) { item in
                    HStack(alignment: .firstTextBaseline) {
                        VStack(alignment: .leading, spacing: 2) {
                            Text(attributionText(item.model))
                            if !item.model.source.isEmpty {
                                Text(item.model.source)
                                    .font(.caption)
                                    .foregroundStyle(.secondary)
                            }
                        }
                        Spacer()
                        Text("\(numericText(item.totals.totalTokens)) tokens")
                            .monospacedDigit()
                        Text(costText(item.totals.estimatedUsdMicros))
                            .monospacedDigit()
                            .frame(width: 90, alignment: .trailing)
                    }
                    .accessibilityIdentifier("usage.model.\(item.dimensionKey)")
                    Divider()
                }
            }
            SectionCard(title: "每日趋势 · \(response.reportingTimeZone)") {
                if response.trend.isEmpty {
                    Text("当前范围没有趋势点。")
                        .foregroundStyle(.secondary)
                }
                ForEach(response.trend, id: \.key) { point in
                    HStack {
                        Text(point.key).monospacedDigit()
                        Spacer()
                        Text("\(numericText(point.totals.totalTokens)) tokens")
                        Text(costText(point.totals.estimatedUsdMicros)).frame(width: 90, alignment: .trailing)
                    }
                    Divider()
                }
            }
            if response.hasDegradedReason {
                StateBanner(title: "用量结果退化：\(response.degradedReason)", systemImage: "exclamationmark.triangle", color: .orange)
            }
            if !response.unpricedReasons.isEmpty {
                SectionCard(title: "未计价原因") {
                    ForEach(response.unpricedReasons, id: \.reason) { reason in
                        KeyValueRow(key: reason.reason, value: numericText(reason.count))
                    }
                }
            }
        }
    }
}

struct LocalStatusView: View {
    @ObservedObject var model: AppModel

    var body: some View {
        ScrollView {
            VStack(alignment: .leading, spacing: 18) {
                FeatureStateView(state: model.healthProjectionState, emptyTitle: "尚无可信健康投影", emptySystemImage: "heart.slash") {
                    HealthProjectionView(response: $0)
                }
                RuntimeControlPanel(state: model.runtimeActionState, runAction: model.runRuntimeAction)
                FeatureStateView(state: model.dataHealthState, emptyTitle: "尚无运行时采样", emptySystemImage: "waveform.path.ecg") {
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
                Text("诊断事件").font(.title2.bold())
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
            FeatureStateView(state: model.healthState, emptyTitle: "当前条件下没有诊断事件", emptySystemImage: "checkmark.circle") {
                HealthEventsView(
                    response: $0,
                    selected: Binding(get: { model.selectedHealthEventID }, set: { model.selectHealthEvent($0) }),
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
                if response.stale { StatusPill(text: "stale") }
            }
            if response.hasPrimary {
                SectionCard(title: response.primary.component) {
                    KeyValueRow(key: "级别", value: response.primary.level)
                    KeyValueRow(key: "影响", value: response.primary.impact)
                    KeyValueRow(key: "保护", value: response.primary.protection)
                    KeyValueRow(key: "建议恢复", value: response.primary.recoveryAction.isEmpty ? "无" : response.primary.recoveryAction)
                    Text("健康投影中的 recovery action 是诊断建议，不会自动映射为可执行命令。")
                        .font(.caption).foregroundStyle(.secondary)
                }
            }
            LazyVGrid(columns: [GridItem(.adaptive(minimum: 230), spacing: 12)], spacing: 12) {
                ForEach(response.components, id: \.component) { component in
                    SectionCard(title: component.component) {
                        StatusPill(text: component.level)
                        Text(component.reason.isEmpty ? "无额外原因" : component.reason)
                            .font(.caption)
                            .foregroundStyle(.secondary)
                        if !component.recoveryAction.isEmpty {
                            Text("恢复入口：\(component.recoveryAction)")
                                .font(.caption)
                        }
                    }
                }
            }
            if !response.failure.isEmpty {
                StateBanner(title: "投影不可完整计算：\(response.failure)", systemImage: "exclamationmark.triangle", color: .orange)
            }
        }
    }
}

private struct RuntimeControlPanel: View {
    let state: ActionState
    let runAction: (RuntimeControlAction) -> Void

    var body: some View {
        SectionCard(title: "调度控制") {
            Text("以下四项是 RunRuntimeAction contract 明确允许的全局控制，与 provider recovery command key 分属不同命名空间。")
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
                Label("正在等待 Core receipt…", systemImage: "arrow.clockwise")
                    .font(.caption).foregroundStyle(.secondary)
            case .succeeded(let result):
                Label("操作完成：\(result)", systemImage: "checkmark.circle")
                    .font(.caption).foregroundStyle(.green)
            case .unavailable(let notice):
                Label("操作不可用：\(notice.code)", systemImage: "exclamationmark.triangle")
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
                    MetricCard(title: "CPU", value: String(format: "%.1f%%", response.latest.cpuPercent), detail: "最新 Helper 采样")
                    MetricCard(title: "内存", value: bytesText(response.latest.rssBytes), detail: "RSS")
                    MetricCard(title: "数据库", value: bytesText(response.latest.dbBytes), detail: "SQLite 由 Go 侧度量")
                    MetricCard(title: "Live 队列", value: numericText(response.latest.liveQueueDepth), detail: "不等同 transport 状态")
                }
            } else {
                Text("没有最新采样；不能推断运行正常。")
                    .foregroundStyle(.secondary)
            }
            SectionCard(title: "调度器与来源") {
                KeyValueRow(key: "完成周期", value: numericText(response.scheduler.completedCycles))
                KeyValueRow(key: "失败周期", value: numericText(response.scheduler.failedCycles))
                KeyValueRow(key: "来源 current", value: numericText(response.sources.current))
                KeyValueRow(key: "来源 stale", value: numericText(response.sources.stale))
                KeyValueRow(key: "来源 unavailable", value: numericText(response.sources.unavailable))
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
                        Text(item.code).font(.headline)
                        Text("\(item.domain) · \(item.component)").font(.caption).foregroundStyle(.secondary)
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
                Button(isLoading ? "正在加载…" : "加载更多诊断") { loadMore() }
                    .disabled(isLoading)
                    .accessibilityIdentifier("health.load-more")
            }
            FeatureStateView(state: detailState, emptyTitle: "选择一个事件查看恢复建议", emptySystemImage: "stethoscope") { detail in
                SectionCard(title: "诊断详情") {
                    KeyValueRow(key: "影响", value: detail.item.impact)
                    KeyValueRow(key: "保护", value: detail.item.protection)
                    KeyValueRow(key: "出现次数", value: numericText(detail.item.occurrenceCount))
                    KeyValueRow(
                        key: "恢复入口",
                        value: detail.item.recoveryAction.kind.isEmpty ? "无" : detail.item.recoveryAction.kind
                    )
                    if detail.item.recoveryAction.hasCommandKey {
                        KeyValueRow(key: "诊断 command key", value: detail.item.recoveryAction.commandKey)
                    }
                    Text("当前仅展示 provider 诊断建议；CoreService 未提供对应执行 RPC，不会把它伪装成已执行恢复。")
                        .font(.caption)
                        .foregroundStyle(.orange)
                }
            }
        }
    }
}
