package raids

import (
	"testing"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/registry"
)

// vanilla: SetRaidStatus, ResetRaidStatus, RingBell
func TestVillagersGatherForARaidAndResumeWhenItStops(t *testing.T) {
	t.Parallel()
	srv, c, bell := village(t, 2)
	startRaid(t, c, srv, 0)
	srv.SetPlayerGameMode("Hero", protocol.GameModeSpectator)
	id := srv.SpawnMobSeeded("minecraft:villager", float64(bell[0])+0.5, float64(bell[1]), float64(bell[2])+0.5, 2162)
	c.WatchVariant(srv, "minecraft:villager")
	rang := false
	c.ReadTicks(srv, 200, func(pid int32, data []byte) bool {
		if pid == protocol.IDPlayBlockAction {
			r := protocol.NewReader(data)
			x, y, z := r.ReadPosition()
			action, direction, block := r.ReadU8(), r.ReadU8(), r.ReadVarInt()
			if [3]int32{x, y, z} == bell {
				if err := r.Err(); err != nil {
					t.Fatal(err)
				}
				if action != 1 || direction != uint8(registry.StateFacing(srv.BlockAt(x, y, z))) || block != int32(registry.BlockBell) {
					t.Fatalf("bell action = (%d, %d, %d)", action, direction, block)
				}
				rang = true
			}
		}
		return false
	})
	if !rang {
		t.Fatal("villager did not send the meeting bell's block action before wave one")
	}
	if got := srv.VillagerActivity(id); got != "pre_raid" {
		t.Fatalf("activity %q before wave one, want pre_raid", got)
	}
	run(c, srv, 230)
	if r := srv.Raids()[0]; !r.Started || r.Groups != 1 {
		t.Fatalf("raid = %+v, want wave one spawned", r)
	}
	if got := srv.VillagerActivity(id); got != "raid" {
		t.Fatalf("activity %q during wave one, want raid", got)
	}
	// Difficulty zero stops the real raid, and the next view must clear it.
	srv.SetDifficulty(0)
	run(c, srv, 100)
	if got := srv.VillagerActivity(id); got == "pre_raid" || got == "raid" {
		t.Fatalf("stopped raid kept activity %q", got)
	}
}

// vanilla: Villager.customServerAiStep
func TestVillagersSweatDuringAnActiveRaid(t *testing.T) {
	t.Parallel()
	srv, c, bell := village(t, 2)
	startRaid(t, c, srv, 0)
	srv.SetPlayerGameMode("Hero", protocol.GameModeSpectator)
	id := srv.SpawnMobSeeded("minecraft:villager", float64(bell[0])+0.5, float64(bell[1]), float64(bell[2])+0.5, 2162)
	still := srv.SpawnMobSeeded("minecraft:villager", float64(bell[0])+1.5, float64(bell[1]), float64(bell[2])+0.5, 2162)
	srv.SetMobNoAI(still)
	c.WatchVariant(srv, "minecraft:villager")
	sweat := 0
	c.ReadTicks(srv, 1000, func(pid int32, data []byte) bool {
		if pid != protocol.IDPlayEntityEvent {
			return false
		}
		r := protocol.NewReader(data)
		mob, status := r.ReadI32(), r.ReadU8()
		if err := r.Err(); err != nil {
			t.Fatal(err)
		}
		if status == 42 {
			if mob == still {
				t.Fatal("NoAI villager sent splash particles during the raid")
			}
			if mob == id {
				sweat++
			}
		}
		return false
	})
	if sweat == 0 {
		t.Fatal("villager sent no splash particles in 1000 raid ticks")
	}
	if r := srv.Raids()[0]; !r.Active || r.Status != "ongoing" {
		t.Fatalf("raid = %+v, want an active, ongoing raid", r)
	}
	srv.SetDifficulty(0)
	c.ExpectNoneWithinTicks(srv, 1000, func(pid int32, data []byte) bool {
		if pid != protocol.IDPlayEntityEvent {
			return false
		}
		r := protocol.NewReader(data)
		mob, status := r.ReadI32(), r.ReadU8()
		return mob == id && status == 42
	})
}
