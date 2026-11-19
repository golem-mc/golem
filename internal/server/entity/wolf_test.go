package entity

import (
	"math/rand"
	"testing"

	"github.com/google/uuid"

	"github.com/golem-mc/golem/internal/server/rules"
)

// TestAWolfShakesFor42DryTicksAndSoundsOnce steps a wet wolf in dry weather:
// the shake sound plays on its first dry tick only, and the shake ends on
// the 42nd, after which the wolf is dry.
// vanilla: Wolf.tick
func TestAWolfShakesFor42DryTicksAndSoundsOnce(t *testing.T) {
	w := newTestWorld()
	m := NewMob(TypeWolf, 1, 0.5, 64, 0.5, 1)
	m.OnGround = true
	m.wolf.wet = true
	sounds, ticks := 0, 0
	for ; m.wolf.wet && ticks < 100; ticks++ {
		for _, ev := range m.wolfWetStep(w, nil) {
			if ev.Kind == MobWolfShakeSound {
				sounds++
			}
		}
	}
	if sounds != 1 {
		t.Errorf("shake sounds = %d, want 1", sounds)
	}
	if ticks != 42 {
		t.Errorf("the wolf dried after %d ticks, want 42", ticks)
	}
}

// TestAWolfSoundsNothingWhileItStaysWet: in the rain a started shake is
// cancelled at once and never plays the shake sound.
func TestAWolfSoundsNothingWhileItStaysWet(t *testing.T) {
	w := newTestWorld()
	w.rain = true
	m := NewMob(TypeWolf, 1, 0.5, 64, 0.5, 1)
	m.OnGround = true
	for range 30 {
		for _, ev := range m.wolfWetStep(w, nil) {
			if ev.Kind == MobWolfShakeSound {
				t.Fatal("a wolf in the rain played the shake sound")
			}
		}
	}
}

func TestAWolfSpeaksAtFortyPercent(t *testing.T) {
	if v := MobSoundVolume(TypeWolf); v != 0.4 {
		t.Errorf("wolf volume = %v, want 0.4", v)
	}
}

// TestAStruckWolfTurnsOnItsAttackerAndRalliesThePack: a wild wolf hit by a
// player targets them, and a neighbouring wolf is sent after them too.
// vanilla: Wolf.registerGoals' HurtByTargetGoal.setAlertOthers
func TestAStruckWolfTurnsOnItsAttackerAndRalliesThePack(t *testing.T) {
	w := newTestWorld()
	floor64(w, t)
	wolf := NewMob(TypeWolf, 1, 0.5, 64, 0.5, 3)
	packmate := NewMob(TypeWolf, 2, 3.5, 64, 3.5, 4)
	attacker := survivor(7, 5.5)
	wolf.TakeDamage(1, attacker)
	for range 5 {
		wolf.tick(w, []Player{attacker}, []*Mob{packmate})
	}
	if wolf.TargetID != 7 {
		t.Errorf("a struck wolf targeted %d, want 7", wolf.TargetID)
	}
	if packmate.TargetID != 7 {
		t.Errorf("the packmate targeted %d, want 7", packmate.TargetID)
	}
}

// TestAWolfLeavesAPlayerAloneUntilStruck: a wolf beside a player that never
// hit it picks no target.
func TestAWolfLeavesAPlayerAloneUntilStruck(t *testing.T) {
	w := newTestWorld()
	floor64(w, t)
	wolf := NewMob(TypeWolf, 1, 0.5, 64, 0.5, 3)
	for range 40 {
		wolf.Tick(w, []Player{survivor(7, 2.5)})
	}
	if wolf.TargetID != 0 {
		t.Errorf("an unprovoked wolf targeted %d", wolf.TargetID)
	}
}

// TestAWolfBitesForFour: the hunt's bite carries the wolf's attack damage.
// vanilla: Wolf.createAttributes
func TestAWolfBitesForFour(t *testing.T) {
	w := newTestWorld()
	floor64(w, t)
	wolf := NewMob(TypeWolf, 1, 0.5, 64, 0.5, 3)
	attacker := survivor(7, 1.0)
	wolf.TakeDamage(1, attacker)
	bitten := false
	for i := 0; i < 60 && !bitten; i++ {
		for _, ev := range wolf.Tick(w, []Player{attacker}) {
			if ev.Kind == MobAttackedPlayer {
				bitten = true
				if ev.Damage != 4 {
					t.Errorf("bite = %v, want 4", ev.Damage)
				}
			}
		}
	}
	if !bitten {
		t.Error("the struck wolf never bit")
	}
}

