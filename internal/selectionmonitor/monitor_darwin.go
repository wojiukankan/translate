package selectionmonitor

import "translate/modules/selection"

// New retains the existing macOS native module.
func New() (selection.Monitor, error) { return selection.NewMonitor() }
