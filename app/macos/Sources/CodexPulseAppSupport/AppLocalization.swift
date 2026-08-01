import Foundation

public enum AppLanguagePreference: String, CaseIterable, Identifiable, Sendable {
    case system = "system"
    case chineseSimplified = "zh-CN"
    case englishUS = "en-US"

    public var id: String { rawValue }

    public init(rawValue: String) {
        switch rawValue {
        case Self.chineseSimplified.rawValue: self = .chineseSimplified
        case Self.englishUS.rawValue: self = .englishUS
        default: self = .system
        }
    }

    public var localeIdentifier: String {
        switch self {
        case .system: AppLocalization.resolveSystemLanguage().localeIdentifier
        case .chineseSimplified: "zh_CN"
        case .englishUS: "en_US"
        }
    }

    public var languageCode: String {
        switch self {
        case .system: AppLocalization.resolveSystemLanguage().languageCode
        case .chineseSimplified: "zh"
        case .englishUS: "en"
        }
    }
}

public enum AppLanguage: String, Sendable {
    case chineseSimplified = "zh-CN"
    case englishUS = "en-US"

    public var languageCode: String {
        switch self {
        case .chineseSimplified: "zh"
        case .englishUS: "en"
        }
    }

    public var localeIdentifier: String {
        switch self {
        case .chineseSimplified: "zh_CN"
        case .englishUS: "en_US"
        }
    }
}

public struct AppLocalization: Equatable, Sendable {
    private static let swiftPMResourceBundleName =
        "CodexPulseMacOS_CodexPulseAppSupport.bundle"

    public let preference: AppLanguagePreference
    public let language: AppLanguage

    public init(
        preference: AppLanguagePreference,
        preferredLanguages: [String] = Locale.preferredLanguages
    ) {
        self.preference = preference
        self.language = switch preference {
        case .system:
            Self.resolveSystemLanguage(preferredLanguages: preferredLanguages)
        case .chineseSimplified:
            .chineseSimplified
        case .englishUS:
            .englishUS
        }
    }

    public static let chineseSimplified = AppLocalization(preference: .chineseSimplified)
    public static let englishUS = AppLocalization(preference: .englishUS)
    public static let system = AppLocalization(preference: .system)

    public var locale: Locale { Locale(identifier: language.localeIdentifier) }

    public var bundleLanguageName: String {
        language == .englishUS ? "en" : "zh-hans"
    }

    public func text(_ key: String) -> String {
        let bundle = Self.languageBundle(named: bundleLanguageName, in: .main)
            ?? Self.swiftPMLanguageBundle(named: bundleLanguageName)
            ?? Bundle.main
        return bundle.localizedString(forKey: key, value: key, table: nil)
    }

    public func textValue(_ value: String) -> String {
        text(value)
    }

    public func format(_ key: String, _ arguments: CVarArg...) -> String {
        String(format: text(key), locale: locale, arguments: arguments)
    }

    public func quantity(_ key: String, count: Int64) -> String {
        let isSingular = count == 1 || count == -1
        let category = language == .englishUS && isSingular ? "one" : "other"
        return format("\(key).\(category)", count)
    }

    public func number(_ value: Int64) -> String {
        let formatter = NumberFormatter()
        formatter.locale = locale
        formatter.numberStyle = .decimal
        return formatter.string(from: NSNumber(value: value)) ?? String(value)
    }

    public func percent(_ value: Double, fractionDigits: Int = 0) -> String {
        let formatter = NumberFormatter()
        formatter.locale = locale
        formatter.numberStyle = .percent
        formatter.minimumFractionDigits = fractionDigits
        formatter.maximumFractionDigits = fractionDigits
        return formatter.string(from: NSNumber(value: value / 100)) ?? "--"
    }

    public static func resolveSystemLanguage(
        preferredLanguages: [String] = Locale.preferredLanguages
    ) -> AppLanguage {
        resolveSystemLanguageInternal(preferredLanguages: preferredLanguages)
    }

    private static func resolveSystemLanguageInternal(preferredLanguages: [String]) -> AppLanguage {
        for identifier in preferredLanguages {
            let languageCode = Locale(identifier: identifier).language.languageCode?.identifier
            switch languageCode {
            case "en": return .englishUS
            case "zh": return .chineseSimplified
            default: continue
            }
        }
        return .chineseSimplified
    }

    private static func languageBundle(named name: String, in parent: Bundle) -> Bundle? {
        parent.path(forResource: name, ofType: "lproj").flatMap(Bundle.init(path:))
    }

    private static func swiftPMLanguageBundle(named languageName: String) -> Bundle? {
        let roots = [
            Bundle.main.bundleURL,
            Bundle.main.resourceURL,
            Bundle.main.executableURL?.deletingLastPathComponent(),
        ].compactMap { $0 }

        for root in roots {
            let bundleURL = root.appendingPathComponent(swiftPMResourceBundleName)
            guard let resourceBundle = Bundle(url: bundleURL),
                  let languageBundle = languageBundle(named: languageName, in: resourceBundle)
            else {
                continue
            }
            return languageBundle
        }
        return nil
    }
}

public final class AppLocalizationRegistry: @unchecked Sendable {
    public static let shared = AppLocalizationRegistry()

    private let lock = NSLock()
    private var value = AppLocalization.chineseSimplified

    private init() {}

    public var current: AppLocalization {
        lock.lock()
        defer { lock.unlock() }
        return value
    }

    public func update(_ localization: AppLocalization) {
        lock.lock()
        value = localization
        lock.unlock()
    }
}
