package selectionmonitor

import "translate/modules/selection"

// New uses the macOS implementation of the shared monitor contract.
func New() (selection.Monitor, error) { return selection.NewMonitor() }
