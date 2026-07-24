import Charts
import CodexPulseAppSupport
import SwiftUI

struct RootView: View {
    @ObservedObject var model: AppModel

    var body: some View {
        NavigationSplitView {
            List(selection: selection) {
                Section("使用情况") {
                    ForEach([AppFeature.overview, .sessions, .projects, .quotaUsage]) { section in
                        Label(section.title, systemImage: section.symbol)
                            .tag(section)
                    }
                }
                Section("系统") {
                    ForEach([AppFeature.localStatus, .sourcesJobs, .settings]) { section in
                        Label(section.title, systemImage: section.symbol)
                            .tag(section)
                    }
                }
            }
            .listStyle(.sidebar)
            .navigationTitle("Codex Pulse")
            .frame(minWidth: 190)
        } detail: {
            featureContent
                .navigationTitle(model.selectedFeature.title)
                .toolbar {
                    ToolbarItemGroup(placement: .primaryAction) {
                        Button {
                            model.refresh(model.selectedFeature)
                        } label: {
                            currentRefreshLabel
                        }
                        .keyboardShortcut("r", modifiers: .command)
                        .disabled(!model.canRefreshOrRestart || model.isRefreshing(model.selectedFeature))
                        .help(currentRefreshHelp)
                        .accessibilityIdentifier("toolbar.refresh.current")
                        Button {
                            model.refreshAllFeatures()
                        } label: {
                            Label("刷新全部页面", systemImage: "arrow.triangle.2.circlepath")
                        }
                        .disabled(!model.canRefreshOrRestart)
                        .help("刷新全部页面")
                        .accessibilityIdentifier("toolbar.refresh.all")
                    }
                }
        }
        .frame(minWidth: 820, minHeight: 560)
        .onChange(of: model.selectedFeature) { _, next in
            model.load(next)
            model.markFeatureRendered(next)
        }
    }

    private var selection: Binding<AppFeature?> {
        Binding(
            get: { model.selectedFeature },
            set: { if let feature = $0 { model.navigate(to: feature) } }
        )
    }

    @ViewBuilder
    private var currentRefreshLabel: some View {
        if model.requiresCoreRestart {
            Label("重新连接", systemImage: "bolt.horizontal.circle")
        } else if model.isRefreshing(model.selectedFeature) {
            ProgressView()
                .controlSize(.small)
                .frame(width: 16, height: 16)
                .accessibilityLabel("正在刷新当前页面")
        } else {
            Label("刷新当前页面", systemImage: "arrow.clockwise")
        }
    }

    private var currentRefreshHelp: String {
        if model.requiresCoreRestart { return "重新连接本地数据" }
        return model.isRefreshing(model.selectedFeature) ? "正在刷新当前页面" : "刷新当前页面"
    }

    @ViewBuilder
    private var featureContent: some View {
        Group {
            switch model.selectedFeature {
            case .overview:
                OverviewStateView(model: model) { model.navigate(to: $0) }
            case .sessions:
                RuntimeAwarePage(model: model) { SessionsView(model: model) }
            case .projects:
                RuntimeAwarePage(model: model) { ProjectsView(model: model) }
            case .quotaUsage:
                RuntimeAwarePage(model: model) { QuotaUsageView(model: model) }
            case .localStatus:
                RuntimeAwarePage(model: model) { LocalStatusView(model: model) }
            case .sourcesJobs:
                RuntimeAwarePage(model: model) { SourcesJobsView(model: model) }
            case .settings:
                RuntimeAwarePage(model: model) { SettingsView(model: model) }
            }
        }
        .id(model.selectedFeature)
        .onAppear { model.markFeatureRendered(model.selectedFeature) }
    }
}

struct OverviewStateView: View {
    @ObservedObject var model: AppModel
    var onNavigate: (AppFeature) -> Void = { _ in }

