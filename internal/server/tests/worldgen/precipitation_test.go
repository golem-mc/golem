package worldgen

import (
	"github.com/golem-mc/golem/internal/config"
	"github.com/golem-mc/golem/internal/server/servertest"
	"strconv"
	"testing"

	"github.com/golem-mc/golem/internal/server/world"
	"github.com/golem-mc/golem/registry"
)

// The rain's work on the blocks, driven through a whole server: a snowy
// world under a storm the test holds on, its sky driven by the tick loop's
// own random tick passes, read back out of the world. The sky's column
// picks are dice, so the test asks only for the near-certainties hundreds of
// visits make — every cell of a pond frozen, a snow pile grown to the game
// rule's height, a cauldron filled by its tenth-part chance — and holds the
// world still elsewhere so the drive stays cheap: a bottom-slab roof covers
// the whole loaded window, and the slab refuses the snow-layer the sky
// would try to lay on it, so only the hole columns the scene stands in ever
// change.

const (
	precipSeed      = int64(12)
	precipTickSpeed = 60000
	precipPasses    = 6
	precipSnowRule  = 5
	precipRoofY     = 70
)

// The scene the sky works on, all in the chunk the player stands in. The
// pond is the six-by-six at (4..9, 63, 4..9), water sunk into the top stone
// and open to the sky; the cauldrons stand out at (11..13, 64, 4..6); the
// snow probes are the bare columns at (2, 2) and (14, 2), piles the sky
// grows where nothing else intervenes.
var (
	precipPond   = [2]int32{4, 9}
	precipStream = int32(11) // z of the flowing-water row beside the pond, x over precipPond
	precipProbeX = [2]int32{2, 14}
	precipProbeZ = [2]int32{2, 2}
)

// precipSceneColumn reports whether (x, z) is one the scene occupies, which
// are the holes the roof leaves: the pond, the cauldrons and the probes.
func precipSceneColumn(x, z int32) bool {
	if x >= precipPond[0] && x <= precipPond[1] && z >= precipPond[0] && z <= precipPond[1] {
		return true
	}
	if z == precipStream && x >= precipPond[0] && x <= precipPond[1] {
		return true
	}
	if (x == precipProbeX[0] || x == precipProbeX[1]) && (z == precipProbeZ[0] || z == precipProbeZ[1]) {
		return true
	}
	return x >= 11 && x <= 13 && z >= 4 && z <= 6
}

