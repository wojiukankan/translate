//go:build darwin

package selection

import (
	"errors"
	"reflect"
	"sync"
	"testing"
	"time"

	"github.com/ebitengine/purego"
	"github.com/ebitengine/purego/objc"
)

func TestGestureState(t *testing.T) {
	var shortPress gestureState
	shortTimer := shortPress.begin()
	if shortPress.end() || shortPress.fire(shortTimer) {
		t.Fatal("a short press must be replayed and cannot trigger later")
	}

	var longPress gestureState
	longTimer := longPress.begin()
	if !longPress.fire(longTimer) || longPress.fire(longTimer) || !longPress.end() {
		t.Fatal("a long press must trigger once and must not be replayed")
	}

	var successive gestureState
	oldTimer := successive.begin()
	successive.end()
	newTimer := successive.begin()
	if successive.fire(oldTimer) || !successive.fire(newTimer) || !successive.end() {
		t.Fatal("an old timer affected the next press")
	}
}

type fakeClipboard struct {
	count, reads, restorations int
	value                      string
	snapshotAvailable          bool
	changeBeforeRestore        bool
}

func newFakeClipboard() *fakeClipboard {
	return &fakeClipboard{count: 1, value: "old clipboard", snapshotAvailable: true}
}

func (f *fakeClipboard) changeCount() int { return f.count }
func (f *fakeClipboard) snapshot() *clipboardSnapshot {
	if !f.snapshotAvailable {
		return nil
	}
	return &clipboardSnapshot{changeCount: f.count, items: []map[string][]byte{{"text": []byte(f.value)}}}
}
func (f *fakeClipboard) text() string { f.reads++; return f.value }
func (f *fakeClipboard) restore(snapshot *clipboardSnapshot, ifUnchanged int) {
	if f.changeBeforeRestore {
		f.count++
		f.value = "newer user copy"
	}
	if f.count == ifUnchanged {
		f.restorations++
		f.value = string(snapshot.items[0]["text"])
		f.count++
	}
}

// These cases replace the Swift CopyChecks run by TestGestureState before
// the macOS implementation moved to Go.
func TestCopyChecks(t *testing.T) {
	var copier selectionCopier
	t.Run("unchanged clipboard", func(t *testing.T) {
		board := newFakeClipboard()
		_, err := copier.copiedText(board, 3, func() {}, func() bool { return true })
		if !errors.Is(err, ErrNoSelection) || board.reads != 0 {
			t.Fatalf("error = %v, clipboard reads = %d", err, board.reads)
		}
	})
	t.Run("delayed copy restores original", func(t *testing.T) {
		board := newFakeClipboard()
		text, err := copier.copiedText(board, 3, func() {
			board.count++
			board.value = "selected text"
		}, func() bool { return true })
		if err != nil || text != "selected text" || board.value != "old clipboard" || board.restorations != 1 {
			t.Fatalf("text = %q, error = %v, clipboard = %+v", text, err, board)
		}
	})
	t.Run("no snapshot leaves selection", func(t *testing.T) {
		board := newFakeClipboard()
		board.snapshotAvailable = false
		text, err := copier.copiedText(board, 1, func() {}, func() bool {
			board.count++
			board.value = "selection"
			return true
		})
		if err != nil || text != "selection" || board.value != "selection" || board.restorations != 0 {
			t.Fatalf("text = %q, error = %v, clipboard = %+v", text, err, board)
		}
	})
	t.Run("newer user copy is not overwritten", func(t *testing.T) {
		board := newFakeClipboard()
		board.changeBeforeRestore = true
		text, err := copier.copiedText(board, 1, func() {}, func() bool {
			board.count++
			board.value = "selection"
			return true
		})
		if err != nil || text != "selection" || board.value != "newer user copy" || board.restorations != 0 {
			t.Fatalf("text = %q, error = %v, clipboard = %+v", text, err, board)
		}
	})
	t.Run("copy failure", func(t *testing.T) {
		board := newFakeClipboard()
		_, err := copier.copiedText(board, 1, func() {}, func() bool { return false })
		if !errors.Is(err, ErrNoSelection) || board.value != "old clipboard" || board.reads != 0 {
			t.Fatalf("error = %v, clipboard = %+v", err, board)
		}
	})
	t.Run("copy requests are serialized", func(t *testing.T) {
		firstStarted, releaseFirst := make(chan struct{}), make(chan struct{})
		secondStarted := make(chan struct{})
		var done sync.WaitGroup
		done.Add(2)
		go func() {
			defer done.Done()
			board := newFakeClipboard()
			_, _ = copier.copiedText(board, 1, func() {}, func() bool {
				close(firstStarted)
				<-releaseFirst
				board.count++
				board.value = "first"
				return true
			})
		}()
		<-firstStarted
		go func() {
			defer done.Done()
			board := newFakeClipboard()
			_, _ = copier.copiedText(board, 1, func() {}, func() bool {
				close(secondStarted)
				board.count++
				board.value = "second"
				return true
			})
		}()
		select {
		case <-secondStarted:
			t.Error("second copy started before the first finished")
		case <-time.After(50 * time.Millisecond):
		}
		close(releaseFirst)
		done.Wait()
		select {
		case <-secondStarted:
		default:
			t.Fatal("second copy never started")
		}
	})
}

