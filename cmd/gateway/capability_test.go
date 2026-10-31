package main

import (
	"bytes"
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/RinoPaw/Reinor/internal/capability"
	"github.com/RinoPaw/Reinor/internal/wire"
	"github.com/coder/websocket"
)

func testDeviceSocket(t *testing.T, h *hub, id, osName string, statuses []capability.Status) (*websocket.Conn, context.Context) {
	t.Helper()
	const token="capabilities-test-token-with-sufficient-length"
	mux:=http.NewServeMux()
	mux.HandleFunc("/agent/ws",func(w http.ResponseWriter,r *http.Request){h.serveAgent(w,r,token,nil)})
	srv:=httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	ctx,cancel:=context.WithTimeout(context.Background(),6*time.Second)
	t.Cleanup(cancel)
	url:="ws"+strings.TrimPrefix(srv.URL,"http")+"/agent/ws"
	conn,_,err:=websocket.Dial(ctx,url,&websocket.DialOptions{HTTPHeader:http.Header{"Authorization":[]string{"Bearer "+token}}})
	if err!=nil{t.Fatal(err)}
	t.Cleanup(func(){_ = conn.CloseNow()})
	payload,err:=capability.Encode(statuses)
	if err!=nil{t.Fatal(err)}
	hello,_:=wire.Marshal(wire.Envelope{Type:"hello",DeviceID:id,OS:osName,Capabilities:payload})
	if err:=conn.Write(ctx,websocket.MessageBinary,hello);err!=nil{t.Fatal(err)}
	for i:=0;i<100;i++{
		h.mu.Lock()
		online:=h.agents[id]!=nil
		h.mu.Unlock()
		if online{return conn,ctx}
		time.Sleep(10*time.Millisecond)
	}
	t.Fatal("Agent did not appear online")
	return nil,ctx
}

func TestCapabilitySnapshotAndLiveRefresh(t *testing.T){
	h:=newHub()
	headless:=[]capability.Status{{ID:capability.DesktopCapture,Supported:true,Available:false,Reason:capability.NoDesktopSession}}
	conn,ctx:=testDeviceSocket(t,h,"desktop-pc","windows",headless)
	devices:=h.list()
	if len(devices)!=1 {t.Fatalf("unexpected devices: %+v",devices)}
	advertised,ok:=devices[0]["capabilities"].([]capability.Status)
	if !ok||len(advertised)!=1||advertised[0]!=headless[0]{
		t.Fatalf("list_devices must expose observed capabilities: %+v",devices[0])
	}
	if devices[0]["capabilities_observed_at"]==nil {t.Fatalf("missing capability observation timestamp")}

	done:=make(chan []capability.Status,1)
	failed:=make(chan error,1)
	go func(){
		refreshed,_,err:=h.deviceCapabilities(ctx,"desktop-pc")
		if err!=nil{failed<-err;return}
		done<-refreshed
	}()
	_,data,err:=conn.Read(ctx)
	if err!=nil{t.Fatal(err)}
	req,err:=wire.Unmarshal(data)
	if err!=nil||req.Type!="get_capabilities"||req.TaskID==""{
		t.Fatalf("unexpected capability request: %+v, %v",req,err)
	}
	available:=[]capability.Status{{ID:capability.DesktopCapture,Supported:true,Available:true}}
	body,_:=capability.Encode(available)
	resp,_:=wire.Marshal(wire.Envelope{Type:"capabilities_result",TaskID:req.TaskID,Capabilities:body})
	if err:=conn.Write(ctx,websocket.MessageBinary,resp);err!=nil{t.Fatal(err)}
	select{
	case got:=<-done:
		if len(got)!=1||got[0]!=available[0]{t.Fatalf("unexpected refreshed capabilities: %+v",got)}
	case err:=<-failed:t.Fatal(err)
	case <-ctx.Done():t.Fatal(ctx.Err())
	}
	latest:=h.list()[0]["capabilities"].([]capability.Status)
	if len(latest)!=1||latest[0]!=available[0]{t.Fatalf("list_devices cache was not updated: %+v",latest)}
}

