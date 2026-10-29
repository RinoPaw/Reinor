package registry

import (
 "crypto/rand"
 "crypto/sha256"
 "encoding/hex"
 "encoding/json"
 "errors"
 "fmt"
 "os"
 "path/filepath"
 "sort"
 "strings"
 "sync"
 "time"
)

type Account struct {
 ID string `json:"id"`
 Name string `json:"name"`
 TokenHash string `json:"token_hash"`
 CreatedAt time.Time `json:"created_at"`
}
type JoinRequest struct {
 ID string `json:"id"`
 AccountID string `json:"account_id"`
 DeviceID string `json:"device_id"`
 Label string `json:"label"`
 OS string `json:"os"`
 TicketHash string `json:"ticket_hash"`
 State string `json:"state"`
 CreatedAt time.Time `json:"created_at"`
 ExpiresAt time.Time `json:"expires_at"`
}
type Device struct {
 ID string `json:"id"`
 AccountID string `json:"account_id"`
 Label string `json:"label"`
 OS string `json:"os"`
 JoinedAt time.Time `json:"joined_at"`
}
type state struct {
 Accounts map[string]Account `json:"accounts"`
 Requests map[string]JoinRequest `json:"requests"`
 Devices map[string]Device `json:"devices"`
}
type Store struct{mu sync.Mutex; path string; data state}
var ErrNotFound=errors.New("not found")
var ErrUnauthorized=errors.New("invalid account or request credential")
var ErrConflict=errors.New("request or device already claimed")
var ErrExpired=errors.New("join request expired")
func empty()state{return state{Accounts:map[string]Account{},Requests:map[string]JoinRequest{},Devices:map[string]Device{}}}
func Open(path string)(*Store,error){
 if path==""{return nil,errors.New("registry path is required")}
 s:=&Store{path:path,data:empty()}
 b,err:=os.ReadFile(path)
 if errors.Is(err,os.ErrNotExist){return s,nil}
 if err!=nil{return nil,err}
 if err:=json.Unmarshal(b,&s.data);err!=nil{return nil,fmt.Errorf("invalid registry: %w",err)}
 if s.data.Accounts==nil||s.data.Requests==nil||s.data.Devices==nil{return nil,errors.New("incomplete registry")}
 return s,nil
}
func randomID(prefix string)(string,error){
 b:=make([]byte,24)
 if _,err:=rand.Read(b);err!=nil{return "",err}
 return prefix+hex.EncodeToString(b),nil
}
func hash(token string)string{h:=sha256.Sum256([]byte(token));return hex.EncodeToString(h[:])}
func snapshot(d state)state{
 n:=empty()
 for k,v:=range d.Accounts{n.Accounts[k]=v}
 for k,v:=range d.Requests{n.Requests[k]=v}
 for k,v:=range d.Devices{n.Devices[k]=v}
 return n
}
func (s *Store)commit(d state)error{
 b,err:=json.MarshalIndent(d,"","  ");if err!=nil{return err}
 dir:=filepath.Dir(s.path)
 if err:=os.MkdirAll(dir,0700);err!=nil{return err}
 f,err:=os.CreateTemp(dir,".registry-*");if err!=nil{return err}
 defer os.Remove(f.Name())
 if err:=f.Chmod(0600);err!=nil{f.Close();return err}
 if _,err:=f.Write(b);err!=nil{f.Close();return err}
 if err:=f.Sync();err!=nil{f.Close();return err}
 if err:=f.Close();err!=nil{return err}
 if err:=os.Rename(f.Name(),s.path);err!=nil{return err}
 s.data=d
 return nil
}
func (s *Store)CreateAccount(name string)(Account,string,error){
 name=strings.TrimSpace(name)
 if len(name)<1||len(name)>80{return Account{},"",errors.New("account name must be 1-80 characters")}
 id,err:=randomID("acct_");if err!=nil{return Account{},"",err}
 token,err:=randomID("rma_");if err!=nil{return Account{},"",err}
 account:=Account{ID:id,Name:name,TokenHash:hash(token),CreatedAt:time.Now().UTC()}
 s.mu.Lock();defer s.mu.Unlock()
 next:=snapshot(s.data);next.Accounts[id]=account
 if err:=s.commit(next);err!=nil{return Account{},"",err}
 return account,token,nil
}
func (s *Store)Authenticate(token string)(Account,bool){
 if len(token)<32||len(token)>128{return Account{},false}
 digest:=hash(token)
 s.mu.Lock();defer s.mu.Unlock()
 for _,a:=range s.data.Accounts{if a.TokenHash==digest{return a,true}}
 return Account{},false
}
func (s *Store)GetAccount(id string)(Account,bool){
 s.mu.Lock();defer s.mu.Unlock()
 a,ok:=s.data.Accounts[id];return a,ok
}
func (s *Store)ListAccounts()[]Account{
 s.mu.Lock();defer s.mu.Unlock()
 list:=make([]Account,0,len(s.data.Accounts))
 for _,a:=range s.data.Accounts{list=append(list,a)}
 sort.Slice(list,func(i,j int)bool{return list[i].CreatedAt.Before(list[j].CreatedAt)})
 return list
}
func validDeviceID(id string)bool{
 if len(id)<1||len(id)>128{return false}
 for _,c:=range id{if !(c>='a'&&c<='z'||c>='A'&&c<='Z'||c>='0'&&c<='9'||c=='_'||c=='-'||c=='.'){return false}}
 return true
}
func (s *Store)RequestJoin(accountID,deviceID,label,osName string)(JoinRequest,string,error){
 if !validDeviceID(deviceID)||len(label)>120||len(osName)>48{return JoinRequest{},"",errors.New("invalid device identity or metadata")}
 id,err:=randomID("join_");if err!=nil{return JoinRequest{},"",err}
 ticket,err:=randomID("ticket_");if err!=nil{return JoinRequest{},"",err}
 now:=time.Now().UTC()
 req:=JoinRequest{ID:id,AccountID:accountID,DeviceID:deviceID,Label:label,OS:osName,TicketHash:hash(ticket),State:"pending",CreatedAt:now,ExpiresAt:now.Add(24*time.Hour)}
 s.mu.Lock();defer s.mu.Unlock()
 if _,exists:=s.data.Accounts[accountID];!exists{return JoinRequest{},"",ErrNotFound}
 if d,exists:=s.data.Devices[deviceID];exists&&d.AccountID!=accountID{return JoinRequest{},"",ErrConflict}
 for _,old:=range s.data.Requests{
  if old.DeviceID==deviceID&&old.AccountID==accountID&&old.State=="pending"&&time.Now().Before(old.ExpiresAt){return JoinRequest{},"",ErrConflict}
 }
 next:=snapshot(s.data);next.Requests[id]=req
 if err:=s.commit(next);err!=nil{return JoinRequest{},"",err}
 return req,ticket,nil
}
func (s *Store)PollJoin(id,ticket string)(JoinRequest,error){
 s.mu.Lock();defer s.mu.Unlock()
 req,ok:=s.data.Requests[id]
 if !ok||len(ticket)<32||hash(ticket)!=req.TicketHash{return JoinRequest{},ErrUnauthorized}
 req.TicketHash=""
 if req.State=="pending"&&time.Now().After(req.ExpiresAt){req.State="expired"}
 return req,nil
}
func (s *Store)ListJoins(accountID string)[]JoinRequest{
 s.mu.Lock();defer s.mu.Unlock()
 items:=[]JoinRequest{}
 for _,r:=range s.data.Requests{
  if r.AccountID!=accountID{continue}
  r.TicketHash=""
  if r.State=="pending"&&time.Now().After(r.ExpiresAt){r.State="expired"}
  items=append(items,r)
 }
 sort.Slice(items,func(i,j int)bool{return items[i].CreatedAt.Before(items[j].CreatedAt)})
 return items
}
func (s *Store)Decide(accountID,requestID string,approve bool)(JoinRequest,error){
 s.mu.Lock();defer s.mu.Unlock()
 old,ok:=s.data.Requests[requestID]
 if !ok||old.AccountID!=accountID{return JoinRequest{},ErrNotFound}
 if old.State!="pending"{return JoinRequest{},ErrConflict}
 if time.Now().After(old.ExpiresAt){return JoinRequest{},ErrExpired}
 next:=snapshot(s.data)
 if approve{
  if existing,exists:=next.Devices[old.DeviceID];exists&&existing.AccountID!=accountID{return JoinRequest{},ErrConflict}
  old.State="approved"
  next.Devices[old.DeviceID]=Device{ID:old.DeviceID,AccountID:accountID,Label:old.Label,OS:old.OS,JoinedAt:time.Now().UTC()}
 }else{old.State="rejected"}
 next.Requests[requestID]=old
 if err:=s.commit(next);err!=nil{return JoinRequest{},err}
 old.TicketHash=""
 return old,nil
}
func (s *Store)ListDevices(accountID string)[]Device{
 s.mu.Lock();defer s.mu.Unlock()
 out:=[]Device{}
 for _,d:=range s.data.Devices{if d.AccountID==accountID{out=append(out,d)}}
 sort.Slice(out,func(i,j int)bool{return out[i].ID<out[j].ID})
 return out
}
func (s *Store)Owns(accountID,deviceID string)bool{
 s.mu.Lock();defer s.mu.Unlock()
 d,ok:=s.data.Devices[deviceID];return ok&&d.AccountID==accountID
}

func (s *Store)AuthorizeDeviceToken(token,deviceID string)bool{
 if len(token)<32||len(token)>128{return false}
 digest:=hash(token)
 s.mu.Lock();defer s.mu.Unlock()
 for _,r:=range s.data.Requests{
  if r.TicketHash==digest&&r.DeviceID==deviceID&&r.State=="approved"{
   d,ok:=s.data.Devices[deviceID]
   return ok&&d.AccountID==r.AccountID
  }
 }
 return false
}
