package storage

import (
	"github.com/golem-mc/golem/internal/server/player"
	"github.com/golem-mc/golem/internal/server/servertest"
	"maps"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"testing"

	"github.com/golem-mc/golem/internal/config"
	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/world"
	"github.com/golem-mc/golem/nbt"
	"github.com/golem-mc/golem/registry"
)

// TestInventorySurvivesRejoining checks that what a creative player puts in
// their inventory is still there when they come back, and that it reached disk
// in the shape a vanilla server writes.
func TestInventorySurvivesRejoining(t *testing.T) {
	t.Parallel()
	cfg := config.Default()
	cfg.GameMode = "creative" // items are handed out through the creative inventory
	cfg.OnlineMode = false
	cfg.LevelName = t.TempDir()

	diamond, ok := registry.ItemID("minecraft:diamond")
	if !ok {
		t.Fatal("the item registry has no diamond")
	}
	stone, _ := registry.ItemID("minecraft:stone")

	_, connect := servertest.Start(t, cfg)

	c := connect()
	c.Login("Testificate")
	c.Configure()

	// A new player carries nothing.
	for slot, stack := range c.Inventory() {
		if !stack.Empty() {
			t.Errorf("a new player has %+v in slot %d", stack, slot)
		}
	}
	c.EnterWorld("Testificate")

	// Slot 36 is the first hotbar slot, slot 9 the first of the main inventory.
	c.SetCreativeSlot(36, protocol.ItemStack{ItemID: diamond, Count: 5})
	c.SetCreativeSlot(9, protocol.ItemStack{ItemID: stone, Count: 64})
	c.Send(protocol.IDPlayHeldItemIn, func(b *protocol.Buffer) { b.PutI16(3) })
	c.Disconnect()

	// On disk it is a list of the occupied slots, each naming its item, with
	// the hotbar numbered from zero the way a player file numbers it.
	root := servertest.ReadPlayerFile(t, servertest.PlayerFile(cfg.LevelName, "Testificate"))
	items, ok := nbt.Get[nbt.List](root, "Inventory")
	if !ok || len(items.Items) != 2 {
		t.Fatalf("Inventory = %v, %v; want the two slots that hold something", items, ok)
	}
	want := []struct {
		slot  nbt.Byte
		id    nbt.String
		count nbt.Int
	}{
		{0, "minecraft:diamond", 5},
		{9, "minecraft:stone", 64},
	}
	for i, w := range want {
		entry, ok := items.Items[i].(nbt.Compound)
		if !ok {
			t.Fatalf("item %d is a %s, want a compound", i, items.Items[i].Type())
		}
		if got, _ := nbt.Get[nbt.Byte](entry, "Slot"); got != w.slot {
			t.Errorf("item %d Slot = %v, want %v", i, got, w.slot)
		}
		if got, _ := nbt.Get[nbt.String](entry, "id"); got != w.id {
			t.Errorf("item %d id = %v, want %v", i, got, w.id)
		}
		if got, _ := nbt.Get[nbt.Int](entry, "count"); got != w.count {
			t.Errorf("item %d count = %v, want %v", i, got, w.count)
		}
	}
	if got, ok := nbt.Get[nbt.Int](root, "SelectedItemSlot"); !ok || got != 3 {
		t.Errorf("SelectedItemSlot = %v, %v; want 3", got, ok)
	}

	// And the next connection gets it all back.
	again := connect()
	again.Login("Testificate")
	again.Configure()

	// The hotbar selection comes first, where the vanilla server sends it.
	if got := protocol.NewReader(again.AwaitPlay(protocol.IDPlayHeldItem)).ReadVarInt(); got != 3 {
		t.Errorf("held hotbar slot = %d, want 3", got)
	}

	slots := again.Inventory()
	if len(slots) != 46 {
		t.Fatalf("inventory has %d slots, want 46", len(slots))
	}
	if got := slots[36]; got.ItemID != diamond || got.Count != 5 {
		t.Errorf("slot 36 = %+v, want 5 diamonds (item %d)", got, diamond)
	}
	if got := slots[9]; got.ItemID != stone || got.Count != 64 {
		t.Errorf("slot 9 = %+v, want 64 stone (item %d)", got, stone)
	}
	for slot, stack := range slots {
		if slot != 36 && slot != 9 && !stack.Empty() {
			t.Errorf("slot %d holds %+v, want nothing", slot, stack)
		}
	}
}

// TestInventoryFallsBackToTheBackup checks the crash window between the two
// renames in a save: with only the backup left on disk, the returning player
// gets the inventory it holds rather than an empty one.
func TestInventoryFallsBackToTheBackup(t *testing.T) {
	t.Parallel()
	cfg := config.Default()
	cfg.GameMode = "creative" // items are handed out through the creative inventory
	cfg.OnlineMode = false
	cfg.LevelName = t.TempDir()

	diamond, ok := registry.ItemID("minecraft:diamond")
	if !ok {
		t.Fatal("the item registry has no diamond")
	}

	_, connect := servertest.Start(t, cfg)

	c := connect().Join("Testificate")
	c.SetCreativeSlot(36, protocol.ItemStack{ItemID: diamond, Count: 5})
	c.Disconnect()

	// The current file is gone and only the previous save is left: what a
	// crash between rotating the backup out and the temp file in leaves.
	dat := servertest.PlayerFile(cfg.LevelName, "Testificate")
	if err := os.Rename(dat, dat+"_old"); err != nil {
		t.Fatalf("leave only the backup: %v", err)
	}

	again := connect()
	again.Login("Testificate")
	again.Configure()

	if got := again.Inventory()[36]; got.ItemID != diamond || got.Count != 5 {
		t.Errorf("slot 36 = %+v, want the 5 diamonds the backup holds", got)
	}
	again.Disconnect()
}

// TestCorruptPlayerFileKeepsACopy checks that a damaged current file is kept
// under its timestamped corrupted name when the backup stands in, so the
// evidence of the damage survives the next save.
func TestCorruptPlayerFileKeepsACopy(t *testing.T) {
	t.Parallel()
	cfg := config.Default()
	cfg.GameMode = "creative" // items are handed out through the creative inventory
	cfg.OnlineMode = false
	cfg.LevelName = t.TempDir()

	diamond, ok := registry.ItemID("minecraft:diamond")
	if !ok {
		t.Fatal("the item registry has no diamond")
	}

	_, connect := servertest.Start(t, cfg)

	c := connect().Join("Testificate")
	c.SetCreativeSlot(36, protocol.ItemStack{ItemID: diamond, Count: 5})
	c.Disconnect()

	// A good previous save beside a current file cut short, what an
	// interrupted write leaves.
	dat := servertest.PlayerFile(cfg.LevelName, "Testificate")
	if err := os.Rename(dat, dat+"_old"); err != nil {
		t.Fatalf("leave a backup: %v", err)
	}
	if err := os.WriteFile(dat, []byte("half a file"), 0o644); err != nil {
		t.Fatalf("corrupt the current file: %v", err)
	}

	again := connect()
	again.Login("Testificate")
	again.Configure()

	// The player is loaded from the backup, and the damaged file is kept
	// rather than overwritten or deleted.
	if got := again.Inventory()[36]; got.ItemID != diamond || got.Count != 5 {
		t.Errorf("slot 36 = %+v, want the 5 diamonds the backup holds", got)
	}
	matches, err := filepath.Glob(filepath.Join(filepath.Dir(dat), "*_corrupted_*"))
	if err != nil {
		t.Fatalf("glob corrupted copies: %v", err)
	}
	if len(matches) != 1 {
		t.Errorf("found %d corrupted copies, want the damaged file kept as one: %v", len(matches), matches)
	}
	again.Disconnect()
}

