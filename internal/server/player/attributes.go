package player

import (
	"slices"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/registry"
)

// Modifier is one attribute modifier as an attribute instance holds it.
type Modifier struct {
	ID        string
	Amount    float64
	Operation registry.AttributeOperation

	// Persistent modifiers are saved with the player; the rest are rebuilt
	// from equipment, effects and state.
	Persistent bool
}

// AttributeInstance is one attribute of a player: its base and the modifiers
// on it, in the order they went on. vanilla: AttributeInstance
type AttributeInstance struct {
	Attribute registry.Attribute
	Base      float64
	Modifiers []Modifier
}

// Value is the base with adds, then base multiples, then total multiples
// applied, clamped to the attribute's range.
// vanilla: AttributeInstance.calculateValue
func (in *AttributeInstance) Value() float64 {
	base := in.Base
	for _, m := range in.Modifiers {
		if m.Operation == registry.AttributeAddValue {
			base += m.Amount
		}
	}
	value := base
	for _, m := range in.Modifiers {
		if m.Operation == registry.AttributeAddMultipliedBase {
			value += base * m.Amount
		}
	}
	for _, m := range in.Modifiers {
		if m.Operation == registry.AttributeAddMultipliedTotal {
			value *= 1 + m.Amount
		}
	}
	return in.Attribute.Sanitize(value)
}

// EquipmentSlots names the equipment slots a player has, in vanilla's
// EquipmentSlot order; Equipment indexes its stacks the same way.
var EquipmentSlots = [...]string{"mainhand", "offhand", "feet", "legs", "chest", "head"}

// Equipment is what a player holds and wears, indexed as EquipmentSlots.
type Equipment [len(EquipmentSlots)]protocol.ItemStack

// EquipmentWindowSlots is the window slot each EquipmentSlots entry occupies,
// in the same order.
func (inv *Inventory) EquipmentWindowSlots() [len(EquipmentSlots)]int {
	return [...]int{
		HotbarSlot + int(inv.Selected),
		OffHandSlot,
		ArmorSlot + 3,
		ArmorSlot + 2,
		ArmorSlot + 1,
		ArmorSlot,
	}
}

// Equipment is what the player holds and wears now.
func (inv *Inventory) Equipment() Equipment {
	slots := inv.EquipmentWindowSlots()
	var eq Equipment
	for i := range eq {
		eq[i] = inv.Slots[slots[i]]
	}
	return eq
}

// HasEquipment reports whether any held or worn stack satisfies f, without
// copying the stacks.
func (inv *Inventory) HasEquipment(f func(protocol.ItemStack) bool) bool {
	for _, slot := range inv.EquipmentWindowSlots() {
		if f(inv.Slots[slot]) {
			return true
		}
	}
	return false
}

// playerBases is the base of every attribute for a player: the registry's
// default but where Player.createAttributes sets its own.
var playerBases = func() *[registry.NumAttributes]float64 {
	var b [registry.NumAttributes]float64
	for i, attr := range registry.Attributes {
		b[i] = attr.Default
	}
	b[registry.AttributeAttackDamage] = 1
	b[registry.AttributeMovementSpeed] = float64(float32(0.1))
	b[registry.AttributeWaypointTransmitRange] = 6e7
	b[registry.AttributeWaypointReceiveRange] = 6e7
	return &b
}()

// startingAttributes are the three instances a vanilla server sends every
// player at join or rejoin; equipment adds the rest with its modifiers.
var startingAttributes = []registry.AttributeID{
	registry.AttributeMovementSpeed,
	registry.AttributeBlockInteractionRange,
	registry.AttributeEntityInteractionRange,
}

// attributeSet is a set of attributes by ID, one bit each.
type attributeSet uint64

func (s attributeSet) has(id registry.AttributeID) bool { return s&(1<<id) != 0 }

