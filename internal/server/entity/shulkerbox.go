package entity

import (
	"math"
	"slices"

	"github.com/golem-mc/golem/internal/server/player"
	"github.com/golem-mc/golem/internal/server/world"
	"github.com/golem-mc/golem/nbt"
	"github.com/golem-mc/golem/registry"
)

// shulkerPeekStep is how far the box's opening moves each tick.
// vanilla: Shulker.updatePeekAmount
const shulkerPeekStep = 0.05

// physicalPeek is how far the shell's box reaches for an opening of amount,
// 0 to 1.
// vanilla: Shulker.getPhysicalPeek
func physicalPeek(amount float32) float32 {
	return 0.5 - world.MthSin(float64((0.5+amount)*float32(math.Pi)))*0.5
}

// shulkerBox is the scaled box on the shulker's cell grown toward the opening
// by the physical peek.
// vanilla: Shulker.makeBoundingBox, getProgressAabb
func (m *Mob) shulkerBox() AABB {
	return shulkerDeltaBox(m.attrScale(), m.shulker.face^1, -1, physicalPeek(m.shulker.peekNow), m.X, m.Y, m.Z)
}

// shulkerDeltaBox is the slab between two openings along dir, a direction id:
// from -1 it is the whole box at the later opening.
// vanilla: Shulker.getProgressDeltaAabb
func shulkerDeltaBox(size float64, dir uint8, from, to float32, x, y, z float64) AABB {
	hi, lo := float64(max(from, to)), float64(min(from, to))
	box := AABB{x - size*0.5, y, z - size*0.5, x + size*0.5, y + size, z + size*0.5}
	step := faceStep[dir]
	grow := [3]float64{float64(step[0]) * hi * size, float64(step[1]) * hi * size, float64(step[2]) * hi * size}
	cut := [3]float64{float64(step[0]) * (1 + lo) * size, float64(step[1]) * (1 + lo) * size, float64(step[2]) * (1 + lo) * size}
	lows, highs := [3]*float64{&box.minX, &box.minY, &box.minZ}, [3]*float64{&box.maxX, &box.maxY, &box.maxZ}
	for i := range 3 {
		// expandTowards, then contract by the opposite of the lower reach.
		if grow[i] < 0 {
			*lows[i] += grow[i]
		} else {
			*highs[i] += grow[i]
		}
		if cut[i] > 0 {
			*lows[i] += cut[i]
		} else {
			*highs[i] += cut[i]
		}
	}
	return box
}

// stepShulkerPeek moves the box's opening a step toward the raw one and
// pushes the mobs in the way of a growing shell.
// vanilla: Shulker.updatePeekAmount, onPeekAmountChange
func (m *Mob) stepShulkerPeek(w World, t *tickView) {
	old := m.shulker.peekNow
	target := float32(m.shulker.peek) * 0.01
	if old == target {
		return
	}
	var now float32
	if old > target {
		now = min(max(old-shulkerPeekStep, target), 1)
	} else {
		now = min(max(old+shulkerPeekStep, 0), target)
	}
	m.shulker.peekNow = now
	size := m.attrScale()
	push := (physicalPeek(now) - physicalPeek(old)) * float32(size)
	if push <= 0 {
		return
	}
	dir := m.shulker.face ^ 1
	slab := shulkerDeltaBox(size, dir, physicalPeek(old), physicalPeek(now), m.X, m.Y, m.Z)
	step := faceStep[dir]
	delta := [3]float64{float64(push) * float64(step[0]), float64(push) * float64(step[1]), float64(push) * float64(step[2])}
	t.events = append(t.events, MobEvent{Mob: m, Kind: MobShulkerShoved, VX: delta[0], VY: delta[1], VZ: delta[2],
		Slab: [6]float64{slab.minX, slab.minY, slab.minZ, slab.maxX, slab.maxY, slab.maxZ}})
	// A vex holds noPhysics, so the shell does not move it.
	others := t.others
	if g := m.push; g != nil {
		// The entries are body boxes, which is what mobBox is for a non-shulker.
		g.shove = g.gatherBoxInto(g.shove[:0], slab)
		others = g.shove
		sortSnapshot(others)
	}
	for _, o := range others {
		if o == m || o.IsShulker() || o.Type == TypeVex || !o.Alive() || !slab.Crosses(o.mobBox()) {
			continue
		}
		o.move(w, delta[0], delta[1], delta[2])
		if m.push != nil {
			m.push.moved(o)
		}
	}
}

// shulkerUndyed is DATA_COLOR_ID's default, NO_COLOR.
const shulkerUndyed = 16

// ShulkerColor is DATA_COLOR_ID, the byte the client is told: a DyeColor id,
// 16 for none, or whatever a file stored.
func (m *Mob) ShulkerColor() uint8 { return m.shulker.colorOff + shulkerUndyed }

// SetShulkerColor sets DATA_COLOR_ID.
func (m *Mob) SetShulkerColor(raw uint8) { m.SetMetadataShulkerColorOff(raw - shulkerUndyed) }

// dyeColor is getColor as the byte a copy is given: the DyeColor id, 16
// for a byte past 15 and 0 (white) for a negative one, which DyeColor.byId
// clamps to the first colour.
// vanilla: Shulker.getColor, setVariant, DyeColor.byId
func (m *Mob) dyeColor() uint8 {
	switch raw := int8(m.ShulkerColor()); {
	case raw < 0:
		return 0
	case raw > 15:
		return shulkerUndyed
	default:
		return uint8(raw)
	}
}

