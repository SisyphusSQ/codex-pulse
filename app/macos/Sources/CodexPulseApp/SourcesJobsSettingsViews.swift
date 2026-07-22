import CodexPulseAppSupport
import CodexPulseProtocolGenerated
import SwiftUI

struct SourcesJobsView: View {
    enum Section: String, CaseIterable, Identifiable {
        case sources
        case jobs
        var id: String { rawValue }
        var title: String { self == .sources ? "数据源" : "任务" }
    }

    @ObservedObject var model: AppModel
    @State private var section: Section = .sources

    var body: some View {
        VStack(spacing: 0) {
            Picker("内容", selection: $section) {
                ForEach(Section.allCases) { Text($0.title).tag($0) }
            }
            .pickerStyle(.segmented)
            .frame(maxWidth: 320)
            .padding(12)
            Divider()
            switch section {
            case .sources: sources
            case .jobs: jobs
            }
        }
        .accessibilityIdentifier("page.sources-jobs")
    }

    private var sources: some View {
        VStack(spacing: 0) {
            HStack {
                Picker("状态", selection: sourceStateFilter) {
                    Text("全部状态").tag("all")
                    Text("当前").tag("current")
                    Text("过期").tag("stale")
                    Text("不可用").tag("unavailable")
                    Text("未知").tag("unknown")
                }
                .frame(width: 145)
                TextField("类型（精确）", text: sourceKindFilter)
                    .textFieldStyle(.roundedBorder)
                Button("应用") { model.sourceFiltersChanged() }
            }
            .padding(12)
            FeatureStateView(state: model.sourcesState, emptyTitle: "当前条件下没有数据源", emptySystemImage: "externaldrive") {
                SourceSplitView(
                    response: $0,
                    selected: Binding(get: { model.selectedSourceKey }, set: { model.selectSource($0) }),
                    detailState: model.sourceDetailState,
                    isLoading: model.sourcesState.isLoading,
                    loadMore: { model.loadSources(reset: false) }
                )
            }
        }
    }

    private var jobs: some View {
        VStack(spacing: 0) {
            HStack {
                Picker("状态", selection: jobStateFilter) {
                    Text("全部状态").tag("all")
                    Text("排队").tag("queued")
                    Text("运行中").tag("running")
                    Text("成功").tag("succeeded")
                    Text("失败").tag("failed")
                    Text("取消").tag("cancelled")
                    Text("中断").tag("interrupted")
                }
                .frame(width: 145)
                TextField("阶段（精确）", text: jobPhaseFilter)
                    .textFieldStyle(.roundedBorder)
                Button("应用") { model.jobFiltersChanged() }
            }
            .padding(12)
            FeatureStateView(state: model.jobsState, emptyTitle: "当前条件下没有任务", emptySystemImage: "tray") {
                JobSplitView(
                    response: $0,
                    selected: Binding(get: { model.selectedJobID }, set: { model.selectJob($0) }),
                    detailState: model.jobDetailState,
                    isLoading: model.jobsState.isLoading,
                    loadMore: { model.loadJobs(reset: false) }
                )
            }
        }
    }

    private var sourceStateFilter: Binding<String> {
        Binding(
            get: { model.sourceOptions.firstValues.first ?? "all" },
            set: {
                model.sourceOptions.firstField = $0 == "all" ? "" : "state"
                model.sourceOptions.firstValues = $0 == "all" ? [] : [$0]
            }
        )
    }

    private var sourceKindFilter: Binding<String> {
        Binding(
            get: { model.sourceOptions.secondValues.first ?? "" },
            set: {
                model.sourceOptions.secondField = $0.isEmpty ? "" : "kind"
                model.sourceOptions.secondValues = $0.isEmpty ? [] : [$0]
            }
        )
    }

    private var jobStateFilter: Binding<String> {
        Binding(
            get: { model.jobOptions.firstValues.first ?? "all" },
            set: {
                model.jobOptions.firstField = $0 == "all" ? "" : "state"
                model.jobOptions.firstValues = $0 == "all" ? [] : [$0]
            }
        )
    }

