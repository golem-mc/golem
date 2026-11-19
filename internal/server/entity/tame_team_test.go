package entity

import (
	"testing"

	"github.com/google/uuid"
)

// vanilla: TamableAnimal.getTeam, OwnableEntity.getRootOwner
func TestTameTeamResolvesRootOwnersAndCycles(t *testing.T) {
	pet := &Mob{Type: TypeWolf, Tamable: Tamable{Tame: true}}
	parent := &Mob{Type: TypeCat, Tamable: Tamable{Tame: true}}
	root := &Mob{Type: TypeZombie}
	parentID, rootID := uuid.New(), uuid.New()
	pet.Tamable.Owner, parent.Tamable.Owner = parentID, rootID
	lookup := func(id uuid.UUID) (*Mob, string) {
		switch id {
		case parentID:
			return parent, "intermediate"
		case rootID:
			return root, "root"
		}
		return nil, ""
	}
	if got := pet.TeamWithOwner("own", lookup); got != "own" {
		t.Fatalf("own team = %q, want own", got)
	}
	if got := pet.TeamWithOwner("", lookup); got != "root" {
		t.Fatalf("inherited team = %q, want root, skipping intermediate's team", got)
	}
	parent.Tamable.Owner = parentID
	if got := pet.TeamWithOwner("", lookup); got != "" {
		t.Fatalf("cyclic owner's team = %q, want none", got)
	}
	parent.Type, parent.Horse.Owner = TypeHorse, rootID
	if got := pet.TeamWithOwner("", lookup); got != "root" {
		t.Fatalf("horse owner's team = %q, want root", got)
	}
	pet.Tamable.Tame = false
	if got := pet.TeamWithOwner("", lookup); got != "" {
		t.Fatalf("wild animal's team = %q, want none", got)
	}
}
