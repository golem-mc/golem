package entity

import (
	"math"
	"testing"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/rules"
	"github.com/golem-mc/golem/registry"
)

// The enderman's behaviour as unit tests against the plain test world.

// wideFloor lays a stone slab under y=64 wide enough for the teleports'
// thirty-two block reach.
func wideFloor(w testWorld, t testing.TB) {
	stone := defaultState(t, "minecraft:stone")
	for x := int32(-40); x <= 40; x++ {
		for z := int32(-40); z <= 40; z++ {
			w.set(x, 63, z, stone)
		}
	}
}

func newEnderman() *Mob { return NewMob(TypeEnderman, 1, 0.5, 64, 0.5, 11) }

// gazeAt turns the player to look straight at the enderman's eyes.
func gazeAt(p *Player, m *Mob) {
	dx, dy, dz := m.X-p.X, m.Y+m.eyeHeight()-playerEye(p), m.Z-p.Z
	p.Yaw = math.Atan2(-dx, dz) * 180 / math.Pi
	p.Pitch = -math.Atan2(dy, math.Hypot(dx, dz)) * 180 / math.Pi
}

func starer(m *Mob, dist float64) Player {
	p := Player{X: m.X + dist, Y: 64, Z: m.Z, EntityID: 7, Huntable: true}
	gazeAt(&p, m)
	return p
}

// tickFor ticks the mob until stop answers true or the ticks run out, and
// reports the tick it stopped on (or -1).
func tickFor(m *Mob, w testWorld, players []Player, ticks int, stop func(events []MobEvent) bool) int {
	for i := range ticks {
		if stop(m.Tick(w, players)) {
			return i
		}
	}
	return -1
}

// tickStaring is tickFor with the player re-aiming at the enderman's eyes
// before every tick, so a stroll does not spoil the gaze.
func tickStaring(m *Mob, w testWorld, p Player, ticks int, stop func(events []MobEvent) bool) int {
	for i := range ticks {
		gazeAt(&p, m)
		if stop(m.Tick(w, []Player{p})) {
			return i
		}
	}
	return -1
}

func teleported(events []MobEvent) bool {
	for _, ev := range events {
		if ev.Kind == MobTeleported {
			return true
		}
	}
	return false
}

func TestAnEndermanIsProvokedByAGazeAtItsEyes(t *testing.T) {
	w := newTestWorld()
	wideFloor(w, t)
	m := newEnderman()
	if tickStaring(m, w, starer(m, 8), 60, func([]MobEvent) bool { return m.TargetID == 7 }) < 0 {
		t.Fatal("a player staring at the eyes never became the enderman's target")
	}
	if !m.Creepy() || !m.StaredAt() {
		t.Errorf("Creepy = %v, StaredAt = %v, want both set with a target", m.Creepy(), m.StaredAt())
	}
}

func TestAnEndermanIgnoresAGazeThatMissesItsEyes(t *testing.T) {
	w := newTestWorld()
	wideFloor(w, t)
	m := newEnderman()
	p := starer(m, 8)
	// Looking at the feet, 2.55 blocks under the eyes, leaves the cone
	// 0.025/8 wide.
	p.Pitch += 12
	players := []Player{p}
	if tickFor(m, w, players, 200, func([]MobEvent) bool { return m.TargetID != 0 }) >= 0 {
		t.Fatal("a gaze at the enderman's feet provoked it")
	}
}

func TestAnEndermanIgnoresAGazeThroughAWall(t *testing.T) {
	w := newTestWorld()
	wideFloor(w, t)
	stone := defaultState(t, "minecraft:stone")
	for y := int32(64); y <= 68; y++ {
		w.set(4, y, 0, stone)
	}
	m := newEnderman()
	players := []Player{starer(m, 8)}
	if tickFor(m, w, players, 200, func([]MobEvent) bool { return m.TargetID != 0 }) >= 0 {
		t.Fatal("a gaze through a wall provoked the enderman")
	}
}

