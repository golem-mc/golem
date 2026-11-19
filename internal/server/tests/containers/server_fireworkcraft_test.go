package containers

import (
	"github.com/golem-mc/golem/internal/server/servertest"
	"github.com/golem-mc/golem/registry"
	"slices"
	"testing"

	"github.com/golem-mc/golem/internal/protocol"
)

// The firework special recipes through the player's own 2x2 grid: the result
// slot shows a rocket or star carrying the components the grid computes.
// vanilla: FireworkRocketRecipe, FireworkStarRecipe, FireworkStarFadeRecipe

// craftInGrid puts the stacks into the grid cells one after another and
// expects the last placement to leave the result slot worth want.
func craftInGrid(w *servertest.WindowedClient, want protocol.ItemStack, stacks ...protocol.ItemStack) {
	w.T.Helper()
	for i, stack := range stacks {
		w.SetCreativeSlot(36, stack)
		w.Click(36, 0, protocol.ClickModePickup)
		w.ExpectClickResult(map[int16]protocol.ItemStack{36: {}}, stack)
		w.Click(int16(1+i), 0, protocol.ClickModePickup)
		corrected := map[int16]protocol.ItemStack{}
		if i == len(stacks)-1 {
			corrected[0] = want
		}
		w.ExpectClickResult(corrected, protocol.ItemStack{})
	}
}

// withComponent is a stack of one item carrying a raw component.
func withComponent(t *testing.T, name string, count int32, component int32, data []byte) protocol.ItemStack {
	t.Helper()
	s := servertest.ItemStack(t, name, count)
	s.Raw = []protocol.RawComponent{{Type: component, Data: data}}
	return s
}

// explosionBytes is the wire form of a firework_explosion: shape, colours,
// fade colours, trail, twinkle.
func explosionBytes(shape int32, colours, fade []int32, trail, twinkle bool) []byte {
	var b protocol.Buffer
	b.PutVarInt(shape)
	for _, list := range [][]int32{colours, fade} {
		b.PutVarInt(int32(len(list)))
		for _, c := range list {
			b.PutI32(c)
		}
	}
	b.PutBool(trail)
	b.PutBool(twinkle)
	return b.Bytes()
}

func TestFireworkStarCraftsFromGunpowderAndDyes(t *testing.T) {
	t.Parallel()
	_, w := servertest.OpenWindowed(t, servertest.BlockTestConfig())
	// Fire charge is the large ball, diamond the trail; red's firework
	// colour is 0xB3312C.
	want := withComponent(t, "minecraft:firework_star", 1, protocol.DataComponentFireworkExplosion,
		explosionBytes(1, []int32{0xB3312C}, nil, true, false))
	craftInGrid(w, want,
		servertest.ItemStack(t, "minecraft:gunpowder", 1), servertest.ItemStack(t, "minecraft:red_dye", 1),
		servertest.ItemStack(t, "minecraft:fire_charge", 1), servertest.ItemStack(t, "minecraft:diamond", 1))
}

func TestFireworkStarFadeAddsFadeColours(t *testing.T) {
	t.Parallel()
	_, w := servertest.OpenWindowed(t, servertest.BlockTestConfig())
	star := withComponent(t, "minecraft:firework_star", 1, protocol.DataComponentFireworkExplosion,
		explosionBytes(2, []int32{0xB3312C}, nil, false, true))
	want := withComponent(t, "minecraft:firework_star", 1, protocol.DataComponentFireworkExplosion,
		explosionBytes(2, []int32{0xB3312C}, []int32{0x253192, 0x3B511A}, false, true))
	craftInGrid(w, want, star,
		servertest.ItemStack(t, "minecraft:blue_dye", 1), servertest.ItemStack(t, "minecraft:green_dye", 1))
}

func TestFireworkRocketCraftsWithFlightAndExplosions(t *testing.T) {
	t.Parallel()
	_, w := servertest.OpenWindowed(t, servertest.BlockTestConfig())
	star := withComponent(t, "minecraft:firework_star", 1, protocol.DataComponentFireworkExplosion,
		explosionBytes(4, []int32{0xB3312C}, nil, false, false))
	var b protocol.Buffer
	b.PutVarInt(2) // flight: the gunpowder count
	b.PutVarInt(1)
	b.PutRaw(explosionBytes(4, []int32{0xB3312C}, nil, false, false))
	want := withComponent(t, "minecraft:firework_rocket", 3, protocol.DataComponentFireworks, b.Bytes())
	craftInGrid(w, want, servertest.ItemStack(t, "minecraft:paper", 1),
		servertest.ItemStack(t, "minecraft:gunpowder", 1), servertest.ItemStack(t, "minecraft:gunpowder", 1), star)
}

