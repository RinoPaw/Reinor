package trust

import (
 "crypto/ed25519"
 "os"
 "path/filepath"
 "strings"
 "testing"
 "time"
)

func setup(t *testing.T)(DeviceIdentity,ed25519.PublicKey,ed25519.PrivateKey,SignedJoinRequest,SignedApproval,Pairing,time.Time){
 t.Helper()
 now:=time.Unix(1800000000,0)
 device,err:=GenerateDevice();if err!=nil{t.Fatal(err)}
 ownerPub,ownerPriv,err:=GenerateOwner();if err!=nil{t.Fatal(err)}
 request,err:=NewJoinRequest(device,"https://gateway.example","acct-one","test pc",now)
 if err!=nil{t.Fatal(err)}
 if err:=VerifyJoinRequest(request,now);err!=nil{t.Fatal(err)}
 approval,err:=SignApproval(ownerPriv,ApprovalFor(request.Request));if err!=nil{t.Fatal(err)}
 pairing,err:=ApplyApproval(ownerPub,request,approval,now)
 if err!=nil{t.Fatal(err)}
 return device,ownerPub,ownerPriv,request,approval,pairing,now
}

func TestJoinRequestAndApproval(t *testing.T){
 device,pub,owner,request,approval,_,now:=setup(t)
 if request.Request.DeviceID!=DeviceID(device.PublicKey){t.Fatal("device identity mismatch")}
 if KeyFingerprint(pub)==KeyFingerprint(device.PublicKey){t.Fatal("key fingerprints unexpectedly equal")}
 cases:=map[string]SignedJoinRequest{}
 forged:=request
 forged.Request.AccountID="acct-two"
 cases["altered-account"]=forged
 forged=request
 forged.Request.DevicePublicKey=append([]byte(nil),device.PublicKey...)
 forged.Request.DevicePublicKey[0]^=1
 cases["altered-device-key"]=forged
 forged=request
 forged.Request.Nonce="substituted"
 cases["altered-nonce"]=forged
 for name,candidate:=range cases{t.Run(name,func(t *testing.T){if err:=VerifyJoinRequest(candidate,now);err==nil{t.Fatal("forgery accepted")}})}
 other,_,err:=GenerateOwner();if err!=nil{t.Fatal(err)}
 if _,err:=ApplyApproval(other,request,approval,now);err==nil{t.Fatal("different owner key accepted")}
 wrong:=approval
 wrong.Approval.AccountID="acct-two"
 if _,err:=ApplyApproval(pub,request,wrong,now);err==nil{t.Fatal("approval for another account accepted")}
 wrong=approval
 wrong.Approval.RequestID="other-request"
 if _,err:=ApplyApproval(pub,request,wrong,now);err==nil{t.Fatal("approval for another request accepted")}
 wrong=approval
 wrong.Approval.GatewayID="https://different-gateway.example"
 if _,err:=ApplyApproval(pub,request,wrong,now);err==nil{t.Fatal("different gateway accepted")}
 if _,err:=ApplyApproval(pub,request,approval,now.Add(16*time.Minute));err==nil{t.Fatal("expired request accepted")}
 fakeSigned,err:=SignApproval(owner,ApprovalFor(request.Request));if err!=nil||len(fakeSigned.Signature)!=ed25519.SignatureSize{t.Fatal("signature missing",err)}
 if _,err:=ApplyApproval(nil,request,approval,now);err==nil{t.Fatal("untrusted public key accepted")}
}

func TestSignedCommandAndPersistentReplay(t *testing.T){
 _,_,owner,_,_,pairing,now:=setup(t)
 replayFile:=filepath.Join(t.TempDir(),"agent","seen.json")
 verifier,err:=NewAgentVerifier(pairing,replayFile)
 if err!=nil{t.Fatal(err)}
 taskID,err:=RandomID();if err!=nil{t.Fatal(err)}
 nonce,err:=RandomID();if err!=nil{t.Fatal(err)}
 base:=Command{GatewayID:pairing.GatewayID,AccountID:pairing.AccountID,DeviceID:pairing.DeviceID,TaskID:taskID,Nonce:nonce,Shell:"powershell",Command:"hostname",IssuedAt:now.Unix(),ExpiresAt:now.Add(time.Minute).Unix()}
 good,err:=SignCommand(owner,base);if err!=nil{t.Fatal(err)}
 if err:=verifier.Accept(good,now);err!=nil{t.Fatalf("valid command failed: %v",err)}
 if err:=verifier.Accept(good,now);err==nil{t.Fatal("replayed command accepted")}
 reloaded,err:=NewAgentVerifier(pairing,replayFile);if err!=nil{t.Fatal(err)}
 if err:=reloaded.Accept(good,now);err==nil{t.Fatal("replay accepted after Agent restart")}
 body,err:=os.ReadFile(replayFile);if err!=nil||!strings.Contains(string(body),taskID){t.Fatalf("replay log not persisted: %s %v",body,err)}
 tests:=map[string]SignedCommand{}
 tampered:=good
 tampered.Command.Command="Remove-Item -Recurse C:\\"
 tests["tampered-command"]=tampered
 tampered=good;tampered.Command.AccountID="acct-two";tests["other-account"]=tampered
 tampered=good;tampered.Command.DeviceID="dev_another";tests["other-device"]=tampered
 tampered=good;tampered.Command.GatewayID="https://evil.example";tests["other-gateway"]=tampered
 tampered=good;tampered.Signature=append([]byte(nil),good.Signature...);tampered.Signature[0]^=1;tests["forged-signature"]=tampered
 for name,bad:=range tests{t.Run(name,func(t *testing.T){if err:=verifier.Accept(bad,now);err==nil{t.Fatal("unauthorized command accepted")}})}
 noExpiry:=base;noExpiry.ExpiresAt=now.Add(-time.Second).Unix()
 expired,_:=SignCommand(owner,noExpiry)
 if err:=verifier.Accept(expired,now);err==nil{t.Fatal("expired command accepted")}
 future:=base;future.IssuedAt=now.Add(time.Minute).Unix();future.ExpiresAt=now.Add(2*time.Minute).Unix()
 futureSigned,_:=SignCommand(owner,future)
 if err:=verifier.Accept(futureSigned,now);err==nil{t.Fatal("future command accepted")}
 differentOwner,err:=NewAgentVerifier(Pairing{GatewayID:pairing.GatewayID,AccountID:pairing.AccountID,DeviceID:pairing.DeviceID,OwnerPublicKey:make([]byte,ed25519.PublicKeySize)},filepath.Join(t.TempDir(),"replay.json"))
 if err!=nil{t.Fatal(err)}
 if err:=differentOwner.Accept(good,now);err==nil{t.Fatal("untrusted owner accepted")}
}

func TestFailClosedWhenReplayStoreUnavailable(t *testing.T){
 _,_,owner,_,_,pairing,now:=setup(t)
 replayFile:=t.TempDir() // a directory cannot be read as the replay file
 v,err:=NewAgentVerifier(pairing,replayFile);if err!=nil{t.Fatal(err)}
 cmd:=Command{GatewayID:pairing.GatewayID,AccountID:pairing.AccountID,DeviceID:pairing.DeviceID,TaskID:"task001",Nonce:"nonce001",Shell:"powershell",Command:"hostname",IssuedAt:now.Unix(),ExpiresAt:now.Add(time.Minute).Unix()}
 signed,_:=SignCommand(owner,cmd)
 if err:=v.Accept(signed,now);err==nil{t.Fatal("Agent must refuse execution without durable replay storage")}
}
