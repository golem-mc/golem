package mobs

import (
	"github.com/golem-mc/golem/internal/server/servertest"

	"testing"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server"
	"github.com/golem-mc/golem/internal/server/world"
	"github.com/golem-mc/golem/nbt"
	"github.com/golem-mc/golem/registry"
)

// The mob breath on the wire: a summoned slime under water sends its air
// down as metadata index 1 and drowns at the floor, a zombie, which breathes
// under water, never sends the entry at all, and a cow with water_breathing
// holds its breath under water the way the effects spare it. The main loop is
// stopped and every mob tick is the test's own, so the countdown is a count
// of ticks rather than a guess at wall-clock time.

// mobAir is what one Set Entity Metadata packet said about a mob's breath.
type mobAir struct {
	entityID int32
	hasAir   bool
	air      int32
}

// readMobAir parses one Set Entity Metadata packet for the air entry,
// stepping over the other fields by their declared type. A field type this
// server never writes for these kinds fails the test.
func readMobAir(t *testing.T, data []byte) mobAir {
	t.Helper()
	r := protocol.NewReader(data)
	m := mobAir{entityID: r.ReadVarInt()}
	for r.Err() == nil {
		index := r.ReadU8()
		if index == protocol.MetadataEnd {
			break
		}
		switch fieldType := r.ReadVarInt(); fieldType {
		case protocol.MetadataByte, protocol.MetadataBoolean:
			r.ReadU8()
		case protocol.MetadataVarInt, protocol.MetadataPose, protocol.MetadataHumanoidArm:
			value := r.ReadVarInt()
			if index == protocol.MetadataIndexAir {
				m.hasAir, m.air = true, value
			}
		case protocol.MetadataFloat:
			r.ReadF32()
		default:
			t.Fatalf("mob metadata field %d of type %d", index, fieldType)
		}
	}
	if err := r.Err(); err != nil {
		t.Fatalf("mob metadata: %v", err)
	}
	return m
}

// summonInPool stands a survival watcher beside a walled, two-deep pool and
// summons a NoAI mob of the kind into it, with the main loop stopped. The
// walls keep a hopping cube in the water; NoAI keeps every other kind there.
func summonInPool(t *testing.T, kind string) (*server.Server, *servertest.Client, int32) {
	t.Helper()
	return summonInDeepPool(t, kind, "", 2)
}

// summonInDeepPool is summonInPool with the pool depth blocks deep and the
// extra comma-led NBT fields merged into the summon's NoAI.
func summonInDeepPool(t *testing.T, kind, extraNBT string, depth int32) (*server.Server, *servertest.Client, int32) {
	t.Helper()
	srv, c := servertest.Join(t, servertest.MobTestConfig(), "Watcher", servertest.OpsListing(t, "Watcher"))
	c.Settle()
	srv.StopTick()
	// The stopped loop streams no chunks, so the pool's own chunk is held by
	// the test: the mob's tick runs only while its chunk is.
	release := srv.HoldChunk(5, 5)
	t.Cleanup(release)

	water := servertest.BlockState(t, "minecraft:water")
	stone := servertest.BlockState(t, "minecraft:stone")
	for x := int32(4); x <= 6; x++ {
		for z := int32(4); z <= 6; z++ {
			srv.SetBlock(x, 63, z, stone)
			for y := int32(64); y < 64+depth; y++ {
				srv.SetBlock(x, y, z, water)
			}
		}
	}
	for y := int32(64); y <= 64+depth; y++ {
		for i := int32(4); i <= 6; i++ {
			srv.SetBlock(3, y, i, stone)
			srv.SetBlock(7, y, i, stone)
			srv.SetBlock(i, y, 3, stone)
			srv.SetBlock(i, y, 7, stone)
		}
	}
	// A cell the chunk never loaded is air, so the water cell is the proof
	// the pool went into a live chunk.
	if srv.BlockAt(5, 64, 5) != water {
		t.Fatal("the pool's chunk was not loaded")
	}

	c.ChatCommand("summon minecraft:" + kind + " 5.5 64 5.5 {NoAI:1b" + extraNBT + "}")
	typeID, ok := registry.EntityTypeID("minecraft:" + kind)
	if !ok {
		t.Fatalf("no entity type for minecraft:%s", kind)
	}
	id := c.ExpectSummonedMob(typeID)
	c.Settle()
	return srv, c, id
}

