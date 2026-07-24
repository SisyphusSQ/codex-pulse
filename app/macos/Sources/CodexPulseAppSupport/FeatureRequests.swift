import CodexPulseProtocolGenerated
import Foundation

public enum DateRangePreset: String, CaseIterable, Hashable, Identifiable, Sendable {
    case quotaWeek = "quota_week"
    case today = "today"
    case sevenDays = "seven_days"
    case thirtyDays = "thirty_days"
    case all = "all"

    public var id: String { rawValue }

    public var title: String {
        switch self {
        case .quotaWeek: "周额度"
        case .today: "今天"
        case .sevenDays: "近 7 天"
        case .thirtyDays: "近 30 天"
        case .all: "全部"
        }
    }
}

public struct SessionQueryOptions: Equatable, Sendable {
    public var range: DateRangePreset = .sevenDays
    public var exactRange: Codexpulse_Core_V1_UTCTimeRange?
    public var activity: String = "all"
    public var projectID: String = ""
    public var modelKey: String = ""
    public var sortField: String = "lastActivityAt"
    public var sortDirection: String = "desc"

    public init() {}
}

public struct ProjectQueryOptions: Equatable, Sendable {
    public var range: DateRangePreset = .thirtyDays
    public var exactRange: Codexpulse_Core_V1_UTCTimeRange?
    public var projectID: String = ""
    public var confidence: String = "all"
    public var sortField: String = "lastActivityAt"
    public var sortDirection: String = "desc"

    public init() {}
}

public struct RuntimeQueryOptions: Equatable, Sendable {
    public var firstField: String = ""
    public var firstValues: [String] = []
    public var secondField: String = ""
    public var secondValues: [String] = []

    public init(
        firstField: String = "",
        firstValues: [String] = [],
        secondField: String = "",
        secondValues: [String] = []
    ) {
        self.firstField = firstField
        self.firstValues = firstValues
        self.secondField = secondField
        self.secondValues = secondValues
    }
}

public enum FeatureRequestFactory {
    public static func usage(
        range preset: DateRangePreset,
        now: Date = Date(),
        calendar: Calendar = .current
    ) -> Codexpulse_Core_V1_UsageCostRequest {
        var request = Codexpulse_Core_V1_UsageCostRequest()
        request.range = localDateRange(preset == .all ? .thirtyDays : preset, now: now, calendar: calendar)
        request.granularity = "day"
        return request
    }

    public static func quota(now: Date = Date()) -> Codexpulse_Core_V1_QuotaCurrentRequest {
        var request = Codexpulse_Core_V1_QuotaCurrentRequest()
        request.evaluatedAtMs = Int64(now.timeIntervalSince1970 * 1_000)
        return request
    }

    public static func sessions(
        options: SessionQueryOptions,
        cursor: String? = nil,
        limit: Int32 = 50,
        now: Date = Date(),
        calendar: Calendar = .current
    ) -> Codexpulse_Core_V1_ListSessionsRequest {
        var filters: [Codexpulse_Core_V1_FilterTerm] = []
        if options.activity == "active" || options.activity == "idle" {
            filters.append(filter(field: "activity", values: [options.activity]))
        }
        if !options.projectID.trimmingCharacters(in: .whitespacesAndNewlines).isEmpty {
            filters.append(filter(field: "projectId", values: [trimmed(options.projectID)]))
        }
        if !options.modelKey.trimmingCharacters(in: .whitespacesAndNewlines).isEmpty {
            filters.append(filter(field: "modelKey", values: [trimmed(options.modelKey)]))
        }
        let allowedSort = ["lastActivityAt", "totalTokens", "estimatedCost"]
        let sortField = allowedSort.contains(options.sortField) ? options.sortField : "lastActivityAt"
        let direction = options.sortDirection == "asc" ? "asc" : "desc"
        var request = Codexpulse_Core_V1_ListSessionsRequest()
        request.query = query(
            cursor: cursor,
            limit: limit,
            sortField: sortField,
            direction: direction,
            filters: filters,
            timeRange: options.range == .all ? nil : localDateRange(options.range, now: now, calendar: calendar)
        )
        if let exactRange = options.exactRange {
            request.query.clearTimeRange()
            request.query.exactTimeRange = exactRange
        }
        return request
    }

