import CodexPulseAppSupport
import CodexPulseProtocolGenerated
import SwiftUI

struct CodexAccountsSettingsSection: View {
    @ObservedObject var model: AppModel
    @State private var editor: CodexSubscriptionEditorSession?
    @State private var pendingLink: CodexSubscriptionLinkPrompt?
    @State private var pendingUnlink: Codexpulse_Core_V1_CodexSubscriptionAccount?
    @State private var pendingDelete: Codexpulse_Core_V1_CodexSubscriptionAccount?
    @State private var pendingLegacyHistoryLink: Codexpulse_Core_V1_CodexSubscriptionAccount?
    @State private var pendingLegacyHistoryUnlink: Codexpulse_Core_V1_CodexSubscriptionAccount?
    @State private var unlinkNeedsEmail = false

    var body: some View {
        Section {
            header
            actionStatus
            accountRows
        } header: {
            Text(localizedCopy("Codex 账号与订阅"))
        } footer: {
            Text(localizedCopy("自动识别账号与手动记录保存在本机；续费日与到期日仅支持手动维护"))
        }
        .accessibilityIdentifier("settings.codex-accounts")
        .sheet(item: $editor) { session in
            CodexSubscriptionEditorSheet(
                session: Binding(
                    get: { editor ?? session },
                    set: { editor = $0 }
                ),
                isRunning: isRunning,
                conflictMessage: conflictMessage,
                onCancel: { editor = nil },
                onSave: submitEditor
            )
        }
        .onChange(of: model.codexSubscriptionActionState) { _, state in
            switch state {
            case .applied, .noop:
                editor = nil
            case .conflict(let reason):
                if reason == "revision_changed",
                   let current = editor,
                   let response = model.codexSubscriptionAccountsState.value
                {
                    editor = current.rebased(on: response.accounts)
                }
            default:
                break
            }
        }
        .sheet(item: $pendingLink) { prompt in
            CodexSubscriptionLinkSheet(
                prompt: prompt,
                isRunning: isRunning,
                onCancel: { pendingLink = nil },
                onConfirm: {
                    model.linkCodexSubscriptionAccount(
                        detectedAccountID: prompt.detectedAccountID,
                        manualEntryID: prompt.manualEntryID,
                        expectedManualRevision: prompt.expectedManualRevision
                    )
                    pendingLink = nil
                }
            )
        }
        .confirmationDialog(
            localizedCopy("确认取消关联"),
            isPresented: Binding(
                get: { pendingUnlink != nil },
                set: { if !$0 { pendingUnlink = nil } }
            ),
            titleVisibility: .visible
        ) {
            Button(localizedCopy("取消关联"), role: .destructive) {
                if let account = pendingUnlink {
                    model.unlinkCodexSubscriptionAccount(account)
                }
                pendingUnlink = nil
            }
            Button(localizedCopy("取消"), role: .cancel) { pendingUnlink = nil }
        } message: {
            Text(localizedCopy("邮箱相同不代表同一身份"))
        }
        .confirmationDialog(
            localizedCopy("确认删除账号"),
            isPresented: Binding(
                get: { pendingDelete != nil },
                set: { if !$0 { pendingDelete = nil } }
            ),
            titleVisibility: .visible
        ) {
            Button(localizedCopy("删除账号"), role: .destructive) {
                if let account = pendingDelete {
                    model.deleteCodexSubscriptionAccount(account)
                }
                pendingDelete = nil
            }
            Button(localizedCopy("取消"), role: .cancel) { pendingDelete = nil }
        } message: {
            Text(deleteConfirmationMessage)
        }
        .confirmationDialog(
            localizedCopy("恢复历史配额曲线？"),
            isPresented: Binding(
                get: { pendingLegacyHistoryLink != nil },
                set: { if !$0 { pendingLegacyHistoryLink = nil } }
            ),
            titleVisibility: .visible
        ) {
            Button(localizedCopy("恢复历史曲线")) {
                if let account = pendingLegacyHistoryLink {
                    model.linkLegacyQuotaHistory(account)
                }
                pendingLegacyHistoryLink = nil
            }
            Button(localizedCopy("取消"), role: .cancel) { pendingLegacyHistoryLink = nil }
        } message: {
            Text(localizedCopy("只会将本机旧版本保存的观测用于历史曲线和基线，不会修改当前配额、刷新状态或原始观测；可随时撤销。"))
        }
        .confirmationDialog(
            localizedCopy("停止使用这段历史？"),
            isPresented: Binding(
                get: { pendingLegacyHistoryUnlink != nil },
                set: { if !$0 { pendingLegacyHistoryUnlink = nil } }
            ),
            titleVisibility: .visible
        ) {
            Button(localizedCopy("停止使用历史"), role: .destructive) {
                if let account = pendingLegacyHistoryUnlink {
                    model.unlinkLegacyQuotaHistory(account)
                }
                pendingLegacyHistoryUnlink = nil
            }
            Button(localizedCopy("取消"), role: .cancel) { pendingLegacyHistoryUnlink = nil }
        } message: {
            Text(localizedCopy("只会取消曲线关联，本机原始观测不会被删除，之后仍可重新恢复。"))
        }
        .alert(
            localizedCopy("取消关联前需要补全邮箱"),
            isPresented: $unlinkNeedsEmail
        ) {
            Button(localizedCopy("好"), role: .cancel) {}
        }
    }

