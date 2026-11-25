package mobs

import (
	"testing"

	"github.com/golem-mc/golem/internal/server"
	"github.com/golem-mc/golem/internal/server/servertest"
	"github.com/golem-mc/golem/registry"
	"github.com/google/uuid"
)

// bulletsIn lists the bullets in flight.
func bulletsIn(srv *server.Server) map[int32]bool {
	out := map[int32]bool{}
	for _, it := range srv.ThrownInDimension("minecraft:overworld") {
		if it.Kind == "minecraft:shulker_bullet" {
			out[it.ID] = true
		}
	}
	return out
}

// TestAShulkerBulletHurtsAndLevitatesItsTarget: a shulker beside a player
// opens up, fires, and the bullet that lands takes four points and gives
// Levitation for ten seconds.
// vanilla: ShulkerBullet.onHitEntity
func TestAShulkerBulletHurtsAndLevitatesItsTarget(t *testing.T) {
	t.Parallel()
	srv, _, y := shulkerTests(t)
	at := srv.PlayerPosition("Watcher")
	srv.SpawnMob("minecraft:shulker", at.X+3, y, at.Z)
	before := srv.PlayerHealth("Watcher")
	for range 900 {
		srv.TickMobs(1)
		srv.TickEntities(1)
		if _, _, ok := srv.PlayerEffect("Watcher", "minecraft:levitation"); ok {
			break
		}
	}
	dur, amp, ok := srv.PlayerEffect("Watcher", "minecraft:levitation")
	if !ok || amp != 0 || dur < 190 || dur > 200 {
		t.Fatalf("levitation = %d ticks, amplifier %d, present %v; want 200 ticks, amplifier 0", dur, amp, ok)
	}
	if got := before - srv.PlayerHealth("Watcher"); got != 4 {
		t.Errorf("the bullet took %v health, want 4", got)
	}
}

// TestAShulkerFiresOnItsCadence: the first bullet leaves after the 20 ticks
// the goal starts on and the next ones 20, 30, ... or 110 ticks apart.
// vanilla: Shulker.ShulkerAttackGoal.tick
func TestAShulkerFiresOnItsCadence(t *testing.T) {
	t.Parallel()
	srv, _, y := shulkerTests(t)
	at := srv.PlayerPosition("Watcher")
	srv.SpawnMobSeeded("minecraft:shulker", at.X+12, y, at.Z, 1)
	seen := map[int32]bool{}
	var shots []int
	for tick := 1; tick <= 1500; tick++ {
		srv.TickMobs(1)
		srv.TickEntities(1)
		for id := range bulletsIn(srv) {
			if !seen[id] {
				seen[id] = true
				shots = append(shots, tick)
			}
		}
	}
	if len(shots) < 3 {
		t.Fatalf("only %d bullets in 1500 ticks", len(shots))
	}
	if shots[0] < 18 || shots[0] > 30 {
		t.Errorf("the first bullet left at tick %d, want about 20 ticks after the shell opened", shots[0])
	}
	for i := 1; i < len(shots); i++ {
		if gap := shots[i] - shots[i-1]; gap < 20 || gap > 110 || gap%10 != 0 {
			t.Errorf("bullets %d and %d were %d ticks apart, want one of 20, 30, ..., 110", i-1, i, gap)
		}
	}
}

func TestShulkerBulletScaffoldingUsesFeetHeight(t *testing.T) {
	t.Parallel()
	srv, _ := servertest.Join(t, servertest.SpawningTestConfig(), "Watcher", func(s *server.Server) {
		servertest.FreezeUntilHeld(t, s)
	})
	servertest.HoldTicks(t, srv)

	const y = 70
	const up, east = 1, 5
	stone := registry.BlockStone.DefaultState()
	scaffold, _ := registry.WithInt(registry.BlockScaffolding.DefaultState(), registry.PropDistance, 0)
	for _, tc := range []struct {
		name    string
		z       int32
		feetY   float64
		wantDir int8
	}{
		{name: "below plate", z: 4, feetY: y + 0.5, wantDir: east},
		// Within the isAbove epsilon while the bullet still probes this row.
		{name: "at top of plate", z: 8, feetY: y + 1 - 5e-6, wantDir: up},
	} {
		t.Run(tc.name, func(t *testing.T) {
			z := float64(tc.z) + 0.5
			target := srv.SpawnMob("minecraft:shulker", 12.5, y+3, z)
			srv.LaunchShulkerBullet(5.5, tc.feetY, z, target, 3)
			var bullet uuid.UUID
			for _, it := range srv.ThrownInDimension("minecraft:overworld") {
				if it.Kind == "minecraft:shulker_bullet" && it.Z == z {
					bullet = uuid.MustParse(it.UUID)
					break
				}
			}
			if _, _, dir, _, ok := srv.ShulkerBulletState(bullet); !ok || dir != east {
				t.Fatalf("initial bullet direction = %d, present %v; want east", dir, ok)
			}
			// Leave room for the bullet's first move before it probes east.
			srv.SetBlock(6, y-1, tc.z, stone)
			if !srv.SetBlock(6, y, tc.z, scaffold) {
				t.Fatal("could not place scaffolding ahead of the bullet")
			}
			srv.TickEntities(1)
			if _, _, dir, _, ok := srv.ShulkerBulletState(bullet); !ok || dir != tc.wantDir {
				t.Errorf("bullet direction = %d, present %v; want %d", dir, ok, tc.wantDir)
			}
		})
	}
}
