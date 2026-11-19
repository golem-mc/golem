package commands

import (
	"bytes"
	"fmt"
	"github.com/golem-mc/golem/internal/server/servertest"
	"math"
	"path/filepath"
	"reflect"
	"testing"
	"time"

	"github.com/golem-mc/golem/internal/config"
	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server"
	"github.com/golem-mc/golem/internal/server/world"
	"github.com/golem-mc/golem/nbt"
	"github.com/golem-mc/golem/registry"
)

// TestASummonReadsTheMobTagsASavedMobDoes summons a pig whose compound names
// every field a save keeps — the custom name and its visibility, the
// persistence, the variant and sound variant, the age and its lock, and
// NoAI — and reads each back off the spawn metadata and the compound the
// world later saves: the summon's tag goes through the same reader a chunk's
// saved mobs do.
func TestASummonReadsTheMobTagsASavedMobDoes(t *testing.T) {
	t.Parallel()
	cfg := servertest.MobTestConfig()
	srv, c := servertest.Join(t, cfg, "Summoner", servertest.OpsListing(t, "Summoner"))

	c.ChatCommand(`summon minecraft:pig 3.5 64 3.5 {CustomName:"Bob",CustomNameVisible:1b,` +
		`PersistenceRequired:1b,variant:"minecraft:cold",sound_variant:"minecraft:big",` +
		`Age:-100,AgeLocked:1b,NoAI:1b}`)
	pig := c.ExpectMobSpawn(protocol.EntityTypePig)

	if !reflect.DeepEqual(pig.Name, nbt.String("Bob")) {
		t.Errorf("the summoned pig's CustomName = %v, want Bob", pig.Name)
	}
	if !pig.NameVisible {
		t.Error("the summoned pig's CustomNameVisible was not read")
	}
	if !pig.Baby {
		t.Error("the summoned pig's Age:-100 did not make it a baby")
	}
	if !pig.AgeLocked {
		t.Error("the summoned pig's AgeLocked was not read")
	}
	if pig.MobFlags&protocol.EntityFlagNoAI == 0 {
		t.Errorf("the summoned pig's mob flags = %#x, want the NoAI bit", pig.MobFlags)
	}
	if want := registry.MustID("minecraft:pig_variant", "minecraft:cold"); pig.Variant != want {
		t.Errorf("the summoned pig's variant = %d, want the cold %d", pig.Variant, want)
	}
	if want := registry.MustID("minecraft:pig_sound_variant", "minecraft:big"); pig.SoundVariant != want {
		t.Errorf("the summoned pig's sound variant = %d, want the big %d", pig.SoundVariant, want)
	}

	c.Disconnect()
	servertest.PollUntil(t, 10*time.Second, func() bool { return srv.MobCount() == 0 })
	srv.FlushMobs()

	saved := servertest.SavedMobsByUUID(t, cfg.LevelName)
	if len(saved) != 1 {
		t.Fatalf("the world saved %d mobs, want the pig", len(saved))
	}
	for _, compound := range saved {
		for key, want := range map[string]nbt.Tag{
			"CustomName":          nbt.String("Bob"),
			"CustomNameVisible":   nbt.Byte(1),
			"PersistenceRequired": nbt.Byte(1),
			"variant":             nbt.String("minecraft:cold"),
			"sound_variant":       nbt.String("minecraft:big"),
			"Age":                 nbt.Int(-100),
			"AgeLocked":           nbt.Byte(1),
			"NoAI":                nbt.Byte(1),
		} {
			if got := compound[key]; !reflect.DeepEqual(got, want) {
				t.Errorf("the summoned pig saved %s = %v, want %v", key, got, want)
			}
		}
	}
}

// TestASummonKeepsAMixedListTagThroughTheSave summons a pig whose unknown
// tag holds a list of mixed types, then saves the chunk: the summon must not
// stop the chunk's write. vanilla: ListTag wraps a heterogeneous list as
// {"":value}, so it stays homogeneous on disk.
func TestASummonKeepsAMixedListTagThroughTheSave(t *testing.T) {
	t.Parallel()
	cfg := servertest.MobTestConfig()
	srv, c := servertest.Join(t, cfg, "Summoner", servertest.OpsListing(t, "Summoner"))

	c.ChatCommand(`summon minecraft:pig 1.5 64 0.5 {foo:[1,"x"]}`)
	c.ExpectMobSpawn(protocol.EntityTypePig)
	c.ChatCommand(`summon minecraft:pig 3.5 64 0.5`)
	c.ExpectMobSpawn(protocol.EntityTypePig)

	c.Disconnect()
	servertest.PollUntil(t, 10*time.Second, func() bool { return srv.MobCount() == 0 })
	srv.FlushMobs()

	if saved := servertest.SavedMobsByUUID(t, cfg.LevelName); len(saved) != 2 {
		t.Fatalf("the world saved %d mobs, want both pigs", len(saved))
	}
}

