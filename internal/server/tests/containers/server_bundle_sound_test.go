package containers

import (
	"testing"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/player"
	"github.com/golem-mc/golem/internal/server/servertest"
)

// vanilla: BundleItem.overrideStackedOnOther, overrideOtherStackedOnMe
func TestBundleClicksPlaySoundsOnlyToNearbyPlayers(t *testing.T) {
	t.Parallel()
	dirt := servertest.ItemStack(t, "minecraft:dirt", 64)
	empty := rawBundle(t)
	full := rawBundle(t, dirt)
	for _, tc := range []struct {
		name          string
		held, clicked protocol.ItemStack
		button        int8
		slot          int16
		sound         string
	}{
		{"cursor insert", empty, dirt, 0, 37, "insert"},
		{"cursor full", full, dirt, 0, 37, "insert_fail"},
		{"cursor refused item", empty, servertest.ItemStack(t, "minecraft:shulker_box", 1), 0, 37, "insert_fail"},
		{"cursor remove", full, protocol.ItemStack{}, 1, 37, "remove_one"},
		{"cursor remove refused", full, protocol.ItemStack{}, 1, 5, ""},
		{"cursor remove partial", rawBundle(t, servertest.ItemStack(t, "minecraft:carved_pumpkin", 64)), protocol.ItemStack{}, 1, 5, ""},
		{"slot insert refused", dirt, empty, 0, 5, "insert_fail"},
		{"slot remove refused", protocol.ItemStack{}, full, 1, 5, ""},
		{"cursor remove empty", empty, protocol.ItemStack{}, 1, 37, ""},
		{"slot insert", dirt, empty, 0, 37, "insert"},
		{"slot full", dirt, full, 0, 37, "insert_fail"},
		{"slot remove", protocol.ItemStack{}, full, 1, 37, "remove_one"},
		{"slot remove empty", protocol.ItemStack{}, empty, 1, 37, ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			srv, c := servertest.Join(t, servertest.BlockTestConfig(), "Sorter")
			watcher := servertest.DialServer(t, srv).Join("Watcher")
			servertest.HoldTicks(t, srv)
			c.MoveTo(1.5, 64, 1.5, 0, 0)
			watcher.MoveTo(4.5, 64, 1.5, 0, 0)
			c.Settle()
			watcher.Settle()
			w := &servertest.WindowedClient{Client: c}
			srv.GiveSlot("Sorter", player.HotbarSlot, tc.held)
			srv.GiveSlot("Sorter", int(tc.slot), tc.clicked)
			if !tc.held.Empty() {
				w.Click(36, 0, protocol.ClickModePickup)
				c.Settle()
			}
			// Drain setup before watching both sides of the click.
			watcher.Settle()
			w.Click(tc.slot, tc.button, protocol.ClickModePickup)
			var clickerSounds []soundHeard
			collect := func(sounds *[]soundHeard) func(int32, []byte) bool {
				return func(id int32, data []byte) bool {
					if id == protocol.IDPlaySoundEffect {
						var sound soundHeard
						sound.read(t, data)
						for _, name := range []string{"insert", "insert_fail", "remove_one"} {
							if sound.sound == soundRegistryWireID(t, "minecraft:item.bundle."+name) {
								*sounds = append(*sounds, sound)
							}
						}
					}
					return false
				}
			}
			c.ReadTicks(srv, 0, collect(&clickerSounds))
			var heard []soundHeard
			watcher.ReadTicks(srv, 0, collect(&heard))
			if len(clickerSounds) != 0 {
				t.Errorf("clicker received %d server bundle sounds", len(clickerSounds))
			}
			if tc.sound == "" {
				if len(heard) != 0 {
					t.Fatalf("watcher heard %v on a silent click", heard)
				}
				return
			}
			if len(heard) != 1 {
				t.Fatalf("watcher heard %d bundle sounds, want one %s", len(heard), tc.sound)
			}
			s := heard[0]
			if s.sound != soundRegistryWireID(t, "minecraft:item.bundle."+tc.sound) || s.category != protocol.SoundCategoryPlayer || s.x != 12 || s.y != 512 || s.z != 12 {
				t.Errorf("sound = %+v, want %s in players at (1.5,64,1.5)", s, tc.sound)
			}
			if tc.sound == "insert_fail" {
				if s.volume != 1 || s.pitch != 1 {
					t.Errorf("failure volume/pitch = %v/%v, want 1/1", s.volume, s.pitch)
				}
			} else if s.volume != 0.8 || s.pitch < 0.8 || s.pitch > 1.2 {
				t.Errorf("volume/pitch = %v/%v, want 0.8/[0.8,1.2]", s.volume, s.pitch)
			}
		})
	}
}
