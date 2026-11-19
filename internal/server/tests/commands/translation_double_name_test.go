package commands

import (
	"bytes"
	"testing"

	"github.com/golem-mc/golem/internal/config"
	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/servertest"
	"github.com/golem-mc/golem/nbt"
	"github.com/golem-mc/golem/registry"
)

// vanilla: TranslatableContents.ARG_CODEC, EnchantCommand.enchant
func TestItemDoubleTranslationNameInEnchantmentError(t *testing.T) {
	t.Parallel()
	cfg := servertest.ItemCreativeConfig()
	cfg.ViewDistance, cfg.SimulationDistance = config.MinDistance, config.MinDistance
	srv, c := servertest.Join(t, cfg, "Tester", servertest.OpsListing(t, "Tester"))
	servertest.HoldTicks(t, srv)

	c.ChatCommand(`item replace entity @s weapon.mainhand with diamond_sword[custom_name="[suffix]"]`)
	c.ExpectWithinTicks(srv, 0, protocol.IDPlayContainerSlot)
	c.ExpectWithinTicks(srv, 0, protocol.IDPlaySystemChat)

	const name = `{translate:"chat.square_brackets",with:[2.5d],bold:true}`
	wantName, err := nbt.ParseSNBT(name)
	if err != nil {
		t.Fatal(err)
	}
	c.ChatCommand(`item replace entity @s weapon.mainhand with diamond_sword[custom_name=` + name + `]`)
	r := protocol.NewReader(c.ExpectWithinTicks(srv, 0, protocol.IDPlayContainerSlot))
	r.ReadVarInt() // container
	r.ReadVarInt() // state
	if slot := r.ReadI16(); slot != 36 {
		t.Fatalf("updated slot = %d, want the main hand", slot)
	}
	if count, item, added, removed := r.ReadVarInt(), r.ReadVarInt(), r.ReadVarInt(), r.ReadVarInt(); count != 1 || item != int32(registry.ItemDiamondSword) || added != 1 || removed != 0 {
		t.Fatalf("slot header = %d, %d, %d, %d, want one named diamond sword", count, item, added, removed)
	}
	if component := r.ReadVarInt(); component != protocol.DataComponentCustomName {
		t.Fatalf("component = %d, want custom_name", component)
	}
	payload := r.Rest()
	gotName, consumed, err := nbt.DecodeNetwork(payload)
	if err != nil || consumed != len(payload) || !nbt.Equal(gotName, wantName) {
		t.Fatalf("inventory name = %v, %v, want %s", gotName, err, name)
	}

	item := servertest.ItemFeedbackText("minecraft:diamond_sword", "", 1)
	item.With = []protocol.Text{{Extra: []protocol.Text{{Italic: true, Extra: []protocol.Text{{Raw: wantName}}}}}}
	item.HoverItem.Components = nbt.Compound{"minecraft:custom_name": wantName}
	var feedback protocol.Buffer
	feedback.PutText(protocol.Text{Translate: itemEntitySetSingleKey, With: []protocol.Text{
		servertest.ItemNumber(1), servertest.PlayerDisplayComponent("Tester"), item,
	}})
	feedback.PutBool(false)
	if got := c.ExpectWithinTicks(srv, 0, protocol.IDPlaySystemChat); !bytes.Equal(got, feedback.Bytes()) {
		t.Fatalf("replacement feedback lost the styled double argument: %x", got)
	}

	c.ChatCommand("enchant @s minecraft:protection")
	wantError := itemFailureText("commands.enchant.failed.incompatible", protocol.Text{Text: "[2.5]"})
	if got := c.ExpectWithinTicks(srv, 0, protocol.IDPlaySystemChat); !bytes.Equal(got, wantError) {
		tag, _, _ := nbt.DecodeNetwork(got)
		t.Fatalf("enchantment error = %s, want [2.5] cannot support that enchantment", nbt.Stringify(tag))
	}
	c.Disconnect()
}
