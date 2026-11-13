package selection

import (
	"sync"
	"sync/atomic"
	"time"

	"github.com/ebitengine/purego"
	"github.com/egoist/mygo"
)

const (
	leftMouseDown      = 1
	rightMouseDown     = 3
	rightMouseUp       = 4
	rightMouseDragged  = 7
	otherMouseDown     = 25
	tapDisabledTimeout = 0xfffffffe
	tapDisabledInput   = 0xffffffff
	holdPeriod         = 500 * time.Millisecond
)

// gestureState follows one right press. Old timers cannot trigger a later
// press, even when the timer reaches the main thread after its release.
type gestureState struct {
	active, triggered bool
	generation        uint64
}

func (g *gestureState) begin() uint64 {
	g.generation++
	g.active, g.triggered = true, false
	return g.generation
}

func (g *gestureState) fire(generation uint64) bool {
	if !g.active || g.triggered || g.generation != generation {
		return false
	}
	g.triggered = true
	return true
}

func (g *gestureState) end() bool {
	triggered := g.triggered
	g.reset()
	return triggered
}

func (g *gestureState) reset() {
	g.generation++
	g.active, g.triggered = false, false
}

type queuedMacCallback struct {
	session uint64
	fn      func()
}

type macMonitor struct {
	api       *macAPI
	runOnMain func(func())
	afterFunc func(time.Duration, func()) *time.Timer
	copier    selectionCopier

	// These fields are accessed on the main thread, including the tap callback.
	tap, source, handle uintptr
	popup, settings     Area
	onHold, onOutside   func()
	onStopped           func()
	gesture             gestureState
	timer               *time.Timer
	down                uintptr
	drags               []uintptr

	session     atomic.Uint64
	callbackMu  sync.Mutex
	callbacks   []queuedMacCallback
	dispatching bool
}

var macMonitors sync.Map // opaque integer handle -> *macMonitor
var nextMacHandle atomic.Uint64

// purego callbacks live for the process, so use one callback for all monitors
// and pass an integer handle to CoreGraphics rather than a Go pointer.
var macTapCallback = purego.NewCallback(func(proxy uintptr, kind uint32, event, context uintptr) uintptr {
	if value, ok := macMonitors.Load(context); ok {
		return value.(*macMonitor).handleEvent(proxy, kind, event)
	}
	return event
})

// NewMonitor creates the macOS implementation without prompting for access.
func NewMonitor() (Monitor, error) {
	api, err := loadMacAPI()
	if err != nil {
		return nil, err
	}
	return &macMonitor{api: api, runOnMain: mygo.RunOnMain, afterFunc: time.AfterFunc}, nil
}

func (m *macMonitor) Start(onHold, onOutside, onStopped func()) error {
	var err error
	m.runOnMain(func() { err = m.start(onHold, onOutside, onStopped) })
	return err
}

func (m *macMonitor) start(onHold, onOutside, onStopped func()) error {
	if !m.api.trusted() {
		return ErrNoPermission
	}
	if m.tap != 0 {
		return nil
	}
	mask := uint64(1<<rightMouseDown | 1<<rightMouseUp | 1<<rightMouseDragged | 1<<leftMouseDown | 1<<otherMouseDown)
	handle := uintptr(nextMacHandle.Add(1))
	macMonitors.Store(handle, m)
	// kCGSessionEventTap, kCGHeadInsertEventTap, kCGEventTapOptionDefault.
	tap := m.api.tapCreate(1, 0, 0, mask, macTapCallback, handle)
	if tap == 0 {
		macMonitors.Delete(handle)
		return ErrMonitor
	}
	source := m.api.portSource(0, tap, 0)
	if source == 0 {
		macMonitors.Delete(handle)
		m.api.portInvalidate(tap)
		m.api.release(tap)
		return ErrMonitor
	}
	m.tap, m.source, m.handle = tap, source, handle
	m.onHold, m.onOutside, m.onStopped = onHold, onOutside, onStopped
	m.session.Store(uint64(handle))
	m.api.runLoopAdd(m.api.runLoopMain(), source, m.api.commonModes)
	m.api.tapEnable(tap, true)
	return nil
}

func (m *macMonitor) SetAreas(popup, settings Area) {
	m.runOnMain(func() { m.popup, m.settings = popup, settings })
}

func (m *macMonitor) Stop() { m.runOnMain(m.stop) }

