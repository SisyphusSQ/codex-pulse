import SwiftUI

public struct SegmentedChoice: Identifiable, Equatable, Sendable {
    public let id: String
    public let title: String

    public init(id: String, title: String) {
        self.id = id
        self.title = title
    }
}

public enum SegmentedSelection {
    public static func resolved(_ selection: String?, in ids: [String]) -> String? {
        if let selection, ids.contains(selection) { return selection }
        return ids.first
    }

    public static func moved(_ selection: String?, in ids: [String], by offset: Int) -> String? {
        guard let current = resolved(selection, in: ids),
              let index = ids.firstIndex(of: current) else { return nil }
        return ids[max(0, min(ids.count - 1, index + offset))]
    }
}

/// 动态窗口使用 SwiftUI 按钮，避免 NSSegmentedControl 缩减分段时更新旧索引。
public struct DynamicSegmentedSelector: View {
    public let choices: [SegmentedChoice]
    @Binding private var selection: String?
    @FocusState private var focusedID: String?

    public init(choices: [SegmentedChoice], selection: Binding<String?>) {
        self.choices = choices
        self._selection = selection
    }

    private var ids: [String] { choices.map(\.id) }
    private var selectedID: String? { SegmentedSelection.resolved(selection, in: ids) }

    public var body: some View {
        ScrollViewReader { proxy in
            GeometryReader { geometry in
                ScrollView(.horizontal) {
                    segments.frame(minWidth: geometry.size.width, alignment: .trailing)
                }
                .scrollIndicators(.hidden)
            }
            .frame(height: 32)
            .onChange(of: focusedID) { _, id in
                if let id { proxy.scrollTo(id) }
            }
        }
        .onChange(of: ids, initial: true) { _, _ in
            if selection != selectedID { selection = selectedID }
            if let focusedID, !ids.contains(focusedID) { self.focusedID = nil }
        }
        .onMoveCommand { direction in
            let offset: Int
            switch direction {
            case .left: offset = -1
            case .right: offset = 1
            default: return
            }
            guard let next = SegmentedSelection.moved(focusedID ?? selectedID, in: ids, by: offset) else { return }
            selection = next
            focusedID = next
        }
    }

    private var segments: some View {
        HStack(spacing: 2) {
            ForEach(choices) { choice in
                Button {
                    guard ids.contains(choice.id) else { return }
                    selection = choice.id
                } label: {
                    Text(verbatim: choice.title)
                        .font(.subheadline)
                        .fixedSize()
                        .padding(.horizontal, 10)
                        .padding(.vertical, 5)
                        .background {
                            if choice.id == selectedID {
                                RoundedRectangle(cornerRadius: 5)
                                    .fill(Color.accentColor.opacity(0.18))
                            }
                        }
                        .contentShape(Rectangle())
                }
                .id(choice.id)
                .buttonStyle(.plain)
                .focused($focusedID, equals: choice.id)
                .accessibilityLabel(choice.title)
                .accessibilityAddTraits(choice.id == selectedID ? .isSelected : [])
                .accessibilityIdentifier("dynamic-option.\(choice.id)")
            }
        }
        .padding(2)
        .background(.quaternary, in: RoundedRectangle(cornerRadius: 7))
    }
}
