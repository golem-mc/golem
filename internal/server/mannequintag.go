package server

import (
	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/effect"
	"github.com/golem-mc/golem/internal/server/entity"
	"github.com/golem-mc/golem/internal/server/rules"
	"github.com/golem-mc/golem/internal/server/world"
	"github.com/golem-mc/golem/nbt"
	"github.com/google/uuid"
)

// mannequinPoses are the poses a mannequin accepts, by saved name.
// vanilla: Mannequin.VALID_POSES
var mannequinPoses = map[string]int32{
	"standing":    protocol.PoseStanding,
	"crouching":   protocol.PoseSneaking,
	"swimming":    protocol.PoseSwimming,
	"fall_flying": protocol.PoseFallFlying,
	"sleeping":    protocol.PoseSleeping,
}

// mannequinParts are the PlayerModelPart names in bit order.
var mannequinParts = [...]string{
	"cape", "jacket", "left_sleeve", "right_sleeve", "left_pants_leg", "right_pants_leg", "hat",
}

// mannequinTag writes a mannequin as the compound an entity file holds.
// vanilla: Mannequin.addAdditionalSaveData
func mannequinTag(ent *mannequinEntity) *nbt.OrderedCompound {
	return mannequinTagAs(ent, mannequinEntityID)
}

func mannequinTagAs(ent *mannequinEntity, id string) *nbt.OrderedCompound {
	m := ent.mannequin
	tag := orderedEntityBaseTag(id, m.X, m.Y, m.Z, m.VX, m.VY, m.VZ, m.Yaw, m.Pitch, ent.uuid, m.OnGround, 0, m.FireTicks, 0)
	tag.Set("Air", nbt.Short(m.AirSupply))
	tag.Set("fall_distance", nbt.Double(m.FallDistance))
	tag.Set("Invulnerable", boolTag(m.Invulnerable))
	ent.custom.put(tag)
	if ent.look.silent {
		tag.Set("Silent", boolTag(true))
	}
	if m.NoGravity {
		tag.Set("NoGravity", boolTag(true))
	}
	if ent.look.glowing {
		tag.Set("Glowing", boolTag(true))
	}
	if m.TicksFrozen > 0 {
		tag.Set("TicksFrozen", nbt.Int(m.TicksFrozen))
	}
	if ent.look.visualFire {
		tag.Set("HasVisualFire", boolTag(true))
	}
	putTags(tag, ent.tags.Values())
	ent.look.putData(tag)
	tag.Set("Health", nbt.Float(m.Health))
	living := m.LivingState
	living.AbsorptionAmount = float32(m.Absorption)
	living.SaveWithAttributes(tag, func() {
		// vanilla: LivingEntity.addAdditionalSaveData always stores TAG_ATTRIBUTES.
		attrs := m.Attributes.Save(nil)
		if attrs == nil {
			attrs = &nbt.List{Elem: nbt.TypeCompound}
		}
		tag.Set("attributes", *attrs)
		if active := effect.Encode(m.AllEffects()); active != nil {
			tag.Set("active_effects", *active)
		}
	})
	equipment := &nbt.OrderedCompound{}
	writeHeldEquipment(equipment, m.MainHand, m.OffHand, m.Worn)
	if len(equipment.Entries()) > 0 {
		tag.Set("equipment", equipment)
	}
	profile, ok := protocol.OrderedProfileToTag(mannequinProfileWire(m))
	if !ok {
		profile = &nbt.OrderedCompound{}
	}
	tag.Set("profile", profile)
	var hidden []nbt.Tag
	for bit, name := range mannequinParts {
		if m.Layers&(1<<bit) == 0 {
			hidden = append(hidden, nbt.String(name))
		}
	}
	tag.Set("hidden_layers", nbt.List{Elem: nbt.TypeString, Items: hidden})
	if m.LeftHanded {
		tag.Set("main_hand", nbt.String("left"))
	} else {
		tag.Set("main_hand", nbt.String("right"))
	}
	for name, pose := range mannequinPoses {
		if pose == m.Pose {
			tag.Set("pose", nbt.String(name))
		}
	}
	tag.Set("immovable", boolTag(m.Immovable))
	switch {
	case m.HideLabel:
		tag.Set("hide_description", boolTag(true))
	case m.Label != nil:
		tag.Set("description", protocol.TextTag(*m.Label))
	}
	return tag
}

// mannequinOfTag builds a mannequin from its saved compound, nil for one
// that is not a mannequin, has no position or names an invalid pose.
// vanilla: Mannequin.readAdditionalSaveData
func mannequinOfTag(s *Server, w *world.World, compound nbt.Compound) *mannequinEntity {
	if id, _ := nbt.Get[nbt.String](compound, "id"); string(id) != mannequinEntityID {
		return nil
	}
	x, y, z, ok := entityPositionOfTag(compound)
	if !ok {
		return nil
	}
	identity, has := entityUUIDOfTag(compound)
	if !has {
		identity = uuid.New()
	}
	ent := &mannequinEntity{
		mannequin: entity.NewMannequin(s.nextEntity(), x, y, z),
		w:         w,
		uuid:      identity,
	}
	if !applyMannequinTag(ent, compound) {
		return nil
	}
	return ent
}