    var body: some View {
        switch model.state {
        case .idle:
            loading("准备启动…")
        case .loading(let message):
            loading(message)
        case .overview(let overview):
            overviewContent(overview)
        case .partial(let overview):
            overviewContent(overview)
        case .stale(let overview, _):
            overviewContent(overview)
        case .recovery:
            ContentUnavailableView {
                Label("正在修复本地数据", systemImage: "wrench.and.screwdriver")
            } description: {
                Text("修复完成后会自动恢复页面内容。")
            } actions: {
                Button("重试") { model.retryRecovery() }
            }
        case .restartRequired:
            ContentUnavailableView {
                Label("需要重新连接本地数据", systemImage: "arrow.triangle.2.circlepath")
            } description: {
                Text("数据修复已经完成，重新连接后即可继续。")
            } actions: {
                Button("重新连接") { model.restartCore() }
            }
        case .unavailable(let notice):
            ContentUnavailableView {
                Label("本地数据暂时不可用", systemImage: "bolt.slash")
            } description: {
                Text(notice.retryable ? "可以重试连接。" : "当前版本无法读取这些数据，请更新 App。")
            } actions: {
                if notice.retryable { Button("重试") { model.restartCore() } }
            }
        case .cancelled:
            ContentUnavailableView("加载已取消", systemImage: "xmark.circle")
        case .shuttingDown:
            loading("正在安全退出…")
        case .stopped:
            ContentUnavailableView("本地数据服务已停止", systemImage: "stop.circle")
        }
    }

    private func overviewContent(_ overview: OverviewPresentation) -> some View {
        OverviewContentView(
            overview: overview,
            selectedRange: model.overviewRange,
            onSelectRange: model.selectOverviewRange,
            onNavigate: onNavigate,
            onSelectProject: { projectKey in
                model.projectOptions.range = model.overviewRange
                model.projectOptions.exactRange = overview.contentRange
                model.navigate(to: .projects)
                model.selectProject(projectKey)
            },
            onSelectSession: { sessionID in
                model.sessionOptions.range = model.overviewRange
                model.sessionOptions.exactRange = overview.contentRange
                model.navigate(to: .sessions)
                model.selectSession(sessionID)
            }
        )
    }

    private func loading(_ text: String) -> some View {
        VStack(spacing: 12) {
            ProgressView()
                .controlSize(.large)
            Text(text)
                .foregroundStyle(.secondary)
        }
        .frame(maxWidth: .infinity, maxHeight: .infinity)
        .accessibilityElement(children: .combine)
    }
}

private struct OverviewContentView: View {
    let overview: OverviewPresentation
    let selectedRange: DateRangePreset
    let onSelectRange: (DateRangePreset) -> Void
    let onNavigate: (AppFeature) -> Void
    let onSelectProject: (String) -> Void
    let onSelectSession: (String) -> Void
    @State private var selectedTrendDate: Date?

    private let ranges: [DateRangePreset] = [.quotaWeek, .today, .sevenDays, .thirtyDays]

    var body: some View {
        ScrollView {
            VStack(alignment: .leading, spacing: 16) {
                pageHeader
                if overview.fellBackFromQuotaWeek { fallbackNotice }
                quotaStatusStrip
                consumptionSection
                highConsumptionSessions
            }
            .padding(24)
            .frame(maxWidth: .infinity, alignment: .leading)
        }
        .accessibilityIdentifier("page.overview")
    }

    private var pageHeader: some View {
        HStack(alignment: .center, spacing: 20) {
            VStack(alignment: .leading, spacing: 4) {
                Text("额度概览")
                    .font(.largeTitle.bold())
                Text("看清消耗了多少，以及消耗去了哪里")
                    .foregroundStyle(.secondary)
            }
            Spacer()
            VStack(alignment: .trailing, spacing: 6) {
                Picker("概览范围", selection: rangeBinding) {
                    ForEach(ranges) { range in Text(range.title).tag(range) }
                }
                .pickerStyle(.segmented)
                .frame(width: 340)
                Label("更新于 \(timestampText(overview.evaluatedAtMS))", systemImage: "clock")
                    .font(.caption)
                    .foregroundStyle(.secondary)
            }
        }
    }

    private var rangeBinding: Binding<DateRangePreset> {
        Binding(get: { selectedRange }, set: { value in onSelectRange(value) })
    }

    private var fallbackNotice: some View {
        Label(
            "暂未获取到周额度周期，当前显示最近 7 天。",
            systemImage: "arrow.trianglehead.2.clockwise.rotate.90"
        )
        .font(.caption.weight(.medium))
        .foregroundStyle(.orange)
        .padding(.horizontal, 12)
        .padding(.vertical, 8)
        .background(Color.orange.opacity(0.1), in: RoundedRectangle(cornerRadius: 9))
    }