// A private named pasteboard exercises native multi-item and multi-format
// snapshots without changing the user's clipboard.
func TestCopyChecksNamedPasteboard(t *testing.T) {
	api, err := loadMacAPI()
	if err != nil {
		t.Fatal(err)
	}
	name := t.TempDir()
	var board objc.ID
	api.withPool(func() {
		board = objc.ID(objc.GetClass("NSPasteboard")).Send(objc.RegisterName("pasteboardWithName:"), nsString(name)).Send(objc.RegisterName("retain"))
	})
	t.Cleanup(func() {
		api.withPool(func() {
			board.Send(objc.RegisterName("releaseGlobally"))
			board.Send(objc.RegisterName("release"))
		})
	})
	clipboard := systemSelectionClipboard{api: api, board: board}
	original := &clipboardSnapshot{items: []map[string][]byte{
		{"public.utf8-plain-text": []byte("original"), "com.example.translate.test": {0, 1, 2, 3}},
		{"public.utf8-plain-text": []byte("second item")},
	}}
	clipboard.restore(original, clipboard.changeCount())
	before := clipboard.snapshot()
	if before == nil || !reflect.DeepEqual(before.items, original.items) {
		t.Fatalf("initial snapshot = %#v", before)
	}
	setText := func(text string) bool {
		var ok bool
		api.withPool(func() {
			board.Send(selClearContents)
			ok = objc.Send[bool](board, objc.RegisterName("setString:forType:"), nsString(text), nsString("public.utf8-plain-text"))
		})
		return ok
	}
	var copier selectionCopier
	text, err := copier.copiedText(clipboard, 2, func() {}, func() bool { return setText("copied text") })
	if err != nil || text != "copied text" {
		t.Fatalf("text = %q, error = %v", text, err)
	}
	if after := clipboard.snapshot(); after == nil || !reflect.DeepEqual(after.items, before.items) {
		t.Fatalf("restored snapshot = %#v, want %#v", after, before)
	}
	if !setText("selection") {
		t.Fatal("could not write selection to named pasteboard")
	}
	selectionCount := clipboard.changeCount()
	if !setText("newer clipboard") {
		t.Fatal("could not write newer text to named pasteboard")
	}
	clipboard.restore(before, selectionCount)
	if got := clipboard.text(); got != "newer clipboard" {
		t.Fatalf("newer clipboard = %q", got)
	}
}

