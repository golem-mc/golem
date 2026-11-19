package entity

import (
	"math"
	"math/rand"
)

// The cube mobs (AbstractCubeMob): slime and magma cube. Box, health, pace
// and blow scale with size; they move only by hopping, hurt by touch, and
// split into smaller cubes on death unless at the smallest size.

// cubeMove is CubeMobMoveControl's state: target yaw, countdown to the next
// hop, whether hops are the hunt's quick ones, and this tick's wanted move.
type cubeMove struct {
	yRot        float32
	jumpDelay   int
	aggressive  bool
	wanted      bool
	speedMod    float64
	wasOnGround bool
}

// isCube reports whether the mob is one of the cube mobs.
func (m *Mob) isCube() bool { return m.kind().cube }

// SetSize is AbstractCubeMob.setSize: the size clamped to 1..127, the max
// health its square, and with full the health filled to it. A changed size
// in water splashes one time in twenty, told on the next step.
// vanilla: AbstractCubeMob.onSyncedDataUpdated
func (m *Mob) SetSize(size int, full bool) {
	old := m.Size
	m.SetMetadataSize(min(max(size, 1), 127))
	if m.Size != old && m.InWater() && m.Rand.Intn(20) == 0 {
		m.queued = m.waterSplash(m.queued)
	}
	m.MaxHealth = float64(m.Size * m.Size)
	if m.Type == TypeSulfurCube {
		m.MaxHealth = float64(4 * m.Size)
		if full && m.Size == 1 && !m.IsBaby() {
			m.SetBaby(true)
		}
	}
	m.syncMaxHealth()
	if full || m.Health > m.MaxHealth {
		m.SetMetadataHealth(m.MaxHealth)
	}
}

// tiny is AbstractCubeMob.isTiny.
func (m *Mob) tiny() bool { return m.Size <= 1 }

// cubeDealsDamage is isDealsDamage: a slime bigger than the smallest, and
// every magma cube.
func (m *Mob) cubeDealsDamage() bool {
	return m.Type != TypeSulfurCube && (m.Type == TypeMagmaCube || !m.tiny())
}

// cubeAttackDamage is the cube's attack damage, its size, and the magma
// cube's two more (MagmaCube.getAttackDamage).
func (m *Mob) cubeAttackDamage() float64 {
	if m.Type == TypeMagmaCube {
		return float64(m.Size) + 2
	}
	return float64(m.Size)
}

// cubePace is setSize's movement speed, 0.2 + 0.1 a size in float
// arithmetic.
func (m *Mob) cubePace() float64 {
	return float64(float32(0.2) + float32(0.1)*float32(m.Size))
}

// SetSpawnSize is AbstractCubeMob.setSpawnSize: size 1, 2 or 4, larger more
// often as the special multiplier grows. Other kinds are untouched.
func (m *Mob) SetSpawnSize(special float32) {
	if !m.isCube() {
		return
	}
	if m.Type == TypeSulfurCube {
		size := 2
		if m.IsBaby() {
			size = 1
		}
		m.SetSize(size, true)
		return
	}
	scale := m.Rand.Intn(3)
	if scale < 2 && m.Rand.Float32() < 0.5*special {
		scale++
	}
	m.SetSize(1<<scale, true)
}

// cubeGoals is AbstractCubeMob.registerGoals with the slime's and the
// magma cube's own attack and target goals.
func cubeGoals(m *Mob) []Goal {
	if m.Type == TypeSulfurCube {
		return []Goal{&cubeFloatGoal{m: m}, &temptGoal{m: m, priority: 2, reach: 8, stopAt: 1}, &sulfurSearchGoal{m: m}, &cubeRandomDirectionGoal{m: m}, &cubeKeepJumpingGoal{m: m}}
	}
	return []Goal{
		&cubeFloatGoal{m: m},
		// vanilla: NearestAttackableTargetGoal(Player, 10, true, false,
		// within four blocks up or down)
		&nearestTargetGoal{m: m, priority: 1, accept: func(_ *tickView, p *Player) bool {
			return math.Abs(p.Y-m.Y) <= 4
		}},
		&cubeAttackGoal{m: m},
		&mobTargetGoal{m: m, priority: 3, victim: TypeIronGolem},
		&cubeRandomDirectionGoal{m: m},
		&cubeKeepJumpingGoal{m: m},
	}
}

// cubeFloatGoal is CubeMobFloatGoal: in water or lava, a jump four ticks in
// five and the wanted movement of 1.2.
type cubeFloatGoal struct{ m *Mob }

