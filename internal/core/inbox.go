package core

import (
	"context"
	"errors"
	"fmt"

	"github.com/codemodify/buildbee/internal/models"
	"github.com/codemodify/buildbee/internal/store"
	"github.com/google/uuid"
)

// Hello returns the Person with this name, creating them on first use.
// There is no login on a LAN deployment: the name is the identity.
func (s *Service) Hello(ctx context.Context, personName string) (*models.Person, error) {
	n, err := name("name", personName)
	if err != nil {
		return nil, err
	}
	return s.st.UpsertPerson(ctx, n)
}

// Rename changes the acting Person's name, everywhere they appear.
func (s *Service) Rename(ctx context.Context, a Actor, personName string) (*models.Person, error) {
	if !a.IsPerson() {
		return nil, ErrNoActor
	}
	n, err := name("name", personName)
	if err != nil {
		return nil, err
	}
	var out *models.Person
	err = s.tx(ctx, func(w *work) error {
		old, err := w.st.GetPerson(ctx, a.PersonID)
		if err != nil {
			return err
		}
		members, err := w.st.RenamePerson(ctx, a.PersonID, n)
		if errors.Is(err, store.ErrConflict) {
			return fmt.Errorf("%w: someone is already called %s", store.ErrConflict, n)
		}
		if err != nil {
			return err
		}
		for i := range members {
			m := &members[i]
			if err := w.activity(ctx, m.ProjectID, whoOf(m, a), models.TypeMember, "updated", m.ID,
				map[string]any{"display_name": map[string]string{"from": old.Name, "to": n}}); err != nil {
				return err
			}
		}
		out, err = w.st.GetPerson(ctx, a.PersonID)
		return err
	})
	return out, err
}

// Person returns one Person.
func (s *Service) Person(ctx context.Context, id string) (*models.Person, error) {
	return s.st.GetPerson(ctx, id)
}

// Inbox is one page of a Person's notifications plus their unread count.
type Inbox struct {
	Items   []models.Notification `json:"items"`
	Unread  int                   `json:"unread"`
	HasMore bool                  `json:"has_more"`
}

// Notifications returns the acting Person's inbox across all Projects.
func (s *Service) Notifications(ctx context.Context, a Actor, unreadOnly bool, p store.Page) (*Inbox, error) {
	if !a.IsPerson() {
		return nil, ErrNoActor
	}
	items, more, err := s.st.ListNotifications(ctx, a.PersonID, unreadOnly, p)
	if err != nil {
		return nil, err
	}
	unread, err := s.st.CountUnread(ctx, a.PersonID)
	if err != nil {
		return nil, err
	}
	return &Inbox{Items: items, Unread: unread, HasMore: more}, nil
}

// MarkRead marks one of the acting Person's notifications read.
func (s *Service) MarkRead(ctx context.Context, a Actor, id string) (*models.Notification, error) {
	if !a.IsPerson() {
		return nil, ErrNoActor
	}
	return s.st.MarkNotificationRead(ctx, id, a.PersonID, s.now())
}

// MarkAllRead marks every unread notification of the acting Person read.
func (s *Service) MarkAllRead(ctx context.Context, a Actor) (int, error) {
	if !a.IsPerson() {
		return 0, ErrNoActor
	}
	return s.st.MarkAllNotificationsRead(ctx, a.PersonID, s.now())
}

// Preferences returns the acting Person's notification preferences.
func (s *Service) Preferences(ctx context.Context, a Actor) (*models.Preferences, error) {
	if !a.IsPerson() {
		return nil, ErrNoActor
	}
	return s.st.GetPreferences(ctx, a.PersonID)
}

// PreferencesPatch changes only the fields that are set.
type PreferencesPatch struct {
	MuteMentions *bool `json:"mute_mentions"`
	MuteRoutines *bool `json:"mute_routines"`
}

// SetPreferences updates the acting Person's notification preferences.
func (s *Service) SetPreferences(ctx context.Context, a Actor, patch PreferencesPatch) (*models.Preferences, error) {
	if !a.IsPerson() {
		return nil, ErrNoActor
	}
	var out *models.Preferences
	err := s.tx(ctx, func(w *work) error {
		p, err := w.st.GetPreferences(ctx, a.PersonID)
		if err != nil {
			return err
		}
		if patch.MuteMentions != nil {
			p.MuteMentions = *patch.MuteMentions
		}
		if patch.MuteRoutines != nil {
			p.MuteRoutines = *patch.MuteRoutines
		}
		p.UpdatedAt = w.now
		out = p
		return w.st.SetPreferences(ctx, *p)
	})
	return out, err
}

// notice is a notification to deliver.
type notice struct {
	projectID, kind, title, body, href string
}

// notifyPerson delivers n to one Person unless their preferences mute its kind.
func (w *work) notifyPerson(ctx context.Context, personID string, n notice) error {
	if personID == "" {
		return nil
	}
	prefs, err := w.st.GetPreferences(ctx, personID)
	if err != nil {
		return err
	}
	if (n.kind == "mention" && prefs.MuteMentions) || (n.kind == "routine" && prefs.MuteRoutines) {
		return nil
	}
	saved, err := w.st.InsertNotification(ctx, models.Notification{
		ID: uuid.NewString(), PersonID: personID, ProjectID: n.projectID, Kind: n.kind,
		Title: truncate(n.title, 300), Body: truncate(n.body, 2000), Href: n.href, CreatedAt: w.now,
	})
	if err != nil {
		return err
	}
	w.emit("person:"+personID, saved.Seq, "notification", saved)
	return nil
}

// notifyMember delivers n to a Member if it is a Person; Bots have no inbox.
func (w *work) notifyMember(ctx context.Context, m *models.Member, n notice) error {
	if m == nil || m.Kind != models.KindHuman {
		return nil
	}
	return w.notifyPerson(ctx, m.PersonID, n)
}

// notifyPeople delivers n to every Person in the Project except the actor.
func (w *work) notifyPeople(ctx context.Context, projectID string, except *models.Member, n notice) error {
	members, err := w.st.ListMembers(ctx, projectID)
	if err != nil {
		return err
	}
	for i := range members {
		m := &members[i]
		if m.Kind != models.KindHuman || (except != nil && m.ID == except.ID) {
			continue
		}
		if err := w.notifyPerson(ctx, m.PersonID, n); err != nil {
			return err
		}
	}
	return nil
}
