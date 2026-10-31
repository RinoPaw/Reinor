package main

import (
 "context"
 "net/http"
 "net/http/httptest"
 "path/filepath"
 "strings"
 "sync/atomic"
 "testing"

 "github.com/coder/websocket"
 "github.com/RinoPaw/Reinor/internal/wire"
)

func TestResumableJoinSurvivesWebSocketDisconnect(t *testing.T){
 var connections atomic.Int32
 const device="test-joined-pc"
 const request="join_recovery-test"
 const ticket="join_secret_device-long-unique-test-secret-0000000000000000"
 server:=httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter,r *http.Request){
  if r.Header.Get("Authorization")!="Bearer join-request"{http.Error(w,"bad Authorization",401);return}
  conn,err:=websocket.Accept(w,r,nil)
  if err!=nil{return}
  defer conn.CloseNow()
  _,data,err:=conn.Read(context.Background())
  if err!=nil{return}
  hello,err:=wire.Unmarshal(data)
  if err!=nil||hello.DeviceID!=device||hello.ProtocolVersion!=wire.ProtocolVersion{return}
  n:=connections.Add(1)
  if n==1{
   if hello.Command!=""{return}
   pending,_:=wire.Marshal(wire.Envelope{Type:"join_pending",ProtocolVersion:wire.ProtocolVersion,TaskID:request,DeviceID:device,Command:ticket})
   _=conn.Write(context.Background(),websocket.MessageBinary,pending)
   return // simulate WebSocket disconnect before owner approval
  }
  if hello.Command!=ticket{return}
  approved,_:=wire.Marshal(wire.Envelope{Type:"join_approved",ProtocolVersion:wire.ProtocolVersion,TaskID:request,Command:ticket})
  _=conn.Write(context.Background(),websocket.MessageBinary,approved)
 }))
 defer server.Close()
 address:="ws"+strings.TrimPrefix(server.URL,"http")
 path:=filepath.Join(t.TempDir(),"joined-test.json")
 if err:=requestTicketViaWS(address,"http://local",device,path);err==nil{t.Fatal("first connection should disconnect")}
 pending,exists,err:=readJoinCredential(path,"http://local",device)
 if err!=nil||!exists||!pending.Pending||pending.Ticket!=ticket||pending.RequestID!=request{t.Fatalf("no durable pending ticket: %+v %v",pending,err)}
 if err:=requestTicketViaWS(address,"http://local",device,path);err!=nil{t.Fatalf("resume failed: %v",err)}
 approved,exists,err:=readJoinCredential(path,"http://local",device)
 if err!=nil||!exists||approved.Pending||approved.Ticket!=ticket{t.Fatalf("approved ticket missing: %+v %v",approved,err)}
 if connections.Load()!=2{t.Fatal("incorrect number of connections")}
}
