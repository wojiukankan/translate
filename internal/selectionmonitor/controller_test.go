package selectionmonitor

import (
	"reflect"
	"testing"
	"time"

	"translate/modules/selection"
)

func TestHookSession(t *testing.T) {
	callbacks := make(chan string, 8)
	replayed := make(chan []point, 8)
	s := &hookSession{
		onHold:    func() { callbacks <- "hold" },
		onOutside: func() { callbacks <- "outside" },
		runOnMain: func(fn func()) { fn() },
		replay: func(points []point) error {
			replayed <- points
			return nil
		},
	}
	s.running.Store(true)
	t.Cleanup(func() {
		s.running.Store(false)
		s.mu.Lock()
		if s.timer != nil {
			s.timer.Stop()
		}
		s.mu.Unlock()
	})

	// Right-clicks on our own result and settings must reach those windows.
	s.popup = selection.Area{X: -200, Y: 20, Width: 100, Height: 100, Visible: true}
	s.settings = selection.Area{X: 100, Y: 20, Width: 100, Height: 100, Visible: true}
	if s.handle(wmRButtonDown, point{-190, 30}) || s.handle(wmRButtonUp, point{-190, 30}) {
		t.Fatal("right click inside the popup was intercepted")
	}
	if s.handle(wmRButtonDown, point{110, 30}) || s.handle(wmRButtonUp, point{110, 30}) {
		t.Fatal("right click inside settings was intercepted")
	}
	select {
	case got := <-callbacks:
		if got != "outside" {
			t.Fatalf("clicking settings yielded %q", got)
		}
	case <-time.After(time.Second):
		t.Fatal("click outside popup did not dismiss it")
	}

	// A short press delivers its original positions to the replay worker.
	s.mu.Lock()
	s.popup.Visible = false
	s.mu.Unlock()
	if !s.handle(wmRButtonDown, point{-500, 10}) || !s.handle(wmMouseMove, point{-490, 12}) || !s.handle(wmRButtonUp, point{-480, 15}) {
		t.Fatal("short right press was not intercepted")
	}
	select {
	case got := <-replayed:
		want := []point{{-500, 10}, {-490, 12}, {-480, 15}}
		if !reflect.DeepEqual(got, want) {
			t.Fatalf("replayed positions = %v, want %v", got, want)
		}
	case <-time.After(time.Second):
		t.Fatal("short right press was not replayed")
	}

	// Outside dismissal is delivered before the delayed hold callback.
	s.mu.Lock()
	s.popup.Visible = true
	s.mu.Unlock()
	if !s.handle(wmRButtonDown, point{500, 200}) {
		t.Fatal("long right press was not intercepted")
	}
	for _, want := range []string{"outside", "hold"} {
		select {
		case got := <-callbacks:
			if got != want {
				t.Fatalf("callback = %q, want %q", got, want)
			}
		case <-time.After(2 * time.Second):
			t.Fatalf("missing %q callback", want)
		}
	}
	if !s.handle(wmRButtonUp, point{500, 200}) {
		t.Fatal("long-press release was not intercepted")
	}
	select {
	case got := <-replayed:
		t.Fatalf("long press was replayed: %v", got)
	default:
	}
}
