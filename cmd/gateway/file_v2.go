package main

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"errors"
	"io"
	"mime"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/RinoPaw/Reinor/internal/capability"
	"github.com/RinoPaw/Reinor/internal/wire"
)

const v2Limit int64 = 256 << 20
const v2Prefix = "/transfer/v2/"
const v2TTL = 15 * time.Minute

type v2Record struct {
	ID        string    `json:"transfer_id"`
	Device    string    `json:"device_id"`
	Name      string    `json:"filename"`
	Direction string    `json:"direction"`
	Status    string    `json:"status"`
	Size      int64     `json:"size"`
	Received  int64     `json:"received_bytes"`
	SHA       string    `json:"sha256,omitempty"`
	Error     string    `json:"error,omitempty"`
	URL       string    `json:"url,omitempty"`
	Expires   time.Time `json:"expires_at"`
}
type v2Transfer struct {
	v2Record
	agent                     *agent
	receiveKey, readKey, path string
	cancel                    context.CancelFunc
	ctx                       context.Context
	remotePath                string
}
type v2Manager struct {
	mu          sync.Mutex
	h           *hub
	origin, dir string
	records     map[string]*v2Transfer
	receivers   chan struct{}
}

func v2Key() (string, error) {
	var b [32]byte
	_, err := rand.Read(b[:])
	return hex.EncodeToString(b[:]), err
}
func newV2Manager(h *hub, origin, dir string) (*v2Manager, error) {
	u, err := url.Parse(origin)
	if err != nil || u.Scheme != "https" || u.Host == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" || (u.Path != "" && u.Path != "/") {
		return nil, errors.New("REINOR_PUBLIC_ORIGIN must be an HTTPS origin")
	}
	if err = os.MkdirAll(dir, 0700); err != nil {
		return nil, err
	}
	// Transfer URLs are intentionally invalidated by a Gateway restart.
	// Only remove files matching our own staging names in the dedicated store.
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}
	for _, entry := range entries {
		name := entry.Name()
		id := strings.TrimSuffix(name, ".bin")
		if !entry.Type().IsRegular() {
			continue
		}
		if strings.HasPrefix(name, ".transfer-") || (strings.HasSuffix(name, ".bin") && len(id) == 32 && strings.Trim(id, "0123456789abcdef") == "") {
			if err := os.Remove(filepath.Join(dir, name)); err != nil {
				return nil, err
			}
		}
	}
	return &v2Manager{h: h, origin: strings.TrimSuffix(origin, "/"), dir: dir, records: map[string]*v2Transfer{}, receivers: make(chan struct{}, 4)}, nil
}
func (m *v2Manager) active(a *agent) bool {
	m.h.mu.Lock()
	defer m.h.mu.Unlock()
	return m.h.agents[a.id] == a && !a.revoked && a.temporaryID == ""
}
func (m *v2Manager) cleanup() {
	now := time.Now()
	for id, t := range m.records {
		if !now.Before(t.Expires) {
			if t.cancel != nil {
				t.cancel()
			}
			if t.path != "" {
				_ = os.Remove(t.path)
			}
			delete(m.records, id)
		}
	}
}
func (m *v2Manager) snapshot(id string) (v2Record, bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.cleanup()
	t, ok := m.records[id]
	if !ok {
		return v2Record{}, false
	}
	return t.v2Record, true
}
func (m *v2Manager) start(device, path string) (v2Record, error) {
	return m.prepare(device, path, "download", 0, "")
}
func (m *v2Manager) startUpload(device, path string, size int64, digest string) (v2Record, error) {
	b, err := hex.DecodeString(digest)
	if size < 0 || size > v2Limit || err != nil || len(b) != sha256.Size {
		return v2Record{}, errors.New("invalid size or SHA-256")
	}
	return m.prepare(device, path, "upload", size, strings.ToLower(digest))
}
func (m *v2Manager) prepare(device, path, direction string, size int64, digest string) (v2Record, error) {
	if !validDeviceID.MatchString(device) || path == "" || len(path) > 4096 {
		return v2Record{}, errors.New("invalid device or path")
	}
	m.h.mu.Lock()
	a := m.h.agents[device]
	supported := false
	if a != nil {
		c, ok := capability.Find(a.capabilities, capability.FileV2)
		supported = ok && c.Supported && c.Available && a.temporaryID == "" && !a.revoked
	}
	m.h.mu.Unlock()
	if !supported {
		return v2Record{}, errors.New("online permanent file-v2 device required")
	}
	id, err := newRequestID()
	if err != nil {
		return v2Record{}, err
	}
	receive, err := v2Key()
	if err != nil {
		return v2Record{}, err
	}
	read, err := v2Key()
	if err != nil {
		return v2Record{}, err
	}
	name := filepath.Base(strings.ReplaceAll(path, "\\", "/"))
	if name == "." || name == "/" || name == "" {
		return v2Record{}, errors.New("invalid filename")
	}
	ctx, cancel := context.WithTimeout(context.Background(), v2TTL)
	t := &v2Transfer{v2Record: v2Record{ID: id, Device: device, Name: name, Direction: direction, Status: "pending", Size: size, SHA: digest, Expires: time.Now().Add(v2TTL)}, agent: a, receiveKey: receive, readKey: read, cancel: cancel, ctx: ctx, remotePath: path}
	if direction == "upload" {
		t.URL = m.origin + v2Prefix + "stage/" + id + "/" + read
	}
	m.mu.Lock()
	m.cleanup()
	if len(m.records) >= 4 {
		m.mu.Unlock()
		cancel()
		return v2Record{}, errors.New("transfer capacity reached")
	}
	for _, old := range m.records {
		if old.Device == device && (old.Status == "pending" || old.Status == "transferring" || old.Status == "staged" || old.Status == "delivering") {
			m.mu.Unlock()
			cancel()
			return v2Record{}, errors.New("device transfer busy")
		}
	}
	m.records[id] = t
	r := t.v2Record
	m.mu.Unlock()
	if direction == "download" {
		go m.dispatch(ctx, t, path)
	}
	return r, nil
}
func (m *v2Manager) dispatch(ctx context.Context, t *v2Transfer, path string) {
	defer t.cancel()
	ch := make(chan wire.Envelope, 1)
	m.h.mu.Lock()
	m.h.pending[t.ID] = pending{agent: t.agent, ch: ch}
	m.h.mu.Unlock()
	defer func() { m.h.mu.Lock(); delete(m.h.pending, t.ID); m.h.mu.Unlock() }()
	if !m.active(t.agent) {
		m.fail(t, "device disconnected")
		return
	}
	sendCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	err := t.agent.send(sendCtx, wire.Envelope{Type: "file_v2_send", TaskID: t.ID, TransferID: t.ID, Path: path, Feature: t.receiveKey})
	cancel()
	if err != nil {
		m.fail(t, "dispatch failed")
		return
	}
	select {
	case r := <-ch:
		if r.Error != "" {
			m.fail(t, r.Error)
		} else {
			m.fail(t, "transfer did not complete")
		}
	case <-ctx.Done():
		m.fail(t, "transfer interrupted or expired")
	}
}
func (m *v2Manager) fail(t *v2Transfer, message string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.records[t.ID] == t && (t.Status == "pending" || t.Status == "transferring" || t.Status == "staged" || t.Status == "delivering") {
		t.Status = "failed"
		t.URL = ""
		t.Error = message
		t.cancel()
	}
}
func (m *v2Manager) cancel(id string) bool {
	m.mu.Lock()
	m.cleanup()
	t := m.records[id]
	if t == nil {
		m.mu.Unlock()
		return false
	}
	active := t.Status == "pending" || t.Status == "transferring" || t.Status == "staged" || t.Status == "delivering"
	t.cancel()
	if t.path != "" {
		_ = os.Remove(t.path)
	}
	delete(m.records, id)
	m.mu.Unlock()
	if active && m.active(t.agent) {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		_ = t.agent.send(ctx, wire.Envelope{Type: "file_v2_cancel", TransferID: id})
	}
	return true
}
func v2Equal(a, b string) bool {
	return len(a) == len(b) && subtle.ConstantTimeCompare([]byte(a), []byte(b)) == 1
}
func (m *v2Manager) serve(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Referrer-Policy", "no-referrer")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	parts := strings.Split(strings.TrimPrefix(r.URL.Path, v2Prefix), "/")
	if len(parts) == 2 && parts[0] == "receive" && r.Method == http.MethodPut {
		m.receive(w, r, parts[1], "")
		return
	}
	if len(parts) == 3 && parts[0] == "stage" && r.Method == http.MethodPut {
		m.receive(w, r, parts[1], parts[2])
		return
	}
	if len(parts) == 2 && parts[0] == "source" && r.Method == http.MethodGet {
		m.source(w, r, parts[1])
		return
	}
	if len(parts) == 3 && parts[0] == "download" && (r.Method == http.MethodGet || r.Method == http.MethodHead) {
		m.download(w, r, parts[1], parts[2])
		return
	}
	http.NotFound(w, r)
}
func (m *v2Manager) receive(w http.ResponseWriter, r *http.Request, id, stageKey string) {
	select {
	case m.receivers <- struct{}{}:
		defer func() { <-m.receivers }()
	default:
		http.Error(w, "transfer capacity reached", 503)
		return
	}
	m.mu.Lock()
	m.cleanup()
	t := m.records[id]
	if t == nil || t.Status != "pending" || !(t.Direction == "download" && stageKey == "" && v2Equal(r.Header.Get("Authorization"), "Bearer "+t.receiveKey) || t.Direction == "upload" && stageKey != "" && v2Equal(stageKey, t.readKey)) {
		m.mu.Unlock()
		http.NotFound(w, r)
		return
	}
	if !m.active(t.agent) {
		m.mu.Unlock()
		http.Error(w, "device disconnected", 409)
		return
	}
	size := r.ContentLength
	digest := strings.ToLower(r.Header.Get("X-Reinor-SHA256"))
	if t.Direction == "upload" {
		if size != t.Size {
			m.mu.Unlock()
			http.Error(w, "incorrect file size", 400)
			return
		}
		digest = t.SHA
	}
	decoded, err := hex.DecodeString(digest)
	if size < 0 || size > v2Limit || err != nil || len(decoded) != sha256.Size || r.Header.Get("Content-Type") != "application/octet-stream" {
		m.mu.Unlock()
		http.Error(w, "invalid file metadata", 400)
		return
	}
	t.Status = "transferring"
	t.URL = ""
	t.Size = size
	m.mu.Unlock()
	f, err := os.CreateTemp(m.dir, ".transfer-")
	if err != nil {
		m.fail(t, "storage unavailable")
		http.Error(w, "storage unavailable", 500)
		return
	}
	defer f.Close()
	defer os.Remove(f.Name())
	_ = http.NewResponseController(w).SetReadDeadline(t.Expires)
	hash := sha256.New()
	reader := http.MaxBytesReader(w, r.Body, v2Limit)
	n, err := io.Copy(io.MultiWriter(f, hash, &v2Progress{m: m, t: t}), reader)
	if err != nil || n != size || hex.EncodeToString(hash.Sum(nil)) != digest {
		m.fail(t, "incomplete transfer or checksum mismatch")
		http.Error(w, "verification failed", 400)
		return
	}
	if err = f.Sync(); err == nil {
		err = f.Close()
	}
	if err != nil {
		m.fail(t, "storage failure")
		http.Error(w, "storage failure", 500)
		return
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.records[id] != t || t.Status != "transferring" || !time.Now().Before(t.Expires) || !m.active(t.agent) {
		http.Error(w, "transfer no longer authorized", 409)
		return
	}
	dest := filepath.Join(m.dir, id+".bin")
	if err = os.Rename(f.Name(), dest); err != nil {
		t.Status = "failed"
		t.Error = "storage failure"
		http.Error(w, "storage failure", 500)
		return
	}
	t.path = dest
	if t.Direction == "upload" {
		t.Status = "staged"
		go m.dispatchUpload(t)
		w.WriteHeader(http.StatusNoContent)
		return
	}
	t.Status = "completed"
	t.SHA = digest
	t.URL = m.origin + v2Prefix + "download/" + id + "/" + t.readKey
	t.Expires = time.Now().Add(10 * time.Minute)
	w.WriteHeader(http.StatusNoContent)
}

type v2Progress struct {
	m *v2Manager
	t *v2Transfer
}

func (p *v2Progress) Write(b []byte) (int, error) {
	p.m.mu.Lock()
	defer p.m.mu.Unlock()
	if p.m.records[p.t.ID] != p.t || p.t.Status != "transferring" || !time.Now().Before(p.t.Expires) {
		return 0, errors.New("transfer ended")
	}
	p.t.Received += int64(len(b))
	return len(b), nil
}
func (m *v2Manager) download(w http.ResponseWriter, r *http.Request, id, key string) {
	m.mu.Lock()
	m.cleanup()
	t := m.records[id]
	if t == nil || t.Direction != "download" || t.Status != "completed" || !v2Equal(key, t.readKey) {
		m.mu.Unlock()
		http.NotFound(w, r)
		return
	}
	f, err := os.Open(t.path)
	name := t.Name
	m.mu.Unlock()
	if err != nil {
		http.NotFound(w, r)
		return
	}
	defer f.Close()
	w.Header().Set("Content-Type", "application/octet-stream")
	w.Header().Set("Content-Disposition", mime.FormatMediaType("attachment", map[string]string{"filename": name}))
	http.ServeContent(w, r, name, time.Time{}, &v2GuardedFile{File: f, allowed: func() bool {
		m.mu.Lock()
		defer m.mu.Unlock()
		return m.records[id] == t && t.Status == "completed" && time.Now().Before(t.Expires)
	}})
}

type v2GuardedFile struct {
	File    *os.File
	allowed func() bool
}

func (f *v2GuardedFile) Read(b []byte) (int, error) {
	if !f.allowed() {
		return 0, errors.New("transfer authorization ended")
	}
	return f.File.Read(b)
}
func (f *v2GuardedFile) Seek(offset int64, whence int) (int64, error) {
	if !f.allowed() {
		return 0, errors.New("transfer authorization ended")
	}
	return f.File.Seek(offset, whence)
}
