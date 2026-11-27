package main

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/RinoPaw/Reinor/internal/wire"
)

func TestFileV2AgentTLS(t *testing.T) {
	payload := bytes.Repeat([]byte{0, 255, 127, 1}, 2<<20)
	digest := sha256.Sum256(payload)
	hash := hex.EncodeToString(digest[:])
	id, key := strings.Repeat("a", 32), strings.Repeat("b", 64)
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer "+key {
			t.Error("incorrect authorization")
			w.WriteHeader(401)
			return
		}
		switch r.Method {
		case http.MethodPut:
			if r.URL.Path != "/transfer/v2/receive/"+id || r.ContentLength != int64(len(payload)) || r.Header.Get("Content-Type") != "application/octet-stream" || r.Header.Get("X-Reinor-SHA256") != hash {
				t.Error("incorrect transfer metadata")
				w.WriteHeader(400)
				return
			}
			raw, err := io.ReadAll(r.Body)
			if err != nil || !bytes.Equal(raw, payload) {
				t.Error("binary upload corrupted")
				w.WriteHeader(400)
				return
			}
			w.WriteHeader(204)
		case http.MethodGet:
			if r.URL.Path != "/transfer/v2/source/"+id {
				t.Error("incorrect source path")
				w.WriteHeader(404)
				return
			}
			w.Header().Set("Content-Type", "application/octet-stream")
			http.ServeContent(w, r, "ignored", time.Time{}, bytes.NewReader(payload))
		default:
			w.WriteHeader(405)
		}
	}))
	defer server.Close()
	gateway := strings.Replace(server.URL, "https:", "wss:", 1) + "/agent/ws"
	dir := t.TempDir()
	source := filepath.Join(dir, "source.bin")
	dest := filepath.Join(dir, "dest.bin")
	if err := os.WriteFile(source, payload, 0600); err != nil {
		t.Fatal(err)
	}
	f := wire.Envelope{TransferID: id, Feature: key, Path: source}
	if err := sendFileV2WithClient(context.Background(), gateway, f, server.Client()); err != nil {
		t.Fatal(err)
	}
	f.Path = dest
	f.Size = uint64(len(payload))
	f.SHA256 = hash
	if err := receiveFileV2WithClient(context.Background(), gateway, f, server.Client()); err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(dest)
	if err != nil || !bytes.Equal(raw, payload) {
		t.Fatal("published bytes differ")
	}
	if err = receiveFileV2WithClient(context.Background(), gateway, f, server.Client()); err == nil {
		t.Fatal("overwrote existing destination")
	}
	f.Path = filepath.Join(dir, "bad.bin")
	f.SHA256 = strings.Repeat("0", 64)
	if err = receiveFileV2WithClient(context.Background(), gateway, f, server.Client()); err == nil {
		t.Fatal("accepted bad checksum")
	}
	if _, err = os.Stat(f.Path); !os.IsNotExist(err) {
		t.Fatal("published invalid file")
	}
}
