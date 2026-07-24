import AppKit
import Charts
import CodexPulseAppSupport
import Combine
import SwiftUI

@MainActor
final class StatusItemController: NSObject {
    private let statusItem: NSStatusItem
    private let statusBarView = StatusBarQuotaContentView()
    private let popover = NSPopover()
    private let model: AppModel
    private let displayPreferences = StatusBarDisplayPreferences()
    private var cancellables: Set<AnyCancellable> = []

    init(
        model: AppModel,
        onOpenOverview: @escaping @MainActor () -> Void,
        onQuit: @escaping @MainActor () -> Void
    ) {
        self.model = model
        self.statusItem = NSStatusBar.system.statusItem(withLength: NSStatusItem.variableLength)
        super.init()

        if let button = statusItem.button {
            button.image = nil
            button.title = ""
            statusBarView.translatesAutoresizingMaskIntoConstraints = false
            button.addSubview(statusBarView)
            NSLayoutConstraint.activate([
                statusBarView.leadingAnchor.constraint(equalTo: button.leadingAnchor),
                statusBarView.trailingAnchor.constraint(equalTo: button.trailingAnchor),
                statusBarView.topAnchor.constraint(equalTo: button.topAnchor),
                statusBarView.bottomAnchor.constraint(equalTo: button.bottomAnchor),
            ])
            button.target = self
            button.action = #selector(togglePopover(_:))
            button.sendAction(on: [.leftMouseUp])
        }
        updateStatusBarView()

        popover.behavior = .transient
        popover.animates = true
        let popoverView = MenuBarPopoverView(
            model: model,
            preferences: displayPreferences,
            onOpenOverview: {
                self.popover.performClose(nil)
                onOpenOverview()
            },
            onQuit: onQuit
        )
        popover.contentViewController = NSHostingController(rootView: popoverView)
        popover.contentSize = NSSize(width: 420, height: 640)

        model.$state
            .receive(on: RunLoop.main)
            .sink { [weak self] _ in
                self?.updateStatusBarView()
            }
            .store(in: &cancellables)

        displayPreferences.$style
            .removeDuplicates()
            .receive(on: RunLoop.main)
            .sink { [weak self] _ in self?.updateStatusBarView() }
            .store(in: &cancellables)
    }

    private func updateStatusBarView() {
        let title = model.statusItemTitle
        let summary = model.presentation.flatMap(StatusBarQuotaPresentation.init)
        statusBarView.update(
            summary: summary,
            fallbackText: title,
            style: displayPreferences.style
        )
        statusItem.length = statusBarView.preferredWidth
        let summaryLabel = summary?.accessibilityLabel ?? title
        statusItem.button?.toolTip = "Codex Pulse · \(summaryLabel)"
        statusItem.button?.setAccessibilityLabel("Codex Pulse · \(summaryLabel)")
    }

    @objc private func togglePopover(_ sender: Any?) {
        guard statusItem.button != nil else { return }
        if popover.isShown {
            popover.performClose(sender)
        } else {
            showPopover()
        }
    }

    private func showPopover() {
        guard let button = statusItem.button else { return }
        popover.show(relativeTo: button.bounds, of: button, preferredEdge: .minY)
    }

    func verifyNativeSurfacesForSmoke() -> Bool {
        updateStatusBarView()
        guard let button = statusItem.button,
              popover.contentViewController != nil,
              statusBarView.superview === button,
              statusBarView.hasSummary,
              statusBarView.preferredWidth > 0
        else { return false }
        popover.show(relativeTo: button.bounds, of: button, preferredEdge: .minY)
        let shown = popover.isShown
        popover.performClose(nil)
        return shown
    }
}

private struct MenuBarPopoverView: View {
    @ObservedObject var model: AppModel
    @ObservedObject var preferences: StatusBarDisplayPreferences
    let onOpenOverview: @MainActor () -> Void
    let onQuit: @MainActor () -> Void
    @State private var route: PopoverRoute = .main
    @State private var selectedDailyTrendKey: String?

    var body: some View {
        ZStack {
            NativePopoverBackdrop()

            switch route {
            case .main:
                mainContent
            case .resetCredits:
                ResetCreditsDetailView(overview: model.presentation) { route = .main }
            case .displaySettings:
                StatusDisplaySettingsView(
                    preferences: preferences,
                    onBack: { route = .main },
                    onOpenSettings: {
                        model.navigate(to: .settings)
                        onOpenOverview()
                    }
                )
            }
        }
        .foregroundStyle(.primary)
        .frame(width: 420, height: 640)
    }