func TestAWallBreaksTheLineOfTheGaze(t *testing.T) {
	w := newTestWorld()
	wideFloor(w, t)
	m := newEnderman()
	p := starer(m, 8)
	if !m.beingStaredBy(w, &p) {
		t.Fatal("an open gaze at the eyes was not a stare")
	}
	w.set(4, 66, 0, defaultState(t, "minecraft:stone"))
	if m.beingStaredBy(w, &p) {
		t.Error("a gaze through a stone block was a stare")
	}
}

func TestACarvedPumpkinMakesAGazeHarmless(t *testing.T) {
	w := newTestWorld()
	wideFloor(w, t)
	pumpkin, ok := registry.ItemID("minecraft:carved_pumpkin")
	if !ok {
		t.Fatal("no carved pumpkin item")
	}
	m := newEnderman()
	p := starer(m, 8)
	p.Worn[wornHead] = protocol.ItemStack{ItemID: pumpkin, Count: 1}
	if tickStaring(m, w, p, 200, func([]MobEvent) bool { return m.TargetID != 0 }) >= 0 {
		t.Fatal("a player in a carved pumpkin provoked the enderman by staring")
	}
	// The same gaze without it does.
	p.Worn[wornHead] = protocol.ItemStack{}
	if tickStaring(m, w, p, 60, func([]MobEvent) bool { return m.TargetID != 0 }) < 0 {
		t.Fatal("the control stare without the pumpkin never provoked the enderman")
	}
}

func TestAStaredAtEndermanStandsStill(t *testing.T) {
	w := newTestWorld()
	wideFloor(w, t)
	m := newEnderman()
	m.TargetID = 7
	p := starer(m, 10)
	startX := m.X
	for range 40 {
		gazeAt(&p, m)
		m.Tick(w, []Player{p})
		// Out of the 16-block blink range, so it stays put, as it also
		// does not walk.
		if p.X-m.X < 4 {
			t.Fatalf("the enderman closed on a staring target to %v", m.X)
		}
	}
	if math.Abs(m.X-startX) > 0.01 {
		t.Errorf("a stared-at enderman moved from %v to %v", startX, m.X)
	}
	// Looking away frees it.
	p.Pitch = -80
	tickFor(m, w, []Player{p}, 60, func([]MobEvent) bool { return m.X > startX+0.5 })
	if m.X <= startX+0.5 {
		t.Errorf("the enderman never walked at a target that looked away, x = %v", m.X)
	}
}

func TestAnEndermanBlinksAwayFromAStaringTargetUpClose(t *testing.T) {
	w := newTestWorld()
	wideFloor(w, t)
	m := newEnderman()
	// Becomes the target, then the near stare teleports it.
	if tickStaring(m, w, starer(m, 3), 100, teleported) < 0 {
		t.Fatal("a target staring from three blocks never made the enderman teleport")
	}
}

func TestAnEndermanBlinksTowardsAFarTargetThatLooksAway(t *testing.T) {
	w := newTestWorld()
	wideFloor(w, t)
	m := newEnderman()
	p := starer(m, 25)
	if tickStaring(m, w, p, 60, func([]MobEvent) bool { return m.TargetID == 7 }) < 0 {
		t.Fatal("a stare from 25 blocks never became a target")
	}
	// Looking at the sky is no stare; thirty ticks later the enderman jumps
	// sixteen blocks closer.
	p.Pitch = -90
	before := Dist2(m.X, m.Y, m.Z, p.X, p.Y, p.Z)
	var after float64
	n := tickFor(m, w, []Player{p}, 200, func(events []MobEvent) bool {
		if teleported(events) {
			after = Dist2(m.X, m.Y, m.Z, p.X, p.Y, p.Z)
			return true
		}
		return false
	})
	if n < 0 {
		t.Fatal("the enderman never teleported towards a far target that looked away")
	}
	if n < 25 {
		t.Errorf("teleported after %d ticks, want the 30-tick wait (15 selections)", n)
	}
	if math.Sqrt(before)-math.Sqrt(after) < 5 {
		t.Errorf("distance went from %v to %v, want a jump of about 16 closer", math.Sqrt(before), math.Sqrt(after))
	}
}

