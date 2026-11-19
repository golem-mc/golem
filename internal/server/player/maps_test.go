package player

import (
	"slices"
	"sync"
	"testing"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/registry"
)

func holdingsMap(id int32) protocol.ItemStack {
	stack := protocol.ItemStack{ItemID: int32(registry.ItemFilledMap), Count: 1}
	stack.SetMapID(id)
	return stack
}

func checkMapHoldings(t *testing.T, p *Player, want []int32, hidden bool) {
	t.Helper()
	// Check both a rebuild and a cached read without retaining the borrowed IDs.
	for range 2 {
		p.MapHoldings(func(ids []int32, gotHidden bool) {
			if !slices.Equal(ids, want) || gotHidden != hidden {
				t.Errorf("MapHoldings = (%v, %v), want (%v, %v)", ids, gotHidden, want, hidden)
			}
		})
	}
}

func TestMapHoldingsInventoryChanges(t *testing.T) {
	t.Parallel()
	var p Player
	checkMapHoldings(t, &p, nil, false)
	p.Inventory(func(inv *Inventory) { inv.Slots[MainSlot] = holdingsMap(7) })
	checkMapHoldings(t, &p, []int32{7}, false)
	p.Inventory(func(inv *Inventory) { inv.Slots[OffHandSlot] = holdingsMap(7) })
	checkMapHoldings(t, &p, []int32{7}, false)
	p.Inventory(func(inv *Inventory) { inv.Slots[MainSlot] = holdingsMap(8) })
	checkMapHoldings(t, &p, []int32{8, 7}, false)
	p.Inventory(func(inv *Inventory) { inv.Slots[MainSlot] = protocol.ItemStack{} })
	checkMapHoldings(t, &p, []int32{7}, false)
	p.Inventory(func(inv *Inventory) { inv.Slots[OffHandSlot] = protocol.ItemStack{} })
	checkMapHoldings(t, &p, nil, false)
	// A read-only inventory callback is also safe between cached reads.
	p.Inventory(func(*Inventory) {})
	checkMapHoldings(t, &p, nil, false)
}

func TestMapHoldingsCarriedSlots(t *testing.T) {
	t.Parallel()
	var p Player
	var want []int32
	p.Inventory(func(inv *Inventory) {
		for slot := range inv.Slots {
			inv.Slots[slot] = holdingsMap(int32(slot))
			if slot >= ArmorSlot && slot <= OffHandSlot {
				want = append(want, int32(slot))
			}
		}
		inv.Body, inv.Saddle = holdingsMap(100), holdingsMap(101)
		inv.Ender[0] = holdingsMap(102)
	})
	checkMapHoldings(t, &p, want, false)
}

func TestMapHoldingsRequiresMapItemAndID(t *testing.T) {
	t.Parallel()
	var p Player
	p.Inventory(func(inv *Inventory) {
		inv.Slots[MainSlot] = holdingsMap(1)
		inv.Slots[MainSlot].ItemID = int32(registry.ItemStone)
		inv.Slots[MainSlot+1] = protocol.ItemStack{ItemID: int32(registry.ItemFilledMap), Count: 1}
		inv.Slots[MainSlot+2] = holdingsMap(2)
		inv.Slots[MainSlot+2].Raw[0].Data = []byte{0x80} // incomplete map_id VarInt
	})
	checkMapHoldings(t, &p, nil, false)
	p.Inventory(func(inv *Inventory) { inv.Slots[MainSlot+1].SetMapID(0) })
	checkMapHoldings(t, &p, []int32{0}, false)
}

func TestMapHoldingsInvisibilityEquipment(t *testing.T) {
	t.Parallel()
	var p Player
	p.Inventory(func(inv *Inventory) { inv.Slots[MainSlot] = holdingsMap(7) })
	checkMapHoldings(t, &p, []int32{7}, false)
	pumpkin := protocol.ItemStack{ItemID: int32(registry.ItemCarvedPumpkin), Count: 1}
	for _, slot := range equipmentSlots {
		if slot.window == OffHandSlot {
			continue
		}
		p.Inventory(func(inv *Inventory) { *inv.equipmentStack(slot.window) = pumpkin })
		checkMapHoldings(t, &p, []int32{7}, true)
		p.Inventory(func(inv *Inventory) { *inv.equipmentStack(slot.window) = protocol.ItemStack{} })
		checkMapHoldings(t, &p, []int32{7}, false)
	}
	p.Inventory(func(inv *Inventory) {
		inv.Slots[HotbarSlot] = pumpkin
		inv.Slots[OffHandSlot] = pumpkin
	})
	checkMapHoldings(t, &p, []int32{7}, false)
}

func TestMapHoldingsTagReload(t *testing.T) {
	// Tag reloads are global; this test must not run in parallel.
	t.Cleanup(func() { registry.ApplyPackTags(nil) })
	var p Player
	p.Inventory(func(inv *Inventory) {
		inv.Slots[ArmorSlot] = protocol.ItemStack{ItemID: int32(registry.ItemStone), Count: 1}
		inv.Slots[MainSlot] = holdingsMap(7)
	})
	checkMapHoldings(t, &p, []int32{7}, false)
	problems := registry.ApplyPackTags(registry.PackTags{"minecraft:item": {
		"minecraft:map_invisibility_equipment": {
			Replace: true,
			Entries: []registry.PackTagEntry{{ID: "minecraft:stone", Required: true}},
		},
	}})
	if len(problems) != 0 {
		t.Fatal(problems)
	}
	// No inventory access: the generation alone must refresh the hiding flag.
	checkMapHoldings(t, &p, []int32{7}, true)
	registry.ApplyPackTags(nil)
	checkMapHoldings(t, &p, []int32{7}, false)
}

func TestMapHoldingsConcurrentInventoryAccess(t *testing.T) {
	t.Parallel()
	var p Player
	p.Inventory(func(inv *Inventory) { inv.Slots[MainSlot] = holdingsMap(1) })
	var wg sync.WaitGroup
	wg.Go(func() {
		for i := range 100 {
			p.Inventory(func(inv *Inventory) { inv.Slots[MainSlot] = holdingsMap(int32(i%2 + 1)) })
		}
	})
	wg.Go(func() {
		for range 100 {
			p.MapHoldings(func(ids []int32, hidden bool) {
				if len(ids) != 1 || (ids[0] != 1 && ids[0] != 2) || hidden {
					t.Errorf("inconsistent holdings: %v, hidden %v", ids, hidden)
				}
			})
		}
	})
	wg.Wait()
}
