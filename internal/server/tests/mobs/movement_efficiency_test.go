package mobs

import (
	"fmt"
	"testing"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/servertest"
)

// A mob's movement_efficiency attribute lerps the block speed factor to 1.
// vanilla: LivingEntity.getBlockSpeedFactor
func TestMobMovementEfficiencyCancelsSoulSandSlowdown(t *testing.T) {
	t.Parallel()
	srv, c := servertest.Join(t, servertest.MobTestConfig(), "Keeper", servertest.OpsListing(t, "Keeper"))
	t.Cleanup(srv.HoldChunk(5, 5))
	servertest.HoldTicks(t, srv)
	c.Settle()

	soul := servertest.BlockState(t, "minecraft:soul_sand")
	stone := servertest.BlockState(t, "minecraft:stone")
	air := servertest.BlockState(t, "minecraft:air")
	// Rows of z: 1 stone, 4 soul sand with base 0, 7 soul sand with base 1.
	var ids [3]int32
	for i, r := range []struct {
		z    int32
		soul bool
		base string
	}{{1, false, "1"}, {4, true, "0"}, {7, true, "1"}} {
		for x := int32(3); x <= 12; x++ {
			if r.soul {
				srv.SetBlock(x, 63, r.z, soul)
			} else {
				srv.SetBlock(x, 63, r.z, stone)
			}
			srv.SetBlock(x, 64, r.z, air)
			srv.SetBlock(x, 65, r.z, air)
		}
		id := srv.SpawnMob("minecraft:zombie", 4.5, 64, float64(r.z)+0.5)
		c.ExpectMobSpawn(protocol.EntityTypeZombie)
		ids[i] = id
		tag := fmt.Sprintf("row%d", i)
		c.ChatCommand(fmt.Sprintf("tag @e[type=zombie,x=4.5,y=64,z=%d.5,distance=..0.5] add %s", r.z, tag))
		c.ChatCommand(fmt.Sprintf("attribute @e[tag=%s,limit=1] minecraft:movement_speed base set 0", tag))
		c.ChatCommand(fmt.Sprintf("attribute @e[tag=%s,limit=1] minecraft:movement_efficiency base set %s", tag, r.base))
		c.ChatCommand(fmt.Sprintf("data merge entity @e[tag=%s,limit=1] {NoAI:0b,Motion:[0.3d,0d,0d]}", tag))
	}
	c.Settle()
	servertest.RunTicks(t, srv, 3)
	var dist [3]float64
	for i, id := range ids {
		x, _, _, ok := srv.MobPos(id)
		if !ok {
			t.Fatal("zombie disappeared")
		}
		dist[i] = x - 4.5
	}
	if dist[0] <= 0.3 {
		t.Fatalf("stone zombie moved %g", dist[0])
	}
	if d := dist[2] - dist[0]; d > 1e-4 || d < -1e-4 {
		t.Errorf("efficiency 1 on soul sand moved %g, stone %g", dist[2], dist[0])
	}
	if dist[1] >= dist[0]*0.8 {
		t.Errorf("efficiency 0 on soul sand moved %g, stone %g", dist[1], dist[0])
	}
}