func TestAnEndermanTeleportsAwayFromAnArrow(t *testing.T) {
	w := newTestWorld()
	wideFloor(w, t)
	m := newEnderman()
	m.Tick(w, nil)
	x, z := m.X, m.Z
	landed, soft := m.HurtByArrow(6, 1, 0, 0, 5, true, false)
	if !landed || !soft {
		t.Fatalf("HurtByArrow = (%v, %v), want the hit swallowed without a hurt telling", landed, soft)
	}
	if !m.TookDodge() {
		t.Error("the dodge was not reported to the arrow")
	}
	if m.TookDodge() {
		t.Error("the dodge was reported twice")
	}
	if m.Health != m.MaxHealth {
		t.Errorf("an arrow hurt the enderman to %v health", m.Health)
	}
	if m.X == x && m.Z == z {
		t.Fatal("the enderman stayed where the arrow hit it, the teleport waiting for a tick")
	}
	if d := math.Hypot(m.X-x, m.Z-z); d > 32.01 {
		t.Errorf("teleported %v blocks sideways, want at most 32", d)
	}
	if math.Abs(m.Y-64) > 32 {
		t.Errorf("teleported to y = %v, want within 32 of 64", m.Y)
	}
	if !teleported(m.Tick(w, nil)) {
		t.Error("the teleport of the hit was not told on the next tick")
	}
}

func TestAnEndermanThatCannotTeleportBouncesTheArrow(t *testing.T) {
	w := newTestWorld()
	m := newEnderman()
	m.Tick(w, nil)
	if landed, _ := m.HurtByArrow(6, 1, 0, 0, 5, true, false); landed {
		t.Error("the hit counts as landed with nowhere to teleport to")
	}
	if m.TookDodge() {
		t.Error("a failed teleport was reported as a dodge")
	}
	if m.Health != m.MaxHealth {
		t.Errorf("health = %v, want no damage", m.Health)
	}
}

// vanilla: Enderman.hurtServer refuses projectile damage even when all teleports fail.
func TestAnEndermanRefusesProjectileDamageWithoutATeleportDestination(t *testing.T) {
	for _, amount := range []float64{0, 2} {
		for _, cause := range []int32{0, 5} {
			m := newEnderman()
			m.Tick(newTestWorld(), nil)
			x, y, z := m.X, m.Y, m.Z
			landed, _ := m.HurtByCommand(amount, rules.DamageTypeArrow, cause, cause != 0, false, 0, 0)
			if landed || m.Health != m.MaxHealth || m.InvulnTicks != 0 {
				t.Errorf("amount %v, cause %d: landed %v, health %v, cooldown %d", amount, cause, landed, m.Health, m.InvulnTicks)
			}
			if m.X != x || m.Y != y || m.Z != z {
				t.Error("teleported without a destination")
			}
		}
	}
}

func TestAnEndermanTeleportsFromEveryProjectile(t *testing.T) {
	w := newTestWorld()
	wideFloor(w, t)
	hits := map[string]func(m *Mob) bool{
		"thrown":      func(m *Mob) bool { l, _ := m.HurtByThrown(0, 1, 0, 5, true, false); return l },
		"wind charge": func(m *Mob) bool { l, _ := m.HurtByWindCharge(1, 1, 0, 5, true, false); return l },
		"fireball": func(m *Mob) bool {
			l, _ := m.HurtByProjectile(6, rules.DamageTypeFireball, 1, 0, 5, true, false)
			return l
		},
		"trident": func(m *Mob) bool { l, _ := m.HurtByTrident(8, 1, 0, 0, 5, true, false); return l },
	}
	for name, hit := range hits {
		m := newEnderman()
		m.Tick(w, nil)
		x, z := m.X, m.Z
		if !hit(m) {
			t.Errorf("%s: the hit did not count as landed", name)
		}
		if m.Health != m.MaxHealth {
			t.Errorf("%s: health %v, want the hit dodged", name, m.Health)
		}
		if m.X == x && m.Z == z {
			t.Errorf("%s: no teleport", name)
		}
	}
}

