package entity

import (
	"math"

	"github.com/golem-mc/golem/internal/server/world"
	"github.com/golem-mc/golem/registry"
)

// sulfurPushRange is SulfurCube.PUSH_DISTANCE_THRESHOLD, (double)1.3f.
const sulfurPushRange = float64(float32(1.3))

type vec3 struct{ x, y, z float64 }

func (v vec3) sub(o vec3) vec3 { return vec3{v.x - o.x, v.y - o.y, v.z - o.z} }

// normalize is Vec3.normalize: zero under 1e-5.
func (v vec3) normalize() vec3 {
	d := math.Sqrt(v.x*v.x + v.y*v.y + v.z*v.z)
	if d < float64(float32(1.0e-5)) {
		return vec3{}
	}
	return vec3{v.x / d, v.y / d, v.z / d}
}

// rotate2 is Vec2.rotate: the table sine and cosine of a double angle.
func rotate2(x, y float32, angle float64) (float32, float32) {
	cos, sin := world.MthCos(angle), world.MthSin(angle)
	return x*cos - y*sin, y*cos + x*sin
}

// HitSource is where an attacker stood and looked, which a cube holding a
// block bends its shove by.
type HitSource struct {
	Type         MobType
	X, Y, Z, Eye float64
	Yaw, Pitch   float64
}

// PlayerHitSource is a player's, eyes at the standing height.
func PlayerHitSource(p Player) HitSource {
	return HitSource{X: p.X, Y: p.Y, Z: p.Z, Eye: p.Y + p.eyeHeight(), Yaw: p.Yaw, Pitch: p.Pitch}
}

// HitSource is where the mob stood and looked.
func (m *Mob) HitSource() HitSource {
	_, eye, _ := m.EyePosition()
	return HitSource{Type: m.Type, X: m.X, Y: m.Y, Z: m.Z, Eye: eye, Yaw: float64(m.Yaw), Pitch: float64(m.Pitch)}
}

// hitKnockback is the default shove of a hit: a cube holding a block bends it
// by the attacker's aim, any other body takes the plain one.
// vanilla: LivingEntity.dealDefaultKnockback, SulfurCube.knockback
func (m *Mob) hitKnockback(from *HitSource, xd, zd, damage float64) {
	if from != nil && m.SulfurCubeFilled() {
		m.sulfurKnockback(*from, knockbackStrength, xd, zd, damage, false)
		return
	}
	m.knockback(knockbackStrength, xd, zd)
}

// sulfurKnockback is the hit shove of a cube holding a block: the profile's
// powers bent by where the attacker looked and stood.
// vanilla: SulfurCube.knockback
func (m *Mob) sulfurKnockback(src HitSource, power, xd, zd, damage float64, fromEffect bool) {
	prof := m.sulfurProf()
	hp, vp := prof.kbHorizontal, prof.kbVertical
	origH, origV := hp, vp
	eye := vec3{src.X, src.Eye, src.Z}
	lx, ly, lz := LookVector(float32(src.Yaw), float32(src.Pitch))
	look := vec3{lx, ly, lz}.normalize()
	h := float32(m.height())
	center := vec3{m.X, m.Y + float64(h)/2, m.Z}

	// vanilla: applyHorizontalHitAngleScale
	toTarget := center.sub(eye).normalize()
	diff := float32(math.Atan2(look.x*toTarget.z-look.z*toTarget.x, look.x*toTarget.x+look.z*toTarget.z))
	ax, az := rotate2(float32(xd), float32(zd), float64(diff*1.6))

	// vanilla: applyVerticalHitAnglePowerTransfer
	half := 0.5 * float64(h)
	top := vec3{center.x, center.y + half, center.z}.sub(eye).normalize()
	bottom := vec3{center.x, center.y - half, center.z}.sub(eye).normalize()
	factor := float32(clampedMap(look.y, top.y, bottom.y))
	ratio := float32(math.Abs(float64(factor * 0.5)))
	if factor < 0 {
		ratio = -ratio
	}
	hp, vp = hp*(1-ratio), vp*(1+ratio)

	// vanilla: applyVerticalPositionAnglePowerRotation
	dy := m.Y - src.Y
	pitch := float32(math.Atan2(-dy, math.Hypot(m.X-src.X, m.Z-src.Z)))
	hp, vp = rotate2(hp, vp, float64(-pitch*0.8))
	var hRatio, vRatio float32
	if origH > 0 {
		hRatio = float32(math.Abs(float64(hp))) / origH
	}
	if origV > 0 {
		vRatio = float32(math.Abs(float64(vp))) / origV
	}
	if maxRatio := max(hRatio, vRatio); maxRatio > 1 {
		hp, vp = hp*(1/maxRatio), vp*(1/maxRatio)
	}

	mult := float32(math.Sqrt(damage))
	if fromEffect {
		mult *= float32(power) * 0.25
	}
	resist := float32(1 - m.KnockbackResistance())
	hp, vp = hp*mult*resist*0.4, vp*mult*resist
	hp = max(-128, min(128, hp))
	vp = max(-128, min(128, vp))
	kb := vec3{float64(ax), 0, float64(az)}.normalize()
	m.VX -= kb.x * float64(hp)
	m.VY += float64(vp) * 1.2
	m.VZ -= kb.z * float64(hp)
	m.Impulse = true
	m.sulfur.hitSound = true
}

