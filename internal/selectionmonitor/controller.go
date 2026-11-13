package selectionmonitor

import (
	"sync"
	"sync/atomic"
	"time"

	"translate/modules/selection"
)

const (
	wmMouseMove     = 0x0200
	wmLButtonDown   = 0x0201
	wmRButtonDown   = 0x0204
	wmRButtonUp     = 0x0205
	wmMButtonDown   = 0x0207
	wmXButtonDown   = 0x020B
	mouseHoldPeriod = 500 * time.Millisecond
)

type hookSession struct {
	running  atomic.Bool
	stopping atomic.Bool
	threadID uint32
	done     chan struct{}

	mu          sync.Mutex
	exited      bool
	popup       selection.Area
	settings    selection.Area
	gesture     gesture
	timer       *time.Timer
	onHold      func()
	onOutside   func()
	onStopped   func()
	runOnMain   func(func())
	replay      func([]point) error
	callbacks   []func()
	dispatching bool
	replays     [][]point
	replaying   bool
}

func contains(area selection.Area, p point) bool {
	return area.Visible && int(p.x) >= area.X && int(p.y) >= area.Y &&
		int(p.x) < area.X+area.Width && int(p.y) < area.Y+area.Height
}

// handle returns true when Windows should suppress the original event.
func (s *hookSession) handle(message uint32, p point) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.running.Load() {
		return false
	}
	if message == wmLButtonDown || message == wmRButtonDown || message == wmMButtonDown || message == wmXButtonDown {
		if s.popup.Visible && !contains(s.popup, p) {
			s.queueCallbackLocked(s.onOutside)
		}
	}
	switch message {
	case wmRButtonDown:
		if contains(s.popup, p) || contains(s.settings, p) {
			return false
		}
		generation := s.gesture.begin(p)
		if s.timer != nil {
			s.timer.Stop()
		}
		s.timer = time.AfterFunc(mouseHoldPeriod, func() { s.fire(generation) })
		return true
	case wmMouseMove:
		return s.gesture.move(p)
	case wmRButtonUp:
		intercepted, replay := s.gesture.end(p)
		if intercepted && s.timer != nil {
			s.timer.Stop()
			s.timer = nil
		}
		if len(replay) > 0 {
			s.queueReplayLocked(replay)
		}
		return intercepted
	default:
		return false
	}
}

func (s *hookSession) fire(generation uint64) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.running.Load() && s.gesture.fire(generation) {
		s.queueCallbackLocked(s.onHold)
	}
}

// The FIFO dispatcher preserves the order of outside and hold callbacks while
// keeping the Windows hook thread independent of the MyGo UI thread.
func (s *hookSession) queueCallbackLocked(fn func()) {
	if fn == nil {
		return
	}
	s.callbacks = append(s.callbacks, fn)
	if !s.dispatching {
		s.dispatching = true
		go s.dispatchCallbacks()
	}
}

func (s *hookSession) dispatchCallbacks() {
	for {
		s.mu.Lock()
		if !s.running.Load() || len(s.callbacks) == 0 {
			s.dispatching = false
			s.mu.Unlock()
			return
		}
		fn := s.callbacks[0]
		s.callbacks[0] = nil
		s.callbacks = s.callbacks[1:]
		s.mu.Unlock()
		s.runOnMain(func() {
			if s.running.Load() {
				fn()
			}
		})
	}
}

func (s *hookSession) queueReplayLocked(points []point) {
	s.replays = append(s.replays, points)
	if !s.replaying {
		s.replaying = true
		go s.replayClicks()
	}
}

func (s *hookSession) replayClicks() {
	for {
		s.mu.Lock()
		if len(s.replays) == 0 {
			s.replaying = false
			s.mu.Unlock()
			return
		}
		points := s.replays[0]
		s.replays[0] = nil
		s.replays = s.replays[1:]
		s.mu.Unlock()
		if err := s.replay(points); err != nil && s.running.Load() && s.onStopped != nil {
			go s.runOnMain(s.onStopped)
			return
		}
	}
}
