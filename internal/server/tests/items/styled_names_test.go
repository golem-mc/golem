package items

import (
	"testing"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/servertest"
	"github.com/golem-mc/golem/nbt"
)

// TestStyledItemNamesKeepTheirStyle checks that a name's style the Text model
// has no field for, and a false italic that overrides an item name's default
// italic, reach the client as typed.
// vanilla: Style.Serializer.MAP_CODEC
func TestStyledItemNamesKeepTheirStyle(t *testing.T) {
	t.Parallel()
	cfg := servertest.BlockTestConfig()
	_, connect := servertest.Start(t, cfg, servertest.WriteOpsFile(t,
		servertest.OpRecord{UUID: protocol.OfflineUUID("Smith").String(), Name: "Smith", Level: 2}))
	c := connect()
	c.Login("Smith")
	c.Configure()
	c.Inventory()
	c.EnterWorld("Smith")

	c.ChatCommand(`give @s diamond[custom_name={text:"Coin",italic:0s}]`)
	updates, _ := servertest.AwaitSlotUpdates(t, c, 1)
	if name := updates[0].Stack.CustomName; name == nil {
		t.Errorf("the coin has no name: %+v", updates[0])
	} else if tag, ok := nbt.CompoundView(protocol.TextTag(*name)); !ok || tag["italic"] != nbt.Short(0) {
		t.Errorf("the coin's name = %#v, want italic:0s kept", protocol.TextTag(*name))
	}

	c.ChatCommand(`give @s diamond_sword[custom_name={text:"Excalibur",bold:1}]`)
	updates, _ = servertest.AwaitSlotUpdates(t, c, 1)
	if name := updates[0].Stack.CustomName; name == nil {
		t.Errorf("the sword has no name: %+v", updates[0])
	} else if tag, ok := nbt.CompoundView(protocol.TextTag(*name)); !ok || tag["bold"] != nbt.Int(1) {
		t.Errorf("the sword's name = %#v, want bold:1", protocol.TextTag(*name))
	}
	c.Disconnect()
}
