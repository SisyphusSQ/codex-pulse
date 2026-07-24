import Charts
import CodexPulseAppSupport
import CodexPulseProtocolGenerated
import SwiftUI

struct SessionsView: View {
    @ObservedObject var model: AppModel
    @State private var showsFilters = false

    var body: some View {
        VStack(spacing: 0) {
            pageHeader
            if showsFilters { filters }
            Divider()
            FeatureStateView(
                state: model.sessionsState, emptyTitle: "当前条件下没有会话", emptySystemImage: "text.bubble"
            ) {
                sessionResults($0)
            }
        }
        .frame(maxWidth: .infinity, maxHeight: .infinity, alignment: .topLeading)
        .accessibilityIdentifier("page.sessions")
    }

    private var pageHeader: some View {
        HStack(alignment: .center, spacing: 16) {
            VStack(alignment: .leading, spacing: 3) {
                Text("会话").font(.title.bold())
                Text("查看本机会话的用量、模型和活动时间")
                    .font(.subheadline)
                    .foregroundStyle(.secondary)
            }
            Spacer()
            Button {
                withAnimation(.snappy) { showsFilters.toggle() }
            } label: {
                Label(showsFilters ? "收起筛选" : "筛选", systemImage: "line.3.horizontal.decrease.circle")
            }
            .accessibilityIdentifier("sessions.filters.toggle")
        }
        .padding(.horizontal, 18)
        .padding(.vertical, 14)
    }

    private var filters: some View {
        VStack(alignment: .leading, spacing: 10) {
            HStack {
                Text("筛选条件")
                    .font(.headline)
                Spacer()
                Button("应用筛选") { model.sessionFiltersChanged() }
                    .keyboardShortcut(.return, modifiers: [])
                    .accessibilityIdentifier("sessions.filters.apply")
            }

            LazyVGrid(
                columns: [
                    GridItem(.adaptive(minimum: 205, maximum: 280), spacing: 12, alignment: .leading)
                ],
                alignment: .leading,
                spacing: 10
            ) {
                SessionFilterField(title: "范围") {
                    Picker("范围", selection: $model.sessionOptions.range) {
                        ForEach(DateRangePreset.allCases.filter {
                            $0 != .quotaWeek || model.sessionOptions.exactRange != nil
                        }) { Text($0.title).tag($0) }
                    }
                    .labelsHidden()
                    .frame(maxWidth: .infinity)
                    .onChange(of: model.sessionOptions.range) { oldValue, newValue in
                        if oldValue != newValue { model.sessionOptions.exactRange = nil }
                    }
                }
                SessionFilterField(title: "活跃状态") {
                    Picker("活跃状态", selection: $model.sessionOptions.activity) {
                        Text("全部状态").tag("all")
                        Text("活跃").tag("active")
                        Text("空闲").tag("idle")
                    }
                    .labelsHidden()
                    .frame(maxWidth: .infinity)
                }
                SessionFilterField(title: "项目名称") {
                    TextField("可选，精确匹配", text: $model.sessionOptions.projectID)
                        .textFieldStyle(.roundedBorder)
                        .accessibilityLabel("项目名称，精确匹配")
                }
                SessionFilterField(title: "模型名称") {
                    TextField("可选，精确匹配", text: $model.sessionOptions.modelKey)
                        .textFieldStyle(.roundedBorder)
                        .accessibilityLabel("模型名称，精确匹配")
                }
                SessionFilterField(title: "排序") {
                    Picker("排序", selection: $model.sessionOptions.sortField) {
                        Text("最近活动").tag("lastActivityAt")
                        Text("Token").tag("totalTokens")
                        Text("API 折算成本").tag("estimatedCost")
                    }
                    .labelsHidden()
                    .frame(maxWidth: .infinity)
                }
            }
        }
        .padding(16)
        .accessibilityElement(children: .contain)
        .accessibilityIdentifier("sessions.filters")
    }

    @ViewBuilder
    private func sessionResults(_ response: Codexpulse_Core_V1_SessionListResponse) -> some View {
        if response.items.isEmpty {
            ContentUnavailableView {
                Label("没有可显示的会话", systemImage: "text.bubble")
            } description: {
                Text("当前筛选条件没有匹配结果。")
            }
            .frame(maxWidth: .infinity, maxHeight: .infinity)
        } else {
            sessionSplit(response)
        }
    }

