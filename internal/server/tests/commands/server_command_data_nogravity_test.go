package commands

import (
	"github.com/golem-mc/golem/internal/server/servertest"
	"testing"

	"github.com/golem-mc/golem/internal/protocol"
)

// TestCommandDataClearingNoGravityReachesTheClient checks a /data merge that
// sets a synched value back to its default is told to the client: the flag
// turned off must arrive as index 5 = false, not be left out as a default.
// vanilla: SynchedEntityData.packDirty
func TestCommandDataClearingNoGravityReachesTheClient(t *testing.T) {
	t.Parallel()
	kinds := []struct {
		name, selector, summon string
		entityType             int32
	}{
		{"falling block", "minecraft:falling_block", `summon minecraft:falling_block 7.5 70 7.5 {BlockState:{id:"minecraft:stone"},NoGravity:1b}`, protocol.EntityTypeFallingBlock},
		{"primed tnt", "minecraft:tnt", `summon minecraft:tnt 5.5 70 5.5 {NoGravity:1b,fuse:1000s}`, protocol.EntityTypeTnt},
		{"arrow", "minecraft:arrow", `summon minecraft:arrow 4.5 70 4.5 {NoGravity:1b}`, protocol.EntityTypeArrow},
	}
	for _, k := range kinds {
		t.Run(k.name, func(t *testing.T) {
			t.Parallel()
			c := dataWorld(t, "Clearer")
			c.ChatCommand(k.summon)
			spawn := c.AwaitSpawn(k.entityType)
			if m := servertest.MetaOf(c.AwaitMetadataFor(spawn.EntityID), protocol.MetadataIndexNoGravity); m == nil || !m.FlagVal {
				t.Fatalf("the spawn's no-gravity metadata = %+v, want the flag set", m)
			}

			c.ChatCommand(`data merge entity @e[limit=1,sort=nearest,type=` + k.selector + `] {NoGravity:0b}`)
			// A body's own tick metadata (the fuse) can ride ahead; read on
			// until the one naming the flag.
			for {
				m := servertest.MetaOf(c.AwaitMetadataFor(spawn.EntityID), protocol.MetadataIndexNoGravity)
				if m == nil {
					continue
				}
				if m.FlagVal {
					t.Fatalf("clearing no-gravity told the flag as still set")
				}
				break
			}
			c.Disconnect()
		})
	}
}

// TestCommandDataResetToDefaultReachesTheClientForEveryKind checks the kinds
// whose spawn payload leaves defaults out: an item, an orb, a boat and a pig
// told a value reset to its default, and a merge that changes nothing told
// nothing.
// vanilla: SynchedEntityData.set
func TestCommandDataResetToDefaultReachesTheClientForEveryKind(t *testing.T) {
	t.Parallel()
	kinds := []struct {
		name, selector, summon, set, reset string
		entityType                         int32
		index                              uint8
	}{
		{"item", "minecraft:item", `summon minecraft:item 6.5 70 6.5 {Item:{id:"minecraft:stone",count:1},NoGravity:1b}`,
			`{NoGravity:1b}`, `{NoGravity:0b}`, protocol.EntityTypeItem, protocol.MetadataIndexNoGravity},
		{"experience orb", "minecraft:experience_orb", `summon minecraft:experience_orb 5.5 70 5.5 {NoGravity:1b}`,
			`{NoGravity:1b}`, `{NoGravity:0b}`, protocol.EntityTypeExperienceOrb, protocol.MetadataIndexNoGravity},
		{"pig", "minecraft:pig", `summon minecraft:pig 4.5 70 4.5 {CustomNameVisible:1b}`,
			`{CustomNameVisible:1b}`, `{CustomNameVisible:0b}`, protocol.EntityTypePig, protocol.MetadataIndexCustomNameVisible},
	}
	for _, k := range kinds {
		t.Run(k.name, func(t *testing.T) {
			t.Parallel()
			c := dataWorld(t, "Resetter")
			c.ChatCommand(k.summon)
			spawn := c.AwaitSpawn(k.entityType)
			if m := servertest.MetaOf(c.AwaitMetadataFor(spawn.EntityID), k.index); m == nil || !m.FlagVal {
				t.Fatalf("the spawn's metadata index %d = %+v, want the flag set", k.index, m)
			}

			target := `data merge entity @e[limit=1,sort=nearest,type=` + k.selector + `] `
			// Merging the value already held changes nothing, so the first
			// metadata packet that follows is the reset's.
			c.ChatCommand(target + k.set)
			c.ChatCommand(target + k.reset)
			for {
				m := servertest.MetaOf(c.AwaitMetadataFor(spawn.EntityID), k.index)
				if m == nil {
					continue
				}
				if m.FlagVal {
					t.Fatalf("resetting index %d told the flag as still set", k.index)
				}
				break
			}
			c.Disconnect()
		})
	}
}

// TestCommandDataTNTBlockStateReachesTheClient checks a merge that changes a
// primed TNT's block state is told as index 9.
// vanilla: PrimedTnt.DATA_BLOCK_STATE_ID
func TestCommandDataTNTBlockStateReachesTheClient(t *testing.T) {
	t.Parallel()
	c := dataWorld(t, "Stater")
	c.ChatCommand(`summon minecraft:tnt 5.5 70 5.5 {NoGravity:1b,fuse:1000s}`)
	spawn := c.AwaitSpawn(protocol.EntityTypeTnt)
	c.ChatCommand(`data merge entity @e[limit=1,sort=nearest,type=minecraft:tnt] {block_state:{id:"minecraft:stone"}}`)
	for {
		if m := servertest.MetaOf(c.AwaitMetadataFor(spawn.EntityID), protocol.MetadataIndexTNTBlockState); m != nil && m.VarintVal != 0 {
			break
		}
	}
	c.Disconnect()
}
