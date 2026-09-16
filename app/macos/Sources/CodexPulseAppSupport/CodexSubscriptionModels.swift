import Foundation
import CodexPulseProtocolGenerated

public struct CodexSubscriptionEvaluationContext: Equatable, Sendable {
    public let evaluatedAtMs: Int64
    public let timeZone: String

    public static func current(
        now: Date = Date(),
        timeZone: TimeZone = .current
    ) -> Self {
        Self(
            evaluatedAtMs: Int64((now.timeIntervalSince1970 * 1_000).rounded()),
            timeZone: timeZone.identifier
        )
    }
}

public enum CodexSubscriptionCalendar {
    public static func nextLocalDayBoundary(
        now: Date = Date(),
        timeZone: TimeZone = .current
    ) -> Date {
        var calendar = Calendar(identifier: .gregorian)
        calendar.timeZone = timeZone
        let start = calendar.startOfDay(for: now)
        return calendar.date(byAdding: .day, value: 1, to: start) ?? now.addingTimeInterval(86_400)
    }
}

public enum CodexSubscriptionCivilDate {
    public static func parse(_ value: String) -> DateComponents? {
        let bytes = Array(value.utf8)
        guard bytes.count == 10, bytes[4] == 45, bytes[7] == 45 else { return nil }
        for index in bytes.indices where index != 4 && index != 7 {
            guard (48...57).contains(bytes[index]) else { return nil }
        }
        let parts = value.split(separator: "-")
        guard parts.count == 3,
              let year = Int(parts[0]),
              let month = Int(parts[1]),
              let day = Int(parts[2]),
              year >= 1,
              (1...12).contains(month),
              (1...31).contains(day)
        else { return nil }
        var calendar = Calendar(identifier: .gregorian)
        calendar.timeZone = TimeZone(secondsFromGMT: 0)!
        guard let date = calendar.date(
            from: DateComponents(year: year, month: month, day: day)
        ) else { return nil }
        let verified = calendar.dateComponents([.year, .month, .day], from: date)
        guard verified.year == year, verified.month == month, verified.day == day else {
            return nil
        }
        var components = DateComponents()
        components.calendar = Calendar(identifier: .gregorian)
        components.year = year
        components.month = month
        components.day = day
        return components
    }

    public static func format(_ components: DateComponents) -> String? {
        guard let year = components.year, let month = components.month, let day = components.day else {
            return nil
        }
        let value = String(format: "%04d-%02d-%02d", year, month, day)
        return parse(value) == nil ? nil : value
    }

    public static func date(
        from iso: String,
        timeZone: TimeZone = .current
    ) -> Date? {
        guard let parsed = parse(iso) else { return nil }
        var calendar = Calendar(identifier: .gregorian)
        calendar.timeZone = timeZone
        return calendar.date(
            from: DateComponents(year: parsed.year, month: parsed.month, day: parsed.day)
        )
    }

    public static func isoString(
        from date: Date,
        timeZone: TimeZone = .current
    ) -> String {
        var calendar = Calendar(identifier: .gregorian)
        calendar.timeZone = timeZone
        let components = calendar.dateComponents([.year, .month, .day], from: date)
        return format(components) ?? ""
    }

    public static func displayString(
        from iso: String,
        localization: AppLocalization,
        timeZone: TimeZone = .current
    ) -> String {
        guard let date = date(from: iso, timeZone: timeZone) else { return "--" }
        let formatter = DateFormatter()
        formatter.calendar = Calendar(identifier: .gregorian)
        formatter.locale = localization.locale
        formatter.timeZone = timeZone
        formatter.dateStyle = .medium
        formatter.timeStyle = .none
        return formatter.string(from: date)
    }

    public static func dayOfMonth(from iso: String) -> Int? {
        parse(iso)?.day
    }

    public static func monthlyRenewalAnchor(dayOfMonth: Int) -> String {
        guard (1...31).contains(dayOfMonth) else { return "" }
        return String(format: "2000-01-%02d", dayOfMonth)
    }
}

public enum CodexSubscriptionPlanCopy {
    public static var selectablePlans: [Codexpulse_Core_V1_CodexSubscriptionPlan] {
        Codexpulse_Core_V1_CodexSubscriptionPlan.allCases.filter { $0 != .unspecified }
    }

