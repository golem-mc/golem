package entity

import (
	"math/rand"
	"testing"

	"github.com/golem-mc/golem/internal/server/rules"
	"github.com/golem-mc/golem/internal/server/world"
	"github.com/golem-mc/golem/registry"
)

// fortressTestWorld is a spawn test world with a fortress piece and bounds
// the test set.
type fortressTestWorld struct {
	spawnTestWorld
	inBounds, inPiece bool
}

func (w fortressTestWorld) FortressAt(x, y, z int32) (bool, bool) { return w.inBounds, w.inPiece }

func netherSpawnWorld(t *testing.T) spawnTestWorld {
	t.Helper()
	w := newSpawnTestWorld()
	w.dimension = world.NetherName
	w.biomeName = "minecraft:nether_wastes"
	w.groundAt(t, 10, 63, 10)
	w.blockLights[[3]int32{10, 64, 10}] = 15
	return w
}

// TestBlazesSpawnInLightAndGhastsOnOneRollInTwenty pins
// checkAnyLightMonsterSpawnRules (no light check) and checkGhastSpawnRules
// (nextInt(20) == 0, no light check).
// vanilla: Monster.checkAnyLightMonsterSpawnRules, Ghast.checkGhastSpawnRules
func TestBlazesSpawnInLightAndGhastsOnOneRollInTwenty(t *testing.T) {
	w := netherSpawnWorld(t)
	rng := rand.New(rand.NewSource(1))
	if !validSpawnPositionForType(w, rng, TypeBlaze, 10, 64, 10, 100, CategoryMonster) {
		t.Error("a blaze was refused in block light 15, vanilla has no light check")
	}
	if validSpawnPositionForType(w, rng, TypeZombie, 10, 64, 10, 100, CategoryMonster) {
		t.Error("a zombie spawned in block light 15")
	}
	const trials = 4000
	passed := 0
	for range trials {
		if validSpawnPositionForType(w, rng, TypeGhast, 10, 64, 10, 100, CategoryMonster) {
			passed++
		}
	}
	if passed < trials/20*7/10 || passed > trials/20*13/10 {
		t.Errorf("%d of %d ghast rolls passed, want about one in twenty", passed, trials)
	}
	if got := maxSpawnClusterSize(TypeGhast); got != 1 {
		t.Errorf("the ghast cluster size = %d, want 1", got)
	}
}

// TestAFortressPieceSpawnsTheFortressList pins mobsAt: inside a fortress
// piece the monster list is the fortress's whatever the block below is; in
// the bounds alone it takes nether bricks below; elsewhere the biome's.
// vanilla: NaturalSpawner.mobsAt, NaturalSpawner.isInNetherFortressBounds
func TestAFortressPieceSpawnsTheFortressList(t *testing.T) {
	base := netherSpawnWorld(t)
	inList := func(w SpawnWorld, mob MobType) bool {
		for _, d := range spawnListAt(w, 10, 64, 10, CategoryMonster, w.Biome(10, 64, 10)) {
			if d.mob == mob {
				return true
			}
		}
		return false
	}
	piece := fortressTestWorld{spawnTestWorld: base, inBounds: true, inPiece: true}
	if !inList(piece, TypeWitherSkeleton) {
		t.Error("a piece of the fortress does not list the wither skeleton")
	}
	bounds := fortressTestWorld{spawnTestWorld: netherSpawnWorld(t), inBounds: true}
	if inList(bounds, TypeWitherSkeleton) {
		t.Error("the fortress's bounds over stone list the wither skeleton")
	}
	bricks := fortressTestWorld{spawnTestWorld: netherSpawnWorld(t), inBounds: true}
	bricks.set(10, 63, 10, defaultState(t, "minecraft:nether_bricks"))
	if !inList(bricks, TypeWitherSkeleton) {
		t.Error("nether bricks inside the fortress's bounds do not list the wither skeleton")
	}
	if inList(base, TypeWitherSkeleton) {
		t.Error("a nether wastes position outside a fortress lists the wither skeleton")
	}
	// The pack's entry must still belong to the list where it stands.
	data := &fortressEnemies[2]
	if !canSpawnMobAt(piece, 10, 64, 10, CategoryMonster, data) {
		t.Error("the fortress's entry is refused inside the fortress")
	}
	if canSpawnMobAt(base, 10, 64, 10, CategoryMonster, data) {
		t.Error("the fortress's entry is allowed outside the fortress")
	}
}