func TestMacMonitorEventReplay(t *testing.T) {
	var replayed, released []uintptr
	var reenabled int
	api := &macAPI{
		eventLocation: func(event uintptr) cgPoint { return cgPoint{X: float64(event), Y: 10} },
		eventCopy:     func(event uintptr) uintptr { return event + 100 },
		tapPostEvent:  func(_ uintptr, event uintptr) { replayed = append(replayed, event) },
		release:       func(event uintptr) { released = append(released, event) },
		tapEnable:     func(uintptr, bool) { reenabled++ },
	}
	holds := make(chan struct{}, 1)
	outside := make(chan struct{}, 1)
	stopped := make(chan struct{}, 1)
	m := &macMonitor{
		api: api, runOnMain: func(fn func()) { fn() },
		afterFunc: func(time.Duration, func()) *time.Timer { return nil },
		onHold:    func() { holds <- struct{}{} }, onOutside: func() { outside <- struct{}{} },
		onStopped: func() { stopped <- struct{}{} }, tap: 99,
	}
	m.session.Store(1)
	t.Cleanup(func() {
		m.session.Store(0)
		if m.timer != nil {
			m.timer.Stop()
		}
	})
	if m.handleEvent(1, rightMouseDown, 10) != 0 || m.handleEvent(1, rightMouseDragged, 11) != 0 || m.handleEvent(1, rightMouseUp, 12) != 12 {
		t.Fatal("short press did not suppress down and drag, then pass up")
	}
	if !reflect.DeepEqual(replayed, []uintptr{110, 111}) || !reflect.DeepEqual(released, replayed) {
		t.Fatalf("replayed = %v, released = %v", replayed, released)
	}
	replayed, released = nil, nil
	if m.handleEvent(1, rightMouseDown, 20) != 0 {
		t.Fatal("long press down was not intercepted")
	}
	if m.timer != nil {
		m.timer.Stop()
	}
	m.fire(m.gesture.generation)
	select {
	case <-holds:
	case <-time.After(time.Second):
		t.Fatal("long press did not trigger")
	}
	if m.handleEvent(1, rightMouseUp, 21) != 0 || len(replayed) != 0 || !reflect.DeepEqual(released, []uintptr{120}) {
		t.Fatalf("long press replayed = %v, released = %v", replayed, released)
	}

	m.popup = Area{X: 30, Y: 0, Width: 10, Height: 20, Visible: true}
	if m.handleEvent(1, rightMouseDown, 30) != 30 || m.handleEvent(1, rightMouseUp, 31) != 31 {
		t.Fatal("right click inside popup was intercepted")
	}
	if m.handleEvent(1, leftMouseDown, 50) != 50 {
		t.Fatal("outside click was intercepted")
	}
	select {
	case <-outside:
	case <-time.After(time.Second):
		t.Fatal("outside click did not dismiss popup")
	}
	m.popup.Visible = false
	replayed, released = nil, nil
	m.handleEvent(1, rightMouseDown, 40)
	m.handleEvent(1, rightMouseDragged, 41)
	m.handleEvent(1, tapDisabledTimeout, 0)
	if !reflect.DeepEqual(replayed, []uintptr{140, 141}) || !reflect.DeepEqual(released, replayed) || reenabled != 1 || m.gesture.active {
		t.Fatalf("interrupted replay = %v, released = %v, reenables = %d", replayed, released, reenabled)
	}
	m.handleEvent(1, tapDisabledInput, 0)
	select {
	case <-stopped:
	case <-time.After(time.Second):
		t.Fatal("user-disabled tap did not notify the app")
	}
}

func TestMacCopyShortcutEvents(t *testing.T) {
	type keyEvent struct {
		key  uint16
		down bool
	}
	var keys []keyEvent
	var flags []uint64
	var posted, released []uintptr
	api := &macAPI{
		eventSource: func(int32) uintptr { return 1 },
		keyboardEvent: func(_ uintptr, key uint16, down bool) uintptr {
			keys = append(keys, keyEvent{key: key, down: down})
			return uintptr(len(keys) + 1)
		},
		setEventFlags: func(_ uintptr, flag uint64) { flags = append(flags, flag) },
		eventPost:     func(_ uint32, event uintptr) { posted = append(posted, event) },
		release:       func(event uintptr) { released = append(released, event) },
	}
	if !api.postCopyShortcut() {
		t.Fatal("copy shortcut was not posted")
	}
	if !reflect.DeepEqual(keys, []keyEvent{{55, true}, {8, true}, {8, false}, {55, false}}) ||
		!reflect.DeepEqual(flags, []uint64{0x100000, 0x100000, 0x100000, 0}) ||
		!reflect.DeepEqual(posted, []uintptr{2, 3, 4, 5}) || len(released) != 5 {
		t.Fatalf("keys = %v, flags = %v, posted = %v, released = %v", keys, flags, posted, released)
	}
}