// TestASummonRefusesADuplicateUUID summons a pig with a UUID twice: the
// second is refused with the duplicate-UUID message and no second mob is
// left behind, the way tryAddFreshEntityWithPassengers refuses.
func TestASummonRefusesADuplicateUUID(t *testing.T) {
	t.Parallel()
	cfg := servertest.MobTestConfig()
	srv, c := servertest.Join(t, cfg, "Summoner", servertest.OpsListing(t, "Summoner"))

	// The name makes the first success line one the test can read past the
	// spawn it announces, leaving nothing for the refusal to be confused with.
	c.ChatCommand(`summon minecraft:pig 1.5 64 0.5 {UUID:[I;1,2,3,4],CustomName:"Bob"}`)
	c.ExpectCommandFeedbackText("commands.summon.success", namedDisplay("minecraft:pig", "Bob", 4))
	c.ChatCommand(`summon minecraft:pig 3.5 64 0.5 {UUID:[I;1,2,3,4]}`)
	var want protocol.Buffer
	want.PutText(protocol.Text{Color: protocol.ColorRed, Extra: []protocol.Text{
		{Translate: "commands.summon.failed.uuid"},
	}})
	want.PutBool(false)
	if got := c.ExpectPlay(protocol.IDPlaySystemChat); !bytes.Equal(got, want.Bytes()) {
		t.Errorf("duplicate-UUID refusal = % X, want % X", got, want.Bytes())
	}

	if n := srv.MobCount(); n != 1 {
		t.Errorf("the world holds %d mobs, want the one the first summon made", n)
	}
}

// TestASummonNamesANamedEntityInTheSuccessLine summons a mob and an armor
// stand with a custom name: each success line names it by that name, with the
// UUID insertion and a show-entity hover, not by its kind.
// vanilla: SummonCommand.spawnEntity, Entity.getDisplayName
func TestASummonNamesANamedEntityInTheSuccessLine(t *testing.T) {
	t.Parallel()
	cfg := servertest.MobTestConfig()
	_, c := servertest.Join(t, cfg, "Summoner", servertest.OpsListing(t, "Summoner"))

	c.ChatCommand(`summon minecraft:zombie 1.5 64 0.5 {UUID:[I;1,2,3,4],NoAI:1b,CustomName:"Bob"}`)
	servertest.AwaitCommandFeedbackText(t, c, "commands.summon.success", namedDisplay("minecraft:zombie", "Bob", 4))
	c.ChatCommand(`summon minecraft:armor_stand 3.5 64 0.5 {UUID:[I;1,2,3,5],CustomName:"Bob"}`)
	servertest.AwaitCommandFeedbackText(t, c, "commands.summon.success", namedDisplay("minecraft:armor_stand", "Bob", 5))
}

// TestANamedEntitysClickEventIsDroppedFromItsName summons a mob whose
// custom name carries a click event: both the summon line and a later /tag
// line name it without the click.
// vanilla: Entity.getName, Entity.removeAction
func TestANamedEntitysClickEventIsDroppedFromItsName(t *testing.T) {
	t.Parallel()
	cfg := servertest.MobTestConfig()
	_, c := servertest.Join(t, cfg, "Summoner", servertest.OpsListing(t, "Summoner"))

	c.ChatCommand(`summon minecraft:zombie 1.5 64 0.5 {UUID:[I;1,2,3,6],NoAI:1b,CustomName:{text:"Bob",click_event:{action:"suggest_command",command:"/x"}}}`)
	servertest.AwaitCommandFeedbackText(t, c, "commands.summon.success", namedDisplay("minecraft:zombie", "Bob", 6))
	c.ChatCommand(`tag @e[name=Bob,limit=1] add t`)
	servertest.AwaitCommandFeedbackText(t, c, "commands.tag.add.success.single",
		protocol.Text{Text: "t"}, namedDisplay("minecraft:zombie", "Bob", 6))
}

// namedDisplay is Entity.getDisplayName for an entity of the kind with the
// UUID [I;1,2,3,last] and a plain custom name.
func namedDisplay(kind, name string, last uint32) protocol.Text {
	return protocol.Text{
		Text:      name,
		Insertion: fmt.Sprintf("00000001-0000-0002-0000-0003%08x", last),
		HoverEntity: &protocol.HoverEntity{
			Name: protocol.Text{Text: name},
			ID:   kind,
			UUID: [4]uint32{1, 2, 3, last},
		},
	}
}