    private var mainContent: some View {
        VStack(spacing: 0) {
            PopoverHeader(
                title: "Codex Pulse",
                subtitle: model.isOverviewRefreshing
                    ? "正在刷新本机数据…"
                    : model.presentation.map { "本机数据 · \(relativeTimestamp($0.evaluatedAtMS))" } ?? "正在连接 Helper…",
                onOpen: onOpenOverview,
                onRefresh: model.refreshOrRestart,
                canRefresh: model.canRefreshOrRestart,
                isRefreshing: model.isOverviewRefreshing
            )

            ScrollView {
                if let overview = model.presentation {
                    VStack(alignment: .leading, spacing: 18) {
                        quotaSection(overview)
                        dailyTrendSection(overview)
                        resetCreditsSection(overview)
                        if preferences.showCostSummary { costSection(overview) }
                        if preferences.showProjectRanking { projectRankingSection(overview) }
                    }
                    .padding(.horizontal, 18)
                    .padding(.vertical, 16)
                } else {
                    VStack(spacing: 12) {
                        ProgressView()
                        Text(model.statusItemTitle).foregroundStyle(.secondary)
                    }
                    .frame(maxWidth: .infinity, minHeight: 440)
                }
            }
            .scrollIndicators(.hidden)

            PopoverFooter(
                onSettings: { route = .displaySettings },
                onQuit: onQuit
            )
        }
    }

    private func quotaSection(_ overview: OverviewPresentation) -> some View {
        VStack(alignment: .leading, spacing: 12) {
            PopoverSectionTitle(title: "配额", systemImage: "gauge.with.dots.needle.67percent")
            if overview.quotaWindows.isEmpty {
                PulseCard { Text("尚未取得可信额度数据").foregroundStyle(.secondary) }
            } else {
                ForEach(overview.quotaWindows.prefix(2)) { window in
                    VStack(alignment: .leading, spacing: 8) {
                        HStack(alignment: .firstTextBaseline) {
                            Text(window.title).font(.system(size: 15, weight: .semibold))
                            Spacer()
                            Text(percentText(window.remainingPercent))
                                .font(.system(size: 14, weight: .bold, design: .rounded))
                                .monospacedDigit()
                        }
                        GeometryReader { geometry in
                            ZStack(alignment: .leading) {
                                Capsule().fill(.quaternary)
                                Capsule()
                                    .fill(quotaColor(window.remainingPercent))
                                    .frame(width: geometry.size.width * progress(window.remainingPercent))
                            }
                        }
                        .frame(height: 9)
                        HStack {
                            Text("\(percentText(window.remainingPercent)) 剩余")
                            Spacer()
                            Text("下次重置：\(durationText(window.resetRemainingMS))")
                        }
                        .font(.caption)
                        .foregroundStyle(.secondary)
                    }
                }
            }
        }
    }

    private func resetCreditsSection(_ overview: OverviewPresentation) -> some View {
        let credits = overview.resetCredits
        return VStack(alignment: .leading, spacing: 10) {
            PopoverSectionTitle(title: "重置次数", systemImage: "clock.arrow.circlepath")
            Button { route = .resetCredits } label: {
                PulseCard {
                    VStack(alignment: .leading, spacing: 7) {
                        HStack {
                            Text("\(optionalCount(credits.availableCount)) 可用 / \(optionalCount(credits.totalCount)) 总数")
                                .font(.system(size: 15, weight: .semibold))
                            Spacer()
                            Image(systemName: "chevron.right").foregroundStyle(.tertiary)
                        }
                        HStack {
                            Text("总剩余：\(durationText(credits.cumulativeRemainingMS))")
                            Spacer()
                            Text(nextExpiryText(credits))
                        }
                        .font(.caption)
                        .foregroundStyle(.secondary)
                    }
                }
            }
            .contentShape(RoundedRectangle(cornerRadius: PopoverInteractionMetrics.cardCornerRadius, style: .continuous))
            .buttonStyle(InteractiveCardButtonStyle())
            .accessibilityIdentifier("popover.reset-credits")
        }
    }

