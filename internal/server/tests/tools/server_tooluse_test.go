package tools

import (
	"fmt"
	"github.com/golem-mc/golem/internal/server/servertest"
	"reflect"
	"testing"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/entity"
	"github.com/golem-mc/golem/internal/server/player"
	"github.com/golem-mc/golem/internal/server/rules"
	"github.com/golem-mc/golem/internal/server/world"
	"github.com/golem-mc/golem/registry"
)

// TestAnAxeStripsALog: the stripped log keeps the axis, the axe takes one
// wear, and the strip sound reaches the other player. vanilla:
// AxeItem.useOn
func TestAnAxeStripsALog(t *testing.T) {
	t.Parallel()
	srv, actor, watcher := servertest.ToolScene(t, "minecraft:iron_axe")

	srv.SetBlock(3, 63, 0, servertest.BlockStateWith(t, "minecraft:oak_log", "axis=x"))
	actor.UseBlock(3, 63, 0)
	actor.Settle()

	if got, want := srv.BlockAt(3, 63, 0), servertest.BlockStateWith(t, "minecraft:stripped_oak_log", "axis=x"); got != want {
		t.Errorf("the log after the axe = %d, want %d (stripped, axis=x)", got, want)
	}
	if got := srv.InventorySlot("Actor", player.HotbarSlot); got.Damage != 1 {
		t.Errorf("the axe's wear = %d, want 1", got.Damage)
	}
	h := servertest.HearTool(t, watcher)
	if h.Sounds["minecraft:item.axe.strip"] != 1 {
		t.Errorf("the watcher heard %v, want one item.axe.strip", h.Sounds)
	}
	if len(h.Events) != 0 {
		t.Errorf("a strip sent world events %v, want none", h.Events)
	}
}

// TestAnAxeScrapesWeatheredCopper: one stage of oxidation comes off, with
// the scrape sound and particles (level event 3005) for the others.
func TestAnAxeScrapesWeatheredCopper(t *testing.T) {
	t.Parallel()
	srv, actor, watcher := servertest.ToolScene(t, "minecraft:iron_axe")

	srv.SetBlock(3, 63, 0, registry.BlockStates["minecraft:weathered_copper"])
	actor.UseBlock(3, 63, 0)
	actor.Settle()

	if got, want := srv.BlockAt(3, 63, 0), registry.BlockStates["minecraft:exposed_copper"]; got != want {
		t.Errorf("the copper after the axe = %d, want %d (exposed)", got, want)
	}
	if got := srv.InventorySlot("Actor", player.HotbarSlot); got.Damage != 1 {
		t.Errorf("the axe's wear = %d, want 1", got.Damage)
	}
	h := servertest.HearTool(t, watcher)
	if h.Sounds["minecraft:item.axe.scrape"] != 1 {
		t.Errorf("the watcher heard %v, want one item.axe.scrape", h.Sounds)
	}
	if got := h.Events[3005]; len(got) != 1 || got[0] != [3]int32{3, 63, 0} {
		t.Errorf("scrape particles at %v, want one at (3, 63, 0)", got)
	}
}

// TestAnAxeTakesTheWaxOff: waxed copper loses its wax and keeps its state
// otherwise, with the wax-off sound and particles (level event 3004).
func TestAnAxeTakesTheWaxOff(t *testing.T) {
	t.Parallel()
	srv, actor, watcher := servertest.ToolScene(t, "minecraft:iron_axe")

	srv.SetBlock(3, 63, 0, servertest.BlockStateWith(t, "minecraft:waxed_weathered_cut_copper_stairs",
		"facing=east,half=top,shape=straight,waterlogged=false"))
	actor.UseBlock(3, 63, 0)
	actor.Settle()

	want := servertest.BlockStateWith(t, "minecraft:weathered_cut_copper_stairs",
		"facing=east,half=top,shape=straight,waterlogged=false")
	if got := srv.BlockAt(3, 63, 0); got != want {
		t.Errorf("the stairs after the axe = %d, want %d (unwaxed, same shape)", got, want)
	}
	h := servertest.HearTool(t, watcher)
	if h.Sounds["minecraft:item.axe.wax_off"] != 1 {
		t.Errorf("the watcher heard %v, want one item.axe.wax_off", h.Sounds)
	}
	if got := h.Events[3004]; len(got) != 1 || got[0] != [3]int32{3, 63, 0} {
		t.Errorf("wax-off particles at %v, want one at (3, 63, 0)", got)
	}
}

