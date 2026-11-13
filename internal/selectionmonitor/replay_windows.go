package selectionmonitor

import (
	"fmt"
	"syscall"
	"unsafe"
)

const (
	inputMouse                = 0
	mouseeventfMove           = 0x0001
	mouseeventfRightDown      = 0x0008
	mouseeventfRightUp        = 0x0010
	mouseeventfMoveNoCoalesce = 0x2000
	mouseeventfVirtualDesk    = 0x4000
	mouseeventfAbsolute       = 0x8000
	smXVirtualScreen          = 76
	smYVirtualScreen          = 77
	smCXVirtualScreen         = 78
	smCYVirtualScreen         = 79
)

var (
	procSendInput        = user32.NewProc("SendInput")
	procGetSystemMetrics = user32.NewProc("GetSystemMetrics")
)

func screenMetric(metric uintptr) int32 {
	v, _, _ := procGetSystemMetrics.Call(metric)
	return int32(v)
}

// replayMouse sends a short right click and any buffered drag at the original
// screen positions. Injected events are marked so our own hook passes them on.
func replayMouse(points []point) error {
	if len(points) < 2 {
		return nil
	}
	x, y := screenMetric(smXVirtualScreen), screenMetric(smYVirtualScreen)
	w, h := screenMetric(smCXVirtualScreen), screenMetric(smCYVirtualScreen)
	events := make([]input, 0, len(points)+2)
	for i, p := range points {
		events = append(events, input{mouse: mouseInput{
			dx: absoluteCoordinate(p.x, x, w), dy: absoluteCoordinate(p.y, y, h),
			flags:     mouseeventfMove | mouseeventfAbsolute | mouseeventfVirtualDesk | mouseeventfMoveNoCoalesce,
			extraInfo: replayMarker,
		}})
		if i == 0 {
			events = append(events, input{mouse: mouseInput{flags: mouseeventfRightDown, extraInfo: replayMarker}})
		}
	}
	events = append(events, input{mouse: mouseInput{flags: mouseeventfRightUp, extraInfo: replayMarker}})
	count, _, callErr := procSendInput.Call(uintptr(len(events)), uintptr(unsafe.Pointer(&events[0])), unsafe.Sizeof(events[0]))
	if count != uintptr(len(events)) {
		if callErr != syscall.Errno(0) {
			return fmt.Errorf("回放右键事件失败：%w", callErr)
		}
		return fmt.Errorf("回放右键事件失败")
	}
	return nil
}
