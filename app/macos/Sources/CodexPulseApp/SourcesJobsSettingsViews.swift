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
            HStack {
                VStack(alignment: .leading, spacing: 3) {
                    Text("数据源与任务").font(.title.bold())
                    Text("查看本机数据的更新状态和处理进度")
                        .font(.subheadline)
                        .foregroundStyle(.secondary)
                }
                Spacer()
            }
            .padding(.horizontal, 18)
            .padding(.top, 14)
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
            FeatureStateView(
                state: model.sourcesState, emptyTitle: "当前条件下没有数据源", emptySystemImage: "externaldrive"
            ) {
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
                            Text(ProductCopy.sourceName(item.kind)).font(.headline).lineLimit(1)
                            Spacer()
                            StatusPill(text: item.state)
                        }
                        Text("已整理 \(bytesText(item.parsedBytes))")
                            .font(.caption).foregroundStyle(.secondary)
                        if item.hasFailureCode {
                            Text("最近一次更新未完成").font(.caption2).foregroundStyle(.orange)
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
            FeatureStateView(
                state: detailState, emptyTitle: "选择一个数据源查看详情", emptySystemImage: "sidebar.right"
            ) { detail in
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
                    Text(ProductCopy.sourceName(item.kind)).font(.title2.bold())
                    Spacer()
                    StatusPill(text: item.state)
                }
                SectionCard(title: "数据概览") {
                    KeyValueRow(key: "状态", value: ProductCopy.status(item.state))
                    KeyValueRow(key: "大小", value: bytesText(item.sizeBytes))
                    KeyValueRow(key: "已整理", value: bytesText(item.parsedBytes))
                    KeyValueRow(key: "最近更新", value: timestampText(item.lastAttemptAtMs))
                    KeyValueRow(key: "最近完成", value: timestampText(item.lastSuccessAtMs))
                    KeyValueRow(key: "连续失败", value: numericText(item.consecutiveFailures))
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
                            Text(ProductCopy.jobName(item.jobType)).font(.headline).lineLimit(1)
                            Spacer()
                            StatusPill(text: item.state)
                        }
                        Text(ProductCopy.phase(item.phase))
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
            FeatureStateView(
                state: detailState, emptyTitle: "选择一个任务查看详情", emptySystemImage: "sidebar.right"
            ) { detail in
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
                    Text(ProductCopy.jobName(item.jobType)).font(.title2.bold())
                    Spacer()
                    StatusPill(text: item.state)
                }
                SectionCard(title: "任务进度") {
                    KeyValueRow(key: "状态", value: ProductCopy.status(item.state))
                    KeyValueRow(key: "当前步骤", value: ProductCopy.phase(item.phase))
                    KeyValueRow(key: "数据源", value: item.hasSourceKey ? "本机数据" : "--")
                    KeyValueRow(
                        key: "进度",
                        value: "\(numericText(item.progress.current)) / \(numericText(item.progress.total))")
                    KeyValueRow(key: "失败次数", value: numericText(item.failureCount))
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
        SectionCard(title: "处理建议") {
            if action.kind.isEmpty || action.kind == "none" {
                Text("暂无建议操作。")
                    .foregroundStyle(.secondary)
            } else {
                KeyValueRow(key: "建议操作", value: ProductCopy.recoveryAction(action.kind))
                Text("请根据建议检查本机数据后重试。")
                    .font(.caption).foregroundStyle(.secondary)
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
                Text("该操作会改变本机数据更新状态。")
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

struct SettingsView: View {
    @ObservedObject var model: AppModel

    var body: some View {
        FeatureStateView(state: model.settingsState, emptyTitle: "设置不可用", emptySystemImage: "gearshape") { response in
            settingsContent(response)
        }
        .accessibilityIdentifier("page.settings")
    }

    private func settingsContent(_ response: Codexpulse_Core_V1_SettingsResponse) -> some View {
        VStack(spacing: 0) {
            HStack {
                VStack(alignment: .leading, spacing: 3) {
                    Text("设置").font(.largeTitle.bold())
                    Text("管理数据更新、默认页面和版本检查")
                        .foregroundStyle(.secondary)
                }
                Spacer()
                saveStatus
                Button("保存更改") { model.saveSettings() }
                    .buttonStyle(.borderedProminent)
                    .disabled(!canSave(response))
                    .keyboardShortcut("s", modifiers: .command)
                    .accessibilityIdentifier("settings.save")
            }
            .padding(.horizontal, 20)
            .padding(.vertical, 16)
            Divider()
            Form {
                if model.settingsDraft != nil {
                    onlineSection(response)
                    refreshSection(response)
                    updatesSection(response)
                    uiSection(response)
                }
                Section("本机数据") {
                    LabeledContent("配置状态", value: response.snapshot.home.configured ? "已配置" : "未配置")
                    LabeledContent("当前状态", value: ProductCopy.status(response.snapshot.home.switchStatus))
                    Text("Codex Pulse 只读取本机 Codex 数据。")
                        .font(.caption)
                        .foregroundStyle(.secondary)
                }
            }
            .formStyle(.grouped)
        }
    }

    private func onlineSection(_ response: Codexpulse_Core_V1_SettingsResponse) -> some View {
        Section("数据更新") {
            Toggle("启用额度采集", isOn: draftBinding(\.quotaEnabled))
                .disabled(!editable("online.quotaEnabled", response) || settingsAreBusy)
            Toggle("启用重置额度采集", isOn: draftBinding(\.resetCreditsEnabled))
                .disabled(!editable("online.resetCreditsEnabled", response) || settingsAreBusy)
        }
    }

    private func refreshSection(_ response: Codexpulse_Core_V1_SettingsResponse) -> some View {
        Section("刷新频率") {
            intervalField(
                "额度", keyPath: \.quotaIntervalSeconds, key: "refresh.quotaIntervalSeconds",
                range: 60...1_800, step: 60, response: response)
            intervalField(
                "重置次数", keyPath: \.resetCreditsIntervalSeconds, key: "refresh.resetCreditsIntervalSeconds",
                range: 60...3_600, step: 60, response: response)
            intervalField(
                "用量校准", keyPath: \.reconcileIntervalSeconds, key: "refresh.reconcileIntervalSeconds",
                range: 60...86_400, step: 60, response: response)
        }
    }

    private func updatesSection(_ response: Codexpulse_Core_V1_SettingsResponse) -> some View {
        Section("版本更新") {
            Toggle("自动检查更新", isOn: draftBinding(\.autoCheckEnabled))
                .disabled(!editable("updates.autoCheckEnabled", response) || settingsAreBusy)
            intervalField(
                "检查频率", keyPath: \.checkIntervalSeconds, key: "updates.checkIntervalSeconds",
                range: 3_600...86_400, step: 3_600, response: response)
            KeyValueRow(
                key: "下载策略", value: response.snapshot.updates.autoDownloadEnabled ? "自动下载" : "仅检查")
            KeyValueRow(key: "更新渠道", value: ProductCopy.settingOption(response.snapshot.updates.channel))
        }
    }

    private func uiSection(_ response: Codexpulse_Core_V1_SettingsResponse) -> some View {
        Section("界面") {
            Picker("启动行为", selection: draftBinding(\.launchBehavior)) {
                ForEach(
                    options("ui.launchBehavior", response, fallback: ["main_window", "tray"]), id: \.self
                ) {
                    Text(ProductCopy.settingOption($0)).tag($0)
                }
            }
            .disabled(!editable("ui.launchBehavior", response) || settingsAreBusy)
            Picker("默认概览范围", selection: draftBinding(\.overviewRange)) {
                ForEach(
                    options(
                        "ui.overviewRange",
                        response,
                        fallback: ["quota_week", "today", "seven_days", "thirty_days"]
                    ), id: \.self
                ) {
                    Text(ProductCopy.settingOption($0)).tag($0)
                }
            }
            .disabled(!editable("ui.overviewRange", response) || settingsAreBusy)
        }
    }

    private func intervalField(
        _ title: String,
        keyPath: WritableKeyPath<SettingsDraft, Int64>,
        key: String,
        range: ClosedRange<Int64>,
        step: Int,
        response: Codexpulse_Core_V1_SettingsResponse
    ) -> some View {
        let field = response.editableFields.first(where: { $0.key == key })
        let minimum = field.flatMap { $0.hasMinimum ? $0.minimum : nil } ?? range.lowerBound
        let maximum = field.flatMap { $0.hasMaximum ? $0.maximum : nil } ?? range.upperBound
        return Stepper(
            value: draftBinding(keyPath),
            in: minimum...maximum,
            step: step
        ) {
            LabeledContent(
                title, value: ProductCopy.interval(seconds: model.settingsDraft![keyPath: keyPath]))
        }
        .disabled(!editable(key, response) || settingsAreBusy)
    }

    @ViewBuilder
    private var saveStatus: some View {
        switch model.settingsSaveState {
        case .idle: EmptyView()
        case .saving: ProgressView().controlSize(.small).accessibilityLabel("正在保存设置")
        case .applied: Label("已保存", systemImage: "checkmark.circle").foregroundStyle(.green)
        case .reconcileRequired:
            Label("已保存，正在更新数据", systemImage: "arrow.clockwise").foregroundStyle(.secondary)
        case .conflict:
            Label("设置已更新，请重新加载", systemImage: "exclamationmark.triangle").foregroundStyle(.orange)
        case .unavailable:
            Label("保存失败，请重试", systemImage: "exclamationmark.triangle").foregroundStyle(.orange)
        }
    }

    private func canSave(_ response: Codexpulse_Core_V1_SettingsResponse) -> Bool {
        guard model.canRefreshOrRestart, !model.requiresCoreRestart, !model.settingsState.isLoading
        else { return false }
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

    private func options(
        _ key: String, _ response: Codexpulse_Core_V1_SettingsResponse, fallback: [String]
    ) -> [String] {
        let values = response.editableFields.first(where: { $0.key == key })?.options ?? []
        return values.isEmpty ? fallback : values
    }

    private func draftBinding<Value>(_ keyPath: WritableKeyPath<SettingsDraft, Value>) -> Binding<
        Value
    > {
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
