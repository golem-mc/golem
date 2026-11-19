package mobs

import (
	"fmt"
	"testing"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/servertest"
)

// vanilla: Entity.updateFluidInteraction, Entity.doWaterSplashEffect
func TestProjectilesEnteringWaterSplash(t *testing.T) {
	t.Parallel()
	for _, kind := range []string{"returning trident", "firework", "eye of ender"} {
		t.Run(kind, func(t *testing.T) {
			t.Parallel()
			srv, c, splashes, _, _ := splashPool(t)
			servertest.HoldTicks(t, srv)
			switch kind {
			case "returning trident":
				// A spent trident returns through the basin's wall toward its owner.
				words := servertest.OfflineUUIDWords("Swimmer")
				srv.RunLine(fmt.Sprintf(`summon minecraft:trident 8.5 140 4.5 {DealtDamage:1b,Owner:[I;%d,%d,%d,%d],item:{id:"minecraft:trident",count:1,components:{"minecraft:enchantments":{"minecraft:loyalty":3}}}}`, int32(words[0]), int32(words[1]), int32(words[2]), int32(words[3])))
			case "firework":
				srv.SpawnFireworkAtAngle(4.5, 143, 4.5, 0, -0.5, 0, 100)
			case "eye of ender":
				srv.SpawnEyeEntity(4.5, 143, 4.5, 0, -0.5, 0)
			}
			servertest.UntilTicks(t, srv, 30, "the projectile's splash game event", func() bool { return splashes.Load() > 0 })
			var heard int
			c.SettleAndCollect(func(id int32, data []byte) {
				if id != protocol.IDPlaySoundEffect {
					return
				}
				name, category, _, _, _, volume, pitch := servertest.ReadAmbient(t, data)
				if name == "minecraft:entity.generic.splash" {
					heard++
					if category != protocol.SoundCategoryNeutral || volume <= 0 || volume > 1 || pitch < 0.6 || pitch > 1.4 {
						t.Errorf("splash category=%d volume=%v pitch=%v", category, volume, pitch)
					}
				}
			})
			if heard != 1 || splashes.Load() != 1 {
				t.Fatalf("splash sounds=%d game events=%d, want one each", heard, splashes.Load())
			}
		})
	}
}