// TestAShieldInTheOffHandHoldsTheAxeBack: a main-hand axe with a shield
// in the off hand is a block, not a strip, unless the player sneaks.
// vanilla: AxeItem.playerHasBlockingItemUseIntent
func TestAShieldInTheOffHandHoldsTheAxeBack(t *testing.T) {
	t.Parallel()
	srv, actor, _ := servertest.ToolScene(t, "minecraft:iron_axe")
	srv.GiveSlot("Actor", player.OffHandSlot, protocol.ItemStack{ItemID: servertest.ItemIDOrDie(t, "minecraft:shield"), Count: 1})

	log := servertest.BlockStateWith(t, "minecraft:oak_log", "axis=y")
	srv.SetBlock(3, 63, 0, log)
	actor.UseBlock(3, 63, 0)
	actor.Settle()
	if got := srv.BlockAt(3, 63, 0); got != log {
		t.Errorf("the log under a shield-holder's axe = %d, want %d (untouched)", got, log)
	}
	if got := srv.InventorySlot("Actor", player.HotbarSlot); got.Damage != 0 {
		t.Errorf("the axe's wear = %d, want 0", got.Damage)
	}

	actor.Send(protocol.IDPlayPlayerInput, func(b *protocol.Buffer) { b.PutU8(protocol.InputSneak) })
	actor.Settle()
	actor.UseBlock(3, 63, 0)
	actor.Settle()
	if got, want := srv.BlockAt(3, 63, 0), servertest.BlockStateWith(t, "minecraft:stripped_oak_log", "axis=y"); got != want {
		t.Errorf("the log under a sneaking shield-holder's axe = %d, want %d (stripped)", got, want)
	}
}

// TestAShieldInTheOffHandHoldsTheShovelBack: a main-hand shovel with a
// shield in the off hand flattens no path unless the player sneaks.
// vanilla: BlockTransformer.playerHasBlockingItemUseIntent
func TestAShieldInTheOffHandHoldsTheShovelBack(t *testing.T) {
	t.Parallel()
	shieldHoldsBack(t, "minecraft:iron_shovel", "minecraft:dirt_path")
}

// TestAShieldInTheOffHandHoldsTheHoeBack: a main-hand hoe with a shield in
// the off hand tills nothing unless the player sneaks.
// vanilla: BlockTransformer.playerHasBlockingItemUseIntent
func TestAShieldInTheOffHandHoldsTheHoeBack(t *testing.T) {
	t.Parallel()
	shieldHoldsBack(t, "minecraft:iron_hoe", "minecraft:farmland")
}

// shieldHoldsBack clicks grass with tool and a shield in the off hand: the
// grass stays and the tool unworn, then a sneaking click turns it into the
// block named into.
func shieldHoldsBack(t *testing.T, tool, into string) {
	t.Helper()
	srv, actor, _ := servertest.ToolScene(t, tool)
	srv.GiveSlot("Actor", player.OffHandSlot, protocol.ItemStack{ItemID: servertest.ItemIDOrDie(t, "minecraft:shield"), Count: 1})

	grass := registry.BlockStates["minecraft:grass_block"]
	srv.SetBlock(3, 63, 0, grass)
	actor.UseBlock(3, 63, 0)
	actor.Settle()
	if got := srv.BlockAt(3, 63, 0); got != grass {
		t.Errorf("the grass under a shield-holder's %s = %d, want %d (untouched)", tool, got, grass)
	}
	if got := srv.InventorySlot("Actor", player.HotbarSlot); got.Damage != 0 {
		t.Errorf("the %s's wear = %d, want 0", tool, got.Damage)
	}

	actor.Send(protocol.IDPlayPlayerInput, func(b *protocol.Buffer) { b.PutU8(protocol.InputSneak) })
	actor.Settle()
	actor.UseBlock(3, 63, 0)
	actor.Settle()
	if got, want := srv.BlockAt(3, 63, 0), registry.BlockStates[into]; got != want {
		t.Errorf("the grass under a sneaking shield-holder's %s = %d, want %d", tool, got, want)
	}
}

// TestAnAxeScrapesBothHalvesOfACopperDoor: a sneaking click scrapes the
// half it lands on, and the other half follows through the door's shape
// update, which adopts its partner's block. Without the sneak the click
// opens the door instead.
func TestAnAxeScrapesBothHalvesOfACopperDoor(t *testing.T) {
	t.Parallel()
	srv, actor, _ := servertest.ToolScene(t, "minecraft:iron_axe")

	const props = ",hinge=left,open=false,powered=false"
	srv.SetBlock(3, 64, 0, servertest.BlockStateWith(t, "minecraft:weathered_copper_door", "facing=north,half=lower"+props))
	srv.SetBlock(3, 65, 0, servertest.BlockStateWith(t, "minecraft:weathered_copper_door", "facing=north,half=upper"+props))

	actor.Send(protocol.IDPlayPlayerInput, func(b *protocol.Buffer) { b.PutU8(protocol.InputSneak) })
	actor.Settle()
	actor.UseBlock(3, 64, 0)
	actor.Settle()

	if got, want := srv.BlockAt(3, 64, 0), servertest.BlockStateWith(t, "minecraft:exposed_copper_door", "facing=north,half=lower"+props); got != want {
		t.Errorf("the lower half = %d, want %d (exposed)", got, want)
	}
	if got, want := srv.BlockAt(3, 65, 0), servertest.BlockStateWith(t, "minecraft:exposed_copper_door", "facing=north,half=upper"+props); got != want {
		t.Errorf("the upper half = %d, want %d (exposed)", got, want)
	}
}

