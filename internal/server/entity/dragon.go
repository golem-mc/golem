package entity

import (
	"math"
	"slices"

	"github.com/golem-mc/golem/internal/server/rules"
	"github.com/golem-mc/golem/internal/server/world"
	"github.com/golem-mc/golem/nbt"
	"github.com/golem-mc/golem/registry"
)

// TypeEnderDragon is the multipart End boss.
const TypeEnderDragon = registry.EntityTypeEnderDragon

var dragonKind = &kind{name: "Ender Dragon", width: 16, height: 8, maxHealth: 200, fireImmune: true,
	volume: 5.0, // vanilla: EnderDragon.getSoundVolume
	sounds: MobSounds{Ambient: registry.SoundEntityEnderDragonAmbient, Hurt: registry.SoundEntityEnderDragonHurt, Death: registry.SoundEntityGenericDeath}}

type dragonState struct {
	phase, deathTime                 int
	phaseTicks, flameCount, charge   int
	attackID                         int32
	pathStarted, endFlame            bool
	sittingDamage                    float64
	turn                             float64
	target                           [3]float64
	hasTarget, clockwise, nodesReady bool
	nodes                            [24][3]float64
	path                             []int
	pathEnd                          [3]float64
	history                          [64][2]float64
	historyIndex                     int
	historyReady                     bool
	parts                            [8]world.Box
	crystal                          int32
	crystals                         int
	inWall                           bool
	// headAimed says the next hurt lands on the head or neck, which take a blow whole. vanilla: EnderDragon.hurt
	headAimed bool
}

// newDragonState is a fresh dragon's state, which starts hovering.
// vanilla: EnderDragonPhaseManager.<init>
func newDragonState() *dragonState {
	d := &dragonState{phase: DragonHovering}
	// Parts stay where they were constructed, at the origin, until the first
	// tickPart; a NoAI dragon never ticks them.
	for i, size := range [8][2]float64{{1, 1}, {3, 3}, {5, 3}, {2, 2}, {2, 2}, {2, 2}, {4, 2}, {4, 2}} {
		d.parts[i] = world.Box{MinX: -size[0] / 2, MaxX: size[0] / 2, MaxY: size[1], MinZ: -size[0] / 2, MaxZ: size[0] / 2}
	}
	return d
}

// DragonPhase is the synched phase id, HOVERING (10) for a fresh dragon.
func (m *Mob) DragonPhase() int {
	if m.dragon == nil {
		return DragonHovering
	}
	return m.dragon.phase
}

func (m *Mob) dragonFields(out nbt.CompoundTag, load bool) {
	tag := out.Entries()
	if m.Type != TypeEnderDragon {
		return
	}
	if m.dragon == nil {
		m.dragon = newDragonState()
		m.InvalidateMetadataDragon()
	}
	d := m.dragon
	if load {
		if v, ok := nbt.IntField(tag, "DragonPhase"); ok {
			m.SetDragonPhase(int(v), 0)
		}
		if v, ok := nbt.IntField(tag, "DragonDeathTime"); ok {
			d.deathTime = int(v)
		}
		if v, ok := nbt.FloatField(tag, "sitting_damage_received"); ok {
			d.sittingDamage = float64(v)
		}
	} else {
		out.Set("DragonPhase", nbt.Int(d.phase))
		out.Set("DragonDeathTime", nbt.Int(d.deathTime))
		out.Set("sitting_damage_received", nbt.Float(d.sittingDamage))
	}
}

// DragonHead, DragonNeck and DragonBody are the part indexes a hurt tells
// apart: the head and neck take a blow whole, every other part as the body does.
const (
	DragonHead = 0
	DragonNeck = 1
	DragonBody = 2
)

// Phase ids follow EnderDragonPhase's registration order.
const (
	DragonHolding = iota
	DragonStrafe
	DragonLandingApproach
	DragonLanding
	DragonTakeoff
	DragonSittingFlaming
	DragonSittingScanning
	DragonSittingAttacking
	DragonCharging
	dragonDying
	DragonHovering
)

// DragonSitting reports AbstractDragonSittingPhase.isSitting.
func (m *Mob) DragonSitting() bool {
	return m.dragon != nil && m.dragon.phase >= DragonSittingFlaming && m.dragon.phase <= DragonSittingAttacking
}

