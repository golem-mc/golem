package entities

import (
	"testing"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/servertest"
	"github.com/golem-mc/golem/nbt"
)

// TestEntitySummonedWithUnreadableHoverDropsHoverAndStaysJoinable summons a
// pig with an unreadable hover_event in its CustomName, then joins a second
// client: both receive the pig's metadata with plain name "Bob" and no hover,
// and /data merge with another unreadable hover keeps only the plain name.
// vanilla: Entity.load, TagValueInput.read, ComponentSerialization.CODEC
func TestEntitySummonedWithUnreadableHoverDropsHoverAndStaysJoinable(t *testing.T) {
	t.Parallel()
	srv, c := servertest.Join(t, servertest.MobTestConfig(), "Summoner", servertest.OpsListing(t, "Summoner"))

	c.ChatCommand(`summon pig ~3 ~ ~ {NoAI:1b,CustomName:{text:"Bob",hover_event:{action:"show_text"}},CustomNameVisible:1b}`)
	spawn := c.AwaitSpawn(protocol.EntityTypePig)
	meta := c.AwaitMetadataFor(spawn.EntityID)
	nameEntry := servertest.MetaOf(meta, protocol.MetadataIndexCustomName)
	if nameEntry == nil || nameEntry.TextVal != nbt.String("Bob") {
		t.Fatalf("summoner metadata CustomName = %+v, want plain string Bob", nameEntry)
	}
	if vis := servertest.MetaOf(meta, protocol.MetadataIndexCustomNameVisible); vis == nil || !vis.FlagVal {
		t.Fatalf("summoner metadata CustomNameVisible = %+v, want true", vis)
	}

	if got := awaitEntityDataCompound(t, srv, c, "@e[type=minecraft:pig,limit=1]", "CustomName"); got != `"Bob"` {
		t.Errorf("/data get entity CustomName = %s, want %q", got, `"Bob"`)
	}

	// A second player joining receives the same cleaned metadata without
	// failing to decode set_entity_data.
	second := servertest.DialServer(t, srv).JoinKeeping("Viewer")
	secondMeta := second.AwaitMetadataFor(spawn.EntityID)
	secondName := servertest.MetaOf(secondMeta, protocol.MetadataIndexCustomName)
	if secondName == nil || secondName.TextVal != nbt.String("Bob") {
		t.Fatalf("joining viewer metadata CustomName = %+v, want plain string Bob", secondName)
	}

	// /data merge with an unreadable hover also keeps only the plain text.
	c.ChatCommand(`data merge entity @e[type=minecraft:pig,limit=1] {CustomName:{text:"x",hover_event:{action:"show_text"}}}`)
	mergedMeta := c.AwaitMetadataFor(spawn.EntityID)
	mergedName := servertest.MetaOf(mergedMeta, protocol.MetadataIndexCustomName)
	if mergedName == nil || mergedName.TextVal != nbt.String("x") {
		t.Fatalf("merged metadata CustomName = %+v, want plain string x", mergedName)
	}
	if got := awaitEntityDataCompound(t, srv, c, "@e[type=minecraft:pig,limit=1]", "CustomName"); got != `"x"` {
		t.Errorf("/data get entity CustomName after merge = %s, want %q", got, `"x"`)
	}
}

// TestSummonedItemsDisplaysAndMannequinDropUnreadableHover verifies that
// unreadable style fields are dropped leniently on item frame items, item
// entities, text displays, and mannequin descriptions.
// vanilla: TagValueInput.read, ComponentSerialization.CODEC
func TestSummonedItemsDisplaysAndMannequinDropUnreadableHover(t *testing.T) {
	t.Parallel()
	srv, c := servertest.Join(t, servertest.MobTestConfig(), "Summoner", servertest.OpsListing(t, "Summoner"))

	// Item frame with unreadable hover in custom_name keeps the stone named "S".
	c.ChatCommand(`summon item_frame 2.5 64 2.5 {Facing:1b,Item:{id:"minecraft:stone",count:1,components:{"minecraft:custom_name":{text:"S",hover_event:{action:"show_text"}}}}}`)
	c.AwaitSpawn(protocol.EntityTypeItemFrame)
	if got := awaitEntityDataCompound(t, srv, c, "@e[type=minecraft:item_frame,limit=1]", `Item.components."minecraft:custom_name"`); got != `"S"` {
		t.Errorf("item_frame custom_name = %s, want %q", got, `"S"`)
	}

	// Item entity with unreadable hover in custom_name, item_name, and lore keeps the plain text.
	c.ChatCommand(`summon item 4.5 64 2.5 {Item:{id:"minecraft:stone",count:1,components:{"minecraft:custom_name":{text:"S",hover_event:{action:"show_text"}},"minecraft:item_name":{text:"I",color:"bogus"},"minecraft:lore":[{text:"L",hover_event:{action:"show_text"}}]}}}`)
	c.AwaitSpawn(protocol.EntityTypeItem)
	if got := awaitEntityDataCompound(t, srv, c, "@e[type=minecraft:item,limit=1]", `Item.components."minecraft:custom_name"`); got != `"S"` {
		t.Errorf("item entity custom_name = %s, want %q", got, `"S"`)
	}
	if got := awaitEntityDataCompound(t, srv, c, "@e[type=minecraft:item,limit=1]", `Item.components."minecraft:item_name"`); got != `"I"` {
		t.Errorf("item entity item_name = %s, want %q", got, `"I"`)
	}
	if got := awaitEntityDataCompound(t, srv, c, "@e[type=minecraft:item,limit=1]", `Item.components."minecraft:lore"[0]`); got != `"L"` {
		t.Errorf("item entity lore[0] = %s, want %q", got, `"L"`)
	}

	// Text display with unreadable hover stores and sends "T".
	c.ChatCommand(`summon text_display 6.5 64 2.5 {text:{text:"T",hover_event:{action:"show_text"}}}`)
	c.AwaitSpawn(protocol.EntityTypeTextDisplay)
	if got := awaitEntityDataCompound(t, srv, c, "@e[type=minecraft:text_display,limit=1]", "text"); got != `"T"` {
		t.Errorf("text_display text = %s, want %q", got, `"T"`)
	}

	// Mannequin with unreadable hover in description keeps "D".
	c.ChatCommand(`summon mannequin 8.5 64 2.5 {description:{text:"D",hover_event:{action:"show_text"}}}`)
	c.AwaitSpawn(protocol.EntityTypeMannequin)
	if got := awaitEntityDataCompound(t, srv, c, "@e[type=minecraft:mannequin,limit=1]", "description"); got != `"D"` {
		t.Errorf("mannequin description = %s, want %q", got, `"D"`)
	}
}