    private var quotaStatusStrip: some View {
        HStack(spacing: 18) {
            Label("额度状态", systemImage: "gauge.with.dots.needle.67percent")
                .font(.headline)
            if !overview.quotaAvailable || overview.quotaWindows.isEmpty {
                Text("暂时无法获取额度")
                    .foregroundStyle(.secondary)
            } else {
                ForEach(Array(overview.quotaWindows.prefix(2).enumerated()), id: \.element.id) {
                    index, window in
                    if index > 0 { Divider().frame(height: 34) }
                    VStack(alignment: .leading, spacing: 4) {
                        HStack(spacing: 8) {
                            Text(window.title).font(.subheadline.weight(.medium))
                            Text(percentText(window.remainingPercent))
                                .font(.subheadline.bold())
                                .monospacedDigit()
                                .foregroundStyle(quotaColor(window.remainingPercent))
                        }
                        HStack(spacing: 8) {
                            ProgressView(value: quotaProgress(window.remainingPercent))
                                .tint(quotaColor(window.remainingPercent))
                                .frame(width: 92)
                            Text(quotaResetText(window))
                                .font(.caption)
                                .foregroundStyle(.secondary)
                        }
                    }
                }
            }
            Spacer(minLength: 12)
            Button("额度详情") { onNavigate(.quotaUsage) }.buttonStyle(.link)
        }
        .padding(.horizontal, 16)
        .padding(.vertical, 12)
        .background(.thinMaterial, in: RoundedRectangle(cornerRadius: 12, style: .continuous))
        .overlay {
            RoundedRectangle(cornerRadius: 12, style: .continuous)
                .stroke(.quaternary, lineWidth: 1)
        }
    }

    private var consumptionSection: some View {
        SectionCard(title: "消耗概览") {
            HStack(alignment: .top, spacing: 22) {
                VStack(alignment: .leading, spacing: 18) {
                    usageSummary
                    Divider()
                    trendChart
                }
                .frame(maxWidth: .infinity, alignment: .leading)
                Divider()
                projectBreakdown
                    .frame(minWidth: 220, idealWidth: 270, maxWidth: 310)
            }
        }
    }

    private var usageSummary: some View {
        HStack(alignment: .top, spacing: 28) {
            VStack(alignment: .leading, spacing: 12) {
                VStack(alignment: .leading, spacing: 5) {
                    Text("Token 总量")
                        .font(.subheadline.weight(.medium))
                        .foregroundStyle(.secondary)
                    Text(metricText(overview.tokenBreakdown.total))
                        .font(.system(size: 25, weight: .semibold, design: .rounded))
                        .monospacedDigit()
                }
                HStack(alignment: .top, spacing: 28) {
                    usageBreakdownMetric(
                        title: "输入",
                        value: overview.tokenBreakdown.input,
                        detailTitle: "缓存",
                        detailValue: overview.tokenBreakdown.cachedInput
                    )
                    usageBreakdownMetric(
                        title: "输出",
                        value: overview.tokenBreakdown.output,
                        detailTitle: "推理",
                        detailValue: overview.tokenBreakdown.reasoning
                    )
                }
            }
            .frame(maxWidth: .infinity, alignment: .leading)

            Divider()

            VStack(alignment: .leading, spacing: 5) {
                Text("API 等价成本")
                    .font(.subheadline.weight(.medium))
                    .foregroundStyle(.secondary)
                Text(metricText(overview.estimatedCost, cost: true))
                    .font(.system(size: 25, weight: .semibold, design: .rounded))
                    .monospacedDigit()
            }
            .frame(minWidth: 180, idealWidth: 220, maxWidth: 260, alignment: .leading)
        }
    }

    private func usageBreakdownMetric(
        title: String,
        value: DisplayMetric,
        detailTitle: String,
        detailValue: DisplayMetric
    ) -> some View {
        VStack(alignment: .leading, spacing: 3) {
            Text(title)
                .font(.caption)
                .foregroundStyle(.secondary)
            Text(metricText(value))
                .font(.headline)
                .monospacedDigit()
            Text("\(detailTitle) \(metricText(detailValue))")
                .font(.caption2)
                .foregroundStyle(.tertiary)
        }
        .frame(minWidth: 128, alignment: .leading)
        .accessibilityElement(children: .combine)
    }

