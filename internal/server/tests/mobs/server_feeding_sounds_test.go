package mobs

import (
	"fmt"
	"testing"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/player"
	"github.com/golem-mc/golem/internal/server/servertest"
)

// vanilla: Animal.mobInteract and each kind's playEatingSound override.
func TestAnimalFeedingSounds(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		kind, food, tag, sound string
		foodCount              int32
		creative               bool
		count                  int
		volume, low, high      float32
	}{
		{"fox", "sweet_berries", "", "fox.eat", 2, false, 1, 1, 1, 1},
		{"fox", "sweet_berries", "Age:-24000,", "fox.eat", 2, false, 1, 1, 1, 1},
		{"panda", "bamboo", "", "panda.eat", 2, false, 0, 0, 0, 0},
		{"panda", "bamboo", "Age:-24000,", "panda.eat", 2, false, 0, 0, 0, 0},
		{"frog", "slime_ball", "", "frog.eat", 2, false, 1, 2, 1, 1},
		{"goat", "wheat", "", "goat.eat", 2, false, 2, 1, 0.8, 1.2},
		{"goat", "wheat", "Age:-24000,", "goat.eat", 2, false, 2, 1, 0.8, 1.2},
		{"goat", "wheat", "IsScreamingGoat:1b,", "goat.screaming.eat", 2, false, 2, 1, 0.8, 1.2},
		{"sniffer", "torchflower_seeds", "", "sniffer.eat", 2, false, 2, 1, 0.8, 1.2},
		{"sniffer", "torchflower_seeds", "Age:-24000,", "sniffer.eat", 2, false, 2, 1, 0.8, 1.2},
		{"cat", "cod", "", "cat.eat", 2, false, 1, 1, 1, 1},
		{"cat", "cod", "Age:-24000,", "baby_cat.eat", 2, false, 1, 1, 1, 1},
		{"strider", "warped_fungus", "", "strider.eat", 2, false, 1, 1, 0.8, 1.2},
		{"strider", "warped_fungus", "Age:-24000,", "strider.eat", 2, false, 1, 1, 0.8, 1.2},
		{"goat", "wheat", "", "goat.eat", 1, false, 1, 1, 0.8, 1.2},
		{"goat", "wheat", "", "goat.eat", 1, true, 2, 1, 0.8, 1.2},
		{"sniffer", "torchflower_seeds", "", "sniffer.eat", 1, false, 2, 1, 0.8, 1.2},
		{"sulfur_cube", "slime_ball", "Size:0,Age:-24000,", "small_sulfur_cube.eat", 2, false, 1, 0.4, 1.3, 1.7},
	} {
		for _, silent := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/%s/food=%d/creative=%t/silent=%t", tc.kind, tc.tag, tc.foodCount, tc.creative, silent), func(t *testing.T) {
				t.Parallel()
				const name = "Feeder"
				mode := "survival"
				if tc.creative {
					mode = "creative"
				}
				srv, c := servertest.BreedingPlayer(t, mode, name, "minecraft:"+tc.food, tc.foodCount, servertest.OpsListing(t, name))
				servertest.HoldTicks(t, srv)
				tag := tc.tag
				if tc.kind == "cat" {
					tag += `Owner:"` + protocol.OfflineUUID(name).String() + `",sound_variant:"minecraft:classic",`
				}
				if silent {
					tag += "Silent:1b,"
				}
				c.ChatCommand("summon minecraft:" + tc.kind + " 1.5 64 2.5 {" + tag + "NoAI:1b}")
				id := protocol.NewReader(c.ExpectMobPacket(protocol.IDPlaySpawnEntity)).ReadVarInt()
				c.UseOn(id)
				heard := 0
				c.ReadTicks(srv, 3, func(packet int32, data []byte) bool {
					if packet == protocol.IDPlaySoundEffect {
						sound, category, x, y, z, volume, pitch := servertest.ReadAmbient(t, data)
						if sound == "minecraft:entity."+tc.sound {
							heard++
							if category != protocol.SoundCategoryNeutral || x != 12 || y != 512 || z != 20 {
								t.Errorf("sound category/position = %d (%d,%d,%d), want neutral (12,512,20)", category, x, y, z)
							}
							if volume != tc.volume || pitch < tc.low || pitch > tc.high {
								t.Errorf("volume/pitch = %v/%v, want %v/[%v,%v]", volume, pitch, tc.volume, tc.low, tc.high)
							}
						}
					}
					return false
				})
				want := tc.count
				if silent {
					want = 0
				}
				if heard != want {
					t.Errorf("heard %d %s, want %d", heard, tc.sound, want)
				}
				wantLeft := tc.foodCount - 1
				if tc.creative {
					wantLeft = tc.foodCount
				}
				if left := srv.InventorySlot(name, player.HotbarSlot).Count; left != wantLeft {
					t.Errorf("food left = %d, want %d after feeding", left, wantLeft)
				}
			})
		}
	}
}
