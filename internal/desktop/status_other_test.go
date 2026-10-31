//go:build !windows

package desktop

import (
	"testing"

	"github.com/RinoPaw/Reinor/internal/capability"
)

func TestNonWindowsHeadlessAndGraphicalWithoutBackend(t *testing.T) {
	t.Setenv("DISPLAY","")
	t.Setenv("WAYLAND_DISPLAY","")
	status:=Capability()
	if status.Supported||status.Available||status.Reason!=capability.NoDesktopSession {
		t.Fatalf("headless server should report NO_DESKTOP_SESSION: %+v",status)
	}
	t.Setenv("DISPLAY",":99")
	status=Capability()
	if status.Supported||status.Available||status.Reason!=capability.Unsupported {
		t.Fatalf("GUI host without a capture backend should report CAPABILITY_UNSUPPORTED: %+v",status)
	}
}
