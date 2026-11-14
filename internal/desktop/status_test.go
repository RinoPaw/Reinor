package desktop

import (
	"runtime"
	"testing"

	"github.com/RinoPaw/Reinor/internal/capability"
)

func TestDesktopCapabilityDeclaration(t *testing.T) {
	status := Capability()
	if status.ID != capability.DesktopCapture {
		t.Fatalf("incorrect capability ID: %+v", status)
	}
	if status.Available && !status.Supported {
		t.Fatalf("invalid capability state: %+v", status)
	}
	if !status.Available && status.Reason == "" {
		t.Fatalf("missing unavailable reason: %+v", status)
	}
	if runtime.GOOS == "windows" && !status.Supported {
		t.Fatalf("Windows Agent must implement desktop capture: %+v", status)
	}
	if runtime.GOOS != "windows" && status.Supported {
		t.Fatalf("non-Windows backend unexpectedly advertised: %+v", status)
	}
	t.Logf("desktop capability: %+v", status)
}
