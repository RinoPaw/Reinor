package main

import (
 "context"
 "runtime"
 "strings"
 "testing"
 "time"

 "github.com/RinoPaw/Reinor/internal/wire"
)

func TestAgentShellCommandPlatform(t *testing.T) {
 cmd := agentShellCommand(context.Background(), "echo reinor-shell")
 if runtime.GOOS == "windows" {
  if !strings.HasSuffix(strings.ToLower(cmd.Path), "powershell.exe") {
   t.Fatalf("expected PowerShell on Windows: %s", cmd.Path)
  }
 } else if cmd.Path != "/bin/sh" {
  t.Fatalf("expected /bin/sh on Unix: %s", cmd.Path)
 }
}

func TestAgentExecuteNativeShell(t *testing.T) {
 got := execute(wire.Envelope{Command: "echo reinor-native-shell", TimeoutMS: 5000})
 if got.ExitCode != 0 || got.Error != "" || !strings.Contains(string(got.Stdout), "reinor-native-shell") {
  t.Fatalf("unexpected result: exit=%d error=%q stdout=%q stderr=%q",got.ExitCode,got.Error,got.Stdout,got.Stderr)
 }
}

func TestAgentExecuteInvalidCommand(t *testing.T) {
 for _, c := range []string{"", strings.Repeat("x", 4097)} {
  got := execute(wire.Envelope{Command: c, TimeoutMS: 1000})
  if got.Error != "invalid command" {
   t.Fatalf("expected invalid command rejection for length %d, got %q",len(c),got.Error)
  }
 }
}

func TestAgentExecuteExitCode(t *testing.T) {
 got := execute(wire.Envelope{Command: "exit 7", TimeoutMS: 5000})
 if got.ExitCode != 7 {
  t.Fatalf("expected exit 7, got %d (error=%q, stderr=%q)",got.ExitCode,got.Error,got.Stderr)
 }
}

func TestAgentExecuteDeadlineUnix(t *testing.T) {
 if runtime.GOOS == "windows" { t.Skip("uses POSIX sleep") }
 started := time.Now()
 got := execute(wire.Envelope{Command: "sleep 2", TimeoutMS: 1000})
 if got.Error != "execution deadline exceeded" {
  t.Fatalf("deadline not enforced: exit=%d error=%q",got.ExitCode,got.Error)
 }
 if time.Since(started)>4*time.Second {
  t.Fatal("deadline took too long")
 }
}
