package storage

import (
	"maps"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"testing"

	"github.com/golem-mc/golem/internal/server/servertest"

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
	armor := slices.IndexFunc(list.Items, func(tag nbt.Tag) bool {
		return tag.(nbt.Compound)["id"] == nbt.String("minecraft:armor")
	})
	if armor < 0 {
		t.Fatalf("attributes = %v, want an armor entry", list)
	}
	entry := list.Items[armor].(nbt.Compound)
	mods, _ := nbt.Get[nbt.List](entry, "modifiers")
	if entry["base"] != nbt.Double(7) || len(mods.Items) != 1 || mods.Items[0].(nbt.Compound)["id"] != nbt.String("minecraft:kept") {
		t.Errorf("attributes entry = %v, want base 7 and the one readable modifier", entry)
	}
}
