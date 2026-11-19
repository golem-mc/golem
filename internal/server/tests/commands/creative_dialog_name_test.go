package commands

import (
	"fmt"
	"strings"
	"testing"

	"github.com/golem-mc/golem/internal/config"
	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/servertest"
	"github.com/golem-mc/golem/nbt"
)

// vanilla: ServerGamePacketListenerImpl.handleSetCreativeModeSlot, ClickEvent.ShowDialog.CODEC
func TestCreativeDialogNamesSurviveMovingAndReload(t *testing.T) {
	t.Parallel()
	cfg := servertest.BlockTestConfig()
	cfg.ViewDistance, cfg.SimulationDistance = config.MinDistance, config.MinDistance
	srv, c := servertest.Join(t, cfg, "Actor", servertest.OpsListing(t, "Actor"))
	servertest.HoldTicks(t, srv)
	var want []protocol.ItemStack
	for i, dialog := range []string{`{type:"notice",title:"Title"}`, `"minecraft:server_links"`} {
		name, err := nbt.ParseSNBT(`{text:"Named",click_event:{action:"show_dialog",dialog:` + dialog + `}}`)
		if err != nil {
			t.Fatal(err)
		}
		c.ChatCommand(`give @s stone[custom_name=` + nbt.Stringify(name) + `]`)
		c.ExpectWithinTicks(srv, 0, protocol.IDPlayContainerSlot)
		c.ExpectWithinTicks(srv, 0, protocol.IDPlaySystemChat)
		stack := servertest.ItemStack(t, "minecraft:stone", 1)
		stack.CustomName = &protocol.Text{Raw: name}
		want = append(want, stack)
		c.SetCreativeSlot(36, protocol.ItemStack{})
		c.SetCreativeSlot(int16(37+i), stack)
		c.ChatCommand(fmt.Sprintf(`data get entity @s Inventory[{Slot:%db}].components."minecraft:custom_name"`, i+1))
		data := c.ExpectWithinTicks(srv, 0, protocol.IDPlaySystemChat)
		tag, _, err := nbt.DecodeNetwork(data)
		if err != nil || !strings.Contains(nbt.Stringify(tag), "show_dialog") {
			t.Fatalf("creative move lost the dialog name: %s, %v", nbt.Stringify(tag), err)
		}
	}
	bad := protocol.Text{Raw: nbt.Compound{
		"text": nbt.String("Bad"),
		"click_event": nbt.Compound{
			"action": nbt.String("show_dialog"),
			"dialog": nbt.Compound{"type": nbt.String("notice")},
		},
	}}
	stack := servertest.ItemStack(t, "minecraft:stone", 1)
	stack.CustomName = &bad
	c.SetCreativeSlot(39, stack)
	r := protocol.NewReader(c.ExpectWithinTicks(srv, 0, protocol.IDPlayContainerSlot))
	r.ReadVarInt() // container
	r.ReadVarInt() // state
	slot := r.ReadI16()
	if got, ok := r.ReadSlot(); !ok || slot != 39 || !got.Empty() || r.Err() != nil {
		t.Fatalf("malformed dialog slot correction = %d, %+v, %v, %v", slot, got, ok, r.Err())
	}
	c.Disconnect()
	srv.Shutdown()

	_, reconnect := servertest.Start(t, cfg)
	again := reconnect()
	again.Login("Actor")
	again.Configure()
	slots := again.Inventory()
	for i, stack := range want {
		if !slots[37+i].Equal(stack) {
			t.Errorf("reloaded slot %d changed the dialog stack: %+v", 37+i, slots[37+i])
		}
	}
	again.Disconnect()
}
