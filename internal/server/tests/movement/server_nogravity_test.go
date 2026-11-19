package movement

import (
	"math"
	"testing"

	"github.com/golem-mc/golem/internal/server/servertest"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server"
	"github.com/golem-mc/golem/internal/server/world"
	"github.com/golem-mc/golem/nbt"
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
