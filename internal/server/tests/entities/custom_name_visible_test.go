package entities

import (
	"strings"
	"testing"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/servertest"
)

// TestASummonedArmorStandAndMinecartShowTheirNamesAlways summons a floating-text
// stand and a named cart with CustomNameVisible set: each spawn's metadata
// carries the flag, and the stand's save keeps it.
// vanilla: Entity.load, Entity.saveWithoutId
func TestASummonedArmorStandAndMinecartShowTheirNamesAlways(t *testing.T) {
	t.Parallel()
	srv, c := servertest.Join(t, servertest.MobTestConfig(), "Summoner", servertest.OpsListing(t, "Summoner"))

	for _, body := range []struct {
		command string
		typ     int32
	}{
		{`summon minecraft:armor_stand 0.5 64 2.5 {CustomName:"Hi",CustomNameVisible:1b,Invisible:1b}`, protocol.EntityTypeArmorStand},
		{`summon minecraft:minecart 2.5 64 2.5 {CustomName:"Cart",CustomNameVisible:1b}`, protocol.EntityTypeMinecart},
	} {
		c.ChatCommand(body.command)
		spawn := c.AwaitSpawn(body.typ)
		meta := c.AwaitMetadataFor(spawn.EntityID)
		if e := servertest.MetaOf(meta, protocol.MetadataIndexCustomNameVisible); e == nil || !e.FlagVal {
			t.Errorf("%s: the spawn's name-visible entry = %+v, want true", body.command, e)
		}
		if servertest.MetaOf(meta, protocol.MetadataIndexCustomName) == nil {
			t.Errorf("%s: the spawn carries no custom name", body.command)
		}
	}

	if compound := awaitEntityDataCompound(t, srv, c, "@e[type=minecraft:armor_stand,limit=1]"); !strings.Contains(compound, "CustomNameVisible: 1b") {
		t.Errorf("/data get entity of the stand = %s, want CustomNameVisible: 1b", compound)
	}
}
