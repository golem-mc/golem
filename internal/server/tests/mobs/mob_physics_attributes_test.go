package mobs

import (
	"testing"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/servertest"
)

func TestMobGravityAttributeControlsFall(t *testing.T) {
	t.Parallel()
	srv, c := servertest.Join(t, servertest.MobTestConfig(), "Keeper", servertest.OpsListing(t, "Keeper"))
	servertest.HoldTicks(t, srv)
	c.ChatCommand(`summon minecraft:pig 1.5 100 0.5 {Tags:["slow"]}`)
	slow := c.ExpectMobSpawn(protocol.EntityTypePig)
	c.ChatCommand(`summon minecraft:pig 4.5 100 0.5`)
	normal := c.ExpectMobSpawn(protocol.EntityTypePig)
	c.ChatCommand("attribute @e[tag=slow,limit=1] minecraft:gravity base set 0.01")
	c.Settle()
	servertest.RunTicks(t, srv, 10)
	for _, tc := range []struct {
		id   int32
		slow bool
	}{{slow.EntityID, true}, {normal.EntityID, false}} {
		_, y, _, ok := srv.MobPos(tc.id)
		if !ok {
			t.Fatal("pig disappeared")
		}
		fallen := 100 - y
		if tc.slow && fallen >= 1 || !tc.slow && fallen <= 3 {
			t.Errorf("slow=%v: pig fell %g blocks in ten ticks", tc.slow, fallen)
		}
	}
}
