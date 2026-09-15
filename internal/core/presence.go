package core

import (
	"context"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/codemodify/buildbee/internal/models"
)

// workerFresh is how long a worker counts as online after it last claimed
// or heartbeated; idle workers long-poll for claims well inside it.
const workerFresh = 90 * time.Second

// presence tracks, in this Server process, who has the app open and which
// workers are polling. It is not stored: a restart starts it afresh.
type presence struct {
	mu      sync.Mutex
	people  map[string]int // Person ID -> open sockets
	workers map[string]WorkerSeen
	local   LocalWorker
	seq     atomic.Int64
}

func newPresence() *presence {
	return &presence{people: map[string]int{}, workers: map[string]WorkerSeen{}}
}

// WorkerSeen is a worker as the Server last saw it.
type WorkerSeen struct {
	Name     string    `json:"name"`
	Agents   []string  `json:"agents"`
	Slots    int       `json:"slots,omitempty"` // Runs it executes at once, if it said
	Running  int       `json:"running"`
	Local    bool      `json:"local,omitempty"` // the Server's own worker
	LastSeen time.Time `json:"last_seen"`
}

// AgentLoad is one agent across the workers: who offers it and how many
// Runs asked for it are running or waiting. Agent "any" is Runs that take
// whichever agent a worker has.
type AgentLoad struct {
	Agent   string `json:"agent"`
	Workers int    `json:"workers"`
	Running int    `json:"running"`
	Queued  int    `json:"queued"`
}

// LocalWorker is whether the Server runs agents itself, and why not.
type LocalWorker struct {
	State  string `json:"state"` // off, starting, running, unavailable
	Reason string `json:"reason,omitempty"`
	Name   string `json:"name,omitempty"`
	// Isolation is where its agents run: container, or host (directly on
	// the Server's machine).
	Isolation string `json:"isolation,omitempty"`
}

// Presence is who is online now, and what the agents are doing.
type Presence struct {
	People  []models.Person `json:"people"`
	Workers []WorkerSeen    `json:"workers"`
	Agents  []AgentLoad     `json:"agents"`
	Slots   int             `json:"slots"`   // Runs the online workers can execute at once
	Running int             `json:"running"` // Runs executing now, merges included
	Local   LocalWorker     `json:"local"`
}

// Online marks a Person online for as long as one of their connections is
// open; call the returned func when the connection closes.
func (s *Service) Online(personID string) (release func()) {
	s.presence.mu.Lock()
	s.presence.people[personID]++
	first := s.presence.people[personID] == 1
	s.presence.mu.Unlock()
	if first {
		s.presenceChanged()
	}
	var once sync.Once
	return func() {
		once.Do(func() {
			s.presence.mu.Lock()
			s.presence.people[personID]--
			last := s.presence.people[personID] <= 0
			if last {
				delete(s.presence.people, personID)
			}
			s.presence.mu.Unlock()
			if last {
				s.presenceChanged()
			}
		})
	}
}

// sawWorker records a worker's claim or heartbeat. agents is nil for a
// heartbeat, which keeps what the worker offered.
func (s *Service) sawWorker(name string, agents []string) {
	s.presence.mu.Lock()
	w, known := s.presence.workers[name]
	fresh := known && time.Since(w.LastSeen) < workerFresh
	w.Name, w.LastSeen = name, time.Now().UTC()
	if agents != nil {
		w.Agents = slices.Clone(agents)
	}
	s.presence.workers[name] = w
	s.presence.mu.Unlock()
	if !fresh {
		s.presenceChanged()
	}
}

// WorkerSlots records how many Runs a worker executes at once.
func (s *Service) WorkerSlots(name string, slots int) {
	if name == "" || slots < 1 {
		return
	}
	s.presence.mu.Lock()
	w := s.presence.workers[name]
	w.Name, w.Slots = name, slots
	s.presence.workers[name] = w
	s.presence.mu.Unlock()
}

// SetLocalWorker records whether the Server runs agents itself.
func (s *Service) SetLocalWorker(l LocalWorker) {
	s.presence.mu.Lock()
	s.presence.local = l
	s.presence.mu.Unlock()
	s.presenceChanged()
}

// Presence returns the people with the app open, the workers seen lately
// and the load on each agent.
func (s *Service) Presence(ctx context.Context) (*Presence, error) {
	load, err := s.st.RunLoad(ctx)
	if err != nil {
		return nil, err
	}
	s.presence.mu.Lock()
	ids := make([]string, 0, len(s.presence.people))
	for id := range s.presence.people {
		ids = append(ids, id)
	}
	out := &Presence{People: []models.Person{}, Workers: []WorkerSeen{}, Agents: []AgentLoad{}, Local: s.presence.local}
	if out.Local.State == "" {
		out.Local.State = "off"
	}
	for name, w := range s.presence.workers {
		if time.Since(w.LastSeen) < workerFresh {
			w.Local = w.Name == out.Local.Name && out.Local.State == "running"
			out.Workers = append(out.Workers, w)
		} else if time.Since(w.LastSeen) > 24*time.Hour {
			delete(s.presence.workers, name)
		}
	}
	s.presence.mu.Unlock()
	slices.Sort(ids)
	for _, id := range ids {
		if p, err := s.st.GetPerson(ctx, id); err == nil {
			out.People = append(out.People, *p)
		}
	}
	slices.SortFunc(out.Workers, func(a, b WorkerSeen) int { return strings.Compare(a.Name, b.Name) })
	agents := map[string]*AgentLoad{}
	agent := func(name string) *AgentLoad {
		if name == "" {
			name = "any"
		}
		if agents[name] == nil {
			agents[name] = &AgentLoad{Agent: name}
		}
		return agents[name]
	}
	byWorker := map[string]int{}
	for _, l := range load {
		if l.Status == models.RunRunning {
			byWorker[l.Worker] += l.Runs
			out.Running += l.Runs
		}
		if l.Kind == models.RunMerge { // merges need no agent
			continue
		}
		if l.Status == models.RunRunning {
			agent(l.Agent).Running += l.Runs
		} else {
			agent(l.Agent).Queued += l.Runs
		}
	}
	for i := range out.Workers {
		w := &out.Workers[i]
		w.Running = byWorker[w.Name]
		out.Slots += w.Slots
		for _, a := range w.Agents {
			agent(a).Workers++
		}
	}
	for _, a := range agents {
		out.Agents = append(out.Agents, *a)
	}
	slices.SortFunc(out.Agents, func(a, b AgentLoad) int { return strings.Compare(a.Agent, b.Agent) })
	return out, nil
}

// presenceChanged tells "presence:server" subscribers to look again.
func (s *Service) presenceChanged() {
	s.pub.Publish(Event{Topic: "presence:server", Cursor: s.presence.seq.Add(1), Type: "presence", Data: map[string]any{}})
}
