package entity

import "math"

// vanilla: DragonHoldingPatternPhase, DragonStrafePlayerPhase
func (m *Mob) dragonPhaseTick(w World, players []Player, out []MobEvent) []MobEvent {
	d := m.dragon
	d.phaseTicks++
	var p *Player
	if d.attackID != 0 {
		p = m.dragonPlayer(nil, players, d.attackID, math.Inf(1), false)
	}
	distance := Dist2(m.X, m.Y, m.Z, d.target[0], d.target[1], d.target[2])
	switch d.phase {
	case DragonHovering:
		if !d.hasTarget {
			d.target, d.hasTarget = [3]float64{m.X, m.Y, m.Z}, true
		}
	case DragonHolding:
		if d.pathStarted && len(d.path) == 0 && (distance < 100 || distance > 22500) {
			if m.Rand.Intn(d.crystals+3) == 0 {
				m.SetDragonPhase(DragonLandingApproach, 0)
				break
			}
			egg := m.dragonPodium(w)
			p = m.dragonNearest(nil, players, egg, math.Inf(1), false)
			if p != nil && (m.Rand.Intn(int(Dist2(p.X, p.Y, p.Z, egg[0], egg[1]+0.5, egg[2])/512+2)) == 0 || m.Rand.Intn(d.crystals+2) == 0) {
				m.SetDragonPhase(DragonStrafe, p.EntityID)
			}
		}
	case DragonStrafe:
		if p == nil {
			m.SetDragonPhase(DragonHolding, 0)
			break
		}
		if !d.hasTarget {
			ho := math.Min(0.4+math.Hypot(math.Floor(p.X)-m.X, math.Floor(p.Z)-m.Z)/80-1, 10)
			final := [3]float64{math.Floor(p.X), math.Floor(p.Y + ho), math.Floor(p.Z)}
			m.dragonPathTo([3]float64{p.X, p.Y, p.Z}, &final)
		}
		if len(d.path) == 0 {
			ho := math.Min(0.4+math.Hypot(p.X-m.X, p.Z-m.Z)/80-1, 10)
			d.target, d.hasTarget = [3]float64{p.X, p.Y + ho, p.Z}, true
		}
		distance = Dist2(m.X, m.Y, m.Z, d.target[0], d.target[1], d.target[2])
		if distance < 100 || distance > 22500 {
			m.dragonTarget()
		}
		if Dist2(p.X, p.Y, p.Z, m.X, m.Y, m.Z) < 4096 && m.seesTarget(w, p) {
			d.charge++
			if d.charge >= 5 && m.dragonAngle(p) < 10 {
				m.dragonParts()
				head := d.parts[0]
				vx, _, vz := lookVector(m.Yaw, m.Pitch)
				x, y, z := (head.MinX+head.MaxX)/2-vx, (head.MinY+head.MaxY)/2+0.5, (head.MinZ+head.MaxZ)/2-vz
				out = append(out, MobEvent{Mob: m, Kind: MobDragonShot, X: x, Y: y, Z: z, VX: p.X - x, VY: p.Y + p.height()/2 - y, VZ: p.Z - z})
				m.SetDragonPhase(DragonHolding, 0)
			}
		} else if d.charge > 0 {
			d.charge--
		}
	case DragonLandingApproach:
		if !d.hasTarget || distance < 100 || distance > 22500 {
			if !d.pathStarted {
				egg := m.dragonPodium(w)
				p = m.dragonNearest(nil, players, egg, math.Inf(1), false)
				target := [3]float64{40, egg[1], 0}
				if p != nil {
					x, _, z := dragonNormalize(p.X, 0, p.Z)
					target = [3]float64{-x * 40, 105, -z * 40}
				}
				m.dragonPathTo(target, &egg)
			} else {
				m.dragonNextNode()
			}
			if len(d.path) == 0 {
				m.SetDragonPhase(DragonLanding, 0)
			}
		}
	case DragonLanding:
		d.target, d.hasTarget = m.dragonPodium(w), true
		if Dist2(m.X, m.Y, m.Z, d.target[0], d.target[1], d.target[2]) < 1 {
			d.flameCount = 0
			m.SetDragonPhase(DragonSittingScanning, 0)
		}
	case DragonTakeoff:
		if !d.pathStarted {
			vx, _, vz := lookVector(m.Yaw, m.Pitch)
			m.dragonPathTo([3]float64{-vx * 40, 105, -vz * 40}, nil)
		} else {
			egg := m.dragonPodium(w)
			if Dist2(m.X, m.Y, m.Z, egg[0], egg[1]+0.5, egg[2]) >= 100 {
				m.SetDragonPhase(DragonHolding, 0)
			}
		}
	case DragonCharging:
		if !d.hasTarget {
			if p == nil {
				m.SetDragonPhase(DragonHolding, 0)
				break
			}
			d.target, d.hasTarget = [3]float64{p.X, p.Y, p.Z}, true
		}
		distance = Dist2(m.X, m.Y, m.Z, d.target[0], d.target[1], d.target[2])
		if d.charge > 0 {
			d.charge++
			if d.charge > 10 {
				m.SetDragonPhase(DragonHolding, 0)
				break
			}
		}
		if distance < 100 || distance > 22500 {
			d.charge++
		}
	case DragonSittingScanning, DragonSittingAttacking, DragonSittingFlaming:
		out = m.dragonSittingTick(w, players, out)
	}
	return out
}

