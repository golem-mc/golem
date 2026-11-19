package redstone

import (
	"bytes"
	"fmt"
	"testing"
	"time"

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
	srv.StopTick()
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
			srv.StartTick()
			expectPlateSettles(t, srv, tc.prop, tc.want)
		})
	}
}

// TestOrbPressesLightPlate puts one experience orb on a light weighted
// plate: vanilla ExperienceOrb.tick applies block effects too.
func TestOrbPressesLightPlate(t *testing.T) {
	t.Parallel()
	srv, _ := plateServer(t, "minecraft:light_weighted_pressure_plate")
	srv.SpawnOrb(plateX+0.5, plateY, plateZ+0.5, 1)
	srv.StartTick()
	expectPlateSettles(t, srv, "power", "1")
}

// TestSpectatorDoesNotPressPlate stands a spectator on an oak plate, which
// vanilla's NO_SPECTATORS leaves up; the same player in creative presses it.
func TestSpectatorDoesNotPressPlate(t *testing.T) {
	t.Parallel()
	srv, c := plateServer(t, "minecraft:oak_pressure_plate")
	srv.StartTick()
	c.ChatCommand("gamemode spectator")
	c.MoveTo(plateX+0.5, plateY, plateZ+0.5, 0, 0)
	c.Settle()
	servertest.AwaitTicks(t, srv, 5)
	if got := plateProps(srv)["powered"]; got != "false" {
		t.Fatalf("spectator pressed the plate: powered = %q", got)
	}
	c.ChatCommand("gamemode creative")
	servertest.AwaitTicks(t, srv, 40)
	t.Logf("DEBUG %+v", srv.ArrowsInDimension("minecraft:overworld"))
	expectPlateSettles(t, srv, "powered", "true")
}

// TestPlateClickSounds checks the click a pressed plate plays: the metal
// pair for the weighted plates (BlockSetType GOLD and IRON), the wooden one
// for oak, at volume and pitch one (BasePressurePlateBlock.checkPressed).
func TestPlateClickSounds(t *testing.T) {
	t.Parallel()
	for plate, sound := range map[string]string{
		"minecraft:light_weighted_pressure_plate": "minecraft:block.metal_pressure_plate.click_on",
		"minecraft:heavy_weighted_pressure_plate": "minecraft:block.metal_pressure_plate.click_on",
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
			srv.StartTick()
			for id, data := range c.Stream() {
				if id != protocol.IDPlaySoundEffect {
					continue
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
				return
			}
		})
	}
}

// TestMobOnPlateKeepsTicking summons a cow onto an unpressed plate. The
// sweep once held the mob lock while the plate asked who stood on it, which
// took the same lock again and stopped the game tick for good (#63).
func TestMobOnPlateKeepsTicking(t *testing.T) {
	t.Parallel()
	srv, c := plateServer(t, "minecraft:stone_pressure_plate")
	srv.StartTick()
	c.ChatCommand(fmt.Sprintf("summon minecraft:cow %d.5 %d %d.5", plateX, plateY, plateZ))
	// A stuck tick holds locks the cleanups wait on, so a failing t.Fatal
	// would hang; the watchdog panics off the test goroutine, which ends
	// the binary without running them.
	watchdog := time.AfterFunc(20*time.Second, func() {
		panic("TestMobOnPlateKeepsTicking: the game tick stopped after a mob stepped on a plate")
	})
	defer watchdog.Stop()
	servertest.AwaitTicks(t, srv, 10)
	if got := plateProps(srv)["powered"]; got != "true" {
		t.Errorf("cow on the stone plate: powered = %q, want true", got)
	}
	c.ChatCommand("time query gametime")
	for {
		if data := c.ExpectPlay(protocol.IDPlaySystemChat); bytes.Contains(data, []byte("commands.time.query.gametime")) {
			break
		}
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
			srv.StartTick()
			expectPlateSettles(t, srv, tc.prop, tc.pressed)
			c.ChatCommand("kill @e[type=minecraft:arrow]")
			expectPlateSettles(t, srv, tc.prop, tc.released)
		})
	}
}

// TestFlyingArrowPressesPlate shoots an arrow straight down onto an oak
// plate: the arrow's own sweep presses it where it lands.
func TestFlyingArrowPressesPlate(t *testing.T) {
	t.Parallel()
	srv, _ := plateServer(t, "minecraft:oak_pressure_plate")
	srv.ShootArrow(plateX+0.5, plateY+3, plateZ+0.5, 0, -1, 0)
	srv.StartTick()
	expectPlateSettles(t, srv, "powered", "true")
	if arrows := srv.ArrowsInDimension("minecraft:overworld"); len(arrows) != 1 {
		t.Fatalf("arrows = %+v, want the one that landed", arrows)
	}
}

