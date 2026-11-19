package commands

import (
	"bytes"
	"fmt"
	"github.com/golem-mc/golem/internal/server/servertest"
	"reflect"
	"testing"
	"time"

	"github.com/golem-mc/golem/internal/protocol"
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
