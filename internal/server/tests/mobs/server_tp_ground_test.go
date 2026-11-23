package mobs

import (
	"fmt"
	"testing"

	"github.com/golem-mc/golem/internal/config"
	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/servertest"
)

func tpGroundConfig() *config.Config {
	cfg := servertest.HostileTestConfig()
	cfg.ViewDistance, cfg.SimulationDistance = config.MinDistance, config.MinDistance
	return cfg
}

// A /tp sets the ground flag, so a client sees a cube mob land on the target.
// vanilla: TeleportCommand.performTeleport
func TestTeleportCommandSetsMobOnGround(t *testing.T) {
	t.Parallel()
	srv, c := servertest.Join(t, tpGroundConfig(), "Watcher")
	servertest.HoldTicks(t, srv)
	srv.RunLine(`summon magma_cube 0.5 100 0.5 {Tags:[c],NoAI:1b,Size:1}`)
	id := srv.MobIDsOfKind("minecraft:magma_cube")[0]
	servertest.RunTicks(t, srv, 1)
	if srv.MobOnGround(id) {
		t.Fatal("a NoAI cube in the air starts on the ground")
	}
	c.Settle()

	srv.RunLine(`tp @e[tag=c,limit=1] 0.5 105 0.5`)
	var sync servertest.EntityPositionSync
	told := c.ReadTicks(srv, 5, func(pid int32, data []byte) bool {
		if pid != protocol.IDPlayTeleportEntity {
			return false
		}
		sync = servertest.ParseEntityPositionSync(data)
		return sync.ID == id
	})
	if !told {
		t.Fatal("no Teleport Entity for the teleported cube")
	}
	if !sync.OnGround || !srv.MobOnGround(id) {
		t.Errorf("after /tp: packet on-ground %v, mob on ground %v, want both true", sync.OnGround, srv.MobOnGround(id))
	}
}

// A cube landed by /tp reaches the client on a stepped path over the ticks
// since the last update. NoAI: an AI cube hops on the landing tick, so no
// sync carries the flag.
// vanilla: SteppedInterpolationTracker, ServerEntity.sendChanges
func TestTeleportCommandLandsCubeWithSteppedPath(t *testing.T) {
	t.Parallel()
	srv, c := servertest.Join(t, tpGroundConfig(), "Watcher")
	servertest.HoldTicks(t, srv)
	srv.RunLine(`summon magma_cube 0.5 106 0.5 {Size:1,NoAI:1b}`)
	id := srv.MobIDsOfKind("minecraft:magma_cube")[0]
	servertest.RunTicks(t, srv, 1)
	c.Settle()
	srv.RunLine(`tp @e[type=magma_cube,limit=1] 0.5 101 0.5`)
	var sync servertest.EntityPositionSync
	seen := c.ReadTicks(srv, 10, func(pid int32, data []byte) bool {
		if pid != protocol.IDPlayTeleportEntity {
			return false
		}
		sync = servertest.ParseEntityPositionSync(data)
		return sync.ID == id && sync.OnGround
	})
	if !seen {
		t.Fatal("no grounded sync for the cube")
	}
	if !sync.Stepped || sync.Y != 101 || sync.StepTicks < 1 || sync.StepTicks > 3 {
		t.Errorf("sync = %+v, want stepped at y 101 over 1-3 ticks", sync)
	}
}

// A gliding mob stays off the ground through /tp.
// vanilla: TeleportCommand.performTeleport, LivingEntity.isFallFlying
func TestTeleportCommandKeepsFallFlyingMobOffGround(t *testing.T) {
	t.Parallel()
	srv, _ := servertest.Join(t, tpGroundConfig(), "Watcher")
	servertest.HoldTicks(t, srv)
	srv.RunLine(`tick freeze`)
	srv.RunLine(`summon zombie 0.5 206 0.5 {FallFlying:1b,NoAI:1b,Tags:[z]}`)
	id := srv.MobIDsOfKind("minecraft:zombie")[0]
	if srv.MobOnGround(id) {
		t.Fatal("a gliding zombie starts on the ground")
	}
	srv.RunLine(`tp @e[tag=z,limit=1] 0.5 201 0.5`)
	if srv.MobOnGround(id) {
		t.Error("a gliding zombie is on the ground after /tp")
	}
}

