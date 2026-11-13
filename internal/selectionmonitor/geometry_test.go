package selectionmonitor

import "testing"

func TestAbsoluteCoordinate(t *testing.T) {
	for _, tc := range []struct {
		value, origin, span, want int32
	}{
		{-1920, -1920, 3840, 0},
		{1919, -1920, 3840, 65535},
		{-2000, -1920, 3840, 0},
		{2000, -1920, 3840, 65535},
		{0, -1, 3, 32768},
		{10, 10, 1, 0},
	} {
		if got := absoluteCoordinate(tc.value, tc.origin, tc.span); got != tc.want {
			t.Errorf("coordinate %d in [%d, %d): got %d, want %d", tc.value, tc.origin, tc.origin+tc.span, got, tc.want)
		}
	}
}
