package entity

import (
	"math/rand"
	"testing"

	"github.com/golem-mc/golem/internal/server/rules"
	"github.com/golem-mc/golem/registry"
)

// The mob voice, as ordinary unit tests: the sound set a farm animal's sound
// variant speaks, the volume getSoundVolume gives, the die the idle sound
// rolls against, and the timer a wound restarts. The black-box tests in
// internal/server/*_test.go are the proof of the sounds on the wire.

// TestAFarmAnimalsSoundVariantSpeaksItsOwnEvents holds every non-classic
// sound variant to the events the vanilla variant class names, the classic
// variant to the kind's own set, and a baby to the baby set its kind keeps —
// or, for a cow, to its variant's set, the cow having no baby one.
// vanilla: CowSoundVariant, PigSoundVariant, ChickenSoundVariant
func TestAFarmAnimalsSoundVariantSpeaksItsOwnEvents(t *testing.T) {
	cases := []struct {
		kind                 MobType
		variant              string
		baby                 bool
		ambient, hurt, death string
	}{
		{TypeCow, "minecraft:moody", false,
			"minecraft:entity.cow_moody.ambient", "minecraft:entity.cow_moody.hurt", "minecraft:entity.cow_moody.death"},
		{TypePig, "minecraft:big", false,
			"minecraft:entity.pig_big.ambient", "minecraft:entity.pig_big.hurt", "minecraft:entity.pig_big.death"},
		{TypePig, "minecraft:mini", false,
			"minecraft:entity.pig_mini.ambient", "minecraft:entity.pig_mini.hurt", "minecraft:entity.pig_mini.death"},
		{TypeChicken, "minecraft:picky", false,
			"minecraft:entity.chicken_picky.ambient", "minecraft:entity.chicken_picky.hurt", "minecraft:entity.chicken_picky.death"},
		// A baby speaks the kind's baby set whatever its variant chose.
		{TypePig, "minecraft:big", true,
			"minecraft:entity.baby_pig.ambient", "minecraft:entity.baby_pig.hurt", "minecraft:entity.baby_pig.death"},
		{TypeChicken, "minecraft:picky", true,
			"minecraft:entity.baby_chicken.ambient", "minecraft:entity.baby_chicken.hurt", "minecraft:entity.baby_chicken.death"},
		// The cow keeps no baby set, so a baby moody cow still speaks the
		// variant's events.
		{TypeCow, "minecraft:moody", true,
			"minecraft:entity.cow_moody.ambient", "minecraft:entity.cow_moody.hurt", "minecraft:entity.cow_moody.death"},
	}
	for _, tc := range cases {
		m := NewMob(tc.kind, 1, 0.5, 64, 0.5, 3)
		m.SoundVariant = tc.variant
		m.SetBaby(tc.baby)
		s := m.Sounds()
		if s.Ambient.Name() != tc.ambient || s.Hurt.Name() != tc.hurt || s.Death.Name() != tc.death {
			t.Errorf("%s %s baby=%v speaks %s/%s/%s, want %s/%s/%s",
				tc.kind, tc.variant, tc.baby, s.Ambient.Name(), s.Hurt.Name(), s.Death.Name(), tc.ambient, tc.hurt, tc.death)
		}
		for _, event := range []string{tc.ambient, tc.hurt, tc.death} {
			if _, ok := registry.SoundEventID(event); !ok {
				t.Errorf("the sound registry carries no %s", event)
			}
		}
	}

	// The classic variant is the kind's own set, the ambient the base one.
	classic := NewMob(TypeCow, 1, 0.5, 64, 0.5, 3)
	classic.SoundVariant = "minecraft:classic"
	if s := classic.Sounds(); s.Ambient.Name() != "minecraft:entity.cow.ambient" {
		t.Errorf("a classic cow moans %s, want the kind's own", s.Ambient.Name())
	}
}

// TestACowSpeaksAtItsKindsOwnVolume checks getSoundVolume: the cow's quiet
// 0.4, one for the kinds that carry none, and the cubes' 0.4 a size.
func TestACowSpeaksAtItsKindsOwnVolume(t *testing.T) {
	if v := NewMob(TypeCow, 1, 0.5, 64, 0.5, 3).SoundVolume(); v != 0.4 {
		t.Errorf("a cow speaks at %v, want AbstractCow.getSoundVolume's 0.4", v)
	}
	if v := NewMob(TypePig, 1, 0.5, 64, 0.5, 3).SoundVolume(); v != 1.0 {
		t.Errorf("a pig speaks at %v, want LivingEntity.getSoundVolume's one", v)
	}
	cube := NewMob(TypeSlime, 1, 0.5, 64, 0.5, 3)
	cube.SetSize(2, true)
	if v := cube.SoundVolume(); v != 0.8 {
		t.Errorf("a size-two cube speaks at %v, want AbstractCubeMob's 0.4 a size", v)
	}
}

// TestAMoanRollsTheCounterBeforeTheIncrement checks the idle sound's die
// against Mob.baseTick's post-increment: a roll equal to the counter does
// not fire, because the comparison reads the counter before it climbs.
func TestAMoanRollsTheCounterBeforeTheIncrement(t *testing.T) {
	const seed = 11
	m := NewMob(TypeCow, 1, 0.5, 64, 0.5, seed)
	roll := rand.New(rand.NewSource(seed)).Intn(ambientDie)
	m.ambientTimer = roll
	if m.moanStep() {
		t.Fatalf("a moan fired on a roll of %d against a counter of %d, want the comparison before the increment", roll, roll)
	}
	if m.ambientTimer != roll+1 {
		t.Errorf("the counter went to %d, want the post-increment's %d", m.ambientTimer, roll+1)
	}
}

