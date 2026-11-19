package growth

import (
	"github.com/golem-mc/golem/internal/server/servertest"
	"testing"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/world"
)

// The weathering copper's oxidation, black-box: the bare copper block
// climbing a stage under the pass's dice until the first change the test
// catches, the waxed twin holding through every pass, and the state
// properties — a slab's half, a door's halves and hinge, a chest's facing
// — arriving at the next stage whole. The dice are pinned and the tick
// rule raised the way the farm tests pin and raise theirs; one stage is
// the only change a landed die can write, so the caught change names the
// stage the block stopped on.

// TestCopperOxidizesOnRandomTicks checks the family's advance: a copper
// block's first stage under the random ticks is exposed_copper, and the
// waxed twin two squares off stays waxed through the same passes — wax
// is not one of the stages a neighbour's weathering sees, so the bare
// block beside it advances as if alone.
func TestCopperOxidizesOnRandomTicks(t *testing.T) {
	t.Parallel()
	srv, c := servertest.Join(t, servertest.BlockTestConfig(), "Camper", servertest.OpsListing(t, "Camper"))

	srv.StopTick()
	servertest.SeedPhysics(t, srv, servertest.CropsSeed)
	srv.SetGameRuleValue(world.RuleRandomTickSpeed.String(), servertest.CropsTickSpeed)

	servertest.SetFarmBlock(t, c, srv, 5, 64, 0, "minecraft:copper_block", nil)
	servertest.SetFarmBlock(t, c, srv, 7, 64, 0, "minecraft:waxed_copper_block", nil)

	var weathered string
	for range 200 {
		srv.TickRandomTicks(100)
		if w := servertest.CropsNameOf(t, srv.BlockAt(7, 64, 0)); w != "minecraft:waxed_copper_block" {
			t.Fatalf("the waxed twin weathered to %s, want it staying waxed", w)
		}
		if name := servertest.CropsNameOf(t, srv.BlockAt(5, 64, 0)); name != "minecraft:copper_block" {
			weathered = name
			break
		}
	}
	if weathered == "" {
		t.Fatal("20000 random tick passes never weathered the copper block")
	}
	if weathered != "minecraft:exposed_copper" {
		t.Errorf("the copper block's first stage was %s, want minecraft:exposed_copper", weathered)
	}
}

