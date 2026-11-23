package commands

import (
	"testing"

	"github.com/golem-mc/golem/internal/config"
	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/servertest"
)

// vanilla: DataComponents.ENCHANTMENT_GLINT_OVERRIDE is Codec.BOOL, saved as
// 0b/1b and read through NbtOps.getBooleanValue (doubleValue() != 0.0).
func TestGlintOverrideNBT(t *testing.T) {
	t.Parallel()
	cfg := servertest.ItemCreativeConfig()
	cfg.ViewDistance, cfg.SimulationDistance = config.MinDistance, config.MinDistance
	srv, c := servertest.Join(t, cfg, "Testificate", servertest.OpsListing(t, "Testificate"))
	servertest.HoldTicks(t, srv)
	sword := servertest.MustItem("minecraft:diamond_sword")

	// Saving: /item writes the byte the held stack reports.
	for _, tc := range []struct {
		value, saved string
	}{{"false", "0b"}, {"true", "1b"}} {
		c.ChatCommand("item replace entity @s weapon.mainhand with minecraft:diamond_sword[enchantment_glint_override=" + tc.value + "]")
		c.ExpectWithinTicks(srv, 3, protocol.IDPlaySystemChat)
		c.ChatCommand(`data get entity @s SelectedItem.components."minecraft:enchantment_glint_override"`)
		r := protocol.NewReader(c.ExpectWithinTicks(srv, 3, protocol.IDPlaySystemChat))
		line, ok := protocol.ReadText(r)
		if !ok {
			t.Fatalf("decode feedback: %v", r.Err())
		}
		if line.Translate != "commands.data.entity.query" || len(line.With) != 2 || line.With[1].Plain() != tc.saved {
			t.Errorf("glint %s: data get = %+v, want %s", tc.value, line, tc.saved)
		}
	}

	// Loading: raw NBT is read by the component decoder, not copied from a live stack.
	c.ChatCommand("setblock 2 64 3 minecraft:chest")
	servertest.RunTicks(t, srv, 2)
	for _, tc := range []struct {
		name, value string
		want        int8
	}{
		{"0b", "0b", protocol.GlintHidden},
		{"1b", "1b", protocol.GlintShown},
		{"legacy 2b", "2b", protocol.GlintHidden},
		{"int 2", "2", protocol.GlintShown},
		{"fraction", "0.5d", protocol.GlintShown},
	} {
		c.ChatCommand(`data modify block 2 64 3 Items set value [{Slot:0b,id:"minecraft:diamond_sword",count:1,components:{"minecraft:enchantment_glint_override":` + tc.value + `}}]`)
		servertest.RunTicks(t, srv, 2)
		c.ChatCommand("item replace entity @s hotbar.6 from block 2 64 3 container.0")
		var got protocol.ItemStack
		found := c.ReadTicks(srv, 3, func(id int32, data []byte) bool {
			if id != protocol.IDPlayContainerSlot {
				return false
			}
			r := protocol.NewReader(data)
			r.ReadVarInt()
			r.ReadVarInt()
			if r.ReadI16() != 42 {
				return false
			}
			got = servertest.MustReadSlot(t, r)
			return true
		})
		if !found || got.ItemID != sword || got.GlintOverride != tc.want {
			t.Errorf("%s: hotbar.6 = %+v (arrived %v), want a sword with glint %d", tc.name, got, found, tc.want)
		}
	}
}
