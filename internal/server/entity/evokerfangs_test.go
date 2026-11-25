package entity

import (
	"testing"

	"github.com/google/uuid"
)

// TestFangsAlliedSparesIllagersAndOwnVexes covers the alliance the bite checks.
func TestFangsAlliedSparesIllagersAndOwnVexes(t *testing.T) {
	evoker := NewMob(TypeEvoker, 1, 0, 64, 0, 1)
	own := NewMob(TypeVex, 2, 0, 64, 0, 2)
	own.SetVexOwnerUUID(uuid.UUID{1})
	stray := NewMob(TypeVex, 3, 0, 64, 0, 3)
	other := NewMob(TypeEvoker, 6, 0, 64, 0, 6)
	theirs := NewMob(TypeVex, 7, 0, 64, 0, 7)
	theirs.SetVexOwnerUUID(uuid.UUID{2})
	owners := map[uuid.UUID]*Mob{{1}: evoker, {2}: other}
	lookup := func(id uuid.UUID) AllianceMember { return AllianceMember{Mob: owners[id]} }
	for _, tc := range []struct {
		victim *Mob
		want   bool
	}{
		{NewMob(TypeVindicator, 4, 0, 64, 0, 4), true},
		{own, true},
		{stray, false},
		{theirs, true},
		{NewMob(TypeZombie, 5, 0, 64, 0, 5), false},
	} {
		if got := FangsAllied(AllianceMember{Mob: evoker}, AllianceMember{Mob: tc.victim}, lookup); got != tc.want {
			t.Errorf("FangsAllied(evoker, %s) = %v, want %v", tc.victim.Type, got, tc.want)
		}
	}
}
