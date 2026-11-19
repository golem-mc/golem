package mobs

import (
	"fmt"
	"testing"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/servertest"
	"github.com/golem-mc/golem/nbt"
	"github.com/golem-mc/golem/registry"
)

func TestMobOxygenBonus(t *testing.T) {
	t.Parallel()
	srv, c := servertest.Join(t, servertest.MobTestConfig(), "Watcher", servertest.OpsListing(t, "Watcher"))
	servertest.HoldTicks(t, srv)
	release := srv.HoldChunk(5, 5)
	t.Cleanup(release)
	for x := int32(4); x <= 6; x++ {
		for y := int32(64); y <= 67; y++ {
			for z := int32(4); z <= 6; z++ {
				srv.SetBlock(x, y, z, registry.BlockWater.DefaultState())
			}
		}
	}
	for _, tc := range []struct {
		bonus, air int
	}{{0, 0}, {1024, 99}} {
		t.Run(fmt.Sprint(tc.bonus), func(t *testing.T) {
			c.ChatCommand(fmt.Sprintf(`summon minecraft:pig 5.5 64 5.5 {NoAI:1b,NoGravity:1b,PersistenceRequired:1b,Air:100s,Tags:["oxygen%d"]}`, tc.bonus))
			id := c.ExpectSummonedMob(protocol.EntityTypePig)
			c.ChatCommand(fmt.Sprintf("attribute @e[tag=oxygen%d,limit=1] minecraft:oxygen_bonus base set %d", tc.bonus, tc.bonus))
			c.Settle()
			// Seed 1 spends one air in 100 ticks at bonus 1024, including
			// the idle-sound draws on the same mob stream.
			srv.SeedMob(id, 1)
			servertest.RunTicks(t, srv, 100)
			tag, ok := srv.MobSaveTag(id)
			if !ok {
				t.Fatal("pig disappeared")
			}
			if got, ok := nbt.IntField(tag, "Air"); !ok || int(got) != tc.air {
				t.Fatalf("oxygen bonus %d: Air after 100 ticks = %d, want %d", tc.bonus, got, tc.air)
			}
		})
	}
}
