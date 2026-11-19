package entity

import (
	"math/rand"
	"testing"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/registry"
)

// vanilla: Consumable.emitParticlesAndSounds, FoodProperties.onConsume
func TestFoxMealSoundDrawOrder(t *testing.T) {
	for _, animation := range []int32{1, protocol.ItemUseAnimationDrink} {
		for seed := range int64(16) {
			fox := NewMob(TypeFox, 1, 0.5, 64, 0.5, 1)
			fox.Rand = rand.New(rand.NewSource(seed))
			fox.MainHand = protocol.ItemStack{ItemID: int32(registry.ItemSweetBerries), Count: 1}
			fox.MainHand.SetConsumable(protocol.ConsumableComponent{Animation: animation, Sound: protocol.SoundHolder{ID: int32(registry.SoundEntityGenericEat) - 1}})
			rng := rand.New(rand.NewSource(seed))
			volume := float32(1)
			if rng.Int31n(2) == 0 {
				volume = 0.5
			}
			pitch := float32(1) + float32(0.2)*(rng.Float32()-rng.Float32())
			drinkPitch := rng.Float32()*(float32(1)-float32(0.9)) + float32(0.9)
			if animation == protocol.ItemUseAnimationDrink {
				volume, pitch = 0.5, drinkPitch
			}
			foodPitch := float32(1) + float32(0.4)*(rng.Float32()-rng.Float32())
			events := fox.foxFinishMeal(nil)
			if len(events) != 2 {
				t.Fatalf("animation %d seed %d: got %d events, want 2", animation, seed, len(events))
			}
			if got := events[0]; got.Kind != MobSounded || got.Volume != volume || got.Pitch != pitch {
				t.Errorf("animation %d seed %d: consume sound kind/volume/pitch = %d/%v/%v, want %d/%v/%v", animation, seed, got.Kind, got.Volume, got.Pitch, MobSounded, volume, pitch)
			}
			if got := events[1]; got.Kind != MobSounded || got.Volume != 1 || got.Pitch != foodPitch {
				t.Errorf("animation %d seed %d: food sound kind/volume/pitch = %d/%v/%v, want %d/1/%v", animation, seed, got.Kind, got.Volume, got.Pitch, MobSounded, foodPitch)
			}
			if got, want := fox.Rand.Int63(), rng.Int63(); got != want {
				t.Errorf("animation %d seed %d: next RNG value = %d, want %d", animation, seed, got, want)
			}
		}
	}
}

// TestASleepingFoxSnores: a sleeping fox's ambient sound is its sleep sound.
// vanilla: Fox.getAmbientSound
func TestASleepingFoxSnores(t *testing.T) {
	fox := NewMob(TypeFox, 1, 0, 64, 0, 1)
	if got := fox.Sounds().Ambient.Name(); got != "minecraft:entity.fox.ambient" {
		t.Errorf("awake ambient %q", got)
	}
	fox.Fox.Sleeping = true
	if got := fox.Sounds().Ambient.Name(); got != "minecraft:entity.fox.sleep" {
		t.Errorf("sleeping ambient %q", got)
	}
}

// TestAFoxIsNotAlertedByASleepingPlayer: a player in a bed within range does
// not count as alertable, a standing one does.
// vanilla: Fox.FoxAlertableEntitiesSelector.test
func TestAFoxIsNotAlertedByASleepingPlayer(t *testing.T) {
	fox := NewMob(TypeFox, 1, 0, 64, 0, 1)
	for _, tc := range []struct {
		name     string
		sleeping bool
		want     bool
	}{
		{"standing", false, true},
		{"in bed", true, false},
	} {
		view := &tickView{players: []Player{{X: 4, Y: 64, Z: 0, Huntable: true, Sleeping: tc.sleeping}}}
		if got := fox.foxAlertable(view); got != tc.want {
			t.Errorf("%s: alertable = %v, want %v", tc.name, got, tc.want)
		}
	}
}

