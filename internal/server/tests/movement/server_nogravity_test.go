package movement

import (
	"github.com/golem-mc/golem/internal/server/servertest"
	"math"
	"testing"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server"
	"github.com/golem-mc/golem/internal/server/world"
	"github.com/golem-mc/golem/nbt"
	"github.com/golem-mc/golem/registry"
)

// The NoGravity tag: a body the server holds up carries Entity.DATA_NO_GRAVITY
// on the wire and skips the gravity step in its tick, whether it was summoned
// with the tag or read back from a world that saved it.

// noGravityIndex is Entity.DATA_NO_GRAVITY's metadata index, the field a
// client reads to hold a body up rather than predict its fall.
const noGravityIndex = 5

// TestASummonedNoGravityItemHangsInPlace summons a stick five blocks up with
// NoGravity and holds it there: the spawn tells the client the body has no
// gravity at the metadata field, and forty hand-driven ticks leave it at the
// height the summon named.
func TestASummonedNoGravityItemHangsInPlace(t *testing.T) {
	t.Parallel()
	cfg := servertest.BlockTestConfig()
	cfg.LevelName = t.TempDir()

	srv, c := servertest.Join(t, cfg, "Summoner", servertest.OpsListing(t, "Summoner"))

	// Away from the summoner, so a body that does fall on main lands clear of
	// the pickup range that would take it off the ground.
	c.ChatCommand(`summon minecraft:item 5.5 70 5.5 {Item:{id:"minecraft:stick"},NoGravity:1b}`)

	spawned := c.AwaitSpawn(protocol.EntityTypeItem)
	if spawned.Y < 69.5 || spawned.Y > 70.5 {
		t.Fatalf("the summoned item spawned at y=%v, want the 70 the summon named", spawned.Y)
	}
	if e := servertest.MetaOf(c.AwaitMetadataFor(spawned.EntityID), noGravityIndex); e == nil || !e.FlagVal {
		t.Errorf("the item's metadata at index %d = %v, want the no-gravity flag", noGravityIndex, e)
	}

	// The live loop stopped, the hang is the hand-driven ticks' to hold.
	srv.StopTick()
	before := servertest.SoleItemY(t, srv)
	srv.TickEntities(40)
	if after := servertest.SoleItemY(t, srv); math.Abs(after-before) > 0.01 {
		t.Errorf("the no-gravity item fell from y=%v to y=%v over forty ticks, want it hanging in place", before, after)
	}
}

// TestASummonedNoGravityMobDoesNotFall summons a cow high with NoGravity: the
// metadata says so and the body hangs where it was summoned under forty
// hand-driven mob ticks.
func TestASummonedNoGravityMobDoesNotFall(t *testing.T) {
	t.Parallel()
	cfg := servertest.BlockTestConfig()
	cfg.LevelName = t.TempDir()

	srv, c := servertest.Join(t, cfg, "Summoner", servertest.OpsListing(t, "Summoner"))

	c.ChatCommand("summon minecraft:cow 5.5 70 5.5 {NoGravity:1b}")

	spawned := c.AwaitSpawn(protocol.EntityTypeCow)
	if e := servertest.MetaOf(c.AwaitMetadataFor(spawned.EntityID), noGravityIndex); e == nil || !e.FlagVal {
		t.Errorf("the mob's metadata at index %d = %v, want the no-gravity flag", noGravityIndex, e)
	}

	srv.StopTick()
	_, before, _, ok := srv.MobPos(spawned.EntityID)
	if !ok {
		t.Fatal("the summoned cow is not in the herd")
	}
	srv.TickMobs(40)
	_, after, _, ok := srv.MobPos(spawned.EntityID)
	if !ok {
		t.Fatal("the cow left the herd before the forty ticks were over")
	}
	if math.Abs(after-before) > 0.01 {
		t.Errorf("the no-gravity cow fell from y=%v to y=%v over forty ticks, want it hanging", before, after)
	}
}

