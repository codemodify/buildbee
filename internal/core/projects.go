package core

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/codemodify/buildbee/internal/models"
	"github.com/codemodify/buildbee/internal/store"
	"github.com/google/uuid"
)

// CreateProject creates a Project owned by the acting Person, with the four
// default Bots, a #general Channel and a disabled morning-digest Routine.
func (s *Service) CreateProject(ctx context.Context, a Actor, projectName string, autoRun bool) (*models.ProjectBundle, error) {
	n, err := text("name", projectName, true, 100)
	if err != nil {
		return nil, err
	}
	var out *models.ProjectBundle
	err = s.tx(ctx, func(w *work) error {
		p := models.Project{ID: uuid.NewString(), Name: n, AutoRun: autoRun, CreatedAt: w.now}
		if err := w.st.InsertProject(ctx, p); err != nil {
			return err
		}
		var members []models.Member
		var owner *models.Member
		if a.IsPerson() {
			person, err := w.st.GetPerson(ctx, a.PersonID)
			if err != nil {
				return err
			}
			m := models.Member{ID: uuid.NewString(), ProjectID: p.ID, PersonID: person.ID, Kind: models.KindHuman,
				DisplayName: person.Name, Role: models.RoleOwner, CreatedAt: w.now}
			if err := w.st.InsertMember(ctx, m); err != nil {
				return err
			}
			members = append(members, m)
			owner = &members[0]
		}
		for i, b := range models.DefaultBots() {
			m := models.Member{ID: uuid.NewString(), ProjectID: p.ID, Kind: models.KindBot, DisplayName: b.Name,
				Role: b.Role, Instructions: b.Instructions, CreatedAt: w.now.Add(time.Duration(i+1) * time.Microsecond)}
			if err := w.st.InsertMember(ctx, m); err != nil {
				return err
			}
			members = append(members, m)
		}
		ch := models.Channel{ID: uuid.NewString(), ProjectID: p.ID, Name: "general", CreatedAt: w.now}
		if err := w.st.InsertChannel(ctx, ch); err != nil {
			return err
		}
		routine := models.Routine{ID: uuid.NewString(), ProjectID: p.ID, Name: "morning-digest", Schedule: "24h", CreatedAt: w.now}
		if pulse := models.MemberByRole(members, models.RolePulse); pulse != nil {
			routine.BotMemberID = pulse.ID
		}
		if err := w.st.InsertRoutine(ctx, routine); err != nil {
			return err
		}
		if err := w.activity(ctx, p.ID, whoOf(owner, a), models.TypeProject, "created", p.ID,
			map[string]any{"name": p.Name, "auto_run": p.AutoRun}); err != nil {
			return err
		}
		out = &models.ProjectBundle{Project: p, Members: members, Channels: []models.Channel{ch}}
		return nil
	})
	return out, err
}

// Projects lists Projects, oldest first; archived ones only on request.
func (s *Service) Projects(ctx context.Context, includeArchived bool) ([]models.Project, error) {
	return s.st.ListProjects(ctx, includeArchived)
}

// Project returns a Project with its Members and open Channels.
func (s *Service) Project(ctx context.Context, id string) (*models.ProjectBundle, error) {
	p, err := s.st.GetProject(ctx, id)
	if err != nil {
		return nil, err
	}
	members, err := s.st.ListMembers(ctx, id)
	if err != nil {
		return nil, err
	}
	channels, err := s.st.ListChannels(ctx, id, false)
	if err != nil {
		return nil, err
	}
	return &models.ProjectBundle{Project: *p, Members: members, Channels: channels}, nil
}

// ProjectPatch changes only the fields that are set.
type ProjectPatch struct {
	Name          *string `json:"name"`
	AutoRun       *bool   `json:"auto_run"`
	RepoURL       *string `json:"repo_url"`
	DefaultBranch *string `json:"default_branch"`
	Archived      *bool   `json:"archived"`
}