    private func dailyTrendSection(_ overview: OverviewPresentation) -> some View {
        let buckets = overview.weeklyUsageModelTrend
        let modelNames = dailyTrendModelNames(buckets)
        let colors = dailyTrendColors(count: modelNames.count)
        let selectedBucket = buckets.first { $0.key == selectedDailyTrendKey }
        return VStack(alignment: .leading, spacing: 10) {
            PopoverSectionTitle(title: "本周每日 Token", systemImage: "chart.bar.fill")
            PulseCard {
                if !overview.weeklyUsageAvailable {
                    Text("本周用量趋势暂时不可用")
                        .foregroundStyle(.secondary)
                } else if buckets.isEmpty {
                    Text("本周还没有每日用量")
                        .foregroundStyle(.secondary)
                } else {
                    VStack(alignment: .leading, spacing: 10) {
                        HStack(alignment: .firstTextBaseline) {
                            Text(overview.weeklyUsageRangeLabel)
                            Spacer()
                            if buckets.contains(where: { !$0.breakdownAvailable }) {
                                Text("部分日期仅有总计")
                            }
                        }
                        .font(.caption2)
                        .foregroundStyle(.secondary)

                        Chart {
                                ForEach(buckets) { bucket in
                                    ForEach(bucket.segments) { segment in
                                        BarMark(
                                            x: .value("日期", segment.bucketKey),
                                            y: .value("Token", segment.tokens)
                                        )
                                        .foregroundStyle(by: .value("模型", segment.modelName))
                                        .opacity(
                                            selectedBucket == nil || selectedBucket?.key == bucket.key
                                                ? 1 : 0.3)
                                        .cornerRadius(2)
                                        .accessibilityLabel(
                                            "\(compactDailyTrendDate(segment.bucketKey)) · \(segment.modelName)"
                                        )
                                        .accessibilityValue(
                                            "\(TokenQuantityFormatter.string(segment.tokens)) Token")
                                    }
                                    if bucket.totalTokens > 0 {
                                        PointMark(
                                            x: .value("日期", bucket.key),
                                            y: .value("总计", bucket.totalTokens)
                                        )
                                        .foregroundStyle(.clear)
                                        .symbolSize(0)
                                        .annotation(position: .top, spacing: 3) {
                                            Text(TokenQuantityFormatter.compactString(bucket.totalTokens))
                                                .font(.system(size: 9, weight: .semibold, design: .rounded))
                                                .monospacedDigit()
                                                .foregroundStyle(.secondary)
                                        }
                                        .accessibilityHidden(true)
                                    }
                                }
                                if let selectedBucket {
                                    RuleMark(x: .value("选中日期", selectedBucket.key))
                                        .foregroundStyle(Color.secondary.opacity(0.55))
                                        .lineStyle(StrokeStyle(lineWidth: 1, dash: [4, 4]))
                                        .accessibilityHidden(true)
                                }
                        }
                        .chartForegroundStyleScale(domain: modelNames, range: colors)
                        .chartYAxis {
                            AxisMarks(position: .trailing, values: .automatic(desiredCount: 3)) { value in
                                AxisGridLine().foregroundStyle(.quaternary)
                                AxisValueLabel {
                                    if let value = value.as(Int64.self) {
                                        Text(TokenQuantityFormatter.compactString(value))
                                    }
                                }
                            }
                        }
                        .chartXAxis {
                            AxisMarks(values: buckets.map(\.key)) { value in
                                AxisValueLabel {
                                    if let value = value.as(String.self) {
                                        Text(compactDailyTrendDate(value))
                                    }
                                }
                            }
                        }
                        .chartLegend(.hidden)
                        .chartXSelection(value: $selectedDailyTrendKey)
                        .chartOverlay { proxy in
                            GeometryReader { geometry in
                                Rectangle()
                                    .fill(.clear)
                                    .contentShape(Rectangle())
                                    .onContinuousHover { phase in
                                        switch phase {
                                        case .active(let location):
                                            guard let plotFrame = proxy.plotFrame else {
                                                selectedDailyTrendKey = nil
                                                return
                                            }
                                            let plotRect = geometry[plotFrame]
                                            guard plotRect.contains(location) else {
                                                selectedDailyTrendKey = nil
                                                return
                                            }
                                            selectedDailyTrendKey = proxy.value(
                                                atX: location.x - plotRect.origin.x,
                                                as: String.self
                                            )
                                        case .ended:
                                            selectedDailyTrendKey = nil
                                        }
                                    }
                            }
                        }
                        .frame(height: 142)

                        if let selectedBucket {
                            dailyTrendHoverDetail(
                                selectedBucket,
                                modelNames: modelNames,
                                colors: colors
                            )
                            .allowsHitTesting(false)
                            .transition(.opacity)
                        } else {
                            LazyVGrid(
                                columns: [GridItem(.adaptive(minimum: 108), spacing: 8)],
                                alignment: .leading,
                                spacing: 6
                            ) {
                                ForEach(Array(modelNames.enumerated()), id: \.element) { index, name in
                                    HStack(spacing: 5) {
                                        Image(systemName: "circle.fill")
                                            .font(.system(size: 7))
                                            .foregroundStyle(colors[index])
                                        Text(name)
                                            .font(.caption2)
                                            .foregroundStyle(.secondary)
                                            .lineLimit(1)
                                    }
                                }
                            }
                        }
                    }
                }
            }
        }
        .accessibilityElement(children: .contain)
        .accessibilityIdentifier("popover.daily-trend")
    }

    private func dailyTrendModelNames(_ buckets: [UsageModelTrendBucket]) -> [String] {
        var names: [String] = []
        var seen = Set<String>()
        for bucket in buckets {
            for segment in bucket.segments where seen.insert(segment.modelName).inserted {
                names.append(segment.modelName)
            }
        }
        return names
    }

    private func dailyTrendColors(count: Int) -> [Color] {
        let palette: [Color] = [.blue, .green, .orange, .purple, .pink, .teal, .indigo, .mint]
        return (0..<count).map { palette[$0 % palette.count] }
    }