// DragonDying reports the flight after the killing blow, before tickDeath.
func (m *Mob) DragonDying() bool {
	return m.dragon != nil && m.dragon.phase == dragonDying
}

// SetDragonPhase begins a phase, optionally targeting a living entity id.
func (m *Mob) SetDragonPhase(phase int, target int32) {
	if m.dragon == nil {
		m.dragon = newDragonState()
		m.InvalidateMetadataDragon()
	}
	if phase < 0 || phase > DragonHovering {
		phase = DragonHolding
	}
	d := m.dragon
	if d.phase == phase {
		if target != 0 {
			d.attackID = target
		}
		return
	}
	if d.phase == DragonSittingFlaming {
		d.endFlame = true
	}
	m.SetMetadataDragonPhase(phase)
	d.attackID, d.phaseTicks, d.charge = target, 0, 0
	d.hasTarget, d.pathStarted, d.path = false, false, nil
	if phase == DragonSittingFlaming {
		d.flameCount++
	}
}

// dragonIsSitting reports DragonPhaseInstance.isSitting, which hovering also answers.
func (m *Mob) dragonIsSitting() bool {
	return m.DragonSitting() || m.dragon != nil && m.dragon.phase == DragonHovering
}

func (m *Mob) dragonSittingDamage(before float64) {
	if !m.dragonIsSitting() {
		return
	}
	m.dragon.sittingDamage += before - m.Health
	if m.dragon.sittingDamage > m.MaxHealth*0.25 {
		m.dragon.sittingDamage = 0
		m.SetDragonPhase(DragonTakeoff, 0)
	}
}

// AimDragonPart names the part the hurts that follow land on, until it is
// aimed again. A body that is no dragon ignores it.
func (m *Mob) AimDragonPart(part int) {
	if m.dragon != nil {
		m.dragon.headAimed = part == DragonHead || part == DragonNeck
	}
}

// dragonHurt reduces the part's damage before the living hurt window.
// vanilla: EnderDragon.hurt, reallyHurt
func (m *Mob) dragonHurt(amount float64, damageType registry.DamageType, byPlayer bool) float64 {
	if m.dragon.phase == dragonDying {
		return 0
	}
	if !m.dragon.headAimed {
		amount = amount/4 + math.Min(amount, 1)
	}
	if amount < 0.01 || !byPlayer && !damageType.InTag(registry.DamageTypeTagIDAlwaysHurtsEnderDragons) {
		return 0
	}
	return amount
}

// DragonPartBox returns a part's box as last positioned by the dragon's tick; clients address parts as parent id + 1..8.
func (m *Mob) DragonPartBox(part int) world.Box {
	if m.dragon == nil {
		m.dragon = newDragonState()
		m.InvalidateMetadataDragon()
	}
	return m.dragon.parts[part]
}

// DragonCrystals checks the last healing crystal before occasionally choosing
// the nearest crystal in the dragon's box inflated by 32.
// vanilla: EnderDragon.checkCrystals
func (m *Mob) DragonCrystals(crystals []EndCrystal) {
	if m.dragon == nil {
		m.dragon = newDragonState()
		m.InvalidateMetadataDragon()
	}
	d := m.dragon
	found := false
	for _, c := range crystals {
		if c.ID == d.crystal {
			found = true
			break
		}
	}
	if !found {
		d.crystal = 0
	} else if m.Age%10 == 0 && m.Health > 0 && m.Health < m.MaxHealth {
		m.Heal(1)
	}
	if m.Rand.Intn(10) != 0 {
		return
	}
	d.crystal = 0
	nearest := math.Inf(1)
	for _, c := range crystals {
		if c.X+1 < m.X-40 || c.X-1 > m.X+40 || c.Y+2 < m.Y-32 || c.Y > m.Y+40 || c.Z+1 < m.Z-40 || c.Z-1 > m.Z+40 {
			continue
		}
		dist := sq(c.X-m.X) + sq(c.Y-m.Y) + sq(c.Z-m.Z)
		if dist < nearest {
			nearest, d.crystal = dist, c.ID
		}
	}
}