func (g *cubeFloatGoal) Priority() int    { return 1 }
func (g *cubeFloatGoal) Flags() GoalFlags { return GoalJump | GoalMove }
func (g *cubeFloatGoal) CanStart(*tickView) bool {
	return g.m.InWater() || g.m.InLava()
}
func (g *cubeFloatGoal) Start(*tickView)                 {}
func (g *cubeFloatGoal) ShouldContinue(t *tickView) bool { return g.CanStart(t) }
func (g *cubeFloatGoal) Stop(*tickView)                  {}
func (g *cubeFloatGoal) RequiresEveryTick() bool         { return true }
func (g *cubeFloatGoal) Tick(*tickView) {
	if g.m.Rand.Float32() < 0.8 {
		g.m.jump = true
	}
	g.m.cube.wanted, g.m.cube.speedMod = true, 1.2
}

// cubeAttackGoal is CubeMobAttackGoal: with a target, turn toward it ten
// degrees a tick and hop at it (quick hops only for damaging cubes), giving
// up after 300 ticks.
type cubeAttackGoal struct {
	m         *Mob
	growTired int
}

func (g *cubeAttackGoal) Priority() int    { return 2 }
func (g *cubeAttackGoal) Flags() GoalFlags { return GoalLook }
func (g *cubeAttackGoal) CanStart(t *tickView) bool {
	return g.m.targetIn(t) != nil
}
func (g *cubeAttackGoal) Start(*tickView) { g.growTired = reducedTickDelay(300) }
func (g *cubeAttackGoal) ShouldContinue(t *tickView) bool {
	if g.m.targetIn(t) == nil {
		return false
	}
	g.growTired--
	return g.growTired > 0
}
func (g *cubeAttackGoal) Stop(*tickView)          {}
func (g *cubeAttackGoal) RequiresEveryTick() bool { return true }
func (g *cubeAttackGoal) Tick(t *tickView) {
	if p := g.m.targetIn(t); p != nil {
		// vanilla: Entity.lookAt(target, 10, 10), which turns the body
		g.m.Yaw = turnToward(g.m.Yaw, float32(yawToward(p.X-g.m.X, p.Z-g.m.Z)), 10)
	}
	g.m.cube.yRot, g.m.cube.aggressive = g.m.Yaw, g.m.cubeDealsDamage()
}

// cubeRandomDirectionGoal is CubeMobRandomDirectionGoal: with no target, a
// new heading every 40 to 99 ticks.
type cubeRandomDirectionGoal struct {
	m      *Mob
	chosen float32
	next   int
}

func (g *cubeRandomDirectionGoal) Priority() int    { return 4 }
func (g *cubeRandomDirectionGoal) Flags() GoalFlags { return GoalLook }
func (g *cubeRandomDirectionGoal) CanStart(*tickView) bool {
	return g.m.TargetID == 0 && (g.m.OnGround || g.m.InWater() || g.m.InLava())
}
func (g *cubeRandomDirectionGoal) Start(*tickView)                 {}
func (g *cubeRandomDirectionGoal) ShouldContinue(t *tickView) bool { return g.CanStart(t) }
func (g *cubeRandomDirectionGoal) Stop(*tickView)                  {}
func (g *cubeRandomDirectionGoal) RequiresEveryTick() bool         { return false }
func (g *cubeRandomDirectionGoal) Tick(*tickView) {
	g.next--
	if g.next <= 0 {
		g.next = reducedTickDelay(40 + g.m.Rand.Intn(60))
		g.chosen = float32(g.m.Rand.Intn(360))
	}
	g.m.cube.yRot, g.m.cube.aggressive = g.chosen, false
}

// cubeKeepJumpingGoal is CubeMobKeepOnJumpingGoal: the wanted movement of
// one, always.
type cubeKeepJumpingGoal struct{ m *Mob }

func (g *cubeKeepJumpingGoal) Priority() int                 { return 5 }
func (g *cubeKeepJumpingGoal) Flags() GoalFlags              { return GoalJump | GoalMove }
func (g *cubeKeepJumpingGoal) CanStart(*tickView) bool       { return true }
func (g *cubeKeepJumpingGoal) Start(*tickView)               {}
func (g *cubeKeepJumpingGoal) ShouldContinue(*tickView) bool { return true }
func (g *cubeKeepJumpingGoal) Stop(*tickView)                {}
func (g *cubeKeepJumpingGoal) RequiresEveryTick() bool       { return false }
func (g *cubeKeepJumpingGoal) Tick(*tickView) {
	// Steering goals can stop movement before this lower-priority default runs.
	if !g.m.cube.wanted {
		g.m.cube.wanted, g.m.cube.speedMod = true, 1.0
	}
}

// cubeJumpDelay is AbstractCubeMob.getJumpDelay, ten to 29 ticks, four
// times that for the magma cube.
func (m *Mob) cubeJumpDelay() int {
	d := m.Rand.Intn(20) + 10
	if m.Type == TypeMagmaCube {
		d *= 4
	}
	return d
}

