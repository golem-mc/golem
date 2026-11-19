package entity

import (
	"math"
	"slices"

	"github.com/golem-mc/golem/internal/server/rules"
	"github.com/golem-mc/golem/registry"
)

// nautilusChargeGoal is NautilusAi's FIGHT activity and ChargeAttack.
type nautilusChargeGoal struct {
	m                   *Mob
	target              int32
	x, y, z, vx, vy, vz float64
	ticks               int
}

// vanilla: NautilusAi and ZombieNautilusAi.initFightActivity
func (g *nautilusChargeGoal) speed() float64 {
	if g.m.Type == TypeZombieNautilus {
		return 0.5
	}
	return 0.6
}

func (g *nautilusChargeGoal) Priority() int    { return 3 }
func (g *nautilusChargeGoal) Flags() GoalFlags { return GoalMove | GoalLook }
func (g *nautilusChargeGoal) CanStart(t *tickView) bool {
	m, n := g.m, &g.m.nautilus
	if m.Tamable.Tame || m.IsBaby() || !m.InWater() || m.IsInLove() || n.chargeCooldown > 0 {
		return false
	}
	g.target = 0
	if n.angerUntil > m.Age {
		if p := m.targetByID(t, n.angryAt); g.valid(t, p) && rules.IsWater(t.w.BlockAt(int32(math.Floor(p.X)), int32(math.Floor(p.Y)), int32(math.Floor(p.Z)))) {
			g.target = p.EntityID
		}
	}
	if g.target == 0 && n.attackCooldown == 0 {
		n.attackCooldown = 2400 + m.Rand.Intn(1201)
		if m.Rand.Float32() < 0.5 {
			return false
		}
		best := m.followRange() * m.followRange()
		for _, o := range t.others {
			if !o.Type.InTag(registry.EntityTypeTagIDNautilusHostiles) || !o.InWater() {
				continue
			}
			p := o.asTarget()
			if d := Dist2(m.X, m.Y, m.Z, p.X, p.Y, p.Z); d < best && g.valid(t, &p) && m.seesTarget(t.w, &p) {
				best, g.target = d, o.ID
			}
		}
	}
	return g.target != 0
}
func (g *nautilusChargeGoal) valid(t *tickView, p *Player) bool {
	return p != nil && (g.m.Team == "" || g.m.Team != p.Team) && t.w.InBorderBox(p.X-0.3, p.Z-0.3, p.X+0.3, p.Z+0.3) && !p.Dead && !p.Spectator && !p.Creative && (t.mobByID(p.EntityID) != nil && p.Huntable || t.canAttackPlayer(p))
}
func (g *nautilusChargeGoal) Start(t *tickView) {
	m := g.m
	m.SetMetadataTargetID(g.target)
	m.stopSeeking()
	g.x, g.y, g.z, g.ticks = m.X, m.Y, m.Z, 0
	p := m.targetByID(t, g.target)
	if p == nil {
		return
	}
	dx, dy, dz := p.X-m.X, p.Y-m.Y, p.Z-m.Z
	length := math.Sqrt(dx*dx + dy*dy + dz*dz)
	if length > 0 {
		g.vx, g.vy, g.vz = dx/length*g.speed(), dy/length*g.speed(), dz/length*g.speed()
	}
	if g.ShouldContinue(t) {
		sound := registry.SoundEntityNautilusDash
		if m.Type == TypeZombieNautilus {
			sound = registry.SoundEntityZombieNautilusDash
		}
		t.events = append(t.events, MobEvent{Mob: m, Kind: MobSounded, SoundID: sound, Volume: 1, Pitch: m.VoicePitch()})
	}
}
func (g *nautilusChargeGoal) ShouldContinue(t *tickView) bool {
	m := g.m
	p := m.targetByID(t, g.target)
	return !m.Tamable.Tame && !m.IsInLove() && m.nautilus.chargeCooldown == 0 && g.ticks < 60 && g.valid(t, p) && Dist2(m.X, m.Y, m.Z, g.x, g.y, g.z) < 144 && Dist2(m.X, m.Y, m.Z, p.X, p.Y, p.Z) < 121 && m.seesTarget(t.w, p)
}
func (g *nautilusChargeGoal) Tick(t *tickView) {
	m := g.m
	g.ticks++
	m.VX, m.VY, m.VZ = g.vx, g.vy, g.vz
	if p := m.targetByID(t, g.target); p != nil {
		m.Yaw = float32(yawToward(p.X-m.X, p.Z-m.Z))
		m.HeadYaw = m.Yaw
		m.Pitch = float32(pitchToward(p.X-m.X, p.Y-m.Y, p.Z-m.Z))
	}
	hit := func(p Player, width, height float64) bool {
		if math.Abs(p.X-m.X) >= (width+m.width())/2 || math.Abs(p.Z-m.Z) >= (width+m.width())/2 || p.Y >= m.Y+m.height() || p.Y+height <= m.Y || p.EntityID == m.ID || slices.Contains(m.PassengerIDs(), p.EntityID) || !g.valid(t, &p) {
			return false
		}
		force := (math.Max(0.2, math.Min(2, g.speed()*m.pace())) + 0.25*float64(m.effectLevel(mobEffectSpeed)-m.effectLevel(mobEffectSlowness))) * 2
		t.events = append(t.events, MobEvent{Mob: m, Kind: MobAttackedPlayer, Player: p, Damage: m.attackDamage(), Ram: true, RamYaw: float64(m.Yaw), RamPush: force})
		m.VX, m.VZ = m.VX*0.6, m.VZ*0.6
		m.nautilus.chargeCooldown = 80
		return true
	}
	for _, p := range t.players {
		w, h, _ := p.box()
		if hit(p, w, h) {
			return
		}
	}
	for _, o := range t.others {
		if hit(o.asTarget(), o.width(), o.height()) {
			return
		}
	}
}
func (g *nautilusChargeGoal) Stop(*tickView) {
	g.m.nautilus.chargeCooldown = 80
	g.m.SetMetadataTargetID(0)
}
func (g *nautilusChargeGoal) RequiresEveryTick() bool { return true }
