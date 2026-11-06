package main

import (
	"bytes"
	"encoding/json"
	"testing"

	"github.com/RinoPaw/Reinor/internal/wire"
)

func TestLegacyAgentCapabilityHello(t *testing.T) {
	var capabilities []struct {
		ID        string `json:"id"`
		Supported bool   `json:"supported"`
		Available bool   `json:"available"`
		Reason    string `json:"reason"`
	}
	if err := json.Unmarshal(agentCapabilities(), &capabilities); err != nil {
		t.Fatalf("capability JSON invalid: %v", err)
	}
	if len(capabilities) != 1 || capabilities[0].ID != "desktop.capture" ||
		capabilities[0].Supported || capabilities[0].Available ||
		capabilities[0].Reason != "CAPABILITY_UNSUPPORTED" {
		t.Fatalf("unexpected capability status: %+v", capabilities)
	}

	hello := wire.Envelope{Type: "hello", DeviceID: "win-7", OS: "windows", Capabilities: agentCapabilities()}
	encoded, err := wire.Marshal(hello)
	if err != nil {
		t.Fatal(err)
	}
	decoded, err := wire.Unmarshal(encoded)
	if err != nil {
		t.Fatal(err)
	}
	if decoded.Type != "hello" || decoded.DeviceID != "win-7" ||
		!bytes.Equal(decoded.Capabilities, agentCapabilities()) {
		t.Fatalf("capability announcement was lost: %+v", decoded)
	}
}

func TestLegacyCapabilityResponse(t *testing.T) {
	response := wire.Envelope{Type: "capabilities_result", TaskID: "cap-probe", Capabilities: agentCapabilities()}
	encoded, err := wire.Marshal(response)
	if err != nil {
		t.Fatal(err)
	}
	decoded, err := wire.Unmarshal(encoded)
	if err != nil {
		t.Fatal(err)
	}
	if decoded.Type != "capabilities_result" || decoded.TaskID != "cap-probe" ||
		!bytes.Equal(decoded.Capabilities, agentCapabilities()) {
		t.Fatalf("capability response invalid: %+v", decoded)
	}
}
