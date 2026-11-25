package mobs

import (
	"math"
	"testing"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server"
	"github.com/golem-mc/golem/internal/server/servertest"
	"github.com/golem-mc/golem/registry"
)

// The fox's hunting, fleeing, trust and meals, black box with the loop held.

func mobAlive(srv *server.Server, id int32) bool {
	m, ok := mobWithID(srv, id)
	return ok && m.ID == id
}

// TestAFoxStalksAndPouncesOnAChicken: a fox crouches in sight of a chicken,
// leaps and kills it.
// vanilla: Fox.StalkPreyGoal, Fox.FoxPounceGoal, landTargetGoal
func TestAFoxStalksAndPouncesOnAChicken(t *testing.T) {
	t.Parallel()
	srv := foxScene(t, false)
	srv.SetSpawning(false)
	fox := srv.SpawnMobSeeded(foxType, foxX, 64, 0.5, 3)
	chicken := srv.SpawnMobSeeded("minecraft:chicken", foxX+10, 64, 0.5, 4)
	var crouched, pounced bool
	for range 600 {
		servertest.RunTicks(t, srv, 1)
		flags := foxState(t, srv, fox).Flags
		crouched = crouched || flags&protocol.FoxCrouching != 0
		pounced = pounced || flags&protocol.FoxPouncing != 0
		if !mobAlive(srv, chicken) {
			break
		}
	}
	if mobAlive(srv, chicken) {
		t.Fatal("the fox never killed the chicken")
	}
	if !crouched || !pounced {
		t.Errorf("crouched = %v, pounced = %v, want both before the kill", crouched, pounced)
	}
}

// TestAFoxFleesAPlayerUnlessItTrustsThem: a fox with a player in sight runs
// off; one that trusts the player stays.
// vanilla: Fox.registerGoals' AvoidEntityGoal<Player>, Fox.trusts
func TestAFoxFleesAPlayerUnlessItTrustsThem(t *testing.T) {
	t.Parallel()
	for _, trusted := range []bool{false, true} {
		srv := foxScene(t, false)
		srv.SetSpawning(false)
		fox := srv.SpawnMobSeeded(foxType, 8.5, 64, 0.5, 8)
		if trusted {
			srv.TrustFox(fox, "Fox")
		}
		servertest.RunTicks(t, srv, 80)
		st := foxState(t, srv, fox)
		away := math.Hypot(st.X-0.5, st.Z-0.5) > 14
		if away == trusted {
			t.Errorf("trusted = %v: fox at (%.1f, %.1f), fled = %v", trusted, st.X, st.Z, away)
		}
	}
}

// TestFedFoxesBreedAKitThatTrustsTheirPlayer: two foxes loved by a player
// have a baby fox that trusts the player and does not flee them.
// vanilla: Fox.FoxBreedGoal.breed
func TestFedFoxesBreedAKitThatTrustsTheirPlayer(t *testing.T) {
	t.Parallel()
	srv := foxScene(t, false)
	srv.SetSpawning(false)
	a := srv.SpawnMobSeeded(foxType, foxX, 64, 0.5, 3)
	b := srv.SpawnMobSeeded(foxType, foxX+2, 64, 0.5, 4)
	srv.SetMobInLoveBy(a, 600, "Fox")
	srv.SetMobInLoveBy(b, 600, "Fox")
	var kit int32
	for range 400 {
		servertest.RunTicks(t, srv, 1)
		for _, m := range srv.Mobs() {
			if m.Kind == foxType && m.Baby {
				kit = m.ID
			}
		}
		if kit != 0 {
			break
		}
	}
	if kit == 0 {
		t.Fatal("no kit was born")
	}
	if !srv.FoxTrusts(kit, "Fox") {
		t.Error("the kit does not trust the player who fed its parents")
	}
}

// TestATrustedFoxDefendsItsPlayer: whatever strikes a trusted player becomes
// the fox's target.
// vanilla: Fox.DefendTrustedTargetGoal
func TestATrustedFoxDefendsItsPlayer(t *testing.T) {
	t.Parallel()
	srv := foxScene(t, false)
	srv.SetSpawning(false)
	fox := srv.SpawnMobSeeded(foxType, 12.5, 64, 0.5, 3)
	srv.TrustFox(fox, "Fox")
	zombie := srv.SpawnMobSeeded("minecraft:zombie", 1.5, 64, 0.5, 4)
	servertest.UntilTicks(t, srv, 400, "the fox to take the zombie as its target", func() bool {
		return srv.MobTargetID(fox) == zombie
	})
	if foxState(t, srv, fox).Flags&protocol.FoxDefending == 0 {
		t.Error("the defending fox does not show it")
	}
}