func TestGatewayRoutesByCapabilityAcrossOS(t *testing.T) {
	h:=newHub()
	statuses:=[]capability.Status{{ID:capability.DesktopCapture,Supported:true,Available:true}}
	conn,ctx:=testDeviceSocket(t,h,"future-linux-desktop","linux",statuses)
	done:=make(chan wire.Envelope,1)
	fail:=make(chan error,1)
	go func(){
		response,err:=h.capture(ctx,"future-linux-desktop",720,55,3*time.Second)
		if err!=nil{fail<-err;return}
		done<-response
	}()
	_,data,err:=conn.Read(ctx)
	if err!=nil{t.Fatal(err)}
	req,err:=wire.Unmarshal(data)
	if err!=nil||req.Type!="capture_screen" {t.Fatalf("capture was not routed: %+v %v",req,err)}
	jpg:=[]byte{0xff,0xd8,0xff,0xd9}
	reply,_:=wire.Marshal(wire.Envelope{Type:"capture_result",TaskID:req.TaskID,MIMEType:"image/jpeg",Data:jpg,Width:720,Height:450})
	if err:=conn.Write(ctx,websocket.MessageBinary,reply);err!=nil{t.Fatal(err)}
	select{
	case got:=<-done:
		if !bytes.Equal(got.Data,jpg){t.Fatalf("wrong screenshot data: %+v",got)}
	case err:=<-fail:t.Fatal(err)
	case <-ctx.Done():t.Fatal(ctx.Err())
	}
}

func TestHeadlessAndUndeclaredCapabilityErrors(t *testing.T) {
	h:=newHub()
	h.agents["headless"]=&agent{id:"headless",os:"linux",capabilities:[]capability.Status{{
		ID:capability.DesktopCapture,Supported:false,Available:false,Reason:capability.NoDesktopSession,
	}}}
	h.agents["no-capture"]=&agent{id:"no-capture",os:"windows"}
	for _,tc:=range []struct{id,code string}{
		{"headless",capability.NoDesktopSession},
		{"no-capture",capability.Unsupported},
	}{
		_,err:=h.capture(context.Background(),tc.id,0,0,time.Second)
		var capErr *capabilityDispatchError
		if !errors.As(err,&capErr)||capErr.Code!=tc.code {
			t.Fatalf("%s: code=%s expected, got %v",tc.id,tc.code,err)
		}
	}
}

func TestGatewayRejectsMissingCapabilities(t *testing.T) {
	h:=newHub()
	const token="capabilities-test-token-with-sufficient-length"
	mux:=http.NewServeMux()
	mux.HandleFunc("/agent/ws",func(w http.ResponseWriter,r *http.Request){h.serveAgent(w,r,token,nil)})
	srv:=httptest.NewServer(mux)
	defer srv.Close()
	ctx,cancel:=context.WithTimeout(context.Background(),3*time.Second)
	defer cancel()
	conn,_,err:=websocket.Dial(ctx,"ws"+strings.TrimPrefix(srv.URL,"http")+"/agent/ws",&websocket.DialOptions{HTTPHeader:http.Header{"Authorization":[]string{"Bearer "+token}}})
	if err!=nil{t.Fatal(err)}
	defer conn.CloseNow()
	hello,_:=wire.Marshal(wire.Envelope{Type:"hello",DeviceID:"missing-caps",OS:"windows"})
	if err:=conn.Write(ctx,websocket.MessageBinary,hello);err!=nil{t.Fatal(err)}
	_,_,err=conn.Read(ctx)
	if websocket.CloseStatus(err)!=websocket.StatusPolicyViolation {
		t.Fatalf("expected policy violation: %v",err)
	}
	if len(h.list())!=0 {t.Fatal("invalid announcement created an online device")}
}
