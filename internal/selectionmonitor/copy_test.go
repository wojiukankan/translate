package selectionmonitor

import (
	"errors"
	"testing"
	"time"

	"translate/modules/selection"
)

type fakeCopySource struct {
	seq    uint32
	text   string
	err    error
	onCopy func()
	reads  int
	onPoll func()
}

func (f *fakeCopySource) prepare() (uint32, bool) { return f.seq, true }
func (f *fakeCopySource) copy() error {
	if f.onCopy != nil {
		f.onCopy()
	}
	return f.err
}
func (f *fakeCopySource) textAfter(before uint32) (string, bool) {
	if f.onPoll != nil {
		f.onPoll()
	}
	if f.seq == before {
		return "", false
	}
	f.reads++
	return f.text, true
}

func TestCopiedSelection(t *testing.T) {
	var copier copyCoordinator
	t.Run("unchanged clipboard is not the selection", func(t *testing.T) {
		f := &fakeCopySource{seq: 7, text: "old clipboard"}
		_, err := copier.copiedSelection(f, 3, func() {})
		if !errors.Is(err, selection.ErrNoSelection) || f.reads != 0 {
			t.Fatalf("error = %v, old clipboard reads = %d", err, f.reads)
		}
	})
	t.Run("waits for a new clipboard generation", func(t *testing.T) {
		f := &fakeCopySource{seq: 7, text: "old clipboard"}
		text, err := copier.copiedSelection(f, 3, func() { f.seq++; f.text = "selected text" })
		if err != nil || text != "selected text" {
			t.Fatalf("text = %q, error = %v", text, err)
		}
	})
	t.Run("delayed clipboard text is rendered before comparing", func(t *testing.T) {
		f := &fakeCopySource{seq: 7, text: "old clipboard"}
		f.onPoll = func() { f.seq++; f.text = "delayed selection"; f.onPoll = nil }
		text, err := copier.copiedSelection(f, 2, func() {})
		if err != nil || text != "delayed selection" {
			t.Fatalf("text = %q, error = %v", text, err)
		}
	})
	t.Run("same text can be copied again", func(t *testing.T) {
		f := &fakeCopySource{seq: 7, text: "same text"}
		f.onCopy = func() { f.seq++ }
		text, err := copier.copiedSelection(f, 2, func() {})
		if err != nil || text != "same text" {
			t.Fatalf("text = %q, error = %v", text, err)
		}
	})
	t.Run("copy failure returns without reading", func(t *testing.T) {
		sendErr := errors.New("send failed")
		f := &fakeCopySource{seq: 7, text: "old clipboard", err: sendErr}
		_, err := copier.copiedSelection(f, 2, func() {})
		if !errors.Is(err, sendErr) || f.reads != 0 {
			t.Fatalf("error = %v, clipboard reads = %d", err, f.reads)
		}
	})
}

func TestCopiedSelectionSerializesRequests(t *testing.T) {
	var copier copyCoordinator
	firstStarted, releaseFirst := make(chan struct{}), make(chan struct{})
	secondStarted, done := make(chan struct{}), make(chan struct{}, 2)
	first := &fakeCopySource{seq: 1, text: "first"}
	first.onCopy = func() { close(firstStarted); <-releaseFirst; first.seq++ }
	second := &fakeCopySource{seq: 1, text: "second"}
	second.onCopy = func() { close(secondStarted); second.seq++ }
	go func() { _, _ = copier.copiedSelection(first, 1, func() {}); done <- struct{}{} }()
	<-firstStarted
	go func() { _, _ = copier.copiedSelection(second, 1, func() {}); done <- struct{}{} }()
	select {
	case <-secondStarted:
		t.Error("second copy started before the first copy finished")
	case <-time.After(50 * time.Millisecond):
	}
	close(releaseFirst)
	for range 2 {
		select {
		case <-done:
		case <-time.After(time.Second):
			t.Fatal("copy requests did not finish")
		}
	}
	select {
	case <-secondStarted:
	default:
		t.Fatal("second copy never started")
	}
}
