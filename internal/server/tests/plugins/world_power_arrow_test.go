package plugins_test

import (
	"testing"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/servertest"
	"github.com/golem-mc/golem/internal/server/world"
	"github.com/golem-mc/golem/plugin"
	"github.com/golem-mc/golem/registry"
)

// TestPluginWorldPower checks World.Power reads the signal a lever feeds the
// sponge it is attached to.
// vanilla: Level.getBestNeighborSignal
func TestPluginWorldPower(t *testing.T) {
	t.Parallel()
	srv, h, run := worldPlugin(t, servertest.BlockTestConfig())
	cp := world.ChunkPos{X: 0, Z: 0}
	srv.OverworldWorld().Acquire(cp)
	defer srv.OverworldWorld().Release(cp)
	sponge := plugin.BlockPos{X: 8, Y: 64, Z: 8}
	lever := plugin.BlockPos{X: 9, Y: 64, Z: 8}
	for _, tc := range []struct {
		powered string
		want    func(int) bool
	}{{"true", func(n int) bool { return n > 0 }}, {"false", func(n int) bool { return n == 0 }}} {
		run(func() {
			w := h.Overworld()
			w.SetBlockState(sponge, plugin.BlockState(servertest.MustState(t, "minecraft:sponge", nil)), plugin.UpdateAll)
			w.SetBlockState(lever, plugin.BlockState(servertest.MustState(t, "minecraft:lever",
				map[string]string{"face": "wall", "facing": "east", "powered": tc.powered})), plugin.UpdateAll)
			if got := w.Power(sponge); !tc.want(got) {
				t.Errorf("Power with lever powered=%s = %d", tc.powered, got)
			}
		})
	}
}

// TestPluginArrowPotion checks a crossbow-shot tipped arrow reports its
// potion and a plain one does not.
// vanilla: Arrow.getPotionContents
func TestPluginArrowPotion(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name string
	}{{"tipped"}, {"custom"}, {"plain"}} {
		tipped := tc.name != "plain"
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			srv, h, run := worldPlugin(t, servertest.BowTestConfig())
			c := servertest.DialServer(t, srv)
			c.Join("Archer")
			srv.GiveSlot("Archer", 36, servertest.ItemStack(t, "minecraft:crossbow", 1))
			switch tc.name {
			case "tipped":
				srv.GiveSlot("Archer", 37, servertest.TippedArrow(t, "minecraft:poison"))
			case "custom":
				id, _ := registry.MobEffectID("minecraft:slowness")
				stack := servertest.ItemStack(t, "minecraft:tipped_arrow", 1)
				stack.SetPotionContents(protocol.PotionContents{Potion: protocol.NoPotion,
					CustomEffects: []protocol.PotionEffectSpec{{ID: id, Duration: 800, ShowParticles: true, ShowIcon: true}}})
				srv.GiveSlot("Archer", 37, stack)
			default:
				srv.GiveSlot("Archer", 37, servertest.ItemStack(t, "minecraft:arrow", 1))
			}
			c.MoveTo(0.5, 64, 0.5, -90, 0)
			servertest.DrawBow(c, -90, 0)
			servertest.HoldDraw(t, srv, "Archer", 27)
			servertest.ReleaseBow(c)
			servertest.PollUntil(t, servertest.ExpectTimeout, func() bool { return srv.BowTicks("Archer") == 0 })
			servertest.DrawBow(c, -90, 0)
			var arrow plugin.Arrow
			servertest.UntilTicks(t, srv, 20, "the arrow spawns", func() bool {
				run(func() {
					for e := range h.Overworld().Entities() {
						if a, ok := e.(plugin.Arrow); ok {
							arrow = a
						}
					}
				})
				return arrow != nil
			})
			run(func() {
				key, effects, ok := arrow.Potion()
				if ok != tipped {
					t.Fatalf("Potion ok = %v, want %v", ok, tipped)
				}
				if tc.name == "custom" && (key != plugin.Key{} || len(effects) != 1 || effects[0].Duration != 100) {
					t.Errorf("custom Potion = %v %v", key, effects)
				}
				if tc.name == "tipped" && (key != plugin.MinecraftKey("poison") || len(effects) != 1 || effects[0].Duration != 112) {
					t.Errorf("Potion = %v %v", key, effects)
				}
			})
		})
	}
}
