package commands

import (
	"github.com/golem-mc/golem/internal/server/servertest"
	"slices"
	"testing"
	"time"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/nbt"
)

// The entity scoreboard tags: the /tag command, the tag= selector option
// against a summon's Tags compound, and their survival of the save files.
// vanilla: TagCommand, Entity.addTag, EntitySelectorOptions' tag option,
// Entity's Tags save fields

// TestTagAddsListsAndRemovesAPlayersTag runs the /tag command over the
// runner themself: add, list, the add and remove that change nothing, and
// the names a remove suggests, the target's tags.
func TestTagAddsListsAndRemovesAPlayersTag(t *testing.T) {
	t.Parallel()
	_, c := servertest.Join(t, servertest.MobTestConfig(), "Keeper", servertest.OpsListing(t, "Keeper"))

	c.ChatCommand("tag @s add hello")
	c.ExpectCommandFeedbackText("commands.tag.add.success.single",
		protocol.Text{Text: "hello"}, servertest.PlayerDisplayComponent("Keeper"))
	c.ChatCommand("tag @s add world")
	c.ExpectCommandFeedbackText("commands.tag.add.success.single",
		protocol.Text{Text: "world"}, servertest.PlayerDisplayComponent("Keeper"))

	// The list carries the tags sorted, in green.
	c.ChatCommand("tag @s list")
	c.ExpectCommandFeedbackText("commands.tag.list.single.success",
		servertest.PlayerDisplayComponent("Keeper"), servertest.TagCountText(2), servertest.GreenTagText("hello", "world"))

	// An add that changes nothing and a remove of what is not held.
	c.ChatCommand("tag @s add hello")
	c.ExpectTagRefusal("commands.tag.add.failed")
	c.ChatCommand("tag @s remove what")
	c.ExpectTagRefusal("commands.tag.remove.failed")

	c.ChatCommand("tag @s remove hello")
	c.ExpectCommandFeedbackText("commands.tag.remove.success.single",
		protocol.Text{Text: "hello"}, servertest.PlayerDisplayComponent("Keeper"))
	c.ChatCommand("tag @s remove world")
	c.ExpectCommandFeedbackText("commands.tag.remove.success.single",
		protocol.Text{Text: "world"}, servertest.PlayerDisplayComponent("Keeper"))

	// Every tag gone: the empty-list line, which counts the targets holding
	// a tag, so names none. After adding one back, the remove's name
	// suggestions are the tags the target holds.
	// vanilla: TagCommand.listTags, CommandResponseTracker (NON_ZERO)
	c.ChatCommand("tag @s list")
	c.ExpectCommandFeedbackText("commands.tag.list.multiple.empty", servertest.TagCountText(0))
	c.ChatCommand("tag @s add alpha")
	c.ExpectCommandFeedbackText("commands.tag.add.success.single",
		protocol.Text{Text: "alpha"}, servertest.PlayerDisplayComponent("Keeper"))
	c.CommandSuggestion(1, "/tag @s remove al")
	if _, start, _, matches := c.ExpectCommandSuggestions(); start != int32(len("/tag @s remove ")) || !slices.Equal(matches, []string{"alpha"}) {
		t.Errorf("the remove suggestions = start %d %v, want the held alpha from %d", start, matches, len("/tag @s remove "))
	}
	c.CommandSuggestion(1, "/tag @s remove x")
	if _, start, length, matches := c.ExpectCommandSuggestions(); start != 0 || length != 0 || matches != nil {
		t.Errorf("an unheld tag's remove suggestions = start %d length %d %v, want none", start, length, matches)
	}

	c.Disconnect()
}

