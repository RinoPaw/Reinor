package main

import (
 "bytes"
 "context"
 "errors"
 "flag"
 "fmt"
 "log"
 "math/rand/v2"
 "net/http"
 "os"
 "os/exec"
 "runtime"
 "strings"
 "sync"
 "time"

 "github.com/coder/websocket"
 "github.com/RinoPaw/Reinor/internal/wire"
)
func getToken(path string)string{
 data,err:=os.ReadFile(path);if err!=nil{log.Fatalf("token file: %v",err)}
 v:=strings.TrimSpace(string(data));if len(v)<32{log.Fatal("token needs at least 32 characters")}
 return v
}
type limitedBuffer struct{b bytes.Buffer;max int}
func(l *limitedBuffer)Write(p []byte)(int,error){
 n:=len(p);space:=l.max-l.b.Len()
 if space>0{if space>len(p){space=len(p)};_,_=l.b.Write(p[:space])}
 return n,nil
}
func exitCode(err error)int32{
 if err==nil{return 0}
 var e *exec.ExitError
 if errors.As(err,&e){return int32(e.ExitCode())}
 return -1
}
// agentShellCommand selects the native shell for the enrolled device OS.
// Gateway authentication and command deadlines are enforced by the caller.
func agentShellCommand(ctx context.Context, command string) *exec.Cmd {
 if runtime.GOOS == "windows" {
  return exec.CommandContext(ctx, "powershell.exe", "-NoProfile", "-NonInteractive", "-Command", command)
 }
 return exec.CommandContext(ctx, "/bin/sh", "-c", command)
}

func execute(f wire.Envelope)wire.Envelope{
 reply:=wire.Envelope{Type:"result",TaskID:f.TaskID}
 if len(f.Command)==0||len(f.Command)>4096{reply.Error="invalid command";return reply}
 ms:=f.TimeoutMS
 if ms<1000||ms>20000{ms=20000}
 ctx,cancel:=context.WithTimeout(context.Background(),time.Duration(ms)*time.Millisecond)
 defer cancel()
 cmd:=agentShellCommand(ctx,f.Command)
 cmd.WaitDelay=2*time.Second
 var out,errout limitedBuffer
 out.max=65536;errout.max=65536
 cmd.Stdout=&out;cmd.Stderr=&errout
 err:=cmd.Run()
 reply.ExitCode=exitCode(err)
 reply.Stdout=out.b.Bytes();reply.Stderr=errout.b.Bytes()
 if ctx.Err()!=nil{reply.Error="execution deadline exceeded"}else if err!=nil&&reply.ExitCode==-1{reply.Error=fmt.Sprintf("launch failed: %v",err)}
 return reply
}
func session(parent context.Context,url,id,token string)error{
 header:=http.Header{"Authorization":[]string{"Bearer "+token}}
 ctx,stop:=context.WithTimeout(parent,15*time.Second)
 c,_,err:=websocket.Dial(ctx,url,&websocket.DialOptions{HTTPHeader:header})
 stop()
 if err!=nil{return err}
 defer c.CloseNow()
 c.SetReadLimit(wire.MaxFrame)
 hello,_:=wire.Marshal(wire.Envelope{Type:"hello",DeviceID:id,OS:runtime.GOOS})
 if err:=c.Write(parent,websocket.MessageBinary,hello);err!=nil{return err}
 log.Printf("connected to gateway device=%s",id)
 var writeMu sync.Mutex
 busy:=make(chan struct{},1)
 connection,closeConn:=context.WithCancel(parent)
 defer closeConn()
 send:=func(frame wire.Envelope)error{
  data,err:=wire.Marshal(frame);if err!=nil{return err}
  writeMu.Lock();defer writeMu.Unlock()
  ctx,cancel:=context.WithTimeout(connection,6*time.Second);defer cancel()
  return c.Write(ctx,websocket.MessageBinary,data)
 }
 for{
  typ,data,err:=c.Read(connection)
  if err!=nil{return err}
  if typ!=websocket.MessageBinary{continue}
  f,err:=wire.Unmarshal(data);if err!=nil{return err}
  if f.Type!="execute"{continue}
  select{
  case busy<-struct{}{}:
   go func(f wire.Envelope){
    defer func(){<-busy}()
    reply:=execute(f)
    if err:=send(reply);err!=nil{log.Printf("send result: %v",err);_=c.CloseNow()}
   }(f)
  default:
   _=send(wire.Envelope{Type:"result",TaskID:f.TaskID,Error:"device busy"})
  }
 }
}
func main(){
 if len(os.Args)>1&&os.Args[1]=="join"{
  if err:=joinMain(os.Args[2:]);err!=nil{log.Fatal(err)}
  return
 }
 url:=flag.String("url","wss://xuhuaai.top/agent/ws","WSS gateway URL")
 device:=flag.String("device","rino-pc","stable device identifier")
 tokenPath:=flag.String("token-file","","path to agent bearer token")
 flag.Parse()
 if *tokenPath==""{log.Fatal("-token-file required")}
 token:=getToken(*tokenPath)
 backoff:=time.Second
 for{
  started:=time.Now()
  err:=session(context.Background(),*url,*device,token)
  if time.Since(started)>time.Minute{backoff=time.Second}
  log.Printf("gateway disconnected: %v",err)
  delay:=backoff+time.Duration(rand.Int64N(int64(backoff/2+1)))
  time.Sleep(delay)
  if backoff<30*time.Second{backoff*=2;if backoff>30*time.Second{backoff=30*time.Second}}
 }
}