    private func sessionSplit(_ response: Codexpulse_Core_V1_SessionListResponse) -> some View {
        HSplitView {
            VStack(spacing: 0) {
                HStack {
                    Text("已加载 \(response.items.count) 条")
                    Spacer()
                    Text("匹配 \(numericText(response.matchedCount))")
                }
                .font(.caption)
                .foregroundStyle(.secondary)
                .padding(10)
                List(response.items, id: \.sessionID, selection: sessionSelection) { item in
                    SessionRow(item: item)
                        .tag(item.sessionID)
                }
                if pageHasMore(response.meta) {
                    Button(model.sessionsState.isLoading ? "正在加载…" : "加载更多") {
                        model.loadSessions(reset: false)
                    }
                    .disabled(model.sessionsState.isLoading)
                    .padding(8)
                    .accessibilityIdentifier("sessions.load-more")
                }
            }
            .frame(minWidth: 300, idealWidth: 360, maxWidth: 430)
            sessionDetail
                .frame(minWidth: 340, idealWidth: 480)
        }
        .frame(maxWidth: .infinity, maxHeight: .infinity)
    }

    private var sessionSelection: Binding<String?> {
        Binding(get: { model.selectedSessionID }, set: { model.selectSession($0) })
    }

    @ViewBuilder
    private var sessionDetail: some View {
        if model.selectedSessionID == nil {
            VStack(spacing: 10) {
                Image(systemName: "sidebar.right")
                    .font(.title2)
                    .foregroundStyle(.tertiary)
                Text("选择一个会话")
                    .font(.headline)
                Text("会话用量和活动时间线会显示在这里。")
                    .font(.caption)
                    .foregroundStyle(.secondary)
            }
            .frame(maxWidth: .infinity, maxHeight: .infinity)
            .accessibilityElement(children: .combine)
        } else {
            FeatureStateView(
                state: model.sessionDetailState,
                emptyTitle: "会话没有可显示的详情",
                emptySystemImage: "sidebar.right"
            ) { response in
                SessionDetailView(
                    response: response,
                    isLoading: model.sessionDetailState.isLoading,
                    loadMore: model.loadMoreSessionTurns
                )
            }
        }
    }
}

private struct SessionFilterField<Content: View>: View {
    let title: String
    @ViewBuilder let content: () -> Content

    var body: some View {
        VStack(alignment: .leading, spacing: 5) {
            Text(title)
                .font(.caption)
                .foregroundStyle(.secondary)
            content()
                .frame(maxWidth: .infinity, alignment: .leading)
        }
    }
}

private struct SessionRow: View {
    let item: Codexpulse_Core_V1_SessionItem

    var body: some View {
        VStack(alignment: .leading, spacing: 5) {
            HStack {
                Text(item.displayTitle.isEmpty ? "未命名会话" : item.displayTitle).lineLimit(1)
                Spacer()
                StatusPill(text: item.activity)
            }
            Text("\(attributionText(item.project)) · \(attributionText(item.model))")
                .font(.caption)
                .foregroundStyle(.secondary)
                .lineLimit(1)
            HStack {
                Text(timestampText(item.lastActivityAtMs))
                Spacer()
            }
            .font(.caption2)
            .foregroundStyle(.secondary)
            TokenBreakdownView(tokens: TokenBreakdownPresentation(item.totals), style: .compact)
        }
        .padding(.vertical, 4)
        .accessibilityElement(children: .combine)
        .accessibilityIdentifier("session.\(item.sessionID)")
    }
}

private struct SessionDetailView: View {
    let response: Codexpulse_Core_V1_SessionDetailResponse
    let isLoading: Bool
    let loadMore: () -> Void

