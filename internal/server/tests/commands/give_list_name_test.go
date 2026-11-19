package commands

import (
	"testing"

	"github.com/golem-mc/golem/internal/config"
	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/servertest"
	"github.com/golem-mc/golem/nbt"
	"github.com/golem-mc/golem/registry"
)

// vanilla: ComponentSerialization.createFromList, DataComponents.CUSTOM_NAME
func TestGiveListCustomNameKeepsStyledSuffix(t *testing.T) {
	t.Parallel()
	cfg := config.Default()
	cfg.OnlineMode = false
	cfg.ViewDistance, cfg.SimulationDistance = config.MinDistance, config.MinDistance
	srv, c := servertest.Join(t, cfg, "Tester", servertest.OpsListing(t, "Tester"))
	servertest.HoldTicks(t, srv)

	c.ChatCommand(`give @s diamond_sword[custom_name=["prefix",{text:"suffix",bold:true}]]`)
	want := nbt.List{Elem: nbt.TypeCompound, Items: []nbt.Tag{
		nbt.String("prefix"), nbt.Compound{"text": nbt.String("suffix"), "bold": nbt.Byte(1)},
	}}
	var slot, feedback bool
	c.ReadTicks(srv, 5, func(id int32, data []byte) bool {
		r := protocol.NewReader(data)
		switch id {
		case protocol.IDPlayContainerSlot:
			r.ReadVarInt()
			r.ReadVarInt()
			index := r.ReadI16()
			stack := servertest.MustReadSlot(t, r)
			if index != 36 || registry.Item(stack.ItemID) != registry.ItemDiamondSword || stack.Count != 1 {
				t.Fatalf("slot %d = %+v, want one diamond sword in the hotbar", index, stack)
			}
			if stack.CustomName == nil || stack.CustomName.Plain() != "prefixsuffix" || !nbt.Equal(stack.CustomName.Raw, want) {
				t.Fatalf("inventory name = %+v, want both components with a bold suffix", stack.CustomName)
			}
			slot = true
		case protocol.IDPlaySystemChat:
			tag, _, err := nbt.DecodeNetwork(data)
			if err != nil {
				t.Fatal(err)
			}
			message, _ := tag.(nbt.Compound)
			if message["translate"] != nbt.String("commands.give.success.single") {
				return false
			}
			args, _ := nbt.Get[nbt.List](message, "with")
			if len(args.Items) != 3 || args.Items[0] != nbt.Int(1) || protocol.PlainTextOf(args.Items[2]) != "Tester" {
				t.Fatalf("give feedback arguments = %v, want count, item and Tester", args)
			}
			item, _ := args.Items[1].(nbt.Compound)
			names, _ := nbt.Get[nbt.List](item, "with")
			if item["translate"] != nbt.String("chat.square_brackets") || len(names.Items) != 1 || protocol.PlainTextOf(names.Items[0]) != "prefixsuffix" {
				t.Fatalf("feedback item = %v, want [prefixsuffix]", item)
			}
			feedback = true
		}
		return slot && feedback
	})
	if !slot || !feedback {
		t.Fatalf("give sent inventory update: %v, success feedback: %v; want both", slot, feedback)
	}
	c.Disconnect()
}
