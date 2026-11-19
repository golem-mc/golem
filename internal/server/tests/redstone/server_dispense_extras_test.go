package redstone

import (
	"slices"
	"testing"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/command"
	"github.com/golem-mc/golem/internal/server/servertest"
	"github.com/golem-mc/golem/internal/server/world"
	"github.com/golem-mc/golem/nbt"
	"github.com/golem-mc/golem/registry"
)

func TestADispenserStandsItsArmorStand(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name string
		face int32
		yaw  float32
	}{
		{"south", protocol.FaceSouth, 0}, {"west", protocol.FaceWest, 90},
		{"north", protocol.FaceNorth, 180}, {"east", protocol.FaceEast, 270},
		{"up", protocol.FaceTop, 270}, {"down", protocol.FaceBottom, 270},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			srv, w := servertest.Dispensing(t)
			off := protocol.FaceOffset[tc.face]
			x, y, z := int32(4)+off[0], int32(64)+off[1], off[2]
			// Even an occupied target must spawn the stand.
			srv.SetBlock(x, y, z, registry.MustState("minecraft:stone", nil))
			stack := servertest.ItemStack(t, "minecraft:armor_stand", 2)
			stack.CustomName = &protocol.Text{Text: "Display"}
			data, err := nbt.EncodeNetwork(nbt.Compound{"Small": nbt.Byte(1), "NoGravity": nbt.Byte(1)})
			if err != nil {
				t.Fatal(err)
			}
			var component protocol.Buffer
			component.PutVarInt(protocol.EntityTypeArmorStand)
			component.PutRaw(data)
			stack.Raw = []protocol.RawComponent{{Type: protocol.DataComponentEntityData, Data: component.Bytes()}}
			srv.SetInventoryAt(4, 64, 0, 0, stack)
			w.DrainRecorded()
			srv.Dispense(4, 64, 0, tc.face)
			stand := w.ExpectArmorStandSpawn()
			if stand.X != float64(x)+0.5 || stand.Y != float64(y) || stand.Z != float64(z)+0.5 || stand.Yaw != tc.yaw {
				t.Errorf("spawn at (%v,%v,%v), yaw %v; want (%v,%v,%v), yaw %v", stand.X, stand.Y, stand.Z, stand.Yaw, float64(x)+0.5, y, float64(z)+0.5, tc.yaw)
			}
			selected := srv.SelectedEntities("minecraft:armor_stand")
			if len(selected) != 1 {
				t.Fatalf("stands = %d, want 1", len(selected))
			}
			tag, ok := selected[0].(command.DataTarget).DataTag()
			name, named := protocol.TextOf(tag.Entries()["CustomName"])
			if !ok || !nbt.BoolField(tag.Entries(), "Small") || !nbt.BoolField(tag.Entries(), "NoGravity") || !named || name.Text != "Display" {
				t.Errorf("item data not applied: %v", tag)
			}
			if left := srv.InventoryAt(4, 64, 0)[0]; left.Count != 1 {
				t.Errorf("remaining stack = %+v, want one", left)
			}
			w.DrainRecorded()
			if !slices.Contains(w.Events, protocol.WorldEventDispense) {
				t.Errorf("missing default click: %v", w.Events)
			}
			if n := countDropped(srv.ItemsInDimension(world.DimensionName), "minecraft:armor_stand"); n != 0 {
				t.Errorf("dropped %d stands", n)
			}
		})
	}
}

func TestADispenserMakesMudWithAWaterBottle(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		potion, block string
		convert       bool
	}{
		{"water", "dirt", true}, {"water", "coarse_dirt", true}, {"water", "rooted_dirt", true},
		{"awkward", "dirt", false}, {"water", "stone", false},
	} {
		t.Run(tc.potion+"_"+tc.block, func(t *testing.T) {
			t.Parallel()
			srv, w := servertest.Dispensing(t)
			srv.SetBlock(4, 64, 1, registry.MustState("minecraft:"+tc.block, nil))
			srv.SetInventoryAt(4, 64, 0, 0, servertest.PotionBottle(t, "minecraft:potion", "minecraft:"+tc.potion))
			var events []string
			srv.ObserveGameEvents(func(event string, x, y, z float64) {
				if event == "minecraft:fluid_place" && x == 4.5 && y == 64.5 && z == 0.5 {
					events = append(events, event)
				}
			})
			w.DrainRecorded()
			srv.Dispense(4, 64, 0, protocol.FaceSouth)

			particles, emptySounds := 0, 0
			w.SettleAndCollect(func(id int32, data []byte) {
				w.Record(id, data)
				switch id {
				case protocol.IDPlayLevelParticles:
					r := protocol.NewReader(data)
					kind := r.ReadVarInt()
					if kind != protocol.ParticleSplash {
						return
					}
					tail := servertest.ReadParticleTail(t, r)
					x, y, z, dx, dy, dz, speed, count := tail.X, tail.Y, tail.Z, tail.DX, tail.DY, tail.DZ, tail.Speed, tail.Count
					particles++
					if x < 4 || x >= 5 || y != 65 || z < 0 || z >= 1 || dx != 0 || dy != 0 || dz != 0 || speed != 1 || count != 1 || r.Err() != nil {
						t.Errorf("splash at (%v,%v,%v), offset (%v,%v,%v), speed %v count %d: %v", x, y, z, dx, dy, dz, speed, count, r.Err())
					}
				case protocol.IDPlaySoundEffect:
					if !servertest.SoundIs(data, "minecraft:item.bottle.empty") {
						return
					}
					emptySounds++
					sound := servertest.DecodePlaySoundEffect(t, data)
					if sound.X != 36 || sound.Y != 516 || sound.Z != 4 || sound.Category != protocol.SoundCategoryBlock || sound.Volume != 1 || sound.Pitch != 1 {
						t.Errorf("empty sound = %+v", sound)
					}
				}
			})
			wantParticles, wantSounds, wantClicks := 0, 0, 2
			if tc.convert {
				wantParticles, wantSounds, wantClicks = 5, 1, 1
			}
			if particles != wantParticles || emptySounds != wantSounds {
				t.Errorf("effects = %d splashes, %d sounds; want %d, %d", particles, emptySounds, wantParticles, wantSounds)
			}
			clicks := 0
			for _, event := range w.Events {
				if event == protocol.WorldEventDispense {
					clicks++
				}
			}
			if clicks != wantClicks {
				t.Errorf("clicks = %d, want %d", clicks, wantClicks)
			}
			block := tc.block
			if tc.convert {
				block = "mud"
			}
			if got := srv.BlockAt(4, 64, 1); got != registry.MustState("minecraft:"+block, nil) {
				t.Errorf("block = %d, want %s", got, block)
			}
			left := srv.InventoryAt(4, 64, 0)[0]
			dropped := countDropped(srv.ItemsInDimension(world.DimensionName), "minecraft:potion")
			if tc.convert {
				if left.ItemID != int32(registry.ItemGlassBottle) || left.Count != 1 || dropped != 0 {
					t.Errorf("slot = %+v, dropped = %d", left, dropped)
				}
				if len(events) != 1 {
					t.Errorf("fluid_place events = %v", events)
				}
			} else if !left.Empty() || dropped != 1 || len(events) != 0 {
				t.Errorf("slot = %+v, dropped = %d, events = %v", left, dropped, events)
			}
		})
	}
}
