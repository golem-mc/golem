package mobs

import (
	"github.com/golem-mc/golem/internal/server/servertest"

	"testing"

	"github.com/golem-mc/golem/internal/protocol"
)

// TestMobSilentFlagReachesTheClient checks a Silent mob is told as index 4 at
// spawn and that a /data merge clearing it is told as false.
// vanilla: Entity.DATA_SILENT
func TestMobSilentFlagReachesTheClient(t *testing.T) {
	t.Parallel()
	c := servertest.DataWorld(t, "Hush")
	c.ChatCommand(`summon minecraft:pig 4.5 70 4.5 {Silent:1b}`)
	spawn := c.AwaitSpawn(protocol.EntityTypePig)
	if m := servertest.MetaOf(c.AwaitMetadataFor(spawn.EntityID), protocol.MetadataIndexSilent); m == nil || !m.FlagVal {
		t.Fatalf("the spawn's silent metadata = %+v, want the flag set", m)
	}
	c.ChatCommand(`data merge entity @e[limit=1,sort=nearest,type=minecraft:pig] {Silent:0b}`)
	for {
		m := servertest.MetaOf(c.AwaitMetadataFor(spawn.EntityID), protocol.MetadataIndexSilent)
		if m == nil {
			continue
		}
		if m.FlagVal {
			t.Fatal("clearing Silent told the flag as still set")
		}
		break
	}
	c.Disconnect()
}
