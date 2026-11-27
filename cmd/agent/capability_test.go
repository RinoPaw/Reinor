package main

import (
	"testing"

	"github.com/RinoPaw/Reinor/internal/capability"
	"github.com/RinoPaw/Reinor/internal/desktop"
	"github.com/RinoPaw/Reinor/internal/wire"
)

func TestCapabilityResponse(t *testing.T) {
	got := capabilitySnapshot("probe-1")
	if got.Type != "capabilities_result" || got.TaskID != "probe-1" || got.Error != "" {
		t.Fatalf("unexpected result: %+v", got)
	}
	statuses, err := capability.Decode(got.Capabilities)
	if err != nil {
		t.Fatal(err)
	}
	if len(statuses) != 2 || statuses[0].ID != capability.DesktopCapture || statuses[1].ID != capability.FileV2 {
		t.Fatalf("missing desktop capability %+v", statuses)
	}
	if statuses[0] != desktop.Capability() {
		t.Logf("desktop changed between checks; snapshot=%+v current=%+v", statuses[0], desktop.Capability())
	}
}

func TestCaptureUnavailableReturnsStructuredError(t *testing.T) {
	status := desktop.Capability()
	if status.Available {
		t.Skip("requires a device without an accessible desktop")
	}
	result := captureScreen(wire.Envelope{TaskID: "capture-1"})
	if result.Type != "capture_result" || result.ErrorCode != status.Reason || result.Error == "" {
		t.Fatalf("expected error code %s, got %+v", status.Reason, result)
	}
}
