import Foundation

public enum AppUpdateChannel: String, Equatable, Sendable {
    case stable
    case prerelease
}

public struct AppUpdateReminder: Equatable, Sendable {
    public let version: String
    public let title: String
    public let releaseURL: URL

    public init(version: String, title: String, releaseURL: URL) {
        self.version = version
        self.title = title
        self.releaseURL = releaseURL
    }
}

public struct AppUpdateHTTPResponse: Sendable {
    public let statusCode: Int
    public let data: Data

    public init(statusCode: Int, data: Data) {
        self.statusCode = statusCode
        self.data = data
    }
}

public enum AppUpdateCheckError: Error, Equatable, Sendable {
    case invalidCurrentVersion
    case invalidResponse
    case responseTooLarge
}

public protocol AppUpdateChecking: Sendable {
    func latestUpdate(
        currentVersion: String,
        channel: AppUpdateChannel
    ) async throws -> AppUpdateReminder?
}

public struct GitHubReleaseUpdateChecker: AppUpdateChecking, Sendable {
    public typealias FetchResponse =
        @Sendable (URLRequest) async throws -> AppUpdateHTTPResponse

    private static let endpoint = URL(
        string: "https://api.github.com/repos/SisyphusSQ/codex-pulse/releases?per_page=20"
    )!
    private static let maximumResponseBytes = 1_048_576

    private let fetchResponse: FetchResponse

    public init() {
        fetchResponse = { request in
            let (data, response) = try await URLSession.shared.data(for: request)
            guard let response = response as? HTTPURLResponse else {
                throw AppUpdateCheckError.invalidResponse
            }
            return AppUpdateHTTPResponse(statusCode: response.statusCode, data: data)
        }
    }

    public init(fetchResponse: @escaping FetchResponse) {
        self.fetchResponse = fetchResponse
    }

    public func latestUpdate(
        currentVersion: String,
        channel: AppUpdateChannel
    ) async throws -> AppUpdateReminder? {
        guard let current = SemanticVersion(currentVersion) else {
            throw AppUpdateCheckError.invalidCurrentVersion
        }

        var request = URLRequest(url: Self.endpoint, timeoutInterval: 15)
        request.setValue("application/vnd.github+json", forHTTPHeaderField: "Accept")
        request.setValue("2022-11-28", forHTTPHeaderField: "X-GitHub-Api-Version")
        request.setValue("Codex-Pulse-update-check", forHTTPHeaderField: "User-Agent")

        let response = try await fetchResponse(request)
        guard response.statusCode == 200 else {
            throw AppUpdateCheckError.invalidResponse
        }
        guard response.data.count <= Self.maximumResponseBytes else {
            throw AppUpdateCheckError.responseTooLarge
        }
        let releases: [GitHubRelease]
        do {
            releases = try JSONDecoder().decode([GitHubRelease].self, from: response.data)
        } catch {
            throw AppUpdateCheckError.invalidResponse
        }

        let candidates = releases.compactMap { release -> UpdateCandidate? in
            guard !release.draft,
                  let version = SemanticVersion(release.tagName),
                  version.isPrerelease == release.prerelease,
                  channel == .prerelease || !release.prerelease,
                  version > current,
                  Self.isTrustedReleaseURL(release.htmlURL, tagName: release.tagName)
            else {
                return nil
            }
            let title = release.name?
                .trimmingCharacters(in: .whitespacesAndNewlines)
                .prefix(160)
            return UpdateCandidate(
                version: version,
                reminder: AppUpdateReminder(
                    version: version.displayValue,
                    title: title.map(String.init).flatMap { $0.isEmpty ? nil : $0 }
                        ?? "Codex Pulse v\(version.displayValue)",
                    releaseURL: release.htmlURL
                )
            )
        }
        return candidates.max(by: { $0.version < $1.version })?.reminder
    }

    public static func isTrustedReleaseURL(_ url: URL) -> Bool {
        guard let tagName = url.pathComponents.last else { return false }
        return isTrustedReleaseURL(url, tagName: tagName)
    }

    private static func isTrustedReleaseURL(_ url: URL, tagName: String) -> Bool {
        url.scheme == "https"
            && url.host?.lowercased() == "github.com"
            && url.user == nil
            && url.password == nil
            && url.query == nil
            && url.fragment == nil
            && url.pathComponents
                == ["/", "SisyphusSQ", "codex-pulse", "releases", "tag", tagName]
    }
}

