package entity

import "testing"

func TestIndexedWantedItemsMatchDirectSensing(t *testing.T) {
	t.Run("piglin", func(t *testing.T) {
		w := griefingWorld{newTestWorld()}
		stone := defaultState(t, "minecraft:stone")
		w.set(16, 64, 2, stone)
		w.set(16, 65, 2, stone)
		gold := stackOf(t, "minecraft:gold_ingot")
		items := make([]LootItem, 32)
		copy(items, []LootItem{
			{X: 36.5, Y: 64, Z: 0.5, Stack: gold}, // first of two equally distant items across a cell boundary
			{X: -3.5, Y: 64, Z: 0.5, Stack: gold},
			{X: 16.5, Y: 81, Z: 0.5, Stack: gold}, // closer, but too high
			{X: 16.5, Y: 64, Z: 4.5, Stack: gold}, // closer, but behind a wall
		})
		mobs := make(Mobs, 8)
		for i := range mobs {
			mobs[i] = NewMob(TypePiglin, int32(i+1), 16.5, 64, 0.5, int64(i+1))
			mobs[i].CanPickUpLoot = true
		}
		var scratch BoxGrid
		for _, indexed := range []bool{false, true} {
			for _, m := range mobs {
				m.barter.scanIn, m.barter.hasWanted = 1, false
			}
			if indexed {
				grid := mobs.PrepareLootGrid(items, &scratch, true)
				if grid == nil {
					t.Fatal("due piglins did not get a loot grid")
				}
				mobs.SenseWantedItemsWithGrid(w, items, grid, true)
			} else {
				mobs.SenseWantedItems(w, items)
			}
			for _, m := range mobs {
				if !m.barter.hasWanted || m.barter.wanted.X != items[0].X || m.barter.scanIn != piglinScanTicks {
					t.Fatalf("indexed=%t: wanted item at (%g, %g, %g), found=%t, scanIn=%d; want the first visible ingot and a reset timer",
						indexed, m.barter.wanted.X, m.barter.wanted.Y, m.barter.wanted.Z, m.barter.hasWanted, m.barter.scanIn)
				}
			}
		}
	})

	t.Run("sulfur cube with mob griefing disabled", func(t *testing.T) {
		w := newTestWorld()
		stone := stackOf(t, "minecraft:stone")
		items := make([]LootItem, 32)
		copy(items, []LootItem{
			{X: 23.5, Y: 64, Z: 4.5, Stack: stone},
			{X: 8.5, Y: 64, Z: 4.5, Stack: stone},
			{X: 16, Y: 55.75, Z: 0.5, Stack: stone}, // closer, but below the search box
			{X: 16, Y: 64, Z: 1.5, Stack: stackOf(t, "minecraft:stick")},
		})
		mobs := make(Mobs, 8)
		for i := range mobs {
			mobs[i] = NewMob(TypeSulfurCube, int32(i+1), 16, 64, 0.5, int64(i+1))
		}
		var scratch BoxGrid
		for _, indexed := range []bool{false, true} {
			for _, m := range mobs {
				m.sulfur.hasWanted = false
			}
			if indexed {
				grid := mobs.PrepareLootGrid(items, &scratch, false)
				if grid == nil {
					t.Fatal("searching cubes did not get a loot grid with mob griefing disabled")
				}
				mobs.SenseWantedItemsWithGrid(w, items, grid, false)
			} else {
				mobs.SenseWantedItems(w, items)
			}
			for _, m := range mobs {
				if !m.sulfur.hasWanted || m.sulfur.wanted.X != items[0].X || m.sulfur.wantedUntil != m.Age+2 {
					t.Fatalf("indexed=%t: wanted item at (%g, %g, %g), found=%t, until=%d; want the first stone in reach for two ticks",
						indexed, m.sulfur.wanted.X, m.sulfur.wanted.Y, m.sulfur.wanted.Z, m.sulfur.hasWanted, m.sulfur.wantedUntil)
				}
			}
		}
	})
}

func TestLootGridCountsPickersAndDueSensors(t *testing.T) {
	mobs := make(Mobs, 8)
	for i := range mobs {
		if i < 4 {
			mobs[i] = NewMob(TypeZombie, int32(i+1), 0, 64, 0, int64(i+1))
			mobs[i].CanPickUpLoot = true
			continue
		}
		m := NewMob(TypeAllay, int32(i+1), 0, 64, 0, int64(i+1))
		m.MainHand = stackOf(t, "minecraft:stick")
		m.allay.pickupWaitSet = true
		m.barter.scanIn = 1
		mobs[i] = m
	}
	var scratch BoxGrid
	items := make([]LootItem, 32)
	if grid := mobs.PrepareLootGrid(items[:31], &scratch, true); grid != nil {
		t.Fatal("31 drops built a grid")
	}
	if grid := mobs.PrepareLootGrid(items, &scratch, true); grid == nil {
		t.Fatal("four pickers and four due sensors did not build a grid")
	}
	mobs[7].barter.scanIn = 2
	if grid := mobs.PrepareLootGrid(items, &scratch, true); grid != nil {
		t.Fatal("four pickers and three due sensors built a grid")
	}
}