// A /tp while ticking is frozen waits for the cube's next movement gate and
// reaches the client on a stepped path over the whole gate interval, wherever
// between gates it was issued.
// vanilla: ServerEntity.sendChanges, SteppedInterpolationTracker
func TestFrozenTeleportCommandSendsSteppedPath(t *testing.T) {
	t.Parallel()
	srv, c := servertest.Join(t, tpGroundConfig(), "Watcher")
	servertest.HoldTicks(t, srv)
	srv.RunLine(`summon magma_cube 0.5 106 0.5 {Size:1,NoAI:1b}`)
	id := srv.MobIDsOfKind("minecraft:magma_cube")[0]
	servertest.RunTicks(t, srv, 1)
	srv.RunLine(`tick freeze`)
	servertest.RunTicks(t, srv, 12)
	c.Settle()
	// Three teleports, each one tick further from the previous sync's gate,
	// so at least one is issued between gates.
	for i, y := range []float64{101, 103, 102} {
		servertest.RunTicks(t, srv, i)
		srv.RunLine(fmt.Sprintf(`tp @e[type=magma_cube,limit=1] 0.5 %v 0.5`, y))
		var sync servertest.EntityPositionSync
		seen := c.ReadTicks(srv, 5, func(pid int32, data []byte) bool {
			if pid != protocol.IDPlayTeleportEntity {
				return false
			}
			sync = servertest.ParseEntityPositionSync(data)
			return sync.ID == id
		})
		// A cube's update interval is 3 ticks. vanilla: EntityTypes.MAGMA_CUBE
		if !seen || !sync.Stepped || sync.StepTicks != 3 || sync.Y != y {
			t.Fatalf("teleport %d: seen %v, sync = %+v, want stepped at y %v over 3 ticks", i, seen, sync, y)
		}
	}
}

// The client emits the ring at its interpolated position on the ground transition.
// vanilla: ServerEntity.createMovePacket, AbstractCubeMob.tick
func TestMagmaCubeLandingMovement(t *testing.T) {
	t.Parallel()
	srv, c := servertest.Join(t, tpGroundConfig(), "Watcher")
	servertest.HoldTicks(t, srv)
	srv.RunLine(`gamemode creative Watcher`)
	srv.RunLine(`fill -3 200 -3 3 200 3 stone`)
	srv.RunLine(`tp Watcher 0.5 201 3.5`)
	// Start the cube's first hop so its jump delay is running for both drops.
	srv.RunLine(`summon magma_cube 0.5 201 0.5 {Size:1,Tags:[drop],OnGround:1b}`)
	id := srv.MobIDsOfKind("minecraft:magma_cube")[0]
	servertest.RunTicks(t, srv, 3)
	c.Settle()
	if !c.ReadTicks(srv, 30, func(pid int32, data []byte) bool {
		if pid != protocol.IDPlayTeleportEntity {
			return false
		}
		sync := servertest.ParseEntityPositionSync(data)
		return sync.ID == id && sync.OnGround && sync.Y == 201
	}) {
		t.Fatal("the tracked cube never landed from its first hop")
	}

	onGround := true
	for _, direct := range []bool{false, true} {
		srv.RunLine(`tp @e[tag=drop,limit=1] 0.5 204 0.5`)
		var descents, landings int
		read := func(pid int32, data []byte) bool {
			r := protocol.NewReader(data)
			switch pid {
			case protocol.IDPlayEntityPosition, protocol.IDPlayEntityPositionRot:
				if r.ReadVarInt() != id {
					break
				}
				props := r.ReadVarInt()
				if props>>1 != 1 {
					t.Fatalf("relative movement has %d steps, want 1", props>>1)
				}
				if ground := props&1 != 0; ground != onGround {
					t.Errorf("relative movement set onGround to %v, want %v", ground, onGround)
				}
				ticks := r.ReadVarInt()
				if ticks != 3 {
					t.Errorf("relative movement takes %d ticks, want 3", ticks)
				}
				r.ReadI16()
				if dy := r.ReadI16(); dy < 0 && props&1 == 0 {
					descents++
				}
			case protocol.IDPlayTeleportEntity:
				sync := servertest.ParseEntityPositionSync(data)
				if sync.ID != id {
					break
				}
				if sync.OnGround && !onGround {
					landings++
					if !sync.Stepped || sync.StepTicks != 3 || sync.Y != 201 {
						t.Errorf("landing sync = %+v, want a 3-tick step to the stone", sync)
					}
				}
				onGround = sync.OnGround
			case protocol.IDPlayEntityRotation:
				if r.ReadVarInt() == id && r.ReadBool() != onGround {
					t.Error("rotation changed the cube's ground flag")
				}
			case protocol.IDPlayLevelParticles:
				if r.ReadVarInt() == protocol.ParticleFlame {
					t.Error("the client already draws the flame ring")
				}
			}
			return false
		}
		if direct {
			c.ReadTicks(srv, 6, read)
			if onGround {
				t.Fatal("client never saw the cube above the platform")
			}
			srv.RunLine(`tp @e[tag=drop,limit=1] 0.5 201 0.5`)
			c.ReadTicks(srv, 7, read)
		} else {
			c.ReadTicks(srv, 13, read)
		}
		if descents == 0 || landings != 1 {
			t.Errorf("direct teleport %v: got %d relative descents and %d ground transitions, want a descent and one landing", direct, descents, landings)
		}
	}
}