// Create and copy a CoreGraphics event without posting it to the user session.
func TestMacCoreGraphicsEvent(t *testing.T) {
	api, err := loadMacAPI()
	if err != nil {
		t.Fatal(err)
	}
	lib, err := purego.Dlopen("/System/Library/Frameworks/CoreGraphics.framework/CoreGraphics", purego.RTLD_NOW|purego.RTLD_GLOBAL)
	if err != nil {
		t.Fatal(err)
	}
	var createMouse func(uintptr, uint32, cgPoint, uint32) uintptr
	purego.RegisterLibFunc(&createMouse, lib, "CGEventCreateMouseEvent")
	want := cgPoint{X: -123.5, Y: 54.25}
	event := createMouse(0, rightMouseDown, want, 1)
	if event == 0 {
		t.Fatal("could not create a CoreGraphics mouse event")
	}
	defer api.release(event)
	copy := api.eventCopy(event)
	if copy == 0 {
		t.Fatal("could not copy a CoreGraphics mouse event")
	}
	defer api.release(copy)
	if got := api.eventLocation(copy); got != want {
		t.Fatalf("copied event position = %v, want %v", got, want)
	}

	// Exercise the C callback and original-event replay with real CGEvent
	// objects, but replace the posting function so no input reaches the user.
	var replayed []cgPoint
	testAPI := *api
	testAPI.tapPostEvent = func(_ uintptr, event uintptr) {
		replayed = append(replayed, api.eventLocation(event))
	}
	m := &macMonitor{
		api: &testAPI, runOnMain: func(fn func()) { fn() },
		afterFunc: func(time.Duration, func()) *time.Timer { return nil },
	}
	m.session.Store(1)
	const handle = uintptr(12345)
	macMonitors.Store(handle, m)
	defer macMonitors.Delete(handle)
	var callback func(uintptr, uint32, uintptr, uintptr) uintptr
	purego.RegisterFunc(&callback, macTapCallback)
	if got := callback(0, rightMouseDown, event, handle); got != 0 {
		t.Fatalf("down returned %v, want suppression", got)
	}
	if got := callback(0, rightMouseUp, event, handle); got != event {
		t.Fatalf("up returned %v, want original event", got)
	}
	if !reflect.DeepEqual(replayed, []cgPoint{want}) {
		t.Fatalf("replayed event locations = %v", replayed)
	}

	source := api.eventSource(0)
	if source == 0 {
		t.Fatal("could not create a CoreGraphics keyboard source")
	}
	defer api.release(source)
	key := api.keyboardEvent(source, 8, true)
	if key == 0 {
		t.Fatal("could not create a CoreGraphics keyboard event")
	}
	defer api.release(key)
	api.setEventFlags(key, 0x100000)
	var getFlags func(uintptr) uint64
	var getInteger func(uintptr, uint32) int64
	purego.RegisterLibFunc(&getFlags, lib, "CGEventGetFlags")
	purego.RegisterLibFunc(&getInteger, lib, "CGEventGetIntegerValueField")
	if got := getFlags(key); got&0x100000 == 0 {
		t.Fatalf("keyboard flags = %#x, want Command", got)
	}
	if got := getInteger(key, 9); got != 8 { // kCGKeyboardEventKeycode
		t.Fatalf("keyboard keycode = %d, want C", got)
	}
}

func TestMacEventTapAvailability(t *testing.T) {
	api, err := loadMacAPI()
	if err != nil {
		t.Fatal(err)
	}
	if !api.trusted() {
		t.Skip("event tap requires macOS Accessibility permission")
	}
	// Context 0 has no monitor, so any event is passed through untouched.
	tap := api.tapCreate(1, 0, 0, uint64(1<<rightMouseDown|1<<rightMouseUp), macTapCallback, 0)
	if tap == 0 {
		t.Fatal("could not create a macOS event tap")
	}
	defer api.release(tap)
	defer api.portInvalidate(tap)
	source := api.portSource(0, tap, 0)
	if source == 0 {
		t.Fatal("could not create the tap's run-loop source")
	}
	defer api.release(source)
	loop := api.runLoopMain()
	api.runLoopAdd(loop, source, api.commonModes)
	api.tapEnable(tap, true)
	api.runLoopRemove(loop, source, api.commonModes)
}
