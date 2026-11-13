package selectionmonitor

import "unsafe"

// Windows SDK structures used by the low-level mouse hook and SendInput.
// These definitions have no platform-specific calls so their alignment can be
// checked even when the application is cross-compiled.
type mouseHookData struct {
	point     point
	mouseData uint32
	flags     uint32
	time      uint32
	extraInfo uintptr
}

type winMessage struct {
	hwnd    uintptr
	message uint32
	wParam  uintptr
	lParam  uintptr
	time    uint32
	point   point
	private uint32
}

type mouseInput struct {
	dx, dy    int32
	mouseData uint32
	flags     uint32
	time      uint32
	extraInfo uintptr
}

type keyboardInput struct {
	virtualKey uint16
	scan       uint16
	flags      uint32
	time       uint32
	extraInfo  uintptr
}

// INPUT's union is as large and aligned as MOUSEINPUT on Windows.
type input struct {
	typeID uint32
	_      [unsafe.Sizeof(uintptr(0)) - 4]byte
	mouse  mouseInput
}
