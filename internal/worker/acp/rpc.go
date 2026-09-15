package acp

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strconv"
	"sync"
)

// maxMessage caps one JSON-RPC message from the agent. Tool updates can
// carry whole files, so this is generous; a larger message ends the session.
const maxMessage = 64 << 20

// RPCError is a JSON-RPC error returned by the agent.
type RPCError struct {
	Code    int             `json:"code"`
	Message string          `json:"message"`
	Data    json.RawMessage `json:"data,omitempty"`
}

func (e *RPCError) Error() string {
	if len(e.Data) > 0 && string(e.Data) != "null" {
		return fmt.Sprintf("%s (code %d): %s", e.Message, e.Code, e.Data)
	}
	return fmt.Sprintf("%s (code %d)", e.Message, e.Code)
}

// JSON-RPC and ACP error codes.
const (
	codeMethodNotFound = -32601
	codeInternal       = -32603
	codeAuthRequired   = -32000
)

type message struct {
	JSONRPC string           `json:"jsonrpc"`
	ID      *json.RawMessage `json:"id,omitempty"`
	Method  string           `json:"method,omitempty"`
	Params  json.RawMessage  `json:"params,omitempty"`
	Result  json.RawMessage  `json:"result,omitempty"`
	Error   *RPCError        `json:"error,omitempty"`
}

type response struct {
	result json.RawMessage
	err    error
}

// handlerFunc answers a request from the peer (result or error), or handles
// a notification (the return values are ignored). Notifications are handled
// in order on the read loop; requests each on their own goroutine.
type handlerFunc func(method string, params json.RawMessage) (any, *RPCError)

// conn is a JSON-RPC 2.0 peer over newline-delimited JSON, as ACP uses on
// stdio. Either side may send requests and notifications.
type conn struct {
	w       io.Writer
	wmu     sync.Mutex
	handle  handlerFunc
	mu      sync.Mutex
	nextID  int64
	pending map[string]chan response
	closed  error         // set once the read loop ends
	done    chan struct{} // closed when the read loop ends
}

// newConn returns a peer writing to w. It reads nothing until listen.
func newConn(w io.Writer, handle handlerFunc) *conn {
	return &conn{w: w, handle: handle, pending: map[string]chan response{}, done: make(chan struct{})}
}

// listen starts reading messages from r.
func (c *conn) listen(r io.Reader) *conn {
	go c.readLoop(r)
	return c
}

// errPeerGone means the peer closed its end (for an agent: it exited).
var errPeerGone = errors.New("the agent closed the connection")

func (c *conn) readLoop(r io.Reader) {
	br := bufio.NewReaderSize(r, 1<<20)
	err := func() error {
		for {
			line, err := readLine(br)
			if err != nil {
				return err
			}
			if len(line) == 0 {
				continue
			}
			var m message
			if err := json.Unmarshal(line, &m); err != nil {
				return fmt.Errorf("the agent sent invalid JSON: %w", err)
			}
			c.dispatch(m)
		}
	}()
	if errors.Is(err, io.EOF) || errors.Is(err, io.ErrClosedPipe) {
		err = errPeerGone
	}
	c.mu.Lock()
	c.closed = err
	pending := c.pending
	c.pending = map[string]chan response{}
	c.mu.Unlock()
	for _, ch := range pending {
		ch <- response{err: err}
	}
	close(c.done)
}

// readLine reads one newline-terminated message of at most maxMessage bytes.
func readLine(br *bufio.Reader) ([]byte, error) {
	var line []byte
	for {
		chunk, err := br.ReadSlice('\n')
		if len(line)+len(chunk) > maxMessage {
			return nil, fmt.Errorf("the agent sent a message larger than %d MiB", maxMessage>>20)
		}
		line = append(line, chunk...)
		switch {
		case err == nil:
			return line, nil
		case errors.Is(err, bufio.ErrBufferFull):
			continue
		case errors.Is(err, io.EOF) && len(line) > 0:
			return line, nil
		default:
			return nil, err
		}
	}
}

func (c *conn) dispatch(m message) {
	switch {
	case m.Method != "" && m.ID != nil: // request from the peer
		// Answered off the read loop: a handler may itself wait on the peer.
		go func() {
			result, rerr := c.handle(m.Method, m.Params)
			reply := message{JSONRPC: "2.0", ID: m.ID, Error: rerr}
			if rerr == nil {
				raw, err := json.Marshal(result)
				if err != nil {
					reply.Error = &RPCError{Code: codeInternal, Message: err.Error()}
				} else {
					reply.Result = raw
				}
			}
			_ = c.send(reply)
		}()
	case m.Method != "": // notification, handled in order
		c.handle(m.Method, m.Params)
	case m.ID != nil: // response to one of our requests
		key := string(*m.ID)
		c.mu.Lock()
		ch := c.pending[key]
		delete(c.pending, key)
		c.mu.Unlock()
		if ch == nil {
			return
		}
		if m.Error != nil {
			ch <- response{err: m.Error}
		} else {
			ch <- response{result: m.Result}
		}
	}
}

func (c *conn) send(m message) error {
	raw, err := json.Marshal(m)
	if err != nil {
		return err
	}
	raw = append(raw, '\n')
	c.wmu.Lock()
	defer c.wmu.Unlock()
	_, err = c.w.Write(raw)
	return err
}

// start sends a request and returns the channel its response arrives on.
func (c *conn) start(method string, params any) (<-chan response, error) {
	raw, err := json.Marshal(params)
	if err != nil {
		return nil, err
	}
	c.mu.Lock()
	if c.closed != nil {
		c.mu.Unlock()
		return nil, c.closed
	}
	c.nextID++
	id := json.RawMessage(strconv.FormatInt(c.nextID, 10))
	ch := make(chan response, 1)
	c.pending[string(id)] = ch
	c.mu.Unlock()
	if err := c.send(message{JSONRPC: "2.0", ID: &id, Method: method, Params: raw}); err != nil {
		c.mu.Lock()
		delete(c.pending, string(id))
		c.mu.Unlock()
		return nil, err
	}
	return ch, nil
}

// call sends a request and decodes its result into out (which may be nil).
func (c *conn) call(ctx context.Context, method string, params, out any) error {
	ch, err := c.start(method, params)
	if err != nil {
		return err
	}
	select {
	case <-ctx.Done():
		return ctx.Err()
	case r := <-ch:
		return decode(method, r, out)
	}
}

func decode(method string, r response, out any) error {
	if r.err != nil {
		return fmt.Errorf("%s: %w", method, r.err)
	}
	if out == nil || len(r.result) == 0 {
		return nil
	}
	if err := json.Unmarshal(r.result, out); err != nil {
		return fmt.Errorf("%s: unexpected result: %w", method, err)
	}
	return nil
}

func (c *conn) notify(method string, params any) error {
	raw, err := json.Marshal(params)
	if err != nil {
		return err
	}
	return c.send(message{JSONRPC: "2.0", Method: method, Params: raw})
}
