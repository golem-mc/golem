package registry

// Attribute is one minecraft:attribute entry: wire ID, default base, clamp
// range and whether clients are told of it. The registry is built into the
// client, so the IDs come from the reference server's report and the rest
// from vanilla's Attributes class; both move between versions.
type Attribute struct {
	Name     string
	ID       int32
	Default  float64
	Min, Max float64
	Syncable bool
}

// AttributeID is an attribute's registry ID, an index into Attributes.
type AttributeID int32

// Attribute is the registry entry of the ID.
func (id AttributeID) Attribute() Attribute { return Attributes[id] }

var attributesByName = func() map[string]Attribute {
	m := make(map[string]Attribute, len(Attributes))
	for _, a := range Attributes {
		m[a.Name] = a
	}
	return m
}()

// AttributeByName looks an attribute up by its registry name.
func AttributeByName(name string) (Attribute, bool) {
	a, ok := attributesByName[name]
	return a, ok
}

// Sanitize clamps a computed value into the attribute's range, NaN going to
// the minimum. vanilla: RangedAttribute.sanitizeValue
func (a Attribute) Sanitize(v float64) float64 {
	if v != v {
		return a.Min
	}
	return min(max(v, a.Min), a.Max)
}

// AttributeOperation is an attribute modifier's operation, numbered as
// AttributeModifier.Operation numbers it on the wire.
type AttributeOperation int32

const (
	AttributeAddValue AttributeOperation = iota
	AttributeAddMultipliedBase
	AttributeAddMultipliedTotal
)

var attributeOperationNames = [...]string{"add_value", "add_multiplied_base", "add_multiplied_total"}

// Name is the operation's word in commands and saved data.
// vanilla: AttributeModifier.Operation
func (o AttributeOperation) Name() string { return attributeOperationNames[o] }

// AttributeOperationByName is the operation a word names.
func AttributeOperationByName(name string) (AttributeOperation, bool) {
	for i, n := range attributeOperationNames {
		if n == name {
			return AttributeOperation(i), true
		}
	}
	return 0, false
}

// AttributeModifier is one entry of an item's minecraft:attribute_modifiers:
// the attribute it changes, the modifier's own ID and amount and operation,
// and the EquipmentSlotGroup it works in.
type AttributeModifier struct {
	Attribute AttributeID
	ID        string
	Amount    float64
	Operation AttributeOperation
	Slot      string
}

// SlotGroupHolds reports whether an EquipmentSlotGroup takes in an
// equipment slot, both by their serialized names.
// vanilla: EquipmentSlotGroup.test
func SlotGroupHolds(group, slot string) bool {
	switch group {
	case "any":
		return true
	case "hand":
		return slot == "mainhand" || slot == "offhand"
	case "armor":
		switch slot {
		case "feet", "legs", "chest", "head", "body":
			return true
		}
		return false
	}
	return group == slot
}

var slotGroupNames = [...]string{"any", "mainhand", "offhand", "hand", "feet", "legs", "chest", "head", "armor", "body", "saddle"}

// SlotGroupID is the wire ID of an EquipmentSlotGroup by its serialized name.
// vanilla: EquipmentSlotGroup.STREAM_CODEC
func SlotGroupID(name string) (int32, bool) {
	for i, n := range slotGroupNames {
		if n == name {
			return int32(i), true
		}
	}
	return 0, false
}
