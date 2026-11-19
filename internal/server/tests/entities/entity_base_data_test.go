package entities

import (
	"strings"
	"testing"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/servertest"
)

// TestEntityBaseFieldsSurviveOnNonMobBodies summons bodies with custom data,
// Air, fall_distance and a pitch and reads each back through /data; an end
// crystal's spawn metadata carries its custom name.
// vanilla: Entity.load, Entity.saveWithoutId
func TestEntityBaseFieldsSurviveOnNonMobBodies(t *testing.T) {
	t.Parallel()
	srv, c := servertest.Join(t, servertest.MobTestConfig(), "Summoner", servertest.OpsListing(t, "Summoner"))
	servertest.HoldTicks(t, srv)

	const base = `data:{k:1},Air:150s,fall_distance:2.5d,Rotation:[45f,10f],NoGravity:1b`
	for _, body := range []struct {
		kind, extra string
		typ         int32
		pitch       bool
	}{
		{"oak_boat", "", protocol.EntityTypeOakBoat, true},
		{"minecart", "", protocol.EntityTypeMinecart, true},
		{"arrow", "", protocol.EntityTypeArrow, false},
		{"egg", "", protocol.EntityTypeEgg, false},
		{"experience_orb", "", protocol.EntityTypeExperienceOrb, true},
		{"armor_stand", ",active_effects:[{id:\"minecraft:speed\",duration:100}]", protocol.EntityTypeArmorStand, true},
		{"end_crystal", `,CustomName:"N",CustomNameVisible:1b`, protocol.EntityTypeEndCrystal, true},
	} {
		c.ChatCommand("summon minecraft:" + body.kind + " 0.5 64 2.5 {" + base + body.extra + "}")
		spawn := c.AwaitSpawn(body.typ)
		meta := c.AwaitMetadataFor(spawn.EntityID)
		if body.kind == "end_crystal" {
			if servertest.MetaOf(meta, protocol.MetadataIndexCustomName) == nil {
				t.Errorf("the end crystal's spawn carries no custom name")
			}
			if e := servertest.MetaOf(meta, protocol.MetadataIndexCustomNameVisible); e == nil || !e.FlagVal {
				t.Errorf("the end crystal's name-visible entry = %+v, want true", e)
			}
		}
		compound := awaitEntityDataCompound(t, srv, c, "@e[type=minecraft:"+body.kind+",limit=1]")
		for _, want := range []string{"data: {k: 1}", "Air: 150s", "fall_distance: 2.5d"} {
			if !strings.Contains(compound, want) {
				t.Errorf("%s: /data get entity = %s, want %s", body.kind, compound, want)
			}
		}
		if body.pitch {
			if got := awaitEntityDataCompound(t, srv, c, "@e[type=minecraft:"+body.kind+",limit=1]", "Rotation[1]"); !strings.Contains(got, "10.0f") {
				t.Errorf("%s: Rotation[1] = %s, want 10.0f", body.kind, got)
			}
		}
		if body.kind == "armor_stand" && !strings.Contains(compound, "active_effects") {
			t.Errorf("armor_stand: /data get entity = %s, want active_effects", compound)
		}
		c.ChatCommand("kill @e[type=minecraft:" + body.kind + "]")
	}
}