    public static func displayName(
        _ plan: Codexpulse_Core_V1_CodexSubscriptionPlan?,
        localization: AppLocalization = AppLocalizationRegistry.shared.current
    ) -> String {
        switch plan {
        case .free: localization.textValue("Free")
        case .go: localization.textValue("Go")
        case .plus: localization.textValue("Plus")
        case .pro5X: localization.textValue("Pro 5×")
        case .pro20X: localization.textValue("Pro 20×")
        case .team: localization.textValue("Team")
        case .business: localization.textValue("Business")
        case .enterprise: localization.textValue("Enterprise")
        case .edu: localization.textValue("Edu")
        case .unspecified, .UNRECOGNIZED, .none: "--"
        }
    }
}

public enum CodexSubscriptionActionState: Equatable, Sendable {
    case idle
    case running
    case applied
    case noop
    case conflict(reason: String)
    case unavailable(AppNotice)
}

public struct CodexSubscriptionManualDraft: Equatable, Sendable {
    public var email: String
    public var alias: String
    public var plan: Codexpulse_Core_V1_CodexSubscriptionPlan?
    public var membershipDate: String
    public var dateKind: Codexpulse_Core_V1_CodexSubscriptionDateKind?
    public var newManualEntryID: String?
    private var inheritedAutomaticEmail: String?
    private var inheritedAutomaticPlan: Codexpulse_Core_V1_CodexSubscriptionPlan?
    private var emailStartedAsManual: Bool
    private var planStartedAsManual: Bool

    public init(
        email: String = "",
        alias: String = "",
        plan: Codexpulse_Core_V1_CodexSubscriptionPlan? = nil,
        membershipDate: String = "",
        dateKind: Codexpulse_Core_V1_CodexSubscriptionDateKind? = nil,
        newManualEntryID: String? = nil
    ) {
        self.email = email
        self.alias = alias
        self.plan = plan
        self.membershipDate = membershipDate
        self.dateKind = dateKind
        self.newManualEntryID = newManualEntryID
        inheritedAutomaticEmail = nil
        inheritedAutomaticPlan = nil
        emailStartedAsManual = !email.trimmingCharacters(in: .whitespacesAndNewlines).isEmpty
        planStartedAsManual = plan != nil
    }

    public init(account: Codexpulse_Core_V1_CodexSubscriptionAccount) {
        let detectedEmail = account.hasDetectedEmail
            ? account.detectedEmail.trimmingCharacters(in: .whitespacesAndNewlines)
            : ""
        let automaticPlan = account.hasAutomaticPlan ? account.automaticPlan : nil
        emailStartedAsManual = account.hasManualEmail
        planStartedAsManual = account.hasManualPlan
        inheritedAutomaticEmail = detectedEmail.isEmpty ? nil : detectedEmail
        inheritedAutomaticPlan = automaticPlan
        email = account.hasManualEmail ? account.manualEmail : detectedEmail
        alias = account.hasAlias ? account.alias : ""
        plan = account.hasManualPlan ? account.manualPlan : automaticPlan
        membershipDate = account.hasMembershipDate ? account.membershipDate : ""
        dateKind = account.hasDateKind ? account.dateKind : nil
        if account.detected && !account.hasManual_p {
            newManualEntryID = UUID().uuidString.lowercased()
        } else {
            newManualEntryID = nil
        }
    }

    public var trimmedEmail: String {
        email.trimmingCharacters(in: .whitespacesAndNewlines)
    }

    public var standaloneEmailMissing: Bool {
        trimmedEmail.isEmpty
    }

    public var hasPersistableManualValues: Bool {
        (!trimmedEmail.isEmpty && !emailUsesAutomaticValue)
            || !alias.trimmingCharacters(in: .whitespacesAndNewlines).isEmpty
            || (plan != nil && !planUsesAutomaticValue)
            || !membershipDate.trimmingCharacters(in: .whitespacesAndNewlines).isEmpty
    }

    public var emailUsesAutomaticValue: Bool {
        guard !emailStartedAsManual, let inheritedAutomaticEmail else { return false }
        return trimmedEmail == inheritedAutomaticEmail
    }

    public var planUsesAutomaticValue: Bool {
        !planStartedAsManual && plan != nil && plan == inheritedAutomaticPlan
    }

    public func makeFields() -> Codexpulse_Core_V1_CodexSubscriptionManualFields {
        var fields = Codexpulse_Core_V1_CodexSubscriptionManualFields()
        let trimmedEmail = trimmedEmail
        if !emailUsesAutomaticValue {
            fields.email = trimmedEmail
        }
        fields.alias = alias.trimmingCharacters(in: .whitespacesAndNewlines)
        if let plan, !planUsesAutomaticValue {
            fields.plan = plan
        }
        let trimmedDate = membershipDate.trimmingCharacters(in: .whitespacesAndNewlines)
        if !trimmedDate.isEmpty {
            fields.membershipDate = trimmedDate
        }
        if let dateKind {
            fields.dateKind = dateKind
        }
        return fields
    }