private struct GitHubRelease: Decodable {
    let tagName: String
    let name: String?
    let htmlURL: URL
    let draft: Bool
    let prerelease: Bool

    enum CodingKeys: String, CodingKey {
        case tagName = "tag_name"
        case name
        case htmlURL = "html_url"
        case draft
        case prerelease
    }
}

private struct UpdateCandidate: Sendable {
    let version: SemanticVersion
    let reminder: AppUpdateReminder
}

private struct SemanticVersion: Comparable, Sendable {
    let major: Int
    let minor: Int
    let patch: Int
    let prerelease: [PrereleaseIdentifier]

    var isPrerelease: Bool { !prerelease.isEmpty }

    var displayValue: String {
        let base = "\(major).\(minor).\(patch)"
        guard !prerelease.isEmpty else { return base }
        return base + "-" + prerelease.map(\.rawValue).joined(separator: ".")
    }

    init?(_ rawValue: String) {
        let withoutPrefix = rawValue.hasPrefix("v") ? String(rawValue.dropFirst()) : rawValue
        let withoutBuild = withoutPrefix.split(separator: "+", maxSplits: 1)[0]
        let parts = withoutBuild.split(
            separator: "-",
            maxSplits: 1,
            omittingEmptySubsequences: false
        )
        let numbers = parts[0].split(separator: ".", omittingEmptySubsequences: false)
        guard numbers.count == 3,
              let major = Self.parseNumber(numbers[0]),
              let minor = Self.parseNumber(numbers[1]),
              let patch = Self.parseNumber(numbers[2])
        else {
            return nil
        }
        var prerelease: [PrereleaseIdentifier] = []
        if parts.count == 2 {
            let identifiers = parts[1].split(separator: ".", omittingEmptySubsequences: false)
            guard !identifiers.isEmpty else { return nil }
            for identifier in identifiers {
                guard let parsed = PrereleaseIdentifier(String(identifier)) else { return nil }
                prerelease.append(parsed)
            }
        }
        self.major = major
        self.minor = minor
        self.patch = patch
        self.prerelease = prerelease
    }

    static func < (lhs: SemanticVersion, rhs: SemanticVersion) -> Bool {
        if lhs.major != rhs.major { return lhs.major < rhs.major }
        if lhs.minor != rhs.minor { return lhs.minor < rhs.minor }
        if lhs.patch != rhs.patch { return lhs.patch < rhs.patch }
        if lhs.prerelease.isEmpty { return false }
        if rhs.prerelease.isEmpty { return true }
        for index in 0..<min(lhs.prerelease.count, rhs.prerelease.count) {
            let left = lhs.prerelease[index]
            let right = rhs.prerelease[index]
            if left != right { return left < right }
        }
        return lhs.prerelease.count < rhs.prerelease.count
    }

    private static func parseNumber(_ rawValue: Substring) -> Int? {
        guard !rawValue.isEmpty,
              rawValue.allSatisfy(\.isNumber),
              rawValue.count == 1 || rawValue.first != "0"
        else {
            return nil
        }
        return Int(rawValue)
    }
}

private enum PrereleaseIdentifier: Comparable, Sendable {
    case numeric(Int, rawValue: String)
    case textual(String)

    var rawValue: String {
        switch self {
        case .numeric(_, let rawValue): rawValue
        case .textual(let rawValue): rawValue
        }
    }

    init?(_ rawValue: String) {
        guard !rawValue.isEmpty,
              rawValue.unicodeScalars.allSatisfy({
                  CharacterSet.alphanumerics.contains($0) || $0 == "-"
              })
        else {
            return nil
        }
        if rawValue.allSatisfy(\.isNumber) {
            guard rawValue.count == 1 || rawValue.first != "0",
                  let value = Int(rawValue)
            else {
                return nil
            }
            self = .numeric(value, rawValue: rawValue)
        } else {
            self = .textual(rawValue)
        }
    }

    static func < (lhs: PrereleaseIdentifier, rhs: PrereleaseIdentifier) -> Bool {
        switch (lhs, rhs) {
        case (.numeric(let left, _), .numeric(let right, _)): left < right
        case (.numeric, .textual): true
        case (.textual, .numeric): false
        case (.textual(let left), .textual(let right)): left < right
        }
    }
}