// TestUnmodelledComponentsDoNotRideAFile checks what the file keeps of a
// raw-held component it has no NBT shape for: nothing. The value is wire
// bytes full of registry IDs, which a file cannot name, so saving it would
// tie the file to this version; the painting the player put away comes back
// the plain named item the file could describe.
func TestUnmodelledComponentsDoNotRideAFile(t *testing.T) {
	t.Parallel()
	cfg := config.Default()
	cfg.GameMode = "creative" // items are handed out through the creative inventory
	cfg.OnlineMode = false
	cfg.LevelName = t.TempDir()

	painting, ok := registry.ItemID("minecraft:painting")
	if !ok {
		t.Fatal("the item registry has no painting")
	}
	// The stack a vanilla client sends for a painting off the creative menu:
	// a custom name this server models, and the painting's variant,
	// component 109, here the inline variant (wire id 0, width, height, asset
	// id, no title, no author), which is no registry reference and so has no
	// NBT shape. The value travels length-prefixed, the untrusted slot
	// shape's way.
	contents := append([]byte{0x00, 0x01, 0x01, byte(len("minecraft:kebab"))}, append([]byte("minecraft:kebab"), 0x00, 0x00)...)
	_, connect := servertest.Start(t, cfg)
	c := connect()
	c.Login("Testificate")
	c.Configure()
	c.Inventory()
	c.EnterWorld("Testificate")
	c.Send(protocol.IDPlayCreativeSlot, func(b *protocol.Buffer) {
		b.PutI16(36)
		b.PutVarInt(1)        // count
		b.PutVarInt(painting) // minecraft:painting
		b.PutVarInt(2)        // components to add
		b.PutVarInt(0)        // components to remove
		b.PutVarInt(protocol.DataComponentCustomName)
		b.PutVarInt(6) // the value's byte length
		b.PutU8(0x08)  // a string tag
		b.PutI16(3)
		b.PutRaw([]byte("Bob"))
		b.PutVarInt(protocol.DataComponentPaintingVariant)
		b.PutVarInt(int32(len(contents)))
		b.PutRaw(contents)
	})
	c.Disconnect()

	// The file names the modelled component the usual way and has nothing
	// beside it for the raw one.
	root := servertest.ReadPlayerFile(t, servertest.PlayerFile(cfg.LevelName, "Testificate"))
	items, _ := nbt.Get[nbt.List](root, "Inventory")
	if len(items.Items) != 1 {
		t.Fatalf("Inventory holds %d stacks, want the one that was kept", len(items.Items))
	}
	entry := items.Items[0].(nbt.Compound)
	components, _ := nbt.Get[nbt.Compound](entry, "components")
	if _, ok := components["minecraft:custom_name"]; !ok {
		t.Errorf("the file's components = %v, want the name the stack carries", components)
	}
	if _, ok := entry["phm07:raw_components"]; ok {
		t.Error("the raw component was written under the server's own key")
	}

	// And the next connection gets the stack back without the variant: the
	// plain named painting the file could describe.
	again := connect()
	again.Login("Testificate")
	again.Configure()
	name := protocol.Text{Text: "Bob"}
	want := protocol.ItemStack{ItemID: painting, Count: 1, CustomName: &name}
	if got := again.Inventory()[36]; !got.Equal(want) {
		t.Errorf("slot 36 = %+v, want the painting the file could describe", got)
	}
	again.Disconnect()
}

// TestEquipmentSurvivesRejoining checks the whole of what a player wearing
// armour gets: putting it on reaches disk, and it is still on when they come
// back. It was being dropped on every save, since a file keeps worn items apart
// from the rest of the inventory.
func TestEquipmentSurvivesRejoining(t *testing.T) {
	t.Parallel()
	cfg := config.Default()
	cfg.GameMode = "creative" // items are handed out through the creative inventory
	cfg.OnlineMode = false
	cfg.LevelName = t.TempDir()

	helmet, ok := registry.ItemID("minecraft:diamond_helmet")
	if !ok {
		t.Fatal("the item registry has no diamond helmet")
	}
	boots, _ := registry.ItemID("minecraft:golden_boots")
	shield, _ := registry.ItemID("minecraft:shield")

	_, connect := servertest.Start(t, cfg)

	c := connect()
	c.Login("Testificate")
	c.Configure()
	c.Inventory()
	c.EnterWorld("Testificate")

	// Slots 5 to 8 are the armour, head first, and 45 the off hand. This is
	// what a creative client sends when armour is dragged onto those slots.
	c.SetCreativeSlot(5, protocol.ItemStack{ItemID: helmet, Count: 1})
	c.SetCreativeSlot(8, protocol.ItemStack{ItemID: boots, Count: 1})
	c.SetCreativeSlot(45, protocol.ItemStack{ItemID: shield, Count: 1})
	c.Disconnect()

	// On disk it is the equipment tag, named rather than numbered.
	root := servertest.ReadPlayerFile(t, servertest.PlayerFile(cfg.LevelName, "Testificate"))
	equipment, ok := nbt.Get[nbt.Compound](root, "equipment")
	if !ok {
		t.Fatal("nothing was saved to the equipment tag")
	}
	for name, want := range map[string]string{
		"head":    "minecraft:diamond_helmet",
		"feet":    "minecraft:golden_boots",
		"offhand": "minecraft:shield",
	} {
		entry, ok := nbt.Get[nbt.Compound](equipment, name)
		if !ok {
			t.Errorf("equipment has no %s", name)
			continue
		}
		if id, _ := nbt.Get[nbt.String](entry, "id"); string(id) != want {
			t.Errorf("equipment %s = %v, want %s", name, id, want)
		}
	}

	// And it is still being worn on the way back in.
	again := connect()
	again.Login("Testificate")
	again.Configure()

	slots := again.Inventory()
	if got := slots[5]; got.ItemID != helmet || got.Count != 1 {
		t.Errorf("head slot = %+v, want the diamond helmet", got)
	}
	if got := slots[8]; got.ItemID != boots || got.Count != 1 {
		t.Errorf("feet slot = %+v, want the golden boots", got)
	}
	if got := slots[45]; got.ItemID != shield || got.Count != 1 {
		t.Errorf("off hand = %+v, want the shield", got)
	}
	for _, empty := range []int{6, 7} {
		if !slots[empty].Empty() {
			t.Errorf("slot %d holds %+v, want nothing", empty, slots[empty])
		}
	}
}

// TestArmourIsEquippedByUsingIt checks the other way armour goes on: with it in
// hand, right-clicking. The client only reports the use and waits to be told
// what became of it, so a server that ignores it leaves the armour in the
// player's hand - and, once they log out, nowhere at all.
//
// The behaviour is what a vanilla server was seen to do with the same
// packet: the helmet left the hotbar and turned up in the equipment tag.
func TestArmourIsEquippedByUsingIt(t *testing.T) {
	t.Parallel()
	cfg := config.Default()
	cfg.GameMode = "creative" // items are handed out through the creative inventory
	cfg.OnlineMode = false
	cfg.LevelName = t.TempDir()

	helmet, _ := registry.ItemID("minecraft:diamond_helmet")
	shield, _ := registry.ItemID("minecraft:shield")

	_, connect := servertest.Start(t, cfg)

	c := connect()
	c.Login("Testificate")
	c.Configure()
	c.Inventory()
	c.EnterWorld("Testificate")

	// A helmet in the first hotbar slot, which is the one selected.
	c.SetCreativeSlot(36, protocol.ItemStack{ItemID: helmet, Count: 1})
	c.Send(protocol.IDPlayUseItem, func(b *protocol.Buffer) {
		b.PutVarInt(protocol.HandMain)
		b.PutVarInt(1) // sequence
		b.PutF32(0)    // yaw
		b.PutF32(0)    // pitch
	})

	// Both ends of the swap come back: a creative player keeps a copy in hand.
	for _, want := range []struct {
		slot  int16
		stack protocol.ItemStack
	}{
		{36, protocol.ItemStack{ItemID: helmet, Count: 1}},
		{5, protocol.ItemStack{ItemID: helmet, Count: 1}},
	} {
		r := protocol.NewReader(c.ExpectPlay(protocol.IDPlayContainerSlot))
		r.ReadVarInt() // window ID
		r.ReadVarInt() // state ID
		if got := r.ReadI16(); got != want.slot {
			t.Errorf("corrected slot = %d, want %d", got, want.slot)
		}
		got, ok := r.ReadSlot()
		if !ok {
			t.Fatalf("slot %d did not read back", want.slot)
		}
		if want.stack.Empty() {
			if !got.Empty() {
				t.Errorf("slot %d = %+v, want nothing", want.slot, got)
			}
		} else if !got.Equal(want.stack) {
			t.Errorf("slot %d = %+v, want %+v", want.slot, got, want.stack)
		}
	}

	// A shield names the off hand but is not swappable, so using it raises it
	// rather than stowing it: the slot must not change.
	c.SetCreativeSlot(36, protocol.ItemStack{ItemID: shield, Count: 1})
	c.Send(protocol.IDPlayUseItem, func(b *protocol.Buffer) {
		b.PutVarInt(protocol.HandMain)
		b.PutVarInt(2)
		b.PutF32(0)
		b.PutF32(0)
	})

	// Nothing is sent back for a use that changed nothing, so the next thing to
	// arrive is the answer to this.
	c.Settle()
	c.Disconnect()

	root := servertest.ReadPlayerFile(t, servertest.PlayerFile(cfg.LevelName, "Testificate"))
	equipment, ok := nbt.Get[nbt.Compound](root, "equipment")
	if !ok {
		t.Fatal("the helmet was not saved as equipment")
	}
	head, ok := nbt.Get[nbt.Compound](equipment, "head")
	if !ok {
		t.Fatal("equipment has no head")
	}
	if id, _ := nbt.Get[nbt.String](head, "id"); string(id) != "minecraft:diamond_helmet" {
		t.Errorf("head = %v, want the diamond helmet", id)
	}
	if _, ok := equipment["offhand"]; ok {
		t.Error("the shield was stowed in the off hand, but it is not swappable")
	}

	// And the shield is still in the hand it was used from.
	items, _ := nbt.Get[nbt.List](root, "Inventory")
	if len(items.Items) != 1 {
		t.Fatalf("Inventory holds %d stacks, want the shield", len(items.Items))
	}
	if id, _ := nbt.Get[nbt.String](items.Items[0].(nbt.Compound), "id"); string(id) != "minecraft:shield" {
		t.Errorf("Inventory holds %v, want the shield", id)
	}
}

