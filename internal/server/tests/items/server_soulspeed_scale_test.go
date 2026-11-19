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

// TestSoulSpeedParticlesSpreadOverAScaledBox checks that a scale-3 walker's
// soul particles spread over its 1.8-wide box, not a 0.6-wide strip.
// vanilla: SpawnParticlesEffect.PositionSource in_bounding_box
func TestSoulSpeedParticlesSpreadOverAScaledBox(t *testing.T) {
	t.Parallel()
	cfg := config.Default()
	cfg.OnlineMode = false
	cfg.ViewDistance, cfg.SimulationDistance = config.MinDistance, config.MinDistance
	srv, c := servertest.Join(t, cfg, "Walker", servertest.OpsListing(t, "Walker"))
	servertest.HoldTicks(t, srv)
	c.ChatCommand("attribute @s minecraft:scale base set 3")
	servertest.AwaitSystemChatSaying(t, c, "commands.attribute.base_value.set.success")

	pos := srv.PlayerPosition("Walker")
	bx, by, bz := int32(math.Floor(pos.X)), int32(math.Floor(pos.Y-0.5)), int32(math.Floor(pos.Z))
	sand := registry.MustState("minecraft:soul_sand", nil)
	srv.SetBlock(bx, by, bz, sand)
	srv.SetBlock(bx+1, by, bz, sand)
	boots, _ := registry.ItemID("minecraft:diamond_boots")
	soulSpeed, _ := registry.EnchantmentID("minecraft:soul_speed")
	srv.GiveSlot("Walker", player.ArmorSlot+3, protocol.ItemStack{ItemID: boots, Count: 1, Enchantments: map[int32]int32{soulSpeed: 3}})

	minX, maxX := math.Inf(1), math.Inf(-1)
	collect := func(id int32, data []byte) {
		if id != protocol.IDPlayLevelParticles {
			return
		}
		r := protocol.NewReader(data)
		if r.ReadVarInt() != protocol.ParticleSoul {
			return
		}
		tail := servertest.ReadParticleTail(t, r)
		minX, maxX = math.Min(minX, tail.X), math.Max(maxX, tail.X)
	}
	x0 := float64(bx) + 0.5
	for i := range 200 {
		c.MoveTo(x0+float64((i+1)%2), pos.Y, pos.Z, 0, 0)
		c.EndTick()
		c.SettleAndCollect(collect)
		servertest.RunTicks(t, srv, 1)
	}
	// The walker's x moves by 1, so the spread of one box is at most 1.8 plus
	// that; a 0.6 box would stay under 1.6.
	if spread := maxX - minX; spread <= 1.6 {
		t.Errorf("soul particle x spread = %v, want more than 1.6 for a 1.8-wide box", spread)
	}
}