// TestANoGravityItemSurvivesASaveAndReload plants an item carrying the tag in
// a chunk's entity file — the state a vanilla world saves it in — and reads it
// back: the reload keeps it in the air, and the flag survives the save that
// writes it out again.
func TestANoGravityItemSurvivesASaveAndReload(t *testing.T) {
	t.Parallel()
	cfg := servertest.BlockTestConfig()
	cfg.LevelName = t.TempDir()

	servertest.PlantChunkEntities(t, cfg.LevelName, world.ChunkPos{X: 0, Z: 0}, nbt.Compound{
		"id":   nbt.String("minecraft:item"),
		"UUID": servertest.UuidWords(servertest.FixtureUUID(4, 2, 0, 7)),
		"Pos": nbt.List{Elem: nbt.TypeDouble, Items: []nbt.Tag{
			nbt.Double(5.5), nbt.Double(70), nbt.Double(5.5),
		}},
		"Motion":    nbt.List{Elem: nbt.TypeDouble, Items: []nbt.Tag{nbt.Double(0), nbt.Double(0), nbt.Double(0)}},
		"NoGravity": nbt.Byte(1),
		"Item": nbt.Compound{
			"id":    nbt.String("minecraft:stick"),
			"count": nbt.Int(1),
		},
	})

	srv, _ := servertest.Join(t, cfg, "Keeper")

	servertest.WaitFor(t, srv, 200, func() bool { return srv.ItemCount() == 1 })
	srv.StopTick()

	before := servertest.SoleItemY(t, srv)
	srv.TickEntities(40)
	if after := servertest.SoleItemY(t, srv); math.Abs(after-before) > 0.01 {
		t.Errorf("the reloaded no-gravity item fell from y=%v to y=%v over forty ticks, want it hanging", before, after)
	}

	srv.FlushMobs()
	servertest.EntityFileIs(t, cfg.LevelName, world.ChunkPos{X: 0, Z: 0}, "minecraft:item", func(compound nbt.Compound) {
		flag, ok := nbt.Get[nbt.Byte](compound, "NoGravity")
		if !ok || flag != 1 {
			t.Errorf("the reloaded item saved NoGravity = %v (present %v), want 1", flag, ok)
		}
	})
}

// TestASummonedNoGravityOrbHangsInPlace summons an experience orb high with
// NoGravity: the metadata says so, and forty hand-driven entity ticks leave
// the orb at the height the summon named.
func TestASummonedNoGravityOrbHangsInPlace(t *testing.T) {
	t.Parallel()
	cfg := servertest.BlockTestConfig()
	cfg.LevelName = t.TempDir()

	srv, c := servertest.Join(t, cfg, "Summoner", servertest.OpsListing(t, "Summoner"))

	c.ChatCommand(`summon minecraft:experience_orb 5.5 70 5.5 {Value:1,NoGravity:1b}`)

	spawned := c.AwaitSpawn(protocol.EntityTypeExperienceOrb)
	if e := servertest.MetaOf(c.AwaitMetadataFor(spawned.EntityID), noGravityIndex); e == nil || !e.FlagVal {
		t.Errorf("the orb's metadata at index %d = %v, want the no-gravity flag", noGravityIndex, e)
	}

	srv.StopTick()
	_, before, _, ok := srv.OrbPos(spawned.EntityID)
	if !ok {
		t.Fatal("the summoned orb is not in the world")
	}
	srv.TickEntities(40)
	_, after, _, ok := srv.OrbPos(spawned.EntityID)
	if !ok {
		t.Fatal("the orb left the world before the forty ticks were over")
	}
	if math.Abs(after-before) > 0.01 {
		t.Errorf("the no-gravity orb fell from y=%v to y=%v over forty ticks, want it hanging", before, after)
	}
}

