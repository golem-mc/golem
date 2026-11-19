package items

import (
	"github.com/golem-mc/golem/internal/server/servertest"
	"math"
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

// vanilla: BrushableBlockEntity.dropContent, EntityTickList.forEach, ItemEntity.playerTouch
func TestUncollectedBrushLootWaitsForItsFirstItemTick(t *testing.T) {
	t.Parallel()
	cfg := servertest.SurvivalTestConfig()
	cfg.ViewDistance, cfg.SimulationDistance = config.MinDistance, config.MinDistance
	srv, _, c, cell := brushSceneWith(t, cfg, "minecraft:suspicious_gravel", nbt.Compound{
		"item": nbt.Compound{"id": nbt.String("minecraft:diamond"), "count": nbt.Int(1)},
	})
	for slot := player.MainSlot; slot < player.HotbarSlot+player.HotbarSlots; slot++ {
		if slot != player.HotbarSlot {
			srv.GiveSlot("Brusher", slot, protocol.ItemStack{ItemID: int32(registry.ItemStone), Count: 64})
		}
	}
	brushDown(c, cell)
	servertest.RunTicks(t, srv, 95)
	items := srv.ItemsInDimension("minecraft:overworld")
	if len(items) != 1 {
		t.Fatalf("finishing stroke: %+v, want one uncollected diamond", items)
	}
	it := items[0]
	if registry.Item(it.Stack.ItemID) != registry.ItemDiamond || it.Stack.Count != 1 ||
		it.Age != 0 || it.PickupDelay != 0 || it.VX != 0 || it.VY != 0 || it.VZ != 0 {
		t.Fatalf("finishing stroke: %+v, want stationary newborn diamond with no pickup delay", it)
	}
	if got := srv.InventorySlot("Brusher", player.HotbarSlot).Damage; got != 1 {
		t.Errorf("brush wear = %d, want 1", got)
	}
	servertest.RunTicks(t, srv, 1)
	items = srv.ItemsInDimension("minecraft:overworld")
	if len(items) != 1 || items[0].Age != 1 || items[0].Y >= it.Y {
		t.Fatalf("following tick: %+v, want the diamond's first fall", items)
	}
	srv.GiveSlot("Brusher", player.MainSlot, protocol.ItemStack{})
	servertest.RunTicks(t, srv, 1)
	if items = srv.ItemsInDimension("minecraft:overworld"); len(items) != 0 {
		t.Fatalf("after making room: %+v, want the diamond picked up", items)
	}
	if got := srv.InventorySlot("Brusher", player.MainSlot); registry.Item(got.ItemID) != registry.ItemDiamond || got.Count != 1 {
		t.Errorf("loot in inventory = %+v, want one diamond", got)
	}
}

// TestLettingGoOfTheBrushLetsTheDustSettle: a block left alone recovers two
// strokes' worth forty ticks after the last, then every four.
// vanilla: BrushableBlockEntity.checkReset
func TestLettingGoOfTheBrushLetsTheDustSettle(t *testing.T) {
	t.Parallel()
	srv, c, cell := brushScene(t, "minecraft:suspicious_sand", archaeologyTag)
	brushDown(c, cell)
	servertest.RunTicks(t, srv, 25) // strokes at 5, 15 and 25: count 3, dusted 2
	if got := dustedAt(t, srv, cell); got != "2" {
		t.Fatalf("dusted = %s, want 2", got)
	}
	c.Send(protocol.IDPlayPlayerAction, func(b *protocol.Buffer) {
		b.PutVarInt(protocol.PlayerActionReleaseUseItem)
		b.PutPosition(0, 0, 0)
		b.PutI8(0)
		b.PutVarInt(2)
	})
	c.Settle()
	servertest.RunTicks(t, srv, 38)
	if got := dustedAt(t, srv, cell); got != "2" {
		t.Fatalf("dusted before the reset = %s, want 2", got)
	}
	servertest.RunTicks(t, srv, 2) // tick 65, forty after the last stroke: count 3 -> 1
	if got := dustedAt(t, srv, cell); got != "1" {
		t.Fatalf("dusted after the first reset = %s, want 1", got)
	}
	servertest.RunTicks(t, srv, 3)
	if got := dustedAt(t, srv, cell); got != "1" {
		t.Fatalf("dusted before the second reset = %s, want 1", got)
	}
	servertest.RunTicks(t, srv, 1) // four ticks on: count 1 -> 0
	if got := dustedAt(t, srv, cell); got != "0" {
		t.Fatalf("dusted after the dust settled = %s, want 0", got)
	}
}

// TestBrushingStopsWhenTheLookLeavesTheBlock: a hold does not carry on
// against a block no longer looked at.
// vanilla: BrushItem.onUseTick
func TestBrushingStopsWhenTheLookLeavesTheBlock(t *testing.T) {
	t.Parallel()
	srv, c, cell := brushScene(t, "minecraft:suspicious_sand", archaeologyTag)
	brushDown(c, cell)
	pos := srv.PlayerPosition("Brusher")
	c.MoveTo(pos.X, pos.Y, pos.Z, 0, -90)
	c.Settle()
	servertest.RunTicks(t, srv, 30)
	if got := dustedAt(t, srv, cell); got != "0" {
		t.Fatalf("dusted = %s, want 0 after the look turned up", got)
	}
}

// TestBrushingPlainSandBrushesNothing: only the brushable blocks take the
// strokes; the brush itself stays unworn.
func TestBrushingPlainSandBrushesNothing(t *testing.T) {
	t.Parallel()
	srv, c, cell := brushScene(t, "minecraft:suspicious_sand", nil)
	srv.SetBlock(cell[0], cell[1], cell[2], registry.BlockStates["minecraft:sand"])
	brushDown(c, cell)
	servertest.RunTicks(t, srv, 100)
	if got, want := srv.BlockAt(cell[0], cell[1], cell[2]), registry.BlockStates["minecraft:sand"]; got != want {
		t.Errorf("block = %d, want sand", got)
	}
	if got := srv.InventorySlot("Brusher", player.HotbarSlot).Damage; got != 0 {
		t.Errorf("brush wear = %d, want 0", got)
	}
}

// TestABrushableBlockReplacedStartsOver: a block broken and laid again does
// not inherit the strokes the old one took.
// vanilla: BrushableBlockEntity.brushCount
func TestABrushableBlockReplacedStartsOver(t *testing.T) {
	t.Parallel()
	srv, c, cell := brushScene(t, "minecraft:suspicious_sand", archaeologyTag)
	brushDown(c, cell)
	servertest.RunTicks(t, srv, 25) // three strokes: dusted 2
	srv.SetBlock(cell[0], cell[1], cell[2], registry.BlockStates["minecraft:air"])
	srv.RemoveBlockEntity(cell[0], cell[1], cell[2])
	servertest.RunTicks(t, srv, 1)
	srv.SetBlock(cell[0], cell[1], cell[2], servertest.BlockStateWith(t, "minecraft:suspicious_sand", "dusted=0"))
	srv.SetBlockEntityTagAt(cell[0], cell[1], cell[2], "minecraft:brushable_block", archaeologyTag)
	servertest.RunTicks(t, srv, 9) // the fourth stroke lands on the new block: its first
	if got := dustedAt(t, srv, cell); got != "1" {
		t.Fatalf("dusted after one stroke on the new block = %s, want 1", got)
	}
}

// TestBrushingSeesTheOutlineNotTheCollisionShape: a torch in the look's way
// has no collision but is what the brush meets, so the sand behind it is
// not brushed.
// vanilla: BrushItem.calculateHitResult
func TestBrushingSeesTheOutlineNotTheCollisionShape(t *testing.T) {
	t.Parallel()
	srv, c, cell := brushScene(t, "minecraft:suspicious_sand", archaeologyTag)
	srv.SetBlock(cell[0], cell[1]+1, cell[2], registry.BlockStates["minecraft:torch"])
	brushDown(c, cell)
	servertest.RunTicks(t, srv, 30)
	if got := dustedAt(t, srv, cell); got != "0" {
		t.Fatalf("dusted = %s, want 0 with a torch in the way", got)
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

// TestTheBrushedLootLiesAgainstTheFaceBrushed: the drop is laid in the cell
// in front of the face the look first met, at rest.
// vanilla: BrushableBlockEntity.dropContent
func TestTheBrushedLootLiesAgainstTheFaceBrushed(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name       string
		dx, dz     int32
		yaw, pitch float32
	}{
		{"top", 0, 0, 0, 90},
		{"west face", 1, 0, -90, 0},
		{"east face", -1, 0, 90, 0},
		{"north face", 0, 1, 0, 0},
		{"south face", 0, -1, 180, 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			srv, c, cell := brushScene(t, "minecraft:suspicious_sand", archaeologyTag)
			fillInventory(t, srv)
			target := cell
			if tc.pitch == 0 {
				srv.SetBlock(cell[0], cell[1], cell[2], registry.BlockStates["minecraft:stone"])
				target = [3]int32{cell[0] + tc.dx, cell[1] + 2, cell[2] + tc.dz}
				// Suspicious sand falls without a block under it.
				srv.SetBlock(target[0], target[1]-1, target[2], registry.BlockStates["minecraft:stone"])
				srv.SetBlock(target[0], target[1], target[2], servertest.BlockStateWith(t, "minecraft:suspicious_sand", "dusted=0"))
				srv.SetBlockEntityTagAt(target[0], target[1], target[2], "minecraft:brushable_block", archaeologyTag)
				pos := srv.PlayerPosition("Brusher")
				c.MoveTo(pos.X, pos.Y, pos.Z, tc.yaw, 0)
				c.Settle()
			}
			brushDown(c, target)
			servertest.RunTicks(t, srv, 95)
			items := srv.ItemsInDimension("minecraft:overworld")
			if len(items) != 1 {
				t.Fatalf("items on the ground = %d, want 1", len(items))
			}
			// The cell the look came from: against the face the ray met.
			wantX := float64(target[0]-tc.dx) + 0.5
			wantZ := float64(target[2]-tc.dz) + 0.5
			wantY := float64(target[1]) + 0.5
			if tc.pitch != 0 {
				wantY++
			}
			if it := items[0]; math.Abs(it.X-wantX) > 0.01 || math.Abs(it.Z-wantZ) > 0.01 || math.Abs(it.Y-wantY) > 0.2 {
				t.Errorf("drop at (%.2f, %.2f, %.2f), want near (%.2f, %.2f, %.2f)", it.X, it.Y, it.Z, wantX, wantY, wantZ)
			}
		})
	}
}

// TestBrushingTellsWatchersWhichFaceAndItem: the block entity update after a
// stroke carries the face brushed from and the item the block hides.
// vanilla: BrushableBlockEntity.getUpdateTag
func TestBrushingTellsWatchersWhichFaceAndItem(t *testing.T) {
	t.Parallel()
	srv, c, cell := brushScene(t, "minecraft:suspicious_sand", archaeologyTag)
	brushDown(c, cell)
	servertest.RunTicks(t, srv, 5)
	x, y, z, kind, data := c.AwaitBlockEntityData()
	if [3]int32{x, y, z} != cell || kind != protocol.BlockEntityBrushableBlock {
		t.Fatalf("block entity update at (%d,%d,%d) type %d", x, y, z, kind)
	}
	if got := data["hit_direction"]; got != nbt.Int(protocol.FaceTop) {
		t.Errorf("hit_direction = %#v, want the top face %d", got, protocol.FaceTop)
	}
	if _, ok := data["item"]; !ok {
		t.Errorf("update carries no item: %v", data)
	}
}

// TestABrushStrokeSendsTheBlockBeforeItsBlockEntityData: the update tag
// follows the dusted state on the wire, not the other way round.
// vanilla: ChunkHolder.broadcastBlockEntityIfNeeded
func TestABrushStrokeSendsTheBlockBeforeItsBlockEntityData(t *testing.T) {
	t.Parallel()
	srv, c, cell := brushScene(t, "minecraft:suspicious_sand", archaeologyTag)
	brushDown(c, cell)
	servertest.RunTicks(t, srv, 4)
	c.ExpectBlockBeforeEntityData(srv, 2, cell)
}

// TestFinishingABrushPlaysLevelEvent3008: the finishing stroke tells the
// watchers the particle and sound event, with the block state as its data.
// vanilla: BrushableBlockEntity.brushingCompleted
func TestFinishingABrushPlaysLevelEvent3008(t *testing.T) {
	t.Parallel()
	srv, c, cell := brushScene(t, "minecraft:suspicious_gravel", archaeologyTag)
	brushDown(c, cell)
	servertest.RunTicks(t, srv, 94)
	state := srv.BlockAt(cell[0], cell[1], cell[2])
	servertest.RunTicks(t, srv, 1)
	found := false
	c.SettleAndCollect(func(id int32, data []byte) {
		if id != protocol.IDPlayWorldEvent {
			return
		}
		r := protocol.NewReader(data)
		event := r.ReadI32()
		x, y, z := r.ReadPosition()
		if event == 3008 {
			found = true
			if [3]int32{x, y, z} != cell || r.ReadI32() != int32(state) {
				t.Errorf("event 3008 at (%d,%d,%d) with the wrong data", x, y, z)
			}
		}
	})
	if !found {
		t.Error("no level event 3008 after the finishing stroke")
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

// TestABrushHoldEndsAfterTwoHundredTicks: strokes stop with the hold, so a
// block that is never finished is stroked twenty times.
// vanilla: BrushItem.onUseTick
func TestABrushHoldEndsAfterTwoHundredTicks(t *testing.T) {
	t.Parallel()
	if got := len(brushSoundsHeard(t, "minecraft:sand", nil, 300)); got != 20 {
		t.Errorf("strokes heard over a 300-tick hold = %d, want 20", got)
	}
}

// TestTheOffhandBrushWearsWhenItFinishes: a brush in the off hand brushes
// and takes the wear.
// vanilla: BrushItem.useOn
func TestTheOffhandBrushWearsWhenItFinishes(t *testing.T) {
	t.Parallel()
	srv, _, c, cell := brushSceneWith(t, servertest.SurvivalTestConfig(), "minecraft:suspicious_sand", archaeologyTag)
	srv.GiveSlot("Brusher", player.OffHandSlot, srv.InventorySlot("Brusher", player.HotbarSlot))
	srv.GiveSlot("Brusher", player.HotbarSlot, protocol.ItemStack{})
	brushDownIn(c, protocol.HandOff, cell)
	servertest.RunTicks(t, srv, 95)
	if got, want := srv.BlockAt(cell[0], cell[1], cell[2]), registry.BlockStates["minecraft:sand"]; got != want {
		t.Fatalf("block = %d, want sand %d", got, want)
	}
	if got := srv.InventorySlot("Brusher", player.OffHandSlot).Damage; got != 1 {
		t.Errorf("off-hand brush wear = %d, want 1", got)
	}
}

// TestACreativeBrushDoesNotWear: the block is finished all the same.
// vanilla: ItemStack.hurtAndBreak
func TestACreativeBrushDoesNotWear(t *testing.T) {
	t.Parallel()
	srv, _, c, cell := brushSceneWith(t, servertest.BlockTestConfig(), "minecraft:suspicious_sand", archaeologyTag)
	brushDown(c, cell)
	servertest.RunTicks(t, srv, 95)
	if got, want := srv.BlockAt(cell[0], cell[1], cell[2]), registry.BlockStates["minecraft:sand"]; got != want {
		t.Fatalf("block = %d, want sand %d", got, want)
	}
	if got := srv.InventorySlot("Brusher", player.HotbarSlot).Damage; got != 0 {
		t.Errorf("creative brush wear = %d, want 0", got)
	}
}

// TestABrushHoldEnds: a swapped hotbar slot, the brush gone from the hand, death and spectator each stop
// the hold before the first stroke.
// vanilla: LivingEntity.updatingUsingItem, BrushItem.onUseTick
func TestABrushHoldEnds(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name  string
		end   func(srv *server.Server, c *servertest.Client)
		ticks int // how long to wait before reading the block
	}{
		{"slot swap", func(_ *server.Server, c *servertest.Client) {
			c.Send(protocol.IDPlayHeldItemIn, func(b *protocol.Buffer) { b.PutI16(1) })
			c.Settle()
		}, 30},
		{"brush taken from the hand", func(srv *server.Server, _ *servertest.Client) {
			srv.GiveSlot("Brusher", player.HotbarSlot, protocol.ItemStack{})
		}, 30},
		// A dead player's chunk holds drop twenty ticks after death
		// (LivingEntity.tickDeath), so the block is read inside that window.
		{"death", func(srv *server.Server, _ *servertest.Client) { srv.Hurt("Brusher", 1000) }, 10},
		{"spectator", func(srv *server.Server, c *servertest.Client) {
			c.SendChangeGameMode(protocol.GameModeSpectator)
			servertest.PollUntil(t, servertest.ExpectTimeout, func() bool { return srv.PlayerGameMode("Brusher") == protocol.GameModeSpectator })
		}, 30},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			srv, _, c, cell := brushSceneWith(t, servertest.SurvivalTestConfig(), "minecraft:suspicious_sand", archaeologyTag, servertest.OpsListing(t, "Brusher"))
			brushDown(c, cell)
			servertest.RunTicks(t, srv, 2)
			tc.end(srv, c)
			servertest.RunTicks(t, srv, tc.ticks)
			if got := dustedAt(t, srv, cell); got != "0" {
				t.Errorf("dusted = %s, want 0 after the hold ended", got)
			}
		})
	}
}

