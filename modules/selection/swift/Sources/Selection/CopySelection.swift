import AppKit
import CoreGraphics
import Foundation

// A snapshot keeps every representation of every item. If a representation
// cannot be materialized, leave the new clipboard alone instead of restoring
// only part of the original clipboard.
struct ClipboardSnapshot {
    let changeCount: Int
    let items: [[NSPasteboard.PasteboardType: Data]]
}

protocol SelectionClipboard {
    var changeCount: Int { get }
    func snapshot() -> ClipboardSnapshot?
    func text() -> String?
    func restore(_ snapshot: ClipboardSnapshot, ifUnchanged count: Int)
}

struct SystemSelectionClipboard: SelectionClipboard {
    let board: NSPasteboard

    var changeCount: Int { board.changeCount }

    func snapshot() -> ClipboardSnapshot? {
        let count = board.changeCount
        let pasteboardItems = board.pasteboardItems ?? []
        // An inaccessible legacy clipboard must not be mistaken for an empty one.
        if pasteboardItems.isEmpty, let types = board.types, !types.isEmpty { return nil }
        var items: [[NSPasteboard.PasteboardType: Data]] = []
        for item in pasteboardItems {
            var representations: [NSPasteboard.PasteboardType: Data] = [:]
            for type in item.types {
                guard let data = item.data(forType: type) else { return nil }
                representations[type] = data
            }
            items.append(representations)
        }
        guard board.changeCount == count else { return nil }
        return ClipboardSnapshot(changeCount: count, items: items)
    }

    func text() -> String? { board.string(forType: .string) }

    func restore(_ snapshot: ClipboardSnapshot, ifUnchanged count: Int) {
        var items: [NSPasteboardItem] = []
        for representations in snapshot.items {
            let item = NSPasteboardItem()
            for (type, data) in representations {
                guard item.setData(data, forType: type) else { return }
            }
            items.append(item)
        }
        // Do not replace a clipboard that changed while the copy was read.
        guard board.changeCount == count else { return }
        board.clearContents()
        if !items.isEmpty { _ = board.writeObjects(items) }
    }
}

// Clipboard copies are serialized so simultaneous translation requests do not
// mistake one another's clipboard update for their own.
final class SelectionCopier {
    private let lock = NSLock()

    func copiedText(clipboard: any SelectionClipboard,
                    attempts: Int = 100,
                    pause: () -> Void = { Thread.sleep(forTimeInterval: 0.02) },
                    sendCopy: () -> Bool) -> String? {
        lock.lock()
        defer { lock.unlock() }

        let snapshot = clipboard.snapshot()
        let beforeCopy = clipboard.changeCount
        guard sendCopy() else { return nil }

        for attempt in 0..<attempts {
            let count = clipboard.changeCount
            if count != beforeCopy, let text = clipboard.text(), !text.isEmpty,
               clipboard.changeCount == count {
                if let snapshot, snapshot.changeCount == beforeCopy {
                    clipboard.restore(snapshot, ifUnchanged: count)
                }
                return text
            }
            if attempt + 1 < attempts { pause() }
        }
        return nil
    }
}

func postCopyShortcut() -> Bool {
    guard let source = CGEventSource(stateID: .combinedSessionState),
          let commandDown = CGEvent(keyboardEventSource: source, virtualKey: 55, keyDown: true),
          let cDown = CGEvent(keyboardEventSource: source, virtualKey: 8, keyDown: true),
          let cUp = CGEvent(keyboardEventSource: source, virtualKey: 8, keyDown: false),
          let commandUp = CGEvent(keyboardEventSource: source, virtualKey: 55, keyDown: false) else {
        return false
    }
    commandDown.flags = .maskCommand
    cDown.flags = .maskCommand
    cUp.flags = .maskCommand
    commandUp.flags = []
    for event in [commandDown, cDown, cUp, commandUp] { event.post(tap: .cghidEventTap) }
    return true
}