// TestAWolfChasesATargetBehindAWall: the wolf's melee goal keeps re-pathing
// to a target it cannot see.
// vanilla: Wolf.registerGoals' MeleeAttackGoal(this, 1.0, true)
func TestAWolfChasesATargetBehindAWall(t *testing.T) {
	w := newTestWorld()
	floor64(w, t)
	stone := defaultState(t, "minecraft:stone")
	for y := int32(64); y <= 65; y++ {
		for z := int32(-1); z <= 1; z++ {
			w.set(3, y, z, stone)
		}
	}
	wolf := NewMob(TypeWolf, 1, 0.5, 64, 0.5, 3)
	wolf.OnGround = true
	wolf.TargetID = 7
	var melee *meleeAttackGoal
	for _, g := range wolfGoals(wolf) {
		if mg, ok := g.(*meleeAttackGoal); ok {
			melee = mg
		}
	}
	melee.Tick(&tickView{w: w, players: []Player{survivor(7, 6.5)}})
	if melee.pathedTargetX == 0 {
		t.Error("the wolf planned no search for a target behind a wall")
	}
}

// TestAWolfPanicsAtFireButNotAtABlow: the wolf flees an environmental hurt
// and stands its ground against a player's blow.
// vanilla: Wolf.registerGoals' TamableAnimalPanicGoal
func TestAWolfPanicsAtFireButNotAtABlow(t *testing.T) {
	w := newTestWorld()
	floor(w, -6, -6, 6, 6, 63, defaultState(t, "minecraft:stone"))
	wolf := NewMob(TypeWolf, 1, 0.5, 64, 0.5, 1)
	wolf.OnGround = true
	var panic *panicGoal
	for _, g := range wolfGoals(wolf) {
		if pg, ok := g.(*panicGoal); ok {
			panic = pg
		}
	}
	if panic == nil {
		t.Fatal("the wolf has no panic goal")
	}
	wolf.noteLastDamage(rules.DamageTypePlayerAttack)
	if panic.CanStart(&tickView{w: w}) {
		t.Error("a wolf panicked at a player's blow")
	}
	wolf.noteLastDamage(rules.DamageTypeLava)
	if !panic.CanStart(&tickView{w: w}) {
		t.Error("a wolf did not panic at lava")
	}
	if panic.pace != 1.5 {
		t.Errorf("panic pace = %v, want 1.5", panic.pace)
	}
}

var (
	ownerA = uuid.MustParse("00000000-0000-0000-0000-00000000000a")
	ownerB = uuid.MustParse("00000000-0000-0000-0000-00000000000b")
)

// tameWolf is a tame wolf of the owner at the place given, standing.
func tameWolf(id int32, x, z float64, owner uuid.UUID) *Mob {
	m := NewMob(TypeWolf, id, x, 64, z, int64(id))
	m.OnGround = true
	m.Tamable.Tame, m.Tamable.Owner = true, owner
	return m
}

// TestABoneRollTamesAWolfOneTimeInThree runs the roll over many wolves.
// vanilla: Wolf.tryToTame
func TestABoneRollTamesAWolfOneTimeInThree(t *testing.T) {
	tamed := 0
	for seed := range 3000 {
		m := NewMob(TypeWolf, 1, 0.5, 64, 0.5, int64(seed))
		if m.TryToTame(ownerA) {
			tamed++
			if !m.IsOwnedBy(ownerA) || !m.Tamable.OrderedToSit || m.Health != 40 || m.MaxHealth != 40 {
				t.Fatalf("a tamed wolf is %+v with %v of %v health", m.Tamable, m.Health, m.MaxHealth)
			}
		} else if m.Tamable.Tame {
			t.Fatal("a failed roll tamed the wolf")
		}
	}
	if tamed < 900 || tamed > 1100 {
		t.Errorf("%d of 3000 wolves tamed, want about a third", tamed)
	}
}

