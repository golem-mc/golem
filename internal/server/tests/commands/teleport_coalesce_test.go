package commands

import (
	"testing"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/command"
	"github.com/golem-mc/golem/internal/server/player"
	"github.com/golem-mc/golem/internal/server/servertest"
	"github.com/golem-mc/golem/internal/server/world"
)

// TestTeleportsAreToldOncePerTick checks that teleports of an armor stand
// and a mob reach a watcher through the tracker's own send: twenty that
// leave the body where it stood send no Teleport Entity, and twenty to new
// places in one tick send one, carrying the last place. A frozen world's
// tracking tells them the same way.
func TestTeleportsAreToldOncePerTick(t *testing.T) {
	t.Parallel()
	for _, frozen := range []bool{false, true} {
		name := "running"
		if frozen {
			name = "frozen"
		}
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			testTeleportsToldOncePerTick(t, frozen)
		})
	}
}

func testTeleportsToldOncePerTick(t *testing.T, frozen bool) {
	srv, c := servertest.Join(t, servertest.MobTestConfig(), "Testificate", servertest.OpsListing(t, "Testificate"))
	c.MoveToGround(0.5, 64, 0.5, true)
	c.Settle()

	c.ChatCommand("summon minecraft:armor_stand 2.5 64 4.5 {NoGravity:1b}")
	stand := c.ExpectArmorStandSpawn()
	c.ChatCommand("summon minecraft:cow 4.5 64 2.5 {NoAI:1b}")
	cow := c.ExpectMobSpawn(protocol.EntityTypeCow)

	servertest.HoldTicks(t, srv)
	if frozen {
		srv.RunLine("tick freeze")
	}
	// The summons' own first movement ticks pass before anything is counted.
	c.ReadTicks(srv, 20, func(int32, []byte) bool { return false })

	ids := map[int32]bool{stand.EntityID: true, cow.EntityID: true}
	selected := append(srv.SelectedEntities("minecraft:armor_stand"), srv.SelectedEntities("minecraft:cow")...)
	if len(selected) != 2 {
		t.Fatalf("selected %d bodies, want the stand and the cow", len(selected))
	}

	// A command teleport sets a mob's ground flag, which is itself a change
	// to tell; let that one pass before counting.
	for _, e := range selected {
		e.Teleport(command.Move{Pos: e.Position()}, world.DimensionName, "test")
	}
	c.ReadTicks(srv, 10, func(int32, []byte) bool { return false })

	// Twenty teleports onto the spot each body already stands on.
	for _, e := range selected {
		for range 20 {
			e.Teleport(command.Move{Pos: e.Position()}, world.DimensionName, "test")
		}
	}
	c.ExpectNoneWithinTicks(srv, 10, func(id int32, data []byte) bool {
		return id == protocol.IDPlayTeleportEntity && ids[protocol.NewReader(data).ReadVarInt()]
	})

	// Twenty teleports to new places in one tick.
	for _, e := range selected {
		at := e.Position()
		for i := range 20 {
			e.Teleport(command.Move{Pos: player.Position{X: at.X + float64(i+1)*0.25, Y: at.Y, Z: at.Z}},
				world.DimensionName, "test")
		}
	}
	told := map[int32]int{}
	c.ReadTicks(srv, 10, func(id int32, data []byte) bool {
		if id != protocol.IDPlayTeleportEntity {
			return false
		}
		r := protocol.NewReader(data)
		eid := r.ReadVarInt()
		if !ids[eid] {
			return false
		}
		told[eid]++
		r.ReadVarInt() // position path
		x := r.ReadF64()
		want := stand.X + 5
		if eid == cow.EntityID {
			want = cow.X + 5
		}
		if x != want {
			t.Errorf("entity %d told at x %v, want the last teleport's %v", eid, x, want)
		}
		return false
	})
	for id := range ids {
		if told[id] != 1 {
			t.Errorf("entity %d was sent %d Teleport Entity packets for twenty teleports in one tick, want 1", id, told[id])
		}
	}

	// A step below the movement threshold, and a turn on the spot, are each
	// still a teleport the watcher is told of.
	for _, step := range []struct {
		dx  float64
		yaw float32
	}{{dx: 1.0 / 64}, {dx: 1.0 / 64, yaw: 90}} {
		for _, e := range selected {
			at := e.Position()
			at.X += 5 + step.dx
			at.Yaw = step.yaw
			e.Teleport(command.Move{Pos: at}, world.DimensionName, "test")
		}
		told := map[int32]int{}
		c.ReadTicks(srv, 10, func(id int32, data []byte) bool {
			if id == protocol.IDPlayTeleportEntity {
				told[protocol.NewReader(data).ReadVarInt()]++
			}
			return false
		})
		for id := range ids {
			if told[id] != 1 {
				t.Errorf("entity %d was sent %d Teleport Entity packets for a teleport by %v turning to %v, want 1",
					id, told[id], step.dx, step.yaw)
			}
		}
	}
	c.Disconnect()
}
