package wire

import (
	"bytes"
	"testing"
)

func TestScreenshotRoundTrip(t *testing.T) {
	in := Envelope{
		Type: "capture_result", TaskID: "shot-1",
		MIMEType: "image/jpeg", Data: []byte{0xff, 0xd8, 1, 2, 3, 0xff, 0xd9},
		Width: 1600, Height: 1000, OriginX: -1920, OriginY: -120,
		PhysicalWidth: 2560, PhysicalHeight: 1600,
		Quality: 75,
	}
	raw, err := Marshal(in)
	if err != nil {
		t.Fatal(err)
	}
	out, err := Unmarshal(raw)
	if err != nil {
		t.Fatal(err)
	}
	if out.Type != in.Type || out.TaskID != in.TaskID || !bytes.Equal(out.Data, in.Data) ||
		out.MIMEType != in.MIMEType || out.Width != in.Width || out.Height != in.Height ||
		out.OriginX != in.OriginX || out.OriginY != in.OriginY ||
		out.PhysicalWidth != in.PhysicalWidth || out.PhysicalHeight != in.PhysicalHeight ||
		out.Quality != in.Quality {
		t.Fatalf("screenshot transport mismatch: %#v", out)
	}
}

func TestScreenshotFrameLimit(t *testing.T) {
	_, err := Marshal(Envelope{Type: "capture_result", Data: make([]byte, MaxFrame)})
	if err == nil {
		t.Fatal("oversized frame should be rejected")
	}
	if _, err := Unmarshal(make([]byte, MaxFrame+1)); err == nil {
		t.Fatal("oversized incoming frame should be rejected")
	}
}