// TestATameWolfSitsWithoutItsOwnerAndStandsWithThem: a tame wolf whose owner
// is away sits; with the owner about it stands unless ordered down.
// vanilla: SitWhenOrderedToGoal.canUse
func TestATameWolfSitsWithoutItsOwnerAndStandsWithThem(t *testing.T) {
	w := newTestWorld()
	floor64(w, t)
	away := tameWolf(1, 0.5, 0.5, ownerA)
	near := tameWolf(2, 0.5, 0.5, ownerA)
	owner := Player{X: 3.5, Y: 64, Z: 0.5, EntityID: 7, Huntable: true, UUID: ownerA}
	for range 6 {
		away.tick(w, nil, nil)
		near.tick(w, []Player{owner}, nil)
	}
	if !away.Tamable.SittingPose {
		t.Error("a tame wolf without its owner stands")
	}
	if near.Tamable.SittingPose {
		t.Error("a tame wolf beside its owner sits unordered")
	}
	near.ToggleSit()
	for range 6 {
		near.tick(w, []Player{owner}, nil)
	}
	if !near.Tamable.SittingPose {
		t.Error("an ordered wolf stands")
	}
	near.TakeDamage(1, survivor(9, 3))
	for range 6 {
		near.tick(w, []Player{owner}, nil)
	}
	if near.Tamable.OrderedToSit || near.Tamable.SittingPose {
		t.Error("a hurt wolf is still sitting")
	}
}

// TestAnOrderedWolfRisesWhileItsOwnerIsHurtNearby: the owner under attack
// within twelve blocks pulls an ordered wolf up, but not one further off.
// vanilla: SitWhenOrderedToGoal.canUse
func TestAnOrderedWolfRisesWhileItsOwnerIsHurtNearby(t *testing.T) {
	w := newTestWorld()
	floor64(w, t)
	for _, c := range []struct {
		x       float64
		sitting bool
	}{{3.5, false}, {20.5, true}} {
		wolf := tameWolf(1, 0.5, 0.5, ownerA)
		wolf.Tamable.OrderedToSit = true
		owner := Player{X: c.x, Y: 64, Z: 0.5, EntityID: 7, Huntable: true, UUID: ownerA, HurtBy: 9, HurtByStamp: 5}
		for range 6 {
			wolf.tick(w, []Player{owner}, nil)
		}
		if wolf.Tamable.SittingPose != c.sitting {
			t.Errorf("owner at %v hurt: sitting = %v, want %v", c.x, wolf.Tamable.SittingPose, c.sitting)
		}
	}
}

// TestATameWolfDoesNotTurnOnItsOwner: the owner's own blow is no grudge.
// vanilla: TamableAnimal.canAttack
func TestATameWolfDoesNotTurnOnItsOwner(t *testing.T) {
	w := newTestWorld()
	floor64(w, t)
	wolf := tameWolf(1, 0.5, 0.5, ownerA)
	owner := Player{X: 1.5, Y: 64, Z: 0.5, EntityID: 7, Huntable: true, UUID: ownerA}
	wolf.TakeDamage(1, owner)
	for range 6 {
		wolf.tick(w, []Player{owner}, nil)
	}
	if wolf.TargetID != 0 {
		t.Errorf("a wolf struck by its owner targets %d", wolf.TargetID)
	}
}

// TestATameWolfCallsOnlyItsOwnersPack: a struck tame wolf rallies the wolves
// of its owner and no others.
// vanilla: HurtByTargetGoal.alertOthers
func TestATameWolfCallsOnlyItsOwnersPack(t *testing.T) {
	w := newTestWorld()
	floor64(w, t)
	struck := tameWolf(1, 0.5, 0.5, ownerA)
	kin := tameWolf(2, 2.5, 0.5, ownerA)
	stranger := tameWolf(3, 2.5, 2.5, ownerB)
	wild := NewMob(TypeWolf, 4, 0.5, 64, 2.5, 4)
	attacker := survivor(9, 4.5)
	struck.TakeDamage(1, attacker)
	for range 4 {
		struck.tick(w, []Player{attacker}, []*Mob{kin, stranger, wild})
	}
	if kin.TargetID != 9 {
		t.Errorf("the owner's other wolf targets %d, want 9", kin.TargetID)
	}
	if stranger.TargetID != 0 || wild.TargetID != 0 {
		t.Errorf("another owner's wolf targets %d and a wild one %d, want neither called", stranger.TargetID, wild.TargetID)
	}
}

// TestAWolfOnlyFollowsFromTenBlocks: the follow starts at ten blocks, and a
// sitting wolf never does.
// vanilla: FollowOwnerGoal
func TestAWolfOnlyFollowsFromTenBlocks(t *testing.T) {
	w := newTestWorld()
	for x := int32(-20); x <= 20; x++ {
		for z := int32(-4); z <= 4; z++ {
			w.set(x, 63, z, defaultState(t, "minecraft:stone"))
		}
	}
	for _, c := range []struct {
		dist    float64
		sit     bool
		follows bool
	}{{9, false, false}, {11, false, true}, {11, true, false}} {
		wolf := tameWolf(1, 0.5, 0.5, ownerA)
		wolf.Tamable.OrderedToSit = c.sit
		owner := Player{X: 0.5 + c.dist, Y: 64, Z: 0.5, EntityID: 7, Huntable: true, UUID: ownerA}
		g := &followOwnerGoal{m: wolf}
		if got := g.CanStart(&tickView{w: w, players: []Player{owner}}); got != c.follows {
			t.Errorf("owner %v away, sitting=%v: follow starts = %v, want %v", c.dist, c.sit, got, c.follows)
		}
	}
}