// clampedMap is Mth.clampedMap into -1..1.
func clampedMap(v, from, to float64) float64 {
	t := (v - from) / (to - from)
	switch {
	case t < 0:
		return -1
	case t > 1:
		return 1
	}
	return -1 + t*2
}

// sulfurBlocked reports a hit of a type the cube is immune to while it holds
// a block.
func (m *Mob) sulfurBlocked(damageType registry.DamageType) bool {
	return m.SulfurCubeFilled() && damageType.InTag(registry.DamageTypeTagIDSulfurCubeWithBlockImmuneTo)
}

// sulfurTouches is SulfurCube.playerPush for each player whose touch box
// reaches the cube: close and level with it, the player's speed shoves the
// cube along, and a hot one burns them.
// vanilla: Player.touch, SulfurCube.playerTouch
func (m *Mob) sulfurTouches(players []Player, events []MobEvent) []MobEvent {
	if !m.SulfurCubeFilled() || !m.Alive() {
		return events
	}
	prof := m.sulfurProf()
	minX, minY, minZ, maxX, maxY, maxZ := m.BlastBox()
	for i := range players {
		p := &players[i]
		pw, ph, _ := p.box()
		if p.Dead || p.Spectator ||
			p.X+pw/2+1 <= minX || p.X-pw/2-1 >= maxX ||
			p.Z+pw/2+1 <= minZ || p.Z-pw/2-1 >= maxZ ||
			p.Y+ph+0.5 <= minY || p.Y-0.5 >= maxY {
			continue
		}
		dx, dz := m.X-p.X, m.Z-p.Z
		if math.Hypot(dx, dz) >= sulfurPushRange || p.Y > maxY || p.Y+ph <= minY {
			continue
		}
		knockback := math.Max(0, 1-m.KnockbackResistance())
		dir := vec3{dx, 0, dz}.normalize()
		scale := float64(float32(0.3))
		if p.Passenger {
			scale = float64(float32(0.16))
		}
		speed := math.Max(0, math.Min(0.5, math.Sqrt(p.KnownVX*p.KnownVX+p.KnownVY*p.KnownVY+p.KnownVZ*p.KnownVZ)*2*scale))
		vy := 0.0
		if m.OnGround {
			vy = knockback * float64(float32(0.3))
		}
		px, py, pz := dir.x*knockback*speed, vy*speed, dir.z*knockback*speed
		threshold := float64(prof.pushThreshold)
		if px*px+py*py+pz*pz > threshold*threshold && m.sulfur.pushCooldown <= 0 {
			m.sulfur.pushCooldown = int(prof.pushCooldown * 20)
			events = append(events, MobEvent{Mob: m, Kind: MobSounded, SoundID: prof.pushSound, Volume: 1,
				Pitch: (m.Rand.Float32()-m.Rand.Float32())*0.2 + 1})
		}
		m.VX, m.VY, m.VZ = m.VX+px, m.VY+py, m.VZ+pz
		m.Impulse = true
		events = m.sulfurBurnPlayer(events, *p)
	}
	return events
}

// sulfurContactAmount samples a contact damage's float provider.
func (m *Mob) sulfurContactAmount(c *registry.SulfurContact) float64 {
	if c.Min == c.Max {
		return float64(c.Min)
	}
	return float64(c.Min + m.Rand.Float32()*(c.Max-c.Min))
}

// sulfurBurnMob is SulfurCube.applyContactDamage on a mob the cube pushes.
func (m *Mob) sulfurBurnMob(events []MobEvent, other *Mob) []MobEvent {
	for _, c := range m.sulfurProf().contacts {
		if landed, soft := other.plainHurt(m.sulfurContactAmount(c), c.DamageType, false); landed {
			other.noteLastDamage(c.DamageType)
			events = append(events, MobEvent{Mob: other, Kind: MobBurned, DamageType: c.DamageType, Softened: soft})
		}
	}
	return events
}

// sulfurBurnPlayer is SulfurCube.applyContactDamage on a player, through an
// event the server answers.
func (m *Mob) sulfurBurnPlayer(events []MobEvent, p Player) []MobEvent {
	for _, c := range m.sulfurProf().contacts {
		events = append(events, MobEvent{Mob: m, Kind: MobContactHurt, Player: p, Damage: m.sulfurContactAmount(c), DamageType: c.DamageType})
	}
	return events
}
