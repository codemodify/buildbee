// Package acp runs a coding agent over the Agent Client Protocol.
//
// The agent is a subprocess speaking ACP (JSON-RPC 2.0 over stdio). A Run
// is one session: initialize, session/new in the Run's directory, one
// session/prompt, streaming session/update notifications as Events until
// the agent ends its turn. BuildBee runs agents unattended, so it asks the
// agent to skip permission prompts where it can and approves the rest.
//
// Agents use their own CLI's login on the machine they run on; BuildBee
// passes no credentials.
package acp

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"slices"
	"strings"
	"sync"
	"time"
)

// protocolVersion is the ACP major version BuildBee speaks.
const protocolVersion = 1

// authTimeout bounds an authenticate call, which should only reuse a login.
const authTimeout = 30 * time.Second

// Event is one piece of agent output, posted as a RunEvent.
type Event struct {
	Kind    string
	Payload map[string]any
}

// Handler receives Events in order. Returning an error stops the Run.
type Handler func(Event) error

// Launch is the command that starts each agent's ACP server. Claude Code
// and Codex speak ACP through adapters (npm @agentclientprotocol/claude-agent-acp
// and @zed-industries/codex-acp); the others have it built in.
var Launch = map[string][]string{
	"claude":   {"claude-agent-acp"},
	"codex":    {"codex-acp"},
	"grok":     {"grok", "agent", "--always-approve", "--no-leader", "stdio"},
	"opencode": {"opencode", "acp"},
	"goose":    {"goose", "acp"},
}

// installHint says how to get each agent's ACP command.
var installHint = map[string]string{
	"claude":   "npm install -g @agentclientprotocol/claude-agent-acp (it uses your Claude Code login)",
	"codex":    "npm install -g @zed-industries/codex-acp (it uses your Codex login)",
	"grok":     "install the Grok CLI and log in",
	"opencode": "install OpenCode and log in",
	"goose":    "install Goose and configure a provider",
}

// Config selects the agent and where it works.
type Config struct {
	Agent   string
	Command []string // overrides Launch[Agent]
	WorkDir string   // the session's working directory (absolute)
	// CancelGrace is how long a canceled session may take to wind down
	// before the agent is killed (default 10s).
	CancelGrace time.Duration
	// Steer delivers people's messages while the agent works (optional).
	Steer <-chan Steer
}

// Steer is a person's message to the working agent. It is sent as the next
// turn once the current one ends; Interrupt cancels the current turn first.
type Steer struct {
	By        string
	Text      string
	Interrupt bool
}

// ErrNoAgent means the agent's command is not installed on this machine.
var ErrNoAgent = errors.New("agent not installed")

// ErrNotLoggedIn means the agent needs its CLI logged in on the worker.
var ErrNotLoggedIn = errors.New("agent not logged in")

// Command returns the command line that starts agent, preferring
// overrides[agent].
func Command(agent string, overrides map[string][]string) ([]string, error) {
	argv := overrides[agent]
	if len(argv) == 0 {
		argv = Launch[agent]
	}
	if len(argv) == 0 {
		return nil, fmt.Errorf("%w: no launch command for agent %q", ErrNoAgent, agent)
	}
	if _, err := exec.LookPath(argv[0]); err != nil {
		hint := installHint[agent]
		if hint == "" || len(overrides[agent]) > 0 {
			hint = "install it or fix BUILDBEE_AGENT_" + strings.ToUpper(agent)
		}
		return nil, fmt.Errorf("%w: %s is not on PATH for agent %s; %s", ErrNoAgent, argv[0], agent, hint)
	}
	return argv, nil
}

// Installed lists the agents whose command is on PATH, sorted.
func Installed(overrides map[string][]string) []string {
	var out []string
	seen := map[string]bool{}
	for _, m := range []map[string][]string{Launch, overrides} {
		for agent := range m {
			if seen[agent] {
				continue
			}
			seen[agent] = true
			if _, err := Command(agent, overrides); err == nil {
				out = append(out, agent)
			}
		}
	}
	slices.Sort(out)
	return out
}

