package mobs

import (
	"testing"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/player"
	"github.com/golem-mc/golem/internal/server/servertest"
	"github.com/golem-mc/golem/registry"
)

const goatOp = "Herder"

// goatWatch is what a client heard of one goat during a run of ticks.
type goatWatch struct {
	lowered, raised int
	hurt            bool
	motionX         float64
	motion          bool
	sounds          map[string]int
}

func readGoat(t *testing.T, c *servertest.Client, goat int32, w *goatWatch, data []byte, id int32) {
	t.Helper()
	switch id {
	case protocol.IDPlayEntityEvent:
		r := protocol.NewReader(data)
		if r.ReadI32() != goat {
			return
		}
		switch r.ReadU8() {
		case protocol.EntityStatusGoatLowerHead:
			w.lowered++
		case protocol.EntityStatusGoatRaiseHead:
			w.raised++
		}
	case protocol.IDPlaySoundEffect:
		name, _, _, _, _, _, _ := servertest.ReadAmbient(t, data)
		if w.sounds == nil {
			w.sounds = map[string]int{}
		}
		w.sounds[name]++
	case protocol.IDPlayDamageEvent:
		w.hurt = true
	case protocol.IDPlaySetEntityMotion:
		r := protocol.NewReader(data)
		if r.ReadVarInt() == c.PlayerEntityID {
			vx, _, _ := r.ReadLpVec3()
			w.motion, w.motionX = true, min(w.motionX, vx)
		}
	}
}

// TestAGoatPreparesAndRamsAPlayer: a goat eight blocks from a survival
// player with no ram cooldown walks to a run-up cell, lowers its head
// (entity event 58), charges, hurts the player and shoves them away from
// the goat, then raises its head (59).
func TestAGoatPreparesAndRamsAPlayer(t *testing.T) {
	t.Parallel()
	srv, c := servertest.BreedingPlayer(t, "survival", "Rammed", "", 0)
	servertest.HoldTicks(t, srv)
	id := srv.SpawnMob("minecraft:goat", 8.5, 64, 0.5)
	c.ExpectMobSpawn(protocol.EntityTypeGoat)
	srv.SetGoatRamCooldown(id, 0)
	var w goatWatch
	c.ReadTicks(srv, 400, func(packetID int32, data []byte) bool {
		readGoat(t, c, id, &w, data, packetID)
		return w.motion && w.raised > 0
	})
	if w.lowered == 0 {
		t.Error("the goat never lowered its head (entity event 58)")
	}
	if w.sounds["minecraft:entity.goat.prepare_ram"] == 0 {
		t.Error("no prepare_ram sound")
	}
	if w.sounds["minecraft:entity.goat.ram_impact"] == 0 {
		t.Error("no ram_impact sound")
	}
	if !w.hurt {
		t.Error("the ram dealt no damage")
	}
	if !w.motion || w.motionX >= -0.5 {
		t.Errorf("the player's motion x = %.2f (told %v), want a shove of more than half a block a tick away from the goat", w.motionX, w.motion)
	}
	if w.raised == 0 {
		t.Error("the goat never raised its head again (entity event 59)")
	}
}

