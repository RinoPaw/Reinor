package trust

// Package trust provides the Agent-side signature boundary.
// The Gateway does not possess owner signing keys, and cannot grant itself execution.

import (
 "crypto/ed25519"
 "crypto/rand"
 "crypto/sha256"
 "crypto/subtle"
 "encoding/hex"
 "encoding/json"
 "errors"
 "fmt"
 "os"
 "path/filepath"
 "sync"
 "time"
)

const (
 approvalDomain = "RinoMesh/v1/enroll\x00"
 requestDomain = "RinoMesh/v1/request\x00"
 commandDomain = "RinoMesh/v1/execute\x00"
)

type DeviceIdentity struct {
 PublicKey []byte `json:"public_key"`
 PrivateKey []byte `json:"private_key"`
}

func GenerateDevice() (DeviceIdentity,error) {
 pub,priv,err:=ed25519.GenerateKey(rand.Reader)
 if err!=nil{return DeviceIdentity{},err}
 return DeviceIdentity{PublicKey:pub,PrivateKey:priv},nil
}
func GenerateOwner() (ed25519.PublicKey,ed25519.PrivateKey,error) {
 return ed25519.GenerateKey(rand.Reader)
}
func DeviceID(pub []byte) string {
 sum:=sha256.Sum256(pub)
 return "dev_"+hex.EncodeToString(sum[:])
}
func KeyFingerprint(pub []byte) string {
 sum:=sha256.Sum256(pub)
 return hex.EncodeToString(sum[:8])
}
func RandomID() (string,error) {
 b:=make([]byte,24)
 if _,err:=rand.Read(b);err!=nil{return "",err}
 return hex.EncodeToString(b),nil
}
func signed(domain string,body any) ([]byte,error) {
 payload,err:=json.Marshal(body)
 if err!=nil{return nil,err}
 return append([]byte(domain),payload...),nil
}
func validID(value string) bool {
 if len(value)<3||len(value)>256{return false}
 for _,c:=range value {if c<=32||c==127{return false}}
 return true
}
type JoinRequest struct {
 GatewayID string `json:"gateway_id"`
 AccountID string `json:"account_id"`
 RequestID string `json:"request_id"`
 DeviceID string `json:"device_id"`
 DevicePublicKey []byte `json:"device_public_key"`
 Label string `json:"label"`
 Nonce string `json:"nonce"`
 ExpiresAt int64 `json:"expires_at"`
}
type SignedJoinRequest struct {
 Request JoinRequest `json:"request"`
 Signature []byte `json:"signature"`
}
func NewJoinRequest(identity DeviceIdentity,gateway,account,label string,now time.Time)(SignedJoinRequest,error) {
 if len(identity.PublicKey)!=ed25519.PublicKeySize||len(identity.PrivateKey)!=ed25519.PrivateKeySize||!validID(gateway)||!validID(account) {return SignedJoinRequest{},errors.New("invalid device identity or account")}
 requestID,err:=RandomID();if err!=nil{return SignedJoinRequest{},err}
 nonce,err:=RandomID();if err!=nil{return SignedJoinRequest{},err}
 req:=JoinRequest{GatewayID:gateway,AccountID:account,RequestID:requestID,DeviceID:DeviceID(identity.PublicKey),DevicePublicKey:identity.PublicKey,Label:label,Nonce:nonce,ExpiresAt:now.Add(15*time.Minute).Unix()}
 payload,err:=signed(requestDomain,req);if err!=nil{return SignedJoinRequest{},err}
 return SignedJoinRequest{Request:req,Signature:ed25519.Sign(ed25519.PrivateKey(identity.PrivateKey),payload)},nil
}
func VerifyJoinRequest(req SignedJoinRequest,now time.Time) error {
 v:=req.Request
 if !validID(v.GatewayID)||!validID(v.AccountID)||!validID(v.RequestID)||!validID(v.Nonce)||len(v.DevicePublicKey)!=ed25519.PublicKeySize||v.DeviceID!=DeviceID(v.DevicePublicKey) {return errors.New("invalid join request")}
 if v.ExpiresAt<=now.Unix()||v.ExpiresAt>now.Add(20*time.Minute).Unix(){return errors.New("join request expired or exceeds max lifetime")}
 payload,err:=signed(requestDomain,v);if err!=nil{return err}
 if !ed25519.Verify(ed25519.PublicKey(v.DevicePublicKey),payload,req.Signature){return errors.New("invalid device identity signature")}
 return nil
}

