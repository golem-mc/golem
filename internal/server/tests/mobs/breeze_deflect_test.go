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

// TestAWindChargeStillHitsABreeze flies a wind charge at a breeze, which
// turns every projectile back but the wind charges: it takes the charge's
// hurt.
func TestAWindChargeStillHitsABreeze(t *testing.T) {
	t.Parallel()
	srv, c, breeze := breezeAhead(t, "Windy")
	c.ChatCommand(`summon minecraft:wind_charge 4.5 64.9 4.5 {Motion:[1d,0d,0d],acceleration_power:0d}`)
	c.AwaitSpawn(protocol.EntityTypeWindCharge)

	c.ReadTicks(srv, 10, func(id int32, data []byte) bool {
		return id == protocol.IDPlayEntitySoundEffect && servertest.SoundIs(data, "minecraft:entity.breeze.deflect")
	})
	if hp := breezeHealth(t, srv, breeze); hp >= 30 {
		t.Errorf("the breeze's health = %v, want the wind charge's hurt taken", hp)
	}
}

// TestThrownBodiesTurnBackOffABreeze flies a snowball and a small fireball
// at a breeze: neither hurts it, the snowball flies on, and the fireball's
// acceleration power is halved, which its next motion carries.
func TestThrownBodiesTurnBackOffABreeze(t *testing.T) {
	t.Parallel()
	srv, c, breeze := breezeAhead(t, "Thrower")
	c.ChatCommand(`summon minecraft:snowball 4.5 64.9 4.5 {Motion:[1d,0d,0d],NoGravity:1b}`)
	snowball := c.AwaitSpawn(protocol.EntityTypeSnowball).EntityID
	c.ChatCommand(`summon minecraft:small_fireball 4.5 65.5 4.5 {Motion:[1d,0d,0d],acceleration_power:0.1d}`)
	fireball := c.AwaitSpawn(protocol.EntityTypeSmallFireball).EntityID

	var power float64
	halved := c.ReadTicks(srv, 40, func(id int32, data []byte) bool {
		if id != protocol.IDPlayProjectilePower {
			return false
		}
		r := protocol.NewReader(data)
		if r.ReadVarInt() != fireball {
			return false
		}
		power = r.ReadF64()
		return power != 0.1
	})
	if !halved || power != 0.05 {
		t.Errorf("the fireball's projectile power = %v, want 0.1 halved to 0.05", power)
	}
	if hp := breezeHealth(t, srv, breeze); hp != 30 {
		t.Errorf("the breeze's health = %v, want 30: thrown bodies are turned back", hp)
	}
	alive := false
	for _, it := range srv.ThrownInDimension(world.DimensionName) {
		alive = alive || it.ID == snowball
	}
	if !alive {
		t.Error("the snowball broke on the breeze, want it turned back and flying on")
	}
}