// TestBrushingRollsWithTheBrushersLuck: a pack table of no rolls and one
// bonus roll, an emerald for a player THIS holding a brush, hides an emerald
// for a player under Luck I. The pack layer is process-wide, so the test is
// not parallel and puts the built-in tables back when it ends.
// vanilla: BrushableBlockEntity.unpackLootTable, LootPool.addRandomItems
func TestBrushingRollsWithTheBrushersLuck(t *testing.T) {
	t.Cleanup(func() { registry.ApplyPackLoot(nil, nil, nil) })
	cfg := servertest.SurvivalTestConfig()
	cfg.LevelName = t.TempDir()
	servertest.WritePackFile(t, cfg, "luck", "luck/loot_table/brushed.json",
		`{"pools":[{"rolls":0,"bonus_rolls":1,"entries":[{"type":"minecraft:item","name":"minecraft:emerald","conditions":[
		{"condition":"minecraft:entity_properties","entity":"this","predicate":{"minecraft:entity_type":"minecraft:player"}},
		{"condition":"minecraft:match_tool","predicate":{"items":"minecraft:brush"}}]}]}]}`)
	srv, _, c, cell := brushSceneWith(t, cfg, "minecraft:suspicious_sand", nbt.Compound{"LootTable": nbt.String("luck:brushed")})
	srv.RunLine("effect give Brusher minecraft:luck 100 0")
	servertest.RunTicks(t, srv, 1)
	brushDown(c, cell)
	servertest.RunTicks(t, srv, 5) // the first stroke unpacks the table
	tag, _ := srv.BlockEntityTagAt(cell[0], cell[1], cell[2])
	if _, pending := tag["LootTable"]; pending {
		t.Fatal("the table is still pending after the first stroke")
	}
	item, _ := nbt.Get[nbt.Compound](tag, "item")
	if id, _ := nbt.Get[nbt.String](item, "id"); id != "minecraft:emerald" {
		t.Errorf("the hidden item is %q, want minecraft:emerald (the bonus roll at luck 1)", id)
	}
}
