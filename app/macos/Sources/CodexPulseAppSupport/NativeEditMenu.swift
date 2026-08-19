import AppKit

@MainActor
public enum NativeEditMenu {
    public static func make(localization: AppLocalization) -> NSMenuItem {
        let menu = NSMenu(title: localization.textValue("编辑"))
        menu.addItem(command(
            title: localization.textValue("撤销"),
            action: Selector(("undo:")),
            keyEquivalent: "z"
        ))
        menu.addItem(command(
            title: localization.textValue("重做"),
            action: Selector(("redo:")),
            keyEquivalent: "z",
            modifiers: [.command, .shift]
        ))
        menu.addItem(.separator())
        menu.addItem(command(
            title: localization.textValue("剪切"),
            action: #selector(NSText.cut(_:)),
            keyEquivalent: "x"
        ))
        menu.addItem(command(
            title: localization.textValue("复制"),
            action: #selector(NSText.copy(_:)),
            keyEquivalent: "c"
        ))
        menu.addItem(command(
            title: localization.textValue("粘贴"),
            action: #selector(NSText.paste(_:)),
            keyEquivalent: "v"
        ))
        menu.addItem(.separator())
        menu.addItem(command(
            title: localization.textValue("全选"),
            action: #selector(NSText.selectAll(_:)),
            keyEquivalent: "a"
        ))

        let menuItem = NSMenuItem()
        menuItem.submenu = menu
        return menuItem
    }

    private static func command(
        title: String,
        action: Selector,
        keyEquivalent: String,
        modifiers: NSEvent.ModifierFlags = .command
    ) -> NSMenuItem {
        let item = NSMenuItem(title: title, action: action, keyEquivalent: keyEquivalent)
        item.keyEquivalentModifierMask = modifiers
        return item
    }
}
