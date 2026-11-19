package entity

import (
	"math"

	"github.com/golem-mc/golem/internal/server/rules"
)

// vanilla: DragonSittingScanningPhase, DragonSittingAttackingPhase, DragonSittingFlamingPhase
func (m *Mob) dragonSittingTick(w World, players []Player, out []MobEvent) []MobEvent {
	d := m.dragon
	switch d.phase {
	case DragonSittingScanning:
		p := m.dragonPlayer(w, players, 0, 20, true)
		if p != nil {
			if d.phaseTicks > 25 {
				m.SetDragonPhase(DragonSittingAttacking, 0)
			} else if m.dragonAngle(p) > 10 {
				m.dragonParts()
				head := d.parts[0]
				xd, zd := p.X-(head.MinX+head.MaxX)/2, p.Z-(head.MinZ+head.MaxZ)/2
				delta := math.Max(-100, math.Min(100, wrapDegrees(180-math.Atan2(xd, zd)*180/math.Pi-float64(m.Yaw))))
				speed := math.Hypot(xd, zd) + 1
				d.turn = d.turn*0.8 + delta*0.7/math.Min(speed, 40)/speed
				m.Yaw += float32(d.turn)
			}
		} else if d.phaseTicks >= 100 {
			p = m.dragonPlayer(w, players, 0, 150, false)
			m.SetDragonPhase(DragonTakeoff, 0)
			if p != nil {
				m.SetDragonPhase(DragonCharging, p.EntityID)
			}
		}
	case DragonSittingAttacking:
		if d.phaseTicks > 40 {
			m.SetDragonPhase(DragonSittingFlaming, 0)
		}
	case DragonSittingFlaming:
		if d.phaseTicks >= 200 {
			phase := DragonSittingScanning
			if d.flameCount >= 4 {
				phase = DragonTakeoff
			}
			m.SetDragonPhase(phase, 0)
		} else if d.phaseTicks == 10 {
			m.dragonParts()
			head := d.parts[0]
			hx, hz := (head.MinX+head.MaxX)/2, (head.MinZ+head.MaxZ)/2
			dx, _, dz := dragonNormalize(hx-m.X, 0, hz-m.Z)
			x, y, z := hx+dx*2.5, (head.MinY+head.MaxY)/2, hz+dz*2.5
			initial := y
			for rules.IsAir(w.BlockAt(int32(math.Floor(x)), int32(math.Floor(y)), int32(math.Floor(z)))) {
				y--
				if y < 0 {
					y = initial
					break
				}
			}
			out = append(out, MobEvent{Mob: m, Kind: MobDragonFlame, X: x, Y: math.Floor(y) + 1, Z: z})
		}
	}
	return out
}
