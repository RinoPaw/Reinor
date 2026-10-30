//go:build !windows

package desktop

import "errors"

// CaptureScreen is only implemented for interactive Windows desktops.
func CaptureScreen(maxWidth, quality int) (Screenshot, error) {
	if _, _, err := normalizeOptions(maxWidth, quality); err != nil {
		return Screenshot{}, err
	}
	return Screenshot{}, errors.New("desktop capture is currently supported only on Windows")
}