// Run starts the agent, sends prompt, streams Events to emit until the
// agent ends its turn, and returns the transcript. Canceling ctx cancels
// the turn, then kills the agent after CancelGrace.
func Run(ctx context.Context, cfg Config, prompt string, emit Handler) (string, error) {
	if cfg.CancelGrace <= 0 {
		cfg.CancelGrace = 10 * time.Second
	}
	if cfg.Agent == "fake" && len(cfg.Command) == 0 {
		return runFake(ctx, cfg, prompt, emit)
	}
	argv := cfg.Command
	if len(argv) == 0 {
		var err error
		if argv, err = Command(cfg.Agent, nil); err != nil {
			return "", err
		}
	}
	p, err := startProcess(argv, cfg.WorkDir)
	if err != nil {
		return "", fmt.Errorf("starting %s: %w", cfg.Agent, err)
	}
	defer p.stop()
	s := newSession(cfg, emit)
	stderrDone := make(chan struct{})
	go func() { defer close(stderrDone); s.forwardStderr(p.stderr) }()
	out, err := s.drive(ctx, p.stdout, p.stdin, prompt, func() {
		// When the agent died, let its last words reach the error.
		if err := ctx.Err(); err == nil {
			select {
			case <-stderrDone:
			case <-time.After(time.Second):
			}
		}
	})
	return out, err
}

// session is the client side of one ACP conversation.
type session struct {
	cfg  Config
	sink *sink
	id   string

	mu         sync.Mutex
	transcript strings.Builder
	stderrTail []string
}

func newSession(cfg Config, emit Handler) *session {
	return &session{cfg: cfg, sink: newSink(emit)}
}

// drive runs the conversation over r and w. peerGone, if set, is called
// before reporting that the agent went away.
func (s *session) drive(ctx context.Context, r io.Reader, w io.Writer, prompt string, peerGone func()) (string, error) {
	ctx, stop := context.WithCancelCause(ctx)
	defer stop(nil)
	s.sink.start(func(err error) { stop(err) })
	c := newConn(w, s.handle).listen(r)
	err := s.converse(ctx, c, prompt)
	if err != nil && errors.Is(err, errPeerGone) {
		if peerGone != nil {
			peerGone()
		}
		if tail := s.stderr(); tail != "" {
			err = fmt.Errorf("%w; its last output:\n%s", err, tail)
		}
	}
	if serr := s.sink.close(); serr != nil && err == nil {
		err = serr
	}
	return s.text(), err
}

func (s *session) converse(ctx context.Context, c *conn, prompt string) error {
	var init struct {
		ProtocolVersion int `json:"protocolVersion"`
		AgentInfo       *struct {
			Name    string `json:"name"`
			Version string `json:"version"`
		} `json:"agentInfo"`
		AuthMethods []struct {
			ID   string `json:"id"`
			Type string `json:"type"`
		} `json:"authMethods"`
	}
	if err := c.call(ctx, "initialize", map[string]any{
		"protocolVersion": protocolVersion,
		// The agent uses its own file and shell tools inside WorkDir.
		"clientCapabilities": map[string]any{"fs": map[string]bool{"readTextFile": false, "writeTextFile": false}, "terminal": false},
		"clientInfo":         map[string]string{"name": "buildbee", "version": "0"},
	}, &init); err != nil {
		return s.explain(ctx, err)
	}
	if init.ProtocolVersion != protocolVersion {
		return fmt.Errorf("agent %s speaks ACP version %d; BuildBee speaks %d", s.cfg.Agent, init.ProtocolVersion, protocolVersion)
	}
	agentName := s.cfg.Agent
	if init.AgentInfo != nil && init.AgentInfo.Name != "" {
		agentName = strings.TrimSpace(init.AgentInfo.Name + " " + init.AgentInfo.Version)
	}

	var sess struct {
		SessionID string `json:"sessionId"`
		Modes     *struct {
			CurrentModeID  string `json:"currentModeId"`
			AvailableModes []struct {
				ID string `json:"id"`
			} `json:"availableModes"`
		} `json:"modes"`
		ConfigOptions []configOption `json:"configOptions"`
	}
	newSession := map[string]any{"cwd": s.cfg.WorkDir, "mcpServers": []any{}}
	err := c.call(ctx, "session/new", newSession, &sess)
	var rerr *RPCError
	if errors.As(err, &rerr) && rerr.Code == codeAuthRequired {
		// Some agents (Grok) want an explicit authenticate, which uses the
		// login their CLI already has. Only agent-handled methods: a
		// terminal method would wait for a person.
		for _, m := range init.AuthMethods {
			if m.Type != "" && m.Type != "agent" {
				continue
			}
			actx, cancel := context.WithTimeout(ctx, authTimeout)
			aerr := c.call(actx, "authenticate", map[string]string{"methodId": m.ID}, nil)
			cancel()
			if aerr == nil {
				err = c.call(ctx, "session/new", newSession, &sess)
			}
			break
		}
	}
	if err != nil {
		return s.explain(ctx, err)
	}
	s.id = sess.SessionID
	s.emitLog(fmt.Sprintf("acp session %s with %s\n", s.id, agentName))

	// Unattended: prefer a mode that skips permission prompts.
	if opt, value, ok := bypassOption(sess.ConfigOptions); ok {
		if err := c.call(ctx, "session/set_config_option", map[string]any{"sessionId": s.id, "configId": opt, "value": value}, nil); err != nil {
			s.emitLog("could not switch to " + value + ": " + err.Error() + "\n")
		}
	} else if sess.Modes != nil {
		for _, m := range sess.Modes.AvailableModes {
			if isBypass(m.ID) && m.ID != sess.Modes.CurrentModeID {
				if err := c.call(ctx, "session/set_mode", map[string]any{"sessionId": s.id, "modeId": m.ID}, nil); err != nil {
					s.emitLog("could not switch to mode " + m.ID + ": " + err.Error() + "\n")
				}
				break
			}
		}
	}

	send := func(text string) (<-chan response, error) {
		return c.start("session/prompt", map[string]any{
			"sessionId": s.id,
			"prompt":    []map[string]string{{"type": "text", "text": text}},
		})
	}
	ch, err := send(prompt)
	if err != nil {
		return err
	}
	steer := s.cfg.Steer
	var pending []Steer
	interrupted := false
	for {
		var out struct {
			StopReason string `json:"stopReason"`
		}
		select {
		case st, ok := <-steer:
			if !ok {
				steer = nil
				continue
			}
			pending = append(pending, st)
			s.write("\n[" + st.By + "] " + st.Text + "\n")
			s.emitLog(fmt.Sprintf("message from %s for the agent%s\n", st.By, map[bool]string{true: " (interrupting)", false: ""}[st.Interrupt]))
			if st.Interrupt && !interrupted {
				interrupted = true
				_ = c.notify("session/cancel", map[string]string{"sessionId": s.id})
			}
			continue
		case r := <-ch:
			if err := decode("session/prompt", r, &out); err != nil {
				return s.explain(ctx, err)
			}
		case <-ctx.Done():
			// Ask the agent to stop, give it a moment to wrap up, then give up.
			_ = c.notify("session/cancel", map[string]string{"sessionId": s.id})
			select {
			case <-ch:
			case <-c.done:
			case <-time.After(s.cfg.CancelGrace):
			}
			return context.Cause(ctx)
		}
		if out.StopReason == "end_turn" || (out.StopReason == "cancelled" && interrupted && len(pending) > 0) {
			if len(pending) == 0 {
				return nil
			}
			ch, err = send(steerPrompt(pending))
			if err != nil {
				return err
			}
			pending, interrupted = nil, false
			continue
		}
		return stopError(ctx, out.StopReason)
	}
}