// TestASummonedNoGravityTNTDoesNotFall summons a primed TNT high with
// NoGravity: the metadata says so, and forty hand-driven entity ticks leave
// the body at the height the summon named while its fuse burns.
func TestASummonedNoGravityTNTDoesNotFall(t *testing.T) {
	t.Parallel()
	cfg := servertest.BlockTestConfig()
	cfg.LevelName = t.TempDir()

	srv, c := servertest.Join(t, cfg, "Summoner", servertest.OpsListing(t, "Summoner"))

	c.ChatCommand("summon minecraft:tnt 5.5 70 5.5 {NoGravity:1b,fuse:1000s}")

	spawned := c.AwaitSpawn(protocol.EntityTypeTnt)
	if e := servertest.MetaOf(c.AwaitMetadataFor(spawned.EntityID), noGravityIndex); e == nil || !e.FlagVal {
		t.Errorf("the primed TNT's metadata at index %d = %v, want the no-gravity flag", noGravityIndex, e)
	}

	srv.StopTick()
	before := servertest.SoleTNTY(t, srv)
	srv.TickEntities(40)
	if after := servertest.SoleTNTY(t, srv); math.Abs(after-before) > 0.01 {
		t.Errorf("the no-gravity primed TNT fell from y=%v to y=%v over forty ticks, want it hanging", before, after)
	}
}

// TestANoGravityArrowSurvivesASaveAndReload plants an arrow carrying the tag
// in a chunk's entity file and reads it back: the reload keeps it in the air,
// its own tick leaves it where it was, and the flag survives the save that
// writes it out again.
func TestANoGravityArrowSurvivesASaveAndReload(t *testing.T) {
	t.Parallel()
	cfg := servertest.BlockTestConfig()
	cfg.LevelName = t.TempDir()

	servertest.PlantChunkEntities(t, cfg.LevelName, world.ChunkPos{X: 0, Z: 0}, nbt.Compound{
		"id":   nbt.String("minecraft:arrow"),
		"UUID": servertest.UuidWords(servertest.FixtureUUID(4, 2, 0, 8)),
		"Pos": nbt.List{Elem: nbt.TypeDouble, Items: []nbt.Tag{
			nbt.Double(5.5), nbt.Double(70), nbt.Double(5.5),
		}},
		"Motion":    nbt.List{Elem: nbt.TypeDouble, Items: []nbt.Tag{nbt.Double(0), nbt.Double(0), nbt.Double(0)}},
		"inGround":  nbt.Byte(0),
		"NoGravity": nbt.Byte(1),
	})

	srv, _ := servertest.Join(t, cfg, "Keeper")

	servertest.WaitFor(t, srv, 200, func() bool { return srv.ArrowCount() == 1 })
	srv.StopTick()

	before := soleArrowY(t, srv)
	srv.TickEntities(40)
	if after := soleArrowY(t, srv); math.Abs(after-before) > 0.01 {
		t.Errorf("the reloaded no-gravity arrow fell from y=%v to y=%v over forty ticks, want it hanging", before, after)
	}

	srv.FlushMobs()
	servertest.EntityFileIs(t, cfg.LevelName, world.ChunkPos{X: 0, Z: 0}, "minecraft:arrow", func(compound nbt.Compound) {
		flag, ok := nbt.Get[nbt.Byte](compound, "NoGravity")
		if !ok || flag != 1 {
			t.Errorf("the reloaded arrow saved NoGravity = %v (present %v), want 1", flag, ok)
		}
	})
}