    private func dailyTrendHoverDetail(
        _ bucket: UsageModelTrendBucket,
        modelNames: [String],
        colors: [Color]
    ) -> some View {
        let shape = RoundedRectangle(cornerRadius: 10, style: .continuous)
        return VStack(alignment: .leading, spacing: 6) {
            HStack(alignment: .firstTextBaseline, spacing: 8) {
                Text(compactDailyTrendDate(bucket.key))
                    .font(.system(size: 12, weight: .bold))
                Spacer(minLength: 12)
                Text("总计 \(TokenQuantityFormatter.compactString(bucket.totalTokens)) Token")
                    .font(.caption2.monospacedDigit())
                    .foregroundStyle(.secondary)
            }
            Divider()
            TokenBreakdownView(tokens: bucket.tokenBreakdown, style: .compact)
            Divider()
            LazyVGrid(
                columns: [GridItem(.flexible()), GridItem(.flexible())],
                alignment: .leading,
                spacing: 7
            ) {
                ForEach(bucket.segments) { segment in
                    VStack(alignment: .leading, spacing: 2) {
                        HStack(spacing: 5) {
                            Image(systemName: "circle.fill")
                                .font(.system(size: 6))
                                .foregroundStyle(dailyTrendColor(
                                    for: segment.modelName,
                                    modelNames: modelNames,
                                    colors: colors
                                ))
                            Text(segment.modelName)
                                .font(.caption2)
                                .lineLimit(1)
                        }
                        Text(
                            "\(TokenQuantityFormatter.compactString(segment.tokens)) · "
                                + dailyTrendShareText(segment.tokens, total: bucket.totalTokens)
                        )
                        .font(.caption2.monospacedDigit())
                        .foregroundStyle(.secondary)
                        .padding(.leading, 11)
                    }
                }
            }
        }
        .padding(9)
        .frame(maxWidth: .infinity)
        .background(.primary.opacity(0.035), in: shape)
        .overlay(shape.strokeBorder(.primary.opacity(0.1), lineWidth: 1))
        .accessibilityElement(children: .combine)
        .accessibilityLabel(
            "\(compactDailyTrendDate(bucket.key))，总计 "
                + "\(TokenQuantityFormatter.string(bucket.totalTokens)) Token"
        )
    }

    private func dailyTrendColor(
        for modelName: String,
        modelNames: [String],
        colors: [Color]
    ) -> Color {
        guard let index = modelNames.firstIndex(of: modelName), colors.indices.contains(index) else {
            return .secondary
        }
        return colors[index]
    }

    private func dailyTrendShareText(_ tokens: Int64, total: Int64) -> String {
        guard total > 0 else { return "0%" }
        return String(format: "%.0f%%", Double(tokens) / Double(total) * 100)
    }

    private func compactDailyTrendDate(_ key: String) -> String {
        let parts = key.split(separator: "-")
        guard parts.count == 3, let month = Int(parts[1]), let day = Int(parts[2]) else {
            return key
        }
        return "\(month)/\(day)"
    }

    private func costSection(_ overview: OverviewPresentation) -> some View {
        VStack(alignment: .leading, spacing: 10) {
            PopoverSectionTitle(title: "API 等价成本", systemImage: "dollarsign.circle")
            PulseCard {
                VStack(alignment: .leading, spacing: 6) {
                    HStack(alignment: .firstTextBaseline) {
                        Text(metricText(overview.estimatedCost, cost: true))
                            .font(.system(size: 20, weight: .bold, design: .rounded))
                            .monospacedDigit()
                        Spacer()
                        Text(overview.usageRangeLabel).font(.caption).foregroundStyle(.secondary)
                    }
                    TokenBreakdownView(tokens: overview.tokenBreakdown, style: .compact)
                    Text("本地会话估算")
                        .font(.caption2)
                        .foregroundStyle(.tertiary)
                }
            }
        }
    }

    private func projectRankingSection(_ overview: OverviewPresentation) -> some View {
        VStack(alignment: .leading, spacing: 10) {
            PopoverSectionTitle(title: "本周项目 Token 排行", systemImage: "chart.bar.xaxis")
            PulseCard(padding: 0) {
                if !overview.weeklyProjectRankingAvailable {
                    Text("周额度项目排行暂时不可用")
                        .foregroundStyle(.secondary)
                        .padding(12)
                } else if overview.weeklyProjectRanking.isEmpty {
                    Text("本周暂无已归类项目用量")
                        .foregroundStyle(.secondary)
                        .padding(12)
                } else {
                    VStack(spacing: 0) {
                        ForEach(Array(overview.weeklyProjectRanking.enumerated()), id: \.element.id) { index, project in
                            HStack(spacing: 10) {
                                Text("\(index + 1)")
                                    .font(.caption.bold())
                                    .foregroundStyle(.secondary)
                                    .frame(width: 18)
                                VStack(alignment: .leading, spacing: 3) {
                                    Text(project.title).font(.system(size: 13, weight: .semibold)).lineLimit(1)
                                    Text("周额度周期")
                                        .font(.caption2)
                                        .foregroundStyle(.secondary)
                                        .lineLimit(1)
                                }
                                Spacer(minLength: 8)
                                Text(metricText(project.tokens))
                                    .font(.caption.weight(.semibold))
                                    .monospacedDigit()
                                    .foregroundStyle(.secondary)
                            }
                            .padding(.horizontal, 12)
                            .padding(.vertical, 9)
                            if index < overview.weeklyProjectRanking.count - 1 {
                                Divider().padding(.leading, 29)
                            }
                        }
                    }
                }
            }
        }
    }

