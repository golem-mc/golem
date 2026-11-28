package commands_test

import (
	"bytes"
	"testing"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/servertest"
)

func expectSummonUUIDRefusal(t *testing.T, c *servertest.Client, what string) {
	t.Helper()
	var want protocol.Buffer
	want.PutText(protocol.Text{Color: protocol.ColorRed, Extra: []protocol.Text{
		{Translate: "commands.summon.failed.uuid"},
	}})
	want.PutBool(false)
	if got := c.ExpectPlay(protocol.IDPlaySystemChat); !bytes.Equal(got, want.Bytes()) {
		t.Errorf("%s with a taken UUID: reply = % X, want the duplicate-UUID refusal % X", what, got, want.Bytes())
	}
}

// TestASummonedBodyRefusesAUUIDAnotherKindHolds summons a marker, then an
// item and an armor stand with its UUID: each is refused.
// vanilla: SummonCommand.createEntity, PersistentEntitySectionManager.addEntityUuid
func TestASummonedBodyRefusesAUUIDAnotherKindHolds(t *testing.T) {
	t.Parallel()
	cfg := servertest.MobTestConfig()
	srv, c := servertest.Join(t, cfg, "Summoner", servertest.OpsListing(t, "Summoner"))

	c.ChatCommand(`summon minecraft:marker 1.5 64 0.5 {UUID:[I;1,2,3,4]}`)
	c.NextSystemChat()
	c.ChatCommand(`summon minecraft:marker 2.5 64 0.5 {UUID:[I;1,2,3,4]}`)
	expectSummonUUIDRefusal(t, c, "a second marker")
	c.ChatCommand(`summon minecraft:item 2.5 64 0.5 {UUID:[I;1,2,3,4],Item:{id:"minecraft:stone",count:1}}`)
	expectSummonUUIDRefusal(t, c, "an item")
	c.ChatCommand(`summon minecraft:armor_stand 2.5 64 0.5 {UUID:[I;1,2,3,4]}`)
	expectSummonUUIDRefusal(t, c, "an armor stand")

	c.ChatCommand(`summon minecraft:armor_stand 2.5 64 0.5 {UUID:[I;5,6,7,8]}`)
	c.NextSystemChat()
	c.ChatCommand(`summon minecraft:item 3.5 64 0.5 {UUID:[I;5,6,7,8],Item:{id:"minecraft:stone",count:1}}`)
	expectSummonUUIDRefusal(t, c, "an item over an armor stand")
	if n := srv.ItemCount(); n != 0 {
		t.Errorf("%d items left behind by refused summons, want 0", n)
	}
}

// TestAnUnloadedBodyNoLongerHoldsItsUUID leaves an item's chunk behind and
// summons an item with the same UUID where the player now is.
func TestAnUnloadedBodyNoLongerHoldsItsUUID(t *testing.T) {
	t.Parallel()
	cfg := servertest.MobTestConfig()
	cfg.LevelName = t.TempDir()
	srv, c := servertest.Join(t, cfg, "Summoner", servertest.OpsListing(t, "Summoner"))

	c.ChatCommand(`summon minecraft:item 1.5 64 0.5 {UUID:[I;1,2,3,4],Item:{id:"minecraft:stone",count:1}}`)
	c.NextSystemChat()
	c.ChatCommand(`tp @s 1000 64 1000`)
	c.NextSystemChat()
	servertest.WaitFor(t, srv, 400, func() bool { return srv.ItemCount() == 0 })
	servertest.RunTicks(t, srv, 40)
	c.ChatCommand(`summon minecraft:item ~ ~ ~ {UUID:[I;1,2,3,4],Item:{id:"minecraft:stone",count:1}}`)
	c.NextSystemChat()
	if n := srv.ItemCount(); n != 1 {
		t.Errorf("%d items after the summon, want 1", n)
	}
}
