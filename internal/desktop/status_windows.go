//go:build windows

package desktop

import (
	"runtime"
	"syscall"
	"unsafe"

	"github.com/RinoPaw/Reinor/internal/capability"
)

var (
	openInputDesktop         = user32.NewProc("OpenInputDesktop")
	closeDesktop             = user32.NewProc("CloseDesktop")
	getUserObjectInformation = user32.NewProc("GetUserObjectInformationW")
)

const (
	desktopReadObjects = 0x0001
	uoiName            = 2
)

// Probe the interactive input desktop, including Windows lock-screen changes.
// This is a point-in-time observation. CaptureScreen rechecks just before use.
func probeDesktop() (bool, bool, string) {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()

	// Per-monitor DPI awareness is scoped to this thread only.
	previous, _, _ := setThreadDPIAwarenessContext.Call(^uintptr(3))
	if previous == 0 {
		return true, false, capability.NoDesktopSession
	}
	defer setThreadDPIAwarenessContext.Call(previous)

	if metric(smCXVirtualScreen) <= 0 || metric(smCYVirtualScreen) <= 0 {
		return true, false, capability.NoDesktopSession
	}
	handle, _, _ := openInputDesktop.Call(0, 0, desktopReadObjects)
	if handle == 0 {
		return true, false, capability.NoDesktopSession
	}
	defer closeDesktop.Call(handle)

	name := make([]uint16, 128)
	var needed uint32
	ok, _, _ := getUserObjectInformation.Call(handle, uoiName, uintptr(unsafe.Pointer(&name[0])), uintptr(len(name)*2), uintptr(unsafe.Pointer(&needed)))
	if ok == 0 {
		return true, false, capability.NoDesktopSession
	}
	if syscall.UTF16ToString(name) != "Default" {
		return true, false, capability.DesktopLocked
	}
	return true, true, ""
}
