public enum MainWindowNavigationRequest: Equatable, Sendable {
    case revealCurrent
    case open(AppFeature)

    public var navigationTarget: AppFeature? {
        switch self {
        case .revealCurrent:
            nil
        case .open(let feature):
            feature
        }
    }
}