// TestAGoatRammingABlockDropsAHorn: a goat that charges where its target
// stood and meets a log snaps a horn off (the item with an instrument
// of its tag), plays the impact and horn-break sounds, and loses the horn
// from its metadata.
func TestAGoatRammingABlockDropsAHorn(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name      string
		tag       string
		prepare   string
		instrumts []string
	}{
		{"regular", "{}", "minecraft:entity.goat.prepare_ram", []string{"ponder", "sing", "seek", "feel"}},
		{"screaming", "{IsScreamingGoat:1b}", "minecraft:entity.goat.screaming.prepare_ram", []string{"admire", "call", "yearn", "dream"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			srv, c := servertest.BreedingPlayer(t, "survival", goatOp, "", 0, servertest.OpsListing(t, goatOp))
			servertest.HoldTicks(t, srv)
			log := registry.MustState("minecraft:oak_log", map[string]string{"axis": "y"})
			srv.SetBlock(-1, 64, 0, log)
			srv.SetBlock(-1, 65, 0, log)
			c.ChatCommand("summon minecraft:goat 8.5 64 0.5 " + tc.tag)
			goat := c.ExpectMobSpawn(protocol.EntityTypeGoat)
			srv.SetGoatRamCooldown(goat.EntityID, 0)
			var w goatWatch
			var stack protocol.ItemStack
			c.ReadTicks(srv, 300, func(packetID int32, data []byte) bool {
				readGoat(t, c, goat.EntityID, &w, data, packetID)
				return w.sounds[tc.prepare] > 0
			})
			if w.sounds[tc.prepare] == 0 {
				t.Fatalf("the goat never prepared a ram; sounds %v", w.sounds)
			}
			// The charge is on: the player steps out of the way, so the goat
			// runs on into the log behind where they stood.
			c.MoveTo(0.5, 64, 6.5, 0, 0)
			c.Settle()
			dropped := false
			c.ReadTicks(srv, 200, func(packetID int32, data []byte) bool {
				readGoat(t, c, goat.EntityID, &w, data, packetID)
				if packetID == protocol.IDPlaySpawnEntity {
					drop := servertest.ParseItemSpawn(data)
					if drop.EntityID != 0 {
						c.ReadDropStack(&drop)
						stack, dropped = drop.Stack, true
					}
				}
				return dropped
			})
			if !dropped {
				t.Fatalf("no horn dropped; sounds %v", w.sounds)
			}
			horn, _ := registry.ItemID("minecraft:goat_horn")
			if stack.ItemID != horn {
				t.Fatalf("the drop is item %d, want the goat horn %d", stack.ItemID, horn)
			}
			// vanilla: ItemEntity's default pickup delay, 0
			for _, it := range srv.ItemsInDimension("minecraft:overworld") {
				if it.Stack.ItemID == horn && it.PickupDelay != 0 {
					t.Errorf("the dropped horn has pickup delay %d, want 0", it.PickupDelay)
				}
			}
			in, ok := stack.InstrumentOf()
			if !ok {
				t.Fatal("the horn carries no instrument")
			}
			reg, _ := registry.Get("minecraft:instrument")
			got := reg.Entries[in]
			found := false
			for _, name := range tc.instrumts {
				found = found || got == "minecraft:"+name+"_goat_horn"
			}
			if !found {
				t.Errorf("the instrument is %s, want one of %v", got, tc.instrumts)
			}
			c.ReadTicks(srv, 5, func(packetID int32, data []byte) bool {
				readGoat(t, c, goat.EntityID, &w, data, packetID)
				return false
			})
			if w.sounds["minecraft:entity.goat.horn_break"] == 0 {
				t.Error("no horn_break sound")
			}
			left, right := srv.GoatHornsLeft(goat.EntityID)
			if left == right {
				t.Errorf("horns left %v/%v, want exactly one lost", left, right)
			}
		})
	}
}

// TestMilkingAGoatFillsTheBucket: a grown goat fills the bucket and a
// bystander hears its milk sound, the screaming goat's own.
func TestMilkingAGoatFillsTheBucket(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		tag, sound string
	}{
		{"{}", "minecraft:entity.goat.milk"},
		{"{IsScreamingGoat:1b}", "minecraft:entity.goat.screaming.milk"},
	} {
		t.Run(tc.tag, func(t *testing.T) {
			t.Parallel()
			srv, connect := servertest.Start(t, servertest.MobTestConfig(), servertest.OpsListing(t, goatOp))
			c := connect().Join(goatOp)
			c.MoveToGround(0.5, 64, 0.5, true)
			c.Settle()
			srv.GiveSlot(goatOp, player.HotbarSlot, protocol.ItemStack{ItemID: servertest.MustItemID(t, "minecraft:bucket"), Count: 1})
			watcher := connect().Join("Watcher")
			watcher.Settle()
			c.Settle()
			servertest.HoldTicks(t, srv)
			c.ChatCommand("summon minecraft:goat 1.5 64 0.5 " + tc.tag)
			goat := c.ExpectMobSpawn(protocol.EntityTypeGoat)
			watcher.ExpectMobSpawn(protocol.EntityTypeGoat)
			c.UseOn(goat.EntityID)
			milk, _ := registry.ItemID("minecraft:milk_bucket")
			_, stack := c.ExpectContainerSlot()
			if stack.ItemID != milk {
				t.Errorf("the bucket became item %d, want the milk bucket %d", stack.ItemID, milk)
			}
			var w goatWatch
			watcher.ReadTicks(srv, 3, func(packetID int32, data []byte) bool {
				readGoat(t, watcher, goat.EntityID, &w, data, packetID)
				return false
			})
			if w.sounds[tc.sound] != 1 {
				t.Errorf("the watcher heard %v, want %s once", w.sounds, tc.sound)
			}
		})
	}
}