func TestAnEndermanRollsNoTeleportForALivingCause(t *testing.T) {
	w := newTestWorld()
	wideFloor(w, t)
	for _, tc := range []struct {
		name string
		hurt func(m *Mob)
		want bool
	}{
		{"nobody", func(m *Mob) { m.HurtByWorld(1, rules.DamageTypeMagic) }, true},
		{"blast with nobody behind", func(m *Mob) { m.HurtByCausedBlast(1, 0, 0, 0, rules.DamageTypeExplosion, false, false, 0) }, true},
		{"blast of a creeper", func(m *Mob) { m.HurtByCausedBlast(1, 0, 0, 0, rules.DamageTypeExplosion, false, true, 0) }, false},
		{"thorns", func(m *Mob) { m.HurtByPlayerThorns(1, rules.DamageTypeThorns, 5, false, 0, 0) }, false},
	} {
		got := false
		for seed := range int64(40) {
			m := NewMob(TypeEnderman, 1, 0.5, 64, 0.5, seed)
			m.Tick(w, nil)
			x, z := m.X, m.Z
			tc.hurt(m)
			if m.X != x || m.Z != z {
				got = true
			}
		}
		if got != tc.want {
			t.Errorf("%s: teleported = %v, want %v", tc.name, got, tc.want)
		}
	}
}

func TestLosingTheTargetResetsTheDaylightHold(t *testing.T) {
	w := newTestWorld()
	m := newEnderman()
	m.Age = 1000
	m.TargetID = 5
	m.endermanStep(w, nil)
	if m.enderman.targetChangeAge != 1000 {
		t.Fatalf("targetChangeAge = %d after a new target, want 1000", m.enderman.targetChangeAge)
	}
	m.Age = 1100
	m.TargetID = 0
	m.endermanStep(w, nil)
	if m.enderman.targetChangeAge != 0 {
		t.Errorf("targetChangeAge = %d after losing the target, want 0", m.enderman.targetChangeAge)
	}
}

func TestAnEndermanTakesAMeleeHitWithoutTeleporting(t *testing.T) {
	w := newTestWorld()
	wideFloor(w, t)
	m := newEnderman()
	m.TakeDamage(5, Player{X: 2, Y: 64, Z: 0.5, EntityID: 5})
	if m.Health != 35 {
		t.Fatalf("health after a 5-point melee hit = %v, want 35", m.Health)
	}
	for range 40 {
		if teleported(m.Tick(w, nil)) {
			t.Fatal("a melee hit made the enderman teleport")
		}
	}
}

func TestAnEndermanInWaterIsHurtAndTeleportsAway(t *testing.T) {
	w := newTestWorld()
	wideFloor(w, t)
	water := defaultState(t, "minecraft:water")
	for y := int32(64); y <= 67; y++ {
		w.set(0, y, 0, water)
	}
	m := newEnderman()
	var saw bool
	tickFor(m, w, nil, 400, func(events []MobEvent) bool {
		saw = saw || teleported(events)
		return saw
	})
	if m.Health >= m.MaxHealth {
		t.Errorf("health = %v after standing in water, want the drowning bite", m.Health)
	}
	if !saw {
		t.Error("no teleport after the water's bites")
	}
}

