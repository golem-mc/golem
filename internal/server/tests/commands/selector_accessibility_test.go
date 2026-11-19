package commands

import (
	"bytes"
	"testing"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/servertest"
	"github.com/golem-mc/golem/internal/server/world"
)

// vanilla: PersistentEntitySectionManager.startTracking, Visibility.isAccessible
func TestSelectorsHideInaccessibleEntities(t *testing.T) {
	t.Parallel()
	srv, c := servertest.Join(t, servertest.CrossingTestConfig(t), "Searcher", servertest.OpsListing(t, "Searcher"))
	servertest.HoldTicks(t, srv)
	servertest.FreezeUntilHeld(t, srv)
	if srv.ChunkLoadedInDimension(world.NetherName, -1, -1) {
		t.Fatal("destination was already loaded")
	}

	type body struct{ kind, uuid string }
	var bodies []body
	for _, tc := range []struct {
		kind, tag string
		transfer  bool
	}{
		{"cow", "{NoAI:1b}", true},
		{"armor_stand", "{NoGravity:1b}", true},
		{"item", `{Item:{id:"minecraft:stone",count:1},NoGravity:1b}`, true},
		{"evoker_fangs", "{Warmup:40}", false},
		{"eye_of_ender", "{}", false},
		{"lightning_bolt", "{}", false},
	} {
		prefix, at := "", "5.5 64 5.5"
		if !tc.transfer {
			prefix, at = "execute in minecraft:the_nether run ", "-0.5 70 -0.5"
		}
		c.ChatCommand(prefix + "summon " + tc.kind + " " + at + " " + tc.tag)
		got, ok := protocol.ReadText(protocol.NewReader(c.ExpectSystemChatPastMobs()))
		if !ok || got.Translate != "commands.summon.success" || len(got.With) != 1 || got.With[0].Insertion == "" {
			t.Fatalf("summon %s: %+v", tc.kind, got)
		}
		identity := got.With[0].Insertion
		if tc.transfer {
			c.ChatCommand("execute in minecraft:the_nether run tp " + identity + " -0.5 70 -0.5")
			if got := c.ExpectSystemChatPastMobs(); !bytes.Contains(got, []byte("commands.teleport.success.location.single")) {
				t.Fatalf("transfer %s: % X", tc.kind, got)
			}
		}
		bodies = append(bodies, body{tc.kind, identity})
	}
	if srv.ChunkLoadedInDimension(world.NetherName, -1, -1) {
		t.Fatal("adding entities loaded their destination terrain")
	}

	checkHidden := func() {
		t.Helper()
		for _, b := range bodies {
			// Exercise the visible lookup, the spatial section lookup and UUIDs.
			selectors := []string{"@e[type=" + b.kind + ",limit=1]", "@e[type=" + b.kind + ",x=-1,y=69,z=-1,dx=1,dy=2,dz=1,limit=1]"}
			selectors = append(selectors, b.uuid)
			for _, selector := range selectors {
				c.ChatCommand("execute in minecraft:the_nether run data get entity " + selector + " Pos")
				c.ExpectCommandFailure(servertest.CommandErrorMessageForTest("argument.entity.notfound.entity"))
			}
			c.ChatCommand("execute in minecraft:the_nether run kill @e[type=" + b.kind + "]")
			c.ExpectCommandFailure(servertest.CommandErrorMessageForTest("argument.entity.notfound.entity"))
		}
	}
	checkHidden()

	c.ChatCommand("execute in minecraft:the_nether run forceload add -1 -1")
	c.ExpectSystemChatPastMobs()
	for _, b := range bodies {
		c.ChatCommand("execute in minecraft:the_nether run data get entity @e[type=" + b.kind + ",limit=1] Pos")
		got, ok := protocol.ReadText(protocol.NewReader(c.ExpectSystemChatPastMobs()))
		if !ok || got.Translate != "commands.data.entity.query" || len(got.With) != 2 || got.With[1].Plain() != "[-0.5d, 70.0d, -0.5d]" {
			t.Errorf("accessible %s data: %+v", b.kind, got)
		}
		c.ChatCommand("data get entity " + b.uuid + " Pos")
		if got := c.ExpectSystemChatPastMobs(); !bytes.Contains(got, []byte("commands.data.entity.query")) {
			t.Errorf("accessible %s UUID: % X", b.kind, got)
		}
	}
	c.ChatCommand("execute in minecraft:the_nether run forceload remove -1 -1")
	c.ExpectSystemChatPastMobs()
	srv.FlushSaves()
	if srv.ChunkLoadedInDimension(world.NetherName, -1, -1) {
		t.Fatal("destination remained loaded after releasing its only hold")
	}
	checkHidden()
}
