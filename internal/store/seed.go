package store

import (
	"time"

	"github.com/codemodify/buildbee/internal/models"
	"github.com/google/uuid"
)

func seedHuman(projectID string, t time.Time) models.Member {
	return models.Member{
		ID: uuid.NewString(), ProjectID: projectID, Kind: "human",
		DisplayName: "You", Role: models.RoleOwner, CreatedAt: t,
	}
}

func seedBots(projectID string, t time.Time) []models.Member {
	bots := models.DefaultBots()
	out := make([]models.Member, 0, len(bots))
	for i, b := range bots {
		out = append(out, models.Member{
			ID: uuid.NewString(), ProjectID: projectID, Kind: "bot",
			DisplayName: b.Name, Role: b.Role, Instructions: b.Instructions,
			CreatedAt: t.Add(time.Duration(i+1) * time.Millisecond),
		})
	}
	return out
}

func pulseID(bots []models.Member) string {
	if m := models.MemberByRole(bots, models.RolePulse); m != nil {
		return m.ID
	}
	if len(bots) > 0 {
		return bots[len(bots)-1].ID
	}
	return ""
}