// TestVanillaEquipmentIsRead checks this server against armour a real vanilla
// server wrote: it is handed back to the player in the right slots, and
// still in the file afterwards.
func TestVanillaEquipmentIsRead(t *testing.T) {
	t.Parallel()
	cfg := config.Default()
	cfg.OnlineMode = false
	cfg.LevelName = t.TempDir()

	// The fixture is the file a vanilla server wrote for the offline player
	// "Knight", wearing a diamond helmet, an iron chestplate, leather leggings
	// and golden boots, with a shield in the off hand.
	const name = "Knight"
	before := servertest.InstallPlayerFixture(t, cfg, name, "vanilla-equipment.dat")
	saved := servertest.PlayerFile(cfg.LevelName, name)

	// The file was written after the player died: left as it is, the join
	// drops the chunk holds on its first tick and sends no chunks. Revive
	// the player, which leaves the equipment as the file has it.
	revived := maps.Clone(before)
	revived["Health"] = nbt.Float(20)
	revived["DeathTime"] = nbt.Short(0)
	servertest.WritePlayerFile(t, saved, revived)

	_, connect := servertest.Start(t, cfg)
	c := connect()
	c.Login(name)
	c.Configure()

	slots := c.Inventory()
	for _, want := range []struct {
		slot int
		item string
	}{
		{5, "minecraft:diamond_helmet"},
		{6, "minecraft:iron_chestplate"},
		{7, "minecraft:leather_leggings"},
		{8, "minecraft:golden_boots"},
		{45, "minecraft:shield"},
	} {
		id, ok := registry.ItemID(want.item)
		if !ok {
			t.Fatalf("the item registry has no %s", want.item)
		}
		if got := slots[want.slot]; got.ItemID != id || got.Count != 1 {
			t.Errorf("slot %d = %+v, want one %s (item %d)", want.slot, got, want.item, id)
		}
	}

	c.EnterWorld(name)
	c.Disconnect()

	after := servertest.ReadPlayerFile(t, saved)
	want, _ := nbt.Get[nbt.Compound](before, "equipment")
	got, ok := nbt.Get[nbt.Compound](after, "equipment")
	if !ok {
		t.Fatal("the equipment tag was dropped")
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("equipment =\n%v\nwant\n%v", got, want)
	}
}

// TestVanillaInventoryIsRead checks this server against an inventory a real
// vanilla server wrote: the stacks it can model are handed to the player,
// and the ones it cannot are still in the file after the player has been here.
func TestVanillaInventoryIsRead(t *testing.T) {
	t.Parallel()
	cfg := config.Default()
	cfg.OnlineMode = false
	cfg.LevelName = t.TempDir()

	// The fixture is the file a vanilla server wrote for the offline player
	// "Packrat", who was given five diamonds, seventeen oak logs, two stone
	// renamed "Bob", and a potion of healing.
	const name = "Packrat"
	before := servertest.InstallPlayerFixture(t, cfg, name, "vanilla-inventory.dat")
	saved := servertest.PlayerFile(cfg.LevelName, name)

	_, connect := servertest.Start(t, cfg)
	c := connect()
	c.Login(name)
	c.Configure()

	// Every stack arrives, in the hotbar slots the file's slots 0 to 3 mean:
	// the renamed stone with its name, the potion with the contents the file
	// named in NBT.
	diamond, _ := registry.ItemID("minecraft:diamond")
	oakLog, _ := registry.ItemID("minecraft:oak_log")
	stone, _ := registry.ItemID("minecraft:stone")
	potion, _ := registry.ItemID("minecraft:potion")

	slots := c.Inventory()
	if got := slots[36]; got.ItemID != diamond || got.Count != 5 {
		t.Errorf("slot 36 = %+v, want 5 diamonds", got)
	}
	if got := slots[37]; got.ItemID != oakLog || got.Count != 17 {
		t.Errorf("slot 37 = %+v, want 17 oak logs", got)
	}
	if got := slots[38]; got.ItemID != stone || got.Count != 2 || got.CustomName == nil || got.CustomName.Text != "Bob" {
		t.Errorf("slot 38 = %+v, want two stone named Bob", got)
	}
	if got := slots[39]; got.ItemID != potion || got.Count != 1 {
		t.Errorf("slot 39 = %+v, want the potion of healing", got)
	}

	c.EnterWorld(name)
	c.Disconnect()

	// And the whole inventory is still in the file, components and all.
	after := servertest.ReadPlayerFile(t, saved)
	want, _ := nbt.Get[nbt.List](before, "Inventory")
	got, ok := nbt.Get[nbt.List](after, "Inventory")
	if !ok || len(got.Items) != len(want.Items) {
		t.Fatalf("Inventory holds %v, want the %d stacks it was loaded with", got, len(want.Items))
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("Inventory =\n%v\nwant\n%v", got, want)
	}
}

// TestOlderPlayerFileVersionRidesThrough checks that a player file older than
// this server is saved as still being that version: it keeps its own
// DataVersion, and the items it holds keep that version's shape — the Count
// and tag a stack carried before 1.20.5 gave them components — rather than
// being restamped current over them, which would leave the game reading them
// as count-one stacks and skipping its item fixers for good.
// vanilla: PlayerDataStorage.load runs those fixers on the file's own
// DataVersion before anything reads it.
func TestOlderPlayerFileVersionRidesThrough(t *testing.T) {
	t.Parallel()
	cfg := config.Default()
	cfg.OnlineMode = false
	cfg.LevelName = t.TempDir()

	const name = "Oldtimer"
	items := nbt.List{Elem: nbt.TypeCompound, Items: []nbt.Tag{
		nbt.Compound{
			"Slot":  nbt.Byte(0),
			"id":    nbt.String("minecraft:diamond_sword"),
			"Count": nbt.Byte(3),
			"tag": nbt.Compound{
				"Enchantments": nbt.List{Elem: nbt.TypeCompound, Items: []nbt.Tag{
					nbt.Compound{"id": nbt.String("minecraft:sharpness"), "lvl": nbt.Short(5)},
				}},
				"Damage": nbt.Int(10),
			},
		},
	}}
	ender := nbt.List{Elem: nbt.TypeCompound, Items: []nbt.Tag{
		nbt.Compound{
			"Slot":  nbt.Byte(1),
			"id":    nbt.String("minecraft:diamond"),
			"Count": nbt.Byte(5),
		},
	}}
	servertest.WritePlayerFile(t, servertest.PlayerFile(cfg.LevelName, name), nbt.Compound{
		"DataVersion": nbt.Int(3700), // 1.20.4, the last release before items grew components
		"Inventory":   items,
		"EnderItems":  ender,
	})

	_, c := servertest.Join(t, cfg, name)
	c.Disconnect()

	root := servertest.ReadPlayerFile(t, servertest.PlayerFile(cfg.LevelName, name))
	if version, ok := nbt.Get[nbt.Int](root, "DataVersion"); !ok || version != 3700 {
		t.Errorf("DataVersion = %v, %v; want 3700, the claim the file rode in with", version, ok)
	}
	for field, want := range map[string]nbt.List{"Inventory": items, "EnderItems": ender} {
		got, ok := nbt.Get[nbt.List](root, field)
		if !ok || !reflect.DeepEqual(got, want) {
			t.Errorf("%s =\n%v\nwant\n%v", field, got, want)
		}
	}
}

// TestOlderPlayerFileVersionKeepsWornEquipment checks a file that claims a
// version before PlayerEquipmentFix (4312): worn items ride in the Inventory
// list's numbered 100-103 and -106 slots rather than an equipment tag, and
// are saved back there, so the file's own fixer still builds the tag vanilla
// reads.
func TestOlderPlayerFileVersionKeepsWornEquipment(t *testing.T) {
	t.Parallel()
	cfg := config.Default()
	cfg.GameMode = "creative" // items are handed out through the creative inventory
	cfg.OnlineMode = false
	cfg.LevelName = t.TempDir()

	helmet, ok := registry.ItemID("minecraft:diamond_helmet")
	if !ok {
		t.Fatal("the item registry has no diamond helmet")
	}
	shield, _ := registry.ItemID("minecraft:shield")

	const name = "Knight"
	servertest.WritePlayerFile(t, servertest.PlayerFile(cfg.LevelName, name), nbt.Compound{
		"DataVersion": nbt.Int(3700), // 1.20.4, before worn items moved to the equipment tag
	})

	_, connect := servertest.Start(t, cfg)
	c := connect()
	c.Login(name)
	c.Configure()
	c.Inventory()
	c.EnterWorld(name)

	c.SetCreativeSlot(5, protocol.ItemStack{ItemID: helmet, Count: 1})
	c.SetCreativeSlot(45, protocol.ItemStack{ItemID: shield, Count: 1})
	c.Disconnect()

	root := servertest.ReadPlayerFile(t, servertest.PlayerFile(cfg.LevelName, name))
	if version, ok := nbt.Get[nbt.Int](root, "DataVersion"); !ok || version != 3700 {
		t.Errorf("DataVersion = %v, %v; want 3700", version, ok)
	}
	if _, ok := root["equipment"]; ok {
		t.Error("an old-claiming file was saved with the equipment tag")
	}
	slots := map[int8]string{}
	items, ok := nbt.Get[nbt.List](root, "Inventory")
	if !ok {
		t.Fatal("the worn items were not saved in the Inventory list")
	}
	for _, item := range items.Items {
		entry, ok := item.(nbt.Compound)
		if !ok {
			continue
		}
		slot, ok := nbt.Get[nbt.Byte](entry, "Slot")
		if !ok {
			continue
		}
		id, _ := nbt.Get[nbt.String](entry, "id")
		slots[int8(slot)] = string(id)
	}
	for slot, want := range map[int8]string{
		103:  "minecraft:diamond_helmet",
		-106: "minecraft:shield",
	} {
		if got := slots[slot]; got != want {
			t.Errorf("Inventory slot %d = %q, want %q", slot, got, want)
		}
	}

	// And the numbered slots come back onto the body.
	again := connect()
	again.Login(name)
	again.Configure()
	worn := again.Inventory()
	if got := worn[5]; got.ItemID != helmet || got.Count != 1 {
		t.Errorf("head slot = %+v, want the diamond helmet", got)
	}
	if got := worn[45]; got.ItemID != shield || got.Count != 1 {
		t.Errorf("off hand = %+v, want the shield", got)
	}
	again.Disconnect()
}

