package entities

import (
	"fmt"
	"math"
	"testing"

	"github.com/golem-mc/golem/internal/config"
	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server"
	"github.com/golem-mc/golem/internal/server/servertest"
)

// boltBeside joins a player on easy, where a bolt lights no fire, holds the
// loop, and returns the point eight blocks east of the player at its feet.
func boltBeside(t *testing.T) (*server.Server, *servertest.Client, float64, float64, float64) {
	t.Helper()
	cfg := servertest.BlockTestConfig()
	cfg.ViewDistance = config.MinDistance
	cfg.SimulationDistance = config.MinDistance
	srv, c := servertest.Join(t, cfg, "Striker", servertest.OpsListing(t, "Striker"))
	servertest.HoldTicks(t, srv)
	srv.SetDifficulty(1)
	pos := srv.PlayerPosition("Striker")
	return srv, c, math.Floor(pos.X+8) + 0.5, math.Floor(pos.Y), math.Floor(pos.Z) + 0.5
}

// summonBolt summons a bolt at the point and waits for it to stand.
func summonBolt(t *testing.T, srv *server.Server, c *servertest.Client, x, y, z float64) {
	t.Helper()
	c.ChatCommand(fmt.Sprintf("summon minecraft:lightning_bolt %v %v %v", x, y, z))
	c.Settle()
	if srv.BoltCount() != 1 {
		t.Fatal("the bolt was never summoned")
	}
}

// TestALightningBoltBreaksABoatAndAMinecart: the bolt's five damage lays
// fifty on a vehicle, past the forty that breaks it, so each goes and drops
// its own item.
// vanilla: Entity.thunderHit, VehicleEntity.hurtServer
func TestALightningBoltBreaksABoatAndAMinecart(t *testing.T) {
	t.Parallel()
	srv, c, x, y, z := boltBeside(t)
	if srv.SpawnBoat(x, y, z, "minecraft:oak_boat") == 0 {
		t.Fatal("SpawnBoat came to nothing")
	}
	cart := srv.SpawnMinecart(x+1, y, z)
	summonBolt(t, srv, c, x, y, z)
	srv.TickBolts(1)
	srv.TickBoats(1)
	srv.TickMinecarts(1)
	if n := srv.BoatCount(); n != 0 {
		t.Errorf("%d boats outlast the strike, want the struck hull gone", n)
	}
	if _, _, _, _, ok := srv.MinecartState(cart); ok {
		t.Error("the struck minecart still stands")
	}
	items := srv.ItemsInDimension("minecraft:overworld")
	for _, want := range []string{"minecraft:oak_boat", "minecraft:minecart"} {
		if servertest.ItemCountOf(items, want) != 1 {
			t.Errorf("the strike left %+v, want one %s", items, want)
		}
	}
}

// TestALightningBoltExplodesAnEndCrystal: lightning_bolt is not
// is_explosion, so the struck crystal is removed and its blast runs.
// vanilla: Entity.thunderHit, EndCrystal.hurtServer
func TestALightningBoltExplodesAnEndCrystal(t *testing.T) {
	t.Parallel()
	srv, c, x, y, z := boltBeside(t)
	id := srv.SpawnEndCrystal(x, y+3, z)
	c.Settle()
	summonBolt(t, srv, c, x, y+3, z)
	srv.TickBolts(1)
	exploded, removed := false, false
	c.SettleAndCollect(func(pid int32, data []byte) {
		switch pid {
		case protocol.IDPlayExplosion:
			exploded = true
		case protocol.IDPlayRemoveEntities:
			r := protocol.NewReader(data)
			for range r.ReadVarInt() {
				removed = removed || r.ReadVarInt() == id
			}
		}
	})
	if n := srv.EndCrystalCount(); n != 0 {
		t.Errorf("%d crystals outlast the strike, want the struck one gone", n)
	}
	if !removed || !exploded {
		t.Errorf("the client saw the crystal removed %v and an explosion %v, want both", removed, exploded)
	}
}
