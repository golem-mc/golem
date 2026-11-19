package containers

import (
	"path/filepath"

	"github.com/golem-mc/golem/internal/config"
	"github.com/golem-mc/golem/internal/server/servertest"
	"github.com/golem-mc/golem/nbt"
	"slices"
	"strconv"
	"testing"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server"
	"github.com/golem-mc/golem/internal/server/player"
	"github.com/golem-mc/golem/registry"
)

// crafterState returns the block state ID a crafter would be in with the given
// orientation and triggered flag, crafting at its default.
func crafterState(t *testing.T, orientation string, triggered bool) uint16 {
	t.Helper()
	s, ok := registry.FindState("minecraft:crafter", map[string]string{
		"orientation": orientation,
		"triggered":   strconv.FormatBool(triggered),
	})
	if !ok {
		t.Fatalf("FindState crafter orientation=%s triggered=%v: not found", orientation, triggered)
	}
	return s
}

// TestCrafterPlacementUsesTheLook checks CrafterBlock.getStateForPlacement:
// ORIENTATION pairs the front — the nearest looking direction reversed — with
// a top that is up for a horizontal front and follows the horizontal look
// when the front is vertical. A crafter placed looking steeply down while
// facing north is up_north; looking west across the ground is east_up.
func TestCrafterPlacementUsesTheLook(t *testing.T) {
	t.Parallel()
	srv, actor := servertest.AttachActor(t)
	actor.MoveToGround(4.5, 64, 4.5, true)
	actor.Settle()

	crafter, _ := registry.ItemID("minecraft:crafter")
	actor.SetCreativeSlot(player.HotbarSlot, protocol.ItemStack{ItemID: crafter, Count: 1})
	actor.Settle()

	// Facing north (yaw 180) and looking steeply down: the front is up and
	// the top is the horizontal look, north.
	actor.FacePlayer(4.5, 64, 4.5, 180, 60)
	actor.PlaceBlock(5, 63, 5, protocol.FaceTop)
	actor.Settle()
	if got := srv.BlockAt(5, 64, 5); got != crafterState(t, "up_north", false) {
		t.Errorf("crafter looking down facing north = %d, want %d (up_north)", got, crafterState(t, "up_north", false))
	}

	// Looking west horizontally: the front is east and the top is up.
	actor.FacePlayer(4.5, 64, 4.5, 90, 0)
	actor.PlaceBlock(6, 63, 5, protocol.FaceTop)
	actor.Settle()
	if got := srv.BlockAt(6, 64, 5); got != crafterState(t, "east_up", false) {
		t.Errorf("crafter looking west = %d, want %d (east_up)", got, crafterState(t, "east_up", false))
	}
}

// TestCrafterPlacementReadsThePower checks the triggered half of
// CrafterBlock.getStateForPlacement: a crafter placed beside a powered
// redstone block arrives with its triggered property set.
func TestCrafterPlacementReadsThePower(t *testing.T) {
	t.Parallel()
	srv, actor := servertest.AttachActor(t)
	actor.MoveToGround(4.5, 64, 6.5, true)
	actor.Settle()

	// A redstone block beside the cell the crafter takes, so the placement
	// reads a signal at its own cell.
	servertest.Setblock(t, actor, 6, 64, 7, "redstone_block")
	actor.Settle()

	crafter, _ := registry.ItemID("minecraft:crafter")
	actor.SetCreativeSlot(player.HotbarSlot, protocol.ItemStack{ItemID: crafter, Count: 1})
	actor.Settle()

	actor.FacePlayer(4.5, 64, 6.5, 0, 0)
	actor.PlaceBlock(5, 63, 7, protocol.FaceTop)
	actor.Settle()

	if got := srv.BlockAt(5, 64, 7); got != crafterState(t, "north_up", true) {
		t.Errorf("crafter beside a powered redstone block = %d, want %d (triggered)", got, crafterState(t, "north_up", true))
	}
}