    public func rebasedAutomaticValues(
        on account: Codexpulse_Core_V1_CodexSubscriptionAccount
    ) -> Self {
        let latest = Self(account: account)
        let emailWasUntouched = !emailStartedAsManual
            && trimmedEmail == (inheritedAutomaticEmail ?? "")
        let planWasUntouched = !planStartedAsManual
            && plan == inheritedAutomaticPlan
        var rebased = self
        rebased.inheritedAutomaticEmail = latest.inheritedAutomaticEmail
        rebased.inheritedAutomaticPlan = latest.inheritedAutomaticPlan
        if emailWasUntouched {
            rebased.email = latest.email
            rebased.emailStartedAsManual = latest.emailStartedAsManual
        } else if !emailStartedAsManual {
            rebased.emailStartedAsManual = true
        }
        if planWasUntouched {
            rebased.plan = latest.plan
            rebased.planStartedAsManual = latest.planStartedAsManual
        } else if !planStartedAsManual {
            rebased.planStartedAsManual = true
        }
        if account.hasManual_p {
            rebased.newManualEntryID = nil
        }
        return rebased
    }
}

public enum CodexSubscriptionBadge: String, Equatable, Sendable {
    case current
    case detected
    case manual
    case linked

    public func title(localization: AppLocalization) -> String {
        switch self {
        case .current: localization.textValue("当前")
        case .detected: localization.textValue("曾识别")
        case .manual: localization.textValue("手动")
        case .linked: localization.textValue("已关联")
        }
    }
}

public struct CodexSubscriptionAccountRowPresentation: Equatable, Sendable {
    public let account: Codexpulse_Core_V1_CodexSubscriptionAccount
    public let title: String
    public let planText: String
    public let dateKindText: String
    public let dateText: String
    public let remainingText: String
    public let badges: [CodexSubscriptionBadge]
    public let canUnlink: Bool
    public let unlinkNeedsEmail: Bool
    public let canDelete: Bool

    public init(
        _ account: Codexpulse_Core_V1_CodexSubscriptionAccount,
        localization: AppLocalization = AppLocalizationRegistry.shared.current
    ) {
        self.account = account
        let alias = account.hasAlias ? account.alias.trimmingCharacters(in: .whitespacesAndNewlines) : ""
        let email = account.hasDisplayEmail
            ? account.displayEmail.trimmingCharacters(in: .whitespacesAndNewlines)
            : ""
        if !alias.isEmpty {
            title = alias
        } else if !email.isEmpty {
            title = email
        } else {
            title = "--"
        }
        planText = CodexSubscriptionPlanCopy.displayName(
            account.hasResolvedPlan ? account.resolvedPlan : nil,
            localization: localization
        )
        if account.hasDateKind, account.hasMembershipDate {
            dateKindText = CodexSubscriptionDateCopy.kindTitle(account.dateKind, localization: localization)
            dateText = CodexSubscriptionDateCopy.dateText(
                kind: account.dateKind,
                isoDate: account.membershipDate,
                localization: localization
            )
        } else {
            dateKindText = "--"
            dateText = "--"
        }
        remainingText = CodexSubscriptionDateCopy.remainingText(
            state: account.dateState,
            dayDelta: account.hasDayDelta ? account.dayDelta : nil,
            localization: localization
        )
        var badges: [CodexSubscriptionBadge] = []
        if account.current { badges.append(.current) }
        if account.detected { badges.append(.detected) }
        if account.hasManual_p { badges.append(.manual) }
        if account.linked { badges.append(.linked) }
        self.badges = badges
        canUnlink = account.linked && account.hasDetectedAccountID && account.hasManualEntryID
        unlinkNeedsEmail = canUnlink && (!account.hasManualEmail
            || account.manualEmail.trimmingCharacters(in: .whitespacesAndNewlines).isEmpty)
        canDelete = !account.current
    }
}

public enum CodexSubscriptionDateCopy {
    public static func kindTitle(
        _ kind: Codexpulse_Core_V1_CodexSubscriptionDateKind,
        localization: AppLocalization
    ) -> String {
        switch kind {
        case .nextRenewal: localization.textValue("每月续费")
        case .membershipExpiry: localization.textValue("到期")
        case .unspecified, .UNRECOGNIZED: "--"
        }
    }