    public static func sessionDetail(
        sessionID: String,
        turnCursor: String? = nil,
        turnLimit: Int32 = 30,
        reportingTimeZone: TimeZone = .current
    ) -> Codexpulse_Core_V1_SessionDetailRequest {
        var request = Codexpulse_Core_V1_SessionDetailRequest()
        request.sessionID = sessionID
        request.reportingTimezone = reportingTimeZone.identifier
        request.turnPage = page(cursor: turnCursor, limit: turnLimit)
        return request
    }

    public static func projects(
        options: ProjectQueryOptions,
        cursor: String? = nil,
        limit: Int32 = 50,
        now: Date = Date(),
        calendar: Calendar = .current
    ) -> Codexpulse_Core_V1_ListProjectsRequest {
        var filters: [Codexpulse_Core_V1_FilterTerm] = []
        if !options.projectID.trimmingCharacters(in: .whitespacesAndNewlines).isEmpty {
            filters.append(filter(field: "projectId", values: [trimmed(options.projectID)]))
        }
        if options.confidence != "all" && !options.confidence.isEmpty {
            filters.append(filter(field: "confidence", values: [options.confidence]))
        }
        let allowedSort = ["lastActivityAt", "totalTokens", "estimatedCost", "displayName"]
        let sortField = allowedSort.contains(options.sortField) ? options.sortField : "lastActivityAt"
        let direction = options.sortDirection == "asc" ? "asc" : "desc"
        var request = Codexpulse_Core_V1_ListProjectsRequest()
        request.query = query(
            cursor: cursor,
            limit: limit,
            sortField: sortField,
            direction: direction,
            filters: filters,
            timeRange: localDateRange(options.range == .all ? .thirtyDays : options.range, now: now, calendar: calendar)
        )
        if let exactRange = options.exactRange {
            request.query.clearTimeRange()
            request.query.exactTimeRange = exactRange
        }
        return request
    }

    public static func projectDetail(
        dimensionKey: String,
        range: DateRangePreset,
        exactRange: Codexpulse_Core_V1_UTCTimeRange? = nil,
        sessionCursor: String? = nil,
        modelCursor: String? = nil,
        pageLimit: Int32 = 30,
        now: Date = Date(),
        calendar: Calendar = .current
    ) -> Codexpulse_Core_V1_ProjectDetailRequest {
        var request = Codexpulse_Core_V1_ProjectDetailRequest()
        request.dimensionKey = dimensionKey
        if let exactRange {
            request.exactRange = exactRange
        } else {
            request.range = localDateRange(range == .all ? .thirtyDays : range, now: now, calendar: calendar)
        }
        request.sessionPage = page(cursor: sessionCursor, limit: pageLimit)
        request.modelPage = page(cursor: modelCursor, limit: pageLimit)
        return request
    }

    public static func sources(
        options: RuntimeQueryOptions,
        cursor: String? = nil,
        limit: Int32 = 50
    ) -> Codexpulse_Core_V1_ListSourcesRequest {
        var request = Codexpulse_Core_V1_ListSourcesRequest()
        request.query = runtimeQuery(
            options: options,
            allowedFields: ["state", "kind"],
            cursor: cursor,
            limit: limit
        )
        return request
    }

    public static func jobs(
        options: RuntimeQueryOptions,
        cursor: String? = nil,
        limit: Int32 = 50
    ) -> Codexpulse_Core_V1_ListJobsRequest {
        var request = Codexpulse_Core_V1_ListJobsRequest()
        request.query = runtimeQuery(
            options: options,
            allowedFields: ["state", "phase"],
            cursor: cursor,
            limit: limit
        )
        return request
    }

