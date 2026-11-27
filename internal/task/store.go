package task

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"sync"
	"time"
)

type Status string

const (
	Pending    Status = "pending"
	Dispatched Status = "dispatched"
	Running    Status = "running"
	Succeeded  Status = "succeeded"
	Failed     Status = "failed"
	TimedOut   Status = "timed_out"
	Unknown    Status = "unknown"
	Cancelled  Status = "cancelled"
)

func (s Status) Active() bool { return s == Pending || s == Dispatched || s == Running }

type Record struct {
	ID              string    `json:"task_id"`
	DeviceID        string    `json:"device_id"`
	Status          Status    `json:"status"`
	CreatedAt       time.Time `json:"created_at"`
	UpdatedAt       time.Time `json:"updated_at"`
	CancelRequested bool      `json:"cancel_requested,omitempty"`
	TimeoutSeconds  int       `json:"timeout_seconds"`
	ExitCode        *int32    `json:"exit_code,omitempty"`
	Stdout          string    `json:"stdout,omitempty"`
	Stderr          string    `json:"stderr,omitempty"`
	Error           string    `json:"error,omitempty"`
}

var validID = regexp.MustCompile("^[a-f0-9]{32}$")

type Store struct {
	mu      sync.Mutex
	dir     string
	records map[string]Record
	cancels map[string]chan struct{}
}

func Open(dir string) (*Store, error) {
	if err := os.MkdirAll(dir, 0700); err != nil {
		return nil, err
	}
	s := &Store{dir: dir, records: map[string]Record{}, cancels: map[string]chan struct{}{}}
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}
	for _, e := range entries {
		if e.IsDir() || filepath.Ext(e.Name()) != ".json" {
			continue
		}
		raw, err := os.ReadFile(filepath.Join(dir, e.Name()))
		if err != nil {
			return nil, err
		}
		var r Record
		if err = json.Unmarshal(raw, &r); err != nil {
			return nil, err
		}
		if !validID.MatchString(r.ID) || e.Name() != r.ID+".json" {
			return nil, errors.New("invalid task record")
		}
		if r.Status.Active() {
			r.Status = Unknown
			r.Error = "Gateway restarted; outcome unknown; not retried"
			r.UpdatedAt = time.Now().UTC()
			if err = s.write(r); err != nil {
				return nil, err
			}
		}
		s.records[r.ID] = r
	}
	return s, nil
}
func (s *Store) write(r Record) error {
	data, err := json.Marshal(r)
	if err != nil {
		return err
	}
	f, err := os.CreateTemp(s.dir, ".task-")
	if err != nil {
		return err
	}
	defer os.Remove(f.Name())
	defer f.Close()
	if _, err = f.Write(data); err != nil {
		return err
	}
	if err = f.Sync(); err != nil {
		return err
	}
	if err = f.Close(); err != nil {
		return err
	}
	return os.Rename(f.Name(), filepath.Join(s.dir, r.ID+".json"))
}
func (s *Store) Begin(r Record) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if !validID.MatchString(r.ID) || r.DeviceID == "" || r.TimeoutSeconds <= 0 {
		return errors.New("invalid task")
	}
	if _, ok := s.records[r.ID]; ok {
		return errors.New("duplicate task ID")
	}
	for _, v := range s.records {
		if v.DeviceID == r.DeviceID && v.Status.Active() {
			return errors.New("device already has an active task")
		}
	}
	r.Status = Pending
	r.CreatedAt = time.Now().UTC()
	r.UpdatedAt = r.CreatedAt
	if err := s.write(r); err != nil {
		return err
	}
	s.records[r.ID] = r
	s.cancels[r.ID] = make(chan struct{})
	return nil
}
func (s *Store) Update(id string, status Status, exit *int32, out, stderr, message string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	r, ok := s.records[id]
	if !ok {
		return errors.New("task not found")
	}
	if !(r.Status == Pending && (status == Dispatched || status == Failed || status == Unknown || status == Cancelled) || (r.Status == Dispatched || r.Status == Running) && (status == Succeeded || status == Failed || status == TimedOut || status == Unknown || status == Cancelled)) {
		return errors.New("invalid task transition")
	}
	r.Status = status
	r.UpdatedAt = time.Now().UTC()
	r.ExitCode = exit
	if status != Unknown {
		if len(out) > 65536 {
			out = out[:65536]
		}
		if len(stderr) > 65536 {
			stderr = stderr[:65536]
		}
		r.Stdout = out
		r.Stderr = stderr
	}
	r.Error = message
	if err := s.write(r); err != nil {
		return err
	}
	s.records[id] = r
	if !status.Active() {
		delete(s.cancels, id)
	}
	return nil
}
func (s *Store) Get(id string) (Record, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	r, ok := s.records[id]
	return r, ok
}
func (s *Store) List(device string, limit int) []Record {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]Record, 0)
	for _, r := range s.records {
		if device == "" || r.DeviceID == device {
			r.Stdout = ""
			r.Stderr = ""
			out = append(out, r)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].CreatedAt.After(out[j].CreatedAt) })
	if len(out) > limit {
		out = out[:limit]
	}
	return out
}

func (s *Store) CancelSignal(id string) <-chan struct{} {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.cancels[id]
}
func (s *Store) RequestCancel(id string) (Record, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	r, ok := s.records[id]
	if !ok {
		return Record{}, errors.New("task not found")
	}
	if !r.Status.Active() {
		return r, nil
	}
	if r.CancelRequested {
		return r, nil
	}
	ch := s.cancels[id]
	if ch == nil {
		return Record{}, errors.New("task cancellation unavailable")
	}
	r.CancelRequested = true
	r.UpdatedAt = time.Now().UTC()
	if err := s.write(r); err != nil {
		return Record{}, err
	}
	s.records[id] = r
	close(ch)
	return r, nil
}

func (s *Store) UpdateProgress(id, out, stderr string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	r, ok := s.records[id]
	if !ok || (r.Status != Dispatched && r.Status != Running) {
		return errors.New("task not running")
	}
	if len(out) > 65536 || len(stderr) > 65536 {
		return errors.New("task output exceeds limit")
	}
	if len(out) < len(r.Stdout) || len(stderr) < len(r.Stderr) {
		return errors.New("task output regressed")
	}
	if r.Status == Running && r.Stdout == out && r.Stderr == stderr {
		return nil
	}
	r.Status = Running
	r.Stdout = out
	r.Stderr = stderr
	r.UpdatedAt = time.Now().UTC()
	if err := s.write(r); err != nil {
		return err
	}
	s.records[id] = r
	return nil
}
