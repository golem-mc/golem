package items

import (
	"github.com/golem-mc/golem/internal/server/servertest"
	"strings"
	"testing"

	"github.com/golem-mc/golem/internal/config"
	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server"
	"github.com/golem-mc/golem/internal/server/player"
	"github.com/golem-mc/golem/nbt"
	"github.com/golem-mc/golem/registry"
)

// brushScene is a survival player holding a brush and looking straight down
// at a suspicious block of the given kind laid under their feet, with the
// given block entity tag.
func brushScene(t *testing.T, block string, tag nbt.Compound) (*server.Server, *servertest.Client, [3]int32) {
	t.Helper()
	srv, _, c, cell := brushSceneWith(t, servertest.SurvivalTestConfig(), block, tag)
	return srv, c, cell
}

// brushSceneWith is brushScene under the given config and setups, also
// returning the function that connects further clients.
func brushSceneWith(t *testing.T, cfg *config.Config, block string, tag nbt.Compound, setups ...func(*server.Server)) (*server.Server, func() *servertest.Client, *servertest.Client, [3]int32) {
	t.Helper()
	srv, connect := servertest.Start(t, cfg, setups...)
	c := connect().Join("Brusher")
	servertest.HoldTicks(t, srv)
	srv.GiveSlot("Brusher", player.HotbarSlot, protocol.ItemStack{ItemID: servertest.ItemIDOrDie(t, "minecraft:brush"), Count: 1})
	pos := srv.PlayerPosition("Brusher")
	c.MoveTo(pos.X, pos.Y, pos.Z, 0, 90)
	c.Settle()
	cell := [3]int32{int32(pos.X - 0.5 + 0.5), int32(pos.Y) - 1, int32(pos.Z - 0.5 + 0.5)}
	if pos.X < 0 {
		cell[0]--
	}
	if pos.Z < 0 {
		cell[2]--
	}
	state := registry.BlockStates[block]
	if tag != nil || block != "minecraft:sand" {
		state = servertest.BlockStateWith(t, block, "dusted=0")
	}
	srv.SetBlock(cell[0], cell[1], cell[2], state)
	if tag != nil {
		srv.SetBlockEntityTagAt(cell[0], cell[1], cell[2], "minecraft:brushable_block", tag)
	}
	return srv, connect, c, cell
}

func brushDown(c *servertest.Client, cell [3]int32) {
	c.T.Helper()
	brushDownIn(c, protocol.HandMain, cell)
}

func brushDownIn(c *servertest.Client, hand int32, cell [3]int32) {
	c.T.Helper()
	c.Send(protocol.IDPlayUseItemOn, func(b *protocol.Buffer) {
		b.PutVarInt(hand)
		b.PutPosition(cell[0], cell[1], cell[2])
		b.PutVarInt(protocol.FaceTop)
		b.PutF32(0.5)
		b.PutF32(1)
		b.PutF32(0.5)
		b.PutBool(false)
		b.PutBool(false)
		b.PutVarInt(1)
	})
	c.Settle()
}

func dustedAt(t *testing.T, srv *server.Server, cell [3]int32) string {
	t.Helper()
	_, props, _ := registry.BlockStateName(srv.BlockAt(cell[0], cell[1], cell[2]))
	return registry.ParseStateProps(props)["dusted"]
}

var archaeologyTag = nbt.Compound{"LootTable": nbt.String("minecraft:archaeology/desert_pyramid")}

// TestBrushingSuspiciousSandAdvancesTheDustedStages: strokes land on the
// fifth tick and every tenth after, dusted follows the count, and the tenth
// stroke turns the block to sand.
// vanilla: BrushItem.onUseTick, BrushableBlockEntity.brush
func TestBrushingSuspiciousSandAdvancesTheDustedStages(t *testing.T) {
	t.Parallel()
	srv, c, cell := brushScene(t, "minecraft:suspicious_sand", archaeologyTag)
	brushDown(c, cell)

	servertest.RunTicks(t, srv, 4)
	if got := dustedAt(t, srv, cell); got != "0" {
		t.Fatalf("dusted after 4 ticks = %s, want 0", got)
	}
	servertest.RunTicks(t, srv, 1)
	if got := dustedAt(t, srv, cell); got != "1" {
		t.Fatalf("dusted after the first stroke = %s, want 1", got)
	}
	servertest.RunTicks(t, srv, 20) // strokes 2 and 3
	if got := dustedAt(t, srv, cell); got != "2" {
		t.Fatalf("dusted after the third stroke = %s, want 2", got)
	}
	servertest.RunTicks(t, srv, 30) // strokes 4 to 6
	if got := dustedAt(t, srv, cell); got != "3" {
		t.Fatalf("dusted after the sixth stroke = %s, want 3", got)
	}
	servertest.RunTicks(t, srv, 39) // strokes 7 to 9, the tenth not yet
	if got := dustedAt(t, srv, cell); got != "3" {
		t.Fatalf("dusted before the tenth stroke = %s, want 3", got)
	}
	servertest.RunTicks(t, srv, 1)
	if got, want := srv.BlockAt(cell[0], cell[1], cell[2]), registry.BlockStates["minecraft:sand"]; got != want {
		t.Fatalf("block after the tenth stroke = %d, want sand %d", got, want)
	}
}

