package selectionmonitor

import (
	"fmt"
	"runtime"
	"syscall"
	"unicode/utf16"
	"unsafe"

	"translate/modules/selection"
)

const (
	inputKeyboard      = 1
	keyboardEventKeyUp = 0x0002
	vkControl          = 0x11
	vkC                = 0x43
	cfUnicodeText      = 13
)

var (
	procGetClipboardSequenceNumber = user32.NewProc("GetClipboardSequenceNumber")
	procOpenClipboard              = user32.NewProc("OpenClipboard")
	procCloseClipboard             = user32.NewProc("CloseClipboard")
	procGetClipboardData           = user32.NewProc("GetClipboardData")
	procGetAsyncKeyState           = user32.NewProc("GetAsyncKeyState")
	procGlobalLock                 = kernel32.NewProc("GlobalLock")
	procGlobalUnlock               = kernel32.NewProc("GlobalUnlock")
	procGlobalSize                 = kernel32.NewProc("GlobalSize")
)

type windowsCopySource struct{}

func clipboardSequence() uint32 {
	seq, _, _ := procGetClipboardSequenceNumber.Call()
	return uint32(seq)
}

func (windowsCopySource) prepare() (uint32, bool) {
	// Materialize any existing delayed text before taking the baseline.
	// GetClipboardData does not read or decode the old clipboard content.
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	opened, _, _ := procOpenClipboard.Call(0)
	if opened == 0 {
		return 0, false
	}
	defer procCloseClipboard.Call()
	procGetClipboardData.Call(cfUnicodeText)
	return clipboardSequence(), true
}

func keyInput(key uint16, flags uint32) input {
	event := input{typeID: inputKeyboard}
	*(*keyboardInput)(unsafe.Pointer(&event.mouse)) = keyboardInput{
		virtualKey: key, flags: flags, extraInfo: replayMarker,
	}
	return event
}

func (windowsCopySource) copy() error {
	// Do not synthesize a Ctrl release if the user already holds Ctrl.
	state, _, _ := procGetAsyncKeyState.Call(vkControl)
	ownsControl := uint16(state)&0x8000 == 0
	var events []input
	if ownsControl {
		events = append(events, keyInput(vkControl, 0))
	}
	events = append(events, keyInput(vkC, 0), keyInput(vkC, keyboardEventKeyUp))
	if ownsControl {
		events = append(events, keyInput(vkControl, keyboardEventKeyUp))
	}
	count, _, err := procSendInput.Call(uintptr(len(events)), uintptr(unsafe.Pointer(&events[0])), unsafe.Sizeof(events[0]))
	if count == uintptr(len(events)) {
		return nil
	}
	// A partial SendInput can leave an injected key held down.
	releases := []input{keyInput(vkC, keyboardEventKeyUp)}
	if ownsControl {
		releases = append(releases, keyInput(vkControl, keyboardEventKeyUp))
	}
	procSendInput.Call(uintptr(len(releases)), uintptr(unsafe.Pointer(&releases[0])), unsafe.Sizeof(releases[0]))
	if err != syscall.Errno(0) {
		return fmt.Errorf("模拟 Ctrl+C 失败：%w", err)
	}
	return fmt.Errorf("模拟 Ctrl+C 失败；请检查目标应用的权限")
}

// GlobalLock returns an address in Windows-managed memory, not a Go pointer.
func clipboardPointer(addr uintptr) unsafe.Pointer {
	return *(*unsafe.Pointer)(unsafe.Pointer(&addr))
}

func (windowsCopySource) textAfter(before uint32) (string, bool) {
	// OpenClipboard and CloseClipboard run on the same OS thread. Holding the
	// clipboard open also keeps its owner and text consistent during the read.
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	opened, _, _ := procOpenClipboard.Call(0)
	if opened == 0 {
		return "", false
	}
	defer procCloseClipboard.Call()
	// Rendering a delayed copy can itself increment the sequence number, so
	// request the text before comparing the generation. The old text was
	// already materialized in prepare and is never decoded if unchanged.
	handle, _, _ := procGetClipboardData.Call(cfUnicodeText)
	if handle == 0 || clipboardSequence() == before {
		return "", false
	}
	size, _, _ := procGlobalSize.Call(handle)
	if size < 2 {
		return "", false
	}
	ptr, _, _ := procGlobalLock.Call(handle)
	if ptr == 0 {
		return "", false
	}
	defer procGlobalUnlock.Call(handle)
	chars := unsafe.Slice((*uint16)(clipboardPointer(ptr)), int(size/2))
	end := 0
	for end < len(chars) && chars[end] != 0 {
		end++
	}
	return string(utf16.Decode(chars[:end])), true
}

// SelectedText asks the active application to copy its selection. Windows
// clipboard formats can have delayed providers and non-memory handles, so we
// leave the new clipboard intact rather than partially restoring the old one.
func (m *windowsMonitor) SelectedText() (string, error) {
	return m.copier.copiedSelection(windowsCopySource{}, 100, waitForCopy)
}

var _ selection.Monitor = (*windowsMonitor)(nil)
