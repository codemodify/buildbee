// Package core holds BuildBee's business rules. Every operation runs in one
// database transaction that writes the change, its Activity entry (naming
// who acted) and any notifications together; live events are published only
// after the transaction commits. HTTP handlers, the Routines scheduler and
// webhooks all go through this package, so the same action has the same
// effects whichever way it arrives.
package core

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sort"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/codemodify/buildbee/internal/config"
	"github.com/codemodify/buildbee/internal/models"
	"github.com/codemodify/buildbee/internal/store"
	"github.com/google/uuid"
)

// Errors returned by Service. Store errors (store.ErrNotFound,
// store.ErrConflict, store.ErrInvalid) pass through unchanged.
var (
	// ErrNoActor: the action needs to know which Person is acting.
	ErrNoActor = errors.New("tell BuildBee who you are first: POST /v1/me with your name, or send the X-BuildBee-As header")
	// ErrUnavailable: an integration the action needs is not configured.
	ErrUnavailable = errors.New("unavailable")
)

// invalid wraps store.ErrInvalid with a message for the client.
func invalid(format string, args ...any) error {
	return fmt.Errorf("%w: %s", store.ErrInvalid, fmt.Sprintf(format, args...))
}

// Actor is who performs an action: a Person, a worker, or a named system
// process (the Routines scheduler) when both IDs are empty.
type Actor struct {
	PersonID string
	Worker   string // set for requests from a buildbee-worker
	Name     string
}

// System returns a non-Person actor such as "routines" or "github".
func System(name string) Actor { return Actor{Name: name} }

// WorkerActor returns the actor for a worker called name.
func WorkerActor(name string) Actor { return Actor{Worker: name, Name: "worker " + name} }

// IsPerson reports whether a Person is acting.
func (a Actor) IsPerson() bool { return a.PersonID != "" }

// Event is published after a transaction commits. Topics:
// project:<id> (Activity), channel:<id> (messages), run:<id> (RunEvents),
// person:<id> (notifications). Cursor orders events within a topic.
type Event struct {
	Topic  string `json:"topic"`
	Cursor int64  `json:"cursor"`
	Type   string `json:"type"`
	Data   any    `json:"data"`
}

// Publisher delivers committed events to live subscribers.
type Publisher interface {
	Publish(Event)
}

type nopPublisher struct{}

func (nopPublisher) Publish(Event) {}

// Options configures a Service.
type Options struct {
	GitHub config.GitHub
	Logger *slog.Logger
	Now    func() time.Time
}

// Service implements BuildBee's operations over a Store.
type Service struct {
	st       *store.Store
	pub      Publisher
	log      *slog.Logger
	now      func() time.Time
	github   config.GitHub
	queue    *signal // closed and replaced whenever a Run is queued
	presence *presence
}

// New returns a Service. A nil Publisher drops events.
func New(st *store.Store, pub Publisher, opts Options) *Service {
	if pub == nil {
		pub = nopPublisher{}
	}
	s := &Service{st: st, pub: pub, log: opts.Logger, now: opts.Now, github: opts.GitHub, queue: newSignal(), presence: newPresence()}
	if s.log == nil {
		s.log = slog.Default()
	}
	if s.now == nil {
		s.now = func() time.Time { return time.Now().UTC() }
	}
	return s
}

// Ping reports whether the database answers.
func (s *Service) Ping(ctx context.Context) error { return s.st.Ping(ctx) }

// work is one transaction: its store, clock and the events to publish once
// it commits.
type work struct {
	st      *store.Store
	now     time.Time
	events  []Event
	lastRun *models.Run // Run started by the latest handoff, if any
	queued  bool        // a Run entered the queue; wake waiting workers after commit
	load    bool        // a Run started or stopped; the agents' load changed
}

func (s *Service) tx(ctx context.Context, fn func(w *work) error) error {
	var w *work
	err := s.st.Tx(ctx, func(tx *store.Store) error {
		w = &work{st: tx, now: s.now()}
		return fn(w)
	})
	if err != nil {
		return err
	}
	for _, e := range inTopicOrder(w.events) {
		s.pub.Publish(e)
	}
	if w.queued {
		s.queue.notify()
	}
	if w.queued || w.load {
		s.presenceChanged()
	}
	return nil
}

// inTopicOrder sorts each topic's events by cursor, keeping the slots each
// topic had among the others: a transaction may write a message, then emit
// a reply to it before the message itself.
func inTopicOrder(events []Event) []Event {
	slots := map[string][]int{}
	for i, e := range events {
		slots[e.Topic] = append(slots[e.Topic], i)
	}
	out := make([]Event, len(events))
	for _, idx := range slots {
		group := make([]Event, len(idx))
		for j, i := range idx {
			group[j] = events[i]
		}
		sort.SliceStable(group, func(a, b int) bool { return group[a].Cursor < group[b].Cursor })
		for j, i := range idx {
			out[i] = group[j]
		}
	}
	return out
}

