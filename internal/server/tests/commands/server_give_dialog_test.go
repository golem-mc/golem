package commands

import (
	"reflect"
	"strings"
	"testing"

	"github.com/golem-mc/golem/internal/config"
	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/servertest"
	"github.com/golem-mc/golem/nbt"
	"github.com/golem-mc/golem/registry"
)

// vanilla: ClickEvent.ShowDialog.CODEC, ItemStack.getDisplayName
func TestGiveKeepsDialogClicksInCustomNames(t *testing.T) {
	t.Parallel()
	cfg := config.Default()
	cfg.OnlineMode = false
	cfg.ViewDistance, cfg.SimulationDistance = config.MinDistance, config.MinDistance
	cfg.GameMode = "creative"
	srv, c := servertest.Join(t, cfg, "Actor", servertest.OpsListing(t, "Actor"))
	servertest.HoldTicks(t, srv)

	for _, name := range []string{
		`{text:"Named",color:"green",hover_event:{action:"show_item",id:"player_head",components:{profile:{name:"Actor"}}},click_event:{action:"show_dialog",dialog:{type:"notice",title:"Title"}}}`,
		`{text:"Registered",click_event:{action:"show_dialog",dialog:"minecraft:server_links"}}`,
		`{text:"Nested",extra:[{text:" dialog",click_event:{action:"show_dialog",dialog:{type:"notice",title:"Nested title"}}}]}`,
	} {
		c.ChatCommand("give @s stone[custom_name=" + name + "]")
		r := protocol.NewReader(c.ExpectWithinTicks(srv, 0, protocol.IDPlayContainerSlot))
		r.ReadVarInt() // container
		r.ReadVarInt() // state
		r.ReadI16()    // slot
		if count, item, added, removed := r.ReadVarInt(), r.ReadVarInt(), r.ReadVarInt(), r.ReadVarInt(); count != 1 || item != int32(registry.ItemStone) || added != 1 || removed != 0 {
			t.Fatalf("slot header = %d, %d, %d, %d", count, item, added, removed)
		}
		if component := r.ReadVarInt(); component != protocol.DataComponentCustomName {
			t.Fatalf("component = %d, want custom_name", component)
		}
		// Read the tag directly to check the complete event and hover payload.
		gotName, _, decodeErr := nbt.DecodeNetwork(r.Rest())
		want, err := nbt.ParseSNBT(name)
		if err != nil {
			t.Fatal(err)
		}
		if decodeErr != nil || !reflect.DeepEqual(gotName, want) {
			t.Fatalf("given name = %v, %v, want %s", gotName, decodeErr, name)
		}
		feedback, _, err := nbt.DecodeNetwork(c.ExpectWithinTicks(srv, 0, protocol.IDPlaySystemChat))
		if err != nil {
			t.Fatal(err)
		}
		// The displayed name inherits its click through the italic wrapper;
		// the show-item tooltip must also carry the original component.
		got := nbt.Stringify(feedback)
		if !strings.Contains(got, "commands.give.success.single") || strings.Count(got, "show_dialog") != 2 {
			t.Fatalf("feedback lost the name's dialog click: %s", got)
		}
	}
	c.Disconnect()
}

// vanilla: Dialog.CODEC, NoticeDialog.MAP_CODEC, CommonDialogData.MAP_CODEC
func TestGiveRefusesMalformedDialogClicks(t *testing.T) {
	t.Parallel()
	cfg := config.Default()
	cfg.OnlineMode = false
	cfg.ViewDistance, cfg.SimulationDistance = config.MinDistance, config.MinDistance
	cfg.GameMode = "creative"
	srv, c := servertest.Join(t, cfg, "Actor", servertest.OpsListing(t, "Actor"))
	servertest.HoldTicks(t, srv)
	for _, click := range []string{
		`{action:"show_dialog"}`,
		`{action:"show_dialog",dialog:"minecraft:missing"}`,
		`{action:"show_dialog",dialog:{type:"notice"}}`,
		`{action:"show_dialog",dialog:{type:"missing",title:"Title"}}`,
		`{action:"show_dialog",dialog:{type:"notice",title:5}}`,
		`{action:"show_dialog",dialog:{type:"notice",title:"Title",action:{label:"Bad",width:0}}}`,
	} {
		c.ChatCommand(`give @s stone[custom_name={text:"Bad",click_event:` + click + `}]`)
		data := c.ExpectWithinTicks(srv, 0, protocol.IDPlaySystemChat)
		tag, _, err := nbt.DecodeNetwork(data)
		if err != nil || !strings.Contains(nbt.Stringify(tag), "arguments.item.component.malformed") {
			t.Fatalf("%s: refusal = %v, %v", click, tag, err)
		}
		c.ExpectNoneWithinTicks(srv, 0, servertest.PacketIs(protocol.IDPlayContainerSlot))
	}
	c.Disconnect()
}
