package selectionmonitor

import (
	"fmt"
	"runtime"
	"sync/atomic"
	"syscall"
	"unsafe"

	"github.com/egoist/mygo"
	"golang.org/x/sys/windows"

	"translate/modules/selection"
)

const (
	whMouseLL     = 14
	wmQuit        = 0x0012
	llmhfInjected = 0x00000001
	replayMarker  = 0x54524E53
)

var (
	user32                 = windows.NewLazySystemDLL("user32.dll")
	kernel32               = windows.NewLazySystemDLL("kernel32.dll")
	procSetWindowsHookExW  = user32.NewProc("SetWindowsHookExW")
	procUnhookWindowsHook  = user32.NewProc("UnhookWindowsHookEx")
	procCallNextHookEx     = user32.NewProc("CallNextHookEx")
	procGetMessageW        = user32.NewProc("GetMessageW")
	procPeekMessageW       = user32.NewProc("PeekMessageW")
	procTranslateMessage   = user32.NewProc("TranslateMessage")
	procDispatchMessageW   = user32.NewProc("DispatchMessageW")
	procPostThreadMessageW = user32.NewProc("PostThreadMessageW")
	procGetModuleHandleW   = kernel32.NewProc("GetModuleHandleW")
	procGetCurrentThreadID = kernel32.NewProc("GetCurrentThreadId")
	activeHook             atomic.Pointer[hookSession]
	hookCallback           = syscall.NewCallback(lowLevelMouseProc)
)

type windowsMonitor struct {
	session *hookSession
	copier  copyCoordinator
}

func New() (selection.Monitor, error) { return &windowsMonitor{}, nil }

// Start installs the hook on its own message-loop thread; the low-level hook
// must return promptly and must not wait for the application's UI thread.
func (m *windowsMonitor) Start(onHold, onOutside, onStopped func()) error {
	if m.session != nil {
		return nil
	}
	s := &hookSession{
		done: make(chan struct{}), onHold: onHold, onOutside: onOutside, onStopped: onStopped,
		runOnMain: mygo.RunOnMain, replay: replayMouse,
	}
	s.running.Store(true)
	if !activeHook.CompareAndSwap(nil, s) {
		return fmt.Errorf("%w: Windows 鼠标监听已启动", selection.ErrMonitor)
	}
	started := make(chan error, 1)
	go s.run(started)
	if err := <-started; err != nil {
		<-s.done
		return err
	}
	m.session = s
	return nil
}

func (m *windowsMonitor) SetAreas(popup, settings selection.Area) {
	if s := m.session; s != nil {
		s.mu.Lock()
		s.popup, s.settings = popup, settings
		s.mu.Unlock()
	}
}

func (m *windowsMonitor) Stop() {
	s := m.session
	if s == nil {
		return
	}
	m.session = nil
	s.stopping.Store(true)
	s.running.Store(false)
	s.mu.Lock()
	if s.timer != nil {
		s.timer.Stop()
	}
	s.gesture.reset()
	s.callbacks = nil
	if !s.exited {
		procPostThreadMessageW.Call(uintptr(s.threadID), wmQuit, 0, 0)
	}
	s.mu.Unlock()
	<-s.done
}

func (s *hookSession) run(started chan<- error) {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	defer func() {
		s.mu.Lock()
		s.exited = true
		s.mu.Unlock()
		activeHook.CompareAndSwap(s, nil)
		close(s.done)
	}()

	// PostThreadMessage only works after the thread owns a message queue.
	var msg winMessage
	procPeekMessageW.Call(uintptr(unsafe.Pointer(&msg)), 0, 0, 0, 0)
	id, _, _ := procGetCurrentThreadID.Call()
	s.threadID = uint32(id)
	module, _, _ := procGetModuleHandleW.Call(0)
	hook, _, hookErr := procSetWindowsHookExW.Call(whMouseLL, hookCallback, module, 0)
	if hook == 0 {
		s.running.Store(false)
		started <- fmt.Errorf("%w: %v", selection.ErrMonitor, hookErr)
		return
	}
	defer procUnhookWindowsHook.Call(hook)
	started <- nil

	for {
		result, _, _ := procGetMessageW.Call(uintptr(unsafe.Pointer(&msg)), 0, 0, 0)
		if int32(result) <= 0 {
			break
		}
		procTranslateMessage.Call(uintptr(unsafe.Pointer(&msg)))
		procDispatchMessageW.Call(uintptr(unsafe.Pointer(&msg)))
	}
	s.running.Store(false)
	if !s.stopping.Load() && s.onStopped != nil {
		go mygo.RunOnMain(s.onStopped)
	}
}

func lowLevelMouseProc(code int32, message uintptr, data *mouseHookData) uintptr {
	if code >= 0 && data != nil {
		if s := activeHook.Load(); s != nil && s.running.Load() {
			event := *data
			if event.flags&llmhfInjected == 0 || event.extraInfo != replayMarker {
				if s.handle(uint32(message), event.point) {
					return 1
				}
			}
		}
	}
	result, _, _ := procCallNextHookEx.Call(0, uintptr(code), message, uintptr(unsafe.Pointer(data)))
	return result
}
