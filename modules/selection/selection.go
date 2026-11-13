// Package selection defines the selection and mouse gesture bridge.
package selection

import "github.com/egoist/mygo/native"

var (
	ErrNoSelection  = native.NewError("no_selection")
	ErrNoPermission = native.NewError("no_permission")
	ErrMonitor      = native.NewError("monitor")
)

// Area is a window's bounds in the platform's mouse-event screen coordinates:
// points on macOS and physical pixels on Windows.
type Area struct {
	X, Y, Width, Height int
	Visible             bool
}

// Monitor observes the right-button gesture and obtains the current selection.
//
//mygo:native
type Monitor interface {
	// Start begins monitoring until Stop. Callbacks are delivered to the UI thread.
	//mygo:mainthread
	Start(onHold func(), onOutside func(), onStopped func()) error

	// SetAreas identifies our windows so their own right-clicks pass through.
	//mygo:mainthread
	SetAreas(popup Area, settings Area)

	// Stop removes the system event tap.
	//mygo:mainthread
	Stop()

	// SelectedText copies and returns the focused application's selected text.
	SelectedText() (string, error)
}