// TestButtonHandUseSoundSkipsTheUser presses a wooden button by hand with two
// clients watching: the other player hears the press click once and the user
// hears nothing, because their client predicted the press and vanilla's play
// sound skips the user there. The release the scheduled tick plays reaches both.
// vanilla: ButtonBlock.playSound
func TestButtonHandUseSoundSkipsTheUser(t *testing.T) {
	t.Parallel()
	srv, connect := servertest.Start(t, servertest.BlockTestConfig())

	watcher := connect().Join("Watcher")

	actor := connect().Join("Actor")

	entityID := watcher.ExpectPlayerAdded("Actor")
	watcher.ExpectAnnouncement("multiplayer.player.joined", "Actor")

	// Within a hand's reach of the spawn, unlike the plate tests' far cell.
	const bx, by, bz = 5, 64, 0
	if !srv.SetBlock(bx, by, bz, servertest.BlockStateWith(t, "minecraft:oak_button", "face=floor,facing=north,powered=false")) {
		t.Fatal("could not place the button")
	}

	on, ok := registry.SoundEventID("minecraft:block.wooden_button.click_on")
	if !ok {
		t.Fatal("no sound event minecraft:block.wooden_button.click_on")
	}

	actor.UseBlock(bx, by, bz)

	// The press: the other player hears the click once, the user not at all.
	// Block updates follow the sound, so reading to the button's update
	// settles what came of the press.
	presses := 0
	for id, data := range watcher.Stream() {
		if id == protocol.IDPlaySoundEffect {
			presses++
			if got := protocol.NewReader(data).ReadVarInt() - 1; got != on {
				t.Errorf("button press sound = %d, want %d (minecraft:block.wooden_button.click_on)", got, on)
			}
		}
		if id == protocol.IDPlayBlockUpdate {
			break
		}
	}
	if presses != 1 {
		t.Errorf("the other player heard %d press clicks, want exactly 1", presses)
	}
	for id := range actor.Stream() {
		if id == protocol.IDPlaySoundEffect {
			t.Error("the user heard the button press echoed back, but the client predicts it already")
		}
		if id == protocol.IDPlayBlockUpdate {
			break
		}
	}

	// The release reaches everyone, the user included.
	servertest.ExpectSoundEvent(t, watcher, "minecraft:block.wooden_button.click_off")
	servertest.ExpectSoundEvent(t, actor, "minecraft:block.wooden_button.click_off")

	actor.Conn.Close()
	watcher.ExpectPlayerRemoved(entityID, "Actor")
}

// TestArrowHeldWoodenButtonStaysPressed keeps an arrow in an oak button past
// several press spans: the scheduled tick must leave a held button alone, not
// click off and let the arrow's sweep press it again. The arrow gone, the
// button releases.
// vanilla: ButtonBlock.checkPressed
func TestArrowHeldWoodenButtonStaysPressed(t *testing.T) {
	t.Parallel()
	srv, c := plateServerWith(t, servertest.BlockStateWith(t, "minecraft:oak_button", "face=floor,facing=north,powered=false"))
	srv.SpawnStuckArrow("disallowed", plateX+0.5, plateY+0.05, plateZ+0.5)
	srv.StartTick()
	expectPlateSettles(t, srv, "powered", "true")

	off, ok := registry.SoundEventID("minecraft:block.wooden_button.click_off")
	if !ok {
		t.Fatal("no sound event minecraft:block.wooden_button.click_off")
	}
	// Six press spans: a button the arrow no longer holds would click off at
	// ticks 29, 59, 89 and 119 and the arrow's sweep would press it again.
	offClicks := 0
	for range 6 {
		servertest.AwaitTicks(t, srv, 20)
		c.SettleAndCollect(func(id int32, data []byte) {
			if id == protocol.IDPlaySoundEffect && protocol.NewReader(data).ReadVarInt()-1 == off {
				offClicks++
			}
		})
	}
	if offClicks != 0 {
		t.Fatalf("the arrow-held button clicked off %d times in 120 ticks, want 0", offClicks)
	}
	if got := plateProps(srv)["powered"]; got != "true" {
		t.Fatalf("arrow-held button powered = %q, want true", got)
	}

	c.ChatCommand("kill @e[type=minecraft:arrow]")
	expectPlateSettles(t, srv, "powered", "false")
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
			srv.StartTick()
			expectPlateSettles(t, srv, "powered", pressed)
			c.ChatCommand("kill @e[type=minecraft:arrow]")
			expectPlateSettles(t, srv, "powered", "false")
		})
	}
}

// TestArrowOnlyPressesButtonItsBoxTouches checks the arrow against the
// button's shape rather than its cell: one stuck high in a floor button's
// cell leaves it alone, and one whose centre is in the next cell but whose box
// reaches a wall button's plate presses it.
// vanilla: ButtonBlock.checkPressed
func TestArrowOnlyPressesButtonItsBoxTouches(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name, state string
		x, y, z     float64
		powered     string
	}{
		{"high in floor cell", "face=floor,facing=north,powered=false", plateX + 0.5, plateY + 0.9, plateZ + 0.5, "false"},
		{"low in floor cell", "face=floor,facing=north,powered=false", plateX + 0.5, plateY + 0.05, plateZ + 0.5, "true"},
		{"next cell reaching wall plate", "face=wall,facing=east,powered=false", plateX - 0.2, plateY + 0.4, plateZ + 0.5, "true"},
		{"next cell clear of wall plate", "face=wall,facing=east,powered=false", plateX - 0.4, plateY + 0.4, plateZ + 0.5, "false"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			srv, _ := plateServerWith(t, servertest.BlockStateWith(t, "minecraft:oak_button", tc.state))
			srv.SpawnStuckArrow("disallowed", tc.x, tc.y, tc.z)
			srv.StartTick()
			expectPlateSettles(t, srv, "powered", tc.powered)
		})
	}
}