// TestAWolfTakesItsVariantFromTheBiomeAndItsPackKeepsIt: the first wolf of a
// pack picks the variant of the biome, the rest repeat it, whatever biome
// they stand in; a biome without a variant gives the pale wolf.
// vanilla: Wolf.finalizeSpawn, WolfVariants.bootstrap
func TestAWolfTakesItsVariantFromTheBiomeAndItsPackKeepsIt(t *testing.T) {
	for biome, want := range map[string]string{
		"minecraft:forest":                  "minecraft:woods",
		"minecraft:savanna":                 "minecraft:spotted",
		"minecraft:jungle":                  "minecraft:rusty",
		"minecraft:badlands":                "minecraft:striped",
		"minecraft:grove":                   "minecraft:snowy",
		"minecraft:old_growth_pine_taiga":   "minecraft:black",
		"minecraft:snowy_taiga":             "minecraft:ashen",
		"minecraft:old_growth_spruce_taiga": "minecraft:chestnut",
		"minecraft:plains":                  "minecraft:pale",
	} {
		first := NewMob(TypeWolf, 1, 0, 64, 0, 1)
		group := NewSpawnGroup(GoDice{rand.New(rand.NewSource(1))})
		first.FinalizeSpawn(GoDice{first.Rand}, testBiome(biome), group, rules.LocalDifficulty{})
		if first.Variant != want {
			t.Errorf("%s: variant %q, want %q", biome, first.Variant, want)
		}
		next := NewMob(TypeWolf, 2, 0, 64, 0, 2)
		next.FinalizeSpawn(GoDice{next.Rand}, testBiome("minecraft:desert"), group, rules.LocalDifficulty{})
		if next.Variant != want {
			t.Errorf("%s: the pack's next wolf is %q, want %q", biome, next.Variant, want)
		}
		if first.SoundVariant == "" {
			t.Errorf("%s: no sound variant", biome)
		}
	}
}

// TestAWolfSpawnsOnWolvesSpawnableOn checks Wolf.checkWolfSpawnRules: the
// ground is the wolves_spawnable_on tag, not just grass.
func TestAWolfSpawnsOnWolvesSpawnableOn(t *testing.T) {
	spawnGroundCases(t, TypeWolf, map[string]bool{
		"minecraft:podzol":      true,
		"minecraft:coarse_dirt": true,
		"minecraft:snow_block":  true,
		"minecraft:grass_block": true,
		"minecraft:stone":       false,
	})
}

// spawnGroundCases checks validSpawnPositionAt for creature t, in full sky
// light, on each ground block.
func spawnGroundCases(t *testing.T, typ MobType, cases map[string]bool) {
	t.Helper()
	rng := rand.New(rand.NewSource(1))
	for ground, want := range cases {
		w := newSpawnTestWorld()
		w.sky = 15
		floor(w.testWorld, -4, -4, 4, 4, 63, defaultState(t, ground))
		if got := validSpawnPositionAt(w, GoDice{rng}, typ, 0.5, 64, 0.5, 0, CategoryCreature); got != want {
			t.Errorf("%v on %s = %v, want %v", typ, ground, got, want)
		}
	}
}

// TestAnAngryWolfRefusesALead checks that a wolf with anger set cannot be
// leashed while a calm one in reach can.
// vanilla: Wolf.canBeLeashed
func TestAnAngryWolfRefusesALead(t *testing.T) {
	h := &LeashHolder{X: 0.5, Y: 64, Z: 1.5, Height: 1.8, Present: true, SameLevel: true}
	m := NewMob(TypeWolf, 1, 0.5, 64, 0.5, 1)
	if !LeashableBy(m, h) {
		t.Fatal("a calm wolf in reach refuses a lead")
	}
	m.SetAnger(100, uuid.New(), true)
	if !m.Angry() {
		t.Fatal("the wolf is not angry after SetAnger")
	}
	if LeashableBy(m, h) {
		t.Error("an angry wolf accepts a lead")
	}
}