func (m *macMonitor) stop() {
	m.session.Store(0)
	m.callbackMu.Lock()
	m.callbacks = nil
	m.callbackMu.Unlock()
	if m.timer != nil {
		m.timer.Stop()
		m.timer = nil
	}
	if m.source != 0 {
		m.api.runLoopRemove(m.api.runLoopMain(), m.source, m.api.commonModes)
		m.api.release(m.source)
	}
	if m.tap != 0 {
		m.api.portInvalidate(m.tap)
		m.api.release(m.tap)
	}
	if m.handle != 0 {
		macMonitors.Delete(m.handle)
	}
	m.tap, m.source, m.handle = 0, 0, 0
	m.onHold, m.onOutside, m.onStopped = nil, nil, nil
	m.clearBuffered()
	m.gesture.reset()
}

func (m *macMonitor) SelectedText() (string, error) {
	if !m.api.trusted() {
		return "", ErrNoPermission
	}
	return m.copier.copiedText(generalSelectionClipboard(m.api), 100, waitForMacCopy, m.api.postCopyShortcut)
}

func containsMac(area Area, point cgPoint) bool {
	return area.Visible && point.X >= float64(area.X) && point.Y >= float64(area.Y) &&
		point.X < float64(area.X+area.Width) && point.Y < float64(area.Y+area.Height)
}

func (m *macMonitor) handleEvent(proxy uintptr, kind uint32, event uintptr) uintptr {
	if kind == tapDisabledTimeout || kind == tapDisabledInput {
		m.finishInterrupted(proxy)
		m.gesture.reset()
		if kind == tapDisabledTimeout && m.tap != 0 {
			m.api.tapEnable(m.tap, true)
		} else if kind == tapDisabledInput {
			m.queueCallback(m.onStopped)
		}
		return event
	}
	point := m.api.eventLocation(event)
	if (kind == leftMouseDown || kind == rightMouseDown || kind == otherMouseDown) &&
		m.popup.Visible && !containsMac(m.popup, point) {
		m.queueCallback(m.onOutside)
	}
	switch kind {
	case rightMouseDown:
		if containsMac(m.popup, point) || containsMac(m.settings, point) {
			return event
		}
		copy := m.api.eventCopy(event)
		if copy == 0 {
			return event
		}
		m.clearBuffered()
		m.down = copy
		generation := m.gesture.begin()
		if m.timer != nil {
			m.timer.Stop()
		}
		m.timer = m.afterFunc(holdPeriod, func() {
			m.runOnMain(func() { m.fire(generation) })
		})
		return 0
	case rightMouseDragged:
		if !m.gesture.active {
			return event
		}
		if !m.gesture.triggered {
			if copy := m.api.eventCopy(event); copy != 0 {
				m.drags = append(m.drags, copy)
			}
		}
		return 0
	case rightMouseUp:
		if !m.gesture.active {
			return event
		}
		triggered := m.gesture.end()
		if m.timer != nil {
			m.timer.Stop()
			m.timer = nil
		}
		if triggered {
			m.clearBuffered()
			return 0
		}
		m.replayBuffered(proxy)
		return event
	default:
		return event
	}
}

func (m *macMonitor) fire(generation uint64) {
	if m.session.Load() != 0 && m.gesture.fire(generation) {
		m.queueCallback(m.onHold)
	}
}

func (m *macMonitor) replayBuffered(proxy uintptr) {
	if m.down != 0 {
		m.api.tapPostEvent(proxy, m.down)
	}
	for _, event := range m.drags {
		m.api.tapPostEvent(proxy, event)
	}
	m.clearBuffered()
}

func (m *macMonitor) clearBuffered() {
	if m.down != 0 {
		m.api.release(m.down)
	}
	for _, event := range m.drags {
		m.api.release(event)
	}
	m.down = 0
	m.drags = nil
}

func (m *macMonitor) finishInterrupted(proxy uintptr) {
	if m.gesture.triggered {
		m.clearBuffered()
	} else {
		m.replayBuffered(proxy)
	}
}

// Deliver notifications asynchronously and in the order they were observed,
// without holding the system event tap while the app updates its windows.
func (m *macMonitor) queueCallback(fn func()) {
	if fn == nil {
		return
	}
	session := m.session.Load()
	if session == 0 {
		return
	}
	m.callbackMu.Lock()
	m.callbacks = append(m.callbacks, queuedMacCallback{session: session, fn: fn})
	if !m.dispatching {
		m.dispatching = true
		go m.dispatchCallbacks()
	}
	m.callbackMu.Unlock()
}

func (m *macMonitor) dispatchCallbacks() {
	for {
		m.callbackMu.Lock()
		if len(m.callbacks) == 0 {
			m.dispatching = false
			m.callbackMu.Unlock()
			return
		}
		callback := m.callbacks[0]
		m.callbacks[0] = queuedMacCallback{}
		m.callbacks = m.callbacks[1:]
		m.callbackMu.Unlock()
		m.runOnMain(func() {
			if m.session.Load() == callback.session {
				callback.fn()
			}
		})
	}
}

var _ Monitor = (*macMonitor)(nil)
