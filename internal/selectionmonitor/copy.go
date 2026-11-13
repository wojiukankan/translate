package selectionmonitor

import (
	"sync"
	"time"

	"translate/modules/selection"
)

// copySource only returns text from a clipboard generation newer than before.
type copySource interface {
	prepare() (uint32, bool)
	copy() error
	textAfter(before uint32) (string, bool)
}

type copyCoordinator struct{ mu sync.Mutex }

func (c *copyCoordinator) copiedSelection(source copySource, attempts int, pause func()) (string, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	var before uint32
	prepared := false
	for attempt := 0; attempt < attempts; attempt++ {
		if before, prepared = source.prepare(); prepared {
			break
		}
		if attempt+1 < attempts {
			pause()
		}
	}
	if !prepared {
		return "", selection.ErrNoSelection
	}
	if err := source.copy(); err != nil {
		return "", err
	}
	for attempt := 0; attempt < attempts; attempt++ {
		if text, ok := source.textAfter(before); ok && text != "" {
			return text, nil
		}
		if attempt+1 < attempts {
			pause()
		}
	}
	return "", selection.ErrNoSelection
}

func waitForCopy() { time.Sleep(20 * time.Millisecond) }
