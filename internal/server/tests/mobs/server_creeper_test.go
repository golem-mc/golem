package mobs

import (
	"testing"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/player"
	"github.com/golem-mc/golem/internal/server/rules"
	"github.com/golem-mc/golem/internal/server/servertest"
	"github.com/golem-mc/golem/registry"
)

// The spider and the creeper tests: the second and third monsters, driven
// the way the zombie's are — through a real connection, in the flat world
// the harness grows, where every mob is one the test planted. The spider
// hunts in the dark and ignores a player it is not struck by in the light,
// climbs the wall it is pressed against, and drops its loot when a player
// kills it; the creeper swells when its target comes close, goes up at the
// end of the fuse — the blast destroying the blocks around it and running
// the player's hurt pipeline — stops swelling when the target walks
// out of reach, and lights its fuse at a creeper_igniters item whatever
// its targets do.

// blastSkip is the background a watching client hears while a monster is
// being watched: the world's own traffic. The movement packets are kept —
// the climb test reads a spider's rise out of them — and so are the sound
// effects, the priming of a fuse being one of the things the tests watch
// for.
func blastSkip(id int32) bool {
	switch id {
	case protocol.IDPlayUpdateTime,
		protocol.IDPlayUpdateLight, protocol.IDPlayBlockUpdate, protocol.IDPlaySectionBlocks,
		protocol.IDPlayChunkData, protocol.IDPlayChunkBatchStart,
		protocol.IDPlayChunkBatchFinished, protocol.IDPlayCenterChunk,
		protocol.IDPlayUnloadChunk, protocol.IDPlayWorldEvent,
		protocol.IDPlaySetEntityMotion,
		protocol.IDPlayEntityAnimation, protocol.IDPlayEntityEquipment,
		protocol.IDPlayContainerSlot, protocol.IDPlayPingResponse:
		return true
	}
	return false
}