type Approval struct {
 GatewayID string `json:"gateway_id"`
 AccountID string `json:"account_id"`
 RequestID string `json:"request_id"`
 DeviceID string `json:"device_id"`
 DevicePublicKey []byte `json:"device_public_key"`
 Nonce string `json:"nonce"`
 ExpiresAt int64 `json:"expires_at"`
}
type SignedApproval struct {
 Approval Approval `json:"approval"`
 Signature []byte `json:"signature"`
}
func ApprovalFor(request JoinRequest) Approval {
 return Approval{GatewayID:request.GatewayID,AccountID:request.AccountID,RequestID:request.RequestID,DeviceID:request.DeviceID,DevicePublicKey:request.DevicePublicKey,Nonce:request.Nonce,ExpiresAt:request.ExpiresAt}
}
func SignApproval(owner ed25519.PrivateKey,approval Approval)(SignedApproval,error) {
 if len(owner)!=ed25519.PrivateKeySize{return SignedApproval{},errors.New("invalid owner private key")}
 payload,err:=signed(approvalDomain,approval);if err!=nil{return SignedApproval{},err}
 return SignedApproval{Approval:approval,Signature:ed25519.Sign(owner,payload)},nil
}
type Pairing struct {
 GatewayID string `json:"gateway_id"`
 AccountID string `json:"account_id"`
 DeviceID string `json:"device_id"`
 OwnerPublicKey []byte `json:"owner_public_key"`
}
func ApplyApproval(ownerPublicKey ed25519.PublicKey,request SignedJoinRequest,approval SignedApproval,now time.Time)(Pairing,error) {
 if len(ownerPublicKey)!=ed25519.PublicKeySize{return Pairing{},errors.New("owner public key must be independently pinned")}
 if err:=VerifyJoinRequest(request,now);err!=nil{return Pairing{},err}
 expected:=ApprovalFor(request.Request)
 provided:=approval.Approval
 if expected.GatewayID!=provided.GatewayID||expected.AccountID!=provided.AccountID||expected.RequestID!=provided.RequestID||expected.DeviceID!=provided.DeviceID||expected.Nonce!=provided.Nonce||expected.ExpiresAt!=provided.ExpiresAt||subtle.ConstantTimeCompare(expected.DevicePublicKey,provided.DevicePublicKey)!=1{return Pairing{},errors.New("approval is not for this join request")}
 payload,err:=signed(approvalDomain,provided);if err!=nil{return Pairing{},err}
 if !ed25519.Verify(ownerPublicKey,payload,approval.Signature){return Pairing{},errors.New("invalid owner approval signature")}
 return Pairing{GatewayID:provided.GatewayID,AccountID:provided.AccountID,DeviceID:provided.DeviceID,OwnerPublicKey:append([]byte(nil),ownerPublicKey...)},nil
}

type Command struct {
 GatewayID string `json:"gateway_id"`
 AccountID string `json:"account_id"`
 DeviceID string `json:"device_id"`
 TaskID string `json:"task_id"`
 Shell string `json:"shell"`
 Command string `json:"command"`
 Nonce string `json:"nonce"`
 IssuedAt int64 `json:"issued_at"`
 ExpiresAt int64 `json:"expires_at"`
}
type SignedCommand struct {
 Command Command `json:"command"`
 Signature []byte `json:"signature"`
}
func SignCommand(owner ed25519.PrivateKey,command Command)(SignedCommand,error){
 if len(owner)!=ed25519.PrivateKeySize{return SignedCommand{},errors.New("invalid owner private key")}
 payload,err:=signed(commandDomain,command);if err!=nil{return SignedCommand{},err}
 return SignedCommand{Command:command,Signature:ed25519.Sign(owner,payload)},nil
}

type AgentVerifier struct {
 pairing Pairing
 replay *ReplayLog
}
func NewAgentVerifier(pairing Pairing,replayPath string)(*AgentVerifier,error) {
 if len(pairing.OwnerPublicKey)!=ed25519.PublicKeySize||!validID(pairing.AccountID)||!validID(pairing.GatewayID)||!validID(pairing.DeviceID)||replayPath==""{return nil,errors.New("invalid device pairing")}
 return &AgentVerifier{pairing:pairing,replay:&ReplayLog{path:replayPath}},nil
}
// Accept must be invoked before ANY local command execution.
// Marking the task is durable: a repeated delivery cannot execute twice after reconnect or restart.
func(v *AgentVerifier)Accept(envelope SignedCommand,now time.Time) error{
 cmd:=envelope.Command
 if cmd.GatewayID!=v.pairing.GatewayID||cmd.AccountID!=v.pairing.AccountID||cmd.DeviceID!=v.pairing.DeviceID{return errors.New("command target does not match local pairing")}
 if !validID(cmd.TaskID)||!validID(cmd.Nonce)||cmd.Command==""||len(cmd.Command)>4096||cmd.Shell!="powershell"{return errors.New("invalid command payload")}
 if cmd.IssuedAt>now.Add(30*time.Second).Unix()||cmd.ExpiresAt<=now.Unix()||cmd.ExpiresAt<=cmd.IssuedAt||cmd.ExpiresAt>cmd.IssuedAt+300{return errors.New("command outside authorized time window")}
 payload,err:=signed(commandDomain,cmd);if err!=nil{return err}
 if !ed25519.Verify(ed25519.PublicKey(v.pairing.OwnerPublicKey),payload,envelope.Signature){return errors.New("invalid command signature")}
 return v.replay.mark(cmd.TaskID+"|"+cmd.Nonce,cmd.ExpiresAt,now.Unix())
}
type ReplayLog struct{
 mu sync.Mutex
 path string
}
func(r *ReplayLog)mark(key string,expiration,now int64)error{
 r.mu.Lock();defer r.mu.Unlock()
 seen:=map[string]int64{}
 body,err:=os.ReadFile(r.path)
 if err==nil{if err:=json.Unmarshal(body,&seen);err!=nil{return fmt.Errorf("replay log corrupt: %w",err)}}else if !errors.Is(err,os.ErrNotExist){return err}
 for k,expiry:=range seen{if expiry<=now{delete(seen,k)}}
 if _,exists:=seen[key];exists{return errors.New("command already accepted")}
 seen[key]=expiration
 data,err:=json.Marshal(seen);if err!=nil{return err}
 if err:=os.MkdirAll(filepath.Dir(r.path),0700);err!=nil{return err}
 tmp,err:=os.CreateTemp(filepath.Dir(r.path),".replay-*");if err!=nil{return err}
 defer os.Remove(tmp.Name())
 if err:=tmp.Chmod(0600);err!=nil{tmp.Close();return err}
 if _,err:=tmp.Write(data);err!=nil{tmp.Close();return err}
 if err:=tmp.Sync();err!=nil{tmp.Close();return err}
 if err:=tmp.Close();err!=nil{return err}
 return os.Rename(tmp.Name(),r.path)
}