    private func nextExpiryText(_ credits: ResetCreditsPresentation) -> String {
        credits.nextExpiresAtMS == nil ? "无近期到期" : "最近到期 \(minimumRemainingText(credits))"
    }
}

private enum PopoverRoute {
    case main
    case resetCredits
    case displaySettings
}

private struct RefreshArrowSymbol: View {
    let isAnimating: Bool

    var body: some View {
        TimelineView(.animation(minimumInterval: 1 / 30, paused: !isAnimating)) { context in
            Image(systemName: "arrow.clockwise")
                .rotationEffect(.degrees(rotationAngle(at: context.date)))
        }
    }

    private func rotationAngle(at date: Date) -> Double {
        guard isAnimating else { return 0 }
        let duration = 0.8
        return date.timeIntervalSinceReferenceDate
            .truncatingRemainder(dividingBy: duration) / duration * 360
    }
}

private struct PopoverHeader: View {
    let title: String
    let subtitle: String
    let onOpen: @MainActor () -> Void
    let onRefresh: @MainActor () -> Void
    let canRefresh: Bool
    let isRefreshing: Bool

    var body: some View {
        HStack(spacing: 12) {
            VStack(alignment: .leading, spacing: 3) {
                Text(title).font(.title3.bold())
                Text(subtitle).font(.caption).foregroundStyle(.secondary)
            }
            Spacer()
            HStack(spacing: 16) {
                Button(action: onOpen) { Image(systemName: "macwindow") }
                    .help("打开主窗口")
                    .accessibilityIdentifier("popover.open-overview")
                Button(action: onRefresh) {
                    RefreshArrowSymbol(isAnimating: isRefreshing)
                        .frame(width: 18, height: 18)
                }
                    .disabled(!canRefresh)
                    .help(isRefreshing ? "正在刷新本地数据" : "刷新本地数据")
                    .accessibilityLabel(isRefreshing ? "正在刷新本地数据" : "刷新本地数据")
                    .accessibilityValue(isRefreshing ? "进行中" : "就绪")
                    .accessibilityIdentifier("popover.refresh")
            }
            .buttonStyle(PopoverIconButtonStyle())
        }
        .padding(.horizontal, 18)
        .padding(.vertical, 14)
        .nativeGlass(in: Rectangle())
    }
}

private struct ResetCreditsDetailView: View {
    let overview: OverviewPresentation?
    let onBack: () -> Void

    var body: some View {
        ScrollView {
            VStack(alignment: .leading, spacing: 18) {
                PopoverBackButton(onBack: onBack)
                if let credits = overview?.resetCredits {
                    HStack(alignment: .firstTextBaseline) {
                        VStack(alignment: .leading, spacing: 3) {
                            Text("重置次数").font(.title2.bold())
                            Text("更新于 \(credits.lastSuccessAtMS.map(relativeTimestamp) ?? "--")")
                                .font(.caption).foregroundStyle(.secondary)
                        }
                        Spacer()
                        Text("\(optionalCount(credits.availableCount))/\(optionalCount(credits.totalCount))")
                            .font(.title2.bold().monospacedDigit())
                    }
                    LazyVGrid(columns: [GridItem(.flexible()), GridItem(.flexible())], spacing: 10) {
                        SummaryTile(label: "可用", value: optionalCount(credits.availableCount), color: .green)
                        SummaryTile(label: "已使用", value: optionalCount(credits.redeemedCount), color: .orange)
                        SummaryTile(label: "总剩余", value: durationText(credits.cumulativeRemainingMS), color: .blue)
                        SummaryTile(label: "最近到期", value: minimumRemainingText(credits), color: .orange)
                    }
                    VStack(alignment: .leading, spacing: 8) {
                        Text("到期风险").font(.headline)
                        GeometryReader { geometry in
                            Capsule().fill(.quaternary)
                                .overlay(alignment: .leading) {
                                    Capsule().fill(Color.green).frame(width: geometry.size.width * availabilityRatio(credits))
                                }
                        }
                        .frame(height: 10)
                        Text("可用 \(optionalCount(credits.availableCount)) · 已使用或过期 \(unavailableCount(credits))")
                            .font(.caption).foregroundStyle(.secondary)
                    }
                    VStack(alignment: .leading, spacing: 10) {
                        Text("次数").font(.headline)
                        if credits.items.isEmpty {
                            PulseCard { Text("当前没有逐条次数事实").foregroundStyle(.secondary) }
                        } else {
                            ForEach(Array(credits.items.enumerated()), id: \.element.id) { index, item in
                                PulseCard {
                                    HStack(alignment: .top) {
                                        VStack(alignment: .leading, spacing: 4) {
                                            Text("次数 \(index + 1)").font(.system(size: 13, weight: .semibold))
                                            Text("到期：\(absoluteTimestamp(item.expiresAtMS))")
                                                .font(.caption).foregroundStyle(.secondary)
                                        }
                                        Spacer()
                                        VStack(alignment: .trailing, spacing: 4) {
                                            StatusCapsule(status: item.status)
                                            Text(item.remainingMS.map(durationText) ?? "--")
                                                .font(.caption.monospacedDigit()).foregroundStyle(.secondary)
                                        }
                                    }
                                }
                            }
                        }
                    }
                } else {
                    Text("重置次数").font(.title2.bold())
                    ProgressView().frame(maxWidth: .infinity, minHeight: 480)
                }
            }
            .padding(18)
        }
        .scrollIndicators(.hidden)
    }
}

