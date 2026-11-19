package mobs

import (
	"fmt"
	"math"
	"testing"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/player"
	"github.com/golem-mc/golem/internal/server/servertest"
)

// TestTemptRangeOnEveryAnimal reads tempt_range off kinds built on
// Animal.createAnimalAttributes, then lures a cow from beyond the default
// range once its tempt_range is raised.
func TestTemptRangeOnEveryAnimal(t *testing.T) {
	t.Parallel()
	srv, c := servertest.Join(t, servertest.MobTestConfig(), "Herder", servertest.OpsListing(t, "Herder"))

	pos := srv.PlayerPosition("Herder")
	for i, kind := range []string{"wolf", "bee", "horse", "llama", "camel"} {
		c.ChatCommand(fmt.Sprintf("summon minecraft:%s %.1f %.1f %.1f {NoAI:1b}", kind, pos.X-6, pos.Y, pos.Z+float64(3*i)))
		if got := mobAttribute(t, c, kind, "minecraft:tempt_range"); got != 10 {
			t.Errorf("%s tempt_range = %v, want 10", kind, got)
		}
	}

	srv.GiveSlot("Herder", player.HotbarSlot, protocol.ItemStack{ItemID: servertest.ItemIDOrDie(t, "minecraft:wheat"), Count: 1})
	servertest.HoldTicks(t, srv)
	cowID := srv.SpawnMob("minecraft:cow", pos.X+15, pos.Y, pos.Z)
	c.ExpectMobSpawn(protocol.EntityTypeCow)
	c.ChatCommand("attribute @e[type=cow,limit=1] minecraft:tempt_range base set 20")
	c.Settle()
	servertest.UntilTicks(t, srv, 300, "the cow comes to the wheat 15 blocks off", func() bool {
		x, _, z, ok := srv.MobPos(cowID)
		return ok && math.Hypot(x-pos.X, z-pos.Z) < 5
	})
}
