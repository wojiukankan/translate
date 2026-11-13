package selection

import (
	"runtime"
	"sync"
	"time"
	"unsafe"

	"github.com/ebitengine/purego/objc"
)

var (
	selAlloc                      = objc.RegisterName("alloc")
	selInit                       = objc.RegisterName("init")
	selAutorelease                = objc.RegisterName("autorelease")
	selInitString                 = objc.RegisterName("initWithBytes:length:encoding:")
	selUTF8String                 = objc.RegisterName("UTF8String")
	selLengthOfBytesUsingEncoding = objc.RegisterName("lengthOfBytesUsingEncoding:")
	selGeneralPasteboard          = objc.RegisterName("generalPasteboard")
	selChangeCount                = objc.RegisterName("changeCount")
	selPasteboardItems            = objc.RegisterName("pasteboardItems")
	selTypes                      = objc.RegisterName("types")
	selCount                      = objc.RegisterName("count")
	selObjectAtIndex              = objc.RegisterName("objectAtIndex:")
	selDataForType                = objc.RegisterName("dataForType:")
	selStringForType              = objc.RegisterName("stringForType:")
	selLength                     = objc.RegisterName("length")
	selBytes                      = objc.RegisterName("bytes")
	selDataWithBytes              = objc.RegisterName("dataWithBytes:length:")
	selSetDataForType             = objc.RegisterName("setData:forType:")
	selArrayWithCapacity          = objc.RegisterName("arrayWithCapacity:")
	selAddObject                  = objc.RegisterName("addObject:")
	selClearContents              = objc.RegisterName("clearContents")
	selWriteObjects               = objc.RegisterName("writeObjects:")
)

const utf8Encoding = 4 // NSUTF8StringEncoding

func nsString(s string) objc.ID {
	str := objc.ID(objc.GetClass("NSString")).Send(selAlloc).Send(selInitString,
		uintptr(unsafe.Pointer(unsafe.StringData(s))), uintptr(len(s)), uintptr(utf8Encoding))
	runtime.KeepAlive(s)
	return str.Send(selAutorelease)
}

func goString(str objc.ID) string {
	if str == 0 {
		return ""
	}
	p := uintptr(str.Send(selUTF8String))
	n := int(str.Send(selLengthOfBytesUsingEncoding, uintptr(utf8Encoding)))
	if p == 0 || n == 0 {
		return ""
	}
	return string(unsafe.Slice(*(**byte)(unsafe.Pointer(&p)), n))
}

func nsData(data []byte) objc.ID {
	var p unsafe.Pointer
	if len(data) != 0 {
		p = unsafe.Pointer(&data[0])
	}
	value := objc.ID(objc.GetClass("NSData")).Send(selDataWithBytes, uintptr(p), uintptr(len(data)))
	runtime.KeepAlive(data)
	return value
}

func goData(data objc.ID) ([]byte, bool) {
	if data == 0 {
		return nil, false
	}
	n := int(data.Send(selLength))
	if n == 0 {
		return []byte{}, true
	}
	p := uintptr(data.Send(selBytes))
	if p == 0 {
		return nil, false
	}
	return append([]byte(nil), unsafe.Slice(*(**byte)(unsafe.Pointer(&p)), n)...), true
}

// A snapshot retains every native representation of every item. If any
// representation cannot be read, the new clipboard is left in place.
type clipboardSnapshot struct {
	changeCount int
	items       []map[string][]byte
}

type selectionClipboard interface {
	changeCount() int
	snapshot() *clipboardSnapshot
	text() string
	restore(*clipboardSnapshot, int)
}

type systemSelectionClipboard struct {
	api   *macAPI
	board objc.ID
}

func generalSelectionClipboard(api *macAPI) systemSelectionClipboard {
	var board objc.ID
	api.withPool(func() {
		board = objc.ID(objc.GetClass("NSPasteboard")).Send(selGeneralPasteboard)
	})
	return systemSelectionClipboard{api: api, board: board}
}

func (c systemSelectionClipboard) changeCount() int {
	var count int
	c.api.withPool(func() { count = int(c.board.Send(selChangeCount)) })
	return count
}

