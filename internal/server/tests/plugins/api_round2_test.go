package plugins_test

import (
	"sync/atomic"
	"testing"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/servertest"
	"github.com/golem-mc/golem/plugin"
	"github.com/golem-mc/golem/registry"
)

func countItem(inv plugin.Inventory, item registry.Item) (n int) {
	for i := range inv.Size() {
		if s := inv.Item(i); s.Item == item {
			n += s.Count
		}
	}
	return n
}

// An /invsee window shows 45 slots over 41 stored ones; the spare four refuse items.
// vanilla: Slot.mayPlace
func TestPluginInvseeSpareSlotsRefuseItems(t *testing.T) {
	srv, h, run, viewer, _ := twoWindowed(t)
	var vp, op plugin.Player
	run(func() {
		vp, _ = h.PlayerNamed("Viewer")
		op, _ = h.PlayerNamed("Owner")
		vp.Inventory().SetItem(0, plugin.NewStack(registry.ItemStick, 1))
	})
	viewer.Settle()
	run(func() { vp.OpenInventory(op.Inventory()) })
	viewer.ExpectContainerOpen(protocol.MenuGeneric9x1+4, false, 0, 0, 0)
	viewer.Click(72, 0, protocol.ClickModePickup)
	viewer.Click(42, 0, protocol.ClickModePickup)
	viewer.CloseWindow()
	servertest.UntilTicks(t, srv, 20, "the window to close", func() bool {
		_, open := vp.OpenView()
		return !open
	})
	if n := countItem(vp.Inventory(), registry.ItemStick) + countItem(op.Inventory(), registry.ItemStick); n != 1 {
		t.Errorf("%d sticks left of 1", n)
	}
}

// vanilla: ServerPlayer.doTick closes a menu that is no longer valid
func TestPluginServerSideCloseFiresInventoryCloseEvent(t *testing.T) {
	srv, h, run, viewer, owner := twoWindowed(t)
	var closes atomic.Int32
	run(func() {
		vp, _ := h.PlayerNamed("Viewer")
		op, _ := h.PlayerNamed("Owner")
		plugin.Listen(h, func(e *plugin.InventoryCloseEvent) { closes.Add(1) })
		vp.OpenInventory(op.Inventory())
	})
	viewer.ExpectContainerOpen(protocol.MenuGeneric9x1+4, false, 0, 0, 0)
	owner.Raw.Close()
	servertest.UntilTicks(t, srv, 40, "the close event", func() bool { return closes.Load() == 1 })
}

// vanilla: AbstractContainerMenu.slotsChanged
func TestPluginStationGridWritable(t *testing.T) {
	_, h, run, w := inventoryPlayer(t)
	run(func() {
		inv := h.CreateInventory(plugin.InvCrafting, 0, plugin.Plain("Bench"), nil)
		v := testerOf(t, h).OpenInventory(inv)
		if v == nil {
			t.Fatal("open refused")
		}
		// The 2x2 at the grid's top left: slots 1, 2, 4, 5 of the 3x3.
		cells := []int{1, 2, 4, 5}
		for _, i := range cells {
			v.Top().SetItem(i, plugin.NewStack(registry.ItemOakPlanks, 1))
		}
		for _, i := range cells {
			if got := v.Top().Item(i); got.Item != registry.ItemOakPlanks {
				t.Errorf("grid slot %d reads %+v", i, got)
			}
		}
		if got := v.Top().Item(0); got.Item != registry.ItemCraftingTable {
			t.Errorf("result reads %+v, want a crafting table", got)
		}
	})
	w.ExpectOpenTitle(protocol.MenuCrafting, 0, 0, 0, 0)
	// Each write recomputes the result; the last one is the table.
	want := servertest.ItemStack(t, "minecraft:crafting_table", 1)
	w.AwaitWindowPackets(func(id int32, data []byte) bool {
		if id != protocol.IDPlayContainerSlot {
			return false
		}
		r := protocol.NewReader(data)
		r.ReadVarInt()
		r.ReadVarInt()
		if slot := r.ReadI16(); slot != 0 {
			return false
		}
		got, ok := r.ReadSlot()
		return ok && got.Equal(want)
	})
}

