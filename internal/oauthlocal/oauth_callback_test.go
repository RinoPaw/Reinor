package oauthlocal

import (
 "crypto/sha256"
 "encoding/base64"
 "encoding/json"
 "io"
 "net/http"
 "net/http/httptest"
 "net/url"
 "path/filepath"
 "strings"
 "testing"
)

func TestGenericMCPRedirectURIs(t *testing.T) {
 valid:=[]string{
  "https://oauth-redirect.googleusercontent.com/r/user_bound_custom-mcp-105635764049000022585-xuhuaai_top",
  "https://chatgpt.com/connector_platform_oauth_redirect",
  "https://another-mcp-client.example/oauth/callback?tenant=abc",
  "https://localhost:8443/callback",
  "http://127.0.0.1:43122/callback",
  "http://[::1]:43122/callback",
  "http://localhost:43122/callback",
 }
 invalid:=[]string{
  "", "https://", "http://example.org/callback",
  "https://example.org/callback#frag",
  "https://alice@example.org/callback",
  "https://*.example.org/callback",
  "file:///tmp/callback", "javascript:alert(1)",
  "https://example.org/callback\nX-Injected: true",
  "http://192.168.1.50/callback",
 }
 for _,raw:=range valid {if !validCallback(raw){t.Errorf("valid OAuth callback denied: %s",raw)}}
 for _,raw:=range invalid {if validCallback(raw){t.Errorf("unsafe OAuth callback accepted: %s",raw)}}
}

func TestGeminiDynamicRegistrationAndExactRedirect(t *testing.T){
 s,err:=New(strings.Repeat("x",64),filepath.Join(t.TempDir(),"oauth-clients.json"))
 if err!=nil{t.Fatal(err)}
 mux:=http.NewServeMux();s.InstallRoutes(mux)
 ts:=httptest.NewServer(mux);defer ts.Close()
 client:=ts.Client()
 client.CheckRedirect=func(_ *http.Request,_ []*http.Request) error {return http.ErrUseLastResponse}
 redirect:="https://oauth-redirect.googleusercontent.com/r/user_bound_custom-mcp-105635764049000022585-xuhuaai_top"
 registration:=map[string]any{"redirect_uris":[]string{redirect},"client_name":"Gemini <script>alert(1)</script>","token_endpoint_auth_method":"none"}
 encoded,err:=json.Marshal(registration);if err!=nil{t.Fatal(err)}
 res,err:=client.Post(ts.URL+"/register","application/json",strings.NewReader(string(encoded)));if err!=nil{t.Fatal(err)}
 defer res.Body.Close()
 var created map[string]any
 if err:=json.NewDecoder(res.Body).Decode(&created);err!=nil{t.Fatal(err)}
 if res.StatusCode!=http.StatusCreated {t.Fatalf("Gemini DCR rejected: status=%d payload=%v",res.StatusCode,created)}
 id,ok:=created["client_id"].(string)
 if !ok||id==""{t.Fatalf("missing public client ID: %v",created)}
 if created["token_endpoint_auth_method"]!="none"{t.Fatalf("wrong client method: %v",created)}
 challenge:=sha256.Sum256([]byte(strings.Repeat("v",43)))
 q:=url.Values{"client_id":{id},"redirect_uri":{redirect+"/wrong"},"response_type":{"code"},"code_challenge":{base64.RawURLEncoding.EncodeToString(challenge[:])},"code_challenge_method":{"S256"},"resource":{Resource}}
 resBad,err:=client.Get(ts.URL+"/authorize?"+q.Encode());if err!=nil{t.Fatal(err)}
 resBad.Body.Close()
 if resBad.StatusCode!=http.StatusBadRequest{t.Fatalf("unregistered callback allowed: %d",resBad.StatusCode)}
 q.Set("redirect_uri",redirect)
 resGood,err:=client.Get(ts.URL+"/authorize?"+q.Encode());if err!=nil{t.Fatal(err)}
 defer resGood.Body.Close()
 page,err:=io.ReadAll(resGood.Body);if err!=nil{t.Fatal(err)}
 if resGood.StatusCode!=http.StatusOK {t.Fatalf("registered callback rejected: %d",resGood.StatusCode)}
 htmlPage:=string(page)
 if !strings.Contains(htmlPage,redirect){t.Fatal("authorization screen must show the full redirect URI")}
 if !strings.Contains(htmlPage,"Gemini &lt;script&gt;alert(1)&lt;/script&gt;"){t.Fatal("untrusted client name not HTML-escaped")}
 if strings.Contains(htmlPage,"<script>alert(1)</script>"){t.Fatal("untrusted DCR client name injected markup")}
}
