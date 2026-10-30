package oauthlocal

import (
 "crypto/hmac"
 "crypto/rand"
 "crypto/sha256"
 "crypto/subtle"
 "encoding/base64"
 "encoding/json"
 "errors"
 "fmt"
 "html"
 "io"
 "net/http"
 "net/url"
 "os"
 "path/filepath"
 "strings"
 "sync"
 "time"
)

const Origin = "https://xuhuaai.top"
const Resource = Origin + "/mcp"
const Scope = "device:execute"
const Metadata = Origin + "/.well-known/oauth-protected-resource/mcp"

type Client struct {
 ID string `json:"client_id"`
 Redirects []string `json:"redirect_uris"`
 Name string `json:"client_name,omitempty"`
}
type Code struct{
 ClientID string
 Redirect string
 Challenge string
 Expires time.Time
}
type Authorization struct{
 ClientID string
 Redirect string
 Challenge string
 State string
 Expires time.Time
}
type CompletedAuthorization struct {
 RedirectURL string
 Expires time.Time
}
type Server struct {
 mu sync.Mutex
 owner []byte
 signing []byte
 file string
 clients map[string]Client
 codes map[string]Code
 authorizations map[string]Authorization
 completed map[string]CompletedAuthorization
}
func New(ownerToken,filename string)(*Server,error){
 if len(ownerToken)<32{return nil,errors.New("owner token too short")}
 if filename==""{return nil,errors.New("OAuth client storage path required")}
 derived:=sha256.Sum256([]byte("rinomesh/oauth-signing/v1:"+ownerToken))
 s:=&Server{owner:[]byte(ownerToken),signing:derived[:],file:filename,clients:map[string]Client{},codes:map[string]Code{},authorizations:map[string]Authorization{},completed:map[string]CompletedAuthorization{}}
 buf,err:=os.ReadFile(filename)
 if errors.Is(err,os.ErrNotExist){return s,nil}
 if err!=nil{return nil,err}
 if err:=json.Unmarshal(buf,&s.clients);err!=nil{return nil,fmt.Errorf("OAuth clients file: %w",err)}
 if s.clients==nil{return nil,errors.New("invalid OAuth client registry")}
 return s,nil
}
func (s *Server)saveClients(next map[string]Client)error {
 if err:=os.MkdirAll(filepath.Dir(s.file),0700);err!=nil{return err}
 data,err:=json.MarshalIndent(next,"","  ");if err!=nil{return err}
 file,err:=os.CreateTemp(filepath.Dir(s.file),".oauth-*");if err!=nil{return err}
 defer os.Remove(file.Name())
 if err:=file.Chmod(0600);err!=nil{file.Close();return err}
 if _,err:=file.Write(data);err!=nil{file.Close();return err}
 if err:=file.Sync();err!=nil{file.Close();return err}
 if err:=file.Close();err!=nil{return err}
 if err:=os.Rename(file.Name(),s.file);err!=nil{return err}
 s.clients=next
 return nil
}
func random(prefix string)(string,error){
 b:=make([]byte,32)
 if _,err:=rand.Read(b);err!=nil{return "",err}
 return prefix+base64.RawURLEncoding.EncodeToString(b),nil
}
func jsonReply(w http.ResponseWriter,status int,value any){
 w.Header().Set("Content-Type","application/json")
 w.Header().Set("Cache-Control","no-store")
 w.WriteHeader(status)
 _=json.NewEncoder(w).Encode(value)
}
func oauthError(w http.ResponseWriter,status int,code string){
 jsonReply(w,status,map[string]string{"error":code})
}
func validCallback(raw string)bool{
 u,err:=url.Parse(raw)
 if err!=nil||u.Scheme!="https"||u.Host!="chatgpt.com"||u.User!=nil||u.RawQuery!=""||u.Fragment!="" {return false}
 return u.Path=="/connector_platform_oauth_redirect"||strings.HasPrefix(u.Path,"/connector/oauth/")
}
func (s *Server)Register(w http.ResponseWriter,r *http.Request){
 if r.Method!=http.MethodPost{oauthError(w,405,"invalid_request");return}
 var request struct{
  Redirects []string `json:"redirect_uris"`
  Name string `json:"client_name"`
  AuthMethod string `json:"token_endpoint_auth_method"`
 }
 d:=json.NewDecoder(io.LimitReader(r.Body,16<<10));d.DisallowUnknownFields()
 // DCR permits additional optional metadata fields, so decode permissively.
 _=d
 var raw map[string]json.RawMessage
 if err:=json.NewDecoder(io.LimitReader(r.Body,16<<10)).Decode(&raw);err!=nil{oauthError(w,400,"invalid_client_metadata");return}
 if err:=json.Unmarshal(raw["redirect_uris"],&request.Redirects);err!=nil||len(request.Redirects)==0||len(request.Redirects)>8{oauthError(w,400,"invalid_redirect_uri");return}
 for _,item:=range request.Redirects{if !validCallback(item){oauthError(w,400,"invalid_redirect_uri");return}}
 _=json.Unmarshal(raw["client_name"],&request.Name)
 _=json.Unmarshal(raw["token_endpoint_auth_method"],&request.AuthMethod)
 if request.AuthMethod!=""&&request.AuthMethod!="none"{oauthError(w,400,"invalid_client_metadata");return}
 if len(request.Name)>160{request.Name=request.Name[:160]}
 id,err:=random("mcp_");if err!=nil{oauthError(w,500,"server_error");return}
 client:=Client{ID:id,Redirects:request.Redirects,Name:request.Name}
 s.mu.Lock()
 next:=make(map[string]Client,len(s.clients)+1)
 for k,v:=range s.clients{next[k]=v}
 next[id]=client
 err=s.saveClients(next)
 s.mu.Unlock()
 if err!=nil{oauthError(w,500,"server_error");return}
 jsonReply(w,201,map[string]any{"client_id":id,"client_id_issued_at":time.Now().Unix(),"redirect_uris":client.Redirects,"client_name":client.Name,"token_endpoint_auth_method":"none","grant_types":[]string{"authorization_code","refresh_token"},"response_types":[]string{"code"}})
}
func (s *Server)ResourceMetadata(w http.ResponseWriter,r *http.Request){
 if r.Method!=http.MethodGet{oauthError(w,405,"invalid_request");return}
 jsonReply(w,200,map[string]any{"resource":Resource,"authorization_servers":[]string{Origin},"scopes_supported":[]string{Scope},"bearer_methods_supported":[]string{"header"}})
}
func (s *Server)ServerMetadata(w http.ResponseWriter,r *http.Request){
 if r.Method!=http.MethodGet{oauthError(w,405,"invalid_request");return}
 jsonReply(w,200,map[string]any{
  "issuer":Origin,"authorization_response_iss_parameter_supported":true,"authorization_endpoint":Origin+"/authorize","token_endpoint":Origin+"/token","registration_endpoint":Origin+"/register",
  "response_types_supported":[]string{"code"},"grant_types_supported":[]string{"authorization_code","refresh_token"},
  "code_challenge_methods_supported":[]string{"S256"},"token_endpoint_auth_methods_supported":[]string{"none"},
  "scopes_supported":[]string{Scope},
 })
}
func (s *Server)Challenge(w http.ResponseWriter){
 w.Header().Set("WWW-Authenticate",`Bearer resource_metadata="`+Metadata+`", scope="`+Scope+`"`)
 w.Header().Set("Cache-Control","no-store")
 http.Error(w,"OAuth authorization required",http.StatusUnauthorized)
}
func allowedRedirect(c Client,value string)bool {
 for _,uri:=range c.Redirects{if uri==value{return true}}
 return false
}
func validChallenge(value string)bool{
 if len(value)!=43{return false}
 b,err:=base64.RawURLEncoding.DecodeString(value)
 return err==nil&&len(b)==32
}
func showAuthorized(w http.ResponseWriter,callbackURL string){
 w.Header().Set("Content-Type","text/html; charset=utf-8")
 w.Header().Set("Cache-Control","no-store")
 w.Header().Set("Referrer-Policy","no-referrer")
 w.Header().Set("Content-Security-Policy","default-src 'none'; style-src 'unsafe-inline'; frame-ancestors 'none'; base-uri 'none'")
 address:=html.EscapeString(callbackURL)
 _,_=io.WriteString(w,"<!doctype html><html lang='zh-CN'><meta charset='utf-8'><meta name='viewport' content='width=device-width'><meta http-equiv='refresh' content='1;url="+address+"'><title>Reinor 授权成功</title><style>body{font:16px system-ui,sans-serif;max-width:440px;margin:8vh auto;padding:20px;color:#222}a{display:block;text-align:center;padding:14px;background:#1769c2;color:white;border-radius:6px;text-decoration:none;margin-top:18px}</style><h2>已授权 ChatGPT</h2><p>正在返回 ChatGPT。如果浏览器没有自动跳转，请点击下面的按钮。</p><a rel='noreferrer' href='"+address+"'>返回 ChatGPT 完成连接</a><p>请勿分享此页面的跳转地址。</p></html>")
}

