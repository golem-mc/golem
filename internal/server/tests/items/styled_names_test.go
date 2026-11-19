package items

import (
	"testing"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/servertest"
	"github.com/golem-mc/golem/nbt"
	"github.com/golem-mc/golem/registry"
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

// TestBoldNamedSwordSurvivesAStorage checks that a creative slot accepts a
// bold-named sword and a chest saves and reloads it, name and all.
func TestBoldNamedSwordSurvivesAStorage(t *testing.T) {
	t.Parallel()
	cfg := servertest.BlockTestConfig()
	cfg.LevelName = t.TempDir()

	sword, ok := registry.ItemID("minecraft:diamond_sword")
	if !ok {
		t.Fatal("the registry has no diamond_sword")
	}
	name, ok := protocol.RawTextOf(nbt.Compound{"text": nbt.String("Excalibur"), "bold": nbt.Int(1)})
	if !ok {
		t.Fatal("RawTextOf refuses the bold name")
	}
	named := protocol.ItemStack{ItemID: sword, Count: 1, CustomName: &name}

	srv, w := servertest.OpenWindowed(t, cfg)
	chest, ok := registry.ItemID("minecraft:chest")
	if !ok {
		t.Fatal("the registry has no chest")
	}
	w.SetCreativeSlot(36, protocol.ItemStack{ItemID: chest, Count: 1})
	w.PlaceBlock(4, 63, 0, protocol.FaceTop)
	w.ExpectBlockAck()
	w.UseBlock(4, 64, 0)
	w.ExpectContainerOpen(protocol.MenuGeneric9x3, true, 4, 64, 0)

	w.SetCreativeSlot(36, named)
	w.Click(54, 0, protocol.ClickModePickup)
	w.ExpectClickResult(map[int16]protocol.ItemStack{54: {}}, named)
	w.Click(0, 0, protocol.ClickModePickup)
	w.ExpectClickResult(map[int16]protocol.ItemStack{0: named}, protocol.ItemStack{})
	w.CloseWindow()
	w.ExpectWindowZero()

	srv.FlushSaves()
	srv.Shutdown()
	restarted := *cfg
	srv2, connect := servertest.Start(t, &restarted)
	c := connect()
	c.Login("Testificate")
	c.Configure()
	c.Inventory()
	c.EnterWorld("Testificate")

	got := srv2.InventoryAt(4, 64, 0)
	if len(got) == 0 || !got[0].Equal(named) {
		t.Errorf("the reloaded chest = %v, want the bold-named sword", got)
	}
	c.Disconnect()
}
