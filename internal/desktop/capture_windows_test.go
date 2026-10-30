//go:build windows

package desktop

import (
	"bytes"
	"image/jpeg"
	"os"
	"testing"
)

// This opt-in test requires an authorized unlocked interactive Windows session.
// CI Windows workers without desktops must not run it automatically.
func TestCaptureScreenInteractive(t *testing.T) {
	if os.Getenv("REINOR_CAPTURE_TEST") != "1" {
		t.Skip("set REINOR_CAPTURE_TEST=1 in an authorized interactive Windows session")
	}
	shot,err := CaptureScreen(1200,70)
	if err!=nil {t.Fatal(err)}
	if shot.MIMEType!="image/jpeg"||len(shot.Data)==0 {t.Fatal("missing encoded screenshot")}
	cfg,err:=jpeg.DecodeConfig(bytes.NewReader(shot.Data))
	if err!=nil {t.Fatal(err)}
	if cfg.Width!=shot.Width||cfg.Height!=shot.Height {
		t.Fatalf("encoded dimensions %dx%d != response %dx%d",cfg.Width,cfg.Height,shot.Width,shot.Height)
	}
	if shot.PhysicalWidth<shot.Width||shot.PhysicalHeight<shot.Height {
		t.Fatalf("invalid physical desktop dimensions: %+v",shot)
	}
	t.Logf("JPEG=%d bytes, scaled=%dx%d, physical=%dx%d origin=(%d,%d)",
		len(shot.Data),shot.Width,shot.Height,
		shot.PhysicalWidth,shot.PhysicalHeight,shot.OriginX,shot.OriginY)
}
