package entities

import (
	"testing"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server"
	"github.com/golem-mc/golem/internal/server/servertest"
	"github.com/golem-mc/golem/registry"
)

// soundsSeen reads n ticks of c's stream and returns the registry IDs of the
// sound effects and the world events the server sent.
func soundsSeen(c *servertest.Client, srv *server.Server, n int) (sounds map[int32]bool, events map[int32]bool) {
	sounds, events = map[int32]bool{}, map[int32]bool{}
	c.ReadTicks(srv, n, func(id int32, data []byte) bool {
		r := protocol.NewReader(data)
		switch id {
		case protocol.IDPlaySoundEffect:
			sounds[r.ReadVarInt()-1] = true
		case protocol.IDPlayWorldEvent:
			events[r.ReadI32()] = true
		}
		return false
	})
	return sounds, events
}

func soundID(t *testing.T, name string) int32 {
	t.Helper()
	id, ok := registry.SoundEventID(name)
	if !ok {
		t.Fatalf("unknown sound %s", name)
	}
	return id
}

// A Silent item frame, painting and falling anvil send no sound or level
// event for the actions the player and the body cause; a plain frame still
// sends its add and rotate sounds.
// vanilla: Entity.playSound, AnvilBlock.onLand, AnvilBlock.onBrokenAfterFall
func TestSilentHangingAndFallingEntitiesMakeNoSound(t *testing.T) {
	t.Parallel()
	srv, c := servertest.Join(t, servertest.SurvivalTestConfig(), "Op", servertest.OpsListing(t, "Op"))
	servertest.HoldTicks(t, srv)
	srv.SetBlock(5, 64, 4, servertest.PaintingWallState)
	c.ChatCommand("tp @s 6 64 3")
	servertest.AwaitSystemChatSaying(t, c, "commands.teleport.success.location.single")
	c.ChatCommand("give @s diamond 3")
	servertest.AwaitSystemChatSaying(t, c, "commands.give.success.single")
	c.Settle()

	add, rotate := soundID(t, "minecraft:entity.item_frame.add_item"), soundID(t, "minecraft:entity.item_frame.rotate_item")
	paintingBreak := soundID(t, "minecraft:entity.painting.break")

	frame := func(tag string) int32 {
		c.ChatCommand("summon minecraft:item_frame 6.5 64 4.5 {Facing:1b" + tag + "}")
		spawn := c.AwaitSpawn(protocol.EntityTypeItemFrame)
		c.AwaitMetadataFor(spawn.EntityID)
		servertest.AwaitSystemChatSaying(t, c, "commands.summon.success")
		return spawn.EntityID
	}
	for _, tc := range []struct {
		name, tag string
		silent    bool
	}{{"plain", "", false}, {"silent", ",Silent:1b", true}} {
		id := frame(tc.tag)
		c.UseOn(id)
		c.UseOn(id)
		sounds, _ := soundsSeen(c, srv, 3)
		if sounds[add] == tc.silent || sounds[rotate] == tc.silent {
			t.Errorf("%s frame: add_item=%v rotate_item=%v, silent=%v", tc.name, sounds[add], sounds[rotate], tc.silent)
		}
		c.Attack(id)
		c.Attack(id)
		sounds, _ = soundsSeen(c, srv, 3)
		for _, n := range []string{"remove_item", "break"} {
			if got := sounds[soundID(t, "minecraft:entity.item_frame."+n)]; got == tc.silent {
				t.Errorf("%s frame: %s sound = %v, silent=%v", tc.name, n, got, tc.silent)
			}
		}
	}

	c.ChatCommand(`summon painting 5 64 5 {variant:"minecraft:kebab",facing:0b,Silent:1b}`)
	spawn := c.AwaitSpawn(protocol.EntityTypePainting)
	servertest.AwaitSystemChatSaying(t, c, "commands.summon.success")
	c.Attack(spawn.EntityID)
	sounds, _ := soundsSeen(c, srv, 3)
	if sounds[paintingBreak] {
		t.Error("a silent painting played its break sound")
	}
	if srv.PaintingCount() != 0 {
		t.Fatal("the blow did not break the painting")
	}

	for _, tc := range []struct {
		anvil, tag string
		event      int32
	}{
		{"anvil", "", protocol.WorldEventAnvilLand},
		{"damaged_anvil", "", protocol.WorldEventAnvilLand},
		{"anvil", ",Silent:1b", 0},
		{"damaged_anvil", ",Silent:1b", 0},
	} {
		c.ChatCommand(`summon falling_block 8.5 90 3.5 {BlockState:{id:"minecraft:` + tc.anvil + `"},Time:1` + tc.tag + `}`)
		servertest.AwaitSystemChatSaying(t, c, "commands.summon.success")
		_, events := soundsSeen(c, srv, 60)
		if tc.event != 0 && !events[tc.event] {
			t.Errorf("a plain falling %s sent events %v, want %d", tc.anvil, events, tc.event)
		}
		if tc.event == 0 && (events[protocol.WorldEventAnvilLand] || events[protocol.WorldEventAnvilBroken]) {
			t.Errorf("a silent falling %s sent anvil level events %v", tc.anvil, events)
		}
		c.ChatCommand("setblock 8 64 3 air")
		servertest.AwaitSystemChatSaying(t, c, "commands.setblock.success")
	}
}
