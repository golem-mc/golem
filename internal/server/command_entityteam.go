package server

import (
	"github.com/golem-mc/golem/internal/server/entity"
	"github.com/google/uuid"
)

// Team is the body's effective team at the time the command reads it.
// vanilla: TamableAnimal.getTeam
func (e *entitySnapshot) Team() string {
	s := e.srv
	team, _ := s.scoreboard.PlayerTeam(e.uuid.String())
	ent, ok := e.ref.(*mobEntity)
	if !ok || team != "" {
		return team
	}
	var ownerID uuid.UUID
	s.mobsMu.Lock()
	team = ent.mob.TeamWithOwner(team, func(id uuid.UUID) (*entity.Mob, string) {
		for _, dim := range s.allDims() {
			if owner := dim.mobs.byUUID[id]; owner != nil {
				ownTeam, _ := s.scoreboard.PlayerTeam(dim.mobs.book[owner].uuidText)
				return owner, ownTeam
			}
		}
		ownerID = id
		return nil, ""
	})
	s.mobsMu.Unlock()
	// Resolve players outside mobsMu: the connection map has its own lock.
	if ownerID != uuid.Nil {
		if owner := s.clientByUUID(ownerID); owner != nil {
			team, _ = s.scoreboard.PlayerTeam(owner.player.Name)
		}
	}
	return team
}
