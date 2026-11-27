package main

import (
	"bytes"
	"context"
	"errors"
	"flag"
	"fmt"
	"log"
	"math/rand/v2"
	"net/http"
	"os"
	"os/exec"
	"runtime"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/RinoPaw/Reinor/internal/capability"
	"github.com/RinoPaw/Reinor/internal/desktop"
	"github.com/RinoPaw/Reinor/internal/wire"
	"github.com/coder/websocket"
)

func getToken(path string) string {
	data, err := os.ReadFile(path)
	if err != nil {
		log.Fatalf("token file: %v", err)
	}
	v := strings.TrimSpace(string(data))
	if len(v) < 32 {
		log.Fatal("token needs at least 32 characters")
	}
	return v
}

type limitedBuffer struct {
	mu  sync.Mutex
	b   bytes.Buffer
	max int
}

func (l *limitedBuffer) Write(p []byte) (int, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	n := len(p)
	space := l.max - l.b.Len()
	if space > 0 {
		if space > len(p) {
			space = len(p)
		}
		_, _ = l.b.Write(p[:space])
	}
	return n, nil
}
func (l *limitedBuffer) snapshot() []byte {
	l.mu.Lock()
	defer l.mu.Unlock()
	return bytes.Clone(l.b.Bytes())
}
func exitCode(err error) int32 {
	if err == nil {
		return 0
	}
	var e *exec.ExitError
	if errors.As(err, &e) {
		return int32(e.ExitCode())
	}
	return -1
}

// agentShellCommand selects the native shell for the enrolled device OS.
// Gateway authentication and command deadlines are enforced by the caller.
// shellExecutableForOS chooses an OS-native shell. Termux has its own
// prefix, so /system/bin/sh is a stable absolute path for Android.
func shellExecutableForOS(goos string) string {
	switch goos {
	case "windows":
		return "powershell.exe"
	case "android":
		return "/system/bin/sh"
	default:
		return "/bin/sh"
	}
}

func agentShellCommand(ctx context.Context, command string) *exec.Cmd {
	shell := shellExecutableForOS(runtime.GOOS)
	if runtime.GOOS == "windows" {
		return exec.CommandContext(ctx, shell, "-NoProfile", "-NonInteractive", "-Command", command)
	}
	return exec.CommandContext(ctx, shell, "-c", command)
}

func execute(f wire.Envelope) wire.Envelope { return executeWithContext(context.Background(), f) }

func executeWithContext(parent context.Context, f wire.Envelope) wire.Envelope {
	return executeWithProgress(parent, f, nil)
}
func executeWithProgress(parent context.Context, f wire.Envelope, progress func(wire.Envelope) error) wire.Envelope {
	reply := wire.Envelope{Type: "result", TaskID: f.TaskID}
	if len(f.Command) == 0 || len(f.Command) > 4096 {
		reply.Error = "invalid command"
		return reply
	}
	ms := f.TimeoutMS
	maxMS := uint32(20000)
	if f.Type == "execute_task" {
		maxMS = 3600000
	}
	if ms < 1000 || ms > maxMS {
		ms = 20000
	}
	ctx, cancel := context.WithTimeout(parent, time.Duration(ms)*time.Millisecond)
	defer cancel()
	cmd := agentShellCommand(ctx, f.Command)
	cmd.WaitDelay = 2 * time.Second
	var killed atomic.Bool
	kill := cmd.Cancel
	cmd.Cancel = func() error {
		err := kill()
		if err == nil {
			killed.Store(true)
		}
		return err
	}
	var out, errout limitedBuffer
	out.max = 65536
	errout.max = 65536
	cmd.Stdout = &out
	cmd.Stderr = &errout
	err := cmd.Start()
	started := err == nil
	if started {
		if progress == nil || f.Type != "execute_task" {
			err = cmd.Wait()
		} else {
			done := make(chan struct{})
			stopped := make(chan struct{})
			go func() {
				defer close(stopped)
				tick := time.NewTicker(time.Second)
				defer tick.Stop()
				lastOut, lastErr := -1, -1
				for {
					stdout, stderr := out.snapshot(), errout.snapshot()
					if len(stdout) != lastOut || len(stderr) != lastErr {
						if progress(wire.Envelope{Type: "task_progress", TaskID: f.TaskID, Stdout: stdout, Stderr: stderr}) != nil {
							return
						}
						lastOut, lastErr = len(stdout), len(stderr)
					}
					select {
					case <-done:
						return
					case <-tick.C:
					}
				}
			}()
			err = cmd.Wait()
			close(done)
			<-stopped
		}
	}
	reply.ExitCode = exitCode(err)
	reply.Stdout = out.snapshot()
	reply.Stderr = errout.snapshot()
	if err != nil && errors.Is(ctx.Err(), context.Canceled) && f.Type == "execute_task" && (killed.Load() || !started) {
		reply.Error = "task cancelled"
	} else if errors.Is(ctx.Err(), context.DeadlineExceeded) && (f.Type != "execute_task" || err != nil) {
		reply.Error = "execution deadline exceeded"
	} else if err != nil && reply.ExitCode == -1 {
		reply.Error = fmt.Sprintf("launch failed: %v", err)
	}
	return reply
}
func capabilitySnapshot(taskID string) wire.Envelope {
	reply := wire.Envelope{Type: "capabilities_result", TaskID: taskID}
	payload, err := capability.Encode([]capability.Status{desktop.Capability(), {ID: capability.FileV2, Supported: true, Available: true}, {ID: capability.AsyncTask, Supported: true, Available: true}})
	if err != nil {
		reply.Error = err.Error()
		return reply
	}
	reply.Capabilities = payload
	return reply
}