// UpdateProject renames, toggles auto_run, or archives/unarchives a Project.
// An archived Project is read-only and its Routines stop firing.
func (s *Service) UpdateProject(ctx context.Context, a Actor, id string, patch ProjectPatch) (*models.Project, error) {
	var out *models.Project
	err := s.tx(ctx, func(w *work) error {
		p, err := w.st.GetProject(ctx, id)
		if err != nil {
			return err
		}
		if p.ArchivedAt != nil && (patch.Archived == nil || *patch.Archived) {
			return fmt.Errorf("%w: project is archived; unarchive it first", store.ErrConflict)
		}
		m, err := w.member(ctx, a, id, true)
		if err != nil {
			return err
		}
		changes := map[string]any{}
		if patch.Name != nil {
			n, err := text("name", *patch.Name, true, 100)
			if err != nil {
				return err
			}
			changes["name"], p.Name = n, n
		}
		if patch.AutoRun != nil {
			changes["auto_run"], p.AutoRun = *patch.AutoRun, *patch.AutoRun
		}
		if patch.RepoURL != nil {
			u, err := text("repo_url", *patch.RepoURL, false, 500)
			if err != nil {
				return err
			}
			if u != "" && !validRepoURL(u) {
				return invalid("repo_url must be a git URL (https://…, ssh://…, git@host:owner/repo or a local path)")
			}
			changes["repo_url"], p.RepoURL = u, u
		}
		if patch.DefaultBranch != nil {
			b, err := text("default_branch", *patch.DefaultBranch, false, 200)
			if err != nil {
				return err
			}
			changes["default_branch"], p.DefaultBranch = b, b
		}
		action := "updated"
		if patch.Archived != nil {
			if *patch.Archived && p.ArchivedAt == nil {
				t := w.now
				p.ArchivedAt, action = &t, "archived"
			} else if !*patch.Archived && p.ArchivedAt != nil {
				p.ArchivedAt, action = nil, "unarchived"
			}
		}
		if err := w.st.UpdateProject(ctx, *p); err != nil {
			return err
		}
		out = p
		return w.activity(ctx, id, whoOf(m, a), models.TypeProject, action, id, changes)
	})
	return out, err
}

// --- members ---

// Members lists a Project's Members.
func (s *Service) Members(ctx context.Context, projectID string) ([]models.Member, error) {
	if _, err := s.st.GetProject(ctx, projectID); err != nil {
		return nil, err
	}
	return s.st.ListMembers(ctx, projectID)
}

// Join makes the acting Person a Member of the Project (idempotent).
func (s *Service) Join(ctx context.Context, a Actor, projectID string) (*models.Member, error) {
	var out *models.Member
	err := s.tx(ctx, func(w *work) error {
		if _, err := w.openProject(ctx, projectID); err != nil {
			return err
		}
		m, err := w.requireMember(ctx, a, projectID)
		out = m
		return err
	})
	return out, err
}

// NewMember describes a Member to add: a Person by name, or a Bot.
type NewMember struct {
	Kind         string `json:"kind"`
	DisplayName  string `json:"display_name"`
	Role         string `json:"role"`
	Instructions string `json:"instructions"`
	Agent        string `json:"agent"`
}