// TestAFoxFinishesAMealWithItsEffectsAndRemainder: a fox holding a honey
// bottle swallows it after 600 ticks and keeps the glass bottle; a golden
// apple leaves regeneration.
// vanilla: Fox.aiStep, Consumable.onConsume, UseRemainder
func TestAFoxFinishesAMealWithItsEffectsAndRemainder(t *testing.T) {
	t.Parallel()
	srv := foxScene(t, false)
	srv.SetSpawning(false)
	bottleFox := srv.SpawnMobSeeded(foxType, foxX, 64, 0.5, 3)
	appleFox := srv.SpawnMobSeeded(foxType, foxX, 64, 6.5, 4)
	srv.SetFoxMouth(bottleFox, "minecraft:honey_bottle")
	srv.SetFoxMouth(appleFox, "minecraft:golden_apple")
	servertest.RunTicks(t, srv, 590)
	if foxState(t, srv, bottleFox).Mouth != servertest.MustItemID(t, "minecraft:honey_bottle") {
		t.Fatal("the fox ate before the 600 ticks")
	}
	servertest.RunTicks(t, srv, 40)
	if got, want := foxState(t, srv, bottleFox).Mouth, registry.MustItem("minecraft:glass_bottle"); got != want {
		t.Errorf("mouth after the meal = %d, want the glass bottle %d", got, want)
	}
	if foxState(t, srv, appleFox).Mouth != 0 {
		t.Error("the apple was not swallowed")
	}
	if _, _, ok := srv.MobEffectOf(appleFox, "minecraft:regeneration"); !ok {
		t.Error("the golden apple left no regeneration")
	}
}

// TestASnowFoxHuntsCodButNotPufferfish: with no other prey, a snow fox goes
// for a cod in a pool and leaves a pufferfish there alone.
// vanilla: Fox.registerGoals (AbstractSchoolingFish filter), Pufferfish extends AbstractFish
func TestASnowFoxHuntsCodButNotPufferfish(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		fish     string
		wantDead bool
		deep     bool // a pufferfish suffocates in one block of water
		seed     int64
		wide     int32 // 1 for a pool a pufferfish does not flop out of
	}{
		{"minecraft:cod", true, false, 4, 0},
		{"minecraft:pufferfish", false, true, 5, 1},
	} {
		t.Run(tc.fish, func(t *testing.T) {
			t.Parallel()
			srv := foxScene(t, false)
			srv.SetSpawning(false)
			srv.SetOverworldBiome(foxBlock, 0, "minecraft:snowy_taiga")
			srv.SetOverworldBiome(foxBlock, -1, "minecraft:snowy_taiga")
			water := servertest.BlockState(t, "minecraft:water")
			stone := servertest.BlockState(t, "minecraft:stone")
			for dx := int32(-1); dx <= 4; dx++ {
				for dz := int32(-1); dz <= 2; dz++ {
					srv.SetBlock(foxBlock+4+dx, 61, dz, stone)
				}
			}
			for dx := int32(0); dx <= 1+2*tc.wide; dx++ {
				for dz := int32(0); dz <= tc.wide; dz++ {
					srv.SetBlock(foxBlock+4+dx, 63, dz, water)
					if tc.deep {
						srv.SetBlock(foxBlock+4+dx, 62, dz, water)
					} else {
						srv.SetBlock(foxBlock+4+dx, 62, dz, stone)
					}
				}
			}
			fox := srv.SpawnMobSeeded(foxType, foxX, 64, 0.5, 3)
			if foxState(t, srv, fox).Variant != 1 {
				t.Fatal("the fox is not a snow fox")
			}
			ticks := 0
			fish := srv.SpawnMobSeeded(tc.fish, foxX+5, 63.3, 0.5, tc.seed)
			for range 600 {
				servertest.RunTicks(t, srv, 1)
				if !tc.wantDead && srv.MobTargetID(fox) != 0 && ticks < 32 {
					t.Fatalf("fox targets the pufferfish at tick %d", ticks)
				}
				ticks++
				if !mobAlive(srv, fish) {
					break
				}
			}
			if dead := !mobAlive(srv, fish); dead != tc.wantDead {
				t.Errorf("%s dead = %v, want %v", tc.fish, dead, tc.wantDead)
			}
		})
	}
}
