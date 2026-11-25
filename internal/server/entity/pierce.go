package entity

import "github.com/golem-mc/golem/registry"

// PiercingMobPadding bounds the horizontal overhang of ordinary mob hitboxes.
const PiercingMobPadding = 16

// MayExceedPiercingPadding classifies kinds whose hitboxes need a separate search.
func (m *Mob) MayExceedPiercingPadding() bool {
	switch m.Type {
	case TypePhantom, TypeSalmon, TypePufferfish, TypeShulker:
		return true
	}
	k := m.kind()
	return k.cube || max(k.width, k.babyWidth)*registry.AttributeScale.Attribute().Max/2 > PiercingMobPadding
}

// PiercingRay is a piercing jab's look ray: from the eyes along the unit
// look vector between the reaches (blocks from the eyes), with the hitbox
// margin, and the attacker's forward speed, which lengthens the far reach
// (never shortens it).
type PiercingRay struct {
	EyeX, EyeY, EyeZ    float64
	LookX, LookY, LookZ float64
	MinReach, MaxReach  float64
	Margin              float64
	Forward             float64
}

// PiercingHits returns the indices of the boxes the jab hits. A block shape
// nearer than the minimum reach stops the jab; one beyond it shortens the
// ray. A box containing the start is hit; one reached only by the margin is
// hit when nothing solid lies between the margin entry and its centre.
// vanilla: ProjectileUtil.getHitEntitiesAlong, getManyEntityHitResult
func PiercingHits(w World, r PiercingRay, boxes []AABB) []int {
	far := r.MaxReach + max(0, r.Forward)
	fromX, fromY, fromZ := r.EyeX+r.LookX*r.MinReach, r.EyeY+r.LookY*r.MinReach, r.EyeZ+r.LookZ*r.MinReach
	toX, toY, toZ := r.EyeX+r.LookX*far, r.EyeY+r.LookY*far, r.EyeZ+r.LookZ*far
	if c := clipArrowFlight(w, r.EyeX, r.EyeY, r.EyeZ, toX-r.EyeX, toY-r.EyeY, toZ-r.EyeZ); c.ok {
		// Compared as distances from the eyes.
		if c.t*far < r.MinReach {
			return nil
		}
		toX, toY, toZ = r.EyeX+(toX-r.EyeX)*c.t, r.EyeY+(toY-r.EyeY)*c.t, r.EyeZ+(toZ-r.EyeZ)*c.t
	}
	dx, dy, dz := toX-fromX, toY-fromY, toZ-fromZ
	var hits []int
	for i, b := range boxes {
		if b.contains(fromX, fromY, fromZ) {
			hits = append(hits, i)
			continue
		}
		if _, ok := b.clip(fromX, fromY, fromZ, dx, dy, dz); ok {
			hits = append(hits, i)
			continue
		}
		if r.Margin <= 0 {
			continue
		}
		t, ok := b.Inflate(r.Margin).clip(fromX, fromY, fromZ, dx, dy, dz)
		if !ok {
			continue
		}
		// From the margin entry toward the box's centre, cut short by the first
		// solid block.
		ox, oy, oz := fromX+dx*t, fromY+dy*t, fromZ+dz*t
		cx, cy, cz := (b.minX+b.maxX)/2, (b.minY+b.maxY)/2, (b.minZ+b.maxZ)/2
		if c := clipArrowFlight(w, ox, oy, oz, cx-ox, cy-oy, cz-oz); c.ok {
			cx, cy, cz = ox+(cx-ox)*c.t, oy+(cy-oy)*c.t, oz+(cz-oz)*c.t
		}
		if _, ok := b.clip(ox, oy, oz, cx-ox, cy-oy, cz-oz); ok {
			hits = append(hits, i)
		}
	}
	return hits
}

// contains is AABB.contains: min faces inclusive, max faces exclusive.
func (b AABB) contains(x, y, z float64) bool {
	return x >= b.minX && x < b.maxX && y >= b.minY && y < b.maxY && z >= b.minZ && z < b.maxZ
}

// clip is AABB.clip: the fraction in (0, 1) of the segment at which it
// enters the box through a facing face. A segment starting inside enters
// nothing.
func (b AABB) clip(x, y, z, dx, dy, dz float64) (float64, bool) {
	const eps = 1e-7
	scale, hit := 1.0, false
	face := func(point, from, da, fromB, db, minB, maxB, fromC, dc, minC, maxC float64) {
		s := (point - from) / da
		pb, pc := fromB+s*db, fromC+s*dc
		if 0 < s && s < scale && minB-eps < pb && pb < maxB+eps && minC-eps < pc && pc < maxC+eps {
			scale, hit = s, true
		}
	}
	if dx > eps {
		face(b.minX, x, dx, y, dy, b.minY, b.maxY, z, dz, b.minZ, b.maxZ)
	} else if dx < -eps {
		face(b.maxX, x, dx, y, dy, b.minY, b.maxY, z, dz, b.minZ, b.maxZ)
	}
	if dy > eps {
		face(b.minY, y, dy, z, dz, b.minZ, b.maxZ, x, dx, b.minX, b.maxX)
	} else if dy < -eps {
		face(b.maxY, y, dy, z, dz, b.minZ, b.maxZ, x, dx, b.minX, b.maxX)
	}
	if dz > eps {
		face(b.minZ, z, dz, x, dx, b.minX, b.maxX, y, dy, b.minY, b.maxY)
	} else if dz < -eps {
		face(b.maxZ, z, dz, x, dx, b.minX, b.maxX, y, dy, b.minY, b.maxY)
	}
	return scale, hit
}
