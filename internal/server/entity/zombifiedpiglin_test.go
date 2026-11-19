package entity

import (
	"math/rand"
	"testing"

	"github.com/google/uuid"

	"github.com/golem-mc/golem/internal/server/rules"
	"github.com/golem-mc/golem/internal/server/world"
	"github.com/golem-mc/golem/registry"
)

// TestAZombifiedPiglinIsBornWithAGoldenWeaponAndNoReinforcements checks
// ZombifiedPiglin.populateDefaultEquipmentSlots and
// randomizeReinforcementsChance over many seeds: a golden sword or, one time
// in twenty, a golden spear; no armour; a reinforcement chance of nought.
func TestAZombifiedPiglinIsBornWithAGoldenWeaponAndNoReinforcements(t *testing.T) {
	spears := 0
	const n = 2000
	for seed := range int64(n) {
		m := NewMob(TypeZombifiedPiglin, 1, 0, 64, 0, seed)
		m.FinalizeSpawn(GoDice{m.Rand}, testBiome("minecraft:nether_wastes"), nil, rules.LocalDifficulty{})
		switch m.MainHand.ItemID {
		case int32(registry.ItemGoldenSword):
		case int32(registry.ItemGoldenSpear):
			spears++
		default:
			t.Fatalf("seed %d: main hand = %+v, want a golden sword or spear", seed, m.MainHand)
		}
		for slot, piece := range m.Worn {
			if !piece.Empty() {
				t.Fatalf("seed %d: armour slot %d = %+v, want none", seed, slot, piece)
			}
		}
		if got := m.ReinforcementChance(); got != 0 {
			t.Fatalf("seed %d: reinforcement chance = %v, want 0", seed, got)
		}
	}
	if spears < 50 || spears > 160 {
		t.Errorf("%d spears in %d piglins, want about one in twenty", spears, n)
	}
}

// TestAZombifiedPiglinHitsFiveAndIsFireImmune checks the kind's numbers.
func TestAZombifiedPiglinHitsFiveAndIsFireImmune(t *testing.T) {
	m := NewMob(TypeZombifiedPiglin, 1, 0, 64, 0, 1)
	k := m.kind()
	if k.attackDamage != 5 || !k.fireImmune || k.maxHealth != 20 {
		t.Errorf("attack damage %v, fire immune %v, health %v; want 5, true, 20", k.attackDamage, k.fireImmune, k.maxHealth)
	}
	calm := m.pace()
	m.anger.ticks = 100
	if got := m.pace() - calm; got < 0.0499 || got > 0.0501 {
		t.Errorf("an angry piglin's pace rises by %v, want 0.05", got)
	}
	m.SetBaby(true)
	babyAngry := m.pace()
	m.anger.ticks = 0
	if m.pace() != babyAngry {
		t.Error("an angry baby piglin got the attacking speed modifier")
	}
}

// TestAZombifiedPiglinAlertsOnlyItsOwnKind checks the alert lists: a struck
// zombie leaves the piglins out, a struck piglin calls only piglins.
// vanilla: Zombie.addBehaviourGoals setAlertOthers(ZombifiedPiglin.class)
func TestAZombifiedPiglinAlertsOnlyItsOwnKind(t *testing.T) {
	cases := []struct {
		striker, other MobType
		want           bool
	}{
		{TypeZombie, TypeZombifiedPiglin, false},
		{TypeHusk, TypeZombifiedPiglin, false},
		{TypeZombifiedPiglin, TypeZombifiedPiglin, true},
		{TypeZombifiedPiglin, TypeZombie, false},
		{TypeZombie, TypeHusk, true},
	}
	for _, c := range cases {
		if got := alertedBy(c.striker, c.other); got != c.want {
			t.Errorf("alertedBy(%s, %s) = %v, want %v", c.striker, c.other, got, c.want)
		}
	}
}

// TestAnAngryZombifiedPiglinGrowlsItsAngrySound pins getAmbientSound: the
// angry sound while the anger lasts, the plain one otherwise.
func TestAnAngryZombifiedPiglinGrowlsItsAngrySound(t *testing.T) {
	m := NewMob(TypeZombifiedPiglin, 1, 0, 64, 0, 1)
	if got := m.Sounds().Ambient.Name(); got == "minecraft:entity.zombified_piglin.angry" {
		t.Fatalf("a calm piglin growls %q", got)
	}
	m.SetAnger(100, uuid.UUID{}, false)
	if got := m.Sounds().Ambient.Name(); got != "minecraft:entity.zombified_piglin.angry" {
		t.Errorf("an angry piglin's ambient sound = %q, want the angry one", got)
	}
}

// TestZombifiedPiglinsSpawnInTheNetherLightButNotOnNetherWart pins
// checkZombifiedPiglinSpawnRules: no light check, and no nether wart block
// below, where a zombie keeps to the darkness rule.
func TestZombifiedPiglinsSpawnInTheNetherLightButNotOnNetherWart(t *testing.T) {
	w := newSpawnTestWorld()
	w.dimension = world.NetherName
	w.groundAt(t, 10, 63, 10)
	w.blockLights[[3]int32{10, 64, 10}] = 15
	rng := rand.New(rand.NewSource(1))
	spawns := func(kind MobType) bool {
		return validSpawnPositionForType(w, rng, kind, 10, 64, 10, 100, CategoryMonster)
	}
	if !spawns(TypeZombifiedPiglin) {
		t.Error("a piglin was refused beside lava light, vanilla has no light check")
	}
	if spawns(TypeZombie) {
		t.Error("a zombie spawned in block light 15")
	}
	w.set(10, 63, 10, defaultState(t, "minecraft:nether_wart_block"))
	if spawns(TypeZombifiedPiglin) {
		t.Error("a piglin spawned on a nether wart block")
	}
}
