package world

import (
	"bytes"
	"slices"
	"testing"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/nbt"
	"github.com/golem-mc/golem/registry"
)

// chunkPacketReference is the Chunk Data payload appended field by field into
// one growing buffer, against which ChunkPacket's sized build is checked.
func chunkPacketReference(w *World, c *Chunk) []byte {
	w.mu.RLock()
	var b protocol.Buffer
	b.PutI32(c.x)
	b.PutI32(c.z)
	b.PutRaw(c.heightmaps)
	b.PutBytes(c.encoded)
	light := w.chunkLightWired(c)
	own := c.blockEntities()
	rides := c.sortedPistonRides()
	carried := slices.Clone(c.carriedBlockEntities.Items)
	active := c.activeTrialSpawners(carried)
	w.mu.RUnlock()
	putBlockEntities(&b, own, rides, carried, active)
	b.PutRaw(light)
	return b.Bytes()
}

// TestChunkPacketMatchesReference checks ChunkPacket's bytes against the
// reference for a chunk with no block entities, then with a chest, a sign with
// text and a carried banner.
func TestChunkPacketMatchesReference(t *testing.T) {
	t.Parallel()
	w, _ := NewWorld(t.TempDir(), NewFlatGenerator())
	defer w.Close()
	pos := ChunkPos{X: 1, Z: -2}
	c, _ := w.Acquire(pos)
	defer w.Release(pos)
	x0, z0 := pos.X*16, pos.Z*16

	check := func(what string) {
		t.Helper()
		got := w.ChunkPacket(c)
		if want := chunkPacketReference(w, c); !bytes.Equal(got, want) {
			t.Errorf("%s: ChunkPacket differs from the reference (%d bytes, want %d)", what, len(got), len(want))
		}
	}
	check("no block entities")

	w.SetBlock(x0+1, GroundLevel, z0+1, registry.BlockStates["minecraft:chest"])
	w.EnsureBlockEntity(x0+1, GroundLevel, z0+1, KindChest)
	sign, _ := registry.FindState("minecraft:oak_sign", map[string]string{"rotation": "0"})
	w.SetBlock(x0+5, GroundLevel, z0+9, sign)
	w.EnsureBlockEntity(x0+5, GroundLevel, z0+9, KindSign).Use(func(b *BlockEntity) {
		b.SignFront.Messages[0] = "a line long enough to need its own room"
	})
	w.mu.Lock()
	c.carriedBlockEntities = nbt.List{Elem: nbt.TypeCompound, Items: []nbt.Tag{nbt.Compound{
		"id": nbt.String("minecraft:banner"),
		"x":  nbt.Int(x0 + 15), "y": nbt.Int(GroundLevel), "z": nbt.Int(z0 + 15),
		"CustomName": nbt.String(`"Flag"`),
	}}}
	w.mu.Unlock()
	check("block entities")
}

// TestChunkSectionsMatchReference checks the cached section form of generated
// overworld and nether chunks against their sections written one after another
// into one growing buffer.
func TestChunkSectionsMatchReference(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	seedWorldDir(t, dir, 5)
	overworld, _ := NewWorld(dir, NewOverworldGenerator(5))
	for _, w := range []*World{overworld, NewDimensionWorld(t.TempDir(), true, NewNetherGenerator(7))} {
		defer w.Close()
		for i := range int32(4) {
			c := newChunk(i*5, -i*3)
			w.gen.Generate(c)
			var want protocol.Buffer
			for s := w.firstSection(); s < w.firstSection()+w.wireSections(); s++ {
				c.sections[s].encode(&want)
			}
			if got := w.encodeChunkWired(c); !bytes.Equal(got, want.Bytes()) {
				t.Errorf("chunk (%d,%d): sections differ from the reference", c.x, c.z)
			}
		}
	}
}