    @ViewBuilder
    private var trendChart: some View {
        VStack(alignment: .leading, spacing: 10) {
            HStack(alignment: .firstTextBaseline) {
                Text("Token 趋势").font(.subheadline.weight(.semibold))
                Text(overview.usageRangeLabel).font(.caption).foregroundStyle(.secondary)
            }
            if !overview.usageAvailable {
                ContentUnavailableView(
                    "Token 用量暂时不可用",
                    systemImage: "chart.xyaxis.line",
                    description: Text("额度与其他区域仍可继续查看。")
                )
                .frame(height: 220)
            } else if chartPoints.isEmpty {
                ContentUnavailableView(
                    "当前范围暂无用量",
                    systemImage: "chart.xyaxis.line",
                    description: Text("产生新的会话后，趋势会显示在这里。")
                )
                .frame(height: 220)
            } else {
                Chart {
                    ForEach(chartPoints) { point in
                        AreaMark(
                            x: .value("时间", point.date),
                            y: .value("Token", point.tokens)
                        )
                        .interpolationMethod(.monotone)
                        .foregroundStyle(
                            LinearGradient(
                                colors: [Color.blue.opacity(0.30), Color.blue.opacity(0.03)],
                                startPoint: .top,
                                endPoint: .bottom
                            )
                        )
                        LineMark(
                            x: .value("时间", point.date),
                            y: .value("Token", point.tokens)
                        )
                        .interpolationMethod(.monotone)
                        .foregroundStyle(Color.blue)
                        .lineStyle(
                            StrokeStyle(lineWidth: 2.2, lineCap: .round, lineJoin: .round))
                        PointMark(
                            x: .value("时间", point.date),
                            y: .value("Token", point.tokens)
                        )
                        .foregroundStyle(Color.blue)
                        .symbolSize(selectedTrendPoint?.id == point.id ? 70 : 28)
                        .accessibilityLabel(trendPointTimeText(point.date))
                        .accessibilityValue("\(TokenQuantityFormatter.string(point.tokens)) Token")
                    }
                    if let selected = selectedTrendPoint {
                        RuleMark(x: .value("选中时间", selected.date))
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
                    AxisMarks(position: .leading) { value in
                        AxisGridLine().foregroundStyle(.quaternary)
                        AxisValueLabel {
                            if let value = value.as(Int64.self) {
                                Text(TokenQuantityFormatter.string(value))
                            }
                        }
                    }
                }
                .chartXAxis { AxisMarks(values: .automatic(desiredCount: 6)) }
                .chartXSelection(value: $selectedTrendDate)
                .frame(height: 230)
                .onChange(of: selectedRange) { _, _ in selectedTrendDate = nil }
            }
        }
    }

    private var projectBreakdown: some View {
        VStack(alignment: .leading, spacing: 12) {
            HStack {
                Text("项目消耗").font(.subheadline.weight(.semibold))
                Spacer()
                Button("全部项目") { onNavigate(.projects) }.buttonStyle(.link)
            }
            if !overview.projectsAvailable {
                Text("项目消耗暂时不可用。")
                    .font(.subheadline)
                    .foregroundStyle(.secondary)
                    .padding(.vertical, 8)
            } else if overview.projects.isEmpty {
                Text("当前范围暂无项目消耗。")
                    .font(.subheadline)
                    .foregroundStyle(.secondary)
                    .padding(.vertical, 8)
            } else {
                ForEach(Array(overview.projects.enumerated()), id: \.element.id) { index, project in
                    projectBreakdownRow(project, rank: index + 1)
                }
            }
        }
    }

    @ViewBuilder
    private func projectBreakdownRow(_ project: ProjectPresentation, rank: Int) -> some View {
        if project.isOther {
            ProjectUsageRow(
                rank: rank,
                title: project.title,
                tokens: project.tokenBreakdown,
                fraction: projectFraction(project.tokens)
            )
        } else {
            Button { onSelectProject(project.id) } label: {
                ProjectUsageRow(
                    rank: rank,
                    title: project.title,
                    tokens: project.tokenBreakdown,
                    fraction: projectFraction(project.tokens)
                )
            }
            .buttonStyle(.plain)
        }
    }

    private var highConsumptionSessions: some View {
        SectionCard(title: "高消耗会话") {
            if !overview.sessionsAvailable {
                Text("会话消耗暂时不可用。").foregroundStyle(.secondary)
            } else if overview.sessions.isEmpty {
                Text("当前范围暂无会话消耗。").foregroundStyle(.secondary)
            } else {
                ForEach(Array(overview.sessions.enumerated()), id: \.element.id) { index, session in
                    Button { onSelectSession(session.id) } label: {
                        HStack(spacing: 12) {
                            Text("\(index + 1)")
                                .font(.caption.bold())
                                .foregroundStyle(.secondary)
                                .frame(width: 20)
                            Image(systemName: "terminal").foregroundStyle(.tint).frame(width: 22)
                            VStack(alignment: .leading, spacing: 3) {
                                Text(session.title).lineLimit(1)
                                Text(
                                    [session.project, ProductCopy.status(session.activity)]
                                        .compactMap { $0 }.joined(separator: " · ")
                                )
                                .font(.caption)
                                .foregroundStyle(.secondary)
                            }
                            Spacer()
                            TokenBreakdownView(tokens: session.tokenBreakdown, style: .compact)
                                .frame(maxWidth: 250, alignment: .trailing)
                            Image(systemName: "chevron.right")
                                .font(.caption)
                                .foregroundStyle(.tertiary)
                        }
                        .contentShape(Rectangle())
                    }
                    .buttonStyle(.plain)
                    .padding(.vertical, 4)
                    if index < overview.sessions.count - 1 { Divider() }
                }
                Button("查看全部会话") { onNavigate(.sessions) }.buttonStyle(.link)
            }
        }
    }

    private var chartPoints: [OverviewChartPoint] {
        overview.trend.compactMap { point in
            guard let startAtMS = point.startAtMS,
                  let tokens = metricValue(point.tokens)
            else { return nil }
            return OverviewChartPoint(
                id: point.id,
                date: Date(timeIntervalSince1970: Double(startAtMS) / 1_000),
                tokens: tokens,
                tokenBreakdown: point.tokenBreakdown,
                estimatedCost: point.estimatedCost)
        }
    }

    private var selectedTrendPoint: OverviewChartPoint? {
        guard let selected = TrendSelectionResolver.nearest(
            to: selectedTrendDate,
            in: overview.trend
        ),
            let startAtMS = selected.startAtMS,
            let tokens = metricValue(selected.tokens)
        else { return nil }
        return OverviewChartPoint(
            id: selected.id,
            date: Date(timeIntervalSince1970: Double(startAtMS) / 1_000),
            tokens: tokens,
            tokenBreakdown: selected.tokenBreakdown,
            estimatedCost: selected.estimatedCost
        )
    }

    private func selectedTrendDetail(_ point: OverviewChartPoint) -> some View {
        VStack(alignment: .leading, spacing: 3) {
            Text(trendPointTimeText(point.date))
                .font(.caption)
                .foregroundStyle(.secondary)
            TokenBreakdownView(tokens: point.tokenBreakdown, style: .compact)
            if case .known = point.estimatedCost {
                Text("API 等价成本 \(metricText(point.estimatedCost, cost: true))")
                    .font(.caption)
                    .foregroundStyle(.secondary)
            }
        }
        .padding(.horizontal, 10)
        .padding(.vertical, 8)
        .background(.regularMaterial, in: RoundedRectangle(cornerRadius: 8, style: .continuous))
        .overlay {
            RoundedRectangle(cornerRadius: 8, style: .continuous)
                .stroke(.quaternary, lineWidth: 1)
        }
    }

    private func trendPointTimeText(_ date: Date) -> String {
        if selectedRange == .today {
            return date.formatted(.dateTime.month().day().hour().minute())
        }
        return date.formatted(.dateTime.year().month().day())
    }

    private var projectTokenTotal: Int64? {
        let metrics = overview.projects.map(\.tokens) + [overview.otherProjectTokens].compactMap { $0 }
        var total: Int64 = 0
        for metric in metrics {
            guard let value = metricValue(metric) else { return nil }
            let (next, overflow) = total.addingReportingOverflow(value)
            guard !overflow else { return nil }
            total = next
        }
        return total > 0 ? total : nil
    }

    private func projectFraction(_ metric: DisplayMetric) -> Double? {
        guard let total = projectTokenTotal, let value = metricValue(metric) else { return nil }
        return min(max(Double(value) / Double(total), 0), 1)
    }

    private func percentText(_ value: Double?) -> String {
        value.map { String(format: "%.0f%%", $0) } ?? "--"
    }

    private func quotaProgress(_ value: Double?) -> Double {
        min(max((value ?? 0) / 100, 0), 1)
    }

    private func quotaColor(_ value: Double?) -> Color {
        switch QuotaRemainingLevel(remainingPercent: value) {
        case .healthy: .green
        case .warning: .yellow
        case .critical: .red
        case .unavailable: .secondary
        }
    }

    private func quotaResetText(_ window: QuotaWindowPresentation) -> String {
        guard window.resetRemainingMS != nil else { return "重置时间待定" }
        return "\(ProductCopy.duration(milliseconds: window.resetRemainingMS))后重置"
    }
}

private struct OverviewChartPoint: Identifiable {
    let id: String
    let date: Date
    let tokens: Int64
    let tokenBreakdown: TokenBreakdownPresentation
    let estimatedCost: DisplayMetric
}

private struct ProjectUsageRow: View {
    let rank: Int?
    let title: String
    let tokens: TokenBreakdownPresentation
    let fraction: Double?