// TestPlayerPositionSurvivesRejoining checks the whole of what a player who
// leaves and comes back gets: their step is saved when their connection ends,
// and the next one starts where it stopped.
func TestPlayerPositionSurvivesRejoining(t *testing.T) {
	t.Parallel()
	cfg := config.Default()
	cfg.OnlineMode = false
	cfg.LevelName = t.TempDir()

	_, connect := servertest.Start(t, cfg)

	c := connect()
	c.Login("Testificate")
	c.Configure()

	// Nobody has played here, so the player starts at spawn.
	if got := c.SpawnedAt(); got.X != 0.5 || got.Z != 0.5 {
		t.Errorf("first join put the player at (%g, %g, %g), want spawn", got.X, got.Y, got.Z)
	}
	c.EnterWorld("Testificate")

	c.MoveTo(103.5, 70, -8.5, 90, -45)
	c.Disconnect()

	// The step reached disk, in the file and format the game uses.
	root := servertest.ReadPlayerFile(t, servertest.PlayerFile(cfg.LevelName, "Testificate"))
	pos, ok := nbt.Get[nbt.List](root, "Pos")
	if !ok || len(pos.Items) != 3 {
		t.Fatalf("Pos = %v, %v; want three doubles", pos, ok)
	}
	for i, want := range []nbt.Double{103.5, 70, -8.5} {
		if pos.Items[i] != want {
			t.Errorf("Pos[%d] = %v, want %v", i, pos.Items[i], want)
		}
	}
	if version, ok := nbt.Get[nbt.Int](root, "DataVersion"); !ok || version == 0 {
		t.Errorf("DataVersion = %v, %v; want the version that wrote the file", version, ok)
	}
	if got, ok := nbt.Get[nbt.Byte](root, "OnGround"); !ok || got != 1 {
		t.Errorf("OnGround = %v, %v; want 1", got, ok)
	}

	// And the next connection is put back there rather than at spawn.
	again := connect()
	again.Login("Testificate")
	again.Configure()

	got := again.SpawnedAt()
	if got.X != 103.5 || got.Y != 70 || got.Z != -8.5 {
		t.Errorf("rejoined at (%g, %g, %g), want (103.5, 70, -8.5)", got.X, got.Y, got.Z)
	}
	if got.Yaw != 90 || got.Pitch != -45 {
		t.Errorf("rejoined facing (%g, %g), want (90, -45)", got.Yaw, got.Pitch)
	}
}

// TestAirAndFireSurviveRejoining checks that the breath and the burn a player
// leaves with are written to their file and read back on the way in, so a
// nearly drowned or still burning body is not reset by a relog.
func TestAirAndFireSurviveRejoining(t *testing.T) {
	t.Parallel()
	cfg := config.Default()
	cfg.OnlineMode = false
	cfg.LevelName = t.TempDir()

	srv, connect := servertest.Start(t, cfg)
	// The environmental tick would refill the breath and burn the fire down
	// between the two sessions; the state is all this test is about.
	servertest.StopTicking(t, srv)

	c := connect().Join("Diver")

	// Nearly out of breath and still alight as they leave.
	srv.SetPlayerAir("Diver", 40)
	srv.SetPlayerFire("Diver", 200)
	c.Disconnect()

	root := servertest.ReadPlayerFile(t, servertest.PlayerFile(cfg.LevelName, "Diver"))
	if air, ok := nbt.Get[nbt.Short](root, "Air"); !ok || air != 40 {
		t.Errorf("saved Air = %v, %v; want 40, true", air, ok)
	}
	if fire, ok := nbt.Get[nbt.Short](root, "Fire"); !ok || fire != 200 {
		t.Errorf("saved Fire = %v, %v; want 200, true", fire, ok)
	}

	// The next connection comes back with the same breath and the same fire.
	again := connect().Join("Diver")

	if got := srv.PlayerAir("Diver"); got != 40 {
		t.Errorf("the breath after rejoining = %d, want the 40 it left with", got)
	}
	if got := srv.PlayerFire("Diver"); got != 200 {
		t.Errorf("the fire after rejoining = %d, want the 200 it left with", got)
	}
	again.Disconnect()
}

// TestAirKeptAsAnIntSurvivesRejoining checks the breath is read at whatever
// width the numeric tag is written, as Entity.load's getIntOr does: a file that
// keeps Air as an int is not ignored and reset to the full supply.
func TestAirKeptAsAnIntSurvivesRejoining(t *testing.T) {
	t.Parallel()
	cfg := config.Default()
	cfg.OnlineMode = false
	cfg.LevelName = t.TempDir()

	const name = "Breather"
	servertest.WritePlayerFile(t, servertest.PlayerFile(cfg.LevelName, name), nbt.Compound{
		"Air": nbt.Int(42),
	})

	srv, connect := servertest.Start(t, cfg)
	// A land tick would refill the breath before the check; the state read
	// back is all this test is about.
	servertest.StopTicking(t, srv)

	c := connect().Join(name)

	if got := srv.PlayerAir(name); got != 42 {
		t.Errorf("the breath from an Air kept as an int = %d, want 42", got)
	}
	c.Disconnect()
}

// TestAFallStartedBeforeRejoiningStillHurts checks that the fall a player has
// banked mid-air survives the relog: a body that quits on the way down and
// comes back must still take the landing, not reach the ground on the safe
// distance.
func TestAFallStartedBeforeRejoiningStillHurts(t *testing.T) {
	t.Parallel()
	cfg := config.Default()
	cfg.OnlineMode = false
	cfg.LevelName = t.TempDir()

	srv, connect := servertest.Start(t, cfg)
	// The fall is banked by the reported steps themselves; the tick loop would
	// only add noise around them.
	servertest.StopTicking(t, srv)

	c := connect().Join("Faller")

	// Up off the ground, then down thirteen blocks of it, held in the air.
	c.MoveToGround(0.5, 80, 0.5, false)
	c.MoveToGround(0.5, 72, 0.5, false)
	c.MoveToGround(0.5, 67, 0.5, false)
	c.Settle()
	banked := srv.PlayerFallDistance("Faller")
	if banked <= 3 {
		t.Fatalf("the fall carried into the relog = %v, want a fall past the safe three", banked)
	}
	c.Disconnect()

	root := servertest.ReadPlayerFile(t, servertest.PlayerFile(cfg.LevelName, "Faller"))
	if fall, ok := nbt.Get[nbt.Double](root, "fall_distance"); !ok || float64(fall) != banked {
		t.Fatalf("saved fall_distance = %v, %v; want the banked %v", fall, ok, banked)
	}

	// Back in mid-air with the fall carried, and down onto the stone: the
	// landing has to hurt as it would have before the relog.
	again := connect().Join("Faller")
	again.MoveToGround(0.5, 64, 0.5, true)
	again.Settle()

	if got := srv.PlayerHealth("Faller"); got >= 20 {
		t.Errorf("health after landing the fall carried across the relog = %v, want the fall's damage", got)
	}
	if got := srv.PlayerFallDistance("Faller"); got != 0 {
		t.Errorf("the fall after landing = %v, want it spent", got)
	}
	again.Disconnect()
}

// TestReportedRotationIsSnapped checks that the look a client reports is
// wrapped and clamped before it is kept: a yaw past the wrapped form comes
// back folded into [-180, 180), a pitch past straight down is held there,
// and both reach the file the player leaves behind.
func TestReportedRotationIsSnapped(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name       string
		yaw, pitch float32
		wantYaw    float32
		wantPitch  float32
	}{
		{"a full turn past the wrapped form", 400, 100, 40, 90},
		{"a yaw the wrap carries behind zero", 200, -20, -160, -20},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			cfg := config.Default()
			cfg.OnlineMode = false
			cfg.LevelName = t.TempDir()

			_, c := servertest.Join(t, cfg, "Looker")

			c.Send(protocol.IDPlayRotation, func(b *protocol.Buffer) {
				b.PutF32(tc.yaw)
				b.PutF32(tc.pitch)
				b.PutU8(0x01) // on the ground
			})
			c.Disconnect()

			root := servertest.ReadPlayerFile(t, servertest.PlayerFile(cfg.LevelName, "Looker"))
			rotation, ok := nbt.Get[nbt.List](root, "Rotation")
			if !ok || len(rotation.Items) != 2 {
				t.Fatalf("Rotation = %v, %v; want two floats", rotation, ok)
			}
			if got := rotation.Items[0].(nbt.Float); float32(got) != tc.wantYaw {
				t.Errorf("saved yaw = %v, want %v", got, tc.wantYaw)
			}
			if got := rotation.Items[1].(nbt.Float); float32(got) != tc.wantPitch {
				t.Errorf("saved pitch = %v, want %v", got, tc.wantPitch)
			}
		})
	}
}

