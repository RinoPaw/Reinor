package registry

import (
 "errors"
 "path/filepath"
 "testing"
 "time"
)
func TestAccountsAndJoins(t *testing.T){
 path:=filepath.Join(t.TempDir(),"accounts.json")
 s,err:=Open(path);if err!=nil{t.Fatal(err)}
 a,ta,err:=s.CreateAccount("Alice");if err!=nil{t.Fatal(err)}
 b,tb,err:=s.CreateAccount("Bob");if err!=nil{t.Fatal(err)}
 if a.ID==b.ID||ta==tb{t.Fatal("account identity collision")}
 if _,ok:=s.Authenticate("invalid");ok{t.Fatal("invalid credential accepted")}
 if who,ok:=s.Authenticate(tb);!ok||who.ID!=b.ID{t.Fatal("bad login")}
 if who,ok:=s.Authenticate(ta);!ok||who.ID!=a.ID{t.Fatal("bad first account login")}
 if len(s.ListAccounts())!=2{t.Fatal("accounts not saved")}
 req,ticket,err:=s.RequestJoin(a.ID,"device-one","first pc","windows");if err!=nil{t.Fatal(err)}
 if req.State!="pending"{t.Fatal("not pending")}
 if len(s.ListJoins(b.ID))!=0{t.Fatal("cross-account request leaked")}
 if _,err:=s.PollJoin(req.ID,"wrong");!errors.Is(err,ErrUnauthorized){t.Fatal("poll should require ticket")}
 if _,err:=s.Decide(b.ID,req.ID,true);!errors.Is(err,ErrNotFound){t.Fatal("other account approved request")}
 if _,_,err:=s.RequestJoin(a.ID,"device-one","first pc","windows");!errors.Is(err,ErrConflict){t.Fatal("duplicate request accepted")}
 if got,err:=s.Decide(a.ID,req.ID,true);err!=nil||got.State!="approved"{t.Fatalf("approve: %#v %v",got,err)}
 if !s.Owns(a.ID,"device-one")||s.Owns(b.ID,"device-one"){t.Fatal("wrong device ownership")}
 if _,_,err:=s.RequestJoin(b.ID,"device-one","claimed pc","windows");!errors.Is(err,ErrConflict){t.Fatal("another account may claim existing device")}
 if got,err:=s.PollJoin(req.ID,ticket);err!=nil||got.State!="approved"||got.TicketHash!=""{t.Fatalf("bad poll: %#v %v",got,err)}
 if _,err:=s.Decide(a.ID,req.ID,false);!errors.Is(err,ErrConflict){t.Fatal("already approved request changed state")}
 declined,ticket2,err:=s.RequestJoin(b.ID,"device-two","second","linux");if err!=nil{t.Fatal(err)}
 if _,err:=s.Decide(b.ID,declined.ID,false);err!=nil{t.Fatal(err)}
 if got,err:=s.PollJoin(declined.ID,ticket2);err!=nil||got.State!="rejected"{t.Fatal("reject not visible")}
 restored,err:=Open(path);if err!=nil{t.Fatal(err)}
 if len(restored.ListDevices(a.ID))!=1||len(restored.ListDevices(b.ID))!=0{t.Fatal("device registry not persistent")}
 if got,ok:=restored.Authenticate(ta);!ok||got.Name!="Alice"{t.Fatal("login not persistent")}
 if got:=restored.ListJoins(b.ID);len(got)!=1||got[0].State!="rejected"{t.Fatal("join history not persistent")}
}
func TestBadInputs(t *testing.T){
 s,err:=Open(filepath.Join(t.TempDir(),"db.json"));if err!=nil{t.Fatal(err)}
 if _,_,err:=s.CreateAccount("");err==nil{t.Fatal("empty account accepted")}
 if _,_,err:=s.RequestJoin("missing","dev","pc","windows");!errors.Is(err,ErrNotFound){t.Fatal("unknown account accepted")}
 a,_,err:=s.CreateAccount("owner");if err!=nil{t.Fatal(err)}
 if _,_,err:=s.RequestJoin(a.ID,"invalid device","pc","windows");err==nil{t.Fatal("invalid device ID accepted")}
 req,ticket,err:=s.RequestJoin(a.ID,"dev","pc","windows");if err!=nil{t.Fatal(err)}
 // Simulate an expired request without clock delays using an internal state update.
 s.mu.Lock()
 next:=snapshot(s.data);old:=next.Requests[req.ID];old.ExpiresAt=time.Now().Add(-time.Second);next.Requests[req.ID]=old
 err=s.commit(next);s.mu.Unlock();if err!=nil{t.Fatal(err)}
 if _,err:=s.Decide(a.ID,req.ID,true);!errors.Is(err,ErrExpired){t.Fatal("expired request approved")}
 if state,err:=s.PollJoin(req.ID,ticket);err!=nil||state.State!="expired"{t.Fatal("expired request not reported")}
}
