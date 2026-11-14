package wire

import (
	"bytes"
	"testing"
)

func TestFileEnvelopeRoundTrip(t *testing.T) {
	original := Envelope{
		Type: "file_read", TaskID: "task-1", TransferID: "0123456789abcdef0123456789abcdef",
		Path: "C:\\Users\\rino\\Desktop\\文件.xlsx", Data: []byte{0, 1, 255, 200},
		Offset: 1<<32 + 12, Size: 4 << 30, SHA256: "0123456789abcdef",
		Feature: "file-v1",
	}
	data, err := Marshal(original)
	if err != nil {
		t.Fatal(err)
	}
	got, err := Unmarshal(data)
	if err != nil {
		t.Fatal(err)
	}
	if got.Type != original.Type || got.TaskID != original.TaskID || got.TransferID != original.TransferID ||
		got.Path != original.Path || got.Offset != original.Offset || got.Size != original.Size ||
		got.SHA256 != original.SHA256 || got.Feature != original.Feature ||
		!bytes.Equal(got.Data, original.Data) {
		t.Fatalf("file envelope mismatch: got %+v", got)
	}
}

func TestFileFrameTooLarge(t *testing.T) {
	if _, err := Marshal(Envelope{Type: "file_chunk", Data: make([]byte, MaxFrame)}); err == nil {
		t.Fatal("oversized file frame accepted")
	}
}
