package entities

import (
	"math"
	"testing"

	"github.com/golem-mc/golem/internal/server/servertest"
	"github.com/golem-mc/golem/registry"
)

// The skeleton trap: a thunderstorm strike that rolls it leaves a trap horse
// under a harmless bolt, and a player coming near springs it.

// TestAThunderstormTrapStrikeLeavesAHorseAndNoFire checks the strike's trap
// roll: with it forced, each strike stands a waiting skeleton horse on its
// cell and its bolt lights nothing.
// vanilla: ServerLevel.tickThunder
func TestAThunderstormTrapStrikeLeavesAHorseAndNoFire(t *testing.T) {
	t.Parallel()
	srv, c := servertest.Join(t, servertest.BlockTestConfig(), "Watcher")
	srv.StopTick()
	srv.SetDifficulty(2)
	thunderstorm(srv)
	srv.SetLightningOdds(1)
	srv.SetSkeletonTrapOdds(1)

	srv.TickRandomTicks(1)
	arrived, _ := boltWatch(c, srv, 1)
	if len(arrived) == 0 {
		t.Fatal("a thunderstorm with the die forced showed no lightning bolt")
	}
	for _, b := range arrived {
		name, _, _ := registry.BlockStateName(srv.BlockAt(int32(math.Floor(b.x)), int32(math.Floor(b.y)), int32(math.Floor(b.z))))
		if name == "minecraft:fire" {
			t.Errorf("the trap's bolt at (%v, %v, %v) lit a fire", b.x, b.y, b.z)
		}
	}
	horses := srv.MobIDsOfKind("minecraft:skeleton_horse")
	if len(horses) != len(arrived) {
		t.Fatalf("%d skeleton horses for %d trap strikes, want one each", len(horses), len(arrived))
	}
	for _, id := range horses {
		if trap, tame, _ := srv.HorseTrap(id); !trap || tame {
			t.Errorf("horse %d: trap %v tame %v, want an untamed trap", id, trap, tame)
		}
		x, y, z, _ := srv.MobPos(id)
		struck := false
		for _, b := range arrived {
			struck = struck || b.x == x+0.5 && b.y == y && b.z == z+0.5
		}
		if !struck {
			t.Errorf("horse %d stands at (%v, %v, %v), under no bolt", id, x, y, z)
		}
	}
}

// TestASprungSkeletonTrapSeatsFourArmedRiders checks the trap goal: a player
// within ten blocks springs it into a harmless bolt and four tame skeleton
// horses, each ridden by a persistent skeleton in a helmet.
// vanilla: SkeletonTrapGoal
func TestASprungSkeletonTrapSeatsFourArmedRiders(t *testing.T) {
	t.Parallel()
	srv, c := servertest.Join(t, servertest.SurvivalTestConfig(), "Near")
	srv.StopTick()
	srv.SetDifficulty(2)
	pos := srv.PlayerPosition("Near")
	trap := srv.SpawnTrapHorse(pos.X+5, pos.Y, pos.Z)

	for range 3 {
		srv.TickMobs(1)
	}
	if still, tame, _ := srv.HorseTrap(trap); still || !tame {
		t.Fatalf("after three ticks near a player: trap %v tame %v, want sprung and tame", still, tame)
	}
	arrived, _ := boltWatch(c, srv, 1)
	if len(arrived) != 1 {
		t.Fatalf("the sprung trap showed %d bolts, want 1", len(arrived))
	}
	name, _, _ := registry.BlockStateName(srv.BlockAt(int32(math.Floor(arrived[0].x)), int32(math.Floor(arrived[0].y)), int32(math.Floor(arrived[0].z))))
	if name == "minecraft:fire" {
		t.Error("the trap's bolt lit a fire")
	}

	horses := srv.MobIDsOfKind("minecraft:skeleton_horse")
	if len(horses) != 4 {
		t.Fatalf("%d skeleton horses after the trap, want 4", len(horses))
	}
	helmet, _ := registry.ItemID("minecraft:iron_helmet")
	for _, h := range horses {
		if _, tame, _ := srv.HorseTrap(h); !tame {
			t.Errorf("horse %d is not tame", h)
		}
		riders := srv.MobPassengerIDs(h)
		if len(riders) != 1 {
			t.Errorf("horse %d carries %v, want one skeleton", h, riders)
			continue
		}
		r := riders[0]
		if !srv.MobPersistent(r) {
			t.Errorf("rider %d is not persistent", r)
		}
		if head := srv.MobHeadStack(r); head.Empty() {
			t.Errorf("rider %d wears nothing on its head", r)
		} else if head.ItemID == helmet && len(head.Enchantments) == 0 {
			t.Errorf("rider %d's iron helmet is not enchanted", r)
		}
		if len(srv.MobHand(r).Enchantments) == 0 {
			t.Errorf("rider %d's weapon is not enchanted", r)
		}
	}
	if n := len(srv.MobIDsOfKind("minecraft:skeleton")); n != 4 {
		t.Errorf("%d skeletons after the trap, want 4", n)
	}
}

// TestATrapHorseWithNobodyNearWaits is the control: a player beyond ten
// blocks springs nothing.
func TestATrapHorseWithNobodyNearWaits(t *testing.T) {
	t.Parallel()
	srv, _ := servertest.Join(t, servertest.SurvivalTestConfig(), "Far")
	srv.StopTick()
	srv.SetDifficulty(2)
	pos := srv.PlayerPosition("Far")
	trap := srv.SpawnTrapHorse(pos.X+12, pos.Y, pos.Z)

	srv.TickMobs(20)
	if still, tame, _ := srv.HorseTrap(trap); !still || tame {
		t.Errorf("trap %v tame %v, want an untamed trap still waiting", still, tame)
	}
	if n := len(srv.MobIDsOfKind("minecraft:skeleton")); n != 0 {
		t.Errorf("%d skeletons with nobody near, want 0", n)
	}
}