func TestAnEndermanInRainIsHurt(t *testing.T) {
	w := newTestWorld()
	wideFloor(w, t)
	w.rain = true
	w.sky = 15
	m := newEnderman()
	m.Tick(w, nil)
	if m.Health >= m.MaxHealth {
		t.Fatal("rain landing on the enderman did not hurt it")
	}

	// A roof keeps the rain off.
	w.set(0, 70, 0, defaultState(t, "minecraft:stone"))
	dry := newEnderman()
	dry.Tick(w, nil)
	if dry.Health != dry.MaxHealth {
		t.Errorf("an enderman under a roof lost health to the rain: %v", dry.Health)
	}
}

func TestDaylightSendsAnEndermanAway(t *testing.T) {
	w := newTestWorld()
	wideFloor(w, t)
	w.bright = true
	w.sky = 15
	w.darken = 0
	m := newEnderman()
	first := tickFor(m, w, nil, 3000, teleported)
	if first < 0 {
		t.Fatal("daylight never teleported the enderman")
	}
	if first < 600 {
		t.Errorf("teleported at tick %d, before the 600-tick hold of a fresh target", first)
	}
}

func TestAnEndermanStaysUnderTheNightSky(t *testing.T) {
	w := newTestWorld()
	wideFloor(w, t)
	w.sky = 15
	m := newEnderman()
	if tickFor(m, w, nil, 3000, teleported) >= 0 {
		t.Fatal("the enderman teleported at night with no cause")
	}
}

func TestAnEndermanDropsAnEnderPearl(t *testing.T) {
	pearl, _ := registry.ItemID("minecraft:ender_pearl")
	got := 0
	for seed := range 200 {
		m := NewMob(TypeEnderman, 1, 0.5, 64, 0.5, int64(seed))
		for _, s := range m.RollDrops(rules.LootKiller{}, nil) {
			if s.ItemID == pearl {
				got++
			}
		}
	}
	if got == 0 || got > 150 {
		t.Errorf("%d of 200 endermen dropped a pearl, want about half", got)
	}
}

func TestAnAttackingEndermanWalksFaster(t *testing.T) {
	m := newEnderman()
	idle := m.pace()
	m.TargetID = 7
	if got := m.pace() - idle; math.Abs(got-float64(float32(0.15))) > 1e-9 {
		t.Errorf("attacking speed bonus = %v, want 0.15", got)
	}
}

func TestAThrownDodgeIsNotReadByALaterArrow(t *testing.T) {
	w := newTestWorld()
	wideFloor(w, t)
	m := newEnderman()
	m.Tick(w, nil)
	if landed, _ := m.HurtByThrown(0, 1, 0, 5, true, false); !landed {
		t.Fatal("the thrown item was not dodged")
	}
	m.HurtByArrow(0, 1, 0, 0, 5, true, false)
	if m.TookDodge() {
		t.Error("a zero-damage arrow read the dodge of an earlier thrown item")
	}
}

// TestASecondWaterSplashInTheHurtWindowLandsNothing blinks the enderman away
// again but reports no landed damage, so no damage event is told.
// vanilla: EnderMan.hurtServer
func TestASecondWaterSplashInTheHurtWindowLandsNothing(t *testing.T) {
	w := newTestWorld()
	wideFloor(w, t)
	m := newEnderman()
	m.Tick(w, nil)
	if landed, _ := m.HurtByWaterSplash(0, 1, 0, false, false); !landed {
		t.Fatal("the first splash landed no damage")
	}
	x, z := m.X, m.Z
	if landed, _ := m.HurtByWaterSplash(0, 1, 0, false, false); landed {
		t.Error("a second splash inside the hurt window reported landed damage")
	}
	if m.X == x && m.Z == z {
		t.Error("the second splash did not teleport the enderman")
	}
}

