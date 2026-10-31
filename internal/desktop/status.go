package desktop

import "github.com/RinoPaw/Reinor/internal/capability"

func Capability() capability.Status {
	supported,available,reason := probeDesktop()
	return capability.Status{
		ID: capability.DesktopCapture,
		Supported: supported,
		Available: available,
		Reason: reason,
	}
}
