package main

import (
	"unsafe"

	"github.com/egoist/mygo"
	"golang.org/x/sys/windows"
)

var procSelectionGetWindowRect = windows.NewLazySystemDLL("user32.dll").NewProc("GetWindowRect")

// Low-level Windows mouse hooks report physical pixels, while MyGo's public
// Bounds use DIPs. Use the real window rectangle for the hook's hit testing.
func selectionWindowBounds(w *mygo.Window) mygo.Rectangle {
	var rect struct{ left, top, right, bottom int32 }
	ok, _, _ := procSelectionGetWindowRect.Call(w.NativeHandle(), uintptr(unsafe.Pointer(&rect)))
	if ok == 0 {
		return mygo.Rectangle{}
	}
	return mygo.Rectangle{X: int(rect.left), Y: int(rect.top), Width: int(rect.right - rect.left), Height: int(rect.bottom - rect.top)}
}
