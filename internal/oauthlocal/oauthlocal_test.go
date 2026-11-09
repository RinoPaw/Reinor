package oauthlocal

import(
 "crypto/sha256"
 "encoding/base64"
 "encoding/json"
 "html"
 "io"
 "net/http"
 "net/http/httptest"
 "net/url"
 "path/filepath"
 "strings"
 "testing"
)
func TestOAuthFromRegistrationThroughRefresh(t *testing.T){
 owner:="my-gateway-owner-token-with-64-character-random-value-demo"
 s,err:=New(owner,filepath.Join(t.TempDir(),"oauth-clients.json"));if err!=nil{t.Fatal(err)}
 mux:=http.NewServeMux();s.InstallRoutes(mux)
 server:=httptest.NewServer(mux);defer server.Close()
 client:=server.Client()
 client.CheckRedirect=func(_ *http.Request,_ []*http.Request)error{return http.ErrUseLastResponse}
 get:=func(path string)map[string]any{
  t.Helper()
  res,err:=client.Get(server.URL+path);if err!=nil{t.Fatal(err)}
  defer res.Body.Close()
  if res.StatusCode!=200{t.Fatalf("GET %s: %d",path,res.StatusCode)}
  data,_:=io.ReadAll(res.Body)
  var body map[string]any;if err:=json.Unmarshal(data,&body);err!=nil{t.Fatal(err)}
  return body
 }
 if got:=get("/.well-known/oauth-protected-resource/mcp")["resource"];got!=Resource{t.Fatalf("wrong resource %v",got)}
 meta:=get("/.well-known/oauth-authorization-server")
 if meta["code_challenge_methods_supported"]==nil||meta["registration_endpoint"]!=Origin+"/register"||meta["authorization_response_iss_parameter_supported"]!=true{t.Fatal("incomplete metadata")}
 callback:="https://chatgpt.com/connector_platform_oauth_redirect"
 registered:=map[string]any{"redirect_uris":[]string{callback},"token_endpoint_auth_method":"none","client_name":"ChatGPT"}
 registerJSON,_:=json.Marshal(registered)
 res,err:=client.Post(server.URL+"/register","application/json",strings.NewReader(string(registerJSON)));if err!=nil{t.Fatal(err)}
 var clientInfo map[string]any
 if err:=json.NewDecoder(res.Body).Decode(&clientInfo);err!=nil{t.Fatal(err)};res.Body.Close()
 if res.StatusCode!=201{t.Fatalf("register: %d %+v",res.StatusCode,clientInfo)}
 id:=clientInfo["client_id"].(string)
 restored,err:=New(owner,s.file);if err!=nil{t.Fatal(err)}
 if _,ok:=restored.clients[id];!ok{t.Fatal("client registration not persisted")}
 verifier:=strings.Repeat("v",43)
 hash:=sha256.Sum256([]byte(verifier))
 q:=url.Values{"client_id":{id},"redirect_uri":{callback},"response_type":{"code"},"code_challenge":{base64.RawURLEncoding.EncodeToString(hash[:])},"code_challenge_method":{"S256"},"state":{"my-state"},"resource":{Resource},"scope":{Scope}}
 req,err:=client.Get(server.URL+"/authorize?"+q.Encode());if err!=nil{t.Fatal(err)}
 page,_:=io.ReadAll(req.Body);req.Body.Close()
 if req.StatusCode!=200{t.Fatalf("authorize GET: %d",req.StatusCode)}
 marker:="name=\"request_id\" value=\""
 start:=strings.Index(string(page),marker)
 if start<0{t.Fatal("authorization page missing request ID")}
 value:=string(page)[start+len(marker):]
 requestID:=strings.SplitN(value,"\"",2)[0]
 login:=url.Values{"request_id":{requestID},"owner_token":{owner}}
 res,err=client.PostForm(server.URL+"/authorize",login);if err!=nil{t.Fatal(err)}
 content,_:=io.ReadAll(res.Body);res.Body.Close()
 if res.StatusCode!=200{t.Fatalf("login status %d",res.StatusCode)}
 extractLink:=func(data []byte)string{
  t.Helper()
  marker:="href='"
  index:=strings.Index(string(data),marker)
  if index<0{t.Fatal("success page is missing direct ChatGPT link")}
  return html.UnescapeString(strings.SplitN(string(data)[index+len(marker):],"'",2)[0])
 }
 redirectURL:=extractLink(content)
 location,err:=url.Parse(redirectURL);if err!=nil{t.Fatal(err)}
 if location.Scheme!="https"||location.Host!="chatgpt.com"||location.Query().Get("state")!="my-state"||location.Query().Get("iss")!=Origin{t.Fatalf("bad redirect %s",location)}
 res,err=client.PostForm(server.URL+"/authorize",login);if err!=nil{t.Fatal(err)}
 secondContent,_:=io.ReadAll(res.Body);res.Body.Close()
 if res.StatusCode!=200||extractLink(secondContent)!=redirectURL{t.Fatal("repeat successful authorization lost its callback URL")}
 altered:=url.Values{"request_id":{requestID},"owner_token":{"wrong"}}
 res,err=client.PostForm(server.URL+"/authorize",altered);if err!=nil{t.Fatal(err)}
 res.Body.Close()
 if res.StatusCode!=401{t.Fatal("completed authorization bypassed owner credential")}
 code:=location.Query().Get("code")
 body:=url.Values{"grant_type":{"authorization_code"},"code":{code},"client_id":{id},"redirect_uri":{callback},"code_verifier":{verifier},"resource":{Resource}}
 response,err:=client.PostForm(server.URL+"/token",body);if err!=nil{t.Fatal(err)}
 var issued map[string]any
 if err:=json.NewDecoder(response.Body).Decode(&issued);err!=nil{t.Fatal(err)}
 response.Body.Close()
 if response.StatusCode!=200{t.Fatalf("token failed %+v",issued)}
 access:=issued["access_token"].(string)
 if !s.ValidAccess(access)||!restored.ValidAccess(access){t.Fatal("valid access token rejected")}
 if s.ValidAccess(issued["refresh_token"].(string)){t.Fatal("refresh token accepted as access")}
 response,err=client.PostForm(server.URL+"/token",body);if err!=nil{t.Fatal(err)}
 response.Body.Close()
 if response.StatusCode!=400{t.Fatal("authorization code replay accepted")}
 refresh:=url.Values{"grant_type":{"refresh_token"},"client_id":{id},"refresh_token":{issued["refresh_token"].(string)},"resource":{Resource}}
 response,err=client.PostForm(server.URL+"/token",refresh);if err!=nil{t.Fatal(err)}
 var refreshed map[string]any
 _=json.NewDecoder(response.Body).Decode(&refreshed);response.Body.Close()
 if response.StatusCode!=200||!s.ValidAccess(refreshed["access_token"].(string)){t.Fatalf("refresh failed %+v",refreshed)}
}
func TestOAuthRejectsUnknownRedirectAndWrongPassword(t *testing.T){
 s,err:=New(strings.Repeat("x",48),filepath.Join(t.TempDir(),"clients.json"));if err!=nil{t.Fatal(err)}
 mux:=http.NewServeMux();s.InstallRoutes(mux)
 server:=httptest.NewServer(mux);defer server.Close()
 bad:=`{"redirect_uris":["http://external.example/callback"],"token_endpoint_auth_method":"none"}`
 res,err:=http.Post(server.URL+"/register","application/json",strings.NewReader(bad));if err!=nil{t.Fatal(err)}
 res.Body.Close()
 if res.StatusCode!=400{t.Fatal("accepted non-TLS remote redirect")}
 if s.ValidAccess("garbage"){t.Fatal("accepted random token")}
}
