package blocks

import (
	"testing"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/servertest"
	"github.com/golem-mc/golem/internal/server/world"
	"github.com/golem-mc/golem/nbt"
	"github.com/golem-mc/golem/registry"
)

// The charge cursors a death left in the catalyst reach the region file
// under cursors, and a world opened on that file carries on spreading.
// vanilla: SculkCatalystBlockEntity.saveAdditional, loadAdditional
func TestACatalystKeepsItsCursorsAcrossASaveAndReload(t *testing.T) {
	t.Parallel()
	cfg := servertest.MobTestConfig()
	cfg.LevelName = t.TempDir()
	cfg.AutoSaveTicks = 0
	srv, _ := catalystSceneWith(t, cfg, "minecraft:stone", "zombie", `{NoAI:1b}`, protocol.EntityTypeZombie, 9, 9)
	servertest.AutoSaveEveryTick(srv)
	servertest.RunTicks(t, srv, servertest.AutoSaveEvery)

	w, err := world.NewWorld(cfg.LevelName, world.NewFlatGenerator())
	if err != nil {
		t.Fatalf("reopen world: %v", err)
	}
	defer w.Close()
	pos := world.ChunkPos{X: 0, Z: 0}
	w.Acquire(pos)
	defer w.Release(pos)
	tag, ok := w.BlockEntityTag(8, 64, 8)
	if !ok {
		t.Fatal("the saved catalyst has no block entity")
	}
	if list, _ := tag["cursors"].(nbt.List); len(list.Items) == 0 {
		t.Fatalf("the saved catalyst holds no cursors: %v", tag)
	}

	before := sculkInWorld(w)
	p := world.NewPhysics(w)
	p.SetSeed(7)
	for tick := int64(1); tick <= 40; tick++ {
		p.Tick(tick)
	}
	if after := sculkInWorld(w); after <= before {
		t.Errorf("the reloaded catalyst spread nothing: %d sculk before, %d after", before, after)
	}
}

func sculkInWorld(w *world.World) (n int) {
	for x := int32(-4); x <= 14; x++ {
		for y := int32(62); y <= 67; y++ {
			for z := int32(-6); z <= 10; z++ {
				if name, _, _ := registry.BlockStateName(w.BlockAt(x, y, z)); name == "minecraft:sculk" {
					n++
				}
			}
		}
	}
	return n
}