// TestFireworkRocketWithTwoGunpowderHasFlightTwo is the plain rocket without
// stars: the shapeless two-ingredient recipe cannot match two gunpowder.
func TestFireworkRocketWithTwoGunpowderHasFlightTwo(t *testing.T) {
	t.Parallel()
	_, w := servertest.OpenWindowed(t, servertest.BlockTestConfig())
	var b protocol.Buffer
	b.PutVarInt(2)
	b.PutVarInt(0)
	want := withComponent(t, "minecraft:firework_rocket", 3, protocol.DataComponentFireworks, b.Bytes())
	craftInGrid(w, want, servertest.ItemStack(t, "minecraft:paper", 1),
		servertest.ItemStack(t, "minecraft:gunpowder", 1), servertest.ItemStack(t, "minecraft:gunpowder", 1))
}

// TestPlainFireworkRocketStacksWithOneWithoutAComponent: one gunpowder and
// only a star without an explosion is the item's default fireworks component,
// so the crafted rocket has no patch and a shift-click merges it into a
// rocket that has none either, as one loaded from a save does.
func TestPlainFireworkRocketStacksWithOneWithoutAComponent(t *testing.T) {
	t.Parallel()
	_, w := servertest.OpenWindowed(t, servertest.BlockTestConfig())
	w.SetCreativeSlot(44, servertest.ItemStack(t, "minecraft:firework_rocket", 3))
	rocket := servertest.ItemStack(t, "minecraft:firework_rocket", 3)
	craftInGrid(w, rocket, servertest.ItemStack(t, "minecraft:paper", 1),
		servertest.ItemStack(t, "minecraft:gunpowder", 1), servertest.ItemStack(t, "minecraft:firework_star", 1))
	w.Click(0, 0, protocol.ClickModeQuickMove)
	w.ExpectClickResult(map[int16]protocol.ItemStack{
		1: {}, 2: {}, 3: {}, 0: {},
		44: servertest.ItemStack(t, "minecraft:firework_rocket", 6),
	}, protocol.ItemStack{})
}

// TestFireworkRocketRefusesTwoPapers: a second paper spoils the recipe, so
// the result slot is empty.
func TestFireworkRocketRefusesTwoPapers(t *testing.T) {
	t.Parallel()
	_, w := servertest.OpenWindowed(t, servertest.BlockTestConfig())
	craftInGrid(w, protocol.ItemStack{}, servertest.ItemStack(t, "minecraft:paper", 1),
		servertest.ItemStack(t, "minecraft:paper", 1), servertest.ItemStack(t, "minecraft:gunpowder", 1),
		servertest.ItemStack(t, "minecraft:gunpowder", 1))
}

func TestSuspiciousStewCraftsInThePlayerGrid(t *testing.T) {
	t.Parallel()
	_, w := servertest.OpenWindowed(t, servertest.BlockTestConfig())
	var effects protocol.Buffer
	effects.PutVarInt(1)
	effects.PutVarInt(registry.MustMobEffect("minecraft:night_vision"))
	effects.PutVarInt(100)
	want := withComponent(t, "minecraft:suspicious_stew", 1, protocol.DataComponentSuspiciousStew, effects.Bytes())
	craftInGrid(w, want,
		servertest.ItemStack(t, "minecraft:bowl", 1), servertest.ItemStack(t, "minecraft:brown_mushroom", 1),
		servertest.ItemStack(t, "minecraft:red_mushroom", 1), servertest.ItemStack(t, "minecraft:poppy", 1))
}

// A dye's own component, not its item, gives the explosion colour.
// vanilla: FireworkStarRecipe, DataComponents.DYE
func TestFireworkStarTakesTheDyeComponentsColour(t *testing.T) {
	t.Parallel()
	_, w := servertest.OpenWindowed(t, servertest.BlockTestConfig())
	dye := servertest.ItemStack(t, "minecraft:red_dye", 1)
	blue := slices.Index(registry.DyeNames, "blue")
	dye.SetEnum(protocol.DataComponentDye, int32(blue))
	want := withComponent(t, "minecraft:firework_star", 1, protocol.DataComponentFireworkExplosion,
		explosionBytes(0, []int32{registry.FireworkDyeColour(blue)}, nil, false, false))
	craftInGrid(w, want, servertest.ItemStack(t, "minecraft:gunpowder", 1), dye)
}