// applyMannequinTag loads a compound onto a mannequin: the base fields, then
// its own. false when the pose is invalid, which leaves the pose alone.
// vanilla: Entity.load, Mannequin.readAdditionalSaveData
func applyMannequinTag(ent *mannequinEntity, compound nbt.Compound) bool {
	m := ent.mannequin
	ent.needsSync = true
	if x, y, z, ok := entityPositionOfTag(compound); ok {
		m.SetPos(x, y, z)
	}
	m.Yaw, m.Pitch = entityRotationOfTag(compound)
	m.VX, m.VY, m.VZ = entityMotionOfTag(compound)
	m.OnGround = nbt.BoolField(compound, "OnGround")
	m.LoadFrom(compound)
	m.NoGravity = nbt.BoolField(compound, "NoGravity")
	m.FallDistance = nbt.DoubleFieldOr(compound, "fall_distance", 0)
	ent.tags.Reset(tagsOfTag(compound))
	ent.custom = customNameOfTag(compound)
	ent.look = entityLookOf(compound)
	equipment, _ := nbt.Get[nbt.Compound](compound, "equipment")
	m.MainHand, m.OffHand, m.Worn = heldEquipmentOf(equipment)
	if health, named := nbt.FloatField(compound, "Health"); named {
		m.Health = max(0, float64(health))
	}
	fire, _ := nbt.ShortField(compound, "Fire")
	m.FireTicks = int(fire)
	m.AirSupply = rules.MaxAirSupply
	if _, ok := compound["Air"]; ok {
		air, _ := nbt.IntField(compound, "Air")
		m.AirSupply = int(air)
	}
	frozen, _ := nbt.IntField(compound, "TicksFrozen")
	m.TicksFrozen = int(frozen)
	list, _ := nbt.Get[nbt.List](compound, "active_effects")
	m.RestoreEffects(effect.Decode(list))
	absorption, _ := nbt.FloatField(compound, "AbsorptionAmount")
	m.Absorption = max(0, float64(absorption))
	if attrs, ok := nbt.Get[nbt.List](compound, "attributes"); ok {
		m.Attributes.Load(attrs)
	}
	m.SyncEquipmentModifiers()
	rules.UpdateEffectAttributes(m.Attributes, m.AllEffects())
	m.Health = min(m.Health, m.HealthCap())
	m.Profile = mannequinProfileOfTag(compound["profile"])
	m.Layers = entity.MannequinAllLayers
	if list, ok := nbt.Get[nbt.List](compound, "hidden_layers"); ok {
		m.Layers = mannequinLayersOf(list)
	}
	hand, _ := nbt.Get[nbt.String](compound, "main_hand")
	m.LeftHanded = hand == "left"
	m.Immovable = nbt.BoolField(compound, "immovable")
	m.Invulnerable = nbt.BoolField(compound, "Invulnerable")
	m.HideLabel = nbt.BoolField(compound, "hide_description")
	m.Label = nil
	if label, ok := compound["description"]; ok && protocol.IsTextComponent(label) {
		if text, ok := protocol.StyledTextOf(label); ok {
			m.Label = &text
		}
	}
	m.Pose = protocol.PoseStanding
	if name, named := nbt.Get[nbt.String](compound, "pose"); named {
		pose, valid := mannequinPoses[string(name)]
		if !valid {
			return false
		}
		m.Pose = pose
	}
	return true
}

// mannequinProfileOfTag reads a profile compound or a bare player name, nil
// for anything the codec refuses.
// vanilla: ResolvableProfile.CODEC
func mannequinProfileOfTag(tag nbt.Tag) []byte {
	var compound nbt.Compound
	switch v := tag.(type) {
	case nbt.CompoundTag:
		compound = v.Entries()
	case nbt.String:
		compound = nbt.Compound{"name": v}
	default:
		return nil
	}
	wire, _ := protocol.ProfileFromTag(compound)
	return wire
}

// mannequinLayersOf is the shown mask once the listed parts are hidden; a
// list naming an unknown part is refused whole.
// vanilla: Mannequin.LAYERS_CODEC
func mannequinLayersOf(list nbt.List) byte {
	mask := byte(entity.MannequinAllLayers)
	for _, item := range list.Items {
		name, _ := item.(nbt.String)
		found := false
		for bit, part := range mannequinParts {
			if string(name) == part {
				mask &^= 1 << bit
				found = true
			}
		}
		if !found {
			return entity.MannequinAllLayers
		}
	}
	return mask
}