// TestCrafterPlacementWithoutPower checks the unpowered half of
// CrafterBlock.getStateForPlacement's power read: a crafter a player sets
// down beside an unpowered wire arrives untriggered.
func TestCrafterPlacementWithoutPower(t *testing.T) {
	t.Parallel()
	srv, actor := servertest.AttachActor(t)
	actor.MoveToGround(4.5, 64, 6.5, true)
	actor.Settle()

	// A wire beside the cell the crafter takes carries no signal while it
	// has no source.
	servertest.Setblock(t, actor, 6, 64, 7, "redstone_wire")
	actor.Settle()

	crafter, _ := registry.ItemID("minecraft:crafter")
	actor.SetCreativeSlot(player.HotbarSlot, protocol.ItemStack{ItemID: crafter, Count: 1})
	actor.Settle()

	actor.FacePlayer(4.5, 64, 6.5, 0, 0)
	actor.PlaceBlock(5, 63, 7, protocol.FaceTop)
	actor.Settle()

	if got := srv.BlockAt(5, 64, 7); got != crafterState(t, "north_up", false) {
		t.Errorf("crafter beside an unpowered wire = %d, want %d (untriggered)", got, crafterState(t, "north_up", false))
	}
}

// TestSetblockCrafterStaysUntriggered checks that the power read belongs to
// the placement path, not the engine's every-change wake: /setblock puts a
// crafter beside a powered redstone block and vanilla leaves it untriggered.
func TestSetblockCrafterStaysUntriggered(t *testing.T) {
	t.Parallel()
	srv, actor := servertest.AttachActor(t)
	actor.MoveToGround(4.5, 64, 6.5, true)
	actor.Settle()

	servertest.Setblock(t, actor, 6, 64, 7, "redstone_block")
	actor.Settle()

	servertest.Setblock(t, actor, 5, 64, 7, "crafter")
	actor.Settle()

	if got := srv.BlockAt(5, 64, 7); got != crafterState(t, "north_up", false) {
		t.Errorf("/setblock crafter beside power = %d, want %d (untriggered)", got, crafterState(t, "north_up", false))
	}
}

// crafterFacingSouth stands a crafter at (4, 64, -1) facing south with the
// engine's main loop stopped, a chest at (4, 64, 0) when withChest is set.
func crafterFacingSouth(t *testing.T, withChest bool) (*server.Server, *servertest.WindowedClient) {
	t.Helper()
	return crafterFacingSouthIn(t, withChest, servertest.BlockTestConfig())
}

// crafterFacingSouthIn is crafterFacingSouth in a world built from cfg.
func crafterFacingSouthIn(t *testing.T, withChest bool, cfg *config.Config) (*server.Server, *servertest.WindowedClient) {
	t.Helper()
	srv, w := servertest.OpenWindowedOps(t, cfg)
	w.FacePlayer(4.5, 64, 1.5, 180, 0)
	if withChest {
		w.SetCreativeSlot(36, servertest.ItemStack(t, "minecraft:chest", 1))
		w.PlaceBlock(4, 63, 0, protocol.FaceTop)
		w.ExpectBlockAck()
	}
	w.SetCreativeSlot(36, servertest.ItemStack(t, "minecraft:crafter", 1))
	if withChest {
		w.Sneak(true)
		w.PlaceBlock(4, 64, 0, protocol.FaceNorth)
		w.Sneak(false)
	} else {
		w.PlaceBlock(4, 63, -1, protocol.FaceTop)
	}
	w.SettleKeeping()
	srv.TickSurvival()
	w.ExpectBlockAck()
	if name, props := servertest.StatePropsOf(t, srv, 4, 64, -1); name != "minecraft:crafter" || props["orientation"] != "south_up" {
		t.Fatalf("the crafter stands as %s %v, want one facing south", name, props)
	}
	srv.StopTick()
	return srv, w
}

