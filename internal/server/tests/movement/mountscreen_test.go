package movement

import (
	"testing"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/player"
	"github.com/golem-mc/golem/internal/server/servertest"
	"github.com/golem-mc/golem/registry"
)

// The mount screen: a sneaking click on a tame horse, donkey or mule, or the
// inventory key while riding one, opens it over the mount's own saddle,
// armour and chest.
// vanilla: AbstractHorse.openCustomInventoryScreen, HorseInventoryMenu

// The wire numbers of the saddle's and the body's equipment slots.
// vanilla: EquipmentSlot.SADDLE, EquipmentSlot.BODY
const (
	saddleWireSlot = 7
	bodyWireSlot   = 6
)

// sneakClick is a main-hand click on the entity with the sneak key held.
func sneakClick(c *servertest.Client, id int32) {
	c.T.Helper()
	c.Send(protocol.IDPlayInteractIn, func(b *protocol.Buffer) {
		b.PutVarInt(id)
		b.PutVarInt(protocol.HandMain)
		b.PutLpVec3(0.1, 0.9, 0.2)
		b.PutBool(true)
	})
}

// openMountScreen reads the Mount Screen Open for the entity and the window
// content behind it, and returns the window, its chest columns and slots.
func openMountScreen(t *testing.T, c *servertest.Client, id int32) (*servertest.WindowedClient, int32, []protocol.ItemStack) {
	t.Helper()
	r := protocol.NewReader(c.ExpectPlay(protocol.IDPlayMountScreenOpen))
	window, columns, entity := r.ReadVarInt(), r.ReadVarInt(), r.ReadI32()
	if err := r.Err(); err != nil {
		t.Fatalf("mount screen open: %v", err)
	}
	if entity != id {
		t.Fatalf("mount screen opened for entity %d, want %d", entity, id)
	}
	w := &servertest.WindowedClient{Client: c, Window: window, WantBlockEntityType: -1}
	return w, columns, w.ExpectWindowContent()
}

func stackOf(t *testing.T, name string, count int32) protocol.ItemStack {
	t.Helper()
	id, ok := registry.ItemID(name)
	if !ok {
		t.Fatalf("no item %s", name)
	}
	return protocol.ItemStack{ItemID: id, Count: count}
}

// wornAfterTick runs a mob tick and returns what the mob's equipment packets
// told for the wire slot.
func wornAfterTick(t *testing.T, c *servertest.Client, tick func(), id int32, slot uint8) protocol.ItemStack {
	t.Helper()
	var got protocol.ItemStack
	record := func(pid int32, data []byte) {
		if pid != protocol.IDPlayEntityEquipment {
			return
		}
		r := protocol.NewReader(data)
		if r.ReadVarInt() != id {
			return
		}
		for {
			raw := r.ReadU8()
			stack, _ := r.ReadSlot()
			if raw&0x7F == slot {
				got = stack
			}
			if raw&0x80 == 0 || r.Err() != nil {
				return
			}
		}
	}
	c.SettleAndCollect(record)
	tick()
	c.SettleAndCollect(record)
	return got
}

// TestSneakClickingAChestedDonkeyOpensItsChest: the screen has the chest's
// five columns, shows what the donkey carries, and a click takes it out.
func TestSneakClickingAChestedDonkeyOpensItsChest(t *testing.T) {
	t.Parallel()
	rider, id, _ := equineSummon(t, "minecraft:donkey",
		`{Tame:1b,ChestedHorse:1b,Items:[{Slot:0b,id:"minecraft:stone",count:3}]}`)
	rider.Settle()
	sneakClick(rider, id)
	w, columns, slots := openMountScreen(t, rider, id)
	if columns != 5 {
		t.Errorf("chest columns %d, want 5", columns)
	}
	if want := 2 + 3*5 + player.MainInvSlots; len(slots) != want {
		t.Fatalf("window has %d slots, want %d", len(slots), want)
	}
	stone := stackOf(t, "minecraft:stone", 3)
	if !slots[2].Equal(stone) {
		t.Errorf("first chest slot %+v, want three stone", slots[2])
	}
	w.Click(2, 0, protocol.ClickModePickup)
	w.ExpectClickResult(map[int16]protocol.ItemStack{2: {}}, stone)
}