// Attributes is an attribute map: its instances, those changed since the
// client was told, and the equipment whose modifiers are on. The zero value
// is a fresh player's, not yet introduced.
// vanilla: AttributeMap, with LivingEntity.lastEquipmentItems
type Attributes struct {
	instances [registry.NumAttributes]*AttributeInstance
	// Keep insertion history inline because pairing builds throwaway maps.
	insertionOrder [registry.NumAttributes]uint8
	instanceCount  uint8
	dirty          attributeSet
	initialized    bool
	introduced     bool
	// synthetic marks instances this server made for pairing and equipment
	// before vanilla would have; an explicit Instance clears the mark.
	synthetic attributeSet

	// supplier is the attribute set and bases of a non-player owner; nil
	// for a player. has and bases are its resolution by ID, or a player's.
	supplier *Supplier
	has      attributeSet
	bases    *[registry.NumAttributes]float64

	// Worn is the equipment the modifiers were last taken from.
	Worn Equipment
}

// Supplier is an entity kind's attributes and the bases that differ from the
// defaults.
// vanilla: AttributeSupplier
type Supplier struct {
	Names map[string]bool
	Bases map[string]float64
}

// NewAttributes is a non-player kind's map, introduced from the start so its
// changes reach clients as dirty instances.
// The supplier's names are resolved to IDs here, once per map.
func NewAttributes(s *Supplier) *Attributes {
	a := &Attributes{supplier: s}
	a.init()
	a.introduced = true
	return a
}

func (a *Attributes) init() {
	if a.initialized {
		return
	}
	a.initBases()
	if a.supplier == nil {
		a.initStartingAttributes()
	}
}

func (a *Attributes) initStartingAttributes() {
	for _, id := range startingAttributes {
		a.Instance(id)
	}
}

func (a *Attributes) initBases() {
	a.initialized = true
	if a.supplier == nil {
		a.has, a.bases = playerAttributes, playerBases
		return
	}
	bases := new([registry.NumAttributes]float64)
	for i, attr := range registry.Attributes {
		bases[i] = attr.Default
		if base, ok := a.supplier.Bases[attr.Name]; ok {
			bases[i] = base
		}
		if a.supplier.Names[attr.Name] {
			a.has |= 1 << i
		}
	}
	a.bases = bases
}

// withBasesOnly is a fresh map holding the base of every instance and none of
// its modifiers, not yet introduced to a client.
// vanilla: ServerPlayer.restoreFrom, AttributeMap.assignBaseValues
func (a *Attributes) withBasesOnly() Attributes {
	var out Attributes
	out.initBases()
	for _, id := range packedAttributeOrder(a.insertionOrder[:a.instanceCount]) {
		out.Instance(id).Base = a.instances[id].Base
	}
	out.initStartingAttributes()
	return out
}

// Instance returns the attribute's instance, creating it at the owner's base.
// vanilla: AttributeMap.getInstance
func (a *Attributes) Instance(id registry.AttributeID) *AttributeInstance {
	a.init()
	if in := a.instances[id]; in != nil {
		if a.synthetic.has(id) {
			a.synthetic &^= 1 << id
			// Pairing made this instance early. Insert it at its first native use.
			order := a.insertionOrder[:a.instanceCount]
			i := slices.Index(order, uint8(id))
			copy(order[i:], order[i+1:])
			order[len(order)-1] = uint8(id)
		}
		return in
	}
	in := &AttributeInstance{Attribute: id.Attribute(), Base: a.bases[id]}
	a.instances[id] = in
	a.insertionOrder[a.instanceCount] = uint8(id)
	a.instanceCount++
	return in
}

// Instantiated reports whether the attribute has an instance.
func (a *Attributes) Instantiated(id registry.AttributeID) bool { return a.lookup(id) != nil }

// MarkSynthetic marks an existing instance as not yet created by vanilla; a
// save leaves it out until the body ticks or something instantiates it again.
func (a *Attributes) MarkSynthetic(id registry.AttributeID) {
	if a.lookup(id) != nil {
		a.synthetic |= 1 << id
	}
}

// lookup returns an existing instance or nil, creating none: a read must not
// add a default instance, or a syncable attribute without modifiers would
// reach the client at join.
func (a *Attributes) lookup(id registry.AttributeID) *AttributeInstance {
	a.init()
	return a.instances[id]
}

