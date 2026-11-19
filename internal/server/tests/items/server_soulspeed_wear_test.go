package items

import (
	"math"
	"testing"

	"github.com/golem-mc/golem/internal/config"
	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/player"
	"github.com/golem-mc/golem/internal/server/servertest"
	"github.com/golem-mc/golem/registry"
)

// soulWalk is what a walk of 200 steps between two blocks showed.
type soulWalk struct {
	damage    int32
	heardSoul bool
	particles int
	badVel    int // soul particles whose velocity is not (∓0.2, 0.1, 0)
	idle      int // soul particles sent while the walker stood still
}

// walkSoulSpeedBoots has a player of the game mode in Soul Speed III diamond
// boots step 200 times between two blocks of the named kind, then stand
// still for 10 ticks.
func walkSoulSpeedBoots(t *testing.T, ground string, mode int32) soulWalk {
	t.Helper()
	cfg := config.Default()
	cfg.OnlineMode = false
	cfg.ViewDistance, cfg.SimulationDistance = config.MinDistance, config.MinDistance
	srv, c := servertest.Join(t, cfg, "Walker")
	servertest.HoldTicks(t, srv)
	srv.SetPlayerGameMode("Walker", mode)

	pos := srv.PlayerPosition("Walker")
	bx, by, bz := int32(math.Floor(pos.X)), int32(math.Floor(pos.Y-0.5)), int32(math.Floor(pos.Z))
	state, ok := registry.FindState(ground, nil)
	if !ok {
		t.Fatalf("the block registry has no %s", ground)
	}
	srv.SetBlock(bx, by, bz, state)
	srv.SetBlock(bx+1, by, bz, state)

	boots, _ := registry.ItemID("minecraft:diamond_boots")
	soulSpeed, _ := registry.EnchantmentID("minecraft:soul_speed")
	srv.GiveSlot("Walker", player.ArmorSlot+3, protocol.ItemStack{ItemID: boots, Count: 1, Enchantments: map[int32]int32{soulSpeed: 3}})

	var walk soulWalk
	standing := false
	collect := func(id int32, data []byte) {
		switch id {
		case protocol.IDPlaySoundEffect:
			if name, _, _, _, _, _, _ := servertest.ReadAmbient(t, data); name == "minecraft:particle.soul_escape" {
				walk.heardSoul = true
			}
		case protocol.IDPlayLevelParticles:
			r := protocol.NewReader(data)
			if r.ReadVarInt() != protocol.ParticleSoul {
				return
			}
			tail := servertest.ReadParticleTail(t, r)
			dx, dy, dz := tail.DX, tail.DY, tail.DZ
			if standing {
				walk.idle++
				return
			}
			walk.particles++
			if math.Abs(math.Abs(float64(dx))-0.2) > 1e-6 || dy != 0.1 || dz != 0 {
				walk.badVel++
			}
		}
	}
	x0 := float64(bx) + 0.5
	for i := range 200 {
		c.MoveTo(x0+float64((i+1)%2), pos.Y, pos.Z, 0, 0)
		c.EndTick()
		c.SettleAndCollect(collect)
		servertest.RunTicks(t, srv, 1)
	}
	// A client repeats its position when it stands, which zeroes the server's
	// known movement; one tick takes it in before the standing count starts.
	c.MoveTo(x0, pos.Y, pos.Z, 0, 0)
	c.EndTick()
	servertest.RunTicks(t, srv, 1)
	c.SettleAndCollect(collect) // the last steps' tick output
	standing = true
	for range 10 {
		c.MoveTo(x0, pos.Y, pos.Z, 0, 0)
		c.EndTick()
		servertest.RunTicks(t, srv, 1)
		c.SettleAndCollect(collect)
	}
	walk.damage = srv.InventorySlot("Walker", player.ArmorSlot+3).Damage
	return walk
}