// steerPrompt turns people's messages into the agent's next turn.
func steerPrompt(msgs []Steer) string {
	var b strings.Builder
	for _, m := range msgs {
		fmt.Fprintf(&b, "Message from %s, who is watching your work:\n%s\n\n", m.By, m.Text)
	}
	b.WriteString("Take this into account and carry on with the Task. End with a short summary as before.")
	return b.String()
}

// stopError explains why the agent stopped before finishing its turn.
func stopError(ctx context.Context, reason string) error {
	switch reason {
	case "cancelled":
		if err := context.Cause(ctx); err != nil {
			return err
		}
		return errors.New("the agent canceled its turn")
	case "max_tokens":
		return errors.New("the agent ran out of output tokens")
	case "max_turn_requests":
		return errors.New("the agent reached its limit of model requests for one turn")
	case "refusal":
		return errors.New("the agent refused the task")
	default:
		return fmt.Errorf("the agent stopped: %q", reason)
	}
}

// explain turns protocol errors into what a person needs to do.
func (s *session) explain(ctx context.Context, err error) error {
	if cause := context.Cause(ctx); cause != nil {
		return cause
	}
	var rerr *RPCError
	if errors.As(err, &rerr) && rerr.Code == codeAuthRequired {
		return fmt.Errorf("%w: log in to %s on this worker as the worker's user, then retry (%v)", ErrNotLoggedIn, s.cfg.Agent, rerr)
	}
	return err
}

type configOption struct {
	ID      string          `json:"id"`
	Type    string          `json:"type"`
	Options json.RawMessage `json:"options"`
}

// bypassOption finds a select option (such as claude-agent-acp's "mode")
// offering a value that skips permission prompts.
func bypassOption(opts []configOption) (id, value string, ok bool) {
	for _, o := range opts {
		if o.Type != "select" {
			continue
		}
		var items []struct {
			Value   string `json:"value"`
			Options []struct {
				Value string `json:"value"`
			} `json:"options"` // grouped
		}
		if json.Unmarshal(o.Options, &items) != nil {
			continue
		}
		for _, v := range items {
			if isBypass(v.Value) {
				return o.ID, v.Value, true
			}
			for _, g := range v.Options {
				if isBypass(g.Value) {
					return o.ID, g.Value, true
				}
			}
		}
	}
	return "", "", false
}