// floorWith lays a wide stone floor under y=64 and fills y=64 with body
// across it when body is non-zero; floor replaces the stone when non-zero.
func floorWith(t *testing.T, floor, body uint16) testWorld {
	w := newTestWorld()
	wideFloor(w, t)
	for x := int32(-40); x <= 40; x++ {
		for z := int32(-40); z <= 40; z++ {
			if floor != 0 {
				w.set(x, 63, z, floor)
			}
			if body != 0 {
				w.set(x, 64, z, body)
			}
		}
	}
	return w
}

// An enderman never lands on a floor, or with its body in a block, of
// #enderman_does_not_teleport_to; a plain stone floor takes it.
// vanilla: EnderMan.teleport, LivingEntity.checkPositionAndTeleport
func TestAnEndermanAvoidsItsDoesNotTeleportToBlocks(t *testing.T) {
	for _, tc := range []struct {
		name        string
		floor, body string
		lands       bool
	}{
		{name: "stone floor", lands: true},
		{name: "magma floor", floor: "minecraft:magma_block"},
		{name: "bedrock floor", floor: "minecraft:bedrock"},
		{name: "fire in the body", body: "minecraft:fire"},
	} {
		var floor, body uint16
		if tc.floor != "" {
			floor = defaultState(t, tc.floor)
		}
		if tc.body != "" {
			body = defaultState(t, tc.body)
		}
		w := floorWith(t, floor, body)
		m := newEnderman()
		landed := 0
		for range 200 {
			if teleported(m.teleportRandom(w, nil)) {
				landed++
				if b := registry.BlockOf(w.BlockAt(int32(math.Floor(m.X)), int32(math.Floor(m.Y))-1, int32(math.Floor(m.Z)))); b.InTag(registry.TagIDEndermanDoesNotTeleportTo) {
					t.Fatalf("%s: landed on %s", tc.name, b.Name())
				}
			}
		}
		if got := landed > 0; got != tc.lands {
			t.Errorf("%s: %d of 200 teleports landed, want landing = %v", tc.name, landed, tc.lands)
		}
	}
}

// A cat's teleport avoids #cat_does_not_teleport_to, which unlike the
// enderman's tag leaves out bedrock.
// vanilla: CatRelaxOnOwnerGoal.giveMorningGift
func TestACatsTeleportAvoidsItsOwnTag(t *testing.T) {
	for floor, lands := range map[string]bool{"minecraft:bedrock": true, "minecraft:magma_block": false} {
		w := floorWith(t, defaultState(t, floor), 0)
		m := NewMob(TypeCat, 1, 0.5, 64, 0.5, 11)
		got := teleported(m.teleportTo(w, 4.5, 64, 4.5, registry.TagIDCatDoesNotTeleportTo, nil))
		if got != lands {
			t.Errorf("a cat over %s: teleported = %v, want %v", floor, got, lands)
		}
	}
}

// clampedWorld holds a random teleport inside x and z of [-3, 3).
type clampedWorld struct{ testWorld }

func (clampedWorld) ClampToBorder(x, z float64) (float64, float64) {
	const maxEdge = 3 - float64(float32(1e-5))
	return math.Max(-3, math.Min(x, maxEdge)), math.Max(-3, math.Min(z, maxEdge))
}

// vanilla: LivingEntity.randomTeleport, WorldBorder.clampVec3ToBound
func TestAMobsTeleportClampsToTheWorldBorder(t *testing.T) {
	cw := clampedWorld{floorWith(t, 0, 0)}
	// A boat in the world wraps it in Bodies, which must keep the clamp.
	for name, w := range map[string]World{"plain": cw, "with boats": Bodies{World: cw}} {
		m := newEnderman()
		if !teleported(m.teleportTo(w, 20.5, 64, 0.5, registry.TagIDEndermanDoesNotTeleportTo, nil)) {
			t.Fatalf("%s: the enderman did not teleport", name)
		}
		if want := 3 - float64(float32(1e-5)); m.X != want || m.Z != 0.5 {
			t.Errorf("%s: landed at (%v, %v), want (%v, 0.5)", name, m.X, m.Z, want)
		}
	}
}