    private var header: some View {
        HStack {
            Spacer()
            Button(localizedCopy("添加账号")) {
                editor = CodexSubscriptionEditorSession.create()
            }
            .disabled(isRunning)
            .accessibilityIdentifier("settings.codex-accounts.add")
        }
    }

    @ViewBuilder
    private var actionStatus: some View {
        switch model.codexSubscriptionActionState {
        case .idle, .running, .applied, .noop:
            EmptyView()
        case .conflict(let reason):
            Label(
                CodexSubscriptionDateCopy.conflictMessage(
                    reason: reason,
                    localization: model.localization
                ),
                systemImage: "exclamationmark.triangle"
            )
            .font(.caption)
            .foregroundStyle(.orange)
            .accessibilityIdentifier("settings.codex-accounts.conflict")
        case .unavailable:
            Label(localizedCopy("保存未完成，请确认后再试"), systemImage: "exclamationmark.triangle")
                .font(.caption)
                .foregroundStyle(.orange)
        }
    }

    @ViewBuilder
    private var accountRows: some View {
        if model.codexSubscriptionAccountsState.isLoading,
           model.codexSubscriptionAccountsState.value == nil
        {
            ProgressView()
                .accessibilityIdentifier("settings.codex-accounts.loading")
        } else if case .unavailable = model.codexSubscriptionAccountsState {
            Text(localizedCopy("暂不可用"))
                .foregroundStyle(.secondary)
        } else if let response = model.codexSubscriptionAccountsState.value {
            let accounts = response.accounts
            if accounts.isEmpty {
                Text("--")
                    .foregroundStyle(.secondary)
            } else {
                ForEach(accounts, id: \.accountID) { account in
                    CodexSubscriptionAccountRow(
                        presentation: CodexSubscriptionAccountRowPresentation(
                            account,
                            localization: model.localization
                        ),
                        linkCandidate: linkCandidate(for: account, in: response),
                        isRunning: isRunning,
                        onEdit: {
                            editor = CodexSubscriptionEditorSession.edit(account)
                        },
                        onLink: { candidate in
                            pendingLink = CodexSubscriptionLinkPrompt(
                                candidate: candidate,
                                accounts: accounts
                            )
                        },
                        onUnlink: {
                            let row = CodexSubscriptionAccountRowPresentation(account)
                            if row.unlinkNeedsEmail {
                                unlinkNeedsEmail = true
                            } else {
                                pendingUnlink = account
                            }
                        },
                        onDelete: {
                            pendingDelete = account
                        },
                        onRestoreLegacyQuotaHistory: {
                            pendingLegacyHistoryLink = account
                        },
                        onRevokeLegacyQuotaHistory: {
                            pendingLegacyHistoryUnlink = account
                        }
                    )
                }
            }
        } else {
            Text("--")
                .foregroundStyle(.secondary)
        }
    }

    private var isRunning: Bool {
        if case .running = model.codexSubscriptionActionState { return true }
        return false
    }

    private var conflictMessage: String? {
        if case .conflict(let reason) = model.codexSubscriptionActionState {
            return CodexSubscriptionDateCopy.conflictMessage(
                reason: reason,
                localization: model.localization
            )
        }
        return nil
    }

    private var deleteConfirmationMessage: String {
        guard let pendingDelete else { return "" }
        if pendingDelete.detected {
            return localizedCopy(
                "将删除此账号的本地订阅记录及关联的手工补充；以后再次登录时可能重新出现"
            )
        }
        return localizedCopy("将删除此账号的本地手工记录，此操作无法撤销")
    }