// DragonCrystalDestroyed hurts the head when its healing crystal, at (x, z),
// explodes.
// vanilla: EnderDragon.onCrystalDestroyed
func (m *Mob) DragonCrystalDestroyed(id int32, x, z float64, source *Player) (landed, softened bool) {
	if m.dragon != nil && m.dragon.phase == DragonHolding && source != nil && !source.Creative && !source.Spectator && !source.Dead {
		m.SetDragonPhase(DragonStrafe, source.EntityID)
	}
	if m.dragon != nil && m.dragon.crystal == id {
		m.AimDragonPart(DragonHead)
		var id int32
		damageType := rules.DamageTypeExplosion
		if source != nil {
			id = source.EntityID
			damageType = rules.DamageTypePlayerExplosion
		}
		landed, softened = m.hurtByDamage(10, damageType, id, source != nil, source != nil && source.Creative, x-m.X, z-m.Z)
		m.AimDragonPart(DragonBody)
	}
	return landed, softened
}

func (m *Mob) dragonStep(w World) {
	if m.dragon == nil {
		m.dragon = newDragonState()
		m.InvalidateMetadataDragon()
	}
	d := m.dragon
	m.Age++
	// vanilla: LivingEntity.baseTick
	if m.InvulnTicks > 0 {
		m.InvulnTicks--
	}
	if m.NoAI {
		return
	}
	if !d.historyReady {
		for i := range d.history {
			d.history[i] = [2]float64{m.Y, float64(m.Yaw)}
		}
		d.historyReady = true
	}
	d.historyIndex = (d.historyIndex + 1) & 63
	d.history[d.historyIndex] = [2]float64{m.Y, float64(m.Yaw)}
	if d.phase == dragonDying {
		if !d.hasTarget {
			y := float64(w.MotionBlockingHeight(0, 0) + 1)
			d.target, d.hasTarget = [3]float64{0.5, y, 0.5}, true
		}
		distance := sq(d.target[0]-m.X) + sq(d.target[1]-m.Y) + sq(d.target[2]-m.Z)
		if distance < 100 || distance > 22500 || m.horizontalCollision {
			m.SetMetadataHealth(0)
			m.dragonParts()
			return
		}
		m.SetMetadataHealth(1)
	} else if d.phase != DragonHolding && !d.hasTarget {
		m.dragonParts()
		return
	}
	if d.phase == 0 && !d.nodesReady {
		m.dragonNodes(w)
	}
	dist := sq(d.target[0]-m.X) + sq(d.target[1]-m.Y) + sq(d.target[2]-m.Z)
	// vanilla's collision terms are always false: the dragon has noPhysics
	if d.phase == DragonHolding && (!d.hasTarget || dist < 100 || dist > 22500) {
		m.dragonTarget()
	}
	if d.hasTarget {
		// vanilla: EnderDragon.aiStep, server flight branch
		xd, yd, zd := d.target[0]-m.X, d.target[1]-m.Y, d.target[2]-m.Z
		distance := xd*xd + yd*yd + zd*zd
		if horizontal := math.Hypot(xd, zd); horizontal > 0 {
			limit := 0.6
			switch d.phase {
			case DragonLanding:
				limit = 1.5
			case DragonCharging, dragonDying:
				limit = 3
			case DragonHovering:
				limit = 1
			}
			yd = math.Max(-limit, math.Min(limit, yd/horizontal))
		}
		m.VY += yd * 0.01
		m.Yaw = float32(wrapDegrees(float64(m.Yaw)))
		ax, ay, az := dragonNormalize(xd, d.target[1]-m.Y, zd)
		dx, dy, dz := dragonNormalize(math.Sin(float64(m.Yaw)*math.Pi/180), m.VY, -math.Cos(float64(m.Yaw)*math.Pi/180))
		dot := math.Max((dx*ax+dy*ay+dz*az+0.5)/1.5, 0)
		if math.Abs(xd) > 1e-5 || math.Abs(zd) > 1e-5 {
			delta := math.Max(-50, math.Min(50, wrapDegrees(180-math.Atan2(xd, zd)*180/math.Pi-float64(m.Yaw))))
			rotSpeed := math.Hypot(m.VX, m.VZ) + 1
			turnSpeed := 0.7 / math.Min(rotSpeed, 40) / rotSpeed
			if d.phase == DragonLanding {
				turnSpeed = math.Min(rotSpeed, 40) / rotSpeed
			}
			d.turn = d.turn*0.8 + delta*turnSpeed
			m.Yaw += float32(d.turn * 0.1)
		}
		span := 2 / (distance + 1)
		speed := 0.06 * (dot*span + (1 - span))
		m.VX += math.Sin(float64(m.Yaw)*math.Pi/180) * speed
		m.VZ -= math.Cos(float64(m.Yaw)*math.Pi/180) * speed
		move := 1.0
		if d.inWall {
			move = 0.8
		} // vanilla: EnderDragon.aiStep
		m.X += m.VX * move
		m.Y += m.VY * move
		m.Z += m.VZ * move
		vx, vy, vz := dragonNormalize(m.VX, m.VY, m.VZ)
		drag := 0.8 + 0.15*(vx*dx+vy*dy+vz*dz+1)/2
		m.VX *= drag
		m.VZ *= drag
		m.VY *= 0.91
		m.HeadYaw = m.Yaw
	}
	m.dragonParts()
}

