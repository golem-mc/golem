package mobs

import (
	"math"
	"testing"

	"github.com/golem-mc/golem/internal/config"
	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/servertest"
	"github.com/golem-mc/golem/internal/server/world"
	"github.com/golem-mc/golem/nbt"
	"github.com/golem-mc/golem/registry"
)

func TestPatrolSpawnerAndLongDistanceWalk(t *testing.T) {
	t.Parallel()
	cfg := servertest.MobTestConfig()
	cfg.GameMode = "creative"
	// The captain starts beyond the 32-block tracking radius of view distance two.
	cfg.ViewDistance, cfg.SimulationDistance = 3, config.MinDistance
	srv, c := servertest.Join(t, cfg, "Patrol", servertest.OpsListing(t, "Patrol"))
	c.MoveToGround(0.5, 64, 0.5, true)
	c.Settle()
	servertest.HoldTicks(t, srv)
	srv.SetDifficulty(2)
	// Noon of day six: patrols stay off until the overworld clock reaches 120000.
	c.ChatCommand("time set 126000")
	c.Settle()
	// Seed 9 spawns at (-29, -26); hold its ten-block spawn margin.
	// The 200-tick walk heads east through the player's streamed chunks.
	w := srv.OverworldWorld()
	for x := int32(-3); x <= -2; x++ {
		for z := int32(-3); z <= -1; z++ {
			pos := world.ChunkPos{X: x, Z: z}
			w.Acquire(pos)
			t.Cleanup(func() { w.Release(pos) })
		}
	}
	c.ChatCommand("gamerule spawn_patrols false")
	c.Settle()
	srv.PatrolSpawnerPass(9)
	if ids := srv.MobIDsOfKind("minecraft:pillager"); len(ids) != 0 {
		t.Fatalf("rule disabled: %d pillagers", len(ids))
	}
	c.ChatCommand("gamerule spawn_patrols true")
	c.Settle()
	c.ChatCommand("gamerule spawn_mobs false")
	c.ChatCommand("difficulty easy")
	c.ChatCommand("difficulty normal")
	c.ChatCommand("gamerule spawn_mobs true")
	c.Settle()
	srv.PatrolSpawnerPass(9)
	if ids := srv.MobIDsOfKind("minecraft:pillager"); len(ids) != 0 {
		t.Fatalf("enemy spawning latched off: %d pillagers", len(ids))
	}
	c.ChatCommand("gamerule spawn_monsters false")
	c.ChatCommand("gamerule spawn_monsters true")
	c.Settle()
	srv.PatrolSpawnerPass(9)
	ids := srv.MobIDsOfKind("minecraft:pillager")
	// Normal difficulty on a new world: ceil(1.5)+1.
	if len(ids) != 3 {
		t.Fatalf("patrol size %d, want 3", len(ids))
	}
	var leader int32
	banners := map[int32]bool{}
	c.SettleAndCollect(func(pid int32, data []byte) {
		if pid != protocol.IDPlayEntityEquipment {
			return
		}
		r := protocol.NewReader(data)
		id := r.ReadVarInt()
		for {
			slot := r.ReadU8()
			stack, ok := r.ReadSlot()
			if !ok {
				t.Fatalf("equipment: %v", r.Err())
			}
			if slot&0x7f == protocol.EquipHead && stack.ItemID == int32(registry.ItemWhiteBanner) {
				banners[id] = true
			}
			if slot&0x80 == 0 {
				break
			}
		}
	})
	for _, id := range ids {
		if srv.MobHeadItem(id) == int32(registry.ItemWhiteBanner) {
			if leader != 0 {
				t.Fatal("two captains")
			}
			leader = id
		}
	}
	if leader == 0 || !banners[leader] || len(banners) != 1 {
		t.Fatalf("leader %d, banner equipment packets %v", leader, banners)
	}
	lx, ly, lz, _ := srv.MobPos(leader)
	if ly != 64 || math.Abs(lx) < 24 || math.Abs(lx) > 47 || math.Abs(lz) < 24 || math.Abs(lz) > 47 {
		t.Fatalf("captain at %v %v %v", lx, ly, lz)
	}
	for _, id := range ids {
		identity, _ := srv.MobUUID(id)
		tag := entityCompound(t, c, identity)
		if !nbt.BoolField(tag, "Patrolling") {
			t.Fatalf("member %d is not patrolling: %v", id, tag)
		}
		if id == leader && !nbt.BoolField(tag, "PatrolLeader") {
			t.Fatal("banner wearer is not saved as leader")
		}
	}
	tag := entityCompound(t, c, "@e[type=minecraft:pillager,nbt={PatrolLeader:1b},limit=1]")
	target, ok := tag["patrol_target"].(nbt.IntArray)
	if !ok || len(target) != 3 {
		t.Fatalf("patrol target: %v", tag)
	}
	before := math.Hypot(lx-float64(target[0]), lz-float64(target[2]))
	for range 20 {
		servertest.RunTicks(t, srv, 10)
		lx, _, lz, _ = srv.MobPos(leader)
		for _, id := range ids {
			x, _, z, ok := srv.MobPos(id)
			if !ok || math.Abs(x-lx) > 16 || math.Abs(z-lz) > 16 {
				t.Fatalf("member %d at %v,%v, leader %v,%v", id, x, z, lx, lz)
			}
		}
	}
	after := math.Hypot(lx-float64(target[0]), lz-float64(target[2]))
	t.Logf("distance to target over 200 ticks: %.2f -> %.2f", before, after)
	if after >= before-5 {
		t.Fatalf("distance to patrol target: before %.2f after %.2f", before, after)
	}
}
