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

// agentFresh is how long a Bot counts as connected after its agent last
// claimed or heartbeated; an idle agent long-polls well inside it.
const agentFresh = 90 * time.Second

// presence tracks, in this Server process, who has the app open and which
// Bots' agents are connected. It is not stored: a restart starts it afresh.
type presence struct {
	mu     sync.Mutex
	people map[string]int // Person ID -> open sockets
	bots   map[string]BotSeen
	seq    atomic.Int64
}

func newPresence() *presence {
	return &presence{people: map[string]int{}, bots: map[string]BotSeen{}}
}

// BotSeen is a Bot whose agent is connected: what it runs and where.
type BotSeen struct {
	BotID     string    `json:"bot_id"`
	Name      string    `json:"name"`
	ProjectID string    `json:"project_id"`
	AI        string    `json:"ai"`             // claude, codex, grok, opencode, goose, fake
	Process   string    `json:"process"`        // what the agent calls itself
	Host      string    `json:"host,omitempty"` // the machine it runs on
	Slots     int       `json:"slots,omitempty"`
	Running   int       `json:"running"`
	LastSeen  time.Time `json:"last_seen"`
}

// Presence is who is online now: people with the app open and the Bots
// whose agents are connected.
type Presence struct {
	People []models.Person `json:"people"`
	Bots   []BotSeen       `json:"bots"`
	// Work is what each Bot has going, by Bot ID; "" is work nobody is
	// assigned, such as a merge, which any connected agent takes.
	Work    map[string]BotWork `json:"work"`
	Slots   int                `json:"slots"`   // Runs the connected Bots take at once
	Running int                `json:"running"` // Runs executing now
}

// BotWork is how many Runs a Bot has going and waiting.
type BotWork struct {
	Running int `json:"running"`
	Queued  int `json:"queued"`
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

// sawAgent records a Bot's agent asking for work: it is connected now.
func (s *Service) sawAgent(bot models.Member, process, ai, host string, slots int) {
	s.presence.mu.Lock()
	was, known := s.presence.bots[bot.ID]
	fresh := known && time.Since(was.LastSeen) < agentFresh && was.AI == ai && was.Process == process
	s.presence.bots[bot.ID] = BotSeen{BotID: bot.ID, Name: bot.DisplayName, ProjectID: bot.ProjectID,
		AI: ai, Process: process, Host: host, Slots: slots, LastSeen: time.Now().UTC()}
	s.presence.mu.Unlock()
	if !fresh {
		s.presenceChanged()
	}
}

// stillThere keeps a Bot connected while its agent heartbeats a Run.
func (s *Service) stillThere(botID string) {
	if botID == "" {
		return
	}
	s.presence.mu.Lock()
	if b, ok := s.presence.bots[botID]; ok {
		b.LastSeen = time.Now().UTC()
		s.presence.bots[botID] = b
	}
	s.presence.mu.Unlock()
}

// SetAgentGone forgets a Bot's agent (it said goodbye).
func (s *Service) SetAgentGone(botID string) {
	s.presence.mu.Lock()
	_, had := s.presence.bots[botID]
	delete(s.presence.bots, botID)
	s.presence.mu.Unlock()
	if had {
		s.presenceChanged()
	}
}

// Presence returns the people with the app open and the Bots whose agents
// are connected, with what they are running.
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
	out := &Presence{People: []models.Person{}, Bots: []BotSeen{}}
	for id, b := range s.presence.bots {
		if time.Since(b.LastSeen) < agentFresh {
			out.Bots = append(out.Bots, b)
		} else if time.Since(b.LastSeen) > 24*time.Hour {
			delete(s.presence.bots, id)
		}
	}
	s.presence.mu.Unlock()
	slices.Sort(ids)
	for _, id := range ids {
		if p, err := s.st.GetPerson(ctx, id); err == nil {
			out.People = append(out.People, *p)
		}
	}
	out.Work = map[string]BotWork{}
	for _, l := range load {
		w := out.Work[l.Bot]
		if l.Status == models.RunRunning {
			w.Running += l.Runs
			out.Running += l.Runs
		} else {
			w.Queued += l.Runs
		}
		out.Work[l.Bot] = w
	}
	for i := range out.Bots {
		out.Bots[i].Running = out.Work[out.Bots[i].BotID].Running
		out.Slots += out.Bots[i].Slots
	}
	slices.SortFunc(out.Bots, func(a, b BotSeen) int { return strings.Compare(a.Name+a.BotID, b.Name+b.BotID) })
	return out, nil
}

// presenceChanged tells "presence:server" subscribers to look again.
func (s *Service) presenceChanged() {
	s.pub.Publish(Event{Topic: "presence:server", Cursor: s.presence.seq.Add(1), Type: "presence", Data: map[string]any{}})
}