// TestASummonDropsANameThatIsNotAComponent summons a pig whose CustomName
// is a compound that is no text component: the mob is left unnamed, as a
// vanilla load would, rather than sent to clients as that name.
func TestASummonDropsANameThatIsNotAComponent(t *testing.T) {
	t.Parallel()
	cfg := servertest.MobTestConfig()
	_, c := servertest.Join(t, cfg, "Summoner", servertest.OpsListing(t, "Summoner"))

	c.ChatCommand(`summon minecraft:pig 1.5 64 0.5 {CustomName:{foo:1}}`)
	pig := c.ExpectMobSpawn(protocol.EntityTypePig)
	if pig.Name != nil {
		t.Errorf("the summoned pig's CustomName = %v, want none", pig.Name)
	}
}

// TestASummonLooksUpTheBiomeAtTheFlooredBlock summons a bare cow half a block
// west of the chunk border at x=0, where the block it stands beside is chunk
// -1's and the block its coordinate truncates to is chunk 0's: the finalize
// reads the biome of the block the position floors to, not the one it
// truncates toward. vanilla: Mob.finalizeSpawn asks BlockPos.containing.
func TestASummonLooksUpTheBiomeAtTheFlooredBlock(t *testing.T) {
	t.Parallel()
	cfg := config.Default()
	cfg.OnlineMode = false
	cfg.LevelName = t.TempDir()
	cfg.LevelSeed = "1"
	// The stone flat world, whose ground is at y=63, so a cow summoned at
	// y=64 stands rather than falling through real terrain.
	cfg.LevelType = server.TestFlatLevelType

	// Generate and save the two chunks either side of x=0.
	srv, err := server.New(cfg)
	if err != nil {
		t.Fatalf("create server: %v", err)
	}
	// A join writes usercache.json, so the cache goes to a directory of the
	// test's own rather than the package's shared working directory.
	srv.SetUsercacheFile(filepath.Join(t.TempDir(), "usercache.json"))
	srv.SetSpawning(false)
	srv.StartTick()
	west := srv.HoldChunk(-8, 8)
	east := srv.HoldChunk(8, 8)
	srv.TouchChunk(-8, 8)
	srv.TouchChunk(8, 8)
	srv.Shutdown()
	west()
	east()

	// Chunk -1 cold, chunk 0 warm: the block at -1 is snowy, the one at 0
	// desert, so the two reads the bug picks between differ.
	servertest.RelabelChunkGround(t, cfg.LevelName, world.ChunkPos{X: -1, Z: 0}, 4, math.MaxInt8, "minecraft:snowy_plains")
	servertest.RelabelChunkGround(t, cfg.LevelName, world.ChunkPos{X: 0, Z: 0}, 4, math.MaxInt8, "minecraft:desert")

	restarted := *cfg
	srv2, err := server.New(&restarted)
	if err != nil {
		t.Fatalf("restart server: %v", err)
	}
	srv2.SetUsercacheFile(filepath.Join(t.TempDir(), "usercache.json"))
	srv2.SetSpawning(false)
	srv2.StartTick()
	t.Cleanup(srv2.StopTick)
	releaseWest := srv2.HoldChunk(-8, 8)
	releaseEast := srv2.HoldChunk(8, 8)
	t.Cleanup(releaseWest)
	t.Cleanup(releaseEast)
	servertest.OpsListing(t, "Mapper")(srv2)

	c := servertest.DialServer(t, srv2).Join("Mapper")

	// The biome read is zoomed, so a row is picked where block -1 reads cold
	// and block 0 warm (seed 1 has one).
	row := int32(-1)
	for z := range int32(16) {
		if srv2.OverworldBiomeAt(-1, 64, z) == "minecraft:snowy_plains" && srv2.OverworldBiomeAt(0, 64, z) == "minecraft:desert" {
			row = z
			break
		}
	}
	if row < 0 {
		t.Fatal("no row where block -1 reads snowy and block 0 desert")
	}
	// At x=-0.5 the floored block is -1, cold; the truncated 0 is warm.
	c.ChatCommand(fmt.Sprintf(`summon minecraft:cow -0.5 64 %d.5`, row))
	atFloored := c.ExpectMobSpawn(protocol.EntityTypeCow)
	if want := registry.MustID("minecraft:cow_variant", "minecraft:cold"); atFloored.Variant != want {
		t.Errorf("the cow at x=-0.5 wears variant %d, want the snowy block's cold %d", atFloored.Variant, want)
	}
	// At x=0.5 both the floor and the truncation name block 0, warm.
	c.ChatCommand(fmt.Sprintf(`summon minecraft:cow 0.5 64 %d.5`, row))
	atWhole := c.ExpectMobSpawn(protocol.EntityTypeCow)
	if want := registry.MustID("minecraft:cow_variant", "minecraft:warm"); atWhole.Variant != want {
		t.Errorf("the cow at x=0.5 wears variant %d, want the desert block's warm %d", atWhole.Variant, want)
	}
}