    var body: some View {
        ScrollView {
            VStack(alignment: .leading, spacing: 14) {
                Text(response.item.displayTitle.isEmpty ? "未命名会话" : response.item.displayTitle)
                    .font(.title2.bold())
                SectionCard(title: "使用概览") {
                    KeyValueRow(key: "项目", value: attributionText(response.item.project))
                    KeyValueRow(key: "模型", value: attributionText(response.item.model))
                    KeyValueRow(key: "状态", value: ProductCopy.status(response.item.activity))
                    TokenBreakdownView(tokens: TokenBreakdownPresentation(response.item.totals))
                    KeyValueRow(key: "API 折算成本", value: costText(response.item.totals.estimatedUsdMicros))
                }
                SectionCard(title: "每日趋势") {
                    DailyTokenTrendView(points: response.daily)
                }
                SectionCard(title: "活动时间线") {
                    Text("只展示用量信息，不读取对话内容。")
                        .font(.caption)
                        .foregroundStyle(.secondary)
                    if response.turns.isEmpty {
                        Text("当前页没有活动记录。")
                            .foregroundStyle(.secondary)
                    } else {
                        ForEach(response.turns, id: \.timelineKey) { turn in
                            VStack(alignment: .leading, spacing: 4) {
                                HStack {
                                    Text(timestampText(turn.observedAtMs))
                                    Spacer()
                                    StatusPill(text: turn.state)
                                }
                                Text("模型：\(attributionText(turn.model))")
                                    .font(.caption)
                                    .foregroundStyle(.secondary)
                                TokenBreakdownView(
                                    tokens: TokenBreakdownPresentation(turn.totals),
                                    style: .compact
                                )
                                if turn.hasUnpricedReason {
                                    Text("部分用量暂未折算")
                                        .font(.caption)
                                        .foregroundStyle(.orange)
                                }
                                Divider()
                            }
                        }
                    }
                    if response.turnPage.hasMore_p && response.turnPage.hasNextCursor {
                        Button(isLoading ? "正在加载…" : "加载更多记录") { loadMore() }
                            .disabled(isLoading)
                            .accessibilityIdentifier("session-detail.load-more")
                    }
                }
            }
            .padding(18)
        }
        .accessibilityIdentifier("session.detail")
    }
}

struct ProjectsView: View {
    @ObservedObject var model: AppModel
    @State private var showsFilters = false

    var body: some View {
        VStack(spacing: 0) {
            pageHeader
            if showsFilters { filters }
            Divider()
            FeatureStateView(
                state: model.projectsState, emptyTitle: "当前条件下没有项目", emptySystemImage: "folder"
            ) {
                projectSplit($0)
            }
        }
        .accessibilityIdentifier("page.projects")
    }

    private var pageHeader: some View {
        HStack(alignment: .center, spacing: 16) {
            VStack(alignment: .leading, spacing: 3) {
                Text("项目").font(.title.bold())
                Text("比较不同项目的会话、Token 和折算成本")
                    .font(.subheadline)
                    .foregroundStyle(.secondary)
            }
            Spacer()
            Button {
                withAnimation(.snappy) { showsFilters.toggle() }
            } label: {
                Label(showsFilters ? "收起筛选" : "筛选", systemImage: "line.3.horizontal.decrease.circle")
            }
            .accessibilityIdentifier("projects.filters.toggle")
        }
        .padding(.horizontal, 18)
        .padding(.vertical, 14)
    }

    private var filters: some View {
        HStack(spacing: 10) {
            Picker("范围", selection: $model.projectOptions.range) {
                ForEach(DateRangePreset.allCases.filter {
                    $0 != .all && ($0 != .quotaWeek || model.projectOptions.exactRange != nil)
                }) { Text($0.title).tag($0) }
            }
            .frame(width: 120)
            .onChange(of: model.projectOptions.range) { oldValue, newValue in
                if oldValue != newValue { model.projectOptions.exactRange = nil }
            }
            TextField("项目名称（精确匹配）", text: $model.projectOptions.projectID)
                .textFieldStyle(.roundedBorder)
            Picker("项目识别准确度", selection: $model.projectOptions.confidence) {
                Text("全部准确度").tag("all")
                Text("高").tag("high")
                Text("中").tag("medium")
                Text("低").tag("low")
                Text("未知").tag("unknown")
            }
            .frame(width: 145)
            Picker("排序", selection: $model.projectOptions.sortField) {
                Text("最近活动").tag("lastActivityAt")
                Text("Token").tag("totalTokens")
                Text("API 折算成本").tag("estimatedCost")
                Text("名称").tag("displayName")
            }
            .frame(width: 130)
            Button("应用") { model.projectFiltersChanged() }
                .keyboardShortcut(.return, modifiers: [])
        }
        .padding(12)
        .accessibilityIdentifier("projects.filters")
    }

