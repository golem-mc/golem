package server

import (
	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/entity"
	"github.com/golem-mc/golem/internal/server/rules"
	"github.com/golem-mc/golem/registry"
)

// standLocated is one armor stand's pack location_changed check, queued under
// the stands lock and run after it.
type standLocated struct {
	ent     *armorStandEntity
	x, y, z float64
	changed int32 // the event's Level: entity.LocatedMoved and a bit per changed piece
}

// standLocated runs the location_changed entries of a worn pack
// enchantment: its attribute modifiers go on the stand's attribute map, made
// on first use, with the watchers told, and its other entity effects run at
// the stand, a replace_disk one returned for the batch; the tick entries run
// with them. A stand has no mob effects.
// vanilla: EnchantmentHelper.runLocationChangedEffects, tickEffects
func (s *Server) standLocated(l standLocated) []mobDisk {
	ent := l.ent
	ctx := entityItemEnchContext(ent.w, l.x, l.y, l.z, int32(registry.EntityTypeArmorStand))
	ctx.World = ent.w
	s.lockTick(&s.armorStandsMu)
	st := ent.stand
	if st.Dead {
		s.unlockTick(&s.armorStandsMu)
		return nil
	}
	ctx.Rand = s.levelRng
	minX, minY, _, maxX, maxY, _ := st.BlastBox()
	ctx.Body = bodyLootEntity(ent.w, "minecraft:armor_stand", l.x, l.y, l.z, (maxX-minX)/2,
		rules.LootEntityFlags{OnGround: st.OnGround}, 0, [3]float64{})
	var apply, remove, ticked []rules.LocatedAction
	for i, p := range st.EnchEquipment() {
		changed := l.changed&(1<<i) != 0
		if changed || l.changed&entity.LocatedMoved != 0 {
			a, r := st.Located().Step(p.Slot, p.Stack, ctx, changed)
			apply, remove = append(apply, a...), append(remove, r...)
		}
		if l.changed&entity.LocatedTick != 0 {
			ticked = append(ticked, rules.LocatedEffects(p.Stack, p.Slot, true, ctx, ctx.Rand)...)
		}
	}
	var data []byte
	if len(apply)+len(remove) > 0 {
		a := st.EnsureAttributes()
		entity.ApplyLocatedMods(a, apply, remove)
		if dirty := a.TakeDirty(); len(dirty) > 0 {
			data = attributesPayload(st.ID, dirty)
		}
	}
	watchers := trackedWatchers(ent.tracked)
	s.unlockTick(&s.armorStandsMu)
	if data != nil {
		s.tellWatchers(ent.w, watchers, []outbound{{id: protocol.IDPlayUpdateAttributes, data: data}})
	}
	body := enchantTarget{uuid: ent.uuid, x: l.x, y: l.y, z: l.z, width: maxX - minX, height: maxY - minY, entityID: st.ID}
	var disks []mobDisk
	for _, a := range append(apply, ticked...) {
		e := a.Entry.Effect
		if d, ok := diskOf(ent.w, &e, a.Level, body); ok {
			disks = append(disks, d)
		}
		s.applyPackEffects(ent.w, e, a.Level, body)
	}
	return disks
}
