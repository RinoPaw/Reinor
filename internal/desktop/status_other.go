//go:build !windows

package desktop

import (
	"os"

	"github.com/RinoPaw/Reinor/internal/capability"
)

// The current non-Windows Agent has no desktop capture backend. A headless
// Linux server reports its missing session explicitly; a GUI host can adopt
// a native backend later without changing the Gateway.
func probeDesktop()(bool,bool,string) {
	if os.Getenv("DISPLAY")=="" && os.Getenv("WAYLAND_DISPLAY")=="" {
		return false,false,capability.NoDesktopSession
	}
	return false,false,capability.Unsupported
}