// powerCrafter puts a redstone block on the crafter's roof and runs the
// physics through the visit it books.
func powerCrafter(t *testing.T, srv *server.Server, w *servertest.WindowedClient) {
	w.SetCreativeSlot(36, servertest.ItemStack(t, "minecraft:redstone_block", 1))
	w.Sneak(true)
	w.PlaceBlock(4, 64, -1, protocol.FaceTop)
	w.Sneak(false)
	w.SettleKeeping()
	srv.TickSurvival()
	w.ExpectBlockAck()
	srv.TickPhysics(8)
}

// TestAPoweredCrafterCraftsIntoTheContainerInFront checks the rise's craft:
// the grid's recipe result goes into the chest the face opens onto, one
// item of every filled cell is spent, and the block shows crafting for six
// ticks. A craft that found room plays neither the sound nor the smoke.
func TestAPoweredCrafterCraftsIntoTheContainerInFront(t *testing.T) {
	t.Parallel()
	srv, w := crafterFacingSouth(t, true)
	log := servertest.ItemStack(t, "minecraft:oak_log", 2)
	if !srv.SetInventoryAt(4, 64, -1, 0, log) {
		t.Fatal("the crafter has no block entity to fill")
	}
	powerCrafter(t, srv, w)

	planks, _ := registry.ItemID("minecraft:oak_planks")
	if got := srv.InventoryAt(4, 64, 0); servertest.NothingIn(got) || got[0].ItemID != planks || got[0].Count != 4 {
		t.Errorf("the chest holds %+v, want four oak planks", got)
	}
	if got := srv.InventoryAt(4, 64, -1); got[0].Count != 1 {
		t.Errorf("the crafter's cell holds %+v, want one log spent", got[0])
	}
	if _, props := servertest.StatePropsOf(t, srv, 4, 64, -1); props["crafting"] != "true" {
		t.Errorf("the crafter shows crafting=%s right after a craft, want true", props["crafting"])
	}
	servertest.TickBlockEntitiesByHand(srv, 6)
	if _, props := servertest.StatePropsOf(t, srv, 4, 64, -1); props["crafting"] != "false" {
		t.Errorf("the crafter shows crafting=%s after six ticks, want false", props["crafting"])
	}
}

// TestAPoweredCrafterEjectsTheRemaindersAfterTheResult checks that the
// ingredients' crafting remainders leave the crafter too: four honey bottles
// make a honey block and leave four glass bottles.
func TestAPoweredCrafterEjectsTheRemaindersAfterTheResult(t *testing.T) {
	t.Parallel()
	srv, w := crafterFacingSouth(t, true)
	for _, slot := range []int{0, 1, 3, 4} {
		srv.SetInventoryAt(4, 64, -1, slot, servertest.ItemStack(t, "minecraft:honey_bottle", 1))
	}
	powerCrafter(t, srv, w)

	honey, _ := registry.ItemID("minecraft:honey_block")
	bottle, _ := registry.ItemID("minecraft:glass_bottle")
	var gotHoney, gotBottles int32
	for _, s := range srv.InventoryAt(4, 64, 0) {
		switch s.ItemID {
		case honey:
			gotHoney += s.Count
		case bottle:
			gotBottles += s.Count
		}
	}
	if gotHoney != 1 || gotBottles != 4 {
		t.Errorf("the chest holds %d honey blocks and %d glass bottles, want 1 and 4", gotHoney, gotBottles)
	}
	if got := srv.InventoryAt(4, 64, -1); !servertest.NothingIn(got) {
		t.Errorf("the crafter still holds %+v, want every honey bottle spent", got)
	}
}

