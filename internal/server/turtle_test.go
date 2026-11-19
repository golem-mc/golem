package server

import (
	"testing"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/entity"
	"github.com/golem-mc/golem/nbt"
)

// TestATurtleKeepsItsHomeAndEggAcrossASave: Turtle saves home_pos and
// has_egg and reads them back.
func TestATurtleKeepsItsHomeAndEggAcrossASave(t *testing.T) {
	t.Parallel()
	m := entity.NewMob(entity.TypeTurtle, 1, 10.5, 64, 10.5, 1)
	m.Home = entity.Waypoint{X: 3, Y: 65, Z: -7}
	m.HasEgg = true
	tag := mobTag(&mobEntity{mob: m})
	if home, ok := nbt.Get[nbt.IntArray](tag, "home_pos"); !ok || len(home) != 3 || home[0] != 3 || home[1] != 65 || home[2] != -7 {
		t.Fatalf("saved home_pos %v, want [3 65 -7]", home)
	}
	back := entity.NewMob(entity.TypeTurtle, 2, 10.5, 64, 10.5, 2)
	applyMobTag(back, &mobEntity{mob: back}, tag)
	if back.Home != m.Home || !back.HasEgg {
		t.Errorf("loaded home %v egg %v, want %v and an egg", back.Home, back.HasEgg, m.Home)
	}
	bare := entity.NewMob(entity.TypeTurtle, 3, 10.5, 64, 10.5, 3)
	applyMobTag(bare, &mobEntity{mob: bare}, nbt.Compound{})
	if bare.Home.X != 10 || bare.Home.Z != 10 {
		t.Errorf("a turtle saved without a home stands home at %v, want the block it stands in", bare.Home)
	}
}

// TestATurtleTellsItsEggInItsMetadata: the has-egg and laying-egg booleans
// follow the turtle's state, fields 18 and 19.
func TestATurtleTellsItsEggInItsMetadata(t *testing.T) {
	t.Parallel()
	m := entity.NewMob(entity.TypeTurtle, 1, 0.5, 64, 0.5, 1)
	ent := &mobEntity{mob: m}
	was := mobMetadataStateOf(m)
	m.HasEgg, m.LayingEgg = true, true
	delta := mobMetadataDeltaPayload(ent, was)
	for _, index := range []byte{protocol.MetadataIndexTurtleHasEgg, protocol.MetadataIndexTurtleLayingEgg} {
		found := false
		for i := range delta {
			if delta[i] == index && i+2 < len(delta) && delta[i+1] == protocol.MetadataBoolean && delta[i+2] == 1 {
				found = true
			}
		}
		if !found {
			t.Errorf("metadata delta % x lacks the boolean at index %d", delta, index)
		}
	}
	if whole := mobMetadataPayload(ent); len(whole) <= len(mobMetadataPayload(&mobEntity{mob: entity.NewMob(entity.TypeTurtle, 2, 0.5, 64, 0.5, 1)})) {
		t.Error("the full metadata of an egg-carrying turtle is no longer than an empty one's")
	}
}
