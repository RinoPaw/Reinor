package capability

import (
	"reflect"
	"testing"
)

func TestRoundTrip(t *testing.T) {
	want := []Status{{ID: DesktopCapture, Supported: true, Available: true}}
	data, err := Encode(want)
	if err != nil {
		t.Fatal(err)
	}
	got, err := Decode(data)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("decoded=%+v", got)
	}
}

func TestCapabilityValidation(t *testing.T) {
	for _, raw := range []string{
		"", "null", "{}", "[", "[]suffix",
		`[{"id":"desktop.capture","supported":false,"available":true}]`,
		`[{"id":"desktop.capture","supported":true,"available":false}]`,
		`[{"id":"desktop.capture","supported":true,"available":true,"reason":"LOCKED"}]`,
		`[{"id":"desktop.capture","supported":true,"available":true},{"id":"desktop.capture","supported":true,"available":true}]`,
		`[{"id":"../unsafe","supported":true,"available":true}]`,
	} {
		if _, err := Decode([]byte(raw)); err == nil {
			t.Fatalf("accepted invalid capability payload: %q", raw)
		}
	}
}

func TestHeadlessDevice(t *testing.T) {
	x := Status{ID: DesktopCapture, Supported: false, Available: false, Reason: NoDesktopSession}
	encoded, err := Encode([]Status{x})
	if err != nil {
		t.Fatal(err)
	}
	decoded, err := Decode(encoded)
	if err != nil {
		t.Fatal(err)
	}
	got, ok := Find(decoded, DesktopCapture)
	if !ok || got != x {
		t.Fatalf("unexpected headless capability %+v", got)
	}
}