// TestASummonOfAnOrbTNTOrItemWearsTheTagsUUID summons an experience orb,
// primed TNT and an item, one at a time, with a UUID in the tag: /tag names
// each by that UUID, a second summon of it is refused as a duplicate, and
// /kill clears it for the next kind.
// vanilla: SummonCommand.createEntity, Entity.load
func TestASummonOfAnOrbTNTOrItemWearsTheTagsUUID(t *testing.T) {
	t.Parallel()
	_, c := servertest.Join(t, servertest.BlockTestConfig(), "Summoner", servertest.OpsListing(t, "Summoner"))
	identity, _ := nbt.UUIDFromIntArray(nbt.IntArray{1, 2, 3, 4})

	// Far enough from the player that the orb is not drawn to it and the
	// item is not picked up; NoGravity holds both where they stand.
	for _, kind := range []struct{ name, tag, display string }{
		{"minecraft:experience_orb", `Value:3,NoGravity:1b`, "entity.minecraft.experience_orb"},
		{"minecraft:tnt", `fuse:1000,NoGravity:1b`, "entity.minecraft.tnt"},
		{"minecraft:item", `Item:{id:"minecraft:diamond",count:1},NoGravity:1b`, "item.minecraft.diamond"},
	} {
		summon := fmt.Sprintf("summon %s 20.5 70 20.5 {UUID:[I;1,2,3,4],%s}", kind.name, kind.tag)
		display := servertest.EntityDisplayComponent(kind.name, kind.display, identity)

		c.ChatCommand(summon)
		c.ChatCommand(fmt.Sprintf("tag @e[type=%s,limit=1] add t", kind.name))
		servertest.AwaitCommandFeedbackText(t, c, "commands.tag.add.success.single",
			protocol.Text{Text: "t"}, display)

		c.ChatCommand(summon)
		servertest.AwaitCommandFailure(t, c, "commands.summon.failed.uuid")

		c.ChatCommand(fmt.Sprintf("kill @e[type=%s]", kind.name))
		servertest.AwaitCommandFeedbackText(t, c, "commands.kill.success.single", display)
	}
}

// TestASummonNamesAMobByItsNameWithoutItsClick summons mobs whose CustomName
// carries a run_command and an open_url click: the name is read, and the
// success line names the mob by it with the click dropped.
// vanilla: Entity.getName, Entity.removeAction
func TestASummonNamesAMobByItsNameWithoutItsClick(t *testing.T) {
	t.Parallel()
	cfg := servertest.MobTestConfig()
	_, c := servertest.Join(t, cfg, "Summoner", servertest.OpsListing(t, "Summoner"))

	c.ChatCommand(`summon minecraft:zombie 1.5 64 0.5 {UUID:[I;1,2,3,7],NoAI:1b,CustomName:{text:"Bob",click_event:{action:"run_command",command:"/x"}}}`)
	servertest.AwaitCommandFeedbackText(t, c, "commands.summon.success", namedDisplay("minecraft:zombie", "Bob", 7))
	c.ChatCommand(`summon minecraft:zombie 3.5 64 0.5 {UUID:[I;1,2,3,8],NoAI:1b,CustomName:{text:"Bob",click_event:{action:"open_url",url:"https://example.com"}}}`)
	servertest.AwaitCommandFeedbackText(t, c, "commands.summon.success", namedDisplay("minecraft:zombie", "Bob", 8))
}

// TestASummonNamesAMobByItsNameWithoutPageOrSiblingClicks summons a mob whose
// CustomName has a change_page click and a sibling with a run_command click:
// neither the success line nor the kill line carries either.
// vanilla: Entity.getName, Entity.removeAction
func TestASummonNamesAMobByItsNameWithoutPageOrSiblingClicks(t *testing.T) {
	t.Parallel()
	cfg := servertest.MobTestConfig()
	_, c := servertest.Join(t, cfg, "Summoner", servertest.OpsListing(t, "Summoner"))

	c.ChatCommand(`summon minecraft:zombie 1.5 64 0.5 {UUID:[I;1,2,3,9],NoAI:1b,CustomName:{text:"Bob",click_event:{action:"change_page",page:2},extra:[{text:"!",click_event:{action:"run_command",command:"/x"}}]}}`)
	want := namedDisplay("minecraft:zombie", "Bob", 9)
	want.Extra = []protocol.Text{{Text: "!"}}
	want.HoverEntity.Name.Extra = []protocol.Text{{Text: "!"}}
	servertest.AwaitCommandFeedbackText(t, c, "commands.summon.success", want)

	// The kill line names the mob through the entity snapshot.
	c.ChatCommand("kill @e[type=minecraft:zombie]")
	servertest.AwaitCommandFeedbackText(t, c, "commands.kill.success.single", want)
}
