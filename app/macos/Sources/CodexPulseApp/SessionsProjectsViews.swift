import CodexPulseAppSupport
import CodexPulseProtocolGenerated
import SwiftUI

struct SessionsView: View {
    @ObservedObject var model: AppModel

    var body: some View {
        VStack(spacing: 0) {
            filters
            Divider()
            FeatureStateView(state: model.sessionsState, emptyTitle: "当前条件下没有会话", emptySystemImage: "text.bubble") {
                sessionResults($0)
            }
        }
        .frame(maxWidth: .infinity, maxHeight: .infinity, alignment: .topLeading)
        .accessibilityIdentifier("page.sessions")
    }

    private var filters: some View {
        VStack(alignment: .leading, spacing: 10) {
            HStack {
                Text("筛选")
                    .font(.headline)
                Spacer()
                Button("应用筛选") { model.sessionFiltersChanged() }
                    .keyboardShortcut(.return, modifiers: [])
                    .accessibilityIdentifier("sessions.filters.apply")
            }

            LazyVGrid(
                columns: [GridItem(.adaptive(minimum: 205, maximum: 280), spacing: 12, alignment: .leading)],
                alignment: .leading,
                spacing: 10
            ) {
                SessionFilterField(title: "范围") {
                    Picker("范围", selection: $model.sessionOptions.range) {
                        ForEach(DateRangePreset.allCases) { Text($0.title).tag($0) }
                    }
                    .labelsHidden()
                    .frame(maxWidth: .infinity)
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
                SessionFilterField(title: "项目 ID") {
                    TextField("精确匹配", text: $model.sessionOptions.projectID)
                        .textFieldStyle(.roundedBorder)
                        .accessibilityLabel("项目 ID（精确匹配）")
                }
                SessionFilterField(title: "模型 key") {
                    TextField("精确匹配", text: $model.sessionOptions.modelKey)
                        .textFieldStyle(.roundedBorder)
                        .accessibilityLabel("模型 key（精确匹配）")
                }
                SessionFilterField(title: "排序") {
                    Picker("排序", selection: $model.sessionOptions.sortField) {
                        Text("最近活动").tag("lastActivityAt")
                        Text("Token").tag("totalTokens")
                        Text("估算成本").tag("estimatedCost")
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
                Text("会话事实和 Turn 时间线会显示在这里。")
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
                Text("\(numericText(item.totals.totalTokens)) tokens")
            }
            .font(.caption2)
            .foregroundStyle(.secondary)
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
                SectionCard(title: "事实摘要") {
                    KeyValueRow(key: "会话 ID", value: response.item.sessionID)
                    KeyValueRow(key: "项目", value: attributionText(response.item.project))
                    KeyValueRow(key: "模型", value: attributionText(response.item.model))
                    KeyValueRow(key: "状态", value: response.item.activity.isEmpty ? "unknown" : response.item.activity)
                    KeyValueRow(key: "Token", value: numericText(response.item.totals.totalTokens))
                    KeyValueRow(key: "估算成本", value: costText(response.item.totals.estimatedUsdMicros))
                }
                SectionCard(title: "Turn 时间线（不展示对话内容）") {
                    if response.turns.isEmpty {
                        Text("当前页没有 turn。")
                            .foregroundStyle(.secondary)
                    } else {
                        ForEach(response.turns, id: \.timelineKey) { turn in
                            VStack(alignment: .leading, spacing: 4) {
                                HStack {
                                    Text(timestampText(turn.observedAtMs))
                                    Spacer()
                                    StatusPill(text: turn.state)
                                }
                                Text("模型：\(attributionText(turn.model)) · \(numericText(turn.totals.totalTokens)) tokens")
                                    .font(.caption)
                                    .foregroundStyle(.secondary)
                                if turn.hasUnpricedReason {
                                    Text("未计价：\(turn.unpricedReason)")
                                        .font(.caption)
                                        .foregroundStyle(.orange)
                                }
                                Divider()
                            }
                        }
                    }
                    if response.turnPage.hasMore_p && response.turnPage.hasNextCursor {
                        Button(isLoading ? "正在加载…" : "加载更多 Turn") { loadMore() }
                            .disabled(isLoading)
                            .accessibilityIdentifier("session-detail.load-more")
                    }
                }
                if response.hasDegradedReason {
                    StateBanner(title: "详情退化：\(response.degradedReason)", systemImage: "exclamationmark.triangle", color: .orange)
                }
            }
            .padding(18)
        }
        .accessibilityIdentifier("session.detail")
    }
}

struct ProjectsView: View {
    @ObservedObject var model: AppModel

    var body: some View {
        VStack(spacing: 0) {
            filters
            Divider()
            FeatureStateView(state: model.projectsState, emptyTitle: "当前条件下没有项目", emptySystemImage: "folder") {
                projectSplit($0)
            }
        }
        .accessibilityIdentifier("page.projects")
    }

    private var filters: some View {
        HStack(spacing: 10) {
            Picker("范围", selection: $model.projectOptions.range) {
                ForEach(DateRangePreset.allCases.filter { $0 != .all }) { Text($0.title).tag($0) }
            }
            .frame(width: 120)
            TextField("项目 ID（精确）", text: $model.projectOptions.projectID)
                .textFieldStyle(.roundedBorder)
            Picker("归属可信度", selection: $model.projectOptions.confidence) {
                Text("全部可信度").tag("all")
                Text("高").tag("high")
                Text("中").tag("medium")
                Text("低").tag("low")
                Text("未知").tag("unknown")
            }
            .frame(width: 145)
            Picker("排序", selection: $model.projectOptions.sortField) {
                Text("最近活动").tag("lastActivityAt")
                Text("Token").tag("totalTokens")
                Text("估算成本").tag("estimatedCost")
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
                            Text("\(numericText(item.totals.totalTokens)) tokens")
                        }
                        .font(.caption)
                        .foregroundStyle(.secondary)
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
                SectionCard(title: "项目统计") {
                    KeyValueRow(key: "归属 key", value: response.item.dimensionKey)
                    KeyValueRow(key: "可信度", value: response.item.project.confidence.isEmpty ? "unknown" : response.item.project.confidence)
                    KeyValueRow(key: "会话", value: numericText(response.item.sessionCount))
                    KeyValueRow(key: "Token", value: numericText(response.item.totals.totalTokens))
                    KeyValueRow(key: "估算成本", value: costText(response.item.totals.estimatedUsdMicros))
                }
                SectionCard(title: "每日趋势") {
                    if response.daily.isEmpty {
                        Text("当前范围没有趋势数据。").foregroundStyle(.secondary)
                    } else {
                        ForEach(Array(response.daily.enumerated()), id: \.offset) { _, point in
                            HStack {
                                Text(timestampText(point.bucketStartAtMs))
                                Spacer()
                                Text("\(numericText(point.totals.totalTokens)) tokens")
                                Text(costText(point.totals.estimatedUsdMicros)).frame(width: 80, alignment: .trailing)
                            }
                            .font(.caption)
                            Divider()
                        }
                    }
                }
                SectionCard(title: "模型") {
                    ForEach(response.models, id: \.dimensionKey) { item in
                        KeyValueRow(key: attributionText(item.model), value: "\(numericText(item.totals.totalTokens)) tokens")
                    }
                    if response.models.isEmpty { Text("没有模型统计。").foregroundStyle(.secondary) }
                }
                SectionCard(title: "会话") {
                    ForEach(response.sessions, id: \.sessionID) { item in
                        VStack(alignment: .leading) {
                            Text(item.displayTitle.isEmpty ? "未命名会话" : item.displayTitle)
                            Text("\(attributionText(item.model)) · \(numericText(item.totals.totalTokens)) tokens")
                                .font(.caption).foregroundStyle(.secondary)
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
