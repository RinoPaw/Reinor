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
