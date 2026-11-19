package mobs

import (
	"math"
	"testing"

	"github.com/golem-mc/golem/internal/protocol"
	server "github.com/golem-mc/golem/internal/server"
	"github.com/golem-mc/golem/internal/server/servertest"
	"github.com/golem-mc/golem/internal/server/world"
	"github.com/golem-mc/golem/registry"
)

const creakingPlayer = "Warden"

// creakingWorld stands a player in a flat world with the loop stopped.
func creakingWorld(t *testing.T) (*server.Server, *servertest.Client) {
	t.Helper()
	srv, c, _ := servertest.JoinAs(t, servertest.HostileTestConfig(), creakingPlayer)
	c.Settle()
	srv.StopTick()
	srv.SetSpawning(false)
	srv.SetDifficulty(2)
	return srv, c
}

func creakingByID(t *testing.T, srv *server.Server, id int32) server.TestMob {
	t.Helper()
	for _, m := range srv.Mobs() {
		if m.ID == id {
			return m
		}
	}
	t.Fatalf("creaking %d is gone", id)
	return server.TestMob{}
}

// TestACreakingFreezesWhileAPlayerLooksAtItAndHuntsOnceWoken: a creaking in
// front of a player's gaze is held still and woken; looking away lets it go
// while it stays awake.
// vanilla: Creaking.checkCanMove
func TestACreakingFreezesWhileAPlayerLooksAtItAndHuntsOnceWoken(t *testing.T) {
	t.Parallel()
	srv, c := creakingWorld(t)
	pos := srv.PlayerPosition(creakingPlayer)
	id := srv.SpawnMob("minecraft:creaking", pos.X+8, pos.Y, pos.Z)
	c.MoveTo(pos.X, pos.Y, pos.Z, -90, 0) // east
	c.Settle()
	srv.TickMobs(3)
	m := creakingByID(t, srv, id)
	if !m.CreakingFrozen || !m.CreakingActive {
		t.Fatalf("under the gaze: frozen=%v active=%v, want both", m.CreakingFrozen, m.CreakingActive)
	}
	c.MoveTo(pos.X, pos.Y, pos.Z, 90, 0) // west
	c.Settle()
	srv.TickMobs(3)
	m = creakingByID(t, srv, id)
	if m.CreakingFrozen {
		t.Errorf("looked away from, the creaking is still frozen")
	}
	if !m.CreakingActive {
		t.Errorf("looked away from, the woken creaking went back to sleep")
	}
}

// TestACreakingFreezesWhenWatchedThroughGlass: the creaking's gaze test sees
// through blocks with an empty visual shape (glass, panes) but not stone.
// vanilla: Creaking.checkCanMove, LivingEntity.isLookingAtMe (VISUAL clip)
func TestACreakingFreezesWhenWatchedThroughGlass(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name string
		wall registry.Block
		sees bool
	}{
		{"glass", registry.BlockGlass, true},
		{"glass pane", registry.BlockGlassPane, true},
		{"stone", registry.BlockStone, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			srv, c := creakingWorld(t)
			pos := srv.PlayerPosition(creakingPlayer)
			px, py, pz := int32(math.Floor(pos.X)), int32(math.Floor(pos.Y)), int32(math.Floor(pos.Z))
			for dz := int32(-2); dz <= 2; dz++ {
				for dy := range int32(6) {
					srv.SetBlock(px+4, py+dy, pz+dz, tc.wall.DefaultState())
				}
			}
			id := srv.SpawnMob("minecraft:creaking", pos.X+8, pos.Y, pos.Z)
			c.MoveTo(pos.X, pos.Y, pos.Z, -90, 0) // east
			c.Settle()
			srv.TickMobs(3)
			m := creakingByID(t, srv, id)
			if m.CreakingFrozen != tc.sees || m.CreakingActive != tc.sees {
				t.Errorf("behind %s: frozen=%v active=%v, want both %v", tc.name, m.CreakingFrozen, m.CreakingActive, tc.sees)
			}
		})
	}
}

// TestAFrozenCreakingStaysWhereItStands: held by a gaze the creaking does not
// walk toward the player it hunts, and walks once the gaze is off it.
// vanilla: Creaking.CreakingMoveControl
func TestAFrozenCreakingStaysWhereItStands(t *testing.T) {
	t.Parallel()
	srv, c := creakingWorld(t)
	pos := srv.PlayerPosition(creakingPlayer)
	id := srv.SpawnMob("minecraft:creaking", pos.X+8, pos.Y, pos.Z)
	c.MoveTo(pos.X, pos.Y, pos.Z, -90, 0)
	c.Settle()
	srv.TickMobs(3)
	start := creakingByID(t, srv, id)
	srv.TickMobs(40)
	held := creakingByID(t, srv, id)
	if held.X != start.X || held.Z != start.Z {
		t.Errorf("the creaking walked from (%.2f, %.2f) to (%.2f, %.2f) under the gaze", start.X, start.Z, held.X, held.Z)
	}
	c.MoveTo(pos.X, pos.Y, pos.Z, 90, 0)
	c.Settle()
	srv.TickMobs(40)
	if free := creakingByID(t, srv, id); free.X >= held.X-1 {
		t.Errorf("the creaking is at x=%.2f, want it walking west toward the player from %.2f", free.X, held.X)
	}
}