// TestACopperChestKeepsTheAxeOff: an unsneaked click opens the chest's
// menu instead, so the axe changes nothing and takes no wear; a sneaking
// click scrapes it.
func TestACopperChestKeepsTheAxeOff(t *testing.T) {
	t.Parallel()
	srv, actor, watcher := servertest.ToolScene(t, "minecraft:iron_axe")

	chest := servertest.BlockStateWith(t, "minecraft:exposed_copper_chest", "facing=north,type=single,waterlogged=false")
	srv.SetBlock(3, 64, 0, chest)
	actor.UseBlock(3, 64, 0)
	actor.Settle()
	if got := srv.BlockAt(3, 64, 0); got != chest {
		t.Errorf("the chest under an unsneaked axe = %d, want %d (unchanged)", got, chest)
	}
	if got := srv.InventorySlot("Actor", player.HotbarSlot); got.Damage != 0 {
		t.Errorf("the axe's wear = %d, want 0", got.Damage)
	}
	if h := servertest.HearTool(t, watcher); h.Sounds["minecraft:item.axe.scrape"] != 0 || len(h.Events) != 0 {
		t.Errorf("the unsneaked click played %v and %v, want nothing", h.Sounds, h.Events)
	}

	actor.Send(protocol.IDPlayPlayerInput, func(b *protocol.Buffer) { b.PutU8(protocol.InputSneak) })
	actor.Settle()
	actor.UseBlock(3, 64, 0)
	actor.Settle()
	if got, want := srv.BlockAt(3, 64, 0), servertest.BlockStateWith(t, "minecraft:copper_chest", "facing=north,type=single,waterlogged=false"); got != want {
		t.Errorf("the chest under a sneaking axe = %d, want %d (scraped)", got, want)
	}
	if got := srv.InventorySlot("Actor", player.HotbarSlot); got.Damage != 1 {
		t.Errorf("the axe's wear = %d, want 1", got.Damage)
	}
	h := servertest.HearTool(t, watcher)
	if h.Sounds["minecraft:item.axe.scrape"] != 1 {
		t.Errorf("the watcher heard %v, want one item.axe.scrape", h.Sounds)
	}
	if got := h.Events[3005]; len(got) != 1 || got[0] != [3]int32{3, 64, 0} {
		t.Errorf("scrape particles at %v, want one at (3, 64, 0)", got)
	}
}

// TestAShovelFlattensAPath: grass under air becomes a path, with the
// flatten sound for the others and one wear. vanilla: ShovelItem.useOn
func TestAShovelFlattensAPath(t *testing.T) {
	t.Parallel()
	srv, actor, watcher := servertest.ToolScene(t, "minecraft:iron_shovel")

	srv.SetBlock(3, 63, 0, registry.BlockStates["minecraft:grass_block"])
	actor.UseBlock(3, 63, 0)
	actor.Settle()

	if got, want := srv.BlockAt(3, 63, 0), registry.BlockStates["minecraft:dirt_path"]; got != want {
		t.Errorf("the grass after the shovel = %d, want %d (dirt path)", got, want)
	}
	if got := srv.InventorySlot("Actor", player.HotbarSlot); got.Damage != 1 {
		t.Errorf("the shovel's wear = %d, want 1", got.Damage)
	}
	if h := servertest.HearTool(t, watcher); h.Sounds["minecraft:item.shovel.flatten"] != 1 {
		t.Errorf("the watcher heard %v, want one item.shovel.flatten", h.Sounds)
	}
}

// TestAShovelLeavesCoveredGround: no path under a block, and none from a
// click on the bottom face; the shovel takes no wear for either.
func TestAShovelLeavesCoveredGround(t *testing.T) {
	t.Parallel()
	srv, actor, _ := servertest.ToolScene(t, "minecraft:iron_shovel")

	grass := registry.BlockStates["minecraft:grass_block"]
	srv.SetBlock(3, 63, 0, grass)
	srv.SetBlock(3, 64, 0, registry.BlockStates["minecraft:stone"])
	actor.UseBlock(3, 63, 0)
	actor.Settle()
	if got := srv.BlockAt(3, 63, 0); got != grass {
		t.Errorf("covered grass after the shovel = %d, want %d (grass)", got, grass)
	}

	srv.SetBlock(4, 65, 0, grass)
	actor.PlaceBlockAt(4, 65, 0, protocol.FaceBottom, 0)
	actor.Settle()
	if got := srv.BlockAt(4, 65, 0); got != grass {
		t.Errorf("grass clicked from below = %d, want %d (grass)", got, grass)
	}
	if got := srv.InventorySlot("Actor", player.HotbarSlot); got.Damage != 0 {
		t.Errorf("the shovel's wear = %d, want 0", got.Damage)
	}
}

// TestAShovelPutsOutACampfire: a lit campfire goes out, and everyone gets
// the extinguish event (1009).
func TestAShovelPutsOutACampfire(t *testing.T) {
	t.Parallel()
	srv, actor, watcher := servertest.ToolScene(t, "minecraft:iron_shovel")

	const rest = ",signal_fire=false,waterlogged=false"
	srv.SetBlock(3, 64, 0, servertest.BlockStateWith(t, "minecraft:campfire", "facing=north,lit=true"+rest))
	actor.UseBlock(3, 64, 0)
	actor.Settle()

	if got, want := srv.BlockAt(3, 64, 0), servertest.BlockStateWith(t, "minecraft:campfire", "facing=north,lit=false"+rest); got != want {
		t.Errorf("the campfire after the shovel = %d, want %d (out)", got, want)
	}
	if got := servertest.HearTool(t, watcher).Events[1009]; len(got) != 1 || got[0] != [3]int32{3, 64, 0} {
		t.Errorf("extinguish events at %v, want one at (3, 64, 0)", got)
	}
}