func dragonNormalize(x, y, z float64) (float64, float64, float64) {
	n := math.Sqrt(x*x + y*y + z*z)
	if n < 1e-4 {
		return 0, 0, 0
	}
	return x / n, y / n, z / n
}

// vanilla: EnderDragon.aiStep tickPart offsets and DragonFlightHistory
func (m *Mob) dragonParts() {
	if m.dragon == nil {
		m.dragon = newDragonState()
		m.InvalidateMetadataDragon()
	}
	d := m.dragon
	sample := func(i int) [2]float64 {
		if !d.historyReady {
			return [2]float64{m.Y, float64(m.Yaw)}
		}
		return d.history[(d.historyIndex-i)&63]
	}
	p5, p10, p0 := sample(5), sample(10), sample(0)
	tilt := (p5[0] - p10[0]) * 10 * math.Pi / 180
	ct, st := math.Cos(tilt), math.Sin(tilt)
	s, c := math.Sin(float64(m.Yaw)*math.Pi/180), math.Cos(float64(m.Yaw)*math.Pi/180)
	set := func(i int, x, y, z, width, height float64) {
		d.parts[i] = world.Box{MinX: m.X + x - width/2, MinY: m.Y + y, MinZ: m.Z + z - width/2, MaxX: m.X + x + width/2, MaxY: m.Y + y + height, MaxZ: m.Z + z + width/2}
	}
	hs, hc := math.Sin(float64(m.Yaw)*math.Pi/180-d.turn*0.01), math.Cos(float64(m.Yaw)*math.Pi/180-d.turn*0.01)
	// vanilla: EnderDragon.getHeadYOffset
	yOff := p5[0] - p0[0]
	if m.dragonIsSitting() {
		yOff = -1
	}
	set(0, hs*6.5*ct, yOff+st*6.5, -hc*6.5*ct, 1, 1)
	set(1, hs*5.5*ct, yOff+st*5.5, -hc*5.5*ct, 3, 3)
	set(2, s*0.5, 0, -c*0.5, 5, 3)
	for i := range 3 {
		p := sample(12 + i*2)
		rot := (float64(m.Yaw) + wrapDegrees(p[1]-p5[1])) * math.Pi / 180
		dd := float64(i+1) * 2
		set(3+i, -(s*1.5+math.Sin(rot)*dd)*ct, p[0]-p5[0]-(dd+1.5)*st+1.5, (c*1.5+math.Cos(rot)*dd)*ct, 2, 2)
	}
	set(6, c*4.5, 2, s*4.5, 4, 2)
	set(7, -c*4.5, 2, -s*4.5, 4, 2)
}

// vanilla: EnderDragon.findClosestNode
func (m *Mob) dragonNodes(w World) {
	d := m.dragon
	surface, ok := w.(interface{ SurfaceHeight(int32, int32) int32 })
	for i := range d.nodes {
		radius, angle, lift := 60.0, float64(i)*math.Pi/6, 5.0
		if i >= 20 {
			radius, angle = 20, float64(i-20)*math.Pi/2
		} else if i >= 12 {
			radius, angle, lift = 40, float64(i-12)*math.Pi/4, 15
		}
		x, z := math.Floor(radius*math.Cos(angle)), math.Floor(radius*math.Sin(angle))
		y := 73.0
		if ok {
			y = math.Max(y, float64(surface.SurfaceHeight(int32(x), int32(z))+1)+lift)
		}
		d.nodes[i] = [3]float64{x, y, z}
	}
	d.nodesReady = true
}

