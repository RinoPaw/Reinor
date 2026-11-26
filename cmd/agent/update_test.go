package main

import (
	"bytes"
	"crypto/sha256"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestVerifiedAgentUpdate(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "reinor-agent")
	credential := filepath.Join(dir, "joined-test.json")
	if err := os.WriteFile(target, []byte("old binary"), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(credential, []byte("enrollment"), 0600); err != nil {
		t.Fatal(err)
	}
	check := func(path, expected string) {
		t.Helper()
		b, err := os.ReadFile(path)
		if err != nil || string(b) != expected {
			t.Fatalf("%s = %q, %v; want %q", path, b, err, expected)
		}
	}
	next := sha256.Sum256([]byte("new binary"))
	if err := replaceAgentExecutable(target, strings.NewReader("corrupt"), next, false); err == nil {
		t.Fatal("accepted wrong digest")
	}
	check(target, "old binary")
	if _, err := os.Stat(target + ".previous"); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("unexpected backup after mismatch: %v", err)
	}
	if err := replaceAgentExecutable(target, strings.NewReader("new binary"), next, true); err != nil {
		t.Fatal(err)
	}
	check(target, "old binary")
	if err := replaceAgentExecutable(target, strings.NewReader("new binary"), next, false); err != nil {
		t.Fatal(err)
	}
	check(target, "new binary")
	check(target+".previous", "old binary")
	check(credential, "enrollment")
	if info, err := os.Stat(target); err != nil || info.Mode().Perm()&0100 == 0 {
		t.Fatalf("executable permission lost: %v, %v", info, err)
	}
	old, err := os.ReadFile(target + ".previous")
	if err != nil {
		t.Fatal(err)
	}
	if err := replaceAgentExecutable(target, bytes.NewReader(old), sha256.Sum256([]byte("old binary")), false); err != nil {
		t.Fatal(err)
	}
	check(target, "old binary")
	check(target+".previous", "new binary")
	check(credential, "enrollment")
}

type brokenUpdateReader struct{}

func (brokenUpdateReader) Read([]byte) (int, error) { return 0, io.ErrUnexpectedEOF }

func TestUpdateInterruptionAndLock(t *testing.T) {
	target := filepath.Join(t.TempDir(), "reinor-agent")
	if err := os.WriteFile(target, []byte("old"), 0700); err != nil {
		t.Fatal(err)
	}
	if err := replaceAgentExecutable(target, brokenUpdateReader{}, sha256.Sum256(nil), false); err == nil {
		t.Fatal("accepted interrupted source")
	}
	if err := os.Mkdir(target+".update-lock", 0700); err != nil {
		t.Fatal(err)
	}
	if err := replaceAgentExecutable(target, strings.NewReader("new"), sha256.Sum256([]byte("new")), false); err == nil {
		t.Fatal("accepted concurrent update")
	}
	b, err := os.ReadFile(target)
	if err != nil || string(b) != "old" {
		t.Fatalf("original changed: %q, %v", b, err)
	}
}

func TestUpdateDigest(t *testing.T) {
	for _, s := range []string{"", "abc", strings.Repeat("z", 64), strings.Repeat("a", 65)} {
		if _, err := parseUpdateDigest(s); err == nil {
			t.Fatalf("accepted invalid hash %q", s)
		}
	}
	if _, err := parseUpdateDigest(strings.Repeat("A", 64)); err != nil {
		t.Fatal(err)
	}
}
