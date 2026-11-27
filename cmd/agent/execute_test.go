package main

import (
	"context"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/RinoPaw/Reinor/internal/wire"
)

func TestShellExecutableForOS(t *testing.T) {
	for _, tc := range []struct{ goos, expected string }{
		{"windows", "powershell.exe"},
		{"android", "/system/bin/sh"},
		{"linux", "/bin/sh"},
		{"darwin", "/bin/sh"},
	} {
		if got := shellExecutableForOS(tc.goos); got != tc.expected {
			t.Errorf("shell for %s: got %s, want %s", tc.goos, got, tc.expected)
		}
	}
}

func TestAgentShellCommandPlatform(t *testing.T) {
	cmd := agentShellCommand(context.Background(), "echo reinor-shell")
	if runtime.GOOS == "windows" {
		if !strings.HasSuffix(strings.ToLower(cmd.Path), "powershell.exe") {
			t.Fatalf("expected PowerShell on Windows: %s", cmd.Path)
		}
	} else if cmd.Path != shellExecutableForOS(runtime.GOOS) {
		t.Fatalf("expected native shell on %s: got %s", runtime.GOOS, cmd.Path)
	}
}

func TestAgentExecuteNativeShell(t *testing.T) {
	got := execute(wire.Envelope{Command: "echo reinor-native-shell", TimeoutMS: 5000})
	if got.ExitCode != 0 || got.Error != "" || !strings.Contains(string(got.Stdout), "reinor-native-shell") {
		t.Fatalf("unexpected result: exit=%d error=%q stdout=%q stderr=%q", got.ExitCode, got.Error, got.Stdout, got.Stderr)
	}
}

func TestAgentExecuteInvalidCommand(t *testing.T) {
	for _, c := range []string{"", strings.Repeat("x", 4097)} {
		got := execute(wire.Envelope{Command: c, TimeoutMS: 1000})
		if got.Error != "invalid command" {
			t.Fatalf("expected invalid command rejection for length %d, got %q", len(c), got.Error)
		}
	}
}

func TestAgentExecuteExitCode(t *testing.T) {
	got := execute(wire.Envelope{Command: "exit 7", TimeoutMS: 5000})
	if got.ExitCode != 7 {
		t.Fatalf("expected exit 7, got %d (error=%q, stderr=%q)", got.ExitCode, got.Error, got.Stderr)
	}
}

func TestAgentExecuteDeadlineUnix(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("uses POSIX sleep")
	}
	started := time.Now()
	got := execute(wire.Envelope{Command: "sleep 2", TimeoutMS: 1000})
	if got.Error != "execution deadline exceeded" {
		t.Fatalf("deadline not enforced: exit=%d error=%q", got.ExitCode, got.Error)
	}
	if time.Since(started) > 4*time.Second {
		t.Fatal("deadline took too long")
	}
}

func TestAsyncTaskExecutionBeyondLegacyDeadline(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("POSIX sleep regression runs on Linux CI")
	}
	got := execute(wire.Envelope{Type: "execute_task", Command: "sleep 21; echo finished", TimeoutMS: 30000})
	if got.ExitCode != 0 || got.Error != "" || !strings.Contains(string(got.Stdout), "finished") {
		t.Fatalf("long task did not finish: %+v", got)
	}
}

func TestAgentTaskLiveOutputAndCancel(t *testing.T) {
	command := "echo started; exec sleep 30"
	if runtime.GOOS == "windows" {
		command = "Write-Output started; Start-Sleep -Seconds 30"
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	progress := make(chan wire.Envelope, 8)
	done := make(chan wire.Envelope, 1)
	go func() {
		done <- executeWithProgress(ctx, wire.Envelope{Type: "execute_task", TaskID: "test", Command: command, TimeoutMS: 30000}, func(v wire.Envelope) error { progress <- v; return nil })
	}()
	timer := time.NewTimer(10 * time.Second)
	defer timer.Stop()
	for {
		select {
		case v := <-progress:
			if v.Type != "task_progress" || v.TaskID != "test" {
				t.Fatalf("unexpected progress %+v", v)
			}
			if !strings.Contains(string(v.Stdout), "started") {
				continue
			}
			cancel()
			select {
			case result := <-done:
				if result.Error != "task cancelled" || !strings.Contains(string(result.Stdout), "started") {
					t.Fatalf("unexpected result %+v", result)
				}
				return
			case <-timer.C:
				t.Fatal("cancellation did not stop direct process")
			}
		case result := <-done:
			t.Fatalf("finished before progress: %+v", result)
		case <-timer.C:
			t.Fatal("live output not received")
		}
	}
}