// TestStoredRotationIsSnappedOnAMove checks that the move packets which carry
// no look snap the stored one all the same: a file's rotation that sits
// outside the wrapped form measures the local steps of the teleports that
// follow a move from the wrapped form, in the places a vanilla server answers
// the same steps with.
func TestStoredRotationIsSnappedOnAMove(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name       string
		yaw, pitch float32
		statusOnly bool
		wantX      string
	}{
		{"a yaw the wrap carries behind zero", 200, -20, false, "-0.439689"},
		{"a yaw the wrap carries to the back", 190, -45, false, "-0.484798"},
		{"a negative yaw the wrap carries over the top", -200.25, 30, false, "-0.438132"},
		{"the on-ground flag alone", 200, -20, true, "-0.439689"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			cfg := config.Default()
			cfg.OnlineMode = false
			// /teleport is a gamemaster's command, so the runner is listed as
			// an operator; creative keeps the step down harmless.
			cfg.GameMode = "creative"
			cfg.LevelName = t.TempDir()

			const name = "Looker"
			servertest.WritePlayerFile(t, servertest.PlayerFile(cfg.LevelName, name), nbt.Compound{
				"Pos": nbt.List{Elem: nbt.TypeDouble, Items: []nbt.Tag{
					nbt.Double(0.5), nbt.Double(64), nbt.Double(0.5),
				}},
				"Rotation": nbt.List{Elem: nbt.TypeFloat, Items: []nbt.Tag{
					nbt.Float(tc.yaw), nbt.Float(tc.pitch),
				}},
			})

			_, c := servertest.Dial(t, cfg, servertest.OpsListing(t, name))
			c.Login(name)
			c.Configure()
			c.SpawnedAt()
			c.SkipJoin()
			c.MarkLoaded()

			if tc.statusOnly {
				c.SendMovementFlags(true)
			} else {
				c.SendPosition(0.5, -60, 0.5, true)
			}
			c.ChatCommand("tp @s ^1 ^ ^")
			if pos := servertest.ExpectTeleport(t, c); servertest.FormatCoordForTest(pos.X) != tc.wantX {
				t.Errorf("local teleport x = %s, want %s", servertest.FormatCoordForTest(pos.X), tc.wantX)
			}
			c.Disconnect()
		})
	}
}

// TestSavedRotationIsSnappedOnLoad checks that a player file whose rotation
// sits outside the wrapped form reaches the join teleport with the yaw
// unwrapped and the pitch clamped, and that the rotation the client's accept
// carries back is wrapped before the next save writes it.
// vanilla: ServerGamePacketListenerImpl.handlePlayerPositionChange
func TestSavedRotationIsSnappedOnLoad(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name               string
		yaw, pitch         float32
		joinPitch          float32
		wantYaw, wantPitch float32
	}{
		{"a full turn past the wrapped form", 400, 100, 90, 40, 90},
		{"a yaw the wrap carries behind zero", 200, -20, -20, -160, -20},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			cfg := config.Default()
			cfg.OnlineMode = false
			cfg.LevelName = t.TempDir()

			const name = "Turner"
			servertest.WritePlayerFile(t, servertest.PlayerFile(cfg.LevelName, name), nbt.Compound{
				"Pos": nbt.List{Elem: nbt.TypeDouble, Items: []nbt.Tag{
					nbt.Double(0.5), nbt.Double(64), nbt.Double(0.5),
				}},
				"Rotation": nbt.List{Elem: nbt.TypeFloat, Items: []nbt.Tag{
					nbt.Float(tc.yaw), nbt.Float(tc.pitch),
				}},
			})

			_, connect := servertest.Start(t, cfg)
			c := connect()
			c.Login(name)
			c.Configure()

			// The join teleport carries the yaw as the file held it, the
			// pitch clamped.
			if got := c.SpawnedAt(); got.Yaw != tc.yaw || got.Pitch != tc.joinPitch {
				t.Errorf("join teleport faces (%v, %v), want (%v, %v)", got.Yaw, got.Pitch, tc.yaw, tc.joinPitch)
			}
			c.EnterWorld(name)
			c.Disconnect()

			rotation, ok := nbt.Get[nbt.List](servertest.ReadPlayerFile(t, servertest.PlayerFile(cfg.LevelName, name)), "Rotation")
			if !ok || len(rotation.Items) != 2 {
				t.Fatalf("Rotation = %v, %v; want two floats", rotation, ok)
			}
			if got := rotation.Items[0].(nbt.Float); float32(got) != tc.wantYaw {
				t.Errorf("saved yaw = %v, want %v", got, tc.wantYaw)
			}
			if got := rotation.Items[1].(nbt.Float); float32(got) != tc.wantPitch {
				t.Errorf("saved pitch = %v, want %v", got, tc.wantPitch)
			}
		})
	}
}

// TestPlayerFlightSurvivesRejoining checks that a player who logs out flying
// logs back in still flying: the flying flag is saved in the abilities compound
// the way the game stores it, and sent back in the abilities packet on join.
// Creative mode is where flight can be toggled, so this is a creative player.
func TestPlayerFlightSurvivesRejoining(t *testing.T) {
	t.Parallel()
	cfg := config.Default()
	cfg.OnlineMode = false
	cfg.LevelName = t.TempDir()
	cfg.GameMode = "creative"

	_, connect := servertest.Start(t, cfg)

	c := connect().Join("Testificate")

	// Toggle creative flight on, the way a client does with a double-jump.
	c.Send(protocol.IDPlayPlayerAbilitiesIn, func(b *protocol.Buffer) {
		b.PutI8(protocol.AbilityFlying)
	})
	c.Disconnect()

	// The flying flag reached disk in the abilities compound.
	root := servertest.ReadPlayerFile(t, servertest.PlayerFile(cfg.LevelName, "Testificate"))
	abilities, ok := nbt.Get[nbt.Compound](root, "abilities")
	if !ok {
		t.Fatal("the saved player has no abilities compound")
	}
	flying, ok := nbt.Get[nbt.Byte](abilities, "flying")
	if !ok || flying == 0 {
		t.Errorf("abilities.flying = %v, %v; want 1", flying, ok)
	}

	// The next connection is told it is flying, in the abilities packet that
	// join sends, rather than being dropped to the ground.
	again := connect()
	again.Login("Testificate")
	again.Configure()

	again.Expect(protocol.IDPlayLogin)
	flags := protocol.NewReader(again.AwaitPlay(protocol.IDPlayAbilities)).ReadI8()
	if flags&protocol.AbilityFlying == 0 {
		t.Errorf("join abilities flags = %#02x, want the flying bit set", flags)
	}
}

// TestPlayerPositionSurvivesARestart checks that the world outlives the process:
// a second server reading the same directory finds the players the first one
// saved.
func TestPlayerPositionSurvivesARestart(t *testing.T) {
	t.Parallel()
	cfg := config.Default()
	cfg.OnlineMode = false
	cfg.LevelName = t.TempDir()

	srv, c := servertest.Join(t, cfg, "Testificate")
	c.MoveTo(42.5, 70, 12.5, 0, 0)
	c.Settle()

	// The player is still connected, which is what a server being restarted
	// under its players looks like.
	if got := srv.PlayerCount(); got != 1 {
		t.Fatalf("player count = %d, want 1", got)
	}
	srv.Shutdown()

	if got := servertest.ReadPlayerFile(t, servertest.PlayerFile(cfg.LevelName, "Testificate")); got == nil {
		t.Fatal("shutting down saved nothing")
	}

	// A server of its own, as after a restart, pointed at the same world.
	restarted := *cfg
	_, reconnect := servertest.Start(t, &restarted)
	again := reconnect()
	again.Login("Testificate")
	again.Configure()

	got := again.SpawnedAt()
	if got.X != 42.5 || got.Y != 70 || got.Z != 12.5 {
		t.Errorf("rejoined at (%g, %g, %g), want (42.5, 70, 12.5)", got.X, got.Y, got.Z)
	}
}