    private func linkCandidate(
        for account: Codexpulse_Core_V1_CodexSubscriptionAccount,
        in response: Codexpulse_Core_V1_CodexSubscriptionAccountsResponse
    ) -> Codexpulse_Core_V1_CodexSubscriptionLinkCandidate? {
        response.linkCandidates.first { candidate in
            if account.detected, account.hasDetectedAccountID {
                return candidate.detectedAccountID == account.detectedAccountID
            }
            if account.hasManualEntryID {
                return candidate.manualEntryID == account.manualEntryID
            }
            return false
        }
    }

    private func submitEditor(_ session: CodexSubscriptionEditorSession) {
        if session.kind.isCreate {
            guard !session.draft.standaloneEmailMissing else { return }
            model.createCodexSubscriptionAccount(
                session.draft,
                manualEntryID: session.id
            )
        } else if case .edit(let account) = session.kind {
            model.updateCodexSubscriptionAccount(account, draft: session.draft)
        }
    }
}

private struct CodexSubscriptionAccountRow: View {
    let presentation: CodexSubscriptionAccountRowPresentation
    let linkCandidate: Codexpulse_Core_V1_CodexSubscriptionLinkCandidate?
    let isRunning: Bool
    let onEdit: () -> Void
    let onLink: (Codexpulse_Core_V1_CodexSubscriptionLinkCandidate) -> Void
    let onUnlink: () -> Void
    let onDelete: () -> Void
    let onRestoreLegacyQuotaHistory: () -> Void
    let onRevokeLegacyQuotaHistory: () -> Void

    var body: some View {
        VStack(alignment: .leading, spacing: 8) {
            HStack(alignment: .firstTextBaseline) {
                Text(presentation.title)
                    .font(.body.weight(.medium))
                Spacer()
                if presentation.canDelete {
                    Button(localizedCopy("删除"), role: .destructive) { onDelete() }
                        .disabled(isRunning)
                        .accessibilityIdentifier(
                            "settings.codex-accounts.delete.\(presentation.account.accountID)"
                        )
                }
                Button(localizedCopy("编辑")) { onEdit() }
                    .disabled(isRunning)
                    .accessibilityIdentifier("settings.codex-accounts.edit.\(presentation.account.accountID)")
            }
            HStack(spacing: 8) {
                labeled("套餐", presentation.planText)
                labeled("日期类型", presentation.dateKindText)
                labeled("会员日期", presentation.dateText)
                labeled("剩余天数", presentation.remainingText)
            }
            .font(.caption)
            .foregroundStyle(.secondary)
            HStack(spacing: 6) {
                ForEach(presentation.badges, id: \.self) { badge in
                    Text(badge.title(localization: AppLocalizationRegistry.shared.current))
                        .font(.caption2.weight(.semibold))
                        .padding(.horizontal, 6)
                        .padding(.vertical, 2)
                        .background(Color.accentColor.opacity(0.12), in: Capsule())
                }
                Spacer()
                if let linkCandidate, !presentation.account.linked {
                    Button(localizedCopy("关联记录")) { onLink(linkCandidate) }
                        .disabled(isRunning)
                        .accessibilityIdentifier("settings.codex-accounts.link.\(presentation.account.accountID)")
                }
                if presentation.canUnlink {
                    Button(localizedCopy("取消关联")) { onUnlink() }
                        .disabled(isRunning)
                        .accessibilityIdentifier("settings.codex-accounts.unlink.\(presentation.account.accountID)")
                }
            }
            if let historyText = presentation.legacyQuotaHistoryText {
                HStack(spacing: 8) {
                    Label(historyText, systemImage: "clock.arrow.circlepath")
                        .font(.caption)
                        .foregroundStyle(.secondary)
                    Spacer()
                    if presentation.canRestoreLegacyQuotaHistory {
                        Button(localizedCopy("恢复历史曲线")) {
                            onRestoreLegacyQuotaHistory()
                        }
                        .disabled(isRunning)
                        .accessibilityIdentifier(
                            "settings.codex-accounts.legacy-history.restore.\(presentation.account.accountID)"
                        )
                    }
                    if presentation.canRevokeLegacyQuotaHistory {
                        Button(localizedCopy("停止使用历史")) {
                            onRevokeLegacyQuotaHistory()
                        }
                        .disabled(isRunning)
                        .accessibilityIdentifier(
                            "settings.codex-accounts.legacy-history.revoke.\(presentation.account.accountID)"
                        )
                    }
                }
            }
        }
        .padding(.vertical, 4)
        .accessibilityElement(children: .combine)
        .accessibilityLabel(rowAccessibilityLabel)
        .accessibilityIdentifier("settings.codex-accounts.row.\(presentation.account.accountID)")
    }

