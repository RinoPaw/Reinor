package main

import (
	"github.com/RinoPaw/Reinor/internal/capability"
	"github.com/RinoPaw/Reinor/internal/operation"
)

func (h *hub) operationAvailability(d operation.Definition, device string) (bool, string) {
	h.mu.Lock()
	defer h.mu.Unlock()
	a := h.agents[device]
	if a == nil || a.revoked {
		return false, "device offline"
	}
	for _, requirement := range d.Requires {
		switch requirement {
		case "online":
		case "permanent":
			if a.temporaryID != "" {
				return false, "permanent device required"
			}
		case "file-v1":
			if !a.fileTransfer {
				return false, "file-v1 unavailable"
			}
		default:
			c, ok := capability.Find(a.capabilities, requirement)
			if !ok || !c.Supported || !c.Available {
				return false, requirement + " unavailable"
			}
		}
	}
	return true, ""
}
