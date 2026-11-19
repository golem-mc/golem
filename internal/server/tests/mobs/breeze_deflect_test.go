package mobs

import (
	"math"
	"testing"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server"
	"github.com/golem-mc/golem/internal/server/servertest"
	"github.com/golem-mc/golem/internal/server/world"
)

// breezeAhead summons a NoAI breeze four blocks along +x from where the
// projectiles are summoned, and returns its entity ID.
func breezeAhead(t *testing.T, name string) (*server.Server, *servertest.Client, int32) {
	t.Helper()
	srv, c := servertest.Join(t, servertest.BowTestConfig(), name, servertest.OpsListing(t, name))
	c.ChatCommand(`summon minecraft:breeze 8.5 64 4.5 {NoAI:1b}`)
	return srv, c, c.AwaitSpawn(protocol.EntityTypeBreeze).EntityID
}

// breezeHealth is the breeze's health, failing the test once it is gone.
func breezeHealth(t *testing.T, srv *server.Server, id int32) float64 {
	t.Helper()
	hp, ok := srv.MobHealth(id)
	if !ok {
		t.Fatal("the breeze is gone")
	}
	return hp
}

// TestAnArrowTurnsBackOffABreeze flies an arrow at a breeze: the breeze
// takes no hurt, plays its deflect sound, and the arrow flies back the way
// it came at half its speed.
func TestAnArrowTurnsBackOffABreeze(t *testing.T) {
	t.Parallel()
	srv, c, breeze := breezeAhead(t, "Archer")
	c.ChatCommand(`summon minecraft:arrow 4.5 64.9 4.5 {Motion:[1.5d,0d,0d],NoGravity:1b}`)
	c.AwaitSpawn(protocol.EntityTypeArrow)

	heard := c.ReadTicks(srv, 20, func(id int32, data []byte) bool {
		return id == protocol.IDPlayEntitySoundEffect && servertest.SoundIs(data, "minecraft:entity.breeze.deflect")
	})
	if !heard {
		t.Error("no entity.breeze.deflect within 20 ticks")
	}
	if hp := breezeHealth(t, srv, breeze); hp != 30 {
		t.Errorf("the breeze's health = %v, want 30: an arrow is turned back, not taken", hp)
	}
	arrows := srv.ArrowsInDimension(world.DimensionName)
	if len(arrows) != 1 {
		t.Fatalf("%d arrows fly, want the one turned back", len(arrows))
	}
	// Reversed and halved at the hit, then the air drag of the ticks since:
	// at most 0.75 back along x, and a few ticks' 0.99 under it.
	if vx := arrows[0].VX; vx > -0.75*math.Pow(0.99, 25) || vx < -0.75 {
		t.Errorf("the arrow flies %v along x, want its 1.5 reversed and halved", vx)
	}
}