    private func labeled(_ title: String, _ value: String) -> some View {
        Text("\(localizedCopy(title)) \(value)")
    }

    private var rowAccessibilityLabel: String {
        let localization = AppLocalizationRegistry.shared.current
        let badges = presentation.badges
            .map { $0.title(localization: localization) }
            .joined(separator: "、")
        return [
            presentation.title,
            localization.format("%@ %@", localization.textValue("套餐"), presentation.planText),
            localization.format("%@ %@", localization.textValue("会员日期"), presentation.dateText),
            localization.format("%@ %@", localization.textValue("剩余天数"), presentation.remainingText),
            presentation.legacyQuotaHistoryText ?? "",
            badges,
        ].filter { !$0.isEmpty }.joined(separator: "，")
    }
}

private struct CodexSubscriptionEditorSession: Identifiable {
    enum Kind {
        case create
        case edit(Codexpulse_Core_V1_CodexSubscriptionAccount)
    }

    let id: String
    let kind: Kind
    var draft: CodexSubscriptionManualDraft
    var hasDate: Bool

    var standaloneEmailRequired: Bool {
        switch kind {
        case .create:
            true
        case .edit(let account):
            account.hasManual_p && !account.linked && !account.detected
        }
    }

    var wouldCreateEmptySupplement: Bool {
        draft.newManualEntryID != nil && !draft.hasPersistableManualValues && !hasDate
    }

    static func create() -> Self {
        Self(
            id: UUID().uuidString.lowercased(),
            kind: .create,
            draft: CodexSubscriptionManualDraft(),
            hasDate: false
        )
    }

    static func edit(_ account: Codexpulse_Core_V1_CodexSubscriptionAccount) -> Self {
        let draft = CodexSubscriptionManualDraft(account: account)
        return Self(
            id: account.accountID,
            kind: .edit(account),
            draft: draft,
            hasDate: !draft.membershipDate.isEmpty && draft.dateKind != nil
        )
    }

    func rebased(on accounts: [Codexpulse_Core_V1_CodexSubscriptionAccount]) -> Self {
        guard case .edit(let previous) = kind else { return self }
        let latest = accounts.first { $0.accountID == previous.accountID }
            ?? accounts.first {
                previous.hasDetectedAccountID && $0.hasDetectedAccountID
                    && $0.detectedAccountID == previous.detectedAccountID
            }
            ?? accounts.first {
                previous.hasManualEntryID && $0.hasManualEntryID
                    && $0.manualEntryID == previous.manualEntryID
            }
        guard let latest else { return self }
        let rebasedDraft = draft.rebasedAutomaticValues(on: latest)
        return Self(
            id: id,
            kind: .edit(latest),
            draft: rebasedDraft,
            hasDate: hasDate
        )
    }
}

private struct CodexSubscriptionEditorSheet: View {
    @Binding var session: CodexSubscriptionEditorSession
    let isRunning: Bool
    let conflictMessage: String?
    let onCancel: () -> Void
    let onSave: (CodexSubscriptionEditorSession) -> Void
    @State private var isDatePickerPresented = false