// shulkerFields reads or writes the "AttachFace", "Peek" and "Color" bytes.
// vanilla: Shulker.readAdditionalSaveData, addAdditionalSaveData
func (m *Mob) shulkerFields(out nbt.CompoundTag, load bool) {
	tag := out.Entries()
	if !load {
		out.Set("AttachFace", nbt.Byte(int8(m.shulker.face)))
		out.Set("Peek", nbt.Byte(int8(m.shulker.peek)))
		out.Set("Color", nbt.Byte(int8(m.ShulkerColor())))
		return
	}
	nface, _ := nbt.IntValue(tag["AttachFace"])
	// vanilla: Direction.LEGACY_ID_CODEC, Direction.from3DDataValue
	face := int8(nface) % 6
	if face < 0 {
		face = -face
	}
	m.SetMetadataShulkerFace(uint8(face))
	peek, _ := nbt.ByteField(tag, "Peek")
	m.SetMetadataShulkerPeek(uint8(peek))
	n, ok := nbt.ByteField(tag, "Color")
	if !ok {
		n = shulkerUndyed
	}
	m.SetShulkerColor(uint8(n))
}

// ShulkerLidSlab is the slab an opening box's lid sweeps in one tick, as
// minX, minY, minZ, maxX, maxY, maxZ, and the move it gives what it crosses:
// the slab's size along the facing plus 0.01, along the facing. from and to
// are the lid's openings, 0 to 1.
// vanilla: ShulkerBoxBlockEntity.moveCollidedEntities
func ShulkerLidSlab(facing string, from, to float32, x, y, z int32) (slab [6]float64, delta [3]float64) {
	dir := dirIDOf(facing)
	b := shulkerDeltaBox(1, dir, from, to, float64(x)+0.5, float64(y), float64(z)+0.5)
	size := [3]float64{b.maxX - b.minX, b.maxY - b.minY, b.maxZ - b.minZ}
	for i, s := range faceStep[dir] {
		delta[i] = (size[i] + 0.01) * float64(s)
	}
	return [6]float64{b.minX, b.minY, b.minZ, b.maxX, b.maxY, b.maxZ}, delta
}

// dirIDOf is a facing name's index in faceStep, up for an unknown name.
func dirIDOf(facing string) uint8 {
	if i := slices.Index([]string{"down", "up", "north", "south", "west", "east"}, facing); i >= 0 {
		return uint8(i)
	}
	return 1
}

// syncShulkerArmor installs the shell's native armour instance and modifier.
// vanilla: Shulker.setRawPeekAmount
func (m *Mob) syncShulkerArmor(a *player.Attributes) {
	if m.shulker.armorSet {
		a.Instance(registry.AttributeArmor)
	}
	if m.shulker.covered {
		a.AddModifier(registry.AttributeArmor, player.Modifier{ID: modCovered, Amount: shulkerCoveredArmor,
			Operation: registry.AttributeAddValue, Persistent: true})
	} else {
		a.RemoveModifier(registry.AttributeArmor, modCovered)
	}
}

// shulkerAttributesTag saves the shell's native armour without waking an attribute map.
// vanilla: Shulker.setRawPeekAmount, LivingEntity.addAdditionalSaveData
func shulkerAttributesTag(m *Mob, kept nbt.Tag) nbt.List {
	armor := &attrTraits{id: attrArmor, owned: map[string]bool{modCovered: true}}
	if m.shulker.armorSet {
		base := 0.0
		armor.base = &base
	}
	if m.shulker.covered {
		armor.nonzero(modCovered, shulkerCoveredArmor, opAddValue)
	}
	return mergeTraits(kept, []*attrTraits{armor}, nil)
}

// onlyShulkerArmor reports whether an entry contains just the native shell modifier.
func onlyShulkerArmor(entry nbt.Compound) bool {
	if nbt.DoubleFieldOr(entry, "base", 0) != 0 {
		return false
	}
	mods, _ := nbt.Get[nbt.List](entry, "modifiers")
	for _, item := range mods.Items {
		mod, _ := nbt.CompoundView(item)
		id, _ := nbt.Get[nbt.String](mod, "id")
		op, _ := nbt.Get[nbt.String](mod, "operation")
		if registry.Qualify(string(id)) != modCovered || string(op) != opAddValue || nbt.DoubleFieldOr(mod, "amount", 0) != shulkerCoveredArmor {
			return false
		}
	}
	return true
}

// readShulkerArmor restores native attribute state before an edited map is built.
// vanilla: LivingEntity.readAdditionalSaveData
func (m *Mob) readShulkerArmor(tag nbt.Compound) {
	armor, instantiated := savedAttributeEntry(tag, attrArmor)
	m.shulker.armorSet = instantiated
	m.shulker.covered = false
	mods, _ := nbt.Get[nbt.List](armor, "modifiers")
	for _, item := range mods.Items {
		mod, _ := nbt.CompoundView(item)
		id, _ := nbt.Get[nbt.String](mod, "id")
		if registry.Qualify(string(id)) == modCovered {
			_, amountOK := nbt.NumberValue(mod["amount"])
			op, _ := nbt.Get[nbt.String](mod, "operation")
			_, operationOK := registry.AttributeOperationByName(string(op))
			m.shulker.covered = amountOK && operationOK
		}
	}
}