// AddMember adds a Person (by name, created if new) or a Bot to a Project.
// Adding a Person who is already a Member returns that Member.
func (s *Service) AddMember(ctx context.Context, a Actor, projectID string, in NewMember) (*models.Member, error) {
	n, err := name("display_name", in.DisplayName)
	if err != nil {
		return nil, err
	}
	kind := strings.ToLower(strings.TrimSpace(in.Kind))
	role := strings.ToLower(strings.TrimSpace(in.Role))
	instructions, err := text("instructions", in.Instructions, false, 4000)
	if err != nil {
		return nil, err
	}
	switch kind {
	case "", models.KindHuman:
		kind = models.KindHuman
		if role == "" {
			role = models.RoleMember
		}
		if role != models.RoleMember && role != models.RoleAdmin {
			return nil, invalid("a person's role must be member or admin")
		}
	case models.KindBot:
		if role == "" {
			return nil, invalid("a bot needs a role, such as builder or reviewer")
		}
		if models.HumanRole(role) {
			return nil, invalid("%q is a role for people, not bots", role)
		}
		if in.Agent = strings.ToLower(strings.TrimSpace(in.Agent)); !models.ValidAgent(in.Agent) {
			return nil, invalid("agent must be one of %s", strings.Join(models.Agents, ", "))
		}
	default:
		return nil, invalid("kind must be human or bot")
	}

	var out *models.Member
	err = s.tx(ctx, func(w *work) error {
		if _, err := w.openProject(ctx, projectID); err != nil {
			return err
		}
		actor, err := w.member(ctx, a, projectID, true)
		if err != nil {
			return err
		}
		if kind == models.KindBot {
			m := models.Member{ID: uuid.NewString(), ProjectID: projectID, Kind: kind, DisplayName: n,
				Role: role, Instructions: instructions, Agent: in.Agent, CreatedAt: w.now}
			if err := w.st.InsertMember(ctx, m); err != nil {
				return err
			}
			out = &m
		} else {
			person, err := w.st.UpsertPerson(ctx, n)
			if err != nil {
				return err
			}
			m, joined, err := w.st.JoinPerson(ctx, models.Member{ID: uuid.NewString(), ProjectID: projectID,
				PersonID: person.ID, DisplayName: person.Name, Role: role, CreatedAt: w.now})
			if err != nil {
				return err
			}
			out = m
			if !joined {
				return nil
			}
		}
		return w.activity(ctx, projectID, whoOf(actor, a), models.TypeMember, "added", out.ID,
			map[string]any{"kind": out.Kind, "role": out.Role, "name": out.DisplayName})
	})
	return out, err
}

// MemberPatch changes only the fields that are set. Agent applies to Bots.
type MemberPatch struct {
	DisplayName  *string `json:"display_name"`
	Instructions *string `json:"instructions"`
	Agent        *string `json:"agent"`
}

// UpdateMember renames a Member or changes a Bot's instructions and agent.
func (s *Service) UpdateMember(ctx context.Context, a Actor, id string, patch MemberPatch) (*models.Member, error) {
	var out *models.Member
	err := s.tx(ctx, func(w *work) error {
		m, err := w.st.GetMember(ctx, id)
		if err != nil {
			return err
		}
		if _, err := w.openProject(ctx, m.ProjectID); err != nil {
			return err
		}
		actor, err := w.member(ctx, a, m.ProjectID, true)
		if err != nil {
			return err
		}
		changes := map[string]any{}
		if patch.DisplayName != nil {
			if m.Kind == models.KindHuman {
				return invalid("a person's name is their own; rename the Person instead")
			}
			if m.DisplayName, err = name("display_name", *patch.DisplayName); err != nil {
				return err
			}
			changes["display_name"] = m.DisplayName
		}
		if patch.Instructions != nil {
			if m.Instructions, err = text("instructions", *patch.Instructions, false, 4000); err != nil {
				return err
			}
			changes["instructions"] = true
		}
		if patch.Agent != nil {
			ag := strings.ToLower(strings.TrimSpace(*patch.Agent))
			if m.Kind != models.KindBot {
				return invalid("only a bot runs as an agent")
			}
			if !models.ValidAgent(ag) {
				return invalid("agent must be one of %s", strings.Join(models.Agents, ", "))
			}
			m.Agent, changes["agent"] = ag, ag
		}
		if err := w.st.UpdateMember(ctx, *m); err != nil {
			return err
		}
		out = m
		return w.activity(ctx, m.ProjectID, whoOf(actor, a), models.TypeMember, "updated", m.ID, changes)
	})
	return out, err
}

