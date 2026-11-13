//go:build darwin

package selection

import (
	"os/exec"
	"path/filepath"
	"testing"
)

func TestGestureState(t *testing.T) {
	binary := filepath.Join(t.TempDir(), "gesture-checks")
	cmd := exec.Command("swiftc", "swift/Sources/Selection/Gesture.swift", "swift/Sources/Selection/CopySelection.swift", "swift/Tests/GestureChecks.swift", "swift/Tests/CopyChecks.swift", "-o", binary)
	if output, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("compile gesture checks: %v\n%s", err, output)
	}
	if output, err := exec.Command(binary).CombinedOutput(); err != nil {
		t.Fatalf("gesture checks: %v\n%s", err, output)
	}
}
