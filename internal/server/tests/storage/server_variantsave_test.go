package storage

import (
	"bytes"
	"testing"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/servertest"
)

// TestVariantComponentsSurviveARejoin gives a player a trimmed chestplate, a
// goat horn and three fish buckets carrying their variants, restarts the
// server, and checks each slot comes back with the component bytes it was
// given.
// vanilla: ArmorTrim.CODEC, InstrumentComponent.CODEC,
// TropicalFish.Pattern.CODEC, Axolotl.Variant.CODEC, Salmon.Variant.CODEC
func TestVariantComponentsSurviveARejoin(t *testing.T) {
	t.Parallel()
	cfg := servertest.SurvivalTestConfig()
	cfg.LevelName = t.TempDir()
	srv, connect := servertest.Start(t, cfg, servertest.OpsListing(t, "Keeper"))

	given := []struct {
		item      string
		component int32
	}{
		{"iron_chestplate[trim={material:\"minecraft:gold\",pattern:\"minecraft:coast\"}]", protocol.DataComponentTrim},
		{"goat_horn[instrument=\"minecraft:dream_goat_horn\"]", protocol.DataComponentInstrument},
		{"tropical_fish_bucket[tropical_fish/pattern=\"sunstreak\"]", protocol.DataComponentTropicalFishPattern},
		{"axolotl_bucket[axolotl/variant=\"blue\"]", protocol.DataComponentAxolotlVariant},
		{"salmon_bucket[salmon/size=\"large\"]", protocol.DataComponentSalmonSize},
	}
	c := connect().Join("Keeper")
	before := map[int16]protocol.ItemStack{}
	for _, g := range given {
		c.ChatCommand("give @s " + g.item)
		updates, _ := servertest.AwaitSlotUpdates(t, c, 1)
		stack := updates[0].Stack
		if !carries(stack, g.component) {
			t.Fatalf("/give %s: the stack %+v carries no component %d", g.item, stack, g.component)
		}
		before[updates[0].Slot] = stack
	}
	c.Disconnect()
	srv.Shutdown()

	restarted := *cfg
	_, reconnect := servertest.Start(t, &restarted)
	again := reconnect()
	again.Login("Keeper")
	again.Configure()
	slots := again.Inventory()
	for slot, want := range before {
		got := slots[slot]
		if got.ItemID != want.ItemID || !sameRaw(got.Raw, want.Raw) {
			t.Errorf("slot %d came back as %+v, want %+v", slot, got, want)
		}
	}
	again.Disconnect()
}

// carries reports whether the stack holds a raw component of the type.
func carries(stack protocol.ItemStack, component int32) bool {
	for _, raw := range stack.Raw {
		if raw.Type == component {
			return true
		}
	}
	return false
}

// sameRaw reports whether two stacks' raw components hold the same bytes per
// type, in any order.
func sameRaw(a, b []protocol.RawComponent) bool {
	if len(a) != len(b) {
		return false
	}
	for _, x := range a {
		found := false
		for _, y := range b {
			if x.Type == y.Type && bytes.Equal(x.Data, y.Data) {
				found = true
			}
		}
		if !found {
			return false
		}
	}
	return true
}