// validRepoURL accepts URLs and scp-style addresses git can clone.
func validRepoURL(u string) bool {
	switch {
	case strings.HasPrefix(u, "https://"), strings.HasPrefix(u, "http://"), strings.HasPrefix(u, "ssh://"),
		strings.HasPrefix(u, "git://"), strings.HasPrefix(u, "file://"), strings.HasPrefix(u, "/"):
		return !strings.ContainsAny(u, " \t\n")
	}
	at, colon := strings.Index(u, "@"), strings.Index(u, ":")
	return at > 0 && colon > at && !strings.ContainsAny(u, " \t\n")
}

// --- channels ---

// Channels lists a Project's Channels; archived ones only on request.
func (s *Service) Channels(ctx context.Context, projectID string, includeArchived bool) ([]models.Channel, error) {
	if _, err := s.st.GetProject(ctx, projectID); err != nil {
		return nil, err
	}
	return s.st.ListChannels(ctx, projectID, includeArchived)
}

// CreateChannel adds a Channel to a Project.
func (s *Service) CreateChannel(ctx context.Context, a Actor, projectID, channelName string) (*models.Channel, error) {
	n, err := text("name", strings.TrimPrefix(strings.TrimSpace(channelName), "#"), true, 80)
	if err != nil {
		return nil, err
	}
	var out *models.Channel
	err = s.tx(ctx, func(w *work) error {
		if _, err := w.openProject(ctx, projectID); err != nil {
			return err
		}
		m, err := w.member(ctx, a, projectID, true)
		if err != nil {
			return err
		}
		ch := models.Channel{ID: uuid.NewString(), ProjectID: projectID, Name: n, CreatedAt: w.now}
		if err := w.st.InsertChannel(ctx, ch); err != nil {
			return err
		}
		out = &ch
		return w.activity(ctx, projectID, whoOf(m, a), models.TypeChannel, "created", ch.ID, map[string]any{"name": n})
	})
	return out, err
}

// ChannelPatch changes only the fields that are set.
type ChannelPatch struct {
	Name     *string `json:"name"`
	Archived *bool   `json:"archived"`
}

// UpdateChannel renames or archives/unarchives a Channel.
func (s *Service) UpdateChannel(ctx context.Context, a Actor, id string, patch ChannelPatch) (*models.Channel, error) {
	var out *models.Channel
	err := s.tx(ctx, func(w *work) error {
		ch, err := w.st.GetChannel(ctx, id)
		if err != nil {
			return err
		}
		if _, err := w.openProject(ctx, ch.ProjectID); err != nil {
			return err
		}
		m, err := w.member(ctx, a, ch.ProjectID, true)
		if err != nil {
			return err
		}
		changes := map[string]any{}
		if patch.Name != nil {
			n, err := text("name", strings.TrimPrefix(strings.TrimSpace(*patch.Name), "#"), true, 80)
			if err != nil {
				return err
			}
			changes["name"], ch.Name = n, n
		}
		action := "updated"
		if patch.Archived != nil {
			if *patch.Archived && ch.ArchivedAt == nil {
				t := w.now
				ch.ArchivedAt, action = &t, "archived"
			} else if !*patch.Archived && ch.ArchivedAt != nil {
				ch.ArchivedAt, action = nil, "unarchived"
			}
		}
		if err := w.st.UpdateChannel(ctx, *ch); err != nil {
			return err
		}
		out = ch
		return w.activity(ctx, ch.ProjectID, whoOf(m, a), models.TypeChannel, action, ch.ID, changes)
	})
	return out, err
}

// Activity returns one page of a Project's log, newest first.
func (s *Service) Activity(ctx context.Context, projectID, typ string, p store.Page) ([]models.Activity, bool, error) {
	if _, err := s.st.GetProject(ctx, projectID); err != nil {
		return nil, false, err
	}
	return s.st.ListActivity(ctx, projectID, typ, p)
}