// TestANoGravityFallingBlockSurvivesASaveAndReload plants a falling block
// carrying the tag in a chunk's entity file and reads it back: the reload
// keeps it in the air, the fall's own tick leaves it there, and the flag
// survives the save that writes it out again.
func TestANoGravityFallingBlockSurvivesASaveAndReload(t *testing.T) {
	t.Parallel()
	cfg := servertest.BlockTestConfig()
	cfg.LevelName = t.TempDir()

	servertest.PlantChunkEntities(t, cfg.LevelName, world.ChunkPos{X: 0, Z: 0}, nbt.Compound{
		"id":   nbt.String("minecraft:falling_block"),
		"UUID": servertest.UuidWords(servertest.FixtureUUID(4, 2, 0, 9)),
		"Pos": nbt.List{Elem: nbt.TypeDouble, Items: []nbt.Tag{
			nbt.Double(5.5), nbt.Double(70), nbt.Double(5.5),
		}},
		"Motion":     nbt.List{Elem: nbt.TypeDouble, Items: []nbt.Tag{nbt.Double(0), nbt.Double(0), nbt.Double(0)}},
		"NoGravity":  nbt.Byte(1),
		"BlockState": nbt.Compound{"Name": nbt.String("minecraft:sand")},
	})

	srv, _ := servertest.Join(t, cfg, "Keeper")

	servertest.WaitFor(t, srv, 200, func() bool { return srv.FallingCount() == 1 })
	srv.StopTick()

	srv.TickFallings(40)
	if n := srv.FallingCount(); n != 1 {
		t.Fatalf("falling blocks in the world after forty ticks = %d, want the one hanging", n)
	}

	srv.FlushMobs()
	servertest.EntityFileIs(t, cfg.LevelName, world.ChunkPos{X: 0, Z: 0}, "minecraft:falling_block", func(compound nbt.Compound) {
		flag, ok := nbt.Get[nbt.Byte](compound, "NoGravity")
		if !ok || flag != 1 {
			t.Errorf("the reloaded falling block saved NoGravity = %v (present %v), want 1", flag, ok)
		}
	})
}

// TestAZombieKeepsNoGravityThroughDrowning checks a conversion carries the
// flag: a NoGravity zombie drowned in a pool comes back as a NoGravity
// drowned, saved with the tag the zombie was summoned with.
func TestAZombieKeepsNoGravityThroughDrowning(t *testing.T) {
	t.Parallel()
	cfg := servertest.HostileTestConfig()
	cfg.GameMode = "creative"
	cfg.LevelName = t.TempDir()
	srv, c := servertest.Join(t, cfg, "Diver", servertest.OpsListing(t, "Diver"))
	srv.StopTick()

	water, _ := registry.FindState("minecraft:water", nil)
	stone, _ := registry.FindState("minecraft:stone", nil)
	for x := int32(2); x <= 6; x++ {
		for z := int32(-2); z <= 2; z++ {
			for y := int32(64); y <= 68; y++ {
				if x == 2 || x == 6 || z == -2 || z == 2 {
					srv.SetBlock(x, y, z, stone)
				} else {
					srv.SetBlock(x, y, z, water)
				}
			}
		}
	}
	c.ChatCommand("summon minecraft:zombie 3.5 64 0.5 {NoGravity:1b}")
	c.Settle()
	srv.TickMobs(drownTicks)

	srv.FlushMobs()
	srv.Shutdown()
	servertest.EntityFileIs(t, cfg.LevelName, world.ChunkPos{X: 0, Z: 0}, "minecraft:drowned", func(compound nbt.Compound) {
		flag, ok := nbt.Get[nbt.Byte](compound, "NoGravity")
		if !ok || flag != 1 {
			t.Errorf("the converted drowned saved NoGravity = %v (present %v), want the zombie's 1", flag, ok)
		}
	})
}

// soleArrowY is the height of the one arrow flying in the overworld, failing
// the test when the world holds none or more than one.
func soleArrowY(t *testing.T, srv *server.Server) float64 {
	t.Helper()
	arrows := srv.ArrowsInDimension(world.DimensionName)
	if len(arrows) != 1 {
		t.Fatalf("arrows in the overworld = %d, want the one under test", len(arrows))
	}
	return arrows[0].Y
}

// drownTicks is the underwater conversion's 600 and 300 ticks, and a few for
// the eyes to go under.
const drownTicks = 600 + 300 + 5
