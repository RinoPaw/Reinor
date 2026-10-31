package main

import (
 "context"
 "encoding/json"
 "errors"
 "flag"
 "fmt"
 "log"
 "math/rand/v2"
 "net/http"
 "net/url"
 "os"
 "path/filepath"
 "runtime"
 "strings"
 "time"

 "github.com/coder/websocket"
 "github.com/RinoPaw/Reinor/internal/wire"
)

type joinCredential struct {
 Gateway string `json:"gateway"`
 DeviceID string `json:"device_id"`
 Ticket string `json:"ticket"`
 RequestID string `json:"request_id,omitempty"`
 Pending bool `json:"pending,omitempty"`
}
func joinURL(raw string)(string,string,error){
 u,err:=url.Parse(raw)
 if err!=nil||u==nil||u.Host==""||u.RawQuery!=""||u.Fragment!=""||(u.Path!=""&&u.Path!="/"){return "","",errors.New("invalid Gateway URL")}
 if u.Scheme=="https"{return "https://"+u.Host,"wss://"+u.Host+"/agent/ws",nil}
 if u.Scheme=="http"&&(u.Hostname()=="127.0.0.1"||u.Hostname()=="localhost"){return "http://"+u.Host,"ws://"+u.Host+"/agent/ws",nil}
 return "","",errors.New("Gateway must use HTTPS except when testing on localhost")
}
func joinFile(device string)(string,error){
 if !validLocalDeviceID(device){return "",errors.New("invalid device name")}
 base:=os.Getenv("LOCALAPPDATA")
 if base==""{var err error;base,err=os.UserHomeDir();if err!=nil{return "",err};base=filepath.Join(base,".config")}
 return filepath.Join(base,"RinoMesh","joined-"+device+".json"),nil
}
func validLocalDeviceID(value string)bool{
 if len(value)==0||len(value)>64{return false}
 for _,r:=range value{if !(r>='a'&&r<='z'||r>='A'&&r<='Z'||r>='0'&&r<='9'||r=='_'||r=='-'||r=='.'){return false}}
 return true
}
func readJoinCredential(path,endpoint,device string)(joinCredential,bool,error){
 content,err:=os.ReadFile(path)
 if errors.Is(err,os.ErrNotExist){return joinCredential{},false,nil}
 if err!=nil{return joinCredential{},false,err}
 var saved joinCredential
 if err:=json.Unmarshal(content,&saved);err!=nil{return joinCredential{},false,err}
 if saved.Gateway!=endpoint||saved.DeviceID!=device||!strings.HasPrefix(saved.Ticket,"join_secret_"){return joinCredential{},false,errors.New("saved join ticket belongs to a different Gateway/device")}
 return saved,true,nil
}
func saveJoinCredential(path string,saved joinCredential)error{
 if err:=os.MkdirAll(filepath.Dir(path),0700);err!=nil{return err}
 content,err:=json.MarshalIndent(saved,"","  ")
 if err!=nil{return err}
 return os.WriteFile(path,content,0600)
}
var errJoinRejected=errors.New("device join rejected")
var errJoinResumeInvalid=errors.New("saved pending join is no longer valid")
func joinMain(args []string)error{
 hostname,_:=os.Hostname()
 fs:=flag.NewFlagSet("join",flag.ContinueOnError)
 gateway:=fs.String("gateway","https://xuhuaai.top","Gateway URL")
 device:=fs.String("device",hostname,"Device ID")
 if err:=fs.Parse(args);err!=nil{return err}
 if fs.NArg()>0{return errors.New("unexpected positional arguments")}
 endpoint,ws,err:=joinURL(*gateway);if err!=nil{return err}
 path,err:=joinFile(*device);if err!=nil{return err}
 stored,exists,err:=readJoinCredential(path,endpoint,*device);if err!=nil{return err}
 if !exists||stored.Pending{
  backoff:=time.Second
  for{
   err=requestTicketViaWS(ws,endpoint,*device,path)
   if err==nil{break}
   if errors.Is(err,errJoinRejected){return err}
   if errors.Is(err,errJoinResumeInvalid){
    // The Gateway no longer knows the prior request. Start a fresh request.
    if removeErr:=os.Remove(path);removeErr!=nil{return removeErr}
   }
   log.Printf("join connection lost: %v; retrying",err)
   time.Sleep(backoff+time.Duration(rand.Int64N(int64(backoff/2+1))))
   if backoff<30*time.Second{backoff*=2;if backoff>30*time.Second{backoff=30*time.Second}}
  }
  stored,exists,err=readJoinCredential(path,endpoint,*device)
  if err!=nil||!exists{return fmt.Errorf("approved ticket was not saved: %w",err)}
 }
 fmt.Printf("Connected enrollment for %s; Gateway %s\n",*device,endpoint)
 backoff:=time.Second
 for{
  since:=time.Now()
  err:=session(context.Background(),ws,*device,stored.Ticket)
  if time.Since(since)>time.Minute{backoff=time.Second}
  log.Printf("Gateway disconnected: %v",err)
  time.Sleep(backoff+time.Duration(rand.Int64N(int64(backoff/2+1))))
  if backoff<30*time.Second{backoff*=2;if backoff>30*time.Second{backoff=30*time.Second}}
 }
}
func requestTicketViaWS(address,endpoint,device,path string)error{
 saved,exists,err:=readJoinCredential(path,endpoint,device)
 if err!=nil{return err}
 header:=http.Header{"Authorization":[]string{"Bearer join-request"}}
 ctx:=context.Background()
 dialCtx,stop:=context.WithTimeout(ctx,15*time.Second)
 conn,_,err:=websocket.Dial(dialCtx,address,&websocket.DialOptions{HTTPHeader:header})
 stop()
 if err!=nil{return err}
 defer conn.CloseNow()
 conn.SetReadLimit(wire.MaxFrame)
 hello:=wire.Envelope{Type:"hello",DeviceID:device,OS:runtime.GOOS,ProtocolVersion:wire.ProtocolVersion}
 if exists&&saved.Pending{hello.Command=saved.Ticket}
 payload,_:=wire.Marshal(hello)
 if err:=conn.Write(ctx,websocket.MessageBinary,payload);err!=nil{return err}
 for{
  kind,data,err:=conn.Read(ctx)
  if err!=nil{return err}
  if kind!=websocket.MessageBinary{continue}
  msg,err:=wire.Unmarshal(data)
  if err!=nil{return err}
  if msg.ProtocolVersion!=wire.ProtocolVersion{return fmt.Errorf("incompatible enrollment gateway protocol: expected v%d, got v%d",wire.ProtocolVersion,msg.ProtocolVersion)}
  switch msg.Type{
  case "join_pending":
   if msg.TaskID==""{return errors.New("join request missing ID")}
   if !exists{
    if !strings.HasPrefix(msg.Command,"join_secret_"){return errors.New("Gateway did not supply resumable pending join ticket")}
    saved=joinCredential{Gateway:endpoint,DeviceID:device,Ticket:msg.Command,RequestID:msg.TaskID,Pending:true}
    if err:=saveJoinCredential(path,saved);err!=nil{return err}
    exists=true
    fmt.Printf("Reinor: %s requested connection\nRequest ID: %s\nWaiting for GPT approval...\n",device,msg.TaskID)
   }else if saved.RequestID!=msg.TaskID {return errors.New("Gateway returned wrong pending request")}
  case "join_approved":
   if !strings.HasPrefix(msg.Command,"join_secret_"){return errors.New("Gateway returned invalid approved ticket")}
   if exists&&saved.Ticket!=msg.Command{return errors.New("approved ticket differs from saved request")}
   saved=joinCredential{Gateway:endpoint,DeviceID:device,Ticket:msg.Command}
   if err:=saveJoinCredential(path,saved);err!=nil{return err}
   fmt.Printf("Approved. Enrollment saved: %s\n",path)
   return nil
  case "join_waiting":
   // Keepalive while user is considering the request.
  case "join_error":
   if msg.Error=="request rejected"{return errJoinRejected}
   if exists&&saved.Pending{return fmt.Errorf("%w: %s",errJoinResumeInvalid,msg.Error)}
   return errors.New(msg.Error)
  }
 }
}