// Value is the attribute's current value, its owner's base if nothing has
// touched it. It makes no instance.
func (a *Attributes) Value(id registry.AttributeID) float64 {
	if in := a.lookup(id); in != nil {
		return in.Value()
	}
	return id.Attribute().Sanitize(a.bases[id])
}

// AddModifier puts a modifier on an attribute, replacing one of the same ID.
// vanilla: AttributeInstance.addOrUpdateTransientModifier
func (a *Attributes) AddModifier(id registry.AttributeID, m Modifier) {
	in := a.Instance(id)
	if i := slices.IndexFunc(in.Modifiers, func(o Modifier) bool { return o.ID == m.ID }); i >= 0 {
		if in.Modifiers[i] == m {
			return
		}
		in.Modifiers[i] = m
	} else {
		in.Modifiers = append(in.Modifiers, m)
	}
	a.markDirty(in)
}

// ReplaceModifier puts a modifier on an attribute; one of the same ID that
// differs is removed first, so the new one goes last.
// vanilla: MobEffect.removeAttributeModifiers, addAttributeModifiers
func (a *Attributes) ReplaceModifier(id registry.AttributeID, m Modifier) {
	in := a.Instance(id)
	if i := slices.IndexFunc(in.Modifiers, func(o Modifier) bool { return o.ID == m.ID }); i >= 0 {
		if in.Modifiers[i] == m {
			return
		}
		in.Modifiers = slices.Delete(in.Modifiers, i, i+1)
	}
	in.Modifiers = append(in.Modifiers, m)
	a.markDirty(in)
}

// RemoveModifier removes a modifier by ID, reporting whether it was there. It
// creates no instance, so an untouched attribute leaves the map unchanged.
// vanilla: AttributeInstance.removeModifier
func (a *Attributes) RemoveModifier(id registry.AttributeID, modID string) bool {
	in := a.lookup(id)
	if in == nil {
		return false
	}
	i := slices.IndexFunc(in.Modifiers, func(o Modifier) bool { return o.ID == modID })
	if i < 0 {
		return false
	}
	in.Modifiers = slices.Delete(in.Modifiers, i, i+1)
	a.markDirty(in)
	return true
}

func (a *Attributes) markDirty(in *AttributeInstance) {
	if in.Attribute.Syncable {
		a.dirty |= 1 << in.Attribute.ID
	}
}

// Introduce returns every syncable instance, for the client to start from,
// and from then on lets TakeDirty report changes.
func (a *Attributes) Introduce() []AttributeInstance {
	a.init()
	a.introduced = true
	a.dirty = 0
	return a.Syncable()
}

// TakeDirty returns and forgets copies of the syncable instances changed
// since the last call, by ID; nothing before Introduce.
// vanilla: AttributeMap.getAttributesToSync, cleared by ServerEntity
func (a *Attributes) TakeDirty() []AttributeInstance {
	a.init()
	if !a.introduced {
		return nil
	}
	dirty := a.dirty
	if dirty == 0 {
		return nil
	}
	a.dirty = 0
	return a.snapshot(func(in *AttributeInstance) bool { return dirty.has(registry.AttributeID(in.Attribute.ID)) })
}

// Syncable returns copies of every syncable instance by ID, for a client new
// to the player.
// vanilla: AttributeMap.getSyncableAttributes
func (a *Attributes) Syncable() []AttributeInstance {
	a.init()
	return a.snapshot(func(in *AttributeInstance) bool { return in.Attribute.Syncable })
}

// snapshot returns copies of the instances keep picks, by ID; nil for none.
func (a *Attributes) snapshot(keep func(*AttributeInstance) bool) []AttributeInstance {
	var out []AttributeInstance
	for _, in := range a.instances {
		if in != nil && keep(in) {
			out = append(out, AttributeInstance{Attribute: in.Attribute, Base: in.Base, Modifiers: slices.Clone(in.Modifiers)})
		}
	}
	return out
}

