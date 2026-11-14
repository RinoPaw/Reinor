//go:build !windows

package desktop

import (
	"runtime"
	"testing"

	"github.com/RinoPaw/Reinor/internal/capability"
)

func TestNonWindowsHeadlessAndGraphicalWithoutBackend(t *testing.T) {
	t.Setenv("DISPLAY", "")
	t.Setenv("WAYLAND_DISPLAY", "")
	status := Capability()
	expected := capability.NoDesktopSession
	if runtime.GOOS == "android" {
		expected = capability.Unsupported
	}
	if status.Supported || status.Available || status.Reason != expected {
		t.Fatalf("no native backend should be reported accurately on %s: %+v", runtime.GOOS, status)
	}
	t.Setenv("DISPLAY", ":99")
	status = Capability()
	if status.Supported || status.Available || status.Reason != capability.Unsupported {
		t.Fatalf("GUI host without a capture backend should report CAPABILITY_UNSUPPORTED: %+v", status)
	}
}