// cubeMoveStep is CubeMobMoveControl.tick: turn toward the heading; with a
// wanted move, on the ground hop when the countdown ends (a third as long
// while hunting) and stand still between hops, and in the air keep moving
// at the pace. It reports whether a hop started.
func (m *Mob) cubeMoveStep() bool {
	if m.SulfurCubeFilled() {
		m.walk.speed = 0
		return false
	}
	c := &m.cube
	m.Yaw = turnToward(m.Yaw, c.yRot, 90)
	m.HeadYaw = m.Yaw
	if !c.wanted {
		m.walk.speed = 0
		return false
	}
	c.wanted = false
	yaw := float64(m.Yaw) * math.Pi / 180
	m.walk.dirX, m.walk.dirZ = -math.Sin(yaw), math.Cos(yaw)
	speed := c.speedMod * m.cubePace()
	if !m.OnGround {
		m.walk.speed = speed
		return false
	}
	// vanilla: CubeMobMoveControl.tick
	waiting := c.jumpDelay > 0
	c.jumpDelay--
	if waiting {
		m.walk.speed = 0
		return false
	}
	c.jumpDelay = m.cubeJumpDelay()
	if c.aggressive {
		c.jumpDelay /= 3
	}
	m.walk.speed = speed
	m.jump = true
	return true
}

// cubeJumpBonus is the magma cube's tenth of a block a size added to the hop
// after the block's jump factor has scaled the base power.
// vanilla: MagmaCube.jumpFromGround
func (m *Mob) cubeJumpBonus() float64 {
	if m.Type == TypeMagmaCube {
		return float64(float32(m.Size) * 0.1)
	}
	return 0
}

// touchesPlayer reports whether the mob's box meets the player's grown 1
// sideways and 0.5 vertically.
// vanilla: Player.touch
func (m *Mob) touchesPlayer(p *Player) bool {
	minX, minY, minZ, maxX, maxY, maxZ := m.BlastBox()
	pw, ph, _ := p.box()
	return p.X+pw/2+1 > minX && p.X-pw/2-1 < maxX &&
		p.Z+pw/2+1 > minZ && p.Z-pw/2-1 < maxZ &&
		p.Y+ph+0.5 > minY && p.Y-0.5 < maxY
}

// cubeTouches lists the players a damaging cube hurts by touch:
// dealDamage adds melee reach and line of sight to the touch.
func (m *Mob) cubeTouches(w World, players []Player) []Player {
	if !m.cubeDealsDamage() {
		return nil
	}
	var hit []Player
	for i := range players {
		p := &players[i]
		if !p.Huntable || !m.touchesPlayer(p) {
			continue
		}
		if m.inMeleeReach(p) && m.seesTarget(w, p) {
			hit = append(hit, *p)
		}
	}
	return hit
}

// cubeHitsGolem is Slime.push on an iron golem: a damaging cube hurts the
// golem it overlaps when it sees it.
func (m *Mob) cubeHitsGolem(w World, golem *Mob, events []MobEvent) []MobEvent {
	if !m.isCube() || !m.cubeDealsDamage() || !golem.Alive() || !m.seesPoint(w, golem.X, golem.Y+golem.eyeHeight(), golem.Z) {
		return events
	}
	pitch := (m.Rand.Float32()-m.Rand.Float32())*0.2 + 1
	return append(events, MobEvent{Mob: m, Kind: MobAttackedPlayer, Damage: m.cubeAttackDamage(), Player: golem.asTarget(), Touch: true, Pitch: pitch})
}

// Split is one smaller cube a dying cube leaves.
type Split struct {
	X, Y, Z float64
	Yaw     float32
	Size    int
}

// Splits is AbstractCubeMob.remove for a dead cube above the smallest: two
// to four half-size cubes at the corners of a square half its width, half a
// block up, facing randomly.
func (m *Mob) Splits() []Split {
	if !m.isCube() || m.Size <= 1 {
		return nil
	}
	offset := float32(m.width()) / 2
	count := 2
	if m.Type != TypeSulfurCube {
		count += m.Rand.Intn(3)
	}
	splits := make([]Split, 0, count)
	for i := range count {
		dx := (float32(i%2) - 0.5) * offset
		dz := (float32(i/2) - 0.5) * offset
		splits = append(splits, Split{
			X: m.X + float64(dx), Y: m.Y + 0.5, Z: m.Z + float64(dz),
			Yaw:  m.Rand.Float32() * 360,
			Size: m.Size / 2,
		})
	}
	return splits
}

// cubeSoundPitch is AbstractCubeMob.getSoundPitch, the hop's pitch.
func (m *Mob) cubeSoundPitch(rng *rand.Rand) float32 {
	adjust := float32(0.8)
	if m.tiny() {
		adjust = 1.4
	}
	return ((rng.Float32()-rng.Float32())*0.2 + 1) * adjust
}