// TestFinishingABrushableBlockDropsItsLootAndWearsTheBrush: the roll is
// dropped with no pickup delay, and only the finishing stroke
// wears the brush.
// vanilla: BrushableBlockEntity.brushingCompleted, BrushItem.onUseTick
func TestFinishingABrushableBlockDropsItsLootAndWearsTheBrush(t *testing.T) {
	t.Parallel()
	cfg := servertest.SurvivalTestConfig()
	cfg.ViewDistance, cfg.SimulationDistance = config.MinDistance, config.MinDistance
	srv, _, c, cell := brushSceneWith(t, cfg, "minecraft:suspicious_gravel", archaeologyTag)
	brushDown(c, cell)
	servertest.RunTicks(t, srv, 94)
	if got := srv.InventorySlot("Brusher", player.HotbarSlot).Damage; got != 0 {
		t.Fatalf("brush wear before the last stroke = %d, want 0", got)
	}
	if n := len(srv.ItemsInDimension("minecraft:overworld")); n != 0 {
		t.Fatalf("items on the ground before the last stroke = %d, want 0", n)
	}
	servertest.RunTicks(t, srv, 1)
	if got, want := srv.BlockAt(cell[0], cell[1], cell[2]), registry.BlockStates["minecraft:gravel"]; got != want {
		t.Fatalf("block = %d, want gravel %d", got, want)
	}
	if got := srv.InventorySlot("Brusher", player.HotbarSlot).Damage; got != 1 {
		t.Errorf("brush wear = %d, want 1", got)
	}
	// The player's touch follows the finishing stroke, even though the
	// newborn item's own tick is deferred. vanilla: LivingEntity.tick, Player.aiStep
	if n := len(srv.ItemsInDimension("minecraft:overworld")); n != 0 {
		t.Fatalf("items left on the ground = %d, want 0: the drop is picked up at once", n)
	}
	pool := map[string]bool{}
	for _, n := range []string{"archer_pottery_sherd", "miner_pottery_sherd", "prize_pottery_sherd",
		"skull_pottery_sherd", "diamond", "tnt", "gunpowder", "emerald"} {
		pool["minecraft:"+n] = true
	}
	found := false
	for slot := range 46 {
		name, _ := registry.ItemName(srv.InventorySlot("Brusher", slot).ItemID)
		found = found || pool[name]
	}
	if !found {
		t.Errorf("no item of the desert pyramid's loot in the inventory")
	}
	servertest.RunTicks(t, srv, 10)
	if got := srv.InventorySlot("Brusher", player.HotbarSlot).Damage; got != 1 {
		t.Errorf("brush wear after completion = %d, want 1", got)
	}
}

// fillInventory leaves the player no room to pick anything up, so a drop
// stays where it was laid.
func fillInventory(t *testing.T, srv *server.Server) {
	t.Helper()
	dirt := servertest.ItemStack(t, "minecraft:dirt", 64)
	for slot := 9; slot <= player.OffHandSlot; slot++ {
		if slot != player.HotbarSlot {
			srv.GiveSlot("Brusher", slot, dirt)
		}
	}
}

// brushSoundsHeard runs ticks of a hold over the block and returns the
// sounds a second player standing by hears.
func brushSoundsHeard(t *testing.T, block string, tag nbt.Compound, ticks int) []string {
	t.Helper()
	srv, connect, c, cell := brushSceneWith(t, servertest.SurvivalTestConfig(), block, tag)
	o := connect().Join("Watcher")
	o.Settle()
	brushDown(c, cell)
	servertest.RunTicks(t, srv, ticks)
	var names []string
	o.SettleAndCollect(func(id int32, data []byte) {
		if id != protocol.IDPlaySoundEffect {
			return
		}
		r := protocol.NewReader(data)
		name, ok := servertest.SoundEventName(r.ReadVarInt() - 1)
		if ok && strings.HasPrefix(name, "minecraft:item.brush.") {
			names = append(names, name)
		}
	})
	return names
}

// TestBrushStrokeSoundsFollowTheBlock: a stroke sounds as the block it
// lands on, generic over one that is not brushable, and not to the brusher.
// vanilla: BrushItem.onUseTick
func TestBrushStrokeSoundsFollowTheBlock(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		block, want string
		tag         nbt.Compound
	}{
		{"minecraft:suspicious_sand", "minecraft:item.brush.brushing.sand", archaeologyTag},
		{"minecraft:suspicious_gravel", "minecraft:item.brush.brushing.gravel", archaeologyTag},
		{"minecraft:sand", "minecraft:item.brush.brushing.generic", nil},
	} {
		t.Run(tc.block, func(t *testing.T) {
			t.Parallel()
			names := brushSoundsHeard(t, tc.block, tc.tag, 15)
			if len(names) != 2 || names[0] != tc.want || names[1] != tc.want {
				t.Errorf("sounds = %v, want two of %s", names, tc.want)
			}
		})
	}
}
