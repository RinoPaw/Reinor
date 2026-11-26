package main

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"flag"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"
)

const androidUpdateURL = "https://xuhuaai.top/downloads/reinor/reinor-agent-android-arm64"
const maxAgentUpdateSize = 64 << 20

// Update only the executable, never the enrollment or service configuration.
// Activation happens on the next launch, not by killing an existing session.
func updateMain(args []string) error {
	if runtime.GOOS != "android" || runtime.GOARCH != "arm64" {
		return errors.New("native update currently supports Android ARM64 only")
	}
	fs := flag.NewFlagSet("update", flag.ContinueOnError)
	expected := fs.String("sha256", "", "independently verified SHA-256 of the artifact")
	source := fs.String("url", androidUpdateURL, "HTTPS artifact URL")
	local := fs.String("file", "", "local staged artifact (instead of URL)")
	dryRun := fs.Bool("dry-run", false, "verify without replacing executable")
	rollback := fs.Bool("rollback", false, "restore the previous executable using its verified SHA-256")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if fs.NArg() != 0 {
		return errors.New("unexpected positional arguments")
	}
	digest, err := parseUpdateDigest(*expected)
	if err != nil {
		return err
	}
	target, err := os.Executable()
	if err != nil {
		return err
	}
	target, err = filepath.EvalSymlinks(target)
	if err != nil {
		return err
	}
	if *rollback {
		if *local != "" || *source != androidUpdateURL {
			return errors.New("rollback cannot be combined with a file or URL")
		}
		*local = target + ".previous"
	}
	var reader io.ReadCloser
	if *local != "" {
		reader, err = os.Open(*local)
	} else {
		reader, err = downloadAgentUpdate(*source)
	}
	if err != nil {
		return err
	}
	defer reader.Close()
	if err := replaceAgentExecutable(target, reader, digest, *dryRun); err != nil {
		return err
	}
	if *dryRun {
		fmt.Println("Artifact verified; executable unchanged.")
	} else {
		fmt.Printf("Installed %x. Previous binary: %s.previous\nRestart the Agent to activate. Enrollment unchanged.\n", digest, target)
	}
	return nil
}

func parseUpdateDigest(s string) ([sha256.Size]byte, error) {
	var digest [sha256.Size]byte
	raw, err := hex.DecodeString(s)
	if err != nil || len(raw) != sha256.Size {
		return digest, errors.New("a verified 64-character SHA-256 is required")
	}
	copy(digest[:], raw)
	return digest, nil
}

func downloadAgentUpdate(raw string) (io.ReadCloser, error) {
	u, err := url.Parse(raw)
	if err != nil || u.Scheme != "https" || u.Host == "" || u.User != nil || u.Fragment != "" {
		return nil, errors.New("update URL must use HTTPS without credentials or fragments")
	}
	client := &http.Client{
		Timeout: 90 * time.Second,
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			if len(via) >= 5 || req.URL.Scheme != "https" || req.URL.User != nil {
				return errors.New("unsafe update redirect")
			}
			return nil
		},
	}
	resp, err := client.Get(u.String())
	if err != nil {
		return nil, err
	}
	if resp.StatusCode != http.StatusOK || resp.ContentLength > maxAgentUpdateSize {
		resp.Body.Close()
		return nil, fmt.Errorf("unexpected update response: %s, size %d", resp.Status, resp.ContentLength)
	}
	return resp.Body, nil
}

// Stage and verify on the same filesystem before replacing either executable.
// An interrupted download or digest mismatch leaves the installed binary intact.
func replaceAgentExecutable(target string, source io.Reader, expected [sha256.Size]byte, dryRun bool) error {
	info, err := os.Lstat(target)
	if err != nil {
		return err
	}
	if !info.Mode().IsRegular() {
		return errors.New("installed executable must be a regular file")
	}
	lock := target + ".update-lock"
	if err := os.Mkdir(lock, 0700); err != nil {
		return fmt.Errorf("acquire update lock: %w", err)
	}
	defer os.Remove(lock)
	dir := filepath.Dir(target)
	stage, err := os.CreateTemp(dir, ".reinor-update-*")
	if err != nil {
		return err
	}
	defer os.Remove(stage.Name())
	defer stage.Close()
	h := sha256.New()
	n, err := io.Copy(io.MultiWriter(stage, h), io.LimitReader(source, maxAgentUpdateSize+1))
	if err != nil {
		return err
	}
	if n == 0 || n > maxAgentUpdateSize {
		return errors.New("invalid update artifact size")
	}
	if !strings.EqualFold(hex.EncodeToString(h.Sum(nil)), hex.EncodeToString(expected[:])) {
		return errors.New("update SHA-256 mismatch")
	}
	if err := stage.Chmod(info.Mode().Perm()); err != nil {
		return err
	}
	if err := stage.Sync(); err != nil {
		return err
	}
	if err := stage.Close(); err != nil {
		return err
	}
	if dryRun {
		return nil
	}

	// Keep the previous executable in a separate, complete file. Do not move
	// the live executable out of the way: the final replacement is atomic.
	current, err := os.Open(target)
	if err != nil {
		return err
	}
	defer current.Close()
	backup, err := os.CreateTemp(dir, ".reinor-backup-*")
	if err != nil {
		return err
	}
	defer os.Remove(backup.Name())
	defer backup.Close()
	if _, err := io.Copy(backup, current); err != nil {
		return err
	}
	if err := current.Close(); err != nil {
		return err
	}
	if err := backup.Chmod(info.Mode().Perm()); err != nil {
		return err
	}
	if err := backup.Sync(); err != nil {
		return err
	}
	if err := backup.Close(); err != nil {
		return err
	}
	if err := os.Rename(backup.Name(), target+".previous"); err != nil {
		return err
	}
	if err := os.Rename(stage.Name(), target); err != nil {
		return fmt.Errorf("activate verified update: %w", err)
	}
	return nil
}