    private func projectSplit(_ response: Codexpulse_Core_V1_ProjectListResponse) -> some View {
        HSplitView {
            VStack(spacing: 0) {
                HStack {
                    Text("已加载 \(response.items.count) 个")
                    Spacer()
                    Text("匹配 \(numericText(response.matchedCount))")
                }
                .font(.caption)
                .foregroundStyle(.secondary)
                .padding(10)
                List(response.items, id: \.dimensionKey, selection: projectSelection) { item in
                    VStack(alignment: .leading, spacing: 5) {
                        Text(attributionText(item.project)).font(.headline).lineLimit(1)
                        HStack {
                            Text("\(numericText(item.sessionCount)) 个会话")
                            Spacer()
                        }
                        .font(.caption)
                        .foregroundStyle(.secondary)
                        TokenBreakdownView(
                            tokens: TokenBreakdownPresentation(item.totals),
                            style: .compact
                        )
                    }
                    .padding(.vertical, 4)
                    .tag(item.dimensionKey)
                    .accessibilityIdentifier("project.\(item.dimensionKey)")
                }
                if pageHasMore(response.meta) {
                    Button(model.projectsState.isLoading ? "正在加载…" : "加载更多") {
                        model.loadProjects(reset: false)
                    }
                    .disabled(model.projectsState.isLoading)
                    .padding(8)
                    .accessibilityIdentifier("projects.load-more")
                }
            }
            .frame(minWidth: 320, idealWidth: 390)
            projectDetail
                .frame(minWidth: 340, idealWidth: 480)
        }
    }

    private var projectSelection: Binding<String?> {
        Binding(get: { model.selectedProjectKey }, set: { model.selectProject($0) })
    }

    private var projectDetail: some View {
        FeatureStateView(
            state: model.projectDetailState,
            emptyTitle: model.selectedProjectKey == nil ? "选择一个项目查看详情" : "项目没有可显示的详情",
            emptySystemImage: "sidebar.right"
        ) { response in
            ProjectDetailView(
                response: response,
                isLoading: model.projectDetailState.isLoading,
                loadMore: model.loadMoreProjectDetail
            )
        }
    }
}

private struct ProjectDetailView: View {
    let response: Codexpulse_Core_V1_ProjectDetailResponse
    let isLoading: Bool
    let loadMore: () -> Void

    var body: some View {
        ScrollView {
            VStack(alignment: .leading, spacing: 14) {
                Text(attributionText(response.item.project)).font(.title2.bold())
                SectionCard(title: "项目概览") {
                    KeyValueRow(
                        key: "项目识别准确度",
                        value: ProductCopy.confidence(response.item.project.confidence)
                    )
                    KeyValueRow(key: "会话", value: numericText(response.item.sessionCount))
                    TokenBreakdownView(tokens: TokenBreakdownPresentation(response.item.totals))
                    KeyValueRow(key: "API 折算成本", value: costText(response.item.totals.estimatedUsdMicros))
                }
                SectionCard(title: "每日趋势") {
                    DailyTokenTrendView(points: response.daily)
                }
                SectionCard(title: "模型") {
                    ForEach(response.models, id: \.dimensionKey) { item in
                        VStack(alignment: .leading, spacing: 4) {
                            Text(attributionText(item.model))
                            TokenBreakdownView(
                                tokens: TokenBreakdownPresentation(item.totals),
                                style: .compact
                            )
                        }
                    }
                    if response.models.isEmpty { Text("没有模型统计。").foregroundStyle(.secondary) }
                }
                SectionCard(title: "会话") {
                    ForEach(response.sessions, id: \.sessionID) { item in
                        VStack(alignment: .leading) {
                            Text(item.displayTitle.isEmpty ? "未命名会话" : item.displayTitle)
                            Text(attributionText(item.model))
                                .font(.caption)
                                .foregroundStyle(.secondary)
                            TokenBreakdownView(
                                tokens: TokenBreakdownPresentation(item.totals),
                                style: .compact
                            )
                            Divider()
                        }
                    }
                    if response.sessions.isEmpty { Text("没有会话。").foregroundStyle(.secondary) }
                    if response.sessionPage.hasMore_p || response.modelPage.hasMore_p {
                        Button(isLoading ? "正在加载…" : "加载更多详情") { loadMore() }
                            .disabled(isLoading)
                            .accessibilityIdentifier("project-detail.load-more")
                    }
                }
            }
            .padding(18)
        }
        .accessibilityIdentifier("project.detail")
    }
}

