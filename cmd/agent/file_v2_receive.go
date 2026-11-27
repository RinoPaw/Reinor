package main

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/RinoPaw/Reinor/internal/wire"
)

func receiveFileV2(ctx context.Context, gateway string, f wire.Envelope) error {
	return receiveFileV2WithClient(ctx, gateway, f, fileV2HTTPClient())
}
func receiveFileV2WithClient(ctx context.Context, gateway string, f wire.Envelope, client *http.Client) error {
	u, err := url.Parse(gateway)
	if err != nil || u.Scheme != "wss" || u.Host == "" || u.User != nil {
		return errors.New("file-v2 requires WSS")
	}
	if len(f.TransferID) != 32 || strings.Trim(f.TransferID, "0123456789abcdef") != "" || len(f.Feature) != 64 || f.Size > 256<<20 {
		return errors.New("invalid transfer metadata")
	}
	digest, err := hex.DecodeString(f.SHA256)
	if err != nil || len(digest) != sha256.Size {
		return errors.New("invalid checksum")
	}
	path, err := validFilePath(f.Path)
	if err != nil {
		return err
	}
	if _, err = os.Lstat(path); !os.IsNotExist(err) {
		return errors.New("destination exists or cannot be checked")
	}
	u.Scheme = "https"
	u.Path = "/transfer/v2/source/" + f.TransferID
	u.RawPath = ""
	u.RawQuery = ""
	u.Fragment = ""
	ctx, cancel := context.WithTimeout(ctx, 15*time.Minute)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+f.Feature)

	resp, err := client.Do(req)
	if err != nil {
		return errors.New("binary transfer request failed")
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK || resp.ContentLength != int64(f.Size) || resp.Header.Get("Content-Type") != "application/octet-stream" {
		return errors.New("invalid binary source response")
	}
	temp, err := os.CreateTemp(filepath.Dir(path), ".reinor-transfer-")
	if err != nil {
		return err
	}
	defer temp.Close()
	defer os.Remove(temp.Name())
	hash := sha256.New()
	n, err := io.Copy(io.MultiWriter(temp, hash), io.LimitReader(resp.Body, int64(f.Size)+1))
	if err != nil || n != int64(f.Size) || hex.EncodeToString(hash.Sum(nil)) != f.SHA256 {
		return errors.New("binary verification failed")
	}
	if err = temp.Sync(); err != nil {
		return err
	}
	if err = temp.Close(); err != nil {
		return err
	}
	if err = ctx.Err(); err != nil {
		return err
	}
	if err = os.Link(temp.Name(), path); err != nil {
		return errors.New("atomic no-overwrite publication failed")
	}
	return nil
}