// TestAShovelPutsOutACampfireFromBelow: the douse is the campfire's use,
// which no face refuses. vanilla: CampfireBlock.useItemOn
func TestAShovelPutsOutACampfireFromBelow(t *testing.T) {
	t.Parallel()
	srv, actor, _ := servertest.ToolScene(t, "minecraft:iron_shovel")

	const rest = ",signal_fire=false,waterlogged=false"
	srv.SetBlock(3, 65, 0, servertest.BlockStateWith(t, "minecraft:campfire", "facing=north,lit=true"+rest))
	actor.PlaceBlockAt(3, 65, 0, protocol.FaceBottom, 0)
	actor.Settle()

	if got, want := srv.BlockAt(3, 65, 0), servertest.BlockStateWith(t, "minecraft:campfire", "facing=north,lit=false"+rest); got != want {
		t.Errorf("the campfire clicked from below = %d, want %d (out)", got, want)
	}
	if got := srv.InventorySlot("Actor", player.HotbarSlot); got.Damage != 1 {
		t.Errorf("the shovel's wear = %d, want 1", got.Damage)
	}
}

// TestASneakingShovelLeavesACampfireLit: sneaking with an item in hand skips
// block uses, and the douse is one. vanilla: CampfireBlock.useItemOn,
// ServerPlayerGameMode.useItemOn
func TestASneakingShovelLeavesACampfireLit(t *testing.T) {
	t.Parallel()
	srv, actor, _ := servertest.ToolScene(t, "minecraft:iron_shovel")

	lit := servertest.BlockStateWith(t, "minecraft:campfire", "facing=north,lit=true,signal_fire=false,waterlogged=false")
	srv.SetBlock(3, 64, 0, lit)
	actor.Send(protocol.IDPlayPlayerInput, func(b *protocol.Buffer) { b.PutU8(protocol.InputSneak) })
	actor.Settle()
	actor.UseBlock(3, 64, 0)
	actor.Settle()

	if got := srv.BlockAt(3, 64, 0); got != lit {
		t.Errorf("the campfire under a sneaking shovel = %d, want %d (lit)", got, lit)
	}
	if got := srv.InventorySlot("Actor", player.HotbarSlot); got.Damage != 0 {
		t.Errorf("the shovel's wear = %d, want 0", got.Damage)
	}
}

// TestAnAdventureShovelPutsOutACampfire: a block use has no mayBuild gate,
// so an adventure player's shovel douses a campfire.
// vanilla: CampfireBlock.useItemOn
func TestAnAdventureShovelPutsOutACampfire(t *testing.T) {
	t.Parallel()
	srv, actor, watcher := servertest.ToolScene(t, "minecraft:iron_shovel")

	const rest = ",signal_fire=true,waterlogged=false"
	srv.SetBlock(3, 64, 0, servertest.BlockStateWith(t, "minecraft:soul_campfire", "facing=east,lit=true"+rest))
	srv.SetPlayerGameMode("Actor", protocol.GameModeAdventure)
	actor.Settle()
	watcher.Settle()
	actor.UseBlock(3, 64, 0)
	actor.Settle()

	if got, want := srv.BlockAt(3, 64, 0), servertest.BlockStateWith(t, "minecraft:soul_campfire", "facing=east,lit=false"+rest); got != want {
		t.Errorf("the campfire after an adventure shovel = %d, want %d (out)", got, want)
	}
	if got := srv.InventorySlot("Actor", player.HotbarSlot); got.Damage != 1 {
		t.Errorf("the shovel's wear = %d, want 1", got.Damage)
	}
	if got := servertest.HearTool(t, watcher).Events[1009]; len(got) != 1 || got[0] != [3]int32{3, 64, 0} {
		t.Errorf("extinguish events at %v, want one at (3, 64, 0)", got)
	}
}

// TestShearsCarveAPumpkin: the carved face turns to the clicked side, or to
// the player on a top click; four seeds land out of that side and the
// shears take one wear. vanilla: PumpkinBlock.useItemOn
func TestShearsCarveAPumpkin(t *testing.T) {
	t.Parallel()
	srv, actor, watcher := servertest.ToolScene(t, "minecraft:shears")

	pumpkin := registry.BlockStates["minecraft:pumpkin"]
	srv.SetBlock(3, 64, 0, pumpkin)
	actor.PlaceBlockAt(3, 64, 0, protocol.FaceEast, 0.5)
	actor.Settle()
	if got, want := srv.BlockAt(3, 64, 0), servertest.BlockStateWith(t, "minecraft:carved_pumpkin", "facing=east"); got != want {
		t.Errorf("the pumpkin carved on its east face = %d, want %d (facing east)", got, want)
	}
	items := srv.ItemsInDimension(world.DimensionName)
	if len(items) != 1 || items[0].Item != "minecraft:pumpkin_seeds" || items[0].Count != 4 {
		t.Fatalf("the carve dropped %+v, want one stack of 4 pumpkin seeds", items)
	}
	if it := items[0]; it.X != 3+0.5+0.65 || it.Y != 64.1+entity.ItemHalfSize || it.Z != 0.5 {
		t.Errorf("the seeds at (%v, %v, %v), want (4.15, %v, 0.5)", it.X, it.Y, it.Z, 64.1+entity.ItemHalfSize)
	}
	if got := srv.InventorySlot("Actor", player.HotbarSlot); got.Damage != 1 {
		t.Errorf("the shears' wear = %d, want 1", got.Damage)
	}
	if h := servertest.HearTool(t, watcher); h.Sounds["minecraft:block.pumpkin.carve"] != 1 {
		t.Errorf("the watcher heard %v, want one block.pumpkin.carve", h.Sounds)
	}

	// From the top, the face turns to the player: yaw 0 looks south, so the
	// carving faces north.
	srv.SetBlock(5, 64, 0, pumpkin)
	actor.PlaceBlockAt(5, 64, 0, protocol.FaceTop, 1)
	actor.Settle()
	if got, want := srv.BlockAt(5, 64, 0), servertest.BlockStateWith(t, "minecraft:carved_pumpkin", "facing=north"); got != want {
		t.Errorf("the pumpkin carved from the top = %d, want %d (facing north)", got, want)
	}
}

