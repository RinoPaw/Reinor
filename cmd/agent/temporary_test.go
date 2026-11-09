package main

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/RinoPaw/Reinor/internal/wire"
	"github.com/coder/websocket"
)

func TestTemporaryJoinDoesNotPersistOrReconnect(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("LOCALAPPDATA", dir)
	const device = "temp-friend-mysql"
	const requestID = "join_test"
	ticket := "join_secret_" + strings.Repeat("x", 48)
	var sessions atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		auth := r.Header.Get("Authorization")
		c, err := websocket.Accept(w,r,nil)
		if err != nil { return }
		defer c.CloseNow()
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		typ,data,err := c.Read(ctx)
		if err != nil || typ != websocket.MessageBinary { return }
		hello,err := wire.Unmarshal(data)
		if err != nil || hello.DeviceID != device || hello.Type != "hello" { return }
		if auth == "Bearer join-request" {
			if hello.Command != "temporary" { return }
			for _, m := range []wire.Envelope{
				{Type:"join_pending",TaskID:requestID,Command:ticket},
				{Type:"join_approved",TaskID:requestID,Command:ticket},
			}{
				b,_:=wire.Marshal(m)
				if err:=c.Write(ctx,websocket.MessageBinary,b);err!=nil{return}
			}
			return
		}
		if auth == "Bearer "+ticket {
			sessions.Add(1)
			// Simulate the end of the approved one-time connection.
			_ = c.Close(websocket.StatusNormalClosure,"finished")
		}
	}))
	defer srv.Close()
	done:=make(chan error,1)
	go func(){done<-joinTemporary(strings.Replace(srv.URL,"http://","ws://",1),device)}()
	select{
	case <-done:
	case <-time.After(4*time.Second):
		t.Fatal("temporary client did not exit after disconnection")
	}
	if got:=sessions.Load();got!=1 {t.Fatalf("session dial count = %d, want one",got)}
	path:=filepath.Join(dir,"RinoMesh","joined-"+device+".json")
	if _,err:=os.Stat(path);!os.IsNotExist(err){t.Fatalf("temporary credentials persisted: %v",err)}
}
