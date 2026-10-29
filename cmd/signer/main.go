package main

import (
 "bytes"
 "crypto/ed25519"
 "crypto/rand"
 "crypto/sha256"
 "encoding/base64"
 "encoding/hex"
 "encoding/json"
 "errors"
 "flag"
 "fmt"
 "io"
 "os"
 "path/filepath"
 "strings"
 "time"

 "github.com/RinoPaw/Hexveil/internal/trust"
)

const version = "0.1.0"
type ownerFile struct {
 Version int `json:"version"`
 Seed string `json:"seed"`
}
type ownerIdentity struct {
 Private ed25519.PrivateKey
 Public ed25519.PublicKey
 AccountID string
 Fingerprint string
}
type accountInfo struct {
 AccountID string `json:"account_id"`
 PublicKey string `json:"owner_public_key_base64"`
 Fingerprint string `json:"owner_fingerprint"`
}
type joinInfo struct {
 GatewayID string `json:"gateway_id"`
 AccountID string `json:"account_id"`
 RequestID string `json:"request_id"`
 DeviceID string `json:"device_id"`
 Label string `json:"label"`
 DeviceFingerprint string `json:"device_fingerprint"`
 ExpiresAt string `json:"expires_at"`
 SignatureValid bool `json:"signature_valid"`
}

