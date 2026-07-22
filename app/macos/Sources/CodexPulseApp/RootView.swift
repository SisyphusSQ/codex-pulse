import CodexPulseAppSupport
import SwiftUI

struct RootView: View {
    @ObservedObject var model: AppModel

    var body: some View {
        NavigationSplitView {
            List(AppFeature.allCases, selection: selection) { section in
                Label(section.title, systemImage: section.symbol)
                    .tag(section)
            }
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
                            Label(
                                model.requiresCoreRestart ? "重新连接" : "刷新当前页面",
                                systemImage: model.requiresCoreRestart ? "bolt.horizontal.circle" : "arrow.clockwise"
                            )
                        }
                        .keyboardShortcut("r", modifiers: .command)
                        .disabled(!model.canRefreshOrRestart)
                        .help(model.requiresCoreRestart ? "重新连接核心组件" : "刷新当前页面")
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
            OverviewContentView(overview: overview, onNavigate: onNavigate)
        case .partial(let overview):
            OverviewContentView(overview: overview, onNavigate: onNavigate)
        case .stale(let overview, _):
            VStack(spacing: 0) {
                StateBanner(
                    title: "连接中断，正在显示上次结果",
                    systemImage: "clock.arrow.circlepath",
                    color: .orange
                )
                OverviewContentView(overview: overview, onNavigate: onNavigate)
                if model.requiresCoreRestart {
                    Button("重新连接") { model.restartCore() }
                        .buttonStyle(.borderedProminent)
                        .padding(.bottom, 16)
                }
            }
        case .recovery(let phase, let stage, _):
            ContentUnavailableView {
                Label("核心组件需要恢复", systemImage: "wrench.and.screwdriver")
            } description: {
                Text("阶段：\(phase) · 步骤：\(stage)")
            } actions: {
                Button("重试恢复") { model.retryRecovery() }
            }
        case .restartRequired:
            ContentUnavailableView {
                Label("需要重新启动核心组件", systemImage: "arrow.triangle.2.circlepath")
            } description: {
                Text("恢复已完成，重新建立连接后才能继续。")
            } actions: {
                Button("重新启动") { model.restartCore() }
            }
        case .unavailable(let notice):
            ContentUnavailableView {
                Label("核心组件不可用", systemImage: "bolt.slash")
            } description: {
                Text(notice.retryable ? "可以重试连接。" : "当前组件版本或契约不兼容。")
            } actions: {
                if notice.retryable { Button("重试") { model.restartCore() } }
            }
        case .cancelled:
            ContentUnavailableView("加载已取消", systemImage: "xmark.circle")
        case .shuttingDown:
            loading("正在安全退出…")
        case .stopped:
            ContentUnavailableView("核心组件已停止", systemImage: "stop.circle")
        }
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
    let onNavigate: (AppFeature) -> Void

    var body: some View {
        ScrollView {
            VStack(alignment: .leading, spacing: 20) {
                navigationSection
                quotaSection
                usageSection
                trendSection
                sessionSection
            }
            .padding(24)
            .frame(maxWidth: .infinity, alignment: .leading)
        }
    }

    private var navigationSection: some View {
        LazyVGrid(columns: [GridItem(.adaptive(minimum: 150), spacing: 10)], spacing: 10) {
            ForEach(AppFeature.allCases.filter { $0 != .overview }) { feature in
                Button {
                    onNavigate(feature)
                } label: {
                    Label(feature.title, systemImage: feature.symbol)
                        .frame(maxWidth: .infinity, alignment: .leading)
                        .padding(.vertical, 6)
                }
                .buttonStyle(.bordered)
                .accessibilityIdentifier("overview.navigate.\(feature.rawValue)")
            }
        }
    }

    private var quotaSection: some View {
        VStack(alignment: .leading, spacing: 10) {
            Text("额度").font(.title2.bold())
            LazyVGrid(columns: [GridItem(.adaptive(minimum: 220), spacing: 12)], spacing: 12) {
                if overview.quotaWindows.isEmpty {
                    MetricCard(title: "当前额度", value: "--", detail: "尚未取得可信数据")
                } else {
                    ForEach(overview.quotaWindows) { window in
                        MetricCard(
                            title: window.title,
                            value: window.remainingPercent.map { String(format: "%.0f%%", $0) } ?? "--",
                            detail: window.unknownReason == nil ? window.freshness : "数据未知"
                        )
                    }
                }
            }
        }
    }

    private var usageSection: some View {
        VStack(alignment: .leading, spacing: 10) {
            Text("最近 7 天").font(.title2.bold())
            LazyVGrid(columns: [GridItem(.adaptive(minimum: 220), spacing: 12)], spacing: 12) {
                MetricCard(
                    title: "API 等价成本",
                    value: metricText(overview.estimatedCost, cost: true),
                    detail: "估算值，不代表真实支出"
                )
                MetricCard(
                    title: "Token 总量",
                    value: metricText(overview.totalTokens),
                    detail: "来自 Helper 聚合结果"
                )
            }
        }
    }

    private var trendSection: some View {
        VStack(alignment: .leading, spacing: 10) {
            Text("每日趋势").font(.title2.bold())
            if overview.trend.isEmpty {
                Text("当前范围没有已索引用量。")
                    .foregroundStyle(.secondary)
            } else {
                ForEach(overview.trend) { point in
                    HStack {
                        Text(point.key).monospacedDigit()
                        Spacer()
                        Text(metricText(point.tokens))
                            .foregroundStyle(.secondary)
                        Text(metricText(point.estimatedCost, cost: true))
                            .frame(width: 100, alignment: .trailing)
                    }
                    Divider()
                }
            }
        }
        .padding(16)
        .background(.quaternary.opacity(0.35), in: RoundedRectangle(cornerRadius: 12))
    }

    private var sessionSection: some View {
        VStack(alignment: .leading, spacing: 10) {
            Text("最近会话").font(.title2.bold())
            if overview.sessions.isEmpty {
                Text("当前没有可显示的会话。")
                    .foregroundStyle(.secondary)
            } else {
                ForEach(overview.sessions) { session in
                    HStack {
                        VStack(alignment: .leading) {
                            Text(session.title).lineLimit(1)
                            Text(session.activity).font(.caption).foregroundStyle(.secondary)
                        }
                        Spacer()
                        Text(metricText(session.tokens)).monospacedDigit()
                    }
                    Divider()
                }
            }
        }
        .padding(16)
        .background(.quaternary.opacity(0.35), in: RoundedRectangle(cornerRadius: 12))
    }

}

struct MetricCard: View {
    let title: String
    let value: String
    let detail: String

    var body: some View {
        VStack(alignment: .leading, spacing: 6) {
            Text(title).font(.headline)
            Text(value).font(.system(size: 30, weight: .semibold, design: .rounded)).monospacedDigit()
            Text(detail).font(.caption).foregroundStyle(.secondary)
        }
        .frame(maxWidth: .infinity, alignment: .leading)
        .padding(16)
        .background(.quaternary.opacity(0.35), in: RoundedRectangle(cornerRadius: 12))
        .accessibilityElement(children: .combine)
    }
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
    case .known(let value, _):
        if cost { return String(format: "$%.2f", Double(value) / 1_000_000) }
        return value.formatted()
    case .unknown, .absent:
        return "--"
    }
}