    public static func dateText(
        kind: Codexpulse_Core_V1_CodexSubscriptionDateKind,
        isoDate: String,
        localization: AppLocalization
    ) -> String {
        switch kind {
        case .nextRenewal:
            guard let day = CodexSubscriptionCivilDate.dayOfMonth(from: isoDate) else { return "--" }
            return localization.format("每月 %lld 日", Int64(day))
        case .membershipExpiry:
            return CodexSubscriptionCivilDate.displayString(
                from: isoDate,
                localization: localization
            )
        case .unspecified, .UNRECOGNIZED:
            return "--"
        }
    }

    public static func remainingText(
        state: Codexpulse_Core_V1_CodexSubscriptionDateState,
        dayDelta: Int32?,
        localization: AppLocalization
    ) -> String {
        switch state {
        case .future:
            guard let dayDelta else { return "--" }
            return localization.format("剩余 %lld 天", Int64(dayDelta))
        case .today:
            return localization.textValue("今天")
        case .needsUpdate:
            guard let dayDelta else { return localization.textValue("需要更新") }
            let elapsed = Int64(abs(dayDelta))
            return localization.format("已过 %lld 天 · 需要更新", elapsed)
        case .unavailable, .unspecified, .UNRECOGNIZED:
            return "--"
        }
    }

    public static func popoverDateText(
        kind: Codexpulse_Core_V1_CodexSubscriptionDateKind,
        isoDate: String,
        localization: AppLocalization
    ) -> String {
        let date = dateText(kind: kind, isoDate: isoDate, localization: localization)
        if kind == .nextRenewal {
            return date
        }
        return "\(kindTitle(kind, localization: localization)) \(date)"
    }

    public static func conflictMessage(
        reason: String,
        localization: AppLocalization
    ) -> String {
        switch reason {
        case "revision_changed":
            localization.textValue("列表已更新，请确认后再保存")
        case "already_linked":
            localization.textValue("该记录已经关联")
        case "link_target_changed":
            localization.textValue("关联对象已变化")
        case "manual_email_required_before_unlink":
            localization.textValue("取消关联前需要补全邮箱")
        case "request_id_reused":
            localization.textValue("请关闭后重新打开编辑")
        case "current_account":
            localization.textValue("该账号已成为当前账号，不能删除")
        default:
            localization.textValue("保存未完成，请确认后再试")
        }
    }
}

public enum CodexSubscriptionReadback {
    public static func excludesAccount(
        _ response: Codexpulse_Core_V1_CodexSubscriptionAccountsResponse,
        accountID: String
    ) -> Bool {
        !response.accounts.contains { $0.accountID == accountID }
    }

    public static func containsAccount(
        _ response: Codexpulse_Core_V1_CodexSubscriptionAccountsResponse,
        detectedAccountID: String? = nil,
        manualEntryID: String? = nil,
        detected: Bool? = nil,
        hasManual: Bool? = nil,
        linked: Bool? = nil,
        fields: Codexpulse_Core_V1_CodexSubscriptionManualFields? = nil
    ) -> Bool {
        response.accounts.contains { account in
            if let detectedAccountID,
               !account.hasDetectedAccountID || account.detectedAccountID != detectedAccountID
            {
                return false
            }
            if let manualEntryID,
               !account.hasManualEntryID || account.manualEntryID != manualEntryID
            {
                return false
            }
            if let detected, account.detected != detected { return false }
            if let hasManual, account.hasManual_p != hasManual { return false }
            if let linked, account.linked != linked { return false }
            if let fields, !matchesManualFields(account, fields: fields) {
                return false
            }
            return detectedAccountID != nil || manualEntryID != nil
        }
    }

    private static func matchesManualFields(
        _ account: Codexpulse_Core_V1_CodexSubscriptionAccount,
        fields: Codexpulse_Core_V1_CodexSubscriptionManualFields
    ) -> Bool {
        guard account.hasManual_p,
              matchesOptionalText(
                expectedPresent: fields.hasEmail,
                expected: fields.email,
                actualPresent: account.hasManualEmail,
                actual: account.manualEmail
              ),
              matchesOptionalText(
                expectedPresent: fields.hasAlias,
                expected: fields.alias,
                actualPresent: account.hasAlias,
                actual: account.alias
              ),
              fields.hasPlan == account.hasManualPlan,
              !fields.hasPlan || fields.plan == account.manualPlan,
              matchesOptionalText(
                expectedPresent: fields.hasMembershipDate,
                expected: fields.membershipDate,
                actualPresent: account.hasMembershipDate,
                actual: account.membershipDate
              ),
              fields.hasDateKind == account.hasDateKind,
              !fields.hasDateKind || fields.dateKind == account.dateKind
        else { return false }
        return true
    }