    public static func health(
        options: RuntimeQueryOptions,
        cursor: String? = nil,
        limit: Int32 = 50
    ) -> Codexpulse_Core_V1_ListHealthRequest {
        var request = Codexpulse_Core_V1_ListHealthRequest()
        request.query = runtimeQuery(
            options: options,
            allowedFields: ["active", "severity", "domain"],
            cursor: cursor,
            limit: limit
        )
        return request
    }

    public static func dataHealth(now: Date = Date()) -> Codexpulse_Core_V1_DataHealthRequest {
        var request = Codexpulse_Core_V1_DataHealthRequest()
        request.evaluatedAtMs = Int64(now.timeIntervalSince1970 * 1_000)
        return request
    }

    private static func runtimeQuery(
        options: RuntimeQueryOptions,
        allowedFields: Set<String>,
        cursor: String?,
        limit: Int32
    ) -> Codexpulse_Core_V1_QueryRequest {
        var filters: [Codexpulse_Core_V1_FilterTerm] = []
        if allowedFields.contains(options.firstField) {
            let values = options.firstValues.map(trimmed).filter { !$0.isEmpty }
            if !values.isEmpty { filters.append(filter(field: options.firstField, values: values)) }
        }
        if allowedFields.contains(options.secondField) {
            let values = options.secondValues.map(trimmed).filter { !$0.isEmpty }
            if !values.isEmpty { filters.append(filter(field: options.secondField, values: values)) }
        }
        return query(cursor: cursor, limit: limit, filters: filters)
    }

    private static func query(
        cursor: String?,
        limit: Int32,
        sortField: String? = nil,
        direction: String = "desc",
        filters: [Codexpulse_Core_V1_FilterTerm] = [],
        timeRange: Codexpulse_Core_V1_LocalDateRange? = nil
    ) -> Codexpulse_Core_V1_QueryRequest {
        var request = Codexpulse_Core_V1_QueryRequest()
        request.page = page(cursor: cursor, limit: min(max(limit, 1), 100))
        if let sortField {
            var sort = Codexpulse_Core_V1_SortTerm()
            sort.field = sortField
            sort.direction = direction
            request.sort = [sort]
        }
        request.filters = filters
        if let timeRange { request.timeRange = timeRange }
        return request
    }

    private static func page(cursor: String?, limit: Int32) -> Codexpulse_Core_V1_PageRequest {
        var page = Codexpulse_Core_V1_PageRequest()
        page.limit = min(max(limit, 1), 100)
        if let cursor, !cursor.isEmpty { page.cursor = cursor }
        return page
    }

    private static func filter(
        field: String,
        values: [String]
    ) -> Codexpulse_Core_V1_FilterTerm {
        var filter = Codexpulse_Core_V1_FilterTerm()
        filter.field = field
        filter.operator = values.count == 1 ? "eq" : "in"
        filter.values = values
        return filter
    }

    private static func trimmed(_ value: String) -> String {
        value.trimmingCharacters(in: .whitespacesAndNewlines)
    }

    private static func localDateRange(
        _ preset: DateRangePreset,
        now: Date,
        calendar inputCalendar: Calendar
    ) -> Codexpulse_Core_V1_LocalDateRange {
        var calendar = Calendar(identifier: .gregorian)
        calendar.timeZone = inputCalendar.timeZone
        let today = calendar.startOfDay(for: now)
        let days: Int
        switch preset {
        case .quotaWeek: days = 7
        case .today: days = 1
        case .sevenDays: days = 7
        case .thirtyDays, .all: days = 30
        }
        let start = calendar.date(byAdding: .day, value: -(days - 1), to: today) ?? today
        let end = calendar.date(byAdding: .day, value: 1, to: today) ?? now
        let formatter = DateFormatter()
        formatter.calendar = calendar
        formatter.timeZone = calendar.timeZone
        formatter.locale = Locale(identifier: "en_US_POSIX")
        formatter.dateFormat = "yyyy-MM-dd"
        var result = Codexpulse_Core_V1_LocalDateRange()
        result.startDate = formatter.string(from: start)
        result.endDateExclusive = formatter.string(from: end)
        result.timeZone = calendar.timeZone.identifier
        return result
    }
}