func (c systemSelectionClipboard) snapshot() *clipboardSnapshot {
	var snapshot *clipboardSnapshot
	c.api.withPool(func() {
		count := int(c.board.Send(selChangeCount))
		items := c.board.Send(selPasteboardItems)
		itemCount := int(items.Send(selCount))
		// A legacy clipboard may advertise types without accessible items.
		if itemCount == 0 && c.board.Send(selTypes).Send(selCount) != 0 {
			return
		}
		result := &clipboardSnapshot{changeCount: count}
		for i := 0; i < itemCount; i++ {
			item := items.Send(selObjectAtIndex, uintptr(i))
			types := item.Send(selTypes)
			representations := make(map[string][]byte)
			for j, n := 0, int(types.Send(selCount)); j < n; j++ {
				typ := types.Send(selObjectAtIndex, uintptr(j))
				data, ok := goData(item.Send(selDataForType, typ))
				if !ok {
					return
				}
				representations[goString(typ)] = data
			}
			result.items = append(result.items, representations)
		}
		if int(c.board.Send(selChangeCount)) == count {
			snapshot = result
		}
	})
	return snapshot
}

func (c systemSelectionClipboard) text() string {
	var text string
	c.api.withPool(func() {
		text = goString(c.board.Send(selStringForType, nsString("public.utf8-plain-text")))
	})
	return text
}

func (c systemSelectionClipboard) restore(snapshot *clipboardSnapshot, ifUnchanged int) {
	c.api.withPool(func() {
		items := objc.ID(objc.GetClass("NSMutableArray")).Send(selArrayWithCapacity, uintptr(len(snapshot.items)))
		for _, representations := range snapshot.items {
			item := objc.ID(objc.GetClass("NSPasteboardItem")).Send(selAlloc).Send(selInit).Send(selAutorelease)
			for typ, data := range representations {
				if !objc.Send[bool](item, selSetDataForType, nsData(data), nsString(typ)) {
					return
				}
			}
			items.Send(selAddObject, item)
		}
		if int(c.board.Send(selChangeCount)) != ifUnchanged {
			return
		}
		c.board.Send(selClearContents)
		if len(snapshot.items) != 0 {
			c.board.Send(selWriteObjects, items)
		}
	})
}

// Serializing copies prevents simultaneous requests from reading each other's
// clipboard update.
type selectionCopier struct{ mu sync.Mutex }

func (c *selectionCopier) copiedText(clipboard selectionClipboard, attempts int, pause func(), sendCopy func() bool) (string, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	snapshot := clipboard.snapshot()
	beforeCopy := clipboard.changeCount()
	if !sendCopy() {
		return "", ErrNoSelection
	}
	for attempt := 0; attempt < attempts; attempt++ {
		count := clipboard.changeCount()
		if count != beforeCopy {
			text := clipboard.text()
			if text != "" && clipboard.changeCount() == count {
				if snapshot != nil && snapshot.changeCount == beforeCopy {
					clipboard.restore(snapshot, count)
				}
				return text, nil
			}
		}
		if attempt+1 < attempts {
			pause()
		}
	}
	return "", ErrNoSelection
}

func (a *macAPI) postCopyShortcut() bool {
	const command = uint64(0x00100000) // kCGEventFlagMaskCommand
	source := a.eventSource(0)         // kCGEventSourceStateCombinedSessionState
	if source == 0 {
		return false
	}
	defer a.release(source)
	events := []uintptr{
		a.keyboardEvent(source, 55, true), // Command down
		a.keyboardEvent(source, 8, true),  // C down
		a.keyboardEvent(source, 8, false), // C up
		a.keyboardEvent(source, 55, false),
	}
	valid := true
	for _, event := range events {
		if event == 0 {
			valid = false
		} else {
			defer a.release(event)
		}
	}
	if !valid {
		return false
	}
	for i, event := range events {
		if i < 3 {
			a.setEventFlags(event, command)
		} else {
			a.setEventFlags(event, 0)
		}
		a.eventPost(0, event) // kCGHIDEventTap
	}
	return true
}

func waitForMacCopy() { time.Sleep(20 * time.Millisecond) }
