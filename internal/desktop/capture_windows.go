//go:build windows

package desktop

import (
	"bytes"
	"fmt"
	"image"
	"image/jpeg"
	"runtime"
	"syscall"
	"unsafe"
)

var (
	user32 = syscall.NewLazyDLL("user32.dll")
	gdi32  = syscall.NewLazyDLL("gdi32.dll")

	setThreadDPIAwarenessContext = user32.NewProc("SetThreadDpiAwarenessContext")
	getSystemMetrics             = user32.NewProc("GetSystemMetrics")
	getDC                        = user32.NewProc("GetDC")
	releaseDC                    = user32.NewProc("ReleaseDC")
	createCompatibleDC           = gdi32.NewProc("CreateCompatibleDC")
	deleteDC                     = gdi32.NewProc("DeleteDC")
	createCompatibleBitmap       = gdi32.NewProc("CreateCompatibleBitmap")
	deleteObject                 = gdi32.NewProc("DeleteObject")
	selectObject                 = gdi32.NewProc("SelectObject")
	setStretchBltMode            = gdi32.NewProc("SetStretchBltMode")
	stretchBlt                   = gdi32.NewProc("StretchBlt")
	getDIBits                    = gdi32.NewProc("GetDIBits")
)

const (
	smXVirtualScreen  = 76
	smYVirtualScreen  = 77
	smCXVirtualScreen = 78
	smCYVirtualScreen = 79
	biRGB             = 0
	dibRGBColors      = 0
	halftone          = 4
	srcCopyCaptureBLT = 0x40CC0020 // CAPTUREBLT | SRCCOPY
	maxPixels         = 16_000_000
)

type bitmapInfoHeader struct {
	Size          uint32
	Width         int32
	Height        int32
	Planes        uint16
	BitCount      uint16
	Compression   uint32
	SizeImage     uint32
	XPelsPerMeter int32
	YPelsPerMeter int32
	ClrUsed       uint32
	ClrImportant  uint32
}

type bitmapInfo struct {
	Header bitmapInfoHeader
	Color  uint32
}

func metric(id int) int {
	value, _, _ := getSystemMetrics.Call(uintptr(id))
	return int(int32(value))
}

// CaptureScreen captures the Windows virtual desktop with physical-pixel
// coordinates. The thread-scoped DPI context is restored before returning;
// no process-wide DPI mode is changed. This requires an interactive desktop.
func CaptureScreen(maxWidth, quality int) (Screenshot, error) {
	maxWidth, quality, err := normalizeOptions(maxWidth, quality)
	if err != nil {
		return Screenshot{}, err
	}

	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	// DPI_AWARENESS_CONTEXT_PER_MONITOR_AWARE_V2 = -4
	previous, _, callErr := setThreadDPIAwarenessContext.Call(^uintptr(3))
	if previous == 0 {
		return Screenshot{}, fmt.Errorf("enable per-monitor DPI awareness: %v", callErr)
	}
	defer setThreadDPIAwarenessContext.Call(previous)

	x, y := metric(smXVirtualScreen), metric(smYVirtualScreen)
	physicalWidth, physicalHeight := metric(smCXVirtualScreen), metric(smCYVirtualScreen)
	if physicalWidth <= 0 || physicalHeight <= 0 {
		return Screenshot{}, fmt.Errorf("%w: virtual desktop is %dx%d", ErrNoDesktopSession, physicalWidth, physicalHeight)
	}
	width := physicalWidth
	if width > maxWidth {
		width = maxWidth
	}
	height := int((int64(physicalHeight)*int64(width) + int64(physicalWidth)/2) / int64(physicalWidth))
	if height < 1 || int64(width)*int64(height) > maxPixels {
		return Screenshot{}, errorsCaptureTooLarge(width, height)
	}

	screenDC, _, callErr := getDC.Call(0)
	if screenDC == 0 {
		return Screenshot{}, fmt.Errorf("%w: GetDC: %v", ErrNoDesktopSession, callErr)
	}
	defer releaseDC.Call(0, screenDC)
	memDC, _, callErr := createCompatibleDC.Call(screenDC)
	if memDC == 0 {
		return Screenshot{}, fmt.Errorf("CreateCompatibleDC: %v", callErr)
	}
	defer deleteDC.Call(memDC)
	bmp, _, callErr := createCompatibleBitmap.Call(screenDC, uintptr(width), uintptr(height))
	if bmp == 0 {
		return Screenshot{}, fmt.Errorf("CreateCompatibleBitmap: %v", callErr)
	}
	defer deleteObject.Call(bmp)
	prevObj, _, callErr := selectObject.Call(memDC, bmp)
	if prevObj == 0 || prevObj == ^uintptr(0) {
		return Screenshot{}, fmt.Errorf("SelectObject: %v", callErr)
	}
	selected := true
	defer func() {
		if selected {
			selectObject.Call(memDC, prevObj)
		}
	}()

	setStretchBltMode.Call(memDC, halftone)
	ok, _, callErr := stretchBlt.Call(
		memDC, 0, 0, uintptr(width), uintptr(height),
		screenDC, uintptr(x), uintptr(y), uintptr(physicalWidth), uintptr(physicalHeight),
		srcCopyCaptureBLT,
	)
	if ok == 0 {
		return Screenshot{}, fmt.Errorf("StretchBlt: %v", callErr)
	}
	// GetDIBits requires the bitmap to be deselected from the device context.
	selectObject.Call(memDC, prevObj)
	selected = false

	info := bitmapInfo{Header: bitmapInfoHeader{
		Size:        uint32(unsafe.Sizeof(bitmapInfoHeader{})),
		Width:       int32(width),
		Height:      -int32(height), // top-down BGRA rows
		Planes:      1,
		BitCount:    32,
		Compression: biRGB,
	}}
	bgra := make([]byte, width*height*4)
	lines, _, callErr := getDIBits.Call(
		screenDC, bmp, 0, uintptr(height),
		uintptr(unsafe.Pointer(&bgra[0])), uintptr(unsafe.Pointer(&info)), dibRGBColors,
	)
	if int(lines) != height {
		return Screenshot{}, fmt.Errorf("GetDIBits: got %d of %d rows: %v", lines, height, callErr)
	}
	rgba := image.NewRGBA(image.Rect(0, 0, width, height))
	for i := 0; i < len(bgra); i += 4 {
		rgba.Pix[i] = bgra[i+2]
		rgba.Pix[i+1] = bgra[i+1]
		rgba.Pix[i+2] = bgra[i]
		rgba.Pix[i+3] = 255
	}
	var buf bytes.Buffer
	if err := jpeg.Encode(&buf, rgba, &jpeg.Options{Quality: quality}); err != nil {
		return Screenshot{}, err
	}
	if buf.Len() > MaxImageBytes {
		return Screenshot{}, fmt.Errorf("screenshot JPEG exceeds %d bytes; retry with smaller max_width or quality", MaxImageBytes)
	}
	return Screenshot{
		Data: buf.Bytes(), MIMEType: "image/jpeg",
		Width: width, Height: height,
		OriginX: x, OriginY: y,
		PhysicalWidth: physicalWidth, PhysicalHeight: physicalHeight,
	}, nil
}

func errorsCaptureTooLarge(width, height int) error {
	return fmt.Errorf("screenshot dimensions too large: %dx%d", width, height)
}