// TestACreeperSwellsAndExplodes walks the whole of a creeper's week over
// the wire: the fuse begun beside a survival player — the swell direction
// in its metadata and the priming sound with it — the Explosion packet at
// the end of the thirty ticks, the blocks around it set to air, the player
// hurt by the blast through the pipeline, and the body gone without so
// much as a corpse.
func TestACreeperSwellsAndExplodes(t *testing.T) {
	t.Parallel()
	cfg := servertest.HostileTestConfig()

	srv, c, playerID := servertest.JoinAs(t, cfg, "Bystander")

	// A ring of stone at the creeper's own feet level, a block out from
	// where it will stand: the blast's first food, its crater's chew.
	stone, ok := registry.FindState("minecraft:stone", nil)
	if !ok {
		t.Fatal("the block registry has no stone")
	}
	for dx := int32(1); dx <= 3; dx++ {
		for dz := int32(-1); dz <= 1; dz++ {
			if dx != 2 || dz != 0 { // the creeper's own cell stays open
				srv.SetBlock(dx, 64, dz, stone)
			}
		}
	}

	creeperID := srv.SpawnMob("minecraft:creeper", 2.5, 64, 0.5)
	creeper := c.ExpectMobSpawn(protocol.EntityTypeCreeper)
	if creeper.EntityID != creeperID {
		t.Fatalf("spawned creeper's entity ID = %d, want the one SpawnMob reported (%d)", creeper.EntityID, creeperID)
	}
	if creeper.Health != 20 {
		t.Errorf("spawned creeper's health = %v, want the vanilla twenty", creeper.Health)
	}
	if creeper.SpeedAttribute != 0.25 {
		t.Errorf("spawned creeper's speed = %v, want the vanilla 0.25", creeper.SpeedAttribute)
	}

	swelling := false
	primed := false
	exploded := false
	var blast servertest.Explosion
	hurt := false
	damageEvent := false
	removed := false
	blocksGone := 0
	pumpTicks(c, srv, 400, blastSkip, func(id int32, data []byte) bool {
		switch id {
		case protocol.IDPlayEntityMetadata:
			// The crater's drops spawn items whose metadata is no
			// monster's; only the creeper's own packets are parsed.
			if protocol.NewReader(data).ReadVarInt() != creeperID {
				return false
			}
			m := servertest.ReadMonsterMetadata(c, data)
			if m.HasSwell && m.Swell == 1 {
				swelling = true
			}
		case protocol.IDPlaySoundEffect:
			r := protocol.NewReader(data)
			sound := r.ReadVarInt()
			var name string
			if sound == 0 {
				name = r.ReadString(32767) // an inline definition
			} else {
				name, _ = servertest.SoundEventName(sound - 1)
			}
			if name == "minecraft:entity.creeper.primed" {
				primed = true
			}
		case protocol.IDPlayExplosion:
			blast = servertest.ReadExplosion(c, data)
			if blast.Radius != 3.0 {
				c.T.Errorf("explosion radius = %v, want the creeper's 3", blast.Radius)
			}
			exploded = true
		case protocol.IDPlaySetHealth:
			h := servertest.ExpectHealthReport(c, data)
			if h.Health < 20 && !hurt {
				hurt = true
			}
		case protocol.IDPlayDamageEvent:
			r := protocol.NewReader(data)
			victim := r.ReadVarInt()
			if victim == playerID {
				if got, want := r.ReadVarInt(), registry.MustID("minecraft:damage_type", rules.DamageTypePlayerExplosion.Name()); got != want {
					c.T.Errorf("damage type ID = %d, want %d", got, want)
				}
				if got := r.ReadVarInt(); got != creeperID+1 {
					c.T.Errorf("damage cause ID = %d, want the creeper's %d", got, creeperID+1)
				}
				if got := r.ReadVarInt(); got != creeperID+1 {
					c.T.Errorf("damage direct ID = %d, want the creeper's %d", got, creeperID+1)
				}
				damageEvent = true
			}
		case protocol.IDPlayRemoveEntities:
			r := protocol.NewReader(data)
			if got := r.ReadVarInt(); got != 1 {
				c.T.Errorf("removed entities = %d, want 1", got)
			}
			if got := r.ReadVarInt(); got != creeperID {
				c.T.Errorf("removed entity = %d, want the creeper's %d", got, creeperID)
			}
			removed = true
		}
		// The crater: the four stone blocks standing directly beside the
		// creeper's feet cell are the first cells the rays out of the
		// centre reach, and the blast's chew on them is what the world
		// reads back as air.
		if exploded {
			blocksGone = 0
			for _, pos := range [][3]int32{{1, 64, 0}, {3, 64, 0}, {2, 64, 1}, {2, 64, -1}} {
				if srv.BlockAt(pos[0], pos[1], pos[2]) == 0 {
					blocksGone++
				}
			}
			return swelling && primed && hurt && damageEvent && removed && blocksGone >= 3
		}
		return false
	})
	if !swelling {
		t.Error("the creeper never put the swell direction in its metadata")
	}
	if !primed {
		t.Error("the fuse's first tick never played the priming")
	}
	if !exploded {
		t.Fatal("the creeper never exploded")
	}
	if blast.X != 2.5 || blast.Y != 64 || blast.Z != 0.5 {
		t.Errorf("the blast stood at (%v %v %v), want the creeper's own feet (2.5 64 0.5)", blast.X, blast.Y, blast.Z)
	}
	if !hurt {
		t.Error("the blast never hurt the player beside it")
	}
	if !damageEvent {
		t.Error("the blast never came with its damage event")
	}
	if !removed {
		t.Error("the exploding creeper's body was never taken out of the world")
	}
	if blocksGone < 3 {
		t.Errorf("the blast left %d of the four neighbouring blocks standing, want the crater's chew", blocksGone)
	}
	if n := srv.MobCount(); n != 0 {
		t.Errorf("the world keeps %d mobs, want the exploded creeper gone from the herd too", n)
	}
}

