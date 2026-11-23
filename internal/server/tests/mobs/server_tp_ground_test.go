package mobs

import (
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

// A /tp while ticking is frozen still reaches the client on a stepped path.
// vanilla: ServerEntity.sendChanges, SteppedInterpolationTracker
func TestFrozenTeleportCommandSendsSteppedPath(t *testing.T) {
	t.Parallel()
	srv, c := servertest.Join(t, tpGroundConfig(), "Watcher")
	servertest.HoldTicks(t, srv)
	srv.RunLine(`summon magma_cube 0.5 106 0.5 {Size:1,NoAI:1b}`)
	id := srv.MobIDsOfKind("minecraft:magma_cube")[0]
	servertest.RunTicks(t, srv, 1)
	srv.RunLine(`tick freeze`)
	c.Settle()
	srv.RunLine(`tp @e[type=magma_cube,limit=1] 0.5 101 0.5`)
	var sync servertest.EntityPositionSync
	seen := c.ReadTicks(srv, 5, func(pid int32, data []byte) bool {
		if pid != protocol.IDPlayTeleportEntity {
			return false
		}
		sync = servertest.ParseEntityPositionSync(data)
		return sync.ID == id
	})
	if !seen || !sync.Stepped || sync.StepTicks < 1 || sync.Y != 101 {
		t.Errorf("seen %v, sync = %+v, want stepped at y 101", seen, sync)
	}
}