// TestASaddlePutInTheMountScreenSaddlesTheHorse: the screen's saddle slot is
// the horse's own, so a saddle placed there is worn and told.
func TestASaddlePutInTheMountScreenSaddlesTheHorse(t *testing.T) {
	t.Parallel()
	rider, horse, srv := horseSummon(t, "{Tame:1b}")
	saddle := stackOf(t, "minecraft:saddle", 1)
	srv.GiveSlot("Rider", player.HotbarSlot, saddle)
	rider.Settle()
	sneakClick(rider, horse.entityID)
	w, columns, slots := openMountScreen(t, rider, horse.entityID)
	if columns != 0 || len(slots) != 2+player.MainInvSlots {
		t.Fatalf("a horse's screen has %d columns and %d slots, want none and %d", columns, len(slots), 2+player.MainInvSlots)
	}
	hotbar := int16(2 + 27)
	w.Click(hotbar, 0, protocol.ClickModePickup)
	w.ExpectClickResult(map[int16]protocol.ItemStack{hotbar: {}}, saddle)
	w.Click(0, 0, protocol.ClickModePickup)
	w.ExpectClickResult(map[int16]protocol.ItemStack{0: saddle}, protocol.ItemStack{})
	if got := wornAfterTick(t, rider, func() { srv.TickMobs(1) }, horse.entityID, saddleWireSlot); !got.Equal(saddle) {
		t.Errorf("the horse was told wearing %+v in the saddle slot, want the saddle", got)
	}
}

// TestTheInventoryKeyOpensTheRiddenHorsesScreen: the rider's inventory key
// opens the horse's screen, not their own.
// vanilla: ServerGamePacketListenerImpl.handlePlayerCommand
func TestTheInventoryKeyOpensTheRiddenHorsesScreen(t *testing.T) {
	t.Parallel()
	rider, horse, _ := horseSummon(t, "{Tame:1b}")
	rider.UseOn(horse.entityID)
	rider.Settle()
	rider.Send(protocol.IDPlayPlayerCommand, func(b *protocol.Buffer) {
		b.PutVarInt(0)
		b.PutVarInt(protocol.PlayerCommandOpenInventory)
		b.PutVarInt(0)
	})
	openMountScreen(t, rider, horse.entityID)
}

// vanilla: AbstractHorse.openCustomInventoryScreen
func TestABystanderCannotOpenAMountWithPassengers(t *testing.T) {
	t.Parallel()
	rider, id, srv := equineSummon(t, "minecraft:horse", `{Tame:1b,Passengers:[{id:"minecraft:pig"}]}`)
	servertest.HoldTicks(t, srv)
	rider.Settle()
	if len(srv.MobPassengerIDs(id)) != 1 {
		t.Fatal("horse does not carry the pig")
	}
	sneakClick(rider, id)
	rider.ExpectNoneWithinTicks(srv, 20, func(id int32, _ []byte) bool {
		return id == protocol.IDPlayMountScreenOpen
	})
}

// TestHorseArmourGoesOnByClick: a tame horse takes the clicked armour into
// its body slot; a donkey does not wear it.
// vanilla: AbstractHorse.mobInteract's equipBodyArmor
func TestHorseArmourGoesOnByClick(t *testing.T) {
	t.Parallel()
	rider, horse, srv := horseSummon(t, "{Tame:1b}")
	armour := stackOf(t, "minecraft:iron_horse_armor", 1)
	srv.GiveSlot("Rider", player.HotbarSlot, armour)
	rider.Settle()
	rider.UseOn(horse.entityID)
	if got := wornAfterTick(t, rider, func() { srv.TickMobs(1) }, horse.entityID, bodyWireSlot); !got.Equal(armour) {
		t.Errorf("the horse was told wearing %+v, want the iron horse armour", got)
	}
	if got := srv.InventorySlot("Rider", player.HotbarSlot); !got.Empty() {
		t.Errorf("hand holds %+v after fitting the armour, want nothing", got)
	}

	donkeyRider, donkey, donkeySrv := equineSummon(t, "minecraft:donkey", "{Tame:1b}")
	donkeySrv.GiveSlot("Rider", player.HotbarSlot, armour)
	donkeyRider.Settle()
	donkeyRider.UseOn(donkey)
	donkeyRider.Settle()
	if got := donkeySrv.InventorySlot("Rider", player.HotbarSlot); !got.Equal(armour) {
		t.Errorf("a donkey took horse armour: hand holds %+v", got)
	}
}

// TestTheMountScreenClosesWhenTheMountDies: the open screen goes once the
// horse is gone.
// vanilla: AbstractMountInventoryMenu.stillValid
func TestTheMountScreenClosesWhenTheMountDies(t *testing.T) {
	t.Parallel()
	rider, id, srv := equineSummon(t, "minecraft:donkey", `{Tame:1b,ChestedHorse:1b}`)
	rider.Settle()
	sneakClick(rider, id)
	w, _, _ := openMountScreen(t, rider, id)
	rider.ChatCommand("gamerule mob_drops false")
	rider.ChatCommand("kill @e[type=minecraft:donkey]")
	rider.Settle()
	srv.TickMobs(2)
	srv.TickOpenWindow("Rider")
	data := rider.ExpectPlay(protocol.IDPlayCloseContainer)
	if got := protocol.NewReader(data).ReadVarInt(); got != w.Window {
		t.Errorf("closed window %d, want %d", got, w.Window)
	}
}