// TestFlintAndSteelLightsACreeper checks the igniters on a creeper: flint
// and steel clicked on one lights the fuse — the swell direction and the
// ignited boolean up in the metadata, the priming with it — and the blast
// waits out the vanilla thirty ticks of fuse before it takes the body. The
// clicker is creative, so the creeper hunts nobody: the lit fuse is the only
// one running.
func TestFlintAndSteelLightsACreeper(t *testing.T) {
	t.Parallel()
	cfg := servertest.HostileTestConfig()
	cfg.GameMode = "creative"

	srv, c := servertest.Join(t, cfg, "Pyromaniac")

	flintID, ok := registry.ItemID("minecraft:flint_and_steel")
	if !ok {
		t.Fatal("the item registry has no flint and steel")
	}
	srv.GiveSlot("Pyromaniac", player.HotbarSlot, protocol.ItemStack{ItemID: flintID, Count: 1})

	creeperID := srv.SpawnMob("minecraft:creeper", 2.5, 64, 0.5)
	creeper := c.ExpectMobSpawn(protocol.EntityTypeCreeper)
	if creeper.EntityID != creeperID {
		t.Fatalf("spawned creeper's entity ID = %d, want the one SpawnMob reported (%d)", creeper.EntityID, creeperID)
	}

	// The fuse is stepped by hand rather than on the wall clock: a loop that
	// catches up after a stall runs its missed ticks back to back, so a blast
	// can land in the settle tail of a count that has already stopped.
	srv.StopTick()
	c.UseOn(creeperID)
	// The connection reads one packet at a time, so the ping's answer means
	// the click has lit the fuse before the first stepped tick.
	c.Settle()

	var (
		swelling bool
		lit      bool
		primed   bool
		exploded bool
		removed  bool
	)
	record := func(id int32, data []byte) {
		switch id {
		case protocol.IDPlayEntityMetadata:
			// The blast's cobblestone drops carry their stacks in
			// metadata of their own; only the creeper's state is this
			// test's.
			r := protocol.NewReader(data)
			if r.ReadVarInt() != creeperID {
				return
			}
			m := servertest.ReadMonsterMetadata(c, data)
			if m.HasSwell && m.Swell == 1 {
				swelling = true
			}
			if m.HasIgnite && m.Ignited {
				lit = true
			}
		case protocol.IDPlaySoundEffect:
			if servertest.SoundIs(data, "minecraft:entity.creeper.primed") {
				primed = true
			}
		case protocol.IDPlayExplosion:
			blast := servertest.ReadExplosion(c, data)
			if blast.Radius != 3.0 {
				c.T.Errorf("explosion radius = %v, want the creeper's 3", blast.Radius)
			}
			exploded = true
		case protocol.IDPlayRemoveEntities:
			// A removal naming the creeper is the blast's; the drops'
			// own pickups and merges are background.
			r := protocol.NewReader(data)
			for n := r.ReadVarInt(); n > 0; n-- {
				if r.ReadVarInt() == creeperID {
					removed = true
				}
			}
		}
	}
	// Twenty-nine ticks: the lit fuse swells, but the blast waits for the
	// thirtieth.
	srv.TickMobs(29)
	c.SettleAndCollect(record)
	if !swelling {
		t.Error("the lit fuse never showed the swell direction in the metadata")
	}
	if !lit {
		t.Error("the lit fuse never showed the ignited boolean in the metadata")
	}
	if !primed {
		t.Error("the lighting never played the priming")
	}
	if exploded {
		t.Fatal("the blast came before the fuse's thirty ticks had run")
	}
	srv.TickMobs(1)
	c.SettleAndCollect(record)
	if !exploded {
		t.Fatal("the lit creeper never exploded")
	}
	if !removed {
		t.Error("the exploding creeper's body was never taken out of the world")
	}
	if n := srv.MobCount(); n != 0 {
		t.Errorf("the world keeps %d mobs, want the exploded creeper gone from the herd too", n)
	}
}
