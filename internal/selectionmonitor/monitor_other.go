//go:build !darwin && !windows

package selectionmonitor

import (
	"errors"

	"translate/modules/selection"
)

func New() (selection.Monitor, error) { return nil, errors.ErrUnsupported }
