package task

import (
	"strings"
	"testing"
)

func TestLifecycle(t *testing.T) {
	dir := t.TempDir()
	s, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	id := strings.Repeat("a", 32)
	if err = s.Begin(Record{ID: id, DeviceID: "pc", TimeoutSeconds: 300}); err != nil {
		t.Fatal(err)
	}
	if err = s.Begin(Record{ID: strings.Repeat("b", 32), DeviceID: "pc", TimeoutSeconds: 300}); err == nil {
		t.Fatal("accepted parallel task")
	}
	if err = s.Update(id, Dispatched, nil, "", "", ""); err != nil {
		t.Fatal(err)
	}
	code := int32(0)
	if err = s.Update(id, Succeeded, &code, "hello", "", ""); err != nil {
		t.Fatal(err)
	}
	if err = s.Update(id, Failed, nil, "", "", ""); err == nil {
		t.Fatal("mutated terminal state")
	}
	s, err = Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	r, ok := s.Get(id)
	if !ok || r.Status != Succeeded || r.Stdout != "hello" || r.ExitCode == nil || *r.ExitCode != 0 {
		t.Fatalf("%+v", r)
	}
	if got := s.List("pc", 10); len(got) != 1 || got[0].Stdout != "" {
		t.Fatalf("%+v", got)
	}
	id = strings.Repeat("c", 32)
	if err = s.Begin(Record{ID: id, DeviceID: "pc", TimeoutSeconds: 300}); err != nil {
		t.Fatal(err)
	}
	if err = s.Update(id, Dispatched, nil, "", "", ""); err != nil {
		t.Fatal(err)
	}
	s, err = Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	r, ok = s.Get(id)
	if !ok || r.Status != Unknown {
		t.Fatalf("%+v", r)
	}
}

func TestProgressAndCancelSurviveRestart(t *testing.T) {
	dir := t.TempDir()
	s, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	id := strings.Repeat("d", 32)
	if err = s.Begin(Record{ID: id, DeviceID: "pc", TimeoutSeconds: 300}); err != nil {
		t.Fatal(err)
	}
	signal := s.CancelSignal(id)
	if err = s.Update(id, Dispatched, nil, "", "", ""); err != nil {
		t.Fatal(err)
	}
	if err = s.UpdateProgress(id, "first", ""); err != nil {
		t.Fatal(err)
	}
	if err = s.UpdateProgress(id, "", ""); err == nil {
		t.Fatal("regressing output accepted")
	}
	r, err := s.RequestCancel(id)
	if err != nil || !r.CancelRequested || r.Status != Running {
		t.Fatalf("cancel: %+v %v", r, err)
	}
	select {
	case <-signal:
	default:
		t.Fatal("cancel signal not closed")
	}
	if _, err = s.RequestCancel(id); err != nil {
		t.Fatal(err)
	}
	s, err = Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	r, ok := s.Get(id)
	if !ok || r.Status != Unknown || r.Stdout != "first" || !r.CancelRequested {
		t.Fatalf("restart: %+v", r)
	}
}
