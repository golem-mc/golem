package items

import (
	"github.com/golem-mc/golem/internal/server/servertest"
	"testing"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/player"
	"github.com/golem-mc/golem/registry"
)

// useBlockEffects right-clicks the block at (x, y, z) on the face given with
// whatever the actor holds, returning the sounds the actor heard by name and
// the number of Level Particles packets it was sent.
func useBlockEffects(t *testing.T, c *servertest.Client, x, y, z, face int32) (map[string]int, int) {
	t.Helper()
	c.PlaceBlockAt(x, y, z, face, 0.5)
	sounds := map[string]int{}
	particles := 0
	c.SettleAndCollect(func(id int32, data []byte) {
		r := protocol.NewReader(data)
		switch id {
		case protocol.IDPlaySoundEffect:
			if sid := r.ReadVarInt(); sid > 0 {
				if name, ok := servertest.SoundEventName(sid - 1); ok {
					r.ReadVarInt() // the sound category
					sounds[name]++
				}
			}
		case protocol.IDPlayLevelParticles:
			particles++
		}
	})
	return sounds, particles
}

// TestAWaterBottleTurnsDirtIntoMud: a water bottle used on dirt's top face
// leaves mud, gives back a glass bottle, and sends the splash, the bottle
// empty and the five splash particles.
// vanilla: PotionItem.useOn
func TestAWaterBottleTurnsDirtIntoMud(t *testing.T) {
	t.Parallel()
	srv, c, _ := servertest.JoinAs(t, servertest.SurvivalTestConfig(), "Mudder")
	c.Settle()

	srv.SetBlock(3, 63, 0, registry.BlockStates["minecraft:dirt"])
	srv.GiveSlot("Mudder", player.HotbarSlot, servertest.WaterBottle(t))
	sounds, particles := useBlockEffects(t, c, 3, 63, 0, protocol.FaceTop)

	if got, want := srv.BlockAt(3, 63, 0), registry.BlockStates["minecraft:mud"]; got != want {
		t.Fatalf("the dirt after the water bottle = %d, want %d (mud)", got, want)
	}
	if got := srv.InventorySlot("Mudder", player.HotbarSlot); got.ItemID != servertest.MustItemID(t, "minecraft:glass_bottle") || got.Count != 1 {
		t.Errorf("the hand after the bottle = %+v, want one glass bottle", got)
	}
	if sounds["minecraft:entity.generic.splash"] != 1 {
		t.Errorf("the sounds heard = %v, want one entity.generic.splash", sounds)
	}
	if sounds["minecraft:item.bottle.empty"] != 1 {
		t.Errorf("the sounds heard = %v, want one item.bottle.empty", sounds)
	}
	if particles != 5 {
		t.Errorf("the splash particles = %d packets, want 5", particles)
	}
}