private struct StatusDisplaySettingsView: View {
    @ObservedObject var preferences: StatusBarDisplayPreferences
    let onBack: () -> Void
    let onOpenSettings: () -> Void

    var body: some View {
        ScrollView {
            VStack(alignment: .leading, spacing: 20) {
                PopoverBackButton(onBack: onBack)
                VStack(alignment: .leading, spacing: 3) {
                    Text("显示设置").font(.title2.bold())
                    Text("状态栏与弹窗内容").font(.caption).foregroundStyle(.secondary)
                }
                VStack(alignment: .leading, spacing: 10) {
                    Text("状态栏样式").font(.headline)
                    Picker("状态栏样式", selection: $preferences.style) {
                        ForEach(StatusBarStyle.allCases) { style in Text(style.title).tag(style) }
                    }
                    .pickerStyle(.menu)
                    .labelsHidden()
                    .frame(maxWidth: .infinity, minHeight: PopoverInteractionMetrics.minimumHitTarget, alignment: .leading)
                    .contentShape(Rectangle())
                    .accessibilityIdentifier("popover.status-style")
                    Text("三种样式均显示真实额度周期剩余与同周期 Token；范围无法对齐时用量显示 --。")
                        .font(.caption).foregroundStyle(.secondary)
                }
                PulseCard {
                    VStack(alignment: .leading, spacing: 14) {
                        SettingsToggle(identifier: "cost-summary", title: "显示 API 成本摘要", subtitle: "当前周额度周期的本地 API 等价估算", value: $preferences.showCostSummary)
                        Divider()
                        SettingsToggle(identifier: "project-ranking", title: "显示项目排行", subtitle: "按周额度周期展示 Token 前 5 的已归类项目", value: $preferences.showProjectRanking)
                    }
                }
                Button(action: onOpenSettings) {
                    Label("打开完整设置", systemImage: "gearshape")
                        .frame(maxWidth: .infinity)
                }
                .buttonStyle(NativeGlassButtonStyle())
                .accessibilityIdentifier("popover.open-settings")
            }
            .padding(18)
        }
        .scrollIndicators(.hidden)
    }
}

private struct SettingsToggle: View {
    let identifier: String
    let title: String
    let subtitle: String
    @Binding var value: Bool

    var body: some View {
        Toggle(isOn: $value) {
            VStack(alignment: .leading, spacing: 3) {
                Text(title).font(.system(size: 13, weight: .semibold))
                Text(subtitle).font(.caption).foregroundStyle(.secondary)
            }
        }
        .toggleStyle(.switch)
        .padding(.vertical, 4)
        .frame(maxWidth: .infinity, minHeight: PopoverInteractionMetrics.minimumHitTarget, alignment: .leading)
        .contentShape(Rectangle())
        .accessibilityIdentifier("popover.toggle.\(identifier)")
    }
}

private struct PopoverBackButton: View {
    let onBack: () -> Void

    var body: some View {
        Button(action: onBack) { Label("返回", systemImage: "chevron.left") }
            .buttonStyle(NativeGlassButtonStyle())
            .accessibilityIdentifier("popover.back")
    }
}

private struct PopoverFooter: View {
    let onSettings: () -> Void
    let onQuit: @MainActor () -> Void

    var body: some View {
        HStack {
            Button(action: onSettings) { Label("设置", systemImage: "slider.horizontal.3") }
                .accessibilityIdentifier("popover.settings")
            Spacer()
            Button(action: onQuit) { Label("退出", systemImage: "power") }
                .accessibilityIdentifier("popover.quit")
        }
        .buttonStyle(NativeGlassButtonStyle())
        .padding(.horizontal, 18)
        .padding(.vertical, 12)
        .nativeGlass(in: Rectangle())
    }
}

