package selectionmonitor

// point is a physical Windows screen coordinate, including negative values
// on monitors to the left or above the primary monitor.
type point struct{ x, y int32 }

// gesture keeps the right-button events until the hold either fires or is
// released. Its caller serializes access and schedules the hold timer.
type gesture struct {
	active, triggered bool
	generation        uint64
	points            []point
}

func (g *gesture) begin(p point) uint64 {
	g.generation++
	g.active, g.triggered = true, false
	g.points = append(g.points[:0], p)
	return g.generation
}

func (g *gesture) move(p point) bool {
	if !g.active {
		return false
	}
	if !g.triggered {
		g.points = append(g.points, p)
	}
	return true
}

func (g *gesture) fire(generation uint64) bool {
	if !g.active || g.triggered || g.generation != generation {
		return false
	}
	g.triggered = true
	return true
}

// end returns whether the release was intercepted and, for a short press,
// its down, movement and up positions in order.
func (g *gesture) end(p point) (bool, []point) {
	if !g.active {
		return false, nil
	}
	var replay []point
	if !g.triggered {
		replay = append(replay, g.points...)
		replay = append(replay, p)
	}
	g.reset()
	return true, replay
}

func (g *gesture) reset() {
	g.generation++
	g.active, g.triggered = false, false
	g.points = nil
}