// TestAHoneycombWaxesCopper: the waxed form keeps the state, the others get
// the wax-on event (3003, which carries its own sound), and one honeycomb
// is spent. vanilla: HoneycombItem.useOn
func TestAHoneycombWaxesCopper(t *testing.T) {
	t.Parallel()
	srv, actor, watcher := servertest.ToolScene(t, "minecraft:honeycomb")
	srv.GiveSlot("Actor", player.HotbarSlot, protocol.ItemStack{ItemID: servertest.ItemIDOrDie(t, "minecraft:honeycomb"), Count: 2})

	const props = "facing=south,half=bottom,open=true,powered=false,waterlogged=false"
	srv.SetBlock(3, 64, 0, servertest.BlockStateWith(t, "minecraft:exposed_copper_trapdoor", props))
	actor.Send(protocol.IDPlayPlayerInput, func(b *protocol.Buffer) { b.PutU8(protocol.InputSneak) })
	actor.Settle()
	actor.UseBlock(3, 64, 0)
	actor.Settle()

	if got, want := srv.BlockAt(3, 64, 0), servertest.BlockStateWith(t, "minecraft:waxed_exposed_copper_trapdoor", props); got != want {
		t.Errorf("the trapdoor after the honeycomb = %d, want %d (waxed, same state)", got, want)
	}
	if got := srv.InventorySlot("Actor", player.HotbarSlot); got.Count != 1 {
		t.Errorf("honeycombs left = %d, want 1", got.Count)
	}
	if got := servertest.HearTool(t, watcher).Events[3003]; len(got) != 1 || got[0] != [3]int32{3, 64, 0} {
		t.Errorf("wax-on events at %v, want one at (3, 64, 0)", got)
	}
}

// TestFlintAndSteelLightsACampfire: an unlit campfire lights rather than
// fire starting on top of it, for one wear. vanilla: FlintAndSteelItem.useOn
func TestFlintAndSteelLightsACampfire(t *testing.T) {
	t.Parallel()
	srv, actor, watcher := servertest.ToolScene(t, "minecraft:flint_and_steel")

	const rest = ",signal_fire=false,waterlogged=false"
	srv.SetBlock(3, 64, 0, servertest.BlockStateWith(t, "minecraft:campfire", "facing=north,lit=false"+rest))
	actor.UseBlock(3, 64, 0)
	actor.Settle()

	if got, want := srv.BlockAt(3, 64, 0), servertest.BlockStateWith(t, "minecraft:campfire", "facing=north,lit=true"+rest); got != want {
		t.Errorf("the campfire after the flint and steel = %d, want %d (lit)", got, want)
	}
	if got := srv.BlockAt(3, 65, 0); got != 0 {
		t.Errorf("above the campfire = %d, want air", got)
	}
	if got := srv.InventorySlot("Actor", player.HotbarSlot); got.Damage != 1 {
		t.Errorf("the flint and steel's wear = %d, want 1", got.Damage)
	}
	if h := servertest.HearTool(t, watcher); h.Sounds["minecraft:item.flintandsteel.use"] != 1 {
		t.Errorf("the watcher heard %v, want one item.flintandsteel.use", h.Sounds)
	}
}

