// Package desktop implements owner-authorized desktop capture for the local Agent.
// Coordinates are always physical pixels in the Windows virtual desktop.
package desktop

import (
	"errors"
	"fmt"
)

const (
	DefaultMaxWidth = 1600
	DefaultQuality  = 75
	MaxImageBytes   = 3 << 20
)

// Screenshot contains an encoded screenshot and the physical capture geometry.
// The bitmap dimensions may be scaled down for transport. Virtual geometry
// always describes the original desktop in physical pixels.
type Screenshot struct {
	Data                          []byte
	MIMEType                      string
	Width, Height                 int
	OriginX, OriginY              int
	PhysicalWidth, PhysicalHeight int
}

func normalizeOptions(maxWidth, quality int) (int, int, error) {
	if maxWidth == 0 {
		maxWidth = DefaultMaxWidth
	}
	if quality == 0 {
		quality = DefaultQuality
	}
	if maxWidth < 320 || maxWidth > 3840 {
		return 0, 0, fmt.Errorf("max_width must be between 320 and 3840")
	}
	if quality < 35 || quality > 90 {
		return 0, 0, errors.New("quality must be between 35 and 90")
	}
	return maxWidth, quality, nil
}