// TestACrafterCloningABookEjectsTheOriginalToo checks the book clone's
// remainder: the copy leaves first, then the original written book.
// vanilla: BookCloningRecipe.getRemainingItems
func TestACrafterCloningABookEjectsTheOriginalToo(t *testing.T) {
	t.Parallel()
	srv, w := crafterFacingSouth(t, true)
	original := servertest.ItemStack(t, "minecraft:written_book", 1)
	if !original.SetWrittenBook(protocol.WrittenBook{Title: "Tale", Author: "Anon",
		Pages: []protocol.WrittenPage{{Raw: protocol.Text{Text: "one"}}}}) {
		t.Fatal("SetWrittenBook refused the book")
	}
	srv.SetInventoryAt(4, 64, -1, 0, original)
	srv.SetInventoryAt(4, 64, -1, 1, servertest.ItemStack(t, "minecraft:writable_book", 1))
	powerCrafter(t, srv, w)

	written, _ := registry.ItemID("minecraft:written_book")
	var generations []int32
	for _, s := range srv.InventoryAt(4, 64, 0) {
		if s.Empty() {
			continue
		}
		book, ok := s.WrittenBookOf()
		if s.ItemID != written || !ok {
			t.Errorf("the chest holds %+v, want only written books", s)
			continue
		}
		for range s.Count {
			generations = append(generations, book.Generation)
		}
	}
	slices.Sort(generations)
	if !slices.Equal(generations, []int32{0, 1}) {
		t.Errorf("the chest holds written books of generations %v, want the original 0 and the copy 1", generations)
	}
	if got := srv.InventoryAt(4, 64, -1); !servertest.NothingIn(got) {
		t.Errorf("the crafter still holds %+v, want both books spent", got)
	}
}

// TestACrafterCopyingABannerEjectsThePatternedBanner checks the banner
// duplicate's remainder: the patterned source leaves after its copy.
// vanilla: BannerDuplicateRecipe.getRemainingItems
func TestACrafterCopyingABannerEjectsThePatternedBanner(t *testing.T) {
	t.Parallel()
	srv, w := crafterFacingSouth(t, true)
	patterns, ok := registry.Get("minecraft:banner_pattern")
	if !ok {
		t.Fatal("the registry has no banner_pattern")
	}
	stripe := patterns.ID("minecraft:stripe_top")
	if stripe < 0 {
		t.Fatal("the banner_pattern registry has no stripe_top")
	}
	patterned := servertest.ItemStack(t, "minecraft:white_banner", 1)
	if !patterned.SetBannerPatterns([]protocol.BannerPatternLayer{{Pattern: stripe, Dye: 14}}) {
		t.Fatal("SetBannerPatterns refused the layer")
	}
	srv.SetInventoryAt(4, 64, -1, 0, patterned)
	srv.SetInventoryAt(4, 64, -1, 1, servertest.ItemStack(t, "minecraft:white_banner", 1))
	powerCrafter(t, srv, w)

	var gotPatterned, gotBlank int32
	for _, s := range srv.InventoryAt(4, 64, 0) {
		if s.Empty() {
			continue
		}
		if layers, _ := s.BannerPatternsOf(); len(layers) == 1 {
			gotPatterned += s.Count
		} else {
			gotBlank += s.Count
		}
	}
	if gotPatterned != 2 || gotBlank != 0 {
		t.Errorf("the chest holds %d patterned and %d other banners, want 2 and 0", gotPatterned, gotBlank)
	}
	if got := srv.InventoryAt(4, 64, -1); !servertest.NothingIn(got) {
		t.Errorf("the crafter still holds %+v, want both banners spent", got)
	}
}

// TestAPoweredCrafterDropsTheResultWithNoContainerInFront checks the drop:
// the result is born out of the face as one item entity, with the craft
// sound and the smoke, which are the crafter's events and not the
// dispenser's.
func TestAPoweredCrafterDropsTheResultWithNoContainerInFront(t *testing.T) {
	t.Parallel()
	srv, w := crafterFacingSouth(t, false)
	srv.SetInventoryAt(4, 64, -1, 0, servertest.ItemStack(t, "minecraft:oak_log", 1))
	w.DrainRecorded()
	w.Events, w.Spills = nil, nil
	powerCrafter(t, srv, w)
	w.DrainRecorded()

	if len(w.Spills) != 1 {
		t.Fatalf("the crafter dropped %d items, want one", len(w.Spills))
	}
	for _, event := range []int32{protocol.WorldEventCrafterCraft, protocol.WorldEventCrafterAnim} {
		if !slices.Contains(w.Events, event) {
			t.Errorf("no world event %d played; events were %v", event, w.Events)
		}
	}
	if got := srv.InventoryAt(4, 64, -1); !servertest.NothingIn(got) {
		t.Errorf("the crafter still holds %+v, want its log spent", got)
	}
}