    private var jobPhaseFilter: Binding<String> {
        Binding(
            get: { model.jobOptions.secondValues.first ?? "" },
            set: {
                model.jobOptions.secondField = $0.isEmpty ? "" : "phase"
                model.jobOptions.secondValues = $0.isEmpty ? [] : [$0]
            }
        )
    }
}

private struct SourceSplitView: View {
    let response: Codexpulse_Core_V1_SourceListResponse
    @Binding var selected: String?
    let detailState: FeatureLoadState<Codexpulse_Core_V1_SourceDetailResponse>
    let isLoading: Bool
    let loadMore: () -> Void

    var body: some View {
        HSplitView {
            VStack(spacing: 0) {
                HStack {
                    Text("已加载 \(response.items.count) 个")
                    Spacer()
                    Text("需关注 \(numericText(response.summary.attention))")
                }
                .font(.caption)
                .foregroundStyle(.secondary)
                .padding(10)
                List(response.items, id: \.sourceKey, selection: $selected) { item in
                    VStack(alignment: .leading, spacing: 5) {
                        HStack {
                            Text(item.sourceKey).font(.headline).lineLimit(1)
                            Spacer()
                            StatusPill(text: item.state)
                        }
                        Text("\(item.kind) · \(bytesText(item.parsedBytes)) 已解析")
                            .font(.caption).foregroundStyle(.secondary)
                        if item.hasFailureCode {
                            Text(item.failureCode).font(.caption2).foregroundStyle(.orange)
                        }
                    }
                    .tag(item.sourceKey)
                    .accessibilityIdentifier("source.\(item.sourceKey)")
                }
                if pageHasMore(response.meta) {
                    Button(isLoading ? "正在加载…" : "加载更多") { loadMore() }.padding(8)
                        .disabled(isLoading)
                        .accessibilityIdentifier("sources.load-more")
                }
            }
            .frame(minWidth: 330, idealWidth: 410)
            FeatureStateView(state: detailState, emptyTitle: "选择一个数据源查看详情", emptySystemImage: "sidebar.right") { detail in
                SourceDetailView(item: detail.item)
            }
            .frame(minWidth: 340, idealWidth: 470)
        }
    }
}

private struct SourceDetailView: View {
    let item: Codexpulse_Core_V1_SourceItem

    var body: some View {
        ScrollView {
            VStack(alignment: .leading, spacing: 14) {
                HStack {
                    Text(item.sourceKey).font(.title2.bold())
                    Spacer()
                    StatusPill(text: item.state)
                }
                SectionCard(title: "数据源事实") {
                    KeyValueRow(key: "类型", value: item.kind)
                    KeyValueRow(key: "Provider", value: item.hasProvider ? item.provider : "--")
                    KeyValueRow(key: "大小", value: bytesText(item.sizeBytes))
                    KeyValueRow(key: "已解析", value: bytesText(item.parsedBytes))
                    KeyValueRow(key: "最近尝试", value: timestampText(item.lastAttemptAtMs))
                    KeyValueRow(key: "最近成功", value: timestampText(item.lastSuccessAtMs))
                    KeyValueRow(key: "连续失败", value: numericText(item.consecutiveFailures))
                    if item.hasFailureCode { KeyValueRow(key: "失败代码", value: item.failureCode) }
                }
                RecoveryEntry(action: item.recoveryAction)
            }
            .padding(18)
        }
        .accessibilityIdentifier("source.detail")
    }
}

private struct JobSplitView: View {
    let response: Codexpulse_Core_V1_JobListResponse
    @Binding var selected: String?
    let detailState: FeatureLoadState<Codexpulse_Core_V1_JobDetailResponse>
    let isLoading: Bool
    let loadMore: () -> Void