private struct DailyTokenTrendPoint: Identifiable {
    let date: Date
    let totals: Codexpulse_Core_V1_UsageTotals

    var id: Int64 { Int64(date.timeIntervalSince1970 * 1_000) }
}

private struct DailyTokenTrendView: View {
    private let points: [DailyTokenTrendPoint]
    @State private var selectedDate: Date?

    init(points: [Codexpulse_Core_V1_TrendPoint]) {
        let mapped: [DailyTokenTrendPoint] = points.compactMap { point in
            guard point.startAtMs.hasValue, point.totals.totalTokens.hasValue else { return nil }
            return DailyTokenTrendPoint(
                date: Date(
                    timeIntervalSince1970: TimeInterval(point.startAtMs.value) / 1_000
                ),
                totals: point.totals
            )
        }
        self.points = mapped
        _selectedDate = State(initialValue: mapped.last?.date)
    }

    init(points: [Codexpulse_Core_V1_ProjectDailyPoint]) {
        let mapped: [DailyTokenTrendPoint] = points.compactMap { point in
            guard point.bucketStartAtMs.hasValue, point.totals.totalTokens.hasValue else { return nil }
            return DailyTokenTrendPoint(
                date: Date(
                    timeIntervalSince1970: TimeInterval(point.bucketStartAtMs.value) / 1_000
                ),
                totals: point.totals
            )
        }
        self.points = mapped
        _selectedDate = State(initialValue: mapped.last?.date)
    }

    var body: some View {
        if points.isEmpty {
            Text("当前范围没有趋势数据。")
                .foregroundStyle(.secondary)
        } else {
            Chart {
                ForEach(points) { point in
                    LineMark(
                        x: .value("日期", point.date),
                        y: .value("Token", point.totals.totalTokens.value)
                    )
                    .interpolationMethod(.catmullRom)
                    PointMark(
                        x: .value("日期", point.date),
                        y: .value("Token", point.totals.totalTokens.value)
                    )
                    .symbolSize(selectedPoint?.id == point.id ? 70 : 28)
                    .accessibilityLabel(Self.detailDateFormatter.string(from: point.date))
                    .accessibilityValue(
                        "\(TokenQuantityFormatter.string(point.totals.totalTokens.value)) Token"
                    )
                }
                if let selected = selectedPoint {
                    RuleMark(x: .value("选中日期", selected.date))
                        .lineStyle(StrokeStyle(lineWidth: 1, dash: [5, 5]))
                        .foregroundStyle(.secondary)
                }
            }
            .foregroundStyle(.tint)
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
            .chartXAxis {
                AxisMarks(values: .automatic) { _ in
                    AxisValueLabel(format: .dateTime.month().day())
                }
            }
            .chartXSelection(value: $selectedDate)
            .frame(height: 180)

            if let selected = selectedPoint {
                VStack(alignment: .leading, spacing: 6) {
                    Text(Self.detailDateFormatter.string(from: selected.date))
                        .font(.subheadline.weight(.semibold))
                    TokenBreakdownView(
                        tokens: TokenBreakdownPresentation(selected.totals),
                        style: .compact
                    )
                }
                .accessibilityElement(children: .combine)
                .accessibilityIdentifier("daily-trend.selection-detail")
            }
        }
    }

    private var selectedPoint: DailyTokenTrendPoint? {
        guard let selectedDate else { return nil }
        return points.min {
            abs($0.date.timeIntervalSince1970 - selectedDate.timeIntervalSince1970)
                < abs($1.date.timeIntervalSince1970 - selectedDate.timeIntervalSince1970)
        }
    }

    private static let detailDateFormatter: DateFormatter = {
        let formatter = DateFormatter()
        formatter.locale = Locale(identifier: "zh_CN")
        formatter.calendar = Calendar(identifier: .gregorian)
        formatter.dateStyle = .long
        formatter.timeStyle = .none
        return formatter
    }()
}