private struct PopoverSectionTitle: View {
    let title: String
    let systemImage: String

    var body: some View {
        Label(title, systemImage: systemImage).font(.system(size: 14, weight: .bold))
    }
}

private struct PulseCard<Content: View>: View {
    @Environment(\.controlActiveState) private var controlActiveState
    let padding: CGFloat
    @ViewBuilder let content: Content

    init(padding: CGFloat = 12, @ViewBuilder content: () -> Content) {
        self.padding = padding
        self.content = content()
    }

    var body: some View {
        let shape = RoundedRectangle(cornerRadius: PopoverInteractionMetrics.cardCornerRadius, style: .continuous)
        content
            .padding(padding)
            .frame(maxWidth: .infinity, alignment: .leading)
            .background(cardFill, in: shape)
            .nativeGlass(in: shape)
            .overlay(shape.strokeBorder(cardBorder, lineWidth: 1))
            .contentShape(shape)
    }

    private var cardFill: Color {
        .primary.opacity(controlActiveState == .inactive ? 0.065 : 0.03)
    }

    private var cardBorder: Color {
        .primary.opacity(controlActiveState == .inactive ? 0.18 : 0.1)
    }
}

private struct SummaryTile: View {
    let label: String
    let value: String
    let color: Color

    var body: some View {
        PulseCard {
            VStack(alignment: .leading, spacing: 6) {
                HStack(spacing: 6) { Circle().fill(color).frame(width: 7, height: 7); Text(label) }
                    .font(.caption).foregroundStyle(.secondary)
                Text(value).font(.system(size: 15, weight: .bold, design: .rounded)).monospacedDigit()
            }
        }
    }
}

private struct StatusCapsule: View {
    let status: String

    var body: some View {
        Text(statusTitle)
            .font(.caption2.bold())
            .padding(.horizontal, 7)
            .padding(.vertical, 3)
            .background(statusColor.opacity(0.2), in: Capsule())
            .foregroundStyle(statusColor)
    }

    private var statusTitle: String {
        switch status {
        case "available": "可用"
        case "redeemed", "used": "已使用"
        case "expired": "已过期"
        default: "未知"
        }
    }

    private var statusColor: Color {
        status == "available" ? .green : (status == "expired" ? .secondary : .orange)
    }
}

private struct PopoverIconButtonStyle: ButtonStyle {
    @Environment(\.controlActiveState) private var controlActiveState
    @Environment(\.isEnabled) private var isEnabled

    func makeBody(configuration: Configuration) -> some View {
        let shape = RoundedRectangle(cornerRadius: 8, style: .continuous)
        configuration.label
            .frame(
                width: PopoverInteractionMetrics.iconVisualSize,
                height: PopoverInteractionMetrics.iconVisualSize
            )
            .foregroundStyle(.primary)
            .background(iconFill, in: shape)
            .nativeGlass(in: shape)
            .overlay(shape.strokeBorder(iconBorder, lineWidth: 1))
            .scaleEffect(configuration.isPressed ? 0.94 : 1)
            .opacity(isEnabled ? (configuration.isPressed ? 0.72 : 1) : 0.5)
            .frame(
                width: PopoverInteractionMetrics.minimumHitTarget,
                height: PopoverInteractionMetrics.minimumHitTarget
            )
            .contentShape(Rectangle())
            .padding(-PopoverInteractionMetrics.iconHitSlop)
            .animation(.easeOut(duration: 0.08), value: configuration.isPressed)
    }

    private var iconFill: Color {
        .primary.opacity(controlActiveState == .inactive ? 0.075 : 0.04)
    }

    private var iconBorder: Color {
        .primary.opacity(controlActiveState == .inactive ? 0.22 : 0.12)
    }
}

private struct NativeGlassButtonStyle: ButtonStyle {
    @Environment(\.controlActiveState) private var controlActiveState
    @Environment(\.isEnabled) private var isEnabled

    func makeBody(configuration: Configuration) -> some View {
        let shape = RoundedRectangle(cornerRadius: 8, style: .continuous)
        configuration.label
            .font(.system(size: 13, weight: .medium))
            .padding(.horizontal, 10)
            .frame(height: PopoverInteractionMetrics.compactButtonVisualHeight)
            .foregroundStyle(.primary)
            .background(buttonFill, in: shape)
            .nativeGlass(in: shape)
            .overlay(shape.fill(configuration.isPressed ? Color.primary.opacity(0.08) : .clear))
            .overlay(shape.strokeBorder(buttonBorder, lineWidth: 1))
            .scaleEffect(configuration.isPressed ? 0.97 : 1)
            .opacity(isEnabled ? (configuration.isPressed ? 0.76 : 1) : 0.5)
            .frame(minHeight: PopoverInteractionMetrics.minimumHitTarget)
            .contentShape(Rectangle())
            .padding(.vertical, -PopoverInteractionMetrics.compactButtonHitSlop)
            .animation(.easeOut(duration: 0.08), value: configuration.isPressed)
    }

