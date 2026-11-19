package server

import (
	"slices"

	"github.com/golem-mc/golem/internal/server/entity"
	"github.com/golem-mc/golem/internal/server/rules"
	"github.com/golem-mc/golem/registry"
)

// villageView is a dimension's village POIs as a villager reads them. Its
// only lock is the index's own, so the mob tick may use it under mobsMu.
type villageView struct {
	dim *dimension
	// tick is the raid tick the scans age by, read before the mob lock.
	tick   int64
	claims []rules.POI
}

func (v *villageView) Dimension() string { return v.dim.name }

// POIs lists the points of interest of the kinds within radius blocks,
// closest first.
func (v *villageView) POIs(match func(rules.POIKind) bool, x, y, z int32, radius int) []entity.Waypoint {
	type hit struct {
		p    entity.Waypoint
		dist int64
	}
	var hits []hit
	for _, p := range v.dim.villages.Around(int(x), int(y), int(z), radius, v.tick, rules.VillageScanTicks) {
		if match(p.Kind) {
			dx, dy, dz := int64(p.X)-int64(x), int64(p.Y)-int64(y), int64(p.Z)-int64(z)
			hits = append(hits, hit{entity.Waypoint{X: int32(p.X), Y: int32(p.Y), Z: int32(p.Z)}, dx*dx + dy*dy + dz*dz})
		}
	}
	slices.SortStableFunc(hits, func(a, b hit) int { return int(a.dist - b.dist) })
	out := make([]entity.Waypoint, len(hits))
	for i, h := range hits {
		out[i] = h.p
	}
	return out
}

// Claimed is PoiManager.Occupancy.IS_OCCUPIED for the POI at the block.
func (v *villageView) Claimed(x, y, z int32) bool {
	return v.dim.villages.Claimed(int(x), int(y), int(z))
}

// SectionsToVillage is ServerLevel.sectionsToVillage, capped at limit+1.
func (v *villageView) SectionsToVillage(x, y, z int32, limit int) int {
	return v.dim.villages.SectionsToVillage(int(x), int(y), int(z), limit)
}

// bedsTag is #minecraft:beds, which villagers_can_sleep_on_bed,
// villager_babies_can_jump_on_bed and the cat tags take; the straw bed is
// not in it.
var bedsTag = registry.BlockTagOf("minecraft:beds")

// IsBed reports a bed villagers use: one in #minecraft:beds.
func (v *villageView) IsBed(x, y, z int32) bool {
	return registry.BlockOf(v.dim.w.BlockAt(x, y, z)).InTag(bedsTag)
}

// BedStandUp is BedBlock.findStandUpPosition for a bed head.
func (v *villageView) BedStandUp(x, y, z int32, yaw float32) (sx, sy, sz float64) {
	w := v.dim.w
	sx, sy, sz, _ = standUpPosition(w, x, y, z, registry.StateValue(w.BlockAt(x, y, z), registry.PropFacing), yaw)
	return sx, sy, sz
}

// updateOccupied snapshots tickets once per herd tick, before fox queries.
func (v *villageView) updateOccupied(herd []*entity.Mob) {
	v.claims = v.claims[:0]
	for _, m := range herd {
		for p := range m.VillageClaims(v.dim.name) {
			v.claims = append(v.claims, rules.POI{X: int(p.X), Y: int(p.Y), Z: int(p.Z)})
		}
	}
	v.dim.villages.SetOccupied(v.claims)
}
