package core

import (
	"context"
	"slices"
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
	seq     atomic.Int64
}

func newPresence() *presence {
	return &presence{people: map[string]int{}, workers: map[string]WorkerSeen{}}
}

// WorkerSeen is a worker as the Server last saw it.
type WorkerSeen struct {
	Name     string    `json:"name"`
	Agents   []string  `json:"agents"`
	LastSeen time.Time `json:"last_seen"`
}

// Presence is who is online now.
type Presence struct {
	People  []models.Person `json:"people"`
	Workers []WorkerSeen    `json:"workers"`
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

// Presence returns the people with the app open and the workers seen lately.
func (s *Service) Presence(ctx context.Context) (*Presence, error) {
	s.presence.mu.Lock()
	ids := make([]string, 0, len(s.presence.people))
	for id := range s.presence.people {
		ids = append(ids, id)
	}
	out := &Presence{People: []models.Person{}, Workers: []WorkerSeen{}}
	for name, w := range s.presence.workers {
		if time.Since(w.LastSeen) < workerFresh {
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
	slices.SortFunc(out.Workers, func(a, b WorkerSeen) int {
		if a.Name < b.Name {
			return -1
		}
		if a.Name > b.Name {
			return 1
		}
		return 0
	})
	return out, nil
}

// presenceChanged tells "presence:server" subscribers to look again.
func (s *Service) presenceChanged() {
	s.pub.Publish(Event{Topic: "presence:server", Cursor: s.presence.seq.Add(1), Type: "presence", Data: map[string]any{}})
}
