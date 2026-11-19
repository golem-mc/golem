package plugins_test

import (
	"slices"
	"testing"

	"github.com/golem-mc/golem/internal/server/servertest"
	"github.com/golem-mc/golem/internal/server/world"
	"github.com/golem-mc/golem/plugin"
	"github.com/golem-mc/golem/registry"
)

func TestPluginMobVariantSetters(t *testing.T) {
	_, w, run, _, _ := kindsFixture(t)
	run(func() {
		sheep := spawnAt(t, w, registry.EntityTypeSheep, nil).(plugin.Sheep)
		sheep.SetColor(plugin.Pink)
		sheep.SetSheared(true)
		if sheep.Color() != plugin.Pink || !sheep.Sheared() {
			t.Errorf("sheep colour=%d sheared=%v", sheep.Color(), sheep.Sheared())
		}

		wolf := spawnAt(t, w, registry.EntityTypeWolf, nil)
		wv, ok := wolf.(plugin.VariantHolder)
		cw, ok2 := wolf.(plugin.Collared)
		if !ok || !ok2 {
			t.Fatal("wolf lacks VariantHolder or Collared")
		}
		wv.SetVariant(plugin.MinecraftKey("ashen"))
		cw.SetCollarColor(plugin.Blue)
		if wv.Variant() != plugin.MinecraftKey("ashen") || cw.CollarColor() != plugin.Blue {
			t.Errorf("wolf variant=%v collar=%d", wv.Variant(), cw.CollarColor())
		}
		wv.SetVariant(plugin.MinecraftKey("nonsense"))
		if wv.Variant() != plugin.MinecraftKey("ashen") {
			t.Errorf("unknown variant accepted: %v", wv.Variant())
		}

		cat := spawnAt(t, w, registry.EntityTypeCat, nil).(plugin.VariantHolder)
		cat.SetVariant(plugin.MinecraftKey("black"))
		frog := spawnAt(t, w, registry.EntityTypeFrog, nil).(plugin.VariantHolder)
		frog.SetVariant(plugin.MinecraftKey("cold"))
		if cat.Variant() != plugin.MinecraftKey("black") || frog.Variant() != plugin.MinecraftKey("cold") {
			t.Errorf("cat=%v frog=%v", cat.Variant(), frog.Variant())
		}

		horse := spawnAt(t, w, registry.EntityTypeHorse, nil).(plugin.Horse)
		horse.SetColor(plugin.HorseGray)
		horse.SetMarkings(plugin.MarkingsBlackDots)
		if horse.Color() != plugin.HorseGray || horse.Markings() != plugin.MarkingsBlackDots {
			t.Errorf("horse colour=%d markings=%d", horse.Color(), horse.Markings())
		}
		horse.SetColor(plugin.HorseBrown)
		if horse.Markings() != plugin.MarkingsBlackDots {
			t.Error("colour change lost the markings")
		}

		creeper := spawnAt(t, w, registry.EntityTypeCreeper, nil).(plugin.Creeper)
		creeper.SetPowered(true)
		if !creeper.Powered() {
			t.Error("creeper not charged")
		}

		slime := spawnAt(t, w, registry.EntityTypeSlime, nil).(plugin.Slime)
		slime.SetSize(4)
		if slime.Size() != 4 {
			t.Errorf("slime size=%d", slime.Size())
		}
		slime.SetSize(0)
		if slime.Size() != 4 {
			t.Errorf("size 0 accepted: %d", slime.Size())
		}
	})
}

func TestPluginFireworkItem(t *testing.T) {
	srv, h, run := worldPlugin(t, servertest.BlockTestConfig())
	srv.OverworldWorld().Acquire(world.ChunkPos{X: 0, Z: 0})
	srv.OverworldWorld().InhabitTime([]world.ChunkPos{{X: 0, Z: 0}})
	run(func() {
		w := h.Overworld()
		rocket := spawnAt(t, w, registry.EntityTypeFireworkRocket, nil)
		fw, ok := rocket.(plugin.Firework)
		if !ok {
			t.Fatal("rocket is not a Firework")
		}
		item, err := h.Items().Parse("firework_rocket[fireworks={flight_duration:3,explosions:[{shape:\"star\",colors:[I;16711680]}]}]", 1)
		if err != nil {
			t.Fatal(err)
		}
		fw.SetItem(item)
		if got := h.Items().Format(fw.Item()); got != h.Items().Format(item) {
			t.Errorf("item %s, want %s", got, h.Items().Format(item))
		}
		fw.SetItem(plugin.ItemStack{})
		if h.Items().Format(fw.Item()) != h.Items().Format(item) {
			t.Error("an empty stack replaced the rocket")
		}
	})
}

func TestPluginCommandsAll(t *testing.T) {
	_, h, run := worldPlugin(t, servertest.BlockTestConfig())
	run(func() {
		err := h.Commands().Register(plugin.Command{Name: "warp", Aliases: []string{"w2"}, Description: "go", Permission: "warps.use",
			Run: func(*plugin.CommandContext) (int, error) { return 1, nil }})
		if err != nil {
			t.Fatal(err)
		}
		var warp, tp *plugin.CommandInfo
		all := h.Commands().All()
		for i, c := range all {
			switch c.Name {
			case "warp":
				warp = &all[i]
			case "teleport":
				tp = &all[i]
			}
		}
		if warp == nil || warp.Description != "go" || warp.Permission != "warps.use" || warp.Plugin != "world" || len(warp.Aliases) != 1 || warp.Aliases[0] != "w2" {
			t.Errorf("warp=%+v", warp)
		}
		if tp == nil || tp.Plugin != "" || !slices.Contains(tp.Aliases, "tp") {
			t.Errorf("teleport=%+v", tp)
		}
	})
}

func TestPluginWorldSeaLevelChunksAndTree(t *testing.T) {
	_, w, run, _, _ := kindsFixture(t)
	run(func() {
		// the test world is a flat one, whose sea level is -63
		if w.SeaLevel() != -63 {
			t.Errorf("sea level %d", w.SeaLevel())
		}
		found := false
		for _, c := range w.LoadedChunks() {
			found = found || c.Pos() == (plugin.ChunkPos{})
		}
		if !found {
			t.Error("chunk 0,0 not listed")
		}
		dirt := plugin.DefaultState(registry.BlockDirt)
		for dx := -2; dx <= 2; dx++ {
			for dz := -2; dz <= 2; dz++ {
				w.SetBlockState(plugin.BlockPos{X: 8 + dx, Y: 79, Z: 8 + dz}, dirt, plugin.UpdateAll)
			}
		}
		if !w.GenerateTree(plugin.TreeOak, plugin.BlockPos{X: 8, Y: 80, Z: 8}) {
			t.Fatal("tree did not fit")
		}
		if b := w.BlockState(plugin.BlockPos{X: 8, Y: 80, Z: 8}).Block(); b != registry.BlockOakLog {
			t.Errorf("trunk base is %v", b)
		}
		if b := w.BlockState(plugin.BlockPos{X: 8, Y: 84, Z: 8}).Block(); b != registry.BlockOakLog && !b.IsLeaves() {
			t.Errorf("no crown above the trunk: %v", b)
		}
		if w.GenerateTree(plugin.TreeType(200), plugin.BlockPos{X: 8, Y: 80, Z: 8}) {
			t.Error("unknown tree type grew")
		}
	})
}
