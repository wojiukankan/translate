package selectionmonitor

// absoluteCoordinate maps a physical coordinate into SendInput's 0..65535
// virtual-desktop space, including monitors with negative origins.
func absoluteCoordinate(value, origin, span int32) int32 {
	if span <= 1 {
		return 0
	}
	offset := max(int64(0), min(int64(value)-int64(origin), int64(span)-1))
	return int32((offset*65535 + (int64(span)-1)/2) / (int64(span) - 1))
}
