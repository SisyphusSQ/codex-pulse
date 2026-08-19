import CodexPulseAppSupport
import CodexPulseProtocolGenerated
import SwiftUI

struct SourcesJobsView: View {
    enum Section: String, CaseIterable, Identifiable {
        case sources
        case jobs
        var id: String { rawValue }
        var title: String {
            localizedCopy(self == .sources ? "数据源" : "任务")
        }
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
            HStack(spacing: 8) {
                Text("状态")
                    .foregroundStyle(.secondary)
                Picker("状态", selection: sourceStateFilter) {
                    Text("全部状态").tag("all")
                    Text("当前").tag("current")
                    Text("过期").tag("stale")
                    Text("不可用").tag("unavailable")
                    Text("未知").tag("unknown")
					Text("可用").tag("available")
					Text("部分可用").tag("partial")
					Text("未配置").tag("not_configured")
                }
                .labelsHidden()
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
            HStack(spacing: 8) {
                Text("状态")
                    .foregroundStyle(.secondary)
                Picker("状态", selection: jobStateFilter) {
                    Text("全部状态").tag("all")
                    Text("排队").tag("queued")
                    Text("运行中").tag("running")
                    Text("成功").tag("succeeded")
                    Text("失败").tag("failed")
                    Text("取消").tag("cancelled")
                    Text("中断").tag("interrupted")
                }
                .labelsHidden()
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
				List(selection: $selected) {
					ForEach(providerGroups(response.items), id: \.provider) { group in
						Section(group.provider.title) {
							ForEach(group.items, id: \.sourceKey) { item in
					VStack(alignment: .leading, spacing: 5) {
						HStack {
							Text(item.hasSourceType ? item.sourceType : ProductCopy.sourceName(item.kind)).font(.headline).lineLimit(1)
							Spacer()
							StatusPill(text: item.state)
						}
					let detailText = item.hasProvider
						? "记录 \(numericText(item.rowCount)) · 覆盖度 \(item.hasCoverageState ? item.coverageState : "unknown")"
						: "已整理 \(bytesText(item.parsedBytes))"
					Text(detailText)
                            .font(.caption).foregroundStyle(.secondary)
                        if item.hasFailureCode {
                            Text("最近一次更新未完成").font(.caption2).foregroundStyle(.orange)
                        }
                    }
                    .tag(item.sourceKey)
                    .accessibilityIdentifier("source.\(item.sourceKey)")
							}
						}
					}
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

	private func providerGroups(_ items: [Codexpulse_Core_V1_SourceItem]) -> [(provider: AgentProvider, items: [Codexpulse_Core_V1_SourceItem])] {
		AgentProvider.allCases.compactMap { provider in
			let matches = items.filter {
				let raw = $0.hasProvider ? $0.provider : AgentProvider.codex.rawValue
				return raw == provider.rawValue
			}
			return matches.isEmpty ? nil : (provider, matches)
		}
	}
}

private struct SourceDetailView: View {
    let item: Codexpulse_Core_V1_SourceItem

    var body: some View {
        ScrollView {
            VStack(alignment: .leading, spacing: 14) {
                HStack {
					Text(item.hasSourceType ? item.sourceType : ProductCopy.sourceName(item.kind)).font(.title2.bold())
                    Spacer()
                    StatusPill(text: item.state)
                }
                SectionCard(title: "数据概览") {
					KeyValueRow(key: "客户端", value: item.hasProvider ? item.provider.capitalized : "Codex")
					if item.hasSourceType { KeyValueRow(key: "内部来源", value: item.sourceType) }
					if item.hasCoverageState { KeyValueRow(key: "覆盖度", value: item.coverageState) }
					if item.hasCheckpointKind { KeyValueRow(key: "Checkpoint", value: item.checkpointKind) }
					if item.hasProvider {
						KeyValueRow(key: "记录数", value: numericText(item.rowCount))
						KeyValueRow(key: "Schema 版本", value: numericText(item.schemaVersion))
					}
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
				List(selection: $selected) {
					Section("Codex") {
						ForEach(response.items, id: \.jobID) { item in
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
					}
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
                    KeyValueRow(
                        key: "数据源",
                        value: item.hasSourceKey ? localizedCopy("本机数据") : "--"
                    )
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
        let localization = AppLocalizationRegistry.shared.current
        Button(action.title) { pendingAction = action }
            .disabled(isRunning)
            .accessibilityIdentifier("runtime-action.\(action.rawValue)")
            .confirmationDialog(
                localization.format("确认%@？", action.title),
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
    @ObservedObject var loginItemSettings: LoginItemSettingsModel

    var body: some View {
        FeatureStateView(state: model.settingsState, emptyTitle: "设置不可用", emptySystemImage: "gearshape") { response in
            settingsContent(response)
        }
        .onAppear { loginItemSettings.refreshStatus() }
        .accessibilityIdentifier("page.settings")
    }

    private func settingsContent(_ response: Codexpulse_Core_V1_SettingsResponse) -> some View {
        VStack(spacing: 0) {
            HStack {
                VStack(alignment: .leading, spacing: 3) {
                    Text(AppFeature.settings.title(localization: model.localization))
                        .font(.largeTitle.bold())
                    Text("管理数据更新、启动方式、默认页面和版本检查")
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
                apiCredentialsSection
                if model.settingsDraft != nil {
                    onlineSection(response)
                    refreshSection(response)
                    updatesSection(response)
                    uiSection(response)
                }
                loginItemSection
                Section("本机数据") {
                    LabeledContent(
                        "配置状态",
                        value: localizedCopy(
                            response.snapshot.home.configured ? "已配置" : "默认 Codex Home 不可用"
                        )
                    )
                    LabeledContent("当前状态", value: ProductCopy.status(response.snapshot.home.switchStatus))
                    Text("首次启动会自动使用默认 Codex Home，无需手动确认。")
                        .font(.caption)
                        .foregroundStyle(.secondary)
                    Text("Codex Pulse 只读取本机 Codex 数据。")
                        .font(.caption)
                        .foregroundStyle(.secondary)
                }
            }
            .id(model.localization.preference.rawValue)
            .formStyle(.grouped)
        }
    }

    private var apiCredentialsSection: some View {
        Section(localizedCopy("API 与订阅")) {
            apiCredentialRow(
                title: "DeepSeek API key",
                service: .deepSeek,
                configured: model.apiCredentialStatus?.deepSeekConfigured == true,
                draft: $model.deepSeekAPIKeyDraft
            )
            apiCredentialRow(
                title: "OpenCode Go key",
                service: .openCodeGo,
                configured: model.apiCredentialStatus?.openCodeGoConfigured == true,
                draft: $model.openCodeGoAPIKeyDraft
            )
            Text(localizedCopy("密钥保存在本机私有凭据库中。保存或删除后会重新连接本地数据，以加载新配置。"))
                .font(.caption)
                .foregroundStyle(.secondary)
            apiCredentialActionStatus
        }
    }

    private func apiCredentialRow(
        title: String,
        service: APISubscriptionCredentialService,
        configured: Bool,
        draft: Binding<String>
    ) -> some View {
        VStack(alignment: .leading, spacing: 8) {
            HStack {
                Text(title)
                Spacer()
                Text(localizedCopy(configured ? "已配置" : "未配置"))
                    .font(.caption)
                    .foregroundStyle(configured ? .green : .secondary)
            }
            HStack {
                SecureField(localizedCopy("输入新 key"), text: draft)
                    .textContentType(.password)
                Button(localizedCopy("保存")) { model.saveAPICredential(service) }
                    .disabled(
                        apiCredentialActionIsRunning
                            || draft.wrappedValue.trimmingCharacters(in: .whitespacesAndNewlines).isEmpty
                    )
                Button(localizedCopy("删除"), role: .destructive) { model.deleteAPICredential(service) }
                    .disabled(apiCredentialActionIsRunning || !configured)
            }
        }
    }

    @ViewBuilder
    private var apiCredentialActionStatus: some View {
        switch model.apiCredentialActionState {
        case .idle:
            EmptyView()
        case .running:
            HStack { ProgressView().controlSize(.small); Text(localizedCopy("正在更新安全配置…")) }
                .font(.caption)
        case .succeeded(let result):
            Label(localizedCopy(result == "deleted" ? "密钥已删除" : "密钥已保存"), systemImage: "checkmark.circle")
                .font(.caption)
                .foregroundStyle(.green)
        case .unavailable:
            Label(localizedCopy("密钥更新失败"), systemImage: "exclamationmark.triangle")
                .font(.caption)
                .foregroundStyle(.orange)
        }
    }

    private var apiCredentialActionIsRunning: Bool {
        if case .running = model.apiCredentialActionState { return true }
        return false
    }

    private func onlineSection(_ response: Codexpulse_Core_V1_SettingsResponse) -> some View {
        Section("数据更新") {
            Toggle("启用额度采集", isOn: draftBinding(\.quotaEnabled))
                .disabled(!editable("online.quotaEnabled", response) || settingsAreBusy)
            Toggle("启用重置额度采集", isOn: draftBinding(\.resetCreditsEnabled))
                .disabled(!editable("online.resetCreditsEnabled", response) || settingsAreBusy)
            Toggle("启用 Grok 额度采集", isOn: draftBinding(\.grokQuotaEnabled))
                .disabled(!editable("online.grokQuotaEnabled", response) || settingsAreBusy)
            Toggle("自动续期 Grok 登录凭据", isOn: draftBinding(\.grokAutoRefreshEnabled))
                .disabled(!editable("online.grokAutoRefreshEnabled", response) || settingsAreBusy)
            Text("临近到期时安全更新本机 ~/.grok/auth.json；关闭后不会改写 Grok 凭据。")
                .font(.caption)
                .foregroundStyle(.secondary)
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
                key: "下载策略",
                value: localizedCopy(
                    response.snapshot.updates.autoDownloadEnabled ? "自动下载" : "仅检查"
                )
            )
            Picker("更新渠道", selection: draftBinding(\.updateChannel)) {
                ForEach(
                    options(
                        "updates.channel",
                        response,
                        fallback: ["stable", "prerelease"]
                    ), id: \.self
                ) {
                    Text(verbatim: ProductCopy.settingOption($0, localization: model.localization))
                        .tag($0)
                }
            }
            .disabled(!editable("updates.channel", response) || settingsAreBusy)
        }
    }

    private func uiSection(_ response: Codexpulse_Core_V1_SettingsResponse) -> some View {
        Section("界面") {
            Picker("语言", selection: localeBinding) {
                ForEach(
                    options("ui.locale", response, fallback: ["system", "zh-CN", "en-US"]),
                    id: \.self
                ) {
                    Text(verbatim: ProductCopy.settingOption($0, localization: model.localization))
                        .tag($0)
                }
            }
            .disabled(!editable("ui.locale", response) || settingsAreBusy)
            Picker("启动行为", selection: draftBinding(\.launchBehavior)) {
                ForEach(
                    options("ui.launchBehavior", response, fallback: ["main_window", "tray"]), id: \.self
                ) {
                    Text(verbatim: ProductCopy.settingOption($0, localization: model.localization))
                        .tag($0)
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
                    Text(verbatim: ProductCopy.settingOption($0, localization: model.localization))
                        .tag($0)
                }
            }
            .disabled(!editable("ui.overviewRange", response) || settingsAreBusy)
        }
    }

    private var loginItemSection: some View {
        Section("启动") {
            Toggle(
                "登录时启动 Codex Pulse",
                isOn: Binding(
                    get: { loginItemSettings.isRequested },
                    set: { loginItemSettings.setRequested($0) }
                )
            )
            .disabled(
                loginItemSettings.isChanging || loginItemSettings.status == .notFound
            )
            .accessibilityIdentifier("settings.login-at-launch")

            Text("此项会立即提交给 macOS，无需点击“保存更改”。")
                .font(.caption)
                .foregroundStyle(.secondary)

            HStack(spacing: 8) {
                loginItemStatusLabel
                if loginItemSettings.isChanging {
                    Spacer()
                    ProgressView()
                        .controlSize(.small)
                        .accessibilityLabel("正在更新登录项")
                }
            }

            Text(loginItemStatusDescription)
                .font(.caption)
                .foregroundStyle(.secondary)

            if loginItemSettings.status == .requiresApproval {
                Button("打开系统登录项设置") {
                    loginItemSettings.openSystemSettings()
                }
                .accessibilityIdentifier("settings.login-at-launch.open-system-settings")
            }

            if let failure = loginItemSettings.actionFailure {
                Label(loginItemFailureDescription(failure), systemImage: "exclamationmark.triangle")
                    .font(.caption)
                    .foregroundStyle(.orange)
                    .accessibilityIdentifier("settings.login-at-launch.error")
            }
        }
    }

    @ViewBuilder
    private var loginItemStatusLabel: some View {
        switch loginItemSettings.status {
        case .notRegistered:
            Label("未启用", systemImage: "minus.circle")
                .foregroundStyle(.secondary)
        case .enabled:
            Label("已启用", systemImage: "checkmark.circle.fill")
                .foregroundStyle(.green)
        case .requiresApproval:
            Label("等待系统批准", systemImage: "exclamationmark.triangle.fill")
                .foregroundStyle(.orange)
        case .notFound:
            Label("登录项不可用", systemImage: "questionmark.circle")
                .foregroundStyle(.orange)
        }
    }

    private var loginItemStatusDescription: String {
        switch loginItemSettings.status {
        case .notRegistered:
            localizedCopy("Codex Pulse 不会在你登录 Mac 时自动打开。")
        case .enabled:
            localizedCopy("下次登录 Mac 时，系统会自动打开 Codex Pulse。")
        case .requiresApproval:
            localizedCopy("请前往“系统设置 → 通用 → 登录项与扩展”，允许 Codex Pulse 在登录时打开。")
        case .notFound:
            localizedCopy("系统无法识别当前 Codex Pulse App。请从完整的 App 安装包打开后重试。")
        }
    }

    private func loginItemFailureDescription(_ failure: LoginItemActionFailure) -> String {
        switch failure {
        case .registrationFailed:
            localizedCopy("无法启用登录时启动。系统没有接受本次更改，请稍后重试。")
        case .unregistrationFailed:
            localizedCopy("无法关闭登录时启动。系统没有接受本次更改，请稍后重试。")
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
                localizedCopy(title),
                value: ProductCopy.interval(seconds: model.settingsDraft![keyPath: keyPath])
            )
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

    private var localeBinding: Binding<String> {
        Binding(
            get: { model.settingsDraft?.locale ?? "system" },
            set: { next in
                guard var draft = model.settingsDraft else { return }
                draft.locale = next
                model.settingsDraft = draft
                model.applyLocalePreference(next)
            }
        )
    }
}
