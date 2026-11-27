package main

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/RinoPaw/Reinor/internal/wire"
)

// The data destination is derived from the authenticated control connection,
// never from an arbitrary URL supplied in a command.
func sendFileV2(ctx context.Context, gateway string, f wire.Envelope) error {
	return sendFileV2WithClient(ctx, gateway, f, fileV2HTTPClient())
}
func fileV2HTTPClient() *http.Client {
	return &http.Client{CheckRedirect: func(*http.Request, []*http.Request) error { return errors.New("file transfer redirects forbidden") }}
}
func sendFileV2WithClient(ctx context.Context, gateway string, f wire.Envelope, client *http.Client) error {
	u, err := url.Parse(gateway)
	if err != nil || u.Scheme != "wss" || u.Host == "" || u.User != nil {
		return errors.New("file-v2 requires WSS")
	}
	if len(f.TransferID) != 32 || strings.Trim(f.TransferID, "0123456789abcdef") != "" || len(f.Feature) != 64 {
		return errors.New("invalid transfer credentials")
	}
	u.Scheme = "https"
	u.Path = "/transfer/v2/receive/" + f.TransferID
	u.RawPath = ""
	u.RawQuery = ""
	u.Fragment = ""
	file, size, err := openRegular(f.Path)
	if err != nil {
		return err
	}
	defer file.Close()
	if size > 256<<20 {
		return errors.New("file exceeds v2 size limit")
	}
	hash := sha256.New()
	if _, err = io.Copy(hash, io.LimitReader(file, int64(size)+1)); err != nil {
		return err
	}
	if _, err = file.Seek(0, io.SeekStart); err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(ctx, 15*time.Minute)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodPut, u.String(), io.LimitReader(file, int64(size)))
	if err != nil {
		return err
	}
	req.ContentLength = int64(size)
	if size == 0 {
		req.Body = http.NoBody
	}
	req.Header.Set("Authorization", "Bearer "+f.Feature)
	req.Header.Set("Content-Type", "application/octet-stream")
	req.Header.Set("X-Reinor-SHA256", hex.EncodeToString(hash.Sum(nil)))

	resp, err := client.Do(req)
	if err != nil {
		return errors.New("binary transfer request failed")
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusNoContent {
		return fmt.Errorf("binary transfer rejected: HTTP %d", resp.StatusCode)
	}
	return nil
}
