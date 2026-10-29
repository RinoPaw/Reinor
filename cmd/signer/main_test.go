package main

import (
 "bytes"
 "crypto/ed25519"
 "encoding/json"
 "os"
 "path/filepath"
 "strings"
 "testing"
 "time"

 "github.com/RinoPaw/Reinor/internal/trust"
)

func invoke(t *testing.T,at time.Time,args ...string)(string,string,error){
 t.Helper()
 var stdout,stderr bytes.Buffer
 err:=run(args,&stdout,&stderr,at)
 return stdout.String(),stderr.String(),err
}
func testOwner(t *testing.T,at time.Time)(string,ownerIdentity){
 t.Helper()
 home:=filepath.Join(t.TempDir(),"signer")
 output,_,err:=invoke(t,at,"init","--home",home)
 if err!=nil{t.Fatal(err)}
 if strings.Contains(output,"seed")||strings.Contains(output,"private_key"){t.Fatal("secret leaked into stdout")}
 identity,err:=loadOwner(home);if err!=nil{t.Fatal(err)}
 var account accountInfo
 if err:=json.Unmarshal([]byte(output),&account);err!=nil{t.Fatal(err)}
 if account.AccountID!=identity.AccountID||account.Fingerprint!=identity.Fingerprint||len(account.PublicKey)==0{t.Fatal("init output mismatch")}
 info,err:=os.Stat(keyPath(home));if err!=nil{t.Fatal(err)}
 if info.Mode().Perm()!=0600{t.Fatalf("key permissions %o",info.Mode().Perm())}
 if _,_,err:=invoke(t,at,"init","--home",home);err==nil{t.Fatal("init overwrote existing key")}
 accountOutput,_,err:=invoke(t,at,"account","--home",home);if err!=nil{t.Fatal(err)}
 if output!=accountOutput{t.Fatal("account identity changed")}
 return home,identity
}
func TestOwnerInitAndAccount(t *testing.T){
 testOwner(t,time.Unix(1800000000,0))
}
func TestJoinInspectAndApprove(t *testing.T){
 now:=time.Unix(1800000000,0)
 home,identity:=testOwner(t,now)
 device,err:=trust.GenerateDevice();if err!=nil{t.Fatal(err)}
 request,err:=trust.NewJoinRequest(device,"https://xuhuaai.top",identity.AccountID,"rino-pc",now)
 if err!=nil{t.Fatal(err)}
 data,err:=json.Marshal(request);if err!=nil{t.Fatal(err)}
 joinPath:=filepath.Join(t.TempDir(),"join.json")
 if err:=os.WriteFile(joinPath,data,0600);err!=nil{t.Fatal(err)}
 inspected,_,err:=invoke(t,now,"inspect-join","--request",joinPath)
 if err!=nil{t.Fatal(err)}
 if !strings.Contains(inspected,trust.KeyFingerprint(device.PublicKey))||!strings.Contains(inspected,identity.AccountID){t.Fatal("join inspection omitted identity")}
 approvalFile:=filepath.Join(t.TempDir(),"approval.json")
 _,status,err:=invoke(t,now,"approve","--request",joinPath,"--out",approvalFile,"--home",home)
 if err!=nil{t.Fatal(err)}
 if !strings.Contains(status,trust.KeyFingerprint(device.PublicKey)){t.Fatal("device fingerprint not printed")}
 raw,err:=os.ReadFile(approvalFile);if err!=nil{t.Fatal(err)}
 var approval trust.SignedApproval
 if err:=json.Unmarshal(raw,&approval);err!=nil{t.Fatal(err)}
 paired,err:=trust.ApplyApproval(identity.Public,request,approval,now)
 if err!=nil{t.Fatal(err)}
 if paired.DeviceID!=request.Request.DeviceID||paired.AccountID!=identity.AccountID{t.Fatal("paired wrong identity")}
 if _,_,err:=invoke(t,now,"approve","--request",joinPath,"--out",approvalFile,"--home",home);err==nil{t.Fatal("overwrote existing approval")}
 if _,_,err:=invoke(t,now.Add(16*time.Minute),"approve","--request",joinPath,"--home",home);err==nil{t.Fatal("approved expired request")}
 anotherPub,_,err:=ed25519.GenerateKey(nil);if err!=nil{t.Fatal(err)}
 different:=request
 different.Request.AccountID=accountID(anotherPub)
 forged,err:=json.Marshal(different);if err!=nil{t.Fatal(err)}
 if err:=os.WriteFile(joinPath,forged,0600);err!=nil{t.Fatal(err)}
 if _,_,err:=invoke(t,now,"approve","--request",joinPath,"--home",home);err==nil{t.Fatal("approved different account")}
}
func TestSignedCommandCanBeVerifiedByAgent(t *testing.T){
 now:=time.Unix(1800000000,0)
 home,identity:=testOwner(t,now)
 device,err:=trust.GenerateDevice();if err!=nil{t.Fatal(err)}
 gateway:="https://xuhuaai.top"
 raw,_,err:=invoke(t,now,"sign-command","--gateway",gateway,"--device",trust.DeviceID(device.PublicKey),"--command","hostname","--home",home)
 if err!=nil{t.Fatal(err)}
 var signed trust.SignedCommand
 if err:=json.Unmarshal([]byte(raw),&signed);err!=nil{t.Fatal(err)}
 v,err:=trust.NewAgentVerifier(trust.Pairing{AccountID:identity.AccountID,GatewayID:gateway,DeviceID:trust.DeviceID(device.PublicKey),OwnerPublicKey:identity.Public},filepath.Join(t.TempDir(),"seen.json"))
 if err!=nil{t.Fatal(err)}
 if err:=v.Accept(signed,now);err!=nil{t.Fatal(err)}
 if err:=v.Accept(signed,now);err==nil{t.Fatal("replay accepted")}
 if _,_,err:=invoke(t,now,"sign-command","--gateway",gateway,"--device",trust.DeviceID(device.PublicKey),"--command","hostname","--ttl","10m","--home",home);err==nil{t.Fatal("signed invalid duration")}
 if _,_,err:=invoke(t,now,"sign-command","--gateway",gateway,"--device","hostname","--command","hostname","--home",home);err==nil{t.Fatal("accepted hostname as device identity")}
}
func TestNeverWriteSecretsOutsidePrivateDirectory(t *testing.T){
 now:=time.Unix(1800000000,0)
 worldReadable:=filepath.Join(t.TempDir(),"public")
 if err:=os.Mkdir(worldReadable,0755);err!=nil{t.Fatal(err)}
 if err:=os.Chmod(worldReadable,0755);err!=nil{t.Fatal(err)} // CI/systemd may use umask 0077
 if _,_,err:=invoke(t,now,"init","--home",worldReadable);err==nil{t.Fatal("created key in public directory")}
 if _,err:=os.Stat(filepath.Join(worldReadable,"owner.key"));err==nil{t.Fatal("key left behind")}
}