func (s *Server)Authorize(w http.ResponseWriter,r *http.Request){
 switch r.Method{
 case http.MethodGet:
  params:=r.URL.Query()
  id:=params.Get("client_id")
  redirect:=params.Get("redirect_uri")
  challenge:=params.Get("code_challenge")
  if params.Get("response_type")!="code"||params.Get("code_challenge_method")!="S256"||!validChallenge(challenge)||params.Get("resource")!=""&&params.Get("resource")!=Resource {
   http.Error(w,"Invalid OAuth authorization request",400);return
  }
  scope:=params.Get("scope")
  if scope!=""&&scope!=Scope{http.Error(w,"Unsupported scope",400);return}
  s.mu.Lock()
  client,ok:=s.clients[id]
  s.mu.Unlock()
  if !ok||!allowedRedirect(client,redirect){http.Error(w,"Unknown client or redirect URI",400);return}
  requestID,err:=random("login_");if err!=nil{http.Error(w,"internal error",500);return}
  s.mu.Lock()
  s.authorizations[requestID]=Authorization{ClientID:id,Redirect:redirect,Challenge:challenge,State:params.Get("state"),Expires:time.Now().Add(10*time.Minute)}
  s.mu.Unlock()
  w.Header().Set("Content-Type","text/html; charset=utf-8")
  w.Header().Set("Cache-Control","no-store")
  w.Header().Set("Content-Security-Policy","default-src 'none'; style-src 'unsafe-inline'; form-action 'self'; frame-ancestors 'none'")
  _,_=io.WriteString(w,`<!doctype html><html lang="zh-CN"><meta charset="utf-8"><meta name="viewport" content="width=device-width"><title>Reinor 登录</title><style>body{font:16px system-ui,sans-serif;max-width:440px;margin:8vh auto;padding:20px;color:#222}input,button{font:inherit;padding:12px;box-sizing:border-box;width:100%;margin-top:12px}button{background:#1769c2;color:white;border:0;border-radius:6px;cursor:pointer}</style><h2>授权 ChatGPT 连接 Reinor</h2><p>输入腾讯云上保存的 Gateway 控制令牌，允许 ChatGPT 使用设备查询、审批和命令执行。</p><form method="post" action="/authorize"><input type="hidden" name="request_id" value="`+html.EscapeString(requestID)+`"><input type="password" name="owner_token" placeholder="Gateway 控制令牌" autocomplete="off" required><button type="submit">授权连接</button></form><p>授权后将返回 ChatGPT。</p></html>`)
 case http.MethodPost:
  r.Body=http.MaxBytesReader(w,r.Body,8192)
  if err:=r.ParseForm();err!=nil{http.Error(w,"Invalid request",400);return}
  id:=r.PostForm.Get("request_id")
  received:=r.PostForm.Get("owner_token")
  now:=time.Now()
  s.mu.Lock()
  // A repeated submission of a successful login returns its existing callback.
  // Browser navigation issues must not consume the grant irrecoverably.
  if completed,ok:=s.completed[id];ok&&now.Before(completed.Expires){
   s.mu.Unlock()
   if subtle.ConstantTimeCompare([]byte(received),s.owner)!=1{
    http.Error(w,"Invalid owner token",401)
    return
   }
   showAuthorized(w,completed.RedirectURL)
   return
  }
  authorization,ok:=s.authorizations[id]
  if !ok||now.After(authorization.Expires){
   s.mu.Unlock()
   http.Error(w,"Authorization request expired. Return to ChatGPT and start a new connection attempt.",400)
   return
  }
  if subtle.ConstantTimeCompare([]byte(received),s.owner)!=1{
   s.mu.Unlock()
   http.Error(w,"Invalid owner token. Return to ChatGPT and retry authorization.",401)
   return
  }
  code,err:=random("code_")
  if err!=nil{s.mu.Unlock();http.Error(w,"Internal error",500);return}
  callback,_:=url.Parse(authorization.Redirect)
  params:=callback.Query()
  params.Set("code",code)
  params.Set("iss",Origin)
  if authorization.State!=""{params.Set("state",authorization.State)}
  callback.RawQuery=params.Encode()
  location:=callback.String()
  expires:=now.Add(5*time.Minute)
  s.codes[code]=Code{ClientID:authorization.ClientID,Redirect:authorization.Redirect,Challenge:authorization.Challenge,Expires:expires}
  s.completed[id]=CompletedAuthorization{RedirectURL:location,Expires:expires}
  delete(s.authorizations,id)
  s.mu.Unlock()
  showAuthorized(w,location)
 default:http.Error(w,"Method not allowed",405)
 }
}
func (s *Server) issue(clientID,kind string,ttl time.Duration)(string,error){
 header:=base64.RawURLEncoding.EncodeToString([]byte(`{"typ":"JWT","alg":"HS256"}`))
 payload,err:=json.Marshal(map[string]any{"iss":Origin,"aud":Resource,"sub":"owner","client_id":clientID,"scope":Scope,"kind":kind,"iat":time.Now().Unix(),"exp":time.Now().Add(ttl).Unix()})
 if err!=nil{return "",err}
 prefix:=header+"."+base64.RawURLEncoding.EncodeToString(payload)
 mac:=hmac.New(sha256.New,s.signing);_,_=mac.Write([]byte(prefix))
 return prefix+"."+base64.RawURLEncoding.EncodeToString(mac.Sum(nil)),nil
}
func(s *Server)verify(raw,kind string)(string,error){
 parts:=strings.Split(raw,".")
 if len(parts)!=3||len(raw)>8192{return "",errors.New("invalid token")}
 prefix:=parts[0]+"."+parts[1]
 mac:=hmac.New(sha256.New,s.signing);_,_=mac.Write([]byte(prefix))
 sig,err:=base64.RawURLEncoding.DecodeString(parts[2])
 if err!=nil||!hmac.Equal(sig,mac.Sum(nil)){return "",errors.New("invalid token signature")}
 body,err:=base64.RawURLEncoding.DecodeString(parts[1])
 if err!=nil{return "",err}
 var v struct{
  Iss string `json:"iss"`
  Aud string `json:"aud"`
  ClientID string `json:"client_id"`
  Scope string `json:"scope"`
  Kind string `json:"kind"`
  Exp int64 `json:"exp"`
 }
 if err:=json.Unmarshal(body,&v);err!=nil{return "",err}
 if v.Iss!=Origin||v.Aud!=Resource||v.Scope!=Scope||v.Kind!=kind||time.Now().Unix()>=v.Exp{return "",errors.New("token expired or invalid claims")}
 s.mu.Lock();_,ok:=s.clients[v.ClientID];s.mu.Unlock()
 if !ok{return "",errors.New("unknown client")}
 return v.ClientID,nil
}
func(s *Server)ValidAccess(token string)bool{_,err:=s.verify(token,"access");return err==nil}
func(s *Server)Token(w http.ResponseWriter,r *http.Request){
 if r.Method!=http.MethodPost{oauthError(w,405,"invalid_request");return}
 r.Body=http.MaxBytesReader(w,r.Body,16384)
 if err:=r.ParseForm();err!=nil{oauthError(w,400,"invalid_request");return}
 id:=r.PostForm.Get("client_id")
 resource:=r.PostForm.Get("resource")
 if resource!=""&&resource!=Resource{oauthError(w,400,"invalid_target");return}
 s.mu.Lock();_,exists:=s.clients[id];s.mu.Unlock()
 if !exists{oauthError(w,401,"invalid_client");return}
 switch r.PostForm.Get("grant_type"){
 case "authorization_code":
  code:=r.PostForm.Get("code")
  redirect:=r.PostForm.Get("redirect_uri")
  verifier:=r.PostForm.Get("code_verifier")
  if len(verifier)<43||len(verifier)>128{oauthError(w,400,"invalid_grant");return}
  digest:=sha256.Sum256([]byte(verifier))
  challenge:=base64.RawURLEncoding.EncodeToString(digest[:])
  s.mu.Lock()
  pending,ok:=s.codes[code]
  if ok&&pending.ClientID==id&&pending.Redirect==redirect&&subtle.ConstantTimeCompare([]byte(pending.Challenge),[]byte(challenge))==1&&time.Now().Before(pending.Expires){
   delete(s.codes,code)
  }else{ok=false}
  s.mu.Unlock()
  if !ok{oauthError(w,400,"invalid_grant");return}
 case "refresh_token":
  refresh:=r.PostForm.Get("refresh_token")
  client,err:=s.verify(refresh,"refresh")
  if err!=nil||client!=id{oauthError(w,400,"invalid_grant");return}
 default:oauthError(w,400,"unsupported_grant_type");return
 }
 access,err:=s.issue(id,"access",time.Hour)
 if err!=nil{oauthError(w,500,"server_error");return}
 refresh,err:=s.issue(id,"refresh",30*24*time.Hour)
 if err!=nil{oauthError(w,500,"server_error");return}
 jsonReply(w,200,map[string]any{"access_token":access,"token_type":"Bearer","expires_in":3600,"refresh_token":refresh,"scope":Scope})
}
func (s *Server)InstallRoutes(mux *http.ServeMux){
 mux.HandleFunc("/.well-known/oauth-protected-resource/mcp",s.ResourceMetadata)
 mux.HandleFunc("/.well-known/oauth-protected-resource",s.ResourceMetadata)
 mux.HandleFunc("/.well-known/oauth-authorization-server",s.ServerMetadata)
 mux.HandleFunc("/register",s.Register)
 mux.HandleFunc("/authorize",s.Authorize)
 mux.HandleFunc("/token",s.Token)
}
