package main

import (
	"context"
	"net/http"
	"os"
	"time"

	"github.com/RinoPaw/Reinor/internal/wire"
)

func (m *v2Manager) dispatchUpload(t *v2Transfer) {
	defer t.cancel()
	m.mu.Lock()
	if m.records[t.ID] != t || t.Status != "staged" {
		m.mu.Unlock()
		return
	}
	t.Status = "delivering"
	size, digest := t.Size, t.SHA
	m.mu.Unlock()
	ch := make(chan wire.Envelope, 1)
	m.h.mu.Lock()
	m.h.pending[t.ID] = pending{agent: t.agent, ch: ch}
	m.h.mu.Unlock()
	defer func() { m.h.mu.Lock(); delete(m.h.pending, t.ID); m.h.mu.Unlock() }()
	if !m.active(t.agent) {
		m.fail(t, "device disconnected before delivery")
		return
	}
	ctx, cancel := context.WithTimeout(t.ctx, 5*time.Second)
	err := t.agent.send(ctx, wire.Envelope{Type: "file_v2_receive", TaskID: t.ID, TransferID: t.ID, Path: t.remotePath, Size: uint64(size), SHA256: digest, Feature: t.receiveKey})
	cancel()
	status, message := "unknown", "delivery outcome unknown; do not automatically retry"
	if err == nil {
		select {
		case reply := <-ch:
			if reply.Type == "file_result" && reply.Error == "" {
				status, message = "completed", ""
			} else if reply.Error != "" && reply.Error != "connection lost; execution status unknown" {
				status, message = "failed", reply.Error
			}
		case <-t.ctx.Done():
		}
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.records[t.ID] != t || t.Status != "delivering" {
		return
	}
	t.Status, t.Error = status, message
	t.Expires = time.Now().Add(10 * time.Minute)
	if t.path != "" {
		_ = os.Remove(t.path)
		t.path = ""
	}
}
func (m *v2Manager) source(w http.ResponseWriter, r *http.Request, id string) {
	m.mu.Lock()
	m.cleanup()
	t := m.records[id]
	if t == nil || t.Direction != "upload" || t.Status != "delivering" || !v2Equal(r.Header.Get("Authorization"), "Bearer "+t.receiveKey) || !m.active(t.agent) {
		m.mu.Unlock()
		http.NotFound(w, r)
		return
	}
	f, err := os.Open(t.path)
	m.mu.Unlock()
	if err != nil {
		http.NotFound(w, r)
		return
	}
	defer f.Close()
	w.Header().Set("Content-Type", "application/octet-stream")
	http.ServeContent(w, r, "file.bin", time.Time{}, &v2GuardedFile{File: f, allowed: func() bool {
		m.mu.Lock()
		defer m.mu.Unlock()
		return m.records[id] == t && t.Status == "delivering" && time.Now().Before(t.Expires) && m.active(t.agent)
	}})
}