// TestAHurtRestartsTheIdleTimer checks Mob.playHurtSound's effect on the
// idle sound: a landed hurt sends the counter below its own interval.
func TestAHurtRestartsTheIdleTimer(t *testing.T) {
	m := NewMob(TypeCow, 1, 0.5, 64, 0.5, 3)
	m.ambientTimer = 7
	if landed, _ := m.plainHurt(1, rules.DamageTypeMobAttack, false); !landed {
		t.Fatal("the hurt did not land")
	}
	if want := -m.ambientSoundInterval(); m.ambientTimer != want {
		t.Errorf("after a hurt the idle counter is %d, want the interval's %d", m.ambientTimer, want)
	}
}

// TestAMobKilledByFireDoesNotMoan checks the idle step's isAlive gate: a
// body the fire kills earlier in the tick is past its sounds, however the
// counter stands.
func TestAMobKilledByFireDoesNotMoan(t *testing.T) {
	m := NewMob(TypeCow, 1, 0.5, 64, 0.5, 3)
	m.Health = 1
	m.FireTicks = 20
	m.ambientTimer = ambientDie
	for _, ev := range m.Tick(newTestWorld(), nil) {
		if ev.Kind == MobMoaned {
			t.Fatal("a cow the fire killed this tick moaned, want the idle step behind isAlive")
		}
	}
}

// TestAHappyGhastsVoicePitchIsFlat checks HappyGhast.getVoicePitch: a grown
// body and a ghastling alike speak at a flat 1.0, not the rolled 1.0 ± 0.2 a
// baby steps up.
// vanilla: HappyGhast.getVoicePitch
func TestAHappyGhastsVoicePitchIsFlat(t *testing.T) {
	for _, baby := range []bool{false, true} {
		m := NewMob(TypeHappyGhast, 1, 0.5, 64, 0.5, 3)
		m.SetBaby(baby)
		for range 20 {
			if p := m.VoicePitch(); p != 1.0 {
				t.Fatalf("a baby=%v happy ghast's voice pitch = %v, want the flat 1.0", baby, p)
			}
		}
	}
}

// TestAnAnimalsIdleIntervalIsAnimals holds every kind whose vanilla class
// extends Animal to Animal.getAmbientSoundInterval's 120, but for the
// overrides: AbstractHorse's 400 (camels included), Ocelot's 900, Turtle's
// 200, and HappyGhast's six-fold while ridden.
func TestAnAnimalsIdleIntervalIsAnimals(t *testing.T) {
	for _, c := range []struct {
		t    MobType
		want int
	}{
		{TypeArmadillo, 120}, {TypeAxolotl, 120}, {TypeBee, 120}, {TypeCat, 120},
		{TypeChicken, 120}, {TypeCow, 120}, {TypeFox, 120}, {TypeFrog, 120},
		{TypeGoat, 120}, {TypeHappyGhast, 120}, {TypeHoglin, 120}, {TypeMooshroom, 120},
		{TypeNautilus, 120}, {TypePanda, 120}, {TypeParrot, 120}, {TypePig, 120},
		{TypePolarBear, 120}, {TypeRabbit, 120}, {TypeSheep, 120}, {TypeSniffer, 120},
		{TypeStrider, 120}, {TypeWolf, 120}, {TypeZombieNautilus, 120},
		{TypeCamel, 400}, {TypeCamelHusk, 400}, {TypeDonkey, 400}, {TypeHorse, 400},
		{TypeLlama, 400}, {TypeMule, 400}, {TypeSkeletonHorse, 400}, {TypeTraderLlama, 400},
		{TypeZombieHorse, 400},
		{TypeOcelot, 900},
		{TypeTurtle, 200},
	} {
		m := NewMob(c.t, 1, 0.5, 64, 0.5, 3)
		if got := m.ambientSoundInterval(); got != c.want {
			t.Errorf("%s idles every %d ticks, want %d", c.t, got, c.want)
		}
	}
	ghast := NewMob(TypeHappyGhast, 1, 0.5, 64, 0.5, 3)
	ghast.Horse.ExtraRiderIDs = []int32{2}
	if got := ghast.ambientSoundInterval(); got != 720 {
		t.Errorf("a ridden happy ghast idles every %d ticks, want 720", got)
	}
}

// vanilla: Mob.baseTick, Villager.getAmbientSound, LivingEntity.makeSound
func TestASleepingVillagerKeepsItsAmbientRhythmWithoutRollingPitch(t *testing.T) {
	m := NewMob(TypeVillager, 1, 0.5, 64, 0.5, 5895)
	m.Sleeping, m.Trading = true, true
	m.ambientTimer = ambientDie
	if !m.moanStep() || m.ambientTimer != -m.ambientSoundInterval() {
		t.Fatal("sleeping villager did not reset its ambient timer")
	}
	// A null sound never reaches LivingEntity.getVoicePitch.
	m.Rand = rand.New(rand.NewSource(5895))
	control := rand.New(rand.NewSource(5895))
	if event, _, _ := m.AmbientVoice(); event.Name() != "" {
		t.Errorf("sleeping villager selected %q, want no sound even while trading", event)
	}
	if m.Rand.Int63() != control.Int63() {
		t.Error("a sleeping villager consumed the voice pitch's random rolls")
	}
	if m.moanStep() || m.ambientTimer != -m.ambientSoundInterval()+1 {
		t.Error("sleeping villager did not continue its ambient countdown")
	}
}