// TestCachedChunkPacketInvalidation checks that CachedChunkPacket reuses the
// cached packet when nothing changed and rebuilds after every kind of block
// entity mutation (modelled Use/Put, piston rides, carried edits, hive ticks,
// end gateway ticks and trial spawner state changes).
func TestCachedChunkPacketInvalidation(t *testing.T) {
	t.Parallel()
	w, _ := NewWorld(t.TempDir(), NewFlatGenerator())
	defer w.Close()
	pos := ChunkPos{X: 0, Z: 0}
	c, _ := w.Acquire(pos)
	defer w.Release(pos)

	checkMatches := func(what string) *protocol.CachedPacket {
		t.Helper()
		pkt := w.CachedChunkPacket(c)
		if want := chunkPacketReference(w, c); !bytes.Equal(pkt.Data(), want) {
			t.Fatalf("%s: CachedChunkPacket differs from reference (%d bytes, want %d)", what, len(pkt.Data()), len(want))
		}
		return pkt
	}

	// Sign mutation via Use without SetBlock/Touch.
	w.SetBlock(1, GroundLevel, 1, registry.BlockOakSign.DefaultState())
	sign := w.EnsureBlockEntity(1, GroundLevel, 1, KindSign)
	sign.Use(func(b *BlockEntity) { b.SignFront.Messages[0] = "before" })
	p1 := checkMatches("sign initial")
	if p2 := w.CachedChunkPacket(c); p2 != p1 {
		t.Fatal("unchanged chunk with sign did not reuse cached packet")
	}

	// Read-only Use advances useGen; bytes.Equal fallback must still reuse p1
	// and advance ownGens so a subsequent call hits the fast path.
	sign.Use(func(b *BlockEntity) { _ = b.SignFront.Messages[0] })
	if p2 := w.CachedChunkPacket(c); p2 != p1 {
		t.Fatal("read-only Use did not reuse cached packet")
	}
	if p3 := w.CachedChunkPacket(c); p3 != p1 {
		t.Fatal("call after read-only Use did not reuse cached packet")
	}

	// Mutating Use must rebuild.
	sign.Use(func(b *BlockEntity) { b.SignFront.Messages[0] = "after" })
	p2 := checkMatches("sign mutated")
	if p2 == p1 {
		t.Fatal("sign mutation via Use reused stale cached packet")
	}

	// Campfire mutation via Put inside Use.
	w.SetBlock(2, GroundLevel+1, 2, registry.BlockCampfire.DefaultState())
	campfire := w.EnsureBlockEntity(2, GroundLevel+1, 2, KindCampfire)
	p3 := checkMatches("campfire empty")
	campfire.Use(func(b *BlockEntity) {
		b.PlaceFood(protocol.ItemStack{ItemID: int32(registry.ItemPorkchop), Count: 1})
	})
	p4 := checkMatches("campfire food placed")
	if p4 == p3 {
		t.Fatal("campfire PlaceFood reused stale cached packet")
	}

	// Piston ride add, update and remove.
	p := NewPhysics(w)
	ride := movingPistonStateOf(dirEast, false)
	w.SetBlock(3, GroundLevel+1, 3, ride)
	p.pistonRideCreate([3]int32{3, GroundLevel + 1, 3}, ride, registry.BlockDirt.DefaultState(), dirEast, true, false)
	p5 := checkMatches("piston ride created")
	if p6 := w.CachedChunkPacket(c); p6 != p5 {
		t.Fatal("unchanged chunk with piston ride did not reuse cached packet")
	}
	w.SetBlockEntityTag(3, GroundLevel+1, 3, KindPiston.Name(), nbt.Compound{
		BlockStateKeyPiston: nbt.Compound{"Name": nbt.String("minecraft:stone")},
		"facing":            nbt.Int(int32(dirWest)),
		"extending":         nbt.Byte(0),
		"source":            nbt.Byte(0),
	})
	p6 := checkMatches("piston ride updated")
	if p6 == p5 {
		t.Fatal("piston ride update reused stale cached packet")
	}
	w.mu.Lock()
	c.removePistonRide(3, GroundLevel+1, 3)
	w.mu.Unlock()
	p7 := checkMatches("piston ride removed")
	if p7 == p6 {
		t.Fatal("piston ride removal reused stale cached packet")
	}

	// Carried banner: SetCarriedField and CarriedBlockEntity.Edit.
	w.SetBlock(4, GroundLevel+1, 4, registry.BlockWhiteBanner.DefaultState())
	w.SetBlockEntityTag(4, GroundLevel+1, 4, "minecraft:banner", nbt.Compound{"CustomName": nbt.String(`"One"`)})
	p8 := checkMatches("banner initial")
	w.SetCarriedField(4, GroundLevel+1, 4, "CustomName", nbt.String(`"Two"`))
	p9 := checkMatches("banner SetCarriedField")
	if p9 == p8 {
		t.Fatal("SetCarriedField reused stale cached packet")
	}
	handle := w.CarriedBlockEntity(4, GroundLevel+1, 4)
	if handle == nil || !handle.Edit(func(tag nbt.Compound) { tag["CustomName"] = nbt.String(`"Three"`) }) {
		t.Fatal("CarriedBlockEntity.Edit failed")
	}
	p10 := checkMatches("banner Edit")
	if p10 == p9 {
		t.Fatal("CarriedBlockEntity.Edit reused stale cached packet")
	}

	// Carried end gateway: TickEndGateways updates Age in place.
	w.SetBlock(5, GroundLevel+1, 5, registry.BlockEndGateway.DefaultState())
	w.SetBlockEntityTag(5, GroundLevel+1, 5, "minecraft:end_gateway", nbt.Compound{"Age": nbt.Long(1)})
	p11 := checkMatches("end gateway initial")
	w.TickEndGateways(2400)
	p12 := checkMatches("end gateway ticked")
	if p12 == p11 {
		t.Fatal("TickEndGateways reused stale cached packet")
	}

	// Trial spawner active state change via block state write.
	inactive, ok := registry.FindState("minecraft:trial_spawner", map[string]string{"trial_spawner_state": "waiting_for_players"})
	if !ok {
		t.Fatal("inactive trial_spawner state not found")
	}
	active, ok := registry.FindState("minecraft:trial_spawner", map[string]string{"trial_spawner_state": "active"})
	if !ok {
		t.Fatal("active trial_spawner state not found")
	}
	w.SetBlock(6, GroundLevel+1, 6, inactive)
	w.SetBlockEntityTag(6, GroundLevel+1, 6, "minecraft:trial_spawner", nbt.Compound{
		"next_mob_spawns_at": nbt.Long(1234),
	})
	p13 := checkMatches("trial spawner inactive")
	w.SetBlock(6, GroundLevel+1, 6, active)
	p14 := checkMatches("trial spawner active")
	if p14 == p13 {
		t.Fatal("trial spawner activation reused stale cached packet")
	}
}