    var body: some View {
        HSplitView {
            VStack(spacing: 0) {
                HStack {
                    Text("已加载 \(response.items.count) 个")
                    Spacer()
                    Text("运行中 \(numericText(response.summary.running))")
                }
                .font(.caption).foregroundStyle(.secondary).padding(10)
                List(response.items, id: \.jobID, selection: $selected) { item in
                    VStack(alignment: .leading, spacing: 5) {
                        HStack {
                            Text(item.jobType).font(.headline).lineLimit(1)
                            Spacer()
                            StatusPill(text: item.state)
                        }
                        Text("阶段：\(item.phase.isEmpty ? "unknown" : item.phase)")
                            .font(.caption).foregroundStyle(.secondary)
                        Text("更新：\(timestampText(item.updatedAtMs))")
                            .font(.caption2).foregroundStyle(.secondary)
                    }
                    .tag(item.jobID)
                    .accessibilityIdentifier("job.\(item.jobID)")
                }
                if pageHasMore(response.meta) {
                    Button(isLoading ? "正在加载…" : "加载更多") { loadMore() }.padding(8)
                        .disabled(isLoading)
                        .accessibilityIdentifier("jobs.load-more")
                }
            }
            .frame(minWidth: 330, idealWidth: 410)
            FeatureStateView(state: detailState, emptyTitle: "选择一个任务查看详情", emptySystemImage: "sidebar.right") { detail in
                JobDetailView(item: detail.item)
            }
            .frame(minWidth: 340, idealWidth: 470)
        }
    }
}

private struct JobDetailView: View {
    let item: Codexpulse_Core_V1_JobItem

    var body: some View {
        ScrollView {
            VStack(alignment: .leading, spacing: 14) {
                HStack {
                    Text(item.jobType).font(.title2.bold())
                    Spacer()
                    StatusPill(text: item.state)
                }
                SectionCard(title: "任务事实") {
                    KeyValueRow(key: "任务 ID", value: item.jobID)
                    KeyValueRow(key: "阶段", value: item.phase.isEmpty ? "unknown" : item.phase)
                    KeyValueRow(key: "请求来源", value: item.requestedBy)
                    KeyValueRow(key: "数据源", value: item.hasSourceKey ? item.sourceKey : "--")
                    KeyValueRow(key: "进度", value: "\(numericText(item.progress.current)) / \(numericText(item.progress.total))")
                    KeyValueRow(key: "失败次数", value: numericText(item.failureCount))
                    KeyValueRow(key: "创建时间", value: timestampText(item.createdAtMs))
                    KeyValueRow(key: "更新时间", value: timestampText(item.updatedAtMs))
                }
                RecoveryEntry(action: item.recoveryAction)
            }
            .padding(18)
        }
        .accessibilityIdentifier("job.detail")
    }
}

private struct RecoveryEntry: View {
    let action: Codexpulse_Core_V1_RecoveryAction

    var body: some View {
        SectionCard(title: "恢复入口") {
            if action.kind.isEmpty || action.kind == "none" {
                Text("Helper 未返回恢复建议。")
                    .foregroundStyle(.secondary)
            } else {
                KeyValueRow(key: "建议动作", value: action.kind)
                if action.hasCommandKey { KeyValueRow(key: "命令 key", value: action.commandKey) }
                Text("这是 provider 的诊断 command key；当前 CoreService 没有对应的授权执行 RPC，因此这里只读展示，不会映射成调度控制或高风险修复。")
                    .font(.caption)
                    .foregroundStyle(.orange)
            }
        }
    }
}

struct RuntimeActionControl: View {
    let action: RuntimeControlAction
    let state: ActionState
    let execute: (RuntimeControlAction) -> Void
    var showsStatus = true
    @State private var pendingAction: RuntimeControlAction?