    private static func matchesOptionalText(
        expectedPresent: Bool,
        expected: String,
        actualPresent: Bool,
        actual: String
    ) -> Bool {
        let normalized = expectedPresent
            ? expected.trimmingCharacters(in: .whitespacesAndNewlines)
            : ""
        if normalized.isEmpty {
            return !actualPresent
        }
        return actualPresent && actual == normalized
    }
}

public struct CodexSubscriptionPopoverFacts: Equatable, Sendable {
    public let planText: String
    public let emailText: String
    public let dateText: String?
    public let remainingText: String?
    public let accessibilityLabel: String

    public var secondaryText: String? {
        switch (dateText, remainingText) {
        case let (date?, remaining?): "\(date) · \(remaining)"
        case let (date?, nil): date
        case let (nil, remaining?): remaining
        case (nil, nil): nil
        }
    }
}

public enum CodexSubscriptionPopoverResolution: Equatable, Sendable {
    case facts(CodexSubscriptionPopoverFacts)
    case unavailable
}

public enum CodexQuotaAccountSummaryCopy {
    public static func summary(
        quota: Codexpulse_Core_V1_QuotaCurrentResponse,
        snapshot: Codexpulse_Core_V1_AccountSnapshotResponse
    ) -> PopoverAccountSummaryPresentation? {
        guard let quotaKey = CodexAccountContext.key(fromQuota: quota),
              quotaKey == CodexAccountContext.key(fromAccount: snapshot)
        else { return nil }
        return PopoverAccountSummaryPresentation(
            account: CodexAccountPresentation(snapshot),
            snapshot: snapshot
        )
    }
}

public enum CodexSubscriptionPopoverCopy {
    public static func facts(
        from snapshot: Codexpulse_Core_V1_AccountSnapshotResponse,
        account: CodexAccountPresentation,
        localization: AppLocalization
    ) -> CodexSubscriptionPopoverResolution {
        let accountType = snapshot.hasAccount ? snapshot.account.type : ""
        guard snapshot.hasSubscription, accountType == "chatgpt" else {
            return .facts(
                CodexSubscriptionPopoverFacts(
                    planText: account.planText,
                    emailText: account.emailText,
                    dateText: nil,
                    remainingText: nil,
                    accessibilityLabel: account.accessibilityLabel
                )
            )
        }
        let subscription = snapshot.subscription
        let bindingConfirmed = snapshot.hasBinding && snapshot.binding.state == "confirmed"
        let contextMatches = CodexAccountContext.key(fromAccount: snapshot) != nil
        guard subscription.current, bindingConfirmed, contextMatches, !subscription.accountID.isEmpty else {
            return .unavailable
        }
        let planText = subscription.hasResolvedPlan
            ? CodexSubscriptionPlanCopy.displayName(subscription.resolvedPlan, localization: localization)
            : account.planText
        let emailText: String = {
            if account.emailText != "--" { return account.emailText }
            if subscription.hasDisplayEmail {
                let value = subscription.displayEmail.trimmingCharacters(in: .whitespacesAndNewlines)
                if !value.isEmpty { return value }
            }
            return "--"
        }()
        var dateText: String?
        var remainingText: String?
        if subscription.hasMembershipDate, subscription.hasDateKind {
            dateText = CodexSubscriptionDateCopy.popoverDateText(
                kind: subscription.dateKind,
                isoDate: subscription.membershipDate,
                localization: localization
            )
            remainingText = CodexSubscriptionDateCopy.remainingText(
                state: subscription.dateState,
                dayDelta: subscription.hasDayDelta ? subscription.dayDelta : nil,
                localization: localization
            )
            if remainingText == "--" {
                remainingText = nil
            }
        }
        var accessibility = localization.format("account.plan", planText, emailText)
        let secondary = [dateText, remainingText].compactMap { $0 }.joined(separator: " · ")
        if !secondary.isEmpty {
            accessibility += "，\(secondary)"
        }
        return .facts(
            CodexSubscriptionPopoverFacts(
                planText: planText,
                emailText: emailText,
                dateText: dateText,
                remainingText: remainingText,
                accessibilityLabel: accessibility
            )
        )
    }
}
