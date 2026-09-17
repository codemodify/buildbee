package acp

import (
	"bufio"
	"io"
	"strings"
	"sync"
	"time"
)

// Agents stream replies a few characters at a time. The sink merges
// consecutive text chunks for up to coalesceWindow (or coalesceBytes) so a
// Run posts a handful of events per second, not one per token.
const (
	coalesceWindow = 200 * time.Millisecond
	coalesceBytes  = 8 << 10
	sinkBuffer     = 1024
)

// sink delivers Events to a Handler in order from one goroutine. After the
// Handler fails, later Events are dropped and onError is called once.
type sink struct {
	emit    Handler
	in      chan Event
	done    chan struct{}
	onError func(error)
	err     error

	mu     sync.RWMutex
	closed bool
}

func newSink(emit Handler) *sink {
	return &sink{emit: emit, in: make(chan Event, sinkBuffer), done: make(chan struct{})}
}

func (k *sink) start(onError func(error)) {
	k.onError = onError
	go k.run()
}

// add queues ev; it blocks while the queue is full, which slows the agent
// down to what the Server accepts. Events after close are dropped.
func (k *sink) add(ev Event) {
	k.mu.RLock()
	defer k.mu.RUnlock()
	if !k.closed {
		k.in <- ev
	}
}

// close delivers what is queued and returns the Handler's error, if any.
func (k *sink) close() error {
	k.mu.Lock()
	if !k.closed {
		k.closed = true
		close(k.in)
	}
	k.mu.Unlock()
	<-k.done
	return k.err
}

func (k *sink) run() {
	defer close(k.done)
	var pend *Event
	var window <-chan time.Time
	flush := func() {
		if pend != nil {
			k.send(*pend)
			pend = nil
		}
		window = nil
	}
	for {
		select {
		case ev, ok := <-k.in:
			if !ok {
				flush()
				return
			}
			if pend != nil && pend.Kind == ev.Kind && textOnly(ev) {
				text := pend.Payload["text"].(string) + ev.Payload["text"].(string)
				pend.Payload = map[string]any{"text": text}
				if len(text) >= coalesceBytes {
					flush()
				}
				continue
			}
			flush()
			if (ev.Kind == "token" || ev.Kind == "thought") && textOnly(ev) {
				pend, window = &ev, time.After(coalesceWindow)
				continue
			}
			k.send(ev)
		case <-window:
			flush()
		}
	}
}

func textOnly(ev Event) bool {
	_, ok := ev.Payload["text"].(string)
	return ok && len(ev.Payload) == 1
}

func (k *sink) send(ev Event) {
	if k.err != nil || k.emit == nil {
		return
	}
	if err := k.emit(ev); err != nil {
		k.err = err
		if k.onError != nil {
			k.onError(err)
		}
	}
}

// maxLine caps one line of agent stderr; the rest of a longer line is dropped.
const maxLine = 64 << 10

// scanLines calls each for every line of r until r ends. Long lines are
// truncated, and r is always drained so the agent never blocks on a full pipe.
func scanLines(r io.Reader, each func(string)) {
	br := bufio.NewReaderSize(r, 64<<10)
	var line []byte
	truncated := false
	for {
		chunk, err := br.ReadSlice('\n')
		if room := maxLine - len(line); len(chunk) > room {
			line, truncated = append(line, chunk[:max(room, 0)]...), true
		} else {
			line = append(line, chunk...)
		}
		if err == bufio.ErrBufferFull {
			continue
		}
		if len(line) > 0 {
			text := strings.TrimRight(string(line), "\r\n")
			if truncated {
				text += " …[truncated]"
			}
			each(text)
		}
		line, truncated = line[:0], false
		if err != nil {
			return
		}
	}
}