// TestFlintAndSteelAcksOnce: however the click is answered — a candle
// lit, fire laid, TNT primed, a refusal — one click is one ack, the
// tail's.
func TestFlintAndSteelAcksOnce(t *testing.T) {
	t.Parallel()
	srv, actor, _ := servertest.ToolScene(t, "minecraft:flint_and_steel")
	servertest.HoldTicks(t, srv)

	// A candle lights rather than the fire going above it.
	srv.SetBlock(3, 64, 0, servertest.BlockStateWith(t, "minecraft:candle", "candles=1,lit=false,waterlogged=false"))
	actor.UseBlock(3, 64, 0)
	actor.SettleKeeping()
	srv.TickSurvival()
	if seq := actor.ExpectBlockAck(); seq != 1 {
		t.Errorf("the candle click's ack = %d, want 1", seq)
	}
	actor.ExpectNoneWithinTicks(srv, 4, servertest.PacketIs(protocol.IDPlayAckBlockChange))
	actor.Settle()

	// Fire laid into the air above the stone.
	srv.SetBlock(5, 64, 0, registry.BlockStates["minecraft:stone"])
	actor.UseBlock(5, 64, 0)
	actor.SettleKeeping()
	srv.TickSurvival()
	if seq := actor.ExpectBlockAck(); seq != 1 {
		t.Errorf("the fire click's ack = %d, want 1", seq)
	}
	actor.ExpectNoneWithinTicks(srv, 4, servertest.PacketIs(protocol.IDPlayAckBlockChange))
	actor.Settle()

	// The priming path: a TNT block primes and the block goes to air.
	srv.SetBlock(4, 64, 0, registry.BlockStates["minecraft:tnt"])
	actor.UseBlock(4, 64, 0)
	actor.SettleKeeping()
	srv.TickSurvival()
	if seq := actor.ExpectBlockAck(); seq != 1 {
		t.Errorf("the TNT click's ack = %d, want 1", seq)
	}
	actor.ExpectNoneWithinTicks(srv, 4, servertest.PacketIs(protocol.IDPlayAckBlockChange))
	if got := srv.BlockAt(4, 64, 0); got != 0 {
		t.Errorf("the primed TNT's block = %d, want air", got)
	}
	actor.Settle()

	// The refusal path: the struck face's cell is stone, so nothing lights.
	srv.SetBlock(4, 64, 0, registry.BlockStates["minecraft:stone"])
	actor.PlaceBlockAt(4, 64, 0, protocol.FaceEast, 0.5)
	actor.SettleKeeping()
	srv.TickSurvival()
	if seq := actor.ExpectBlockAck(); seq != 1 {
		t.Errorf("the refused click's ack = %d, want 1", seq)
	}
	actor.ExpectNoneWithinTicks(srv, 4, servertest.PacketIs(protocol.IDPlayAckBlockChange))
}

// TestRipeBerriesArePicked: a ripe sweet berry bush gives two or three
// berries and drops back to age 1; a berried cave vine gives one glow berry
// and loses its berries. vanilla: SweetBerryBushBlock.useWithoutItem,
// CaveVines.use
func TestRipeBerriesArePicked(t *testing.T) {
	t.Parallel()
	srv, actor, watcher := servertest.ToolScene(t, "minecraft:stick")

	srv.SetBlock(3, 63, 0, registry.BlockStates["minecraft:grass_block"])
	srv.SetBlock(3, 64, 0, servertest.BlockStateWith(t, "minecraft:sweet_berry_bush", "age=3"))
	actor.UseBlock(3, 64, 0)
	actor.Settle()
	if got, want := srv.BlockAt(3, 64, 0), servertest.BlockStateWith(t, "minecraft:sweet_berry_bush", "age=1"); got != want {
		t.Errorf("the picked bush = %d, want %d (age 1)", got, want)
	}
	berries := int32(0)
	for _, it := range srv.ItemsInDimension(world.DimensionName) {
		if it.Item == "minecraft:sweet_berries" {
			berries += it.Count
		}
	}
	if berries < 2 || berries > 3 {
		t.Errorf("the ripe bush gave %d berries, want 2 or 3", berries)
	}
	if h := servertest.HearTool(t, watcher); h.Sounds["minecraft:block.sweet_berry_bush.pick_berries"] != 1 {
		t.Errorf("the watcher heard %v, want one block.sweet_berry_bush.pick_berries", h.Sounds)
	}

	srv.SetBlock(5, 66, 0, registry.BlockStates["minecraft:stone"])
	srv.SetBlock(5, 65, 0, servertest.BlockStateWith(t, "minecraft:cave_vines", "age=0,berries=true"))
	actor.UseBlock(5, 65, 0)
	actor.Settle()
	if got, want := srv.BlockAt(5, 65, 0), servertest.BlockStateWith(t, "minecraft:cave_vines", "age=0,berries=false"); got != want {
		t.Errorf("the picked vine = %d, want %d (no berries)", got, want)
	}
	glow := int32(0)
	for _, it := range srv.ItemsInDimension(world.DimensionName) {
		if it.Item == "minecraft:glow_berries" {
			glow += it.Count
		}
	}
	if glow != 1 {
		t.Errorf("the vine gave %d glow berries, want 1", glow)
	}
}

// toolRolls is what one pick and two carves rolled on a seeded scene: the
// pitch the pick's sound went out at, and the motion each carve threw its
// seeds with.
type toolRolls struct {
	pitch      float32
	velocities [][3]float64
}

