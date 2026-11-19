package redstone

import (
	"fmt"
	"testing"

	"github.com/golem-mc/golem/internal/server/servertest"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server"
	"github.com/golem-mc/golem/internal/server/entity"
	"github.com/golem-mc/golem/internal/server/world"
)

// plateEntityCases are the three sensitivities a pressing body meets: a
// wooden plate counts every entity, a stone plate the living only, and a
// light weighted plate one entity in its fifteen.
var plateEntityCases = []struct {
	plate string
	prop  string
	want  string
}{
	{"minecraft:oak_pressure_plate", "powered", "true"},
	{"minecraft:stone_pressure_plate", "powered", "false"},
	{"minecraft:light_weighted_pressure_plate", "power", "1"},
}

// awaitPlateProp waits for the plate's property to reach want, without the
// settle a body that does not outlive its press cannot give. A press can be
// over between two polls of the world on a loaded machine, so the plate's
// changes are read off the connection, which carries every one of them.
func awaitPlateProp(t *testing.T, srv *server.Server, c *servertest.Client, prop, want string) {
	t.Helper()
	if plateProps(srv)[prop] == want {
		return
	}
	section := int64(uint64((plateX>>4)&0x3FFFFF)<<42 | uint64((plateZ>>4)&0x3FFFFF)<<20 | uint64((plateY>>4)&0xFFFFF))
	cell := int64((plateX&15)<<8 | (plateZ&15)<<4 | (plateY & 15))
	for id, data := range c.Stream() {
		state := int64(-1)
		switch id {
		case protocol.IDPlayBlockUpdate:
			r := protocol.NewReader(data)
			if x, y, z := r.ReadPosition(); x == plateX && y == plateY && z == plateZ {
				state = int64(r.ReadVarInt())
			}
		case protocol.IDPlaySectionBlocks:
			if pos, entries := servertest.ReadSectionBlocks(t, data); pos == section {
				for _, entry := range entries {
					if entry&0xFFF == cell {
						state = entry >> 12
					}
				}
			}
		}
		if state >= 0 {
			if _, props := servertest.StateProps(uint16(state)); props[prop] == want {
				return
			}
		}
	}
}

// TestPrimedTNTPressesPlates stands a primed TNT on each plate: vanilla's
// PressurePlateBlock counts every entity, the stone plate living only.
// vanilla: PressurePlateBlock.getSignalStrength, WeightedPressurePlateBlock.getSignalStrength
func TestPrimedTNTPressesPlates(t *testing.T) {
	t.Parallel()
	for _, tc := range plateEntityCases {
		t.Run(tc.plate, func(t *testing.T) {
			t.Parallel()
			srv, _ := plateServer(t, tc.plate)
			// A long fuse, so the press is watched for as long as it takes
			// and the blast never races the check.
			if srv.SpawnTNT(plateX+0.5, plateY, plateZ+0.5, 600) == 0 {
				t.Fatal("SpawnTNT came to nothing")
			}
			srv.StartTick()
			expectPlateSettles(t, srv, tc.prop, tc.want)
		})
	}
}

// TestBoatPressesPlates floats a boat on each plate. The boat rests on the
// plate and counts like any other body.
func TestBoatPressesPlates(t *testing.T) {
	t.Parallel()
	for _, tc := range plateEntityCases {
		t.Run(tc.plate, func(t *testing.T) {
			t.Parallel()
			srv, _ := plateServer(t, tc.plate)
			if srv.SpawnBoat(plateX+0.5, plateY, plateZ+0.5, "minecraft:oak_boat") == 0 {
				t.Fatal("SpawnBoat came to nothing")
			}
			srv.StartTick()
			expectPlateSettles(t, srv, tc.prop, tc.want)
		})
	}
}

// TestAPrimedTNTInAnotherDimensionLeavesAPlateAlone checks the dimension
// filter the touch snapshot runs: a TNT burning at the same coordinates in
// the nether is nowhere near the overworld plate.
func TestAPrimedTNTInAnotherDimensionLeavesAPlateAlone(t *testing.T) {
	t.Parallel()
	srv, _ := plateServer(t, "minecraft:oak_pressure_plate")
	if srv.SpawnTNTInDimension(world.NetherName, plateX+0.5, plateY, plateZ+0.5, 600) == 0 {
		t.Fatal("SpawnTNTInDimension came to nothing")
	}
	srv.StartTick()
	servertest.AwaitTicks(t, srv, 5)
	if got := plateProps(srv)["powered"]; got != "false" {
		t.Fatalf("a nether TNT pressed the overworld plate: powered = %q", got)
	}
}

// TestThrownSnowballPressesPlates throws a snowball through the plate's
// cell: the body breaks on the plate, so the plate is waited down and not
// to stay.
func TestThrownSnowballPressesPlates(t *testing.T) {
	t.Parallel()
	for _, tc := range plateEntityCases {
		t.Run(tc.plate, func(t *testing.T) {
			t.Parallel()
			srv, c := plateServer(t, tc.plate)
			if srv.ThrowThrown(entity.ThrownSnowball, plateX+0.5, plateY+0.3, plateZ+0.5, 0, -90, 0, 1) == 0 {
				t.Fatal("ThrowThrown came to nothing")
			}
			srv.StartTick()
			awaitPlateProp(t, srv, c, tc.prop, tc.want)
		})
	}
}

