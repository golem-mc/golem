package mobs

import (
	"fmt"
	"math"
	"testing"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/servertest"
)

// vanilla: Shulker.teleportSomewhere
func TestHurtShulkerNeverTeleportsOntoBedrock(t *testing.T) {
	t.Parallel()
	srv, _ := servertest.Join(t, servertest.HostileTestConfig(), "Watcher")
	servertest.HoldTicks(t, srv)
	// The grass is beyond teleport reach. Bedrock is valid for staying
	// attached, but not teleporting.
	srv.RunLine(`fill -8 99 -8 8 99 8 bedrock`)
	id := srv.SpawnMob("minecraft:shulker", .5, 100, .5)
	// Increasing damage lands through the hurt cooldown without extra ticks.
	for i := range 200 {
		srv.SetMobHealth(id, 12)
		srv.HurtMobByWorld(id, 1+float64(i)/1000, "minecraft:out_of_world")
		servertest.RunTicks(t, srv, 1)
		x, y, z, ok := srv.MobPos(id)
		if !ok || x != .5 || y != 100 || z != .5 {
			t.Fatalf("shulker teleported onto forbidden support at %v,%v,%v (alive %v)", x, y, z, ok)
		}
	}
	// Prove the same cell permits a hurt teleport once its supports are allowed.
	srv.RunLine(`fill -8 99 -8 8 99 8 stone`)
	hurt := 2.0
	servertest.UntilTicks(t, srv, 200, "hurt shulker teleports onto stone", func() bool {
		x, y, z, _ := srv.MobPos(id)
		if x != .5 || y != 100 || z != .5 {
			return true
		}
		srv.SetMobHealth(id, 12)
		hurt += .001
		srv.HurtMobByWorld(id, hurt, "minecraft:out_of_world")
		return false
	})
}

// vanilla: Shulker.startRiding, tick, hurtServer, teleportSomewhere
func TestHurtShulkerTeleportsOutOfVehicle(t *testing.T) {
	t.Parallel()
	for _, kind := range []string{"oak_boat", "minecart", "pig"} {
		t.Run(kind, func(t *testing.T) {
			t.Parallel()
			srv, c := servertest.Join(t, servertest.HostileTestConfig(), "Watcher")
			servertest.HoldTicks(t, srv)
			srv.RunLine(`fill -8 99 -8 8 99 8 stone`)
			srv.RunLine(fmt.Sprintf(`summon %s 0.5 100 0.5 {Tags:[vehicle],NoAI:1b}`, kind))
			srv.RunLine(`summon shulker 0.5 100 0.5 {Tags:[shell],AttachFace:5b,NoAI:1b}`)
			id := srv.MobIDsOfKind("minecraft:shulker")[0]
			srv.RunLine(`ride @e[tag=shell,limit=1] mount @e[tag=vehicle,limit=1]`)
			veh := srv.MobVehicleID(id)
			if veh == 0 {
				t.Fatal("shulker did not board")
			}
			if face, _, _ := srv.ShulkerState(id); face != 0 {
				t.Errorf("mounted face = %d, want down", face)
			}
			srv.SetShulkerPeek(id, 100)
			servertest.RunTicks(t, srv, 10)
			_, y, _, _ := srv.MobPos(id)
			_, _, _, _, top, _, _ := srv.MobBox(id)
			if math.Abs(top-y-1.5) > 1e-5 {
				t.Errorf("riding shell height = %v, want 1.5", top-y)
			}
			c.Settle()
			srv.RunLine(`data merge entity @e[tag=shell,limit=1] {NoAI:0b}`)
			hurt := 1.0
			servertest.UntilTicks(t, srv, 200, "hurt shulker leaves vehicle", func() bool {
				if srv.MobVehicleID(id) == 0 {
					return true
				}
				srv.SetMobHealth(id, 12)
				hurt += .001
				srv.HurtMobByWorld(id, hurt, "minecraft:out_of_world")
				return false
			})
			x, y, z, ok := srv.MobPos(id)
			if !ok || (y != 100 && y != 98) || x != math.Floor(x)+.5 || z != math.Floor(z)+.5 {
				t.Errorf("teleport destination = %v,%v,%v (alive %v), want a stone-supported cell", x, y, z, ok)
			}
			var passengers []int32
			updated, told := false, false
			collect := func(pid int32, data []byte) {
				if pid == protocol.IDPlaySetPassengers {
					vehicle, ids := servertest.ReadPassengers(c, data)
					if vehicle == veh {
						passengers = ids
						updated = true
					}
				}
				if pid == protocol.IDPlayTeleportEntity {
					r := protocol.NewReader(data)
					if r.ReadVarInt() == id {
						if path := r.ReadVarInt(); path != 0 {
							t.Fatalf("position path = %d, want linear", path)
						}
						px, py, pz := r.ReadF64(), r.ReadF64(), r.ReadF64()
						if err := r.Err(); err != nil {
							t.Fatal(err)
						}
						told = px == x && py == y && pz == z
					}
				}
			}
			c.SettleAndCollect(collect)
			if !updated {
				t.Fatal("no passenger update after teleport")
			}
			if len(passengers) != 0 {
				t.Errorf("vehicle still lists %v after teleport", passengers)
			}
			// The shulker's movement cadence is three ticks.
			servertest.RunTicks(t, srv, 3)
			c.SettleAndCollect(collect)
			if !told {
				t.Error("client was not told the teleport destination after dismount")
			}
			nx, ny, nz, _ := srv.MobPos(id)
			if nx != x || ny != y || nz != z {
				t.Errorf("vehicle moved teleported shulker from %v,%v,%v to %v,%v,%v", x, y, z, nx, ny, nz)
			}
		})
	}
}

// vanilla: Shulker.hitByShulkerBullet
func TestRidingShulkerSplitsWhenHitByBullet(t *testing.T) {
	t.Parallel()
	srv, _ := servertest.Join(t, servertest.HostileTestConfig(), "Watcher")
	servertest.HoldTicks(t, srv)
	srv.RunLine(`fill -8 99 -8 8 99 8 stone`)
	srv.RunLine(`summon oak_boat 0.5 100 0.5 {Tags:[vehicle]}`)
	srv.RunLine(`summon shulker 0.5 100 0.5 {Tags:[shell]}`)
	id := srv.MobIDsOfKind("minecraft:shulker")[0]
	srv.RunLine(`ride @e[tag=shell,limit=1] mount @e[tag=vehicle,limit=1]`)
	if srv.MobVehicleID(id) == 0 {
		t.Fatal("shulker did not board")
	}
	ticks := 0
	servertest.UntilTicks(t, srv, 200, "bullet teleports the riding shulker", func() bool {
		if srv.MobVehicleID(id) == 0 {
			return true
		}
		srv.SetMobHealth(id, 30)
		srv.SetShulkerPeek(id, 100)
		if ticks%10 == 0 {
			x, y, z, _ := srv.MobPos(id)
			srv.LaunchShulkerBullet(x, y+.75, z, id, 3)
		}
		ticks++
		return false
	})
	if n := len(srv.MobIDsOfKind("minecraft:shulker")); n != 2 {
		t.Errorf("%d shulkers after bullet teleport, want original and copy", n)
	}
}