func captureScreen(f wire.Envelope) wire.Envelope {
	reply := wire.Envelope{Type: "capture_result", TaskID: f.TaskID}
	status := desktop.Capability()
	if !status.Supported || !status.Available {
		reply.ErrorCode = status.Reason
		if reply.ErrorCode == "" {
			reply.ErrorCode = capability.Unsupported
		}
		reply.Error = fmt.Sprintf("%s is unavailable: %s", status.ID, reply.ErrorCode)
		return reply
	}
	shot, err := desktop.CaptureScreen(int(f.Width), int(f.Quality))
	if err != nil {
		reply.Error = err.Error()
		reply.ErrorCode = capability.CaptureFailed
		if errors.Is(err, desktop.ErrNoDesktopSession) {
			reply.ErrorCode = capability.NoDesktopSession
		}
		return reply
	}
	reply.Data = shot.Data
	reply.MIMEType = shot.MIMEType
	reply.Width = uint32(shot.Width)
	reply.Height = uint32(shot.Height)
	reply.OriginX = int32(shot.OriginX)
	reply.OriginY = int32(shot.OriginY)
	reply.PhysicalWidth = uint32(shot.PhysicalWidth)
	reply.PhysicalHeight = uint32(shot.PhysicalHeight)
	return reply
}

func session(parent context.Context, url, id, token string) error {
	header := http.Header{"Authorization": []string{"Bearer " + token}}
	ctx, stop := context.WithTimeout(parent, 15*time.Second)
	c, _, err := websocket.Dial(ctx, url, &websocket.DialOptions{HTTPHeader: header})
	stop()
	if err != nil {
		return err
	}
	defer c.CloseNow()
	c.SetReadLimit(wire.MaxFrame)
	capabilities, err := capability.Encode([]capability.Status{desktop.Capability(), {ID: capability.FileV2, Supported: true, Available: true}, {ID: capability.AsyncTask, Supported: true, Available: true}})
	if err != nil {
		return fmt.Errorf("collect device capabilities: %w", err)
	}
	hello, _ := wire.Marshal(wire.Envelope{Type: "hello", DeviceID: id, OS: runtime.GOOS, Capabilities: capabilities, Feature: "file-v1"})
	if err := c.Write(parent, websocket.MessageBinary, hello); err != nil {
		return err
	}
	log.Printf("connected to gateway device=%s", id)
	var writeMu sync.Mutex
	busy := make(chan struct{}, 1)
	streamBusy := make(chan struct{}, 1)
	var streamMu sync.Mutex
	var streamID string
	var streamCancel context.CancelFunc
	var taskMu sync.Mutex
	var activeTaskID string
	var activeTaskCancel context.CancelFunc
	files := newFileTransfers()
	defer files.closeAll()
	connection, closeConn := context.WithCancel(parent)
	defer closeConn()
	go func() {
		ticker := time.NewTicker(time.Minute)
		defer ticker.Stop()
		for {
			select {
			case <-connection.Done():
				return
			case <-ticker.C:
				files.reapExpired(time.Now())
			}
		}
	}()
	send := func(frame wire.Envelope) error {
		data, err := wire.Marshal(frame)
		if err != nil {
			return err
		}
		writeMu.Lock()
		defer writeMu.Unlock()
		ctx, cancel := context.WithTimeout(connection, 6*time.Second)
		defer cancel()
		return c.Write(ctx, websocket.MessageBinary, data)
	}
	for {
		typ, data, err := c.Read(connection)
		if err != nil {
			return err
		}
		if typ != websocket.MessageBinary {
			continue
		}
		f, err := wire.Unmarshal(data)
		if err != nil {
			return err
		}
		if f.Type == "cancel_task" {
			taskMu.Lock()
			if activeTaskID == f.TaskID && activeTaskCancel != nil {
				activeTaskCancel()
			}
			taskMu.Unlock()
			continue
		}
		if f.Type == "file_v2_cancel" {
			streamMu.Lock()
			if streamID == f.TransferID && streamCancel != nil {
				streamCancel()
			}
			streamMu.Unlock()
			continue
		}
		if f.Type == "file_v2_send" || f.Type == "file_v2_receive" {
			select {
			case streamBusy <- struct{}{}:
				streamCtx, stopStream := context.WithCancel(connection)
				streamMu.Lock()
				streamID = f.TransferID
				streamCancel = stopStream
				streamMu.Unlock()
				go func(f wire.Envelope) {
					defer func() {
						stopStream()
						streamMu.Lock()
						streamID = ""
						streamCancel = nil
						streamMu.Unlock()
						<-streamBusy
					}()
					reply := wire.Envelope{Type: "file_result", TaskID: f.TaskID}
					var err error
					if f.Type == "file_v2_receive" {
						err = receiveFileV2(streamCtx, url, f)
					} else {
						err = sendFileV2(streamCtx, url, f)
					}
					if err != nil {
						reply.Error = err.Error()
					}
					_ = send(reply)
				}(f)
			default:
				_ = send(wire.Envelope{Type: "file_result", TaskID: f.TaskID, Error: "file stream busy"})
			}
			continue
		}
		if f.Type != "execute" && f.Type != "execute_task" && f.Type != "capture_screen" && f.Type != "get_capabilities" && !strings.HasPrefix(f.Type, "file_") {
			continue
		}
		select {
		case busy <- struct{}{}:
			execCtx := connection
			stopTask := func() {}
			if f.Type == "execute_task" {
				execCtx, stopTask = context.WithCancel(connection)
				taskMu.Lock()
				activeTaskID = f.TaskID
				activeTaskCancel = stopTask
				taskMu.Unlock()
			}
			go func(f wire.Envelope, execCtx context.Context, stopTask context.CancelFunc) {
				defer func() {
					if f.Type == "execute_task" {
						stopTask()
						taskMu.Lock()
						activeTaskID = ""
						activeTaskCancel = nil
						taskMu.Unlock()
					}
					<-busy
				}()
				var reply wire.Envelope
				switch f.Type {
				case "capture_screen":
					reply = captureScreen(f)
				case "get_capabilities":
					reply = capabilitySnapshot(f.TaskID)
				default:
					if strings.HasPrefix(f.Type, "file_") {
						reply = files.handle(f)
					} else {
						reply = executeWithProgress(execCtx, f, send)
					}
				}
				if err := send(reply); err != nil {
					log.Printf("send result: %v", err)
					_ = c.CloseNow()
				}
			}(f, execCtx, stopTask)
		default:
			kind := "result"
			if strings.HasPrefix(f.Type, "file_") {
				kind = "file_result"
			}
			_ = send(wire.Envelope{Type: kind, TaskID: f.TaskID, Error: "device busy"})
		}
	}
}
func main() {
	if len(os.Args) > 1 && os.Args[1] == "update" {
		if err := updateMain(os.Args[2:]); err != nil {
			log.Fatal(err)
		}
		return
	}
	if len(os.Args) > 1 && os.Args[1] == "join" {
		if err := joinMain(os.Args[2:]); err != nil {
			log.Fatal(err)
		}
		return
	}
	url := flag.String("url", "wss://xuhuaai.top/agent/ws", "WSS gateway URL")
	device := flag.String("device", "rino-pc", "stable device identifier")
	tokenPath := flag.String("token-file", "", "path to agent bearer token")
	flag.Parse()
	if *tokenPath == "" {
		log.Fatal("-token-file required")
	}
	token := getToken(*tokenPath)
	backoff := time.Second
	for {
		started := time.Now()
		err := session(context.Background(), *url, *device, token)
		if time.Since(started) > time.Minute {
			backoff = time.Second
		}
		log.Printf("gateway disconnected: %v", err)
		delay := backoff + time.Duration(rand.Int64N(int64(backoff/2+1)))
		time.Sleep(delay)
		if backoff < 30*time.Second {
			backoff *= 2
			if backoff > 30*time.Second {
				backoff = 30 * time.Second
			}
		}
	}
}
