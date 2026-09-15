package acp

import (
	"context"
	"encoding/json"
	"io"
	"strings"
	"sync"
	"testing"
	"time"
)

// StreamStep paces the fake agent's output so a person can watch it stream.
var StreamStep = 150 * time.Millisecond

func init() {
	if testing.Testing() {
		StreamStep = 0
	}
}

// runFake runs the fake agent in-process over pipes, through the same ACP
// client code as a real agent. It is for demos and tests, never a fallback.
func runFake(ctx context.Context, cfg Config, prompt string, emit Handler) (string, error) {
	toAgent, fromClient := io.Pipe()
	toClient, fromAgent := io.Pipe()
	agentDone := make(chan struct{})
	go func() {
		defer close(agentDone)
		ServeFake(toAgent, fromAgent)
		_ = fromAgent.Close()
	}()
	s := newSession(cfg, emit)
	out, err := s.drive(ctx, toClient, fromClient, prompt, nil)
	_ = fromClient.Close()
	_ = toClient.Close()
	<-agentDone
	return out, err
}

// fakeOptions tune the fake agent for tests.
var fakeOptions struct {
	noBypass bool // offer no permission-skipping mode, so it asks for approval
}

// ServeFake is an ACP agent that pretends to work on the prompt: it plans,
// calls one tool (asking permission unless told to skip prompts), and
// replies. It serves one connection until r ends.
func ServeFake(r io.Reader, w io.Writer) {
	f := &fakeAgent{cancel: make(chan struct{})}
	f.conn = newConn(w, f.handle)
	<-f.conn.listen(r).done
}

type fakeAgent struct {
	conn       *conn
	mu         sync.Mutex
	bypass     bool
	cancelOnce sync.Once
	cancel     chan struct{}
}

func (f *fakeAgent) handle(method string, params json.RawMessage) (any, *RPCError) {
	switch method {
	case "initialize":
		return map[string]any{
			"protocolVersion":   protocolVersion,
			"agentInfo":         map[string]string{"name": "buildbee-fake-agent", "version": "1"},
			"agentCapabilities": map[string]any{},
			"authMethods":       []any{},
		}, nil
	case "session/new":
		res := map[string]any{"sessionId": "fake-session"}
		if !fakeOptions.noBypass {
			res["modes"] = map[string]any{"currentModeId": "default", "availableModes": []map[string]string{
				{"id": "default", "name": "Ask"}, {"id": "bypassPermissions", "name": "Bypass permissions"}}}
		}
		return res, nil
	case "session/set_mode":
		var p struct {
			ModeID string `json:"modeId"`
		}
		_ = json.Unmarshal(params, &p)
		f.mu.Lock()
		f.bypass = p.ModeID == "bypassPermissions"
		f.mu.Unlock()
		return map[string]any{}, nil
	case "session/cancel":
		f.cancelOnce.Do(func() { close(f.cancel) })
		return nil, nil
	case "session/prompt":
		var p struct {
			Prompt []struct {
				Text string `json:"text"`
			} `json:"prompt"`
		}
		_ = json.Unmarshal(params, &p)
		text := ""
		if len(p.Prompt) > 0 {
			text = p.Prompt[0].Text
		}
		return f.work(text), nil
	default:
		return nil, &RPCError{Code: codeMethodNotFound, Message: method + " not supported by the fake agent"}
	}
}

func (f *fakeAgent) work(prompt string) any {
	task := ""
	for _, line := range strings.Split(prompt, "\n") {
		if t, ok := strings.CutPrefix(line, "Task: "); ok {
			task = t
			break
		}
	}
	update := func(u map[string]any) bool {
		select {
		case <-f.cancel:
			return false
		case <-time.After(StreamStep):
		}
		_ = f.conn.notify("session/update", map[string]any{"sessionId": "fake-session", "update": u})
		return true
	}
	chunk := func(text string) map[string]any {
		return map[string]any{"sessionUpdate": "agent_message_chunk", "content": map[string]string{"type": "text", "text": text}}
	}
	cancelled := map[string]string{"stopReason": "cancelled"}
	steps := []map[string]any{
		{"sessionUpdate": "agent_thought_chunk", "content": map[string]string{"type": "text", "text": "The fake agent does no real work."}},
		chunk("Plan: "), chunk("read the Task, "), chunk("then report.\n"),
		{"sessionUpdate": "plan", "entries": []map[string]string{
			{"content": "Read the Task", "priority": "high", "status": "in_progress"},
			{"content": "Report", "priority": "medium", "status": "pending"}}},
		{"sessionUpdate": "tool_call", "toolCallId": "call-1", "title": "Read the Task", "kind": "read",
			"status": "pending", "rawInput": map[string]string{"task": task}},
	}
	for _, u := range steps {
		if !update(u) {
			return cancelled
		}
	}
	f.mu.Lock()
	bypass := f.bypass
	f.mu.Unlock()
	status, result := "completed", "Task context loaded"
	if !bypass {
		var answer struct {
			Outcome struct {
				Outcome  string `json:"outcome"`
				OptionID string `json:"optionId"`
			} `json:"outcome"`
		}
		err := f.conn.call(context.Background(), "session/request_permission", map[string]any{
			"sessionId": "fake-session",
			"toolCall":  map[string]any{"toolCallId": "call-1", "title": "Read the Task"},
			"options": []map[string]string{
				{"optionId": "no", "name": "Reject", "kind": "reject_once"},
				{"optionId": "yes", "name": "Allow", "kind": "allow_once"},
			},
		}, &answer)
		if err != nil || answer.Outcome.OptionID != "yes" {
			status, result = "failed", "permission denied"
		}
	}
	for _, u := range []map[string]any{
		{"sessionUpdate": "tool_call_update", "toolCallId": "call-1", "status": status,
			"content": []map[string]any{{"type": "content", "content": map[string]string{"type": "text", "text": result}}}},
		{"sessionUpdate": "usage_update", "used": 1200, "size": 200000},
		chunk("Done.\n"),
	} {
		if !update(u) {
			return cancelled
		}
	}
	return map[string]string{"stopReason": "end_turn"}
}
