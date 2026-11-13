package selection

import (
	"fmt"
	"runtime"
	"sync"
	"unsafe"

	"github.com/ebitengine/purego"
)

// cgPoint has the layout of CoreGraphics' CGPoint on 64-bit macOS.
type cgPoint struct{ X, Y float64 }

type macAPI struct {
	trusted        func() bool
	tapCreate      func(uint32, uint32, uint32, uint64, uintptr, uintptr) uintptr
	tapEnable      func(uintptr, bool)
	tapPostEvent   func(uintptr, uintptr)
	eventCopy      func(uintptr) uintptr
	eventLocation  func(uintptr) cgPoint
	eventSource    func(int32) uintptr
	keyboardEvent  func(uintptr, uint16, bool) uintptr
	setEventFlags  func(uintptr, uint64)
	eventPost      func(uint32, uintptr)
	portSource     func(uintptr, uintptr, int64) uintptr
	portInvalidate func(uintptr)
	runLoopMain    func() uintptr
	runLoopAdd     func(uintptr, uintptr, uintptr)
	runLoopRemove  func(uintptr, uintptr, uintptr)
	release        func(uintptr)
	poolPush       func() uintptr
	poolPop        func(uintptr)
	commonModes    uintptr
}

var macLibraries struct {
	once sync.Once
	api  macAPI
	err  error
}

// Load each framework once; callbacks and Objective-C classes can then use
// their symbols for the lifetime of the process.
func loadMacAPI() (*macAPI, error) {
	macLibraries.once.Do(func() {
		defer func() {
			if r := recover(); r != nil {
				macLibraries.err = fmt.Errorf("加载 macOS 系统接口失败：%v", r)
			}
		}()
		open := func(path string) uintptr {
			lib, err := purego.Dlopen(path, purego.RTLD_NOW|purego.RTLD_GLOBAL)
			if err != nil {
				panic(err)
			}
			return lib
		}
		cf := open("/System/Library/Frameworks/CoreFoundation.framework/CoreFoundation")
		cg := open("/System/Library/Frameworks/CoreGraphics.framework/CoreGraphics")
		accessibility := open("/System/Library/Frameworks/ApplicationServices.framework/ApplicationServices")
		open("/System/Library/Frameworks/AppKit.framework/AppKit")
		objc := open("/usr/lib/libobjc.A.dylib")
		a := &macLibraries.api
		purego.RegisterLibFunc(&a.trusted, accessibility, "AXIsProcessTrusted")
		purego.RegisterLibFunc(&a.tapCreate, cg, "CGEventTapCreate")
		purego.RegisterLibFunc(&a.tapEnable, cg, "CGEventTapEnable")
		purego.RegisterLibFunc(&a.tapPostEvent, cg, "CGEventTapPostEvent")
		purego.RegisterLibFunc(&a.eventCopy, cg, "CGEventCreateCopy")
		purego.RegisterLibFunc(&a.eventLocation, cg, "CGEventGetLocation")
		purego.RegisterLibFunc(&a.eventSource, cg, "CGEventSourceCreate")
		purego.RegisterLibFunc(&a.keyboardEvent, cg, "CGEventCreateKeyboardEvent")
		purego.RegisterLibFunc(&a.setEventFlags, cg, "CGEventSetFlags")
		purego.RegisterLibFunc(&a.eventPost, cg, "CGEventPost")
		purego.RegisterLibFunc(&a.portSource, cf, "CFMachPortCreateRunLoopSource")
		purego.RegisterLibFunc(&a.portInvalidate, cf, "CFMachPortInvalidate")
		purego.RegisterLibFunc(&a.runLoopMain, cf, "CFRunLoopGetMain")
		purego.RegisterLibFunc(&a.runLoopAdd, cf, "CFRunLoopAddSource")
		purego.RegisterLibFunc(&a.runLoopRemove, cf, "CFRunLoopRemoveSource")
		purego.RegisterLibFunc(&a.release, cf, "CFRelease")
		purego.RegisterLibFunc(&a.poolPush, objc, "objc_autoreleasePoolPush")
		purego.RegisterLibFunc(&a.poolPop, objc, "objc_autoreleasePoolPop")
		p, err := purego.Dlsym(cf, "kCFRunLoopCommonModes")
		if err != nil {
			panic(err)
		}
		// Dlsym returns the address of the exported CFStringRef variable.
		a.commonModes = **(**uintptr)(unsafe.Pointer(&p))
	})
	return &macLibraries.api, macLibraries.err
}

// Objective-C autorelease pools belong to the OS thread that created them.
func (a *macAPI) withPool(fn func()) {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	pool := a.poolPush()
	defer a.poolPop(pool)
	fn()
}
