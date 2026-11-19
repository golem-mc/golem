package commands

import (
	"bytes"
	"strconv"
	"strings"
	"testing"

	"github.com/golem-mc/golem/internal/config"
	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/servertest"
	"github.com/golem-mc/golem/nbt"
	"github.com/golem-mc/golem/registry"
)

// vanilla: AttributeModifier.MAP_CODEC, NbtOps.getNumberValue
func TestItemAttributeModifierNumericAmounts(t *testing.T) {
	t.Parallel()
	testAttributeModifierNumericAmounts(t, "item replace entity @s armor.head with ", true)
}

// vanilla: AttributeModifier.MAP_CODEC, NbtOps.getNumberValue
func TestGiveAttributeModifierNumericAmounts(t *testing.T) {
	t.Parallel()
	testAttributeModifierNumericAmounts(t, "give @s ", false)
}

func testAttributeModifierNumericAmounts(t *testing.T, command string, worn bool) {
	t.Helper()
	cfg := servertest.ItemCreativeConfig()
	cfg.ViewDistance, cfg.SimulationDistance = config.MinDistance, config.MinDistance
	srv, c := servertest.Join(t, cfg, "Actor", servertest.OpsListing(t, "Actor"))
	servertest.HoldTicks(t, srv)
	head, _ := registry.SlotGroupID("head")
	for i, amount := range []string{"2", "2d", "2b", "2s", "2L", "2.5f", "2.5d"} {
		c.ChatCommand(command + `leather_helmet[attribute_modifiers=[{type:"minecraft:oxygen_bonus",id:"test:oxygen",amount:` + amount + `,operation:"add_value",slot:"head"}]]`)
		r := protocol.NewReader(c.ExpectWithinTicks(srv, 0, protocol.IDPlayContainerSlot))
		r.ReadVarInt() // container
		r.ReadVarInt() // state
		slot := r.ReadI16()
		stack := servertest.MustReadSlot(t, r)
		wantSlot := int16(36 + i)
		if worn {
			wantSlot = 5
		}
		wantAmount := 2.0
		if strings.Contains(amount, ".") {
			wantAmount = 2.5
		}
		entries, ok := stack.AttributeModifiersOf()
		want := protocol.AttributeModifierEntry{Attribute: int32(registry.AttributeOxygenBonus), ID: "test:oxygen", Amount: wantAmount, Slot: head}
		if r.Err() != nil || slot != wantSlot || stack.ItemID != int32(registry.ItemLeatherHelmet) || stack.Count != 1 || !ok || len(entries) != 1 || entries[0] != want {
			t.Fatalf("slot %d, stack %+v, modifiers %+v, error %v; want slot %d with modifier %+v", slot, stack, entries, r.Err(), wantSlot, want)
		}
		c.ExpectWithinTicks(srv, 0, protocol.IDPlaySystemChat)
		if !worn {
			c.ChatCommand("item replace entity @s armor.head from entity @s hotbar." + strconv.Itoa(i))
			c.ExpectWithinTicks(srv, 0, protocol.IDPlayContainerSlot)
			c.ExpectWithinTicks(srv, 0, protocol.IDPlaySystemChat)
		}
		servertest.RunTicks(t, srv, 1) // Equipment modifiers refresh on the player's tick.
		c.ChatCommand("attribute @s minecraft:oxygen_bonus get")
		wantFeedback := servertest.SystemChatWire(protocol.Text{Translate: "commands.attribute.value.get.success", With: []protocol.Text{
			{Translate: "attribute.name.oxygen_bonus"}, servertest.PlayerDisplayComponent("Actor"), {Text: strconv.FormatFloat(wantAmount, 'f', 1, 64)},
		}})
		if got := c.ExpectWithinTicks(srv, 0, protocol.IDPlaySystemChat); !bytes.Equal(got, wantFeedback) {
			t.Fatalf("amount %s: attribute feedback = %x, want %x", amount, got, wantFeedback)
		}
	}
	for _, amount := range []string{`"2"`, "{}", "[]"} {
		c.ChatCommand(command + `leather_helmet[attribute_modifiers=[{type:"minecraft:oxygen_bonus",id:"test:oxygen",amount:` + amount + `,operation:"add_value",slot:"head"}]]`)
		tag, _, err := nbt.DecodeNetwork(c.ExpectWithinTicks(srv, 0, protocol.IDPlaySystemChat))
		if err != nil || !strings.Contains(nbt.Stringify(tag), "arguments.item.component.malformed") {
			t.Fatalf("amount %s: refusal = %v, %v", amount, tag, err)
		}
		c.ExpectNoneWithinTicks(srv, 0, servertest.PacketIs(protocol.IDPlayContainerSlot))
	}
	c.Disconnect()
}