// rolledToolWork runs a ripe-berry pick and two pumpkin carves on a scene
// of the test's own seed, reading the pick's pitch and the seeds' motion
// off the watcher's wire.
func rolledToolWork(t *testing.T) toolRolls {
	t.Helper()
	srv, actor, watcher := servertest.ToolScene(t, "minecraft:stick")

	srv.SetBlock(3, 63, 0, registry.BlockStates["minecraft:grass_block"])
	srv.SetBlock(3, 64, 0, servertest.BlockStateWith(t, "minecraft:sweet_berry_bush", "age=3"))
	actor.UseBlock(3, 64, 0)
	actor.Settle()
	var rolls toolRolls
	watcher.SettleAndCollect(func(id int32, data []byte) {
		if id != protocol.IDPlaySoundEffect {
			return
		}
		r := protocol.NewReader(data)
		if sid := r.ReadVarInt(); sid > 0 {
			if name, _ := servertest.SoundEventName(sid - 1); name == "minecraft:block.sweet_berry_bush.pick_berries" {
				r.ReadVarInt() // the block category
				r.ReadI32()
				r.ReadI32()
				r.ReadI32()
				r.ReadF32() // volume
				rolls.pitch = r.ReadF32()
			}
		}
	})
	if rolls.pitch == 0 {
		t.Fatal("no pick sound reached the watcher")
	}

	srv.GiveSlot("Actor", player.HotbarSlot, protocol.ItemStack{ItemID: servertest.ItemIDOrDie(t, "minecraft:shears"), Count: 1})
	actor.Settle()
	for _, x := range []int32{3, 5} {
		srv.SetBlock(x, 64, 0, registry.BlockStates["minecraft:pumpkin"])
		actor.PlaceBlockAt(x, 64, 0, protocol.FaceEast, 0.5)
		actor.Settle()
		watcher.SettleAndCollect(func(id int32, data []byte) {
			if id != protocol.IDPlaySpawnEntity {
				return
			}
			r := protocol.NewReader(data)
			r.ReadVarInt()
			r.ReadUUID()
			if got := r.ReadVarInt(); got != protocol.EntityTypeItem {
				return
			}
			r.ReadF64()
			r.ReadF64()
			r.ReadF64()
			vx, vy, vz := r.ReadLpVec3()
			rolls.velocities = append(rolls.velocities, [3]float64{vx, vy, vz})
		})
	}
	if len(rolls.velocities) != 2 {
		t.Fatalf("the carves threw %d seed stacks past the watcher, want 2", len(rolls.velocities))
	}
	return rolls
}

// TestTheSameSeedPicksAndCarvesTheSame: the pick's pitch and the carve's
// seed throw are rolls of the server's own sources, so two servers grown
// from one seed sound and throw alike, and would not if either came off a
// source outside the seed.
func TestTheSameSeedPicksAndCarvesTheSame(t *testing.T) {
	t.Parallel()
	first := rolledToolWork(t)
	second := rolledToolWork(t)
	if first.pitch != second.pitch {
		t.Errorf("the same seed picked at %v and %v", first.pitch, second.pitch)
	}
	if !reflect.DeepEqual(first.velocities, second.velocities) {
		t.Errorf("the same seed threw its seeds differently:\nfirst  %v\nsecond %v", first.velocities, second.velocities)
	}
}

// TestAFireChargeLightsFireAndLightableBlocks: a charge used on a stone top
// puts fire above it, lights a candle, an unlit campfire and a candle cake
// in place, and each takes one charge and plays the fire charge sound.
// vanilla: FireChargeItem.useOn
func TestAFireChargeLightsFireAndLightableBlocks(t *testing.T) {
	t.Parallel()
	srv, actor, watcher := servertest.ToolScene(t, "minecraft:fire_charge")
	charges := func() int { return int(srv.InventorySlot("Actor", player.HotbarSlot).Count) }
	give := func() {
		srv.GiveSlot("Actor", player.HotbarSlot, protocol.ItemStack{ItemID: servertest.ItemIDOrDie(t, "minecraft:fire_charge"), Count: 3})
	}
	give()

	srv.SetBlock(3, 64, 0, registry.BlockStates["minecraft:stone"])
	actor.PlaceBlock(3, 64, 0, protocol.FaceTop)
	heardByActor := servertest.HearTool(t, actor)
	if name, _, _ := registry.BlockStateName(srv.BlockAt(3, 65, 0)); name != "minecraft:fire" {
		t.Errorf("above the stone = %q, want fire", name)
	}
	if got := charges(); got != 2 {
		t.Errorf("charges after lighting fire = %d, want 2", got)
	}
	if h := servertest.HearTool(t, watcher); h.Sounds["minecraft:item.firecharge.use"] != 1 {
		t.Errorf("the watcher heard %v, want one fire charge use", h.Sounds)
	}
	if heardByActor.Sounds["minecraft:item.firecharge.use"] != 1 {
		t.Errorf("the actor heard %v, want one fire charge use", heardByActor.Sounds)
	}

	for i, tc := range []struct{ block, props string }{
		{"minecraft:candle", "candles=1,lit=%s,waterlogged=false"},
		{"minecraft:campfire", "facing=north,lit=%s,signal_fire=false,waterlogged=false"},
		{"minecraft:candle_cake", "lit=%s"},
	} {
		z := int32(i + 1)
		srv.SetBlock(3, 64, z, servertest.BlockStateWith(t, tc.block, fmt.Sprintf(tc.props, "false")))
		give()
		actor.PlaceBlockAt(3, 64, z, protocol.FaceTop, 0.875)
		actor.Settle()
		if got, want := srv.BlockAt(3, 64, z), servertest.BlockStateWith(t, tc.block, fmt.Sprintf(tc.props, "true")); got != want {
			t.Errorf("%s after the charge = %d, want %d (lit)", tc.block, got, want)
		}
		if got := srv.BlockAt(3, 65, z); got != 0 {
			t.Errorf("above the lit %s = %d, want air", tc.block, got)
		}
		if got := charges(); got != 2 {
			t.Errorf("charges after lighting %s = %d, want 2", tc.block, got)
		}
	}
}

