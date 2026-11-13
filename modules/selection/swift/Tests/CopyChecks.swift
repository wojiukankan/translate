import AppKit
import Foundation

private final class FakeClipboard: SelectionClipboard {
    var changeCount = 1
    var value = "old clipboard"
    var reads = 0
    var restorations = 0
    var snapshotAvailable = true
    var changeBeforeRestore = false

    func snapshot() -> ClipboardSnapshot? {
        guard snapshotAvailable else { return nil }
        return ClipboardSnapshot(changeCount: changeCount, items: [[.string: Data(value.utf8)]])
    }

    func text() -> String? {
        reads += 1
        return value
    }

    func restore(_ snapshot: ClipboardSnapshot, ifUnchanged count: Int) {
        if changeBeforeRestore {
            changeCount += 1
            value = "newer user copy"
        }
        guard changeCount == count else { return }
        restorations += 1
        value = String(data: snapshot.items[0][.string]!, encoding: .utf8)!
        changeCount += 1
    }
}

struct CopyChecks {
    static func run() {
        let copier = SelectionCopier()
        let unchanged = FakeClipboard()
        let absent = copier.copiedText(clipboard: unchanged, attempts: 3, pause: {}, sendCopy: { true })
        precondition(absent == nil && unchanged.reads == 0, "do not read an unchanged clipboard")

        let delayed = FakeClipboard()
        let result = copier.copiedText(clipboard: delayed, attempts: 3, pause: {
            delayed.changeCount += 1
            delayed.value = "selected text"
        }, sendCopy: { true })
        precondition(result == "selected text" && delayed.value == "old clipboard" && delayed.restorations == 1)

        let noSnapshot = FakeClipboard()
        noSnapshot.snapshotAvailable = false
        let withoutRestore = copier.copiedText(clipboard: noSnapshot, attempts: 1, pause: {}, sendCopy: {
            noSnapshot.changeCount += 1
            noSnapshot.value = "selection"
            return true
        })
        precondition(withoutRestore == "selection" && noSnapshot.value == "selection" && noSnapshot.restorations == 0)

        let userCopy = FakeClipboard()
        userCopy.changeBeforeRestore = true
        let priorSelection = copier.copiedText(clipboard: userCopy, attempts: 1, pause: {}, sendCopy: {
            userCopy.changeCount += 1
            userCopy.value = "selection"
            return true
        })
        precondition(priorSelection == "selection" && userCopy.value == "newer user copy" && userCopy.restorations == 0)

        let sendFailure = FakeClipboard()
        let failed = copier.copiedText(clipboard: sendFailure, attempts: 1, pause: {}, sendCopy: { false })
        precondition(failed == nil && sendFailure.value == "old clipboard" && sendFailure.reads == 0)

        // Two requests must not send overlapping copy shortcuts.
        let firstStarted = DispatchSemaphore(value: 0)
        let releaseFirst = DispatchSemaphore(value: 0)
        let secondAttempted = DispatchSemaphore(value: 0)
        let secondStarted = DispatchSemaphore(value: 0)
        let finished = DispatchGroup()
        let firstCopy = FakeClipboard()
        let secondCopy = FakeClipboard()
        finished.enter()
        DispatchQueue.global().async {
            _ = copier.copiedText(clipboard: firstCopy, attempts: 1, pause: {}, sendCopy: {
                firstStarted.signal()
                releaseFirst.wait()
                firstCopy.changeCount += 1
                firstCopy.value = "first"
                return true
            })
            finished.leave()
        }
        precondition(firstStarted.wait(timeout: .now() + 1) == .success)
        finished.enter()
        DispatchQueue.global().async {
            secondAttempted.signal()
            _ = copier.copiedText(clipboard: secondCopy, attempts: 1, pause: {}, sendCopy: {
                secondStarted.signal()
                secondCopy.changeCount += 1
                secondCopy.value = "second"
                return true
            })
            finished.leave()
        }
        precondition(secondAttempted.wait(timeout: .now() + 1) == .success)
        let wasBlocked = secondStarted.wait(timeout: .now() + 0.05) == .timedOut
        releaseFirst.signal()
        precondition(finished.wait(timeout: .now() + 1) == .success && wasBlocked,
                     "copy requests must be serialized")

        // A private named pasteboard exercises real multi-item, multi-format
        // snapshots and restores without touching the user's clipboard.
        let board = NSPasteboard(name: NSPasteboard.Name("translate-tests-\(UUID().uuidString)"))
        defer { board.releaseGlobally() }
        let first = NSPasteboardItem()
        let second = NSPasteboardItem()
        let custom = NSPasteboard.PasteboardType("com.example.translate.test")
        precondition(first.setString("original", forType: .string))
        precondition(first.setData(Data([0, 1, 2, 3]), forType: custom))
        precondition(second.setString("second item", forType: .string))
        precondition(board.writeObjects([first, second]))
        let system = SystemSelectionClipboard(board: board)
        let original = system.snapshot()!
        let copied = copier.copiedText(clipboard: system, attempts: 2, pause: {}, sendCopy: {
            board.clearContents()
            return board.setString("copied text", forType: .string)
        })
        precondition(copied == "copied text")
        precondition(system.snapshot()?.items == original.items, "restore all items and formats")

        let beforeNewerCopy = system.snapshot()!
        board.clearContents()
        precondition(board.setString("selection", forType: .string))
        let selectionCount = board.changeCount
        board.clearContents()
        precondition(board.setString("newer clipboard", forType: .string))
        system.restore(beforeNewerCopy, ifUnchanged: selectionCount)
        precondition(board.string(forType: .string) == "newer clipboard", "do not overwrite a newer copy")
    }
}