// TestTheTagSelectorMatchesASummonsTags summons a pig whose compound carried
// Tags:["b"] beside a plain one, and picks each out by its tags: the named
// one by tag=b, the plain one by tag=, and nobody by an inverted tag every
// pig matches.
func TestTheTagSelectorMatchesASummonsTags(t *testing.T) {
	t.Parallel()
	srv, c := servertest.Join(t, servertest.MobTestConfig(), "Keeper", servertest.OpsListing(t, "Keeper"))

	c.ChatCommand(`summon minecraft:pig 1.5 64 0.5 {Tags:["b"]}`)
	tagged := c.ExpectMobSpawn(protocol.EntityTypePig)
	c.ChatCommand("summon minecraft:pig 3.5 64 0.5")
	plain := c.ExpectMobSpawn(protocol.EntityTypePig)

	// tag=b names the tagged pig alone, so the kill line names it.
	c.ChatCommand("kill @e[tag=b]")
	servertest.AwaitCommandFeedbackText(t, c, "commands.kill.success.single",
		servertest.EntityDisplayComponent("minecraft:pig", "entity.minecraft.pig", tagged.Uuid))
	// The tag the compound carried is one /tag reads: tag= finds the plain
	// pig, the only pig carrying none.
	c.ChatCommand("tag @e[type=minecraft:pig,tag=] add marked")
	servertest.AwaitCommandFeedbackText(t, c, "commands.tag.add.success.single",
		protocol.Text{Text: "marked"}, servertest.EntityDisplayComponent("minecraft:pig", "entity.minecraft.pig", plain.Uuid))
	// An inverted tag every living pig matches: nobody.
	c.ChatCommand("kill @e[type=minecraft:pig,tag=!marked]")
	c.ExpectTagRefusal("argument.entity.notfound.entity")

	// The remove's name suggestions read a bracketed selector the line
	// typed, and offer the tags the pigs it names hold.
	c.CommandSuggestion(1, "/tag @e[type=minecraft:pig] remove mar")
	var skipped []servertest.PushedPacket
	defer func() { c.Unread(skipped...) }()
	c.ReadTicks(srv, 200, func(packetID int32, data []byte) bool {
		if packetID != protocol.IDPlayCommandSuggestions {
			skipped = append(skipped, servertest.PushedPacket{Id: packetID, Data: data})
			return false
		}
		r := protocol.NewReader(data)
		if id := r.ReadVarInt(); id != 1 {
			skipped = append(skipped, servertest.PushedPacket{Id: packetID, Data: data})
			return false
		}
		start := r.ReadVarInt()
		r.ReadVarInt() // length, which the one match spells by itself
		var matches []string
		for range r.ReadVarInt() {
			matches = append(matches, r.ReadString(32767))
			r.ReadBool() // tooltip, which the server does not send
		}
		if start != int32(len("/tag @e[type=minecraft:pig] remove ")) || !slices.Equal(matches, []string{"marked"}) {
			t.Errorf("the remove suggestions = start %d %v, want the held marked from %d", start, matches, len("/tag @e[type=minecraft:pig] remove "))
		}
		return true
	})

	// The kill's corpse counts until the dying ticks are over: the marked
	// plain pig is the mob the world is left with.
	servertest.PollUntil(t, 10*time.Second, func() bool { return srv.MobCount() == 1 })
	c.Disconnect()
}

// TestTagsSurviveASaveAndReload writes the tag a summon carried and the tag
// /tag added onto the mob's file, and /tag's own onto the player's, then
// reads them back through the file the chunk's save wrote and the one the
// player left in.
func TestTagsSurviveASaveAndReload(t *testing.T) {
	t.Parallel()
	cfg := servertest.MobTestConfig()
	srv, connect := servertest.Start(t, cfg, servertest.OpsListing(t, "Keeper"))
	c := connect().Join("Keeper")

	c.ChatCommand(`summon minecraft:pig 1.5 64 0.5 {Tags:["b"]}`)
	pig := c.ExpectMobSpawn(protocol.EntityTypePig)
	c.ChatCommand("tag @e[type=minecraft:pig] add a")
	servertest.AwaitCommandFeedbackText(t, c, "commands.tag.add.success.single",
		protocol.Text{Text: "a"}, servertest.EntityDisplayComponent("minecraft:pig", "entity.minecraft.pig", pig.Uuid))
	c.ChatCommand("tag @s add mine")
	servertest.AwaitCommandFeedbackText(t, c, "commands.tag.add.success.single",
		protocol.Text{Text: "mine"}, servertest.PlayerDisplayComponent("Keeper"))

	c.Disconnect()
	servertest.PollUntil(t, 10*time.Second, func() bool { return srv.MobCount() == 0 })
	srv.FlushMobs()

	// The entity compound the save wrote the pig under: the summon's tag
	// and the added one, a pair whatever order the list comes out in.
	saved := servertest.SavedMobsByUUID(t, cfg.LevelName)
	compound, ok := saved[pig.Uuid]
	if !ok {
		t.Fatalf("the pig's save was not written at all")
	}
	list, ok := nbt.Get[nbt.List](compound, "Tags")
	if !ok {
		t.Fatalf("the saved pig carries no Tags, want a and b")
	}
	var tags []string
	for _, item := range list.Items {
		tags = append(tags, string(item.(nbt.String)))
	}
	slices.Sort(tags)
	if !slices.Equal(tags, []string{"a", "b"}) {
		t.Errorf("the saved pig's Tags = %v, want a and b", tags)
	}

	// Back through the files: the pig's chunk reloads its tags into the
	// selector, the player's file replays theirs into /tag.
	second := connect().Join("Keeper")
	second.ChatCommand("tag @e[type=minecraft:pig,tag=b] list")
	servertest.AwaitCommandFeedbackText(t, second, "commands.tag.list.single.success",
		servertest.EntityDisplayComponent("minecraft:pig", "entity.minecraft.pig", pig.Uuid),
		servertest.TagCountText(2), servertest.GreenTagText("a", "b"))
	second.ChatCommand("tag @s list")
	servertest.AwaitCommandFeedbackText(t, second, "commands.tag.list.single.success",
		servertest.PlayerDisplayComponent("Keeper"), servertest.TagCountText(1), servertest.GreenTagText("mine"))
	second.Disconnect()
}