// playerAttributes are a player's supplier's attributes, the only ones a
// command may read or edit on one.
// vanilla: Player.createAttributes, LivingEntity.createLivingAttributes
var playerAttributes = func() attributeSet {
	var s attributeSet
	for _, id := range []registry.AttributeID{
		registry.AttributeMaxHealth,
		registry.AttributeKnockbackResistance,
		registry.AttributeMovementSpeed,
		registry.AttributeArmor,
		registry.AttributeArmorToughness,
		registry.AttributeMaxAbsorption,
		registry.AttributeStepHeight,
		registry.AttributeScale,
		registry.AttributeGravity,
		registry.AttributeSafeFallDistance,
		registry.AttributeFallDamageMultiplier,
		registry.AttributeJumpStrength,
		registry.AttributeEntityInteractionRange,
		registry.AttributeOxygenBonus,
		registry.AttributeBurningTime,
		registry.AttributeExplosionKnockbackResistance,
		registry.AttributeWaterMovementEfficiency,
		registry.AttributeMovementEfficiency,
		registry.AttributeAttackKnockback,
		registry.AttributeCameraDistance,
		registry.AttributeWaypointTransmitRange,
		registry.AttributeWaypointReceiveRange,
		registry.AttributeBounciness,
		registry.AttributeAirDragModifier,
		registry.AttributeFrictionModifier,
		registry.AttributeNameTagDistance,
		registry.AttributeBelowNameDistance,
		registry.AttributeAttackDamage,
		registry.AttributeAttackSpeed,
		registry.AttributeLuck,
		registry.AttributeBlockInteractionRange,
		registry.AttributeBlockBreakSpeed,
		registry.AttributeSubmergedMiningSpeed,
		registry.AttributeSneakingSpeed,
		registry.AttributeMiningEfficiency,
		registry.AttributeSweepingDamageRatio,
	} {
		s |= 1 << id
	}
	return s
}()

// Has reports whether a player carries the attribute at all.
// vanilla: AttributeMap.hasAttribute
func (a *Attributes) Has(id registry.AttributeID) bool {
	a.init()
	return a.has.has(id)
}

// Base is the attribute's base value, the player's own where nothing has
// changed it. vanilla: AttributeMap.getBaseValue
func (a *Attributes) Base(id registry.AttributeID) float64 {
	if in := a.lookup(id); in != nil {
		return in.Base
	}
	return a.bases[id]
}

// SetBase instantiates the attribute and sets its base; the same value marks no change.
// vanilla: AttributeInstance.setBaseValue
func (a *Attributes) SetBase(id registry.AttributeID, value float64) {
	if !a.Has(id) {
		return
	}
	in := a.Instance(id)
	if in.Base == value {
		return
	}
	in.Base = value
	a.markDirty(in)
}

// ResetBase puts the attribute's base back to the player's own, reporting
// whether a player has the attribute. vanilla: AttributeMap.resetBaseValue
func (a *Attributes) ResetBase(id registry.AttributeID) bool {
	if !a.Has(id) {
		return false
	}
	if in := a.lookup(id); in != nil {
		a.SetBase(id, a.bases[id])
	}
	return true
}

// Modifier is the attribute's modifier of that ID, if it has one.
// vanilla: AttributeMap.hasModifier, getModifierValue
func (a *Attributes) Modifier(id registry.AttributeID, modID string) (Modifier, bool) {
	in := a.lookup(id)
	if in == nil {
		return Modifier{}, false
	}
	i := slices.IndexFunc(in.Modifiers, func(m Modifier) bool { return m.ID == modID })
	if i < 0 {
		return Modifier{}, false
	}
	return in.Modifiers[i], true
}

// Modifiers are the IDs of the modifiers on the attribute, in the order they
// went on.
func (a *Attributes) Modifiers(id registry.AttributeID) []string {
	in := a.lookup(id)
	if in == nil {
		return nil
	}
	ids := make([]string, len(in.Modifiers))
	for i, m := range in.Modifiers {
		ids[i] = m.ID
	}
	return ids
}
