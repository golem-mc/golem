package entity

import (
	"slices"
	"testing"

	"github.com/golem-mc/golem/registry"
)

func TestMobTargetKindsMatchTheirPredicates(t *testing.T) {
	for _, tc := range []struct {
		name    string
		kinds   []MobType
		matches func(MobType) bool
	}{
		{"skeleton", skeletonKinds, isSkeletonKind},
		{"illager", illagerKinds, IsIllagerType},
		{"raider", raiderKinds, isRaiderType},
		{"healable raider", witchRaiderKinds, func(kind MobType) bool { return isRaiderType(kind) && kind != TypeWitch }},
		{"schooling fish", schoolingFishKinds, func(t MobType) bool { return FishKind(t) && t != TypePufferfish }},
		{"piglin", piglinKinds, piglinKind},
		{"villager", abstractVillagerKinds, isAbstractVillager},
	} {
		t.Run(tc.name, func(t *testing.T) {
			for id := range registry.EntityTypeCount {
				kind := MobType(id)
				if got, want := slices.Contains(tc.kinds, kind), tc.matches(kind); got != want {
					t.Errorf("membership of kind %d = %t, predicate = %t", kind, got, want)
				}
			}
		})
	}
}

func TestMobTargetSkipsAbsentKindsAndStillChecksPlayers(t *testing.T) {
	hunter := NewMob(TypeWolf, 1, 0.5, 64, 0.5, 1)
	cow := NewMob(TypeCow, 2, 1.5, 64, 0.5, 2)
	sheep := NewMob(TypeSheep, 3, 2.5, 64, 0.5, 3)
	view := &tickView{
		w:       newTestWorld(),
		others:  []*Mob{hunter, cow},
		players: []Player{{EntityID: 4, X: 4.5, Y: 64, Z: 0.5, Huntable: true}},
	}
	checks := 0
	goal := &mobTargetGoal{
		m: hunter, kinds: []MobType{TypeRabbit, TypeSheep}, eager: true, unsighted: true, players: true,
		matches: func(o *Mob) bool {
			checks++
			return o.Type == TypeRabbit || o.Type == TypeSheep
		},
	}
	grid := takePushGrid(3, nil)
	defer grid.release()
	grid.build(view.others)
	hunter.push = grid
	if !goal.CanStart(view) || hunter.TargetID != view.players[0].EntityID {
		t.Fatal("the hunter did not target the player when no prey kind was in the herd")
	}
	if checks != 0 {
		t.Errorf("the target predicate ran %d times with no prey kind in the herd", checks)
	}

	view.others = append(view.others, sheep)
	nextGrid := takePushGrid(len(view.others), nil)
	defer nextGrid.release()
	nextGrid.build(view.others)
	for _, grid := range []*pushGrid{nextGrid, nil} {
		hunter.push = grid
		hunter.SetMetadataTargetID(0)
		if !goal.CanStart(view) || hunter.TargetID != sheep.ID {
			t.Errorf("the hunter targeted %d instead of the closer sheep %d (grid present: %t)", hunter.TargetID, sheep.ID, grid != nil)
		}
	}
}
