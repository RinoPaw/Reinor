package wire

import (
	"bytes"
	"testing"

	"github.com/RinoPaw/Reinor/internal/capability"
)

func TestCapabilityAndErrorCodeRoundTrip(t *testing.T) {
	encoded,err:=capability.Encode([]capability.Status{{
		ID:capability.DesktopCapture,
		Supported:false,
		Available:false,
		Reason:capability.NoDesktopSession,
	}})
	if err!=nil{t.Fatal(err)}
	hello:=Envelope{Type:"hello",DeviceID:"headless",OS:"linux",Capabilities:encoded}
	wireBytes,err:=Marshal(hello)
	if err!=nil{t.Fatal(err)}
	decoded,err:=Unmarshal(wireBytes)
	if err!=nil{t.Fatal(err)}
	if !bytes.Equal(decoded.Capabilities,encoded) {
		t.Fatalf("capability payload lost in transit: %+v",decoded)
	}

	failed:=Envelope{Type:"capture_result",TaskID:"shot-1",ErrorCode:capability.NoDesktopSession,Error:"desktop not available"}
	wireBytes,err=Marshal(failed)
	if err!=nil{t.Fatal(err)}
	got,err:=Unmarshal(wireBytes)
	if err!=nil{t.Fatal(err)}
	if got.ErrorCode!=capability.NoDesktopSession || got.Error!=failed.Error {
		t.Fatalf("structured error code lost: %+v",got)
	}
}