    var body: some View {
        VStack(alignment: .leading, spacing: 16) {
            Text(localizedCopy(session.kind.isCreate ? "添加 Codex 账号" : "编辑账号"))
                .font(.title2.bold())
            if let conflictMessage {
                Text(conflictMessage)
                    .foregroundStyle(.orange)
                    .accessibilityIdentifier("settings.codex-accounts.editor.conflict")
            }
            Form {
                VStack(alignment: .leading, spacing: 4) {
                    TextField(localizedCopy("邮箱"), text: $session.draft.email)
                        .textContentType(.username)
                        .accessibilityIdentifier("settings.codex-accounts.editor.email")
                    if session.draft.emailUsesAutomaticValue {
                        Text(localizedCopy("自动识别，修改后按手工值保存"))
                            .font(.caption)
                            .foregroundStyle(.secondary)
                    }
                }
                TextField(localizedCopy("备注名"), text: $session.draft.alias)
                    .accessibilityIdentifier("settings.codex-accounts.editor.alias")
                VStack(alignment: .leading, spacing: 4) {
                    Picker(localizedCopy("套餐"), selection: $session.draft.plan) {
                        Text("--").tag(Optional<Codexpulse_Core_V1_CodexSubscriptionPlan>.none)
                        ForEach(CodexSubscriptionPlanCopy.selectablePlans, id: \.rawValue) { plan in
                            Text(CodexSubscriptionPlanCopy.displayName(plan)).tag(Optional(plan))
                        }
                    }
                    if session.draft.planUsesAutomaticValue {
                        Text(localizedCopy("自动识别，修改后按手工值保存"))
                            .font(.caption)
                            .foregroundStyle(.secondary)
                    }
                }
                Toggle(localizedCopy("会员日期"), isOn: $session.hasDate)
                if session.hasDate {
                    Picker(localizedCopy("日期类型"), selection: dateKindBinding) {
                        Text(localizedCopy("每月续费")).tag(
                            Codexpulse_Core_V1_CodexSubscriptionDateKind.nextRenewal
                        )
                        Text(localizedCopy("会员到期")).tag(
                            Codexpulse_Core_V1_CodexSubscriptionDateKind.membershipExpiry
                        )
                    }
                    if dateKindBinding.wrappedValue == .nextRenewal {
                        Picker(localizedCopy("续费日"), selection: renewalDayBinding) {
                            ForEach(1...31, id: \.self) { day in
                                Text(monthlyRenewalText(day)).tag(day)
                            }
                        }
                        .accessibilityIdentifier("settings.codex-accounts.editor.date")
                    } else {
                        LabeledContent(localizedCopy("到期日期")) {
                            Button {
                                isDatePickerPresented = true
                            } label: {
                                HStack(spacing: 6) {
                                    Text(dateButtonText)
                                    Image(systemName: "calendar")
                                        .foregroundStyle(.secondary)
                                }
                            }
                            .buttonStyle(.bordered)
                            .accessibilityLabel(localizedCopy("到期日期"))
                            .accessibilityValue(dateButtonText)
                            .accessibilityIdentifier("settings.codex-accounts.editor.date")
                            .popover(isPresented: $isDatePickerPresented) {
                                DatePicker(
                                    localizedCopy("到期日期"),
                                    selection: popoverDateBinding,
                                    displayedComponents: .date
                                )
                                .datePickerStyle(.graphical)
                                .labelsHidden()
                                .environment(\.calendar, Calendar(identifier: .gregorian))
                                .environment(
                                    \.locale,
                                    AppLocalizationRegistry.shared.current.locale
                                )
                                .padding(12)
                                .frame(minWidth: 280)
                            }
                        }
                    }
                }
            }
            .formStyle(.grouped)
            if session.standaloneEmailRequired, session.draft.standaloneEmailMissing {
                Text(localizedCopy("独立记录需要填写邮箱"))
                    .font(.caption)
                    .foregroundStyle(.orange)
            }
            HStack {
                Spacer()
                Button(localizedCopy("取消"), role: .cancel, action: onCancel)
                Button(localizedCopy("保存账号")) {
                    applyDateState()
                    onSave(session)
                }
                .keyboardShortcut(.defaultAction)
                .disabled(
                    isRunning
                        || (session.standaloneEmailRequired && session.draft.standaloneEmailMissing)
                        || session.wouldCreateEmptySupplement
                )
                .accessibilityIdentifier("settings.codex-accounts.editor.save")
            }
        }
        .padding(20)
        .frame(minWidth: 420, minHeight: 360)
    }

    private var dateKindBinding: Binding<Codexpulse_Core_V1_CodexSubscriptionDateKind> {
        Binding(
            get: { session.draft.dateKind ?? .nextRenewal },
            set: { kind in
                guard session.draft.dateKind != kind else { return }
                session.draft.dateKind = kind
                if kind == .nextRenewal {
                    session.draft.membershipDate = CodexSubscriptionCivilDate.monthlyRenewalAnchor(
                        dayOfMonth: storedDayOfMonth
                    )
                } else {
                    session.draft.membershipDate = CodexSubscriptionCivilDate.isoString(from: Date())
                }
            }
        )
    }