func (w *work) emit(topic string, cursor int64, typ string, data any) {
	w.events = append(w.events, Event{Topic: topic, Cursor: cursor, Type: typ, Data: data})
}

// who names the actor of an Activity entry: a Member when there is one,
// otherwise the system actor's name.
type who struct {
	memberID string
	name     string
}

func whoOf(m *models.Member, a Actor) who {
	if m != nil {
		return who{memberID: m.ID, name: m.DisplayName}
	}
	if a.Name != "" {
		return who{name: a.Name}
	}
	return who{name: "system"}
}

// activity appends to the Project log in this transaction and queues the
// entry for live subscribers of the Project.
func (w *work) activity(ctx context.Context, projectID string, by who, typ, action, subjectID string, payload map[string]any) error {
	a, err := w.st.InsertActivity(ctx, models.Activity{
		ProjectID: projectID, ActorMemberID: by.memberID, Actor: by.name,
		Type: typ, Action: action, SubjectID: subjectID, Payload: payload, CreatedAt: w.now,
	})
	if err != nil {
		return err
	}
	w.emit("project:"+projectID, a.Seq, "activity", a)
	return nil
}

// member returns the acting Person's Member in a Project. When join is true
// a Person who is not yet a Member joins as "member" (writes imply joining;
// the LAN has no invitations). System actors have no Member.
func (w *work) member(ctx context.Context, a Actor, projectID string, join bool) (*models.Member, error) {
	if !a.IsPerson() {
		return nil, nil
	}
	m, err := w.st.MemberForPerson(ctx, projectID, a.PersonID)
	if err == nil && (m.LeftAt == nil || !join) {
		return m, nil
	}
	if err != nil && (!errors.Is(err, store.ErrNotFound) || !join) {
		return m, err
	}
	p, err := w.st.GetPerson(ctx, a.PersonID)
	if err != nil {
		return nil, err
	}
	m, joined, err := w.st.JoinPerson(ctx, models.Member{
		ID: uuid.NewString(), ProjectID: projectID, PersonID: p.ID,
		DisplayName: p.Name, Role: models.RoleMember, CreatedAt: w.now,
	})
	if err != nil {
		return nil, err
	}
	if joined {
		if err := w.activity(ctx, projectID, whoOf(m, a), models.TypeMember, "joined", m.ID,
			map[string]any{"kind": m.Kind, "role": m.Role, "name": m.DisplayName}); err != nil {
			return nil, err
		}
	}
	return m, nil
}

// requireMember is member(join=true) for actions only a Person may take.
func (w *work) requireMember(ctx context.Context, a Actor, projectID string) (*models.Member, error) {
	if !a.IsPerson() {
		return nil, ErrNoActor
	}
	return w.member(ctx, a, projectID, true)
}

// openProject returns a Project that accepts writes. The direct space
// holds only DMs, so it takes no Project writes.
func (w *work) openProject(ctx context.Context, id string) (*models.Project, error) {
	p, err := w.openSpace(ctx, id)
	if err == nil && p.Kind == models.DirectSpace {
		return nil, fmt.Errorf("%w: direct messages are not a project", store.ErrConflict)
	}
	return p, err
}

// openSpace returns a Project or the direct space, if it accepts messages.
func (w *work) openSpace(ctx context.Context, id string) (*models.Project, error) {
	p, err := w.st.GetProject(ctx, id)
	if err != nil {
		return nil, err
	}
	if p.ArchivedAt != nil {
		return nil, fmt.Errorf("%w: project %q is archived", store.ErrConflict, p.Name)
	}
	return p, nil
}

// --- input hygiene ---

// text trims s, requires it when required, and caps it at max characters.
func text(field, s string, required bool, max int) (string, error) {
	s = strings.TrimSpace(s)
	if required && s == "" {
		return "", invalid("%s is required", field)
	}
	if utf8.RuneCountInString(s) > max {
		return "", invalid("%s is longer than %d characters", field, max)
	}
	return s, nil
}

// name validates a display name: 1-60 printable characters.
func name(field, s string) (string, error) {
	s, err := text(field, s, true, 60)
	if err != nil {
		return "", err
	}
	for _, r := range s {
		if !unicode.IsPrint(r) {
			return "", invalid("%s has characters that cannot be displayed", field)
		}
	}
	return s, nil
}

// truncate cuts s to at most max bytes without splitting a character.
func truncate(s string, max int) string {
	if len(s) <= max {
		return s
	}
	cut := max
	for cut > 0 && !utf8.RuneStart(s[cut]) {
		cut--
	}
	return s[:cut]
}

func href(projectID string, rest ...string) string {
	h := "#/projects/" + projectID
	for _, r := range rest {
		h += "/" + r
	}
	return h
}