// A rocket made without a LifeTime rolls one from its flight, so it flies.
// vanilla: FireworkRocketEntity.<init>
func TestPluginSpawnedRocketLives(t *testing.T) {
	srv, h, run, _ := inventoryPlayer(t)
	var rockets []plugin.Entity
	run(func() {
		at := testerOf(t, h).Location()
		w := at.World
		at.Pos.X += 3 // clear of the player, whose body a rocket bursts on
		for _, flight := range []string{"0", "3"} {
			item, err := h.Items().Parse("firework_rocket[fireworks={flight_duration:"+flight+"}]", 1)
			if err != nil {
				t.Fatal(err)
			}
			e, err := w.Spawn(registry.EntityTypeFireworkRocket, at, func(e plugin.Entity) {
				e.(plugin.Firework).SetItem(item)
			})
			if err != nil {
				t.Fatal(err)
			}
			rockets = append(rockets, e)
		}
	})
	servertest.RunTicks(t, srv, 5)
	for i, e := range rockets {
		if !e.IsValid() {
			t.Errorf("rocket %d burst within 5 ticks", i)
		}
	}
}

// vanilla: ServerExplosion.explode, Creeper.explodeCreeper
func TestPluginCancelledExplodeSendsNoPacket(t *testing.T) {
	for _, cancel := range []bool{true, false} {
		srv, c := tickScene(t, func(s plugin.Server) {
			plugin.Listen(s, func(e *plugin.EntityExplodeEvent) { e.SetCancelled(cancel) })
		})
		srv.SpawnTNT(4.5, 62, 4.5, 1)
		packets := 0
		for range 3 {
			srv.RunTick()
			c.SettleAndCollect(func(id int32, _ []byte) {
				if id == protocol.IDPlayExplosion {
					packets++
				}
			})
		}
		if want := map[bool]int{true: 0, false: 1}[cancel]; packets != want {
			t.Errorf("cancel=%v: %d Explosion packets, want %d", cancel, packets, want)
		}
	}
}

// vanilla: AbstractArrow.setOwner
func TestPluginSetShooterAllowsPickup(t *testing.T) {
	_, h, run, _ := inventoryPlayer(t)
	run(func() {
		at := testerOf(t, h).Location()
		w := at.World
		e, err := w.Spawn(registry.EntityTypeArrow, at, nil)
		if err != nil {
			t.Fatal(err)
		}
		arrow := e.(plugin.Arrow)
		if arrow.Pickup() != plugin.PickupDisallowed {
			t.Fatalf("a fresh arrow's pickup is %v", arrow.Pickup())
		}
		arrow.SetShooter(testerOf(t, h))
		if arrow.Pickup() != plugin.PickupAllowed {
			t.Errorf("pickup %v after a player shooter", arrow.Pickup())
		}
	})
}

// vanilla: AbstractHorse, TamableAnimal; Fox is neither
func TestPluginTameableKinds(t *testing.T) {
	_, h, run, _ := inventoryPlayer(t)
	run(func() {
		at := testerOf(t, h).Location()
		w := at.World
		for _, tc := range []struct {
			typ  registry.EntityType
			want bool
		}{
			{registry.EntityTypeHorse, true}, {registry.EntityTypeDonkey, true}, {registry.EntityTypeMule, true},
			{registry.EntityTypeLlama, true}, {registry.EntityTypeTraderLlama, true}, {registry.EntityTypeSkeletonHorse, true},
			{registry.EntityTypeZombieHorse, true}, {registry.EntityTypeCamel, true}, {registry.EntityTypeCamelHusk, true},
			{registry.EntityTypeWolf, true}, {registry.EntityTypeFox, false},
		} {
			e, err := w.Spawn(tc.typ, at, nil)
			if err != nil {
				t.Errorf("%v: %v", tc.typ, err)
				continue
			}
			ta, ok := e.(plugin.Tameable)
			if ok != tc.want {
				t.Errorf("%v: Tameable %v, want %v", tc.typ, ok, tc.want)
				continue
			}
			if !ok {
				continue
			}
			owner := plugin.UUID{1, 2, 3}
			ta.SetOwner(owner)
			if o, has := ta.Owner(); !has || o != owner || !ta.Tamed() {
				t.Errorf("%v: owner %v %v tamed %v", tc.typ, o, has, ta.Tamed())
			}
		}
	})
}