    private var renewalDayBinding: Binding<Int> {
        Binding(
            get: { storedDayOfMonth },
            set: { day in
                session.draft.membershipDate = CodexSubscriptionCivilDate.monthlyRenewalAnchor(
                    dayOfMonth: day
                )
            }
        )
    }

    private var storedDayOfMonth: Int {
        CodexSubscriptionCivilDate.dayOfMonth(from: session.draft.membershipDate)
            ?? CodexSubscriptionCivilDate.dayOfMonth(
                from: CodexSubscriptionCivilDate.isoString(from: Date())
            )
            ?? 1
    }

    private func monthlyRenewalText(_ day: Int) -> String {
        AppLocalizationRegistry.shared.current.format("每月 %lld 日", Int64(day))
    }

    private var dateBinding: Binding<Date> {
        Binding(
            get: {
                CodexSubscriptionCivilDate.date(from: session.draft.membershipDate) ?? Date()
            },
            set: { date in
                session.draft.membershipDate = CodexSubscriptionCivilDate.isoString(from: date)
            }
        )
    }

    private var popoverDateBinding: Binding<Date> {
        Binding(
            get: { dateBinding.wrappedValue },
            set: { date in
                dateBinding.wrappedValue = date
                isDatePickerPresented = false
            }
        )
    }

    private var dateButtonText: String {
        CodexSubscriptionCivilDate.displayString(
            from: CodexSubscriptionCivilDate.isoString(from: dateBinding.wrappedValue),
            localization: AppLocalizationRegistry.shared.current
        )
    }

    private func applyDateState() {
        if session.hasDate {
            if session.draft.dateKind == nil {
                session.draft.dateKind = .nextRenewal
            }
            if session.draft.membershipDate.isEmpty {
                if session.draft.dateKind == .nextRenewal {
                    session.draft.membershipDate = CodexSubscriptionCivilDate.monthlyRenewalAnchor(
                        dayOfMonth: storedDayOfMonth
                    )
                } else {
                    session.draft.membershipDate = CodexSubscriptionCivilDate.isoString(from: Date())
                }
            }
        } else {
            session.draft.membershipDate = ""
            session.draft.dateKind = nil
        }
    }
}

private extension CodexSubscriptionEditorSession.Kind {
    var isCreate: Bool {
        if case .create = self { return true }
        return false
    }
}

private struct CodexSubscriptionLinkPrompt: Identifiable {
    let id: String
    let detectedAccountID: String
    let manualEntryID: String
    let expectedManualRevision: Int64
    let detectedSummary: String
    let manualSummary: String

    init(
        candidate: Codexpulse_Core_V1_CodexSubscriptionLinkCandidate,
        accounts: [Codexpulse_Core_V1_CodexSubscriptionAccount]
    ) {
        detectedAccountID = candidate.detectedAccountID
        manualEntryID = candidate.manualEntryID
        let detected = accounts.first {
            $0.hasDetectedAccountID && $0.detectedAccountID == candidate.detectedAccountID
        }
        let manual = accounts.first {
            $0.hasManualEntryID && $0.manualEntryID == candidate.manualEntryID
        }
        expectedManualRevision = manual?.hasManualRevision == true ? manual!.manualRevision : 0
        id = "\(candidate.detectedAccountID):\(candidate.manualEntryID)"
        detectedSummary = detected.map { CodexSubscriptionAccountRowPresentation($0).title } ?? "--"
        manualSummary = manual.map { CodexSubscriptionAccountRowPresentation($0).title } ?? "--"
    }
}

private struct CodexSubscriptionLinkSheet: View {
    let prompt: CodexSubscriptionLinkPrompt
    let isRunning: Bool
    let onCancel: () -> Void
    let onConfirm: () -> Void

    var body: some View {
        VStack(alignment: .leading, spacing: 16) {
            Text(localizedCopy("关联记录"))
                .font(.title2.bold())
            Text(localizedCopy("邮箱相同不代表同一身份"))
                .foregroundStyle(.secondary)
            LabeledContent(localizedCopy("曾识别"), value: prompt.detectedSummary)
            LabeledContent(localizedCopy("手动"), value: prompt.manualSummary)
            HStack {
                Spacer()
                Button(localizedCopy("取消"), role: .cancel, action: onCancel)
                Button(localizedCopy("确认关联"), action: onConfirm)
                    .disabled(isRunning)
                    .accessibilityIdentifier("settings.codex-accounts.link.confirm")
            }
        }
        .padding(20)
        .frame(minWidth: 360)
    }
}