    var body: some View {
        VStack(alignment: .leading, spacing: 5) {
            HStack(spacing: 8) {
                Text(rank.map(String.init) ?? "·")
                    .font(.caption.bold())
                    .foregroundStyle(.secondary)
                    .frame(width: 18)
                Text(title).lineLimit(1)
                Spacer(minLength: 8)
            }
            TokenBreakdownView(tokens: tokens, style: .compact)
                .padding(.leading, 26)
            ProgressView(value: fraction ?? 0)
                .tint(.blue)
                .opacity(fraction == nil ? 0.35 : 1)
        }
        .padding(.vertical, 3)
        .contentShape(Rectangle())
    }
}

struct MetricCard: View {
    let title: String
    let value: String
    let detail: String
    var systemImage: String? = nil

    var body: some View {
        VStack(alignment: .leading, spacing: 8) {
            HStack(spacing: 7) {
                if let systemImage {
                    Image(systemName: systemImage)
                        .foregroundStyle(.tint)
                }
                Text(title).font(.headline)
            }
            Text(value).font(.system(size: 30, weight: .semibold, design: .rounded)).monospacedDigit()
            Text(detail).font(.caption).foregroundStyle(.secondary)
        }
        .frame(maxWidth: .infinity, alignment: .leading)
        .padding(16)
        .background(.regularMaterial, in: RoundedRectangle(cornerRadius: 14, style: .continuous))
        .overlay {
            RoundedRectangle(cornerRadius: 14, style: .continuous)
                .stroke(.quaternary, lineWidth: 1)
        }
        .accessibilityElement(children: .combine)
    }
}

private struct QuotaOverviewCard: View {
    let window: QuotaWindowPresentation