// TestCachedChunkPacketZeroAllocsOnHit verifies that repeated CachedChunkPacket
// calls on an unchanged chunk with update-tag-bearing entities and piston rides
// do not allocate.
func TestCachedChunkPacketZeroAllocsOnHit(t *testing.T) {
	w, _ := NewWorld(t.TempDir(), NewFlatGenerator())
	defer w.Close()
	c, _ := w.Acquire(ChunkPos{})
	defer w.Release(ChunkPos{})

	w.SetBlock(1, GroundLevel, 1, registry.BlockOakSign.DefaultState())
	w.EnsureBlockEntity(1, GroundLevel, 1, KindSign).Use(func(b *BlockEntity) {
		b.SignFront.Messages[0] = "cached"
	})
	w.SetBlock(2, GroundLevel+1, 2, registry.BlockCampfire.DefaultState())
	w.EnsureBlockEntity(2, GroundLevel+1, 2, KindCampfire).Use(func(b *BlockEntity) {
		b.PlaceFood(protocol.ItemStack{ItemID: int32(registry.ItemPorkchop), Count: 1})
	})
	p := NewPhysics(w)
	ride := movingPistonStateOf(dirEast, false)
	w.SetBlock(3, GroundLevel+1, 3, ride)
	p.pistonRideCreate([3]int32{3, GroundLevel + 1, 3}, ride, registry.BlockDirt.DefaultState(), dirEast, true, false)

	// Warm the cache.
	w.CachedChunkPacket(c)
	if allocs := testing.AllocsPerRun(50, func() { w.CachedChunkPacket(c) }); allocs != 0 {
		t.Fatalf("CachedChunkPacket cache hit allocated %.1f times/op, want 0", allocs)
	}
}
