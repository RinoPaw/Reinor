package main

import (
	"path/filepath"
	"testing"
	"time"
)

func TestTemporaryGrantIsOneUseAndDurable(t *testing.T) {
	path := filepath.Join(t.TempDir(), "joins.json")
	s, err := newJoinStore(path)
	if err != nil {
		t.Fatal(err)
	}
	rec, secret, err := s.submitTemporary("temp-friend-mysql", "windows")
	if err != nil {
		t.Fatal(err)
	}
	if rec.Mode != "temporary" || rec.State != "pending" || !rec.ExpiresAt.After(time.Now()) {
		t.Fatalf("bad temporary request: %+v", rec)
	}
	if s.allowed(rec.DeviceID, secret) {
		t.Fatal("pending credential authorized")
	}
	approved, err := s.decide(rec.ID, true)
	if err != nil {
		t.Fatal(err)
	}
	if approved.State != "approved" || approved.ExpiresAt.Sub(time.Now()) > temporarySessionTTL || !approved.ExpiresAt.After(time.Now().Add(59*time.Minute)) {
		t.Fatalf("bad approved expiry: %+v", approved)
	}
	if !s.allowed(rec.DeviceID, secret) {
		t.Fatal("approved temporary ticket should work once")
	}
	granted, err := s.claim(rec.DeviceID, secret)
	if err != nil || granted.Mode != "temporary" || granted.UsedAt.IsZero() {
		t.Fatalf("claim: %+v, %v", granted, err)
	}
	if s.allowed(rec.DeviceID, secret) {
		t.Fatal("one-use ticket remains reusable")
	}
	if _, err := s.claim(rec.DeviceID, secret); err == nil {
		t.Fatal("second connection allowed")
	}
	reloaded, err := newJoinStore(path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := reloaded.claim(rec.DeviceID, secret); err == nil {
		t.Fatal("reconnect allowed after Gateway restart")
	}
	revoked, err := reloaded.revokeTemporary(rec.ID)
	if err != nil || revoked.State != "revoked" {
		t.Fatalf("revoke: %+v, %v", revoked, err)
	}
	if _, err := reloaded.claim(rec.DeviceID, secret); err == nil {
		t.Fatal("revoked credential accepted")
	}
	if _, err := reloaded.revokeTemporary(rec.ID); err != nil {
		t.Fatalf("idempotent revoke: %v", err)
	}
}

func TestTemporaryRejectsCollisionsAndExpiry(t *testing.T) {
	s, err := newJoinStore(filepath.Join(t.TempDir(), "joins.json"))
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := s.submitTemporary("friend-mysql", "windows"); err == nil {
		t.Fatal("missing temp prefix allowed")
	}
	rec, secret, err := s.submitTemporary("temp-friend-mysql", "windows")
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := s.submitTemporary(rec.DeviceID, "windows"); err == nil {
		t.Fatal("same device ID reused while pending")
	}
	if _, err := s.apply(rec.ID, func(r *joinRecord) error { r.ExpiresAt = time.Now().Add(-time.Second); return nil }); err != nil {
		t.Fatal(err)
	}
	if _, err := s.decide(rec.ID, true); err == nil {
		t.Fatal("expired join request approved")
	}
	status, err := s.status(rec.ID, secret)
	if err != nil || status.State != "expired" {
		t.Fatalf("pending expiry status: %+v %v", status, err)
	}
	rec2, secret2, err := s.submitTemporary(rec.DeviceID, "windows")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.decide(rec2.ID, true); err != nil {
		t.Fatal(err)
	}
	if _, _, err := s.submitTemporary(rec.DeviceID, "windows"); err == nil {
		t.Fatal("active approved ID collision")
	}
	if _, err := s.apply(rec2.ID, func(r *joinRecord) error { r.ExpiresAt = time.Now().Add(-time.Second); return nil }); err != nil {
		t.Fatal(err)
	}
	if s.allowed(rec2.DeviceID, secret2) {
		t.Fatal("expired approved ticket allowed")
	}
	if _, err := s.claim(rec2.DeviceID, secret2); err == nil {
		t.Fatal("expired approved ticket claimed")
	}
}

func TestTemporaryCannotRevokeLongLivedJoin(t *testing.T) {
	s, err := newJoinStore(filepath.Join(t.TempDir(), "joins.json"))
	if err != nil {
		t.Fatal(err)
	}
	rec, secret, err := s.submit("my-personal-device", "windows")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.decide(rec.ID, true); err != nil {
		t.Fatal(err)
	}
	if _, err := s.revokeTemporary(rec.ID); err == nil {
		t.Fatal("revoke unexpectedly disabled permanent Agent")
	}
	if !s.allowed(rec.DeviceID, secret) {
		t.Fatal("permanent ticket broken")
	}
	first, err := s.claim(rec.DeviceID, secret)
	if err != nil || first.Mode == "temporary" {
		t.Fatalf("permanent claim failed: %v", err)
	}
	if _, err := s.claim(rec.DeviceID, secret); err != nil {
		t.Fatalf("permanent reconnect broken: %v", err)
	}
}

func TestTemporaryConsentDurationAndBounds(t *testing.T) {
	s, err := newJoinStore(filepath.Join(t.TempDir(), "joins.json"))
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		id       string
		duration time.Duration
	}{
		{"temp-1second", time.Second},
		{"temp-90seconds", 90 * time.Second},
		{"temp-30minutes", 30 * time.Minute},
		{"temp-2hours", 2 * time.Hour},
		{"temp-3h30m", 3*time.Hour + 30*time.Minute},
		{"temp-48hours", 48 * time.Hour},
		{"temp-240hours", 240 * time.Hour},
	} {
		rec, ticket, err := s.submitTemporaryWithDuration(tc.id, "windows", tc.duration)
		if err != nil {
			t.Fatal(err)
		}
		if rec.Duration != tc.duration.String() {
			t.Fatalf("duration not persisted: %+v", rec)
		}
		approved, err := s.decide(rec.ID, true)
		if err != nil {
			t.Fatal(err)
		}
		remaining := time.Until(approved.ExpiresAt)
		if remaining > tc.duration || remaining < tc.duration-time.Minute {
			t.Fatalf("unexpected expiry: %s", remaining)
		}
		if !s.allowed(tc.id, ticket) {
			t.Fatal("approved ticket rejected")
		}
	}
	for _, d := range []time.Duration{0, -time.Nanosecond, -time.Hour} {
		if _, _, err := s.submitTemporaryWithDuration("temp-invalid", "windows", d); err == nil {
			t.Fatalf("accepted unsupported duration %s", d)
		}
	}
}

func TestLegacyTemporaryStoredWithoutDurationDefaultsToOneHour(t *testing.T) {
	s, err := newJoinStore(filepath.Join(t.TempDir(), "joins.json"))
	if err != nil {
		t.Fatal(err)
	}
	rec, _, err := s.submitTemporary("temp-old-client", "windows")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.apply(rec.ID, func(r *joinRecord) error { r.Duration = ""; return nil }); err != nil {
		t.Fatal(err)
	}
	approved, err := s.decide(rec.ID, true)
	if err != nil {
		t.Fatal(err)
	}
	if remaining := time.Until(approved.ExpiresAt); remaining > time.Hour || remaining < 59*time.Minute {
		t.Fatalf("old session lost default TTL: %s", remaining)
	}
}
