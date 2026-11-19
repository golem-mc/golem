package redstone

import (
	"fmt"
	"testing"

	"github.com/golem-mc/golem/internal/server/servertest"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server"
	"github.com/golem-mc/golem/registry"
)

// plateX is where the plate tests put their plate: over eight blocks from
// the player at spawn, out of an orb's follow and an item's pickup.
const plateX, plateY, plateZ = 12, 64, 0

// plateItems are distinct items, so no two of them merge on the plate.
var plateItems = []string{
	"minecraft:stone", "minecraft:dirt", "minecraft:cobblestone", "minecraft:oak_planks",
	"minecraft:sand", "minecraft:gravel", "minecraft:glass", "minecraft:oak_log",
	"minecraft:birch_log", "minecraft:bricks", "minecraft:white_wool", "minecraft:orange_wool",
	"minecraft:magenta_wool", "minecraft:light_blue_wool", "minecraft:yellow_wool", "minecraft:lime_wool",
}

// plateServer starts a server with one player at spawn and the named plate
// at plateX/Y/Z, its tick loop stopped so a test can lay its entities down
// before the first sweep sees any of them.
func plateServer(t *testing.T, plate string) (*server.Server, *servertest.Client) {
	t.Helper()
	return plateServerWith(t, servertest.BlockState(t, plate))
}

// plateServerWith is plateServer placing the given state.
func plateServerWith(t *testing.T, state uint16) (*server.Server, *servertest.Client) {
	t.Helper()
	srv, c, _ := servertest.JoinAs(t, servertest.BlockTestConfig(), "Plater", servertest.OpsListing(t, "Plater"))
	servertest.HoldTicks(t, srv)
	if !srv.SetBlock(plateX, plateY, plateZ, state) {
		t.Fatalf("could not place state %d", state)
	}
	return srv, c
}

// plateProps reads the plate's state properties.
func plateProps(srv *server.Server) map[string]string {
	_, props := servertest.StateProps(srv.BlockAt(plateX, plateY, plateZ))
	return props
}

// expectPlateSettles waits for the plate's property to reach want and then
// checks it holds past the weighted plate's re-check.
func expectPlateSettles(t *testing.T, srv *server.Server, prop, want string) {
	t.Helper()
	servertest.UntilTicks(t, srv, 200, fmt.Sprintf("plate %s = %q", prop, want), func() bool {
		return plateProps(srv)[prop] == want
	})
	servertest.AwaitTicks(t, srv, 25)
	if got := plateProps(srv)[prop]; got != want {
		t.Fatalf("plate %s = %q after settling, want %q", prop, got, want)
	}
}

// TestItemsPressPlates lays item entities on a plate and nothing else: the
// server's own entity sweep presses it, vanilla ItemEntity.tick running
// applyEffectsFromBlocks every tick. A weighted plate counts entities, not
// the items in a stack, up to its budget: ceil(n / max * 15).
func TestItemsPressPlates(t *testing.T) {
	t.Parallel()
	cases := []struct {
		plate string
		items int
		stack int
		prop  string
		want  string
	}{
		{"minecraft:light_weighted_pressure_plate", 1, 1, "power", "1"},
		{"minecraft:light_weighted_pressure_plate", 2, 1, "power", "2"},
		{"minecraft:light_weighted_pressure_plate", 15, 1, "power", "15"},
		{"minecraft:light_weighted_pressure_plate", 16, 1, "power", "15"},
		{"minecraft:light_weighted_pressure_plate", 1, 64, "power", "1"},
		{"minecraft:heavy_weighted_pressure_plate", 1, 1, "power", "1"},
		{"minecraft:heavy_weighted_pressure_plate", 11, 1, "power", "2"},
		{"minecraft:oak_pressure_plate", 1, 1, "powered", "true"},
		{"minecraft:stone_pressure_plate", 1, 1, "powered", "false"},
	}
	for _, tc := range cases {
		t.Run(fmt.Sprintf("%s/%dx%d", tc.plate, tc.items, tc.stack), func(t *testing.T) {
			t.Parallel()
			srv, _ := plateServer(t, tc.plate)
			for _, item := range plateItems[:tc.items] {
				if srv.SpawnItemEntity(plateX+0.5, plateY+0.125, plateZ+0.5, item, tc.stack) == 0 {
					t.Fatalf("could not spawn %s", item)
				}
			}
			expectPlateSettles(t, srv, tc.prop, tc.want)
		})
	}
}