// TestFallingBlockPressesPlates drops a falling block onto each plate from
// the cell above. It carries water, a block with no item form, so a landing
// the plate refuses leaves no item behind to press the plate in its place:
// the press is the body's own as it passes through the plate's quarter-block
// touch box on its way down.
func TestFallingBlockPressesPlates(t *testing.T) {
	t.Parallel()
	for _, tc := range plateEntityCases {
		t.Run(tc.plate, func(t *testing.T) {
			t.Parallel()
			srv, c := plateServer(t, tc.plate)
			if srv.SpawnFallingBlock(plateX, plateY+1, plateZ, servertest.BlockState(t, "minecraft:water")) == 0 {
				t.Fatal("SpawnFallingBlock came to nothing")
			}
			srv.StartTick()
			awaitPlateProp(t, srv, c, tc.prop, tc.want)
		})
	}
}

// TestArmorStandPressesEveryPlate summons an armour stand on each plate: a
// living body for every plate kind unless it is a marker.
// vanilla: ArmorStand.isIgnoringBlockTriggers
func TestArmorStandPressesEveryPlate(t *testing.T) {
	t.Parallel()
	cases := []struct {
		plate, prop, want, nbt string
	}{
		{"minecraft:oak_pressure_plate", "powered", "true", ""},
		{"minecraft:stone_pressure_plate", "powered", "true", ""},
		{"minecraft:light_weighted_pressure_plate", "power", "1", ""},
		{"minecraft:stone_pressure_plate", "powered", "false", " {Marker:1b}"},
	}
	for _, tc := range cases {
		t.Run(tc.plate+tc.nbt, func(t *testing.T) {
			t.Parallel()
			srv, c := plateServer(t, tc.plate)
			srv.StartTick()
			c.ChatCommand(fmt.Sprintf("summon minecraft:armor_stand %d.5 %d %d.5%s", plateX, plateY, plateZ, tc.nbt))
			servertest.AwaitTicks(t, srv, 10)
			if got := plateProps(srv)[tc.prop]; got != tc.want {
				t.Errorf("%s = %q, want %q", tc.prop, got, tc.want)
			}
		})
	}
}

// TestBatDoesNotPressPlates hangs a bat on each plate; vanilla's bat ignores
// block triggers.
// vanilla: Bat.isIgnoringBlockTriggers
func TestBatDoesNotPressPlates(t *testing.T) {
	t.Parallel()
	for _, plate := range []string{"minecraft:oak_pressure_plate", "minecraft:stone_pressure_plate"} {
		t.Run(plate, func(t *testing.T) {
			t.Parallel()
			srv, c := plateServer(t, plate)
			srv.StartTick()
			c.ChatCommand(fmt.Sprintf("summon minecraft:bat %d.5 %d %d.5 {NoAI:1b}", plateX, plateY, plateZ))
			servertest.AwaitTicks(t, srv, 10)
			if got := plateProps(srv)["powered"]; got != "false" {
				t.Errorf("powered = %q, want false", got)
			}
		})
	}
}

// TestFireworkPressesPlates catches the rocket's first ticks before it rises
// clear of the plate's detection box.
func TestFireworkPressesPlates(t *testing.T) {
	t.Parallel()
	for _, tc := range plateEntityCases {
		t.Run(tc.plate, func(t *testing.T) {
			t.Parallel()
			srv, _ := plateServer(t, tc.plate)
			servertest.HoldTicks(t, srv)
			if srv.SpawnFireworkStackEntity(servertest.FireworkStack(3, 0), plateX+0.5, plateY, plateZ+0.5).ID == 0 {
				t.Fatal("rocket did not spawn")
			}
			servertest.RunTicks(t, srv, 1)
			if got := plateProps(srv)[tc.prop]; got != tc.want {
				t.Fatalf("%s = %q, want %q", tc.prop, got, tc.want)
			}
		})
	}
}

func TestEndCrystalPressesPlates(t *testing.T) {
	t.Parallel()
	for _, tc := range plateEntityCases {
		t.Run(tc.plate, func(t *testing.T) {
			t.Parallel()
			srv, c := plateServer(t, tc.plate)
			servertest.HoldTicks(t, srv)
			c.ChatCommand(fmt.Sprintf("summon minecraft:end_crystal %d.5 %d %d.5", plateX, plateY, plateZ))
			c.Settle()
			servertest.RunTicks(t, srv, 1)
			if got := plateProps(srv)[tc.prop]; got != tc.want {
				t.Fatalf("%s = %q, want %q", tc.prop, got, tc.want)
			}
		})
	}
}

func TestFishingBobberPressesPlates(t *testing.T) {
	t.Parallel()
	for _, tc := range plateEntityCases {
		t.Run(tc.plate, func(t *testing.T) {
			t.Parallel()
			srv, c := plateServer(t, tc.plate)
			servertest.HoldTicks(t, srv)
			c.ChatCommand("gamemode survival")
			srv.GiveSlot("Plater", 36, protocol.ItemStack{ItemID: servertest.MustItem("minecraft:fishing_rod"), Count: 1})
			c.MoveTo(plateX+0.5, plateY, plateZ-1.5, 0, 40)
			servertest.UseHeld(c, 0, 40)
			c.Settle()
			if tc.want == "false" {
				servertest.RunTicks(t, srv, 40)
				if got := plateProps(srv)[tc.prop]; got != tc.want {
					t.Fatalf("%s = %q, want %q", tc.prop, got, tc.want)
				}
			} else {
				servertest.UntilTicks(t, srv, 40, "bobber presses plate", func() bool {
					return plateProps(srv)[tc.prop] == tc.want
				})
			}
		})
	}
}
