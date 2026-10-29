package wire

import (
 "bytes"
 "testing"
)
func TestRoundTrip(t *testing.T) {
 original:=Envelope{Type:"result",DeviceID:"rino-pc",TaskID:"t1",Command:"hostname",ExitCode:-1,Stdout:[]byte("ok"),Stderr:[]byte("bad"),TimeoutMS:20000,OS:"windows",Error:"none"}
 data,err:=Marshal(original)
 if err!=nil{t.Fatal(err)}
 got,err:=Unmarshal(data)
 if err!=nil{t.Fatal(err)}
 if got.Type!=original.Type||got.DeviceID!=original.DeviceID||got.TaskID!=original.TaskID||got.ExitCode!=original.ExitCode||got.TimeoutMS!=original.TimeoutMS||!bytes.Equal(got.Stdout,original.Stdout)||!bytes.Equal(got.Stderr,original.Stderr)||got.Error!=original.Error{t.Fatalf("roundtrip mismatch: %+v",got)}
}
func TestInvalidFrame(t *testing.T) {
 if _,err:=Unmarshal([]byte{0x0a,0xff});err==nil{t.Fatal("expected error")}
}