// TestSpectatorDoesNotPressPlate stands a spectator on an oak plate, which
// vanilla's NO_SPECTATORS leaves up; the same player in creative presses it.
func TestSpectatorDoesNotPressPlate(t *testing.T) {
	t.Parallel()
	srv, c := plateServer(t, "minecraft:oak_pressure_plate")
	c.ChatCommand("gamemode spectator")
	c.Settle()
	c.MoveTo(plateX+0.5, plateY, plateZ+0.5, 0, 0)
	c.Settle()
	servertest.AwaitTicks(t, srv, 5)
	if got := plateProps(srv)["powered"]; got != "false" {
		t.Fatalf("spectator pressed the plate: powered = %q", got)
	}
	c.ChatCommand("gamemode creative")
	c.Settle()
	servertest.AwaitTicks(t, srv, 40)
	expectPlateSettles(t, srv, "powered", "true")
}

// TestPlateClickSounds checks the click a pressed plate plays: the metal
// pair for the weighted plates (BlockSetType GOLD and IRON), the wooden one
// for oak, at volume and pitch one (BasePressurePlateBlock.checkPressed).
func TestPlateClickSounds(t *testing.T) {
	t.Parallel()
	for plate, sound := range map[string]string{
		"minecraft:light_weighted_pressure_plate": "minecraft:block.metal_pressure_plate.click_on",
		"minecraft:oak_pressure_plate":            "minecraft:block.wooden_pressure_plate.click_on",
	} {
		t.Run(plate, func(t *testing.T) {
			t.Parallel()
			want, ok := registry.SoundEventID(sound)
			if !ok {
				t.Fatalf("no sound event %s", sound)
			}
			srv, c := plateServer(t, plate)
			srv.SpawnItemEntity(plateX+0.5, plateY+0.125, plateZ+0.5, "minecraft:stone", 1)
			heard := c.ReadTicks(srv, 5, func(id int32, data []byte) bool {
				if id != protocol.IDPlaySoundEffect {
					return false
				}
				r := protocol.NewReader(data)
				if got := r.ReadVarInt() - 1; got != want {
					t.Fatalf("sound = %d, want %d (%s)", got, want, sound)
				}
				if got := r.ReadVarInt(); got != protocol.SoundCategoryBlock {
					t.Errorf("category = %d, want blocks", got)
				}
				r.ReadI32()
				r.ReadI32()
				r.ReadI32()
				if vol, pitch := r.ReadF32(), r.ReadF32(); vol != 1 || pitch != 1 {
					t.Errorf("volume, pitch = %v, %v, want 1, 1", vol, pitch)
				}
				return true
			})
			if !heard {
				t.Fatalf("no click sound for %s", plate)
			}
		})
	}
}

// TestArrowsPressPlates sticks an arrow in a plate: wooden and weighted
// plates count every entity, arrows included, and a stone plate only the
// living ones. Killing the arrow lets the plate up again.
// vanilla: PressurePlateBlock.getSignalStrength, WeightedPressurePlateBlock.getSignalStrength
func TestArrowsPressPlates(t *testing.T) {
	t.Parallel()
	cases := []struct {
		plate, prop, pressed, released string
	}{
		{"minecraft:oak_pressure_plate", "powered", "true", "false"},
		{"minecraft:light_weighted_pressure_plate", "power", "1", "0"},
		{"minecraft:stone_pressure_plate", "powered", "false", "false"},
	}
	for _, tc := range cases {
		t.Run(tc.plate, func(t *testing.T) {
			t.Parallel()
			srv, c := plateServer(t, tc.plate)
			srv.SpawnStuckArrow("disallowed", plateX+0.5, plateY+0.05, plateZ+0.5)
			expectPlateSettles(t, srv, tc.prop, tc.pressed)
			c.ChatCommand("kill @e[type=minecraft:arrow]")
			c.Settle()
			expectPlateSettles(t, srv, tc.prop, tc.released)
		})
	}
}

// TestArrowPressesWoodenButton sticks an arrow in a floor button: a wooden
// button is held down while the arrow stays and lets go once it is gone, a
// stone one ignores it.
// vanilla: ButtonBlock.checkPressed
func TestArrowPressesWoodenButton(t *testing.T) {
	t.Parallel()
	for button, pressed := range map[string]string{
		"minecraft:oak_button":   "true",
		"minecraft:stone_button": "false",
	} {
		t.Run(button, func(t *testing.T) {
			t.Parallel()
			srv, c := plateServerWith(t, servertest.BlockStateWith(t, button, "face=floor,facing=north,powered=false"))
			srv.SpawnStuckArrow("disallowed", plateX+0.5, plateY+0.05, plateZ+0.5)
			expectPlateSettles(t, srv, "powered", pressed)
			c.ChatCommand("kill @e[type=minecraft:arrow]")
			c.Settle()
			expectPlateSettles(t, srv, "powered", "false")
		})
	}
}
