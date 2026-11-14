package main

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"hash"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"sync"
	"time"

	"github.com/RinoPaw/Reinor/internal/wire"
)

// File operations are owner-authorized by the existing authenticated MCP boundary.
// Paths are absolute on the agent host, and all writes use its OS permissions.
// A temporary file is kept in the destination directory until the entire upload
// has been verified. Existing destination files are never overwritten.
const maxFileChunk = 128 << 10
const maxFileSize uint64 = 4 << 30
const maxUploads = 2
const uploadIdleTTL = 15 * time.Minute

var transferIDPattern = regexp.MustCompile(`^[0-9a-f]{32}$`)

type uploadState struct {
	file      *os.File
	temp      string
	path      string
	size      uint64
	written   uint64
	sha256    string
	hash      hash.Hash
	updatedAt time.Time
}

type fileTransfers struct {
	mu      sync.Mutex
	uploads map[string]*uploadState
}

func newFileTransfers() *fileTransfers {
	return &fileTransfers{uploads: make(map[string]*uploadState)}
}

func (s *fileTransfers) reapExpired(now time.Time) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for id, u := range s.uploads {
		if now.Sub(u.updatedAt) > uploadIdleTTL {
			s.discard(id)
		}
	}
}

func (s *fileTransfers) closeAll() {
	s.mu.Lock()
	defer s.mu.Unlock()
	for id, u := range s.uploads {
		u.file.Close()
		os.Remove(u.temp)
		delete(s.uploads, id)
	}
}

func validFilePath(path string) (string, error) {
	if path == "" || !filepath.IsAbs(path) || bytes.IndexByte([]byte(path), 0) >= 0 {
		return "", errors.New("absolute file path required")
	}
	return filepath.Clean(path), nil
}

func checkFileSize(size uint64) error {
	if size > maxFileSize {
		return fmt.Errorf("file exceeds %d byte limit", maxFileSize)
	}
	return nil
}

func (s *fileTransfers) handle(f wire.Envelope) wire.Envelope {
	reply := wire.Envelope{Type: "file_result", TaskID: f.TaskID, TransferID: f.TransferID}
	s.mu.Lock()
	defer s.mu.Unlock()
	var err error
	switch f.Type {
	case "file_start":
		reply, err = s.start(f, reply)
	case "file_chunk":
		reply, err = s.write(f, reply)
	case "file_finish":
		reply, err = s.finish(f, reply)
	case "file_abort":
		reply, err = s.abort(f, reply)
	case "file_info":
		reply, err = s.info(f, reply)
	case "file_read":
		reply, err = s.read(f, reply)
	default:
		err = errors.New("unsupported file operation")
	}
	if err != nil {
		reply.Error = err.Error()
	}
	return reply
}

func (s *fileTransfers) start(f, reply wire.Envelope) (wire.Envelope, error) {
	if !transferIDPattern.MatchString(f.TransferID) {
		return reply, errors.New("invalid transfer id")
	}
	if err := checkFileSize(f.Size); err != nil {
		return reply, err
	}
	hashBytes, err := hex.DecodeString(f.SHA256)
	if err != nil || len(hashBytes) != sha256.Size {
		return reply, errors.New("sha256 must be 64 hexadecimal characters")
	}
	path, err := validFilePath(f.Path)
	if err != nil {
		return reply, err
	}
	if len(s.uploads) >= maxUploads {
		return reply, errors.New("too many uploads")
	}
	if _, ok := s.uploads[f.TransferID]; ok {
		return reply, errors.New("transfer id already in use")
	}
	if _, err := os.Lstat(path); err == nil {
		return reply, errors.New("destination already exists")
	} else if !os.IsNotExist(err) {
		return reply, err
	}
	file, err := os.CreateTemp(filepath.Dir(path), ".reinor-upload-*")
	if err != nil {
		return reply, err
	}
	s.uploads[f.TransferID] = &uploadState{
		file: file, temp: file.Name(), path: path, size: f.Size,
		sha256: f.SHA256, hash: sha256.New(),
	}
	reply.Path = path
	reply.Size = f.Size
	return reply, nil
}

func (s *fileTransfers) discard(id string) {
	if u := s.uploads[id]; u != nil {
		u.file.Close()
		os.Remove(u.temp)
		delete(s.uploads, id)
	}
}

