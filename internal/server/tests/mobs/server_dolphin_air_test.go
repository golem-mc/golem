package mobs

import (
	"fmt"
	"testing"

	"github.com/golem-mc/golem/internal/server/servertest"
	"github.com/golem-mc/golem/nbt"
	"github.com/golem-mc/golem/registry"
)

func TestDolphinAirBranches(t *testing.T) {
	t.Parallel()
	srv, c := servertest.Join(t, servertest.MobTestConfig(), "Keeper", servertest.OpsListing(t, "Keeper"))
	servertest.HoldTicks(t, srv)
	release := srv.HoldChunk(5, 5)
	t.Cleanup(release)
	for i, tc := range []struct {
		name                string
		block               registry.Block
		air, ticks, wantAir int
		wantHealth          float64
		effects             []string
		noAI                bool
	}{
		{name: "ordinary breath", block: registry.BlockWater, air: 1, ticks: 1, wantAir: 0, wantHealth: 10},
		{name: "drowning", block: registry.BlockWater, air: 1, ticks: 21, wantAir: 0, wantHealth: 8},
		{name: "long breath", block: registry.BlockWater, air: 4800, ticks: 1, wantAir: 4799, wantHealth: 10},
		{name: "surfacing", block: registry.BlockAir, air: 1000, ticks: 1, wantAir: 4800, wantHealth: 10},
		{name: "bubble column", block: registry.BlockBubbleColumn, air: 1000, ticks: 1, wantAir: 4800, wantHealth: 10},
		{name: "water breathing", block: registry.BlockWater, air: 1000, ticks: 1, wantAir: 4800, wantHealth: 10, effects: []string{"water_breathing"}},
		{name: "conduit power", block: registry.BlockWater, air: 1000, ticks: 1, wantAir: 4800, wantHealth: 10, effects: []string{"conduit_power"}},
		{name: "nautilus freezes breath", block: registry.BlockWater, air: 1000, ticks: 1, wantAir: 1000, wantHealth: 10, effects: []string{"breath_of_the_nautilus"}},
		{name: "water breathing with nautilus", block: registry.BlockWater, air: 1000, ticks: 1, wantAir: 4800, wantHealth: 10, effects: []string{"breath_of_the_nautilus", "water_breathing"}},
		{name: "conduit with nautilus", block: registry.BlockWater, air: 1000, ticks: 1, wantAir: 4800, wantHealth: 10, effects: []string{"breath_of_the_nautilus", "conduit_power"}},
		{name: "surfacing with nautilus", block: registry.BlockAir, air: 1000, ticks: 1, wantAir: 4800, wantHealth: 10, effects: []string{"breath_of_the_nautilus"}},
		{name: "overfull breath", block: registry.BlockAir, air: 5000, ticks: 1, wantAir: 5000, wantHealth: 10},
		{name: "no AI", block: registry.BlockWater, air: 1, ticks: 1, wantAir: 4800, wantHealth: 10, noAI: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			selector := fmt.Sprintf("@e[tag=breath%d,limit=1]", i)
			// A roof keeps the breathing goal from escaping before the drowning bite.
			for x := int32(4); x <= 6; x++ {
				for z := int32(4); z <= 6; z++ {
					for y := int32(63); y <= 68; y++ {
						block := tc.block
						if x == 4 || x == 6 || z == 4 || z == 6 || y == 63 || y == 68 {
							block = registry.BlockStone
						}
						srv.SetBlock(x, y, z, block.DefaultState())
					}
				}
			}
			c.ChatCommand(fmt.Sprintf(`summon minecraft:dolphin 5.5 64 5.5 {Air:%d,NoGravity:1b,NoAI:%t,PersistenceRequired:1b,Tags:["breath%d"]}`, tc.air, tc.noAI, i))
			id := c.ExpectSummonedMob(int32(registry.EntityTypeDolphin))
			for _, name := range tc.effects {
				c.ChatCommand("effect give " + selector + " minecraft:" + name + " infinite")
			}
			c.Settle()
			servertest.RunTicks(t, srv, tc.ticks)
			tag, ok := srv.MobSaveTag(id)
			if !ok {
				t.Fatal("dolphin disappeared")
			}
			air, _ := nbt.IntField(tag, "Air")
			health, _ := srv.MobHealth(id)
			if int(air) != tc.wantAir || health != tc.wantHealth {
				t.Fatalf("Air=%d Health=%g, want %d/%g", air, health, tc.wantAir, tc.wantHealth)
			}
			c.ChatCommand("kill " + selector)
			c.Settle()
		})
	}
}