// TestASlimeInWaterSendsAirAndDrowns is the issue's repro: a summoned slime
// under water sends the air metadata index 1 as it counts down — the first
// tick says 299 — and the breath at the floor drowns it, the bubble burst
// event ahead of the bite.
func TestASlimeInWaterSendsAirAndDrowns(t *testing.T) {
	t.Parallel()
	srv, c, slimeID := summonInPool(t, "slime")

	srv.TickMobs(1)
	first, sawAir := int32(-1), false
	c.SettleAndCollect(func(id int32, data []byte) {
		if id != protocol.IDPlayEntityMetadata || protocol.NewReader(data).ReadVarInt() != slimeID {
			return
		}
		if m := readMobAir(t, data); m.hasAir && !sawAir {
			first, sawAir = m.air, true
		}
	})
	if !sawAir {
		t.Fatal("a tick under water sent no air metadata")
	}
	if first != 299 {
		t.Errorf("the first air metadata said %d, want 299", first)
	}

	// The rest of the countdown reaches the floor on the three hundred and
	// twentieth tick, the bite told of as the drown's own, its bubble burst
	// ahead of it.
	srv.TickMobs(319)
	drowned, burst := false, false
	c.SettleAndCollect(func(id int32, data []byte) {
		switch id {
		case protocol.IDPlayEntityEvent:
			r := protocol.NewReader(data)
			if r.ReadI32() == slimeID && r.ReadU8() == protocol.EntityEventDrown {
				burst = true
			}
		case protocol.IDPlayDamageEvent:
			r := protocol.NewReader(data)
			if r.ReadVarInt() != slimeID {
				return
			}
			if r.ReadVarInt() == registry.MustID("minecraft:damage_type", "minecraft:drown") {
				drowned = true
			}
		}
	})
	if !drowned {
		t.Error("the slime's breath at the floor never sent a drown damage event")
	}
	if !burst {
		t.Error("the slime's breath at the floor never sent the drown entity event")
	}
}

// TestAMobKeepsItsBreathAcrossARestart checks the Air a mob saves and loads:
// a cow with a part-spent breath is written out with it, and a restarted
// world stands it back up with the air it left rather than a full breath.
func TestAMobKeepsItsBreathAcrossARestart(t *testing.T) {
	t.Parallel()
	cfg := servertest.MobTestConfig()
	cfg.LevelName = t.TempDir()

	srv, c := servertest.Join(t, cfg, "Keeper")

	cowID := srv.SpawnMob("minecraft:cow", 5.5, 64, 5.5)
	c.ExpectMobSpawn(protocol.EntityTypeCow)

	// The loop stopped so the out-of-water refill cannot climb the breath
	// between the set and the save.
	srv.StopTick()
	srv.SetMobAir(cowID, 123)
	srv.FlushMobs()

	servertest.EntityFileIs(t, cfg.LevelName, world.ChunkPos{X: 0, Z: 0}, "minecraft:cow", func(compound nbt.Compound) {
		air, ok := nbt.Get[nbt.Short](compound, "Air")
		if !ok {
			t.Fatal("the saved cow has no Air")
		}
		if air != 123 {
			t.Errorf("the saved cow's Air = %d, want the 123 it had", air)
		}
	})
	srv.Shutdown()

	restarted := *cfg
	_, reconnect := servertest.Start(t, &restarted)
	again := reconnect()
	again.Login("Keeper")
	again.Configure()
	shown := again.JoinCapturing("Keeper", map[int32]string{
		protocol.EntityTypeCow: "cow",
	})
	back := servertest.CapturedOr(t, again, shown, "cow", protocol.EntityTypeCow)
	entry := servertest.MetaOf(back.Meta, protocol.MetadataIndexAir)
	if entry == nil {
		t.Fatal("the returned cow carries no air, want the part-spent breath it was saved with")
	}
	if entry.Typ != protocol.MetadataVarInt {
		t.Errorf("cow air field type = %d, want %d (a varint)", entry.Typ, protocol.MetadataVarInt)
	}
	if entry.VarintVal < 123 || entry.VarintVal >= 300 {
		t.Errorf("the returned cow breathes %d, want the 123 it was saved with, plus the few ticks since", entry.VarintVal)
	}
}