func (s *fileTransfers) write(f, reply wire.Envelope) (wire.Envelope, error) {
	u := s.uploads[f.TransferID]
	if u == nil {
		return reply, errors.New("unknown transfer id")
	}
	if len(f.Data) > maxFileChunk {
		return reply, errors.New("file chunk exceeds 128 KiB")
	}
	length := uint64(len(f.Data))
	if f.Offset > u.written || length > u.size-min(f.Offset, u.size) {
		return reply, errors.New("invalid chunk offset or length")
	}
	if f.Offset < u.written {
		// A lost response can be retried without silently duplicating bytes.
		if f.Offset+length > u.written {
			return reply, errors.New("overlapping chunk")
		}
		existing := make([]byte, len(f.Data))
		if _, err := u.file.ReadAt(existing, int64(f.Offset)); err != nil {
			return reply, err
		}
		if !bytes.Equal(existing, f.Data) {
			return reply, errors.New("retry differs from stored bytes")
		}
	} else {
		if _, err := u.file.Write(f.Data); err != nil {
			s.discard(f.TransferID)
			return reply, err
		}
		if _, err := u.hash.Write(f.Data); err != nil {
			s.discard(f.TransferID)
			return reply, err
		}
		u.written += length
	}
	reply.Offset = u.written
	reply.Size = u.size
	return reply, nil
}

func (s *fileTransfers) finish(f, reply wire.Envelope) (wire.Envelope, error) {
	u := s.uploads[f.TransferID]
	if u == nil {
		return reply, errors.New("unknown transfer id")
	}
	if u.written != u.size {
		reply.Offset = u.written
		return reply, errors.New("upload incomplete")
	}
	actual := hex.EncodeToString(u.hash.Sum(nil))
	if actual != u.sha256 {
		s.discard(f.TransferID)
		return reply, errors.New("sha256 mismatch; incomplete file removed")
	}
	if err := u.file.Sync(); err != nil {
		s.discard(f.TransferID)
		return reply, err
	}
	if err := u.file.Close(); err != nil {
		s.discard(f.TransferID)
		return reply, err
	}
	// Create the final name only after full verification. Hard-link creation is
	// atomic and rejects an existing target, including in a concurrent race.
	if err := os.Link(u.temp, u.path); err != nil {
		s.discard(f.TransferID)
		return reply, fmt.Errorf("atomic no-overwrite commit failed: %w", err)
	}
	delete(s.uploads, f.TransferID)
	os.Remove(u.temp)
	reply.Path, reply.Size, reply.SHA256 = u.path, u.size, actual
	return reply, nil
}

func (s *fileTransfers) abort(f, reply wire.Envelope) (wire.Envelope, error) {
	if s.uploads[f.TransferID] == nil {
		return reply, errors.New("unknown transfer id")
	}
	s.discard(f.TransferID)
	return reply, nil
}

func openRegular(path string) (*os.File, uint64, error) {
	clean, err := validFilePath(path)
	if err != nil {
		return nil, 0, err
	}
	file, err := os.Open(clean)
	if err != nil {
		return nil, 0, err
	}
	stat, err := file.Stat()
	if err != nil {
		file.Close()
		return nil, 0, err
	}
	if !stat.Mode().IsRegular() || stat.Size() < 0 {
		file.Close()
		return nil, 0, errors.New("regular file required")
	}
	size := uint64(stat.Size())
	if err := checkFileSize(size); err != nil {
		file.Close()
		return nil, 0, err
	}
	return file, size, nil
}

func (s *fileTransfers) info(f, reply wire.Envelope) (wire.Envelope, error) {
	file, size, err := openRegular(f.Path)
	if err != nil {
		return reply, err
	}
	defer file.Close()
	sum := sha256.New()
	if _, err := io.Copy(sum, file); err != nil {
		return reply, err
	}
	reply.Size, reply.SHA256 = size, hex.EncodeToString(sum.Sum(nil))
	return reply, nil
}

func (s *fileTransfers) read(f, reply wire.Envelope) (wire.Envelope, error) {
	file, size, err := openRegular(f.Path)
	if err != nil {
		return reply, err
	}
	defer file.Close()
	if f.Offset > size {
		return reply, errors.New("offset beyond end of file")
	}
	length := uint64(maxFileChunk)
	if size-f.Offset < length {
		length = size - f.Offset
	}
	data := make([]byte, int(length))
	if length > 0 {
		if _, err := file.ReadAt(data, int64(f.Offset)); err != nil {
			return reply, err
		}
	}
	sum := sha256.Sum256(data)
	reply.Offset, reply.Size, reply.Data = f.Offset, size, data
	reply.SHA256 = hex.EncodeToString(sum[:])
	return reply, nil
}
