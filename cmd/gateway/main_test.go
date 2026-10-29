package main

import (
 "context"
 "net/http"
 "net/http/httptest"
 "strings"
 "testing"
 "time"

 "github.com/coder/websocket"
 "github.com/RinoPaw/Reinor/internal/wire"
)

func TestGatewayAgentRoundTrip(t *testing.T) {
 h:=newHub()
 const token="local-development-agent-token-with-enough-entropy"
 mux:=http.NewServeMux()
 mux.HandleFunc("/agent/ws",func(w http.ResponseWriter,r *http.Request){h.serveAgent(w,r,token,nil)})
 server:=httptest.NewServer(mux)
 defer server.Close()
 url:="ws"+strings.TrimPrefix(server.URL,"http")+"/agent/ws"
 unauthorizedCtx,cancel:=context.WithTimeout(context.Background(),time.Second*2)
 _,resp,err:=websocket.Dial(unauthorizedCtx,url,nil)
 cancel()
 if err==nil {t.Fatal("unauthenticated connection accepted")}
 if resp==nil||resp.StatusCode!=http.StatusUnauthorized {t.Fatalf("unexpected denial: %v, %v",resp,err)}
 ctx,stop:=context.WithTimeout(context.Background(),10*time.Second)
 defer stop()
 conn,_,err:=websocket.Dial(ctx,url,&websocket.DialOptions{HTTPHeader:http.Header{"Authorization":[]string{"Bearer "+token}}})
 if err!=nil{t.Fatal(err)}
 defer conn.CloseNow()
 hello,_:=wire.Marshal(wire.Envelope{Type:"hello",DeviceID:"test-pc",OS:"windows"})
 if err:=conn.Write(ctx,websocket.MessageBinary,hello);err!=nil{t.Fatal(err)}
 for i:=0;i<100;i++{
  if len(h.list())==1{break}
  time.Sleep(10*time.Millisecond)
 }
 devices:=h.list()
 if len(devices)!=1||devices[0]["device_id"]!="test-pc"||devices[0]["online"]!=true{t.Fatalf("unexpected devices: %#v",devices)}
 resultCh:=make(chan wire.Envelope,1)
 errCh:=make(chan error,1)
 go func(){
  result,err:=h.execute(ctx,"test-pc","hostname",5*time.Second)
  if err!=nil{errCh<-err;return}
  resultCh<-result
 }()
 typ,data,err:=conn.Read(ctx)
 if err!=nil{t.Fatal(err)}
 if typ!=websocket.MessageBinary{t.Fatalf("unexpected websocket frame %d",typ)}
 req,err:=wire.Unmarshal(data)
 if err!=nil{t.Fatal(err)}
 if req.Type!="execute"||req.Command!="hostname"||req.TaskID==""{t.Fatalf("unexpected request: %+v",req)}
 response,_:=wire.Marshal(wire.Envelope{Type:"result",TaskID:req.TaskID,ExitCode:0,Stdout:[]byte("test-pc")})
 if err:=conn.Write(ctx,websocket.MessageBinary,response);err!=nil{t.Fatal(err)}
 select{
 case result:=<-resultCh:
  if string(result.Stdout)!="test-pc"||result.ExitCode!=0{t.Fatalf("unexpected result %+v",result)}
 case err:=<-errCh:t.Fatal(err)
 case <-ctx.Done():t.Fatal(ctx.Err())
 }
 _=conn.Close(websocket.StatusNormalClosure,"test done")
 for i:=0;i<100;i++{if len(h.list())==1&&h.list()[0]["online"]==false{break};time.Sleep(10*time.Millisecond)}
 if len(h.list())!=1||h.list()[0]["online"]!=false{t.Fatalf("device should be offline: %#v",h.list())}
}