// TestAFoxStrollsTowardAVillageAtNight: near a village at night the stroll
// starts, and does not with no village near.
// vanilla: StrollThroughVillageGoal.canUse
func TestAFoxStrollsTowardAVillageAtNight(t *testing.T) {
	w := newTestWorld()
	stone := defaultState(t, "minecraft:stone")
	for x := int32(-20); x <= 20; x++ {
		for z := int32(-20); z <= 20; z++ {
			w.set(x, 63, z, stone)
		}
	}
	fox := NewMob(TypeFox, 1, 0.5, 64, 0.5, 1)
	fox.Village = fakeVillage{w: w, pois: []Waypoint{{X: 48, Y: 64, Z: 0}}}
	g := &strollThroughVillage{m: fox, interval: 1}
	w.bright = true
	if g.CanStart(&tickView{w: w}) {
		t.Fatal("started by day")
	}
	w.bright = false
	// A pick lands on the one floor layer about one time in fifteen.
	started := false
	for range 100 {
		if started = g.CanStart(&tickView{w: w}); started {
			break
		}
	}
	if !started {
		t.Fatal("did not start at night near a village")
	}
	fox.Village = fakeVillage{w: w}
	if g.CanStart(&tickView{w: w}) {
		t.Error("started with no village near")
	}
}

// vanilla: Fox.FoxFollowParentGoal, Fox.registerGoals
func TestAFoxKitFollowsItsParentAtVanillaPace(t *testing.T) {
	w := newTestWorld()
	floor(w, -2, -2, 12, 2, 63, defaultState(t, "minecraft:stone"))
	kit := NewMob(TypeFox, 1, 0.5, 64, 0.5, 1)
	kit.BabyAge = -24000
	kit.Fox.Sleeping = true
	parent := NewMob(TypeFox, 2, 8.5, 64, 0.5, 2)
	view := &tickView{w: w, others: []*Mob{kit, parent}}
	var follow *foxGuard
	for _, goal := range foxGoals(kit) {
		if guard, ok := goal.(*foxGuard); ok {
			if _, ok := guard.Goal.(*followParentGoal); ok {
				follow = guard
			}
		}
	}
	if follow == nil || !follow.CanStart(view) {
		t.Fatal("kit did not select its parent")
	}
	follow.Start(view)
	follow.Tick(view)
	if kit.Fox.Sleeping {
		t.Error("kit followed its parent while sleeping")
	}
	if len(kit.navOf().nav.path) == 0 {
		t.Fatal("kit did not plan a path to its parent")
	}
	if got := kit.navOf().nav.speedFactor; got != 1.25 {
		t.Errorf("parent follow pace = %v, want 1.25", got)
	}
	kit.Fox.Defending = true
	if follow.ShouldContinue(view) {
		t.Error("defending kit kept following its parent")
	}
}

// vanilla: UseRemainder.convertIntoRemainder, LivingEntity.handleExtraItemsCreatedOnUse
func TestAFoxMealRemainderDependsOnWhetherItsMouthEmpties(t *testing.T) {
	for _, count := range []int32{1, 2} {
		fox := NewMob(TypeFox, 1, 0.5, 64, 0.5, 1)
		fox.MainHand = protocol.ItemStack{ItemID: registry.MustItem("minecraft:honey_bottle"), Count: count}
		events := fox.foxFinishMeal(nil)
		want := registry.MustItem("minecraft:glass_bottle")
		if count > 1 {
			want = registry.MustItem("minecraft:honey_bottle")
		}
		if fox.MainHand.ItemID != want || fox.MainHand.Count != 1 {
			t.Errorf("meal from %d bottles left %+v in mouth", count, fox.MainHand)
		}
		for _, event := range events {
			if event.Kind != MobSounded && event.Kind != MobEffectChanged {
				t.Errorf("meal from %d bottles emitted extra event %+v", count, event)
			}
		}
	}
}