// TestAFireChargeIsKeptWhenNothingLights: a click on a face whose neighbour
// is solid lights nothing and keeps the charge.
func TestAFireChargeIsKeptWhenNothingLights(t *testing.T) {
	t.Parallel()
	srv, actor, _ := servertest.ToolScene(t, "minecraft:fire_charge")
	srv.SetBlock(3, 64, 0, registry.BlockStates["minecraft:stone"])
	srv.SetBlock(3, 65, 0, registry.BlockStates["minecraft:stone"])
	actor.PlaceBlock(3, 64, 0, protocol.FaceTop)
	actor.Settle()
	if got := srv.InventorySlot("Actor", player.HotbarSlot).Count; got != 1 {
		t.Errorf("charges = %d, want 1 (kept)", got)
	}
}

// TestAFireToolLightsAPortalFrameWithItsSound: a fire charge or a flint and
// steel used on the floor of a complete obsidian frame fills it with portal
// blocks and plays the tool's sound to the watcher; the fire charge's user
// hears it too, the flint and steel's does not.
// vanilla: FireChargeItem.useOn, FlintAndSteelItem.useOn
func TestAFireToolLightsAPortalFrameWithItsSound(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		tool, sound string
		actorHears  int
	}{
		{"minecraft:fire_charge", "minecraft:item.firecharge.use", 1},
		{"minecraft:flint_and_steel", "minecraft:item.flintandsteel.use", 0},
	} {
		t.Run(tc.tool, func(t *testing.T) {
			t.Parallel()
			srv, actor, watcher := servertest.ToolScene(t, tc.tool)
			for x := int32(2); x <= 5; x++ {
				srv.SetBlock(x, 65, 0, rules.ObsidianState)
				srv.SetBlock(x, 69, 0, rules.ObsidianState)
			}
			for y := int32(66); y <= 68; y++ {
				srv.SetBlock(2, y, 0, rules.ObsidianState)
				srv.SetBlock(5, y, 0, rules.ObsidianState)
			}
			actor.PlaceBlock(3, 65, 0, protocol.FaceTop)
			heardByActor := servertest.HearTool(t, actor)
			if got := srv.BlockAt(3, 67, 0); got != rules.NetherPortalX {
				name, _, _ := registry.BlockStateName(got)
				t.Errorf("the frame interior = %s, want the lit portal", name)
			}
			if h := servertest.HearTool(t, watcher); h.Sounds[tc.sound] != 1 {
				t.Errorf("the watcher heard %v, want one %s", h.Sounds, tc.sound)
			}
			if heardByActor.Sounds[tc.sound] != tc.actorHears {
				t.Errorf("the actor heard %v, want %d of %s", heardByActor.Sounds, tc.actorHears, tc.sound)
			}
		})
	}
}

// TestShearsAndBottlesHarvestAFullHive: shears take three honeycomb and one
// wear, a glass bottle becomes a honey bottle, both empty the hive, and a
// hive below level 5 is left alone. vanilla: BeehiveBlock.useItemOn
func TestShearsAndBottlesHarvestAFullHive(t *testing.T) {
	t.Parallel()
	srv, actor, _ := servertest.ToolScene(t, "minecraft:shears")
	hive := func(name, level string) uint16 {
		return servertest.BlockStateWith(t, name, "facing=north,honey_level="+level)
	}

	srv.SetBlock(3, 64, 0, hive("minecraft:beehive", "4"))
	actor.UseBlock(3, 64, 0)
	actor.Settle()
	if got := srv.BlockAt(3, 64, 0); got != hive("minecraft:beehive", "4") {
		t.Errorf("a level 4 hive after shears = %d, want it unchanged", got)
	}
	if items := srv.ItemsInDimension(world.DimensionName); len(items) != 0 {
		t.Errorf("a level 4 hive dropped %+v, want nothing", items)
	}

	srv.SetBlock(3, 64, 0, hive("minecraft:bee_nest", "5"))
	actor.UseBlock(3, 64, 0)
	actor.Settle()
	if got := srv.BlockAt(3, 64, 0); got != hive("minecraft:bee_nest", "0") {
		t.Errorf("a full nest after shears = %d, want level 0", got)
	}
	items := srv.ItemsInDimension(world.DimensionName)
	if len(items) != 1 || items[0].Item != "minecraft:honeycomb" || items[0].Count != 3 {
		t.Fatalf("the shearing dropped %+v, want 3 honeycomb", items)
	}
	if got := srv.InventorySlot("Actor", player.HotbarSlot); got.Damage != 1 {
		t.Errorf("the shears' wear = %d, want 1", got.Damage)
	}

	srv.SetBlock(5, 64, 0, hive("minecraft:beehive", "5"))
	srv.GiveSlot("Actor", player.HotbarSlot, servertest.ItemStack(t, "minecraft:glass_bottle", 1))
	actor.UseBlock(5, 64, 0)
	actor.Settle()
	if got := srv.BlockAt(5, 64, 0); got != hive("minecraft:beehive", "0") {
		t.Errorf("a full hive after a bottle = %d, want level 0", got)
	}
	if got := srv.InventorySlot("Actor", player.HotbarSlot); got.ItemID != servertest.ItemIDOrDie(t, "minecraft:honey_bottle") {
		t.Errorf("the hand after a bottle = %+v, want a honey bottle", got)
	}
}