// TestADuplicateSummonTagReadsAsOne runs a summon whose Tags carries the same
// tag twice: vanilla keeps tags in a set, so the pig holds the one tag and a
// single remove leaves it with none.
// vanilla: Entity's read of Tags
func TestADuplicateSummonTagReadsAsOne(t *testing.T) {
	t.Parallel()
	_, c := servertest.Join(t, servertest.MobTestConfig(), "Keeper", servertest.OpsListing(t, "Keeper"))

	c.ChatCommand(`summon minecraft:pig 1.5 64 0.5 {Tags:["a","a"]}`)
	pig := c.ExpectMobSpawn(protocol.EntityTypePig)

	// One tag, not two: the single remove takes it and the list empties.
	c.ChatCommand("tag @e[type=minecraft:pig] remove a")
	servertest.AwaitCommandFeedbackText(t, c, "commands.tag.remove.success.single",
		protocol.Text{Text: "a"}, servertest.EntityDisplayComponent("minecraft:pig", "entity.minecraft.pig", pig.Uuid))
	c.ChatCommand("tag @e[type=minecraft:pig] list")
	servertest.AwaitCommandFeedbackText(t, c, "commands.tag.list.multiple.empty", servertest.TagCountText(0))
	// The tag is gone from the selector too.
	c.ChatCommand("kill @e[type=minecraft:pig,tag=a]")
	c.ExpectTagRefusal("argument.entity.notfound.entity")

	c.Disconnect()
}

// TestTagChangesSeveralTargetsAtOnce runs /tag over two pigs: the add, the
// list and the remove all answer in the multiple form, counting the bodies
// each changed or listed.
func TestTagChangesSeveralTargetsAtOnce(t *testing.T) {
	t.Parallel()
	_, c := servertest.Join(t, servertest.MobTestConfig(), "Keeper", servertest.OpsListing(t, "Keeper"))

	c.ChatCommand("summon minecraft:pig 1.5 64 0.5")
	c.ExpectMobSpawn(protocol.EntityTypePig)
	c.ChatCommand("summon minecraft:pig 3.5 64 0.5")
	c.ExpectMobSpawn(protocol.EntityTypePig)

	c.ChatCommand("tag @e[type=minecraft:pig] add herd")
	servertest.AwaitCommandFeedbackText(t, c, "commands.tag.add.success.multiple",
		protocol.Text{Text: "herd"}, servertest.TagCountText(2))
	c.ChatCommand("tag @e[type=minecraft:pig] list")
	servertest.AwaitCommandFeedbackText(t, c, "commands.tag.list.multiple.success",
		servertest.TagCountText(2), servertest.TagCountText(1), servertest.GreenTagText("herd"))
	// An add that changes no pig answers the refusal, not the line.
	c.ChatCommand("tag @e[type=minecraft:pig] add herd")
	c.ExpectTagRefusal("commands.tag.add.failed")
	c.ChatCommand("tag @e[type=minecraft:pig] remove herd")
	servertest.AwaitCommandFeedbackText(t, c, "commands.tag.remove.success.multiple",
		protocol.Text{Text: "herd"}, servertest.TagCountText(2))
	c.ChatCommand("tag @e[type=minecraft:pig] list")
	// No pig holds a tag, so the empty line counts none of them.
	servertest.AwaitCommandFeedbackText(t, c, "commands.tag.list.multiple.empty", servertest.TagCountText(0))

	c.Disconnect()
}