// TestCopperOxidationKeepsStateProperties checks the change carries every
// property: a top slab, a west-facing door in both halves and a single
// west-facing chest each reach their exposed stage without losing one.
// The state each cell is caught in is the one its properties are read
// from, taken at the first change so a further stage the tests drive
// while the others ripen answers nothing here.
func TestCopperOxidationKeepsStateProperties(t *testing.T) {
	t.Parallel()
	srv, c := servertest.Join(t, servertest.BlockTestConfig(), "Camper", servertest.OpsListing(t, "Camper"))

	srv.StopTick()
	servertest.SeedPhysics(t, srv, servertest.CropsSeed)
	// The door's lower half carries the pair alone — the upper takes no die
	// of its own — so the dice run past the farm tests' fifty to land enough
	// of them on the one door the pass budget has room for.
	srv.SetGameRuleValue(world.RuleRandomTickSpeed.String(), 8*servertest.CropsTickSpeed)

	// The three stand six apart, past the weathering scan's reach, so
	// each ages on its own dice alone.
	cells := []struct {
		x, y, z     int32
		planted     string
		plantState  uint16
		exposedName string
	}{
		{5, 64, 1, "cut_copper_slab", 0, "minecraft:exposed_cut_copper_slab"},
		{5, 64, 7, "the lower copper door half", 0, "minecraft:exposed_copper_door"},
		{5, 65, 7, "the upper copper door half", 0, "minecraft:exposed_copper_door"},
		{5, 64, 13, "copper_chest", 0, "minecraft:exposed_copper_chest"},
	}
	cells[0].plantState = servertest.SetFarmBlock(t, c, srv, 5, 64, 1, "minecraft:cut_copper_slab", map[string]string{"type": "top"})
	cells[1].plantState = servertest.SetFarmBlock(t, c, srv, 5, 64, 7, "minecraft:copper_door",
		map[string]string{"facing": "west", "half": "lower", "hinge": "right", "open": "true", "powered": "false"})
	cells[2].plantState = servertest.SetFarmBlock(t, c, srv, 5, 65, 7, "minecraft:copper_door",
		map[string]string{"facing": "west", "half": "upper", "hinge": "right", "open": "true", "powered": "false"})
	cells[3].plantState = servertest.SetFarmBlock(t, c, srv, 5, 64, 13, "minecraft:copper_chest", map[string]string{"facing": "west"})

	// One pass a look until every watched cell has changed stage, so the
	// state read is the first change the cell took — the one its
	// properties arrive with.
	weathered := make(map[[3]int32]uint16, len(cells))
	for range 20000 {
		if len(weathered) == len(cells) {
			break
		}
		srv.TickRandomTicks(1)
		for _, cell := range cells {
			key := [3]int32{cell.x, cell.y, cell.z}
			if _, done := weathered[key]; done {
				continue
			}
			if state := srv.BlockAt(cell.x, cell.y, cell.z); state != cell.plantState {
				weathered[key] = state
			}
		}
	}
	for _, cell := range cells {
		state, ok := weathered[[3]int32{cell.x, cell.y, cell.z}]
		if !ok {
			t.Fatalf("the %s at (%d, %d, %d) never weathered through 20000 passes",
				cell.planted, cell.x, cell.y, cell.z)
		}
		props := servertest.CropsProps(t, state, cell.exposedName)
		switch cell.planted {
		case "cut_copper_slab":
			if props["type"] != "top" {
				t.Errorf("the oxidised slab stands type=%s, want its top kept", props["type"])
			}
		case "copper_chest":
			if props["facing"] != "west" || props["type"] != "single" {
				t.Errorf("the oxidised chest stands %v, want it kept facing west and single", props)
			}
		default:
			wantHalf := "lower"
			if cell.y == 65 {
				wantHalf = "upper"
			}
			if props["facing"] != "west" || props["half"] != wantHalf || props["hinge"] != "right" || props["open"] != "true" {
				t.Errorf("the oxidised %s door half stands %v, want west, right hinge and open kept",
					wantHalf, props)
			}
		}
	}
}

// TestAnOpenCopperChestHoldsItsStage checks the server's side of the opener
// gate: a copper chest a player is looking into holds its stage under the
// random ticks, and ages once the window is closed.
// vanilla: WeatheringCopperChestBlock.randomTick
func TestAnOpenCopperChestHoldsItsStage(t *testing.T) {
	t.Parallel()
	srv, c := servertest.Dial(t, servertest.BlockTestConfig(), servertest.OpsListing(t, "Camper"))
	w := servertest.WindowedOf(t, "Camper", c)

	srv.StopTick()
	servertest.SeedPhysics(t, srv, servertest.CropsSeed)
	srv.SetGameRuleValue(world.RuleRandomTickSpeed.String(), 8*servertest.CropsTickSpeed)

	servertest.SetFarmBlock(t, c, srv, 5, 64, 0, "minecraft:copper_chest", map[string]string{"facing": "west"})
	w.UseBlock(5, 64, 0)
	w.ExpectContainerOpen(protocol.MenuGeneric9x3, true, 5, 64, 0)

	// The watched chest holds its stage however many dice land on it; the
	// count is enough that the same passes weather it once unwatched.
	for range 2000 {
		srv.TickRandomTicks(1)
		if name := servertest.CropsNameOf(t, srv.BlockAt(5, 64, 0)); name != "minecraft:copper_chest" {
			t.Fatalf("the open chest weathered to %s, want it holding", name)
		}
	}

	w.CloseWindow()
	w.ExpectWindowZero()

	for range 20000 {
		srv.TickRandomTicks(1)
		if name := servertest.CropsNameOf(t, srv.BlockAt(5, 64, 0)); name != "minecraft:copper_chest" {
			return
		}
	}
	t.Fatal("the closed chest never weathered through 20000 passes")
}
