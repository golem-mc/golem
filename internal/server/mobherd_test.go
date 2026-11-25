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
