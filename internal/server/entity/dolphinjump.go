package entity

import (
	"math"

	"github.com/golem-mc/golem/internal/server/rules"
	"github.com/golem-mc/golem/registry"
)

// vanilla: DolphinJumpGoal
type dolphinJumpGoal struct {
	m        *Mob
	breached bool
}

func (g *dolphinJumpGoal) Priority() int           { return 5 }
func (g *dolphinJumpGoal) Flags() GoalFlags        { return GoalJump }
func (g *dolphinJumpGoal) RequiresEveryTick() bool { return false }
func (g *dolphinJumpGoal) Uninterruptible() bool   { return true }
func (g *dolphinJumpGoal) CanStart(t *tickView) bool {
	m := g.m
	if m.Rand.Intn(reducedTickDelay(10)) != 0 {
		return false
	}
	dx, dz := dolphinDirection(float64(m.Yaw))
	at := m.cell()
	for _, step := range []int32{0, 1, 4, 5, 6, 7} {
		x, z := at.X+dx*step, at.Z+dz*step
		if !rules.IsWater(t.w.BlockAt(x, at.Y, z)) || blocksMotion(t.w.BlockAt(x, at.Y, z)) || !rules.IsAir(t.w.BlockAt(x, at.Y+1, z)) || !rules.IsAir(t.w.BlockAt(x, at.Y+2, z)) {
			return false
		}
	}
	return true
}
func (g *dolphinJumpGoal) ShouldContinue(*tickView) bool {
	m := g.m
	return !(m.VY*m.VY < float64(float32(0.03)) && m.Pitch != 0 && math.Abs(float64(m.Pitch)) < 10 && m.InWater() || m.OnGround)
}
func (g *dolphinJumpGoal) Start(*tickView) {
	m := g.m
	dx, dz := dolphinDirection(float64(m.Yaw))
	m.VX += float64(dx) * 0.6
	m.VY += 0.7
	m.VZ += float64(dz) * 0.6
	m.stopSeeking()
}
func (g *dolphinJumpGoal) Stop(*tickView) { g.m.Pitch = 0 }
func (g *dolphinJumpGoal) Tick(t *tickView) {
	m := g.m
	was := g.breached
	if !was {
		at := m.cell()
		g.breached = rules.IsWater(t.w.BlockAt(at.X, at.Y, at.Z))
	}
	if g.breached && !was {
		t.events = append(t.events, MobEvent{Mob: m, Kind: MobSounded, SoundID: registry.SoundEntityDolphinJump, Volume: 1, Pitch: 1})
	}
	if m.VY*m.VY < float64(float32(0.03)) && m.Pitch != 0 {
		m.Pitch += 0.2 * float32(wrapDegrees(-float64(m.Pitch)))
	} else if m.VX*m.VX+m.VY*m.VY+m.VZ*m.VZ > 1e-10 {
		m.Pitch = float32(math.Atan2(-m.VY, math.Hypot(m.VX, m.VZ)) * 57.2957763671875)
	}
}

// vanilla: Direction.fromYRot
func dolphinDirection(yaw float64) (int32, int32) {
	switch int(math.Floor(yaw/90+0.5)) & 3 {
	case 0:
		return 0, 1
	case 1:
		return -1, 0
	case 2:
		return 0, -1
	default:
		return 1, 0
	}
}
