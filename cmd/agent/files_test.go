package main

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/RinoPaw/Reinor/internal/wire"
)

const testID = "0123456789abcdef0123456789abcdef"

func digest(data []byte) string {
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

func requireOK(t *testing.T, reply wire.Envelope) {
	t.Helper()
	if reply.Error != "" {
		t.Fatalf("file operation failed: %s", reply.Error)
	}
}

func TestBinaryFileRoundTrip(t *testing.T) {
	s := newFileTransfers()
	defer s.closeAll()
	path := filepath.Join(t.TempDir(), "binary test.xlsx")
	content := append(bytes.Repeat([]byte{0, 255, 1, 127}, 33000), []byte("尾部")...)
	requireOK(t, s.handle(wire.Envelope{Type: "file_start", TransferID: testID, Path: path, Size: uint64(len(content)), SHA256: digest(content)}))
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatal("destination published prematurely")
	}

	first := content[:maxFileChunk]
	second := content[maxFileChunk:]
	requireOK(t, s.handle(wire.Envelope{Type: "file_chunk", TransferID: testID, Offset: 0, Data: first}))
	retry := s.handle(wire.Envelope{Type: "file_chunk", TransferID: testID, Offset: 0, Data: first})
	requireOK(t, retry)
	if retry.Offset != uint64(len(first)) {
		t.Fatal("duplicate write changed offset")
	}
	mismatch := s.handle(wire.Envelope{Type: "file_chunk", TransferID: testID, Offset: 0, Data: []byte("wrong")})
	if !strings.Contains(mismatch.Error, "differs") {
		t.Fatalf("incorrect retry accepted: %+v", mismatch)
	}
	requireOK(t, s.handle(wire.Envelope{Type: "file_chunk", TransferID: testID, Offset: uint64(len(first)), Data: second}))
	finish := s.handle(wire.Envelope{Type: "file_finish", TransferID: testID})
	requireOK(t, finish)
	if finish.SHA256 != digest(content) {
		t.Fatal("unexpected checksum")
	}
	got, err := os.ReadFile(path)
	if err != nil || !bytes.Equal(got, content) {
		t.Fatalf("binary bytes corrupted: %v", err)
	}

	info := s.handle(wire.Envelope{Type: "file_info", Path: path})
	requireOK(t, info)
	if info.Size != uint64(len(content)) || info.SHA256 != digest(content) {
		t.Fatalf("bad metadata: %+v", info)
	}
	var downloaded []byte
	for offset := uint64(0); offset < info.Size; {
		chunk := s.handle(wire.Envelope{Type: "file_read", Path: path, Offset: offset})
		requireOK(t, chunk)
		if chunk.Offset != offset || chunk.Size != info.Size || chunk.SHA256 != digest(chunk.Data) {
			t.Fatalf("bad chunk: %+v", chunk)
		}
		downloaded = append(downloaded, chunk.Data...)
		offset += uint64(len(chunk.Data))
	}
	if !bytes.Equal(downloaded, content) {
		t.Fatal("download bytes corrupted")
	}
}

func TestTransferRejectsOverwrite(t *testing.T) {
	s := newFileTransfers()
	defer s.closeAll()
	path := filepath.Join(t.TempDir(), "existing.txt")
	if err := os.WriteFile(path, []byte("original"), 0600); err != nil {
		t.Fatal(err)
	}
	r := s.handle(wire.Envelope{Type: "file_start", TransferID: testID, Path: path, Size: 0, SHA256: digest(nil)})
	if r.Error == "" {
		t.Fatal("allowed overwrite")
	}
	got, _ := os.ReadFile(path)
	if string(got) != "original" {
		t.Fatal("existing data modified")
	}
}

