package server

import (
	"testing"

	"github.com/golem-mc/golem/internal/server/entity"
)

func TestMobHerdIllagerCount(t *testing.T) {
	var h mobHerd
	pig := &entity.Mob{Type: entity.TypePig}
	witch := &entity.Mob{Type: entity.TypeWitch}
	pillager := &entity.Mob{Type: entity.TypePillager}
	vindicator := &entity.Mob{Type: entity.TypeVindicator}

	check := func(want int32) {
		t.Helper()
		if h.illagers != want {
			t.Fatalf("herd with %d mobs has illager count %d, want %d", len(h.herd), h.illagers, want)
		}
	}

	h.add(pig, witch)
	check(0)
	h.add(pillager, vindicator)
	check(2)
	h.remove(pillager)
	check(1)
	h.remove(vindicator)
	check(0)
	h.setHerd(entity.Mobs{pig, pillager, vindicator})
	check(2)
	h.setHerd(entity.Mobs{witch})
	check(0)
}

func TestMobHerdColliderCount(t *testing.T) {
	var h mobHerd
	pig := &entity.Mob{Type: entity.TypePig}
	shulker := &entity.Mob{Type: entity.TypeShulker}
	ghast := &entity.Mob{Type: entity.TypeHappyGhast}

	check := func(want int32) {
		t.Helper()
		if h.colliders != want {
			t.Fatalf("herd with %d mobs has collider count %d, want %d", len(h.herd), h.colliders, want)
		}
	}

	h.add(pig, shulker, ghast)
	check(2)
	h.remove(shulker)
	check(1)
	h.remove(ghast)
	check(0)
	h.setHerd(entity.Mobs{pig, shulker, ghast})
	check(2)
	h.setHerd(entity.Mobs{pig})
	check(0)
}
