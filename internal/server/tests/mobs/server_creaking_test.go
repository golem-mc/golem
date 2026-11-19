package mobs

import (
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