func TestChecksumFailureAndAbortCleanup(t *testing.T) {
	s := newFileTransfers()
	dir := t.TempDir()
	path := filepath.Join(dir, "missing.bin")
	requireOK(t, s.handle(wire.Envelope{Type: "file_start", TransferID: testID, Path: path, Size: 4, SHA256: digest([]byte("good"))}))
	requireOK(t, s.handle(wire.Envelope{Type: "file_chunk", TransferID: testID, Data: []byte("bad!"), Offset: 0}))
	finish := s.handle(wire.Envelope{Type: "file_finish", TransferID: testID})
	if !strings.Contains(finish.Error, "sha256 mismatch") {
		t.Fatalf("missing checksum rejection: %+v", finish)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatal("invalid file made visible")
	}
	files, _ := os.ReadDir(dir)
	if len(files) != 0 {
		t.Fatalf("temporary files left after checksum failure: %d", len(files))
	}

	path2 := filepath.Join(dir, "cancel.bin")
	requireOK(t, s.handle(wire.Envelope{Type: "file_start", TransferID: testID, Path: path2, Size: 2, SHA256: digest([]byte("ok"))}))
	requireOK(t, s.handle(wire.Envelope{Type: "file_abort", TransferID: testID}))
	if files, _ := os.ReadDir(dir); len(files) != 0 {
		t.Fatal("abort did not remove staging file")
	}

	requireOK(t, s.handle(wire.Envelope{Type: "file_start", TransferID: testID, Path: path2, Size: 2, SHA256: digest([]byte("ok"))}))
	s.closeAll()
	if files, _ := os.ReadDir(dir); len(files) != 0 {
		t.Fatal("connection close did not remove staging file")
	}
}

func TestTransferInvalidInput(t *testing.T) {
	s := newFileTransfers()
	defer s.closeAll()
	path := filepath.Join(t.TempDir(), "invalid.bin")
	tests := []wire.Envelope{
		{Type: "file_start", TransferID: testID, Path: "relative/path", SHA256: digest(nil)},
		{Type: "file_start", TransferID: "bad", Path: path, SHA256: digest(nil)},
		{Type: "file_start", TransferID: testID, Path: path, SHA256: "no"},
		{Type: "file_start", TransferID: testID, Path: path, Size: maxFileSize + 1, SHA256: digest(nil)},
	}
	for _, f := range tests {
		if r := s.handle(f); r.Error == "" {
			t.Fatalf("accepted invalid input: %+v", f)
		}
	}
	requireOK(t, s.handle(wire.Envelope{Type: "file_start", TransferID: testID, Path: path, Size: 0, SHA256: digest(nil)}))
	if r := s.handle(wire.Envelope{Type: "file_chunk", TransferID: testID, Data: []byte{1}}); r.Error == "" {
		t.Fatal("allowed overrun")
	}
	if r := s.handle(wire.Envelope{Type: "file_chunk", TransferID: testID, Data: make([]byte, maxFileChunk+1)}); r.Error == "" {
		t.Fatal("allowed oversized chunk")
	}
	requireOK(t, s.handle(wire.Envelope{Type: "file_finish", TransferID: testID}))
	data, err := os.ReadFile(path)
	if err != nil || len(data) != 0 {
		t.Fatal("zero-byte transfer failed")
	}
}

func TestIdleUploadExpiration(t *testing.T) {
	s := newFileTransfers()
	dir := t.TempDir()
	target := filepath.Join(dir, "expired.bin")
	requireOK(t, s.handle(wire.Envelope{Type: "file_start", TransferID: testID, Path: target, Size: 1, SHA256: digest([]byte{1})}))
	s.mu.Lock()
	s.uploads[testID].updatedAt = time.Now().Add(-uploadIdleTTL - time.Second)
	s.mu.Unlock()
	s.reapExpired(time.Now())
	if files, err := os.ReadDir(dir); err != nil || len(files) != 0 {
		t.Fatalf("expired upload left temporary file: %v, %v", files, err)
	}
	if r := s.handle(wire.Envelope{Type: "file_chunk", TransferID: testID, Data: []byte{1}}); r.Error == "" {
		t.Fatal("expired transfer remained active")
	}
}