// TestVanillaPlayerFileIsRead checks this server against a file a real vanilla
// server wrote: the player is put back where that server left them, and
// everything it saved about them that this server does not model yet is still
// in the file after they have been here.
func TestVanillaPlayerFileIsRead(t *testing.T) {
	t.Parallel()
	cfg := config.Default()
	cfg.OnlineMode = false
	cfg.LevelName = t.TempDir()

	// The fixture is the file a vanilla server wrote for the offline player
	// "Probe", copied out of a world it had been played in.
	const name = "Probe"
	before := servertest.InstallPlayerFixture(t, cfg, name, "vanilla-player.dat")
	saved := servertest.PlayerFile(cfg.LevelName, name)

	srv, connect := servertest.Start(t, cfg)

	// The world the file came from had open air where the player stood; the
	// flat stone world this server generates has stone there, and a body
	// vanilla left at twelve and five-sixths hearts would be crushed by the
	// wall the tick it loads. The cells it stands in and walks through are
	// carved before it connects, so a slow join cannot outlast the join's
	// load grace and the round-trip stays about the file, not the ground.
	air := servertest.BlockState(t, "minecraft:air")
	for y := int32(-62); y <= -57; y++ {
		srv.SetLoadedBlock(103, y, 5, air)
		srv.SetLoadedBlock(103, y, 6, air)
	}

	firstTick := srv.TicksRun()
	c := connect()
	c.Login(name)
	c.Configure()

	// Where the vanilla server left the player standing.
	if got := c.SpawnedAt(); got.X != 103.5 || got.Y != -60 || got.Z != 5.5 {
		t.Errorf("joined at (%g, %g, %g), want (103.5, -60, 5.5)", got.X, got.Y, got.Z)
	}
	c.ReadThroughJoin(name)
	servertest.HoldTicks(t, srv)
	c.MarkLoaded()
	c.MoveTo(103.5, -60, 6.5, 0, 0)
	c.Disconnect()

	// Everything the vanilla server saved that this one does not model is still
	// there. Losing it would empty a player's pockets the first time they
	// logged in here.
	after := servertest.ReadPlayerFile(t, saved)
	for _, field := range []string{
		"Health", "foodLevel", "XpLevel", "Inventory", "EnderItems",
		"abilities", "attributes", "recipeBook", "warden_spawn_tracker",
	} {
		want, ok := before[field]
		if !ok {
			t.Fatalf("the fixture has no %s to carry through", field)
		}
		got, ok := after[field]
		if !ok {
			t.Errorf("%s was dropped when the player was saved", field)
			continue
		}
		if field == "attributes" {
			// Default player instances are also saved; each loaded base and
			// modifier must survive alongside them, regardless of list order.
			for _, entry := range want.(nbt.List).Items {
				id := entry.(nbt.Compound)["id"]
				i := slices.IndexFunc(got.(nbt.List).Items, func(tag nbt.Tag) bool {
					return tag.(nbt.Compound)["id"] == id
				})
				if i < 0 || !reflect.DeepEqual(got.(nbt.List).Items[i], entry) {
					t.Errorf("loaded attribute %v changed in %v", entry, got)
				}
			}
			continue
		}
		if field == "warden_spawn_tracker" {
			// The warning survives; its timer now advances while connected.
			tracker := maps.Clone(want.(nbt.Compound))
			oldTicks := tracker["ticks_since_last_warning"].(nbt.Int)
			savedTracker, ok := got.(nbt.Compound)
			if !ok {
				t.Fatalf("warden_spawn_tracker = %v, want a compound", got)
			}
			newTicks, ok := nbt.Get[nbt.Int](savedTracker, "ticks_since_last_warning")
			if !ok || newTicks < oldTicks || uint64(newTicks-oldTicks) > srv.TicksRun()-firstTick+1 {
				t.Errorf("warning timer = %v, started at %v; must advance only by connected ticks", newTicks, oldTicks)
			}
			tracker["ticks_since_last_warning"] = newTicks
			want = tracker
		}
		if !reflect.DeepEqual(got, want) {
			t.Errorf("%s = %v, want %v", field, got, want)
		}
	}

	// And the position is the step this server saw.
	pos, ok := nbt.Get[nbt.List](after, "Pos")
	if !ok || len(pos.Items) != 3 || pos.Items[2] != nbt.Double(6.5) {
		t.Errorf("Pos = %v, want the position the player walked to", pos)
	}
}

// TestBedSpawnSurvivesRejoining checks the whole of what a player with a bed
// spawn gets: the click that sets it puts it on disk in the compound vanilla
// keeps it in, the next connection comes back carrying it, and the death they
// then die returns them to the bed rather than to the world spawn.
func TestBedSpawnSurvivesRejoining(t *testing.T) {
	t.Parallel()
	cfg := servertest.SurvivalTestConfig()

	// /kill is a gamemaster's command, so the player is listed as an operator.
	srv, connect := servertest.Start(t, cfg, servertest.OpsListing(t, "Sleeper"))

	c := connect().Join("Sleeper")

	// A bed clicked by day: the spawn point is set and the sleep is refused,
	// the spawn the click sets being the bed's head.
	if !srv.SetBlock(5, 64, 0, servertest.BedStateName("red", "foot", "south")) ||
		!srv.SetBlock(5, 64, 1, servertest.BedStateName("red", "head", "south")) {
		t.Fatal("could not stand the bed")
	}
	c.StandAt(2.5, 64, 0.5, 0)
	srv.SetDayTime(6000)
	c.UseBlock(5, 64, 0)
	c.ExpectSystemChat("block.minecraft.set_spawn", false)
	c.ExpectSystemChat("block.minecraft.bed.no_sleep", true)

	if spawn, _, ok := srv.SpawnPointOf("Sleeper"); !ok || spawn != (world.SpawnPos{X: 5, Y: 64, Z: 1}) {
		t.Fatalf("spawn point = (%d, %d, %d), %v; want the bed's head (5, 64, 1), true",
			spawn.X, spawn.Y, spawn.Z, ok)
	}

	c.Disconnect()

	// On disk it is the compound the game writes: the position as an int
	// array, the dimension beside it and the angles after, the forced flag
	// left out the way a bed's spawn is.
	root := servertest.ReadPlayerFile(t, servertest.PlayerFile(cfg.LevelName, "Sleeper"))
	want := nbt.Compound{
		"pos":       nbt.IntArray{5, 64, 1},
		"dimension": nbt.String(world.DimensionName),
		"yaw":       nbt.Float(0),
		"pitch":     nbt.Float(0),
	}
	respawn, ok := nbt.Get[nbt.Compound](root, "respawn")
	if !ok {
		t.Fatal("the bed spawn was not saved")
	}
	if !reflect.DeepEqual(respawn, want) {
		t.Errorf("respawn =\n%v\nwant\n%v", respawn, want)
	}

	// And the next connection comes back carrying it, unforced the way a
	// bed's is, so the death they die then returns them to the bed rather
	// than to the world spawn.
	again := connect()
	entityID := again.JoinAs("Sleeper")

	if spawn, forced, ok := srv.SpawnPointOf("Sleeper"); !ok || spawn != (world.SpawnPos{X: 5, Y: 64, Z: 1}) || forced {
		t.Fatalf("the rejoin carried spawn point (%d, %d, %d) forced %v, %v; want the bed's head unforced",
			spawn.X, spawn.Y, spawn.Z, forced, ok)
	}

	again.ChatCommand("kill")
	servertest.ExpectDeathSequence(t, again, entityID, protocol.Text{
		Translate: "death.attack.genericKill",
		With:      []protocol.Text{servertest.PlayerDisplayComponent("Sleeper")},
	})
	again.Send(protocol.IDPlayClientCommandIn, func(b *protocol.Buffer) {
		b.PutVarInt(protocol.ClientCommandPerformRespawn)
	})
	// The /kill feedback rides the mailbox behind the death, so it is
	// stepped over rather than taken as the next packet.
	again.AwaitPlay(protocol.IDPlayRespawn)
	if got := again.ExpectHealth(); got.Health != 20 {
		t.Errorf("health after the respawn = %v, want 20", got.Health)
	}
	again.Expect(protocol.IDPlaySetExperience)
	again.Expect(protocol.IDPlayAbilities)
	if pos := servertest.ExpectTeleport(t, again); pos.X != 4.5 || pos.Y != 64 || pos.Z != 1.5 || pos.Yaw != -90 || pos.Pitch != 0 {
		t.Errorf("respawn = (%v, %v, %v) yaw %v pitch %v, want the stand-up cell (4.5, 64, 1.5) yaw -90 pitch 0",
			pos.X, pos.Y, pos.Z, pos.Yaw, pos.Pitch)
	}
	again.Disconnect()
}

// TestBedClickStoresThePlayerAngles checks that a bed's click keeps the
// angles the player clicked under, in the form the file stores: a click made
// facing yaw 200, pitch -20 is written as yaw -160, pitch -20, with no
// forced flag the way a bed's spawn is.
func TestBedClickStoresThePlayerAngles(t *testing.T) {
	t.Parallel()
	cfg := servertest.BlockTestConfig()

	srv, c := servertest.Join(t, cfg, "Sleeper")

	if !srv.SetBlock(5, 64, 0, servertest.BedStateName("red", "foot", "south")) ||
		!srv.SetBlock(5, 64, 1, servertest.BedStateName("red", "head", "south")) {
		t.Fatal("could not stand the bed")
	}
	c.MoveTo(2.5, 64, 0.5, 200, -20)
	srv.SetDayTime(6000)
	c.UseBlock(5, 64, 0)
	c.ExpectSystemChat("block.minecraft.set_spawn", false)
	c.Disconnect()

	root := servertest.ReadPlayerFile(t, servertest.PlayerFile(cfg.LevelName, "Sleeper"))
	respawn, ok := nbt.Get[nbt.Compound](root, "respawn")
	if !ok {
		t.Fatal("the bed spawn was not saved")
	}
	want := nbt.Compound{
		"pos":       nbt.IntArray{5, 64, 1},
		"dimension": nbt.String(world.DimensionName),
		"yaw":       nbt.Float(-160),
		"pitch":     nbt.Float(-20),
	}
	if !reflect.DeepEqual(respawn, want) {
		t.Errorf("respawn =\n%v\nwant\n%v", respawn, want)
	}
}

