import SwiftUI

public struct FeatureStateView<Value: Sendable, Content: View>: View {
    private let state: FeatureLoadState<Value>
    private let emptyTitle: String
    private let emptySystemImage: String
    private let content: (Value) -> Content

    public init(
        state: FeatureLoadState<Value>,
        emptyTitle: String,
        emptySystemImage: String,
        @ViewBuilder content: @escaping (Value) -> Content
    ) {
        self.state = state
        self.emptyTitle = emptyTitle
        self.emptySystemImage = emptySystemImage
        self.content = content
    }

    public var body: some View {
        Group {
            if let value = state.value {
                content(value)
            } else {
                contentUnavailableState
            }
        }
        .frame(maxWidth: .infinity, maxHeight: .infinity, alignment: .topLeading)
    }

    @ViewBuilder
    private var contentUnavailableState: some View {
        switch state {
        case .idle:
            ContentUnavailableView(emptyTitle, systemImage: emptySystemImage)
        case .loading:
            PageProgressView(title: "正在加载…")
        case .empty:
            ContentUnavailableView(emptyTitle, systemImage: emptySystemImage)
        case .unavailable(let notice):
            ContentUnavailableView {
                Label("当前数据不可用", systemImage: "exclamationmark.icloud")
            } description: {
                Text(notice.retryable ? "请稍后重试。" : "当前版本无法读取这部分数据。")
            }
        case .cancelled:
            ContentUnavailableView("加载已取消", systemImage: "xmark.circle")
        case .ready, .partial, .stale:
            EmptyView()
        }
    }
}

public struct PageProgressView: View {
    private let title: String

    public init(title: String) {
        self.title = title
    }

    public var body: some View {
        VStack(spacing: 12) {
            ProgressView().controlSize(.large)
            Text(title).foregroundStyle(.secondary)
        }
        .frame(maxWidth: .infinity, maxHeight: .infinity)
        .accessibilityElement(children: .combine)
    }
}