// TestSummonTagsOnNonMobEntities summons an item, a primed TNT and an
// experience orb, each carrying a tag, and picks each out by it: Tags is read
// for every entity kind, not only the mobs.
// vanilla: Entity.load
func TestSummonTagsOnNonMobEntities(t *testing.T) {
	t.Parallel()
	_, c := servertest.Join(t, servertest.MobTestConfig(), "Keeper", servertest.OpsListing(t, "Keeper"))

	c.ChatCommand(`summon minecraft:item 6.5 64 0.5 {Item:{id:"minecraft:stone",count:1},Tags:["gem"]}`)
	item := c.AwaitSpawn(protocol.EntityTypeItem)
	c.ChatCommand(`summon minecraft:tnt 8.5 64 0.5 {fuse:200,Tags:["boom"]}`)
	tnt := c.AwaitSpawn(protocol.EntityTypeTnt)
	c.ChatCommand(`summon minecraft:experience_orb 10.5 64 0.5 {Value:3,Tags:["sparkle"]}`)
	orb := c.AwaitSpawn(protocol.EntityTypeExperienceOrb)

	c.ChatCommand("tag @e[tag=gem] list")
	servertest.AwaitCommandFeedbackText(t, c, "commands.tag.list.single.success",
		servertest.EntityDisplayComponent("minecraft:item", "block.minecraft.stone", item.Uuid),
		servertest.TagCountText(1), servertest.GreenTagText("gem"))
	c.ChatCommand("tag @e[tag=boom] list")
	servertest.AwaitCommandFeedbackText(t, c, "commands.tag.list.single.success",
		servertest.EntityDisplayComponent("minecraft:tnt", "entity.minecraft.tnt", tnt.Uuid),
		servertest.TagCountText(1), servertest.GreenTagText("boom"))
	c.ChatCommand("tag @e[tag=sparkle] list")
	servertest.AwaitCommandFeedbackText(t, c, "commands.tag.list.single.success",
		servertest.EntityDisplayComponent("minecraft:experience_orb", "entity.minecraft.experience_orb", orb.Uuid),
		servertest.TagCountText(1), servertest.GreenTagText("sparkle"))

	c.Disconnect()
}

// TestAnArmorStandAnswersSelectorsAndKeepsItsTags summons a stand whose
// compound carried Tags, picks it out with the type and tag options and
// adds another tag through /tag, then writes it out with its chunk and
// reads both tags back off the entity file and the stand a rejoin wakes.
// vanilla: Entity.saveWithoutId, Entity.load
func TestAnArmorStandAnswersSelectorsAndKeepsItsTags(t *testing.T) {
	t.Parallel()
	cfg := servertest.MobTestConfig()
	srv, connect := servertest.Start(t, cfg, servertest.OpsListing(t, "Keeper"))
	c := connect().Join("Keeper")

	c.ChatCommand(`summon minecraft:armor_stand 1.5 64 0.5 {Tags:["dressed"]}`)
	stand := c.ExpectArmorStandSpawn()

	// The selector's type and tag options find the stand, and /tag adds to
	// it as it does to every loaded body.
	c.ChatCommand("tag @e[type=minecraft:armor_stand,tag=dressed] add marked")
	servertest.AwaitCommandFeedbackText(t, c, "commands.tag.add.success.single",
		protocol.Text{Text: "marked"},
		servertest.EntityDisplayComponent("minecraft:armor_stand", "entity.minecraft.armor_stand", stand.Identity))

	c.Disconnect()
	servertest.PollUntil(t, 10*time.Second, func() bool { return srv.ArmorStandCount() == 0 })
	srv.FlushMobs()

	// The entity file carries the summon's tag and the added one, a pair
	// whatever order the list comes out in.
	saved := servertest.SavedMobsByUUID(t, cfg.LevelName)
	compound, ok := saved[stand.Identity]
	if !ok {
		t.Fatalf("the stand's save was not written at all")
	}
	list, ok := nbt.Get[nbt.List](compound, "Tags")
	if !ok {
		t.Fatalf("the saved stand carries no Tags, want dressed and marked")
	}
	var tags []string
	for _, item := range list.Items {
		tags = append(tags, string(item.(nbt.String)))
	}
	slices.Sort(tags)
	if !slices.Equal(tags, []string{"dressed", "marked"}) {
		t.Errorf("the saved stand's Tags = %v, want dressed and marked", tags)
	}

	// The rejoin wakes the stand with both tags still in it.
	second := connect().Join("Keeper")
	second.ChatCommand("tag @e[type=minecraft:armor_stand,tag=dressed] list")
	servertest.AwaitCommandFeedbackText(t, second, "commands.tag.list.single.success",
		servertest.EntityDisplayComponent("minecraft:armor_stand", "entity.minecraft.armor_stand", stand.Identity),
		servertest.TagCountText(2), servertest.GreenTagText("dressed", "marked"))
	second.Disconnect()
}