// TestABlazeOutsideItsHomeWalksBackToIt: MoveTowardsRestrictionGoal starts
// only outside the home and picks a cell nearer to it.
// vanilla: MoveTowardsRestrictionGoal
func TestABlazeOutsideItsHomeWalksBackToIt(t *testing.T) {
	w := newTestWorld()
	stone := defaultState(t, "minecraft:stone")
	for x := int32(-20); x <= 60; x++ {
		for z := int32(-20); z <= 20; z++ {
			w.set(x, 63, z, stone)
		}
	}
	view := &tickView{w: w}
	m := NewMob(TypeBlaze, 1, 40.5, 64, 0.5, 1)
	g := &homeGoal{m: m, priority: blazeHomePriority, pace: 1}
	wired := false
	for _, goal := range blazeGoals(m) {
		_, isHome := goal.(*homeGoal)
		wired = wired || isHome
	}
	if !wired {
		t.Error("the blaze's goals hold no return home")
	}
	if g.CanStart(view) {
		t.Error("a blaze with no home started walking home")
	}
	m.RestrictTo(0, 64, 0, 8)
	if !g.CanStart(view) {
		t.Fatal("a blaze outside its home did not start walking home")
	}
	if g.target.X >= 40 {
		t.Errorf("the walk's cell is x=%d, want one nearer the home than 40", g.target.X)
	}
	m.X = 1.5
	if g.CanStart(view) {
		t.Error("a blaze inside its home started walking home")
	}
}

// TestOnlyAPlayersFireballHurtsAGhastAndItKillsAndDropsTheDisc: the fire
// immunity stops a mob's fireball, the reflected one deals 1000 and the kill
// rolls the music disc.
// vanilla: Ghast.isInvulnerableTo, Ghast.hurtServer, entities/ghast.json
func TestOnlyAPlayersFireballHurtsAGhastAndItKillsAndDropsTheDisc(t *testing.T) {
	m := NewMob(TypeGhast, 1, 0.5, 64, 0.5, 1)
	if landed, _ := m.HurtByProjectile(6, rules.DamageTypeFireball, 1, 0, 9, false, false); landed || m.Health != 10 {
		t.Fatalf("a mob's fireball hurt a ghast: landed %v health %v", landed, m.Health)
	}
	if landed, _ := m.HurtByReflectedFireball(1, 0, 7, false); !landed || m.Health != 0 {
		t.Fatalf("a reflected fireball left the ghast landed %v health %v", landed, m.Health)
	}
	killer := rules.LootKiller{Weapon: rules.LootTool{Item: "minecraft:iron_sword"}}
	discs := 0
	for seed := range 20 {
		g := NewMob(TypeGhast, 1, 0.5, 64, 0.5, int64(seed))
		g.HurtByReflectedFireball(1, 0, 7, false)
		for _, stack := range g.RollDrops(killer, nil) {
			if name, _ := registry.ItemName(stack.ItemID); name == "minecraft:music_disc_tears" {
				discs++
			}
		}
	}
	if discs != 20 {
		t.Errorf("%d discs from 20 reflected kills, want 20", discs)
	}
	// An empty hand has no Weapon; the Player flag still makes the attacker.
	bare := rules.LootKiller{Player: true}
	g := NewMob(TypeGhast, 1, 0.5, 64, 0.5, 1)
	g.HurtByReflectedFireball(1, 0, 7, false)
	found := false
	for _, stack := range g.RollDrops(bare, nil) {
		if name, _ := registry.ItemName(stack.ItemID); name == "minecraft:music_disc_tears" {
			found = true
		}
	}
	if !found {
		t.Error("a bare-handed player's reflected kill dropped no disc")
	}
}

// TestAGhastHeadFollowsItsBodyTenDegreesATick: the head eases to the body's
// yaw instead of snapping.
// vanilla: LookControl.tick
func TestAGhastHeadFollowsItsBodyTenDegreesATick(t *testing.T) {
	m := NewMob(TypeGhast, 1, 0.5, 64, 0.5, 1)
	m.HeadYaw = 0
	m.VX, m.VZ = -1, 0 // yaw 90
	(&ghastLookGoal{m: m}).Tick(nil)
	if m.Yaw != 90 || m.HeadYaw != ghastHeadTurnSpeed {
		t.Errorf("yaw %v head %v, want 90 and %d", m.Yaw, m.HeadYaw, ghastHeadTurnSpeed)
	}
}

// TestAHoglinSpawnsInLightButNeverOnNetherWartBlock pins
// checkHoglinSpawnRules: no light check, refused over a nether wart block.
// vanilla: Hoglin.checkHoglinSpawnRules
func TestAHoglinSpawnsInLightButNeverOnNetherWartBlock(t *testing.T) {
	w := netherSpawnWorld(t)
	w.biomeName = "minecraft:crimson_forest"
	w.set(10, 63, 10, defaultState(t, "minecraft:crimson_nylium"))
	rng := rand.New(rand.NewSource(1))
	if !validSpawnPositionForType(w, rng, TypeHoglin, 10, 64, 10, 100, CategoryMonster) {
		t.Error("a hoglin was refused on crimson nylium in block light 15, vanilla has no light check")
	}
	w.set(10, 63, 10, defaultState(t, "minecraft:nether_wart_block"))
	w.blockLights[[3]int32{10, 64, 10}] = 0
	if validSpawnPositionForType(w, rng, TypeHoglin, 10, 64, 10, 100, CategoryMonster) {
		t.Error("a hoglin spawned on a nether wart block")
	}
}
