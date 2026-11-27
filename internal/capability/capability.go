// Package capability defines device features separately from their current availability.
package capability

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"
)

const (
	DesktopCapture = "desktop.capture"
	FileV2         = "file.stream.v2"
	AsyncTask      = "task.async.v1"

	NoDesktopSession = "NO_DESKTOP_SESSION"
	Unsupported      = "CAPABILITY_UNSUPPORTED"
	DesktopLocked    = "DESKTOP_LOCKED"
	CaptureFailed    = "CAPTURE_FAILED"

	maxBytes = 8192
)

// Status describes an Agent feature. Available is an observation, not a promise
// that the desktop will remain available until the next operation.
type Status struct {
	ID        string `json:"id"`
	Supported bool   `json:"supported"`
	Available bool   `json:"available"`
	Reason    string `json:"reason,omitempty"`
}

func Encode(statuses []Status) ([]byte, error) {
	if statuses == nil {
		statuses = []Status{}
	}
	data, err := json.Marshal(statuses)
	if err != nil {
		return nil, err
	}
	if _, err := Decode(data); err != nil {
		return nil, err
	}
	return data, nil
}

func Decode(data []byte) ([]Status, error) {
	if len(data) == 0 || len(data) > maxBytes {
		return nil, errors.New("missing or oversized capabilities")
	}
	var statuses []Status
	if err := json.Unmarshal(data, &statuses); err != nil {
		return nil, fmt.Errorf("invalid capability JSON: %w", err)
	}
	if statuses == nil || len(statuses) > 32 {
		return nil, errors.New("invalid capability list")
	}
	seen := map[string]struct{}{}
	for _, v := range statuses {
		if v.ID == "" || len(v.ID) > 80 || strings.Trim(v.ID, "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789._-") != "" {
			return nil, errors.New("invalid capability name")
		}
		if _, exists := seen[v.ID]; exists {
			return nil, errors.New("duplicate capability")
		}
		seen[v.ID] = struct{}{}
		if v.Available && !v.Supported {
			return nil, errors.New("unimplemented capability cannot be available")
		}
		if v.Available && v.Reason != "" {
			return nil, errors.New("available capability cannot have a failure reason")
		}
		if !v.Available && v.Reason == "" {
			return nil, errors.New("unavailable capability must have a reason")
		}
	}
	return statuses, nil
}

func Find(statuses []Status, id string) (Status, bool) {
	for _, v := range statuses {
		if v.ID == id {
			return v, true
		}
	}
	return Status{}, false
}