    var body: some View {
        VStack(alignment: .leading, spacing: 12) {
            HStack(alignment: .firstTextBaseline) {
                Label(window.title, systemImage: "gauge.with.dots.needle.67percent")
                    .font(.headline)
                Spacer()
                Text(percentText)
                    .font(.system(size: 26, weight: .semibold, design: .rounded))
                    .monospacedDigit()
            }
            ProgressView(value: progress)
                .tint(progressColor)
            HStack {
                Text(window.remainingPercent == nil ? "暂时无法获取" : "剩余额度")
                Spacer()
                Text(resetText)
            }
            .font(.caption)
            .foregroundStyle(.secondary)
        }
        .padding(16)
        .background(.regularMaterial, in: RoundedRectangle(cornerRadius: 14, style: .continuous))
        .overlay {
            RoundedRectangle(cornerRadius: 14, style: .continuous)
                .stroke(.quaternary, lineWidth: 1)
        }
        .accessibilityElement(children: .combine)
    }

    private var percentText: String {
        window.remainingPercent.map { String(format: "%.0f%%", $0) } ?? "--"
    }

    private var progress: Double {
        min(max((window.remainingPercent ?? 0) / 100, 0), 1)
    }

    private var progressColor: Color {
        switch QuotaRemainingLevel(remainingPercent: window.remainingPercent) {
        case .healthy: .green
        case .warning: .yellow
        case .critical: .red
        case .unavailable: .secondary
        }
    }

    private var resetText: String {
        guard window.resetRemainingMS != nil else { return "重置时间待定" }
        return "将在 \(ProductCopy.duration(milliseconds: window.resetRemainingMS))后重置"
    }
}

private func metricValue(_ metric: DisplayMetric) -> Int64? {
    if case .known(let value, _) = metric { return value }
    return nil
}

struct StateBanner: View {
    let title: String
    let systemImage: String
    let color: Color

    var body: some View {
        Label(title, systemImage: systemImage)
            .font(.caption.weight(.medium))
            .foregroundStyle(color)
            .frame(maxWidth: .infinity, alignment: .leading)
            .padding(.horizontal, 18)
            .padding(.vertical, 7)
            .accessibilityLabel(title)
    }
}

func metricText(_ metric: DisplayMetric, cost: Bool = false) -> String {
    switch metric {
    case .known(let value, let unit):
        if cost { return String(format: "$%.2f", Double(value) / 1_000_000) }
        if unit == "tokens" { return TokenQuantityFormatter.string(value) }
        return value.formatted()
    case .unknown, .absent:
        return "--"
    }
}