// TestVanillaRespawnPointIsRead checks this server against a respawn point a
// real vanilla server wrote: the player comes back carrying it, and the
// file they are saved into still holds it, compound for compound.
func TestVanillaRespawnPointIsRead(t *testing.T) {
	t.Parallel()
	cfg := config.Default()
	cfg.OnlineMode = false
	cfg.LevelName = t.TempDir()

	// The fixture is the file a vanilla server wrote for the player
	// "Enchanter", whose respawn point /spawnpoint had put at (1, 2, 3).
	const name = "Enchanter"
	before := servertest.InstallPlayerFixture(t, cfg, name, "vanilla-enchanted.dat")
	saved := servertest.PlayerFile(cfg.LevelName, name)

	srv, connect := servertest.Start(t, cfg)
	c := connect()
	c.Login(name)
	c.Configure()

	// The join's chunks, with the loading screen still up: the flat stone
	// world has stone where the vanilla file left the body standing, and the
	// round-trip is about the file and not about the ground.
	c.ReadThroughJoin(name)

	// The point Enchanter carries was /spawnpoint's on the vanilla server,
	// so it comes back forced.
	if spawn, forced, ok := srv.SpawnPointOf(name); !ok || spawn != (world.SpawnPos{X: 1, Y: 2, Z: 3}) || !forced {
		t.Errorf("spawn point = (%d, %d, %d), forced %v, %v; want the file's (1, 2, 3), forced, true",
			spawn.X, spawn.Y, spawn.Z, forced, ok)
	}

	c.Disconnect()

	after := servertest.ReadPlayerFile(t, saved)
	want, ok := nbt.Get[nbt.Compound](before, "respawn")
	if !ok {
		t.Fatal("the fixture has no respawn compound to carry through")
	}
	got, ok := nbt.Get[nbt.Compound](after, "respawn")
	if !ok {
		t.Fatal("the respawn compound was dropped when the player was saved")
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("respawn =\n%v\nwant\n%v", got, want)
	}
}

// TestRespawnTheServerCannotActOnIsCarriedThrough checks that a respawn point
// a session here cannot return a dying player to - one a dimension the
// server does not run names, or one the world cannot hold - still leaves in
// the file the compound it came in with, and that the death it cannot serve
// returns the player to the world spawn rather than to the point itself.
func TestRespawnTheServerCannotActOnIsCarriedThrough(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name    string
		respawn nbt.Compound
		death   bool
	}{
		{"one a dimension the server does not run", nbt.Compound{
			"dimension": nbt.String("minecraft:unknown"),
			"pos":       nbt.IntArray{3, 70, 4},
			"yaw":       nbt.Float(90),
			"pitch":     nbt.Float(30),
			"forced":    nbt.Byte(1),
		}, true},
		{"one outside the world", nbt.Compound{
			"dimension": nbt.String(world.DimensionName),
			"pos":       nbt.IntArray{2_000_000_000, 0, 0},
			"yaw":       nbt.Float(0),
			"pitch":     nbt.Float(0),
			"forced":    nbt.Byte(1),
		}, false},
		{"one the overworld holds", nbt.Compound{
			"dimension": nbt.String(world.DimensionName),
			"pos":       nbt.IntArray{1, 2, 3},
			"yaw":       nbt.Float(90),
			"pitch":     nbt.Float(30),
		}, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			cfg := config.Default()
			cfg.OnlineMode = false
			cfg.LevelName = t.TempDir()

			const name = "Wanderer"
			servertest.WritePlayerFile(t, servertest.PlayerFile(cfg.LevelName, name), nbt.Compound{"respawn": tc.respawn})

			// /kill is a gamemaster's command, so the player is listed as an
			// operator.
			_, c, entityID := servertest.JoinAs(t, cfg, name, servertest.OpsListing(t, name))

			// A death the point cannot serve comes back at the world spawn,
			// not at coordinates the point names.
			if tc.death {
				c.ChatCommand("kill")
				servertest.ExpectDeathSequence(t, c, entityID, protocol.Text{
					Translate: "death.attack.genericKill",
					With:      []protocol.Text{servertest.PlayerDisplayComponent(name)},
				})
				c.Send(protocol.IDPlayClientCommandIn, func(b *protocol.Buffer) {
					b.PutVarInt(protocol.ClientCommandPerformRespawn)
				})
				// The /kill feedback rides the mailbox behind the death, so
				// it is stepped over rather than taken as the next packet.
				c.AwaitPlay(protocol.IDPlayRespawn)
				if got := c.ExpectHealth(); got.Health != 20 {
					t.Errorf("health after the respawn = %v, want 20", got.Health)
				}
				c.Expect(protocol.IDPlaySetExperience)
				c.Expect(protocol.IDPlayAbilities)
				if pos := servertest.ExpectTeleport(t, c); pos.X == 3.5 && pos.Y == 70 && pos.Z == 4.5 {
					t.Errorf("respawn = %v, want the world spawn rather than the point the file carried", pos)
				}
			}
			c.Disconnect()

			root := servertest.ReadPlayerFile(t, servertest.PlayerFile(cfg.LevelName, name))
			got, ok := nbt.Get[nbt.Compound](root, "respawn")
			if !ok {
				t.Fatal("the respawn compound was dropped when the player was saved")
			}
			if !reflect.DeepEqual(got, tc.respawn) {
				t.Errorf("respawn =\n%v\nwant\n%v", got, tc.respawn)
			}
		})
	}
}

// TestBannerPatternsAndHeadProfilesSurviveRejoining checks that the
// components a file can name with plain strings - a banner's layers and a
// head's profile - go to disk in the shape a vanilla server writes and come
// back whole beside the custom name.
func TestBannerPatternsAndHeadProfilesSurviveRejoining(t *testing.T) {
	t.Parallel()
	cfg := config.Default()
	cfg.GameMode = "creative"
	cfg.OnlineMode = false
	cfg.LevelName = t.TempDir()

	banner, _ := registry.ItemID("minecraft:white_banner")
	head, _ := registry.ItemID("minecraft:player_head")
	reg, _ := registry.Get("minecraft:banner_pattern")
	stripe := reg.ID("minecraft:stripe_top")
	if stripe < 0 {
		t.Fatal("the banner_pattern registry has no stripe_top")
	}
	name := protocol.Text{Text: "x"}
	patterned := protocol.ItemStack{ItemID: banner, Count: 1, CustomName: &name}
	if !patterned.SetBannerPatterns([]protocol.BannerPatternLayer{{Pattern: stripe, Dye: 14}}) {
		t.Fatal("the banner layers were refused")
	}
	skull := protocol.ItemStack{ItemID: head, Count: 1}
	if !skull.SetProfile(protocol.Profile{Name: "Notch", HasName: true}) {
		t.Fatal("the profile was refused")
	}

	_, connect := servertest.Start(t, cfg)
	c := connect()
	c.Login("Testificate")
	c.Configure()
	c.Inventory()
	c.EnterWorld("Testificate")
	c.SetCreativeSlot(36, patterned)
	c.SetCreativeSlot(37, skull)
	c.Disconnect()

	root := servertest.ReadPlayerFile(t, servertest.PlayerFile(cfg.LevelName, "Testificate"))
	items, _ := nbt.Get[nbt.List](root, "Inventory")
	byID := map[nbt.String]nbt.Compound{}
	for _, item := range items.Items {
		entry := item.(nbt.Compound)
		components, _ := nbt.Get[nbt.Compound](entry, "components")
		id, _ := nbt.Get[nbt.String](entry, "id")
		byID[id] = components
	}
	wantLayers := nbt.List{Elem: nbt.TypeCompound, Items: []nbt.Tag{nbt.Compound{
		"pattern": nbt.String("minecraft:stripe_top"),
		"color":   nbt.String("red"),
	}}}
	if got := byID["minecraft:white_banner"]["minecraft:banner_patterns"]; !reflect.DeepEqual(got, nbt.Tag(wantLayers)) {
		t.Errorf("banner_patterns on disk = %v, want %v", got, wantLayers)
	}
	profile, _ := byID["minecraft:player_head"]["minecraft:profile"].(nbt.Compound)
	if profile["name"] != nbt.String("Notch") {
		t.Errorf("profile on disk = %v, want the name Notch", profile)
	}

	again := connect()
	again.Login("Testificate")
	again.Configure()
	got := again.Inventory()
	if !got[36].Equal(patterned) {
		t.Errorf("slot 36 = %+v, want the patterned banner %+v", got[36], patterned)
	}
	if !got[37].Equal(skull) {
		t.Errorf("slot 37 = %+v, want the head %+v", got[37], skull)
	}
	again.Disconnect()
}