// TestAThrownPearlAnswersSelectorsAndKeepsItsTags throws a pearl, picks it
// out of the air with the type option and tags it, then reads the tag back
// off the entity tag the leaving wrote into the player's file and off the
// pearl a rejoin raises.
// vanilla: Entity.saveWithoutId, Entity.load, ServerPlayer.saveEnderPearls
func TestAThrownPearlAnswersSelectorsAndKeepsItsTags(t *testing.T) {
	t.Parallel()
	cfg := servertest.MobTestConfig()
	srv, connect := servertest.Start(t, cfg, servertest.OpsListing(t, "Thrower"))
	c := connect().Join("Thrower")
	srv.GiveSlot("Thrower", 36, protocol.ItemStack{ItemID: servertest.MustItem("minecraft:ender_pearl"), Count: 16})

	// The loop is stopped before the throw, so the pearl does not move
	// however loaded the run is: a pearl in real-time flight can land before
	// the tag, the departure and the rejoin are through, which is the same
	// outcome under -race.
	srv.StopTick()

	c.MoveTo(0.5, 64, 0.5, -90, -60)
	servertest.UseHeld(c, -90, -60)
	pearl := c.ExpectThrownSpawn(srv, protocol.EntityTypeEnderPearl)

	c.ChatCommand("tag @e[type=minecraft:ender_pearl] add marked")
	servertest.AwaitCommandFeedbackText(t, c, "commands.tag.add.success.single",
		protocol.Text{Text: "marked"},
		servertest.EntityDisplayComponent("minecraft:ender_pearl", "entity.minecraft.ender_pearl", pearl.Uuid))

	c.Disconnect()

	// The leaving wrote the in-flight pearl into the player's file, the
	// tag in its entity tag.
	root := servertest.ReadPlayerFile(t, servertest.PlayerFile(cfg.LevelName, "Thrower"))
	list, ok := nbt.Get[nbt.List](root, "ender_pearls")
	if !ok || len(list.Items) != 1 {
		t.Fatalf("ender_pearls = %v, %v; want the one pearl in flight", list, ok)
	}
	saved, ok := list.Items[0].(nbt.Compound)
	if !ok {
		t.Fatalf("the saved pearl is a %s, want a compound", list.Items[0].Type())
	}
	tags, ok := nbt.Get[nbt.List](saved, "Tags")
	if !ok || len(tags.Items) != 1 || tags.Items[0] != nbt.Tag(nbt.String("marked")) {
		t.Errorf("the saved pearl's Tags = %v, %v; want the one marked", tags, ok)
	}

	// The rejoin raises the pearl with the tag still in it. The raise runs
	// on the thrown pass, so the stopped loop is stepped once for it, which
	// is also what sends the spawn.
	again := connect().Join("Thrower")
	srv.TickEntities(1)
	raised := again.ExpectThrownSpawn(srv, protocol.EntityTypeEnderPearl)
	if raised.Uuid != pearl.Uuid {
		t.Errorf("the raised pearl's identity = %v, want the thrown one's %v", raised.Uuid, pearl.Uuid)
	}
	again.ChatCommand("tag @e[type=minecraft:ender_pearl,tag=marked] list")
	servertest.AwaitCommandFeedbackText(t, again, "commands.tag.list.single.success",
		servertest.EntityDisplayComponent("minecraft:ender_pearl", "entity.minecraft.ender_pearl", pearl.Uuid),
		servertest.TagCountText(1), servertest.GreenTagText("marked"))
	again.Disconnect()
}