func accountID(pub ed25519.PublicKey) string {
 h:=sha256.Sum256(pub)
 return "acct_"+hex.EncodeToString(h[:])
}
func homePath(override string)(string,error) {
 if override!="" {return filepath.Abs(override)}
 if xdg:=os.Getenv("XDG_CONFIG_HOME");xdg!="" {
  return filepath.Join(xdg,"rinomesh","signer"),nil
 }
 home,err:=os.UserHomeDir()
 if err!=nil{return "",err}
 return filepath.Join(home,".config","rinomesh","signer"),nil
}
func keyPath(home string)string{return filepath.Join(home,"owner.key")}
func loadOwner(home string)(ownerIdentity,error){
 info,err:=os.Stat(keyPath(home))
 if err!=nil{return ownerIdentity{},fmt.Errorf("owner key unavailable: %w (run init on this phone)",err)}
 if !info.Mode().IsRegular()||info.Mode().Perm()&0077!=0{return ownerIdentity{},errors.New("owner.key must be a regular file accessible only by its owner (chmod 600 owner.key)")}
 content,err:=os.ReadFile(keyPath(home))
 if err!=nil{return ownerIdentity{},err}
 var k ownerFile
 if err:=decodeJSON(content,&k);err!=nil{return ownerIdentity{},err}
 if k.Version!=1{return ownerIdentity{},errors.New("unsupported owner.key version")}
 seed,err:=base64.StdEncoding.Strict().DecodeString(k.Seed)
 if err!=nil||len(seed)!=ed25519.SeedSize{return ownerIdentity{},errors.New("invalid owner.key seed")}
 priv:=ed25519.NewKeyFromSeed(seed)
 pub:=priv.Public().(ed25519.PublicKey)
 return ownerIdentity{Private:priv,Public:pub,AccountID:accountID(pub),Fingerprint:trust.KeyFingerprint(pub)},nil
}
func createOwner(home string)(ownerIdentity,error){
 if err:=os.MkdirAll(home,0700);err!=nil{return ownerIdentity{},err}
 // Refuse directories with broad access. Android's shared /sdcard is unsuitable.
 d,err:=os.Stat(home)
 if err!=nil{return ownerIdentity{},err}
 if !d.IsDir()||d.Mode().Perm()&0077!=0{return ownerIdentity{},errors.New("signer directory must have permissions 0700; use private Termux/Arch HOME")}
 seed:=make([]byte,ed25519.SeedSize)
 if _,err:=rand.Read(seed);err!=nil{return ownerIdentity{},err}
 data,err:=json.Marshal(ownerFile{Version:1,Seed:base64.StdEncoding.EncodeToString(seed)})
 if err!=nil{return ownerIdentity{},err}
 f,err:=os.OpenFile(keyPath(home),os.O_WRONLY|os.O_CREATE|os.O_EXCL,0600)
 if err!=nil{return ownerIdentity{},fmt.Errorf("refusing to overwrite existing owner key: %w",err)}
 if _,err:=f.Write(append(data,'\n'));err!=nil{f.Close();return ownerIdentity{},err}
 if err:=f.Sync();err!=nil{f.Close();return ownerIdentity{},err}
 if err:=f.Close();err!=nil{return ownerIdentity{},err}
 return loadOwner(home)
}
func decodeJSON(data []byte,v any)error{
 d:=json.NewDecoder(bytes.NewReader(data))
 d.DisallowUnknownFields()
 if err:=d.Decode(v);err!=nil{return err}
 var extra any
 if err:=d.Decode(&extra);err!=io.EOF {
  if err==nil{return errors.New("multiple JSON values")}
  return err
 }
 return nil
}
func loadJSON(path string,v any)error{
 var data []byte
 var err error
 if path=="-" {data,err=io.ReadAll(io.LimitReader(os.Stdin,512*1024+1))}else{data,err=os.ReadFile(path)}
 if err!=nil{return err}
 if len(data)>512*1024{return errors.New("input exceeds 512 KiB")}
 return decodeJSON(data,v)
}
func outputJSON(value any,path string,stdout io.Writer)error{
 data,err:=json.MarshalIndent(value,"","  ")
 if err!=nil{return err}
 data=append(data,'\n')
 if path==""||path=="-" {_,err=stdout.Write(data);return err}
 // Signature envelopes are public data; restrictive mode avoids unwanted exposure anyway.
 f,err:=os.OpenFile(path,os.O_WRONLY|os.O_CREATE|os.O_EXCL,0600)
 if err!=nil{return fmt.Errorf("refusing to overwrite output: %w",err)}
 if _,err:=f.Write(data);err!=nil{f.Close();return err}
 if err:=f.Close();err!=nil{return err}
 return nil
}
func identityInfo(i ownerIdentity)accountInfo{
 return accountInfo{AccountID:i.AccountID,PublicKey:base64.StdEncoding.EncodeToString(i.Public),Fingerprint:i.Fingerprint}
}
func flags(name string)*flag.FlagSet {
 fs:=flag.NewFlagSet(name,flag.ContinueOnError)
 fs.SetOutput(io.Discard)
 return fs
}
func noArgs(fs *flag.FlagSet)error{
 if fs.NArg()!=0{return fmt.Errorf("unexpected positional arguments: %v",fs.Args())}
 return nil
}
func usage()string{
 return `RinoMesh Signer `+version+`  (offline owner-key CLI)
Usage:
  rinomesh-signer init [--home DIR]
  rinomesh-signer account [--home DIR]
  rinomesh-signer inspect-join --request JOIN.json
  rinomesh-signer approve --request JOIN.json [--out APPROVAL.json] [--home DIR]
  rinomesh-signer sign-command --gateway ID --device ID --command TEXT [--shell powershell] [--ttl 2m] [--out COMMAND.json] [--home DIR]
  rinomesh-signer version
Notes:
  init creates owner.key ONLY on this device; never put it on Gateway.
  Approval/command JSON is suitable for sending through the Gateway.
  --out - writes JSON to standard output (the default).
  A device must independently pin the public owner key before trusting approvals.
`
}
func run(args []string,stdout,stderr io.Writer,now time.Time)error {
 if len(args)==0 {_,_=io.WriteString(stderr,usage());return errors.New("missing subcommand")}
 switch args[0] {
 case "help","-h","--help":
  _,_=io.WriteString(stdout,usage());return nil
 case "version":
  _,_=fmt.Fprintln(stdout,version);return nil
 case "init":
  fs:=flags("init")
  homeOverride:=fs.String("home","","signer directory")
  if err:=fs.Parse(args[1:]);err!=nil{return err}
  if err:=noArgs(fs);err!=nil{return err}
  home,err:=homePath(*homeOverride);if err!=nil{return err}
  identity,err:=createOwner(home);if err!=nil{return err}
  _,_=fmt.Fprintf(stderr,"Owner key generated locally: %s\nKeep owner.key on your phone. Do not upload it to Gateway or GitHub.\n",keyPath(home))
  return outputJSON(identityInfo(identity),"",stdout)
 case "account":
  fs:=flags("account")
  homeOverride:=fs.String("home","","signer directory")
  if err:=fs.Parse(args[1:]);err!=nil{return err}
  if err:=noArgs(fs);err!=nil{return err}
  home,err:=homePath(*homeOverride);if err!=nil{return err}
  identity,err:=loadOwner(home);if err!=nil{return err}
  return outputJSON(identityInfo(identity),"",stdout)
 case "inspect-join":
  fs:=flags("inspect-join")
  input:=fs.String("request","","signed join request JSON")
  if err:=fs.Parse(args[1:]);err!=nil{return err}
  if err:=noArgs(fs);err!=nil{return err}
  if *input==""{return errors.New("--request required")}
  var request trust.SignedJoinRequest
  if err:=loadJSON(*input,&request);err!=nil{return err}
  if err:=trust.VerifyJoinRequest(request,now);err!=nil{return err}
  v:=request.Request
  return outputJSON(joinInfo{GatewayID:v.GatewayID,AccountID:v.AccountID,RequestID:v.RequestID,DeviceID:v.DeviceID,Label:v.Label,DeviceFingerprint:trust.KeyFingerprint(v.DevicePublicKey),ExpiresAt:time.Unix(v.ExpiresAt,0).UTC().Format(time.RFC3339),SignatureValid:true},"",stdout)
 case "approve":
  fs:=flags("approve")
  input:=fs.String("request","","signed join request JSON")
  out:=fs.String("out","-","output JSON file or -")
  homeOverride:=fs.String("home","","signer directory")
  if err:=fs.Parse(args[1:]);err!=nil{return err}
  if err:=noArgs(fs);err!=nil{return err}
  if *input==""{return errors.New("--request required")}
  home,err:=homePath(*homeOverride);if err!=nil{return err}
  identity,err:=loadOwner(home);if err!=nil{return err}
  var request trust.SignedJoinRequest
  if err:=loadJSON(*input,&request);err!=nil{return err}
  if err:=trust.VerifyJoinRequest(request,now);err!=nil{return err}
  if request.Request.AccountID!=identity.AccountID {return fmt.Errorf("join request is for %s; current owner account is %s",request.Request.AccountID,identity.AccountID)}
  approval,err:=trust.SignApproval(identity.Private,trust.ApprovalFor(request.Request))
  if err!=nil{return err}
  if _,err:=trust.ApplyApproval(identity.Public,request,approval,now);err!=nil{return err}
  _,_=fmt.Fprintf(stderr,"Signing enrollment: device=%s fingerprint=%s gateway=%s\nVerify the device fingerprint against the device's own display.\n",request.Request.DeviceID,trust.KeyFingerprint(request.Request.DevicePublicKey),request.Request.GatewayID)
  return outputJSON(approval,*out,stdout)
 case "sign-command":
  fs:=flags("sign-command")
  gateway:=fs.String("gateway","","gateway ID used in Agent pairing")
  device:=fs.String("device","","paired device ID")
  shell:=fs.String("shell","powershell","target shell")
  cmd:=fs.String("command","","complete command")
  ttl:=fs.Duration("ttl",2*time.Minute,"validity duration, maximum 5 minutes")
  out:=fs.String("out","-","output JSON file or -")
  homeOverride:=fs.String("home","","signer directory")
  if err:=fs.Parse(args[1:]);err!=nil{return err}
  if err:=noArgs(fs);err!=nil{return err}
  if *gateway==""||*device==""||*cmd==""{return errors.New("--gateway, --device and --command are required")}
  if !strings.HasPrefix(*device,"dev_")||len(*device)!=68{return errors.New("device ID must be dev_<64 hexadecimal characters>")}
  if _,err:=hex.DecodeString(strings.TrimPrefix(*device,"dev_"));err!=nil{return fmt.Errorf("invalid device ID: %w",err)}
  if *ttl<=0||*ttl>5*time.Minute {return errors.New("--ttl must be between 1ns and 5m")}
  if len(*cmd)>4096||*shell!="powershell"{return errors.New("current Agent protocol supports PowerShell commands up to 4096 bytes")}
  home,err:=homePath(*homeOverride);if err!=nil{return err}
  identity,err:=loadOwner(home);if err!=nil{return err}
  task,err:=trust.RandomID();if err!=nil{return err}
  nonce,err:=trust.RandomID();if err!=nil{return err}
  payload:=trust.Command{GatewayID:*gateway,AccountID:identity.AccountID,DeviceID:*device,TaskID:task,Shell:*shell,Command:*cmd,Nonce:nonce,IssuedAt:now.Unix(),ExpiresAt:now.Add(*ttl).Unix()}
  if payload.ExpiresAt<=payload.IssuedAt{return errors.New("command validity must be at least one second")}
  signed,err:=trust.SignCommand(identity.Private,payload);if err!=nil{return err}
  _,_=fmt.Fprintf(stderr,"Signing for account=%s device=%s gateway=%s; expires=%s\n",identity.AccountID,*device,*gateway,time.Unix(payload.ExpiresAt,0).Format(time.RFC3339))
  return outputJSON(signed,*out,stdout)
 default:return fmt.Errorf("unknown command %q\n%s",args[0],usage())
 }
}
func main(){
 if err:=run(os.Args[1:],os.Stdout,os.Stderr,time.Now());err!=nil{
  _,_=fmt.Fprintln(os.Stderr,"error:",err)
  os.Exit(1)
 }
}
