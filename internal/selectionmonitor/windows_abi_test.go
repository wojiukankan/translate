package selectionmonitor

import (
	"testing"
	"unsafe"
)

func TestWindowsABI64(t *testing.T) {
	if unsafe.Sizeof(uintptr(0)) != 8 {
		t.Skip("the Windows release targets a 64-bit architecture")
	}
	for _, tc := range []struct {
		name string
		got  uintptr
		want uintptr
	}{
		{"MSLLHOOKSTRUCT size", unsafe.Sizeof(mouseHookData{}), 32},
		{"MSLLHOOKSTRUCT extra info offset", unsafe.Offsetof(mouseHookData{}.extraInfo), 24},
		{"MSG size", unsafe.Sizeof(winMessage{}), 48},
		{"MOUSEINPUT size", unsafe.Sizeof(mouseInput{}), 32},
		{"MOUSEINPUT extra info offset", unsafe.Offsetof(mouseInput{}.extraInfo), 24},
		{"KEYBDINPUT size", unsafe.Sizeof(keyboardInput{}), 24},
		{"KEYBDINPUT extra info offset", unsafe.Offsetof(keyboardInput{}.extraInfo), 16},
		{"INPUT size", unsafe.Sizeof(input{}), 40},
		{"INPUT mouse offset", unsafe.Offsetof(input{}.mouse), 8},
	} {
		if tc.got != tc.want {
			t.Errorf("%s = %d, want %d", tc.name, tc.got, tc.want)
		}
	}
}