// TestAPoweredCrafterWithoutARecipePlaysTheFailEvent checks the failed
// craft: nothing in the grid matches, so nothing is spent and the crafter
// clicks with its own event, 1050.
func TestAPoweredCrafterWithoutARecipePlaysTheFailEvent(t *testing.T) {
	t.Parallel()
	srv, w := crafterFacingSouth(t, true)
	srv.SetInventoryAt(4, 64, -1, 0, servertest.ItemStack(t, "minecraft:dirt", 1))
	w.DrainRecorded()
	w.Events = nil
	powerCrafter(t, srv, w)
	w.DrainRecorded()

	if !slices.Contains(w.Events, protocol.WorldEventCrafterFail) {
		t.Errorf("no fail event played; events were %v", w.Events)
	}
	if got := srv.InventoryAt(4, 64, -1); got[0].Count != 1 {
		t.Errorf("the crafter's cell holds %+v, want the dirt kept", got[0])
	}
	if got := srv.InventoryAt(4, 64, 0); !servertest.NothingIn(got) {
		t.Errorf("the chest holds %+v, want nothing", got)
	}
}

// TestAPoweredCrafterPoursOneItemIntoAOneItemContainer checks that a product
// larger than the target's slot limit goes in an item at a time and the rest
// drops: a hay block makes nine wheat, the composter under a down-facing
// crafter takes one and eight fall out of the face.
func TestAPoweredCrafterPoursOneItemIntoAOneItemContainer(t *testing.T) {
	t.Parallel()
	srv, w := crafterFacingSouth(t, false)
	srv.SetBlock(4, 64, -1, crafterState(t, "down_north", false))
	srv.SetBlock(4, 63, -1, servertest.BlockStateWith(t, "minecraft:composter", "level=0"))
	srv.SetInventoryAt(4, 64, -1, 0, servertest.ItemStack(t, "minecraft:hay_block", 1))
	w.DrainRecorded()
	w.Events, w.Spills = nil, nil
	powerCrafter(t, srv, w)
	w.DrainRecorded()

	var dropped int32
	for _, spill := range w.Spills {
		dropped += spill.Stack.Count
	}
	if len(w.Spills) == 0 || dropped != 8 {
		t.Errorf("the crafter dropped %d wheat in %d stacks, want 8 beside the one the composter took", dropped, len(w.Spills))
	}
	if got := srv.InventoryAt(4, 64, -1); !servertest.NothingIn(got) {
		t.Errorf("the crafter still holds %+v, want its hay block spent", got)
	}
}