// TestColdRainLaysSnowFreezesWaterAndFillsCauldrons is the weather's side of
// the blocks, through the tick passes the stopped loop no longer runs: the
// storm lays snow on the open columns and grows the pile to the
// max_snow_accumulation_height rule, no further, the pond freezes to ice,
// and the open cauldrons take the snowfall's powder snow a level at a time.
// The nether and the end, whose skies no storm of the overworld's reaches,
// keep the pass's hands off them; the world-level precipitation tests drive
// the pass over their own chunks directly.
func TestColdRainLaysSnowFreezesWaterAndFillsCauldrons(t *testing.T) {
	t.Parallel()
	cfg := config.Default()
	cfg.OnlineMode = false
	// A small view keeps the loaded window the passes walk close around
	// the player.
	cfg.ViewDistance = 2
	servertest.SnowyFlatWorld(t, cfg)

	srv, c := servertest.Join(t, cfg, "Snowbound")
	srv.StopTick()

	// A storm at full strength: the fade runs in TickWeather, which the
	// stopped loop no longer drives by itself.
	srv.SetWeatherState(true, false, 100000, 100000, 0)
	for range 120 {
		srv.TickWeather()
	}
	if !srv.IsRaining() {
		t.Fatal("the world is not raining")
	}
	servertest.SeedPhysics(t, srv, precipSeed)
	srv.SetGameRuleValue(world.RuleRandomTickSpeed.String(), precipTickSpeed)
	srv.SetGameRuleValue(world.RuleMaxSnowHeight.String(), precipSnowRule)

	// The pond: water sunk into the top stone, open to the sky.
	water := servertest.BlockStateWith(t, "minecraft:water", "level=0")
	for x := precipPond[0]; x <= precipPond[1]; x++ {
		for z := precipPond[0]; z <= precipPond[1]; z++ {
			if !srv.SetBlock(x, 63, z, water) {
				t.Fatalf("the pond at (%d, 63, %d) did not dig", x, z)
			}
		}
	}
	// A stream of flowing water: vanilla freezes source water alone.
	flowing := servertest.BlockStateWith(t, "minecraft:water", "level=1")
	for x := precipPond[0]; x <= precipPond[1]; x++ {
		if !srv.SetBlock(x, 63, precipStream, flowing) {
			t.Fatalf("the stream at (%d, 63, %d) did not dig", x, precipStream)
		}
	}
	// The cauldrons, set out bare beside it.
	cauldron := registry.BlockStates["minecraft:cauldron"]
	for x := int32(11); x <= 13; x++ {
		for z := int32(4); z <= 6; z++ {
			if !srv.SetBlock(x, 64, z, cauldron) {
				t.Fatalf("the cauldron at (%d, 64, %d) did not stand", x, z)
			}
		}
	}
	// The roof: a bottom slab at precipRoofY over every loaded column the
	// scene does not occupy. The slab counts on the motion heightmap, so a
	// roofed column answers the sky's questions with the slab: none of
	// them are the column-top water a freeze looks for, and the snow
	// layer cannot survive on the slab's half-filled top, so the roof
	// grows nothing either way. The gather the passes walk reaches every
	// loaded chunk near the player — the spawn chunks well past the
	// client's view window among them — so the roof has to cover them
	// all, or the drives spend themselves snowing the whole square.
	slab := registry.MustState("minecraft:stone_slab", map[string]string{"type": "bottom"})
	for cx := int32(-10); cx <= 10; cx++ {
		for cz := int32(-10); cz <= 10; cz++ {
			if !srv.ChunkLoadedInDimension(world.DimensionName, cx*16+8, cz*16+8) {
				continue
			}
			for lx := range int32(16) {
				for lz := range int32(16) {
					x, z := cx*16+lx, cz*16+lz
					if precipSceneColumn(x, z) {
						continue
					}
					srv.SetBlock(x, precipRoofY, z, slab)
				}
			}
		}
	}

	// The sky's work: passes driven by hand, the dice pinned by the seed,
	// the client drained in between so the packets the changes broadcast
	// queue up for a reader, not a 32-megabyte mailbox.
	for range precipPasses {
		srv.TickRandomTicks(1)
		c.ReadTicks(srv, 0, func(int32, []byte) bool { return false })
	}

	// The pond froze to the ice the cold laid on it, every cell that held
	// water under the open sky.
	ice := registry.BlockStates["minecraft:ice"]
	for x := precipPond[0]; x <= precipPond[1]; x++ {
		for z := precipPond[0]; z <= precipPond[1]; z++ {
			if got := srv.BlockAt(x, 63, z); got != ice {
				t.Errorf("the pond at (%d, 63, %d) = %d, want frozen ice", x, z, got)
			}
		}
	}

	for x := precipPond[0]; x <= precipPond[1]; x++ {
		if got := srv.BlockAt(x, 63, precipStream); got != flowing {
			t.Errorf("the stream at (%d, 63, %d) = %d, want flowing water %d", x, precipStream, got, flowing)
		}
	}

	// The cauldrons took the snowfall's powder snow, a level or three of
	// it, and nothing but: the fill is the snow's own powder, never the
	// rain's water, and never past the full three.
	filled := 0
	for x := int32(11); x <= 13; x++ {
		for z := int32(4); z <= 6; z++ {
			state := srv.BlockAt(x, 64, z)
			name, props, ok := registry.BlockStateName(state)
			if !ok {
				t.Fatalf("the cauldron at (%d, 64, %d) reads no state", x, z)
			}
			switch name {
			case "minecraft:cauldron":
			case "minecraft:powder_snow_cauldron":
				filled++
				if level := propsLevel(t, props, "level"); level < 1 || level > 3 {
					t.Errorf("the cauldron at (%d, %d) filled to level %d, want one to three", x, z, level)
				}
			default:
				t.Errorf("the cauldron at (%d, 64, %d) became %s %s, want powder snow or the empty cauldron", x, z, name, props)
			}
		}
	}
	if filled < 6 {
		t.Errorf("%d of 9 cauldrons filled, want most of them: the tenth-part chance had dozens of visits each", filled)
	}

	// The snow fell on the open probes and grew each pile to the
	// max_snow_accumulation_height rule's five layers, no further — the
	// growth is a layer a visit and stops at the rule.
	for i := range precipProbeX {
		state := srv.BlockAt(precipProbeX[i], 64, precipProbeZ[i])
		name, props, _ := registry.BlockStateName(state)
		if name != "minecraft:snow" {
			t.Errorf("probe (%d, 64, %d) = %s %s, want a snow pile", precipProbeX[i], precipProbeZ[i], name, props)
			continue
		}
		if layers := propsLevel(t, props, "layers"); layers != precipSnowRule {
			t.Errorf("the snow pile at (%d, 64, %d) reached %d layers, want the game rule's %d",
				precipProbeX[i], precipProbeZ[i], layers, precipSnowRule)
		}
	}

	c.Disconnect()
}

// propsLevel reads a state's named integer property, failing on anything
// else.
func propsLevel(t *testing.T, props, key string) int {
	t.Helper()
	return propInt(t, registry.ParseStateProps(props)[key])
}

// propInt parses a state property the test holds to be an integer.
func propInt(t *testing.T, raw string) int {
	t.Helper()
	value, err := strconv.Atoi(raw)
	if err != nil {
		t.Fatalf("state property %q is no integer", raw)
	}
	return value
}
