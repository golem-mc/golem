package entity

import (
	"math"

	"github.com/golem-mc/golem/internal/server/rules"
	"github.com/golem-mc/golem/internal/server/world"
	"github.com/golem-mc/golem/registry"
)

func (m *Mob) dragonTick(w World, players []Player, others []*Mob, out []MobEvent) []MobEvent {
	m.TickTimers()
	out = m.queuedStep(out)
	if m.Health <= 0 {
		d := m.dragon
		d.deathTime++
		out = append(out, MobEvent{Mob: m, Kind: MobDragonDeath, Status: int32(d.deathTime), X: m.X, Y: m.Y, Z: m.Z})
		m.Y += float64(float32(0.1)) // vanilla: EnderDragon.tickDeath
		m.dragonParts()
		if d.deathTime >= 200 {
			m.gone = true
			out = append(out, MobEvent{Mob: m, Kind: MobVanished})
		}
		return out
	}
	out = m.ambientStep(w, players, out)
	if !m.NoAI {
		if m.dragon == nil {
			m.dragon = newDragonState()
			m.InvalidateMetadataDragon()
		}
		if !m.dragon.nodesReady {
			m.dragonNodes(w)
		}
		phase := m.DragonPhase()
		out = m.dragonPhaseTick(w, players, out)
		if phase != m.DragonPhase() {
			out = m.dragonPhaseTick(w, players, out)
		}
	}
	if m.dragon != nil && m.dragon.endFlame {
		out = append(out, MobEvent{Mob: m, Kind: MobDragonFlameEnded})
		m.dragon.endFlame = false
	}
	m.dragonStep(w)
	if m.NoAI || m.Health <= 0 {
		return out
	}
	out = m.dragonContacts(players, others, out)
	return m.dragonWalls(w, out)
}

// vanilla: EnderDragon.aiStep, knockBack, hurt
func (m *Mob) dragonContacts(players []Player, others []*Mob, out []MobEvent) []MobEvent {
	if m.HurtTime != 0 {
		return out
	}
	for _, part := range []int{6, 7, 0, 1} {
		b := m.dragon.parts[part]
		wing := part >= 6
		inflate, yInflate, yMove := 1.0, 1.0, 0.0
		if wing {
			inflate, yInflate, yMove = 4, 2, -2
		}
		b = world.Box{MinX: b.MinX - inflate, MinY: b.MinY - yInflate + yMove, MinZ: b.MinZ - inflate, MaxX: b.MaxX + inflate, MaxY: b.MaxY + yInflate + yMove, MaxZ: b.MaxZ + inflate}
		intersects := func(box world.Box) bool {
			return rules.BoxesIntersect([3]float64{b.MinX, b.MinY, b.MinZ}, [3]float64{b.MaxX, b.MaxY, b.MaxZ}, [3]float64{box.MinX, box.MinY, box.MinZ}, [3]float64{box.MaxX, box.MaxY, box.MaxZ})
		}
		contact := func(p Player) {
			damage := 10.0
			var push [3]float64
			if wing {
				body := m.dragon.parts[DragonBody]
				dx, dz := p.X-(body.MinX+body.MaxX)/2, p.Z-(body.MinZ+body.MaxZ)/2
				dd := math.Max(dx*dx+dz*dz, 0.1)
				push = [3]float64{dx / dd * 4, 0.2, dz / dd * 4}
				damage = 5
				if m.dragonIsSitting() || p.HurtByStamp >= p.Age-2 {
					damage = 0
				}
			}
			out = append(out, MobEvent{Mob: m, Kind: MobAttackedPlayer, Player: p, Damage: damage, Roar: true, DragonContact: true, ContactPush: push})
		}
		for _, p := range players {
			if p.Dead || p.Creative || p.Spectator {
				continue
			}
			pw, ph, _ := p.box()
			if intersects(world.Box{MinX: p.X - pw/2, MinY: p.Y, MinZ: p.Z - pw/2, MaxX: p.X + pw/2, MaxY: p.Y + ph, MaxZ: p.Z + pw/2}) {
				contact(p)
			}
		}
		for _, o := range others {
			if o == m || !o.Alive() {
				continue
			}
			x0, y0, z0, x1, y1, z1 := o.BlastBox()
			if !intersects(world.Box{MinX: x0, MinY: y0, MinZ: z0, MaxX: x1, MaxY: y1, MaxZ: z1}) {
				continue
			}
			p := o.asTarget()
			p.Age, p.HurtByStamp = o.Age, o.retaliateStamp
			contact(p)
		}
	}
	return out
}

var dragonImmune = registry.BlockTagOf("minecraft:dragon_immune")
var dragonTransparent = registry.BlockTagOf("minecraft:dragon_transparent")

// vanilla: EnderDragon.checkWalls
func (m *Mob) dragonWalls(w World, out []MobEvent) []MobEvent {
	m.dragon.inWall = false
	var removed map[[3]int32]bool
	for _, part := range []int{0, 1, 2} {
		b := m.dragon.parts[part]
		x0, y0, z0 := int32(math.Floor(b.MinX)), int32(math.Floor(b.MinY)), int32(math.Floor(b.MinZ))
		x1, y1, z1 := int32(math.Floor(b.MaxX)), int32(math.Floor(b.MaxY)), int32(math.Floor(b.MaxZ))
		destroyed := false
		for x := x0; x <= x1; x++ {
			for y := y0; y <= y1; y++ {
				for z := z0; z <= z1; z++ {
					state := w.BlockAt(x, y, z)
					block := registry.BlockOf(state)
					if rules.IsAir(state) || block.InTag(dragonTransparent) {
						continue
					}
					if !w.MobGriefing() || block.InTag(dragonImmune) {
						m.dragon.inWall = true
						continue
					}
					if world.RemovalLeaves(state) == state {
						continue
					}
					key := [3]int32{x, y, z}
					if removed[key] {
						continue
					}
					if removed == nil {
						removed = make(map[[3]int32]bool)
					}
					removed[key] = true
					out = append(out, MobEvent{Mob: m, Kind: MobRemovedBlock, X: float64(x), Y: float64(y), Z: float64(z)})
					destroyed = true
				}
			}
		}
		if destroyed {
			// vanilla: LevelEvent.PARTICLES_DRAGON_BLOCK_BREAK
			out = append(out, MobEvent{Mob: m, Kind: MobLevelEvent, Status: 2008, X: float64(x0 + int32(m.Rand.Intn(int(x1-x0+1)))), Y: float64(y0 + int32(m.Rand.Intn(int(y1-y0+1)))), Z: float64(z0 + int32(m.Rand.Intn(int(z1-z0+1))))})
		}
	}
	return out
}