// TestPatternedShieldKeepsItsBaseColorAcrossRejoining checks that a shield's
// base colour goes to disk as its dye name beside its banner layers and
// comes back with them.
func TestPatternedShieldKeepsItsBaseColorAcrossRejoining(t *testing.T) {
	t.Parallel()
	cfg := config.Default()
	cfg.GameMode = "creative"
	cfg.OnlineMode = false
	cfg.LevelName = t.TempDir()

	shieldID, _ := registry.ItemID("minecraft:shield")
	reg, _ := registry.Get("minecraft:banner_pattern")
	stripe := reg.ID("minecraft:stripe_top")
	shield := protocol.ItemStack{ItemID: shieldID, Count: 1}
	if !shield.SetBannerPatterns([]protocol.BannerPatternLayer{{Pattern: stripe, Dye: 14}}) {
		t.Fatal("the shield layers were refused")
	}
	if !shield.SetBaseColor(11) {
		t.Fatal("the base colour was refused")
	}

	_, connect := servertest.Start(t, cfg)
	c := connect()
	c.Login("Testificate")
	c.Configure()
	c.Inventory()
	c.EnterWorld("Testificate")
	c.SetCreativeSlot(36, shield)
	c.Disconnect()

	root := servertest.ReadPlayerFile(t, servertest.PlayerFile(cfg.LevelName, "Testificate"))
	items, _ := nbt.Get[nbt.List](root, "Inventory")
	var components nbt.Compound
	for _, item := range items.Items {
		entry := item.(nbt.Compound)
		if id, _ := nbt.Get[nbt.String](entry, "id"); id == "minecraft:shield" {
			components, _ = nbt.Get[nbt.Compound](entry, "components")
		}
	}
	if got := components["minecraft:base_color"]; got != nbt.String("blue") {
		t.Errorf("base_color on disk = %v, want blue", got)
	}

	again := connect()
	again.Login("Testificate")
	again.Configure()
	got := again.Inventory()
	if !got[36].Equal(shield) {
		t.Errorf("slot 36 = %+v, want the blue patterned shield %+v", got[36], shield)
	}
	again.Disconnect()
}

// TestBlockEntityComponentsSurviveRejoining checks that a decorated pot's
// sherds, a head's note block sound and a hive's bees go to disk under their
// component names and come back whole.
func TestBlockEntityComponentsSurviveRejoining(t *testing.T) {
	t.Parallel()
	cfg := config.Default()
	cfg.GameMode = "creative"
	cfg.OnlineMode = false
	cfg.LevelName = t.TempDir()

	itemID := func(name string) int32 {
		id, ok := registry.ItemID(name)
		if !ok {
			t.Fatalf("the item registry has no %s", name)
		}
		return id
	}
	// A bare back, a left sherd carrying a component, a plain right sherd and
	// a bare front.
	named := protocol.ItemStack{ItemID: itemID("minecraft:arms_up_pottery_sherd"), Count: 1,
		CustomName: &protocol.Text{Text: "Marked"}}
	pot := protocol.ItemStack{ItemID: itemID("minecraft:decorated_pot"), Count: 1}
	if !pot.SetPotDecorations([4]protocol.ItemStack{{}, named, {ItemID: itemID("minecraft:brick"), Count: 1}, {}}) {
		t.Fatal("the pot decorations were refused")
	}
	head := protocol.ItemStack{ItemID: itemID("minecraft:player_head"), Count: 1}
	if !head.SetNoteBlockSound("minecraft:entity.pig.ambient") {
		t.Fatal("the note block sound was refused")
	}
	beeType, ok := registry.EntityTypeID("minecraft:bee")
	if !ok {
		t.Fatal("the entity registry has no bee")
	}
	hive := protocol.ItemStack{ItemID: itemID("minecraft:beehive"), Count: 1}
	if !hive.SetBees([]protocol.Bee{{
		EntityType:     beeType,
		Data:           nbt.Compound{"Age": nbt.Int(5)},
		TicksInHive:    12,
		MinTicksInHive: 2400,
	}}) {
		t.Fatal("the bees were refused")
	}

	_, connect := servertest.Start(t, cfg)
	c := connect()
	c.Login("Testificate")
	c.Configure()
	c.Inventory()
	c.EnterWorld("Testificate")
	c.SetCreativeSlot(36, pot)
	c.SetCreativeSlot(37, head)
	c.SetCreativeSlot(38, hive)
	c.Disconnect()

	root := servertest.ReadPlayerFile(t, servertest.PlayerFile(cfg.LevelName, "Testificate"))
	items, _ := nbt.Get[nbt.List](root, "Inventory")
	byID := map[nbt.String]nbt.Compound{}
	for _, item := range items.Items {
		entry := item.(nbt.Compound)
		components, _ := nbt.Get[nbt.Compound](entry, "components")
		id, _ := nbt.Get[nbt.String](entry, "id")
		byID[id] = components
	}
	sherds, _ := byID["minecraft:decorated_pot"]["minecraft:pot_decorations"].(nbt.Compound)
	left, _ := nbt.Get[nbt.Compound](sherds, "left")
	leftComponents, _ := nbt.Get[nbt.Compound](left, "components")
	right, _ := nbt.Get[nbt.Compound](sherds, "right")
	if _, hasBack := sherds["back"]; hasBack || len(sherds) != 2 {
		t.Errorf("pot_decorations on disk = %v, want only left and right", sherds)
	}
	if left["id"] != nbt.String("minecraft:arms_up_pottery_sherd") || leftComponents["minecraft:custom_name"] == nil {
		t.Errorf("the left sherd on disk = %v, want the sherd with its custom name", left)
	}
	if right["id"] != nbt.String("minecraft:brick") {
		t.Errorf("the right sherd on disk = %v, want a brick", right)
	}
	if got := byID["minecraft:player_head"]["minecraft:note_block_sound"]; got != nbt.Tag(nbt.String("minecraft:entity.pig.ambient")) {
		t.Errorf("note_block_sound on disk = %v", got)
	}
	bees, _ := byID["minecraft:beehive"]["minecraft:bees"].(nbt.List)
	if len(bees.Items) != 1 {
		t.Fatalf("bees on disk = %v, want one occupant", bees)
	}
	occupant := bees.Items[0].(nbt.Compound)
	data, _ := nbt.Get[nbt.Compound](occupant, "entity_data")
	if data["id"] != nbt.String("minecraft:bee") || occupant["ticks_in_hive"] != nbt.Int(12) {
		t.Errorf("bee on disk = %v", occupant)
	}

	again := connect()
	again.Login("Testificate")
	again.Configure()
	got := again.Inventory()
	for slot, want := range map[int]protocol.ItemStack{36: pot, 37: head, 38: hive} {
		if !got[slot].Equal(want) {
			t.Errorf("slot %d = %+v, want %+v", slot, got[slot], want)
		}
	}
	again.Disconnect()
}

// TestPlayerAttributesAreLoadedFromTheFile checks the file's attributes list
// reaches the player: a saved entry comes back rewritten from the loaded
// instance, with the modifier it could not read dropped.
func TestPlayerAttributesAreLoadedFromTheFile(t *testing.T) {
	t.Parallel()
	cfg := config.Default()
	cfg.OnlineMode = false
	cfg.LevelName = t.TempDir()
	const name = "Attributed"
	modifier := func(id, op string) nbt.Compound {
		return nbt.Compound{"id": nbt.String(id), "amount": nbt.Double(2), "operation": nbt.String(op)}
	}
	servertest.WritePlayerFile(t, servertest.PlayerFile(cfg.LevelName, name), nbt.Compound{
		"DataVersion": nbt.Int(4800),
		"Health":      nbt.Float(20),
		"attributes": nbt.List{Elem: nbt.TypeCompound, Items: []nbt.Tag{nbt.Compound{
			"id":   nbt.String("minecraft:armor"),
			"base": nbt.Double(7),
			"modifiers": nbt.List{Elem: nbt.TypeCompound, Items: []nbt.Tag{
				modifier("minecraft:kept", "add_value"),
				modifier("minecraft:unreadable", "bogus"),
			}},
		}}},
	})

	_, c := servertest.Join(t, cfg, name)
	c.Disconnect()

	root := servertest.ReadPlayerFile(t, servertest.PlayerFile(cfg.LevelName, name))
	list, _ := nbt.Get[nbt.List](root, "attributes")
	if len(list.Items) != 5 {
		t.Fatalf("attributes = %v, want the four player instances and loaded armor", list)
	}
	entry := list.Items[0].(nbt.Compound)
	mods, _ := nbt.Get[nbt.List](entry, "modifiers")
	if entry["base"] != nbt.Double(7) || len(mods.Items) != 1 || mods.Items[0].(nbt.Compound)["id"] != nbt.String("minecraft:kept") {
		t.Errorf("attributes entry = %v, want base 7 and the one readable modifier", entry)
	}
}

// TestOminousBannerKeepsItsRarityAndTooltipAcrossRejoining checks that the
// raid captain's banner is still the same stack after a save and a load:
// rarity and tooltip_display go to disk with it.
func TestOminousBannerKeepsItsRarityAndTooltipAcrossRejoining(t *testing.T) {
	t.Parallel()
	cfg := config.Default()
	cfg.GameMode = "creative"
	cfg.OnlineMode = false
	cfg.LevelName = t.TempDir()

	srv, connect := servertest.Start(t, cfg)
	banner := srv.OminousBanner()
	c := connect()
	c.Login("Testificate")
	c.Configure()
	c.Inventory()
	c.EnterWorld("Testificate")
	c.SetCreativeSlot(36, banner)
	servertest.WaitFor(t, srv, 100, func() bool {
		return srv.InventorySlot("Testificate", player.HotbarSlot).Same(banner)
	})
	c.Disconnect()

	again := connect()
	again.Login("Testificate")
	again.Configure()
	got := again.Inventory()
	if !got[36].Same(banner) {
		t.Errorf("slot 36 = %+v, want the ominous banner %+v", got[36], banner)
	}
	again.Disconnect()
}
