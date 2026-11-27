package main

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"log"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/RinoPaw/Reinor/internal/capability"
	"github.com/RinoPaw/Reinor/internal/operation"
	"github.com/RinoPaw/Reinor/internal/task"
	"github.com/RinoPaw/Reinor/internal/wire"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

type submitTaskArgs struct {
	execArgs
	TimeoutSeconds int `json:"timeout_seconds,omitempty" jsonschema:"Execution deadline in seconds (1-3600; default 300)"`
}
type taskIDArgs struct {
	TaskID string `json:"task_id"`
}
type listTaskArgs struct {
	DeviceID string `json:"device_id,omitempty"`
	Limit    int    `json:"limit,omitempty" jsonschema:"Number of recent tasks (1-100; default 20)"`
}

func submitTask(h *hub, s *task.Store, args submitTaskArgs) (task.Record, error) {
	cmd, err := decodeExecuteCommand(args.execArgs)
	if err != nil {
		return task.Record{}, err
	}
	if len(cmd) == 0 || len(cmd) > 4096 || !utf8.ValidString(cmd) || strings.ContainsRune(cmd, 0) {
		return task.Record{}, errors.New("invalid command")
	}
	if !validDeviceID.MatchString(args.DeviceID) {
		return task.Record{}, errors.New("invalid device_id")
	}
	seconds := args.TimeoutSeconds
	if seconds == 0 {
		seconds = 300
	}
	if seconds < 1 || seconds > 3600 {
		return task.Record{}, errors.New("timeout_seconds must be 1-3600")
	}
	h.mu.Lock()
	a := h.agents[args.DeviceID]
	supported := false
	if a != nil {
		c, ok := capability.Find(a.capabilities, capability.AsyncTask)
		supported = ok && c.Supported && c.Available && !a.revoked && (a.temporaryID == "" || time.Now().Before(a.temporaryExpiresAt))
	}
	h.mu.Unlock()
	if a == nil {
		return task.Record{}, errors.New("device offline")
	}
	if !supported {
		return task.Record{}, errors.New("device does not support asynchronous tasks or its session ended")
	}
	var id [16]byte
	if _, err = rand.Read(id[:]); err != nil {
		return task.Record{}, err
	}
	r := task.Record{ID: hex.EncodeToString(id[:]), DeviceID: args.DeviceID, TimeoutSeconds: seconds}
	if err = s.Begin(r); err != nil {
		return task.Record{}, err
	}
	r, _ = s.Get(r.ID)
	go runTask(h, s, a, r, cmd)
	return r, nil
}
func runTask(h *hub, s *task.Store, a *agent, r task.Record, cmd string) {
	update := func(status task.Status, exit *int32, out, stderr, message string) bool {
		if err := s.Update(r.ID, status, exit, out, stderr, message); err != nil {
			log.Printf("persist task %s: %v", r.ID, err)
			return false
		}
		return true
	}
	if !update(task.Dispatched, nil, "", "", "") {
		return
	}
	reply, err := h.dispatchTarget(context.Background(), r.DeviceID, wire.Envelope{Type: "execute_task", Command: cmd, TaskID: r.ID}, time.Duration(r.TimeoutSeconds)*time.Second, a)
	if err != nil {
		update(task.Unknown, nil, "", "", err.Error())
		return
	}
	if reply.Error == "connection lost; execution status unknown" {
		update(task.Unknown, nil, "", "", reply.Error)
		return
	}
	if reply.Type != "result" {
		update(task.Unknown, nil, "", "", "unexpected task response")
		return
	}
	status := task.Succeeded
	if reply.Error == "execution deadline exceeded" {
		status = task.TimedOut
	} else if reply.Error != "" || reply.ExitCode != 0 {
		status = task.Failed
	}
	update(status, &reply.ExitCode, string(reply.Stdout), string(reply.Stderr), reply.Error)
}
func registerTaskTools(m *mcp.Server, h *hub, s *task.Store, reg *operation.Registry) {
	operation.Register(reg, m, operation.Spec{Name: "task.submit", Effect: operation.Action, Version: "1", Requires: []string{"online", capability.AsyncTask}}, &mcp.Tool{Name: "submit_task", Description: "Start a command on an online task-capable device and return a persistent task ID. Does not retry after disconnect. Runs with the Agent account's privileges."}, func(_ context.Context, _ *mcp.CallToolRequest, args submitTaskArgs) (*mcp.CallToolResult, any, error) {
		r, err := submitTask(h, s, args)
		if err != nil {
			return result(map[string]string{"error": err.Error()}, true), nil, nil
		}
		return result(r, false), nil, nil
	})
	operation.Register(reg, m, operation.Spec{Name: "task.get", Effect: operation.Read, Version: "1"}, &mcp.Tool{Name: "get_task", Description: "Get task status and final bounded stdout/stderr by task ID."}, func(_ context.Context, _ *mcp.CallToolRequest, args taskIDArgs) (*mcp.CallToolResult, any, error) {
		r, ok := s.Get(args.TaskID)
		if !ok {
			return result(map[string]string{"error": "task not found"}, true), nil, nil
		}
		return result(r, false), nil, nil
	})
	operation.Register(reg, m, operation.Spec{Name: "task.list", Effect: operation.Read, Version: "1"}, &mcp.Tool{Name: "list_tasks", Description: "List recent task metadata, optionally filtered by device."}, func(_ context.Context, _ *mcp.CallToolRequest, args listTaskArgs) (*mcp.CallToolResult, any, error) {
		if args.Limit == 0 {
			args.Limit = 20
		}
		if args.Limit < 1 || args.Limit > 100 {
			return result(map[string]string{"error": "limit must be 1-100"}, true), nil, nil
		}
		return result(s.List(args.DeviceID, args.Limit), false), nil, nil
	})
}