    private var buttonFill: Color {
        .primary.opacity(controlActiveState == .inactive ? 0.075 : 0.04)
    }

    private var buttonBorder: Color {
        .primary.opacity(controlActiveState == .inactive ? 0.22 : 0.12)
    }
}

private struct InteractiveCardButtonStyle: ButtonStyle {
    func makeBody(configuration: Configuration) -> some View {
        configuration.label
            .modifier(InteractiveFeedbackModifier(
                shape: RoundedRectangle(cornerRadius: PopoverInteractionMetrics.cardCornerRadius, style: .continuous),
                isPressed: configuration.isPressed
            ))
    }
}

private struct InteractiveFeedbackModifier<S: InsettableShape>: ViewModifier {
    @Environment(\.isEnabled) private var isEnabled
    let shape: S
    let isPressed: Bool

    func body(content: Content) -> some View {
        content
            .overlay(shape.fill(isPressed ? Color.primary.opacity(0.08) : .clear))
            .contentShape(shape)
            .scaleEffect(isPressed ? 0.995 : 1)
            .opacity(isEnabled ? 1 : 0.55)
            .animation(.easeOut(duration: 0.08), value: isPressed)
    }
}

private struct NativePopoverBackdrop: View {
    @Environment(\.controlActiveState) private var controlActiveState

    var body: some View {
        Rectangle()
            .fill(Color.primary.opacity(controlActiveState == .inactive ? 0.025 : 0))
            .nativeGlass(in: Rectangle())
            .ignoresSafeArea()
    }
}

private enum PopoverInteractionMetrics {
    static let minimumHitTarget: CGFloat = 44
    static let iconVisualSize: CGFloat = 28
    static let iconHitSlop = (minimumHitTarget - iconVisualSize) / 2
    static let compactButtonVisualHeight: CGFloat = 28
    static let compactButtonHitSlop = (minimumHitTarget - compactButtonVisualHeight) / 2
    static let cardCornerRadius: CGFloat = 12
}

private struct NativeGlassModifier<S: Shape>: ViewModifier {
    let shape: S

    @ViewBuilder
    func body(content: Content) -> some View {
        if #available(macOS 26.0, *) {
            content.glassEffect(.regular, in: shape)
        } else {
            content.background(.ultraThinMaterial, in: shape)
        }
    }
}

private extension View {
    func nativeGlass<S: Shape>(in shape: S) -> some View {
        modifier(NativeGlassModifier(shape: shape))
    }
}

private func percentText(_ value: Double?) -> String {
    value.map { String(format: "%.0f%%", $0) } ?? "--"
}

private func progress(_ value: Double?) -> CGFloat {
    CGFloat(max(0, min(100, value ?? 0))) / 100
}

private func quotaColor(_ value: Double?) -> Color {
    switch QuotaRemainingLevel(remainingPercent: value) {
    case .healthy: .green
    case .warning: .yellow
    case .critical: .red
    case .unavailable: .gray
    }
}

private func optionalCount(_ value: Int64?) -> String {
    value?.formatted() ?? "--"
}

private func durationText(_ milliseconds: Int64?) -> String {
    guard let milliseconds, milliseconds >= 0 else { return "--" }
    let totalMinutes = milliseconds / 60_000
    let days = totalMinutes / 1_440
    let hours = totalMinutes % 1_440 / 60
    let minutes = totalMinutes % 60
    if days > 0 { return "\(days)天 \(hours)小时" }
    if hours > 0 { return "\(hours)小时 \(minutes)分钟" }
    return "\(minutes)分钟"
}

private func relativeTimestamp(_ milliseconds: Int64) -> String {
    guard milliseconds > 0 else { return "时间未知" }
    let formatter = RelativeDateTimeFormatter()
    formatter.unitsStyle = .short
    return formatter.localizedString(for: Date(timeIntervalSince1970: Double(milliseconds) / 1_000), relativeTo: Date())
}

private func absoluteTimestamp(_ milliseconds: Int64) -> String {
    guard milliseconds > 0 else { return "--" }
    return Date(timeIntervalSince1970: Double(milliseconds) / 1_000).formatted(
        .dateTime.year().month().day().hour().minute()
    )
}

private func availabilityRatio(_ credits: ResetCreditsPresentation) -> CGFloat {
    guard let available = credits.availableCount, let total = credits.totalCount, total > 0 else { return 0 }
    return CGFloat(max(0, min(total, available))) / CGFloat(total)
}

private func unavailableCount(_ credits: ResetCreditsPresentation) -> String {
    guard let available = credits.availableCount, let total = credits.totalCount else { return "--" }
    return max(0, total - available).formatted()
}

private func minimumRemainingText(_ credits: ResetCreditsPresentation) -> String {
    credits.items.compactMap(\.remainingMS).min().map(durationText) ?? "--"
}