// vanilla: EnderDragon.nodeAdjacency
var dragonAdjacency = [24]uint32{6146, 8197, 8202, 16404, 32808, 32848, 65696, 131392, 131712, 263424, 526848, 525313, 1581057, 3166214, 2138120, 6373424, 4358208, 12910976, 9044480, 9706496, 15216640, 0xD0E000, 11763712, 0x7E0000}

// SetDragonCrystalCount supplies the fight's most recent spike crystal census.
func (m *Mob) SetDragonCrystalCount(count int) { m.dragon.crystals = count }

func (m *Mob) dragonClosestNode() int {
	d := m.dragon
	from, closest := 0, 10000.0
	for i := m.dragonMinimumNode(); i < len(d.nodes); i++ {
		n := d.nodes[i]
		dist := sq(n[0]-math.Floor(m.X)) + sq(n[1]-math.Floor(m.Y)) + sq(n[2]-math.Floor(m.Z))
		if dist < closest {
			from, closest = i, dist
		}
	}
	return from
}

func (m *Mob) dragonMinimumNode() int {
	if m.dragon.crystals == 0 {
		return 12
	}
	return 0
}

// dragonTarget follows HoldingPattern's node graph.
func (m *Mob) dragonTarget() {
	d := m.dragon
	if len(d.path) == 0 {
		from := m.dragonClosestNode()
		to := from
		if m.Rand.Intn(8) == 0 {
			d.clockwise = !d.clockwise
			to += 6
		}
		if d.clockwise {
			to++
		} else {
			to--
		}
		if d.phase == DragonStrafe && d.crystals <= 0 {
			to = ((to - 12) & 7) + 12
		} else {
			to = (to%12 + 12) % 12
		}
		d.pathStarted = true
		d.path = dragonPath(d.nodes, from, to, m.dragonMinimumNode())
		if len(d.path) > 0 {
			d.path = d.path[1:]
		}
	}
	m.dragonNextNode()
}

// dragonPath is A* over the fixed 24-node flight graph.
func dragonPath(nodes [24][3]float64, from, to, minimum int) []int {
	distance := func(a, b int) float64 {
		return math.Sqrt(sq(nodes[a][0]-nodes[b][0]) + sq(nodes[a][1]-nodes[b][1]) + sq(nodes[a][2]-nodes[b][2]))
	}
	var cost [24]float64
	var parent [24]int
	var closed [24]bool
	for i := range cost {
		cost[i] = math.Inf(1)
		parent[i] = -1
	}
	cost[from] = 0
	closest := from
	reconstruct := func(to int) []int {
		path := []int{to}
		for parent[to] >= 0 {
			to = parent[to]
			path = append(path, to)
		}
		slices.Reverse(path)
		return path
	}
	for {
		next, best := -1, math.Inf(1)
		for i := range cost {
			if f := cost[i] + distance(i, to); !closed[i] && f < best {
				next, best = i, f
			}
		}
		if next < 0 {
			if closest == from {
				return nil
			}
			return reconstruct(closest)
		}
		if next == to {
			return reconstruct(to)
		}
		if distance(next, to) < distance(closest, to) {
			closest = next
		}
		closed[next] = true
		for i := minimum; i < len(cost); i++ {
			if !closed[i] && dragonAdjacency[next]&(1<<i) != 0 {
				if c := cost[next] + distance(next, i); c < cost[i] {
					cost[i], parent[i] = c, next
				}
			}
		}
	}
}

// dragonAccepts is EnderDragon.hurt's result, independent of reallyHurt's
// window and immunity result; projectiles consume an accepted hit.
func (m *Mob) dragonAccepts(amount float64) bool {
	if m.dragon == nil || m.Dead || m.dragon.phase == dragonDying {
		return false
	}
	if !m.dragon.headAimed {
		amount = amount/4 + math.Min(amount, 1)
	}
	return amount >= 0.01
}

func dragonHurtResult(accepted bool, landed, softened *bool) {
	if accepted && !*landed {
		*landed, *softened = true, true
	}
}