// TestACrafterExtendsAMapAndRefusesAFullyZoomedOne checks that a crafter
// resolves the scale mark: a map ringed by paper comes out under a fresh id
// without the mark, and a scale-4 map is refused with nothing spent.
// vanilla: MapExtendingRecipe.matches, CrafterBlock.dispenseFrom
func TestACrafterExtendsAMapAndRefusesAFullyZoomedOne(t *testing.T) {
	t.Parallel()
	for _, scale := range []int8{0, 4} {
		cfg := servertest.BlockTestConfig()
		cfg.LevelName = t.TempDir()
		servertest.WritePlayerFile(t, filepath.Join(cfg.LevelName, "data", "minecraft", "maps", "5.dat"), nbt.Compound{
			"DataVersion": nbt.Int(4903),
			"data": nbt.Compound{
				"dimension": nbt.String("minecraft:overworld"),
				"xCenter":   nbt.Int(64),
				"zCenter":   nbt.Int(64),
				"scale":     nbt.Byte(scale),
				"colors":    nbt.ByteArray(make([]byte, 128*128)),
				"frames":    nbt.List{},
			},
		})
		srv, w := crafterFacingSouthIn(t, true, cfg)
		m := servertest.ItemStack(t, "minecraft:filled_map", 1)
		m.SetMapID(5)
		for slot := range 9 {
			if slot == 4 {
				srv.SetInventoryAt(4, 64, -1, slot, m)
			} else {
				srv.SetInventoryAt(4, 64, -1, slot, servertest.ItemStack(t, "minecraft:paper", 1))
			}
		}
		powerCrafter(t, srv, w)

		got := srv.InventoryAt(4, 64, 0)
		if scale == 4 {
			if !servertest.NothingIn(got) {
				t.Errorf("scale 4: the chest holds %+v, want nothing", got)
			}
			if kept := srv.InventoryAt(4, 64, -1); kept[0].Count != 1 || kept[4].Count != 1 {
				t.Errorf("scale 4: the crafter holds %+v, want its nine stacks", kept)
			}
			continue
		}
		id, ok := got[0].MapIDOf()
		if _, marked := got[0].MapPostProcessingOf(); got[0].Empty() || !ok || id == 5 || marked {
			t.Errorf("scale 0: the chest holds %+v (map id %d, marked %t), want a map under a fresh id, unmarked", got[0], id, marked)
		}
	}
}

// TestAPoweredCrafterWindowShowsTheTriggeredProperty powers a crafter while
// its window is open and checks the next tick tells the window it is
// triggered; reopened while still powered, the window opens triggered.
// vanilla: CrafterBlock.setBlockEntityTriggered, AbstractContainerMenu.broadcastChanges
func TestAPoweredCrafterWindowShowsTheTriggeredProperty(t *testing.T) {
	t.Parallel()
	srv, w := crafterFacingSouth(t, false)
	w.UseBlock(4, 64, -1)
	w.ExpectContainerOpenType(protocol.MenuCrafter, protocol.BlockEntityCrafter, 4, 64, -1)
	w.AwaitWindowProperty(protocol.CrafterPropertyTriggered, 0)

	w.ChatCommand("setblock 4 65 -1 minecraft:redstone_block")
	w.SettleKeeping()
	srv.TickPhysics(8)
	srv.TickOpenWindow("Testificate")
	w.AwaitWindowProperty(protocol.CrafterPropertyTriggered, 1)

	w.CloseWindow()
	w.UseBlock(4, 64, -1)
	w.ExpectContainerOpenType(protocol.MenuCrafter, protocol.BlockEntityCrafter, 4, 64, -1)
	w.AwaitWindowProperty(protocol.CrafterPropertyTriggered, 1)
}

// TestACrafterSlotToggleReachesTheOtherViewer has two players open one
// crafter; a slot one of them disables is shown disabled to the other.
// vanilla: CrafterMenu's shared containerData, AbstractContainerMenu.broadcastChanges
func TestACrafterSlotToggleReachesTheOtherViewer(t *testing.T) {
	t.Parallel()
	srv, connect := servertest.Start(t, servertest.BlockTestConfig())
	first := servertest.WindowedOf(t, "Testificate", connect())
	other := servertest.WindowedOf(t, "Other", connect())
	openCrafter(t, first)
	other.UseBlock(4, 64, 0)
	other.ExpectContainerOpenType(protocol.MenuCrafter, protocol.BlockEntityCrafter, 4, 64, 0)
	other.AwaitWindowProperty(protocol.CrafterPropertyTriggered, 0)

	toggleCrafterSlot(first, 0, false)
	first.Settle()
	srv.TickOpenWindow("Other")
	other.AwaitWindowProperty(protocol.CrafterPropertySlots, 1)
}
