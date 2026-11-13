import AppKit
import ApplicationServices
import CoreGraphics
import Foundation

func makeMonitor() throws -> any Monitor { NativeMonitor() }

private let mouseTapCallback: CGEventTapCallBack = { proxy, type, event, context in
    guard let context else { return Unmanaged.passUnretained(event) }
    let monitor = Unmanaged<NativeMonitor>.fromOpaque(context).takeUnretainedValue()
    return MainActor.assumeIsolated {
        monitor.handle(proxy: proxy, type: type, event: event)
    }
}

final class NativeMonitor: Monitor {
    private var tap: CFMachPort?
    private var source: CFRunLoopSource?
    private var onHold: (@Sendable () -> Void)?
    private var onOutside: (@Sendable () -> Void)?
    private var onStopped: (@Sendable () -> Void)?
    private var popup = Area(x: 0, y: 0, width: 0, height: 0, visible: false)
    private var settings = Area(x: 0, y: 0, width: 0, height: 0, visible: false)
    private var gesture = GestureState()
    private var down: CGEvent?
    private var drags: [CGEvent] = []
    private let copier = SelectionCopier()

    @MainActor func start(onHold: @escaping @Sendable () -> Void,
                          onOutside: @escaping @Sendable () -> Void,
                          onStopped: @escaping @Sendable () -> Void) throws {
        guard AXIsProcessTrusted() else { throw SelectionError.noPermission }
        guard tap == nil else { return }
        let types: [CGEventType] = [.rightMouseDown, .rightMouseUp, .rightMouseDragged,
                                    .leftMouseDown, .otherMouseDown]
        let mask = types.reduce(CGEventMask(0)) { $0 | (CGEventMask(1) << $1.rawValue) }
        guard let tap = CGEvent.tapCreate(tap: .cgSessionEventTap, place: .headInsertEventTap,
                                          options: .defaultTap, eventsOfInterest: mask,
                                          callback: mouseTapCallback,
                                          userInfo: Unmanaged.passUnretained(self).toOpaque()) else {
            throw SelectionError.monitor
        }
        guard let source = CFMachPortCreateRunLoopSource(kCFAllocatorDefault, tap, 0) else {
            CFMachPortInvalidate(tap)
            throw SelectionError.monitor
        }
        self.onHold = onHold
        self.onOutside = onOutside
        self.onStopped = onStopped
        self.tap = tap
        self.source = source
        CFRunLoopAddSource(CFRunLoopGetMain(), source, .commonModes)
        CGEvent.tapEnable(tap: tap, enable: true)
    }

    @MainActor func setAreas(popup: Area, settings: Area) {
        self.popup = popup
        self.settings = settings
    }

    @MainActor func stop() {
        if let source { CFRunLoopRemoveSource(CFRunLoopGetMain(), source, .commonModes) }
        if let tap { CFMachPortInvalidate(tap) }
        tap = nil
        source = nil
        onHold = nil
        onOutside = nil
        onStopped = nil
        down = nil
        drags.removeAll()
        gesture.reset()
    }

    func selectedText() throws -> String {
        guard AXIsProcessTrusted() else { throw SelectionError.noPermission }
        guard let text = copier.copiedText(
            clipboard: SystemSelectionClipboard(board: .general), sendCopy: postCopyShortcut
        ) else { throw SelectionError.noSelection }
        return text
    }

    @MainActor fileprivate func handle(proxy: CGEventTapProxy, type: CGEventType,
                                        event: CGEvent) -> Unmanaged<CGEvent>? {
        if type == .tapDisabledByTimeout {
            finishInterruptedGesture(proxy: proxy)
            gesture.reset()
            if let tap { CGEvent.tapEnable(tap: tap, enable: true) }
            return Unmanaged.passUnretained(event)
        }
        if type == .tapDisabledByUserInput {
            finishInterruptedGesture(proxy: proxy)
            gesture.reset()
            onStopped?()
            return Unmanaged.passUnretained(event)
        }
        let point = event.location
        if [.leftMouseDown, .rightMouseDown, .otherMouseDown].contains(type),
           popup.visible, !contains(popup, point) {
            onOutside?()
        }
        switch type {
        case .rightMouseDown:
            if contains(popup, point) || contains(settings, point) {
                return Unmanaged.passUnretained(event)
            }
            guard let copy = event.copy() else { return Unmanaged.passUnretained(event) }
            down = copy
            drags.removeAll()
            let generation = gesture.begin()
            DispatchQueue.main.asyncAfter(deadline: .now() + .milliseconds(500)) { [weak self] in
                guard let self, self.gesture.fire(generation: generation) else { return }
                self.onHold?()
            }
            return nil
        case .rightMouseDragged:
            guard gesture.active else { return Unmanaged.passUnretained(event) }
            if !gesture.triggered, let copy = event.copy() { drags.append(copy) }
            return nil
        case .rightMouseUp:
            guard gesture.active else { return Unmanaged.passUnretained(event) }
            let fired = gesture.end()
            if fired {
                down = nil
                drags.removeAll()
                return nil
            }
            replayBuffered(proxy: proxy)
            return Unmanaged.passUnretained(event)
        default:
            return Unmanaged.passUnretained(event)
        }
    }

    @MainActor private func replayBuffered(proxy: CGEventTapProxy) {
        if let down { down.tapPostEvent(proxy) }
        for drag in drags { drag.tapPostEvent(proxy) }
        down = nil
        drags.removeAll()
    }

    @MainActor private func finishInterruptedGesture(proxy: CGEventTapProxy) {
        if gesture.triggered {
            down = nil
            drags.removeAll()
        } else {
            replayBuffered(proxy: proxy)
        }
    }

    private func contains(_ area: Area, _ point: CGPoint) -> Bool {
        area.visible && point.x >= CGFloat(area.x) && point.y >= CGFloat(area.y) &&
        point.x < CGFloat(area.x + area.width) && point.y < CGFloat(area.y + area.height)
    }
}