func isBypass(id string) bool {
	id = strings.ToLower(strings.NewReplacer("-", "", "_", "").Replace(id))
	return id == "bypasspermissions" || id == "yolo" || id == "fullaccess"
}

// handle answers the agent's requests and notifications.
func (s *session) handle(method string, params json.RawMessage) (any, *RPCError) {
	switch method {
	case "session/update":
		var n struct {
			Update json.RawMessage `json:"update"`
		}
		if json.Unmarshal(params, &n) == nil {
			s.update(n.Update)
		}
		return nil, nil
	case "session/request_permission":
		return s.permit(params), nil
	default:
		return nil, &RPCError{Code: codeMethodNotFound, Message: "BuildBee does not provide " + method}
	}
}

// permit approves a tool call: BuildBee runs agents unattended in a Run's
// own directory. It prefers approving once over approving forever.
func (s *session) permit(params json.RawMessage) any {
	var req struct {
		ToolCall struct {
			ToolCallID string  `json:"toolCallId"`
			Title      *string `json:"title"`
		} `json:"toolCall"`
		Options []struct {
			OptionID string `json:"optionId"`
			Kind     string `json:"kind"`
		} `json:"options"`
	}
	_ = json.Unmarshal(params, &req)
	title := req.ToolCall.ToolCallID
	if req.ToolCall.Title != nil {
		title = *req.ToolCall.Title
	}
	for _, kind := range []string{"allow_once", "allow_always"} {
		for _, o := range req.Options {
			if o.Kind == kind {
				s.sink.add(Event{Kind: "log", Payload: map[string]any{
					"text": "approved: " + title + "\n", "tool_call_id": req.ToolCall.ToolCallID}})
				return map[string]any{"outcome": map[string]string{"outcome": "selected", "optionId": o.OptionID}}
			}
		}
	}
	s.sink.add(Event{Kind: "log", Payload: map[string]any{"text": "no option approves: " + title + "\n"}})
	return map[string]any{"outcome": map[string]string{"outcome": "cancelled"}}
}

// maxField caps tool input and output copied into one RunEvent.
const maxField = 16 << 10

// update maps one session/update to an Event.
func (s *session) update(raw json.RawMessage) {
	var u struct {
		SessionUpdate string          `json:"sessionUpdate"`
		Content       json.RawMessage `json:"content"`
		ToolCallID    string          `json:"toolCallId"`
		Title         *string         `json:"title"`
		Kind          string          `json:"kind"`
		Status        string          `json:"status"`
		RawInput      json.RawMessage `json:"rawInput"`
		RawOutput     json.RawMessage `json:"rawOutput"`
		Locations     []struct {
			Path string `json:"path"`
		} `json:"locations"`
		Entries []struct {
			Content string `json:"content"`
			Status  string `json:"status"`
		} `json:"entries"`
		Used int64           `json:"used"`
		Size int64           `json:"size"`
		Cost json.RawMessage `json:"cost"`
	}
	if json.Unmarshal(raw, &u) != nil {
		return
	}
	switch u.SessionUpdate {
	case "agent_message_chunk":
		if text := blockText(u.Content); text != "" {
			s.write(text)
			s.sink.add(Event{Kind: "token", Payload: map[string]any{"text": text}})
		}
	case "agent_thought_chunk":
		if text := blockText(u.Content); text != "" {
			s.sink.add(Event{Kind: "thought", Payload: map[string]any{"text": text}})
		}
	case "plan":
		entries := make([]map[string]string, 0, len(u.Entries))
		for _, e := range u.Entries {
			entries = append(entries, map[string]string{"content": e.Content, "status": e.Status})
		}
		s.sink.add(Event{Kind: "plan", Payload: map[string]any{"entries": entries}})
	case "tool_call":
		title := ""
		if u.Title != nil {
			title = *u.Title
		}
		s.write("\n[tool] " + title + "\n")
		p := map[string]any{"id": u.ToolCallID, "name": title, "kind": u.Kind, "status": u.Status}
		if in := clip(u.RawInput); in != nil {
			p["input"] = in
		}
		if len(u.Locations) > 0 {
			paths := make([]string, 0, len(u.Locations))
			for _, l := range u.Locations {
				paths = append(paths, l.Path)
			}
			p["paths"] = paths
		}
		s.sink.add(Event{Kind: "tool_call", Payload: p})
	case "tool_call_update":
		p := map[string]any{"id": u.ToolCallID, "status": u.Status, "ok": u.Status != "failed"}
		if u.Title != nil {
			p["name"] = *u.Title
		}
		if out := clip(u.RawOutput); out != nil {
			p["output"] = out
		}
		if summary := toolContent(u.Content); summary != "" {
			p["summary"] = summary
		}
		s.sink.add(Event{Kind: "tool_result", Payload: p})
	case "usage_update":
		p := map[string]any{"used": u.Used, "size": u.Size}
		if c := clip(u.Cost); c != nil {
			p["cost"] = c
		}
		s.sink.add(Event{Kind: "usage", Payload: p})
	}
}