func (m *Mob) dragonAngle(p *Player) float64 {
	x, _, z := dragonNormalize(p.X-m.X, 0, p.Z-m.Z)
	s, c := math.Sin(float64(m.Yaw)*math.Pi/180), -math.Cos(float64(m.Yaw)*math.Pi/180)
	return math.Acos(math.Max(-1, math.Min(1, x*s+z*c)))*180/math.Pi + 0.5
}

func (m *Mob) dragonPlayer(w World, players []Player, id int32, radius float64, band bool) *Player {
	if id == 0 {
		return m.dragonNearest(w, players, [3]float64{m.X, m.Y, m.Z}, radius, band)
	}
	for i := range players {
		p := &players[i]
		if p.EntityID == id && !p.Dead && !p.Creative && !p.Spectator {
			return p
		}
	}
	return nil
}

func (m *Mob) dragonNearest(w World, players []Player, at [3]float64, radius float64, band bool) *Player {
	var nearest *Player
	best := radius * radius
	for i := range players {
		p := &players[i]
		if p.Dead || p.Creative || p.Spectator || band && math.Abs(p.Y-m.Y) > 10 {
			continue
		}
		if distance := Dist2(p.X, p.Y, p.Z, at[0], at[1], at[2]); distance < best && (w == nil || m.seesTarget(w, p)) {
			nearest, best = p, distance
		}
	}
	return nearest
}

func (m *Mob) dragonPodium(w World) [3]float64 {
	y := 64.0
	if surface, ok := w.(interface{ SurfaceHeight(int32, int32) int32 }); ok {
		y = float64(surface.SurfaceHeight(0, 0) + 1)
	}
	return [3]float64{0.5, y, 0.5}
}

func (m *Mob) dragonPathTo(target [3]float64, final *[3]float64) {
	d := m.dragon
	to, best := m.dragonMinimumNode(), math.Inf(1)
	for i := m.dragonMinimumNode(); i < len(d.nodes); i++ {
		n := d.nodes[i]
		if distance := Dist2(n[0], n[1], n[2], target[0], target[1], target[2]); distance < best {
			to, best = i, distance
		}
	}
	d.path = dragonPath(d.nodes, m.dragonClosestNode(), to, m.dragonMinimumNode())
	if len(d.path) > 0 {
		d.path = d.path[1:]
	}
	if final != nil && len(d.path) > 0 {
		d.pathEnd = *final
		d.path = append(d.path, -1)
	}
	d.pathStarted = true
	m.dragonNextNode()
}

func (m *Mob) dragonNextNode() {
	d := m.dragon
	if len(d.path) > 0 {
		n := d.pathEnd
		if d.path[0] >= 0 {
			n = d.nodes[d.path[0]]
		}
		d.path = d.path[1:]
		d.target, d.hasTarget = [3]float64{n[0], n[1] + float64(m.Rand.Float32()*20), n[2]}, true
	}
}
