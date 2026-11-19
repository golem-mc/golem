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
