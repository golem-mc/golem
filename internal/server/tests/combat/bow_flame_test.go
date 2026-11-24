package combat

import (
	"testing"

	"github.com/golem-mc/golem/internal/server/player"
	"github.com/golem-mc/golem/internal/server/servertest"
)

// vanilla: Projectile.applyOnProjectileSpawned, AbstractArrow.onHitEntity
func TestBowShotIgnitesItsTargetWithFlameOnBowOrArrow(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name string
		bow  bool
		ammo bool
	}{
		{name: "plain"},
		{name: "flame on bow", bow: true},
		{name: "flame on arrow", ammo: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			srv, c := servertest.Join(t, servertest.BowTestConfig(), "Archer")
			servertest.HoldTicks(t, srv)
			bow := servertest.ItemStack(t, "minecraft:bow", 1)
			ammo := servertest.ItemStack(t, "minecraft:arrow", 1)
			if tc.bow {
				bow = servertest.EnchantedStack(t, "minecraft:bow", "minecraft:flame", 1)
			}
			if tc.ammo {
				ammo = servertest.EnchantedStack(t, "minecraft:arrow", "minecraft:flame", 1)
			}
			srv.GiveSlot("Archer", player.HotbarSlot, bow)
			srv.GiveSlot("Archer", player.HotbarSlot+1, ammo)

			cow := srv.SpawnMob("minecraft:cow", 4.5, 64, 0.5)
			srv.SetMobNoAI(cow)
			c.MoveTo(0.5, 64, 0.5, -90, 12)
			servertest.DrawBow(c, -90, 12)
			servertest.HoldDraw(t, srv, "Archer", 15)
			servertest.ReleaseBow(c)
			c.Settle()

			servertest.UntilTicks(t, srv, 10, "the arrow hurts the cow", func() bool {
				hp, alive := srv.MobHealth(cow)
				return alive && hp < 10
			})
			if burning, want := srv.MobFireTicks(cow) > 0, tc.bow || tc.ammo; burning != want {
				t.Errorf("cow is burning after the arrow hit = %v, want %v", burning, want)
			}
		})
	}
}