// blockText returns the text of a ContentBlock ("" for images and such).
func blockText(raw json.RawMessage) string {
	var b struct {
		Type string `json:"type"`
		Text string `json:"text"`
	}
	if json.Unmarshal(raw, &b) != nil || b.Type != "text" {
		return ""
	}
	return b.Text
}

// toolContent summarizes a tool call's content: text, and the paths of diffs.
func toolContent(raw json.RawMessage) string {
	var items []struct {
		Type    string          `json:"type"`
		Content json.RawMessage `json:"content"`
		Path    string          `json:"path"`
	}
	if json.Unmarshal(raw, &items) != nil {
		return ""
	}
	var b strings.Builder
	for _, it := range items {
		switch it.Type {
		case "content":
			b.WriteString(blockText(it.Content))
		case "diff":
			b.WriteString("edited " + it.Path + "\n")
		}
	}
	return truncate(b.String(), maxField)
}

// clip keeps small JSON values and summarizes large ones.
func clip(raw json.RawMessage) any {
	if len(raw) == 0 || string(raw) == "null" {
		return nil
	}
	if len(raw) > maxField {
		return fmt.Sprintf("(%d bytes, truncated) %s", len(raw), truncate(string(raw), maxField))
	}
	var v any
	if json.Unmarshal(raw, &v) != nil {
		return nil
	}
	return v
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	for n > 0 && s[n]&0xC0 == 0x80 { // do not split a UTF-8 character
		n--
	}
	return s[:n] + "…"
}

// maxTranscript caps the transcript kept for the Run's Artifact.
const maxTranscript = 8 << 20

func (s *session) write(text string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.transcript.Len()+len(text) <= maxTranscript {
		s.transcript.WriteString(text)
	}
}

func (s *session) text() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.transcript.String()
}

func (s *session) emitLog(text string) {
	s.sink.add(Event{Kind: "log", Payload: map[string]any{"text": text}})
}

// forwardStderr streams the agent's stderr as log Events and keeps the last
// lines for error messages.
func (s *session) forwardStderr(r io.Reader) {
	const keep, maxLines = 20, 5000
	n := 0
	scanLines(r, func(line string) {
		s.mu.Lock()
		s.stderrTail = append(s.stderrTail, line)
		if len(s.stderrTail) > keep {
			s.stderrTail = s.stderrTail[1:]
		}
		s.mu.Unlock()
		if n++; n <= maxLines {
			s.sink.add(Event{Kind: "log", Payload: map[string]any{"text": line + "\n", "stream": "stderr"}})
		}
	})
}

func (s *session) stderr() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return strings.Join(s.stderrTail, "\n")
}

// process is a running agent.
type process struct {
	cmd    *exec.Cmd
	stdin  io.WriteCloser
	stdout io.ReadCloser
	stderr io.ReadCloser
}

func startProcess(argv []string, dir string) (*process, error) {
	cmd := exec.Command(argv[0], argv[1:]...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), "BUILDBEE=1")
	setProcessGroup(cmd)
	p := &process{cmd: cmd}
	var err error
	if p.stdin, err = cmd.StdinPipe(); err != nil {
		return nil, err
	}
	if p.stdout, err = cmd.StdoutPipe(); err != nil {
		return nil, err
	}
	if p.stderr, err = cmd.StderrPipe(); err != nil {
		return nil, err
	}
	if err := cmd.Start(); err != nil {
		return nil, err
	}
	return p, nil
}

// stop closes the agent's stdin, gives it a moment to exit, then kills its
// whole process group (agents start tools and subprocesses of their own).
func (p *process) stop() {
	_ = p.stdin.Close()
	exited := make(chan struct{})
	go func() { _ = p.cmd.Wait(); close(exited) }()
	select {
	case <-exited:
	case <-time.After(3 * time.Second):
		killProcessGroup(p.cmd)
		<-exited
	}
	killProcessGroup(p.cmd) // tools that outlived the agent itself
}