    var body: some View {
        Button(action.title) { pendingAction = action }
                .disabled(isRunning)
                .accessibilityIdentifier("runtime-action.\(action.rawValue)")
                .confirmationDialog(
                    "确认\(action.title)？",
                    isPresented: Binding(
                        get: { pendingAction != nil },
                        set: { if !$0 { pendingAction = nil } }
                    ),
                    titleVisibility: .visible
                ) {
                    Button(action.title, role: action == .pauseAll ? .destructive : nil) {
                        pendingAction = nil
                        execute(action)
                    }
                    Button("取消", role: .cancel) { pendingAction = nil }
                } message: {
                    Text("该操作会改变 Helper 调度状态；完成后界面将读取最新事实。")
                }
        if showsStatus { actionStatus }
    }

    private var isRunning: Bool {
        if case .running = state { return true }
        return false
    }

    @ViewBuilder
    private var actionStatus: some View {
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

struct SettingsView: View {
    @ObservedObject var model: AppModel

    var body: some View {
        FeatureStateView(state: model.settingsState, emptyTitle: "设置不可用", emptySystemImage: "gearshape") { response in
            settingsContent(response)
        }
        .accessibilityIdentifier("page.settings")
    }

    private func settingsContent(_ response: Codexpulse_Core_V1_SettingsResponse) -> some View {
        ScrollView {
            VStack(alignment: .leading, spacing: 16) {
                HStack {
                    VStack(alignment: .leading) {
                        Text("当前配置").font(.title2.bold())
                        Text("Revision：\(response.snapshot.revision)")
                            .font(.caption).foregroundStyle(.secondary).textSelection(.enabled)
                    }
                    Spacer()
                    saveStatus
                    Button("保存") { model.saveSettings() }
                        .buttonStyle(.borderedProminent)
                        .disabled(!canSave(response))
                        .keyboardShortcut("s", modifiers: .command)
                        .accessibilityIdentifier("settings.save")
                }
                if model.settingsDraft != nil {
                    onlineSection(response)
                    refreshSection(response)
                    updatesSection(response)
                    uiSection(response)
                }
                SectionCard(title: "Home 与修复") {
                    KeyValueRow(key: "Home 配置状态", value: response.snapshot.home.configured ? "已配置" : "未配置")
                    KeyValueRow(key: "切换状态", value: response.snapshot.home.switchStatus)
                    Button("切换 Codex Home…") {}
                        .disabled(true)
                        .help("高风险 Home 切换未获本阶段执行授权")
                    Button("分析并修复会话索引…") {}
                        .disabled(true)
                        .help("正式 repair/migration 需要单独授权与确认")
                    Text("高风险 Home 切换、repair、正式 migration 与 release 均保持不可执行，不以 mock 结果冒充完成。")
                        .font(.caption).foregroundStyle(.secondary)
                }
            }
            .padding(20)
        }
    }

    private func onlineSection(_ response: Codexpulse_Core_V1_SettingsResponse) -> some View {
        SectionCard(title: "在线事实") {
            Toggle("启用额度采集", isOn: draftBinding(\.quotaEnabled))
                .disabled(!editable("online.quotaEnabled", response) || settingsAreBusy)
            Toggle("启用重置额度采集", isOn: draftBinding(\.resetCreditsEnabled))
                .disabled(!editable("online.resetCreditsEnabled", response) || settingsAreBusy)
        }
    }

    private func refreshSection(_ response: Codexpulse_Core_V1_SettingsResponse) -> some View {
        SectionCard(title: "刷新节奏") {
            numberField("额度刷新（秒）", keyPath: \.quotaIntervalSeconds, key: "refresh.quotaIntervalSeconds", response: response)
            numberField("重置额度刷新（秒）", keyPath: \.resetCreditsIntervalSeconds, key: "refresh.resetCreditsIntervalSeconds", response: response)
            numberField("对账周期（秒）", keyPath: \.reconcileIntervalSeconds, key: "refresh.reconcileIntervalSeconds", response: response)
            numberField("JSONL debounce（毫秒）", keyPath: \.jsonlDebounceMilliseconds, key: "refresh.jsonlDebounceMilliseconds", response: response)
        }
    }

    private func updatesSection(_ response: Codexpulse_Core_V1_SettingsResponse) -> some View {
        SectionCard(title: "更新检查") {
            Toggle("自动检查更新", isOn: draftBinding(\.autoCheckEnabled))
                .disabled(!editable("updates.autoCheckEnabled", response) || settingsAreBusy)
            numberField("检查周期（秒）", keyPath: \.checkIntervalSeconds, key: "updates.checkIntervalSeconds", response: response)
            KeyValueRow(key: "下载策略", value: response.snapshot.updates.autoDownloadEnabled ? "自动下载" : "仅检查")
            KeyValueRow(key: "渠道", value: response.snapshot.updates.channel)
        }
    }

    private func uiSection(_ response: Codexpulse_Core_V1_SettingsResponse) -> some View {
        SectionCard(title: "原生界面") {
            Picker("启动行为", selection: draftBinding(\.launchBehavior)) {
                ForEach(options("ui.launchBehavior", response, fallback: ["main_window", "tray"]), id: \.self) { Text($0).tag($0) }
            }
            .disabled(!editable("ui.launchBehavior", response) || settingsAreBusy)
            Picker("默认概览范围", selection: draftBinding(\.overviewRange)) {
                ForEach(options("ui.overviewRange", response, fallback: ["today", "7d", "30d"]), id: \.self) { Text($0).tag($0) }
            }
            .disabled(!editable("ui.overviewRange", response) || settingsAreBusy)
            KeyValueRow(key: "Locale", value: response.snapshot.ui.locale)
        }
    }

    private func numberField(
        _ title: String,
        keyPath: WritableKeyPath<SettingsDraft, Int64>,
        key: String,
        response: Codexpulse_Core_V1_SettingsResponse
    ) -> some View {
        HStack {
            Text(title)
            Spacer()
            TextField(title, value: draftBinding(keyPath), format: .number)
                .frame(width: 150)
                .multilineTextAlignment(.trailing)
                .disabled(!editable(key, response) || settingsAreBusy)
        }
    }

    @ViewBuilder
    private var saveStatus: some View {
        switch model.settingsSaveState {
        case .idle: EmptyView()
        case .saving: ProgressView().controlSize(.small).accessibilityLabel("正在保存设置")
        case .applied: StatusPill(text: "已保存并回读")
        case .reconcileRequired: StatusPill(text: "reconcile_required")
        case .conflict: StatusPill(text: "revision conflict")
        case .unavailable(let notice): StatusPill(text: "保存失败：\(notice.code)")
        }
    }

    private func canSave(_ response: Codexpulse_Core_V1_SettingsResponse) -> Bool {
        guard model.canRefreshOrRestart, !model.requiresCoreRestart, !model.settingsState.isLoading else { return false }
        guard let draft = model.settingsDraft, draft != SettingsDraft(response) else { return false }
        if case .saving = model.settingsSaveState { return false }
        return true
    }

    private var settingsAreBusy: Bool {
        if model.settingsState.isLoading { return true }
        if case .saving = model.settingsSaveState { return true }
        return false
    }

    private func editable(_ key: String, _ response: Codexpulse_Core_V1_SettingsResponse) -> Bool {
        response.editableFields.first(where: { $0.key == key })?.editable == true
    }

    private func options(_ key: String, _ response: Codexpulse_Core_V1_SettingsResponse, fallback: [String]) -> [String] {
        let values = response.editableFields.first(where: { $0.key == key })?.options ?? []
        return values.isEmpty ? fallback : values
    }

    private func draftBinding<Value>(_ keyPath: WritableKeyPath<SettingsDraft, Value>) -> Binding<Value> {
        Binding(
            get: { model.settingsDraft![keyPath: keyPath] },
            set: { next in
                guard var draft = model.settingsDraft else { return }
                draft[keyPath: keyPath] = next
                model.settingsDraft = draft
            }
        )
    }
}
