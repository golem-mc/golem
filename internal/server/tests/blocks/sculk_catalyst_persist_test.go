package blocks

import (
	"fmt"
	"math"
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

// Each cell with a cursor gets a charge event whose data is the particle
// count above bit 6 and the cursor's faces below; a spent cursor pops with
// data 0, and the catalyst's bloom sends its soul particles.
// vanilla: SculkSpreader.updateCursors, CatalystListener.bloom
func TestCursorsSendChargeEventsAndTheBloomSendsSoulParticles(t *testing.T) {
	t.Parallel()
	srv, c := catalystSceneWith(t, servertest.MobTestConfig(), "minecraft:stone", "zombie", `{NoAI:1b}`, protocol.EntityTypeZombie, 9, 9)
	soul, _, _ := registry.ParticleID("minecraft:sculk_soul")
	var charged, popped, souls int
	record := func(id int32, data []byte) {
		switch id {
		case protocol.IDPlayWorldEvent:
			r := protocol.NewReader(data)
			if r.ReadI32() != 3006 {
				return
			}
			r.ReadPosition()
			d := r.ReadI32()
			switch {
			case d == 0:
				popped++
			case d>>6 >= 1 && d&63 != 0:
				charged++
			default:
				t.Errorf("charge event data %d has no particle count or no faces", d)
			}
		case protocol.IDPlayLevelParticles:
			r := protocol.NewReader(data)
			kind := r.ReadVarInt()
			if tail := servertest.ReadParticleTail(t, r); tail.Count == 2 && kind == soul {
				souls++
			}
		}
	}
	c.SettleAndCollect(record)
	for range 20 {
		servertest.RunTicks(t, srv, 4)
		c.SettleAndCollect(record)
	}
	if charged == 0 || popped == 0 || souls == 0 {
		t.Errorf("charge events %d, pops %d, soul bursts %d; want all above zero", charged, popped, souls)
	}
}

// Soul sand tops out two sixteenths under a full block, so a body standing on
// a cell that turns to sculk is lifted onto the new top.
// vanilla: SculkVeinBlock.attemptPlaceSculk, Block.pushEntitiesUp
func TestSculkConvertingALowBlockLiftsTheBodyOnIt(t *testing.T) {
	t.Parallel()
	srv, c := catalystSceneWith(t, servertest.MobTestConfig(), "minecraft:soul_sand", "zombie", `{NoAI:1b}`, protocol.EntityTypeZombie, 9, 9)
	type cow struct {
		id   int32
		x, z int32
	}
	// A cell that was not soul sand before the cow came, sculk or the stone
	// past the scene's floor, had nothing to lift it.
	notSoulSand := map[[2]int32]bool{}
	for x := int32(9); x <= 11; x++ {
		for z := int32(9); z <= 11; z++ {
			name, _, _ := registry.BlockStateName(srv.BlockAt(x, 63, z))
			notSoulSand[[2]int32{x, z}] = name != "minecraft:soul_sand"
			c.ChatCommand(fmt.Sprintf("summon minecraft:cow %d.5 63.875 %d.5 {NoAI:1b,Silent:1b}", x, z))
		}
	}
	var cows []cow
	c.SettleAndCollect(func(id int32, data []byte) {
		if id != protocol.IDPlaySpawnEntity {
			return
		}
		r := protocol.NewReader(data)
		eid := r.ReadVarInt()
		r.ReadUUID()
		if r.ReadVarInt() == protocol.EntityTypeCow {
			x, _, z := r.ReadF64(), r.ReadF64(), r.ReadF64()
			cows = append(cows, cow{eid, int32(math.Floor(x)), int32(math.Floor(z))})
		}
	})
	if len(cows) != 9 {
		t.Fatalf("%d cows spawned, want 9", len(cows))
	}
	servertest.RunTicks(t, srv, 150)
	converted := 0
	for _, k := range cows {
		_, y, _, ok := srv.MobPos(k.id)
		if !ok {
			t.Fatalf("cow %d is gone", k.id)
		}
		if name, _, _ := registry.BlockStateName(srv.BlockAt(k.x, 63, k.z)); name != "minecraft:sculk" || notSoulSand[[2]int32{k.x, k.z}] {
			continue
		}
		converted++
		if y < 64 {
			t.Errorf("the cow over the sculk at (%d,%d) stands at y=%v, want 64", k.x, k.z, y)
		}
	}
	if converted == 0 {
		t.Fatal("no cell under a cow turned to sculk")
	}
}
