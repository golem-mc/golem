package entity

import (
	"github.com/golem-mc/golem/internal/server/rules"
	"github.com/golem-mc/golem/registry"
)

// contactSet is a bit set of the contact kinds a block can have.
type contactSet uint16

const (
	contactFire contactSet = 1 << iota // fire, soul fire, the campfires tag
	contactCactus
	contactLavaCauldron
	contactPowderSnow
	contactLayeredCauldron // water cauldron, powder snow cauldron
	contactWitherRose
	contactEyeblossom // open eyeblossom
	contactCrop       // rules.IsTrampledCrop's blocks
	contactBubbleColumn
	contactBerryBush
)

// contactOf is, per block, the contact kinds any of its states has. It is
// built from the rules predicates, so a block with one matching state counts.
var contactOf = func() *[registry.BlockCount]contactSet {
	t := new([registry.BlockCount]contactSet)
	for s := range uint16(registry.StateCount) {
		var set contactSet
		if _, lit := rules.CampfireDamage(s); lit || rules.IsFire(s) {
			set |= contactFire
		}
		for _, c := range []struct {
			bit  contactSet
			test func(uint16) bool
		}{
			{contactCactus, rules.IsCactus},
			{contactBerryBush, rules.IsSweetBerryBush},
			{contactLavaCauldron, rules.IsLavaCauldron},
			{contactPowderSnow, rules.IsPowderSnow},
			{contactLayeredCauldron, rules.IsLayeredCauldron},
			{contactWitherRose, rules.IsWitherRose},
			{contactCrop, rules.IsTrampledCrop},
			{contactEyeblossom, func(s uint16) bool { return registry.BlockOf(s) == registry.BlockOpenEyeblossom }},
			{contactBubbleColumn, func(s uint16) bool { _, ok := rules.BubbleColumnDrag(s); return ok }},
		} {
			if c.test(s) {
				set |= c.bit
			}
		}
		t[registry.BlockOf(s)] |= set
	}
	return t
}()

// contactScan is the contact kinds in a body's cells, valid for box.
type contactScan struct {
	box AABB
	set contactSet
	ok  bool
}

// touches reports whether a cell of the mob's box holds a block of want,
// scanning the cells on first use and again after the box changed. The mob's
// World writes no block, so only the box can invalidate the scan.
func (m *Mob) touches(w World, scan *contactScan, want contactSet) bool {
	return scan.touches(w, m.mobBox(), want)
}

func (scan *contactScan) touches(w World, box AABB, want contactSet) bool {
	if !scan.ok || box != scan.box {
		scan.box, scan.set, scan.ok = box, 0, true
		box.insideCells(func(x, y, z int32) {
			scan.set |= contactOf[registry.BlockOf(w.BlockAt(x, y, z))]
		})
	}
	return scan.set&want != 0
}