// TestABoundCreakingTakesNoDamageAndSways: a player's blow on a creaking
// bound to a heart leaves its health and plays the sway.
// vanilla: Creaking.hurtServer
func TestABoundCreakingTakesNoDamageAndSways(t *testing.T) {
	t.Parallel()
	srv, c := creakingWorld(t)
	pos := srv.PlayerPosition(creakingPlayer)
	id := srv.SpawnMob("minecraft:creaking", pos.X+1.5, pos.Y, pos.Z)
	heart, ok := registry.FindState("minecraft:creaking_heart", nil)
	if !ok {
		t.Fatal("no creaking heart block")
	}
	hx, hy, hz := int32(pos.X)+3, int32(pos.Y), int32(pos.Z)
	srv.SetBlock(hx, hy, hz, heart)
	if !srv.EnsureBlockEntityInDimension(world.DimensionName, hx, hy, hz, world.KindCreakingHeart) {
		t.Fatal("the heart keeps no block entity")
	}
	srv.BindCreaking(id, hx, hy, hz)
	c.Settle()
	c.Attack(id)
	swayed := false
	for range 40 {
		srv.TickMobs(1)
		c.SettleAndCollect(func(pid int32, data []byte) {
			if pid != protocol.IDPlayEntityEvent {
				return
			}
			r := protocol.NewReader(data)
			if r.ReadI32() == id && r.ReadU8() == 66 {
				swayed = true
			}
		})
		if swayed {
			break
		}
	}
	if !swayed {
		t.Errorf("no sway event followed the blow")
	}
	if health, ok := srv.MobHealth(id); !ok || health != 1 {
		t.Errorf("health after the blow = %v (alive %v), want the untouched 1", health, ok)
	}
}

// TestAnUnboundCreakingDiesToOneBlow: only a bound creaking is shielded.
// vanilla: Creaking.hurtServer
func TestAnUnboundCreakingDiesToOneBlow(t *testing.T) {
	t.Parallel()
	srv, c := creakingWorld(t)
	pos := srv.PlayerPosition(creakingPlayer)
	id := srv.SpawnMob("minecraft:creaking", pos.X+1.5, pos.Y, pos.Z)
	c.Settle()
	c.Attack(id)
	for range 40 {
		srv.TickMobs(1)
		c.Settle()
		if _, ok := srv.MobHealth(id); !ok {
			return
		}
	}
	t.Errorf("a creaking with no heart outlived a blow")
}

// TestAWokenCreakingSwingsOnceEveryFortyTicks: a creaking a gaze has woken
// and then left alone walks up to the player and hits for 3 about every 40
// ticks.
// vanilla: CreakingAi.initFightActivity, MeleeAttack
func TestAWokenCreakingSwingsOnceEveryFortyTicks(t *testing.T) {
	t.Parallel()
	srv, c := creakingWorld(t)
	pos := srv.PlayerPosition(creakingPlayer)
	id := srv.SpawnMob("minecraft:creaking", pos.X+3, pos.Y, pos.Z)
	c.MoveTo(pos.X, pos.Y, pos.Z, -90, 0)
	c.Settle()
	srv.TickMobs(3)
	if !creakingByID(t, srv, id).CreakingActive {
		t.Fatal("the gaze did not wake the creaking")
	}
	c.MoveTo(pos.X, pos.Y, pos.Z, 90, 0)
	c.Settle()
	hits := 0
	last := srv.PlayerHealth(creakingPlayer)
	for range 200 {
		srv.TickMobs(1)
		srv.TickSurvival()
		if now := srv.PlayerHealth(creakingPlayer); now < last {
			hits++
			last = now
		}
	}
	if hits < 4 || hits > 5 {
		t.Errorf("%d hits in 200 ticks, want one every 40 ticks (4 or 5)", hits)
	}
}

// TestACreakingBoundToNoHeartDiesOnItsFirstTick: a home_pos with no heart
// behind it is no shield.
// vanilla: Creaking.tick
func TestACreakingBoundToNoHeartDiesOnItsFirstTick(t *testing.T) {
	t.Parallel()
	srv, c := creakingWorld(t)
	pos := srv.PlayerPosition(creakingPlayer)
	id := srv.SpawnMob("minecraft:creaking", pos.X+4, pos.Y, pos.Z)
	srv.BindCreaking(id, int32(pos.X), int32(pos.Y)+5, int32(pos.Z))
	for range 40 {
		srv.TickMobs(1)
		c.Settle()
		if _, ok := srv.MobHealth(id); !ok {
			return
		}
	}
	t.Errorf("a creaking bound to no heart is still alive")
}
