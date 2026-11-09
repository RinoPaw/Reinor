package oauthlocal

import (
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

func TestGeminiConsentImmediateNavigationPreservesState(t *testing.T) {
 owner := strings.Repeat("x",64)
 s,err:=New(owner,filepath.Join(t.TempDir(),"clients.json"))
 if err!=nil { t.Fatal(err) }
 mux:=http.NewServeMux()
 s.InstallRoutes(mux)
 server:=httptest.NewServer(mux)
 defer server.Close()
 client:=server.Client()
 client.CheckRedirect=func(_ *http.Request,_ []*http.Request)error{return http.ErrUseLastResponse}
 callback:="https://oauth-redirect.googleusercontent.com/r/user_bound_custom-mcp-105635764049000022585-xuhuaai_top"
 reg,_:=json.Marshal(map[string]any{"redirect_uris":[]string{callback},"client_name":"Gemini","token_endpoint_auth_method":"none"})
 response,err:=client.Post(server.URL+"/register","application/json",strings.NewReader(string(reg)))
 if err!=nil { t.Fatal(err) }
 var details map[string]any
 err=json.NewDecoder(response.Body).Decode(&details)
 response.Body.Close()
 if err!=nil||response.StatusCode!=201 {t.Fatalf("registration: %v / %v",response.StatusCode,err)}
 id:=details["client_id"].(string)
 verifier:=strings.Repeat("v",43)
 sum:=sha256.Sum256([]byte(verifier))
 state:=strings.Repeat("long-state_for-Gemini-0123456789",50)
 query:=url.Values{
  "client_id":{id},"redirect_uri":{callback},"response_type":{"code"},
  "code_challenge":{base64.RawURLEncoding.EncodeToString(sum[:])},
  "code_challenge_method":{"S256"},"state":{state},"resource":{Resource},"scope":{Scope},
 }
 response,err=client.Get(server.URL+"/authorize?"+query.Encode())
 if err!=nil {t.Fatal(err)}
 body,_:=io.ReadAll(response.Body)
 response.Body.Close()
 if response.StatusCode!=http.StatusOK {t.Fatalf("authorize GET: %d",response.StatusCode)}
 marker:="name=\"request_id\" value=\""
 i:=strings.Index(string(body),marker)
 if i<0 {t.Fatal("missing request id")}
 reqID:=strings.SplitN(string(body)[i+len(marker):],"\"",2)[0]
 response,err=client.PostForm(server.URL+"/authorize",url.Values{"request_id":{reqID},"owner_token":{owner}})
 if err!=nil{t.Fatal(err)}
 body,_=io.ReadAll(response.Body)
 response.Body.Close()
 if response.StatusCode!=200{t.Fatalf("authorize POST: %d",response.StatusCode)}
 page:=string(body)
 if !strings.Contains(page,"http-equiv='refresh' content='0;url=") {t.Fatal("OAuth success page must navigate immediately")}
 start:=strings.Index(page,"href='")
 if start<0{t.Fatal("manual recovery callback link missing")}
 end:=strings.SplitN(page[start+6:],"'",2)[0]
 parsed,err:=url.Parse(html.UnescapeString(end))
 if err!=nil {t.Fatal(err)}
 params:=parsed.Query()
 if parsed.Scheme!="https"||parsed.Host!="oauth-redirect.googleusercontent.com"||parsed.Path!="/r/user_bound_custom-mcp-105635764049000022585-xuhuaai_top" {
  t.Fatalf("incorrect redirect target: %s %s",parsed.Host,parsed.Path)
 }
 if params.Get("code")==""||params.Get("state")!=state||params.Get("iss")!=Origin {
  t.Fatal("callback code, issuer or long state missing or changed")
 }
 // Repeating an already approved POST keeps the same callback.
 response,err=client.PostForm(server.URL+"/authorize",url.Values{"request_id":{reqID},"owner_token":{owner}})
 if err!=nil{t.Fatal(err)}
 repeated,_:=io.ReadAll(response.Body)
 response.Body.Close()
 if !strings.Contains(string(repeated),html.EscapeString(parsed.String())){t.Fatal("repeat consent lost callback")}
}
